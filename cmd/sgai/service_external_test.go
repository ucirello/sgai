package main

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadExternalDirsForTest(t *testing.T, configDir string) map[string]bool {
	t.Helper()

	server, _ := setupTestServer(t)
	server.externalConfigDir = configDir
	require.NoError(t, server.loadExternalDirs())

	server.mu.Lock()
	defer server.mu.Unlock()

	return maps.Clone(server.externalDirs)
}

func externalDirsSnapshotForTest(server *Server) map[string]bool {
	server.mu.Lock()
	defer server.mu.Unlock()

	return maps.Clone(server.externalDirs)
}

func TestLoadExternalDirs(t *testing.T) {
	t.Run("noFile", func(t *testing.T) {
		server, _ := setupTestServer(t)
		server.externalConfigDir = t.TempDir()
		err := server.loadExternalDirs()
		require.NoError(t, err)
	})

	t.Run("validFile", func(t *testing.T) {
		server, _ := setupTestServer(t)
		configDir := t.TempDir()
		server.externalConfigDir = configDir

		externalDir := t.TempDir()
		data := `["` + externalDir + `"]`
		require.NoError(t, os.WriteFile(filepath.Join(configDir, "external.json"), []byte(data), 0o644))

		err := server.loadExternalDirs()
		require.NoError(t, err)
	})

	t.Run("invalidJSON", func(t *testing.T) {
		server, _ := setupTestServer(t)
		configDir := t.TempDir()
		server.externalConfigDir = configDir

		require.NoError(t, os.WriteFile(filepath.Join(configDir, "external.json"), []byte(`{invalid}`), 0o644))

		err := server.loadExternalDirs()
		require.Error(t, err)
	})

	t.Run("keepsUnreadableAttachedWorkspace", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("skipping permission test as root")
		}

		server, _ := setupTestServer(t)
		configDir := t.TempDir()
		server.externalConfigDir = configDir

		validDir := filepath.Join(t.TempDir(), "valid-ws")
		require.NoError(t, os.MkdirAll(validDir, 0o755))

		lockedParent := filepath.Join(t.TempDir(), "locked-parent")
		lockedDir := filepath.Join(lockedParent, "locked-ws")
		require.NoError(t, os.MkdirAll(lockedDir, 0o755))

		data, errJSON := json.Marshal([]string{validDir, lockedDir})
		require.NoError(t, errJSON)
		require.NoError(t, os.WriteFile(filepath.Join(configDir, "external.json"), data, 0o644))

		require.NoError(t, os.Chmod(lockedParent, 0o000))
		defer func() {
			require.NoError(t, os.Chmod(lockedParent, 0o755))
		}()

		_, errStat := os.Stat(lockedDir)
		require.Error(t, errStat)
		require.False(t, os.IsNotExist(errStat))

		require.NoError(t, server.loadExternalDirs())

		loaded := externalDirsSnapshotForTest(server)
		assert.True(t, loaded[resolveSymlinks(validDir)])
		assert.True(t, loaded[resolveSymlinks(lockedDir)])
		assert.Contains(t, readJSONPathList(t, filepath.Join(configDir, "external.json")), resolveSymlinks(lockedDir))
	})

	t.Run("prunedStateDoesNotCommitWhenPersistFails", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("skipping permission test as root")
		}

		server, _ := setupTestServer(t)
		configDir := t.TempDir()
		server.externalConfigDir = configDir

		validDir := filepath.Join(t.TempDir(), "valid-ws")
		require.NoError(t, os.MkdirAll(validDir, 0o755))
		missingDir := filepath.Join(t.TempDir(), "missing-ws")

		data, errJSON := json.Marshal([]string{validDir, missingDir})
		require.NoError(t, errJSON)
		require.NoError(t, os.WriteFile(filepath.Join(configDir, "external.json"), data, 0o644))

		require.NoError(t, os.Chmod(configDir, 0o500))
		defer func() {
			require.NoError(t, os.Chmod(configDir, 0o700))
		}()

		errLoad := server.loadExternalDirs()
		require.Error(t, errLoad)

		assert.Empty(t, externalDirsSnapshotForTest(server))
		assert.ElementsMatch(t, []string{resolveSymlinks(validDir), resolveSymlinks(missingDir)}, readJSONPathList(t, filepath.Join(configDir, "external.json")))
	})
}

func TestSaveExternalDirs(t *testing.T) {
	server, _ := setupTestServer(t)
	configDir := t.TempDir()
	server.externalConfigDir = configDir

	server.mu.Lock()
	server.externalDirs["/some/path"] = true
	server.mu.Unlock()

	err := server.saveExternalDirs()
	require.NoError(t, err)

	data, errRead := os.ReadFile(filepath.Join(configDir, "external.json"))
	require.NoError(t, errRead)
	assert.Contains(t, string(data), "/some/path")
}

func TestAttachExternalWorkspaceService(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		setupFunc   func(*testing.T, string, string)
		wantErr     bool
		errContains string
		validate    func(*testing.T, string, attachExternalResult)
	}{
		{
			name: "attachValidExternalDirectory",
			path: "",
			setupFunc: func(t *testing.T, _, externalPath string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(externalPath, 0o755))
			},
			wantErr:     false,
			errContains: "",
			validate: func(t *testing.T, externalPath string, result attachExternalResult) {
				t.Helper()
				assert.NotEmpty(t, result.Name)
				assert.Equal(t, externalPath, result.Dir)
			},
		},
		{
			name: "attachWithRelativePath",
			path: "relative/path",
			setupFunc: func(_ *testing.T, _, _ string) {
			},
			wantErr:     true,
			errContains: "path must be absolute",
			validate:    nil,
		},
		{
			name: "attachNonExistentDirectory",
			path: "/non/existent/directory",
			setupFunc: func(_ *testing.T, _, _ string) {
			},
			wantErr:     true,
			errContains: "directory does not exist",
			validate:    nil,
		},
		{
			name: "attachFileNotDirectory",
			path: "",
			setupFunc: func(t *testing.T, _, externalPath string) {
				t.Helper()
				require.NoError(t, os.WriteFile(externalPath, []byte("test"), 0o644))
			},
			wantErr:     true,
			errContains: "path is not a directory",
			validate:    nil,
		},
		{
			name: "attachDirectoryUnderRoot",
			path: "",
			setupFunc: func(t *testing.T, _, externalPath string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(externalPath, 0o755))
			},
			wantErr:     false,
			errContains: "",
			validate: func(t *testing.T, externalPath string, result attachExternalResult) {
				t.Helper()
				assert.Equal(t, externalPath, result.Dir)
			},
		},
		{
			name: "attachPersistsExternalDirectory",
			path: "",
			setupFunc: func(t *testing.T, _, externalPath string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(externalPath, 0o755))
			},
			wantErr:     false,
			errContains: "",
			validate:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootDir := t.TempDir()
			server := NewServer(rootDir, newTestServerPaths(), "")
			server.externalConfigDir = t.TempDir()

			var externalPath string
			switch {
			case tt.name == "attachDirectoryUnderRoot":
				externalPath = filepath.Join(rootDir, "subdir")
			case tt.path != "":
				externalPath = tt.path
			default:
				externalPath = filepath.Join(t.TempDir(), "external-workspace")
			}

			tt.setupFunc(t, rootDir, externalPath)

			result, err := server.attachExternalWorkspaceService(externalPath)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, externalPath, result)
			}
			if tt.name == "attachPersistsExternalDirectory" {
				loaded := loadExternalDirsForTest(t, server.externalConfigDir)
				assert.True(t, loaded[resolveSymlinks(externalPath)])
			}
		})
	}
}

func TestAttachExternalWorkspaceServiceAllowsDuplicateBasenames(t *testing.T) {
	rootDir := t.TempDir()
	server := NewServer(rootDir, newTestServerPaths(), "")
	server.externalConfigDir = t.TempDir()

	baseDir := t.TempDir()
	firstDir := filepath.Join(baseDir, "first", "shared-ws")
	secondDir := filepath.Join(baseDir, "second", "shared-ws")
	require.NoError(t, os.MkdirAll(firstDir, 0o755))
	require.NoError(t, os.MkdirAll(secondDir, 0o755))

	firstResult, errAttachFirst := server.attachExternalWorkspaceService(firstDir)
	require.NoError(t, errAttachFirst)
	secondResult, errAttachSecond := server.attachExternalWorkspaceService(secondDir)
	require.NoError(t, errAttachSecond)

	assert.Equal(t, "shared-ws", firstResult.Name)
	assert.Equal(t, "shared-ws", secondResult.Name)
	loaded := loadExternalDirsForTest(t, server.externalConfigDir)
	assert.True(t, loaded[resolveSymlinks(firstDir)])
	assert.True(t, loaded[resolveSymlinks(secondDir)])
}

func TestDetachExternalWorkspaceService(t *testing.T) {
	tests := []struct {
		name        string
		setupFunc   func(*testing.T, string, string, *Server)
		wantErr     bool
		errContains string
		validate    func(*testing.T, detachExternalResult)
	}{
		{
			name: "detachAttachedWorkspace",
			setupFunc: func(t *testing.T, _ string, externalPath string, server *Server) {
				t.Helper()
				require.NoError(t, os.MkdirAll(externalPath, 0o755))
				_, err := server.attachExternalWorkspaceService(externalPath)
				require.NoError(t, err)
			},
			wantErr:     false,
			errContains: "",
			validate: func(t *testing.T, result detachExternalResult) {
				t.Helper()
				assert.True(t, result.Detached)
				assert.Equal(t, "external workspace detached", result.Message)
			},
		},
		{
			name: "detachRemovesPersistedExternalDirectory",
			setupFunc: func(t *testing.T, _ string, externalPath string, server *Server) {
				t.Helper()
				require.NoError(t, os.MkdirAll(externalPath, 0o755))
				_, err := server.attachExternalWorkspaceService(externalPath)
				require.NoError(t, err)
			},
			wantErr:     false,
			errContains: "",
			validate: func(t *testing.T, result detachExternalResult) {
				t.Helper()
				assert.True(t, result.Detached)
			},
		},
		{
			name: "detachNonAttachedWorkspace",
			setupFunc: func(t *testing.T, _ string, externalPath string, _ *Server) {
				t.Helper()
				require.NoError(t, os.MkdirAll(externalPath, 0o755))
			},
			wantErr:     true,
			errContains: "directory is not attached as an external workspace",
			validate:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootDir := t.TempDir()
			server := NewServer(rootDir, newTestServerPaths(), "")
			server.externalConfigDir = t.TempDir()

			externalPath := filepath.Join(t.TempDir(), "external-workspace")

			tt.setupFunc(t, rootDir, externalPath, server)

			result, err := server.detachExternalWorkspaceService(externalPath)

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
			if tt.name == "detachRemovesPersistedExternalDirectory" {
				loaded := loadExternalDirsForTest(t, server.externalConfigDir)
				assert.False(t, loaded[resolveSymlinks(externalPath)])
			}
		})
	}
}

func TestAttachExternalWorkspaceServiceRestoresStateOnSaveFailure(t *testing.T) {
	rootDir := t.TempDir()
	server := NewServer(rootDir, newTestServerPaths(), "")

	blockingPath := filepath.Join(t.TempDir(), "external-config-blocker")
	require.NoError(t, os.WriteFile(blockingPath, []byte("block"), 0o644))
	server.externalConfigDir = blockingPath

	externalPath := filepath.Join(t.TempDir(), "external-workspace")
	require.NoError(t, os.MkdirAll(externalPath, 0o755))

	want := externalDirsSnapshotForTest(server)

	_, errAttach := server.attachExternalWorkspaceService(externalPath)
	require.Error(t, errAttach)
	assert.Contains(t, errAttach.Error(), "saving external dirs")
	assert.Equal(t, want, externalDirsSnapshotForTest(server))
	assert.False(t, server.isExternalWorkspace(externalPath))
}

func TestDetachExternalWorkspaceServiceRestoresStateOnSaveFailure(t *testing.T) {
	rootDir := t.TempDir()
	server := NewServer(rootDir, newTestServerPaths(), "")
	server.externalConfigDir = t.TempDir()

	externalPath := filepath.Join(t.TempDir(), "external-workspace")
	require.NoError(t, os.MkdirAll(externalPath, 0o755))
	_, errAttach := server.attachExternalWorkspaceService(externalPath)
	require.NoError(t, errAttach)

	blockingPath := filepath.Join(t.TempDir(), "external-config-blocker")
	require.NoError(t, os.WriteFile(blockingPath, []byte("block"), 0o644))
	server.externalConfigDir = blockingPath

	want := externalDirsSnapshotForTest(server)

	_, errDetach := server.detachExternalWorkspaceService(externalPath)
	require.Error(t, errDetach)
	assert.Contains(t, errDetach.Error(), "saving workspace lists")
	assert.Equal(t, want, externalDirsSnapshotForTest(server))
	assert.True(t, server.isExternalWorkspace(externalPath))
}

func TestIsExternalWorkspace(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func(*testing.T, string, string, *Server)
		expected  bool
	}{
		{
			name: "isExternalTrue",
			setupFunc: func(t *testing.T, _ string, externalPath string, server *Server) {
				t.Helper()
				require.NoError(t, os.MkdirAll(externalPath, 0o755))
				_, err := server.attachExternalWorkspaceService(externalPath)
				require.NoError(t, err)
			},
			expected: true,
		},
		{
			name: "isExternalFalse",
			setupFunc: func(t *testing.T, _ string, externalPath string, _ *Server) {
				t.Helper()
				require.NoError(t, os.MkdirAll(externalPath, 0o755))
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootDir := t.TempDir()
			server := NewServer(rootDir, newTestServerPaths(), "")

			externalPath := filepath.Join(os.TempDir(), "external-workspace")
			t.Cleanup(func() {
				_ = os.RemoveAll(externalPath)
			})

			tt.setupFunc(t, rootDir, externalPath, server)

			result := server.isExternalWorkspace(externalPath)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBrowseDirectoriesService(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		setupFunc   func(*testing.T, string)
		wantErr     bool
		errContains string
		validate    func(*testing.T, []directoryEntry)
	}{
		{
			name: "browseValidDirectory",
			path: "",
			setupFunc: func(t *testing.T, path string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(path, "dir1"), 0o755))
				require.NoError(t, os.MkdirAll(filepath.Join(path, "dir2"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(path, "file1.txt"), []byte("test"), 0o644))
			},
			wantErr:     false,
			errContains: "",
			validate: func(t *testing.T, entries []directoryEntry) {
				t.Helper()
				assert.GreaterOrEqual(t, len(entries), 2)
				for _, entry := range entries {
					assert.True(t, entry.IsDir)
					assert.NotEmpty(t, entry.Name)
					assert.NotEmpty(t, entry.Path)
				}
			},
		},
		{
			name:        "browseRelativeDirectory",
			path:        ".",
			setupFunc:   func(_ *testing.T, _ string) {},
			wantErr:     true,
			errContains: "path must be absolute",
			validate:    nil,
		},
		{
			name:        "browseNonExistentDirectory",
			path:        "/non/existent/directory",
			setupFunc:   func(_ *testing.T, _ string) {},
			wantErr:     true,
			errContains: "directory does not exist",
			validate:    nil,
		},
		{
			name:        "browseEmptyPath",
			path:        "",
			setupFunc:   func(_ *testing.T, _ string) {},
			wantErr:     false,
			errContains: "",
			validate: func(t *testing.T, entries []directoryEntry) {
				t.Helper()
				assert.NotNil(t, entries)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDir := t.TempDir()
			if tt.path != "" && tt.path != "/non/existent/directory" {
				testDir = tt.path
			}

			tt.setupFunc(t, testDir)

			result, err := browseDirectoriesService(tt.path)

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

func TestBrowseDirectoriesServicePermissionDenied(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping permission test as root")
	}
	dir := t.TempDir()
	restrictedDir := filepath.Join(dir, "restricted")
	require.NoError(t, os.MkdirAll(restrictedDir, 0o755))
	require.NoError(t, os.Chmod(restrictedDir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(restrictedDir, 0o755) })

	_, err := browseDirectoriesService(restrictedDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading directory")
}

func TestBrowseDirectoriesServiceHiddenDirsExcluded(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".hidden"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "visible"), 0o755))

	entries, err := browseDirectoriesService(dir)
	require.NoError(t, err)
	for _, entry := range entries {
		assert.False(t, strings.HasPrefix(entry.Name, "."))
	}
}

func TestClassifyWorkspace(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func(*testing.T, string)
		expected  workspaceKind
	}{
		{
			name: "classifyStandaloneWorkspace",
			setupFunc: func(t *testing.T, workspacePath string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".sgai"), 0o755))
			},
			expected: workspaceStandalone,
		},
		{
			name: "classifyRootWorkspace",
			setupFunc: func(t *testing.T, workspacePath string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".jj", "repo"), 0o755))
				require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".sgai"), 0o755))
			},
			expected: workspaceRoot,
		},
		{
			name: "classifyForkWorkspace",
			setupFunc: func(t *testing.T, workspacePath string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".jj"), 0o755))
				repoFile := filepath.Join(workspacePath, ".jj", "repo")
				require.NoError(t, os.WriteFile(repoFile, []byte("/path/to/parent"), 0o644))
				require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".sgai"), 0o755))
			},
			expected: workspaceFork,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootDir := t.TempDir()
			server := NewServer(rootDir, newTestServerPaths(), "")

			workspacePath := filepath.Join(rootDir, "test-workspace")
			require.NoError(t, os.MkdirAll(workspacePath, 0o755))
			tt.setupFunc(t, workspacePath)

			result := server.classifyWorkspaceCached(workspacePath)
			assert.Equal(t, tt.expected, result)
		})
	}
}
