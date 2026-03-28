package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ucirello/sgai/pkg/state"
)

type externalRepositoryForTest struct {
	Handle        string `json:"handle"`
	DirectoryName string `json:"directoryName"`
	Label         string `json:"label"`
	Title         string `json:"title"`
	Path          string `json:"path"`
	Mode          string `json:"mode"`
	RootHandle    string `json:"rootHandle,omitempty"`
	RootPath      string `json:"rootPath,omitempty"`
	ForkCount     int    `json:"forkCount,omitempty"`
}

type externalFactoryInfoForTest struct {
	Hostname       string                      `json:"hostname"`
	StartDirectory string                      `json:"startDirectory"`
	Repositories   []externalRepositoryForTest `json:"repositories"`
}

type externalSessionActionForTest struct {
	Workspace      externalRepositoryForTest `json:"workspace"`
	Status         string                    `json:"status"`
	Running        bool                      `json:"running"`
	Message        string                    `json:"message"`
	AlreadyRunning bool                      `json:"alreadyRunning,omitempty"`
}

type externalGoalEditLinkForTest struct {
	Workspace externalRepositoryForTest `json:"workspace"`
	URL       string                    `json:"url"`
}

type externalAttachResultForTest struct {
	Workspace externalRepositoryForTest `json:"workspace"`
	HasGoal   bool                      `json:"hasGoal"`
	Message   string                    `json:"message"`
}

type externalForkResultForTest struct {
	Workspace externalRepositoryForTest `json:"workspace"`
	Parent    externalRepositoryForTest `json:"parent"`
	CreatedAt string                    `json:"createdAt"`
	Message   string                    `json:"message"`
}

func connectExternalMCPClient(t *testing.T, server *Server, r *http.Request) *mcp.ClientSession {
	t.Helper()

	mcpServer, errBuild := buildExternalMCPServer(server, r)
	require.NoError(t, errBuild)
	ct, st := mcp.NewInMemoryTransports()
	_, errConnect := mcpServer.Connect(context.Background(), st, nil)
	require.NoError(t, errConnect)

	client := mcp.NewClient(newMCPImplementation("test-client"), nil)
	cs, errClient := client.Connect(context.Background(), ct, nil)
	require.NoError(t, errClient)
	t.Cleanup(func() { _ = cs.Close() })

	return cs
}

func callExternalTool(t *testing.T, cs *mcp.ClientSession, name string, args any) *mcp.CallToolResult {
	t.Helper()

	params := new(mcp.CallToolParams)
	params.Name = name
	params.Arguments = args
	result, errCall := cs.CallTool(context.Background(), params)
	require.NoError(t, errCall)

	return result
}

//nolint:ireturn // Generic test decoder returns the caller-selected structured payload type.
func decodeExternalStructuredContent[T any](t *testing.T, result *mcp.CallToolResult) T {
	t.Helper()

	var out T
	data, errMarshal := json.Marshal(result.StructuredContent)
	require.NoError(t, errMarshal)
	require.NoError(t, json.Unmarshal(data, &out))
	return out
}

func externalToolText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()

	for _, content := range result.Content {
		text, ok := content.(*mcp.TextContent)
		if ok {
			return text.Text
		}
	}
	t.Fatalf("expected text content in tool result")
	return ""
}

func findRepositoryByName(t *testing.T, repositories []externalRepositoryForTest, directoryName string) externalRepositoryForTest {
	t.Helper()

	for i := range repositories {
		repository := repositories[i]
		if repository.DirectoryName == directoryName {
			return repository
		}
	}
	require.FailNowf(t, "repository not found", "repository %q not found", directoryName)
	var repository externalRepositoryForTest
	return repository
}

func decodeExternalStructuredContentMap(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()

	var out map[string]any
	data, errMarshal := json.Marshal(result.StructuredContent)
	require.NoError(t, errMarshal)
	require.NoError(t, json.Unmarshal(data, &out))
	return out
}

func addWorkspaceForTest(t *testing.T, rootPath, forkPath string) {
	t.Helper()

	cmd := exec.Command("jj", "workspace", "add", forkPath)
	cmd.Dir = rootPath
	output, errAdd := cmd.CombinedOutput()
	require.NoError(t, errAdd, string(output))
}

func TestBuildExternalMCPServerExposesFirstWaveTools(t *testing.T) {
	server, _ := setupTestServer(t)
	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)

	result, errList := cs.ListTools(context.Background(), new(mcp.ListToolsParams))
	require.NoError(t, errList)

	toolNames := mcpToolNames(result.Tools)
	assert.True(t, slices.Contains(toolNames, "factory_info"))
	assert.True(t, slices.Contains(toolNames, "start_self_drive"))
	assert.True(t, slices.Contains(toolNames, "stop_workspace"))
	assert.True(t, slices.Contains(toolNames, "reset_workspace"))
	assert.True(t, slices.Contains(toolNames, "goal_edit_link"))
	assert.True(t, slices.Contains(toolNames, "fork_repository"))
	assert.True(t, slices.Contains(toolNames, "attach_repository"))
	assert.False(t, slices.Contains(toolNames, "hello world"))
}

func TestExternalMCPFactoryInfoReturnsHybridRepositoryIdentity(t *testing.T) {
	server, rootDir := setupTestServer(t)

	standaloneDir := setupTestWorkspace(t, server, rootDir, "solo-dir")
	require.NoError(t, os.WriteFile(filepath.Join(standaloneDir, "GOAL.md"), []byte("---\ntitle: Solo Title\n---\n# Solo"), 0o644))

	rootWorkspaceDir := filepath.Join(rootDir, "root-dir")
	require.NoError(t, os.MkdirAll(filepath.Join(rootWorkspaceDir, ".sgai"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rootWorkspaceDir, "GOAL.md"), []byte("---\ntitle: Root Title\n---\n# Root"), 0o644))

	forkWorkspaceDir := filepath.Join(rootDir, "fork-dir")
	require.NoError(t, os.MkdirAll(filepath.Join(forkWorkspaceDir, ".sgai"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(forkWorkspaceDir, "GOAL.md"), []byte("---\ntitle: Fork Title\n---\n# Fork"), 0o644))
	createForkFixture(t, rootWorkspaceDir, forkWorkspaceDir)
	attachWorkspaceFixture(t, server, rootWorkspaceDir, workspaceRoot)
	attachWorkspaceFixture(t, server, forkWorkspaceDir, workspaceFork)
	server.invalidateWorkspaceScanCache()

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "factory_info", struct{}{})
	require.False(t, result.IsError)

	info := decodeExternalStructuredContent[externalFactoryInfoForTest](t, result)
	assert.NotEmpty(t, info.Hostname)
	assert.Equal(t, rootDir, info.StartDirectory)

	standalone := findRepositoryByName(t, info.Repositories, "solo-dir")
	assert.Equal(t, "Solo Title [solo-dir]", standalone.Handle)
	assert.Equal(t, "Solo Title", standalone.Label)
	assert.Equal(t, "Solo Title", standalone.Title)
	assert.Equal(t, standaloneDir, standalone.Path)
	assert.Equal(t, "standalone", standalone.Mode)

	rootRepository := findRepositoryByName(t, info.Repositories, "root-dir")
	assert.Equal(t, "Root Title [root-dir]", rootRepository.Handle)
	assert.Equal(t, "Root Title", rootRepository.Label)
	assert.Equal(t, resolveSymlinks(rootWorkspaceDir), rootRepository.Path)
	assert.Equal(t, "root", rootRepository.Mode)
	assert.Equal(t, 1, rootRepository.ForkCount)

	forkRepository := findRepositoryByName(t, info.Repositories, "fork-dir")
	assert.Equal(t, "Root Title/Fork Title [fork-dir]", forkRepository.Handle)
	assert.Equal(t, "Root Title/Fork Title", forkRepository.Label)
	assert.Equal(t, resolveSymlinks(forkWorkspaceDir), forkRepository.Path)
	assert.Equal(t, "fork", forkRepository.Mode)
	assert.Equal(t, rootRepository.Handle, forkRepository.RootHandle)
	assert.Equal(t, rootRepository.Path, forkRepository.RootPath)
}

func TestExternalMCPFactoryInfoOmitsRoutedNameField(t *testing.T) {
	server, rootDir := setupTestServer(t)
	workspaceDir := setupTestWorkspace(t, server, rootDir, "solo-dir")
	require.NoError(t, os.WriteFile(filepath.Join(workspaceDir, "GOAL.md"), []byte("---\ntitle: Solo Title\n---\n# Solo"), 0o644))

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "factory_info", struct{}{})
	require.False(t, result.IsError)

	payload := decodeExternalStructuredContentMap(t, result)
	repositories, ok := payload["repositories"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, repositories)

	repository, ok := repositories[0].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, repository, "routedName")
}

func TestExternalMCPFactoryInfoPreservesForkModeWithoutAttachedRoot(t *testing.T) {
	server, _ := setupTestServer(t)
	rootWorkspaceDir := filepath.Join(t.TempDir(), "root-only-parent")
	forkWorkspaceDir := filepath.Join(t.TempDir(), "fork-only-child")
	require.NoError(t, os.MkdirAll(filepath.Join(forkWorkspaceDir, ".sgai"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(forkWorkspaceDir, "GOAL.md"), []byte("---\ntitle: Fork Only\n---\n# Fork"), 0o644))
	createForkFixture(t, rootWorkspaceDir, forkWorkspaceDir)
	attachWorkspaceFixture(t, server, forkWorkspaceDir, workspaceFork)
	server.invalidateWorkspaceScanCache()

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "factory_info", struct{}{})
	require.False(t, result.IsError)

	info := decodeExternalStructuredContent[externalFactoryInfoForTest](t, result)
	forkRepository := findRepositoryByName(t, info.Repositories, "fork-only-child")
	assert.Equal(t, "fork", forkRepository.Mode)
	assert.Equal(t, resolveSymlinks(forkWorkspaceDir), forkRepository.Path)
}

func TestExternalMCPGoalEditLinkUsesBasenameOnlyPathWithoutWorkspaceDir(t *testing.T) {
	server, rootDir := setupTestServer(t)
	workspaceDir := setupTestWorkspace(t, server, rootDir, "goal-ws")
	require.NoError(t, os.WriteFile(filepath.Join(workspaceDir, "GOAL.md"), []byte("---\ntitle: Goal Title\n---\n# Goal"), 0o644))

	req := httptestNewRequest(t, "http://127.0.0.1/mcp/external")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "10.10.0.7:9443")

	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "goal_edit_link", externalTargetArgs{Target: "goal-ws"})
	require.False(t, result.IsError)

	link := decodeExternalStructuredContent[externalGoalEditLinkForTest](t, result)
	assert.Equal(t, "https://10.10.0.7:9443/workspaces/goal-ws/goal/edit", link.URL)
	assert.NotContains(t, link.URL, "workspaceDir=")
	assert.Equal(t, "Goal Title [goal-ws]", link.Workspace.Handle)
	assert.Equal(t, workspaceDir, link.Workspace.Path)
}

func TestExternalMCPGoalEditLinkBracketsIPv6Hosts(t *testing.T) {
	server, rootDir := setupTestServer(t)
	workspaceDir := setupTestWorkspace(t, server, rootDir, "ipv6-goal-ws")
	require.NoError(t, os.WriteFile(filepath.Join(workspaceDir, "GOAL.md"), []byte("---\ntitle: IPv6 Goal\n---\n# Goal"), 0o644))

	req := httptestNewRequest(t, "http://[::1]/mcp/external")
	req.Host = "::1"

	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "goal_edit_link", externalTargetArgs{Target: "ipv6-goal-ws"})
	require.False(t, result.IsError)

	link := decodeExternalStructuredContent[externalGoalEditLinkForTest](t, result)
	assert.Equal(t, "http://[::1]/workspaces/ipv6-goal-ws/goal/edit", link.URL)
}

func TestExternalMCPGoalEditLinkRejectsAmbiguousWorkspaceTarget(t *testing.T) {
	server, _ := setupTestServer(t)
	firstDir := filepath.Join(t.TempDir(), "first", "shared-ws")
	secondDir := filepath.Join(t.TempDir(), "second", "shared-ws")
	require.NoError(t, os.MkdirAll(filepath.Join(firstDir, ".sgai"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(secondDir, ".sgai"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(firstDir, "GOAL.md"), []byte("---\ntitle: First Shared\n---\n# First"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(secondDir, "GOAL.md"), []byte("---\ntitle: Second Shared\n---\n# Second"), 0o644))

	server.mu.Lock()
	server.externalDirs[resolveSymlinks(firstDir)] = true
	server.externalDirs[resolveSymlinks(secondDir)] = true
	server.mu.Unlock()
	server.invalidateWorkspaceScanCache()

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "goal_edit_link", externalTargetArgs{Target: "shared-ws"})
	assert.True(t, result.IsError)
	assert.Contains(t, externalToolText(t, result), "matches multiple attached repositories")
	assert.Contains(t, externalToolText(t, result), "absolute path")
}

func TestExternalMCPFactoryInfoKeepsBasenameForDuplicateBasenames(t *testing.T) {
	server, _ := setupTestServer(t)
	firstDir := filepath.Join(t.TempDir(), "first", "shared-ws")
	secondDir := filepath.Join(t.TempDir(), "second", "shared-ws")
	require.NoError(t, os.MkdirAll(filepath.Join(firstDir, ".sgai"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(secondDir, ".sgai"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(firstDir, "GOAL.md"), []byte("---\ntitle: First Shared\n---\n# First"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(secondDir, "GOAL.md"), []byte("---\ntitle: Second Shared\n---\n# Second"), 0o644))

	server.mu.Lock()
	server.externalDirs[resolveSymlinks(firstDir)] = true
	server.externalDirs[resolveSymlinks(secondDir)] = true
	server.mu.Unlock()
	server.invalidateWorkspaceScanCache()

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "factory_info", struct{}{})
	require.False(t, result.IsError)

	info := decodeExternalStructuredContent[externalFactoryInfoForTest](t, result)
	resolvedFirstDir := resolveSymlinks(firstDir)
	resolvedSecondDir := resolveSymlinks(secondDir)

	var firstRepository externalRepositoryForTest
	var secondRepository externalRepositoryForTest
	for _, repository := range info.Repositories {
		switch repository.Path {
		case resolvedFirstDir:
			firstRepository = repository
		case resolvedSecondDir:
			secondRepository = repository
		}
	}

	assert.Equal(t, "shared-ws", firstRepository.DirectoryName)
	assert.Equal(t, "shared-ws", secondRepository.DirectoryName)
}

func TestExternalMCPGoalEditLinkRejectsAmbiguousAbsolutePathTarget(t *testing.T) {
	server, _ := setupTestServer(t)
	firstDir := filepath.Join(t.TempDir(), "first", "shared-ws")
	secondDir := filepath.Join(t.TempDir(), "second", "shared-ws")
	require.NoError(t, os.MkdirAll(filepath.Join(firstDir, ".sgai"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(secondDir, ".sgai"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(firstDir, "GOAL.md"), []byte("---\ntitle: First Shared\n---\n# First"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(secondDir, "GOAL.md"), []byte("---\ntitle: Second Shared\n---\n# Second"), 0o644))

	server.mu.Lock()
	server.externalDirs[resolveSymlinks(firstDir)] = true
	server.externalDirs[resolveSymlinks(secondDir)] = true
	server.mu.Unlock()
	server.invalidateWorkspaceScanCache()

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "goal_edit_link", externalTargetArgs{Target: firstDir})
	assert.True(t, result.IsError)
	assert.Contains(t, externalToolText(t, result), "basename")
	assert.Contains(t, externalToolText(t, result), "ambiguous")
}

func TestExternalMCPFormerRootBecomesStandaloneAfterLastForkRemoved(t *testing.T) {
	server, rootDir := setupTestServer(t)
	workspaceDir := setupTestWorkspace(t, server, rootDir, "former-root")
	require.NoError(t, initializeWorkspace(workspaceDir))
	addWorkspaceForTest(t, workspaceDir, filepath.Join(rootDir, "former-root-fork"))
	require.NoError(t, forgetWorkspace(workspaceDir, "former-root-fork"))
	server.invalidateWorkspaceScanCache()
	server.classifyCache.delete(workspaceDir)
	server.classifyCache.delete(resolveSymlinks(workspaceDir))

	server.mu.Lock()
	runningSession := new(session)
	runningSession.running = true
	server.sessions[workspaceDir] = runningSession
	server.mu.Unlock()

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	infoResult := callExternalTool(t, cs, "factory_info", struct{}{})
	require.False(t, infoResult.IsError)

	info := decodeExternalStructuredContent[externalFactoryInfoForTest](t, infoResult)
	repository := findRepositoryByName(t, info.Repositories, "former-root")
	assert.Equal(t, "standalone", repository.Mode)

	startResult := callExternalTool(t, cs, "start_self_drive", externalTargetArgs{Target: workspaceDir})
	require.False(t, startResult.IsError)
	action := decodeExternalStructuredContent[externalSessionActionForTest](t, startResult)
	assert.True(t, action.AlreadyRunning)
	assert.Equal(t, "running", action.Status)
	assert.Equal(t, "session already running", action.Message)
}

func TestExternalMCPStartSelfDriveReportsAlreadyRunningSession(t *testing.T) {
	server, rootDir := setupTestServer(t)
	workspaceDir := setupTestWorkspace(t, server, rootDir, "run-ws")

	server.mu.Lock()
	runningSession := new(session)
	runningSession.running = true
	server.sessions[workspaceDir] = runningSession
	server.mu.Unlock()

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "start_self_drive", externalTargetArgs{Target: "run-ws"})
	require.False(t, result.IsError)

	action := decodeExternalStructuredContent[externalSessionActionForTest](t, result)
	assert.True(t, action.AlreadyRunning)
	assert.True(t, action.Running)
	assert.Equal(t, "running", action.Status)
	assert.Equal(t, "session already running", action.Message)
	assert.Equal(t, "run-ws", action.Workspace.DirectoryName)
}

func TestExternalMCPStopWorkspaceIsIdempotent(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "stop-ws")

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "stop_workspace", externalTargetArgs{Target: "stop-ws"})
	require.False(t, result.IsError)

	action := decodeExternalStructuredContent[externalSessionActionForTest](t, result)
	assert.False(t, action.Running)
	assert.Equal(t, "stopped", action.Status)
	assert.Equal(t, "session already stopped", action.Message)
}

func TestExternalMCPResetWorkspaceResetsStoppedState(t *testing.T) {
	server, rootDir := setupTestServer(t)
	workspaceDir := setupTestWorkspace(t, server, rootDir, "reset-ws")

	workflow := newTestWorkflow()
	workflow.Status = state.StatusWorking
	coord, errCoord := state.NewCoordinatorWith(filepath.Join(workspaceDir, ".sgai", "state.json"), workflow)
	require.NoError(t, errCoord)

	server.mu.Lock()
	stoppedSession := new(session)
	stoppedSession.coord = coord
	stoppedSession.running = false
	server.sessions[workspaceDir] = stoppedSession
	server.mu.Unlock()

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "reset_workspace", externalTargetArgs{Target: "reset-ws"})
	require.False(t, result.IsError)

	action := decodeExternalStructuredContent[externalSessionActionForTest](t, result)
	assert.False(t, action.Running)
	assert.Equal(t, state.StatusComplete, action.Status)
	assert.Equal(t, "session reset successfully", action.Message)
	assert.Equal(t, state.StatusComplete, workflowStateFromDisk(t, workspaceDir).Status)
}

func TestExternalMCPForkRepositoryRejectsForkTargetsWithGuidance(t *testing.T) {
	server, rootDir := setupTestServer(t)
	rootWorkspaceDir := filepath.Join(rootDir, "root-fork-guidance")
	require.NoError(t, os.MkdirAll(filepath.Join(rootWorkspaceDir, ".sgai"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rootWorkspaceDir, "GOAL.md"), []byte("---\ntitle: Root Guidance\n---\n# Root"), 0o644))

	forkWorkspaceDir := filepath.Join(rootDir, "fork-guidance")
	require.NoError(t, os.MkdirAll(filepath.Join(forkWorkspaceDir, ".sgai"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(forkWorkspaceDir, "GOAL.md"), []byte("---\ntitle: Fork Guidance\n---\n# Fork"), 0o644))
	createForkFixture(t, rootWorkspaceDir, forkWorkspaceDir)
	attachWorkspaceFixture(t, server, rootWorkspaceDir, workspaceRoot)
	attachWorkspaceFixture(t, server, forkWorkspaceDir, workspaceFork)
	server.invalidateWorkspaceScanCache()

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "fork_repository", externalForkArgs{Target: forkWorkspaceDir, GoalContent: "---\nflow: |\n  \"a\" -> \"b\"\n---\n# New Goal"})
	assert.True(t, result.IsError)
	assert.Contains(t, externalToolText(t, result), "already a fork workspace")
	assert.Contains(t, externalToolText(t, result), "standalone repository or a root repository")
}

func TestExternalMCPForkRepositoryCreatesSiblingWorkspace(t *testing.T) {
	server, rootDir := setupTestServer(t)
	workspaceDir := setupTestWorkspace(t, server, rootDir, "forkable-ws")
	require.NoError(t, initializeWorkspace(workspaceDir))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceDir, "GOAL.md"), []byte("---\ntitle: Forkable\n---\n# Goal"), 0o644))

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "fork_repository", externalForkArgs{Target: workspaceDir, GoalContent: "---\nflow: |\n  \"a\" -> \"b\"\n---\n# Fork Goal"})
	require.False(t, result.IsError)

	forkResult := decodeExternalStructuredContent[externalForkResultForTest](t, result)
	assert.Equal(t, "forkable-ws", forkResult.Parent.DirectoryName)
	assert.Equal(t, filepath.Dir(workspaceDir), filepath.Dir(forkResult.Workspace.Path))
	assert.DirExists(t, forkResult.Workspace.Path)
	assert.Equal(t, "fork", forkResult.Workspace.Mode)
	assert.NotEmpty(t, forkResult.CreatedAt)
}

func TestExternalMCPAttachRepositoryRequiresAbsolutePath(t *testing.T) {
	server, _ := setupTestServer(t)
	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "attach_repository", externalAttachArgs{Path: "relative/path"})
	assert.True(t, result.IsError)
	assert.Contains(t, externalToolText(t, result), "absolute on-disk path")
}

func TestExternalMCPAttachRepositoryAttachesWorkspace(t *testing.T) {
	server, _ := setupTestServer(t)
	externalDir := filepath.Join(t.TempDir(), "attach-me")
	require.NoError(t, os.MkdirAll(externalDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(externalDir, "GOAL.md"), []byte("---\ntitle: Attached Goal\n---\n# Goal"), 0o644))

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "attach_repository", externalAttachArgs{Path: externalDir})
	require.False(t, result.IsError)

	attachResult := decodeExternalStructuredContent[externalAttachResultForTest](t, result)
	assert.True(t, attachResult.HasGoal)
	assert.Equal(t, resolveSymlinks(externalDir), attachResult.Workspace.Path)
	assert.Equal(t, "Attached Goal [attach-me]", attachResult.Workspace.Handle)
	assert.Equal(t, "standalone", attachResult.Workspace.Mode)
}

func httptestNewRequest(t *testing.T, rawURL string) *http.Request {
	t.Helper()

	req, errRequest := http.NewRequest(http.MethodPost, rawURL, http.NoBody)
	require.NoError(t, errRequest)
	return req
}
