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

func snapshotWith(update func(*workspaceStateSnapshot)) workspaceStateSnapshot {
	snapshot := workspaceStateSnapshot{
		modTime:      time.Time{},
		status:       "",
		needsInput:   false,
		progressLen:  0,
		todosHash:    "",
		messagesHash: "",
		goalModTime:  time.Time{},
		goalHash:     "",
	}
	if update != nil {
		update(&snapshot)
	}
	return snapshot
}

func TestHashTodos(t *testing.T) {
	tests := []struct {
		name         string
		projectTodos []state.TodoItem
		agentTodos   []state.TodoItem
	}{
		{
			name:         "emptyTodos",
			projectTodos: []state.TodoItem{},
			agentTodos:   []state.TodoItem{},
		},
		{
			name: "projectTodosOnly",
			projectTodos: []state.TodoItem{
				todoItemWith(func(item *state.TodoItem) {
					item.Content = "Task 1"
					item.Status = "pending"
					item.Priority = "high"
				}),
			},
			agentTodos: []state.TodoItem{},
		},
		{
			name:         "agentTodosOnly",
			projectTodos: []state.TodoItem{},
			agentTodos: []state.TodoItem{
				todoItemWith(func(item *state.TodoItem) {
					item.Content = "Task 2"
					item.Status = "completed"
					item.Priority = "low"
				}),
			},
		},
		{
			name: "bothTodos",
			projectTodos: []state.TodoItem{
				todoItemWith(func(item *state.TodoItem) {
					item.Content = "Task 1"
					item.Status = "pending"
					item.Priority = "high"
				}),
			},
			agentTodos: []state.TodoItem{
				todoItemWith(func(item *state.TodoItem) {
					item.Content = "Task 2"
					item.Status = "completed"
					item.Priority = "low"
				}),
			},
		},
		{
			name: "multipleTodos",
			projectTodos: []state.TodoItem{
				todoItemWith(func(item *state.TodoItem) {
					item.Content = "Task 1"
					item.Status = "pending"
					item.Priority = "high"
				}),
				todoItemWith(func(item *state.TodoItem) {
					item.Content = "Task 2"
					item.Status = "in_progress"
					item.Priority = "medium"
				}),
			},
			agentTodos: []state.TodoItem{
				todoItemWith(func(item *state.TodoItem) {
					item.Content = "Task 3"
					item.Status = "completed"
					item.Priority = "low"
				}),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hashTodos(tt.projectTodos, tt.agentTodos)
			assert.Len(t, result, 16)
			assert.NotEmpty(t, result)
		})
	}
}

func TestHashTodosConsistency(t *testing.T) {
	todos := []state.TodoItem{
		todoItemWith(func(item *state.TodoItem) {
			item.Content = "Task 1"
			item.Status = "pending"
			item.Priority = "high"
		}),
	}

	hash1 := hashTodos(todos, []state.TodoItem{})
	hash2 := hashTodos(todos, []state.TodoItem{})

	assert.Equal(t, hash1, hash2, "same input should produce same hash")
}

func TestHashTodosDifferent(t *testing.T) {
	todos1 := []state.TodoItem{
		todoItemWith(func(item *state.TodoItem) {
			item.Content = "Task 1"
			item.Status = "pending"
			item.Priority = "high"
		}),
	}
	todos2 := []state.TodoItem{
		todoItemWith(func(item *state.TodoItem) {
			item.Content = "Task 2"
			item.Status = "pending"
			item.Priority = "high"
		}),
	}

	hash1 := hashTodos(todos1, []state.TodoItem{})
	hash2 := hashTodos(todos2, []state.TodoItem{})

	assert.NotEqual(t, hash1, hash2, "different input should produce different hash")
}

func TestHashMessages(t *testing.T) {
	tests := []struct {
		name     string
		messages []state.Message
	}{
		{
			name:     "emptyMessages",
			messages: []state.Message{},
		},
		{
			name: "singleMessage",
			messages: []state.Message{
				messageWith(func(message *state.Message) {
					message.ID = 1
					message.FromAgent = "agent1"
					message.ToAgent = "agent2"
					message.Body = "Hello"
				}),
			},
		},
		{
			name: "multipleMessages",
			messages: []state.Message{
				messageWith(func(message *state.Message) {
					message.ID = 1
					message.FromAgent = "agent1"
					message.ToAgent = "agent2"
					message.Body = "Hello"
				}),
				messageWith(func(message *state.Message) {
					message.ID = 2
					message.FromAgent = "agent2"
					message.ToAgent = "agent1"
					message.Body = "World"
				}),
			},
		},
		{
			name: "messageWithReadStatus",
			messages: []state.Message{
				messageWith(func(message *state.Message) {
					message.ID = 1
					message.FromAgent = "agent1"
					message.ToAgent = "agent2"
					message.Body = "Hello"
					message.Read = true
				}),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hashMessages(tt.messages)
			assert.Len(t, result, 16)
			assert.NotEmpty(t, result)
		})
	}
}

func TestHashMessagesConsistency(t *testing.T) {
	messages := []state.Message{
		messageWith(func(message *state.Message) {
			message.ID = 1
			message.FromAgent = "agent1"
			message.ToAgent = "agent2"
			message.Body = "Hello"
		}),
	}

	hash1 := hashMessages(messages)
	hash2 := hashMessages(messages)

	assert.Equal(t, hash1, hash2, "same input should produce same hash")
}

func TestHashMessagesDifferent(t *testing.T) {
	messages1 := []state.Message{
		messageWith(func(message *state.Message) {
			message.ID = 1
			message.FromAgent = "agent1"
			message.ToAgent = "agent2"
			message.Body = "Hello"
		}),
	}
	messages2 := []state.Message{
		messageWith(func(message *state.Message) {
			message.ID = 1
			message.FromAgent = "agent1"
			message.ToAgent = "agent2"
			message.Body = "World"
		}),
	}

	hash1 := hashMessages(messages1)
	hash2 := hashMessages(messages2)

	assert.NotEqual(t, hash1, hash2, "different input should produce different hash")
}

func TestHashGoalFile(t *testing.T) {
	tests := []struct {
		name     string
		fileInfo os.FileInfo
	}{
		{
			name:     "nilFileInfo",
			fileInfo: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hashGoalFile(tt.fileInfo)
			if tt.fileInfo == nil {
				assert.Empty(t, result)
			} else {
				assert.Len(t, result, 16)
			}
		})
	}
}

func TestHashGoalFileWithFileInfo(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "goal_test_*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(tmpFile.Name()) })

	_, _ = tmpFile.WriteString("test content")
	_ = tmpFile.Close()

	fileInfo, err := os.Stat(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	result := hashGoalFile(fileInfo)
	assert.Len(t, result, 16)
	assert.NotEmpty(t, result)
}

func TestHashGoalFileConsistency(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "goal_test_*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(tmpFile.Name()) })

	_, _ = tmpFile.WriteString("test content")
	_ = tmpFile.Close()

	fileInfo, err := os.Stat(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	hash1 := hashGoalFile(fileInfo)
	hash2 := hashGoalFile(fileInfo)

	assert.Equal(t, hash1, hash2, "same file info should produce same hash")
}

func TestHashGoalFileDifferentAfterModification(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "goal_test_*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(tmpFile.Name()) })

	_, _ = tmpFile.WriteString("test content")
	_ = tmpFile.Close()

	fileInfo1, err := os.Stat(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	hash1 := hashGoalFile(fileInfo1)

	time.Sleep(10 * time.Millisecond)

	_ = os.WriteFile(tmpFile.Name(), []byte("modified content"), 0o644)

	fileInfo2, err := os.Stat(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	hash2 := hashGoalFile(fileInfo2)

	assert.NotEqual(t, hash1, hash2, "modified file should produce different hash")
}

func TestBuildStateSnapshot(t *testing.T) {
	modTime := time.Now()
	wfState := workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.HumanMessage = "test question"
		workflow.Progress = []state.ProgressEntry{
			progressEntryWith(func(entry *state.ProgressEntry) {
				entry.Agent = "coordinator"
				entry.Description = "started"
			}),
		}
		workflow.ProjectTodos = []state.TodoItem{
			todoItemWith(func(item *state.TodoItem) {
				item.Content = "task1"
				item.Status = "pending"
			}),
		}
		workflow.Messages = []state.Message{
			messageWith(func(message *state.Message) {
				message.ID = 1
				message.FromAgent = "dev"
				message.ToAgent = "coord"
				message.Body = "done"
			}),
		}
	})

	snapshot := buildStateSnapshot(modTime, &wfState, nil)
	assert.Equal(t, modTime, snapshot.modTime)
	assert.Equal(t, state.StatusWorking, snapshot.status)
	assert.True(t, snapshot.needsInput)
	assert.Equal(t, 1, snapshot.progressLen)
	assert.NotEmpty(t, snapshot.todosHash)
	assert.NotEmpty(t, snapshot.messagesHash)
	assert.Empty(t, snapshot.goalHash)
}

func TestBuildStateSnapshotWithGoalInfo(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "goal_test_*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(tmpFile.Name()) })
	_, _ = tmpFile.WriteString("# Goal")
	_ = tmpFile.Close()

	goalInfo, err := os.Stat(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	modTime := time.Now()
	wfState := newTestWorkflow()
	snapshot := buildStateSnapshot(modTime, &wfState, goalInfo)
	assert.NotEmpty(t, snapshot.goalHash)
	assert.False(t, snapshot.goalModTime.IsZero())
}

func TestEmitStateChangeEvents(t *testing.T) {
	server, _ := setupTestServer(t)

	t.Run("noChange", func(_ *testing.T) {
		prev := snapshotWith(func(snapshot *workspaceStateSnapshot) {
			snapshot.status = state.StatusWorking
			snapshot.progressLen = 5
			snapshot.todosHash = "abc"
		})
		server.emitStateChangeEvents(&prev, &prev)
	})

	t.Run("statusChange", func(_ *testing.T) {
		prev := snapshotWith(func(snapshot *workspaceStateSnapshot) {
			snapshot.status = state.StatusWorking
		})
		current := snapshotWith(func(snapshot *workspaceStateSnapshot) {
			snapshot.status = state.StatusComplete
		})
		server.emitStateChangeEvents(&prev, &current)
	})

	t.Run("needsInputChange", func(_ *testing.T) {
		prev := snapshotWith(nil)
		current := snapshotWith(func(snapshot *workspaceStateSnapshot) {
			snapshot.needsInput = true
		})
		server.emitStateChangeEvents(&prev, &current)
	})

	t.Run("progressChange", func(_ *testing.T) {
		prev := snapshotWith(func(snapshot *workspaceStateSnapshot) {
			snapshot.progressLen = 5
		})
		current := snapshotWith(func(snapshot *workspaceStateSnapshot) {
			snapshot.progressLen = 6
		})
		server.emitStateChangeEvents(&prev, &current)
	})
}

func TestCheckWorkspaceStateSecondVisitNoChange(t *testing.T) {
	srv, _ := setupTestServer(t)
	dir := t.TempDir()
	sgaiDir := filepath.Join(dir, ".sgai")
	require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
	sp := filepath.Join(sgaiDir, "state.json")
	_, errCoord := state.NewCoordinatorWith(sp, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
	}))
	require.NoError(t, errCoord)

	snapshots := make(map[string]workspaceStateSnapshot)
	active := make(map[string]bool)

	srv.checkWorkspaceState(dir, snapshots, active)
	assert.Contains(t, snapshots, dir)

	srv.checkWorkspaceState(dir, snapshots, active)
	assert.Contains(t, snapshots, dir)
}

func TestCheckWorkspaceStateWithMapChanges(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "cwsc-ws")
	sp := filepath.Join(wsDir, ".sgai", "state.json")
	_, errCoord := state.NewCoordinatorWith(sp, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
		workflow.Task = "all done"
	}))
	require.NoError(t, errCoord)

	snapshots := make(map[string]workspaceStateSnapshot)
	activeWorkspaces := make(map[string]bool)

	srv.checkWorkspaceState(wsDir, snapshots, activeWorkspaces)
	assert.True(t, activeWorkspaces[wsDir])
	assert.Contains(t, snapshots, wsDir)
	assert.Equal(t, string(state.StatusComplete), snapshots[wsDir].status)
}

func TestBuildStateSnapshotWithGoalHash(t *testing.T) {
	dir := t.TempDir()
	sgaiDir := filepath.Join(dir, ".sgai")
	require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "GOAL.md"), []byte("# Goal"), 0o644))
	sp := filepath.Join(sgaiDir, "state.json")
	_, errCoord := state.NewCoordinatorWith(sp, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
	}))
	require.NoError(t, errCoord)

	srv, _ := setupTestServer(t)
	snapshots := make(map[string]workspaceStateSnapshot)
	activeWorkspaces := make(map[string]bool)
	srv.checkWorkspaceState(dir, snapshots, activeWorkspaces)

	snap, exists := snapshots[dir]
	assert.True(t, exists)
	assert.NotEmpty(t, snap.goalHash)
}

func TestPollWorkspaceStatesWithMultipleWorkspaces(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir1 := setupTestWorkspace(t, srv, rootDir, "poll-ws1")
	wsDir2 := setupTestWorkspace(t, srv, rootDir, "poll-ws2")
	sp1 := filepath.Join(wsDir1, ".sgai", "state.json")
	_, errCoord1 := state.NewCoordinatorWith(sp1, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
	}))
	require.NoError(t, errCoord1)

	stopCachedSession(t, srv, wsDir2, workflowRef(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
	}))
	writeWorkflowStateToDisk(t, wsDir2, workflowRef(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.Task = "building"
	}))

	snapshots := make(map[string]workspaceStateSnapshot)
	srv.pollWorkspaceStates(snapshots)
	assert.NotEmpty(t, snapshots)
	assert.Equal(t, state.StatusComplete, snapshots[wsDir1].status)
	assert.Equal(t, state.StatusWorking, snapshots[wsDir2].status)
	assert.Equal(t, state.StatusWorking, workflowStateFromDisk(t, wsDir2).Status)
}

func TestPollWorkspaceStatesCleanupRemoved(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "poll-cleanup")
	sp := filepath.Join(wsDir, ".sgai", "state.json")
	_, errCoord := state.NewCoordinatorWith(sp, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
	}))
	require.NoError(t, errCoord)

	snapshots := make(map[string]workspaceStateSnapshot)
	snapshots["/nonexistent/removed-ws"] = snapshotWith(func(snapshot *workspaceStateSnapshot) {
		snapshot.status = "old"
	})
	srv.pollWorkspaceStates(snapshots)
	_, exists := snapshots["/nonexistent/removed-ws"]
	assert.False(t, exists)
}
