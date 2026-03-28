package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestMenuBarItem() menuBarItem {
	return menuBarItem{
		name:       "",
		title:      "",
		needsInput: false,
		running:    false,
		stopped:    false,
		pinned:     false,
	}
}

func TestCountAttention(t *testing.T) {
	tests := []struct {
		name     string
		items    []menuBarItem
		expected int
	}{
		{
			name:     "emptyItems",
			items:    []menuBarItem{},
			expected: 0,
		},
		{
			name: "needsInput",
			items: []menuBarItem{
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.needsInput = true }),
			},
			expected: 1,
		},
		{
			name: "stopped",
			items: []menuBarItem{
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.stopped = true }),
			},
			expected: 1,
		},
		{
			name: "running",
			items: []menuBarItem{
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.running = true }),
			},
			expected: 0,
		},
		{
			name: "pinned",
			items: []menuBarItem{
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.pinned = true }),
			},
			expected: 0,
		},
		{
			name: "mixedItems",
			items: []menuBarItem{
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.needsInput = true }),
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.running = true }),
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.stopped = true }),
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.pinned = true }),
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := countAttention(tt.items)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCountRunning(t *testing.T) {
	tests := []struct {
		name     string
		items    []menuBarItem
		expected int
	}{
		{
			name:     "emptyItems",
			items:    []menuBarItem{},
			expected: 0,
		},
		{
			name: "running",
			items: []menuBarItem{
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.running = true }),
			},
			expected: 1,
		},
		{
			name: "notRunning",
			items: []menuBarItem{
				newTestMenuBarItem(),
			},
			expected: 0,
		},
		{
			name: "mixedItems",
			items: []menuBarItem{
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.running = true }),
				newTestMenuBarItem(),
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.running = true }),
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := countRunning(tt.items)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCountActive(t *testing.T) {
	tests := []struct {
		name     string
		items    []menuBarItem
		expected int
	}{
		{
			name:     "emptyItems",
			items:    []menuBarItem{},
			expected: 0,
		},
		{
			name: "running",
			items: []menuBarItem{
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.running = true }),
			},
			expected: 1,
		},
		{
			name: "stopped",
			items: []menuBarItem{
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.stopped = true }),
			},
			expected: 1,
		},
		{
			name: "needsInput",
			items: []menuBarItem{
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.needsInput = true }),
			},
			expected: 1,
		},
		{
			name: "pinned",
			items: []menuBarItem{
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.pinned = true }),
			},
			expected: 1,
		},
		{
			name: "inactive",
			items: []menuBarItem{
				newTestMenuBarItem(),
			},
			expected: 0,
		},
		{
			name: "mixedItems",
			items: []menuBarItem{
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.running = true }),
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.stopped = true }),
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.needsInput = true }),
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.pinned = true }),
				newTestMenuBarItem(),
			},
			expected: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := countActive(tt.items)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFilterVisibleItems(t *testing.T) {
	tests := []struct {
		name     string
		items    []menuBarItem
		expected int
	}{
		{
			name:     "emptyItems",
			items:    []menuBarItem{},
			expected: 0,
		},
		{
			name: "needsInput",
			items: []menuBarItem{
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.needsInput = true; item.name = "test" }),
			},
			expected: 1,
		},
		{
			name: "stopped",
			items: []menuBarItem{
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.stopped = true; item.name = "test" }),
			},
			expected: 1,
		},
		{
			name: "pinned",
			items: []menuBarItem{
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.pinned = true; item.name = "test" }),
			},
			expected: 1,
		},
		{
			name: "runningNotVisible",
			items: []menuBarItem{
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.running = true; item.name = "test" }),
			},
			expected: 0,
		},
		{
			name: "mixedItems",
			items: []menuBarItem{
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.needsInput = true; item.name = "a" }),
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.running = true; item.name = "b" }),
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.stopped = true; item.name = "c" }),
				updated(newTestMenuBarItem(), func(item *menuBarItem) { item.pinned = true; item.name = "d" }),
			},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterVisibleItems(tt.items)
			assert.Len(t, result, tt.expected)
		})
	}
}

func TestFormatMenuItemLabel(t *testing.T) {
	tests := []struct {
		name     string
		item     menuBarItem
		expected string
	}{
		{
			name: "needsInput",
			item: updated(newTestMenuBarItem(), func(item *menuBarItem) {
				item.name = "workspace"
				item.title = "Test Goal"
				item.needsInput = true
			}),
			expected: "\u26A0 Test Goal (Needs Input)",
		},
		{
			name: "runningAndPinned",
			item: updated(newTestMenuBarItem(), func(item *menuBarItem) {
				item.name = "workspace"
				item.title = "Test Goal"
				item.running = true
				item.pinned = true
			}),
			expected: "\u25B6 Test Goal (Running)",
		},
		{
			name: "pinnedOnly",
			item: updated(newTestMenuBarItem(), func(item *menuBarItem) {
				item.name = "workspace"
				item.title = "Test Goal"
				item.pinned = true
			}),
			expected: "\u25CB Test Goal",
		},
		{
			name: "stopped",
			item: updated(newTestMenuBarItem(), func(item *menuBarItem) {
				item.name = "workspace"
				item.title = "Test Goal"
				item.stopped = true
			}),
			expected: "\u25A0 Test Goal (Stopped)",
		},
		{
			name: "default",
			item: updated(newTestMenuBarItem(), func(item *menuBarItem) {
				item.name = "workspace"
				item.title = "Test Goal"
			}),
			expected: "Test Goal",
		},
		{
			name: "noTitle",
			item: updated(newTestMenuBarItem(), func(item *menuBarItem) {
				item.name = "workspace"
			}),
			expected: "workspace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatMenuItemLabel(tt.item)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWorkspaceItemSubpath(t *testing.T) {
	tests := []struct {
		name     string
		item     menuBarItem
		expected string
	}{
		{
			name:     "needsInput",
			item:     updated(newTestMenuBarItem(), func(item *menuBarItem) { item.needsInput = true }),
			expected: "respond",
		},
		{
			name:     "notNeedsInput",
			item:     newTestMenuBarItem(),
			expected: "progress",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := workspaceItemSubpath(tt.item)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWorkspaceURL(t *testing.T) {
	tests := []struct {
		name      string
		baseURL   string
		workspace string
		subpath   string
		expected  string
	}{
		{
			name:      "validURL",
			baseURL:   "http://localhost:8080",
			workspace: "my-workspace",
			subpath:   "progress",
			expected:  "http://localhost:8080/workspaces/my-workspace/progress",
		},
		{
			name:      "urlWithTrailingSlash",
			baseURL:   "http://localhost:8080/",
			workspace: "my-workspace",
			subpath:   "progress",
			expected:  "http://localhost:8080/workspaces/my-workspace/progress",
		},
		{
			name:      "urlWithPath",
			baseURL:   "http://localhost:8080/some/path",
			workspace: "my-workspace",
			subpath:   "progress",
			expected:  "http://localhost:8080/workspaces/my-workspace/progress",
		},
		{
			name:      "invalidURL",
			baseURL:   "://invalid",
			workspace: "my-workspace",
			subpath:   "progress",
			expected:  "://invalid/workspaces/my-workspace/progress",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := workspaceURL(tt.baseURL, tt.workspace, tt.subpath)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAllocTag(t *testing.T) {
	state := newMenuBarState()

	tag1 := allocTag(&state, menuBarAction{actionURL: "url1"})
	assert.Equal(t, 1, tag1)

	tag2 := allocTag(&state, menuBarAction{actionURL: "url2"})
	assert.Equal(t, 2, tag2)

	assert.Len(t, state.tags, 2)
	assert.Equal(t, "url1", state.tags[1].actionURL)
	assert.Equal(t, "url2", state.tags[2].actionURL)
}

func TestFormatMenuItemLabelVariants(t *testing.T) {
	t.Run("needsInput", func(t *testing.T) {
		label := formatMenuItemLabel(updated(newTestMenuBarItem(), func(item *menuBarItem) { item.name = "ws"; item.needsInput = true }))
		assert.Contains(t, label, "Needs Input")
	})
	t.Run("stopped", func(t *testing.T) {
		label := formatMenuItemLabel(updated(newTestMenuBarItem(), func(item *menuBarItem) { item.name = "ws"; item.stopped = true }))
		assert.Contains(t, label, "Stopped")
	})
	t.Run("pinnedRunning", func(t *testing.T) {
		label := formatMenuItemLabel(updated(newTestMenuBarItem(), func(item *menuBarItem) { item.name = "ws"; item.running = true; item.pinned = true }))
		assert.Contains(t, label, "Running")
	})
	t.Run("idle", func(t *testing.T) {
		label := formatMenuItemLabel(updated(newTestMenuBarItem(), func(item *menuBarItem) { item.name = "ws" }))
		assert.Equal(t, "ws", label)
	})
}

func TestFilterVisibleItemsResult(t *testing.T) {
	items := []menuBarItem{
		updated(newTestMenuBarItem(), func(item *menuBarItem) { item.name = "running-ws"; item.running = true }),
		updated(newTestMenuBarItem(), func(item *menuBarItem) { item.name = "idle-ws" }),
		updated(newTestMenuBarItem(), func(item *menuBarItem) { item.name = "pinned-ws"; item.pinned = true }),
		updated(newTestMenuBarItem(), func(item *menuBarItem) { item.name = "input-ws"; item.needsInput = true }),
		updated(newTestMenuBarItem(), func(item *menuBarItem) { item.name = "stopped-ws"; item.stopped = true }),
	}
	filtered := filterVisibleItems(items)
	assert.Len(t, filtered, 3)
}

func TestToMenuBarItemRepairsMissingGoalTitle(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	goalPath := filepath.Join(wsDir, "GOAL.md")
	require.NoError(t, os.WriteFile(goalPath, []byte("---\nflow: test\n---\n# Improve Menu Title"), 0o644))

	server.goalTitleComposer = func(_ string, _ []byte) (string, error) {
		return "Menu Repair Title", nil
	}

	item := toMenuBarItem(server, updated(newTestWorkspaceInfo(), func(workspace *workspaceInfo) { workspace.DirName = "test-ws"; workspace.Directory = wsDir }))
	assert.Equal(t, "test-ws", item.title)

	require.Eventually(t, func() bool {
		data, errRead := os.ReadFile(goalPath)
		return errRead == nil && strings.Contains(string(data), "title: Menu Repair Title")
	}, time.Second, 10*time.Millisecond)

	item = toMenuBarItem(server, updated(newTestWorkspaceInfo(), func(workspace *workspaceInfo) { workspace.DirName = "test-ws"; workspace.Directory = wsDir }))
	assert.Equal(t, "Menu Repair Title", item.title)
}
