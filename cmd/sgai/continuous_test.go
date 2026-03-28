package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ucirello/sgai/pkg/state"
)

func TestHasHumanPartnerMessage(t *testing.T) {
	tests := []struct {
		name          string
		messages      []state.Message
		expectedFound bool
		expectedID    int
	}{
		{
			name:          "emptyMessages",
			messages:      []state.Message{},
			expectedFound: false,
			expectedID:    0,
		},
		{
			name: "humanPartnerMessage",
			messages: []state.Message{
				{ID: 1, FromAgent: "Human Partner", ToAgent: "", Body: "", Read: false, ReadAt: "", ReadBy: "", CreatedAt: ""},
			},
			expectedFound: true,
			expectedID:    1,
		},
		{
			name: "humanPartnerMessageAlreadyRead",
			messages: []state.Message{
				{ID: 1, FromAgent: "Human Partner", ToAgent: "", Body: "", Read: true, ReadAt: "", ReadBy: "", CreatedAt: ""},
			},
			expectedFound: false,
			expectedID:    0,
		},
		{
			name: "otherAgentMessage",
			messages: []state.Message{
				{ID: 1, FromAgent: "agent1", ToAgent: "", Body: "", Read: false, ReadAt: "", ReadBy: "", CreatedAt: ""},
			},
			expectedFound: false,
			expectedID:    0,
		},
		{
			name: "mixedMessages",
			messages: []state.Message{
				{ID: 1, FromAgent: "agent1", ToAgent: "", Body: "", Read: false, ReadAt: "", ReadBy: "", CreatedAt: ""},
				{ID: 2, FromAgent: "Human Partner", ToAgent: "", Body: "", Read: false, ReadAt: "", ReadBy: "", CreatedAt: ""},
				{ID: 3, FromAgent: "agent2", ToAgent: "", Body: "", Read: false, ReadAt: "", ReadBy: "", CreatedAt: ""},
			},
			expectedFound: true,
			expectedID:    2,
		},
		{
			name: "multipleHumanPartnerMessages",
			messages: []state.Message{
				{ID: 1, FromAgent: "Human Partner", ToAgent: "", Body: "", Read: true, ReadAt: "", ReadBy: "", CreatedAt: ""},
				{ID: 2, FromAgent: "Human Partner", ToAgent: "", Body: "", Read: false, ReadAt: "", ReadBy: "", CreatedAt: ""},
			},
			expectedFound: true,
			expectedID:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found, msg := hasHumanPartnerMessage(tt.messages)
			assert.Equal(t, tt.expectedFound, found)
			if tt.expectedFound {
				assert.NotNil(t, msg)
				assert.Equal(t, tt.expectedID, msg.ID)
			} else {
				assert.Nil(t, msg)
			}
		})
	}
}

func TestReadContinuousModePrompt(t *testing.T) {
	tests := []struct {
		name        string
		goalContent string
		expected    string
	}{
		{
			name: "withContinuousModePrompt",
			goalContent: `---
continuousModePrompt: "Check for new issues every hour"
---
# Test Goal`,
			expected: "Check for new issues every hour",
		},
		{
			name: "withoutContinuousModePrompt",
			goalContent: `---
---
# Test Goal`,
			expected: "",
		},
		{
			name:        "noGoalFile",
			goalContent: "",
			expected:    "",
		},
		{
			name: "emptyContinuousModePrompt",
			goalContent: `---
continuousModePrompt: ""
---
# Test Goal`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspacePath := t.TempDir()

			if tt.goalContent != "" {
				goalPath := filepath.Join(workspacePath, "GOAL.md")
				require.NoError(t, os.WriteFile(goalPath, []byte(tt.goalContent), 0o644))
			}

			result := readContinuousModePrompt(workspacePath)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestReadContinuousModeAutoCron(t *testing.T) {
	tests := []struct {
		name           string
		goalContent    string
		expectedDur    time.Duration
		expectedPrompt string
	}{
		{
			name: "withAutoCron",
			goalContent: `---
continuousModeAuto: "1h"
continuousModeCron: "Check for updates"
---
# Test Goal`,
			expectedDur:    time.Hour,
			expectedPrompt: "Check for updates",
		},
		{
			name: "withAutoCronNoPrompt",
			goalContent: `---
continuousModeAuto: "30m"
---
# Test Goal`,
			expectedDur:    30 * time.Minute,
			expectedPrompt: "",
		},
		{
			name: "withoutAutoCron",
			goalContent: `---
---
# Test Goal`,
			expectedDur:    0,
			expectedPrompt: "",
		},
		{
			name:           "noGoalFile",
			goalContent:    "",
			expectedDur:    0,
			expectedPrompt: "",
		},
		{
			name: "invalidDuration",
			goalContent: `---
continuousModeAuto: "invalid"
---
# Test Goal`,
			expectedDur:    0,
			expectedPrompt: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspacePath := t.TempDir()

			if tt.goalContent != "" {
				goalPath := filepath.Join(workspacePath, "GOAL.md")
				require.NoError(t, os.WriteFile(goalPath, []byte(tt.goalContent), 0o644))
			}

			dur, prompt := readContinuousModeAutoCron(workspacePath)
			assert.Equal(t, tt.expectedDur, dur)
			assert.Equal(t, tt.expectedPrompt, prompt)
		})
	}
}

func TestUpdateContinuousModeState(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	wf := testWorkflow()
	wf.Status = state.StatusWorking
	wf.Progress = []state.ProgressEntry{}
	coord, err := state.NewCoordinatorWith(statePath, wf)
	require.NoError(t, err)

	updateContinuousModeState(coord, "running tests", "test-agent", "started test execution")

	snapshot := coord.State()
	assert.Equal(t, "running tests", snapshot.Task)
	assert.Equal(t, "test-agent", snapshot.CurrentAgent)
	assert.Len(t, snapshot.Progress, 1)
	assert.Equal(t, "test-agent", snapshot.Progress[0].Agent)
	assert.Equal(t, "started test execution", snapshot.Progress[0].Description)
}

func TestUpdateContinuousModeProgress(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	wf := testWorkflow()
	wf.Status = state.StatusWorking
	progress := testProgressEntry()
	progress.Agent = "initial"
	progress.Description = "first entry"
	wf.Progress = []state.ProgressEntry{progress}
	coord, err := state.NewCoordinatorWith(statePath, wf)
	require.NoError(t, err)

	updateContinuousModeProgress(coord, "completed phase 2")

	snapshot := coord.State()
	assert.Len(t, snapshot.Progress, 2)
	assert.Equal(t, "continuous-mode", snapshot.Progress[1].Agent)
	assert.Equal(t, "completed phase 2", snapshot.Progress[1].Description)
}

func TestMarkMessageAsRead(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	wf := testWorkflow()
	wf.Status = state.StatusWorking
	wf.Messages = []state.Message{
		{ID: 1, FromAgent: "Human Partner", ToAgent: "coordinator", Body: "run the tests", Read: false, ReadAt: "", ReadBy: "", CreatedAt: ""},
		{ID: 2, FromAgent: "agent1", ToAgent: "agent2", Body: "other msg", Read: false, ReadAt: "", ReadBy: "", CreatedAt: ""},
	}
	coord, err := state.NewCoordinatorWith(statePath, wf)
	require.NoError(t, err)

	markMessageAsRead(coord, 1)

	snapshot := coord.State()
	assert.True(t, snapshot.Messages[0].Read)
	assert.Equal(t, "continuous-mode", snapshot.Messages[0].ReadBy)
	assert.NotEmpty(t, snapshot.Messages[0].ReadAt)
	assert.False(t, snapshot.Messages[1].Read)
}

func TestMarkMessageAsReadNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	wf := testWorkflow()
	wf.Status = state.StatusWorking
	wf.Messages = []state.Message{{ID: 1, FromAgent: "agent1", ToAgent: "agent2", Body: "msg", Read: false, ReadAt: "", ReadBy: "", CreatedAt: ""}}
	coord, err := state.NewCoordinatorWith(statePath, wf)
	require.NoError(t, err)

	markMessageAsRead(coord, 999)

	snapshot := coord.State()
	assert.False(t, snapshot.Messages[0].Read)
}

func TestResetWorkflowForNextCycle(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	wf := testWorkflow()
	wf.Status = state.StatusComplete
	wf.InteractionMode = state.ModeSelfDrive
	wf.CurrentAgent = "backend-developer"
	coord, err := state.NewCoordinatorWith(statePath, wf)
	require.NoError(t, err)

	resetWorkflowForNextCycle(coord)

	snapshot := coord.State()
	assert.Equal(t, state.StatusWorking, snapshot.Status)
	assert.Equal(t, state.ModeContinuous, snapshot.InteractionMode)
	assert.Equal(t, "coordinator", snapshot.CurrentAgent)
}

func TestPrependSteeringMessage(t *testing.T) {
	tests := []struct {
		name         string
		existingGoal string
		message      string
		expected     string
		skipCreate   bool
	}{
		{
			name:         "noFrontmatter",
			existingGoal: "# My Goal\n\nSome content",
			message:      "Steering message",
			expected:     "Steering message\n\n# My Goal\n\nSome content",
			skipCreate:   false,
		},
		{
			name: "withFrontmatter",
			existingGoal: `---
flow: |
  "a" -> "b"
---
# My Goal

Some content`,
			message: "Steering message",
			expected: `---
flow: |
  "a" -> "b"
---

Steering message

# My Goal

Some content`,
			skipCreate: false,
		},
		{
			name: "emptyGoal",
			existingGoal: `---
---
`,
			message:    "Steering message",
			expected:   "---\n---\n\nSteering message\n\n",
			skipCreate: false,
		},
		{
			name:         "emptyContent",
			existingGoal: "",
			message:      "Steering message",
			expected:     "Steering message\n\n",
			skipCreate:   true,
		},
		{
			name: "unclosedFrontmatter",
			existingGoal: `---
flow: |
  "a" -> "b"
# My Goal`,
			message:    "Steering message",
			expected:   "Steering message\n\n---\nflow: |\n  \"a\" -> \"b\"\n# My Goal",
			skipCreate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			goalPath := filepath.Join(tmpDir, "GOAL.md")
			if tt.existingGoal != "" && !tt.skipCreate {
				require.NoError(t, os.WriteFile(goalPath, []byte(tt.existingGoal), 0o644))
			}

			err := prependSteeringMessage(goalPath, tt.message)
			if tt.skipCreate {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			content, err := os.ReadFile(goalPath)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, string(content))
		})
	}
}

func TestPrependSteeringMessageNoFile(t *testing.T) {
	tmpDir := t.TempDir()
	goalPath := filepath.Join(tmpDir, "GOAL.md")

	err := prependSteeringMessage(goalPath, "test message")
	require.Error(t, err)
}

func TestWatchForTriggerCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dir := t.TempDir()
	sgaiDir := filepath.Join(dir, ".sgai")
	require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "GOAL.md"), []byte("# Goal"), 0o644))

	statePath := filepath.Join(sgaiDir, "state.json")
	coord, errCoord := state.NewCoordinatorWith(statePath, testWorkflow())
	require.NoError(t, errCoord)

	result := watchForTrigger(ctx, dir, coord, 0, "")
	assert.Equal(t, triggerNone, result)
}

func TestWatchForTriggerIgnoresGoalChanges(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	dir := t.TempDir()
	sgaiDir := filepath.Join(dir, ".sgai")
	require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
	goalPath := filepath.Join(dir, "GOAL.md")
	require.NoError(t, os.WriteFile(goalPath, []byte("# Goal version 1"), 0o644))

	statePath := filepath.Join(sgaiDir, "state.json")
	coord, errCoord := state.NewCoordinatorWith(statePath, testWorkflow())
	require.NoError(t, errCoord)

	pollStarted := make(chan struct{})
	var notifyPollStart sync.Once

	resultCh := make(chan triggerKind, 1)
	go func() {
		resultCh <- watchForTriggerWithAfter(ctx, dir, coord, 0, "", func(d time.Duration) <-chan time.Time {
			notifyPollStart.Do(func() {
				close(pollStarted)
			})
			return time.After(d)
		})
	}()

	select {
	case <-pollStarted:
	case <-time.After(time.Second):
		t.Fatal("watchForTrigger() did not enter its wait loop")
	}

	require.NoError(t, os.WriteFile(goalPath, []byte("# Goal version 2"), 0o644))

	goalChangeObservationWindow := continuousModePollInterval + time.Second
	select {
	case result := <-resultCh:
		t.Fatalf("watchForTrigger() returned %q after GOAL.md changed; want it to keep waiting until cancellation", result)
	case <-time.After(goalChangeObservationWindow):
	}

	cancel()

	select {
	case result := <-resultCh:
		assert.Equal(t, triggerNone, result)
	case <-time.After(time.Second):
		t.Fatal("watchForTrigger() did not return after cancellation")
	}
}

func TestWatchForTriggerSteeringMessage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	dir := t.TempDir()
	sgaiDir := filepath.Join(dir, ".sgai")
	require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "GOAL.md"), []byte("# Goal"), 0o644))

	statePath := filepath.Join(sgaiDir, "state.json")
	msg := testMessage()
	msg.ID = 1
	msg.FromAgent = "Human Partner"
	msg.ToAgent = "coordinator"
	msg.Body = "please fix"
	wf := testWorkflow()
	wf.Messages = []state.Message{msg}
	coord, errCoord := state.NewCoordinatorWith(statePath, wf)
	require.NoError(t, errCoord)

	result := watchForTrigger(ctx, dir, coord, 0, "")
	assert.Equal(t, triggerSteering, result)
}

func TestWatchForTriggerAutoTimer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	dir := t.TempDir()
	sgaiDir := filepath.Join(dir, ".sgai")
	require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "GOAL.md"), []byte("# Goal"), 0o644))

	statePath := filepath.Join(sgaiDir, "state.json")
	coord, errCoord := state.NewCoordinatorWith(statePath, testWorkflow())
	require.NoError(t, errCoord)

	result := watchForTrigger(ctx, dir, coord, 1*time.Millisecond, "")
	assert.Equal(t, triggerAuto, result)
}

func TestWatchForTriggerCronWithAutoFallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	dir := t.TempDir()
	sgaiDir := filepath.Join(dir, ".sgai")
	require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "GOAL.md"), []byte("# Goal"), 0o644))

	statePath := filepath.Join(sgaiDir, "state.json")
	coord, errCoord := state.NewCoordinatorWith(statePath, testWorkflow())
	require.NoError(t, errCoord)

	result := watchForTrigger(ctx, dir, coord, 1*time.Millisecond, "* * * * *")
	assert.Equal(t, triggerAuto, result)
}

func TestWatchForTriggerInvalidCronExpression(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	dir := t.TempDir()
	sgaiDir := filepath.Join(dir, ".sgai")
	require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "GOAL.md"), []byte("# Goal"), 0o644))

	statePath := filepath.Join(sgaiDir, "state.json")
	coord, errCoord := state.NewCoordinatorWith(statePath, testWorkflow())
	require.NoError(t, errCoord)

	result := watchForTrigger(ctx, dir, coord, 1*time.Millisecond, "invalid cron")
	assert.Equal(t, triggerAuto, result)
}

func TestTriggerKindConstants(t *testing.T) {
	assert.Equal(t, triggerNone, triggerKind(""))
	assert.Equal(t, triggerSteering, triggerKind("steering-message"))
	assert.Equal(t, triggerAuto, triggerKind("auto-timer"))
	assert.Equal(t, triggerCron, triggerKind("cron-schedule"))
}
