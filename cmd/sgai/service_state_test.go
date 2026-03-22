package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetWorkspaceStateService(t *testing.T) {
	tests := []struct {
		name          string
		workspaceName string
		setupFunc     func(*testing.T, string)
		wantErr       bool
		errContains   string
		validate      func(*testing.T, workspaceStateResult)
	}{
		{
			name:          "getExistingWorkspaceState",
			workspaceName: "test-workspace",
			setupFunc: func(t *testing.T, rootDir string) {
				workspacePath := filepath.Join(rootDir, "test-workspace")
				require.NoError(t, os.MkdirAll(workspacePath, 0755))
				require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".sgai"), 0755))
			},
			wantErr: false,
			validate: func(t *testing.T, result workspaceStateResult) {
				assert.True(t, result.Found)
				assert.Equal(t, "test-workspace", result.Workspace.Name)
			},
		},
		{
			name:          "getNonExistentWorkspaceState",
			workspaceName: "non-existent-workspace",
			setupFunc:     func(_ *testing.T, _ string) {},
			wantErr:       false,
			validate: func(t *testing.T, result workspaceStateResult) {
				assert.False(t, result.Found)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootDir := t.TempDir()
			server := NewServer(rootDir)

			tt.setupFunc(t, rootDir)
			workspacePath := filepath.Join(rootDir, "test-workspace")
			if _, errStat := os.Stat(workspacePath); errStat == nil {
				server.externalDirs[resolveSymlinks(workspacePath)] = true
			}

			result, err := server.getWorkspaceStateService(tt.workspaceName)

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

func TestGetWorkflowSVGService(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func(*testing.T, string)
		validate  func(*testing.T, string)
	}{
		{
			name: "getSVGForWorkspaceWithGoal",
			setupFunc: func(t *testing.T, workspacePath string) {
				require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".sgai"), 0755))
				goalContent := "---\nflow: |\n  \"agent1\" -> \"agent2\"\n---\n# Test Goal"
				goalPath := filepath.Join(workspacePath, "GOAL.md")
				require.NoError(t, os.WriteFile(goalPath, []byte(goalContent), 0644))
			},
			validate: func(t *testing.T, svg string) {
				assert.NotEmpty(t, svg)
				assert.Contains(t, svg, "svg")
			},
		},
		{
			name: "getSVGForWorkspaceWithoutGoal",
			setupFunc: func(t *testing.T, workspacePath string) {
				require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".sgai"), 0755))
			},
			validate: func(t *testing.T, svg string) {
				assert.Empty(t, svg)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootDir := t.TempDir()
			server := NewServer(rootDir)

			workspacePath := filepath.Join(rootDir, "test-workspace")
			require.NoError(t, os.MkdirAll(workspacePath, 0755))
			tt.setupFunc(t, workspacePath)

			svg := server.getWorkflowSVGService(workspacePath)

			if tt.validate != nil {
				tt.validate(t, svg)
			}
		})
	}
}

func TestGetWorkspaceStateServiceWithMultipleWorkspaces(t *testing.T) {
	rootDir := t.TempDir()
	server := NewServer(rootDir)

	rootPath := filepath.Join(rootDir, "root-workspace")
	require.NoError(t, os.MkdirAll(rootPath, 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(rootPath, ".sgai"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(rootPath, ".jj", "repo"), 0755))
	server.externalDirs[resolveSymlinks(rootPath)] = true

	forkPath := filepath.Join(rootDir, "fork-workspace")
	require.NoError(t, os.MkdirAll(forkPath, 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(forkPath, ".sgai"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(forkPath, ".jj"), 0755))
	repoFile := filepath.Join(forkPath, ".jj", "repo")
	require.NoError(t, os.WriteFile(repoFile, []byte(filepath.Join(rootPath, ".jj", "repo")), 0644))
	server.externalDirs[resolveSymlinks(forkPath)] = true

	standalonePath := filepath.Join(rootDir, "standalone-workspace")
	require.NoError(t, os.MkdirAll(standalonePath, 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(standalonePath, ".sgai"), 0755))
	server.externalDirs[resolveSymlinks(standalonePath)] = true

	result, err := server.getWorkspaceStateService("root-workspace")
	require.NoError(t, err)
	assert.True(t, result.Found)
	assert.Equal(t, "root-workspace", result.Workspace.Name)

	result, err = server.getWorkspaceStateService("fork-workspace")
	require.NoError(t, err)
	assert.True(t, result.Found)
	assert.Equal(t, "fork-workspace", result.Workspace.Name)

	result, err = server.getWorkspaceStateService("standalone-workspace")
	require.NoError(t, err)
	assert.True(t, result.Found)
	assert.Equal(t, "standalone-workspace", result.Workspace.Name)

	result, err = server.getWorkspaceStateService("non-existent-workspace")
	require.NoError(t, err)
	assert.False(t, result.Found)
}

func TestGetWorkspaceStateServiceUsesGroupedRootMode(t *testing.T) {
	rootDir := t.TempDir()
	server := NewServer(rootDir)

	attachedRootDir := filepath.Join(rootDir, "attached-root")
	attachedForkDir := filepath.Join(rootDir, "attached-fork")
	createForkFixture(t, attachedRootDir, attachedForkDir)
	attachWorkspaceFixture(t, server, attachedRootDir, workspaceRoot)

	result, errState := server.getWorkspaceStateService(filepath.Base(attachedRootDir))
	require.NoError(t, errState)
	assert.True(t, result.Found)
	assert.False(t, result.Workspace.IsRoot)
	assert.Empty(t, result.Workspace.Forks)

	attachWorkspaceFixture(t, server, attachedForkDir, workspaceFork)
	server.invalidateWorkspaceScanCache()

	result, errState = server.getWorkspaceStateService(filepath.Base(attachedRootDir))
	require.NoError(t, errState)
	assert.True(t, result.Found)
	assert.True(t, result.Workspace.IsRoot)
	require.Len(t, result.Workspace.Forks, 1)
	assert.Equal(t, filepath.Base(attachedForkDir), result.Workspace.Forks[0].Name)
	assert.Equal(t, resolveSymlinks(attachedForkDir), result.Workspace.Forks[0].Dir)

	server.mu.Lock()
	delete(server.externalDirs, resolveSymlinks(attachedForkDir))
	server.mu.Unlock()
	server.invalidateWorkspaceScanCache()

	result, errState = server.getWorkspaceStateService(filepath.Base(attachedRootDir))
	require.NoError(t, errState)
	assert.True(t, result.Found)
	assert.False(t, result.Workspace.IsRoot)
	assert.Empty(t, result.Workspace.Forks)
}

func TestGetWorkflowSVGServiceWithDifferentGoals(t *testing.T) {
	tests := []struct {
		name        string
		goalContent string
		validate    func(*testing.T, string)
	}{
		{
			name:        "goalWithSimpleFlow",
			goalContent: "---\nflow: |\n  \"agent1\" -> \"agent2\"\n---\n# Test Goal",
			validate: func(t *testing.T, svg string) {
				assert.NotEmpty(t, svg)
				assert.Contains(t, svg, "svg")
				assert.Contains(t, svg, "agent1")
				assert.Contains(t, svg, "agent2")
			},
		},
		{
			name:        "goalWithComplexFlow",
			goalContent: "---\nflow: |\n  \"agent1\" -> \"agent2\"\n  \"agent2\" -> \"agent3\"\n  \"agent1\" -> \"agent3\"\n---\n# Complex Goal",
			validate: func(t *testing.T, svg string) {
				assert.NotEmpty(t, svg)
				assert.Contains(t, svg, "svg")
				assert.Contains(t, svg, "agent1")
				assert.Contains(t, svg, "agent2")
				assert.Contains(t, svg, "agent3")
			},
		},
		{
			name:        "goalWithNoFlow",
			goalContent: "# Test Goal\n\nNo flow defined",
			validate: func(t *testing.T, svg string) {
				assert.NotEmpty(t, svg)
				assert.Contains(t, svg, "svg")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootDir := t.TempDir()
			server := NewServer(rootDir)

			workspacePath := filepath.Join(rootDir, "test-workspace")
			require.NoError(t, os.MkdirAll(workspacePath, 0755))
			require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".sgai"), 0755))

			goalPath := filepath.Join(workspacePath, "GOAL.md")
			require.NoError(t, os.WriteFile(goalPath, []byte(tt.goalContent), 0644))

			svg := server.getWorkflowSVGService(workspacePath)

			if tt.validate != nil {
				tt.validate(t, svg)
			}
		})
	}
}
