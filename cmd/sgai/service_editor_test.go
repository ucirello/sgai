package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenEditorServiceEditorNotAvailable(t *testing.T) {
	server := NewServer(t.TempDir(), newTestServerPaths(), "")
	server.editorAvailable = false
	_, err := server.openEditorService(t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no editor available")
}

func TestOpenEditorFileServiceEditorNotAvailable(t *testing.T) {
	server := NewServer(t.TempDir(), newTestServerPaths(), "")
	server.editorAvailable = false
	_, err := server.openEditorFileService(t.TempDir(), "GOAL.md")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no editor available")
}

func TestOpenEditorFileServiceFileNotFound(t *testing.T) {
	dir := t.TempDir()
	server := NewServer(t.TempDir(), newTestServerPaths(), "")
	server.editorAvailable = true
	server.editor = newConfigurableEditor("echo")
	_, err := server.openEditorFileService(dir, "nonexistent.md")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "file not found")
}

func TestOpenEditorGoalServiceDelegates(t *testing.T) {
	server := NewServer(t.TempDir(), newTestServerPaths(), "")
	server.editorAvailable = false
	_, err := server.openEditorGoalService(t.TempDir())
	require.Error(t, err)
}

func TestOpenEditorProjectManagementServiceDelegates(t *testing.T) {
	server := NewServer(t.TempDir(), newTestServerPaths(), "")
	server.editorAvailable = false
	_, err := server.openEditorProjectManagementService(t.TempDir())
	require.Error(t, err)
}

func TestOpenEditorFileServiceSuccess(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "GOAL.md"), []byte("# Goal"), 0o644))
	server := NewServer(t.TempDir(), newTestServerPaths(), "")
	server.editorAvailable = true
	server.editor = newConfigurableEditor("echo")
	result, err := server.openEditorFileService(dir, "GOAL.md")
	require.NoError(t, err)
	assert.True(t, result.Opened)
}
