package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ucirello/sgai/pkg/state"
)

func newTestDagNode(name string) *dagNode {
	return &dagNode{
		Name:         name,
		Predecessors: nil,
		Successors:   nil,
	}
}

func newTestDag(nodeNames ...string) *dag {
	nodes := make(map[string]*dagNode, len(nodeNames))
	for _, nodeName := range nodeNames {
		nodes[nodeName] = newTestDagNode(nodeName)
	}
	return &dag{
		Nodes:      nodes,
		EntryNodes: nil,
	}
}

func newTestGoalMetadata() GoalMetadata {
	return GoalMetadata{
		Title:                "",
		Flow:                 "",
		Models:               nil,
		Alias:                nil,
		CompletionGateScript: "",
		ContinuousModePrompt: "",
		ContinuousModeAuto:   "",
		ContinuousModeCron:   "",
		Retrospective:        "",
	}
}

//nolint:ireturn // Generic test fixture helper returns the concrete type selected by each test.
func updated[T any](value T, update func(*T)) T {
	update(&value)
	return value
}

func updatedPtr[T any](value T, update func(*T)) *T {
	update(&value)
	return &value
}

func newTestTokenUsage() state.TokenUsage {
	return state.TokenUsage{
		Input:      0,
		Output:     0,
		Reasoning:  0,
		CacheRead:  0,
		CacheWrite: 0,
	}
}

func newTestDollarBreakdown() state.DollarBreakdown {
	return state.DollarBreakdown{
		Input:      0,
		Output:     0,
		Reasoning:  0,
		CacheRead:  0,
		CacheWrite: 0,
		Total:      0,
	}
}

func newTestSessionCost() state.SessionCost {
	return state.SessionCost{
		TotalCost:   0,
		Dollars:     newTestDollarBreakdown(),
		TotalTokens: newTestTokenUsage(),
		ByAgent:     nil,
	}
}

func newTestWorkflow() state.Workflow {
	return state.Workflow{
		Status:              "",
		Task:                "",
		Progress:            nil,
		HumanMessage:        "",
		MultiChoiceQuestion: nil,
		Messages:            nil,
		VisitCounts:         nil,
		CurrentAgent:        "",
		Todos:               nil,
		ProjectTodos:        nil,
		AgentSequence:       nil,
		SessionID:           "",
		Cost:                newTestSessionCost(),
		InteractionMode:     "",
		ModelStatuses:       nil,
		CurrentModel:        "",
		Summary:             "",
		SummaryManual:       false,
	}
}

func newTestMultiChoiceQuestion() state.MultiChoiceQuestion {
	return state.MultiChoiceQuestion{
		Questions:  nil,
		IsWorkGate: false,
	}
}

func newTestMessage() state.Message {
	return state.Message{
		ID:        0,
		FromAgent: "",
		ToAgent:   "",
		Body:      "",
		Read:      false,
		ReadAt:    "",
		ReadBy:    "",
		CreatedAt: "",
	}
}

func newTestSession() *session {
	return &session{
		mu:           sync.Mutex{},
		cancel:       nil,
		running:      false,
		outputLog:    nil,
		mcpCloseOnce: sync.Once{},
		mcpCloseFn:   nil,
		coord:        nil,
	}
}

func newTestServerPaths() serverPaths {
	return serverPaths{
		pinnedConfigDir:   "",
		externalConfigDir: "",
	}
}

func newTestEventsProgressDisplay() eventsProgressDisplay {
	return eventsProgressDisplay{
		Timestamp:       "",
		FormattedTime:   "",
		Agent:           "",
		Description:     "",
		ShowDateDivider: false,
		DateDivider:     "",
	}
}

func newTestWorkspaceGroup() workspaceGroup {
	return workspaceGroup{
		Root:  newTestWorkspaceInfo(),
		Forks: nil,
	}
}

func newTestTodoItem() state.TodoItem {
	return state.TodoItem{
		ID:       "",
		Content:  "",
		Status:   "",
		Priority: "",
	}
}

func newTestAgentSequenceEntry() state.AgentSequenceEntry {
	return state.AgentSequenceEntry{
		Agent:     "",
		StartTime: "",
		IsCurrent: false,
	}
}

func newTestQuestionItem() state.QuestionItem {
	return state.QuestionItem{
		Question:    "",
		Choices:     nil,
		MultiSelect: false,
	}
}

func newTestActionConfig() actionConfig {
	return actionConfig{
		Name:        "",
		Model:       "",
		Prompt:      "",
		Script:      "",
		Description: "",
	}
}

func newTestProjectConfig() projectConfig {
	return projectConfig{
		DefaultModel: "",
		MCP:          nil,
		Editor:       "",
		Actions:      nil,
	}
}

func newTestAPIActionEntry() apiActionEntry {
	return apiActionEntry{
		Name:            "",
		Model:           "",
		Prompt:          "",
		Script:          "",
		Description:     "",
		Kind:            "",
		Variables:       nil,
		ValidationError: "",
	}
}

func newTestAPIRespondRequest() apiRespondRequest {
	return apiRespondRequest{
		PromptToken:     "",
		Answer:          "",
		SelectedChoices: nil,
	}
}

func newTestMultiModelConfig() multiModelConfig {
	return multiModelConfig{
		dir:              "",
		goalPath:         "",
		agent:            "",
		flowDag:          nil,
		statePath:        "",
		coord:            nil,
		retrospectiveDir: "",
		longestNameLen:   0,
		paddedsgai:       "",
		mcpURL:           "",
		logWriter:        nil,
		stdoutLog:        nil,
		stderrLog:        nil,
	}
}

func newTestWorkspaceInfo() workspaceInfo {
	return workspaceInfo{
		Directory:    "",
		DirName:      "",
		Kind:         "",
		IsRoot:       false,
		Running:      false,
		NeedsInput:   false,
		InProgress:   false,
		Pinned:       false,
		HasWorkspace: false,
		External:     false,
	}
}

func newTestPartTokens() partTokens {
	return partTokens{
		Input:     0,
		Output:    0,
		Reasoning: 0,
		Cache: cacheStats{
			Read:  0,
			Write: 0,
		},
	}
}

func newTestToolState() toolState {
	return toolState{
		Status: "",
		Input:  nil,
		Title:  "",
		Output: "",
		Error:  "",
	}
}

func newTestPart() part {
	return part{
		Type:   "",
		Text:   "",
		Tool:   "",
		State:  nil,
		Cost:   0,
		Tokens: newTestPartTokens(),
	}
}

func newTestStreamEvent() streamEvent {
	return streamEvent{
		Type:      "",
		Timestamp: 0,
		SessionID: "",
		Part:      newTestPart(),
	}
}

func newTestJSONPrettyWriter() *jsonPrettyWriter {
	return &jsonPrettyWriter{
		prefix:       "",
		w:            nil,
		buf:          nil,
		currentText:  strings.Builder{},
		sessionID:    "",
		coord:        nil,
		currentAgent: "",
		stepCounter:  0,
		now:          testLogNow,
	}
}

func newBufferedTestJSONPrettyWriter(w io.Writer, prefix string) *jsonPrettyWriter {
	writer := newTestJSONPrettyWriter()
	writer.w = w
	writer.prefix = prefix
	writer.now = testLogNow
	return writer
}

func TestFormatLogTimeOutput(t *testing.T) {
	got := formatLogTime(time.Date(2026, time.March, 28, 5, 4, 3, 0, time.UTC))
	assert.Equal(t, "[05:04:03]", got)
}

func testLogNow() time.Time {
	return time.Date(2026, time.March, 28, 12, 34, 56, 0, time.UTC)
}

func testLogTimestamp() string {
	return formatLogTime(testLogNow())
}

func timestampedTestOutput(prefix string, lines ...string) string {
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(testLogTimestamp())
		b.WriteString(prefix)
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func TestExtractModelFromArgs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"withModel", []string{"run", "--model", "claude-opus-4", "--agent", "build"}, "claude-opus-4"},
		{"withModelAndVariant", []string{"run", "--model", "claude-opus-4", "--variant", "max", "--agent", "build"}, "claude-opus-4 (max)"},
		{"noModel", []string{"run", "--agent", "build"}, ""},
		{"modelAtEnd", []string{"--model"}, ""},
		{"empty", []string{}, ""},
		{"modelFirst", []string{"--model", "gpt-4"}, "gpt-4"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractModelFromArgs(tc.args)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestEnsureImplicitAgentModel(t *testing.T) {
	t.Run("addsModelFromCoordinator", func(t *testing.T) {
		flowDag := newTestDag("retrospective")
		metadata := newTestGoalMetadata()
		metadata.Models = map[string]any{"coordinator": "claude-opus-4"}

		ensureImplicitAgentModel(flowDag, &metadata, "retrospective")

		assert.Equal(t, "claude-opus-4", metadata.Models["retrospective"])
	})

	t.Run("doesNotOverrideExisting", func(t *testing.T) {
		flowDag := newTestDag("retrospective")
		metadata := newTestGoalMetadata()
		metadata.Models = map[string]any{
			"coordinator":   "claude-opus-4",
			"retrospective": "gpt-4",
		}

		ensureImplicitAgentModel(flowDag, &metadata, "retrospective")

		assert.Equal(t, "gpt-4", metadata.Models["retrospective"])
	})

	t.Run("agentNotInDag", func(t *testing.T) {
		flowDag := newTestDag()
		metadata := newTestGoalMetadata()
		metadata.Models = map[string]any{"coordinator": "claude-opus-4"}

		ensureImplicitAgentModel(flowDag, &metadata, "retrospective")

		_, exists := metadata.Models["retrospective"]
		assert.False(t, exists)
	})

	t.Run("noCoordinatorModel", func(t *testing.T) {
		flowDag := newTestDag("retrospective")
		metadata := newTestGoalMetadata()
		metadata.Models = map[string]any{}

		ensureImplicitAgentModel(flowDag, &metadata, "retrospective")

		_, exists := metadata.Models["retrospective"]
		assert.False(t, exists)
	})

	t.Run("nilModelsMap", func(t *testing.T) {
		flowDag := newTestDag("retrospective")
		metadata := newTestGoalMetadata()

		ensureImplicitAgentModel(flowDag, &metadata, "retrospective")

		assert.NotNil(t, metadata.Models)
	})
}

func TestAddRetrospectiveRedirectMessage(t *testing.T) {
	wfState := newTestWorkflow()
	message := newTestMessage()
	message.ID = 1
	message.FromAgent = "dev"
	message.ToAgent = "coordinator"
	message.Body = "done"
	message.Read = true
	wfState.Messages = []state.Message{message}

	addRetrospectiveRedirectMessage(&wfState, "coordinator")

	require.Len(t, wfState.Messages, 2)
	msg := wfState.Messages[1]
	assert.Equal(t, 2, msg.ID)
	assert.Equal(t, "coordinator", msg.FromAgent)
	assert.Equal(t, "retrospective", msg.ToAgent)
	assert.Contains(t, msg.Body, "retrospective analysis")
	assert.False(t, msg.Read)
}

func TestAddProjectCriticCouncilRedirectMessage(t *testing.T) {
	wfState := newTestWorkflow()
	message := newTestMessage()
	message.ID = 1
	message.FromAgent = "dev"
	message.ToAgent = "coordinator"
	message.Body = "done"
	message.Read = true
	wfState.Messages = []state.Message{message}

	addProjectCriticCouncilRedirectMessages(&wfState, "", "coordinator", newTestGoalMetadata().Models)

	require.Len(t, wfState.Messages, 2)
	msg := wfState.Messages[1]
	assert.Equal(t, 2, msg.ID)
	assert.Equal(t, "coordinator", msg.FromAgent)
	assert.Equal(t, "project-critic-council", msg.ToAgent)
	assert.Contains(t, msg.Body, "final completion verdict")
	assert.False(t, msg.Read)
}

func TestAddProjectCriticCouncilRedirectMessageFansOutToCouncilModels(t *testing.T) {
	wfState := newTestWorkflow()
	metadata := newTestGoalMetadata()
	metadata.Models = map[string]any{
		"project-critic-council": []any{"model-a", "model-b"},
	}

	addProjectCriticCouncilRedirectMessages(&wfState, t.TempDir(), "coordinator", metadata.Models)

	require.Len(t, wfState.Messages, 2)
	assert.Equal(t, "project-critic-council:model-a", wfState.Messages[0].ToAgent)
	assert.Equal(t, "project-critic-council:model-b", wfState.Messages[1].ToAgent)
}

func TestHasUnreadMessageForAgent(t *testing.T) {
	assert.False(t, hasUnreadMessageForAgent(nil, "retrospective"))
	readMessage := newTestMessage()
	readMessage.ToAgent = "retrospective"
	readMessage.Read = true
	unreadMessage := newTestMessage()
	unreadMessage.ToAgent = "project-critic-council:model1"
	unreadMessage.Read = false
	assert.False(t, hasUnreadMessageForAgent([]state.Message{readMessage}, "retrospective"))
	assert.True(t, hasUnreadMessageForAgent([]state.Message{unreadMessage}, "project-critic-council"))
}

func TestBlockCompletionOnPendingTodos(t *testing.T) {
	t.Run("noPendingTodos", func(t *testing.T) {
		dir := t.TempDir()
		coord, err := state.NewCoordinatorWith(filepath.Join(dir, "state.json"), newTestWorkflow())
		require.NoError(t, err)

		cfg := newTestMultiModelConfig()
		cfg.coord = coord
		wfState := newTestWorkflow()
		wfState.Status = state.StatusComplete
		doneTodo := newTestTodoItem()
		doneTodo.Content = "done"
		doneTodo.Status = "completed"
		cancelledTodo := newTestTodoItem()
		cancelledTodo.Content = "cancelled"
		cancelledTodo.Status = "cancelled"
		wfState.Todos = []state.TodoItem{doneTodo, cancelledTodo}

		result := blockCompletionOnPendingTodos(&cfg, &wfState)
		assert.Nil(t, result)
	})

	t.Run("hasPendingTodos", func(t *testing.T) {
		dir := t.TempDir()
		coord, err := state.NewCoordinatorWith(filepath.Join(dir, "state.json"), newTestWorkflow())
		require.NoError(t, err)

		cfg := newTestMultiModelConfig()
		cfg.coord = coord
		cfg.agent = "go-developer"
		cfg.paddedsgai = "sgai"
		newState := newTestWorkflow()
		newState.Status = state.StatusComplete
		doneTodo := newTestTodoItem()
		doneTodo.Content = "done"
		doneTodo.Status = "completed"
		pendingTodo := newTestTodoItem()
		pendingTodo.Content = "pending task"
		pendingTodo.Status = "pending"
		newState.Todos = []state.TodoItem{doneTodo, pendingTodo}

		result := blockCompletionOnPendingTodos(&cfg, &newState)
		require.NotNil(t, result)
		assert.Equal(t, state.StatusWorking, result.Status)
	})

	t.Run("coordinatorUsesPendingProjectTodosFromNewState", func(t *testing.T) {
		dir := t.TempDir()
		coord, err := state.NewCoordinatorWith(filepath.Join(dir, "state.json"), newTestWorkflow())
		require.NoError(t, err)

		cfg := newTestMultiModelConfig()
		cfg.coord = coord
		cfg.agent = "coordinator"
		cfg.paddedsgai = "sgai"
		newState := updated(newTestWorkflow(), func(workflow *state.Workflow) {
			workflow.Status = state.StatusComplete
			workflow.ProjectTodos = []state.TodoItem{updated(newTestTodoItem(), func(todo *state.TodoItem) {
				todo.Content = "project task"
				todo.Status = "pending"
				todo.Priority = "high"
			})}
		})
		result := blockCompletionOnPendingTodos(&cfg, &newState)
		require.NotNil(t, result)
		assert.Equal(t, state.StatusWorking, result.Status)
	})
}

func TestCopyCompletionArtifactsToRetrospective(t *testing.T) {
	t.Run("noRetrospectiveDir", func(t *testing.T) {
		cfg := newTestMultiModelConfig()
		require.NoError(t, copyCompletionArtifactsToRetrospective(&cfg))
	})

	t.Run("withRetrospectiveDir", func(t *testing.T) {
		dir := t.TempDir()
		retrospectiveDir := filepath.Join(dir, "retrospective")
		require.NoError(t, os.MkdirAll(retrospectiveDir, 0o755))

		goalPath := filepath.Join(dir, "GOAL.md")
		require.NoError(t, os.WriteFile(goalPath, []byte("# Goal"), 0o644))

		sgaiDir := filepath.Join(dir, ".sgai")
		require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
		pmPath := filepath.Join(sgaiDir, "PROJECT_MANAGEMENT.md")
		require.NoError(t, os.WriteFile(pmPath, []byte("# PM"), 0o644))

		cfg := newTestMultiModelConfig()
		cfg.dir = dir
		cfg.goalPath = goalPath
		cfg.retrospectiveDir = retrospectiveDir

		require.NoError(t, copyCompletionArtifactsToRetrospective(&cfg))

		goalCopy, errGoal := os.ReadFile(filepath.Join(retrospectiveDir, "GOAL.md"))
		require.NoError(t, errGoal)
		assert.Equal(t, "# Goal", string(goalCopy))

		pmCopy, errPM := os.ReadFile(filepath.Join(retrospectiveDir, "PROJECT_MANAGEMENT.md"))
		require.NoError(t, errPM)
		assert.Equal(t, "# PM", string(pmCopy))
	})
}

func TestTryReloadGoalMetadata(t *testing.T) {
	t.Run("fileNotExists", func(t *testing.T) {
		dir := t.TempDir()
		goalPath := filepath.Join(dir, "GOAL.md")
		current := newTestGoalMetadata()
		current.Flow = "coordinator -> dev"

		result, err := tryReloadGoalMetadata(goalPath, &current, newTestDag())
		require.NoError(t, err)
		assert.Equal(t, "coordinator -> dev", result.Flow)
	})

	t.Run("validFrontmatter", func(t *testing.T) {
		dir := t.TempDir()
		goalPath := filepath.Join(dir, "GOAL.md")
		content := "---\nflow: coordinator -> dev\nretrospective: \"true\"\n---\n# Goal"
		require.NoError(t, os.WriteFile(goalPath, []byte(content), 0o644))

		metadata := newTestGoalMetadata()
		result, err := tryReloadGoalMetadata(goalPath, &metadata, newTestDag())
		require.NoError(t, err)
		assert.Equal(t, "coordinator -> dev", result.Flow)
	})

	t.Run("invalidFrontmatter", func(t *testing.T) {
		dir := t.TempDir()
		goalPath := filepath.Join(dir, "GOAL.md")
		content := "---\n  bad yaml: [unclosed\n---\n# Goal"
		require.NoError(t, os.WriteFile(goalPath, []byte(content), 0o644))

		metadata := newTestGoalMetadata()
		_, err := tryReloadGoalMetadata(goalPath, &metadata, newTestDag())
		require.Error(t, err)
	})
}

func TestBlockCompletionOnRetrospective(t *testing.T) {
	t.Run("retrospectiveDisabled", func(t *testing.T) {
		cfg := newTestMultiModelConfig()
		cfg.flowDag = newTestDag("retrospective")
		metadata := newTestGoalMetadata()
		metadata.Retrospective = "false"
		workflow := newTestWorkflow()
		result := blockCompletionOnRetrospective(&cfg, &workflow, &metadata)
		assert.Nil(t, result)
	})

	t.Run("noRetrospectiveInDag", func(t *testing.T) {
		cfg := newTestMultiModelConfig()
		cfg.flowDag = newTestDag()
		metadata := newTestGoalMetadata()
		metadata.Retrospective = "true"
		workflow := newTestWorkflow()
		result := blockCompletionOnRetrospective(&cfg, &workflow, &metadata)
		assert.Nil(t, result)
	})

	t.Run("retrospectiveAlreadyRan", func(t *testing.T) {
		cfg := newTestMultiModelConfig()
		cfg.flowDag = newTestDag("retrospective")
		metadata := newTestGoalMetadata()
		metadata.Retrospective = "true"
		newState := newTestWorkflow()
		newState.VisitCounts = map[string]int{"retrospective": 1}
		result := blockCompletionOnRetrospective(&cfg, &newState, &metadata)
		assert.Nil(t, result)
	})

	t.Run("blocksCompletion", func(t *testing.T) {
		dir := t.TempDir()
		coord, err := state.NewCoordinatorWith(filepath.Join(dir, "state.json"), newTestWorkflow())
		require.NoError(t, err)

		cfg := newTestMultiModelConfig()
		cfg.coord = coord
		cfg.agent = "coordinator"
		cfg.paddedsgai = "sgai"
		cfg.flowDag = newTestDag("retrospective")
		metadata := newTestGoalMetadata()
		metadata.Retrospective = "true"
		newState := newTestWorkflow()
		newState.Status = state.StatusComplete
		newState.VisitCounts = map[string]int{}
		result := blockCompletionOnRetrospective(&cfg, &newState, &metadata)
		require.NotNil(t, result)
		assert.Equal(t, state.StatusAgentDone, result.Status)
	})
}

func TestBlockCompletionOnProjectCriticCouncil(t *testing.T) {
	t.Run("retrospectiveDisabled", func(t *testing.T) {
		cfg := newTestMultiModelConfig()
		cfg.flowDag = newTestDag("project-critic-council")
		metadata := newTestGoalMetadata()
		metadata.Retrospective = "false"
		workflow := newTestWorkflow()
		result := blockCompletionOnProjectCriticCouncil(&cfg, &workflow, &metadata)
		assert.Nil(t, result)
	})

	t.Run("noCouncilInDag", func(t *testing.T) {
		cfg := newTestMultiModelConfig()
		cfg.flowDag = newTestDag()
		metadata := newTestGoalMetadata()
		metadata.Retrospective = "true"
		workflow := newTestWorkflow()
		result := blockCompletionOnProjectCriticCouncil(&cfg, &workflow, &metadata)
		assert.Nil(t, result)
	})

	t.Run("councilAlreadyRan", func(t *testing.T) {
		cfg := newTestMultiModelConfig()
		cfg.flowDag = newTestDag("project-critic-council")
		metadata := newTestGoalMetadata()
		metadata.Retrospective = "true"
		newState := newTestWorkflow()
		newState.VisitCounts = map[string]int{"project-critic-council": 1}
		result := blockCompletionOnProjectCriticCouncil(&cfg, &newState, &metadata)
		assert.Nil(t, result)
	})

	t.Run("blocksCompletion", func(t *testing.T) {
		dir := t.TempDir()
		coord, err := state.NewCoordinatorWith(filepath.Join(dir, "state.json"), newTestWorkflow())
		require.NoError(t, err)

		cfg := newTestMultiModelConfig()
		cfg.coord = coord
		cfg.agent = "coordinator"
		cfg.paddedsgai = "sgai"
		cfg.flowDag = newTestDag("project-critic-council")
		metadata := newTestGoalMetadata()
		metadata.Retrospective = "true"
		newState := newTestWorkflow()
		newState.Status = state.StatusComplete
		newState.VisitCounts = map[string]int{}
		result := blockCompletionOnProjectCriticCouncil(&cfg, &newState, &metadata)
		require.NotNil(t, result)
		assert.Equal(t, state.StatusAgentDone, result.Status)
		require.Len(t, result.Messages, 1)
		assert.Equal(t, "project-critic-council", result.Messages[0].ToAgent)
	})

	t.Run("fansOutToCouncilModels", func(t *testing.T) {
		dir := t.TempDir()
		coord, err := state.NewCoordinatorWith(filepath.Join(dir, "state.json"), newTestWorkflow())
		require.NoError(t, err)

		cfg := newTestMultiModelConfig()
		cfg.coord = coord
		cfg.dir = dir
		cfg.agent = "coordinator"
		cfg.paddedsgai = "sgai"
		cfg.flowDag = newTestDag("project-critic-council")
		metadata := newTestGoalMetadata()
		metadata.Retrospective = "true"
		metadata.Models = map[string]any{
			"project-critic-council": []any{"model-a", "model-b"},
		}
		newState := newTestWorkflow()
		newState.Status = state.StatusComplete
		newState.VisitCounts = map[string]int{}

		result := blockCompletionOnProjectCriticCouncil(&cfg, &newState, &metadata)
		require.NotNil(t, result)
		assert.Equal(t, state.StatusAgentDone, result.Status)
		require.Len(t, result.Messages, 2)
		assert.Equal(t, "project-critic-council:model-a", result.Messages[0].ToAgent)
		assert.Equal(t, "project-critic-council:model-b", result.Messages[1].ToAgent)
	})

	t.Run("doesNotDuplicateUnreadCouncilMessage", func(t *testing.T) {
		dir := t.TempDir()
		coord, err := state.NewCoordinatorWith(filepath.Join(dir, "state.json"), newTestWorkflow())
		require.NoError(t, err)

		cfg := newTestMultiModelConfig()
		cfg.coord = coord
		cfg.agent = "coordinator"
		cfg.paddedsgai = "sgai"
		cfg.flowDag = newTestDag("project-critic-council")
		newState := newTestWorkflow()
		newState.Status = state.StatusComplete
		newState.VisitCounts = map[string]int{}
		message := newTestMessage()
		message.ID = 1
		message.ToAgent = "project-critic-council"
		message.Read = false
		newState.Messages = []state.Message{message}

		metadata := newTestGoalMetadata()
		metadata.Retrospective = "true"
		result := blockCompletionOnProjectCriticCouncil(&cfg, &newState, &metadata)
		require.NotNil(t, result)
		assert.Len(t, result.Messages, 1)
	})
}

func TestBlockCompletionOnGateScript(t *testing.T) {
	t.Run("noGateScript", func(t *testing.T) {
		cfg := newTestMultiModelConfig()
		metadata := newTestGoalMetadata()
		workflow := newTestWorkflow()
		result := blockCompletionOnGateScript(t.Context(), &cfg, &workflow, &metadata)
		assert.Nil(t, result)
	})
}

func TestHandleCompleteStatus(t *testing.T) {
	t.Run("nonCoordinatorAgent", func(t *testing.T) {
		dir := t.TempDir()
		coord, err := state.NewCoordinatorWith(filepath.Join(dir, "state.json"), newTestWorkflow())
		require.NoError(t, err)

		cfg := newTestMultiModelConfig()
		cfg.coord = coord
		cfg.agent = "developer"
		cfg.paddedsgai = "sgai"
		cfg.flowDag = newTestDag()

		newState := newTestWorkflow()
		newState.Status = state.StatusComplete
		metadata := newTestGoalMetadata()
		result := handleCompleteStatus(t.Context(), &cfg, &newState, &metadata)
		assert.Equal(t, state.StatusAgentDone, result.Status)
	})

	t.Run("coordinatorNoPendingTodos", func(t *testing.T) {
		dir := t.TempDir()
		coord, err := state.NewCoordinatorWith(filepath.Join(dir, "state.json"), newTestWorkflow())
		require.NoError(t, err)

		cfg := newTestMultiModelConfig()
		cfg.coord = coord
		cfg.agent = "coordinator"
		cfg.paddedsgai = "sgai"
		cfg.flowDag = newTestDag()

		newState := newTestWorkflow()
		newState.Status = state.StatusComplete
		metadata := newTestGoalMetadata()
		metadata.Retrospective = "false"

		result := handleCompleteStatus(t.Context(), &cfg, &newState, &metadata)
		assert.Equal(t, state.StatusComplete, result.Status)
	})

	t.Run("blockedByPendingTodos", func(t *testing.T) {
		dir := t.TempDir()
		coord, err := state.NewCoordinatorWith(filepath.Join(dir, "state.json"), newTestWorkflow())
		require.NoError(t, err)

		cfg := newTestMultiModelConfig()
		cfg.coord = coord
		cfg.agent = "coordinator"
		cfg.paddedsgai = "sgai"
		cfg.flowDag = newTestDag()

		newState := newTestWorkflow()
		newState.Status = state.StatusComplete
		todo := newTestTodoItem()
		todo.Content = "unfinished"
		todo.Status = "pending"
		todo.Priority = "high"
		newState.ProjectTodos = []state.TodoItem{todo}

		metadata := newTestGoalMetadata()
		metadata.Retrospective = "false"
		result := handleCompleteStatus(t.Context(), &cfg, &newState, &metadata)
		assert.Equal(t, state.StatusWorking, result.Status)
	})

	t.Run("blockedByPendingProjectTodosFromReturnedState", func(t *testing.T) {
		dir := t.TempDir()
		coord, err := state.NewCoordinatorWith(filepath.Join(dir, "state.json"), newTestWorkflow())
		require.NoError(t, err)

		cfg := newTestMultiModelConfig()
		cfg.coord = coord
		cfg.agent = "coordinator"
		cfg.paddedsgai = "sgai"
		cfg.flowDag = newTestDag()

		newState := updated(newTestWorkflow(), func(workflow *state.Workflow) {
			workflow.Status = state.StatusComplete
			workflow.ProjectTodos = []state.TodoItem{updated(newTestTodoItem(), func(todo *state.TodoItem) {
				todo.Content = "project task"
				todo.Status = "pending"
				todo.Priority = "high"
			})}
		})
		metadata := newTestGoalMetadata()
		metadata.Retrospective = "false"
		result := handleCompleteStatus(t.Context(), &cfg, &newState, &metadata)
		assert.Equal(t, state.StatusWorking, result.Status)
	})

	t.Run("blockedByGateScript", func(t *testing.T) {
		dir := t.TempDir()
		coord, err := state.NewCoordinatorWith(filepath.Join(dir, "state.json"), newTestWorkflow())
		require.NoError(t, err)

		cfg := newTestMultiModelConfig()
		cfg.coord = coord
		cfg.agent = "coordinator"
		cfg.paddedsgai = "sgai"
		cfg.dir = dir
		cfg.flowDag = newTestDag()

		newState := newTestWorkflow()
		newState.Status = state.StatusComplete
		metadata := newTestGoalMetadata()
		metadata.CompletionGateScript = "false"
		metadata.Retrospective = "false"

		result := handleCompleteStatus(t.Context(), &cfg, &newState, &metadata)
		assert.Equal(t, state.StatusWorking, result.Status)
	})

	t.Run("blockedByRetrospective", func(t *testing.T) {
		dir := t.TempDir()
		coord, err := state.NewCoordinatorWith(filepath.Join(dir, "state.json"), newTestWorkflow())
		require.NoError(t, err)

		cfg := newTestMultiModelConfig()
		cfg.coord = coord
		cfg.agent = "coordinator"
		cfg.paddedsgai = "sgai"
		cfg.flowDag = newTestDag("retrospective")

		newState := newTestWorkflow()
		newState.Status = state.StatusComplete
		newState.VisitCounts = map[string]int{}

		metadata := newTestGoalMetadata()
		result := handleCompleteStatus(t.Context(), &cfg, &newState, &metadata)
		assert.Equal(t, state.StatusAgentDone, result.Status)
	})

	t.Run("blockedByProjectCriticCouncilBeforeRetrospective", func(t *testing.T) {
		dir := t.TempDir()
		coord, err := state.NewCoordinatorWith(filepath.Join(dir, "state.json"), newTestWorkflow())
		require.NoError(t, err)

		cfg := newTestMultiModelConfig()
		cfg.coord = coord
		cfg.agent = "coordinator"
		cfg.paddedsgai = "sgai"
		cfg.flowDag = newTestDag("project-critic-council", "retrospective")

		newState := newTestWorkflow()
		newState.Status = state.StatusComplete
		newState.VisitCounts = map[string]int{}
		metadata := newTestGoalMetadata()
		result := handleCompleteStatus(t.Context(), &cfg, &newState, &metadata)
		assert.Equal(t, state.StatusAgentDone, result.Status)
		require.Len(t, result.Messages, 1)
		assert.Equal(t, "project-critic-council", result.Messages[0].ToAgent)
	})
}

func TestRedirectToPendingMessageAgent(t *testing.T) {
	t.Run("noMessages", func(t *testing.T) {
		dir := t.TempDir()
		coord, err := state.NewCoordinatorWith(filepath.Join(dir, "state.json"), newTestWorkflow())
		require.NoError(t, err)

		wfState := newTestWorkflow()
		result, errRedirect := redirectToPendingMessageAgent(&wfState, coord, "sgai")
		require.NoError(t, errRedirect)
		assert.False(t, result)
	})

	t.Run("allMessagesRead", func(t *testing.T) {
		dir := t.TempDir()
		coord, err := state.NewCoordinatorWith(filepath.Join(dir, "state.json"), newTestWorkflow())
		require.NoError(t, err)

		wfState := newTestWorkflow()
		message := newTestMessage()
		message.ID = 1
		message.ToAgent = "dev"
		message.Read = true
		wfState.Messages = []state.Message{message}
		result, errRedirect := redirectToPendingMessageAgent(&wfState, coord, "sgai")
		require.NoError(t, errRedirect)
		assert.False(t, result)
	})

	t.Run("unreadMessageRedirects", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state.json")
		coord, err := state.NewCoordinatorWith(statePath, newTestWorkflow())
		require.NoError(t, err)

		wfState := newTestWorkflow()
		wfState.VisitCounts = map[string]int{}
		message := newTestMessage()
		message.ID = 1
		message.ToAgent = "developer"
		message.Read = false
		wfState.Messages = []state.Message{message}
		result, errRedirect := redirectToPendingMessageAgent(&wfState, coord, "sgai")
		require.NoError(t, errRedirect)
		assert.True(t, result)
		assert.Equal(t, "developer", wfState.CurrentAgent)
		assert.Equal(t, state.StatusWorking, wfState.Status)
	})
}

func TestBuildAgentArgsVariants(t *testing.T) {
	cases := []struct {
		name      string
		agent     string
		baseAgent string
		modelSpec string
		sessionID string
		wantModel bool
	}{
		{"basic", "coordinator", "coordinator", "", "", false},
		{"withModel", "builder", "builder", "claude-opus-4", "", true},
		{"withModelVariant", "builder", "builder", "claude-opus-4/fast", "", true},
		{"withSession", "builder", "builder", "", "sess-123", false},
		{"withModelAndSession", "builder", "builder", "gpt-4", "sess-1", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := buildAgentArgs(tc.agent, tc.baseAgent, tc.modelSpec, tc.sessionID)
			assert.Contains(t, args, "run")
			assert.Contains(t, args, "--agent")
			if tc.wantModel {
				assert.Contains(t, args, "--model")
			}
			if tc.sessionID != "" {
				assert.Contains(t, args, "--session")
			}
		})
	}
}

func TestBuildAgentMessageWithPendingMessages(t *testing.T) {
	dag := buildTestDag(map[string][]string{"coordinator": {"builder"}}, []string{"coordinator"})
	cfg := newTestMultiModelConfig()
	cfg.dir = "/tmp/test"
	cfg.agent = "builder"
	cfg.flowDag = dag
	wfState := newTestWorkflow()
	message := newTestMessage()
	message.ID = 1
	message.FromAgent = "coordinator"
	message.ToAgent = "builder"
	message.Body = "Do work"
	message.Read = false
	wfState.Messages = []state.Message{message}
	wfState.VisitCounts = map[string]int{"builder": 1}
	todo := newTestTodoItem()
	todo.Content = "task 1"
	todo.Status = "pending"
	todo.Priority = "high"
	wfState.Todos = []state.TodoItem{todo}
	wfState.CurrentAgent = "builder"
	metadata := newTestGoalMetadata()

	msg := buildAgentMessage(&cfg, &wfState, &metadata)
	assert.Contains(t, msg, "PENDING MESSAGE")
	assert.Contains(t, msg, "pending TODO items")
}

func TestBuildAgentMessageOutboxPendingMessages(t *testing.T) {
	dag := buildTestDag(map[string][]string{"coordinator": {"builder"}}, []string{"coordinator"})
	cfg := newTestMultiModelConfig()
	cfg.dir = "/tmp/test"
	cfg.agent = "builder"
	cfg.flowDag = dag
	wfState := newTestWorkflow()
	message := newTestMessage()
	message.ID = 1
	message.FromAgent = "builder"
	message.ToAgent = "reviewer"
	message.Body = "Review please"
	message.Read = false
	wfState.Messages = []state.Message{message}
	wfState.VisitCounts = map[string]int{"builder": 1}
	metadata := newTestGoalMetadata()

	msg := buildAgentMessage(&cfg, &wfState, &metadata)
	assert.Contains(t, msg, "yield control")
}

func TestBuildAgentMessageWithMultiModel(t *testing.T) {
	dag := buildTestDag(map[string][]string{"coordinator": {"builder"}}, []string{"coordinator"})
	cfg := newTestMultiModelConfig()
	cfg.dir = "/tmp/test"
	cfg.agent = "builder"
	cfg.flowDag = dag
	wfState := newTestWorkflow()
	wfState.Messages = []state.Message{}
	wfState.VisitCounts = map[string]int{"builder": 1}
	wfState.CurrentModel = "model-1"
	metadata := newTestGoalMetadata()
	metadata.Models = map[string]any{
		"builder": []any{"model-1", "model-2"},
	}

	msg := buildAgentMessage(&cfg, &wfState, &metadata)
	assert.NotEmpty(t, msg)
}

func TestBuildAgentMessageWithModelSpecificPendingMessages(t *testing.T) {
	dag := buildTestDag(map[string][]string{"coordinator": {"project-critic-council"}}, []string{"coordinator"})
	cfg := newTestMultiModelConfig()
	cfg.dir = "/tmp/test"
	cfg.agent = "project-critic-council"
	cfg.flowDag = dag
	wfState := newTestWorkflow()
	message := newTestMessage()
	message.ID = 1
	message.FromAgent = "coordinator"
	message.ToAgent = "project-critic-council:model-2"
	message.Body = "Please review"
	message.Read = false
	wfState.Messages = []state.Message{message}
	wfState.VisitCounts = map[string]int{"project-critic-council": 1}
	wfState.CurrentAgent = "project-critic-council"
	wfState.CurrentModel = "project-critic-council:model-2"
	metadata := newTestGoalMetadata()
	metadata.Models = map[string]any{
		"project-critic-council": []any{"model-1", "model-2"},
	}

	msg := buildAgentMessage(&cfg, &wfState, &metadata)
	assert.Contains(t, msg, "PENDING MESSAGE")
}

func TestInitializeWorkspaceDirAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai"), 0o755))
	err := initializeWorkspaceDir(dir)
	require.NoError(t, err)
}

func TestInitializeWorkspaceDirFreshSetup(t *testing.T) {
	dir := t.TempDir()
	err := initializeWorkspaceDir(dir)
	require.NoError(t, err)
	assert.DirExists(t, filepath.Join(dir, ".sgai"))
}

func TestInitializeWorkspaceDirRefreshesExistingDotSGAI(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai", "agent"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sgai", "skills", "startup-overlay"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".sgai", "agent", "coordinator.md"), []byte("stale coordinator"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sgai", "skills", "startup-overlay", "SKILL.md"), []byte("overlay refresh"), 0o644))

	err := initializeWorkspaceDir(dir)
	require.NoError(t, err)

	wantCoordinator := readSkeletonFileForTest(t, ".sgai/agent/coordinator.md")
	gotCoordinator, errRead := os.ReadFile(filepath.Join(dir, ".sgai", "agent", "coordinator.md"))
	require.NoError(t, errRead)
	assert.Equal(t, wantCoordinator, string(gotCoordinator))

	gotOverlay, errRead := os.ReadFile(filepath.Join(dir, ".sgai", "skills", "startup-overlay", "SKILL.md"))
	require.NoError(t, errRead)
	assert.Equal(t, "overlay refresh", string(gotOverlay))
}

func assertInitializeWorkspaceDirRefreshesGitExclude(t *testing.T, initialExclude string) {
	t.Helper()

	dir := t.TempDir()
	excludePath := filepath.Join(dir, ".git", "info", "exclude")
	require.NoError(t, initializeJJ(dir))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(excludePath), 0o755))
	require.NoError(t, os.WriteFile(excludePath, []byte(initialExclude), 0o644))

	err := initializeWorkspaceDir(dir)
	require.NoError(t, err)

	content, errRead := os.ReadFile(excludePath)
	require.NoError(t, errRead)
	assert.Equal(t, "node_modules\n/.sgai\n", string(content))

	err = initializeWorkspaceDir(dir)
	require.NoError(t, err)

	content, errRead = os.ReadFile(excludePath)
	require.NoError(t, errRead)
	assert.Equal(t, "node_modules\n/.sgai\n", string(content))
}

func TestInitializeWorkspaceDirRefreshesGitExcludeForExistingWorkspace(t *testing.T) {
	assertInitializeWorkspaceDirRefreshesGitExclude(t, "node_modules\n")
}

func TestInitializeWorkspaceDirRefreshesGitExcludeWithoutTrailingNewlineForExistingWorkspace(t *testing.T) {
	assertInitializeWorkspaceDirRefreshesGitExclude(t, "node_modules")
}

func TestInitializeWorkspaceDirRefreshesGitExcludeForGitFileWorkspace(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, "repo-meta", "git")
	excludePath := filepath.Join(gitDir, "info", "exclude")
	require.NoError(t, os.MkdirAll(filepath.Dir(excludePath), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: repo-meta/git\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".jj"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".jj", "repo"), []byte("/tmp/root/.jj/repo"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai"), 0o755))
	require.NoError(t, os.WriteFile(excludePath, []byte("node_modules\n"), 0o644))

	err := initializeWorkspaceDir(dir)
	require.NoError(t, err)

	content, errRead := os.ReadFile(excludePath)
	require.NoError(t, errRead)
	assert.Equal(t, "node_modules\n/.sgai\n", string(content))

	err = initializeWorkspaceDir(dir)
	require.NoError(t, err)

	content, errRead = os.ReadFile(excludePath)
	require.NoError(t, errRead)
	assert.Equal(t, "node_modules\n/.sgai\n", string(content))
}

func TestInitializeWorkspaceDirRefreshesGitExcludeForGitWorktreeWorkspace(t *testing.T) {
	base := t.TempDir()
	workspaceDir := filepath.Join(base, "feature")
	gitCommonDir := filepath.Join(base, "main", ".git")
	gitDir := filepath.Join(gitCommonDir, "worktrees", "feature")
	excludePath := filepath.Join(gitDir, "info", "exclude")
	require.NoError(t, os.MkdirAll(filepath.Dir(excludePath), 0o755))
	seedGitCommonDir(t, gitCommonDir)
	seedGitWorktreeMetadata(t, workspaceDir, gitDir, "../..")
	seedWorkspaceInitializationState(t, workspaceDir)
	require.NoError(t, os.WriteFile(excludePath, []byte("node_modules\n"), 0o644))

	err := initializeWorkspaceDir(workspaceDir)
	require.NoError(t, err)

	content, errRead := os.ReadFile(excludePath)
	require.NoError(t, errRead)
	assert.Equal(t, "node_modules\n/.sgai\n", string(content))
}

func TestInitializeWorkspaceDirRejectsGitFileOutsideBoundary(t *testing.T) {
	dir := t.TempDir()
	hostileGitDir := filepath.Join(t.TempDir(), "git-meta")
	require.NoError(t, os.MkdirAll(hostileGitDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: "+hostileGitDir+"\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".jj"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".jj", "repo"), []byte("/tmp/root/.jj/repo"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai"), 0o755))

	err := initializeWorkspaceDir(dir)
	require.Error(t, err)
	require.ErrorContains(t, err, "escapes repository metadata boundary")
	_, errStat := os.Stat(filepath.Join(hostileGitDir, "info", "exclude"))
	assert.True(t, os.IsNotExist(errStat))
}

func TestInitializeWorkspaceDirRejectsForgedGitWorktreeMetadata(t *testing.T) {
	base := t.TempDir()
	workspaceDir := filepath.Join(base, "feature")
	gitDir := filepath.Join(base, "hostile", ".git", "worktrees", "feature")
	seedGitWorktreeMetadata(t, workspaceDir, gitDir, "../../elsewhere")
	seedWorkspaceInitializationState(t, workspaceDir)

	err := initializeWorkspaceDir(workspaceDir)
	require.Error(t, err)
	require.ErrorContains(t, err, "repository metadata boundary")
	_, errStat := os.Stat(filepath.Join(gitDir, "info", "exclude"))
	assert.True(t, os.IsNotExist(errStat))
}

func TestInitializeWorkspaceDirRejectsSelfConsistentForgedGitWorktreeMetadata(t *testing.T) {
	base := t.TempDir()
	workspaceDir := filepath.Join(base, "feature")
	gitDir := filepath.Join(base, "hostile", ".git", "worktrees", "feature")
	seedGitWorktreeMetadata(t, workspaceDir, gitDir, "../..")
	seedWorkspaceInitializationState(t, workspaceDir)

	err := initializeWorkspaceDir(workspaceDir)
	require.Error(t, err)
	require.ErrorContains(t, err, "repository metadata boundary")
	_, errStat := os.Stat(filepath.Join(gitDir, "info", "exclude"))
	assert.True(t, os.IsNotExist(errStat))
}

func TestInitializeWorkspaceDirRejectsGitFileSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	workspaceDir := filepath.Join(base, "workspace")
	hostileGitDir := filepath.Join(base, "elsewhere", "git-meta")
	symlinkPath := filepath.Join(workspaceDir, "meta-link")
	require.NoError(t, os.MkdirAll(workspaceDir, 0o755))
	require.NoError(t, os.MkdirAll(hostileGitDir, 0o755))
	require.NoError(t, os.Symlink(hostileGitDir, symlinkPath))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceDir, ".git"), []byte("gitdir: meta-link\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceDir, ".jj"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceDir, ".jj", "repo"), []byte("/tmp/root/.jj/repo"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceDir, ".sgai"), 0o755))

	err := initializeWorkspaceDir(workspaceDir)
	require.Error(t, err)
	require.ErrorContains(t, err, "escapes repository metadata boundary")
	_, errStat := os.Stat(filepath.Join(hostileGitDir, "info", "exclude"))
	assert.True(t, os.IsNotExist(errStat))
}

func TestInitializeWorkspaceDirRejectsSymlinkedGitEntry(t *testing.T) {
	base := t.TempDir()
	workspaceDir := filepath.Join(base, "workspace")
	hostileGitDir := filepath.Join(base, "elsewhere", ".git")
	require.NoError(t, os.MkdirAll(workspaceDir, 0o755))
	require.NoError(t, os.MkdirAll(hostileGitDir, 0o755))
	require.NoError(t, os.Symlink(hostileGitDir, filepath.Join(workspaceDir, ".git")))
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceDir, ".jj"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceDir, ".jj", "repo"), []byte("/tmp/root/.jj/repo"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceDir, ".sgai"), 0o755))

	err := initializeWorkspaceDir(workspaceDir)
	require.Error(t, err)
	require.ErrorContains(t, err, "symlinked .git entry")
	_, errStat := os.Stat(filepath.Join(hostileGitDir, "info", "exclude"))
	assert.True(t, os.IsNotExist(errStat))
}

func TestInitializeWorkspaceDirRejectsSymlinkedGitInfoDir(t *testing.T) {
	base := t.TempDir()
	workspaceDir := filepath.Join(base, "workspace")
	hostileInfoDir := filepath.Join(base, "elsewhere", "info")
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceDir, ".git"), 0o755))
	require.NoError(t, os.MkdirAll(hostileInfoDir, 0o755))
	require.NoError(t, os.Symlink(hostileInfoDir, filepath.Join(workspaceDir, ".git", "info")))
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceDir, ".jj"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceDir, ".jj", "repo"), []byte("/tmp/root/.jj/repo"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceDir, ".sgai"), 0o755))

	err := initializeWorkspaceDir(workspaceDir)
	require.Error(t, err)
	require.ErrorContains(t, err, "symlinked path is not allowed")
	_, errStat := os.Stat(filepath.Join(hostileInfoDir, "exclude"))
	assert.True(t, os.IsNotExist(errStat))
}

func TestInitializeWorkspaceDirRejectsSymlinkedGitExcludeFile(t *testing.T) {
	base := t.TempDir()
	workspaceDir := filepath.Join(base, "workspace")
	hostileExcludePath := filepath.Join(base, "elsewhere", "exclude")
	gitInfoDir := filepath.Join(workspaceDir, ".git", "info")
	require.NoError(t, os.MkdirAll(gitInfoDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(hostileExcludePath), 0o755))
	require.NoError(t, os.WriteFile(hostileExcludePath, []byte("node_modules\n"), 0o644))
	require.NoError(t, os.Symlink(hostileExcludePath, filepath.Join(gitInfoDir, "exclude")))
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceDir, ".jj"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceDir, ".jj", "repo"), []byte("/tmp/root/.jj/repo"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceDir, ".sgai"), 0o755))

	err := initializeWorkspaceDir(workspaceDir)
	require.Error(t, err)
	require.ErrorContains(t, err, "symlinked path is not allowed")
	content, errRead := os.ReadFile(hostileExcludePath)
	require.NoError(t, errRead)
	assert.Equal(t, "node_modules\n", string(content))
}

func TestInitializeWorkspaceDirPreservesObsoleteFiles(t *testing.T) {
	dir := t.TempDir()
	obsoletePath := filepath.Join(dir, ".sgai", "skills", "obsolete", "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(obsoletePath), 0o755))
	require.NoError(t, os.WriteFile(obsoletePath, []byte("keep me"), 0o644))

	err := initializeWorkspaceDir(dir)
	require.NoError(t, err)

	content, errRead := os.ReadFile(obsoletePath)
	require.NoError(t, errRead)
	assert.Equal(t, "keep me", string(content))
}

func TestFormatDurationVariants(t *testing.T) {
	assert.Equal(t, "45s", formatDuration(45*time.Second))
	assert.Equal(t, "5m 30s", formatDuration(5*time.Minute+30*time.Second))
	assert.Equal(t, "135m 0s", formatDuration(2*time.Hour+15*time.Minute))
}

func TestIsFalsishVariants(t *testing.T) {
	assert.True(t, isFalsish("false"))
	assert.True(t, isFalsish("no"))
	assert.True(t, isFalsish("0"))
	assert.True(t, isFalsish("FALSE"))
	assert.False(t, isFalsish("true"))
	assert.False(t, isFalsish("yes"))
}

func TestRetrospectiveEnabledVariants(t *testing.T) {
	assert.True(t, retrospectiveEnabled(newTestGoalMetadata().Retrospective))
	metadataFalse := newTestGoalMetadata()
	metadataFalse.Retrospective = "false"
	assert.False(t, retrospectiveEnabled(metadataFalse.Retrospective))
	metadataTrue := newTestGoalMetadata()
	metadataTrue.Retrospective = "true"
	assert.True(t, retrospectiveEnabled(metadataTrue.Retrospective))
}

func TestIsExistingDirectoryVariants(t *testing.T) {
	dir := t.TempDir()
	assert.True(t, isExistingDirectory(dir))
	assert.False(t, isExistingDirectory("/nonexistent/12345"))
}

func TestPrintUsageDoesNotPanic(_ *testing.T) {
	printUsage()
}

func TestResolveBaseAgentCases(t *testing.T) {
	alias := map[string]string{}
	assert.Equal(t, "coordinator", resolveBaseAgent(alias, "coordinator"))
	assert.Equal(t, "builder", resolveBaseAgent(alias, "builder"))
	assert.Equal(t, "retrospective", resolveBaseAgent(alias, "retrospective"))
}

func TestResolveBaseAgentWithAlias(t *testing.T) {
	alias := map[string]string{"custom-agent": "coordinator"}
	assert.Equal(t, "coordinator", resolveBaseAgent(alias, "custom-agent"))
	assert.Equal(t, "builder", resolveBaseAgent(alias, "builder"))
}

func TestFindFirstPendingMessageAgentVariants(t *testing.T) {
	t.Run("noMessages", func(t *testing.T) {
		assert.Empty(t, findFirstPendingMessageAgent(newTestWorkflow().Messages))
	})

	t.Run("allRead", func(t *testing.T) {
		wf := newTestWorkflow()
		message := newTestMessage()
		message.ToAgent = "builder"
		message.Read = true
		wf.Messages = []state.Message{message}
		assert.Empty(t, findFirstPendingMessageAgent(wf.Messages))
	})

	t.Run("unreadForAgent", func(t *testing.T) {
		wf := newTestWorkflow()
		message := newTestMessage()
		message.ToAgent = "builder"
		message.Read = false
		wf.Messages = []state.Message{message}
		wf.CurrentAgent = "coordinator"
		assert.Equal(t, "builder", findFirstPendingMessageAgent(wf.Messages))
	})
}

func TestValidateModelsPartial(t *testing.T) {
	t.Run("emptyModels", func(t *testing.T) {
		err := validateModels(nil)
		require.NoError(t, err)
	})

	t.Run("singleValidModel", func(t *testing.T) {
		if _, err := exec.LookPath("opencode"); err != nil {
			t.Skip("opencode not found in PATH")
		}
		models := map[string]any{"coordinator": "opencode/claude-opus-4-6"}
		err := validateModels(models)
		require.NoError(t, err)
	})

	t.Run("invalidModel", func(t *testing.T) {
		if _, err := exec.LookPath("opencode"); err != nil {
			t.Skip("opencode not found in PATH")
		}
		models := map[string]any{"coordinator": "totally-fake-model-xyz"}
		err := validateModels(models)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid model")
	})

	t.Run("listWithValidModels", func(t *testing.T) {
		if _, err := exec.LookPath("opencode"); err != nil {
			t.Skip("opencode not found in PATH")
		}
		models := map[string]any{"coordinator": []any{"opencode/claude-opus-4-6", "opencode/claude-sonnet-4-6"}}
		err := validateModels(models)
		require.NoError(t, err)
	})

	t.Run("listWithInvalidModel", func(t *testing.T) {
		if _, err := exec.LookPath("opencode"); err != nil {
			t.Skip("opencode not found in PATH")
		}
		models := map[string]any{"coordinator": []any{"opencode/claude-opus-4-6", "fake-model-abc"}}
		err := validateModels(models)
		require.Error(t, err)
	})
}

func TestSaveState(t *testing.T) {
	dir := t.TempDir()
	sgaiDir := filepath.Join(dir, ".sgai")
	require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
	statePath := filepath.Join(sgaiDir, "state.json")

	initialState := newTestWorkflow()
	initialState.Status = state.StatusWorking
	coord, errCoord := state.NewCoordinatorWith(statePath, initialState)
	require.NoError(t, errCoord)

	wf := newTestWorkflow()
	wf.Status = state.StatusComplete
	wf.Task = "done"
	require.NoError(t, saveState(coord, &wf))

	updated := coord.State()
	assert.Equal(t, state.StatusComplete, updated.Status)
}

func TestSaveStateReturnsErrorOnPersistFailure(t *testing.T) {
	dir := t.TempDir()
	blockingPath := filepath.Join(dir, "blocking-file")
	require.NoError(t, os.WriteFile(blockingPath, []byte("x"), 0o644))

	coord := state.NewCoordinatorEmpty(filepath.Join(blockingPath, "state.json"))
	wf := newTestWorkflow()
	wf.Status = state.StatusComplete

	errSave := saveState(coord, &wf)
	require.Error(t, errSave)
	assert.Contains(t, errSave.Error(), "state")
}

func TestCopyLayerSubfolder(t *testing.T) {
	workspaceDir := t.TempDir()
	srcDir := filepath.Join(workspaceDir, "sgai", "sub")
	dstDir := filepath.Join(workspaceDir, ".sgai", "sub")

	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("content"), 0o644))

	require.NoError(t, copyLayerSubfolder(workspaceDir, srcDir, dstDir, "sub"))

	data, errRead := os.ReadFile(filepath.Join(dstDir, "file.txt"))
	require.NoError(t, errRead)
	assert.Equal(t, "content", string(data))
}

func TestCopyLayerSubfolderNonExistent(t *testing.T) {
	workspaceDir := t.TempDir()
	srcDir := filepath.Join(workspaceDir, "sgai", "nonexistent")
	dstDir := filepath.Join(workspaceDir, ".sgai", "nonexistent")

	require.NoError(t, copyLayerSubfolder(workspaceDir, srcDir, dstDir, "nonexistent"))
	_, err := os.Stat(dstDir)
	assert.True(t, os.IsNotExist(err))
}

func TestApplyLayerFolderOverlayWithSkills(t *testing.T) {
	baseDir := t.TempDir()

	overlayDir := filepath.Join(baseDir, "sgai")
	skillDir := filepath.Join(overlayDir, "skills", "my-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Skill"), 0o644))

	sgaiDir := filepath.Join(baseDir, ".sgai")
	require.NoError(t, os.MkdirAll(sgaiDir, 0o755))

	err := applyLayerFolderOverlay(baseDir)
	require.NoError(t, err)

	data, errRead := os.ReadFile(filepath.Join(sgaiDir, "skills", "my-skill", "SKILL.md"))
	require.NoError(t, errRead)
	assert.Equal(t, "# Skill", string(data))
}

func TestExtractBodyEmptyFrontmatter(t *testing.T) {
	input := []byte("---\n---\n# Body content")
	result := extractBody(input)
	assert.Equal(t, "# Body content", string(result))
}

func TestExtractBodyNoFrontmatter(t *testing.T) {
	input := []byte("Just plain text")
	result := extractBody(input)
	assert.Equal(t, "Just plain text", string(result))
}

func TestExtractBodyWithDelimiterSubstringInFrontmatterValue(t *testing.T) {
	input := []byte("---\ntitle: Keep --- title\n---\n# Body content")
	result := extractBody(input)
	assert.Equal(t, "# Body content", string(result))
}

func TestExtractFrontmatterDescriptionEmpty(t *testing.T) {
	result := extractFrontmatterDescription("")
	assert.Empty(t, result)
}

func TestExtractFrontmatterDescriptionValid(t *testing.T) {
	content := "---\ndescription: A great skill\n---\n# Skill"
	result := extractFrontmatterDescription(content)
	assert.Equal(t, "A great skill", result)
}

func TestCopyFileAtomicSuccessPath(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "source.txt")
	dstPath := filepath.Join(dir, "subdir", "dest.txt")
	require.NoError(t, os.WriteFile(srcPath, []byte("hello world"), 0o644))
	require.NoError(t, copyFileAtomic(srcPath, dstPath))
	data, errRead := os.ReadFile(dstPath)
	require.NoError(t, errRead)
	assert.Equal(t, "hello world", string(data))
}

func TestCopyFileAtomicMissingSrcError(t *testing.T) {
	dir := t.TempDir()
	err := copyFileAtomic(filepath.Join(dir, "nonexistent"), filepath.Join(dir, "dest"))
	require.Error(t, err)
}

func TestCopyFinalStateToRetrospectiveWithFiles(t *testing.T) {
	dir := t.TempDir()
	retroDir := filepath.Join(dir, "retro")
	sgaiDir := filepath.Join(dir, ".sgai")
	require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
	require.NoError(t, os.MkdirAll(retroDir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(sgaiDir, "state.json"), []byte(`{"status":"complete"}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sgaiDir, "PROJECT_MANAGEMENT.md"), []byte("# PM"), 0o644))

	require.NoError(t, copyFinalStateToRetrospective(dir, retroDir))

	stateData, errState := os.ReadFile(filepath.Join(retroDir, "state.json"))
	require.NoError(t, errState)
	assert.Contains(t, string(stateData), "complete")

	pmData, errPM := os.ReadFile(filepath.Join(retroDir, "PROJECT_MANAGEMENT.md"))
	require.NoError(t, errPM)
	assert.Equal(t, "# PM", string(pmData))
}

func TestCopyFinalStateToRetrospectiveNoFilesDoesNotFail(t *testing.T) {
	dir := t.TempDir()
	retroDir := filepath.Join(dir, "retro")
	require.NoError(t, os.MkdirAll(retroDir, 0o755))
	require.NoError(t, copyFinalStateToRetrospective(dir, retroDir))
}

func TestInitializeJJTest(t *testing.T) {
	dir := t.TempDir()
	err := initializeJJ(dir)
	require.NoError(t, err)
}

func TestIsExistingDirectory(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func(*testing.T, string)
		expected  bool
	}{
		{
			name: "existingDirectory",
			setupFunc: func(t *testing.T, path string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(path, 0o755))
			},
			expected: true,
		},
		{
			name: "nonexistentPath",
			setupFunc: func(_ *testing.T, _ string) {
			},
			expected: false,
		},
		{
			name: "existingFile",
			setupFunc: func(t *testing.T, path string) {
				t.Helper()
				dir := filepath.Dir(path)
				require.NoError(t, os.MkdirAll(dir, 0o755))
				require.NoError(t, os.WriteFile(path, []byte("content"), 0o644))
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			testPath := filepath.Join(dir, "test")
			tt.setupFunc(t, testPath)
			result := isExistingDirectory(testPath)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsProtectedFile(t *testing.T) {
	tests := []struct {
		name      string
		subfolder string
		relPath   string
		expected  bool
	}{
		{
			name:      "protectedCoordinator",
			subfolder: "agent",
			relPath:   "coordinator.md",
			expected:  true,
		},
		{
			name:      "nonProtectedAgent",
			subfolder: "agent",
			relPath:   "other.md",
			expected:  false,
		},
		{
			name:      "nonProtectedSubfolder",
			subfolder: "skills",
			relPath:   "coordinator.md",
			expected:  false,
		},
		{
			name:      "emptyPath",
			subfolder: "agent",
			relPath:   "",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isProtectedFile(tt.subfolder, tt.relPath)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsExecNotFound(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nilError",
			err:      nil,
			expected: false,
		},
		{
			name:     "execNotFound",
			err:      &exec.Error{Name: "test", Err: exec.ErrNotFound},
			expected: true,
		},
		{
			name:     "otherError",
			err:      errors.New("some error"),
			expected: false,
		},
		{
			name:     "wrappedExecNotFound",
			err:      errors.Join(errors.New("wrapper"), &exec.Error{Name: "test", Err: exec.ErrNotFound}),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isExecNotFound(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseYAMLFrontmatter(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantErr     bool
		errContains string
		validate    func(*testing.T, GoalMetadata)
	}{
		{
			name: "validFrontmatter",
			content: `---
title: Canonical Goal Title
flow: |
  "agent1" -> "agent2"
models:
  "agent1": "model1"
---
# Goal`,
			wantErr:     false,
			errContains: "",
			validate: func(t *testing.T, m GoalMetadata) {
				t.Helper()
				assert.Equal(t, "Canonical Goal Title", m.Title)
				assert.Contains(t, m.Flow, "agent1")
				assert.Equal(t, "model1", m.Models["agent1"])
			},
		},
		{
			name:        "noFrontmatter",
			content:     "# Just a goal",
			wantErr:     false,
			errContains: "",
			validate: func(t *testing.T, m GoalMetadata) {
				t.Helper()
				assert.Empty(t, m.Flow)
			},
		},
		{
			name: "unclosedFrontmatter",
			content: `---
flow: "test"
# no closing`,
			wantErr:     true,
			errContains: "no closing",
			validate:    nil,
		},
		{
			name: "invalidYAML",
			content: `---
flow: [invalid yaml
---
# Goal`,
			wantErr:     true,
			errContains: "failed to parse",
			validate:    nil,
		},
		{
			name: "emptyFrontmatter",
			content: `---
---
# Goal`,
			wantErr:     false,
			errContains: "",
			validate: func(t *testing.T, m GoalMetadata) {
				t.Helper()
				assert.Empty(t, m.Flow)
			},
		},
		{
			name: "withRetrospective",
			content: `---
flow: "test"
retrospective: "true"
---
# Goal`,
			wantErr:     false,
			errContains: "",
			validate: func(t *testing.T, m GoalMetadata) {
				t.Helper()
				assert.Equal(t, "true", m.Retrospective)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metadata, err := parseYAMLFrontmatter([]byte(tt.content))

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, metadata)
			}
		})
	}
}

func TestParseYAMLFrontmatterPreservesTitleWhenOtherMetadataIsInvalid(t *testing.T) {
	content := []byte("---\ntitle: Preserved Title\nflow: [invalid yaml\n---\n# Goal")

	metadata, errParse := parseYAMLFrontmatter(content)
	require.Error(t, errParse)
	assert.Equal(t, "Preserved Title", metadata.Title)
}

func TestParseYAMLFrontmatterAllowsDelimiterSubstringInValues(t *testing.T) {
	content := []byte("---\ntitle: Keep --- title\ncontinuousModePrompt: keep --- prompt\n---\n# Goal")

	metadata, errParse := parseYAMLFrontmatter(content)
	require.NoError(t, errParse)
	assert.Equal(t, "Keep --- title", metadata.Title)
	assert.Equal(t, "keep --- prompt", metadata.ContinuousModePrompt)
}

func TestRetrospectiveEnabled(t *testing.T) {
	tests := []struct {
		name     string
		metadata GoalMetadata
		expected bool
	}{
		{
			name: "trueString",
			metadata: func() GoalMetadata {
				metadata := newTestGoalMetadata()
				metadata.Retrospective = "true"
				return metadata
			}(),
			expected: true,
		},
		{
			name: "falseString",
			metadata: func() GoalMetadata {
				metadata := newTestGoalMetadata()
				metadata.Retrospective = "false"
				return metadata
			}(),
			expected: false,
		},
		{
			name:     "emptyString",
			metadata: newTestGoalMetadata(),
			expected: true,
		},
		{
			name: "yesString",
			metadata: func() GoalMetadata {
				metadata := newTestGoalMetadata()
				metadata.Retrospective = "yes"
				return metadata
			}(),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := retrospectiveEnabled(tt.metadata.Retrospective)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFindFirstPendingMessageAgent(t *testing.T) {
	tests := []struct {
		name     string
		workflow state.Workflow
		expected string
	}{
		{
			name: "noMessages",
			workflow: func() state.Workflow {
				workflow := newTestWorkflow()
				workflow.Messages = []state.Message{}
				return workflow
			}(),
			expected: "",
		},
		{
			name: "allRead",
			workflow: func() state.Workflow {
				workflow := newTestWorkflow()
				messageOne := newTestMessage()
				messageOne.ToAgent = "agent1"
				messageOne.Read = true
				messageTwo := newTestMessage()
				messageTwo.ToAgent = "agent2"
				messageTwo.Read = true
				workflow.Messages = []state.Message{messageOne, messageTwo}
				return workflow
			}(),
			expected: "",
		},
		{
			name: "firstUnread",
			workflow: func() state.Workflow {
				workflow := newTestWorkflow()
				messageOne := newTestMessage()
				messageOne.ToAgent = "agent1"
				messageOne.Read = true
				messageTwo := newTestMessage()
				messageTwo.ToAgent = "agent2"
				messageTwo.Read = false
				messageThree := newTestMessage()
				messageThree.ToAgent = "agent3"
				messageThree.Read = false
				workflow.Messages = []state.Message{messageOne, messageTwo, messageThree}
				return workflow
			}(),
			expected: "agent2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findFirstPendingMessageAgent(tt.workflow.Messages)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractFrontmatterDescription(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name: "withDescription",
			content: `---
description: Test description
---
# Content`,
			expected: "Test description",
		},
		{
			name: "noDescription",
			content: `---
name: Test
---
# Content`,
			expected: "",
		},
		{
			name:     "noFrontmatter",
			content:  `# Just content`,
			expected: "",
		},
		{
			name:     "emptyContent",
			content:  "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractFrontmatterDescription(tt.content)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseFrontmatterMap(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected map[string]string
	}{
		{
			name: "simple",
			content: `---
key1: value1
key2: value2
---
# Content`,
			expected: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
		},
		{
			name:     "noFrontmatter",
			content:  `# Just content`,
			expected: map[string]string{},
		},
		{
			name: "emptyFrontmatter",
			content: `---
---
# Content`,
			expected: map[string]string{},
		},
		{
			name: "quotedValue",
			content: `---
key: "quoted value"
---
# Content`,
			expected: map[string]string{
				"key": "\"quoted value\"",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseFrontmatterMap([]byte(tt.content))
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetModelsForAgent(t *testing.T) {
	tests := []struct {
		name     string
		models   map[string]any
		agent    string
		expected []string
	}{
		{
			name:     "agentNotInModels",
			models:   map[string]any{"agent1": "model1"},
			agent:    "agent2",
			expected: nil,
		},
		{
			name:     "singleModel",
			models:   map[string]any{"agent1": "model1"},
			agent:    "agent1",
			expected: []string{"model1"},
		},
		{
			name:     "emptyStringModel",
			models:   map[string]any{"agent1": ""},
			agent:    "agent1",
			expected: nil,
		},
		{
			name: "multipleModels",
			models: map[string]any{
				"agent1": []any{"model1", "model2"},
			},
			agent:    "agent1",
			expected: []string{"model1", "model2"},
		},
		{
			name: "mixedTypesInArray",
			models: map[string]any{
				"agent1": []any{"model1", 123, "model2"},
			},
			agent:    "agent1",
			expected: []string{"model1", "model2"},
		},
		{
			name: "emptyArray",
			models: map[string]any{
				"agent1": []any{},
			},
			agent:    "agent1",
			expected: []string{},
		},
		{
			name: "arrayWithEmptyStrings",
			models: map[string]any{
				"agent1": []any{"", "model1", ""},
			},
			agent:    "agent1",
			expected: []string{"model1"},
		},
		{
			name:     "nilModels",
			models:   nil,
			agent:    "agent1",
			expected: nil,
		},
		{
			name:     "invalidType",
			models:   map[string]any{"agent1": 123},
			agent:    "agent1",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getModelsForAgent(tt.models, tt.agent)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNextMessageID(t *testing.T) {
	tests := []struct {
		name     string
		messages []state.Message
		expected int
	}{
		{
			name:     "empty",
			messages: []state.Message{},
			expected: 1,
		},
		{
			name: "singleMessage",
			messages: []state.Message{
				updated(newTestMessage(), func(message *state.Message) {
					message.ID = 1
				}),
			},
			expected: 2,
		},
		{
			name: "multipleMessages",
			messages: []state.Message{
				updated(newTestMessage(), func(message *state.Message) {
					message.ID = 1
				}),
				updated(newTestMessage(), func(message *state.Message) {
					message.ID = 2
				}),
				updated(newTestMessage(), func(message *state.Message) {
					message.ID = 3
				}),
			},
			expected: 4,
		},
		{
			name: "nonSequential",
			messages: []state.Message{
				updated(newTestMessage(), func(message *state.Message) {
					message.ID = 1
				}),
				updated(newTestMessage(), func(message *state.Message) {
					message.ID = 5
				}),
				updated(newTestMessage(), func(message *state.Message) {
					message.ID = 3
				}),
			},
			expected: 6,
		},
		{
			name: "zeroID",
			messages: []state.Message{
				newTestMessage(),
			},
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := nextMessageID(tt.messages)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAddEnvironmentMessage(t *testing.T) {
	wf := newTestWorkflow()
	wf.Messages = []state.Message{}

	addEnvironmentMessage(&wf, "agent1", "test message")

	assert.Len(t, wf.Messages, 1)
	assert.Equal(t, 1, wf.Messages[0].ID)
	assert.Equal(t, "environment", wf.Messages[0].FromAgent)
	assert.Equal(t, "agent1", wf.Messages[0].ToAgent)
	assert.Equal(t, "test message", wf.Messages[0].Body)
	assert.False(t, wf.Messages[0].Read)
	assert.NotEmpty(t, wf.Messages[0].CreatedAt)

	addEnvironmentMessage(&wf, "agent2", "another message")

	assert.Len(t, wf.Messages, 2)
	assert.Equal(t, 2, wf.Messages[1].ID)
	assert.Equal(t, "agent2", wf.Messages[1].ToAgent)
}

func TestHasMessagesForModel(t *testing.T) {
	tests := []struct {
		name     string
		messages []state.Message
		modelID  string
		expected bool
	}{
		{
			name:     "emptyMessages",
			messages: []state.Message{},
			modelID:  "agent1:model1",
			expected: false,
		},
		{
			name: "messageForModel",
			messages: []state.Message{
				updated(newTestMessage(), func(message *state.Message) {
					message.ToAgent = "agent1:model1"
				}),
			},
			modelID:  "agent1:model1",
			expected: true,
		},
		{
			name: "messageForAgentOnly",
			messages: []state.Message{
				updated(newTestMessage(), func(message *state.Message) {
					message.ToAgent = "agent1"
				}),
			},
			modelID:  "agent1:model1",
			expected: true,
		},
		{
			name: "messageAlreadyRead",
			messages: []state.Message{
				updated(newTestMessage(), func(message *state.Message) {
					message.ToAgent = "agent1:model1"
					message.Read = true
				}),
			},
			modelID:  "agent1:model1",
			expected: false,
		},
		{
			name: "messageForDifferentAgent",
			messages: []state.Message{
				updated(newTestMessage(), func(message *state.Message) {
					message.ToAgent = "agent2:model1"
				}),
			},
			modelID:  "agent1:model1",
			expected: false,
		},
		{
			name: "mixedMessages",
			messages: []state.Message{
				updated(newTestMessage(), func(message *state.Message) {
					message.ToAgent = "agent1:model1"
					message.Read = true
				}),
				updated(newTestMessage(), func(message *state.Message) {
					message.ToAgent = "agent1:model2"
				}),
			},
			modelID:  "agent1:model1",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasMessagesForModel(tt.messages, tt.modelID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHasPendingMessagesForAnyModel(t *testing.T) {
	tests := []struct {
		name     string
		messages []state.Message
		models   []string
		agent    string
		expected bool
	}{
		{
			name:     "emptyMessages",
			messages: []state.Message{},
			models:   []string{"model1", "model2"},
			agent:    "agent1",
			expected: false,
		},
		{
			name: "messageForFirstModel",
			messages: []state.Message{
				updated(newTestMessage(), func(message *state.Message) {
					message.ToAgent = "agent1:model1"
				}),
			},
			models:   []string{"model1", "model2"},
			agent:    "agent1",
			expected: true,
		},
		{
			name: "messageForSecondModel",
			messages: []state.Message{
				updated(newTestMessage(), func(message *state.Message) {
					message.ToAgent = "agent1:model2"
				}),
			},
			models:   []string{"model1", "model2"},
			agent:    "agent1",
			expected: true,
		},
		{
			name: "allMessagesRead",
			messages: []state.Message{
				updated(newTestMessage(), func(message *state.Message) {
					message.ToAgent = "agent1:model1"
					message.Read = true
				}),
				updated(newTestMessage(), func(message *state.Message) {
					message.ToAgent = "agent1:model2"
					message.Read = true
				}),
			},
			models:   []string{"model1", "model2"},
			agent:    "agent1",
			expected: false,
		},
		{
			name: "messageForDifferentAgent",
			messages: []state.Message{
				updated(newTestMessage(), func(message *state.Message) {
					message.ToAgent = "agent2:model1"
				}),
			},
			models:   []string{"model1", "model2"},
			agent:    "agent1",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasPendingMessagesForAnyModel(tt.messages, tt.models, tt.agent)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSyncModelStatuses(t *testing.T) {
	tests := []struct {
		name            string
		existingStatus  map[string]string
		models          []string
		agent           string
		expectedStatus  map[string]string
		expectedDeleted int
	}{
		{
			name:           "nilStatus",
			existingStatus: nil,
			models:         []string{"model1", "model2"},
			agent:          "agent1",
			expectedStatus: map[string]string{
				"agent1:model1": "model-working",
				"agent1:model2": "model-working",
			},
			expectedDeleted: 0,
		},
		{
			name: "addNewModels",
			existingStatus: map[string]string{
				"agent1:model1": "model-working",
			},
			models: []string{"model1", "model2"},
			agent:  "agent1",
			expectedStatus: map[string]string{
				"agent1:model1": "model-working",
				"agent1:model2": "model-working",
			},
			expectedDeleted: 0,
		},
		{
			name: "removeOldModels",
			existingStatus: map[string]string{
				"agent1:model1": "model-working",
				"agent1:model2": "model-done",
			},
			models: []string{"model1"},
			agent:  "agent1",
			expectedStatus: map[string]string{
				"agent1:model1": "model-working",
			},
			expectedDeleted: 0,
		},
		{
			name: "preserveOtherAgentStatuses",
			existingStatus: map[string]string{
				"agent1:model1": "model-working",
				"agent2:model1": "model-done",
			},
			models: []string{"model1"},
			agent:  "agent1",
			expectedStatus: map[string]string{
				"agent1:model1": "model-working",
				"agent2:model1": "model-done",
			},
			expectedDeleted: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := syncModelStatuses(tt.existingStatus, tt.models, tt.agent)
			assert.Equal(t, tt.expectedStatus, result)
		})
	}
}

func TestCleanupModelStatuses(t *testing.T) {
	wf := newTestWorkflow()
	wf.ModelStatuses = map[string]string{
		"agent1/model1": "model-working",
		"agent1/model2": "model-done",
	}
	wf.CurrentModel = "agent1/model1"

	cleanupModelStatuses(&wf)

	assert.Nil(t, wf.ModelStatuses)
	assert.Empty(t, wf.CurrentModel)
}

func TestFormatModelID(t *testing.T) {
	tests := []struct {
		name      string
		agent     string
		modelSpec string
		expected  string
	}{
		{
			name:      "simple",
			agent:     "agent1",
			modelSpec: "model1",
			expected:  "agent1:model1",
		},
		{
			name:      "emptyAgent",
			agent:     "",
			modelSpec: "model1",
			expected:  ":model1",
		},
		{
			name:      "emptyModel",
			agent:     "agent1",
			modelSpec: "",
			expected:  "agent1:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatModelID(tt.agent, tt.modelSpec)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractAgentFromModelID(t *testing.T) {
	tests := []struct {
		name     string
		modelID  string
		expected string
	}{
		{
			name:     "withColon",
			modelID:  "agent1:model1",
			expected: "agent1",
		},
		{
			name:     "noColon",
			modelID:  "agent1",
			expected: "agent1",
		},
		{
			name:     "empty",
			modelID:  "",
			expected: "",
		},
		{
			name:     "multipleColons",
			modelID:  "agent1:model1:variant",
			expected: "agent1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractAgentFromModelID(tt.modelID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAllModelsDone(t *testing.T) {
	tests := []struct {
		name          string
		modelStatuses map[string]string
		expected      bool
	}{
		{
			name:          "empty",
			modelStatuses: map[string]string{},
			expected:      true,
		},
		{
			name: "allDone",
			modelStatuses: map[string]string{
				"model1": "model-done",
				"model2": "model-done",
			},
			expected: true,
		},
		{
			name: "allDoneOrError",
			modelStatuses: map[string]string{
				"model1": "model-done",
				"model2": "model-error",
			},
			expected: true,
		},
		{
			name: "oneRunning",
			modelStatuses: map[string]string{
				"model1": "model-done",
				"model2": "model-running",
			},
			expected: false,
		},
		{
			name: "allRunning",
			modelStatuses: map[string]string{
				"model1": "model-running",
				"model2": "model-running",
			},
			expected: false,
		},
		{
			name: "oneWorking",
			modelStatuses: map[string]string{
				"model1": "model-working",
				"model2": "model-done",
			},
			expected: false,
		},
		{
			name:          "nilStatuses",
			modelStatuses: nil,
			expected:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := allModelsDone(tt.modelStatuses)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildAgentPrefix(t *testing.T) {
	tests := []struct {
		name            string
		dir             string
		paddedAgentName string
		iteration       int
		expected        string
	}{
		{
			name:            "simple",
			dir:             "/path/to/workspace",
			paddedAgentName: "agent1  ",
			iteration:       1,
			expected:        "[workspace][agent1  :0001]",
		},
		{
			name:            "largeIteration",
			dir:             "/path/to/workspace",
			paddedAgentName: "agent2",
			iteration:       12345,
			expected:        "[workspace][agent2:12345]",
		},
		{
			name:            "rootDir",
			dir:             "/workspace",
			paddedAgentName: "agent",
			iteration:       0,
			expected:        "[workspace][agent:0000]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildAgentPrefix(tt.dir, tt.paddedAgentName, tt.iteration)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildAgentArgs(t *testing.T) {
	tests := []struct {
		name      string
		agent     string
		baseAgent string
		modelSpec string
		sessionID string
		expected  []string
	}{
		{
			name:      "simpleAgent",
			agent:     "agent1",
			baseAgent: "agent1",
			modelSpec: "",
			sessionID: "",
			expected:  []string{"run", "--format=json", "--agent", "agent1", "--title", "agent1"},
		},
		{
			name:      "withModel",
			agent:     "agent1",
			baseAgent: "agent1",
			modelSpec: "gpt-4",
			sessionID: "",
			expected:  []string{"run", "--format=json", "--agent", "agent1", "--model", "gpt-4", "--title", "agent1 [gpt-4]"},
		},
		{
			name:      "withModelAndVariant",
			agent:     "agent1",
			baseAgent: "agent1",
			modelSpec: "gpt-4:latest",
			sessionID: "",
			expected:  []string{"run", "--format=json", "--agent", "agent1", "--model", "gpt-4:latest", "--title", "agent1 [gpt-4:latest]"},
		},
		{
			name:      "withSession",
			agent:     "agent1",
			baseAgent: "agent1",
			modelSpec: "",
			sessionID: "session-123",
			expected:  []string{"run", "--format=json", "--agent", "agent1", "--session", "session-123", "--title", "agent1"},
		},
		{
			name:      "withAll",
			agent:     "agent1",
			baseAgent: "base-agent",
			modelSpec: "gpt-4",
			sessionID: "session-123",
			expected:  []string{"run", "--format=json", "--agent", "base-agent", "--model", "gpt-4", "--session", "session-123", "--title", "agent1 [gpt-4]"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildAgentArgs(tt.agent, tt.baseAgent, tt.modelSpec, tt.sessionID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseModelAndVariant(t *testing.T) {
	tests := []struct {
		name          string
		modelSpec     string
		expectedModel string
		expectedVar   string
	}{
		{
			name:          "modelWithVariant",
			modelSpec:     "anthropic/claude-opus-4-6 (max)",
			expectedModel: "anthropic/claude-opus-4-6",
			expectedVar:   "max",
		},
		{
			name:          "modelWithoutVariant",
			modelSpec:     "anthropic/claude-opus-4-6",
			expectedModel: "anthropic/claude-opus-4-6",
			expectedVar:   "",
		},
		{
			name:          "empty",
			modelSpec:     "",
			expectedModel: "",
			expectedVar:   "",
		},
		{
			name:          "variantOnly",
			modelSpec:     "(max)",
			expectedModel: "(max)",
			expectedVar:   "",
		},
		{
			name:          "multipleParentheses",
			modelSpec:     "model (variant) (extra)",
			expectedModel: "model (variant)",
			expectedVar:   "extra",
		},
		{
			name:          "variantWithSpaces",
			modelSpec:     "model (variant with spaces)",
			expectedModel: "model",
			expectedVar:   "variant with spaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, variant := parseModelAndVariant(tt.modelSpec)
			assert.Equal(t, tt.expectedModel, model)
			assert.Equal(t, tt.expectedVar, variant)
		})
	}
}

func TestUpdateProjectManagementWithRetrospectiveDir(t *testing.T) {
	tests := []struct {
		name               string
		existingContent    string
		retrospectiveDir   string
		expectedContains   []string
		expectedNotContain []string
	}{
		{
			name:               "newFileNoExistingContent",
			existingContent:    "",
			retrospectiveDir:   ".sgai/retrospectives/2026-03-05-10-00.abc1",
			expectedContains:   []string{"---", "Retrospective Session: .sgai/retrospectives/2026-03-05-10-00.abc1"},
			expectedNotContain: nil,
		},
		{
			name:               "existingContentWithoutHeader",
			existingContent:    "## Some existing content\n\nHello world\n",
			retrospectiveDir:   ".sgai/retrospectives/2026-03-05-10-00.abc1",
			expectedContains:   []string{"---", "Retrospective Session:", "## Some existing content"},
			expectedNotContain: nil,
		},
		{
			name:               "replaceExistingRetrospectiveHeader",
			existingContent:    "---\nRetrospective Session: .sgai/retrospectives/old-session\n---\n\n## Old content\n",
			retrospectiveDir:   ".sgai/retrospectives/new-session",
			expectedContains:   []string{"Retrospective Session: .sgai/retrospectives/new-session", "## Old content"},
			expectedNotContain: []string{"old-session"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			pmPath := filepath.Join(tmpDir, ".sgai", "PROJECT_MANAGEMENT.md")

			if tt.existingContent != "" {
				require.NoError(t, os.MkdirAll(filepath.Dir(pmPath), 0o755))
				require.NoError(t, os.WriteFile(pmPath, []byte(tt.existingContent), 0o644))
			}

			err := updateProjectManagementWithRetrospectiveDir(pmPath, tt.retrospectiveDir)
			require.NoError(t, err)

			content, err := os.ReadFile(pmPath)
			require.NoError(t, err)

			for _, expected := range tt.expectedContains {
				assert.Contains(t, string(content), expected)
			}
			for _, notExpected := range tt.expectedNotContain {
				assert.NotContains(t, string(content), notExpected)
			}
		})
	}
}

func TestExtractRetrospectiveDirFromProjectManagement(t *testing.T) {
	tests := []struct {
		name            string
		content         string
		expected        string
		wantErrContains string
	}{
		{
			name:            "validHeader",
			content:         "---\nRetrospective Session: .sgai/retrospectives/2026-03-05-10-00.abc1\n---\n\n## Content\n",
			expected:        ".sgai/retrospectives/2026-03-05-10-00.abc1",
			wantErrContains: "",
		},
		{
			name:            "emptyRetrospectiveSession",
			content:         "---\nRetrospective Session: \n---\n",
			expected:        "",
			wantErrContains: "empty Retrospective Session in PROJECT_MANAGEMENT.md",
		},
		{
			name:            "noHeader",
			content:         "## No header here\n",
			expected:        "",
			wantErrContains: "missing frontmatter header in PROJECT_MANAGEMENT.md",
		},
		{
			name:            "emptyFile",
			content:         "",
			expected:        "",
			wantErrContains: "missing frontmatter header in PROJECT_MANAGEMENT.md",
		},
		{
			name:            "headerWithoutRetrospectiveSession",
			content:         "---\nTitle: Some Title\n---\n\n## Content\n",
			expected:        "",
			wantErrContains: "missing Retrospective Session in PROJECT_MANAGEMENT.md",
		},
		{
			name:            "missingClosingFrontmatterDelimiter",
			content:         "---\nRetrospective Session: .sgai/retrospectives/2026-03-05-10-00.abc1\n## Content\n",
			expected:        "",
			wantErrContains: "missing closing frontmatter delimiter in PROJECT_MANAGEMENT.md",
		},
		{
			name:            "malformedClosingFrontmatterDelimiter",
			content:         "---\nRetrospective Session: .sgai/retrospectives/2026-03-05-10-00.abc1\n----\n",
			expected:        "",
			wantErrContains: "missing closing frontmatter delimiter in PROJECT_MANAGEMENT.md",
		},
		{
			name:            "nonExistentFile",
			content:         "",
			expected:        "",
			wantErrContains: "read PROJECT_MANAGEMENT.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			pmPath := filepath.Join(tmpDir, "PROJECT_MANAGEMENT.md")

			if tt.name == "nonExistentFile" {
				result, errExtract := extractRetrospectiveDirFromProjectManagement(filepath.Join(tmpDir, "nonexistent.md"))
				assert.Empty(t, result)
				require.ErrorContains(t, errExtract, tt.wantErrContains)
				return
			}

			require.NoError(t, os.WriteFile(pmPath, []byte(tt.content), 0o644))
			result, errExtract := extractRetrospectiveDirFromProjectManagement(pmPath)
			assert.Equal(t, tt.expected, result)
			if tt.wantErrContains == "" {
				require.NoError(t, errExtract)
				return
			}
			require.ErrorContains(t, errExtract, tt.wantErrContains)
		})
	}
}

func TestCanResumeWorkflow(t *testing.T) {
	tests := []struct {
		name     string
		wfState  state.Workflow
		expected bool
	}{
		{
			name: "workingStatus",
			wfState: updated(newTestWorkflow(), func(wfState *state.Workflow) {
				wfState.Status = state.StatusWorking
			}),
			expected: true,
		},
		{
			name: "agentDoneStatus",
			wfState: updated(newTestWorkflow(), func(wfState *state.Workflow) {
				wfState.Status = state.StatusAgentDone
			}),
			expected: true,
		},
		{
			name: "humanMessagePending",
			wfState: updated(newTestWorkflow(), func(wfState *state.Workflow) {
				wfState.HumanMessage = "question"
			}),
			expected: true,
		},
		{
			name: "multiChoiceQuestionPending",
			wfState: updated(newTestWorkflow(), func(wfState *state.Workflow) {
				wfState.MultiChoiceQuestion = &state.MultiChoiceQuestion{
					Questions: []state.QuestionItem{updated(newTestQuestionItem(), func(question *state.QuestionItem) {
						question.Question = "test"
					})},
					IsWorkGate: false,
				}
			}),
			expected: true,
		},
		{
			name: "completeStatus",
			wfState: updated(newTestWorkflow(), func(wfState *state.Workflow) {
				wfState.Status = state.StatusComplete
			}),
			expected: false,
		},
		{
			name:     "emptyStatus",
			wfState:  newTestWorkflow(),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := canResumeWorkflow(&tt.wfState)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCopyFileAtomic(t *testing.T) {
	t.Run("successfulCopy", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcPath := filepath.Join(tmpDir, "source.txt")
		dstPath := filepath.Join(tmpDir, "dest", "copied.txt")

		require.NoError(t, os.WriteFile(srcPath, []byte("hello world"), 0o644))

		err := copyFileAtomic(srcPath, dstPath)
		require.NoError(t, err)

		content, err := os.ReadFile(dstPath)
		require.NoError(t, err)
		assert.Equal(t, "hello world", string(content))
	})

	t.Run("sourceDoesNotExist", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := copyFileAtomic(filepath.Join(tmpDir, "nonexistent"), filepath.Join(tmpDir, "dest"))
		require.Error(t, err)
	})
}

func TestCopyFinalStateToRetrospective(t *testing.T) {
	t.Run("copiesBothFiles", func(t *testing.T) {
		tmpDir := t.TempDir()
		sgaiDir := filepath.Join(tmpDir, ".sgai")
		require.NoError(t, os.MkdirAll(sgaiDir, 0o755))

		require.NoError(t, os.WriteFile(filepath.Join(sgaiDir, "state.json"), []byte(`{"status":"complete"}`), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(sgaiDir, "PROJECT_MANAGEMENT.md"), []byte("## PM Content"), 0o644))

		retroDir := filepath.Join(tmpDir, "retro")
		require.NoError(t, os.MkdirAll(retroDir, 0o755))

		err := copyFinalStateToRetrospective(tmpDir, retroDir)
		require.NoError(t, err)

		stateContent, err := os.ReadFile(filepath.Join(retroDir, "state.json"))
		require.NoError(t, err)
		assert.Contains(t, string(stateContent), "complete")

		pmContent, err := os.ReadFile(filepath.Join(retroDir, "PROJECT_MANAGEMENT.md"))
		require.NoError(t, err)
		assert.Contains(t, string(pmContent), "PM Content")
	})

	t.Run("missingFilesNoError", func(t *testing.T) {
		tmpDir := t.TempDir()
		retroDir := filepath.Join(tmpDir, "retro")
		require.NoError(t, os.MkdirAll(retroDir, 0o755))

		err := copyFinalStateToRetrospective(tmpDir, retroDir)
		require.NoError(t, err)
	})
}

func TestApplyLayerFolderOverlay(t *testing.T) {
	t.Run("noLayerDir", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := applyLayerFolderOverlay(tmpDir)
		require.NoError(t, err)
	})

	t.Run("copiesSkillsOverlay", func(t *testing.T) {
		tmpDir := t.TempDir()

		srcSkillDir := filepath.Join(tmpDir, "sgai", "skills", "my-skill")
		require.NoError(t, os.MkdirAll(srcSkillDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(srcSkillDir, "SKILL.md"), []byte("# Skill Content"), 0o644))

		dstDir := filepath.Join(tmpDir, ".sgai")
		require.NoError(t, os.MkdirAll(dstDir, 0o755))

		err := applyLayerFolderOverlay(tmpDir)
		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(tmpDir, ".sgai", "skills", "my-skill", "SKILL.md"))
		require.NoError(t, err)
		assert.Equal(t, "# Skill Content", string(content))
	})

	t.Run("protectsCoordinatorMD", func(t *testing.T) {
		tmpDir := t.TempDir()

		srcAgentDir := filepath.Join(tmpDir, "sgai", "agent")
		require.NoError(t, os.MkdirAll(srcAgentDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(srcAgentDir, "coordinator.md"), []byte("SHOULD NOT COPY"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(srcAgentDir, "developer.md"), []byte("SHOULD COPY"), 0o644))

		dstAgentDir := filepath.Join(tmpDir, ".sgai", "agent")
		require.NoError(t, os.MkdirAll(dstAgentDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dstAgentDir, "coordinator.md"), []byte("ORIGINAL"), 0o644))

		err := applyLayerFolderOverlay(tmpDir)
		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(dstAgentDir, "coordinator.md"))
		require.NoError(t, err)
		assert.Equal(t, "ORIGINAL", string(content))

		content, err = os.ReadFile(filepath.Join(dstAgentDir, "developer.md"))
		require.NoError(t, err)
		assert.Equal(t, "SHOULD COPY", string(content))
	})
}

func TestAgentHasUnreadOutgoingMessages(t *testing.T) {
	tests := []struct {
		name         string
		messages     []state.Message
		agentName    string
		currentModel string
		expected     bool
	}{
		{
			name:         "noMessages",
			messages:     []state.Message{},
			agentName:    "test-agent",
			currentModel: "",
			expected:     false,
		},
		{
			name: "hasUnreadOutgoing",
			messages: []state.Message{
				updated(newTestMessage(), func(message *state.Message) {
					message.ID = 1
					message.FromAgent = "test-agent"
					message.ToAgent = "coordinator"
					message.Body = "hello"
				}),
			},
			agentName:    "test-agent",
			currentModel: "",
			expected:     true,
		},
		{
			name: "allOutgoingRead",
			messages: []state.Message{
				updated(newTestMessage(), func(message *state.Message) {
					message.ID = 1
					message.FromAgent = "test-agent"
					message.ToAgent = "coordinator"
					message.Body = "hello"
					message.Read = true
				}),
			},
			agentName:    "test-agent",
			currentModel: "",
			expected:     false,
		},
		{
			name: "unreadFromOtherAgent",
			messages: []state.Message{
				updated(newTestMessage(), func(message *state.Message) {
					message.ID = 1
					message.FromAgent = "other-agent"
					message.ToAgent = "coordinator"
					message.Body = "hello"
				}),
			},
			agentName:    "test-agent",
			currentModel: "",
			expected:     false,
		},
		{
			name: "unreadFromCurrentModel",
			messages: []state.Message{
				updated(newTestMessage(), func(message *state.Message) {
					message.ID = 1
					message.FromAgent = "project-critic-council:model-a"
					message.ToAgent = "coordinator"
					message.Body = "hello"
				}),
			},
			agentName:    "project-critic-council",
			currentModel: "project-critic-council:model-a",
			expected:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf := updated(newTestWorkflow(), func(wf *state.Workflow) {
				wf.Messages = tt.messages
				wf.CurrentModel = tt.currentModel
			})
			result := agentHasUnreadOutgoingMessages(&wf, tt.agentName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildAgentMessage(t *testing.T) {
	dag, err := parseFlow("\"agent1\" -> \"agent2\"\n", "")
	require.NoError(t, err)

	tests := []struct {
		name         string
		cfg          multiModelConfig
		wfState      state.Workflow
		metadata     GoalMetadata
		wantContains []string
	}{
		{
			name: "withPendingMessages",
			cfg: updated(newTestMultiModelConfig(), func(cfg *multiModelConfig) {
				cfg.agent = "agent1"
				cfg.flowDag = dag
				cfg.dir = t.TempDir()
			}),
			wfState: updated(newTestWorkflow(), func(wfState *state.Workflow) {
				wfState.Status = state.StatusWorking
				wfState.VisitCounts = map[string]int{"agent1": 1}
				wfState.Messages = []state.Message{updated(newTestMessage(), func(message *state.Message) {
					message.ID = 1
					message.FromAgent = "coordinator"
					message.ToAgent = "agent1"
					message.Body = "do work"
				})}
			}),
			metadata:     newTestGoalMetadata(),
			wantContains: []string{"YOU HAVE 1 PENDING MESSAGE(S)"},
		},
		{
			name: "withPendingTodos",
			cfg: updated(newTestMultiModelConfig(), func(cfg *multiModelConfig) {
				cfg.agent = "agent1"
				cfg.flowDag = dag
				cfg.dir = t.TempDir()
			}),
			wfState: updated(newTestWorkflow(), func(wfState *state.Workflow) {
				wfState.Status = state.StatusWorking
				wfState.VisitCounts = map[string]int{"agent1": 1}
				wfState.Messages = []state.Message{}
				wfState.Todos = []state.TodoItem{updated(newTestTodoItem(), func(todo *state.TodoItem) {
					todo.Content = "pending task"
					todo.Status = "pending"
					todo.Priority = "high"
				})}
			}),
			metadata:     newTestGoalMetadata(),
			wantContains: []string{"1 pending TODO items"},
		},
		{
			name: "withUnreadOutboxMessages",
			cfg: updated(newTestMultiModelConfig(), func(cfg *multiModelConfig) {
				cfg.agent = "agent1"
				cfg.flowDag = dag
				cfg.dir = t.TempDir()
			}),
			wfState: updated(newTestWorkflow(), func(wfState *state.Workflow) {
				wfState.Status = state.StatusWorking
				wfState.VisitCounts = map[string]int{"agent1": 1}
				wfState.Messages = []state.Message{updated(newTestMessage(), func(message *state.Message) {
					message.ID = 1
					message.FromAgent = "agent1"
					message.ToAgent = "agent2"
					message.Body = "review this"
				})}
			}),
			metadata:     newTestGoalMetadata(),
			wantContains: []string{"messages that haven't been read yet"},
		},
		{
			name: "withUnreadOutboxMessagesFromCurrentModel",
			cfg: updated(newTestMultiModelConfig(), func(cfg *multiModelConfig) {
				cfg.agent = "project-critic-council"
				cfg.flowDag = dag
				cfg.dir = t.TempDir()
			}),
			wfState: updated(newTestWorkflow(), func(wfState *state.Workflow) {
				wfState.Status = state.StatusWorking
				wfState.VisitCounts = map[string]int{"project-critic-council": 1}
				wfState.CurrentModel = "project-critic-council:model-a"
				wfState.Messages = []state.Message{updated(newTestMessage(), func(message *state.Message) {
					message.ID = 1
					message.FromAgent = "project-critic-council:model-a"
					message.ToAgent = "coordinator"
					message.Body = "review this"
				})}
			}),
			metadata: updated(newTestGoalMetadata(), func(metadata *GoalMetadata) {
				metadata.Models = map[string]any{
					"project-critic-council": []any{"model-a", "model-b"},
				}
			}),
			wantContains: []string{"messages that haven't been read yet"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildAgentMessage(&tt.cfg, &tt.wfState, &tt.metadata)
			for _, expected := range tt.wantContains {
				assert.Contains(t, result, expected)
			}
		})
	}
}

func TestBuildAgentEnv(t *testing.T) {
	t.Setenv("SGAI_SHOULD_BE_FILTERED", "1")
	t.Setenv("OPENCODE_CONFIG_DIR", "/tmp/should-not-leak")

	cfg := updated(newTestMultiModelConfig(), func(cfg *multiModelConfig) {
		cfg.agent = "test-agent"
		cfg.dir = "/tmp/test-workspace"
		cfg.mcpURL = "http://127.0.0.1:9999/mcp"
	})

	env, errBuildAgentEnv := buildAgentEnv(&cfg, "")
	require.NoError(t, errBuildAgentEnv)
	envMap := envEntriesToMap(env)

	assert.Equal(t, "/tmp/test-workspace/.sgai", envMap["OPENCODE_CONFIG_DIR"])
	assert.NotEmpty(t, envMap["SGAI_BIN_PATH"])
	assert.Equal(t, "http://127.0.0.1:9999/mcp", envMap["SGAI_MCP_URL"])
	assert.NotContains(t, envMap, "SGAI_SHOULD_BE_FILTERED")
	assert.Equal(t, "test-agent", envMap["SGAI_AGENT_IDENTITY"])
}

func TestBuildAgentEnvUsesCurrentExecutableForSGAIBinPath(t *testing.T) {
	fakeBinDir := t.TempDir()
	fakeSGAIPath := filepath.Join(fakeBinDir, "sgai")
	require.NoError(t, os.WriteFile(fakeSGAIPath, []byte("#!/bin/sh\nexit 0\n"), 0o755))

	t.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SGAI_BIN_PATH", "/tmp/should-not-leak")

	executablePath, errExecutable := os.Executable()
	require.NoError(t, errExecutable)

	cfg := updated(newTestMultiModelConfig(), func(cfg *multiModelConfig) {
		cfg.agent = "test-agent"
		cfg.dir = "/tmp/test-workspace"
		cfg.mcpURL = "http://127.0.0.1:9999/mcp"
	})

	env, errBuildAgentEnv := buildAgentEnv(&cfg, "")
	require.NoError(t, errBuildAgentEnv)
	envMap := envEntriesToMap(env)

	assert.Equal(t, executablePath, envMap["SGAI_BIN_PATH"])
	assert.NotEqual(t, fakeSGAIPath, envMap["SGAI_BIN_PATH"])
}

func TestBuildAgentEnvWithModel(t *testing.T) {
	cfg := updated(newTestMultiModelConfig(), func(cfg *multiModelConfig) {
		cfg.agent = "test-agent"
		cfg.dir = "/tmp/test-workspace"
	})

	env, errBuildAgentEnv := buildAgentEnv(&cfg, "anthropic/claude-opus-4-6 (max)")
	require.NoError(t, errBuildAgentEnv)
	identityValues := envEntriesToMap(env)

	assert.Equal(t, "test-agent|anthropic/claude-opus-4-6|max", identityValues["SGAI_AGENT_IDENTITY"])
}

func envEntriesToMap(env []string) map[string]string {
	values := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		values[key] = value
	}
	return values
}

type failingWriter struct {
	err error
}

func (w *failingWriter) Write(_ []byte) (int, error) {
	return 0, w.err
}

func installTestOpencodeScript(t *testing.T, dir, script string) {
	t.Helper()

	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "opencode"), []byte(script), 0o755))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestExecuteAgentProcessPreservesVariantInAgentIdentity(t *testing.T) {
	tmpDir := t.TempDir()
	installTestOpencodeScript(t, tmpDir, "#!/bin/sh\nprintf '%s' \"$SGAI_AGENT_IDENTITY\" > \"$CAPTURE_FILE\"")

	tests := []struct {
		name         string
		modelSpec    string
		wantIdentity string
	}{
		{
			name:         "maxVariant",
			modelSpec:    "anthropic/claude-opus-4-6 (max)",
			wantIdentity: "test-agent|anthropic/claude-opus-4-6|max",
		},
		{
			name:         "thinkingVariant",
			modelSpec:    "anthropic/claude-opus-4-6 (thinking)",
			wantIdentity: "test-agent|anthropic/claude-opus-4-6|thinking",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capturePath := filepath.Join(tmpDir, tt.name+".txt")
			t.Setenv("CAPTURE_FILE", capturePath)

			cfg := updated(newTestMultiModelConfig(), func(cfg *multiModelConfig) {
				cfg.agent = "test-agent"
				cfg.dir = tmpDir
				cfg.mcpURL = "http://127.0.0.1:7777/mcp"
				cfg.coord = state.NewCoordinatorEmpty(filepath.Join(tmpDir, tt.name+"-state.json"))
			})

			agentArgs := buildAgentArgs(cfg.agent, cfg.agent, tt.modelSpec, "")
			workflow := newTestWorkflow()
			_, _, errExec := executeAgentProcess(context.Background(), &cfg, agentArgs, "", "[test]", newRingWriter(), &workflow)
			require.Nil(t, errExec)

			identity, err := os.ReadFile(capturePath)
			require.NoError(t, err)
			assert.Equal(t, tt.wantIdentity, string(identity))
		})
	}
}

func TestExecuteAgentProcessFlushesBufferedTextOnError(t *testing.T) {
	tmpDir := t.TempDir()
	event, errMarshal := json.Marshal(updated(newTestStreamEvent(), func(event *streamEvent) {
		event.Type = "text"
		event.Part = updated(newTestPart(), func(part *part) {
			part.Text = "final buffered text"
		})
	}))
	require.NoError(t, errMarshal)
	t.Setenv("TEST_AGENT_EVENT", string(event))
	installTestOpencodeScript(t, tmpDir, "#!/bin/sh\nprintf '%s\\n' \"$TEST_AGENT_EVENT\"\nexit 1\n")

	var logBuf bytes.Buffer
	cfg := updated(newTestMultiModelConfig(), func(cfg *multiModelConfig) {
		cfg.agent = "test-agent"
		cfg.dir = tmpDir
		cfg.mcpURL = "http://127.0.0.1:7777/mcp"
		cfg.coord = state.NewCoordinatorEmpty(filepath.Join(tmpDir, "state.json"))
		cfg.logWriter = &logBuf
	})

	workflow := newTestWorkflow()
	_, _, errState := executeAgentProcess(context.Background(), &cfg, []string{"run"}, "", "[test]", newRingWriter(), &workflow)
	require.NotNil(t, errState)
	assert.Contains(t, logBuf.String(), "final buffered text")
}

func TestExecuteAgentProcessFlushesBufferedTextOnInterrupt(t *testing.T) {
	tmpDir := t.TempDir()
	event, errMarshal := json.Marshal(updated(newTestStreamEvent(), func(event *streamEvent) {
		event.Type = "text"
		event.Part = updated(newTestPart(), func(part *part) {
			part.Text = "interrupted buffered text"
		})
	}))
	require.NoError(t, errMarshal)
	readyFile := filepath.Join(tmpDir, "ready")
	t.Setenv("TEST_AGENT_EVENT", string(event))
	t.Setenv("READY_FILE", readyFile)
	installTestOpencodeScript(t, tmpDir, "#!/bin/sh\nprintf '%s\\n' \"$TEST_AGENT_EVENT\"\n: > \"$READY_FILE\"\nsleep 30\n")

	var logBuf bytes.Buffer
	cfg := updated(newTestMultiModelConfig(), func(cfg *multiModelConfig) {
		cfg.agent = "test-agent"
		cfg.dir = tmpDir
		cfg.mcpURL = "http://127.0.0.1:7777/mcp"
		cfg.coord = state.NewCoordinatorEmpty(filepath.Join(tmpDir, "state.json"))
		cfg.logWriter = &logBuf
		cfg.paddedsgai = "test"
	})

	type processResult struct {
		errState *state.Workflow
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ring := newRingWriter()
	workflow := newTestWorkflow()
	resultCh := make(chan processResult, 1)
	go func() {
		_, _, errState := executeAgentProcess(ctx, &cfg, []string{"run"}, "", "[test]", ring, &workflow)
		resultCh <- processResult{errState: errState}
	}()

	require.Eventually(t, func() bool {
		ring.mu.Lock()
		hasOutput := ring.size > 0
		ring.mu.Unlock()
		if !hasOutput {
			return false
		}
		_, errStat := os.Stat(readyFile)
		return errStat == nil
	}, time.Second, 10*time.Millisecond)

	cancel()

	select {
	case result := <-resultCh:
		require.NotNil(t, result.errState)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for interrupted agent process")
	}

	assert.Contains(t, logBuf.String(), "interrupted buffered text")
}

func TestExecuteAgentProcessReportsStderrFlushErrors(t *testing.T) {
	tmpDir := t.TempDir()
	installTestOpencodeScript(t, tmpDir, "#!/bin/sh\nprintf '%s' 'trailing stderr' >&2\nexit 1\n")

	var logBuf bytes.Buffer
	errBoom := errors.New("boom")
	cfg := updated(newTestMultiModelConfig(), func(cfg *multiModelConfig) {
		cfg.agent = "test-agent"
		cfg.dir = tmpDir
		cfg.mcpURL = "http://127.0.0.1:7777/mcp"
		cfg.coord = state.NewCoordinatorEmpty(filepath.Join(tmpDir, "state.json"))
		cfg.logWriter = &logBuf
		cfg.stderrLog = &failingWriter{err: errBoom}
	})

	workflow := newTestWorkflow()
	output := captureDefaultLoggerOutput(t, testLogNow, func() {
		_, _, errState := executeAgentProcess(context.Background(), &cfg, []string{"run"}, "", "[test]", newRingWriter(), &workflow)
		require.NotNil(t, errState)
	})

	assert.Contains(t, output, "failed to flush agent stderr: flush prefixed line: boom")
}

func TestMarkCurrentAgentInSequence(t *testing.T) {
	tests := []struct {
		name         string
		initialSeq   []state.AgentSequenceEntry
		currentAgent string
		expectedLen  int
		expectedLast string
	}{
		{
			name:         "emptySequence",
			initialSeq:   nil,
			currentAgent: "agent1",
			expectedLen:  1,
			expectedLast: "agent1",
		},
		{
			name: "sameAgentAsLast",
			initialSeq: []state.AgentSequenceEntry{
				updated(newTestAgentSequenceEntry(), func(entry *state.AgentSequenceEntry) {
					entry.Agent = "agent1"
				}),
			},
			currentAgent: "agent1",
			expectedLen:  1,
			expectedLast: "agent1",
		},
		{
			name: "differentAgent",
			initialSeq: []state.AgentSequenceEntry{
				updated(newTestAgentSequenceEntry(), func(entry *state.AgentSequenceEntry) {
					entry.Agent = "agent1"
					entry.IsCurrent = true
				}),
			},
			currentAgent: "agent2",
			expectedLen:  2,
			expectedLast: "agent2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf := newTestWorkflow()
			wf.AgentSequence = tt.initialSeq
			markCurrentAgentInSequence(&wf, tt.currentAgent)
			assert.Len(t, wf.AgentSequence, tt.expectedLen)
			last := wf.AgentSequence[len(wf.AgentSequence)-1]
			assert.Equal(t, tt.expectedLast, last.Agent)
			assert.True(t, last.IsCurrent)
		})
	}
}

func TestAddAgentHandoffProgress(t *testing.T) {
	wf := newTestWorkflow()
	wf.Progress = []state.ProgressEntry{}

	addAgentHandoffProgress(&wf, "backend-developer")

	assert.Len(t, wf.Progress, 1)
	assert.Equal(t, "sgai", wf.Progress[0].Agent)
	assert.Contains(t, wf.Progress[0].Description, "Handing off to backend-developer")
}

func TestShouldLogAgent(t *testing.T) {
	t.Run("defaultTrueWhenNoFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		assert.True(t, shouldLogAgent(tmpDir, "nonexistent"))
	})

	t.Run("trueWhenLogIsTrue", func(t *testing.T) {
		tmpDir := t.TempDir()
		agentDir := filepath.Join(tmpDir, ".sgai", "agent")
		require.NoError(t, os.MkdirAll(agentDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(agentDir, "test-agent.md"), []byte("---\nlog: true\n---\n# Agent"), 0o644))

		assert.True(t, shouldLogAgent(tmpDir, "test-agent"))
	})

	t.Run("falseWhenLogIsFalse", func(t *testing.T) {
		tmpDir := t.TempDir()
		agentDir := filepath.Join(tmpDir, ".sgai", "agent")
		require.NoError(t, os.MkdirAll(agentDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(agentDir, "test-agent.md"), []byte("---\nlog: false\n---\n# Agent"), 0o644))

		assert.False(t, shouldLogAgent(tmpDir, "test-agent"))
	})
}

func TestParseAgentSnippets(t *testing.T) {
	t.Run("noAgentFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		result := parseAgentSnippets(tmpDir, "nonexistent")
		assert.Nil(t, result)
	})

	t.Run("agentWithSnippets", func(t *testing.T) {
		tmpDir := t.TempDir()
		agentDir := filepath.Join(tmpDir, ".sgai", "agent")
		require.NoError(t, os.MkdirAll(agentDir, 0o755))
		content := "---\nlog: true\nsnippets:\n  - go/http-server\n  - go/json-encode\n---\n# Agent"
		require.NoError(t, os.WriteFile(filepath.Join(agentDir, "developer.md"), []byte(content), 0o644))

		result := parseAgentSnippets(tmpDir, "developer")
		assert.Equal(t, []string{"go/http-server", "go/json-encode"}, result)
	})
}

func TestParseAgentFileMetadata(t *testing.T) {
	t.Run("noFrontmatter", func(t *testing.T) {
		tmpDir := t.TempDir()
		agentDir := filepath.Join(tmpDir, ".sgai", "agent")
		require.NoError(t, os.MkdirAll(agentDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(agentDir, "test.md"), []byte("# No frontmatter"), 0o644))

		_, ok := parseAgentFileMetadata(tmpDir, "test")
		assert.False(t, ok)
	})
}

func TestValidateModels(t *testing.T) {
	t.Run("emptyModels", func(t *testing.T) {
		err := validateModels(map[string]any{})
		require.NoError(t, err)
	})

	t.Run("nilModels", func(t *testing.T) {
		err := validateModels(nil)
		require.NoError(t, err)
	})
}

func TestReadNewestForkGoal(t *testing.T) {
	t.Run("emptyForks", func(t *testing.T) {
		result := readNewestForkGoal([]workspaceInfo{})
		assert.Empty(t, result)
	})

	t.Run("forkWithGoal", func(t *testing.T) {
		tmpDir := t.TempDir()
		forkDir := filepath.Join(tmpDir, "fork1")
		require.NoError(t, os.MkdirAll(forkDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(forkDir, "GOAL.md"), []byte("# Fork Goal"), 0o644))

		forks := []workspaceInfo{updated(newTestWorkspaceInfo(), func(info *workspaceInfo) {
			info.Directory = forkDir
			info.DirName = "fork1"
		})}
		result := readNewestForkGoal(forks)
		assert.Equal(t, "# Fork Goal", result)
	})

	t.Run("forkWithEmptyGoal", func(t *testing.T) {
		tmpDir := t.TempDir()
		forkDir := filepath.Join(tmpDir, "fork1")
		require.NoError(t, os.MkdirAll(forkDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(forkDir, "GOAL.md"), []byte("   "), 0o644))

		forks := []workspaceInfo{updated(newTestWorkspaceInfo(), func(info *workspaceInfo) {
			info.Directory = forkDir
			info.DirName = "fork1"
		})}
		result := readNewestForkGoal(forks)
		assert.Empty(t, result)
	})

	t.Run("multipleForksSortsByNewest", func(t *testing.T) {
		tmpDir := t.TempDir()

		fork1Dir := filepath.Join(tmpDir, "fork1")
		require.NoError(t, os.MkdirAll(fork1Dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(fork1Dir, "GOAL.md"), []byte("# Old Goal"), 0o644))

		fork2Dir := filepath.Join(tmpDir, "fork2")
		require.NoError(t, os.MkdirAll(fork2Dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(fork2Dir, "GOAL.md"), []byte("# New Goal"), 0o644))

		forks := []workspaceInfo{
			updated(newTestWorkspaceInfo(), func(info *workspaceInfo) {
				info.Directory = fork1Dir
				info.DirName = "fork1"
			}),
			updated(newTestWorkspaceInfo(), func(info *workspaceInfo) {
				info.Directory = fork2Dir
				info.DirName = "fork2"
			}),
		}
		result := readNewestForkGoal(forks)
		assert.NotEmpty(t, result)
	})
}

func TestSplitFrontmatter(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		expectOK     bool
		expectedYAML string
	}{
		{
			name: "validFrontmatter",
			content: `---
key: value
---
content`,
			expectOK:     true,
			expectedYAML: "key: value\n",
		},
		{
			name:         "noFrontmatter",
			content:      "just content",
			expectOK:     false,
			expectedYAML: "",
		},
		{
			name: "unclosedFrontmatter",
			content: `---
key: value
content`,
			expectOK:     false,
			expectedYAML: "",
		},
		{
			name: "emptyFrontmatter",
			content: `---
---
content`,
			expectOK:     true,
			expectedYAML: "",
		},
		{
			name: "multilineFrontmatter",
			content: `---
key1: value1
key2: value2
---
content`,
			expectOK:     true,
			expectedYAML: "key1: value1\nkey2: value2\n",
		},
		{
			name: "openingDelimiterMustEndWithNewline",
			content: `---key: value
---
content`,
			expectOK:     false,
			expectedYAML: "",
		},
		{
			name: "quotedDelimiterSubstringInsideFrontmatterValue",
			content: `---
title: "Quoted --- Title"
flow: test
---
content`,
			expectOK:     true,
			expectedYAML: "title: \"Quoted --- Title\"\nflow: test\n",
		},
		{
			name: "delimiterSubstringInValue",
			content: `---
title: Keep --- title
continuousModePrompt: keep --- prompt
---
content`,
			expectOK:     true,
			expectedYAML: "title: Keep --- title\ncontinuousModePrompt: keep --- prompt\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yamlContent, ok := splitFrontmatter([]byte(tt.content))
			assert.Equal(t, tt.expectOK, ok)
			if tt.expectOK {
				assert.YAMLEq(t, tt.expectedYAML, string(yamlContent))
			}
		})
	}
}

func TestIsTodoTool(t *testing.T) {
	tests := []struct {
		name     string
		tool     string
		expected bool
	}{
		{
			name:     "todowrite",
			tool:     "todowrite",
			expected: true,
		},
		{
			name:     "todoread",
			tool:     "todoread",
			expected: true,
		},
		{
			name:     "sgaiProjectTodowrite",
			tool:     "sgai_project_todowrite",
			expected: true,
		},
		{
			name:     "sgaiProjectTodoread",
			tool:     "sgai_project_todoread",
			expected: true,
		},
		{
			name:     "otherTool",
			tool:     "bash",
			expected: false,
		},
		{
			name:     "emptyTool",
			tool:     "",
			expected: false,
		},
		{
			name:     "similarTool",
			tool:     "todowrites",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTodoTool(tt.tool)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTodoStatusSymbol(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		expected string
	}{
		{
			name:     "pending",
			status:   "pending",
			expected: "○",
		},
		{
			name:     "inProgress",
			status:   "in_progress",
			expected: "◐",
		},
		{
			name:     "completed",
			status:   "completed",
			expected: "●",
		},
		{
			name:     "cancelled",
			status:   "cancelled",
			expected: "✕",
		},
		{
			name:     "unknown",
			status:   "unknown",
			expected: "○",
		},
		{
			name:     "empty",
			status:   "",
			expected: "○",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := todoStatusSymbol(tt.status)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStripMCPTodoPrefix(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected string
	}{
		{
			name:     "withTodosPrefix",
			output:   "todos\n[{\"content\": \"test\"}]",
			expected: "[{\"content\": \"test\"}]",
		},
		{
			name:     "withTodoPrefix",
			output:   "todo\n[{\"content\": \"test\"}]",
			expected: "[{\"content\": \"test\"}]",
		},
		{
			name:     "withSpacedTodosPrefix",
			output:   "  todos  \n[{\"content\": \"test\"}]",
			expected: "[{\"content\": \"test\"}]",
		},
		{
			name:     "withoutPrefix",
			output:   "[{\"content\": \"test\"}]",
			expected: "[{\"content\": \"test\"}]",
		},
		{
			name:     "emptyOutput",
			output:   "",
			expected: "",
		},
		{
			name:     "noNewline",
			output:   "todos",
			expected: "todos",
		},
		{
			name:     "wrongPrefix",
			output:   "other\n[{\"content\": \"test\"}]",
			expected: "other\n[{\"content\": \"test\"}]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripMCPTodoPrefix(tt.output)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatToolCall(t *testing.T) {
	tests := []struct {
		name     string
		tool     string
		input    map[string]any
		expected string
	}{
		{
			name:     "emptyInput",
			tool:     "bash",
			input:    map[string]any{},
			expected: "bash",
		},
		{
			name:     "stringInput",
			tool:     "read",
			input:    map[string]any{"filePath": "/path/to/file"},
			expected: "read(filePath: '/path/to/file')",
		},
		{
			name:     "boolInput",
			tool:     "edit",
			input:    map[string]any{"replaceAll": true},
			expected: "edit(replaceAll: true)",
		},
		{
			name:     "floatInput",
			tool:     "tool",
			input:    map[string]any{"count": float64(42)},
			expected: "tool(count: 42)",
		},
		{
			name:     "truncatesLongString",
			tool:     "tool",
			input:    map[string]any{"content": "this is a very long string that should be truncated because it exceeds the limit"},
			expected: "tool(content: 'this is a very long string that should be trunc...')",
		},
		{
			name:     "doesNotTruncateFilePath",
			tool:     "read",
			input:    map[string]any{"filePath": "/this/is/a/very/long/path/that/should/not/be/truncated/at/all"},
			expected: "read(filePath: '/this/is/a/very/long/path/that/should/not/be/truncated/at/all')",
		},
		{
			name:     "escapesNewlines",
			tool:     "tool",
			input:    map[string]any{"text": "line1\nline2"},
			expected: "tool(text: 'line1\\nline2')",
		},
		{
			name:     "escapesTabs",
			tool:     "tool",
			input:    map[string]any{"text": "col1\tcol2"},
			expected: "tool(text: 'col1\\tcol2')",
		},
		{
			name:     "multipleInputs",
			tool:     "tool",
			input:    map[string]any{"a": "val1", "b": true},
			expected: "tool(a: 'val1', b: true)",
		},
		{
			name:     "intInput",
			tool:     "tool",
			input:    map[string]any{"count": 42},
			expected: "tool(count: 42)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatToolCall(tt.tool, tt.input)
			if tt.name == "multipleInputs" {
				assert.Contains(t, result, "a: 'val1'")
				assert.Contains(t, result, "b: true")
			} else {
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestIsFalsish(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "no",
			input:    "no",
			expected: true,
		},
		{
			name:     "false",
			input:    "false",
			expected: true,
		},
		{
			name:     "zero",
			input:    "0",
			expected: true,
		},
		{
			name:     "off",
			input:    "off",
			expected: true,
		},
		{
			name:     "yes",
			input:    "yes",
			expected: false,
		},
		{
			name:     "true",
			input:    "true",
			expected: false,
		},
		{
			name:     "one",
			input:    "1",
			expected: false,
		},
		{
			name:     "empty",
			input:    "",
			expected: false,
		},
		{
			name:     "uppercaseFalse",
			input:    "FALSE",
			expected: true,
		},
		{
			name:     "spacedFalse",
			input:    " false ",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isFalsish(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResolveBaseAgent(t *testing.T) {
	tests := []struct {
		name      string
		alias     map[string]string
		agentName string
		expected  string
	}{
		{
			name:      "noAlias",
			alias:     nil,
			agentName: "agent1",
			expected:  "agent1",
		},
		{
			name: "hasAlias",
			alias: map[string]string{
				"agent-lite": "agent",
			},
			agentName: "agent-lite",
			expected:  "agent",
		},
		{
			name: "noMatchingAlias",
			alias: map[string]string{
				"other-agent": "agent",
			},
			agentName: "agent1",
			expected:  "agent1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveBaseAgent(tt.alias, tt.agentName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCountPendingTodos(t *testing.T) {
	tests := []struct {
		name     string
		todos    []state.TodoItem
		expected int
	}{
		{
			name:     "emptyTodos",
			todos:    []state.TodoItem{},
			expected: 0,
		},
		{
			name: "pendingTodo",
			todos: []state.TodoItem{updated(newTestTodoItem(), func(todo *state.TodoItem) {
				todo.Content = "Task 1"
				todo.Status = "pending"
			})},
			expected: 1,
		},
		{
			name: "inProgressTodo",
			todos: []state.TodoItem{updated(newTestTodoItem(), func(todo *state.TodoItem) {
				todo.Content = "Task 1"
				todo.Status = "in_progress"
			})},
			expected: 1,
		},
		{
			name: "completedTodo",
			todos: []state.TodoItem{updated(newTestTodoItem(), func(todo *state.TodoItem) {
				todo.Content = "Task 1"
				todo.Status = "completed"
			})},
			expected: 0,
		},
		{
			name: "cancelledTodo",
			todos: []state.TodoItem{updated(newTestTodoItem(), func(todo *state.TodoItem) {
				todo.Content = "Task 1"
				todo.Status = "cancelled"
			})},
			expected: 0,
		},
		{
			name: "mixedTodos",
			todos: []state.TodoItem{
				updated(newTestTodoItem(), func(todo *state.TodoItem) {
					todo.Content = "Task 1"
					todo.Status = "pending"
				}),
				updated(newTestTodoItem(), func(todo *state.TodoItem) {
					todo.Content = "Task 2"
					todo.Status = "completed"
				}),
				updated(newTestTodoItem(), func(todo *state.TodoItem) {
					todo.Content = "Task 3"
					todo.Status = "in_progress"
				}),
				updated(newTestTodoItem(), func(todo *state.TodoItem) {
					todo.Content = "Task 4"
					todo.Status = "cancelled"
				}),
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := countPendingTodos(tt.todos)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTodosForAgent(t *testing.T) {
	wfState := updated(newTestWorkflow(), func(workflow *state.Workflow) {
		workflow.Todos = []state.TodoItem{updated(newTestTodoItem(), func(todo *state.TodoItem) {
			todo.Content = "agent task"
		})}
		workflow.ProjectTodos = []state.TodoItem{updated(newTestTodoItem(), func(todo *state.TodoItem) {
			todo.Content = "project task"
		})}
	})

	t.Run("coordinatorUsesProjectTodos", func(t *testing.T) {
		assert.Equal(t, wfState.ProjectTodos, todosForAgent(&wfState, "coordinator"))
	})

	t.Run("agentUsesAgentTodos", func(t *testing.T) {
		assert.Equal(t, wfState.Todos, todosForAgent(&wfState, "go-developer"))
	})
}

func TestFormatCompletionGateScriptFailureMessage(t *testing.T) {
	script := "make test"
	output := "FAIL: TestSomething\n--- Expected: 1, Actual: 2"

	result := formatCompletionGateScriptFailureMessage(script, output)

	assert.Contains(t, result, "From: environment")
	assert.Contains(t, result, "To: coordinator")
	assert.Contains(t, result, "Subject: computable definition of success has failed")
	assert.Contains(t, result, script)
	assert.Contains(t, result, output)
}

func TestInitVisitCounts(t *testing.T) {
	tests := []struct {
		name     string
		agents   []string
		expected map[string]int
	}{
		{
			name:     "emptyAgents",
			agents:   []string{},
			expected: map[string]int{},
		},
		{
			name:   "singleAgent",
			agents: []string{"agent1"},
			expected: map[string]int{
				"agent1": 0,
			},
		},
		{
			name:   "multipleAgents",
			agents: []string{"agent1", "agent2", "agent3"},
			expected: map[string]int{
				"agent1": 0,
				"agent2": 0,
				"agent3": 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := initVisitCounts(tt.agents)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateRetrospectiveDirName(t *testing.T) {
	result, errName := generateRetrospectiveDirName()
	require.NoError(t, errName)

	assert.Len(t, result, 21)

	now := time.Now()
	expectedPrefix := now.Format("2006-01-02-15-04")
	assert.True(t, strings.HasPrefix(result, expectedPrefix+"."), "expected prefix %s, got %s", expectedPrefix, result)

	suffix := result[len(result)-4:]
	for _, c := range suffix {
		assert.True(t, (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9'), "suffix should be alphanumeric, got %c", c)
	}
}

func TestDotSGAILinePresent(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name:     "present",
			content:  "some content\n/.sgai\nmore content",
			expected: true,
		},
		{
			name:     "presentWithSpaces",
			content:  "some content\n  /.sgai  \nmore content",
			expected: true,
		},
		{
			name:     "notPresent",
			content:  "some content\n/.sgai-other\nmore content",
			expected: false,
		},
		{
			name:     "emptyContent",
			content:  "",
			expected: false,
		},
		{
			name:     "partialMatch",
			content:  "/.sg",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dotSGAILinePresent([]byte(tt.content))
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestJSONPrettyWriterWrite(t *testing.T) {
	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " [test] ")

	event := updated(newTestStreamEvent(), func(event *streamEvent) {
		event.Type = "text"
		event.Part = updated(newTestPart(), func(part *part) {
			part.Text = "hello world"
		})
	})
	data, err := json.Marshal(event)
	require.NoError(t, err)
	data = append(data, '\n')

	n, errWrite := w.Write(data)
	require.NoError(t, errWrite)
	assert.Equal(t, len(data), n)

	w.Flush()
	assert.Contains(t, buf.String(), "hello world")
}

func TestJSONPrettyWriterProcessEventText(t *testing.T) {
	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")

	w.processEvent(updatedPtr(newTestStreamEvent(), func(event *streamEvent) {
		event.Type = "text"
		event.Part = updated(newTestPart(), func(part *part) {
			part.Text = "some text"
		})
	}))
	assert.Equal(t, "some text", w.currentText.String())

	w.Flush()
	assert.Contains(t, buf.String(), "some text")
}

func TestJSONPrettyWriterProcessEventToolPending(t *testing.T) {
	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")

	w.processEvent(updatedPtr(newTestStreamEvent(), func(event *streamEvent) {
		event.Type = "tool"
		event.Part = updated(newTestPart(), func(part *part) {
			part.Tool = "mcp_bash"
			state := updated(newTestToolState(), func(toolState *toolState) {
				toolState.Status = "pending"
				toolState.Input = map[string]any{"command": "ls"}
			})
			part.State = &state
		})
	}))

	output := buf.String()
	assert.Contains(t, output, "mcp_bash")
}

func TestJSONPrettyWriterProcessEventToolRunning(t *testing.T) {
	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")

	w.processEvent(updatedPtr(newTestStreamEvent(), func(event *streamEvent) {
		event.Type = "tool"
		event.Part = updated(newTestPart(), func(part *part) {
			part.Tool = "mcp_read"
			state := updated(newTestToolState(), func(toolState *toolState) {
				toolState.Status = "running"
				toolState.Input = map[string]any{"filePath": "/some/path"}
			})
			part.State = &state
		})
	}))

	output := buf.String()
	assert.Contains(t, output, "mcp_read")
	assert.Contains(t, output, "...")
}

func TestJSONPrettyWriterProcessEventToolCompleted(t *testing.T) {
	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")

	w.processEvent(updatedPtr(newTestStreamEvent(), func(event *streamEvent) {
		event.Type = "tool"
		event.Part = updated(newTestPart(), func(part *part) {
			part.Tool = "mcp_bash"
			state := updated(newTestToolState(), func(toolState *toolState) {
				toolState.Status = "completed"
				toolState.Input = map[string]any{"command": "echo hello"}
				toolState.Output = "hello\nworld"
			})
			part.State = &state
		})
	}))

	output := buf.String()
	assert.Contains(t, output, "mcp_bash")
	assert.Contains(t, output, "→")
}

func TestJSONPrettyWriterProcessEventToolCompletedTodo(t *testing.T) {
	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")

	todos := `[{"content":"task1","status":"completed","priority":"high"},{"content":"task2","status":"pending","priority":"medium"}]`
	w.processEvent(updatedPtr(newTestStreamEvent(), func(event *streamEvent) {
		event.Type = "tool"
		event.Part = updated(newTestPart(), func(part *part) {
			part.Tool = "todowrite"
			state := updated(newTestToolState(), func(toolState *toolState) {
				toolState.Status = "completed"
				toolState.Input = map[string]any{}
				toolState.Output = todos
			})
			part.State = &state
		})
	}))

	output := buf.String()
	assert.Contains(t, output, "●")
	assert.Contains(t, output, "task1")
	assert.Contains(t, output, "○")
	assert.Contains(t, output, "task2")
}

func TestJSONPrettyWriterProcessEventToolCompletedTodoUpdatesWorkflowState(t *testing.T) {
	dir := t.TempDir()
	coord, errCoord := state.NewCoordinatorWith(filepath.Join(dir, "state.json"), newTestWorkflow())
	require.NoError(t, errCoord)

	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")
	w.coord = coord
	w.currentAgent = "test-agent"

	w.processEvent(updatedPtr(newTestStreamEvent(), func(event *streamEvent) {
		event.Type = "tool"
		event.Part = updated(newTestPart(), func(part *part) {
			part.Tool = "todowrite"
			state := updated(newTestToolState(), func(toolState *toolState) {
				toolState.Status = "completed"
				toolState.Input = map[string]any{}
				toolState.Output = `[{"id":"todo-1","content":"trace state","status":"in_progress","priority":"high"}]`
			})
			part.State = &state
		})
	}))

	wfState := coord.State()
	require.Len(t, wfState.Todos, 1)
	assert.Equal(t, "todo-1", wfState.Todos[0].ID)
	assert.Equal(t, "trace state", wfState.Todos[0].Content)
	assert.Equal(t, "in_progress", wfState.Todos[0].Status)
	assert.Equal(t, "high", wfState.Todos[0].Priority)
}

func TestJSONPrettyWriterProcessEventToolError(t *testing.T) {
	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")

	w.processEvent(updatedPtr(newTestStreamEvent(), func(event *streamEvent) {
		event.Type = "tool"
		event.Part = updated(newTestPart(), func(part *part) {
			part.Tool = "mcp_bash"
			state := updated(newTestToolState(), func(toolState *toolState) {
				toolState.Status = "error"
				toolState.Input = map[string]any{}
				toolState.Error = "permission denied"
			})
			part.State = &state
		})
	}))

	output := buf.String()
	assert.Contains(t, output, "ERROR:")
	assert.Contains(t, output, "permission denied")
}

func TestJSONPrettyWriterProcessEventStepStart(t *testing.T) {
	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")

	w.currentText.WriteString("buffered text")
	w.processEvent(updatedPtr(newTestStreamEvent(), func(event *streamEvent) {
		event.Type = "step_start"
	}))

	assert.Equal(t, 1, w.stepCounter)
	assert.Contains(t, buf.String(), "buffered text")
}

func TestJSONPrettyWriterProcessEventReasoning(t *testing.T) {
	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")

	w.processEvent(updatedPtr(newTestStreamEvent(), func(event *streamEvent) {
		event.Type = "reasoning"
		event.Part = updated(newTestPart(), func(part *part) {
			part.Text = "thinking about it"
		})
	}))
	assert.Contains(t, buf.String(), "[thinking]")
}

func TestJSONPrettyWriterProcessEventUnknownType(t *testing.T) {
	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")

	w.processEvent(updatedPtr(newTestStreamEvent(), func(event *streamEvent) {
		event.Type = "custom_event"
	}))
	assert.Contains(t, buf.String(), "[custom_event]")
}

func TestJSONPrettyWriterSessionIDCapture(t *testing.T) {
	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")

	w.processEvent(updatedPtr(newTestStreamEvent(), func(event *streamEvent) {
		event.Type = "text"
		event.SessionID = "sess-123"
		event.Part = updated(newTestPart(), func(part *part) {
			part.Text = "hi"
		})
	}))
	assert.Equal(t, "sess-123", w.sessionID)
}

func TestJSONPrettyWriterFlushEmpty(t *testing.T) {
	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")

	w.Flush()
	assert.Empty(t, buf.String())
}

func TestJSONPrettyWriterProcessBufferMultipleEvents(t *testing.T) {
	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")

	event1, _ := json.Marshal(updated(newTestStreamEvent(), func(event *streamEvent) {
		event.Type = "text"
		event.Part = updated(newTestPart(), func(part *part) {
			part.Text = "hello"
		})
	}))
	event2, _ := json.Marshal(updated(newTestStreamEvent(), func(event *streamEvent) {
		event.Type = "text"
		event.Part = updated(newTestPart(), func(part *part) {
			part.Text = " world"
		})
	}))
	data := string(event1) + "\n" + string(event2) + "\n"

	_, _ = w.Write([]byte(data))
	w.Flush()

	assert.Contains(t, buf.String(), "hello world")
}

func TestJSONPrettyWriterProcessBufferPartialLine(t *testing.T) {
	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")

	event, _ := json.Marshal(updated(newTestStreamEvent(), func(event *streamEvent) {
		event.Type = "text"
		event.Part = updated(newTestPart(), func(part *part) {
			part.Text = "partial"
		})
	}))
	data := string(event)

	_, _ = w.Write([]byte(data[:10]))
	assert.Empty(t, buf.String())

	_, _ = w.Write([]byte(data[10:] + "\n"))
	w.Flush()
	assert.Contains(t, buf.String(), "partial")
}

func TestJSONPrettyWriterRecordStepCost(t *testing.T) {
	dir := t.TempDir()
	coord, err := state.NewCoordinatorWith(filepath.Join(dir, "state.json"), newTestWorkflow())
	require.NoError(t, err)

	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")
	w.coord = coord
	w.currentAgent = "test-agent"
	w.stepCounter = 1

	w.recordStepCost(updatedPtr(newTestPart(), func(part *part) {
		part.Cost = 0.05
		part.Tokens = updated(newTestPartTokens(), func(tokens *partTokens) {
			tokens.Input = 100
			tokens.Output = 50
		})
	}),

		time.Now().UnixMilli())

	wfState := coord.State()
	assert.InDelta(t, 0.05, wfState.Cost.TotalCost, 0.001)
	assert.Equal(t, 100, wfState.Cost.TotalTokens.Input)
	assert.Equal(t, 50, wfState.Cost.TotalTokens.Output)
	assert.InDelta(t, 0.033333, wfState.Cost.Dollars.Input, 0.0001)
	assert.InDelta(t, 0.016667, wfState.Cost.Dollars.Output, 0.0001)
	assert.InDelta(t, 0.05, wfState.Cost.Dollars.Total, 0.0001)
	assert.Len(t, wfState.Cost.ByAgent, 1)
	assert.Equal(t, "test-agent", wfState.Cost.ByAgent[0].Agent)
}

func TestJSONPrettyWriterRecordStepCostTracksDollarBreakdown(t *testing.T) {
	dir := t.TempDir()
	coord, err := state.NewCoordinatorWith(filepath.Join(dir, "state.json"), newTestWorkflow())
	require.NoError(t, err)

	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")
	w.coord = coord
	w.currentAgent = "test-agent"
	w.stepCounter = 1

	w.recordStepCost(updatedPtr(newTestPart(), func(part *part) {
		part.Cost = 1.2
		part.Tokens = updated(newTestPartTokens(), func(tokens *partTokens) {
			tokens.Input = 40
			tokens.Output = 20
			tokens.Reasoning = 10
			tokens.Cache = cacheStats{Read: 20, Write: 10}
		})
	}),

		time.Now().UnixMilli())

	wfState := coord.State()
	assert.InDelta(t, 0.48, wfState.Cost.Dollars.Input, 0.0001)
	assert.InDelta(t, 0.24, wfState.Cost.Dollars.Output, 0.0001)
	assert.InDelta(t, 0.12, wfState.Cost.Dollars.Reasoning, 0.0001)
	assert.InDelta(t, 0.24, wfState.Cost.Dollars.CacheRead, 0.0001)
	assert.InDelta(t, 0.12, wfState.Cost.Dollars.CacheWrite, 0.0001)
	assert.InDelta(t, 1.2, wfState.Cost.Dollars.Total, 0.0001)
	require.Len(t, wfState.Cost.ByAgent, 1)
	assert.InDelta(t, 1.2, wfState.Cost.ByAgent[0].Dollars.Total, 0.0001)
	assert.InDelta(t, 0.12, wfState.Cost.ByAgent[0].Steps[0].Dollars.Reasoning, 0.0001)
}

func TestJSONPrettyWriterRecordStepCostMultipleSteps(t *testing.T) {
	dir := t.TempDir()
	coord, err := state.NewCoordinatorWith(filepath.Join(dir, "state.json"), newTestWorkflow())
	require.NoError(t, err)

	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")
	w.coord = coord
	w.currentAgent = "test-agent"
	w.stepCounter = 1

	w.recordStepCost(updatedPtr(newTestPart(), func(part *part) {
		part.Cost = 0.01
		part.Tokens = updated(newTestPartTokens(), func(tokens *partTokens) {
			tokens.Input = 10
			tokens.Output = 5
		})
	}),

		time.Now().UnixMilli())
	w.stepCounter++
	w.recordStepCost(updatedPtr(newTestPart(), func(part *part) {
		part.Cost = 0.02
		part.Tokens = updated(newTestPartTokens(), func(tokens *partTokens) {
			tokens.Input = 20
			tokens.Output = 10
		})
	}),

		time.Now().UnixMilli())

	wfState := coord.State()
	assert.InDelta(t, 0.03, wfState.Cost.TotalCost, 0.001)
	assert.Len(t, wfState.Cost.ByAgent[0].Steps, 2)
}

func TestJSONPrettyWriterRecordStepCostNilCoord(_ *testing.T) {
	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")
	w.currentAgent = "test-agent"

	w.recordStepCost(updatedPtr(newTestPart(), func(part *part) {
		part.Cost = 0.05
	}),

		time.Now().UnixMilli())
}

func TestJSONPrettyWriterRecordStepCostEmptyAgent(t *testing.T) {
	dir := t.TempDir()
	coord, err := state.NewCoordinatorWith(filepath.Join(dir, "state.json"), newTestWorkflow())
	require.NoError(t, err)

	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")
	w.coord = coord

	w.recordStepCost(updatedPtr(newTestPart(), func(part *part) {
		part.Cost = 0.05
	}),

		time.Now().UnixMilli())
	wfState := coord.State()
	assert.InDelta(t, 0.0, wfState.Cost.TotalCost, 0.001)
}

func TestJSONPrettyWriterRecordStepCostZeroValues(t *testing.T) {
	dir := t.TempDir()
	coord, err := state.NewCoordinatorWith(filepath.Join(dir, "state.json"), newTestWorkflow())
	require.NoError(t, err)

	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")
	w.coord = coord
	w.currentAgent = "agent"

	w.recordStepCost(updatedPtr(newTestPart(), func(part *part) {
		part.Tokens = newTestPartTokens()
	}),

		time.Now().UnixMilli())
	wfState := coord.State()
	assert.InDelta(t, 0.0, wfState.Cost.TotalCost, 0.001)
}

func TestJSONPrettyWriterRecordStepCostCachedTokensWithoutCost(t *testing.T) {
	dir := t.TempDir()
	coord, err := state.NewCoordinatorWith(filepath.Join(dir, "state.json"), newTestWorkflow())
	require.NoError(t, err)

	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")
	w.coord = coord
	w.currentAgent = "agent"
	w.stepCounter = 3

	w.recordStepCost(updatedPtr(newTestPart(), func(part *part) {
		part.Tokens = updated(newTestPartTokens(), func(tokens *partTokens) {
			tokens.Cache = cacheStats{Read: 200, Write: 50}
		})
	}),

		time.Now().UnixMilli())

	wfState := coord.State()
	assert.Equal(t, 200, wfState.Cost.TotalTokens.CacheRead)
	assert.Equal(t, 50, wfState.Cost.TotalTokens.CacheWrite)
	require.Len(t, wfState.Cost.ByAgent, 1)
	assert.Equal(t, 200, wfState.Cost.ByAgent[0].Tokens.CacheRead)
	assert.Equal(t, 50, wfState.Cost.ByAgent[0].Tokens.CacheWrite)
	assert.Len(t, wfState.Cost.ByAgent[0].Steps, 1)
}

func TestJSONPrettyWriterRecordStepCostReasoningTokensWithoutCost(t *testing.T) {
	dir := t.TempDir()
	coord, err := state.NewCoordinatorWith(filepath.Join(dir, "state.json"), newTestWorkflow())
	require.NoError(t, err)

	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")
	w.coord = coord
	w.currentAgent = "agent"
	w.stepCounter = 4

	w.recordStepCost(updatedPtr(newTestPart(), func(part *part) {
		part.Tokens = updated(newTestPartTokens(), func(tokens *partTokens) {
			tokens.Reasoning = 125
		})
	}),

		time.Now().UnixMilli())

	wfState := coord.State()
	assert.Equal(t, 125, wfState.Cost.TotalTokens.Reasoning)
	require.Len(t, wfState.Cost.ByAgent, 1)
	assert.Equal(t, 125, wfState.Cost.ByAgent[0].Tokens.Reasoning)
	assert.Len(t, wfState.Cost.ByAgent[0].Steps, 1)
}

func TestJSONPrettyWriterFormatTodoOutput(t *testing.T) {
	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")

	todos := `[{"content":"write tests","status":"in_progress","priority":"high"},{"content":"fix bug","status":"completed","priority":"medium"},{"content":"deploy","status":"cancelled","priority":"low"}]`
	w.formatTodoOutput(todos)

	output := buf.String()
	assert.Contains(t, output, "◐")
	assert.Contains(t, output, "write tests")
	assert.Contains(t, output, "●")
	assert.Contains(t, output, "fix bug")
	assert.Contains(t, output, "✕")
	assert.Contains(t, output, "deploy")
}

func TestJSONPrettyWriterFormatTodoOutputInvalidJSON(t *testing.T) {
	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")

	w.formatTodoOutput("not json at all")

	output := buf.String()
	assert.Contains(t, output, "→")
	assert.Contains(t, output, "not json at all")
}

func TestJSONPrettyWriterFormatTodoOutputWithPrefix(t *testing.T) {
	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")

	todos := "Updated todos\n" + `[{"content":"task","status":"pending","priority":"high"}]`
	w.formatTodoOutput(todos)

	output := buf.String()
	assert.Contains(t, output, "○")
	assert.Contains(t, output, "task")
}

func TestJSONPrettyWriterProcessEventStepFinish(t *testing.T) {
	dir := t.TempDir()
	coord, err := state.NewCoordinatorWith(filepath.Join(dir, "state.json"), newTestWorkflow())
	require.NoError(t, err)

	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")
	w.coord = coord
	w.currentAgent = "test-agent"
	w.stepCounter = 1

	w.currentText.WriteString("some text")
	w.processEvent(updatedPtr(newTestStreamEvent(), func(event *streamEvent) {
		event.Type = "step_finish"
		event.Timestamp = time.Now().UnixMilli()
		event.Part = updated(newTestPart(), func(part *part) {
			part.Cost = 0.1
			part.Tokens = updated(newTestPartTokens(), func(tokens *partTokens) {
				tokens.Input = 500
				tokens.Output = 200
			})
		})
	}))

	assert.Contains(t, buf.String(), "some text")
	wfState := coord.State()
	assert.InDelta(t, 0.1, wfState.Cost.TotalCost, 0.01)
}

func TestJSONPrettyWriterProcessEventHyphenatedStepEvents(t *testing.T) {
	dir := t.TempDir()
	coord, err := state.NewCoordinatorWith(filepath.Join(dir, "state.json"), newTestWorkflow())
	require.NoError(t, err)

	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")
	w.coord = coord
	w.currentAgent = "test-agent"

	w.processEvent(updatedPtr(newTestStreamEvent(), func(event *streamEvent) {
		event.Type = "step-start"
	}))
	w.processEvent(updatedPtr(newTestStreamEvent(), func(event *streamEvent) {
		event.Type = "step-finish"
		event.Timestamp = time.Now().UnixMilli()
		event.Part = updated(newTestPart(), func(part *part) {
			part.Cost = 0.2
			part.Tokens = updated(newTestPartTokens(), func(tokens *partTokens) {
				tokens.Input = 120
				tokens.Output = 30
				tokens.Reasoning = 10
				tokens.Cache = cacheStats{Read: 40, Write: 5}
			})
		})
	}))

	wfState := coord.State()
	require.Len(t, wfState.Cost.ByAgent, 1)
	assert.InDelta(t, 0.2, wfState.Cost.TotalCost, 0.0001)
	assert.Equal(t, 120, wfState.Cost.TotalTokens.Input)
	assert.Equal(t, 30, wfState.Cost.TotalTokens.Output)
	assert.Equal(t, 10, wfState.Cost.TotalTokens.Reasoning)
	assert.Equal(t, 40, wfState.Cost.TotalTokens.CacheRead)
	assert.Equal(t, 5, wfState.Cost.TotalTokens.CacheWrite)
	assert.Equal(t, "test-agent-step-1", wfState.Cost.ByAgent[0].Steps[0].StepID)
	assert.InDelta(t, 0.2, wfState.Cost.ByAgent[0].Steps[0].Dollars.Total, 0.0001)
}

func TestJSONPrettyWriterToolNilState(t *testing.T) {
	var buf bytes.Buffer
	w := newBufferedTestJSONPrettyWriter(&buf, " ")

	w.processEvent(updatedPtr(newTestStreamEvent(), func(event *streamEvent) {
		event.Type = "tool"
		event.Part = updated(newTestPart(), func(part *part) {
			part.Tool = "mcp_bash"
		})
	}))

	assert.Empty(t, buf.String())
}

func TestPrefixWriter(t *testing.T) {
	var buf bytes.Buffer
	w := newPrefixWriter(" [test] ", &buf, testLogNow)

	n, err := w.Write([]byte("hello\nworld\n"))
	require.NoError(t, err)
	assert.Equal(t, 12, n)
	assert.Equal(t, testLogTimestamp()+" [test] hello\n"+testLogTimestamp()+" [test] world\n", buf.String())
}

func TestPrefixWriterSingleLine(t *testing.T) {
	var buf bytes.Buffer
	w := newPrefixWriter(" [p] ", &buf, testLogNow)

	_, _ = w.Write([]byte("single line\n"))
	assert.Equal(t, testLogTimestamp()+" [p] single line\n", buf.String())
}

func TestPrefixWriterWithoutLabelUsesSpaceAfterTime(t *testing.T) {
	var buf bytes.Buffer
	w := newPrefixWriter("", &buf, testLogNow)

	_, _ = w.Write([]byte("hello\n"))
	assert.Equal(t, testLogTimestamp()+" hello\n", buf.String())
}

func TestPrefixWriterBuffersPartialWritesUntilNewline(t *testing.T) {
	var buf bytes.Buffer
	w := newPrefixWriter(" [test] ", &buf, testLogNow)

	n, err := w.Write([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Empty(t, buf.String())

	n, err = w.Write([]byte(" world\nnext"))
	require.NoError(t, err)
	assert.Equal(t, 11, n)
	assert.Equal(t, testLogTimestamp()+" [test] hello world\n", buf.String())

	n, err = w.Write([]byte(" line\n"))
	require.NoError(t, err)
	assert.Equal(t, 6, n)
	assert.Equal(t, testLogTimestamp()+" [test] hello world\n"+testLogTimestamp()+" [test] next line\n", buf.String())
}

func TestPrefixWriterFlushTrimsTrailingCarriageReturnAtEOF(t *testing.T) {
	var buf bytes.Buffer
	w := newPrefixWriter(" [test] ", &buf, testLogNow)

	_, errWrite := w.Write([]byte("hello\r"))
	require.NoError(t, errWrite)

	errFlush := w.Flush()
	require.NoError(t, errFlush)
	assert.Equal(t, testLogTimestamp()+" [test] hello\n", buf.String())
}

func TestConfigureSgaiLoggerPrefixesLogOutput(t *testing.T) {
	var buf bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	t.Cleanup(func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	})

	configureSgaiLogger(&buf)
	log.Println("logger output")

	assert.Regexp(t, `^\[\d{2}:\d{2}:\d{2}\] logger output\n$`, buf.String())
}

func TestHandleWorkingLoopLogsViaDefaultLogger(t *testing.T) {
	cfg := newTestMultiModelConfig()
	cfg.paddedsgai = "test"
	cfg.agent = "builder"
	sessionID := "session-123"

	output := captureDefaultLoggerOutput(t, testLogNow, func() {
		assert.Equal(t, 1, handleWorkingLoop(&cfg, &sessionID, 0))
	})

	assert.Equal(t, "session-123", sessionID)
	assert.Equal(t, timestampedTestOutput(" ", "[test] agent builder still working, re-running..."), output)
}

func captureDefaultLoggerOutput(t *testing.T, now func() time.Time, fn func()) string {
	t.Helper()

	var buf bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetFlags(0)
	log.SetOutput(newPrefixWriter("", &buf, now))
	t.Cleanup(func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	})

	fn()
	return buf.String()
}

func TestCopyCompletionArtifactsToRetrospectiveNoDir(t *testing.T) {
	cfg := newTestMultiModelConfig()
	require.NoError(t, copyCompletionArtifactsToRetrospective(&cfg))
}

func TestInitializeWorkspaceDirExisting(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai"), 0o755))
	err := initializeWorkspaceDir(dir)
	require.NoError(t, err)
}

func TestCopyFileAtomicSuccess(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "source.txt")
	dstFile := filepath.Join(dstDir, "dest.txt")
	require.NoError(t, os.WriteFile(srcFile, []byte("hello world"), 0o644))
	err := copyFileAtomic(srcFile, dstFile)
	require.NoError(t, err)
	data, errRead := os.ReadFile(dstFile)
	require.NoError(t, errRead)
	assert.Equal(t, "hello world", string(data))
}

func TestCopyFileAtomicMissingSource(t *testing.T) {
	err := copyFileAtomic("/nonexistent/source.txt", filepath.Join(t.TempDir(), "dest.txt"))
	require.Error(t, err)
}

func TestCopyFileAtomicCreatesDir(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "source.txt")
	dstFile := filepath.Join(dstDir, "subdir", "dest.txt")
	require.NoError(t, os.WriteFile(srcFile, []byte("nested"), 0o644))
	err := copyFileAtomic(srcFile, dstFile)
	require.NoError(t, err)
	data, errRead := os.ReadFile(dstFile)
	require.NoError(t, errRead)
	assert.Equal(t, "nested", string(data))
}

func TestCopyFinalStateToRetrospectiveNoFilesNewBatch(t *testing.T) {
	dir := t.TempDir()
	retroDir := t.TempDir()
	err := copyFinalStateToRetrospective(dir, retroDir)
	require.NoError(t, err)
}

func TestRunCompletionGateScriptSuccess(t *testing.T) {
	dir := t.TempDir()
	output, err := runCompletionGateScript(context.Background(), dir, "echo 'hello'")
	require.NoError(t, err)
	assert.Contains(t, output, "hello")
}

func TestRunCompletionGateScriptFailure(t *testing.T) {
	dir := t.TempDir()
	_, err := runCompletionGateScript(context.Background(), dir, "exit 1")
	require.Error(t, err)
}

func TestSaveStateCoord(t *testing.T) {
	dir := t.TempDir()
	sgaiDir := filepath.Join(dir, ".sgai")
	require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
	stateFile := filepath.Join(sgaiDir, "state.json")
	coord, errCoord := state.NewCoordinatorWith(stateFile, newTestWorkflow())
	require.NoError(t, errCoord)
	wf := coord.State()
	require.NoError(t, saveState(coord, &wf))
	assert.FileExists(t, stateFile)
}

func TestOpenRetrospectiveLogsSuccess(t *testing.T) {
	dir := t.TempDir()
	stdoutLog, stderrLog, err := openRetrospectiveLogs(dir)
	require.NoError(t, err)
	require.NotNil(t, stdoutLog)
	require.NotNil(t, stderrLog)
	t.Cleanup(func() {
		_ = stdoutLog.Close()
		_ = stderrLog.Close()
	})
	assert.FileExists(t, filepath.Join(dir, "stdout.log"))
	assert.FileExists(t, filepath.Join(dir, "stderr.log"))
}

func TestOpenRetrospectiveLogsInvalidDir(t *testing.T) {
	_, _, err := openRetrospectiveLogs("/nonexistent/path/to/retro")
	require.Error(t, err)
}

func TestOpenRetrospectiveLogsWritable(t *testing.T) {
	dir := t.TempDir()
	stdoutLog, stderrLog, err := openRetrospectiveLogs(dir)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = stdoutLog.Close()
		_ = stderrLog.Close()
	})
	_, errWrite := stdoutLog.Write([]byte("stdout test\n"))
	require.NoError(t, errWrite)
	_, errWrite2 := stderrLog.Write([]byte("stderr test\n"))
	require.NoError(t, errWrite2)
}

func TestHandleCompleteStatusCoordinatorNoBlockers(t *testing.T) {
	dir := t.TempDir()
	sgaiDir := filepath.Join(dir, ".sgai")
	require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
	statePath := filepath.Join(sgaiDir, "state.json")
	coord, errCoord := state.NewCoordinatorWith(statePath, newTestWorkflow())
	require.NoError(t, errCoord)

	d := newTestDag()
	cfg := updated(newTestMultiModelConfig(), func(cfg *multiModelConfig) {
		cfg.paddedsgai = "test"
		cfg.coord = coord
		cfg.dir = dir
		cfg.agent = "coordinator"
		cfg.flowDag = d
		cfg.goalPath = filepath.Join(dir, "GOAL.md")
	})
	require.NoError(t, os.WriteFile(cfg.goalPath, []byte("# Goal"), 0o644))
	metadata := newTestGoalMetadata()
	result := handleCompleteStatus(
		context.Background(),
		&cfg,
		updatedPtr(newTestWorkflow(), func(wfState *state.Workflow) {
			wfState.Status = state.StatusComplete
		}),

		&metadata,
	)
	assert.Equal(t, state.StatusComplete, result.Status)
}

func TestTerminateProcessGroupUsesProcessGroupID(t *testing.T) {
	exited := make(chan struct{})
	var pids []int
	var signals []syscall.Signal

	cmd := exec.Command("true")
	cmd.Process = &os.Process{Pid: 42}
	terminateProcessGroup(cmd, exited, func(<-chan struct{}) bool {
		close(exited)
		return false
	}, func(pid int, sig syscall.Signal) error {
		pids = append(pids, pid)
		signals = append(signals, sig)
		return nil
	})

	assert.Equal(t, []int{-42}, pids)
	assert.Equal(t, []syscall.Signal{syscall.SIGTERM}, signals)
}

func TestTerminateProcessGroupSkipsSignalsWhenProcessAlreadyExited(t *testing.T) {
	exited := make(chan struct{})
	close(exited)

	var signals []syscall.Signal
	cmd := exec.Command("true")
	cmd.Process = &os.Process{Pid: 42}
	terminateProcessGroup(cmd, exited, func(<-chan struct{}) bool {
		t.Fatal("unexpected grace-period wait")
		return false
	}, func(_ int, sig syscall.Signal) error {
		signals = append(signals, sig)
		return nil
	})

	assert.Empty(t, signals)
}

func TestTerminateProcessGroupSkipsEscalationAfterProcessExit(t *testing.T) {
	exited := make(chan struct{})

	var signals []syscall.Signal
	cmd := exec.Command("true")
	cmd.Process = &os.Process{Pid: 42}
	terminateProcessGroup(cmd, exited, func(<-chan struct{}) bool {
		close(exited)
		return false
	}, func(_ int, sig syscall.Signal) error {
		signals = append(signals, sig)
		return nil
	})

	assert.Equal(t, []syscall.Signal{syscall.SIGTERM}, signals)
}

func TestExportSessionMissingBinary(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai"), 0o755))
	err := exportSession(dir, "session-1", filepath.Join(dir, "output.json"))
	require.Error(t, err)
}

func TestRunCompletionGateScriptCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := runCompletionGateScript(ctx, t.TempDir(), "echo hello")
	require.Error(t, err)
}

func TestFormatCompletionGateScriptFailureMessageContent(t *testing.T) {
	msg := formatCompletionGateScriptFailureMessage("make test", "FAIL: tests failed")
	assert.Contains(t, msg, "make test")
	assert.Contains(t, msg, "FAIL: tests failed")
}

func TestParseAgentFileMetadataValidFile(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, ".sgai", "agent")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))
	agentFile := filepath.Join(agentDir, "test-agent.md")
	content := "---\nlog: true\nsnippets:\n  - go\n---\n# Test Agent\nAgent instructions here"
	require.NoError(t, os.WriteFile(agentFile, []byte(content), 0o644))

	meta, ok := parseAgentFileMetadata(dir, "test-agent")
	assert.True(t, ok)
	assert.True(t, meta.Log)
	assert.Contains(t, meta.Snippets, "go")
}

func TestParseAgentFileMetadataMissing(t *testing.T) {
	_, ok := parseAgentFileMetadata(t.TempDir(), "nonexistent")
	assert.False(t, ok)
}

func TestParseAgentFileMetadataNoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, ".sgai", "agent")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "test.md"), []byte("no frontmatter"), 0o644))

	_, ok := parseAgentFileMetadata(dir, "test")
	assert.False(t, ok)
}

func TestShouldLogAgentDefault(t *testing.T) {
	result := shouldLogAgent(t.TempDir(), "nonexistent")
	assert.True(t, result)
}

func TestShouldLogAgentExplicit(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, ".sgai", "agent")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "quiet.md"), []byte("---\nlog: false\n---\n"), 0o644))

	result := shouldLogAgent(dir, "quiet")
	assert.False(t, result)
}

func TestParseAgentSnippetsEmpty(t *testing.T) {
	result := parseAgentSnippets(t.TempDir(), "nonexistent")
	assert.Nil(t, result)
}

func TestParseAgentSnippetsPopulated(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, ".sgai", "agent")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "dev.md"), []byte("---\nsnippets:\n  - go\n  - react\n---\n"), 0o644))

	result := parseAgentSnippets(dir, "dev")
	assert.Equal(t, []string{"go", "react"}, result)
}

func TestBlockCompletionOnGateScriptPassingScript(t *testing.T) {
	dir := t.TempDir()
	sp := filepath.Join(dir, ".sgai", "state.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(sp), 0o755))
	coord := state.NewCoordinatorEmpty(sp)

	cfg := updated(newTestMultiModelConfig(), func(cfg *multiModelConfig) {
		cfg.paddedsgai = "sgai"
		cfg.agent = "test"
		cfg.dir = dir
		cfg.coord = coord
	})
	metadata := updated(newTestGoalMetadata(), func(metadata *GoalMetadata) {
		metadata.CompletionGateScript = "true"
	})
	wfState := updated(newTestWorkflow(), func(wfState *state.Workflow) {
		wfState.Status = state.StatusComplete
	})
	result := blockCompletionOnGateScript(context.Background(), &cfg, &wfState, &metadata)
	assert.Nil(t, result)
}

func TestBlockCompletionOnGateScriptFailingScript(t *testing.T) {
	dir := t.TempDir()
	sp := filepath.Join(dir, ".sgai", "state.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(sp), 0o755))
	coord := state.NewCoordinatorEmpty(sp)

	cfg := updated(newTestMultiModelConfig(), func(cfg *multiModelConfig) {
		cfg.paddedsgai = "sgai"
		cfg.agent = "test"
		cfg.dir = dir
		cfg.coord = coord
	})
	metadata := updated(newTestGoalMetadata(), func(metadata *GoalMetadata) {
		metadata.CompletionGateScript = "false"
	})
	wfState := updated(newTestWorkflow(), func(wfState *state.Workflow) {
		wfState.Status = state.StatusComplete
	})
	result := blockCompletionOnGateScript(context.Background(), &cfg, &wfState, &metadata)
	require.NotNil(t, result)
	assert.Equal(t, state.StatusWorking, result.Status)
}

func TestCopyCompletionArtifactsWithPM(t *testing.T) {
	dir := t.TempDir()
	retroDir := t.TempDir()
	goalPath := filepath.Join(dir, "GOAL.md")
	require.NoError(t, os.WriteFile(goalPath, []byte("# Goal"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".sgai", "PROJECT_MANAGEMENT.md"), []byte("# PM"), 0o644))

	cfg := updated(newTestMultiModelConfig(), func(cfg *multiModelConfig) {
		cfg.dir = dir
		cfg.goalPath = goalPath
		cfg.retrospectiveDir = retroDir
	})
	require.NoError(t, copyCompletionArtifactsToRetrospective(&cfg))

	_, errGoal := os.Stat(filepath.Join(retroDir, "GOAL.md"))
	require.NoError(t, errGoal)
	_, errPM := os.Stat(filepath.Join(retroDir, "PROJECT_MANAGEMENT.md"))
	require.NoError(t, errPM)
}

func TestHandleWorkingLoopReset(t *testing.T) {
	cfg := updated(newTestMultiModelConfig(), func(cfg *multiModelConfig) {
		cfg.paddedsgai = "sgai"
		cfg.agent = "test"
	})
	sessionID := "session-1"
	result := handleWorkingLoop(&cfg, &sessionID, maxConsecutiveWorkingIterations-1)
	assert.Equal(t, 0, result)
	assert.Empty(t, sessionID)
}

func TestSaveStatePersistence(t *testing.T) {
	dir := t.TempDir()
	sp := filepath.Join(dir, ".sgai", "state.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(sp), 0o755))
	coord := state.NewCoordinatorEmpty(sp)

	wf := updated(newTestWorkflow(), func(wfState *state.Workflow) {
		wfState.Status = state.StatusWorking
		wfState.Task = "testing"
	})
	require.NoError(t, saveState(coord, &wf))

	loaded := coord.State()
	assert.Equal(t, state.StatusWorking, loaded.Status)
	assert.Equal(t, "testing", loaded.Task)
}

func TestCopyFileAtomicMissingSrc(t *testing.T) {
	err := copyFileAtomic("/nonexistent/source.txt", filepath.Join(t.TempDir(), "dest.txt"))
	require.Error(t, err)
}

func TestCopyFinalStateToRetrospectiveNoFiles(t *testing.T) {
	dir := t.TempDir()
	retroDir := filepath.Join(t.TempDir(), "retro")
	err := copyFinalStateToRetrospective(dir, retroDir)
	require.NoError(t, err)
}

func TestApplyLayerFolderOverlayNoLayerDir(t *testing.T) {
	dir := t.TempDir()
	err := applyLayerFolderOverlay(dir)
	require.NoError(t, err)
}

func TestApplyLayerFolderOverlayWithFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai", "skills"), 0o755))
	layerDir := filepath.Join(dir, "sgai", "skills", "my-skill")
	require.NoError(t, os.MkdirAll(layerDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(layerDir, "SKILL.md"), []byte("# My Skill"), 0o644))
	err := applyLayerFolderOverlay(dir)
	require.NoError(t, err)
	content, errRead := os.ReadFile(filepath.Join(dir, ".sgai", "skills", "my-skill", "SKILL.md"))
	require.NoError(t, errRead)
	assert.Equal(t, "# My Skill", string(content))
}

func TestApplyLayerFolderOverlayProtectedCoordinator(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai", "agent"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".sgai", "agent", "coordinator.md"), []byte("# Original"), 0o644))
	layerDir := filepath.Join(dir, "sgai", "agent")
	require.NoError(t, os.MkdirAll(layerDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(layerDir, "coordinator.md"), []byte("# Overlay"), 0o644))
	err := applyLayerFolderOverlay(dir)
	require.NoError(t, err)
	content, errRead := os.ReadFile(filepath.Join(dir, ".sgai", "agent", "coordinator.md"))
	require.NoError(t, errRead)
	assert.Equal(t, "# Original", string(content))
}

func TestCopyLayerSubfolderWithProtected(t *testing.T) {
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src", "agent")
	dstDir := filepath.Join(dir, "dst", "agent")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	require.NoError(t, os.MkdirAll(dstDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "coordinator.md"), []byte("# Override"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "builder.md"), []byte("# Builder"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dstDir, "coordinator.md"), []byte("# Original"), 0o644))
	err := copyLayerSubfolder(dir, srcDir, dstDir, "agent")
	require.NoError(t, err)
	coordContent, errReadCoord := os.ReadFile(filepath.Join(dstDir, "coordinator.md"))
	require.NoError(t, errReadCoord)
	assert.Equal(t, "# Original", string(coordContent))
	builderContent, errReadBuilder := os.ReadFile(filepath.Join(dstDir, "builder.md"))
	require.NoError(t, errReadBuilder)
	assert.Equal(t, "# Builder", string(builderContent))
}

func TestCopyLayerSubfolderRejectsSymlinkedDestination(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior differs on windows")
	}

	workspaceDir := t.TempDir()
	srcDir := filepath.Join(workspaceDir, "sgai", "skills")
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "demo"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "demo", "SKILL.md"), []byte("# skill"), 0o644))

	externalDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceDir, ".sgai"), 0o755))
	require.NoError(t, os.Symlink(externalDir, filepath.Join(workspaceDir, ".sgai", "skills")))

	err := copyLayerSubfolder(workspaceDir, srcDir, filepath.Join(workspaceDir, ".sgai", "skills"), "skills")
	require.Error(t, err)
	require.ErrorContains(t, err, "symlinked path is not allowed")
	assert.NoFileExists(t, filepath.Join(externalDir, "demo", "SKILL.md"))
}

func TestCopyLayerSubfolderRejectsSymlinkedDestinationFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior differs on windows")
	}

	workspaceDir := t.TempDir()
	srcDir := filepath.Join(workspaceDir, "sgai", "skills")
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "demo"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "demo", "SKILL.md"), []byte("# overlay"), 0o644))

	externalDir := t.TempDir()
	externalFile := filepath.Join(externalDir, "SKILL.md")
	require.NoError(t, os.WriteFile(externalFile, []byte("# external"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceDir, ".sgai", "skills", "demo"), 0o755))
	require.NoError(t, os.Symlink(externalFile, filepath.Join(workspaceDir, ".sgai", "skills", "demo", "SKILL.md")))

	err := copyLayerSubfolder(workspaceDir, srcDir, filepath.Join(workspaceDir, ".sgai", "skills"), "skills")
	require.Error(t, err)
	require.ErrorContains(t, err, "symlinked path is not allowed")

	content, errRead := os.ReadFile(externalFile)
	require.NoError(t, errRead)
	assert.Equal(t, "# external", string(content))
}

func TestCopyLayerSubfolderRejectsSymlinkedSourceSubfolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior differs on windows")
	}

	workspaceDir := t.TempDir()
	externalDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(externalDir, "demo"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(externalDir, "demo", "SKILL.md"), []byte("# external"), 0o644))

	require.NoError(t, os.MkdirAll(filepath.Join(workspaceDir, "sgai"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceDir, ".sgai"), 0o755))
	require.NoError(t, os.Symlink(externalDir, filepath.Join(workspaceDir, "sgai", "skills")))

	err := copyLayerSubfolder(workspaceDir, filepath.Join(workspaceDir, "sgai", "skills"), filepath.Join(workspaceDir, ".sgai", "skills"), "skills")
	require.Error(t, err)
	require.ErrorContains(t, err, "overlay source path")
	require.ErrorContains(t, err, "symlinked path is not allowed")
	assert.NoDirExists(t, filepath.Join(workspaceDir, ".sgai", "skills", "demo"))
}

func TestCopyLayerSubfolderRejectsSymlinkedSourceFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior differs on windows")
	}

	workspaceDir := t.TempDir()
	srcDir := filepath.Join(workspaceDir, "sgai", "skills")
	dstDir := filepath.Join(workspaceDir, ".sgai", "skills")
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "demo"), 0o755))
	require.NoError(t, os.MkdirAll(dstDir, 0o755))

	externalFile := filepath.Join(t.TempDir(), "SKILL.md")
	require.NoError(t, os.WriteFile(externalFile, []byte("# external"), 0o644))
	require.NoError(t, os.Symlink(externalFile, filepath.Join(srcDir, "demo", "SKILL.md")))

	err := copyLayerSubfolder(workspaceDir, srcDir, dstDir, "skills")
	require.Error(t, err)
	require.ErrorContains(t, err, "overlay source path")
	require.ErrorContains(t, err, "symlinked path is not allowed")
	assert.NoFileExists(t, filepath.Join(dstDir, "demo", "SKILL.md"))
}

func TestInitializeJJForkWorkspace(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".jj"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".jj", "repo"), []byte("/some/other/path"), 0o644))
	err := initializeJJ(dir)
	require.NoError(t, err)
}

func readSkeletonFileForTest(t *testing.T, relPath string) string {
	t.Helper()

	content, errRead := fs.ReadFile(skelFS, filepath.Join("skel", relPath))
	require.NoError(t, errRead)
	return string(content)
}

func TestExtractBodyWithFrontmatter(t *testing.T) {
	content := []byte("---\ntitle: test\n---\n# Body content")
	result := extractBody(content)
	assert.Equal(t, "# Body content", string(result))
}

func TestExtractBodyIgnoresDelimiterSubstringInsideFrontmatterBlockScalar(t *testing.T) {
	content := []byte("---\ndescription: |\n  first line\n  --- not a delimiter line\nflow: test\n---\n# Body content")
	result := extractBody(content)
	assert.Equal(t, "# Body content", string(result))
}

func TestExtractBodyUnclosedFrontmatter(t *testing.T) {
	content := []byte("---\ntitle: test\n# Body content")
	result := extractBody(content)
	assert.Equal(t, content, result)
}

func TestDotSGAILinePresentVariants(t *testing.T) {
	assert.True(t, dotSGAILinePresent([]byte("/.sgai\n")))
	assert.True(t, dotSGAILinePresent([]byte("other\n/.sgai\nmore")))
	assert.False(t, dotSGAILinePresent([]byte("other\n")))
	assert.False(t, dotSGAILinePresent(nil))
}

func TestCopyFileAtomicCreatesDstDir(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "subdir", "nested", "dst.txt")
	require.NoError(t, os.WriteFile(src, []byte("content"), 0o644))
	require.NoError(t, copyFileAtomic(src, dst))
	data, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "content", string(data))
}

func TestCopyFinalStateToRetrospectiveSuccess(t *testing.T) {
	dir := t.TempDir()
	sgaiDir := filepath.Join(dir, ".sgai")
	require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sgaiDir, "state.json"), []byte(`{"status":"complete"}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sgaiDir, "PROJECT_MANAGEMENT.md"), []byte("# PM"), 0o644))

	retroDir := filepath.Join(t.TempDir(), "retro")
	require.NoError(t, os.MkdirAll(retroDir, 0o755))

	err := copyFinalStateToRetrospective(dir, retroDir)
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(retroDir, "state.json"))
	assert.FileExists(t, filepath.Join(retroDir, "PROJECT_MANAGEMENT.md"))
}

func TestCopyProjectManagementToRetrospectiveWithPM(t *testing.T) {
	dir := t.TempDir()
	sgaiDir := filepath.Join(dir, ".sgai")
	require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sgaiDir, "PROJECT_MANAGEMENT.md"), []byte("# PM content"), 0o644))
	retroDir := t.TempDir()
	require.NoError(t, copyProjectManagementToRetrospective(dir, retroDir))
	assert.FileExists(t, filepath.Join(retroDir, "PROJECT_MANAGEMENT.md"))
}
