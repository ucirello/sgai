package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComposeStateService(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "test-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\ntitle: Compose Title\n---\n# Old Body Title\n\nBody description\n"), 0o644))

	result := server.composeStateService(wsDir)
	assert.Equal(t, "test-ws", result.Workspace)
	assert.NotNil(t, result.State)
	assert.Equal(t, "Compose Title", result.State.Title)
	assert.Equal(t, "# Old Body Title\n\nBody description", result.State.Description)
}

func TestComposeStateServiceRefreshesGeneratedTitleFromDisk(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "repo-dir")
	goalPath := filepath.Join(wsDir, "GOAL.md")
	require.NoError(t, os.WriteFile(goalPath, []byte("---\nflow: |\n  \"coordinator\"\n---\n# Generated Title\n\nBody description\n"), 0o644))

	initial := server.composeStateService(wsDir)
	assert.Equal(t, "repo-dir", initial.State.Title)

	waitForTestCondition(t, func() bool {
		data, errRead := os.ReadFile(goalPath)
		if errRead != nil {
			return false
		}
		return string(data) == "---\ntitle: Generated Title\nflow: |\n  \"coordinator\"\n---\n# Generated Title\n\nBody description\n"
	})

	updated := server.composeStateService(wsDir)
	assert.Equal(t, "Generated Title", updated.State.Title)

	preview, errPreview := server.composePreviewService(wsDir)
	require.NoError(t, errPreview)
	assert.Contains(t, preview.Content, "title: 'Generated Title'")
	assert.NotContains(t, preview.Content, "title: 'repo-dir'")

	_, errSave := server.composeSaveService(wsDir, "")
	require.NoError(t, errSave)

	saved, errRead := os.ReadFile(goalPath)
	require.NoError(t, errRead)
	assert.Contains(t, string(saved), "title: 'Generated Title'")
	assert.NotContains(t, string(saved), "title: 'repo-dir'")
}

func TestComposeDraftServiceKeepsGeneratedTitleAfterBackfill(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "repo-dir")
	goalPath := filepath.Join(wsDir, "GOAL.md")
	require.NoError(t, os.WriteFile(goalPath, []byte("---\nflow: |\n  \"go-developer\"\ncustom: keep me\n---\n# Browser Verified Title\n\nThis workspace verifies missing-title fallback and asynchronous title backfill.\n"), 0o644))

	initial := server.composeStateService(wsDir)
	assert.Equal(t, "repo-dir", initial.State.Title)

	waitForTestCondition(t, func() bool {
		data, errRead := os.ReadFile(goalPath)
		if errRead != nil {
			return false
		}
		return string(data) == "---\ntitle: Browser Verified Title\nflow: |\n  \"go-developer\"\ncustom: keep me\n---\n# Browser Verified Title\n\nThis workspace verifies missing-title fallback and asynchronous title backfill.\n"
	})

	updated := server.composeStateService(wsDir)
	assert.Equal(t, "Browser Verified Title", updated.State.Title)

	server.composeDraftService(wsDir, composerState{
		Description: "This workspace verifies missing-title fallback and asynchronous title backfill.",
		Flow:        `"go-developer"`,
	}, wizardState{
		CurrentStep: 1,
		Description: "This workspace verifies missing-title fallback and asynchronous title backfill.",
	})

	preview, errPreview := server.composePreviewService(wsDir)
	require.NoError(t, errPreview)
	assert.Contains(t, preview.Content, "title: 'Browser Verified Title'")
	assert.NotContains(t, preview.Content, "title: 'repo-dir'")
}

func TestComposeDraftServiceDoesNotBackfillTitleWhileEditing(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "repo-dir")
	goalPath := filepath.Join(wsDir, "GOAL.md")
	original := []byte("# Generated Title\n\nBody description\n")
	require.NoError(t, os.WriteFile(goalPath, original, 0o644))

	server.composeDraftService(wsDir, composerState{}, wizardState{})

	assert.Never(t, func() bool {
		data, errRead := os.ReadFile(goalPath)
		if errRead != nil {
			return false
		}
		return strings.Contains(string(data), "title:")
	}, time.Second, 20*time.Millisecond)

	data, errRead := os.ReadFile(goalPath)
	require.NoError(t, errRead)
	assert.Equal(t, string(original), string(data))
}

func TestCurrentComposerSessionKeepsGeneratedTitleWhenBackfillCompletesBeforeSessionLoad(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "repo-dir")
	goalPath := filepath.Join(wsDir, "GOAL.md")
	require.NoError(t, os.WriteFile(goalPath, []byte("---\nflow: |\n  \"coordinator\"\n---\n# Generated Title\n\nBody description\n"), 0o644))

	type composeSessionResult struct {
		state  composerState
		wizard wizardState
	}

	server.composerSessionsMu.Lock()
	locked := true
	defer func() {
		if locked {
			server.composerSessionsMu.Unlock()
		}
	}()

	resultCh := make(chan composeSessionResult, 1)
	go func() {
		state, wizard := server.currentComposerSession(wsDir)
		resultCh <- composeSessionResult{state: state, wizard: wizard}
	}()

	initial := server.loadComposerStateForInterface(wsDir)
	assert.Equal(t, "repo-dir", initial.Title)

	waitForTestCondition(t, func() bool {
		data, errRead := os.ReadFile(goalPath)
		if errRead != nil {
			return false
		}
		return strings.Contains(string(data), "title: Generated Title\n")
	})

	server.composerSessionsMu.Unlock()
	locked = false

	result := <-resultCh
	assert.Equal(t, "Generated Title", result.state.Title)
	assert.Equal(t, "Generated Title", result.wizard.Title)
}

func TestUpdateComposerSessionKeepsGeneratedTitleWhenBackfillCompletesBeforeSessionLoad(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "repo-dir")
	goalPath := filepath.Join(wsDir, "GOAL.md")
	require.NoError(t, os.WriteFile(goalPath, []byte("---\nflow: |\n  \"coordinator\"\n---\n# Generated Title\n\nBody description\n"), 0o644))

	type composeSessionResult struct {
		state  composerState
		wizard wizardState
	}

	server.composerSessionsMu.Lock()
	locked := true
	defer func() {
		if locked {
			server.composerSessionsMu.Unlock()
		}
	}()

	resultCh := make(chan composeSessionResult, 1)
	go func() {
		state, wizard := server.updateComposerSession(wsDir, composerState{
			Description: "Body description",
			Flow:        `"coordinator"`,
		}, wizardState{Description: "Body description"})
		resultCh <- composeSessionResult{state: state, wizard: wizard}
	}()

	initial := server.loadComposerStateForInterface(wsDir)
	assert.Equal(t, "repo-dir", initial.Title)

	waitForTestCondition(t, func() bool {
		data, errRead := os.ReadFile(goalPath)
		if errRead != nil {
			return false
		}
		return strings.Contains(string(data), "title: Generated Title\n")
	})

	server.composerSessionsMu.Unlock()
	locked = false

	result := <-resultCh
	assert.Equal(t, "Generated Title", result.state.Title)
	assert.Equal(t, "Generated Title", result.wizard.Title)
}

func TestComposeSaveService(t *testing.T) {
	t.Run("savesGoal", func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		wsDir := setupTestWorkspace(t, rootDir, "test-ws")
		server.composeDraftService(wsDir, composerState{
			Title:       "Saved Compose Title",
			Description: "Saved body description",
			Tasks:       "- Task 1",
		}, wizardState{})

		result, err := server.composeSaveService(wsDir, "")
		require.NoError(t, err)
		assert.True(t, result.Saved)
		assert.Equal(t, "test-ws", result.Workspace)

		goalPath := filepath.Join(wsDir, "GOAL.md")
		_, errStat := os.Stat(goalPath)
		assert.NoError(t, errStat)

		saved, errRead := os.ReadFile(goalPath)
		require.NoError(t, errRead)
		assert.Contains(t, string(saved), "title:")
		assert.Contains(t, string(saved), "Saved Compose Title")
		assert.Contains(t, string(saved), "Saved body description")
		assert.NotContains(t, string(saved), "# Saved Compose Title")
	})

	t.Run("etagMismatch", func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		wsDir := setupTestWorkspace(t, rootDir, "test-ws")
		require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\ntitle: Existing Title\n---\n# existing"), 0o644))

		_, err := server.composeSaveService(wsDir, `"wrong-etag"`)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "modified")
	})

	t.Run("etagMatch", func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		wsDir := setupTestWorkspace(t, rootDir, "test-ws")
		content := []byte("---\ntitle: Existing Title\n---\n# existing")
		require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), content, 0o644))

		etag := computeEtag(content)
		result, err := server.composeSaveService(wsDir, etag)
		require.NoError(t, err)
		assert.True(t, result.Saved)
	})
}

func TestComposePreviewServiceBackfillsTitleBeforeBuildingPreview(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "repo-dir")
	goalPath := filepath.Join(wsDir, "GOAL.md")
	require.NoError(t, os.WriteFile(goalPath, []byte("---\nflow: |\n  \"coordinator\"\n---\n# Generated Title\n\nBody description\n"), 0o644))

	result, errPreview := server.composePreviewService(wsDir)
	require.NoError(t, errPreview)
	assert.Contains(t, result.Content, "title: 'Generated Title'")
	assert.NotContains(t, result.Content, "title: 'repo-dir'")

	data, errRead := os.ReadFile(goalPath)
	require.NoError(t, errRead)
	assert.Contains(t, string(data), "title: Generated Title\n")
	assert.Equal(t, computeEtag(data), result.Etag)
}

func TestComposeSaveServiceBackfillsTitleBeforeWriting(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "repo-dir")
	goalPath := filepath.Join(wsDir, "GOAL.md")
	require.NoError(t, os.WriteFile(goalPath, []byte("---\nflow: |\n  \"coordinator\"\n---\n# Generated Title\n\nBody description\n"), 0o644))

	result, errSave := server.composeSaveService(wsDir, "")
	require.NoError(t, errSave)
	assert.True(t, result.Saved)

	data, errRead := os.ReadFile(goalPath)
	require.NoError(t, errRead)
	assert.Contains(t, string(data), "title: 'Generated Title'")
	assert.NotContains(t, string(data), "title: 'repo-dir'")
}

func TestComposeSaveServiceRejectsStaleEtagAfterTitleBackfill(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "repo-dir")
	goalPath := filepath.Join(wsDir, "GOAL.md")
	content := []byte("---\nflow: |\n  \"coordinator\"\n---\n# Generated Title\n\nBody description\n")
	require.NoError(t, os.WriteFile(goalPath, content, 0o644))

	_, errSave := server.composeSaveService(wsDir, computeEtag(content))
	require.ErrorIs(t, errSave, errComposerGoalModified)

	data, errRead := os.ReadFile(goalPath)
	require.NoError(t, errRead)
	assert.Contains(t, string(data), "title: Generated Title\n")
	assert.NotContains(t, string(data), "title: 'repo-dir'")
}

func TestComposePreviewServiceKeepsDirectoryFallbackWhenBackfillCannotDeriveHeadingTitle(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "repo-dir")
	goalPath := filepath.Join(wsDir, "GOAL.md")
	original := []byte("Build a REST API\n\n- [ ] Ship it\n")
	require.NoError(t, os.WriteFile(goalPath, original, 0o644))

	result, errPreview := server.composePreviewService(wsDir)
	require.NoError(t, errPreview)
	assert.Contains(t, result.Content, "title: 'repo-dir'")
	assert.NotContains(t, result.Content, "failed to backfill current GOAL.md title")
	assert.Equal(t, computeEtag(original), result.Etag)

	data, errRead := os.ReadFile(goalPath)
	require.NoError(t, errRead)
	assert.Equal(t, string(original), string(data))
}

func TestComposeSaveServiceAcceptsManualTitleWhenBackfillCannotDeriveHeadingTitle(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "repo-dir")
	goalPath := filepath.Join(wsDir, "GOAL.md")
	original := []byte("Build a REST API\n\n- [ ] Ship it\n")
	require.NoError(t, os.WriteFile(goalPath, original, 0o644))

	state, wizard := server.currentComposerSession(wsDir)
	assert.Equal(t, "repo-dir", state.Title)
	state.Title = "Manual Compose Title"
	wizard.Title = state.Title

	server.composeDraftService(wsDir, state, wizard)

	result, errSave := server.composeSaveService(wsDir, "")
	require.NoError(t, errSave)
	assert.True(t, result.Saved)

	data, errRead := os.ReadFile(goalPath)
	require.NoError(t, errRead)
	assert.Contains(t, string(data), "title: 'Manual Compose Title'")
	assert.Contains(t, string(data), "Build a REST API")
	assert.NotContains(t, string(data), "title: 'repo-dir'")
}

func TestComposeTemplatesService(t *testing.T) {
	server, _ := setupTestServer(t)
	result := server.composeTemplatesService()
	assert.NotEmpty(t, result.Templates)
}

func TestComposePreviewService(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "test-ws")
	server.composeDraftService(wsDir, composerState{
		Title:       "Preview Compose Title",
		Description: "Preview body description",
	}, wizardState{})

	result, err := server.composePreviewService(wsDir)
	require.NoError(t, err)
	assert.NotEmpty(t, result.Content)
	assert.Contains(t, result.Content, "title:")
	assert.Contains(t, result.Content, "Preview Compose Title")
	assert.Contains(t, result.Content, "Preview body description")
	assert.NotContains(t, result.Content, "# Preview Compose Title")
}

func TestComposeSaveServiceWrapsCurrentGoalReadError(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "save-read-error")
	goalPath := filepath.Join(wsDir, "GOAL.md")
	require.NoError(t, os.Mkdir(goalPath, 0o755))

	_, errSave := server.composeSaveService(wsDir, `"etag"`)
	require.Error(t, errSave)
	assert.ErrorContains(t, errSave, "failed to read current GOAL.md")

	var errPath *fs.PathError
	require.ErrorAs(t, errSave, &errPath)
	assert.Equal(t, goalPath, errPath.Path)
}

func TestComposePreviewServiceWrapsCurrentGoalReadError(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "preview-read-error")
	goalPath := filepath.Join(wsDir, "GOAL.md")
	require.NoError(t, os.Mkdir(goalPath, 0o755))

	_, errPreview := server.composePreviewService(wsDir)
	require.Error(t, errPreview)
	assert.ErrorContains(t, errPreview, "failed to read current GOAL.md")

	var errPath *fs.PathError
	require.ErrorAs(t, errPreview, &errPath)
	assert.Equal(t, goalPath, errPath.Path)
}

func TestComposeStateServiceWithInvalidFlow(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "flow-err-ws")
	server.composeDraftService(wsDir, composerState{
		Description: "Test",
		Flow:        `digraph G { "a" -> }`,
	}, wizardState{})
	result := server.composeStateService(wsDir)
	assert.NotEmpty(t, result.FlowError)
}

func TestComposePreviewServiceWithInvalidFlow(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "preview-flow-ws")
	server.composeDraftService(wsDir, composerState{
		Description: "Test",
		Flow:        `digraph G { "a" -> }`,
	}, wizardState{})
	result, err := server.composePreviewService(wsDir)
	require.NoError(t, err)
	assert.NotEmpty(t, result.FlowError)
}

func TestComposePreviewServiceWithValidFlow(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "preview-valid-ws")
	server.composeDraftService(wsDir, composerState{
		Description: "Test",
		Flow:        `"a" -> "b"`,
	}, wizardState{})
	result, err := server.composePreviewService(wsDir)
	require.NoError(t, err)
	assert.Empty(t, result.FlowError)
	assert.NotEmpty(t, result.Content)
}

func TestComposeStateServiceWithValidFlow(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "state-valid-ws")
	server.composeDraftService(wsDir, composerState{
		Description: "Test",
		Flow:        `"x" -> "y"`,
	}, wizardState{})
	result := server.composeStateService(wsDir)
	assert.Empty(t, result.FlowError)
}

func TestComposeDraftService(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "test-ws")

	result := server.composeDraftService(wsDir, composerState{
		Title:       "Draft Title",
		Description: "Test description",
		Tasks:       "Test tasks",
	}, wizardState{})
	assert.True(t, result.Saved)

	stateResult := server.composeStateService(wsDir)
	assert.Equal(t, "Draft Title", stateResult.State.Title)
	assert.Equal(t, "Test description", stateResult.State.Description)
}
