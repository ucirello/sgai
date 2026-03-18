package state

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNeedsHumanInput(t *testing.T) {
	tests := []struct {
		name     string
		workflow Workflow
		expected bool
	}{
		{
			name: "withQuestion",
			workflow: Workflow{
				MultiChoiceQuestion: &MultiChoiceQuestion{Questions: []QuestionItem{{Question: "test"}}},
			},
			expected: true,
		},
		{
			name: "withMessage",
			workflow: Workflow{
				HumanMessage: "Please respond",
			},
			expected: true,
		},
		{
			name: "withBoth",
			workflow: Workflow{
				MultiChoiceQuestion: &MultiChoiceQuestion{Questions: []QuestionItem{{Question: "test"}}},
				HumanMessage:        "Please respond",
			},
			expected: true,
		},
		{
			name:     "withoutQuestionOrMessage",
			workflow: Workflow{},
			expected: false,
		},
		{
			name: "workingWithQuestion",
			workflow: Workflow{
				Status:              StatusWorking,
				MultiChoiceQuestion: &MultiChoiceQuestion{Questions: []QuestionItem{{Question: "test"}}},
			},
			expected: true,
		},
		{
			name: "workingWithMessage",
			workflow: Workflow{
				Status:       StatusWorking,
				HumanMessage: "Please respond",
			},
			expected: true,
		},
		{
			name: "agentDoneWithQuestion",
			workflow: Workflow{
				Status:              StatusAgentDone,
				MultiChoiceQuestion: &MultiChoiceQuestion{Questions: []QuestionItem{{Question: "test"}}},
			},
			expected: true,
		},
		{
			name: "completeWithQuestion",
			workflow: Workflow{
				Status:              StatusComplete,
				MultiChoiceQuestion: &MultiChoiceQuestion{Questions: []QuestionItem{{Question: "test"}}},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.workflow.NeedsHumanInput()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTokenUsageAdd(t *testing.T) {
	tests := []struct {
		name     string
		t1       TokenUsage
		t2       TokenUsage
		expected TokenUsage
	}{
		{
			name: "addTwoUsages",
			t1: TokenUsage{
				Input:     50,
				Output:    30,
				Reasoning: 20,
			},
			t2: TokenUsage{
				Input:     100,
				Output:    60,
				Reasoning: 40,
			},
			expected: TokenUsage{
				Input:     150,
				Output:    90,
				Reasoning: 60,
			},
		},
		{
			name: "addZeroUsage",
			t1: TokenUsage{
				Input:     50,
				Output:    30,
				Reasoning: 20,
			},
			t2: TokenUsage{},
			expected: TokenUsage{
				Input:     50,
				Output:    30,
				Reasoning: 20,
			},
		},
		{
			name:     "addTwoZeroUsages",
			t1:       TokenUsage{},
			t2:       TokenUsage{},
			expected: TokenUsage{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.t1.Add(tt.t2)
			assert.Equal(t, tt.expected, tt.t1)
		})
	}
}

func TestQuestionStateIsMemoryOnly(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	question := &MultiChoiceQuestion{Questions: []QuestionItem{{Question: "test", Choices: []string{"yes", "no"}}}}
	wf := Workflow{HumanMessage: "Please respond", MultiChoiceQuestion: question}

	coord, err := NewCoordinatorWith(statePath, wf)
	require.NoError(t, err)

	snapshot := coord.State()
	assert.Equal(t, "Please respond", snapshot.HumanMessage)
	assert.NotNil(t, snapshot.MultiChoiceQuestion)

	loaded, err := load(statePath)
	require.NoError(t, err)
	assert.Empty(t, loaded.HumanMessage)
	assert.Nil(t, loaded.MultiChoiceQuestion)
}

func TestCurrentPromptTokenChangesAcrossQuestions(t *testing.T) {
	dir := t.TempDir()
	coord := NewCoordinatorEmpty(filepath.Join(dir, "state.json"))

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()
	firstErrCh := make(chan error, 1)
	go func() {
		_, err := coord.AskAndWait(firstCtx, nil, "same question")
		firstErrCh <- err
	}()

	firstToken := waitForCurrentPromptToken(t, coord)
	cancelFirst()
	require.ErrorIs(t, <-firstErrCh, context.Canceled)
	require.Eventually(t, func() bool {
		return coord.CurrentPromptToken() == ""
	}, time.Second, 10*time.Millisecond)

	secondCtx, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	secondAnswerCh := make(chan string, 1)
	secondErrCh := make(chan error, 1)
	go func() {
		answer, err := coord.AskAndWait(secondCtx, nil, "same question")
		if err != nil {
			secondErrCh <- err
			return
		}
		secondAnswerCh <- answer
	}()

	secondToken := waitForCurrentPromptToken(t, coord)
	assert.NotEqual(t, firstToken, secondToken)
	assert.False(t, coord.RespondIfCurrent(firstToken, "stale answer"))
	assert.True(t, coord.RespondIfCurrent(secondToken, "current answer"))
	assert.Equal(t, "current answer", <-secondAnswerCh)
	select {
	case err := <-secondErrCh:
		require.NoError(t, err)
	default:
	}
	require.Eventually(t, func() bool {
		return coord.CurrentPromptToken() == ""
	}, time.Second, 10*time.Millisecond)
}

func waitForCurrentPromptToken(t *testing.T, coord *Coordinator) string {
	t.Helper()
	require.Eventually(t, func() bool {
		return coord.CurrentPromptToken() != ""
	}, time.Second, 10*time.Millisecond)
	return coord.CurrentPromptToken()
}

func TestNewCoordinator(t *testing.T) {
	t.Run("loadExistingState", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state.json")

		wf := Workflow{
			Status:   StatusWorking,
			Task:     "test task",
			Progress: []ProgressEntry{{Description: "test progress"}},
		}
		require.NoError(t, save(statePath, wf))

		coord, err := NewCoordinator(statePath)
		require.NoError(t, err)
		require.NotNil(t, coord)

		state := coord.State()
		assert.Equal(t, StatusWorking, state.Status)
		assert.Equal(t, "test task", state.Task)
		assert.Len(t, state.Progress, 1)
	})

	t.Run("loadNonexistentState", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state.json")

		_, err := NewCoordinator(statePath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "loading coordinator state")
	})
}

func TestNewCoordinatorEmpty(t *testing.T) {
	t.Run("createEmptyCoordinator", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state.json")

		coord := NewCoordinatorEmpty(statePath)
		require.NotNil(t, coord)

		state := coord.State()
		assert.Equal(t, StatusWorking, state.Status)
		assert.Empty(t, state.Task)
		assert.Empty(t, state.Progress)
		assert.Empty(t, state.Messages)
	})
}

func TestNewCoordinatorWith(t *testing.T) {
	t.Run("createWithInitialWorkflow", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state.json")

		wf := Workflow{
			Status:   StatusWorking,
			Task:     "initial task",
			Progress: []ProgressEntry{{Description: "initial progress"}},
		}

		coord, err := NewCoordinatorWith(statePath, wf)
		require.NoError(t, err)
		require.NotNil(t, coord)

		state := coord.State()
		assert.Equal(t, StatusWorking, state.Status)
		assert.Equal(t, "initial task", state.Task)
		assert.Len(t, state.Progress, 1)

		loaded, err := load(statePath)
		require.NoError(t, err)
		assert.Equal(t, wf.Status, loaded.Status)
		assert.Equal(t, wf.Task, loaded.Task)
	})
}

func TestCoordinatorState(t *testing.T) {
	t.Run("returnStateSnapshot", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state.json")

		wf := Workflow{
			Status: StatusWorking,
			Task:   "test task",
		}

		coord, err := NewCoordinatorWith(statePath, wf)
		require.NoError(t, err)

		state := coord.State()
		assert.Equal(t, StatusWorking, state.Status)
		assert.Equal(t, "test task", state.Task)
	})
}

func TestCoordinatorUpdateState(t *testing.T) {
	t.Run("updateAndPersist", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state.json")

		wf := Workflow{
			Status: StatusWorking,
			Task:   "initial task",
		}

		coord, err := NewCoordinatorWith(statePath, wf)
		require.NoError(t, err)

		err = coord.UpdateState(func(wf *Workflow) {
			wf.Task = "updated task"
			wf.Progress = append(wf.Progress, ProgressEntry{Description: "new progress"})
		})
		require.NoError(t, err)

		state := coord.State()
		assert.Equal(t, "updated task", state.Task)
		assert.Len(t, state.Progress, 1)

		loaded, err := load(statePath)
		require.NoError(t, err)
		assert.Equal(t, "updated task", loaded.Task)
		assert.Len(t, loaded.Progress, 1)
	})

	t.Run("onUpdateCallback", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state.json")

		wf := Workflow{Status: StatusWorking}
		coord, err := NewCoordinatorWith(statePath, wf)
		require.NoError(t, err)

		callbackCalled := false
		coord.OnUpdate(func() {
			callbackCalled = true
		})

		err = coord.UpdateState(func(wf *Workflow) {
			wf.Task = "new task"
		})
		require.NoError(t, err)
		assert.True(t, callbackCalled)
	})
}
