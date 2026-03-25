package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ucirello/sgai/pkg/state"
)

func cacheWorkspacePageState(srv *Server, workspacePath string) {
	var pageState apiWorkspaceFullState
	pageState.Name = filepath.Base(workspacePath)
	srv.workspacePageCache.set(workspacePath, pageState)
}

func hasCachedWorkspacePageState(srv *Server, workspacePath string) bool {
	_, ok := srv.workspacePageCache.get(workspacePath)
	return ok
}

func hasCachedWorkspaceListState(srv *Server) bool {
	_, ok := srv.workspaceListCache.get("workspaces")
	return ok
}

func setLaterFileModTime(t *testing.T, path string, modTime time.Time) {
	t.Helper()
	require.NoError(t, os.Chtimes(path, modTime, modTime))
}

func TestCheckWorkspaceStateWithoutStateFile(t *testing.T) {
	srv, _ := setupTestServer(t)
	dir := t.TempDir()
	snapshots := make(map[string]workspaceStateSnapshot)
	activeWorkspaces := make(map[string]bool)

	srv.checkWorkspaceState(dir, snapshots, activeWorkspaces)

	assert.True(t, activeWorkspaces[dir])
	assert.NotContains(t, snapshots, dir)
}

func TestCheckWorkspaceStateStoresSnapshotForWorkspaceStateFile(t *testing.T) {
	srv, _ := setupTestServer(t)
	dir := t.TempDir()
	stateFile := statePath(dir)
	require.NoError(t, os.MkdirAll(filepath.Dir(stateFile), 0o755))
	_, errCoord := state.NewCoordinatorWith(stateFile, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
	}))
	require.NoError(t, errCoord)

	snapshots := make(map[string]workspaceStateSnapshot)
	activeWorkspaces := make(map[string]bool)

	srv.checkWorkspaceState(dir, snapshots, activeWorkspaces)

	assert.True(t, activeWorkspaces[dir])
	assert.Contains(t, snapshots, dir)
	assert.False(t, snapshots[dir].modTime.IsZero())
}

func TestCheckWorkspaceStateInvalidatesWorkspacePageCacheForInactiveStateChanges(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "watcher-inactive")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Goal"), 0o644))
	writeWorkflowStateToDisk(t, wsDir, workflowRef(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.Task = "step 1"
	}))

	snapshots := make(map[string]workspaceStateSnapshot)
	activeWorkspaces := make(map[string]bool)
	srv.checkWorkspaceState(wsDir, snapshots, activeWorkspaces)
	cacheWorkspacePageState(srv, wsDir)

	info, errStat := os.Stat(statePath(wsDir))
	require.NoError(t, errStat)
	writeWorkflowStateToDisk(t, wsDir, workflowRef(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
		workflow.Task = "step 2"
	}))
	setLaterFileModTime(t, statePath(wsDir), info.ModTime().Add(time.Second))

	srv.checkWorkspaceState(wsDir, snapshots, activeWorkspaces)

	assert.False(t, hasCachedWorkspacePageState(srv, wsDir))
}

func TestCheckWorkspaceStateKeepsWorkspaceListCacheForInactivePageOnlyChanges(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "watcher-page-only")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Goal"), 0o644))
	writeWorkflowStateToDisk(t, wsDir, workflowRef(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.Task = "stable summary"
	}))

	snapshots := make(map[string]workspaceStateSnapshot)
	activeWorkspaces := make(map[string]bool)
	srv.checkWorkspaceState(wsDir, snapshots, activeWorkspaces)
	_ = srv.loadWorkspaceListResponse()
	_, errLoad := srv.loadWorkspacePageState(wsDir)
	require.NoError(t, errLoad)

	info, errStat := os.Stat(statePath(wsDir))
	require.NoError(t, errStat)
	writeWorkflowStateToDisk(t, wsDir, workflowRef(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.Task = "stable summary"
		workflow.Messages = []state.Message{messageWith(func(message *state.Message) {
			message.ID = 1
			message.FromAgent = "go-developer"
			message.ToAgent = "coordinator"
			message.Body = "page-only change"
		})}
	}))
	setLaterFileModTime(t, statePath(wsDir), info.ModTime().Add(time.Second))

	srv.checkWorkspaceState(wsDir, snapshots, activeWorkspaces)

	assert.True(t, hasCachedWorkspaceListState(srv))
	assert.False(t, hasCachedWorkspacePageState(srv, wsDir))
}

func TestCheckWorkspaceStateKeepsWorkspacePageCacheForRunningSessionWorkflowChanges(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "watcher-running")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Goal"), 0o644))
	attachRunningSessionCoordinator(t, srv, wsDir, workflowRef(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.Progress = []state.ProgressEntry{{Timestamp: time.Now().UTC().Format(time.RFC3339), Agent: "coordinator", Description: "step 1"}}
	}))

	snapshots := make(map[string]workspaceStateSnapshot)
	activeWorkspaces := make(map[string]bool)
	srv.checkWorkspaceState(wsDir, snapshots, activeWorkspaces)
	cacheWorkspacePageState(srv, wsDir)

	info, errStat := os.Stat(statePath(wsDir))
	require.NoError(t, errStat)
	coord := srv.workspaceCoordinator(wsDir)
	require.NoError(t, coord.UpdateState(func(workflow *state.Workflow) {
		workflow.Progress = append(workflow.Progress, state.ProgressEntry{Timestamp: time.Now().UTC().Format(time.RFC3339), Agent: "coordinator", Description: "step 2"})
	}))
	setLaterFileModTime(t, statePath(wsDir), info.ModTime().Add(time.Second))

	srv.checkWorkspaceState(wsDir, snapshots, activeWorkspaces)

	assert.True(t, hasCachedWorkspacePageState(srv, wsDir))
}

func TestCheckWorkspaceStateInvalidatesWorkspacePageCacheForGoalChangesWhileRunning(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "watcher-goal-running")
	goalPath := filepath.Join(wsDir, "GOAL.md")
	require.NoError(t, os.WriteFile(goalPath, []byte("# Goal"), 0o644))
	attachRunningSessionCoordinator(t, srv, wsDir, workflowRef(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
	}))

	snapshots := make(map[string]workspaceStateSnapshot)
	activeWorkspaces := make(map[string]bool)
	srv.checkWorkspaceState(wsDir, snapshots, activeWorkspaces)
	cacheWorkspacePageState(srv, wsDir)

	goalInfo, errStat := os.Stat(goalPath)
	require.NoError(t, errStat)
	require.NoError(t, os.WriteFile(goalPath, []byte("# Goal updated"), 0o644))
	setLaterFileModTime(t, goalPath, goalInfo.ModTime().Add(time.Second))

	srv.checkWorkspaceState(wsDir, snapshots, activeWorkspaces)

	assert.False(t, hasCachedWorkspacePageState(srv, wsDir))
}

func TestPollWorkspaceStatesRemovesStaleSnapshots(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "watcher-cleanup")
	writeWorkflowStateToDisk(t, wsDir, workflowRef(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
	}))

	snapshots := map[string]workspaceStateSnapshot{
		"/nonexistent/removed-ws": {modTime: time.Now(), goalModTime: time.Time{}, goalHash: ""},
	}

	srv.pollWorkspaceStates(snapshots)

	assert.Contains(t, snapshots, wsDir)
	assert.NotContains(t, snapshots, "/nonexistent/removed-ws")
}
