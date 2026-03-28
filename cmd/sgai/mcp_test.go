package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ucirello/sgai/pkg/state"
)

func TestFindSnippetsByFuzzyMatch(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "http-server.go"), []byte("---\ndescription: HTTP server setup\n---\npackage main\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "json-parsing.go"), []byte("---\ndescription: JSON parsing utilities\n---\npackage main\n"), 0o644))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	t.Run("matchByName", func(t *testing.T) {
		result, err := findSnippetsByFuzzyMatch(dir, entries, "http")
		require.NoError(t, err)
		assert.Contains(t, result, "http-server")
	})

	t.Run("matchByDescription", func(t *testing.T) {
		result, err := findSnippetsByFuzzyMatch(dir, entries, "parsing")
		require.NoError(t, err)
		assert.Contains(t, result, "json-parsing")
	})

	t.Run("noMatch", func(t *testing.T) {
		result, err := findSnippetsByFuzzyMatch(dir, entries, "nonexistent")
		require.NoError(t, err)
		assert.Empty(t, result)
	})
}

func TestAskUserQuestionNoCoord(t *testing.T) {
	result, err := askUserQuestion(t.Context(), nil, newTestAskUserQuestionArgs(newTestQuestionItemArgs("test?", []string{"yes", "no"})))
	require.NoError(t, err)
	assert.Contains(t, result, "Error")
}

func TestAskUserQuestionNoQuestions(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	coord, err := state.NewCoordinatorWith(statePath, updated(newTestWorkflow(), func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.InteractionMode = state.ModeBrainstorming
	}))
	require.NoError(t, err)

	result, errQ := askUserQuestion(t.Context(), coord, newTestAskUserQuestionArgs())
	require.NoError(t, errQ)
	assert.Contains(t, result, "Error")
}

func TestAskUserQuestionEmptyChoices(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	coord, err := state.NewCoordinatorWith(statePath, updated(newTestWorkflow(), func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.InteractionMode = state.ModeBrainstorming
	}))
	require.NoError(t, err)

	result, errQ := askUserQuestion(t.Context(), coord, newTestAskUserQuestionArgs(newTestQuestionItemArgs("test?", nil)))
	require.NoError(t, errQ)
	assert.Contains(t, result, "Error")
	assert.Contains(t, result, "no choices")
}

func TestAskUserWorkGateNoCoord(t *testing.T) {
	result, err := askUserWorkGate(t.Context(), nil, "summary")
	require.NoError(t, err)
	assert.Contains(t, result, "Error")
}

func TestAskUserWorkGateEmptySummary(t *testing.T) {
	result, err := askUserWorkGate(t.Context(), nil, "")
	require.NoError(t, err)
	assert.Contains(t, result, "Error")
	assert.Contains(t, result, "summary is required")
}

func TestAskUserQuestionToolsNotAllowed(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	coord, err := state.NewCoordinatorWith(statePath, updated(newTestWorkflow(), func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.InteractionMode = state.ModeSelfDrive
	}))
	require.NoError(t, err)

	result, errQ := askUserQuestion(t.Context(), coord, newTestAskUserQuestionArgs(newTestQuestionItemArgs("test?", []string{"yes"})))
	require.NoError(t, errQ)
	assert.Equal(t, autoProceedAnswer, result)
}

func TestAskUserWorkGateToolsNotAllowed(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	coord, err := state.NewCoordinatorWith(statePath, updated(newTestWorkflow(), func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.InteractionMode = state.ModeSelfDrive
	}))
	require.NoError(t, err)

	result, errQ := askUserWorkGate(t.Context(), coord, "summary of work")
	require.NoError(t, errQ)
	assert.Equal(t, autoRecordQuestionsAnswer, result)
}

func TestMCPHandlerErrorPaths(t *testing.T) {
	t.Run("findSkillsHandlerError", func(t *testing.T) {
		ctx := &mcpContext{
			workingDir: "/nonexistent/path/12345",
			coord:      nil,
			dagAgents:  nil,
			agentName:  "test",
			humanTools: newTestHumanToolCallbacks(),
		}
		_, _, err := ctx.findSkillsHandler(context.Background(), nil, findSkillsArgs{Name: "exact-match"})
		require.Error(t, err)
	})

	t.Run("findSnippetsHandlerNoError", func(t *testing.T) {
		ctx := &mcpContext{
			workingDir: "/nonexistent/path/12345",
			coord:      nil,
			dagAgents:  nil,
			agentName:  "test",
			humanTools: newTestHumanToolCallbacks(),
		}
		result, _, err := ctx.findSnippetsHandler(context.Background(), nil, newTestFindSnippetsArgs("go", ""))
		require.NoError(t, err)
		require.NotNil(t, result)
	})
}

func newTestQuestionItemArgs(question string, choices []string) questionItem {
	return questionItem{Question: question, Choices: choices, MultiSelect: false}
}

func newTestAskUserQuestionArgs(questions ...questionItem) askUserQuestionArgs {
	return askUserQuestionArgs{Questions: questions}
}

func newTestFindSkillsArgs(name string) findSkillsArgs {
	return findSkillsArgs{Name: name}
}

func newTestFindSnippetsArgs(language, query string) findSnippetsArgs {
	return findSnippetsArgs{Language: language, Query: query}
}

func newTestUpdateWorkflowStateArgs(status workflowStatus, task, addProgress string) updateWorkflowStateArgs {
	return updateWorkflowStateArgs{Status: status, Task: task, AddProgress: addProgress}
}

func newTestHumanToolCallbacks() humanToolCallbacks {
	return humanToolCallbacks{question: nil, workGate: nil}
}

func newTestMCPContext(t *testing.T) (ctx *mcpContext, dir string) {
	t.Helper()
	return newTestMCPContextForAgent(t, "test-agent")
}

func newTestMCPContextForAgent(t *testing.T, agentName string) (ctx *mcpContext, dir string) {
	t.Helper()
	dir = t.TempDir()
	sgaiDir := filepath.Join(dir, ".sgai")
	require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
	skillsDir := filepath.Join(sgaiDir, "skills")
	require.NoError(t, os.MkdirAll(skillsDir, 0o755))
	snippetsDir := filepath.Join(sgaiDir, "snippets")
	require.NoError(t, os.MkdirAll(snippetsDir, 0o755))

	statePath := filepath.Join(sgaiDir, "state.json")
	coord, errCoord := state.NewCoordinatorWith(statePath, updated(newTestWorkflow(), func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.CurrentAgent = agentName
		workflow.Messages = []state.Message{}
		workflow.Progress = []state.ProgressEntry{}
	}))
	require.NoError(t, errCoord)

	ctx = &mcpContext{
		workingDir: dir,
		coord:      coord,
		dagAgents:  []string{"coordinator", agentName, "reviewer"},
		agentName:  agentName,
		humanTools: newTestHumanToolCallbacks(),
	}
	return ctx, dir
}

func TestFindSkillsHandlerSuccess(t *testing.T) {
	ctx, dir := newTestMCPContext(t)

	skillDir := filepath.Join(dir, ".sgai", "skills", "test-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\ndescription: A test skill\n---\nSkill content"), 0o644))

	result, _, err := ctx.findSkillsHandler(context.Background(), nil, newTestFindSkillsArgs(""))
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Content, 1)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "test-skill")
}

func TestFindSkillsHandlerExactMatch(t *testing.T) {
	ctx, dir := newTestMCPContext(t)

	skillDir := filepath.Join(dir, ".sgai", "skills", "my-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\ndescription: My skill\n---\nDetailed skill content"), 0o644))

	result, _, err := ctx.findSkillsHandler(context.Background(), nil, findSkillsArgs{Name: "my-skill"})
	require.NoError(t, err)
	require.NotNil(t, result)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "my-skill")
}

func TestFindSnippetsHandlerListLanguages(t *testing.T) {
	ctx, dir := newTestMCPContext(t)

	goDir := filepath.Join(dir, ".sgai", "snippets", "go")
	require.NoError(t, os.MkdirAll(goDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(goDir, "hello.go"), []byte("// snippet: hello world\npackage main"), 0o644))

	result, _, err := ctx.findSnippetsHandler(context.Background(), nil, newTestFindSnippetsArgs("", ""))
	require.NoError(t, err)
	require.NotNil(t, result)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "go")
}

func TestFindSnippetsHandlerWithLanguage(t *testing.T) {
	ctx, dir := newTestMCPContext(t)

	goDir := filepath.Join(dir, ".sgai", "snippets", "go")
	require.NoError(t, os.MkdirAll(goDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(goDir, "example.go"), []byte("// snippet: example\npackage main"), 0o644))

	result, _, err := ctx.findSnippetsHandler(context.Background(), nil, newTestFindSnippetsArgs("go", ""))
	require.NoError(t, err)
	require.NotNil(t, result)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "example")
}

func TestUpdateWorkflowStateHandlerSuccess(t *testing.T) {
	ctx, _ := newTestMCPContext(t)

	result, _, err := ctx.updateWorkflowStateHandler(context.Background(), nil, newTestUpdateWorkflowStateArgs("working", "test task", "doing something"))
	require.NoError(t, err)
	require.NotNil(t, result)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "State updated successfully")
}

func TestUpdateWorkflowStateHandlerInvalidStatus(t *testing.T) {
	ctx, _ := newTestMCPContext(t)

	result, _, err := ctx.updateWorkflowStateHandler(context.Background(), nil, newTestUpdateWorkflowStateArgs("bogus-status", "", ""))
	require.NoError(t, err)
	require.NotNil(t, result)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "Error")
}

func TestSendMessageHandlerSuccess(t *testing.T) {
	ctx, _ := newTestMCPContext(t)

	result, _, err := ctx.sendMessageHandler(context.Background(), nil, sendMessageArgs{
		ToAgent: "reviewer",
		Body:    "Please review this code",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "Message sent")
}

func TestSendMessageHandlerInvalidRecipient(t *testing.T) {
	ctx, _ := newTestMCPContext(t)

	result, _, err := ctx.sendMessageHandler(context.Background(), nil, sendMessageArgs{
		ToAgent: "nonexistent-agent",
		Body:    "hello",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "Error")
}

func TestDeleteUnreadMessagesSuccess(t *testing.T) {
	ctx, _ := newTestMCPContextForAgent(t, "coordinator")
	require.NoError(t, ctx.coord.UpdateState(func(wf *state.Workflow) {
		wf.Messages = []state.Message{
			updated(newTestMessage(), func(message *state.Message) {
				message.ID = 1
				message.FromAgent = "agent-a"
				message.ToAgent = "coordinator"
				message.Body = "one"
				message.Read = false
			}),
			updated(newTestMessage(), func(message *state.Message) {
				message.ID = 2
				message.FromAgent = "agent-b"
				message.ToAgent = "coordinator"
				message.Body = "two"
				message.Read = false
			}),
			updated(newTestMessage(), func(message *state.Message) {
				message.ID = 3
				message.FromAgent = "agent-c"
				message.ToAgent = "coordinator"
				message.Body = "three"
				message.Read = true
			}),
		}
	}))

	result, _, err := ctx.deleteUnreadMessagesHandler(context.Background(), nil, deleteUnreadMessagesArgs{IDs: []int{1, 2}})
	require.NoError(t, err)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "Deleted unread messages: 1, 2")
	assert.Len(t, ctx.coord.State().Messages, 1)
	assert.Equal(t, 3, ctx.coord.State().Messages[0].ID)
}

func TestDeleteUnreadMessagesRejectsNonCoordinator(t *testing.T) {
	ctx, _ := newTestMCPContext(t)
	require.NoError(t, ctx.coord.UpdateState(func(wf *state.Workflow) {
		wf.Messages = []state.Message{updated(newTestMessage(), func(message *state.Message) {
			message.ID = 1
			message.FromAgent = "agent-a"
			message.ToAgent = "coordinator"
			message.Body = "one"
			message.Read = false
		})}
	}))

	result, _, err := ctx.deleteUnreadMessagesHandler(context.Background(), nil, deleteUnreadMessagesArgs{IDs: []int{1}})
	require.NoError(t, err)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "coordinator-only")
	assert.Len(t, ctx.coord.State().Messages, 1)
}

func TestDeleteUnreadMessagesRejectsReadMessages(t *testing.T) {
	ctx, _ := newTestMCPContextForAgent(t, "coordinator")
	require.NoError(t, ctx.coord.UpdateState(func(wf *state.Workflow) {
		wf.Messages = []state.Message{updated(newTestMessage(), func(message *state.Message) {
			message.ID = 7
			message.FromAgent = "agent"
			message.ToAgent = "coordinator"
			message.Body = "done"
			message.Read = true
		})}
	}))

	result, _, err := ctx.deleteUnreadMessagesHandler(context.Background(), nil, deleteUnreadMessagesArgs{IDs: []int{7}})
	require.NoError(t, err)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "must all be unread")
	assert.Len(t, ctx.coord.State().Messages, 1)
}

func TestDeleteUnreadMessagesRejectsMixedBatchWithoutDeletingAnything(t *testing.T) {
	ctx, _ := newTestMCPContextForAgent(t, "coordinator")
	require.NoError(t, ctx.coord.UpdateState(func(wf *state.Workflow) {
		wf.Messages = []state.Message{
			updated(newTestMessage(), func(message *state.Message) {
				message.ID = 1
				message.FromAgent = "agent-a"
				message.ToAgent = "coordinator"
				message.Body = "pending"
				message.Read = false
			}),
			updated(newTestMessage(), func(message *state.Message) {
				message.ID = 2
				message.FromAgent = "agent-b"
				message.ToAgent = "coordinator"
				message.Body = "done"
				message.Read = true
			}),
		}
	}))

	result, _, err := ctx.deleteUnreadMessagesHandler(context.Background(), nil, deleteUnreadMessagesArgs{IDs: []int{1, 2}})
	require.NoError(t, err)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "must all be unread")
	require.Len(t, ctx.coord.State().Messages, 2)
	assert.Equal(t, []state.Message{
		updated(newTestMessage(), func(message *state.Message) {
			message.ID = 1
			message.FromAgent = "agent-a"
			message.ToAgent = "coordinator"
			message.Body = "pending"
			message.Read = false
		}),
		updated(newTestMessage(), func(message *state.Message) {
			message.ID = 2
			message.FromAgent = "agent-b"
			message.ToAgent = "coordinator"
			message.Body = "done"
			message.Read = true
		}),
	}, ctx.coord.State().Messages)
}

func TestDeleteUnreadMessagesValidationUsesCurrentWorkflowState(t *testing.T) {
	messages := []state.Message{
		updated(newTestMessage(), func(message *state.Message) {
			message.ID = 1
			message.FromAgent = "agent-a"
			message.ToAgent = "coordinator"
			message.Body = "pending"
			message.Read = false
		}),
		updated(newTestMessage(), func(message *state.Message) {
			message.ID = 2
			message.FromAgent = "agent-b"
			message.ToAgent = "coordinator"
			message.Body = "done"
			message.Read = true
		}),
	}

	invalidIDs := invalidUnreadMessageIDs(messages, []int{1, 2})
	assert.Equal(t, []int{2}, invalidIDs)
}

func TestCheckInboxHandlerEmpty(t *testing.T) {
	ctx, _ := newTestMCPContext(t)

	result, _, err := ctx.checkInboxHandler(context.Background(), nil, struct{}{})
	require.NoError(t, err)
	require.NotNil(t, result)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "no messages")
}

func TestCheckInboxHandlerWithMessages(t *testing.T) {
	ctx, _ := newTestMCPContext(t)

	require.NoError(t, ctx.coord.UpdateState(func(wf *state.Workflow) {
		wf.Messages = append(wf.Messages, updated(newTestMessage(), func(message *state.Message) {
			message.ID = 1
			message.FromAgent = "reviewer"
			message.ToAgent = "test-agent"
			message.Body = "Fix this bug"
			message.Read = false
		}))
	}))

	result, _, err := ctx.checkInboxHandler(context.Background(), nil, struct{}{})
	require.NoError(t, err)
	require.NotNil(t, result)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "Fix this bug")
}

func TestCheckOutboxHandlerEmpty(t *testing.T) {
	ctx, _ := newTestMCPContext(t)

	result, _, err := ctx.checkOutboxHandler(context.Background(), nil, struct{}{})
	require.NoError(t, err)
	require.NotNil(t, result)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "not sent any messages")
}

func TestCheckOutboxHandlerWithMessages(t *testing.T) {
	ctx, _ := newTestMCPContext(t)

	require.NoError(t, ctx.coord.UpdateState(func(wf *state.Workflow) {
		wf.Messages = append(wf.Messages, updated(newTestMessage(), func(message *state.Message) {
			message.ID = 1
			message.FromAgent = "test-agent"
			message.ToAgent = "reviewer"
			message.Body = "Ready for review"
			message.Read = false
		}))
	}))

	result, _, err := ctx.checkOutboxHandler(context.Background(), nil, struct{}{})
	require.NoError(t, err)
	require.NotNil(t, result)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "Ready for review")
}

func TestPeekMessageBusHandlerEmpty(t *testing.T) {
	ctx, _ := newTestMCPContext(t)

	result, _, err := ctx.peekMessageBusHandler(context.Background(), nil, struct{}{})
	require.NoError(t, err)
	require.NotNil(t, result)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "No messages")
}

func TestPeekMessageBusHandlerWithMessages(t *testing.T) {
	ctx, _ := newTestMCPContext(t)

	require.NoError(t, ctx.coord.UpdateState(func(wf *state.Workflow) {
		wf.Messages = append(wf.Messages, updated(newTestMessage(), func(message *state.Message) {
			message.ID = 1
			message.FromAgent = "reviewer"
			message.ToAgent = "coordinator"
			message.Body = "Code review complete"
			message.Read = false
		}))
	}))

	result, _, err := ctx.peekMessageBusHandler(context.Background(), nil, struct{}{})
	require.NoError(t, err)
	require.NotNil(t, result)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "Code review complete")
}

func TestProjectTodoWriteHandlerSuccess(t *testing.T) {
	ctx, _ := newTestMCPContext(t)

	todos := []state.TodoItem{
		updated(newTestTodoItem(), func(todo *state.TodoItem) {
			todo.Content = "Task 1"
			todo.Status = "pending"
			todo.Priority = "high"
		}),
		updated(newTestTodoItem(), func(todo *state.TodoItem) {
			todo.Content = "Task 2"
			todo.Status = "completed"
			todo.Priority = "low"
		}),
	}

	result, _, err := ctx.projectTodoWriteHandler(context.Background(), nil, projectTodoWriteArgs{Todos: todos})
	require.NoError(t, err)
	require.NotNil(t, result)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "Task 1")
}

func TestProjectTodoReadHandlerEmpty(t *testing.T) {
	ctx, _ := newTestMCPContext(t)

	result, _, err := ctx.projectTodoReadHandler(context.Background(), nil, struct{}{})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestProjectTodoReadHandlerWithTodos(t *testing.T) {
	ctx, _ := newTestMCPContext(t)

	require.NoError(t, ctx.coord.UpdateState(func(wf *state.Workflow) {
		wf.ProjectTodos = []state.TodoItem{
			updated(newTestTodoItem(), func(todo *state.TodoItem) {
				todo.Content = "Review PR"
				todo.Status = "pending"
				todo.Priority = "high"
			}),
		}
	}))

	result, _, err := ctx.projectTodoReadHandler(context.Background(), nil, struct{}{})
	require.NoError(t, err)
	require.NotNil(t, result)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "Review PR")
}

func TestAskUserQuestionHandlerNoCoord(t *testing.T) {
	ctx, _ := newTestMCPContext(t)
	ctx.coord = nil

	result, _, err := ctx.askUserQuestionHandler(context.Background(), nil, newTestAskUserQuestionArgs())
	require.NoError(t, err)
	require.NotNil(t, result)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "Error")
}

func TestAskUserWorkGateHandlerNoCoord(t *testing.T) {
	ctx, _ := newTestMCPContext(t)
	ctx.coord = nil

	result, _, err := ctx.askUserWorkGateHandler(context.Background(), nil, askUserWorkGateArgs{Summary: "test"})
	require.NoError(t, err)
	require.NotNil(t, result)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "Error")
}

func TestAskUserWorkGateHandlerEmptySummary(t *testing.T) {
	ctx, _ := newTestMCPContext(t)

	result, _, err := ctx.askUserWorkGateHandler(context.Background(), nil, askUserWorkGateArgs{Summary: ""})
	require.NoError(t, err)
	require.NotNil(t, result)
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "Error")
	assert.Contains(t, text, "summary is required")
}

func TestParseAgentIdentityHeader(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected string
	}{
		{
			name:     "emptyHeader",
			header:   "",
			expected: "",
		},
		{
			name:     "simpleAgentName",
			header:   "go-developer",
			expected: "go-developer",
		},
		{
			name:     "agentWithModelAndVariant",
			header:   "go-developer|anthropic/claude-opus-4-6|max",
			expected: "go-developer",
		},
		{
			name:     "agentWithPipeSeparator",
			header:   "react-developer|opencode/model1|",
			expected: "react-developer",
		},
		{
			name:     "onlyPipes",
			header:   "||",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := http.NewRequest(http.MethodGet, "/test", http.NoBody)
			if tt.header != "" {
				r.Header.Set(agentIdentityHeader, tt.header)
			}
			result := parseAgentIdentityHeader(r)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResolveCallerAgent(t *testing.T) {
	tests := []struct {
		name         string
		headerAgent  string
		currentAgent string
		expected     string
	}{
		{
			name:         "missingHeaderWithCurrentAgent",
			headerAgent:  "",
			currentAgent: "go-developer",
			expected:     "go-developer",
		},
		{
			name:         "missingHeaderWithEmptyCurrent",
			headerAgent:  "",
			currentAgent: "",
			expected:     "",
		},
		{
			name:         "nonCoordinatorHeader",
			headerAgent:  "go-developer",
			currentAgent: "react-developer",
			expected:     "go-developer",
		},
		{
			name:         "coordinatorHeaderWithCurrentAgent",
			headerAgent:  "coordinator",
			currentAgent: "go-developer",
			expected:     "go-developer",
		},
		{
			name:         "coordinatorHeaderWithCoordinatorCurrent",
			headerAgent:  "coordinator",
			currentAgent: "coordinator",
			expected:     "coordinator",
		},
		{
			name:         "coordinatorHeaderWithEmptyCurrent",
			headerAgent:  "coordinator",
			currentAgent: "",
			expected:     "coordinator",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statePath := filepath.Join(tmpDir, "state.json")
			coord, err := state.NewCoordinatorWith(statePath, updated(newTestWorkflow(), func(workflow *state.Workflow) {
				workflow.CurrentAgent = tt.currentAgent
			}))
			require.NoError(t, err)
			result := resolveCallerAgent(tt.headerAgent, coord)
			assert.Equal(t, tt.expected, result)
		})
	}
}

type messageMatchTest struct {
	name         string
	agentField   string
	currentAgent string
	currentModel string
	expected     bool
}

func messageMatchCases() []messageMatchTest {
	return []messageMatchTest{
		{
			name:         "matchesAgent",
			agentField:   "go-developer",
			currentAgent: "go-developer",
			currentModel: "",
			expected:     true,
		},
		{
			name:         "matchesModel",
			agentField:   "opencode/glm-5",
			currentAgent: "go-developer",
			currentModel: "opencode/glm-5",
			expected:     true,
		},
		{
			name:         "noMatch",
			agentField:   "react-developer",
			currentAgent: "go-developer",
			currentModel: "",
			expected:     false,
		},
		{
			name:         "emptyAgentField",
			agentField:   "",
			currentAgent: "go-developer",
			currentModel: "",
			expected:     false,
		},
		{
			name:         "emptyCurrentAgent",
			agentField:   "go-developer",
			currentAgent: "",
			currentModel: "",
			expected:     false,
		},
		{
			name:         "modelMatchWithEmptyModel",
			agentField:   "opencode/glm-5",
			currentAgent: "go-developer",
			currentModel: "",
			expected:     false,
		},
	}
}

func TestMessageMatchesRecipient(t *testing.T) {
	for _, tt := range messageMatchCases() {
		t.Run(tt.name, func(t *testing.T) {
			msg := updated(newTestMessage(), func(message *state.Message) {
				message.ToAgent = tt.agentField
			})
			result := messageMatchesRecipient(&msg, tt.currentAgent, tt.currentModel)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMessageMatchesSender(t *testing.T) {
	for _, tt := range messageMatchCases() {
		t.Run(tt.name, func(t *testing.T) {
			msg := updated(newTestMessage(), func(message *state.Message) {
				message.FromAgent = tt.agentField
			})
			result := messageMatchesSender(&msg, tt.currentAgent, tt.currentModel)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildUpdateWorkflowStateSchema(t *testing.T) {
	t.Run("coordinatorAgent", func(t *testing.T) {
		schema, desc := buildUpdateWorkflowStateSchema("coordinator")
		assert.NotNil(t, schema)
		assert.NotEmpty(t, desc)
		assert.Contains(t, desc, "workflow state")
		statusProp := schema.Properties["status"]
		assert.NotNil(t, statusProp)
		assert.Equal(t, []any{state.StatusWorking, state.StatusAgentDone, state.StatusComplete}, statusProp.Enum)
		assert.Contains(t, statusProp.Description, "Valid values: working, agent-done, complete")
	})

	t.Run("nonCoordinatorAgent", func(t *testing.T) {
		schema, desc := buildUpdateWorkflowStateSchema("backend-developer")
		assert.NotNil(t, schema)
		assert.NotEmpty(t, desc)
		statusProp := schema.Properties["status"]
		assert.NotNil(t, statusProp)
		assert.Equal(t, []any{state.StatusWorking, state.StatusAgentDone}, statusProp.Enum)
		assert.Contains(t, statusProp.Description, "Valid values: working, agent-done")
		assert.NotContains(t, statusProp.Description, "complete")
	})
}

func TestMustSchema(t *testing.T) {
	schema, errSchema := schemaFor[findSkillsArgs]()
	require.NoError(t, errSchema)
	assert.NotNil(t, schema)
	assert.Equal(t, "object", schema.Type)
}

func TestSchemaForRejectsUnsupportedType(t *testing.T) {
	type unsupportedSchemaArgs struct {
		Callback func() `json:"callback"`
	}

	schema, errSchema := schemaFor[unsupportedSchemaArgs]()
	assert.Nil(t, schema)
	require.Error(t, errSchema)
	assert.Contains(t, errSchema.Error(), "unsupported")
}

func TestSkillDisplayName(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]string
		relName     string
		expected    string
	}{
		{
			name:        "withName",
			frontmatter: map[string]string{"name": "My Skill"},
			relName:     "skills/my-skill",
			expected:    "My Skill",
		},
		{
			name:        "withoutName",
			frontmatter: map[string]string{},
			relName:     "skills/my-skill",
			expected:    "my-skill",
		},
		{
			name:        "emptyName",
			frontmatter: map[string]string{"name": ""},
			relName:     "skills/my-skill",
			expected:    "my-skill",
		},
		{
			name:        "nestedPath",
			frontmatter: map[string]string{},
			relName:     "skills/category/my-skill",
			expected:    "my-skill",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := skillDisplayName(tt.frontmatter, tt.relName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSkillSteeringMessage(t *testing.T) {
	name := "my-skill"
	desc := "A useful skill for testing"

	result := skillSteeringMessage(name, desc)

	assert.Contains(t, result, "Found skill 'my-skill'")
	assert.Contains(t, result, "A useful skill for testing")
	assert.Contains(t, result, `skill({"name":"my-skill"})`)
}

func TestSkillRelName(t *testing.T) {
	tests := []struct {
		name      string
		skillsDir string
		file      string
		expected  string
	}{
		{
			name:      "simpleSkill",
			skillsDir: "/path/to/.sgai/skills",
			file:      "/path/to/.sgai/skills/my-skill/SKILL.md",
			expected:  "my-skill",
		},
		{
			name:      "nestedSkill",
			skillsDir: "/path/to/.sgai/skills",
			file:      "/path/to/.sgai/skills/category/my-skill/SKILL.md",
			expected:  "category/my-skill",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := skillRelName(tt.skillsDir, tt.file)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSkillDesc(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]string
		expected    string
	}{
		{
			name:        "withDescription",
			frontmatter: map[string]string{"description": "A useful skill"},
			expected:    "A useful skill",
		},
		{
			name:        "withoutDescription",
			frontmatter: map[string]string{},
			expected:    "No description",
		},
		{
			name:        "emptyDescription",
			frontmatter: map[string]string{"description": ""},
			expected:    "No description",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := skillDesc(tt.frontmatter)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestUpdateWorkflowState(t *testing.T) {
	tests := []struct {
		name           string
		initialState   state.Workflow
		callerAgent    string
		args           updateWorkflowStateArgs
		wantErr        bool
		wantContains   string
		wantNotContain string
	}{
		{
			name:           "nilCoordinator",
			initialState:   newTestWorkflow(),
			callerAgent:    "test-agent",
			args:           newTestUpdateWorkflowStateArgs("", "", ""),
			wantErr:        false,
			wantContains:   "Error: workflow coordinator not available.",
			wantNotContain: "",
		},
		{
			name: "setWorkingStatus",
			initialState: updated(newTestWorkflow(), func(workflow *state.Workflow) {
				workflow.Status = state.StatusWorking
				workflow.Progress = []state.ProgressEntry{}
			}),
			callerAgent:    "test-agent",
			args:           newTestUpdateWorkflowStateArgs("working", "doing stuff", "started work"),
			wantErr:        false,
			wantContains:   "State updated successfully.",
			wantNotContain: "",
		},
		{
			name: "invalidStatus",
			initialState: updated(newTestWorkflow(), func(workflow *state.Workflow) {
				workflow.Status = state.StatusWorking
				workflow.Progress = []state.ProgressEntry{}
			}),
			callerAgent:    "test-agent",
			args:           newTestUpdateWorkflowStateArgs("invalid-status", "", ""),
			wantErr:        false,
			wantContains:   "Error: Invalid status",
			wantNotContain: "",
		},
		{
			name: "agentDoneWithPendingTodos",
			initialState: updated(newTestWorkflow(), func(workflow *state.Workflow) {
				workflow.Status = state.StatusWorking
				workflow.CurrentAgent = "test-agent"
				workflow.Progress = []state.ProgressEntry{}
				workflow.Todos = []state.TodoItem{updated(newTestTodoItem(), func(todo *state.TodoItem) {
					todo.Content = "pending task"
					todo.Status = "pending"
					todo.Priority = "high"
				})}
			}),
			callerAgent:    "test-agent",
			args:           newTestUpdateWorkflowStateArgs("agent-done", "", ""),
			wantErr:        false,
			wantContains:   "Error: Cannot transition to 'agent-done'",
			wantNotContain: "",
		},
		{
			name: "agentDoneClearsTask",
			initialState: updated(newTestWorkflow(), func(workflow *state.Workflow) {
				workflow.Status = state.StatusWorking
				workflow.CurrentAgent = "test-agent"
				workflow.Progress = []state.ProgressEntry{}
			}),
			callerAgent:    "test-agent",
			args:           newTestUpdateWorkflowStateArgs("agent-done", "some task", ""),
			wantErr:        false,
			wantContains:   "State updated successfully.",
			wantNotContain: "",
		},
		{
			name: "pendingHumanInputStillUpdatesState",
			initialState: updated(newTestWorkflow(), func(workflow *state.Workflow) {
				workflow.Status = state.StatusWorking
				workflow.HumanMessage = "waiting"
				workflow.Progress = []state.ProgressEntry{}
			}),
			callerAgent:    "test-agent",
			args:           newTestUpdateWorkflowStateArgs("working", "my task", "progress note"),
			wantErr:        false,
			wantContains:   "State updated successfully.",
			wantNotContain: "",
		},
		{
			name: "addProgressNote",
			initialState: updated(newTestWorkflow(), func(workflow *state.Workflow) {
				workflow.Status = state.StatusWorking
				workflow.Progress = []state.ProgressEntry{}
			}),
			callerAgent:    "test-agent",
			args:           newTestUpdateWorkflowStateArgs("", "", "completed step 1"),
			wantErr:        false,
			wantContains:   "Added progress note: completed step 1",
			wantNotContain: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "nilCoordinator" {
				result, err := updateWorkflowState(nil, tt.callerAgent, tt.args)
				require.NoError(t, err)
				assert.Contains(t, result, tt.wantContains)
				return
			}

			tmpDir := t.TempDir()
			statePath := filepath.Join(tmpDir, "state.json")
			coord, err := state.NewCoordinatorWith(statePath, tt.initialState)
			require.NoError(t, err)

			result, err := updateWorkflowState(coord, tt.callerAgent, tt.args)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Contains(t, result, tt.wantContains)
		})
	}
}

func TestSendMessage(t *testing.T) {
	dagAgents := []string{"coordinator", "go-developer", "react-developer", "project-critic-council"}

	tests := []struct {
		name         string
		callerAgent  string
		toAgent      string
		body         string
		goalContent  string
		wantContains string
		wantTargets  []string
	}{
		{
			name:         "nilCoordinator",
			callerAgent:  "test-agent",
			toAgent:      "coordinator",
			body:         "hello",
			goalContent:  "",
			wantContains: "Error: Could not read state.json",
			wantTargets:  nil,
		},
		{
			name:         "invalidTargetAgent",
			callerAgent:  "coordinator",
			toAgent:      "non-existent-agent",
			body:         "hello",
			goalContent:  "",
			wantContains: "Error: Agent 'non-existent-agent' is not in the workflow",
			wantTargets:  nil,
		},
		{
			name:         "sendFromCoordinator",
			callerAgent:  "coordinator",
			toAgent:      "go-developer",
			body:         "please review this",
			goalContent:  "",
			wantContains: "Message sent successfully",
			wantTargets:  nil,
		},
		{
			name:         "sendFromNonCoordinator",
			callerAgent:  "go-developer",
			toAgent:      "coordinator",
			body:         "done with review",
			goalContent:  "",
			wantContains: "IMPORTANT: To receive a response",
			wantTargets:  nil,
		},
		{
			name:         "fansOutToMultiModelAgent",
			callerAgent:  "coordinator",
			toAgent:      "project-critic-council",
			body:         "please review this session",
			goalContent:  "---\nmodels:\n  \"project-critic-council\": [\"model-a\", \"model-b\"]\n---\n# Goal\n",
			wantContains: "Sent 2 messages successfully",
			wantTargets: []string{
				"project-critic-council:model-a",
				"project-critic-council:model-b",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "nilCoordinator" {
				result, err := sendMessage("", nil, dagAgents, tt.callerAgent, tt.toAgent, tt.body)
				require.NoError(t, err)
				assert.Contains(t, result, tt.wantContains)
				return
			}

			tmpDir := t.TempDir()
			statePath := filepath.Join(tmpDir, "state.json")
			coord, err := state.NewCoordinatorWith(statePath, updated(newTestWorkflow(), func(workflow *state.Workflow) {
				workflow.Status = state.StatusWorking
				workflow.Messages = []state.Message{}
			}))
			require.NoError(t, err)
			if tt.goalContent != "" {
				require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "GOAL.md"), []byte(tt.goalContent), 0o644))
			}

			result, err := sendMessage(tmpDir, coord, dagAgents, tt.callerAgent, tt.toAgent, tt.body)
			require.NoError(t, err)
			assert.Contains(t, result, tt.wantContains)
			if len(tt.wantTargets) > 0 {
				messages := coord.State().Messages
				require.Len(t, messages, len(tt.wantTargets))
				for i, wantTarget := range tt.wantTargets {
					assert.Equal(t, wantTarget, messages[i].ToAgent)
				}
			}
		})
	}
}

func TestCheckInbox(t *testing.T) {
	tests := []struct {
		name         string
		callerAgent  string
		messages     []state.Message
		wantContains string
	}{
		{
			name:         "nilCoordinator",
			callerAgent:  "test-agent",
			messages:     nil,
			wantContains: "Error: Could not read state.json",
		},
		{
			name:        "noMessages",
			callerAgent: "test-agent",
			messages: []state.Message{updated(newTestMessage(), func(message *state.Message) {
				message.ID = 1
				message.FromAgent = "coordinator"
				message.ToAgent = "other-agent"
				message.Body = "hello"
			})},
			wantContains: "You have no messages.",
		},
		{
			name:        "hasUnreadMessages",
			callerAgent: "test-agent",
			messages: []state.Message{
				updated(newTestMessage(), func(message *state.Message) {
					message.ID = 1
					message.FromAgent = "coordinator"
					message.ToAgent = "test-agent"
					message.Body = "please do this"
				}),
				updated(newTestMessage(), func(message *state.Message) {
					message.ID = 2
					message.FromAgent = "coordinator"
					message.ToAgent = "test-agent"
					message.Body = "also this"
				}),
			},
			wantContains: "You have 2 message(s):",
		},
		{
			name:        "onlyReadMessages",
			callerAgent: "test-agent",
			messages: []state.Message{updated(newTestMessage(), func(message *state.Message) {
				message.ID = 1
				message.FromAgent = "coordinator"
				message.ToAgent = "test-agent"
				message.Body = "old"
				message.Read = true
			})},
			wantContains: "You have no messages.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "nilCoordinator" {
				result, err := checkInbox(nil, tt.callerAgent)
				require.NoError(t, err)
				assert.Contains(t, result, tt.wantContains)
				return
			}

			tmpDir := t.TempDir()
			statePath := filepath.Join(tmpDir, "state.json")
			coord, err := state.NewCoordinatorWith(statePath, updated(newTestWorkflow(), func(workflow *state.Workflow) {
				workflow.Status = state.StatusWorking
				workflow.Messages = tt.messages
			}))
			require.NoError(t, err)

			result, err := checkInbox(coord, tt.callerAgent)
			require.NoError(t, err)
			assert.Contains(t, result, tt.wantContains)
		})
	}
}

func TestCheckOutbox(t *testing.T) {
	tests := []struct {
		name         string
		callerAgent  string
		messages     []state.Message
		wantContains string
	}{
		{
			name:         "nilCoordinator",
			callerAgent:  "test-agent",
			messages:     nil,
			wantContains: "Error: Could not read state.json",
		},
		{
			name:        "noSentMessages",
			callerAgent: "test-agent",
			messages: []state.Message{updated(newTestMessage(), func(message *state.Message) {
				message.ID = 1
				message.FromAgent = "other-agent"
				message.ToAgent = "test-agent"
				message.Body = "hello"
			})},
			wantContains: "You have not sent any messages.",
		},
		{
			name:        "hasPendingMessages",
			callerAgent: "test-agent",
			messages: []state.Message{updated(newTestMessage(), func(message *state.Message) {
				message.ID = 1
				message.FromAgent = "test-agent"
				message.ToAgent = "coordinator"
				message.Body = "done with work"
			})},
			wantContains: "Pending messages (1):",
		},
		{
			name:        "hasDeliveredMessages",
			callerAgent: "test-agent",
			messages: []state.Message{updated(newTestMessage(), func(message *state.Message) {
				message.ID = 1
				message.FromAgent = "test-agent"
				message.ToAgent = "coordinator"
				message.Body = "done with work"
				message.Read = true
				message.ReadAt = "2026-03-05T10:00:00Z"
			})},
			wantContains: "Delivered messages (1):",
		},
		{
			name:        "mixedPendingAndDelivered",
			callerAgent: "test-agent",
			messages: []state.Message{
				updated(newTestMessage(), func(message *state.Message) {
					message.ID = 1
					message.FromAgent = "test-agent"
					message.ToAgent = "coordinator"
					message.Body = "first msg"
					message.Read = true
					message.ReadAt = "2026-03-05T10:00:00Z"
				}),
				updated(newTestMessage(), func(message *state.Message) {
					message.ID = 2
					message.FromAgent = "test-agent"
					message.ToAgent = "reviewer"
					message.Body = "review request"
				}),
			},
			wantContains: "Pending messages (1):",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "nilCoordinator" {
				result, err := checkOutbox(nil, tt.callerAgent)
				require.NoError(t, err)
				assert.Contains(t, result, tt.wantContains)
				return
			}

			tmpDir := t.TempDir()
			statePath := filepath.Join(tmpDir, "state.json")
			coord, err := state.NewCoordinatorWith(statePath, updated(newTestWorkflow(), func(workflow *state.Workflow) {
				workflow.Status = state.StatusWorking
				workflow.Messages = tt.messages
			}))
			require.NoError(t, err)

			result, err := checkOutbox(coord, tt.callerAgent)
			require.NoError(t, err)
			assert.Contains(t, result, tt.wantContains)
		})
	}
}

func TestPeekMessageBus(t *testing.T) {
	tests := []struct {
		name         string
		messages     []state.Message
		wantContains string
	}{
		{
			name:         "nilCoordinator",
			messages:     nil,
			wantContains: "Error: Could not read state.json",
		},
		{
			name:         "noMessages",
			messages:     []state.Message{},
			wantContains: "No messages in the system.",
		},
		{
			name: "hasMessages",
			messages: []state.Message{
				updated(newTestMessage(), func(message *state.Message) {
					message.ID = 1
					message.FromAgent = "coordinator"
					message.ToAgent = "backend"
					message.Body = "do work"
				}),
				updated(newTestMessage(), func(message *state.Message) {
					message.ID = 2
					message.FromAgent = "backend"
					message.ToAgent = "coordinator"
					message.Body = "done"
					message.Read = true
					message.ReadAt = "2026-03-05T10:00:00Z"
				}),
			},
			wantContains: "Total messages: 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "nilCoordinator" {
				result, err := peekMessageBus(nil)
				require.NoError(t, err)
				assert.Contains(t, result, tt.wantContains)
				return
			}

			tmpDir := t.TempDir()
			statePath := filepath.Join(tmpDir, "state.json")
			coord, err := state.NewCoordinatorWith(statePath, updated(newTestWorkflow(), func(workflow *state.Workflow) {
				workflow.Status = state.StatusWorking
				workflow.Messages = tt.messages
			}))
			require.NoError(t, err)

			result, err := peekMessageBus(coord)
			require.NoError(t, err)
			assert.Contains(t, result, tt.wantContains)
		})
	}
}

func TestFormatTodoList(t *testing.T) {
	tests := []struct {
		name     string
		todos    []state.TodoItem
		contains []string
	}{
		{
			name:     "emptyList",
			todos:    []state.TodoItem{},
			contains: []string{"0 todos"},
		},
		{
			name: "mixedStatuses",
			todos: []state.TodoItem{
				updated(newTestTodoItem(), func(todo *state.TodoItem) {
					todo.Content = "pending task"
					todo.Status = "pending"
					todo.Priority = "high"
				}),
				updated(newTestTodoItem(), func(todo *state.TodoItem) {
					todo.Content = "in progress task"
					todo.Status = "in_progress"
					todo.Priority = "medium"
				}),
				updated(newTestTodoItem(), func(todo *state.TodoItem) {
					todo.Content = "done task"
					todo.Status = "completed"
					todo.Priority = "low"
				}),
				updated(newTestTodoItem(), func(todo *state.TodoItem) {
					todo.Content = "cancelled task"
					todo.Status = "cancelled"
					todo.Priority = "low"
				}),
			},
			contains: []string{"3 todos", "○ pending task", "◐ in progress task", "● done task", "✕ cancelled task"},
		},
		{
			name: "allPending",
			todos: []state.TodoItem{
				updated(newTestTodoItem(), func(todo *state.TodoItem) {
					todo.Content = "task a"
					todo.Status = "pending"
					todo.Priority = "high"
				}),
				updated(newTestTodoItem(), func(todo *state.TodoItem) {
					todo.Content = "task b"
					todo.Status = "pending"
					todo.Priority = "medium"
				}),
			},
			contains: []string{"2 todos"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatTodoList(tt.todos)
			for _, c := range tt.contains {
				assert.Contains(t, result, c)
			}
		})
	}
}

func TestProjectTodoWrite(t *testing.T) {
	t.Run("nilCoordinator", func(t *testing.T) {
		result, err := projectTodoWrite(nil, nil)
		require.NoError(t, err)
		assert.Contains(t, result, "Error: workflow coordinator not available.")
	})

	t.Run("writeTodos", func(t *testing.T) {
		tmpDir := t.TempDir()
		statePath := filepath.Join(tmpDir, "state.json")
		coord, err := state.NewCoordinatorWith(statePath, updated(newTestWorkflow(), func(workflow *state.Workflow) {
			workflow.Status = state.StatusWorking
		}))
		require.NoError(t, err)

		todos := []state.TodoItem{
			updated(newTestTodoItem(), func(todo *state.TodoItem) {
				todo.Content = "first task"
				todo.Status = "pending"
				todo.Priority = "high"
			}),
		}

		result, err := projectTodoWrite(coord, todos)
		require.NoError(t, err)
		assert.Contains(t, result, "first task")

		snapshot := coord.State()
		assert.Len(t, snapshot.ProjectTodos, 1)
	})
}

func TestProjectTodoRead(t *testing.T) {
	t.Run("nilCoordinator", func(t *testing.T) {
		result, err := projectTodoRead(nil)
		require.NoError(t, err)
		assert.Equal(t, "0 todos", result)
	})

	t.Run("withTodos", func(t *testing.T) {
		tmpDir := t.TempDir()
		statePath := filepath.Join(tmpDir, "state.json")
		coord, err := state.NewCoordinatorWith(statePath, updated(newTestWorkflow(), func(workflow *state.Workflow) {
			workflow.Status = state.StatusWorking
			workflow.ProjectTodos = []state.TodoItem{updated(newTestTodoItem(), func(todo *state.TodoItem) {
				todo.Content = "my task"
				todo.Status = "pending"
				todo.Priority = "high"
			})}
		}))
		require.NoError(t, err)

		result, err := projectTodoRead(coord)
		require.NoError(t, err)
		assert.Contains(t, result, "my task")
	})
}

func TestFindSkills(t *testing.T) {
	tests := []struct {
		name       string
		skillName  string
		setup      func(t *testing.T, dir string)
		wantErr    bool
		assertFunc func(t *testing.T, result string)
	}{
		{
			name:      "listAllSkills",
			skillName: "",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				skillDir := filepath.Join(dir, ".sgai", "skills", "test-skill")
				require.NoError(t, os.MkdirAll(skillDir, 0o755))
				content := "---\nname: test-skill\ndescription: A test skill\n---\n# Test Skill"
				require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644))
			},
			assertFunc: func(t *testing.T, result string) {
				t.Helper()
				assert.Contains(t, result, "test-skill")
				assert.Contains(t, result, "A test skill")
			},
			wantErr: false,
		},
		{
			name:      "findByExactMatch",
			skillName: "test-skill",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				skillDir := filepath.Join(dir, ".sgai", "skills", "test-skill")
				require.NoError(t, os.MkdirAll(skillDir, 0o755))
				content := "---\nname: test-skill\ndescription: A test skill\n---\n# Test Skill"
				require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644))
			},
			assertFunc: func(t *testing.T, result string) {
				t.Helper()
				assert.Contains(t, result, "Found skill 'test-skill'")
			},
			wantErr: false,
		},
		{
			name:      "findByPrefix",
			skillName: "coding-practices",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				skillDir := filepath.Join(dir, ".sgai", "skills", "coding-practices", "go-review")
				require.NoError(t, os.MkdirAll(skillDir, 0o755))
				content := "---\nname: go-review\ndescription: Go code review\n---\n# Go Review"
				require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644))
			},
			assertFunc: func(t *testing.T, result string) {
				t.Helper()
				assert.Contains(t, result, "go-review")
			},
			wantErr: false,
		},
		{
			name:      "findByBasename",
			skillName: "go-review",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				skillDir := filepath.Join(dir, ".sgai", "skills", "coding-practices", "go-review")
				require.NoError(t, os.MkdirAll(skillDir, 0o755))
				content := "---\nname: go-review\ndescription: Go code review\n---\n# Go Review"
				require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644))
			},
			assertFunc: func(t *testing.T, result string) {
				t.Helper()
				assert.Contains(t, result, "Found skill 'go-review'")
			},
			wantErr: false,
		},
		{
			name:      "findByFuzzyMatch",
			skillName: "review",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				skillDir := filepath.Join(dir, ".sgai", "skills", "my-review-skill")
				require.NoError(t, os.MkdirAll(skillDir, 0o755))
				content := "---\nname: my-review-skill\ndescription: review stuff\n---\n# Review"
				require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644))
			},
			assertFunc: func(t *testing.T, result string) {
				t.Helper()
				assert.Contains(t, result, "my-review-skill")
			},
			wantErr: false,
		},
		{
			name:      "noMatchReturnsEmpty",
			skillName: "nonexistent-skill-xyz",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				skillDir := filepath.Join(dir, ".sgai", "skills", "test-skill")
				require.NoError(t, os.MkdirAll(skillDir, 0o755))
				content := "---\nname: test-skill\ndescription: something else entirely\n---\n# Test"
				require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644))
			},
			assertFunc: func(t *testing.T, result string) {
				t.Helper()
				assert.Empty(t, result)
			},
			wantErr: false,
		},
		{
			name:       "noSkillsDirectory",
			skillName:  "",
			setup:      func(_ *testing.T, _ string) {},
			wantErr:    true,
			assertFunc: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tt.setup(t, tmpDir)

			result, err := findSkills(tmpDir, tt.skillName)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.assertFunc != nil {
				tt.assertFunc(t, result)
			}
		})
	}
}

func TestFindSnippets(t *testing.T) {
	tests := []struct {
		name       string
		language   string
		query      string
		setup      func(t *testing.T, dir string)
		assertFunc func(t *testing.T, result string)
	}{
		{
			name:     "listLanguages",
			language: "",
			query:    "",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai", "snippets", "go"), 0o755))
				require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai", "snippets", "python"), 0o755))
			},
			assertFunc: func(t *testing.T, result string) {
				t.Helper()
				assert.Contains(t, result, "go")
				assert.Contains(t, result, "python")
			},
		},
		{
			name:     "listSnippetsForLanguage",
			language: "go",
			query:    "",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				langDir := filepath.Join(dir, ".sgai", "snippets", "go")
				require.NoError(t, os.MkdirAll(langDir, 0o755))
				content := "---\ndescription: HTTP server snippet\n---\npackage main\n"
				require.NoError(t, os.WriteFile(filepath.Join(langDir, "http-server.go"), []byte(content), 0o644))
			},
			assertFunc: func(t *testing.T, result string) {
				t.Helper()
				assert.Contains(t, result, "http-server")
				assert.Contains(t, result, "HTTP server snippet")
			},
		},
		{
			name:     "searchSnippets",
			language: "go",
			query:    "http",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				langDir := filepath.Join(dir, ".sgai", "snippets", "go")
				require.NoError(t, os.MkdirAll(langDir, 0o755))
				content := "---\ndescription: HTTP server pattern\n---\npackage main\n"
				require.NoError(t, os.WriteFile(filepath.Join(langDir, "http-server.go"), []byte(content), 0o644))
				content2 := "---\ndescription: JSON encoding\n---\npackage main\n"
				require.NoError(t, os.WriteFile(filepath.Join(langDir, "json-encode.go"), []byte(content2), 0o644))
			},
			assertFunc: func(t *testing.T, result string) {
				t.Helper()
				assert.Contains(t, result, "HTTP server pattern")
			},
		},
		{
			name:     "nonExistentLanguage",
			language: "cobol",
			query:    "",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai", "snippets"), 0o755))
			},
			assertFunc: func(t *testing.T, result string) {
				t.Helper()
				assert.Empty(t, result)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tt.setup(t, tmpDir)

			result, err := findSnippets(tmpDir, tt.language, tt.query)
			require.NoError(t, err)
			if tt.assertFunc != nil {
				tt.assertFunc(t, result)
			}
		})
	}
}

func TestStartMCPHTTPServer(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	coord, errCoord := state.NewCoordinatorWith(stateFile, newTestWorkflow())
	require.NoError(t, errCoord)
	url, closeFn, err := startMCPHTTPServer(t.TempDir(), coord, []string{"builder"})
	require.NoError(t, err)
	assert.NotEmpty(t, url)
	assert.NotNil(t, closeFn)
	t.Cleanup(closeFn)
	assert.Contains(t, url, "http://127.0.0.1:")
	assert.Contains(t, url, "/mcp")
}

func TestBuildMCPHTTPHandler(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	coord, errCoord := state.NewCoordinatorWith(stateFile, newTestWorkflow())
	require.NoError(t, errCoord)
	handler, errHandler := buildMCPHTTPHandler(t.TempDir(), coord, []string{"builder"}, newTestHumanToolCallbacks())
	require.NoError(t, errHandler)
	assert.NotNil(t, handler)
}

func connectInternalMCPClient(t *testing.T, r *http.Request, coord *state.Coordinator, dagAgents []string) *mcp.ClientSession {
	t.Helper()
	mcpServer, errBuild := buildMCPServer(t.TempDir(), r, coord, dagAgents, newTestHumanToolCallbacks())
	require.NoError(t, errBuild)
	ct, st := mcp.NewInMemoryTransports()
	_, errConnect := mcpServer.Connect(context.Background(), st, nil)
	require.NoError(t, errConnect)
	client := mcp.NewClient(newMCPImplementation("test-client"), nil)
	cs, errClient := client.Connect(context.Background(), ct, nil)
	require.NoError(t, errClient)
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func mcpToolNames(tools []*mcp.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

func TestBuildMCPServerOmitsCoordinatorOnlyToolsWithoutAgentHeader(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	coord, errCoord := state.NewCoordinatorWith(stateFile, newTestWorkflow())
	require.NoError(t, errCoord)
	r, _ := http.NewRequest(http.MethodGet, "/", http.NoBody)
	cs := connectInternalMCPClient(t, r, coord, []string{"builder"})
	result, errList := cs.ListTools(context.Background(), new(mcp.ListToolsParams))
	require.NoError(t, errList)
	assert.True(t, slices.Contains(mcpToolNames(result.Tools), "ask_user_question"))
	assert.True(t, slices.Contains(mcpToolNames(result.Tools), "ask_user_work_gate"))
	assert.False(t, slices.Contains(mcpToolNames(result.Tools), "delete_unread_messages"))
}

func TestBuildMCPServerExposesCoordinatorOnlyToolsForCoordinator(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	coord, errCoord := state.NewCoordinatorWith(stateFile, newTestWorkflow())
	require.NoError(t, errCoord)
	r, _ := http.NewRequest(http.MethodGet, "/", http.NoBody)
	r.Header.Set(agentIdentityHeader, "coordinator|")
	cs := connectInternalMCPClient(t, r, coord, []string{"builder", "coordinator"})
	result, errList := cs.ListTools(context.Background(), new(mcp.ListToolsParams))
	require.NoError(t, errList)
	assert.True(t, slices.Contains(mcpToolNames(result.Tools), "delete_unread_messages"))
}

func TestRegisterCommonToolsInternal(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	coord, errCoord := state.NewCoordinatorWith(stateFile, newTestWorkflow())
	require.NoError(t, errCoord)
	server := mcp.NewServer(newMCPImplementation("test"), nil)
	mcpCtx := &mcpContext{workingDir: t.TempDir(), coord: coord, dagAgents: []string{"builder"}, agentName: "builder", humanTools: newTestHumanToolCallbacks()}
	require.NoError(t, registerCommonTools(server, mcpCtx, "builder"))
	assert.NotNil(t, server)
}

func TestRegisterCoordinatorToolsInternal(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	coord, errCoord := state.NewCoordinatorWith(stateFile, newTestWorkflow())
	require.NoError(t, errCoord)
	server := mcp.NewServer(newMCPImplementation("test"), nil)
	mcpCtx := &mcpContext{workingDir: t.TempDir(), coord: coord, dagAgents: []string{"coordinator"}, agentName: "coordinator", humanTools: newTestHumanToolCallbacks()}
	require.NoError(t, registerCoordinatorTools(server, mcpCtx))
	assert.NotNil(t, server)
}

func TestRegisterCoordinatorToolsBrainstormingMode(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	coord, errCoord := state.NewCoordinatorWith(stateFile, updated(newTestWorkflow(), func(workflow *state.Workflow) {
		workflow.InteractionMode = state.ModeBrainstorming
	}))
	require.NoError(t, errCoord)
	server := mcp.NewServer(newMCPImplementation("test"), nil)
	mcpCtx := &mcpContext{workingDir: t.TempDir(), coord: coord, dagAgents: []string{"coordinator"}, agentName: "coordinator", humanTools: newTestHumanToolCallbacks()}
	require.NoError(t, registerCoordinatorTools(server, mcpCtx))
	assert.NotNil(t, server)
}

func TestAskUserQuestionSelfDriveMode(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	coord, errCoord := state.NewCoordinatorWith(stateFile, updated(newTestWorkflow(), func(workflow *state.Workflow) {
		workflow.InteractionMode = state.ModeSelfDrive
	}))
	require.NoError(t, errCoord)
	args := newTestAskUserQuestionArgs(newTestQuestionItemArgs("test?", []string{"yes", "no"}))
	result, err := askUserQuestion(context.Background(), coord, args)
	require.NoError(t, err)
	assert.Equal(t, autoProceedAnswer, result)
}

func TestAskUserWorkGateSelfDriveMode(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	coord, errCoord := state.NewCoordinatorWith(stateFile, updated(newTestWorkflow(), func(workflow *state.Workflow) {
		workflow.InteractionMode = state.ModeSelfDrive
	}))
	require.NoError(t, errCoord)
	result, err := askUserWorkGate(context.Background(), coord, "test summary")
	require.NoError(t, err)
	assert.Equal(t, autoRecordQuestionsAnswer, result)
}

func TestAskUserQuestionNilCoordinator(t *testing.T) {
	args := newTestAskUserQuestionArgs(newTestQuestionItemArgs("test?", []string{"yes", "no"}))
	result, err := askUserQuestion(context.Background(), nil, args)
	require.NoError(t, err)
	assert.Contains(t, result, "workflow coordinator not available")
}

func TestAskUserQuestionEmptyQuestionList(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	coord, errCoord := state.NewCoordinatorWith(stateFile, updated(newTestWorkflow(), func(workflow *state.Workflow) {
		workflow.InteractionMode = state.ModeBrainstorming
	}))
	require.NoError(t, errCoord)
	args := newTestAskUserQuestionArgs()
	result, err := askUserQuestion(context.Background(), coord, args)
	require.NoError(t, err)
	assert.Contains(t, result, "At least one question is required")
}

func TestAskUserQuestionNoChoices(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	coord, errCoord := state.NewCoordinatorWith(stateFile, updated(newTestWorkflow(), func(workflow *state.Workflow) {
		workflow.InteractionMode = state.ModeBrainstorming
	}))
	require.NoError(t, errCoord)
	args := newTestAskUserQuestionArgs(newTestQuestionItemArgs("test?", nil))
	result, err := askUserQuestion(context.Background(), coord, args)
	require.NoError(t, err)
	assert.Contains(t, result, "has no choices")
}

func TestAskUserWorkGateBlankSummary(t *testing.T) {
	result, err := askUserWorkGate(context.Background(), nil, "")
	require.NoError(t, err)
	assert.Contains(t, result, "summary is required")
}

func TestAskUserWorkGateNilCoordinator(t *testing.T) {
	result, err := askUserWorkGate(context.Background(), nil, "my summary")
	require.NoError(t, err)
	assert.Contains(t, result, "workflow coordinator not available")
}

func TestSelectHumanToolCallbacksRetrospectiveDisabled(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "GOAL.md"), []byte("---\nretrospective: \"no\"\n---\n# Goal"), 0o644))
	stateFile := filepath.Join(dir, "state.json")
	coord, errCoord := state.NewCoordinatorWith(stateFile, updated(newTestWorkflow(), func(workflow *state.Workflow) {
		workflow.InteractionMode = state.ModeBrainstorming
	}))
	require.NoError(t, errCoord)

	callbacks := selectHumanToolCallbacks(dir, coord)
	questionAnswer, errQuestion := callbacks.question(context.Background(), coord, newTestAskUserQuestionArgs(newTestQuestionItemArgs("test?", []string{"yes", "no"})))
	require.NoError(t, errQuestion)
	assert.Equal(t, autoProceedAnswer, questionAnswer)

	workGateAnswer, errWorkGate := callbacks.workGate(context.Background(), coord, "summary")
	require.NoError(t, errWorkGate)
	assert.Equal(t, autoSkipRetrospectiveAnswer, workGateAnswer)
	assert.Equal(t, state.ModeBuilding, coord.State().InteractionMode)
}

func TestBuildMCPHTTPHandlerCreation(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")
	coord, errCoord := state.NewCoordinatorWith(stateFile, newTestWorkflow())
	require.NoError(t, errCoord)
	handler, errHandler := buildMCPHTTPHandler(dir, coord, []string{"coordinator"}, newTestHumanToolCallbacks())
	require.NoError(t, errHandler)
	assert.NotNil(t, handler)
}

func TestSearchSnippetsNoEntries(t *testing.T) {
	dir := t.TempDir()
	langDir := filepath.Join(dir, ".sgai", "snippets", "go")
	require.NoError(t, os.MkdirAll(langDir, 0o755))
	entries, _ := os.ReadDir(langDir)
	result, err := searchSnippets(langDir, entries, "test")
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestSearchSnippetsWithEntries(t *testing.T) {
	dir := t.TempDir()
	langDir := filepath.Join(dir, ".sgai", "snippets", "go")
	require.NoError(t, os.MkdirAll(langDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(langDir, "hello.go"), []byte("---\ndescription: Hello World\n---\npackage main"), 0o644))
	entries, _ := os.ReadDir(langDir)
	result, err := searchSnippets(langDir, entries, "hello")
	require.NoError(t, err)
	assert.NotEmpty(t, result)
}

func TestSearchSnippetsNoMatch(t *testing.T) {
	dir := t.TempDir()
	langDir := filepath.Join(dir, ".sgai", "snippets", "go")
	require.NoError(t, os.MkdirAll(langDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(langDir, "hello.go"), []byte("---\ndescription: Hello World\n---\npackage main"), 0o644))
	entries, _ := os.ReadDir(langDir)
	result, err := searchSnippets(langDir, entries, "nonexistent-xyz")
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestFindSnippetsByNameContainsMatch(t *testing.T) {
	dir := t.TempDir()
	snippetDir := filepath.Join(dir, ".sgai", "snippets", "go")
	require.NoError(t, os.MkdirAll(snippetDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(snippetDir, "http-handler.go"), []byte("---\ndescription: HTTP Handler\n---\npackage main"), 0o644))
	snippetFiles, err := os.ReadDir(snippetDir)
	require.NoError(t, err)
	result := findSnippetsByNameContains(snippetDir, snippetFiles, "http")
	assert.NotEmpty(t, result)
}

func TestFindSnippetsByNameContainsNoMatch(t *testing.T) {
	dir := t.TempDir()
	snippetDir := filepath.Join(dir, ".sgai", "snippets", "go")
	require.NoError(t, os.MkdirAll(snippetDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(snippetDir, "hello.go"), []byte("---\ndescription: Hello\n---\npackage main"), 0o644))
	snippetFiles, err := os.ReadDir(snippetDir)
	require.NoError(t, err)
	result := findSnippetsByNameContains(snippetDir, snippetFiles, "nonexistent")
	assert.Empty(t, result)
}

func TestAskUserQuestionWithValidCoordinator(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	coord, err := state.NewCoordinatorWith(stateFile, updated(newTestWorkflow(), func(workflow *state.Workflow) {
		workflow.InteractionMode = state.ModeBrainstorming
	}))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			wf := coord.State()
			if wf.NeedsHumanInput() {
				coord.Respond("my answer")
				return
			}
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
	}()
	t.Cleanup(cancel)

	result, err := askUserQuestion(ctx, coord, askUserQuestionArgs{Questions: []questionItem{newTestQuestionItemArgs("What color?", []string{"red", "blue"})}})
	require.NoError(t, err)
	assert.Contains(t, result, "What color?")
	assert.Contains(t, result, "my answer")
}

func TestAskUserWorkGateWithValidCoordinator(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	coord, err := state.NewCoordinatorWith(stateFile, updated(newTestWorkflow(), func(workflow *state.Workflow) {
		workflow.InteractionMode = state.ModeBrainstorming
	}))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			wf := coord.State()
			if wf.NeedsHumanInput() {
				coord.Respond("DEFINITION IS COMPLETE, BUILD MAY BEGIN")
				return
			}
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
	}()
	t.Cleanup(cancel)

	result, err := askUserWorkGate(ctx, coord, "here is my summary")
	require.NoError(t, err)
	assert.Contains(t, result, "here is my summary")
	assert.Contains(t, result, "DEFINITION IS COMPLETE")
}

func TestFindSnippetsNoLanguage(t *testing.T) {
	dir := t.TempDir()
	snippetsDir := filepath.Join(dir, ".sgai", "snippets")
	require.NoError(t, os.MkdirAll(filepath.Join(snippetsDir, "go"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(snippetsDir, "python"), 0o755))
	result, err := findSnippets(dir, "", "")
	require.NoError(t, err)
	assert.Contains(t, result, "go")
	assert.Contains(t, result, "python")
}

func TestFindSnippetsWithLanguageNoQuery(t *testing.T) {
	dir := t.TempDir()
	snippetsDir := filepath.Join(dir, ".sgai", "snippets", "go")
	require.NoError(t, os.MkdirAll(snippetsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(snippetsDir, "http-server.go"), []byte("---\ndescription: HTTP server\n---\npackage main"), 0o644))
	result, err := findSnippets(dir, "go", "")
	require.NoError(t, err)
	assert.Contains(t, result, "http-server")
}

func TestFindSnippetsNonexistentLanguage(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai", "snippets"), 0o755))
	result, err := findSnippets(dir, "rust", "")
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestFindSkillsEmptyName(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, ".sgai", "skills", "test-skill")
	require.NoError(t, os.MkdirAll(skillsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillsDir, "SKILL.md"), []byte("---\nname: Test Skill\ndescription: A test skill\n---\n# Test Skill"), 0o644))
	result, err := findSkills(dir, "")
	require.NoError(t, err)
	assert.Contains(t, result, "Test Skill")
}

func TestFindSkillsExactMatch(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, ".sgai", "skills", "test-skill")
	require.NoError(t, os.MkdirAll(skillsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillsDir, "SKILL.md"), []byte("---\nname: Test Skill\ndescription: A test skill\n---\n# Test Skill"), 0o644))
	result, err := findSkills(dir, "test-skill")
	require.NoError(t, err)
	assert.NotEmpty(t, result)
}

func TestFindSkillsByBasenameSingleMatch(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, ".sgai", "skills", "category", "my-skill")
	require.NoError(t, os.MkdirAll(skillsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillsDir, "SKILL.md"), []byte("---\nname: My Skill\ndescription: A great skill\n---\n# My Skill content"), 0o644))
	skillFiles, err := collectSkillFiles(filepath.Join(dir, ".sgai", "skills"))
	require.NoError(t, err)
	result := findSkillsByBasename(filepath.Join(dir, ".sgai", "skills"), skillFiles, "my-skill")
	assert.NotEmpty(t, result)
}

func TestFindSkillsByBasenameMultipleMatches(t *testing.T) {
	dir := t.TempDir()
	skillsDir1 := filepath.Join(dir, ".sgai", "skills", "cat1", "shared-name")
	skillsDir2 := filepath.Join(dir, ".sgai", "skills", "cat2", "shared-name")
	require.NoError(t, os.MkdirAll(skillsDir1, 0o755))
	require.NoError(t, os.MkdirAll(skillsDir2, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillsDir1, "SKILL.md"), []byte("---\nname: Skill A\ndescription: First\n---\n# A"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(skillsDir2, "SKILL.md"), []byte("---\nname: Skill B\ndescription: Second\n---\n# B"), 0o644))
	skillFiles, err := collectSkillFiles(filepath.Join(dir, ".sgai", "skills"))
	require.NoError(t, err)
	result := findSkillsByBasename(filepath.Join(dir, ".sgai", "skills"), skillFiles, "shared-name")
	assert.Contains(t, result, "Skill A")
	assert.Contains(t, result, "Skill B")
}

func TestFindSnippetsByNameContainsMultipleMatches(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "http-server.go"), []byte("---\ndescription: HTTP server\n---\npackage main"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "http-client.go"), []byte("---\ndescription: HTTP client\n---\npackage main"), 0o644))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	result := findSnippetsByNameContains(dir, entries, "http")
	assert.Contains(t, result, "http-server")
	assert.Contains(t, result, "http-client")
}

func TestFindSnippetsByNameContainsSingleMatch(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "unique-snippet.go"), []byte("---\ndescription: Unique\n---\npackage main\nfunc unique() {}"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.go"), []byte("---\ndescription: Other\n---\npackage main"), 0o644))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	result := findSnippetsByNameContains(dir, entries, "unique")
	assert.Contains(t, result, "func unique()")
}

func TestFindSnippetsByNameContainsNoDescription(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "http-handler.go"), []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "http-router.go"), []byte("package main"), 0o644))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	result := findSnippetsByNameContains(dir, entries, "http")
	assert.Contains(t, result, "No description")
}

func TestUpdateWorkflowStateInvalidStatus(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	coord, err := state.NewCoordinatorWith(stateFile, newTestWorkflow())
	require.NoError(t, err)
	result, err := updateWorkflowState(coord, "builder", newTestUpdateWorkflowStateArgs("invalid-status", "test task", ""))
	require.NoError(t, err)
	assert.Contains(t, result, "Invalid status")
}

func TestUpdateWorkflowStateRejectsCompleteForNonCoordinator(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	coord, err := state.NewCoordinatorWith(stateFile, updated(newTestWorkflow(), func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
	}))
	require.NoError(t, err)

	result, err := updateWorkflowState(coord, "builder", newTestUpdateWorkflowStateArgs(state.StatusComplete, "", ""))
	require.NoError(t, err)
	assert.Contains(t, result, "Invalid status")
	assert.Equal(t, state.StatusWorking, coord.State().Status)
}

func TestUpdateWorkflowStateAllowsCompleteForCoordinator(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	coord, err := state.NewCoordinatorWith(stateFile, newTestWorkflow())
	require.NoError(t, err)

	result, err := updateWorkflowState(coord, "coordinator", newTestUpdateWorkflowStateArgs(state.StatusComplete, "", ""))
	require.NoError(t, err)
	assert.Contains(t, result, "State updated successfully")
	assert.Equal(t, state.StatusComplete, coord.State().Status)
}

func TestUpdateWorkflowStateWithPendingTodos(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	coord, err := state.NewCoordinatorWith(stateFile, updated(newTestWorkflow(), func(workflow *state.Workflow) {
		workflow.CurrentAgent = "builder"
		workflow.Todos = []state.TodoItem{updated(newTestTodoItem(), func(todo *state.TodoItem) {
			todo.Content = "unfinished task"
			todo.Status = "pending"
			todo.Priority = "high"
		})}
	}))
	require.NoError(t, err)
	result, err := updateWorkflowState(coord, "builder", newTestUpdateWorkflowStateArgs("agent-done", "", ""))
	require.NoError(t, err)
	assert.Contains(t, result, "pending TODO")
}

func TestUpdateWorkflowStatePendingTodosDoesNotMutateStatusOrStartWatchdog(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	coord, err := state.NewCoordinatorWith(stateFile, updated(newTestWorkflow(), func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.CurrentAgent = "builder"
		workflow.Todos = []state.TodoItem{updated(newTestTodoItem(), func(todo *state.TodoItem) {
			todo.Content = "unfinished task"
			todo.Status = "pending"
			todo.Priority = "high"
		})}
	}))
	require.NoError(t, err)
	coord.SetAgentCancel(func() {})

	result, err := updateWorkflowState(coord, "builder", newTestUpdateWorkflowStateArgs("agent-done", "", ""))
	require.NoError(t, err)
	assert.Contains(t, result, "pending TODO")
	assert.Equal(t, state.StatusWorking, coord.State().Status)
	assert.False(t, coord.IsShuttingDown())

	reloaded, err := state.NewCoordinator(stateFile)
	require.NoError(t, err)
	assert.Equal(t, state.StatusWorking, reloaded.State().Status)
}

func TestUpdateWorkflowStateUsesCallerAgentForPendingTodos(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	coord, err := state.NewCoordinatorWith(stateFile, updated(newTestWorkflow(), func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.CurrentAgent = "coordinator"
		workflow.Todos = []state.TodoItem{updated(newTestTodoItem(), func(todo *state.TodoItem) {
			todo.Content = "unfinished task"
			todo.Status = "pending"
			todo.Priority = "high"
		})}
	}))
	require.NoError(t, err)

	result, err := updateWorkflowState(coord, "builder", newTestUpdateWorkflowStateArgs(state.StatusAgentDone, "", ""))
	require.NoError(t, err)
	assert.Contains(t, result, "pending TODO")
	assert.Equal(t, state.StatusWorking, coord.State().Status)
}

func TestUpdateWorkflowStateCoordinatorUsesProjectTodos(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	coord, err := state.NewCoordinatorWith(stateFile, updated(newTestWorkflow(), func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.CurrentAgent = "coordinator"
		workflow.ProjectTodos = []state.TodoItem{updated(newTestTodoItem(), func(todo *state.TodoItem) {
			todo.Content = "unfinished project task"
			todo.Status = "pending"
			todo.Priority = "high"
		})}
	}))
	require.NoError(t, err)

	result, err := updateWorkflowState(coord, "coordinator", newTestUpdateWorkflowStateArgs(state.StatusAgentDone, "", ""))
	require.NoError(t, err)
	assert.Contains(t, result, "pending TODO")
	assert.Equal(t, state.StatusWorking, coord.State().Status)
}

func TestUpdateWorkflowStateWithHumanInput(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	coord, err := state.NewCoordinatorWith(stateFile, updated(newTestWorkflow(), func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.HumanMessage = "waiting"
	}))
	require.NoError(t, err)
	result, err := updateWorkflowState(coord, "builder", newTestUpdateWorkflowStateArgs("working", "new task", "doing stuff"))
	require.NoError(t, err)
	assert.Contains(t, result, "updated successfully")
}

func TestUpdateWorkflowStateClearsTaskOnComplete(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	coord, err := state.NewCoordinatorWith(stateFile, updated(newTestWorkflow(), func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
	}))
	require.NoError(t, err)
	result, err := updateWorkflowState(coord, "builder", newTestUpdateWorkflowStateArgs("agent-done", "should be cleared", ""))
	require.NoError(t, err)
	assert.Contains(t, result, "updated successfully")
	wf := coord.State()
	assert.Empty(t, wf.Task)
}

func TestSendMessageInvalidAgent(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	coord, err := state.NewCoordinatorWith(stateFile, newTestWorkflow())
	require.NoError(t, err)
	result, err := sendMessage("", coord, []string{"coordinator", "builder"}, "builder", "nonexistent-agent", "hello")
	require.NoError(t, err)
	assert.Contains(t, result, "not in the workflow")
}

func TestSendMessageValidAgent(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	coord, err := state.NewCoordinatorWith(stateFile, newTestWorkflow())
	require.NoError(t, err)
	result, err := sendMessage("", coord, []string{"coordinator", "builder"}, "builder", "coordinator", "hello from builder")
	require.NoError(t, err)
	assert.Contains(t, result, "sent")
}

func TestListSnippetLanguagesNoDir(t *testing.T) {
	result, err := listSnippetLanguages(filepath.Join(t.TempDir(), "nonexistent"))
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestListSnippetsForLanguageWithDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "subdir"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.go"), []byte("---\ndescription: Test snippet\n---\npackage main"), 0o644))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	result, err := listSnippetsForLanguage(dir, entries)
	require.NoError(t, err)
	assert.Contains(t, result, "test")
	assert.Contains(t, result, "Test snippet")
}

func TestBuildMCPHTTPHandlerWithHandler(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai"), 0o755))
	stateFile := filepath.Join(dir, ".sgai", "state.json")
	_, errCoord := state.NewCoordinatorWith(stateFile, newTestWorkflow())
	require.NoError(t, errCoord)
	srv, _ := setupTestServer(t)
	srv.rootDir = dir
	handler, errHandler := buildMCPHTTPHandler(dir, srv.workspaceCoordinator(dir), []string{"coordinator"}, newTestHumanToolCallbacks())
	require.NoError(t, errHandler)
	assert.NotNil(t, handler)
	assert.Implements(t, (*http.Handler)(nil), handler)
}
