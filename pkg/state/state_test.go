package state

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testMultiChoiceQuestion(choices ...string) *MultiChoiceQuestion {
	item := QuestionItem{
		Question:    "test",
		Choices:     choices,
		MultiSelect: false,
	}
	questionState := new(MultiChoiceQuestion)
	questionState.Questions = []QuestionItem{item}
	return questionState
}

func testProgressEntry(description string) ProgressEntry {
	var entry ProgressEntry
	entry.Description = description
	return entry
}

func testTokenUsage(input, output, reasoning int) TokenUsage {
	var usage TokenUsage
	usage.Input = input
	usage.Output = output
	usage.Reasoning = reasoning
	return usage
}

func testWorkflowWithReferenceFields() Workflow {
	wf := NewWorkflow()
	wf.Status = StatusWorking
	wf.Task = "stable task"
	wf.Progress = []ProgressEntry{{Timestamp: "2026-03-29T00:00:00Z", Agent: "coordinator", Description: "stable progress"}}
	wf.HumanMessage = "Please respond"
	wf.MultiChoiceQuestion = &MultiChoiceQuestion{
		Questions:  []QuestionItem{{Question: "stable question", Choices: []string{"yes", "no"}, MultiSelect: true}},
		IsWorkGate: true,
	}
	wf.Messages = []Message{{
		ID:        1,
		FromAgent: "coordinator",
		ToAgent:   "go-developer",
		Body:      "stable message",
		Read:      false,
		ReadAt:    "",
		ReadBy:    "",
		CreatedAt: "2026-03-29T00:00:01Z",
	}}
	wf.VisitCounts = map[string]int{"coordinator": 1}
	wf.CurrentAgent = "coordinator"
	wf.Todos = []TodoItem{{ID: "todo-1", Content: "stable todo", Status: "pending", Priority: "high"}}
	wf.ProjectTodos = []TodoItem{{ID: "project-todo-1", Content: "stable project todo", Status: "in_progress", Priority: "medium"}}
	wf.AgentSequence = []AgentSequenceEntry{{Agent: "coordinator", StartTime: "2026-03-29T00:00:02Z", IsCurrent: true}}
	wf.SessionID = "session-1"
	wf.Cost.TotalCost = 1.5
	var zeroDollars DollarBreakdown
	var zeroTokens TokenUsage
	wf.Cost.ByAgent = []AgentCost{{
		Agent:   "coordinator",
		Cost:    1.5,
		Dollars: zeroDollars,
		Tokens:  zeroTokens,
		Steps: []StepCost{{
			StepID:    "step-1",
			Agent:     "coordinator",
			Cost:      1.5,
			Dollars:   zeroDollars,
			Tokens:    zeroTokens,
			Timestamp: "2026-03-29T00:00:03Z",
		}},
	}}
	wf.InteractionMode = ModeBuilding
	wf.ModelStatuses = map[string]string{"coordinator:model": "model-working"}
	wf.CurrentModel = "coordinator:model"
	wf.Summary = "stable summary"
	wf.SummaryManual = true
	return wf
}

func testLargeWorkflowReplacement() Workflow {
	wf := testWorkflowWithReferenceFields()
	wf.Task = "replacement task"
	wf.Progress = append(wf.Progress, testProgressEntry("replacement progress"))
	wf.Summary = strings.Repeat("replacement summary ", 256)
	return wf
}

func mutateWorkflowReferenceFields(wf *Workflow) {
	wf.Progress[0].Description = "mutated progress"
	wf.Messages[0].Body = "mutated message"
	wf.VisitCounts["coordinator"] = 99
	wf.Todos[0].Content = "mutated todo"
	wf.ProjectTodos[0].Content = "mutated project todo"
	wf.AgentSequence[0].Agent = "builder"
	wf.Cost.ByAgent[0].Agent = "builder"
	wf.Cost.ByAgent[0].Steps[0].StepID = "step-2"
	wf.ModelStatuses["coordinator:model"] = "model-done"
	wf.MultiChoiceQuestion.IsWorkGate = false
	wf.MultiChoiceQuestion.Questions[0].Question = "mutated question"
	wf.MultiChoiceQuestion.Questions[0].Choices[0] = "maybe"
}

func TestCoordinatorReplaceStateFileSizeLimitHelper(t *testing.T) {
	statePath := os.Getenv("SGAI_STATE_REPLACE_HELPER_PATH")
	if statePath == "" {
		return
	}

	coord, err := NewCoordinator(statePath)
	require.NoError(t, err)

	signal.Ignore(syscall.SIGXFSZ)
	require.NoError(t, syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: 1024, Max: 1024}))

	replacement := testLargeWorkflowReplacement()
	err = coord.ReplaceState(&replacement)
	require.Error(t, err)
}

func TestSaveWorkflowStateFileModeHelper(t *testing.T) {
	statePath := os.Getenv("SGAI_STATE_MODE_HELPER_PATH")
	if statePath == "" {
		return
	}

	oldMask := syscall.Umask(0o027)
	defer syscall.Umask(oldMask)

	workflow := NewWorkflow()
	errSave := save(statePath, &workflow)
	require.NoError(t, errSave)

	info, errStat := os.Stat(statePath)
	require.NoError(t, errStat)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())
}

func channelClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func TestNeedsHumanInput(t *testing.T) {
	tests := []struct {
		name     string
		workflow Workflow
		expected bool
	}{
		{
			name: "withQuestion",
			workflow: func() Workflow {
				workflow := NewWorkflow()
				workflow.MultiChoiceQuestion = testMultiChoiceQuestion()
				return workflow
			}(),
			expected: true,
		},
		{
			name: "withMessage",
			workflow: func() Workflow {
				workflow := NewWorkflow()
				workflow.HumanMessage = "Please respond"
				return workflow
			}(),
			expected: true,
		},
		{
			name: "withBoth",
			workflow: func() Workflow {
				workflow := NewWorkflow()
				workflow.MultiChoiceQuestion = testMultiChoiceQuestion()
				workflow.HumanMessage = "Please respond"
				return workflow
			}(),
			expected: true,
		},
		{
			name:     "withoutQuestionOrMessage",
			workflow: NewWorkflow(),
			expected: false,
		},
		{
			name: "workingWithQuestion",
			workflow: func() Workflow {
				workflow := NewWorkflow()
				workflow.Status = StatusWorking
				workflow.MultiChoiceQuestion = testMultiChoiceQuestion()
				return workflow
			}(),
			expected: true,
		},
		{
			name: "workingWithMessage",
			workflow: func() Workflow {
				workflow := NewWorkflow()
				workflow.Status = StatusWorking
				workflow.HumanMessage = "Please respond"
				return workflow
			}(),
			expected: true,
		},
		{
			name: "agentDoneWithQuestion",
			workflow: func() Workflow {
				workflow := NewWorkflow()
				workflow.Status = StatusAgentDone
				workflow.MultiChoiceQuestion = testMultiChoiceQuestion()
				return workflow
			}(),
			expected: true,
		},
		{
			name: "completeWithQuestion",
			workflow: func() Workflow {
				workflow := NewWorkflow()
				workflow.Status = StatusComplete
				workflow.MultiChoiceQuestion = testMultiChoiceQuestion()
				return workflow
			}(),
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
	var zeroUsage TokenUsage

	tests := []struct {
		name     string
		t1       TokenUsage
		t2       TokenUsage
		expected TokenUsage
	}{
		{
			name:     "addTwoUsages",
			t1:       testTokenUsage(50, 30, 20),
			t2:       testTokenUsage(100, 60, 40),
			expected: testTokenUsage(150, 90, 60),
		},
		{
			name:     "addZeroUsage",
			t1:       testTokenUsage(50, 30, 20),
			t2:       zeroUsage,
			expected: testTokenUsage(50, 30, 20),
		},
		{
			name:     "addTwoZeroUsages",
			t1:       zeroUsage,
			t2:       zeroUsage,
			expected: zeroUsage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.t1.Add(tt.t2)
			assert.Equal(t, tt.expected, tt.t1)
		})
	}
}

func TestNewWorkflow(t *testing.T) {
	t.Run("initializesExpectedCollections", func(t *testing.T) {
		wf := NewWorkflow()

		assert.Equal(t, []ProgressEntry{}, wf.Progress)
		assert.Equal(t, []Message{}, wf.Messages)
		assert.Equal(t, map[string]int{}, wf.VisitCounts)
		assert.Equal(t, []TodoItem{}, wf.Todos)
		assert.Equal(t, []TodoItem{}, wf.ProjectTodos)
		assert.Equal(t, []AgentSequenceEntry{}, wf.AgentSequence)
		assert.Equal(t, []AgentCost{}, wf.Cost.ByAgent)
		assert.Equal(t, map[string]string{}, wf.ModelStatuses)
		assert.Zero(t, wf.Cost.Dollars)
		assert.Zero(t, wf.Cost.TotalTokens)
		assert.Empty(t, wf.Status)
		assert.Empty(t, wf.InteractionMode)
	})

	t.Run("returnsIndependentCollections", func(t *testing.T) {
		first := NewWorkflow()
		second := NewWorkflow()
		var firstMessage Message
		firstMessage.ID = 1
		var todo TodoItem
		todo.ID = "1"
		todo.Content = "todo"
		var projectTodo TodoItem
		projectTodo.ID = "2"
		projectTodo.Content = "project todo"
		var sequenceEntry AgentSequenceEntry
		sequenceEntry.Agent = "coordinator"
		var agentCost AgentCost
		agentCost.Agent = "coordinator"

		first.Progress = append(first.Progress, testProgressEntry("first progress"))
		first.Messages = append(first.Messages, firstMessage)
		first.VisitCounts["coordinator"] = 1
		first.Todos = append(first.Todos, todo)
		first.ProjectTodos = append(first.ProjectTodos, projectTodo)
		first.AgentSequence = append(first.AgentSequence, sequenceEntry)
		first.Cost.ByAgent = append(first.Cost.ByAgent, agentCost)
		first.ModelStatuses["coordinator:model"] = StatusWorking

		assert.Empty(t, second.Progress)
		assert.Empty(t, second.Messages)
		assert.Empty(t, second.VisitCounts)
		assert.Empty(t, second.Todos)
		assert.Empty(t, second.ProjectTodos)
		assert.Empty(t, second.AgentSequence)
		assert.Empty(t, second.Cost.ByAgent)
		assert.Empty(t, second.ModelStatuses)
	})
}

func TestQuestionStateIsMemoryOnly(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	question := testMultiChoiceQuestion("yes", "no")
	wf := NewWorkflow()
	wf.HumanMessage = "Please respond"
	wf.HumanInputAgent = "builder"
	wf.MultiChoiceQuestion = question

	coord, err := NewCoordinatorWith(statePath, wf)
	require.NoError(t, err)

	snapshot := coord.State()
	assert.Equal(t, "Please respond", snapshot.HumanMessage)
	assert.Equal(t, "builder", snapshot.HumanInputAgent)
	assert.NotNil(t, snapshot.MultiChoiceQuestion)

	loaded, err := load(statePath)
	require.NoError(t, err)
	assert.Empty(t, loaded.HumanMessage)
	assert.Empty(t, loaded.HumanInputAgent)
	assert.Nil(t, loaded.MultiChoiceQuestion)
}

func TestCurrentPromptTokenChangesAcrossQuestions(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		coord := NewCoordinatorEmpty(filepath.Join(dir, "state.json"))

		firstCtx, cancelFirst := context.WithCancel(context.Background())
		defer cancelFirst()
		errFirstCh := make(chan error, 1)
		go func() {
			_, err := coord.AskAndWait(firstCtx, nil, "same question", "builder")
			errFirstCh <- err
		}()

		synctest.Wait()
		firstToken := coord.CurrentPromptToken()
		require.NotEmpty(t, firstToken)

		cancelFirst()
		synctest.Wait()
		require.ErrorIs(t, <-errFirstCh, context.Canceled)
		assert.Empty(t, coord.CurrentPromptToken())

		secondCtx, cancelSecond := context.WithCancel(context.Background())
		defer cancelSecond()
		secondAnswerCh := make(chan string, 1)
		errSecondCh := make(chan error, 1)
		go func() {
			answer, err := coord.AskAndWait(secondCtx, nil, "same question", "builder")
			if err != nil {
				errSecondCh <- err
				return
			}
			secondAnswerCh <- answer
		}()

		synctest.Wait()
		secondToken := coord.CurrentPromptToken()
		require.NotEmpty(t, secondToken)
		assert.NotEqual(t, firstToken, secondToken)
		assert.False(t, coord.RespondIfCurrent(firstToken, "stale answer"))
		require.True(t, coord.RespondIfCurrent(secondToken, "current answer"))

		synctest.Wait()
		select {
		case answer := <-secondAnswerCh:
			require.Equal(t, "current answer", answer)
		case err := <-errSecondCh:
			require.NoError(t, err)
		default:
			t.Fatal("second prompt did not complete")
		}
		assert.Empty(t, coord.CurrentPromptToken())
	})
}

func TestRespondCurrentPromptLockedRejectsPromptAfterPromotion(t *testing.T) {
	dir := t.TempDir()
	coord := NewCoordinatorEmpty(filepath.Join(dir, "state.json"))

	firstPrompt, isCurrent := coord.enqueuePendingPrompt(nil, "first message", "go-developer")
	require.True(t, isCurrent)
	secondPrompt, isCurrent := coord.enqueuePendingPrompt(nil, "second message", "react-developer")
	require.False(t, isCurrent)

	nextPrompt, updateVisiblePrompt := coord.removePendingPrompt(firstPrompt)
	require.True(t, updateVisiblePrompt)
	require.Same(t, secondPrompt, nextPrompt)

	coord.mu.Lock()
	staleAccepted := coord.respondCurrentPromptLocked(firstPrompt, firstPrompt.promptToken, "stale answer")
	coord.mu.Unlock()
	assert.False(t, staleAccepted)

	select {
	case answer := <-firstPrompt.responseCh:
		t.Fatalf("expected promoted prompt to reject stale answer, got %q", answer)
	default:
	}

	coord.mu.Lock()
	currentAccepted := coord.respondCurrentPromptLocked(secondPrompt, secondPrompt.promptToken, "current answer")
	coord.mu.Unlock()
	require.True(t, currentAccepted)

	select {
	case answer := <-secondPrompt.responseCh:
		require.Equal(t, "current answer", answer)
	default:
		t.Fatal("expected current prompt to accept response")
	}
}

func TestNewCoordinator(t *testing.T) {
	t.Run("loadExistingState", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state.json")

		wf := NewWorkflow()
		wf.Status = StatusWorking
		wf.Task = "test task"
		wf.Progress = []ProgressEntry{testProgressEntry("test progress")}
		require.NoError(t, save(statePath, &wf))

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

		wf := NewWorkflow()
		wf.Status = StatusWorking
		wf.Task = "initial task"
		wf.Progress = []ProgressEntry{testProgressEntry("initial progress")}

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

	t.Run("detachesCallerOwnedReferenceFields", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state.json")

		wf := testWorkflowWithReferenceFields()
		coord, err := NewCoordinatorWith(statePath, wf)
		require.NoError(t, err)

		mutateWorkflowReferenceFields(&wf)

		assert.Equal(t, testWorkflowWithReferenceFields(), coord.State())
	})
}

func TestCoordinatorState(t *testing.T) {
	t.Run("returnStateSnapshot", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state.json")

		wf := NewWorkflow()
		wf.Status = StatusWorking
		wf.Task = "test task"

		coord, err := NewCoordinatorWith(statePath, wf)
		require.NoError(t, err)

		state := coord.State()
		assert.Equal(t, StatusWorking, state.Status)
		assert.Equal(t, "test task", state.Task)
	})

	t.Run("returnsDetachedReferenceFields", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state.json")

		wf := testWorkflowWithReferenceFields()
		coord, err := NewCoordinatorWith(statePath, wf)
		require.NoError(t, err)

		state := coord.State()
		mutateWorkflowReferenceFields(&state)

		assert.Equal(t, testWorkflowWithReferenceFields(), coord.State())
	})
}

func TestCoordinatorUpdateState(t *testing.T) {
	t.Run("updateAndPersist", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state.json")

		wf := NewWorkflow()
		wf.Status = StatusWorking
		wf.Task = "initial task"

		coord, err := NewCoordinatorWith(statePath, wf)
		require.NoError(t, err)

		err = coord.UpdateState(func(wf *Workflow) {
			wf.Task = "updated task"
			wf.Progress = append(wf.Progress, testProgressEntry("new progress"))
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

		wf := NewWorkflow()
		wf.Status = StatusWorking
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

	t.Run("concurrentUpdatesPersistNewestState", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state.json")

		wf := NewWorkflow()
		wf.Status = StatusWorking
		coord, err := NewCoordinatorWith(statePath, wf)
		require.NoError(t, err)

		firstSaveStarted := make(chan struct{})
		allowFirstSave := make(chan struct{})
		errCh := make(chan error, 2)
		var wg sync.WaitGroup
		realSave := coord.saveWorkflow
		coord.saveWorkflow = func(path string, wf *Workflow) error {
			if wf.Task == "older task" {
				close(firstSaveStarted)
				<-allowFirstSave
			}
			return realSave(path, wf)
		}

		wg.Add(2)

		go func() {
			defer wg.Done()
			errCh <- coord.UpdateState(func(wf *Workflow) {
				wf.Task = "older task"
				wf.Progress = []ProgressEntry{testProgressEntry("older progress")}
			})
		}()

		require.Eventually(t, func() bool {
			return channelClosed(firstSaveStarted)
		}, time.Second, 10*time.Millisecond)

		locked := coord.mu.TryLock()
		if locked {
			coord.mu.Unlock()
		}
		assert.False(t, locked)

		go func() {
			defer wg.Done()
			errCh <- coord.UpdateState(func(wf *Workflow) {
				wf.Task = "newer task"
				wf.Progress = []ProgressEntry{testProgressEntry("newer progress")}
			})
		}()

		close(allowFirstSave)

		wg.Wait()
		close(errCh)
		for errUpdate := range errCh {
			require.NoError(t, errUpdate)
		}

		assert.Equal(t, "newer task", coord.State().Task)

		loaded, err := load(statePath)
		require.NoError(t, err)
		assert.Equal(t, "newer task", loaded.Task)
		assert.Len(t, loaded.Progress, 1)
		assert.Equal(t, "newer progress", loaded.Progress[0].Description)
	})
}

func TestCoordinatorReplaceState(t *testing.T) {
	t.Run("earlySaveFailureKeepsPreviousState", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state.json")

		initial := testWorkflowWithReferenceFields()
		coord, err := NewCoordinatorWith(statePath, initial)
		require.NoError(t, err)

		loadedBefore, errLoad := load(statePath)
		require.NoError(t, errLoad)

		replacement := testWorkflowWithReferenceFields()
		replacement.Task = "replacement task"
		replacement.Progress = append(replacement.Progress, testProgressEntry("replacement progress"))

		errSave := errors.New("save failed")
		coord.saveWorkflow = func(_ string, _ *Workflow) error {
			return errSave
		}

		err = coord.ReplaceState(&replacement)
		require.ErrorIs(t, err, errSave)

		assert.Equal(t, initial, coord.State())

		loaded, errLoad := load(statePath)
		require.NoError(t, errLoad)
		assert.Equal(t, loadedBefore, loaded)
	})

	t.Run("realSaveFailureKeepsPreviousStateOnDisk", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state.json")

		initial := testWorkflowWithReferenceFields()
		require.NoError(t, save(statePath, &initial))

		loadedBefore, errLoad := load(statePath)
		require.NoError(t, errLoad)

		cmd := exec.Command(os.Args[0], "-test.run=^TestCoordinatorReplaceStateFileSizeLimitHelper$")
		cmd.Env = append(os.Environ(), "SGAI_STATE_REPLACE_HELPER_PATH="+statePath)

		output, errRun := cmd.CombinedOutput()
		require.NoError(t, errRun, string(output))

		loaded, errLoad := load(statePath)
		require.NoError(t, errLoad)
		assert.Equal(t, loadedBefore, loaded)
	})
}

func TestSaveWorkflowStateFileModes(t *testing.T) {
	t.Run("newFileRespectsUmask", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state.json")

		cmd := exec.Command(os.Args[0], "-test.run=^TestSaveWorkflowStateFileModeHelper$")
		cmd.Env = append(os.Environ(), "SGAI_STATE_MODE_HELPER_PATH="+statePath)

		output, errRun := cmd.CombinedOutput()
		require.NoError(t, errRun, string(output))

		info, errStat := os.Stat(statePath)
		require.NoError(t, errStat)
		assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())
	})

	t.Run("existingFilePreservesMode", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state.json")

		errWrite := os.WriteFile(statePath, []byte("{}"), 0o600)
		require.NoError(t, errWrite)

		workflow := NewWorkflow()
		errSave := save(statePath, &workflow)
		require.NoError(t, errSave)

		info, errStat := os.Stat(statePath)
		require.NoError(t, errStat)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	})
}
