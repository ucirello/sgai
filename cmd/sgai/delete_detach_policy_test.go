package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleAPIWorkspaceListIncludesRepositoryActionPolicy(t *testing.T) {
	server, _ := setupTestServer(t)
	baseDir := t.TempDir()

	standaloneDir := filepath.Join(baseDir, "standalone-ws")
	attachWorkspaceFixture(t, server, standaloneDir, workspaceStandalone)

	zeroChildRootDir, zeroChildForkDir := setupNamedAttachedJJRootAndFork(t, server, "zero-child-root-ws", "zero-child-fork-ws")
	runJJForTest(t, zeroChildRootDir, "workspace", "forget", filepath.Base(zeroChildForkDir))
	require.NoError(t, os.RemoveAll(zeroChildForkDir))
	server.mu.Lock()
	delete(server.externalDirs, resolveSymlinks(zeroChildForkDir))
	server.mu.Unlock()

	rootDir, forkDir := setupNamedAttachedJJRootAndFork(t, server, "root-ws", "fork-ws")
	server.invalidateWorkspaceScanCache()

	response := serveHTTP(server, http.MethodGet, "/api/v1/workspaces", "")
	require.Equal(t, http.StatusOK, response.Code)

	workspaces := decodeWorkspaceListByName(t, response.Body.Bytes())

	standalone := workspaces[filepath.Base(standaloneDir)]
	assert.True(t, standalone.IsExternal)
	assert.True(t, standalone.External)
	assertRepositoryAction(t, &standalone.RepositoryAction, &repositoryActionExpectation{
		mode:              "standalone",
		entryPoint:        "confirm",
		defaultOperation:  "detach",
		allowedOperations: []string{"detach"},
		disabledReason:    "",
		attachedForkCount: 0,
	})

	zeroChildRoot := workspaces[filepath.Base(zeroChildRootDir)]
	assertRepositoryAction(t, &zeroChildRoot.RepositoryAction, &repositoryActionExpectation{
		mode:              "standalone",
		entryPoint:        "confirm",
		defaultOperation:  "detach",
		allowedOperations: []string{"detach"},
		disabledReason:    "",
		attachedForkCount: 0,
	})

	root := workspaces[filepath.Base(rootDir)]
	assertRepositoryAction(t, &root.RepositoryAction, &repositoryActionExpectation{
		mode:              "root",
		entryPoint:        "hidden",
		defaultOperation:  "",
		disabledReason:    "forks-attached",
		allowedOperations: nil,
		attachedForkCount: 1,
	})

	fork := workspaces[filepath.Base(forkDir)]
	assertRepositoryAction(t, &fork.RepositoryAction, &repositoryActionExpectation{
		mode:              "fork",
		entryPoint:        "choose",
		defaultOperation:  "",
		allowedOperations: []string{"detach", "delete"},
		disabledReason:    "",
		attachedForkCount: 0,
	})
}

func TestHandleAPIWorkspaceListIncludesRepositoryActionPresentationMetadata(t *testing.T) {
	server, _ := setupTestServer(t)
	baseDir := t.TempDir()

	standaloneDir := filepath.Join(baseDir, "standalone-ws")
	attachWorkspaceFixture(t, server, standaloneDir, workspaceStandalone)
	_, forkDir := setupNamedAttachedJJRootAndFork(t, server, "root-ws", "fork-ws")

	response := serveHTTP(server, http.MethodGet, "/api/v1/workspaces", "")
	require.Equal(t, http.StatusOK, response.Code)

	workspaces := decodeWorkspaceListByName(t, response.Body.Bytes())

	standalonePresentation := workspaces[filepath.Base(standaloneDir)].RepositoryAction.Presentation
	assert.Equal(t, "Detach", standalonePresentation.DetailTriggerLabel)
	assert.Equal(t, "Detach standalone-ws", standalonePresentation.TreeTriggerLabel)
	assert.Equal(t, "Detach standalone-ws", standalonePresentation.ForkRowTriggerLabel)
	assert.Equal(t, "Detach workspace", standalonePresentation.DialogTitle)
	assert.Equal(t, "This will remove 'standalone-ws' from the SGAI workspace list. The files on disk will NOT be deleted.", standalonePresentation.DialogDescription)
	assert.Equal(t, "detach", standalonePresentation.Icon)
	assert.Equal(t, "neutral", standalonePresentation.Tone)

	require.Len(t, standalonePresentation.Operations, 1)
	standaloneOperation := standalonePresentation.Operations[0]
	assert.Equal(t, "detach", standaloneOperation.Operation)
	assert.Equal(t, "Detach", standaloneOperation.Label)
	assert.Equal(t, "detach", standaloneOperation.Icon)
	assert.Equal(t, "neutral", standaloneOperation.Tone)

	forkPresentation := workspaces[filepath.Base(forkDir)].RepositoryAction.Presentation
	assert.Equal(t, "Choose action", forkPresentation.DetailTriggerLabel)
	assert.Equal(t, "Choose action for fork fork-ws", forkPresentation.TreeTriggerLabel)
	assert.Equal(t, "Choose action for fork fork-ws", forkPresentation.ForkRowTriggerLabel)
	assert.Equal(t, "Choose fork action", forkPresentation.DialogTitle)
	assert.Equal(t, "Choose what to do with fork 'fork-ws'. Detach removes the fork from the SGAI workspace list and keeps the files on disk. Delete permanently removes the fork from disk.", forkPresentation.DialogDescription)
	assert.Equal(t, "choose", forkPresentation.Icon)
	assert.Equal(t, "neutral", forkPresentation.Tone)

	require.Len(t, forkPresentation.Operations, 2)
	forkDetachOperation := forkPresentation.Operations[0]
	forkDeleteOperation := forkPresentation.Operations[1]
	assert.Equal(t, "detach", forkDetachOperation.Operation)
	assert.Equal(t, "Detach", forkDetachOperation.Label)
	assert.Equal(t, "detach", forkDetachOperation.Icon)
	assert.Equal(t, "neutral", forkDetachOperation.Tone)
	assert.Equal(t, "delete", forkDeleteOperation.Operation)
	assert.Equal(t, "Delete", forkDeleteOperation.Label)
	assert.Equal(t, "delete", forkDeleteOperation.Icon)
	assert.Equal(t, "destructive", forkDeleteOperation.Tone)
}

func TestHandleAPIWorkspaceListClassifiesZeroChildRootFromJJMetadata(t *testing.T) {
	server, _ := setupTestServer(t)
	rootDir, forkDir := setupAttachedJJRootAndFork(t, server)
	runJJForTest(t, rootDir, "workspace", "forget", filepath.Base(forkDir))
	require.NoError(t, os.RemoveAll(forkDir))

	freshServer, _ := setupTestServer(t)
	freshServer.mu.Lock()
	freshServer.externalDirs[resolveSymlinks(rootDir)] = true
	freshServer.mu.Unlock()

	response := serveHTTP(freshServer, http.MethodGet, "/api/v1/workspaces", "")
	require.Equal(t, http.StatusOK, response.Code)

	workspaces := decodeWorkspaceListByName(t, response.Body.Bytes())
	root := workspaces[filepath.Base(rootDir)]
	assert.False(t, root.IsRoot)
	assert.Empty(t, root.Forks)
	assertRepositoryAction(t, &root.RepositoryAction, &repositoryActionExpectation{
		mode:              "standalone",
		entryPoint:        "confirm",
		defaultOperation:  "detach",
		allowedOperations: []string{"detach"},
		disabledReason:    "",
		attachedForkCount: 0,
	})
}

func TestHandleAPIWorkspaceListHidesRootActionWhenWorkspaceTopologyUnavailable(t *testing.T) {
	server, _ := setupTestServer(t)
	rootDir, forkDir := setupAttachedJJRootAndFork(t, server)
	server.mu.Lock()
	delete(server.externalDirs, resolveSymlinks(forkDir))
	server.mu.Unlock()
	t.Setenv("PATH", t.TempDir())
	server.invalidateWorkspaceScanCache()

	response := serveHTTP(server, http.MethodGet, "/api/v1/workspaces", "")
	require.Equal(t, http.StatusOK, response.Code)

	workspaces := decodeWorkspaceListByName(t, response.Body.Bytes())
	root := workspaces[filepath.Base(rootDir)]
	assert.True(t, root.IsRoot)
	assertRepositoryAction(t, &root.RepositoryAction, &repositoryActionExpectation{
		mode:              "root",
		entryPoint:        "hidden",
		defaultOperation:  "",
		disabledReason:    "topology-unavailable",
		allowedOperations: nil,
		attachedForkCount: 0,
	})
}

func TestHandleAPIDeleteWorkspaceRejectsRootWithAttachedForks(t *testing.T) {
	server, _ := setupTestServer(t)
	baseDir := t.TempDir()
	rootDir := filepath.Join(baseDir, "root-ws")
	forkDir := filepath.Join(baseDir, "fork-ws")
	createForkFixture(t, rootDir, forkDir)
	attachWorkspaceFixture(t, server, rootDir, workspaceRoot)
	attachWorkspaceFixture(t, server, forkDir, workspaceFork)

	response := serveHTTP(server, http.MethodPost, "/api/v1/workspaces/root-ws/delete", `{"confirm":true}`)
	assert.Equal(t, http.StatusConflict, response.Code)
	assert.DirExists(t, rootDir)

	server.mu.Lock()
	_, stillAttached := server.externalDirs[resolveSymlinks(rootDir)]
	server.mu.Unlock()
	assert.True(t, stillAttached)
	assert.Contains(t, response.Body.String(), "fork")
}

func TestHandleAPIDetachWorkspaceRejectsRootWithAttachedForks(t *testing.T) {
	server, _ := setupTestServer(t)
	baseDir := t.TempDir()
	rootDir := filepath.Join(baseDir, "root-ws")
	forkDir := filepath.Join(baseDir, "fork-ws")
	createForkFixture(t, rootDir, forkDir)
	attachWorkspaceFixture(t, server, rootDir, workspaceRoot)
	attachWorkspaceFixture(t, server, forkDir, workspaceFork)

	requestBody := fmt.Sprintf(`{"path":%q}`, rootDir)
	response := serveHTTP(server, http.MethodPost, "/api/v1/workspaces/detach", requestBody)
	assert.Equal(t, http.StatusConflict, response.Code)
	assert.DirExists(t, rootDir)
	assert.DirExists(t, forkDir)
	assert.Contains(t, response.Body.String(), "fork")

	server.mu.Lock()
	_, stillAttached := server.externalDirs[resolveSymlinks(rootDir)]
	server.mu.Unlock()
	assert.True(t, stillAttached)
}

func TestHandleAPIDeleteWorkspaceRejectsRootWithExistingUnattachedChild(t *testing.T) {
	server, _ := setupTestServer(t)
	rootDir, forkDir := setupAttachedJJRootAndFork(t, server)

	server.mu.Lock()
	delete(server.externalDirs, resolveSymlinks(forkDir))
	server.mu.Unlock()
	server.invalidateWorkspaceScanCache()

	response := serveHTTP(server, http.MethodPost, "/api/v1/workspaces/root-ws/delete", `{"confirm":true}`)
	assert.Equal(t, http.StatusConflict, response.Code)
	assert.DirExists(t, rootDir)
	assert.DirExists(t, forkDir)
	assert.Contains(t, response.Body.String(), "fork")

	server.mu.Lock()
	_, stillAttached := server.externalDirs[resolveSymlinks(rootDir)]
	server.mu.Unlock()
	assert.True(t, stillAttached)
}

func TestHandleAPIDeleteWorkspaceRejectsRootWhenWorkspaceTopologyUnavailable(t *testing.T) {
	server, _ := setupTestServer(t)
	rootDir, forkDir := setupAttachedJJRootAndFork(t, server)

	server.mu.Lock()
	delete(server.externalDirs, resolveSymlinks(forkDir))
	server.mu.Unlock()
	server.invalidateWorkspaceScanCache()
	t.Setenv("PATH", t.TempDir())

	response := serveHTTP(server, http.MethodPost, "/api/v1/workspaces/root-ws/delete", `{"confirm":true}`)
	assert.Equal(t, http.StatusConflict, response.Code)
	assert.DirExists(t, rootDir)
	assert.DirExists(t, forkDir)
	assert.Contains(t, response.Body.String(), "not allowed")

	server.mu.Lock()
	_, stillAttached := server.externalDirs[resolveSymlinks(rootDir)]
	server.mu.Unlock()
	assert.True(t, stillAttached)
}

func TestHandleAPIDeleteWorkspaceRejectsRunningRepository(t *testing.T) {
	server, _ := setupTestServer(t)
	workspaceDir := filepath.Join(t.TempDir(), "running-ws")
	attachWorkspaceFixture(t, server, workspaceDir, workspaceStandalone)

	server.mu.Lock()
	var runningSession session
	runningSession.running = true
	server.sessions[resolveSymlinks(workspaceDir)] = &runningSession
	server.mu.Unlock()

	response := serveHTTP(server, http.MethodPost, "/api/v1/workspaces/running-ws/delete", `{"confirm":true}`)
	assert.Equal(t, http.StatusConflict, response.Code)
	assert.DirExists(t, workspaceDir)
	assert.Contains(t, response.Body.String(), "running")

	server.mu.Lock()
	require.NotNil(t, server.sessions[resolveSymlinks(workspaceDir)])
	assert.True(t, server.sessions[resolveSymlinks(workspaceDir)].running)
	server.mu.Unlock()
}

func TestHandleAPIDeleteWorkspaceRejectsAmbiguousDuplicateNameWithoutWorkspaceDir(t *testing.T) {
	server, _ := setupTestServer(t)
	firstDir, secondDir := setupAttachedDuplicateNameWorkspaces(t, server, workspaceStandalone)

	response := serveHTTP(server, http.MethodPost, "/api/v1/workspaces/shared-ws/delete", `{"confirm":true}`)
	require.Equal(t, http.StatusConflict, response.Code)
	assert.Contains(t, response.Body.String(), "ambiguous")
	assert.DirExists(t, firstDir)
	assert.DirExists(t, secondDir)

	server.mu.Lock()
	firstStillAttached := directorySetContains(server.externalDirs, firstDir)
	secondStillAttached := directorySetContains(server.externalDirs, secondDir)
	server.mu.Unlock()
	assert.True(t, firstStillAttached)
	assert.True(t, secondStillAttached)
}

func TestHandleAPIDeleteWorkspaceRejectsRoutedDuplicateNameWorkspace(t *testing.T) {
	server, _ := setupTestServer(t)
	firstDir, secondDir := setupAttachedDuplicateNameWorkspaces(t, server, workspaceStandalone)

	response := serveHTTP(server, http.MethodPost, "/api/v1/workspaces/second/shared-ws/delete", `{"confirm":true}`)
	require.Equal(t, http.StatusNotFound, response.Code)
	assert.DirExists(t, firstDir)
	assert.DirExists(t, secondDir)

	server.mu.Lock()
	firstStillAttached := directorySetContains(server.externalDirs, firstDir)
	secondStillAttached := directorySetContains(server.externalDirs, secondDir)
	server.mu.Unlock()
	assert.True(t, firstStillAttached)
	assert.True(t, secondStillAttached)
}

func TestHandleAPIDeleteWorkspaceRejectsRoutedDuplicateNamedFork(t *testing.T) {
	server, _ := setupTestServer(t)
	_, firstForkDir := setupNamedAttachedJJRootAndFork(t, server, "root-one", "shared-fork")
	_, secondForkDir := setupNamedAttachedJJRootAndFork(t, server, "root-two", "shared-fork")

	response := serveHTTP(server, http.MethodPost, "/api/v1/workspaces/root-two/shared-fork/delete", `{"confirm":true,"operation":"delete"}`)
	require.Equal(t, http.StatusNotFound, response.Code)
	assert.DirExists(t, filepath.Dir(firstForkDir))
	assert.DirExists(t, firstForkDir)
	assert.DirExists(t, secondForkDir)

	server.mu.Lock()
	firstStillAttached := directorySetContains(server.externalDirs, firstForkDir)
	secondStillAttached := directorySetContains(server.externalDirs, secondForkDir)
	server.mu.Unlock()
	assert.True(t, firstStillAttached)
	assert.True(t, secondStillAttached)
}

func TestHandleAPIDeleteForkKeepsFactoryStateHealthy(t *testing.T) {
	server, _ := setupTestServer(t)
	server.externalConfigDir = t.TempDir()
	server.pinnedConfigDir = t.TempDir()
	rootDir, forkDir := setupAttachedJJRootAndFork(t, server)

	server.mu.Lock()
	server.pinnedDirs[resolveSymlinks(forkDir)] = true
	server.mu.Unlock()
	require.NoError(t, server.saveExternalDirs())
	require.NoError(t, server.savePinnedProjects())

	requestBody := fmt.Sprintf(`{"forkDir":%q,"confirm":true}`, forkDir)
	response := serveHTTP(server, http.MethodPost, "/api/v1/workspaces/root-ws/delete-fork", requestBody)
	require.Equal(t, http.StatusOK, response.Code)
	assert.NoDirExists(t, forkDir)

	server.mu.Lock()
	_, stillAttached := server.externalDirs[resolveSymlinks(forkDir)]
	_, stillPinned := server.pinnedDirs[resolveSymlinks(forkDir)]
	server.mu.Unlock()
	assert.False(t, stillAttached)
	assert.False(t, stillPinned)

	stateResponse := serveHTTP(server, http.MethodGet, "/api/v1/workspaces", "")
	require.Equal(t, http.StatusOK, stateResponse.Code)
	workspaces := decodeWorkspaceListByName(t, stateResponse.Body.Bytes())
	root := workspaces[filepath.Base(rootDir)]
	require.NotNil(t, root)
	_, forkPresent := workspaces[filepath.Base(forkDir)]
	assert.False(t, forkPresent)
	assertRepositoryAction(t, &root.RepositoryAction, &repositoryActionExpectation{
		mode:              "standalone",
		entryPoint:        "confirm",
		defaultOperation:  "detach",
		allowedOperations: []string{"detach"},
		disabledReason:    "",
		attachedForkCount: 0,
	})

	assert.NotContains(t, readJSONPathList(t, filepath.Join(server.externalConfigDir, "external.json")), resolveSymlinks(forkDir))
	assert.NotContains(t, readJSONPathList(t, filepath.Join(server.pinnedConfigDir, "pinned.json")), resolveSymlinks(forkDir))
}

func TestHandleAPIDeleteForkRollsBackWhenWorkspaceListCommitFails(t *testing.T) {
	server, _ := setupTestServer(t)
	server.externalConfigDir = t.TempDir()
	server.pinnedConfigDir = t.TempDir()
	rootDir, forkDir := setupAttachedJJRootAndFork(t, server)

	server.mu.Lock()
	server.pinnedDirs[resolveSymlinks(forkDir)] = true
	server.mu.Unlock()
	require.NoError(t, server.saveExternalDirs())
	require.NoError(t, server.savePinnedProjects())

	pinnedFile := filepath.Join(server.pinnedConfigDir, "pinned.json")
	require.NoError(t, os.Remove(pinnedFile))
	require.NoError(t, os.Mkdir(pinnedFile, 0o755))

	requestBody := fmt.Sprintf(`{"forkDir":%q,"confirm":true}`, forkDir)
	response := serveHTTP(server, http.MethodPost, "/api/v1/workspaces/root-ws/delete-fork", requestBody)
	assert.Equal(t, http.StatusInternalServerError, response.Code)
	assert.DirExists(t, forkDir)

	server.mu.Lock()
	stillAttached := directorySetContains(server.externalDirs, forkDir)
	stillPinned := directorySetContains(server.pinnedDirs, forkDir)
	server.mu.Unlock()
	assert.True(t, stillAttached)
	assert.True(t, stillPinned)
	assert.True(t, pathListContains(readJSONPathList(t, filepath.Join(server.externalConfigDir, "external.json")), forkDir))

	workspaceListOutput := runJJForTest(t, rootDir, "workspace", "list")
	assert.Contains(t, workspaceListOutput, filepath.Base(forkDir)+":")
}

func TestHandleAPIDeleteWorkspaceForkDetachOperationKeepsFiles(t *testing.T) {
	server, _ := setupTestServer(t)
	server.externalConfigDir = t.TempDir()
	server.pinnedConfigDir = t.TempDir()
	rootDir, forkDir := setupAttachedJJRootAndFork(t, server)

	server.mu.Lock()
	server.pinnedDirs[resolveSymlinks(forkDir)] = true
	server.mu.Unlock()
	require.NoError(t, server.saveExternalDirs())
	require.NoError(t, server.savePinnedProjects())

	response := serveHTTP(server, http.MethodPost, "/api/v1/workspaces/fork-ws/delete", `{"confirm":true,"operation":"detach"}`)
	require.Equal(t, http.StatusOK, response.Code)
	var result apiDeleteWorkspaceResponse
	require.NoError(t, json.NewDecoder(response.Body).Decode(&result))
	assert.False(t, result.Deleted)
	assert.True(t, result.Detached)
	assert.DirExists(t, forkDir)
	assert.DirExists(t, rootDir)

	server.mu.Lock()
	_, stillAttached := server.externalDirs[resolveSymlinks(forkDir)]
	_, stillPinned := server.pinnedDirs[resolveSymlinks(forkDir)]
	server.mu.Unlock()
	assert.False(t, stillAttached)
	assert.False(t, stillPinned)

	stateResponse := serveHTTP(server, http.MethodGet, "/api/v1/workspaces", "")
	require.Equal(t, http.StatusOK, stateResponse.Code)
	workspaces := decodeWorkspaceListByName(t, stateResponse.Body.Bytes())
	require.NotNil(t, workspaces[filepath.Base(rootDir)])
	_, forkPresent := workspaces[filepath.Base(forkDir)]
	assert.False(t, forkPresent)

	assert.NotContains(t, readJSONPathList(t, filepath.Join(server.externalConfigDir, "external.json")), resolveSymlinks(forkDir))
	assert.NotContains(t, readJSONPathList(t, filepath.Join(server.pinnedConfigDir, "pinned.json")), resolveSymlinks(forkDir))
}

func TestHandleAPIDeleteWorkspaceRollsBackWhenPinnedPersistenceFails(t *testing.T) {
	server, _ := setupTestServer(t)
	server.externalConfigDir = t.TempDir()
	server.pinnedConfigDir = t.TempDir()
	workspaceDir := filepath.Join(t.TempDir(), "rollback-ws")
	attachWorkspaceFixture(t, server, workspaceDir, workspaceStandalone)

	canonical := resolveSymlinks(workspaceDir)
	server.mu.Lock()
	server.pinnedDirs[canonical] = true
	server.mu.Unlock()
	require.NoError(t, server.saveExternalDirs())
	require.NoError(t, server.savePinnedProjects())

	pinnedFile := filepath.Join(server.pinnedConfigDir, "pinned.json")
	require.NoError(t, os.Remove(pinnedFile))
	require.NoError(t, os.Mkdir(pinnedFile, 0o755))

	response := serveHTTP(server, http.MethodPost, "/api/v1/workspaces/rollback-ws/delete", `{"confirm":true}`)
	assert.Equal(t, http.StatusInternalServerError, response.Code)
	assert.DirExists(t, workspaceDir)

	server.mu.Lock()
	stillAttached := directorySetContains(server.externalDirs, workspaceDir)
	stillPinned := directorySetContains(server.pinnedDirs, workspaceDir)
	server.mu.Unlock()
	assert.True(t, stillAttached)
	assert.True(t, stillPinned)
	assert.True(t, pathListContains(readJSONPathList(t, filepath.Join(server.externalConfigDir, "external.json")), workspaceDir))
}

func TestBuildWorkspaceListResponsePrunesMissingAttachedWorkspace(t *testing.T) {
	server, _ := setupTestServer(t)
	server.externalConfigDir = t.TempDir()
	server.pinnedConfigDir = t.TempDir()
	validDir := filepath.Join(t.TempDir(), "valid-ws")
	attachWorkspaceFixture(t, server, validDir, workspaceStandalone)

	missingDir := filepath.Join(t.TempDir(), "missing-ws")
	missingCanonical := resolveSymlinks(missingDir)
	server.mu.Lock()
	server.externalDirs[missingCanonical] = true
	server.pinnedDirs[missingCanonical] = true
	server.mu.Unlock()
	require.NoError(t, server.saveExternalDirs())
	require.NoError(t, server.savePinnedProjects())
	server.invalidateWorkspaceScanCache()

	state := server.buildWorkspaceListResponse()
	require.Len(t, state.Workspaces, 1)
	assert.Equal(t, filepath.Base(validDir), state.Workspaces[0].Name)

	server.mu.Lock()
	_, stillAttached := server.externalDirs[missingCanonical]
	_, stillPinned := server.pinnedDirs[missingCanonical]
	server.mu.Unlock()
	assert.False(t, stillAttached)
	assert.False(t, stillPinned)
	assert.NotContains(t, readJSONPathList(t, filepath.Join(server.externalConfigDir, "external.json")), missingCanonical)
	assert.NotContains(t, readJSONPathList(t, filepath.Join(server.pinnedConfigDir, "pinned.json")), missingCanonical)
}

func TestBuildWorkspaceListResponseDoesNotEmitPhantomRootAfterPruningMissingAttachedRoot(t *testing.T) {
	server, _ := setupTestServer(t)
	server.externalConfigDir = t.TempDir()
	server.pinnedConfigDir = t.TempDir()

	baseDir := t.TempDir()
	metaRootDir := filepath.Join(baseDir, "meta-root")
	missingRootDir := filepath.Join(baseDir, "missing-root")
	forkDir := filepath.Join(baseDir, "fork-ws")

	require.NoError(t, os.MkdirAll(filepath.Join(metaRootDir, ".jj", "repo"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(forkDir, ".sgai"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(forkDir, ".jj"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(forkDir, ".jj", "repo"), []byte(filepath.Join(missingRootDir, ".jj", "repo")), 0o644))

	missingRootCanonical := resolveSymlinks(missingRootDir)
	forkCanonical := resolveSymlinks(forkDir)
	server.mu.Lock()
	server.externalDirs = map[string]bool{
		missingRootCanonical: true,
		forkCanonical:        true,
	}
	server.pinnedDirs = map[string]bool{
		missingRootCanonical: true,
	}
	server.mu.Unlock()
	server.classifyCache.set(missingRootCanonical, workspaceRoot)
	server.classifyCache.set(forkCanonical, workspaceFork)
	require.NoError(t, server.saveExternalDirs())
	require.NoError(t, server.savePinnedProjects())
	server.invalidateWorkspaceScanCache()

	state := server.buildWorkspaceListResponse()
	require.Len(t, state.Workspaces, 1)
	assert.Equal(t, filepath.Base(forkDir), state.Workspaces[0].Name)
	assert.Equal(t, forkCanonical, state.Workspaces[0].Dir)
	assert.False(t, state.Workspaces[0].IsRoot)
	assert.True(t, state.Workspaces[0].IsFork)
	assertRepositoryAction(t, &state.Workspaces[0].RepositoryAction, &repositoryActionExpectation{
		mode:              "fork",
		entryPoint:        "choose",
		defaultOperation:  "",
		allowedOperations: []string{"detach", "delete"},
		disabledReason:    "",
		attachedForkCount: 0,
	})

	server.mu.Lock()
	stateSnapshot := workspaceListState{
		externalDirs: maps.Clone(server.externalDirs),
		pinnedDirs:   maps.Clone(server.pinnedDirs),
	}
	server.mu.Unlock()
	assert.False(t, directorySetContains(stateSnapshot.externalDirs, missingRootCanonical))
	assert.False(t, directorySetContains(stateSnapshot.pinnedDirs, missingRootCanonical))
	assert.NotContains(t, readJSONPathList(t, filepath.Join(server.externalConfigDir, "external.json")), missingRootCanonical)
	assert.NotContains(t, readJSONPathList(t, filepath.Join(server.pinnedConfigDir, "pinned.json")), missingRootCanonical)
}

func TestBuildWorkspaceListResponseKeepsUnreadableAttachedWorkspaceState(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping permission test as root")
	}

	server, _ := setupTestServer(t)
	server.externalConfigDir = t.TempDir()
	server.pinnedConfigDir = t.TempDir()
	validDir := filepath.Join(t.TempDir(), "valid-ws")
	attachWorkspaceFixture(t, server, validDir, workspaceStandalone)

	lockedParent := filepath.Join(t.TempDir(), "locked-parent")
	lockedDir := filepath.Join(lockedParent, "locked-ws")
	require.NoError(t, os.MkdirAll(filepath.Join(lockedDir, ".sgai"), 0o755))
	attachWorkspaceFixture(t, server, lockedDir, workspaceStandalone)

	lockedCanonical := resolveSymlinks(lockedDir)
	server.mu.Lock()
	server.pinnedDirs[lockedCanonical] = true
	server.mu.Unlock()
	require.NoError(t, server.saveExternalDirs())
	require.NoError(t, server.savePinnedProjects())
	server.invalidateWorkspaceScanCache()

	require.NoError(t, os.Chmod(lockedParent, 0o000))
	defer func() {
		require.NoError(t, os.Chmod(lockedParent, 0o755))
	}()

	_, errStat := os.Stat(lockedDir)
	require.Error(t, errStat)
	require.False(t, os.IsNotExist(errStat))

	state := server.buildWorkspaceListResponse()
	require.Len(t, state.Workspaces, 1)
	assert.Equal(t, filepath.Base(validDir), state.Workspaces[0].Name)

	server.mu.Lock()
	_, stillAttached := server.externalDirs[lockedCanonical]
	_, stillPinned := server.pinnedDirs[lockedCanonical]
	server.mu.Unlock()
	assert.True(t, stillAttached)
	assert.True(t, stillPinned)
	assert.Contains(t, readJSONPathList(t, filepath.Join(server.externalConfigDir, "external.json")), lockedCanonical)
	assert.Contains(t, readJSONPathList(t, filepath.Join(server.pinnedConfigDir, "pinned.json")), lockedCanonical)
}

func TestBuildWorkspaceListResponseDoesNotEmitUnreadableAttachedRoot(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping permission test as root")
	}

	server, _ := setupTestServer(t)
	server.externalConfigDir = t.TempDir()
	server.pinnedConfigDir = t.TempDir()

	baseDir := t.TempDir()
	lockedParent := filepath.Join(baseDir, "locked-parent")
	lockedRootDir := filepath.Join(lockedParent, "locked-root")
	forkDir := filepath.Join(baseDir, "fork-ws")
	lockedRootCanonical := resolveSymlinks(lockedRootDir)

	require.NoError(t, os.MkdirAll(filepath.Join(lockedRootDir, ".jj", "repo"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(forkDir, ".sgai"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(forkDir, ".jj"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(forkDir, ".jj", "repo"), []byte(filepath.Join(lockedRootCanonical, ".jj", "repo")), 0o644))

	forkCanonical := resolveSymlinks(forkDir)
	server.mu.Lock()
	server.externalDirs = map[string]bool{
		lockedRootCanonical: true,
		forkCanonical:       true,
	}
	server.mu.Unlock()
	server.classifyCache.set(lockedRootCanonical, workspaceRoot)
	server.classifyCache.set(forkCanonical, workspaceFork)
	require.NoError(t, server.saveExternalDirs())
	server.invalidateWorkspaceScanCache()

	require.NoError(t, os.Chmod(lockedParent, 0o000))
	defer func() {
		require.NoError(t, os.Chmod(lockedParent, 0o755))
	}()

	_, errStat := os.Stat(lockedRootDir)
	require.Error(t, errStat)
	require.False(t, os.IsNotExist(errStat))

	state := server.buildWorkspaceListResponse()
	require.Len(t, state.Workspaces, 1)
	assert.Equal(t, filepath.Base(forkDir), state.Workspaces[0].Name)
	assert.Equal(t, forkCanonical, state.Workspaces[0].Dir)
	assert.False(t, state.Workspaces[0].IsRoot)
	assert.True(t, state.Workspaces[0].IsFork)
	assertRepositoryAction(t, &state.Workspaces[0].RepositoryAction, &repositoryActionExpectation{
		mode:              "fork",
		entryPoint:        "choose",
		defaultOperation:  "",
		allowedOperations: []string{"detach", "delete"},
		disabledReason:    "",
		attachedForkCount: 0,
	})

	server.mu.Lock()
	_, stillAttached := server.externalDirs[lockedRootCanonical]
	server.mu.Unlock()
	assert.True(t, stillAttached)
	assert.Contains(t, readJSONPathList(t, filepath.Join(server.externalConfigDir, "external.json")), lockedRootCanonical)
}

type repositoryActionExpectation struct {
	mode              string
	entryPoint        string
	defaultOperation  string
	allowedOperations []string
	disabledReason    string
	attachedForkCount int
}

func assertRepositoryAction(t *testing.T, repositoryAction *apiRepositoryAction, want *repositoryActionExpectation) {
	t.Helper()

	assert.Equal(t, want.mode, repositoryAction.RepositoryMode)
	assert.Equal(t, want.entryPoint, repositoryAction.EntryPoint)
	assert.Equal(t, want.attachedForkCount, repositoryAction.AttachedForks)

	if want.defaultOperation == "" {
		assert.Empty(t, repositoryAction.DefaultOp)
	} else {
		assert.Equal(t, want.defaultOperation, repositoryAction.DefaultOp)
	}

	if want.disabledReason == "" {
		assert.Empty(t, repositoryAction.DisabledReason)
	} else {
		assert.Equal(t, want.disabledReason, repositoryAction.DisabledReason)
	}

	assert.ElementsMatch(t, want.allowedOperations, repositoryAction.AllowedOps)
}

func decodeWorkspaceListByName(t *testing.T, data []byte) map[string]apiWorkspaceListEntry {
	t.Helper()

	var response apiWorkspaceListResponse
	require.NoError(t, json.Unmarshal(data, &response))

	result := make(map[string]apiWorkspaceListEntry, len(response.Workspaces))
	for i := range response.Workspaces {
		workspace := response.Workspaces[i]
		result[workspace.Name] = workspace
	}
	return result
}

func readJSONPathList(t *testing.T, path string) []string {
	t.Helper()

	data, errRead := os.ReadFile(path)
	require.NoError(t, errRead)

	var result []string
	require.NoError(t, json.Unmarshal(data, &result))
	for i := range result {
		result[i] = resolveSymlinks(result[i])
	}
	return result
}

func pathListContains(paths []string, workspacePath string) bool {
	set := make(map[string]bool, len(paths))
	for _, path := range paths {
		set[path] = true
	}
	return directorySetContains(set, workspacePath)
}

func setupAttachedDuplicateNameWorkspaces(t *testing.T, server *Server, kind workspaceKind) (firstCanonical, secondCanonical string) {
	t.Helper()

	baseDir := t.TempDir()
	firstDir := filepath.Join(baseDir, "first", "shared-ws")
	secondDir := filepath.Join(baseDir, "second", "shared-ws")
	require.NoError(t, os.MkdirAll(firstDir, 0o755))
	require.NoError(t, os.MkdirAll(secondDir, 0o755))
	_, errAttachFirst := server.attachExternalWorkspaceService(firstDir)
	require.NoError(t, errAttachFirst)
	_, errAttachSecond := server.attachExternalWorkspaceService(secondDir)
	require.NoError(t, errAttachSecond)
	server.classifyCache.set(firstDir, kind)
	server.classifyCache.set(resolveSymlinks(firstDir), kind)
	server.classifyCache.set(secondDir, kind)
	server.classifyCache.set(resolveSymlinks(secondDir), kind)
	server.invalidateWorkspaceScanCache()
	firstCanonical = resolveSymlinks(firstDir)
	secondCanonical = resolveSymlinks(secondDir)
	return firstCanonical, secondCanonical
}

func setupAttachedJJRootAndFork(t *testing.T, server *Server) (rootDir, forkDir string) {
	t.Helper()
	return setupNamedAttachedJJRootAndFork(t, server, "root-ws", "fork-ws")
}

func setupNamedAttachedJJRootAndFork(t *testing.T, server *Server, rootName, forkName string) (rootDir, forkDir string) {
	t.Helper()

	baseDir := t.TempDir()
	rootDir = filepath.Join(baseDir, rootName)
	forkDir = filepath.Join(baseDir, forkName)
	require.NoError(t, os.MkdirAll(rootDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(rootDir, ".sgai"), 0o755))

	runJJ := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("jj", args...)
		cmd.Dir = dir
		output, errRun := cmd.CombinedOutput()
		require.NoErrorf(t, errRun, "jj %v: %s", args, output)
	}

	runJJ(rootDir, "git", "init", ".")
	require.NoError(t, os.WriteFile(filepath.Join(rootDir, "README.md"), []byte("root\n"), 0o644))
	runJJ(rootDir, "commit", "-m", "initial")
	runJJ(rootDir, "bookmark", "create", "main", "-r", "@-")
	runJJ(rootDir, "workspace", "add", forkDir)
	require.NoError(t, os.MkdirAll(filepath.Join(forkDir, ".sgai"), 0o755))

	rootCanonical := resolveSymlinks(rootDir)
	forkCanonical := resolveSymlinks(forkDir)
	server.mu.Lock()
	server.externalDirs[rootCanonical] = true
	server.externalDirs[forkCanonical] = true
	server.mu.Unlock()
	server.invalidateWorkspaceScanCache()

	rootDir = rootCanonical
	forkDir = forkCanonical
	return rootDir, forkDir
}

func runJJForTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("jj", args...)
	cmd.Dir = dir
	output, errRun := cmd.CombinedOutput()
	require.NoErrorf(t, errRun, "jj %v: %s", args, output)
	return string(output)
}
