package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoalTitleStateFromContent(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		dirName      string
		wantTitle    string
		wantComputed string
		wantRepair   bool
	}{
		{
			name:         "canonicalTitleFromFrontmatter",
			content:      "---\ntitle: Canonical Title\nflow: test\n---\n# Body Heading",
			dirName:      "workspace-name",
			wantTitle:    "Canonical Title",
			wantComputed: "",
			wantRepair:   false,
		},
		{
			name:         "noFrontmatterFallsBackToDirectory",
			content:      "# Body Heading",
			dirName:      "workspace-name",
			wantTitle:    "",
			wantComputed: "workspace-name",
			wantRepair:   false,
		},
		{
			name:         "missingTitleTriggersRepair",
			content:      "---\nflow: test\n---\n# Improve Repository Titles",
			dirName:      "workspace-name",
			wantTitle:    "",
			wantComputed: "workspace-name",
			wantRepair:   true,
		},
		{
			name:         "quotedDelimiterSubstringInTitleRemainsCanonical",
			content:      "---\ntitle: \"Canonical --- Title\"\nflow: test\n---\n# Body Heading",
			dirName:      "workspace-name",
			wantTitle:    "Canonical --- Title",
			wantComputed: "",
			wantRepair:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := goalTitleStateFromContent([]byte(tt.content), tt.dirName)
			assert.Equal(t, tt.wantTitle, result.Title)
			assert.Equal(t, tt.wantComputed, result.ComputedTitle)
			assert.Equal(t, tt.wantRepair, result.NeedsRepair)
		})
	}
}

func TestComposeGoalTitleFromContent(t *testing.T) {
	content := []byte("---\nflow: test\n---\n# Improve Repository Titles Across the Board\n\nDetails")
	assert.Equal(t, "Improve Repository Titles Across the Board", composeGoalTitleFromContent(content, "fallback"))
}

func TestComposeGoalTitleFromTextFallsBack(t *testing.T) {
	assert.Equal(t, "fallback", composeGoalTitleFromText("\n\n", "fallback"))
}

func TestContentWithInsertedGoalTitle(t *testing.T) {
	content := []byte("---\nflow: test\n---\n# Body")
	updated, errUpdate := contentWithInsertedGoalTitle(content, "Repaired Title")
	require.NoError(t, errUpdate)
	assert.Contains(t, string(updated), "title: Repaired Title")
	assert.Contains(t, string(updated), "flow: test")
	assert.Contains(t, string(updated), "# Body")
}

func TestContentWithInsertedGoalTitlePreservesCRLFFrontmatter(t *testing.T) {
	content := []byte("---\r\nflow: test\r\n---\r\n# Body")
	updated, errUpdate := contentWithInsertedGoalTitle(content, "Repaired Title")
	require.NoError(t, errUpdate)
	assert.Equal(t, "---\r\ntitle: Repaired Title\r\nflow: test\r\n---\r\n# Body", string(updated))
}

func TestContentWithInsertedGoalTitleReplacesExistingBlankTitle(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "emptyScalar",
			content: "---\ntitle:\nflow: test\n---\n# Body",
		},
		{
			name:    "quotedEmptyString",
			content: "---\ntitle: \"\"\nflow: test\n---\n# Body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updated, errUpdate := contentWithInsertedGoalTitle([]byte(tt.content), "Repaired Title")
			require.NoError(t, errUpdate)
			assert.Equal(t, "---\ntitle: Repaired Title\nflow: test\n---\n# Body", string(updated))
			assert.Equal(t, 1, strings.Count(string(updated), "title:"))
		})
	}
}

func TestRepairGoalTitleSanitizesSynthesizedTitle(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "repair-ws")
	goalPath := filepath.Join(wsDir, "GOAL.md")
	require.NoError(t, os.WriteFile(goalPath, []byte("---\nflow: test\n---\n# Goal"), 0o644))

	server.goalTitleComposer = func(_ string, _ []byte) (string, error) {
		return "  Repaired\n\n  Title\tHere  ", nil
	}

	require.NoError(t, server.repairGoalTitle(wsDir))

	state := goalTitleStateFromPath(wsDir, "repair-ws")
	assert.Equal(t, "Repaired Title Here", state.Title)

	data, errRead := os.ReadFile(goalPath)
	require.NoError(t, errRead)
	assert.Contains(t, string(data), "title: Repaired Title Here")
}

func TestRepairGoalTitleIgnoresWrappedNotExistOnInitialRead(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "repair-ws")
	goalPath := filepath.Join(wsDir, "GOAL.md")

	originalReadFile := server.goalTitleReadFile
	t.Cleanup(func() {
		server.goalTitleReadFile = originalReadFile
	})

	server.goalTitleReadFile = func(path string) ([]byte, error) {
		if path != goalPath {
			return originalReadFile(path)
		}
		return nil, fmt.Errorf("wrapped read: %w", &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist})
	}

	require.NoError(t, server.repairGoalTitle(wsDir))

	_, errStat := os.Stat(goalPath)
	assert.True(t, os.IsNotExist(errStat))
}

func TestRepairGoalTitlePreservesFreshlyAddedTitle(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "repair-ws")
	goalPath := filepath.Join(wsDir, "GOAL.md")
	original := []byte("---\nflow: test\n---\n# Original Goal")
	require.NoError(t, os.WriteFile(goalPath, original, 0o644))
	updated := []byte("---\ntitle: User Edited Title\nflow: test\n---\n# Updated Goal\n\nUser edit")

	originalReadFile := server.goalTitleReadFile
	t.Cleanup(func() {
		server.goalTitleReadFile = originalReadFile
	})

	var readCount int
	server.goalTitleReadFile = func(path string) ([]byte, error) {
		if path != goalPath {
			return os.ReadFile(path)
		}
		readCount++
		if readCount == 1 {
			return original, nil
		}
		require.NoError(t, os.WriteFile(goalPath, updated, 0o644))
		return updated, nil
	}

	composerCalled := false

	server.goalTitleComposer = func(_ string, _ []byte) (string, error) {
		composerCalled = true
		return "Composed Title", nil
	}

	require.NoError(t, server.repairGoalTitle(wsDir))
	assert.False(t, composerCalled)

	state := goalTitleStateFromPath(wsDir, "repair-ws")
	assert.Equal(t, "User Edited Title", state.Title)

	data, errRead := os.ReadFile(goalPath)
	require.NoError(t, errRead)
	assert.Contains(t, string(data), "# Updated Goal")
	assert.Contains(t, string(data), "User edit")
	assert.NotContains(t, string(data), "Composed Title")
}

func TestRepairGoalTitleRecomputesTitleFromLatestGoalContent(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "repair-ws")
	goalPath := filepath.Join(wsDir, "GOAL.md")
	original := []byte("---\nflow: test\n---\n# First Goal\n\nFirst body")
	latest := []byte("---\nflow: test\n---\n# Second Goal\n\nSecond body")
	require.NoError(t, os.WriteFile(goalPath, original, 0o644))

	originalReadFile := server.goalTitleReadFile
	t.Cleanup(func() {
		server.goalTitleReadFile = originalReadFile
	})

	var readCount int
	server.goalTitleReadFile = func(path string) ([]byte, error) {
		if path != goalPath {
			return os.ReadFile(path)
		}
		readCount++
		if readCount == 1 {
			return original, nil
		}
		return latest, nil
	}

	server.goalTitleComposer = func(_ string, goalContent []byte) (string, error) {
		return composeGoalTitleFromContent(goalContent, filepath.Base(wsDir)), nil
	}

	require.NoError(t, server.repairGoalTitle(wsDir))

	data, errRead := os.ReadFile(goalPath)
	require.NoError(t, errRead)
	assert.Contains(t, string(data), "title: Second Goal")
	assert.Contains(t, string(data), "Second body")
	assert.NotContains(t, string(data), "title: First Goal")
}

func TestRepairGoalTitleIgnoresWrappedNotExistOnReRead(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "repair-ws")
	goalPath := filepath.Join(wsDir, "GOAL.md")
	original := []byte("---\nflow: test\n---\n# Goal")
	require.NoError(t, os.WriteFile(goalPath, original, 0o644))

	originalReadFile := server.goalTitleReadFile
	t.Cleanup(func() {
		server.goalTitleReadFile = originalReadFile
	})

	var readCount int
	composerCalled := false
	server.goalTitleReadFile = func(path string) ([]byte, error) {
		if path != goalPath {
			return originalReadFile(path)
		}
		readCount++
		if readCount == 1 {
			return original, nil
		}
		require.NoError(t, os.Remove(goalPath))
		return nil, fmt.Errorf("wrapped re-read: %w", &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist})
	}
	server.goalTitleComposer = func(_ string, _ []byte) (string, error) {
		composerCalled = true
		return "Repaired Goal Title", nil
	}

	require.NoError(t, server.repairGoalTitle(wsDir))
	assert.False(t, composerCalled)

	_, errStat := os.Stat(goalPath)
	assert.True(t, os.IsNotExist(errStat))
}

func TestRepairGoalTitleIgnoresWrappedNotExistOnWriteAfterConcurrentDeletion(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "repair-ws")
	goalPath := filepath.Join(wsDir, "GOAL.md")
	require.NoError(t, os.WriteFile(goalPath, []byte("---\nflow: test\n---\n# Goal"), 0o644))

	server.goalTitleComposer = func(_ string, _ []byte) (string, error) {
		require.NoError(t, os.Remove(goalPath))
		return "Repaired Goal Title", nil
	}

	require.NoError(t, server.repairGoalTitle(wsDir))
	_, errStat := os.Stat(goalPath)
	assert.True(t, os.IsNotExist(errStat))
}

func TestEnqueueGoalTitleRepairCollapsesAliasPathsToOneSlot(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "repair-ws")
	goalPath := filepath.Join(wsDir, "GOAL.md")
	require.NoError(t, os.WriteFile(goalPath, []byte("---\nflow: test\n---\n# Goal"), 0o644))

	aliasDir := filepath.Join(rootDir, "repair-ws-alias")
	require.NoError(t, os.Symlink(wsDir, aliasDir))

	startedCh := make(chan struct{})
	releaseCh := make(chan struct{})
	server.goalTitleComposer = func(_ string, _ []byte) (string, error) {
		close(startedCh)
		<-releaseCh
		return "Alias Repair Title", nil
	}

	server.enqueueGoalTitleRepair(wsDir)
	<-startedCh

	server.enqueueGoalTitleRepair(aliasDir)

	server.goalTitleRepairMu.Lock()
	queueLen := len(server.goalTitleRepairQueue)
	queuedLen := len(server.goalTitleRepairQueued)
	server.goalTitleRepairMu.Unlock()

	close(releaseCh)

	require.Eventually(t, func() bool {
		data, errRead := os.ReadFile(goalPath)
		return errRead == nil && strings.Contains(string(data), "title: Alias Repair Title")
	}, time.Second, 10*time.Millisecond)

	assert.Equal(t, 0, queueLen)
	assert.Equal(t, 1, queuedLen)
}
