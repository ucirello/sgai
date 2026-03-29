package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ucirello/sgai/pkg/state"
)

func TestForkWorkspaceService(t *testing.T) {
	tests := []struct {
		name        string
		workspace   string
		goalContent string
		setupFunc   func(*testing.T, string) string
		wantErr     bool
		errContains string
		validate    func(*testing.T, string, forkWorkspaceResult)
	}{
		{
			name:        "forkFromRootWorkspace",
			workspace:   "",
			goalContent: "---\nflow: |\n  \"agent1\" -> \"agent2\"\n---\n# Test Goal",
			setupFunc: func(t *testing.T, rootDir string) string {
				t.Helper()
				workspacePath := filepath.Join(rootDir, "root-workspace")
				require.NoError(t, os.MkdirAll(workspacePath, 0o755))
				require.NoError(t, initializeWorkspace(workspacePath))

				goalPath := filepath.Join(workspacePath, "GOAL.md")
				require.NoError(t, os.WriteFile(goalPath, []byte("initial goal"), 0o644))

				return workspacePath
			},
			wantErr:     false,
			errContains: "",
			validate: func(t *testing.T, _ string, result forkWorkspaceResult) {
				t.Helper()
				const wantGoalContent = "---\nflow: |\n  \"agent1\" -> \"agent2\"\n---\n# Test Goal"

				assertForkNameHasRootPrefix(t, result.Name, "root-workspace")
				assert.DirExists(t, result.Dir)
				assert.Equal(t, result.Name, filepath.Base(result.Dir))
				assert.Equal(t, "root-workspace", result.Parent)
				assert.NotEmpty(t, result.CreatedAt)

				goalPath := filepath.Join(result.Dir, "GOAL.md")
				assert.FileExists(t, goalPath)

				goalContent, errRead := os.ReadFile(goalPath)
				require.NoError(t, errRead)
				assert.Equal(t, wantGoalContent, string(goalContent))
			},
		},
		{
			name:        "forkFromForkWorkspace",
			workspace:   "",
			goalContent: "---\nflow: |\n  \"agent1\" -> \"agent2\"\n---\n# Test Goal",
			setupFunc: func(t *testing.T, rootDir string) string {
				t.Helper()
				rootPath := filepath.Join(rootDir, "root-workspace")
				require.NoError(t, os.MkdirAll(rootPath, 0o755))
				require.NoError(t, initializeWorkspace(rootPath))

				forkPath := filepath.Join(rootDir, "fork-workspace")
				require.NoError(t, os.MkdirAll(forkPath, 0o755))
				require.NoError(t, unpackSkeleton(forkPath))
				require.NoError(t, addGitExclude(forkPath))

				require.NoError(t, os.MkdirAll(filepath.Join(forkPath, ".jj"), 0o755))
				repoFile := filepath.Join(forkPath, ".jj", "repo")
				require.NoError(t, os.WriteFile(repoFile, []byte(rootPath), 0o644))

				return forkPath
			},
			wantErr:     true,
			errContains: "forks cannot create new forks",
			validate:    nil,
		},
		{
			name:        "forkWithEmptyGoalContent",
			workspace:   "",
			goalContent: "",
			setupFunc: func(t *testing.T, rootDir string) string {
				t.Helper()
				workspacePath := filepath.Join(rootDir, "root-workspace")
				require.NoError(t, os.MkdirAll(workspacePath, 0o755))
				require.NoError(t, initializeWorkspace(workspacePath))

				return workspacePath
			},
			wantErr:     true,
			errContains: "GOAL.md must have content describing the goal",
			validate:    nil,
		},
		{
			name:        "forkWithOnlyFrontmatter",
			workspace:   "",
			goalContent: "---\nflow: |\n  \"agent1\" -> \"agent2\"\n---\n",
			setupFunc: func(t *testing.T, rootDir string) string {
				t.Helper()
				workspacePath := filepath.Join(rootDir, "root-workspace")
				require.NoError(t, os.MkdirAll(workspacePath, 0o755))
				require.NoError(t, initializeWorkspace(workspacePath))

				return workspacePath
			},
			wantErr:     true,
			errContains: "GOAL.md must have content describing the goal",
			validate:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				rootDir := t.TempDir()
				server := NewServer(rootDir, newTestServerPaths(), "")
				expectedCreatedAt := time.Now().UTC().Format(time.RFC3339)

				var workspacePath string
				if tt.setupFunc != nil {
					workspacePath = tt.setupFunc(t, rootDir)
				}

				result, err := server.forkWorkspaceService(workspacePath, tt.goalContent)

				if tt.wantErr {
					require.Error(t, err)
					if tt.errContains != "" {
						assert.Contains(t, err.Error(), tt.errContains)
					}
					return
				}

				require.NoError(t, err)
				assert.Equal(t, expectedCreatedAt, result.CreatedAt)
				if tt.validate != nil {
					tt.validate(t, rootDir, result)
				}
			})
		})
	}
}

func TestForkExternalWorkspaceSiblingPlacement(t *testing.T) {
	sgaiRoot := t.TempDir()
	externalParent := t.TempDir()
	externalRepo := filepath.Join(externalParent, "my-external-repo")
	require.NoError(t, os.MkdirAll(externalRepo, 0o755))
	require.NoError(t, initializeWorkspace(externalRepo))

	server := NewServer(sgaiRoot, newTestServerPaths(), "")
	server.mu.Lock()
	server.externalDirs[resolveSymlinks(externalRepo)] = true
	server.mu.Unlock()

	result, err := server.forkWorkspaceService(externalRepo, "---\nflow: |\n  \"a\" -> \"b\"\n---\n# Test Goal")
	require.NoError(t, err)

	assertForkNameHasRootPrefix(t, result.Name, "my-external-repo")
	assert.Equal(t, externalParent, filepath.Dir(result.Dir))
	assert.DirExists(t, result.Dir)
	assert.NotEqual(t, sgaiRoot, filepath.Dir(result.Dir))
}

func TestDeleteWorkspaceService(t *testing.T) {
	tests := []struct {
		name        string
		setupFunc   func(*testing.T, string) string
		wantErr     bool
		errContains string
		validate    func(*testing.T, string)
	}{
		{
			name: "deleteExistingWorkspace",
			setupFunc: func(t *testing.T, rootDir string) string {
				t.Helper()
				workspacePath := filepath.Join(rootDir, "test-workspace")
				require.NoError(t, os.MkdirAll(workspacePath, 0o755))
				require.NoError(t, initializeWorkspace(workspacePath))
				return workspacePath
			},
			wantErr:     true,
			errContains: "workspace operation is not allowed",
			validate: func(t *testing.T, workspacePath string) {
				t.Helper()
				assert.DirExists(t, workspacePath)
			},
		},
		{
			name: "deleteNonExistentWorkspace",
			setupFunc: func(_ *testing.T, rootDir string) string {
				return filepath.Join(rootDir, "non-existent-workspace")
			},
			wantErr:     true,
			errContains: "workspace operation is not allowed",
			validate: func(_ *testing.T, _ string) {
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootDir := t.TempDir()
			server := NewServer(rootDir, newTestServerPaths(), "")

			workspacePath := tt.setupFunc(t, rootDir)

			result, err := server.deleteWorkspaceService(workspacePath)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			assert.True(t, result.Deleted)
			if tt.validate != nil {
				tt.validate(t, workspacePath)
			}
		})
	}
}

func TestDeleteForkService(t *testing.T) {
	tests := []struct {
		name        string
		setupFunc   func(*testing.T, string) (string, string)
		confirm     bool
		wantErr     bool
		errContains string
		validate    func(*testing.T, string, string)
	}{
		{
			name: "deleteForkFromNonRootWorkspace",
			setupFunc: func(t *testing.T, rootDir string) (string, string) {
				t.Helper()
				workspacePath := filepath.Join(rootDir, "standalone-workspace")
				require.NoError(t, os.MkdirAll(workspacePath, 0o755))
				require.NoError(t, initializeWorkspace(workspacePath))

				return workspacePath, workspacePath
			},
			confirm:     true,
			wantErr:     true,
			errContains: "workspace is not a root",
			validate:    nil,
		},
		{
			name: "deleteForkWithoutConfirmation",
			setupFunc: func(t *testing.T, rootDir string) (string, string) {
				t.Helper()
				rootPath := filepath.Join(rootDir, "root-workspace")
				require.NoError(t, os.MkdirAll(rootPath, 0o755))
				require.NoError(t, initializeWorkspace(rootPath))
				require.NoError(t, os.MkdirAll(filepath.Join(rootPath, ".jj", "repo"), 0o755))
				goalPath := filepath.Join(rootPath, "GOAL.md")
				require.NoError(t, os.WriteFile(goalPath, []byte("# Test Goal"), 0o644))

				forkPath := filepath.Join(rootDir, "fork-workspace")
				require.NoError(t, os.MkdirAll(forkPath, 0o755))
				require.NoError(t, initializeWorkspace(forkPath))

				return rootPath, forkPath
			},
			confirm:     false,
			wantErr:     true,
			errContains: "workspace is not a root",
			validate:    nil,
		},
		{
			name: "deleteNonExistentFork",
			setupFunc: func(t *testing.T, rootDir string) (string, string) {
				t.Helper()
				rootPath := filepath.Join(rootDir, "root-workspace")
				require.NoError(t, os.MkdirAll(rootPath, 0o755))
				require.NoError(t, initializeWorkspace(rootPath))
				require.NoError(t, os.MkdirAll(filepath.Join(rootPath, ".jj", "repo"), 0o755))
				goalPath := filepath.Join(rootPath, "GOAL.md")
				require.NoError(t, os.WriteFile(goalPath, []byte("# Test Goal"), 0o644))

				forkPath := filepath.Join(rootDir, "non-existent-fork")
				return rootPath, forkPath
			},
			confirm:     true,
			wantErr:     true,
			errContains: "workspace is not a root",
			validate:    nil,
		},
		{
			name: "deleteForkThatIsNotAFork",
			setupFunc: func(t *testing.T, rootDir string) (string, string) {
				t.Helper()
				rootPath := filepath.Join(rootDir, "root-workspace")
				require.NoError(t, os.MkdirAll(rootPath, 0o755))
				require.NoError(t, initializeWorkspace(rootPath))
				require.NoError(t, os.MkdirAll(filepath.Join(rootPath, ".jj", "repo"), 0o755))
				goalPath := filepath.Join(rootPath, "GOAL.md")
				require.NoError(t, os.WriteFile(goalPath, []byte("# Test Goal"), 0o644))

				standalonePath := filepath.Join(rootDir, "standalone-workspace")
				require.NoError(t, os.MkdirAll(standalonePath, 0o755))
				require.NoError(t, initializeWorkspace(standalonePath))

				return rootPath, standalonePath
			},
			confirm:     true,
			wantErr:     true,
			errContains: "workspace is not a root",
			validate:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootDir := t.TempDir()
			server := NewServer(rootDir, newTestServerPaths(), "")

			workspacePath, forkPath := tt.setupFunc(t, rootDir)

			result, err := server.deleteForkService(workspacePath, forkPath, tt.confirm)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			assert.True(t, result.Deleted)
			if tt.validate != nil {
				tt.validate(t, workspacePath, forkPath)
			}
		})
	}
}

func TestGetGoalService(t *testing.T) {
	tests := []struct {
		name        string
		setupFunc   func(*testing.T, string) string
		wantErr     bool
		errContains string
		validate    func(*testing.T, getGoalResult)
	}{
		{
			name: "getExistingGoal",
			setupFunc: func(t *testing.T, rootDir string) string {
				t.Helper()
				workspacePath := filepath.Join(rootDir, "test-workspace")
				require.NoError(t, os.MkdirAll(workspacePath, 0o755))
				require.NoError(t, initializeWorkspace(workspacePath))

				goalContent := "---\nflow: |\n  \"agent1\" -> \"agent2\"\n---\n# Test Goal"
				goalPath := filepath.Join(workspacePath, "GOAL.md")
				require.NoError(t, os.WriteFile(goalPath, []byte(goalContent), 0o644))

				return workspacePath
			},
			wantErr:     false,
			errContains: "",
			validate: func(t *testing.T, result getGoalResult) {
				t.Helper()
				assert.Contains(t, result.Content, "# Test Goal")
			},
		},
		{
			name: "getGoalFromNonExistentWorkspace",
			setupFunc: func(_ *testing.T, rootDir string) string {
				return filepath.Join(rootDir, "non-existent-workspace")
			},
			wantErr:     true,
			errContains: "failed to read GOAL.md",
			validate:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootDir := t.TempDir()
			server := NewServer(rootDir, newTestServerPaths(), "")

			workspacePath := tt.setupFunc(t, rootDir)

			result, err := server.getGoalService(workspacePath)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, result)
			}
		})
	}
}

func TestUpdateGoalService(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		setupFunc   func(*testing.T, string) string
		wantErr     bool
		errContains string
		validate    func(*testing.T, string, updateGoalResult)
	}{
		{
			name:    "updateExistingGoal",
			content: "---\nflow: |\n  \"agent1\" -> \"agent2\"\n---\n# Updated Goal",
			setupFunc: func(t *testing.T, rootDir string) string {
				t.Helper()
				workspacePath := filepath.Join(rootDir, "test-workspace")
				require.NoError(t, os.MkdirAll(workspacePath, 0o755))
				require.NoError(t, initializeWorkspace(workspacePath))

				goalPath := filepath.Join(workspacePath, "GOAL.md")
				require.NoError(t, os.WriteFile(goalPath, []byte("initial goal"), 0o644))

				return workspacePath
			},
			wantErr:     false,
			errContains: "",
			validate: func(t *testing.T, workspacePath string, result updateGoalResult) {
				t.Helper()
				assert.True(t, result.Updated)
				assert.Equal(t, "test-workspace", result.Workspace)

				goalPath := filepath.Join(workspacePath, "GOAL.md")
				data, err := os.ReadFile(goalPath)
				require.NoError(t, err)
				assert.Contains(t, string(data), "# Updated Goal")
			},
		},
		{
			name:    "updateGoalWithEmptyContent",
			content: "",
			setupFunc: func(t *testing.T, rootDir string) string {
				t.Helper()
				workspacePath := filepath.Join(rootDir, "test-workspace")
				require.NoError(t, os.MkdirAll(workspacePath, 0o755))
				require.NoError(t, initializeWorkspace(workspacePath))
				return workspacePath
			},
			wantErr:     true,
			errContains: "content cannot be empty",
			validate:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootDir := t.TempDir()
			server := NewServer(rootDir, newTestServerPaths(), "")

			workspacePath := tt.setupFunc(t, rootDir)

			result, err := server.updateGoalService(workspacePath, tt.content)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, workspacePath, result)
			}
		})
	}
}

func TestTogglePinService(t *testing.T) {
	tests := []struct {
		name        string
		setupFunc   func(*testing.T, string) string
		wantErr     bool
		errContains string
		validate    func(*testing.T, *Server, string, togglePinResult)
	}{
		{
			name: "togglePinOn",
			setupFunc: func(t *testing.T, rootDir string) string {
				t.Helper()
				workspacePath := filepath.Join(rootDir, "test-workspace")
				require.NoError(t, os.MkdirAll(workspacePath, 0o755))
				require.NoError(t, initializeWorkspace(workspacePath))
				return workspacePath
			},
			wantErr:     false,
			errContains: "",
			validate: func(t *testing.T, server *Server, workspacePath string, result togglePinResult) {
				t.Helper()
				assert.True(t, result.Pinned)
				assert.True(t, server.isPinned(workspacePath))
			},
		},
		{
			name: "togglePinOff",
			setupFunc: func(t *testing.T, rootDir string) string {
				t.Helper()
				workspacePath := filepath.Join(rootDir, "test-workspace")
				require.NoError(t, os.MkdirAll(workspacePath, 0o755))
				require.NoError(t, initializeWorkspace(workspacePath))
				return workspacePath
			},
			wantErr:     false,
			errContains: "",
			validate: func(t *testing.T, server *Server, workspacePath string, result togglePinResult) {
				t.Helper()
				assert.True(t, result.Pinned)

				result2, err := server.togglePinService(workspacePath)
				require.NoError(t, err)
				assert.False(t, result2.Pinned)
				assert.False(t, server.isPinned(workspacePath))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootDir := t.TempDir()
			server := NewServer(rootDir, newTestServerPaths(), "")

			workspacePath := tt.setupFunc(t, rootDir)

			result, err := server.togglePinService(workspacePath)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, server, workspacePath, result)
			}
		})
	}
}

func TestFailForkWorkspaceSetup(t *testing.T) {
	tests := []struct {
		name        string
		message     string
		errCause    error
		wantErr     bool
		errContains string
	}{
		{
			name:        "failWithSetupError",
			message:     "failed to unpack skeleton",
			errCause:    errors.New("skeleton unpack failed"),
			wantErr:     true,
			errContains: "failed to unpack skeleton",
		},
		{
			name:        "failWithGitExcludeError",
			message:     "failed to add git exclude",
			errCause:    errors.New("git exclude failed"),
			wantErr:     true,
			errContains: "failed to add git exclude",
		},
		{
			name:        "failWithGoalWriteError",
			message:     "failed to create GOAL.md",
			errCause:    errors.New("goal write failed"),
			wantErr:     true,
			errContains: "failed to create GOAL.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootDir := t.TempDir()

			workspacePath := filepath.Join(rootDir, "root-workspace")
			require.NoError(t, os.MkdirAll(workspacePath, 0o755))

			forkPath := filepath.Join(rootDir, "fork-workspace")
			require.NoError(t, os.MkdirAll(forkPath, 0o755))

			err := failForkWorkspaceSetup(workspacePath, forkPath, tt.message, tt.errCause)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			}
		})
	}
}

func TestRollbackForkWorkspaceCreation(t *testing.T) {
	tests := []struct {
		name        string
		setupFunc   func(*testing.T, string, string)
		wantErr     bool
		errContains string
		validate    func(*testing.T, string)
	}{
		{
			name: "rollbackForkWithoutJjRepo",
			setupFunc: func(t *testing.T, _, forkPath string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(forkPath, 0o755))
			},
			wantErr:     true,
			errContains: "failed to forget fork workspace during rollback",
			validate: func(t *testing.T, forkPath string) {
				t.Helper()
				assert.NoDirExists(t, forkPath)
			},
		},
		{
			name: "rollbackNonExistentFork",
			setupFunc: func(_ *testing.T, _, _ string) {
			},
			wantErr:     true,
			errContains: "failed to forget fork workspace during rollback",
			validate:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootDir := t.TempDir()

			workspacePath := filepath.Join(rootDir, "root-workspace")
			require.NoError(t, os.MkdirAll(workspacePath, 0o755))

			forkPath := filepath.Join(rootDir, "fork-workspace")

			tt.setupFunc(t, workspacePath, forkPath)

			err := rollbackForkWorkspaceCreation(workspacePath, forkPath)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, forkPath)
			}
		})
	}
}

func TestDeleteMessageService(t *testing.T) {
	tests := []struct {
		name        string
		messageID   int
		setupFunc   func(*testing.T, string)
		wantErr     bool
		errContains string
		validate    func(*testing.T, deleteMessageResult)
	}{
		{
			name:      "deleteNonExistentMessage",
			messageID: 999,
			setupFunc: func(t *testing.T, workspacePath string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".sgai"), 0o755))
			},
			wantErr:     true,
			errContains: "message not found",
			validate:    nil,
		},
		{
			name:      "deleteExistingMessage",
			messageID: 1,
			setupFunc: func(t *testing.T, workspacePath string) {
				t.Helper()
				sgaiDir := filepath.Join(workspacePath, ".sgai")
				require.NoError(t, os.MkdirAll(sgaiDir, 0o755))

				stateData := `{
					"status": "working",
					"messages": [
						{
							"id": 1,
							"fromAgent": "agent1",
							"toAgent": "agent2",
							"body": "test message",
							"read": false,
							"createdAt": "2026-03-05T10:00:00Z"
						}
					]
				}`
				statePath := filepath.Join(sgaiDir, "state.json")
				require.NoError(t, os.WriteFile(statePath, []byte(stateData), 0o644))
			},
			wantErr:     false,
			errContains: "",
			validate: func(t *testing.T, result deleteMessageResult) {
				t.Helper()
				assert.True(t, result.Deleted)
				assert.Equal(t, 1, result.ID)
			},
		},
		{
			name:      "deleteReadMessage",
			messageID: 2,
			setupFunc: func(t *testing.T, workspacePath string) {
				t.Helper()
				sgaiDir := filepath.Join(workspacePath, ".sgai")
				require.NoError(t, os.MkdirAll(sgaiDir, 0o755))

				stateData := `{
					"status": "working",
					"messages": [
						{
							"id": 2,
							"fromAgent": "agent1",
							"toAgent": "agent2",
							"body": "read message",
							"read": true,
							"readAt": "2026-03-05T11:00:00Z",
							"readBy": "agent2",
							"createdAt": "2026-03-05T10:00:00Z"
						}
					]
				}`
				statePath := filepath.Join(sgaiDir, "state.json")
				require.NoError(t, os.WriteFile(statePath, []byte(stateData), 0o644))
			},
			wantErr:     false,
			errContains: "",
			validate: func(t *testing.T, result deleteMessageResult) {
				t.Helper()
				assert.True(t, result.Deleted)
				assert.Equal(t, 2, result.ID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootDir := t.TempDir()
			server := NewServer(rootDir, newTestServerPaths(), "")

			workspacePath := filepath.Join(rootDir, "test-workspace")
			require.NoError(t, os.MkdirAll(workspacePath, 0o755))
			require.NoError(t, initializeWorkspace(workspacePath))

			tt.setupFunc(t, workspacePath)

			result, err := server.deleteMessageService(workspacePath, tt.messageID)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, result)
			}
		})
	}
}

func TestDeleteForkByPathServiceNonExistent(t *testing.T) {
	rootDir := t.TempDir()
	server := NewServer(rootDir, newTestServerPaths(), "")

	forkPath := filepath.Join(rootDir, "non-existent-fork")
	_, err := server.deleteForkByPathService(forkPath)
	require.Error(t, err)
}

func TestDeleteForkByPathServiceStandalone(t *testing.T) {
	rootDir := t.TempDir()
	server := NewServer(rootDir, newTestServerPaths(), "")

	workspacePath := filepath.Join(rootDir, "standalone-workspace")
	require.NoError(t, os.MkdirAll(workspacePath, 0o755))
	require.NoError(t, initializeWorkspace(workspacePath))

	_, err := server.deleteForkByPathService(workspacePath)
	require.Error(t, err)
}

func TestGoalContentBodyIsEmpty(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name:     "emptyContent",
			content:  "",
			expected: true,
		},
		{
			name:     "onlyFrontmatter",
			content:  "---\nflow: |\n  \"agent1\" -> \"agent2\"\n---\n",
			expected: true,
		},
		{
			name:     "frontmatterWithBody",
			content:  "---\nflow: |\n  \"agent1\" -> \"agent2\"\n---\n# Test Goal",
			expected: false,
		},
		{
			name:     "frontmatterWithWhitespaceBody",
			content:  "---\nflow: |\n  \"agent1\" -> \"agent2\"\n---\n   \n\t\n",
			expected: true,
		},
		{
			name:     "noFrontmatter",
			content:  "# Test Goal",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := goalContentBodyIsEmpty(tt.content)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWriteGoalContent(t *testing.T) {
	rootDir := t.TempDir()

	goalPath := filepath.Join(rootDir, "GOAL.md")
	content := "---\nflow: |\n  \"agent1\" -> \"agent2\"\n---\n# Test Goal"

	err := writeGoalContent(rootDir, content)
	require.NoError(t, err)

	data, err := os.ReadFile(goalPath)
	require.NoError(t, err)
	assert.Equal(t, content, string(data))
}

func TestDeleteMessageServiceNotFoundError(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws-delmsg-nf")
	sp := filepath.Join(wsDir, ".sgai", "state.json")
	_, errCoord := state.NewCoordinatorWith(sp, newTestWorkflow())
	require.NoError(t, errCoord)
	_, errDelete := server.deleteMessageService(wsDir, 999)
	require.Error(t, errDelete)
}

func assertForkNameHasRootPrefix(t *testing.T, name, rootName string) {
	t.Helper()
	require.NotEmpty(t, name)
	require.True(t, strings.HasPrefix(name, rootName+"-"), "name %q should start with %q", name, rootName+"-")
	parts := strings.Split(strings.TrimPrefix(name, rootName+"-"), "-")
	require.Len(t, parts, 3)
}

func TestGenerateRandomForkName(t *testing.T) {
	for range 100 {
		name := generateRandomForkName()
		assert.NotEmpty(t, name)
		assert.Greater(t, len(name), 5, "name should be longer than 5 characters")
		assert.Contains(t, name, "-", "name should contain a hyphen")
		parts := strings.Split(name, "-")
		assert.Len(t, parts, 3, "name should have 3 parts separated by hyphens")
	}
}

func TestGenerateRandomForkNameUniqueness(t *testing.T) {
	names := make(map[string]bool)
	for range 1000 {
		name := generateRandomForkName()
		names[name] = true
	}
	assert.Greater(t, len(names), 100, "should generate many unique names")
}

func TestUpdateGoalServiceInvalidatesSVGCache(t *testing.T) {
	rootDir := t.TempDir()
	server := NewServer(rootDir, newTestServerPaths(), "")
	workspacePath := filepath.Join(rootDir, "cache-ws")
	require.NoError(t, os.MkdirAll(workspacePath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workspacePath, "GOAL.md"), []byte("# Old"), 0o644))
	server.svgCache.set(workspacePath+"|coordinator", "<svg>old</svg>")
	server.svgCache.set(workspacePath+"|agent1", "<svg>old2</svg>")
	server.svgCache.set("/other/path|coordinator", "<svg>other</svg>")

	result, err := server.updateGoalService(workspacePath, "# New Goal")
	require.NoError(t, err)
	assert.True(t, result.Updated)

	_, wsOK := server.svgCache.get(workspacePath + "|coordinator")
	assert.False(t, wsOK)
	_, otherOK := server.svgCache.get("/other/path|coordinator")
	assert.True(t, otherOK)
}

func TestTogglePinServiceSuccess(t *testing.T) {
	rootDir := t.TempDir()
	server := NewServer(rootDir, newTestServerPaths(), "")
	workspacePath := filepath.Join(rootDir, "pin-ws")
	require.NoError(t, os.MkdirAll(workspacePath, 0o755))

	result, err := server.togglePinService(workspacePath)
	require.NoError(t, err)
	assert.True(t, result.Pinned)

	result2, err2 := server.togglePinService(workspacePath)
	require.NoError(t, err2)
	assert.False(t, result2.Pinned)
}

func TestDeleteWorkspaceServiceSuccess(t *testing.T) {
	rootDir := t.TempDir()
	server := NewServer(rootDir, newTestServerPaths(), "")
	workspacePath := filepath.Join(rootDir, "delete-ws")
	require.NoError(t, os.MkdirAll(workspacePath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workspacePath, "GOAL.md"), []byte("# Goal"), 0o644))
	server.mu.Lock()
	server.pinnedDirs[resolveSymlinks(workspacePath)] = true
	server.mu.Unlock()

	result, err := server.deleteWorkspaceService(workspacePath)
	require.Error(t, err)
	assert.Empty(t, result.Message)
	assert.False(t, result.Deleted)

	_, errStat := os.Stat(workspacePath)
	require.NoError(t, errStat)
}
