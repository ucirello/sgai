package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ucirello/sgai/pkg/state"
)

type externalRepositoryForTest struct {
	Handle        string   `json:"handle"`
	DirectoryName string   `json:"directoryName"`
	Label         string   `json:"label"`
	Title         string   `json:"title"`
	Path          string   `json:"path"`
	Mode          string   `json:"mode"`
	RootHandle    string   `json:"rootHandle,omitempty"`
	RootPath      string   `json:"rootPath,omitempty"`
	ForkCount     int      `json:"forkCount,omitempty"`
	ActiveAgents  []string `json:"activeAgents"`
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
	RunningMode    string                    `json:"runningMode,omitempty"`
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

type externalWorkspaceArgsForTest struct {
	Workspace string `json:"workspace"`
}

type externalPendingQuestionListForTest struct {
	Workspaces []externalRepositoryForTest `json:"workspaces"`
}

type externalQuestionItemForTest struct {
	Question    string   `json:"question"`
	Choices     []string `json:"choices"`
	MultiSelect bool     `json:"multiSelect"`
}

type externalPendingQuestionForTest struct {
	PromptToken string                        `json:"promptToken"`
	Type        string                        `json:"type"`
	AgentName   string                        `json:"agentName"`
	Message     string                        `json:"message"`
	Questions   []externalQuestionItemForTest `json:"questions,omitempty"`
}

type externalPendingQuestionResultForTest struct {
	Workspace       externalRepositoryForTest      `json:"workspace"`
	PendingQuestion externalPendingQuestionForTest `json:"pendingQuestion"`
}

type externalAnswerPendingQuestionArgsForTest struct {
	Workspace       string   `json:"workspace"`
	PromptToken     string   `json:"promptToken"`
	Answer          string   `json:"answer"`
	SelectedChoices []string `json:"selectedChoices"`
}

type externalAnswerPendingQuestionResultForTest struct {
	Workspace externalRepositoryForTest `json:"workspace"`
	Success   bool                      `json:"success"`
	Message   string                    `json:"message"`
}

type externalSteerNextTurnArgsForTest struct {
	Workspace string `json:"workspace"`
	Message   string `json:"message"`
}

type externalSteerNextTurnResultForTest struct {
	Workspace externalRepositoryForTest `json:"workspace"`
	Success   bool                      `json:"success"`
	Message   string                    `json:"message"`
}

type externalWorkflowLinksForTest struct {
	Progress string `json:"progress"`
	GoalEdit string `json:"goalEdit"`
	Respond  string `json:"respond,omitempty"`
}

type externalWorkflowLinksResultForTest struct {
	Workspace externalRepositoryForTest `json:"workspace"`
	Links     externalWorkflowLinksForTest
}

type externalLogEntryForTest struct {
	Prefix string `json:"prefix"`
	Text   string `json:"text"`
}

type externalWorkspaceLogResultForTest struct {
	Workspace externalRepositoryForTest `json:"workspace"`
	Log       []externalLogEntryForTest `json:"log"`
}

type externalMessageForTest struct {
	ID        int    `json:"id"`
	FromAgent string `json:"fromAgent"`
	ToAgent   string `json:"toAgent"`
	Body      string `json:"body"`
	Read      bool   `json:"read"`
	ReadAt    string `json:"readAt,omitempty"`
	ReadBy    string `json:"readBy,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}

type externalTodoForTest struct {
	ID       string `json:"id"`
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}

type externalAgentTodoSectionForTest struct {
	Agent string                `json:"agent"`
	Todos []externalTodoForTest `json:"todos"`
}

type externalWorkspaceMessagesAndTodosForTest struct {
	Workspace               externalRepositoryForTest         `json:"workspace"`
	Messages                []externalMessageForTest          `json:"messages"`
	ProjectTodos            []externalTodoForTest             `json:"projectTodos"`
	ActiveAgentTodoSections []externalAgentTodoSectionForTest `json:"activeAgentTodoSections"`
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

func findRepositoryMapByPath(t *testing.T, repositories []any, workspacePath string) map[string]any {
	t.Helper()

	for _, item := range repositories {
		repository, ok := item.(map[string]any)
		require.True(t, ok)
		path, ok := repository["path"].(string)
		require.True(t, ok)
		if path == workspacePath {
			return repository
		}
	}

	require.FailNowf(t, "repository not found", "repository %q not found", workspacePath)
	return nil
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
	slices.Sort(toolNames)
	expectedToolNames := []string{
		"answer_pending_question",
		"attach_repository",
		"factory_info",
		"fork_repository",
		"get_pending_question",
		"goal_edit_link",
		"list_pending_questions",
		"peek_workspace_log",
		"reset_workspace",
		"start_continuous",
		"start_interactive",
		"start_self_drive",
		"steer_next_turn",
		"stop_workspace",
		"workflow_links",
		"workspace_messages_and_todos",
	}
	assert.Equal(t, expectedToolNames, toolNames)
	assert.False(t, slices.Contains(toolNames, "inspect_workspace"))
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
	assert.Equal(t, "Solo Title", standalone.Handle)
	assert.Equal(t, "Solo Title", standalone.Label)
	assert.Equal(t, "Solo Title", standalone.Title)
	assert.Equal(t, standaloneDir, standalone.Path)
	assert.Equal(t, "standalone", standalone.Mode)

	rootRepository := findRepositoryByName(t, info.Repositories, "root-dir")
	assert.Equal(t, "root-dir", rootRepository.Handle)
	assert.Equal(t, "root-dir", rootRepository.Label)
	assert.Equal(t, resolveSymlinks(rootWorkspaceDir), rootRepository.Path)
	assert.Equal(t, "root", rootRepository.Mode)
	assert.Equal(t, 1, rootRepository.ForkCount)

	forkRepository := findRepositoryByName(t, info.Repositories, "fork-dir")
	assert.Equal(t, "root-dir/Fork Title", forkRepository.Handle)
	assert.Equal(t, "root-dir/Fork Title", forkRepository.Label)
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
	assert.NotContains(t, repository, "displayLabel")
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
	assert.Equal(t, "Goal Title", link.Workspace.Handle)
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

func TestExternalMCPFactoryInfoUsesDashboardDisambiguationAndNormalizedActiveAgents(t *testing.T) {
	server, _ := setupTestServer(t)
	firstDir := filepath.Join(t.TempDir(), "first", "shared-ws")
	secondDir := filepath.Join(t.TempDir(), "second", "shared-ws")
	activeDir := filepath.Join(t.TempDir(), "active-parent", "active-ws")
	require.NoError(t, os.MkdirAll(filepath.Join(firstDir, ".sgai"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(secondDir, ".sgai"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(activeDir, ".sgai"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(firstDir, "GOAL.md"), []byte("---\ntitle: Shared Title\n---\n# First"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(secondDir, "GOAL.md"), []byte("---\ntitle: Shared Title\n---\n# Second"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(activeDir, "GOAL.md"), []byte("---\ntitle: Active Title\n---\n# Active"), 0o644))

	server.mu.Lock()
	server.externalDirs[resolveSymlinks(firstDir)] = true
	server.externalDirs[resolveSymlinks(secondDir)] = true
	server.externalDirs[resolveSymlinks(activeDir)] = true
	server.mu.Unlock()
	server.invalidateWorkspaceScanCache()

	attachRunningSessionCoordinator(t, server, resolveSymlinks(activeDir), workflowRef(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.CurrentAgent = "go-developer, react-developer"
		workflow.InteractionMode = state.ModeBrainstorming
	}))

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "factory_info", struct{}{})
	require.False(t, result.IsError)

	info := decodeExternalStructuredContent[externalFactoryInfoForTest](t, result)
	payload := decodeExternalStructuredContentMap(t, result)
	repositories, ok := payload["repositories"].([]any)
	require.True(t, ok)
	var firstRepository externalRepositoryForTest
	var secondRepository externalRepositoryForTest
	activeRepository := findRepositoryByName(t, info.Repositories, "active-ws")
	for _, repository := range info.Repositories {
		switch repository.Path {
		case resolveSymlinks(firstDir):
			firstRepository = repository
		case resolveSymlinks(secondDir):
			secondRepository = repository
		}
	}

	assert.Equal(t, "Shared Title · first", firstRepository.Handle)
	assert.Equal(t, "Shared Title · first", firstRepository.Label)
	assert.Equal(t, "Shared Title · second", secondRepository.Handle)
	assert.Equal(t, "Shared Title · second", secondRepository.Label)
	assert.Equal(t, []string{"go-developer", "react-developer"}, activeRepository.ActiveAgents)

	firstRepositoryMap := findRepositoryMapByPath(t, repositories, resolveSymlinks(firstDir))
	secondRepositoryMap := findRepositoryMapByPath(t, repositories, resolveSymlinks(secondDir))
	activeRepositoryMap := findRepositoryMapByPath(t, repositories, resolveSymlinks(activeDir))
	assert.NotContains(t, firstRepositoryMap, "displayLabel")
	assert.NotContains(t, secondRepositoryMap, "displayLabel")
	firstActiveAgents, ok := firstRepositoryMap["activeAgents"].([]any)
	require.True(t, ok)
	assert.Empty(t, firstActiveAgents)
	secondActiveAgents, ok := secondRepositoryMap["activeAgents"].([]any)
	require.True(t, ok)
	assert.Empty(t, secondActiveAgents)
	activeAgents, ok := activeRepositoryMap["activeAgents"].([]any)
	require.True(t, ok)
	assert.Len(t, activeAgents, 2)
}

func TestExternalMCPStopWorkspaceAcceptsAuthoritativeDashboardLabel(t *testing.T) {
	server, _ := setupTestServer(t)
	firstDir := filepath.Join(t.TempDir(), "first", "shared-ws")
	secondDir := filepath.Join(t.TempDir(), "second", "shared-ws")
	require.NoError(t, os.MkdirAll(filepath.Join(firstDir, ".sgai"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(secondDir, ".sgai"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(firstDir, "GOAL.md"), []byte("---\ntitle: Shared Title\n---\n# First"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(secondDir, "GOAL.md"), []byte("---\ntitle: Shared Title\n---\n# Second"), 0o644))

	server.mu.Lock()
	server.externalDirs[resolveSymlinks(firstDir)] = true
	server.externalDirs[resolveSymlinks(secondDir)] = true
	server.mu.Unlock()
	server.invalidateWorkspaceScanCache()

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	infoResult := callExternalTool(t, cs, "factory_info", struct{}{})
	require.False(t, infoResult.IsError)

	info := decodeExternalStructuredContent[externalFactoryInfoForTest](t, infoResult)
	var firstRepository externalRepositoryForTest
	for _, repository := range info.Repositories {
		if repository.Path == resolveSymlinks(firstDir) {
			firstRepository = repository
			break
		}
	}
	require.Equal(t, "Shared Title · first", firstRepository.Label)

	result := callExternalTool(t, cs, "stop_workspace", externalTargetArgs{Target: firstRepository.Label})
	require.False(t, result.IsError)

	action := decodeExternalStructuredContent[externalSessionActionForTest](t, result)
	assert.Equal(t, resolveSymlinks(firstDir), action.Workspace.Path)
	assert.Equal(t, "Shared Title · first", action.Workspace.Label)
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

	attachRunningSessionCoordinator(t, server, workspaceDir, workflowRef(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.CurrentAgent = "coordinator"
		workflow.InteractionMode = state.ModeBrainstorming
	}))

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
	assert.Equal(t, "interactive", action.RunningMode)
	assert.Equal(t, "workspace already running in interactive mode", action.Message)
}

func TestExternalMCPStartSelfDriveReportsAlreadyRunningSession(t *testing.T) {
	server, rootDir := setupTestServer(t)
	workspaceDir := setupTestWorkspace(t, server, rootDir, "run-ws")
	attachRunningSessionCoordinator(t, server, workspaceDir, workflowRef(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.CurrentAgent = "coordinator"
		workflow.InteractionMode = state.ModeBrainstorming
	}))

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "start_self_drive", externalTargetArgs{Target: "run-ws"})
	require.False(t, result.IsError)

	action := decodeExternalStructuredContent[externalSessionActionForTest](t, result)
	assert.True(t, action.AlreadyRunning)
	assert.True(t, action.Running)
	assert.Equal(t, "running", action.Status)
	assert.Equal(t, "interactive", action.RunningMode)
	assert.Equal(t, "workspace already running in interactive mode", action.Message)
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
	result := callExternalTool(t, cs, "fork_repository", externalForkArgs{Target: forkWorkspaceDir, GoalContent: "---\nflow: |\n  \"a\" -> \"b\"\n---\n# New Goal", Title: "Fork Attempt"})
	assert.True(t, result.IsError)
	assert.Contains(t, externalToolText(t, result), "already a fork workspace")
	assert.Contains(t, externalToolText(t, result), "standalone repository or a root repository")
}

func TestExternalMCPForkRepositoryRequiresTitle(t *testing.T) {
	server, rootDir := setupTestServer(t)
	workspaceDir := setupTestWorkspace(t, server, rootDir, "fork-title-required")
	require.NoError(t, initializeWorkspace(workspaceDir))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceDir, "GOAL.md"), []byte("---\ntitle: Root Goal\nflow: |\n  \"a\" -> \"b\"\n---\n# Goal"), 0o644))

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "fork_repository", externalForkArgs{Target: workspaceDir, GoalContent: "# Fork Goal", Title: ""})
	assert.True(t, result.IsError)
	assert.Contains(t, externalToolText(t, result), "title")
	assert.Contains(t, externalToolText(t, result), "required")
}

func TestExternalMCPForkRepositorySchemaRequiresTitleField(t *testing.T) {
	server, rootDir := setupTestServer(t)
	workspaceDir := setupTestWorkspace(t, server, rootDir, "fork-title-schema")
	require.NoError(t, initializeWorkspace(workspaceDir))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceDir, "GOAL.md"), []byte("---\ntitle: Root Goal\nflow: |\n  \"a\" -> \"b\"\n---\n# Goal"), 0o644))

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	params := new(mcp.CallToolParams)
	params.Name = "fork_repository"
	params.Arguments = map[string]any{"target": workspaceDir, "goalContent": "# Fork Goal"}
	_, errCall := cs.CallTool(context.Background(), params)
	require.Error(t, errCall)
	assert.Contains(t, errCall.Error(), "missing properties")
	assert.Contains(t, errCall.Error(), "title")
}

func TestExternalMCPForkRepositoryCreatesSiblingWorkspace(t *testing.T) {
	server, rootDir := setupTestServer(t)
	workspaceDir := setupTestWorkspace(t, server, rootDir, "forkable-ws")
	require.NoError(t, initializeWorkspace(workspaceDir))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceDir, "GOAL.md"), []byte("---\ntitle: Forkable\n---\n# Goal"), 0o644))

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "fork_repository", externalForkArgs{Target: workspaceDir, GoalContent: "---\nflow: |\n  \"a\" -> \"b\"\n---\n# Fork Goal", Title: "Forkable Copy"})
	require.False(t, result.IsError)

	forkResult := decodeExternalStructuredContent[externalForkResultForTest](t, result)
	assert.Equal(t, "forkable-ws", forkResult.Parent.DirectoryName)
	assert.Equal(t, filepath.Dir(workspaceDir), filepath.Dir(forkResult.Workspace.Path))
	assert.DirExists(t, forkResult.Workspace.Path)
	assert.Equal(t, "fork", forkResult.Workspace.Mode)
	assert.NotEmpty(t, forkResult.CreatedAt)
}

func TestExternalMCPForkRepositoryOverridesQuotedCopiedTitleAndPreservesSubmittedBodyText(t *testing.T) {
	server, rootDir := setupTestServer(t)
	workspaceDir := setupTestWorkspace(t, server, rootDir, "fork-title-override")
	require.NoError(t, initializeWorkspace(workspaceDir))

	rootGoalContent := strings.Join([]string{
		"---",
		"\"title\": Root Goal",
		"flow: |",
		"  \"coordinator\" -> \"go-developer\"",
		"models:",
		"  coordinator: root-model",
		"---",
		"",
		"# Root Goal",
	}, "\n")
	require.NoError(t, os.WriteFile(filepath.Join(workspaceDir, "GOAL.md"), []byte(rootGoalContent), 0o644))

	submittedGoalContent := strings.Join([]string{
		"",
		" \t",
		"---",
		"title: Ignored Submitted Title",
		"flow: |",
		"  \"browser\" -> \"editor\"",
		"---",
		"",
		"# Fork Goal",
		"",
		"Implement the override.",
	}, "\n")

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "fork_repository", externalForkArgs{Target: workspaceDir, GoalContent: submittedGoalContent, Title: "External Fork Title"})
	require.False(t, result.IsError)

	forkResult := decodeExternalStructuredContent[externalForkResultForTest](t, result)
	forkGoalContent, errRead := os.ReadFile(filepath.Join(forkResult.Workspace.Path, "GOAL.md"))
	require.NoError(t, errRead)

	metadata, errParse := parseYAMLFrontmatter(forkGoalContent)
	require.NoError(t, errParse)
	assert.Equal(t, "External Fork Title", metadata.Title)
	frontmatter := forkGoalFrontmatterForTest(t, forkGoalContent)
	assert.Contains(t, frontmatter, "flow: |")
	assert.Contains(t, frontmatter, "coordinator: root-model")
	assert.Equal(t, 1, strings.Count(frontmatter, "title:")+strings.Count(frontmatter, "\"title\":"))
	assert.Equal(t, submittedGoalContent, forkGoalBodyForTest(t, forkGoalContent))
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
	assert.Equal(t, "Attached Goal", attachResult.Workspace.Handle)
	assert.Equal(t, "standalone", attachResult.Workspace.Mode)
}

func TestExternalMCPStartInteractiveRejectsContinuousConfiguredWorkspace(t *testing.T) {
	server, rootDir := setupTestServer(t)
	workspaceDir := setupTestWorkspace(t, server, rootDir, "continuous-ws")
	require.NoError(t, os.WriteFile(filepath.Join(workspaceDir, "GOAL.md"), []byte("---\ncontinuousModePrompt: Keep going\nflow: |\n  \"a\" -> \"b\"\n---\n# Goal"), 0o644))

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "start_interactive", externalWorkspaceArgsForTest{Workspace: "continuous-ws"})
	assert.True(t, result.IsError)
	assert.Contains(t, externalToolText(t, result), "start_continuous")
	assert.Contains(t, externalToolText(t, result), "continuous mode")
}

func TestExternalMCPStartSelfDriveUsesExplicitSelfDriveModeForContinuousConfiguredWorkspace(t *testing.T) {
	server, rootDir := setupTestServer(t)
	workspaceDir := setupTestWorkspace(t, server, rootDir, "self-drive-continuous-ws")
	require.NoError(t, initializeWorkspace(workspaceDir))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceDir, "GOAL.md"), []byte("---\ncontinuousModePrompt: Keep going\nflow: |\n  \"coordinator\"\n---\n# Goal"), 0o644))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	server.shutdownCtx = ctx
	t.Cleanup(func() {
		server.stopSession(workspaceDir)
		require.Eventually(t, func() bool {
			if errRemove := os.RemoveAll(workspaceDir); errRemove != nil {
				return false
			}
			_, errStat := os.Stat(workspaceDir)
			return os.IsNotExist(errStat)
		}, time.Second, 10*time.Millisecond)
	})

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "start_self_drive", externalTargetArgs{Target: "self-drive-continuous-ws"})
	require.False(t, result.IsError)

	action := decodeExternalStructuredContent[externalSessionActionForTest](t, result)
	assert.True(t, action.Running)
	assert.False(t, action.AlreadyRunning)
	assert.Equal(t, "self-drive", action.RunningMode)
	assert.Equal(t, "workspace started in self-drive mode", action.Message)
	assert.Equal(t, state.ModeSelfDrive, workflowStateFromDisk(t, workspaceDir).InteractionMode)
}

func TestExternalMCPStartContinuousRejectsWorkspaceWithoutContinuousMode(t *testing.T) {
	server, rootDir := setupTestServer(t)
	workspaceDir := setupTestWorkspace(t, server, rootDir, "interactive-ws")
	require.NoError(t, os.WriteFile(filepath.Join(workspaceDir, "GOAL.md"), []byte("---\nflow: |\n  \"a\" -> \"b\"\n---\n# Goal"), 0o644))

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "start_continuous", externalWorkspaceArgsForTest{Workspace: "interactive-ws"})
	assert.True(t, result.IsError)
	assert.Contains(t, externalToolText(t, result), "start_interactive")
	assert.Contains(t, externalToolText(t, result), "start_self_drive")
	assert.Contains(t, externalToolText(t, result), "continuous mode is not configured")
}

func TestExternalMCPStartContinuousReportsActualRunningMode(t *testing.T) {
	server, rootDir := setupTestServer(t)
	workspaceDir := setupTestWorkspace(t, server, rootDir, "already-running-ws")
	attachRunningSessionCoordinator(t, server, workspaceDir, workflowRef(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.InteractionMode = state.ModeSelfDrive
		workflow.CurrentAgent = "coordinator"
	}))

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "start_continuous", externalWorkspaceArgsForTest{Workspace: "already-running-ws"})
	require.False(t, result.IsError)

	action := decodeExternalStructuredContent[externalSessionActionForTest](t, result)
	assert.True(t, action.AlreadyRunning)
	assert.Equal(t, "self-drive", action.RunningMode)
	assert.Equal(t, "workspace already running in self-drive mode", action.Message)
}

func TestExternalMCPListPendingQuestionsReturnsOnlyLivePendingWorkspaces(t *testing.T) {
	server, rootDir := setupTestServer(t)
	pendingDir := setupTestWorkspace(t, server, rootDir, "pending-ws")
	staleDir := setupTestWorkspace(t, server, rootDir, "stale-ws")
	quietDir := setupTestWorkspace(t, server, rootDir, "quiet-ws")

	_, pendingErrCh, pendingCancel := startWaitingSessionQuestion(t, server, pendingDir, multiChoiceQuestionWith(func(question *state.MultiChoiceQuestion) {
		question.Questions = []state.QuestionItem{questionItemWith(func(item *state.QuestionItem) {
			item.Question = "Pick one"
			item.Choices = []string{"A", "B"}
		})}
	}), "Pick one")
	defer func() {
		pendingCancel()
		require.Error(t, <-pendingErrCh)
	}()

	writeWorkflowStateToDisk(t, staleDir, workflowRef(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.HumanMessage = "stale question"
		workflow.MultiChoiceQuestion = multiChoiceQuestionWith(func(question *state.MultiChoiceQuestion) {
			question.Questions = []state.QuestionItem{questionItemWith(func(item *state.QuestionItem) {
				item.Question = "Old question"
				item.Choices = []string{"X", "Y"}
			})}
		})
	}))
	attachRunningSessionCoordinator(t, server, quietDir, workflowRef(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.CurrentAgent = "coordinator"
	}))

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "list_pending_questions", struct{}{})
	require.False(t, result.IsError)

	list := decodeExternalStructuredContent[externalPendingQuestionListForTest](t, result)
	require.Len(t, list.Workspaces, 1)
	assert.Equal(t, "pending-ws", list.Workspaces[0].DirectoryName)
}

func TestExternalMCPGetPendingQuestionReturnsFullPayload(t *testing.T) {
	server, rootDir := setupTestServer(t)
	workspaceDir := setupTestWorkspace(t, server, rootDir, "pending-payload-ws")
	coord, errCh, cancel := startWaitingSessionQuestion(t, server, workspaceDir, multiChoiceQuestionWith(func(question *state.MultiChoiceQuestion) {
		question.Questions = []state.QuestionItem{
			questionItemWith(func(item *state.QuestionItem) {
				item.Question = "Pick one"
				item.Choices = []string{"A", "B"}
			}),
			questionItemWith(func(item *state.QuestionItem) {
				item.Question = "Pick many"
				item.Choices = []string{"X", "Y"}
				item.MultiSelect = true
			}),
		}
	}), "Please answer both questions")
	defer func() {
		cancel()
		require.Error(t, <-errCh)
	}()
	promptToken := waitForSessionPromptToken(t, coord)

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "get_pending_question", externalWorkspaceArgsForTest{Workspace: "pending-payload-ws"})
	require.False(t, result.IsError)

	payload := decodeExternalStructuredContent[externalPendingQuestionResultForTest](t, result)
	assert.Equal(t, "pending-payload-ws", payload.Workspace.DirectoryName)
	assert.Equal(t, promptToken, payload.PendingQuestion.PromptToken)
	assert.Equal(t, "multi-choice", payload.PendingQuestion.Type)
	assert.Equal(t, "coordinator", payload.PendingQuestion.AgentName)
	assert.Equal(t, "Please answer both questions", payload.PendingQuestion.Message)
	require.Len(t, payload.PendingQuestion.Questions, 2)
	assert.Equal(t, "Pick one", payload.PendingQuestion.Questions[0].Question)
	assert.Equal(t, []string{"A", "B"}, payload.PendingQuestion.Questions[0].Choices)
	assert.True(t, payload.PendingQuestion.Questions[1].MultiSelect)
}

func TestExternalMCPAnswerPendingQuestionRejectsEmptySubmissionWithResteer(t *testing.T) {
	server, rootDir := setupTestServer(t)
	workspaceDir := setupTestWorkspace(t, server, rootDir, "empty-answer-ws")
	coord, errCh, cancel := startWaitingSessionQuestion(t, server, workspaceDir, multiChoiceQuestionWith(func(question *state.MultiChoiceQuestion) {
		question.Questions = []state.QuestionItem{questionItemWith(func(item *state.QuestionItem) {
			item.Question = "Pick one"
			item.Choices = []string{"A", "B"}
		})}
	}), "Please explain your choice")
	defer func() {
		cancel()
		require.Error(t, <-errCh)
	}()
	promptToken := waitForSessionPromptToken(t, coord)

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "answer_pending_question", externalAnswerPendingQuestionArgsForTest{
		Workspace:       "empty-answer-ws",
		PromptToken:     promptToken,
		Answer:          "",
		SelectedChoices: nil,
	})
	assert.True(t, result.IsError)
	message := externalToolText(t, result)
	assert.Contains(t, message, "answer text or selected choices are required")
	assert.Contains(t, message, "get_pending_question")
	assert.Contains(t, message, "provide answer text, selected choices, or both")
	assert.NotContains(t, message, "answer every required part of the current question")
	assert.True(t, coord.State().NeedsHumanInput())
}

func TestExternalMCPAnswerPendingQuestionRejectsStalePromptTokenWithResteer(t *testing.T) {
	server, rootDir := setupTestServer(t)
	workspaceDir := setupTestWorkspace(t, server, rootDir, "stale-answer-ws")
	coord, errCh, cancel := startWaitingSessionQuestion(t, server, workspaceDir, multiChoiceQuestionWith(func(question *state.MultiChoiceQuestion) {
		question.Questions = []state.QuestionItem{questionItemWith(func(item *state.QuestionItem) {
			item.Question = "Pick one"
			item.Choices = []string{"A", "B"}
		})}
	}), "Please answer")
	defer func() {
		cancel()
		require.Error(t, <-errCh)
	}()

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "answer_pending_question", externalAnswerPendingQuestionArgsForTest{
		Workspace:       "stale-answer-ws",
		PromptToken:     "stale-token",
		Answer:          "I choose A",
		SelectedChoices: []string{"A"},
	})
	assert.True(t, result.IsError)
	assert.Contains(t, externalToolText(t, result), "question is no longer current")
	assert.Contains(t, externalToolText(t, result), "get_pending_question")
	assert.True(t, coord.State().NeedsHumanInput())
}

func assertExternalPendingQuestionSubmission(t *testing.T, workspaceName, answer string, selectedChoices []string) {
	t.Helper()

	server, rootDir := setupTestServer(t)
	workspaceDir := setupTestWorkspace(t, server, rootDir, workspaceName)
	coord, errCh, cancel := startWaitingSessionQuestion(t, server, workspaceDir, multiChoiceQuestionWith(func(question *state.MultiChoiceQuestion) {
		question.Questions = []state.QuestionItem{questionItemWith(func(item *state.QuestionItem) {
			item.Question = "Pick one"
			item.Choices = []string{"A", "B"}
		})}
	}), "Please explain your choice")
	defer cancel()
	promptToken := waitForSessionPromptToken(t, coord)

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "answer_pending_question", externalAnswerPendingQuestionArgsForTest{
		Workspace:       workspaceName,
		PromptToken:     promptToken,
		Answer:          answer,
		SelectedChoices: selectedChoices,
	})
	require.False(t, result.IsError)

	response := decodeExternalStructuredContent[externalAnswerPendingQuestionResultForTest](t, result)
	assert.True(t, response.Success)
	assert.Equal(t, "response submitted", response.Message)
	assert.Equal(t, workspaceName, response.Workspace.DirectoryName)
	require.NoError(t, <-errCh)
}

func TestExternalMCPAnswerPendingQuestionSubmitsResponse(t *testing.T) {
	assertExternalPendingQuestionSubmission(t, "answer-success-ws", "I choose A because it is safer.", []string{"A"})
}

func TestExternalMCPAnswerPendingQuestionAllowsChoiceOnlySubmission(t *testing.T) {
	assertExternalPendingQuestionSubmission(t, "choice-only-answer-ws", "", []string{"A"})
}

func TestExternalMCPAnswerPendingQuestionAllowsTextOnlySubmission(t *testing.T) {
	assertExternalPendingQuestionSubmission(t, "text-only-answer-ws", "I prefer A because it is safer.", nil)
}

func TestExternalMCPSteerNextTurnRejectsEmptyInstruction(t *testing.T) {
	server, rootDir := setupTestServer(t)
	workspaceDir := setupTestWorkspace(t, server, rootDir, "steer-empty-ws")
	_, errCoord := state.NewCoordinatorWith(filepath.Join(workspaceDir, ".sgai", "state.json"), workflowWith(func(*state.Workflow) {}))
	require.NoError(t, errCoord)

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "steer_next_turn", externalSteerNextTurnArgsForTest{
		Workspace: "steer-empty-ws",
		Message:   "   ",
	})
	assert.True(t, result.IsError)
	assert.Contains(t, externalToolText(t, result), "cannot be empty")
}

func TestExternalMCPSteerNextTurnAddsResteeringInstruction(t *testing.T) {
	server, rootDir := setupTestServer(t)
	workspaceDir := setupTestWorkspace(t, server, rootDir, "steer-success-ws")
	attachRunningSessionCoordinator(t, server, workspaceDir, workflowRef(func(*state.Workflow) {}))
	coord := server.sessionCoordinator(workspaceDir)
	require.NotNil(t, coord)

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "steer_next_turn", externalSteerNextTurnArgsForTest{
		Workspace: "steer-success-ws",
		Message:   "Please branch before changing the API shape.",
	})
	require.False(t, result.IsError)

	response := decodeExternalStructuredContent[externalSteerNextTurnResultForTest](t, result)
	assert.True(t, response.Success)
	assert.Equal(t, "steering instruction added", response.Message)
	assert.Equal(t, "steer-success-ws", response.Workspace.DirectoryName)
	require.Len(t, coord.State().Messages, 1)
	assert.Equal(t, "Human Partner", coord.State().Messages[0].FromAgent)
	assert.Equal(t, "coordinator", coord.State().Messages[0].ToAgent)
	assert.Equal(t, "Re-steering instruction: Please branch before changing the API shape.", coord.State().Messages[0].Body)
}

func TestExternalMCPWorkflowLinksReturnsMinimalSetWithoutRespondWhenIdle(t *testing.T) {
	server, rootDir := setupTestServer(t)
	workspaceDir := setupTestWorkspace(t, server, rootDir, "workflow-links-ws")
	require.NoError(t, os.WriteFile(filepath.Join(workspaceDir, "GOAL.md"), []byte("---\ntitle: Workflow Links\n---\n# Goal"), 0o644))

	req := httptestNewRequest(t, "http://127.0.0.1/mcp/external")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "10.10.0.7:9443")

	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "workflow_links", externalWorkspaceArgsForTest{Workspace: "workflow-links-ws"})
	require.False(t, result.IsError)

	links := decodeExternalStructuredContent[externalWorkflowLinksResultForTest](t, result)
	assert.Equal(t, "https://10.10.0.7:9443/workspaces/workflow-links-ws/progress", links.Links.Progress)
	assert.Equal(t, "https://10.10.0.7:9443/workspaces/workflow-links-ws/goal/edit", links.Links.GoalEdit)
	assert.Empty(t, links.Links.Respond)
	assert.Equal(t, "workflow-links-ws", links.Workspace.DirectoryName)
}

func TestExternalMCPWorkflowLinksIncludesRespondWhenInputIsPending(t *testing.T) {
	server, rootDir := setupTestServer(t)
	workspaceDir := setupTestWorkspace(t, server, rootDir, "workflow-pending-ws")
	require.NoError(t, os.WriteFile(filepath.Join(workspaceDir, "GOAL.md"), []byte("---\ntitle: Workflow Pending\n---\n# Goal"), 0o644))
	_, errCh, cancel := startWaitingSessionQuestion(t, server, workspaceDir, nil, "Please answer")
	defer func() {
		cancel()
		require.Error(t, <-errCh)
	}()

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "workflow_links", externalWorkspaceArgsForTest{Workspace: "workflow-pending-ws"})
	require.False(t, result.IsError)

	links := decodeExternalStructuredContent[externalWorkflowLinksResultForTest](t, result)
	assert.Equal(t, "http://factory.test/workspaces/workflow-pending-ws/progress", links.Links.Progress)
	assert.Equal(t, "http://factory.test/workspaces/workflow-pending-ws/goal/edit", links.Links.GoalEdit)
	assert.Equal(t, "http://factory.test/workspaces/workflow-pending-ws/respond", links.Links.Respond)
	assert.Equal(t, "workflow-pending-ws", links.Workspace.DirectoryName)
}

func TestExternalMCPWorkflowLinksRejectsAmbiguousAbsolutePathTarget(t *testing.T) {
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
	result := callExternalTool(t, cs, "workflow_links", externalWorkspaceArgsForTest{Workspace: firstDir})
	assert.True(t, result.IsError)

	message := externalToolText(t, result)
	assert.Contains(t, message, "workflow links")
	assert.Contains(t, message, "basename")
	assert.Contains(t, message, "ambiguous")
	assert.NotContains(t, message, "goal edit link")
}

func TestExternalMCPPeekWorkspaceLogReturnsLiveBufferOnly(t *testing.T) {
	server, rootDir := setupTestServer(t)
	workspaceDir := setupTestWorkspace(t, server, rootDir, "log-ws")
	sess := newTestServeSession(nil, true)
	sess.outputLog = newCircularLogBuffer()
	sess.outputLog.add(logLine{prefix: "stdout", text: "first live line"})
	sess.outputLog.add(logLine{prefix: "stderr", text: "second live line"})
	server.mu.Lock()
	server.sessions[workspaceDir] = sess
	server.mu.Unlock()
	require.NoError(t, os.WriteFile(filepath.Join(workspaceDir, ".sgai", "session.log"), []byte("historical line"), 0o644))

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "peek_workspace_log", externalWorkspaceArgsForTest{Workspace: "log-ws"})
	require.False(t, result.IsError)

	logResult := decodeExternalStructuredContent[externalWorkspaceLogResultForTest](t, result)
	require.Len(t, logResult.Log, 2)
	assert.Equal(t, []externalLogEntryForTest{{Prefix: "stdout", Text: "first live line"}, {Prefix: "stderr", Text: "second live line"}}, logResult.Log)
}

func TestExternalMCPWorkspaceMessagesAndTodosUsesInternalOrderAndActiveAgentTodos(t *testing.T) {
	server, rootDir := setupTestServer(t)
	workspaceDir := setupTestWorkspace(t, server, rootDir, "messages-ws")
	attachRunningSessionCoordinator(t, server, workspaceDir, workflowRef(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.CurrentAgent = "go-developer, react-developer"
		workflow.Messages = []state.Message{
			messageWith(func(message *state.Message) {
				message.ID = 1
				message.FromAgent = "coordinator"
				message.ToAgent = "go-developer"
				message.Body = "first message"
			}),
			messageWith(func(message *state.Message) {
				message.ID = 2
				message.FromAgent = "go-developer"
				message.ToAgent = "react-developer"
				message.Body = "second message"
				message.Read = true
			}),
		}
		workflow.ProjectTodos = []state.TodoItem{{ID: "p1", Content: "project todo", Status: "pending", Priority: "high"}}
		workflow.TodosByAgent = map[string][]state.TodoItem{
			"go-developer":    {{ID: "g1", Content: "go todo", Status: "pending", Priority: "high"}},
			"react-developer": {{ID: "r1", Content: "react todo", Status: "in_progress", Priority: "medium"}},
			"coordinator":     {{ID: "c1", Content: "hidden todo", Status: "pending", Priority: "low"}},
		}
	}))

	req := httptestNewRequest(t, "http://factory.test/mcp/external")
	cs := connectExternalMCPClient(t, server, req)
	result := callExternalTool(t, cs, "workspace_messages_and_todos", externalWorkspaceArgsForTest{Workspace: "messages-ws"})
	require.False(t, result.IsError)

	export := decodeExternalStructuredContent[externalWorkspaceMessagesAndTodosForTest](t, result)
	require.Len(t, export.Messages, 2)
	assert.Equal(t, 1, export.Messages[0].ID)
	assert.Equal(t, 2, export.Messages[1].ID)
	require.Len(t, export.ProjectTodos, 1)
	assert.Equal(t, "project todo", export.ProjectTodos[0].Content)
	require.Len(t, export.ActiveAgentTodoSections, 2)
	assert.Equal(t, "go-developer", export.ActiveAgentTodoSections[0].Agent)
	assert.Equal(t, "go todo", export.ActiveAgentTodoSections[0].Todos[0].Content)
	assert.Equal(t, "react-developer", export.ActiveAgentTodoSections[1].Agent)
	assert.Equal(t, "react todo", export.ActiveAgentTodoSections[1].Todos[0].Content)
}

func httptestNewRequest(t *testing.T, rawURL string) *http.Request {
	t.Helper()

	req, errRequest := http.NewRequest(http.MethodPost, rawURL, http.NoBody)
	require.NoError(t, errRequest)
	return req
}
