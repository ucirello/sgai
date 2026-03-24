package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeGeneratedRepositoryTitle(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "collapsesWhitespaceIntoSingleLine",
			input: "  Build\n\ta\tREST API  ",
			want:  "Build a REST API",
		},
		{
			name:    "rejectsBlankTitle",
			input:   " \n\t ",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, errNormalize := normalizeGeneratedRepositoryTitle(tt.input)
			if tt.wantErr {
				require.Error(t, errNormalize)
				return
			}

			require.NoError(t, errNormalize)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPatchGoalTitlePreservesExistingFrontmatterAndBody(t *testing.T) {
	original := []byte("---\nflow: |\n  \"go-developer\" -> \"go-reviewer\"\n# keep this comment\n---\n# Repository Title\n\nBody text.\n")

	updated, errPatch := patchGoalTitle(original, "Repository Title")
	require.NoError(t, errPatch)

	assert.Equal(t, "---\ntitle: Repository Title\nflow: |\n  \"go-developer\" -> \"go-reviewer\"\n# keep this comment\n---\n# Repository Title\n\nBody text.\n", string(updated))
}

func TestPatchGoalTitleReplacesExistingTitleOnly(t *testing.T) {
	original := []byte("---\ntitle: Old Title\nflow: |\n  \"go-developer\"\n# keep this comment\n---\n# Goal\n")

	updated, errPatch := patchGoalTitle(original, "New Title")
	require.NoError(t, errPatch)

	assert.Equal(t, "---\ntitle: New Title\nflow: |\n  \"go-developer\"\n# keep this comment\n---\n# Goal\n", string(updated))
}

func TestPatchGoalTitleHandlesEmptyFrontmatter(t *testing.T) {
	tests := []struct {
		name     string
		original string
		want     string
	}{
		{
			name:     "lf",
			original: "---\n---\n# Goal\n",
			want:     "---\ntitle: New Title\n---\n# Goal\n",
		},
		{
			name:     "crlf",
			original: "---\r\n---\r\n# Goal\r\n",
			want:     "---\r\ntitle: New Title\r\n---\r\n# Goal\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updated, errPatch := patchGoalTitle([]byte(tt.original), "New Title")
			require.NoError(t, errPatch)
			assert.Equal(t, tt.want, string(updated))
		})
	}
}

func TestWriteGoalTitleUsesLatestOnDiskGeneratedTitle(t *testing.T) {
	dir := t.TempDir()
	goalPath := filepath.Join(dir, "GOAL.md")
	require.NoError(t, os.WriteFile(goalPath, []byte("---\n---\n# Stale Goal\n"), 0o644))
	require.NoError(t, os.WriteFile(goalPath, []byte("---\nflow: |\n  \"coordinator\"\n# keep this comment\n---\n# Latest Goal\n"), 0o644))

	title, errWrite := writeGoalTitle(goalPath, "repo-dir")
	require.NoError(t, errWrite)
	assert.Equal(t, "Latest Goal", title)

	data, errRead := os.ReadFile(goalPath)
	require.NoError(t, errRead)
	assert.Equal(t, "---\ntitle: Latest Goal\nflow: |\n  \"coordinator\"\n# keep this comment\n---\n# Latest Goal\n", string(data))
}

func TestRepositoryTitleCandidateFromLineTrimsOnlyATXClosingHashes(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			name: "trimsValidClosingHashes",
			line: "# Release Plan ###",
			want: "Release Plan",
		},
		{
			name: "preservesLiteralHashInsideTitle",
			line: "# C# Guide ###",
			want: "C# Guide",
		},
		{
			name: "preservesLiteralTrailingHashWithoutSeparator",
			line: "# Release Plan#",
			want: "Release Plan#",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, repositoryTitleCandidateFromLine(tt.line))
		})
	}
}

func TestRepositoryTitleCandidateFromLineHonorsATXIndentationBoundary(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			name: "noLeadingIndentation",
			line: "# Release Plan",
			want: "Release Plan",
		},
		{
			name: "oneLeadingSpace",
			line: " # Release Plan",
			want: "Release Plan",
		},
		{
			name: "twoLeadingSpaces",
			line: "  # Release Plan",
			want: "Release Plan",
		},
		{
			name: "threeLeadingSpaces",
			line: "   # Release Plan",
			want: "Release Plan",
		},
		{
			name: "fourLeadingSpaces",
			line: "    # Release Plan",
			want: "",
		},
		{
			name: "leadingTab",
			line: "\t# Release Plan",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, repositoryTitleCandidateFromLine(tt.line))
		})
	}
}

func TestWriteGoalTitleTrimsATXClosingHashesFromGeneratedHeading(t *testing.T) {
	dir := t.TempDir()
	goalPath := filepath.Join(dir, "GOAL.md")
	require.NoError(t, os.WriteFile(goalPath, []byte("---\nflow: |\n  \"coordinator\"\n---\n# Release Plan ###\n"), 0o644))

	title, errWrite := writeGoalTitle(goalPath, "repo-dir")
	require.NoError(t, errWrite)
	assert.Equal(t, "Release Plan", title)

	data, errRead := os.ReadFile(goalPath)
	require.NoError(t, errRead)
	assert.Equal(t, "---\ntitle: Release Plan\nflow: |\n  \"coordinator\"\n---\n# Release Plan ###\n", string(data))
}

func TestWriteGoalTitleRejectsNonHeadingGeneratedTitle(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "proseLine",
			content: "---\nflow: |\n  \"coordinator\"\n---\nBuild a REST API\n",
		},
		{
			name:    "checklistItem",
			content: "---\nflow: |\n  \"coordinator\"\n---\n- [ ] Ship it\n",
		},
		{
			name:    "secondaryHeading",
			content: "---\nflow: |\n  \"coordinator\"\n---\n## Tasks\n\n- [ ] Ship it\n",
		},
		{
			name:    "headingAfterProse",
			content: "---\nflow: |\n  \"coordinator\"\n---\nIntro paragraph.\n\n# Late Title\n",
		},
		{
			name:    "fourSpaceIndentedPseudoHeading",
			content: "---\nflow: |\n  \"coordinator\"\n---\n    # Code Block Title\n",
		},
		{
			name:    "tabIndentedPseudoHeading",
			content: "---\nflow: |\n  \"coordinator\"\n---\n\t# Code Block Title\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			goalPath := filepath.Join(dir, "GOAL.md")
			require.NoError(t, os.WriteFile(goalPath, []byte(tt.content), 0o644))

			title, errWrite := writeGoalTitle(goalPath, "repo-dir")
			require.Error(t, errWrite)
			assert.ErrorIs(t, errWrite, errGeneratedRepositoryTitleMissingHeading)
			assert.Empty(t, title)

			data, errRead := os.ReadFile(goalPath)
			require.NoError(t, errRead)
			assert.Equal(t, tt.content, string(data))
		})
	}
}

func TestRepositoryTitleFromContentPreservesFrontmatterTitleWhenOtherMetadataIsInvalid(t *testing.T) {
	content := []byte("---\ntitle: Preserved Title\nflow: [invalid yaml\n---\n# Body\n")

	title, needsGeneration, errTitle := repositoryTitleFromContent(content, "repo-dir")
	require.NoError(t, errTitle)
	assert.Equal(t, "Preserved Title", title)
	assert.False(t, needsGeneration)
}

func TestRepositoryTitleFromContentAllowsDelimiterSubstringInFrontmatterValues(t *testing.T) {
	content := []byte("---\ntitle: Keep --- title\ncontinuousModePrompt: keep --- prompt\n---\n# Body\n")

	title, needsGeneration, errTitle := repositoryTitleFromContent(content, "repo-dir")
	require.NoError(t, errTitle)
	assert.Equal(t, "Keep --- title", title)
	assert.False(t, needsGeneration)
}

func TestGenerateRepositoryTitleKeepsDirectoryFallbackForEmptyGoalBody(t *testing.T) {
	dir := t.TempDir()
	goalPath := filepath.Join(dir, "GOAL.md")
	original := "---\nflow: |\n  \"coordinator\"\n---\n"
	require.NoError(t, os.WriteFile(goalPath, []byte(original), 0o644))

	srv := NewServer(t.TempDir(), serverPaths{}, "")
	title, errGenerate := srv.generateRepositoryTitle(dir, "repo-dir")
	require.NoError(t, errGenerate)
	assert.Equal(t, "repo-dir", title)

	data, errRead := os.ReadFile(goalPath)
	require.NoError(t, errRead)
	assert.Equal(t, original, string(data))
}

func TestRepositoryTitleFromContentKeepsDirectoryFallbackForEmptyGoalBody(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "emptyFrontmatterOnly",
			content: "---\n---\n",
		},
		{
			name:    "frontmatterWithoutBody",
			content: "---\nflow: |\n  \"coordinator\"\n---\n",
		},
		{
			name:    "whitespaceBodyOnly",
			content: "---\nflow: |\n  \"coordinator\"\n---\n\n \t\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title, needsGeneration, errTitle := repositoryTitleFromContent([]byte(tt.content), "repo-dir")
			require.NoError(t, errTitle)
			assert.Equal(t, "repo-dir", title)
			assert.False(t, needsGeneration)
		})
	}
}

func TestResolveRepositoryTitleForInterfaceQueuesHeadingBackfill(t *testing.T) {
	dir := t.TempDir()
	goalPath := filepath.Join(dir, "GOAL.md")
	require.NoError(t, os.WriteFile(goalPath, []byte("---\nflow: |\n  \"coordinator\"\n# keep this comment\n---\n# Generated   \n\nBody line\n"), 0o644))

	srv := NewServer(t.TempDir(), serverPaths{}, "")
	got := srv.resolveRepositoryTitleForInterface(dir, "repo-dir")
	assert.Equal(t, "repo-dir", got)

	waitForTestCondition(t, func() bool {
		data, errRead := os.ReadFile(goalPath)
		if errRead != nil {
			return false
		}
		return string(data) == "---\ntitle: Generated\nflow: |\n  \"coordinator\"\n# keep this comment\n---\n# Generated   \n\nBody line\n"
	})

	data, errRead := os.ReadFile(goalPath)
	require.NoError(t, errRead)
	assert.Equal(t, "---\ntitle: Generated\nflow: |\n  \"coordinator\"\n# keep this comment\n---\n# Generated   \n\nBody line\n", string(data))
}

func TestGenerateRepositoryTitleRejectsNonHeadingBody(t *testing.T) {
	dir := t.TempDir()
	goalPath := filepath.Join(dir, "GOAL.md")
	original := "---\nflow: |\n  \"coordinator\"\n---\nBuild a REST API\n"
	require.NoError(t, os.WriteFile(goalPath, []byte(original), 0o644))

	srv := NewServer(t.TempDir(), serverPaths{}, "")
	title, errGenerate := srv.generateRepositoryTitle(dir, "repo-dir")
	require.Error(t, errGenerate)
	assert.Empty(t, title)

	data, errRead := os.ReadFile(goalPath)
	require.NoError(t, errRead)
	assert.Equal(t, original, string(data))
}

func TestBuildWorkspaceFullStateKeepsDirectoryNameForLookup(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "repo-dir")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\ntitle: Display Title\n---\n# Goal\n"), 0o644))

	fullState := srv.buildWorkspaceFullState(workspaceInfo{
		Directory:    wsDir,
		DirName:      "repo-dir",
		HasWorkspace: true,
	}, nil)

	assert.Equal(t, "repo-dir", fullState.Name)
	assert.Equal(t, "Display Title", fullState.Title)
}
