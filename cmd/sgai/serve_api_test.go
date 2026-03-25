package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"testing/synctest"
	"time"

	"github.com/adrg/xdg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ucirello/sgai/pkg/state"
)

func newTestServeSession(coord *state.Coordinator, running bool) *session {
	sess := newTestSession()
	sess.coord = coord
	sess.running = running
	return sess
}

type lockedResponseRecorder struct {
	header http.Header
	body   bytes.Buffer
	code   int
	mu     sync.Mutex
}

func newLockedResponseRecorder() *lockedResponseRecorder {
	var recorder lockedResponseRecorder
	recorder.header = make(http.Header)
	return &recorder
}

func (w *lockedResponseRecorder) Header() http.Header {
	return w.header
}

func (w *lockedResponseRecorder) WriteHeader(statusCode int) {
	if w.code == 0 {
		w.code = statusCode
	}
}

func (w *lockedResponseRecorder) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.code == 0 {
		w.code = http.StatusOK
	}
	n, errWrite := w.body.Write(p)
	if errWrite != nil {
		return n, fmt.Errorf("write locked response body: %w", errWrite)
	}
	return n, nil
}

func (w *lockedResponseRecorder) Flush() {}

func (w *lockedResponseRecorder) BodyString() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.String()
}

func workflowWith(update func(*state.Workflow)) state.Workflow {
	return updated(newTestWorkflow(), update)
}

func messageWith(update func(*state.Message)) state.Message {
	return updated(newTestMessage(), update)
}

func questionItemWith(update func(*state.QuestionItem)) state.QuestionItem {
	return updated(newTestQuestionItem(), update)
}

func multiChoiceQuestionWith(update func(*state.MultiChoiceQuestion)) *state.MultiChoiceQuestion {
	question := updated(newTestMultiChoiceQuestion(), update)
	return &question
}

func workspaceWith(update func(*workspaceInfo)) workspaceInfo {
	return updated(newTestWorkspaceInfo(), update)
}

func respondRequestWith(update func(*apiRespondRequest)) apiRespondRequest {
	return updated(newTestAPIRespondRequest(), update)
}

func eventsDisplayWith(update func(*eventsProgressDisplay)) eventsProgressDisplay {
	return updated(newTestEventsProgressDisplay(), update)
}

func projectConfigWith(update func(*projectConfig)) projectConfig {
	return updated(newTestProjectConfig(), update)
}

func actionConfigWith(update func(*actionConfig)) actionConfig {
	return updated(newTestActionConfig(), update)
}

func apiActionEntryWith(update func(*apiActionEntry)) apiActionEntry {
	return updated(newTestAPIActionEntry(), update)
}

func workspaceGroupWith(update func(*workspaceGroup)) workspaceGroup {
	return updated(newTestWorkspaceGroup(), update)
}

func todoItemWith(update func(*state.TodoItem)) state.TodoItem {
	return updated(newTestTodoItem(), update)
}

func workflowRef(update func(*state.Workflow)) *state.Workflow {
	wf := workflowWith(update)
	return &wf
}

type recordingEditor struct {
	path string
	err  error
}

func (e *recordingEditor) open(path string) error {
	e.path = path
	return e.err
}

func attachSessionCoordinator(t *testing.T, srv *Server, wsDir string, wf *state.Workflow) {
	t.Helper()
	attachSessionCoordinatorWithRunning(t, srv, wsDir, wf, false)
}

func attachRunningSessionCoordinator(t *testing.T, srv *Server, wsDir string, wf *state.Workflow) {
	t.Helper()
	attachSessionCoordinatorWithRunning(t, srv, wsDir, wf, true)
}

func attachSessionCoordinatorWithRunning(t *testing.T, srv *Server, wsDir string, wf *state.Workflow, running bool) {
	t.Helper()
	statePath := filepath.Join(wsDir, ".sgai", "state.json")
	coord := state.NewCoordinatorEmpty(statePath)
	require.NoError(t, coord.UpdateState(func(current *state.Workflow) {
		*current = *wf
	}))
	srv.mu.Lock()
	srv.sessions[wsDir] = newTestServeSession(coord, running)
	srv.mu.Unlock()
}

func stopCachedSession(t *testing.T, srv *Server, wsDir string, wf *state.Workflow) *state.Coordinator {
	t.Helper()

	coord, errCoord := state.NewCoordinatorWith(filepath.Join(wsDir, ".sgai", "state.json"), *wf)
	require.NoError(t, errCoord)

	srv.mu.Lock()
	srv.sessions[wsDir] = newTestServeSession(coord, true)
	srv.mu.Unlock()

	srv.stopSession(wsDir)

	return coord
}

func writeWorkflowStateToDisk(t *testing.T, wsDir string, wf *state.Workflow) {
	t.Helper()

	_, errCoord := state.NewCoordinatorWith(filepath.Join(wsDir, ".sgai", "state.json"), *wf)
	require.NoError(t, errCoord)
}

func writeWorkflowStateWithLaterModTime(t *testing.T, wsDir string, wf *state.Workflow) {
	t.Helper()

	info, errStat := os.Stat(statePath(wsDir))
	require.NoError(t, errStat)

	writeWorkflowStateToDisk(t, wsDir, wf)
	setLaterFileModTime(t, statePath(wsDir), info.ModTime().Add(time.Second))
}

func startWaitingSessionQuestion(t *testing.T, srv *Server, wsDir string, question *state.MultiChoiceQuestion, humanMessage string) (*state.Coordinator, <-chan error, context.CancelFunc) {
	t.Helper()
	statePath := filepath.Join(wsDir, ".sgai", "state.json")
	coord := state.NewCoordinatorEmpty(statePath)
	ready := make(chan struct{}, 1)
	coord.OnUpdate(func() {
		if coord.State().NeedsHumanInput() {
			select {
			case ready <- struct{}{}:
			default:
			}
		}
	})
	srv.mu.Lock()
	srv.sessions[wsDir] = newTestServeSession(coord, true)
	srv.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := coord.AskAndWait(ctx, question, humanMessage)
		errCh <- err
	}()
	<-ready
	return coord, errCh, cancel
}

func TestIsAPIRoute(t *testing.T) {
	tests := []struct {
		name     string
		urlPath  string
		expected bool
	}{
		{
			name:     "apiRoute",
			urlPath:  "/api/v1/workspaces",
			expected: true,
		},
		{
			name:     "mcpRoute",
			urlPath:  "/mcp/tools",
			expected: true,
		},
		{
			name:     "rootPath",
			urlPath:  "/",
			expected: false,
		},
		{
			name:     "workspacePath",
			urlPath:  "/workspaces/test",
			expected: false,
		},
		{
			name:     "staticAsset",
			urlPath:  "/assets/main.js",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAPIRoute(tt.urlPath)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsStaticAsset(t *testing.T) {
	tests := []struct {
		name     string
		urlPath  string
		expected bool
	}{
		{
			name:     "jsFile",
			urlPath:  "/assets/main.js",
			expected: true,
		},
		{
			name:     "cssFile",
			urlPath:  "/styles/main.css",
			expected: true,
		},
		{
			name:     "mapFile",
			urlPath:  "/assets/main.js.map",
			expected: true,
		},
		{
			name:     "pngFile",
			urlPath:  "/images/logo.png",
			expected: true,
		},
		{
			name:     "svgFile",
			urlPath:  "/images/icon.svg",
			expected: true,
		},
		{
			name:     "icoFile",
			urlPath:  "/favicon.ico",
			expected: true,
		},
		{
			name:     "woffFile",
			urlPath:  "/fonts/main.woff",
			expected: true,
		},
		{
			name:     "woff2File",
			urlPath:  "/fonts/main.woff2",
			expected: true,
		},
		{
			name:     "ttfFile",
			urlPath:  "/fonts/main.ttf",
			expected: true,
		},
		{
			name:     "jsonFile",
			urlPath:  "/data/config.json",
			expected: true,
		},
		{
			name:     "htmlFile",
			urlPath:  "/page.html",
			expected: false,
		},
		{
			name:     "noExtension",
			urlPath:  "/path/to/resource",
			expected: false,
		},
		{
			name:     "rootPath",
			urlPath:  "/",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isStaticAsset(tt.urlPath)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFindSteerInsertPosition(t *testing.T) {
	tests := []struct {
		name     string
		messages []state.Message
		expected int
	}{
		{
			name:     "empty",
			messages: []state.Message{},
			expected: 0,
		},
		{
			name: "allRead",
			messages: []state.Message{
				messageWith(func(message *state.Message) { message.ID = 1; message.Read = true }),
				messageWith(func(message *state.Message) { message.ID = 2; message.Read = true }),
			},
			expected: 0,
		},
		{
			name: "firstUnread",
			messages: []state.Message{
				messageWith(func(message *state.Message) { message.ID = 1; message.Read = true }),
				messageWith(func(message *state.Message) { message.ID = 2 }),
				messageWith(func(message *state.Message) { message.ID = 3 }),
			},
			expected: 1,
		},
		{
			name: "allUnread",
			messages: []state.Message{
				messageWith(func(message *state.Message) { message.ID = 1 }),
				messageWith(func(message *state.Message) { message.ID = 2 }),
			},
			expected: 0,
		},
		{
			name: "lastUnread",
			messages: []state.Message{
				messageWith(func(message *state.Message) { message.ID = 1; message.Read = true }),
				messageWith(func(message *state.Message) { message.ID = 2; message.Read = true }),
				messageWith(func(message *state.Message) { message.ID = 3 }),
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findSteerInsertPosition(tt.messages)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestQuestionType(t *testing.T) {
	tests := []struct {
		name     string
		wfState  state.Workflow
		expected string
	}{
		{
			name:     "noQuestion",
			wfState:  workflowWith(func(workflow *state.Workflow) { workflow.Status = state.StatusWorking }),
			expected: "",
		},
		{
			name: "freeText",
			wfState: workflowWith(func(workflow *state.Workflow) {
				workflow.Status = state.StatusWorking
				workflow.HumanMessage = "What should I do?"
			}),
			expected: "free-text",
		},
		{
			name: "multiChoice",
			wfState: workflowWith(func(workflow *state.Workflow) {
				workflow.Status = state.StatusWorking
				workflow.MultiChoiceQuestion = multiChoiceQuestionWith(func(question *state.MultiChoiceQuestion) {
					question.Questions = []state.QuestionItem{
						questionItemWith(func(item *state.QuestionItem) { item.Question = "Choose an option" }),
					}
				})
			}),
			expected: "multi-choice",
		},
		{
			name: "workGate",
			wfState: workflowWith(func(workflow *state.Workflow) {
				workflow.Status = state.StatusWorking
				workflow.MultiChoiceQuestion = multiChoiceQuestionWith(func(question *state.MultiChoiceQuestion) {
					question.Questions = []state.QuestionItem{
						questionItemWith(func(item *state.QuestionItem) { item.Question = "Approve?" }),
					}
					question.IsWorkGate = true
				})
			}),
			expected: "work-gate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := questionType(&tt.wfState)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildAPIResponseText(t *testing.T) {
	tests := []struct {
		name     string
		req      apiRespondRequest
		expected string
	}{
		{
			name:     "answerOnly",
			req:      respondRequestWith(func(req *apiRespondRequest) { req.Answer = "My answer" }),
			expected: "My answer",
		},
		{
			name:     "selectedChoicesOnly",
			req:      respondRequestWith(func(req *apiRespondRequest) { req.SelectedChoices = []string{"Option A", "Option B"} }),
			expected: "Selected: Option A, Option B",
		},
		{
			name: "bothAnswerAndChoices",
			req: respondRequestWith(func(req *apiRespondRequest) {
				req.Answer = "My answer"
				req.SelectedChoices = []string{"Option A"}
			}),
			expected: "Selected: Option A\nMy answer",
		},
		{
			name:     "empty",
			req:      respondRequestWith(func(req *apiRespondRequest) { req.SelectedChoices = []string{} }),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildAPIResponseText(tt.req)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertEventsForAPIBoost(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		result := convertEventsForAPI(nil)
		assert.Empty(t, result)
	})

	t.Run("withEvents", func(t *testing.T) {
		displays := []eventsProgressDisplay{
			eventsDisplayWith(func(display *eventsProgressDisplay) {
				display.Agent = "coordinator"
				display.Description = "started work"
				display.Timestamp = "2025-01-01"
			}),
			eventsDisplayWith(func(display *eventsProgressDisplay) {
				display.Agent = "developer"
				display.Description = "writing code"
				display.Timestamp = "2025-01-02"
			}),
		}

		result := convertEventsForAPI(displays)
		assert.Len(t, result, 2)
		assert.Equal(t, "coordinator", result[0].Agent)
		assert.Equal(t, "started work", result[0].Description)
	})
}

func TestResolveCurrentModelVariants(t *testing.T) {
	t.Run("fromState", func(t *testing.T) {
		wfState := workflowWith(func(workflow *state.Workflow) { workflow.CurrentModel = "claude-opus-4" })
		result := resolveCurrentModel("/some/path", &wfState)
		assert.Equal(t, "claude-opus-4", result)
	})

	t.Run("noAgent", func(t *testing.T) {
		wfState := newTestWorkflow()
		result := resolveCurrentModel("/some/path", &wfState)
		assert.Empty(t, result)
	})

	t.Run("fromGoalFile", func(t *testing.T) {
		dir := t.TempDir()
		goalPath := filepath.Join(dir, "GOAL.md")
		content := "---\nmodels:\n  coordinator: claude-opus-4\n---\n# Goal"
		require.NoError(t, os.WriteFile(goalPath, []byte(content), 0o644))

		wfState := workflowWith(func(workflow *state.Workflow) { workflow.CurrentAgent = "coordinator" })
		result := resolveCurrentModel(dir, &wfState)
		assert.Equal(t, "claude-opus-4", result)
	})

	t.Run("agentNotInGoal", func(t *testing.T) {
		dir := t.TempDir()
		goalPath := filepath.Join(dir, "GOAL.md")
		content := "---\nmodels:\n  coordinator: claude-opus-4\n---\n# Goal"
		require.NoError(t, os.WriteFile(goalPath, []byte(content), 0o644))

		wfState := workflowWith(func(workflow *state.Workflow) { workflow.CurrentAgent = "developer" })
		result := resolveCurrentModel(dir, &wfState)
		assert.Empty(t, result)
	})

	t.Run("withExplicitModel", func(t *testing.T) {
		wf := workflowWith(func(workflow *state.Workflow) { workflow.CurrentModel = "opus-4" })
		result := resolveCurrentModel("/tmp", &wf)
		assert.Equal(t, "opus-4", result)
	})

	t.Run("noAgentReturnsEmpty", func(t *testing.T) {
		wf := newTestWorkflow()
		result := resolveCurrentModel("/tmp", &wf)
		assert.Empty(t, result)
	})

	t.Run("noModelReturnsEmpty", func(t *testing.T) {
		dir := t.TempDir()
		wf := newTestWorkflow()
		result := resolveCurrentModel(dir, &wf)
		assert.Empty(t, result)
	})
}

func TestCollectAgentModelsVariants(t *testing.T) {
	t.Run("noGoalFile", func(t *testing.T) {
		dir := t.TempDir()
		result := collectAgentModels(dir)
		assert.Nil(t, result)
	})

	t.Run("noModels", func(t *testing.T) {
		dir := t.TempDir()
		goalPath := filepath.Join(dir, "GOAL.md")
		require.NoError(t, os.WriteFile(goalPath, []byte("# No frontmatter"), 0o644))

		result := collectAgentModels(dir)
		assert.Nil(t, result)
	})

	t.Run("withModels", func(t *testing.T) {
		dir := t.TempDir()
		goalPath := filepath.Join(dir, "GOAL.md")
		content := "---\nmodels:\n  coordinator: claude-opus-4\n  developer: gpt-4\n---\n# Goal"
		require.NoError(t, os.WriteFile(goalPath, []byte(content), 0o644))

		result := collectAgentModels(dir)
		require.Len(t, result, 2)
		assert.Equal(t, "coordinator", result[0].Agent)
		assert.Equal(t, []string{"claude-opus-4"}, result[0].Models)
		assert.Equal(t, "developer", result[1].Agent)
		assert.Equal(t, []string{"gpt-4"}, result[1].Models)
	})

	t.Run("noAgents", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai", "agent"), 0o755))
		result := collectAgentModels(dir)
		assert.Empty(t, result)
	})

	t.Run("withGoal", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "GOAL.md"),
			[]byte("---\nmodels:\n  coordinator: anthropic/claude-opus-4-6\n---\n# Goal"), 0o644))

		result := collectAgentModels(dir)
		assert.NotNil(t, result)
	})

	t.Run("noGoalReturnsNil", func(t *testing.T) {
		result := collectAgentModels(t.TempDir())
		assert.Nil(t, result)
	})
}

func TestBuildAdhocArgs(t *testing.T) {
	t.Run("simpleModel", func(t *testing.T) {
		args := buildAdhocArgs("claude-opus-4")
		assert.Equal(t, []string{"run", "-m", "claude-opus-4", "--agent", "build", "--title", "adhoc [claude-opus-4]"}, args)
	})

	t.Run("modelWithVariant", func(t *testing.T) {
		args := buildAdhocArgs("claude-opus-4:fast")
		assert.Contains(t, args, "run")
		assert.Contains(t, args, "-m")
		assert.Contains(t, args, "--agent")
		assert.Contains(t, args, "build")
		assert.Contains(t, args, "adhoc [claude-opus-4:fast]")
	})

	t.Run("withVariantAddsFlag", func(t *testing.T) {
		args := buildAdhocArgs("anthropic/claude-sonnet-4-6 (thinking)")
		assert.Contains(t, args, "--variant")
		assert.Contains(t, args, "thinking")
	})

	t.Run("withoutVariantNoFlag", func(t *testing.T) {
		args := buildAdhocArgs("anthropic/claude-sonnet-4-6")
		for _, arg := range args {
			assert.NotEqual(t, "--variant", arg)
		}
	})
}

func TestCoordinatorModelFromWorkspace(t *testing.T) {
	tests := []struct {
		name          string
		workspaceName string
		goalContent   string
		want          string
	}{
		{
			name:          "emptyWorkspace",
			workspaceName: "",
			goalContent:   "",
			want:          "",
		},
		{
			name:          "nonexistentWorkspace",
			workspaceName: "nonexistent",
			goalContent:   "",
			want:          "",
		},
		{
			name:          "workspaceWithModel",
			workspaceName: "test-ws",
			goalContent:   "---\nmodels:\n  coordinator: anthropic/claude-sonnet-4-6\nflow: |\n  \"coordinator\"\n---\n# Test",
			want:          "anthropic/claude-sonnet-4-6",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, rootDir := setupTestServer(t)
			if tt.workspaceName != "" && tt.workspaceName != "nonexistent" {
				wsDir := setupTestWorkspace(t, server, rootDir, tt.workspaceName)
				require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte(tt.goalContent), 0o644))
			}

			result := server.coordinatorModelFromWorkspace(tt.workspaceName)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestSessionCoordinator(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "sess-coord-ws")
	sp := filepath.Join(wsDir, ".sgai", "state.json")
	_, errCoord := state.NewCoordinatorWith(sp, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
	}))
	require.NoError(t, errCoord)

	coord := srv.sessionCoordinator(wsDir)
	assert.Nil(t, coord)
}

func TestWriteJSONResponse(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "json-ws")

	w := serveHTTP(srv, "GET", "/api/v1/workspaces", "")
	assert.Equal(t, http.StatusOK, w.Code)

	var resp json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
}

func TestWriteJSONContentType(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "json-ct-ws")
	w := serveHTTP(srv, "GET", "/api/v1/models", "")
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
}

func TestConvertSnippetLanguagesEmpty(t *testing.T) {
	result := convertSnippetLanguages(nil)
	assert.Empty(t, result)
}

func TestIsAPIRouteVariants(t *testing.T) {
	assert.True(t, isAPIRoute("/api/v1/workspaces"))
	assert.True(t, isAPIRoute("/api/v1/agents"))
	assert.False(t, isAPIRoute("/"))
	assert.False(t, isAPIRoute("/index.html"))
}

func TestIsStaticAssetVariants(t *testing.T) {
	assert.True(t, isStaticAsset("/assets/main.js"))
	assert.True(t, isStaticAsset("/assets/style.css"))
	assert.True(t, isStaticAsset("/favicon.ico"))
	assert.False(t, isStaticAsset("/api/v1/workspaces"))
	assert.False(t, isStaticAsset("/"))
}

func TestBuildAPIResponseTextWithChoices(t *testing.T) {
	req := respondRequestWith(func(request *apiRespondRequest) {
		request.SelectedChoices = []string{"Option A", "Option B"}
		request.Answer = "additional feedback"
	})
	result := buildAPIResponseText(req)
	assert.Contains(t, result, "Option A")
	assert.Contains(t, result, "Option B")
	assert.Contains(t, result, "additional feedback")
}

func TestBuildAPIResponseTextOnlyAnswer(t *testing.T) {
	req := respondRequestWith(func(request *apiRespondRequest) { request.Answer = "my answer" })
	result := buildAPIResponseText(req)
	assert.Equal(t, "my answer", result)
}

func TestBuildAPIResponseTextEmpty(t *testing.T) {
	req := newTestAPIRespondRequest()
	result := buildAPIResponseText(req)
	assert.Empty(t, result)
}

func TestCollectForksForAPIFromGroupsEmpty(t *testing.T) {
	srv, _ := setupTestServer(t)
	result := srv.collectForksForAPIFromGroups("/nonexistent", nil)
	assert.Nil(t, result)
}

func TestCollectForksForAPIFromGroupsNoMatch(t *testing.T) {
	srv, _ := setupTestServer(t)
	groups := []workspaceGroup{
		workspaceGroupWith(func(group *workspaceGroup) {
			group.Root = workspaceWith(func(workspace *workspaceInfo) { workspace.Directory = "/some/other/dir" })
		}),
	}
	result := srv.collectForksForAPIFromGroups("/nonexistent", groups)
	assert.Nil(t, result)
}

func TestConvertMessagesForAPITruncation(t *testing.T) {
	longBody := strings.Repeat("x", maxMessageBodyBytes+100)
	messages := []state.Message{
		messageWith(func(message *state.Message) {
			message.ID = 1
			message.FromAgent = "a"
			message.ToAgent = "b"
			message.Body = longBody
		}),
	}
	result := convertMessagesForAPI(messages)
	require.Len(t, result, 1)
	assert.True(t, strings.HasSuffix(result[0].Body, "...[truncated]"))
	assert.Less(t, len(result[0].Body), len(longBody))
}

func TestConvertMessagesForAPIReversesOrder(t *testing.T) {
	messages := []state.Message{
		messageWith(func(message *state.Message) {
			message.ID = 1
			message.FromAgent = "a"
			message.ToAgent = "b"
			message.Body = "first"
		}),
		messageWith(func(message *state.Message) {
			message.ID = 2
			message.FromAgent = "b"
			message.ToAgent = "a"
			message.Body = "second"
		}),
		messageWith(func(message *state.Message) {
			message.ID = 3
			message.FromAgent = "a"
			message.ToAgent = "b"
			message.Body = "third"
		}),
	}
	result := convertMessagesForAPI(messages)
	require.Len(t, result, 3)
	assert.Equal(t, 3, result[0].ID)
	assert.Equal(t, 2, result[1].ID)
	assert.Equal(t, 1, result[2].ID)
}

func TestConvertMessagesForAPIExtractsSubject(t *testing.T) {
	messages := []state.Message{
		messageWith(func(message *state.Message) {
			message.ID = 1
			message.FromAgent = "a"
			message.ToAgent = "b"
			message.Body = "# Important Update\nSome content here"
		}),
	}
	result := convertMessagesForAPI(messages)
	require.Len(t, result, 1)
	assert.Equal(t, "Important Update", result[0].Subject)
}

func TestConvertEventsForAPIWithEntries(t *testing.T) {
	displays := []eventsProgressDisplay{
		eventsDisplayWith(func(display *eventsProgressDisplay) {
			display.Timestamp = "2024-01-01T00:00:00Z"
			display.FormattedTime = "00:00"
			display.Agent = "coordinator"
			display.Description = "started"
		}),
	}
	result := convertEventsForAPI(displays)
	require.Len(t, result, 1)
	assert.Equal(t, "coordinator", result[0].Agent)
	assert.Equal(t, "started", result[0].Description)
}

func TestConvertModelStatusesWithEntries(t *testing.T) {
	displays := []modelStatusDisplay{
		{ModelID: "opus", Status: "running"},
		{ModelID: "sonnet", Status: "completed"},
	}
	result := convertModelStatuses(displays)
	require.Len(t, result, 2)
	assert.Equal(t, "opus", result[0].ModelID)
	assert.Equal(t, "running", result[0].Status)
}

func TestCollectSkillCategoriesEmpty(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai", "skills"), 0o755))
	result := collectSkillCategories(dir)
	assert.Empty(t, result)
}

func TestCollectSkillCategoriesWithSkills(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, ".sgai", "skills", "coding-practices", "go-testing")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: go-testing\ndescription: Go testing patterns\n---\n# Go Testing"), 0o644))

	result := collectSkillCategories(dir)
	assert.NotEmpty(t, result)
}

func TestCollectAgentsEmpty(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai", "agent"), 0o755))
	result := collectAgents(dir)
	assert.Empty(t, result)
}

func TestCollectAgentsWithAgents(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, ".sgai", "agent")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "coordinator.md"),
		[]byte("---\ndescription: Main coordinator\n---\n# Coordinator"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "builder.md"),
		[]byte("---\ndescription: Builder agent\n---\n# Builder"), 0o644))

	result := collectAgents(dir)
	assert.Len(t, result, 2)
}

func TestLoadActionsForAPIUsesConfigActionMetadata(t *testing.T) {
	dir := t.TempDir()
	writeActionTestConfig(t, dir, projectConfigWith(func(config *projectConfig) {
		config.Actions = []actionConfig{
			actionConfigWith(func(action *actionConfig) {
				action.Name = "Summarize"
				action.Model = "openai/gpt-5.4"
				action.Prompt = "hello {{ .Name }}"
				action.Description = "Summarize something"
			}),
			actionConfigWith(func(action *actionConfig) {
				action.Name = "Print"
				action.Model = "ignored-model"
				action.Script = `printf "%s" "{{ .Message }}"`
				action.Description = "Print a message"
			}),
		}
	}))

	result := loadActionsForAPI(dir)
	require.Empty(t, result.ConfigError)
	require.Len(t, result.Actions, 2)
	assert.Equal(t, apiActionEntryWith(func(action *apiActionEntry) {
		action.Name = "Summarize"
		action.Model = "openai/gpt-5.4"
		action.Prompt = "hello {{ .Name }}"
		action.Description = "Summarize something"
		action.Kind = "prompt"
		action.Variables = []string{"Name"}
	}), result.Actions[0])
	assert.Equal(t, apiActionEntryWith(func(action *apiActionEntry) {
		action.Name = "Print"
		action.Script = `printf "%s" "{{ .Message }}"`
		action.Description = "Print a message"
		action.Kind = "script"
		action.Variables = []string{"Message"}
	}), result.Actions[1])
}

func TestLoadActionsForAPISurfacesInvalidScriptValidationError(t *testing.T) {
	dir := t.TempDir()
	writeActionTestConfig(t, dir, projectConfigWith(func(config *projectConfig) {
		config.Actions = []actionConfig{
			actionConfigWith(func(action *actionConfig) {
				action.Name = "Broken"
				action.Script = `printf "%s" "{{ .Message }}" | cat`
			}),
		}
	}))

	result := loadActionsForAPI(dir)
	require.Empty(t, result.ConfigError)
	require.Len(t, result.Actions, 1)
	assert.Equal(t, apiActionEntryWith(func(action *apiActionEntry) {
		action.Name = "Broken"
		action.Script = `printf "%s" "{{ .Message }}" | cat`
		action.Kind = "script"
		action.ValidationError = `action "Broken" rendered an invalid command: unsupported shell operator "|"`
	}), result.Actions[0])
}

func TestConvertActionsForAPIEmpty(t *testing.T) {
	result := convertActionsForAPI(nil)
	assert.Empty(t, result)
}

func TestBuildWorkspaceListResponseWithMultipleWorkspaces(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "ws1")
	setupTestWorkspace(t, server, rootDir, "ws2")
	result := server.buildWorkspaceListResponse()
	assert.GreaterOrEqual(t, len(result.Workspaces), 2)
}

func TestReadNewestForkGoalEmptyList(t *testing.T) {
	result := readNewestForkGoal(nil)
	assert.Empty(t, result)
}

func TestReadNewestForkGoalWithGoalFiles(t *testing.T) {
	dir := t.TempDir()
	fork1 := filepath.Join(dir, "fork1")
	fork2 := filepath.Join(dir, "fork2")
	require.NoError(t, os.MkdirAll(fork1, 0o755))
	require.NoError(t, os.MkdirAll(fork2, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fork1, "GOAL.md"), []byte("fork1 goal"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(fork2, "GOAL.md"), []byte("fork2 goal"), 0o644))

	forks := []workspaceInfo{
		workspaceWith(func(workspace *workspaceInfo) {
			workspace.Directory = fork1
			workspace.DirName = "fork1"
		}),
		workspaceWith(func(workspace *workspaceInfo) {
			workspace.Directory = fork2
			workspace.DirName = "fork2"
		}),
	}
	result := readNewestForkGoal(forks)
	assert.NotEmpty(t, result)
}

func TestReadNewestForkGoalAllEmptyContent(t *testing.T) {
	dir := t.TempDir()
	fork1 := filepath.Join(dir, "fork1")
	require.NoError(t, os.MkdirAll(fork1, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fork1, "GOAL.md"), []byte("  \n\t  "), 0o644))

	forks := []workspaceInfo{workspaceWith(func(workspace *workspaceInfo) {
		workspace.Directory = fork1
		workspace.DirName = "fork1"
	})}
	result := readNewestForkGoal(forks)
	assert.Empty(t, result)
}

func TestResolveForkDirExplicitPath(t *testing.T) {
	server, rootDir := setupTestServer(t)
	forkDir := filepath.Join(rootDir, "fork-ws")
	require.NoError(t, os.MkdirAll(forkDir, 0o755))
	result := server.resolveForkDir(forkDir, "/some/path", "/root/path")
	assert.Equal(t, filepath.Clean(forkDir), result)
}

func TestResolveForkDirImplicitFromWorkspace(t *testing.T) {
	server, _ := setupTestServer(t)
	result := server.resolveForkDir("", "/workspace/path", "/different/root")
	assert.Equal(t, "/workspace/path", result)
}

func TestResolveForkDirEmptyWhenSamePaths(t *testing.T) {
	server, _ := setupTestServer(t)
	result := server.resolveForkDir("", "/same/path", "/same/path")
	assert.Empty(t, result)
}

func TestResolveRootForDeleteForkStandaloneReturnsEmpty(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "standalone-ws")
	result := server.resolveRootForDeleteFork(filepath.Join(rootDir, "standalone-ws"))
	assert.Empty(t, result)
}

func TestResolveRootForDeleteForkForkReturnsPath(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := filepath.Join(rootDir, "fork-ws")
	require.NoError(t, os.MkdirAll(filepath.Join(wsDir, ".sgai"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(wsDir, ".jj"), 0o755))
	rootWs := filepath.Join(rootDir, "root-ws")
	require.NoError(t, os.MkdirAll(filepath.Join(rootWs, ".jj", "repo"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, ".jj", "repo"), []byte(filepath.Join(rootWs, ".jj", "repo")), 0o644))
	result := server.resolveRootForDeleteFork(wsDir)
	assert.NotEmpty(t, result)
}

func TestQuestionTypeFreeformMessage(t *testing.T) {
	wf := workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.HumanMessage = "What do you think?"
	})
	assert.Equal(t, "free-text", questionType(&wf))
}

func TestQuestionTypeMultiChoiceQuestions(t *testing.T) {
	wf := workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.MultiChoiceQuestion = multiChoiceQuestionWith(func(question *state.MultiChoiceQuestion) {
			question.Questions = []state.QuestionItem{
				questionItemWith(func(item *state.QuestionItem) {
					item.Question = "Pick one"
					item.Choices = []string{"A", "B"}
				}),
			}
		})
	})
	assert.Equal(t, "multi-choice", questionType(&wf))
}

func TestQuestionTypeWorkGateFlag(t *testing.T) {
	wf := workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.MultiChoiceQuestion = multiChoiceQuestionWith(func(question *state.MultiChoiceQuestion) {
			question.IsWorkGate = true
			question.Questions = []state.QuestionItem{
				questionItemWith(func(item *state.QuestionItem) {
					item.Question = "Approve?"
					item.Choices = []string{"Yes", "No"}
				}),
			}
		})
	})
	assert.Equal(t, "work-gate", questionType(&wf))
}

func TestLoadWorkspaceStateInvalidJSONReturnsWorkingFallback(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws-large")
	sp := filepath.Join(wsDir, ".sgai", "state.json")
	require.NoError(t, os.WriteFile(sp, []byte(`{invalid}`), 0o644))
	result := server.loadWorkspaceState(wsDir)
	assert.Equal(t, state.StatusWorking, result.Status)
}

func TestLoadWorkspaceStateNoFileReturnsEmpty(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws-nostate")
	result := server.loadWorkspaceState(wsDir)
	assert.Empty(t, result.Status)
}

func TestLoadWorkspaceStateUsesDiskAfterStoppedSession(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws-stopped")

	stoppedCoord := stopCachedSession(t, server, wsDir, workflowRef(func(workflow *state.Workflow) { workflow.Status = state.StatusComplete }))
	writeWorkflowStateToDisk(t, wsDir, workflowRef(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.Task = "resume me"
	}))

	result := server.loadWorkspaceState(wsDir)
	assert.NotSame(t, stoppedCoord, server.workspaceCoordinator(wsDir))
	assert.Equal(t, state.StatusWorking, result.Status)
	assert.Equal(t, "resume me", result.Task)
}

func TestResolveAPIWorkspace(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "resolve-ws")

	w := serveHTTP(srv, "GET", "/api/v1/workspaces/resolve-ws/goal", "")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestResolveAPIWorkspaceFallback(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "fallback-ws")

	mux := http.NewServeMux()
	srv.registerAPIRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestLoadActionsForAPI(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func(*testing.T, string)
		validate  func(*testing.T, actionAPIState)
	}{
		{
			name: "noConfig",
			setupFunc: func(_ *testing.T, _ string) {
			},
			validate: func(t *testing.T, state actionAPIState) {
				t.Helper()
				assert.Empty(t, state.ConfigError)
				assert.Len(t, state.Actions, 3)
				assert.Equal(t, "Create PR", state.Actions[0].Name)
			},
		},
		{
			name: "withConfig",
			setupFunc: func(t *testing.T, dir string) {
				t.Helper()
				config := projectConfigWith(func(project *projectConfig) {
					project.Actions = []actionConfig{
						actionConfigWith(func(action *actionConfig) {
							action.Name = "Custom Action"
							action.Model = "model1"
							action.Prompt = "prompt1"
						}),
					}
				})
				data, err := json.Marshal(config)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(dir, configFileName), data, 0o644))
			},
			validate: func(t *testing.T, state actionAPIState) {
				t.Helper()
				assert.Empty(t, state.ConfigError)
				assert.Len(t, state.Actions, 1)
				assert.Equal(t, "Custom Action", state.Actions[0].Name)
			},
		},
		{
			name: "emptyActions",
			setupFunc: func(t *testing.T, dir string) {
				t.Helper()
				config := projectConfigWith(func(project *projectConfig) {
					project.Actions = []actionConfig{}
				})
				data, err := json.Marshal(config)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(dir, configFileName), data, 0o644))
			},
			validate: func(t *testing.T, state actionAPIState) {
				t.Helper()
				assert.Empty(t, state.ConfigError)
				assert.Len(t, state.Actions, 3)
				assert.Equal(t, "Create PR", state.Actions[0].Name)
			},
		},
		{
			name: "invalidJSON",
			setupFunc: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(dir, configFileName), []byte("not valid json"), 0o644))
			},
			validate: func(t *testing.T, state actionAPIState) {
				t.Helper()
				assert.Empty(t, state.Actions)
				assert.Contains(t, state.ConfigError, "invalid JSON syntax")
			},
		},
		{
			name: "unreadableConfigPath",
			setupFunc: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.Mkdir(filepath.Join(dir, configFileName), 0o755))
			},
			validate: func(t *testing.T, state actionAPIState) {
				t.Helper()
				assert.Empty(t, state.Actions)
				assert.Contains(t, state.ConfigError, "reading config file")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setupFunc(t, dir)
			result := loadActionsForAPI(dir)
			if tt.validate != nil {
				tt.validate(t, result)
			}
		})
	}
}

func TestBuildWorkspaceFullStateIncludesActionConfigError(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "broken-action-config-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, configFileName), []byte("not valid json"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\n---\n# Goal"), 0o644))

	ws := workspaceWith(func(workspace *workspaceInfo) {
		workspace.DirName = "broken-action-config-ws"
		workspace.Directory = wsDir
		workspace.HasWorkspace = true
	})
	result := srv.buildWorkspaceFullState(ws, nil)

	assert.Empty(t, result.Actions)
	assert.Contains(t, result.ActionConfigError, "invalid JSON syntax")
}

func TestConvertActionsForAPI(t *testing.T) {
	tests := []struct {
		name     string
		configs  []actionConfig
		expected []apiActionEntry
	}{
		{
			name:     "empty",
			configs:  []actionConfig{},
			expected: []apiActionEntry{},
		},
		{
			name: "singleAction",
			configs: []actionConfig{
				actionConfigWith(func(action *actionConfig) {
					action.Name = "Action 1"
					action.Model = "model1"
					action.Prompt = "prompt1"
					action.Description = "desc1"
				}),
			},
			expected: []apiActionEntry{
				apiActionEntryWith(func(action *apiActionEntry) {
					action.Name = "Action 1"
					action.Model = "model1"
					action.Prompt = "prompt1"
					action.Description = "desc1"
					action.Kind = "prompt"
				}),
			},
		},
		{
			name: "multipleActions",
			configs: []actionConfig{
				actionConfigWith(func(action *actionConfig) {
					action.Name = "Action 1"
					action.Model = "model1"
					action.Prompt = "prompt1"
				}),
				actionConfigWith(func(action *actionConfig) {
					action.Name = "Action 2"
					action.Model = "model2"
					action.Prompt = "prompt2"
					action.Description = "desc2"
				}),
			},
			expected: []apiActionEntry{
				apiActionEntryWith(func(action *apiActionEntry) {
					action.Name = "Action 1"
					action.Model = "model1"
					action.Prompt = "prompt1"
					action.Kind = "prompt"
				}),
				apiActionEntryWith(func(action *apiActionEntry) {
					action.Name = "Action 2"
					action.Model = "model2"
					action.Prompt = "prompt2"
					action.Description = "desc2"
					action.Kind = "prompt"
				}),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertActionsForAPI(tt.configs)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCollectAgents(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func(*testing.T, string)
		validate  func(*testing.T, []apiAgentEntry)
	}{
		{
			name: "noAgents",
			setupFunc: func(_ *testing.T, _ string) {
			},
			validate: func(t *testing.T, agents []apiAgentEntry) {
				t.Helper()
				assert.Empty(t, agents)
			},
		},
		{
			name: "singleAgent",
			setupFunc: func(t *testing.T, dir string) {
				t.Helper()
				agentDir := filepath.Join(dir, ".sgai", "agent")
				require.NoError(t, os.MkdirAll(agentDir, 0o755))
				agentContent := `---
description: Test agent description
---
# Test Agent`
				require.NoError(t, os.WriteFile(filepath.Join(agentDir, "test-agent.md"), []byte(agentContent), 0o644))
			},
			validate: func(t *testing.T, agents []apiAgentEntry) {
				t.Helper()
				assert.Len(t, agents, 1)
				assert.Equal(t, "test-agent", agents[0].Name)
				assert.Equal(t, "Test agent description", agents[0].Description)
			},
		},
		{
			name: "multipleAgents",
			setupFunc: func(t *testing.T, dir string) {
				t.Helper()
				agentDir := filepath.Join(dir, ".sgai", "agent")
				require.NoError(t, os.MkdirAll(agentDir, 0o755))
				for _, agent := range []struct {
					name string
					desc string
				}{
					{"agent-a", "Agent A description"},
					{"agent-b", "Agent B description"},
					{"agent-c", "Agent C description"},
				} {
					agentContent := `---
description: ` + agent.desc + `
---
# ` + agent.name
					require.NoError(t, os.WriteFile(filepath.Join(agentDir, agent.name+".md"), []byte(agentContent), 0o644))
				}
			},
			validate: func(t *testing.T, agents []apiAgentEntry) {
				t.Helper()
				assert.Len(t, agents, 3)
				assert.Equal(t, "agent-a", agents[0].Name)
				assert.Equal(t, "agent-b", agents[1].Name)
				assert.Equal(t, "agent-c", agents[2].Name)
			},
		},
		{
			name: "nonMarkdownFile",
			setupFunc: func(t *testing.T, dir string) {
				t.Helper()
				agentDir := filepath.Join(dir, ".sgai", "agent")
				require.NoError(t, os.MkdirAll(agentDir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(agentDir, "test.txt"), []byte("content"), 0o644))
			},
			validate: func(t *testing.T, agents []apiAgentEntry) {
				t.Helper()
				assert.Empty(t, agents)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setupFunc(t, dir)
			result := collectAgents(dir)
			if tt.validate != nil {
				tt.validate(t, result)
			}
		})
	}
}

func TestConvertAgentSequence(t *testing.T) {
	tests := []struct {
		name     string
		displays []agentSequenceDisplay
		expected []apiAgentSequenceEntry
	}{
		{
			name:     "empty",
			displays: []agentSequenceDisplay{},
			expected: []apiAgentSequenceEntry{},
		},
		{
			name: "singleEntry",
			displays: []agentSequenceDisplay{
				{Agent: "agent1", Model: "model1", ElapsedTime: "1m", IsCurrent: true},
			},
			expected: []apiAgentSequenceEntry{
				{Agent: "agent1", Model: "model1", ElapsedTime: "1m", IsCurrent: true},
			},
		},
		{
			name: "multipleEntries",
			displays: []agentSequenceDisplay{
				{Agent: "agent1", Model: "model1", ElapsedTime: "1m", IsCurrent: false},
				{Agent: "agent2", Model: "model2", ElapsedTime: "2m", IsCurrent: true},
				{Agent: "agent3", Model: "model3", ElapsedTime: "3m", IsCurrent: false},
			},
			expected: []apiAgentSequenceEntry{
				{Agent: "agent1", Model: "model1", ElapsedTime: "1m", IsCurrent: false},
				{Agent: "agent2", Model: "model2", ElapsedTime: "2m", IsCurrent: true},
				{Agent: "agent3", Model: "model3", ElapsedTime: "3m", IsCurrent: false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertAgentSequence(tt.displays)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestReadGoalAndPMForAPI(t *testing.T) {
	t.Run("noGoalOrPM", func(t *testing.T) {
		tmpDir := t.TempDir()
		goalContent, rawGoal, fullGoal, pmContent, hasPM := readGoalAndPMForAPI(tmpDir)
		assert.Empty(t, goalContent)
		assert.Empty(t, rawGoal)
		assert.Empty(t, fullGoal)
		assert.Empty(t, pmContent)
		assert.False(t, hasPM)
	})

	t.Run("withGoalOnly", func(t *testing.T) {
		tmpDir := t.TempDir()
		goalFileContent := "---\nflow: |\n  \"a\" -> \"b\"\n---\n# My Goal\n\nDo something."
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "GOAL.md"), []byte(goalFileContent), 0o644))

		gc, rawGC, fullGC, pm, hasPM := readGoalAndPMForAPI(tmpDir)
		assert.NotEmpty(t, gc)
		assert.Contains(t, rawGC, "My Goal")
		assert.Contains(t, fullGC, "flow:")
		assert.Empty(t, pm)
		assert.False(t, hasPM)
	})

	t.Run("withGoalAndPM", func(t *testing.T) {
		tmpDir := t.TempDir()
		goalFileContent := "---\nflow: |\n  \"a\" -> \"b\"\n---\n# My Goal\n\nDo something."
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "GOAL.md"), []byte(goalFileContent), 0o644))

		sgaiDir := filepath.Join(tmpDir, ".sgai")
		require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
		pmFileContent := "---\nRetrospective Session: .sgai/retro\n---\n\n## PM Content\n"
		require.NoError(t, os.WriteFile(filepath.Join(sgaiDir, "PROJECT_MANAGEMENT.md"), []byte(pmFileContent), 0o644))

		gc, rawGC, fullGC, pm, hasPM := readGoalAndPMForAPI(tmpDir)
		assert.NotEmpty(t, gc)
		assert.Contains(t, rawGC, "My Goal")
		assert.Contains(t, fullGC, "flow:")
		assert.NotEmpty(t, pm)
		assert.True(t, hasPM)
	})
}

func TestLoadWorkspaceState(t *testing.T) {
	t.Run("nonExistentState", func(t *testing.T) {
		rootDir := t.TempDir()
		server := NewServer(rootDir, newTestServerPaths(), "")
		workDir := filepath.Join(rootDir, "ws")
		require.NoError(t, os.MkdirAll(workDir, 0o755))

		wf := server.loadWorkspaceState(workDir)
		assert.Empty(t, wf.Status)
	})

	t.Run("existingState", func(t *testing.T) {
		rootDir := t.TempDir()
		server := NewServer(rootDir, newTestServerPaths(), "")
		workDir := filepath.Join(rootDir, "ws")
		sgaiDir := filepath.Join(workDir, ".sgai")
		require.NoError(t, os.MkdirAll(sgaiDir, 0o755))

		stateFile := filepath.Join(sgaiDir, "state.json")
		_, err := state.NewCoordinatorWith(stateFile, workflowWith(func(workflow *state.Workflow) {
			workflow.Status = state.StatusComplete
			workflow.CurrentAgent = "test-agent"
			workflow.Progress = []state.ProgressEntry{}
			workflow.Messages = []state.Message{}
		}))
		require.NoError(t, err)

		wf := server.loadWorkspaceState(workDir)
		assert.Equal(t, state.StatusComplete, wf.Status)
		assert.Equal(t, "test-agent", wf.CurrentAgent)
	})

	t.Run("largeValidState", func(t *testing.T) {
		rootDir := t.TempDir()
		server := NewServer(rootDir, newTestServerPaths(), "")
		workDir := filepath.Join(rootDir, "ws")
		sgaiDir := filepath.Join(workDir, ".sgai")
		require.NoError(t, os.MkdirAll(sgaiDir, 0o755))

		stateFile := filepath.Join(sgaiDir, "state.json")
		_, err := state.NewCoordinatorWith(stateFile, workflowWith(func(workflow *state.Workflow) {
			workflow.Status = state.StatusWorking
			workflow.CurrentAgent = "builder"
			workflow.Messages = []state.Message{{
				ID:        1,
				FromAgent: "coordinator",
				ToAgent:   "builder",
				Body:      strings.Repeat("x", 11*1024*1024),
				Read:      false,
				ReadAt:    "",
				ReadBy:    "",
				CreatedAt: "",
			}}
		}))
		require.NoError(t, err)

		wf := server.loadWorkspaceState(workDir)
		assert.Equal(t, state.StatusWorking, wf.Status)
		assert.Equal(t, "builder", wf.CurrentAgent)
		require.Len(t, wf.Messages, 1)
	})
}

func setupTestServer(t *testing.T) (server *Server, rootDir string) {
	t.Helper()
	rootDir = t.TempDir()
	server = NewServer(rootDir, resolveServerPaths(t.TempDir()), "")
	return server, rootDir
}

func TestSetupTestServerUsesIsolatedConfigDirs(t *testing.T) {
	server, _ := setupTestServer(t)
	productionPaths := resolveServerPaths(xdg.ConfigHome)
	assert.NotEqual(t, productionPaths.pinnedConfigDir, server.pinnedConfigDir)
	assert.NotEqual(t, productionPaths.externalConfigDir, server.externalConfigDir)
}

func setupTestWorkspace(t *testing.T, server *Server, rootDir, name string) string {
	t.Helper()
	wsDir := filepath.Join(rootDir, name)
	require.NoError(t, os.MkdirAll(filepath.Join(wsDir, ".sgai"), 0o755))
	canonicalDir := resolveSymlinks(wsDir)
	if server != nil {
		server.mu.Lock()
		server.externalDirs[canonicalDir] = true
		server.mu.Unlock()
		server.invalidateWorkspaceScanCache()
	}
	return canonicalDir
}

func workflowStateFromDisk(t *testing.T, wsDir string) state.Workflow {
	t.Helper()

	coord, errCoord := state.NewCoordinator(filepath.Join(wsDir, ".sgai", "state.json"))
	require.NoError(t, errCoord)

	return coord.State()
}

func serveHTTP(server *Server, method, path, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	server.registerAPIRoutes(mux)

	var reqBody *strings.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	}
	var req *http.Request
	if reqBody != nil {
		req = httptest.NewRequest(method, path, reqBody)
	} else {
		req = httptest.NewRequest(method, path, http.NoBody)
	}
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func TestHandleAPIWorkspaceListRoute(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	goalContent := "---\nflow: |\n  \"a\" -> \"b\"\n---\n# Test"
	require.NoError(t, os.WriteFile(filepath.Join(rootDir, "test-ws", "GOAL.md"), []byte(goalContent), 0o644))

	w := serveHTTP(server, "GET", "/api/v1/workspaces", "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
}

func TestHandleAPIWorkspaceList(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\nflow: |\n  \"a\" -> \"b\"\n---\n# Test"), 0o644))
	_, errCoord := state.NewCoordinatorWith(filepath.Join(wsDir, ".sgai", "state.json"), workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.CurrentAgent = "coordinator"
		workflow.Progress = []state.ProgressEntry{{Timestamp: time.Now().UTC().Format(time.RFC3339), Agent: "coordinator", Description: "step 1"}}
		workflow.Messages = []state.Message{{ID: 1, FromAgent: "a", ToAgent: "b", Body: "heavy message payload", Read: false, ReadAt: "", ReadBy: "", CreatedAt: ""}}
	}))
	require.NoError(t, errCoord)

	w := serveHTTP(server, http.MethodGet, "/api/v1/workspaces", "")
	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Workspaces []map[string]json.RawMessage `json:"workspaces"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Workspaces, 1)
	workspace := resp.Workspaces[0]

	var name string
	require.NoError(t, json.Unmarshal(workspace["name"], &name))
	assert.Equal(t, "test-ws", name)
	assert.NotContains(t, workspace, "messages")
	assert.NotContains(t, workspace, "events")
	assert.NotContains(t, workspace, "log")
	assert.NotContains(t, workspace, "goalContent")
	assert.NotContains(t, workspace, "pmContent")
}

func TestHandleAPIWorkspaceState(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\nflow: |\n  \"a\" -> \"b\"\n---\n# Test"), 0o644))
	_, errCoord := state.NewCoordinatorWith(filepath.Join(wsDir, ".sgai", "state.json"), workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.CurrentAgent = "coordinator"
		workflow.Progress = []state.ProgressEntry{{Timestamp: time.Now().UTC().Format(time.RFC3339), Agent: "coordinator", Description: "step 1"}}
		workflow.Messages = []state.Message{{ID: 1, FromAgent: "a", ToAgent: "b", Body: "hello", Read: false, ReadAt: "", ReadBy: "", CreatedAt: ""}}
	}))
	require.NoError(t, errCoord)

	sess := newTestServeSession(nil, true)
	sess.outputLog = newCircularLogBuffer()
	sess.outputLog.add(logLine{prefix: "", text: "log line"})
	server.mu.Lock()
	server.sessions[wsDir] = sess
	server.mu.Unlock()

	w := serveHTTP(server, http.MethodGet, "/api/v1/workspaces/test-ws/state", "")
	assert.Equal(t, http.StatusOK, w.Code)

	var resp apiWorkspaceFullState
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "test-ws", resp.Name)
	require.Len(t, resp.Messages, 1)
	require.Len(t, resp.Events, 1)
	require.Len(t, resp.Log, 1)
}

func TestHandleAPIWorkspaceStateRejectsAmbiguousBasenameEvenWithWorkspaceHeader(t *testing.T) {
	server, _ := setupTestServer(t)
	firstDir := filepath.Join(t.TempDir(), "first", "shared-ws")
	secondDir := filepath.Join(t.TempDir(), "second", "shared-ws")
	require.NoError(t, os.MkdirAll(filepath.Join(firstDir, ".sgai"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(secondDir, ".sgai"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(firstDir, "GOAL.md"), []byte("# First Goal"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(secondDir, "GOAL.md"), []byte("# Second Goal"), 0o644))

	server.mu.Lock()
	server.externalDirs[resolveSymlinks(firstDir)] = true
	server.externalDirs[resolveSymlinks(secondDir)] = true
	server.mu.Unlock()
	server.invalidateWorkspaceScanCache()

	mux := http.NewServeMux()
	server.registerAPIRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/shared-ws/state", http.NoBody)
	req.Header.Set("X-Sgai-Workspace-Dir", workspaceSignalPath(secondDir))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "workspace name is ambiguous")
}

func TestLoadWorkspacePageStateMissingWorkspace(t *testing.T) {
	server, _ := setupTestServer(t)

	_, err := server.loadWorkspacePageState("/missing/workspace")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "workspace not found")
}

func TestHandleAPIAgents(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	goalContent := "---\nflow: |\n  \"agent1\" -> \"agent2\"\n---\n# Test"
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte(goalContent), 0o644))

	w := serveHTTP(server, "GET", "/api/v1/agents?workspace=test-ws", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func setupAmbiguousWorkspaceQueryServer(t *testing.T, firstGoal, secondGoal string) *Server {
	t.Helper()

	server, _ := setupTestServer(t)
	firstDir := filepath.Join(t.TempDir(), "first", "shared-ws")
	secondDir := filepath.Join(t.TempDir(), "second", "shared-ws")
	require.NoError(t, os.MkdirAll(filepath.Join(firstDir, ".sgai"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(secondDir, ".sgai"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(firstDir, "GOAL.md"), []byte(firstGoal), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(secondDir, "GOAL.md"), []byte(secondGoal), 0o644))

	server.mu.Lock()
	server.externalDirs[resolveSymlinks(firstDir)] = true
	server.externalDirs[resolveSymlinks(secondDir)] = true
	server.mu.Unlock()
	server.invalidateWorkspaceScanCache()

	return server
}

func TestHandleAPIAgentsRejectsAmbiguousWorkspaceQuery(t *testing.T) {
	server := setupAmbiguousWorkspaceQueryServer(t, "# First Goal", "# Second Goal")

	w := serveHTTP(server, "GET", "/api/v1/agents?workspace=shared-ws", "")
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "workspace name is ambiguous")
}

func TestHandleAPISkills(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")

	skillDir := filepath.Join(wsDir, ".sgai", "skills", "test-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: test-skill\ndescription: Test\n---\n# Skill"), 0o644))

	w := serveHTTP(server, "GET", "/api/v1/skills?workspace=test-ws", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPISnippets(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")

	snippetDir := filepath.Join(wsDir, ".sgai", "snippets", "go")
	require.NoError(t, os.MkdirAll(snippetDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(snippetDir, "hello.go"), []byte("---\ndescription: Hello\n---\npackage main"), 0o644))

	w := serveHTTP(server, "GET", "/api/v1/snippets?workspace=test-ws", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPIGetGoal(t *testing.T) {
	setupDuplicateGoals := func(t *testing.T) *Server {
		t.Helper()

		server, _ := setupTestServer(t)
		firstDir := filepath.Join(t.TempDir(), "first", "shared-ws")
		secondDir := filepath.Join(t.TempDir(), "second", "shared-ws")
		require.NoError(t, os.MkdirAll(filepath.Join(firstDir, ".sgai"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(secondDir, ".sgai"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(firstDir, "GOAL.md"), []byte("# First Goal"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(secondDir, "GOAL.md"), []byte("# Second Goal"), 0o644))

		server.mu.Lock()
		server.externalDirs[resolveSymlinks(firstDir)] = true
		server.externalDirs[resolveSymlinks(secondDir)] = true
		server.mu.Unlock()
		server.invalidateWorkspaceScanCache()

		return server
	}

	t.Run("withGoal", func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
		require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Test Goal"), 0o644))

		w := serveHTTP(server, "GET", "/api/v1/workspaces/test-ws/goal", "")
		assert.Equal(t, http.StatusOK, w.Code)

		var resp apiGoalResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.Content, "Test Goal")
	})

	t.Run("missingWorkspace", func(t *testing.T) {
		server, _ := setupTestServer(t)
		w := serveHTTP(server, "GET", "/api/v1/workspaces/nonexistent/goal", "")
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("duplicateWorkspaceRejectsRoutedName", func(t *testing.T) {
		server := setupDuplicateGoals(t)

		w := serveHTTP(server, "GET", "/api/v1/workspaces/second%2Fshared-ws/goal", "")
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("duplicateWorkspaceRejectsAmbiguousBasename", func(t *testing.T) {
		server := setupDuplicateGoals(t)

		w := serveHTTP(server, "GET", "/api/v1/workspaces/shared-ws/goal", "")
		assert.Equal(t, http.StatusConflict, w.Code)
	})
}

func TestHandleAPIUpdateGoal(t *testing.T) {
	setupDuplicateGoals := func(t *testing.T) (*Server, string, string) {
		t.Helper()

		server, _ := setupTestServer(t)
		firstDir := filepath.Join(t.TempDir(), "first", "shared-ws")
		secondDir := filepath.Join(t.TempDir(), "second", "shared-ws")
		require.NoError(t, os.MkdirAll(filepath.Join(firstDir, ".sgai"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(secondDir, ".sgai"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(firstDir, "GOAL.md"), []byte("# First Goal"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(secondDir, "GOAL.md"), []byte("# Second Goal"), 0o644))

		server.mu.Lock()
		server.externalDirs[resolveSymlinks(firstDir)] = true
		server.externalDirs[resolveSymlinks(secondDir)] = true
		server.mu.Unlock()
		server.invalidateWorkspaceScanCache()

		return server, firstDir, secondDir
	}

	t.Run("updateGoal", func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
		require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Old Goal"), 0o644))

		w := serveHTTP(server, "PUT", "/api/v1/workspaces/test-ws/goal", `{"content":"# New Goal"}`)
		assert.Equal(t, http.StatusOK, w.Code)

		updatedContent, err := os.ReadFile(filepath.Join(wsDir, "GOAL.md"))
		require.NoError(t, err)
		assert.Contains(t, string(updatedContent), "New Goal")
	})

	t.Run("invalidBody", func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		setupTestWorkspace(t, server, rootDir, "test-ws")

		w := serveHTTP(server, "PUT", "/api/v1/workspaces/test-ws/goal", `{invalid}`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("duplicateWorkspaceRejectsRoutedName", func(t *testing.T) {
		server, firstDir, secondDir := setupDuplicateGoals(t)

		w := serveHTTP(server, "PUT", "/api/v1/workspaces/second%2Fshared-ws/goal", `{"content":"# Updated Second Goal"}`)
		assert.Equal(t, http.StatusNotFound, w.Code)

		firstContent, errFirstRead := os.ReadFile(filepath.Join(firstDir, "GOAL.md"))
		require.NoError(t, errFirstRead)
		assert.Contains(t, string(firstContent), "First Goal")

		secondContent, errSecondRead := os.ReadFile(filepath.Join(secondDir, "GOAL.md"))
		require.NoError(t, errSecondRead)
		assert.Contains(t, string(secondContent), "Second Goal")
	})

	t.Run("duplicateWorkspaceRejectsAmbiguousBasename", func(t *testing.T) {
		server, firstDir, secondDir := setupDuplicateGoals(t)

		w := serveHTTP(server, "PUT", "/api/v1/workspaces/shared-ws/goal", `{"content":"# Wrong Goal"}`)
		assert.Equal(t, http.StatusConflict, w.Code)

		firstContent, errFirstRead := os.ReadFile(filepath.Join(firstDir, "GOAL.md"))
		require.NoError(t, errFirstRead)
		assert.Contains(t, string(firstContent), "First Goal")

		secondContent, errSecondRead := os.ReadFile(filepath.Join(secondDir, "GOAL.md"))
		require.NoError(t, errSecondRead)
		assert.Contains(t, string(secondContent), "Second Goal")
	})
}

func TestResolveWorkspaceFromPathRejectsAmbiguousBasename(t *testing.T) {
	server, _ := setupTestServer(t)
	firstDir := filepath.Join(t.TempDir(), "first", "shared-ws")
	secondDir := filepath.Join(t.TempDir(), "second", "shared-ws")
	require.NoError(t, os.MkdirAll(filepath.Join(firstDir, ".sgai"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(secondDir, ".sgai"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(firstDir, "GOAL.md"), []byte("# First Goal"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(secondDir, "GOAL.md"), []byte("# Second Goal"), 0o644))

	server.mu.Lock()
	server.externalDirs[resolveSymlinks(firstDir)] = true
	server.externalDirs[resolveSymlinks(secondDir)] = true
	server.mu.Unlock()
	server.invalidateWorkspaceScanCache()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/shared-ws/respond", http.NoBody)
	req.SetPathValue("name", "shared-ws")
	resp := httptest.NewRecorder()

	workspacePath, ok := server.resolveWorkspaceFromPath(resp, req)
	assert.False(t, ok)
	assert.Empty(t, workspacePath)
	assert.Equal(t, http.StatusConflict, resp.Code)
	assert.Contains(t, resp.Body.String(), "workspace name is ambiguous")
}

func TestResolveWorkspaceForActionRejectsRoutedName(t *testing.T) {
	server, _ := setupTestServer(t)
	firstDir := filepath.Join(t.TempDir(), "first", "shared-ws")
	secondDir := filepath.Join(t.TempDir(), "second", "shared-ws")
	require.NoError(t, os.MkdirAll(filepath.Join(firstDir, ".sgai"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(secondDir, ".sgai"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(firstDir, "GOAL.md"), []byte("# First Goal"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(secondDir, "GOAL.md"), []byte("# Second Goal"), 0o644))

	server.mu.Lock()
	server.externalDirs[resolveSymlinks(firstDir)] = true
	server.externalDirs[resolveSymlinks(secondDir)] = true
	server.mu.Unlock()
	server.invalidateWorkspaceScanCache()

	workspacePath, status, message := server.resolveWorkspaceForAction("second/shared-ws")
	assert.Empty(t, workspacePath)
	assert.Equal(t, http.StatusNotFound, status)
	assert.Contains(t, message, "workspace not found")
}

func TestHandleAPITogglePin(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")

	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/pin", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPIStopSession(t *testing.T) {
	t.Run("noActiveSession", func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		setupTestWorkspace(t, server, rootDir, "test-ws")

		w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/stop", "")
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestHandleAPIDeleteWorkspace(t *testing.T) {
	t.Run("missingConfirmation", func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		setupTestWorkspace(t, server, rootDir, "test-ws")

		w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/delete", `{"confirm":false}`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalidBody", func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		setupTestWorkspace(t, server, rootDir, "test-ws")

		w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/delete", `{invalid}`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("nonExistentWorkspace", func(t *testing.T) {
		server, _ := setupTestServer(t)
		w := serveHTTP(server, "POST", "/api/v1/workspaces/nonexistent/delete", `{"confirm":true}`)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("deleteStandaloneWorkspace", func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		setupTestWorkspace(t, server, rootDir, "standalone-ws")

		w := serveHTTP(server, "POST", "/api/v1/workspaces/standalone-ws/delete", `{"confirm":true}`)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestHandleAPIStartSession(t *testing.T) {
	t.Run("invalidBody", func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		setupTestWorkspace(t, server, rootDir, "test-ws")

		w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/start", `{invalid}`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missingWorkspace", func(t *testing.T) {
		server, _ := setupTestServer(t)
		w := serveHTTP(server, "POST", "/api/v1/workspaces/nonexistent/start", `{"model":"opencode/model1","auto":false}`)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestHandleAPIForkWorkspace(t *testing.T) {
	t.Run("invalidBody", func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		setupTestWorkspace(t, server, rootDir, "test-ws")

		w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/fork", `{invalid}`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missingWorkspace", func(t *testing.T) {
		server, _ := setupTestServer(t)
		w := serveHTTP(server, "POST", "/api/v1/workspaces/nonexistent/fork", `{"goalContent":"# Goal"}`)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestHandleAPIDeleteFork(t *testing.T) {
	t.Run("missingWorkspace", func(t *testing.T) {
		server, _ := setupTestServer(t)
		w := serveHTTP(server, "POST", "/api/v1/workspaces/nonexistent/delete-fork", `{}`)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("invalidBody", func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		setupTestWorkspace(t, server, rootDir, "test-ws")
		w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/delete-fork", `{invalid}`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestHandleAPIRespond(t *testing.T) {
	t.Run("invalidBody", func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		setupTestWorkspace(t, server, rootDir, "test-ws")
		w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/respond", `{invalid}`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missingWorkspace", func(t *testing.T) {
		server, _ := setupTestServer(t)
		w := serveHTTP(server, "POST", "/api/v1/workspaces/nonexistent/respond", `{"response":"test"}`)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestHandleAPISteer(t *testing.T) {
	t.Run("missingWorkspace", func(t *testing.T) {
		server, _ := setupTestServer(t)
		w := serveHTTP(server, "POST", "/api/v1/workspaces/nonexistent/steer", `{"message":"do this"}`)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestHandleAPIWorkflowSVG(t *testing.T) {
	t.Run("withGoal", func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
		goalContent := "---\nflow: |\n  \"a\" -> \"b\"\n---\n# Test"
		require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte(goalContent), 0o644))

		w := serveHTTP(server, "GET", "/api/v1/workspaces/test-ws/workflow.svg", "")
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestHandleAPIForkTemplate(t *testing.T) {
	t.Run("missingWorkspace", func(t *testing.T) {
		server, _ := setupTestServer(t)
		w := serveHTTP(server, "GET", "/api/v1/workspaces/nonexistent/fork-template", "")
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("standaloneWorkspace", func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		setupTestWorkspace(t, server, rootDir, "test-ws")
		w := serveHTTP(server, "GET", "/api/v1/workspaces/test-ws/fork-template", "")
		assert.Equal(t, http.StatusOK, w.Code)
		var resp apiForkTemplateResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, goalExampleContent, resp.Content)
	})
}

func TestHandleAPIBrowseDirectories(t *testing.T) {
	server, _ := setupTestServer(t)

	w := serveHTTP(server, "GET", "/api/v1/browse-directories?path=/tmp", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPIDeleteMessage(t *testing.T) {
	t.Run("missingWorkspace", func(t *testing.T) {
		server, _ := setupTestServer(t)
		w := serveHTTP(server, "DELETE", "/api/v1/workspaces/nonexistent/messages/1", "")
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("invalidMessageID", func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
		stateFile := filepath.Join(wsDir, ".sgai", "state.json")
		_, err := state.NewCoordinatorWith(stateFile, workflowWith(func(workflow *state.Workflow) {
			workflow.Status = state.StatusComplete
			workflow.Messages = []state.Message{}
		}))
		require.NoError(t, err)

		w := serveHTTP(server, "DELETE", "/api/v1/workspaces/test-ws/messages/abc", "")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestHandleAPIListModels(t *testing.T) {
	server, _ := setupTestServer(t)
	w := serveHTTP(server, "GET", "/api/v1/models", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPIListModelsRejectsAmbiguousWorkspaceQuery(t *testing.T) {
	server := setupAmbiguousWorkspaceQueryServer(
		t,
		"---\nmodels:\n  coordinator: claude-opus-4\n---\n# First Goal",
		"---\nmodels:\n  coordinator: claude-sonnet-4\n---\n# Second Goal",
	)
	t.Setenv("PATH", t.TempDir())

	w := serveHTTP(server, "GET", "/api/v1/models?workspace=shared-ws", "")
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "workspace name is ambiguous")
}

func TestHandleAPIAdhocStatus(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")

	w := serveHTTP(server, "GET", "/api/v1/workspaces/test-ws/adhoc", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPIAdhoc(t *testing.T) {
	t.Run("invalidBody", func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		setupTestWorkspace(t, server, rootDir, "test-ws")
		w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/adhoc", `{invalid}`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestHandleAPISkillDetail(t *testing.T) {
	t.Run("missingWorkspace", func(t *testing.T) {
		server, _ := setupTestServer(t)
		w := serveHTTP(server, "GET", "/api/v1/skills/test-skill?workspace=nonexistent", "")
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("existingSkill", func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
		skillDir := filepath.Join(wsDir, ".sgai", "skills", "test-skill")
		require.NoError(t, os.MkdirAll(skillDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\ndescription: A test skill\n---\n# Test Skill\nContent here"), 0o644))

		w := serveHTTP(server, "GET", "/api/v1/skills/test-skill?workspace=test-ws", "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "test-skill")
	})

	t.Run("nonexistentSkill", func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		setupTestWorkspace(t, server, rootDir, "test-ws")

		w := serveHTTP(server, "GET", "/api/v1/skills/nonexistent?workspace=test-ws", "")
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestHandleAPISnippetsByLanguage(t *testing.T) {
	t.Run("missingWorkspace", func(t *testing.T) {
		server, _ := setupTestServer(t)
		w := serveHTTP(server, "GET", "/api/v1/snippets/go?workspace=nonexistent", "")
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("nonexistentLanguage", func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		setupTestWorkspace(t, server, rootDir, "test-ws")

		w := serveHTTP(server, "GET", "/api/v1/snippets/rust?workspace=test-ws", "")
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("existingLanguage", func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
		snippetDir := filepath.Join(wsDir, ".sgai", "snippets", "go")
		require.NoError(t, os.MkdirAll(snippetDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(snippetDir, "http-server.go"), []byte("---\ndescription: HTTP server\n---\npackage main"), 0o644))

		w := serveHTTP(server, "GET", "/api/v1/snippets/go?workspace=test-ws", "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "go")
	})
}

func TestHandleAPISnippetDetail(t *testing.T) {
	t.Run("missingWorkspace", func(t *testing.T) {
		server, _ := setupTestServer(t)
		w := serveHTTP(server, "GET", "/api/v1/snippets/go/test?workspace=nonexistent", "")
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("nonexistentSnippet", func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
		require.NoError(t, os.MkdirAll(filepath.Join(wsDir, ".sgai", "snippets", "go"), 0o755))

		w := serveHTTP(server, "GET", "/api/v1/snippets/go/nonexistent?workspace=test-ws", "")
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("existingSnippet", func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
		snippetDir := filepath.Join(wsDir, ".sgai", "snippets", "go")
		require.NoError(t, os.MkdirAll(snippetDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(snippetDir, "http-server.go"), []byte("---\nname: HTTP Server\ndescription: HTTP server setup\nwhen_to_use: when building HTTP servers\n---\npackage main\n\nimport \"net/http\"\n"), 0o644))

		w := serveHTTP(server, "GET", "/api/v1/snippets/go/http-server?workspace=test-ws", "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "HTTP Server")
	})
}

func TestHandleAPIOpenEditor(t *testing.T) {
	t.Run("missingWorkspace", func(t *testing.T) {
		server, _ := setupTestServer(t)
		w := serveHTTP(server, "POST", "/api/v1/workspaces/nonexistent/open-editor", "")
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestHandleAPIAdhocStop(t *testing.T) {
	t.Run("missingWorkspace", func(t *testing.T) {
		server, _ := setupTestServer(t)
		w := serveHTTP(server, "DELETE", "/api/v1/workspaces/nonexistent/adhoc", "")
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("noRunningAdhoc", func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		setupTestWorkspace(t, server, rootDir, "test-ws")

		w := serveHTTP(server, "DELETE", "/api/v1/workspaces/test-ws/adhoc", "")
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestHandleAPIAttachWorkspace(t *testing.T) {
	t.Run("invalidBody", func(t *testing.T) {
		server, _ := setupTestServer(t)
		w := serveHTTP(server, "POST", "/api/v1/workspaces/attach", `{invalid}`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("relativePath", func(t *testing.T) {
		server, _ := setupTestServer(t)
		w := serveHTTP(server, "POST", "/api/v1/workspaces/attach", `{"path":"relative/path"}`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestHandleAPIDetachWorkspace(t *testing.T) {
	t.Run("invalidBody", func(t *testing.T) {
		server, _ := setupTestServer(t)
		w := serveHTTP(server, "POST", "/api/v1/workspaces/detach", `{invalid}`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("notAttached", func(t *testing.T) {
		server, _ := setupTestServer(t)
		w := serveHTTP(server, "POST", "/api/v1/workspaces/detach", `{"path":"/some/random/path"}`)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestHandleAPIRespondInvalidBody(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")

	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/respond", `{invalid}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIStartSessionMissingWorkspace(t *testing.T) {
	server, _ := setupTestServer(t)
	w := serveHTTP(server, "POST", "/api/v1/workspaces/nonexistent/start", `{"model":"test"}`)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAPIStopSessionMissingWorkspace(t *testing.T) {
	server, _ := setupTestServer(t)
	w := serveHTTP(server, "POST", "/api/v1/workspaces/nonexistent/stop", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAPIForkWorkspaceMissingWorkspace(t *testing.T) {
	server, _ := setupTestServer(t)
	w := serveHTTP(server, "POST", "/api/v1/workspaces/nonexistent/fork", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAPIDeleteForkMissingWorkspace(t *testing.T) {
	server, _ := setupTestServer(t)
	w := serveHTTP(server, "POST", "/api/v1/workspaces/nonexistent/delete-fork", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAPIDeleteWorkspaceMissingWorkspace(t *testing.T) {
	server, _ := setupTestServer(t)
	w := serveHTTP(server, "POST", "/api/v1/workspaces/nonexistent/delete", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAPIUpdateGoalMissingWorkspace(t *testing.T) {
	server, _ := setupTestServer(t)
	w := serveHTTP(server, "PUT", "/api/v1/workspaces/nonexistent/goal", `{"content":"test"}`)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAPISteerMissingWorkspace(t *testing.T) {
	server, _ := setupTestServer(t)
	w := serveHTTP(server, "POST", "/api/v1/workspaces/nonexistent/steer", `{"message":"test"}`)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAPITogglePinMissingWorkspace(t *testing.T) {
	server, _ := setupTestServer(t)
	w := serveHTTP(server, "POST", "/api/v1/workspaces/nonexistent/pin", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAPIWorkflowSVGMissingWorkspace(t *testing.T) {
	server, _ := setupTestServer(t)
	w := serveHTTP(server, "GET", "/api/v1/workspaces/nonexistent/workflow.svg", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSPAMiddlewareAPIRoutes(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")

	w := serveHTTP(server, "GET", "/api/v1/workspaces", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPIStartSessionInvalidBody(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")

	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/start", `{invalid}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPISteerInvalidBody(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")

	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/steer", `{invalid}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIUpdateGoalInvalidBody(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")

	w := serveHTTP(server, "PUT", "/api/v1/workspaces/test-ws/goal", `{invalid}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIForkWorkspaceInvalidBody(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")

	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/fork", `{invalid}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIDeleteForkInvalidBody(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")

	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/delete-fork", `{invalid}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIDeleteWorkspaceInvalidBody(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")

	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/delete", `{invalid}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIRespondMissingWorkspace(t *testing.T) {
	server, _ := setupTestServer(t)
	w := serveHTTP(server, "POST", "/api/v1/workspaces/nonexistent/respond", `{"answer":"yes"}`)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAPIStartSessionViaHTTPMissing(t *testing.T) {
	srv, _ := setupTestServer(t)
	w := serveHTTP(srv, "POST", "/api/v1/workspaces/nonexistent/start", "{}")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAPIAgentsViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "agents-ws")

	w := serveHTTP(srv, "GET", "/api/v1/agents?workspace=agents-ws", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPISkillsViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "skills-ws")

	w := serveHTTP(srv, "GET", "/api/v1/skills?workspace=skills-ws", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPISnippetsViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "snippets-ws")

	w := serveHTTP(srv, "GET", "/api/v1/snippets?workspace=snippets-ws", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPIModelsViaHTTP(t *testing.T) {
	srv, _ := setupTestServer(t)

	w := serveHTTP(srv, "GET", "/api/v1/models", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPITogglePinViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "pin-ws")
	srv.pinnedConfigDir = t.TempDir()

	w := serveHTTP(srv, "POST", "/api/v1/workspaces/pin-ws/pin", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPIUpdateGoalViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "goal-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\n---\n# Old Goal"), 0o644))

	w := serveHTTP(srv, "PUT", "/api/v1/workspaces/goal-ws/goal", `{"content":"---\n---\n# New Goal"}`)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPIBrowseDirectoriesViaHTTP(t *testing.T) {
	srv, _ := setupTestServer(t)

	w := serveHTTP(srv, "GET", "/api/v1/browse-directories?path="+srv.rootDir, "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPIAdhocStatusViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "adhoc-ws")

	w := serveHTTP(srv, "GET", "/api/v1/workspaces/adhoc-ws/adhoc", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPIAdhocStopViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "adhoc-stop-ws")

	w := serveHTTP(srv, "DELETE", "/api/v1/workspaces/adhoc-stop-ws/adhoc", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPIForkWorkspaceViaHTTP(t *testing.T) {
	srv, _ := setupTestServer(t)
	rootDir, _ := setupNamedAttachedJJRootAndFork(t, srv, "fork-ws", "existing-fork")
	require.NoError(t, os.WriteFile(filepath.Join(rootDir, "GOAL.md"), []byte("# Root Goal"), 0o644))

	w := serveHTTP(srv, "POST", "/api/v1/workspaces/fork-ws/fork", `{"goalContent":"# Fork goal"}`)
	assert.Equal(t, http.StatusCreated, w.Code)

	var resp apiForkResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "fork-ws", resp.Parent)
	assert.NotEmpty(t, resp.Name)
	assert.Equal(t, resp.Name, filepath.Base(resp.Dir))
	_, errStat := os.Stat(resp.Dir)
	require.NoError(t, errStat)
}

func TestHandleAPIDeleteForkViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "delfork-ws")

	w := serveHTTP(srv, "POST", "/api/v1/workspaces/delfork-ws/delete-fork", `{"forkDir":"/nonexistent"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIDeleteMessageValid(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws-delmsg")
	stateFile := filepath.Join(wsDir, ".sgai", "state.json")
	_, err := state.NewCoordinatorWith(stateFile, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
		workflow.Messages = []state.Message{
			messageWith(func(message *state.Message) {
				message.ID = 1
				message.FromAgent = "dev"
				message.ToAgent = "coordinator"
				message.Body = "test"
			}),
		}
	}))
	require.NoError(t, err)

	w := serveHTTP(server, "DELETE", "/api/v1/workspaces/test-ws-delmsg/messages/1", "")
	assert.Equal(t, 200, w.Code)
}

func TestHandleAPISteerValid(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws-steer")
	stateFile := filepath.Join(wsDir, ".sgai", "state.json")
	_, err := state.NewCoordinatorWith(stateFile, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
	}))
	require.NoError(t, err)

	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws-steer/steer", `{"message":"do this"}`)
	assert.Equal(t, 200, w.Code)
}

func TestHandleAPITogglePinValid(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws-pin")

	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws-pin/pin", "")
	assert.Equal(t, 200, w.Code)
}

func TestHandleAPIUpdateGoalValid(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws-upgoal")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Old"), 0o644))

	w := serveHTTP(server, "PUT", "/api/v1/workspaces/test-ws-upgoal/goal", `{"content":"# New Goal"}`)
	assert.Equal(t, 200, w.Code)
}

func TestHandleAPISnippetsByLanguageViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "snippet-lang-ws")
	goDir := filepath.Join(wsDir, ".sgai", "snippets", "go")
	require.NoError(t, os.MkdirAll(goDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(goDir, "example.go"), []byte("// snippet\npackage main"), 0o644))

	w := serveHTTP(srv, "GET", "/api/v1/snippets/go?workspace=snippet-lang-ws", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestResolveForkTemplateContentWithForks(t *testing.T) {
	srv, _ := setupTestServer(t)
	result := srv.resolveForkTemplateContent("/nonexistent/root")
	assert.Equal(t, goalExampleContent, result)
}

func TestHandleAPIAttachWorkspaceViaHTTP(t *testing.T) {
	srv, _ := setupTestServer(t)
	validDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(validDir, ".sgai"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(validDir, "GOAL.md"), []byte("# Ext Goal"), 0o644))

	w := serveHTTP(srv, "POST", "/api/v1/workspaces/attach", `{"path":"`+validDir+`"}`)
	assert.Equal(t, http.StatusCreated, w.Code)

	var resp apiAttachWorkspaceResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, filepath.Base(validDir), resp.Name)
	assert.True(t, sameWorkspacePath(resp.Dir, validDir))
	assert.True(t, resp.HasGoal)
}

func TestUpdateGoalEmptyContentViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "upgoal-empty-v2")
	w := serveHTTP(srv, "PUT", "/api/v1/workspaces/upgoal-empty-v2/goal", `{"content":""}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateGoalInvalidJSONViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "upgoal-badjson")
	w := serveHTTP(srv, "PUT", "/api/v1/workspaces/upgoal-badjson/goal", `not json`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDeleteWorkspaceNoConfirmViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "del-noconf-v2")
	w := serveHTTP(srv, "POST", "/api/v1/workspaces/del-noconf-v2/delete", `{"confirm":false}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDeleteWorkspaceInvalidBodyViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "del-badjson")
	w := serveHTTP(srv, "POST", "/api/v1/workspaces/del-badjson/delete", `not json`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDeleteWorkspaceStandaloneViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "del-standalone")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\ntitle: Compose Full\n---\n# Goal\n"), 0o644))

	w := serveHTTP(srv, "POST", "/api/v1/workspaces/del-standalone/delete", `{"confirm":true}`)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestSteerInvalidJSONViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "steer-badjson")
	w := serveHTTP(srv, "POST", "/api/v1/workspaces/steer-badjson/steer", `not json`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSteerEmptyMessageViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "steer-emptymsg")
	w := serveHTTP(srv, "POST", "/api/v1/workspaces/steer-emptymsg/steer", `{"message":""}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetGoalNoFileViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "getgoal-nofile")
	w := serveHTTP(srv, "GET", "/api/v1/workspaces/getgoal-nofile/goal", "")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestAdhocMissingPromptViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "adhoc-noprompt-v2")
	w := serveHTTP(srv, "POST", "/api/v1/workspaces/adhoc-noprompt-v2/adhoc", `{"prompt":"","model":""}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAdhocInvalidJSONViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "adhoc-badjson-v2")
	w := serveHTTP(srv, "POST", "/api/v1/workspaces/adhoc-badjson-v2/adhoc", `not json`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestForkWorkspaceInvalidJSONViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "fork-badjson-v2")
	w := serveHTTP(srv, "POST", "/api/v1/workspaces/fork-badjson-v2/fork", `not json`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDeleteForkNoConfirmViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "delfork-noconf-v2")
	w := serveHTTP(srv, "POST", "/api/v1/workspaces/delfork-noconf-v2/delete-fork", `{"confirm":false}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDeleteForkInvalidJSONViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "delfork-badjson-v2")
	w := serveHTTP(srv, "POST", "/api/v1/workspaces/delfork-badjson-v2/delete-fork", `not json`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestStartSessionBadBodyViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "start-badjson-v2")
	w := serveHTTP(srv, "POST", "/api/v1/workspaces/start-badjson-v2/start", `not json`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRespondInvalidJSONViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "respond-badjson")
	w := serveHTTP(srv, "POST", "/api/v1/workspaces/respond-badjson/respond", `not json`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRespondNoSessionViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "respond-nosess-v2")
	statePath := filepath.Join(wsDir, ".sgai", "state.json")
	_, errCoord := state.NewCoordinatorWith(statePath, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.HumanMessage = "What should I do?"
	}))
	require.NoError(t, errCoord)

	w := serveHTTP(srv, "POST", "/api/v1/workspaces/respond-nosess-v2/respond", `{"answer":"do this","promptToken":"q-123"}`)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestAttachWorkspaceInvalidJSONViaHTTP(t *testing.T) {
	srv, _ := setupTestServer(t)
	w := serveHTTP(srv, "POST", "/api/v1/workspaces/attach", `not json`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAgentsWorkspaceNotFoundViaHTTP(t *testing.T) {
	srv, _ := setupTestServer(t)
	w := serveHTTP(srv, "GET", "/api/v1/agents?workspace=nonexistent", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSkillsWorkspaceNotFoundViaHTTP(t *testing.T) {
	srv, _ := setupTestServer(t)
	w := serveHTTP(srv, "GET", "/api/v1/skills?workspace=nonexistent", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSnippetsWorkspaceNotFoundViaHTTP(t *testing.T) {
	srv, _ := setupTestServer(t)
	w := serveHTTP(srv, "GET", "/api/v1/snippets?workspace=nonexistent", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestStopSessionNotFoundViaHTTP(t *testing.T) {
	srv, _ := setupTestServer(t)
	w := serveHTTP(srv, "POST", "/api/v1/workspaces/nonexistent-stop/stop", "{}")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestOpenEditorNotAvailableViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "editor-unavail")
	srv.editorAvailable = false

	w := serveHTTP(srv, "POST", "/api/v1/workspaces/editor-unavail/open-editor", "")
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleAPIListModelsContent(t *testing.T) {
	srv, _ := setupTestServer(t)
	w := serveHTTP(srv, "GET", "/api/v1/models", "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "models")
}

func TestHandleAPISkillDetailNotFound(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "skillnf-ws")
	w := serveHTTP(srv, "GET", "/api/v1/skills/nonexistent?workspace=skillnf-ws", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAPISnippetDetailNotFoundLang(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "snipnf-ws")
	w := serveHTTP(srv, "GET", "/api/v1/snippets/nonexistent/file.go?workspace=snipnf-ws", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAPIDiffWorkspaceNotFound(t *testing.T) {
	srv, _ := setupTestServer(t)
	w := serveHTTP(srv, "GET", "/api/v1/workspaces/nonexistent-diff/diff", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAPIForkTemplateStandaloneViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "tmpl-standalone")
	w := serveHTTP(srv, "GET", "/api/v1/workspaces/tmpl-standalone/fork-template", "")
	assert.Equal(t, http.StatusOK, w.Code)
	var resp apiForkTemplateResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, goalExampleContent, resp.Content)
}

func TestBuildWorkspaceFullStateWithMessages(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "msgs-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\n---\n# Goal"), 0o644))
	statePath := filepath.Join(wsDir, ".sgai", "state.json")
	_, errCoord := state.NewCoordinatorWith(statePath, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
		workflow.Messages = []state.Message{
			messageWith(func(message *state.Message) {
				message.ID = 1
				message.FromAgent = "coordinator"
				message.ToAgent = "builder"
				message.Body = "build"
				message.Read = true
				message.CreatedAt = "2025-01-01T00:00:00Z"
			}),
			messageWith(func(message *state.Message) {
				message.ID = 2
				message.FromAgent = "builder"
				message.ToAgent = "coordinator"
				message.Body = "done"
				message.CreatedAt = "2025-01-01T00:01:00Z"
			}),
		}
	}))
	require.NoError(t, errCoord)

	ws := workspaceWith(func(workspace *workspaceInfo) {
		workspace.DirName = "msgs-ws"
		workspace.Directory = wsDir
		workspace.HasWorkspace = true
	})
	result := srv.buildWorkspaceFullState(ws, nil)
	assert.Len(t, result.Messages, 2)
}

func TestBuildWorkspaceFullStateWithTodos(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "todos-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\n---\n# Goal"), 0o644))
	statePath := filepath.Join(wsDir, ".sgai", "state.json")
	_, errCoord := state.NewCoordinatorWith(statePath, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
		workflow.Todos = []state.TodoItem{
			todoItemWith(func(todo *state.TodoItem) {
				todo.Content = "task1"
				todo.Status = "completed"
				todo.Priority = "high"
			}),
		}
		workflow.ProjectTodos = []state.TodoItem{
			todoItemWith(func(todo *state.TodoItem) {
				todo.Content = "proj-task"
				todo.Status = "pending"
				todo.Priority = "medium"
			}),
		}
	}))
	require.NoError(t, errCoord)

	ws := workspaceWith(func(workspace *workspaceInfo) {
		workspace.DirName = "todos-ws"
		workspace.Directory = wsDir
		workspace.HasWorkspace = true
	})
	result := srv.buildWorkspaceFullState(ws, nil)
	assert.Len(t, result.AgentTodos, 1)
	assert.Len(t, result.ProjectTodos, 1)
}

func TestHandleAPIWorkspaceStateFullIntegration(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "full-int")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\nflow: |\n  digraph G {\n    \"coordinator\" -> \"builder\"\n    \"builder\" -> \"reviewer\"\n  }\nmodels:\n  coordinator: anthropic/claude-opus-4-6\n---\n# Full Integration\n\nBuild a comprehensive test suite."), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, ".sgai", "PROJECT_MANAGEMENT.md"), []byte("# PM\n\n## Progress\n- Step 1 done"), 0o644))

	agentDir := filepath.Join(wsDir, ".sgai", "agent")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "coordinator.md"),
		[]byte("---\ndescription: Main coordinator agent\n---\n# Coordinator"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "builder.md"),
		[]byte("---\ndescription: Builder agent\n---\n# Builder"), 0o644))

	skillDir := filepath.Join(wsDir, ".sgai", "skills", "test-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: test-skill\ndescription: A test\n---\n# Skill"), 0o644))

	snippetDir := filepath.Join(wsDir, ".sgai", "snippets", "go")
	require.NoError(t, os.MkdirAll(snippetDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(snippetDir, "example.go"),
		[]byte("// Example snippet\npackage main\n\nfunc main() {}"), 0o644))

	statePath := filepath.Join(wsDir, ".sgai", "state.json")
	_, errCoord := state.NewCoordinatorWith(statePath, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
		workflow.Task = "all done"
		workflow.CurrentAgent = "coordinator"
	}))
	require.NoError(t, errCoord)

	w := serveHTTP(srv, "GET", "/api/v1/workspaces/full-int/state", "")
	assert.Equal(t, http.StatusOK, w.Code)

	var ws apiWorkspaceFullState
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &ws))
	assert.Equal(t, "full-int", ws.Name)
	assert.Equal(t, string(state.StatusComplete), ws.Status)
}

func TestHandleAPIWorkspaceStateWithPendingQuestion(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "pq-int")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\n---\n# Goal"), 0o644))

	attachRunningSessionCoordinator(t, srv, wsDir, workflowRef(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.HumanMessage = "Which approach should I take?"
		workflow.CurrentAgent = "coordinator"
		workflow.MultiChoiceQuestion = multiChoiceQuestionWith(func(question *state.MultiChoiceQuestion) {
			question.Questions = []state.QuestionItem{
				questionItemWith(func(item *state.QuestionItem) {
					item.Question = "Which approach?"
					item.Choices = []string{"A", "B", "C"}
				}),
				questionItemWith(func(item *state.QuestionItem) {
					item.Question = "Priority?"
					item.Choices = []string{"High", "Low"}
					item.MultiSelect = true
				}),
			}
		})
	}))

	w := serveHTTP(srv, "GET", "/api/v1/workspaces/pq-int/state", "")
	assert.Equal(t, http.StatusOK, w.Code)

	var ws apiWorkspaceFullState
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &ws))
	assert.True(t, ws.NeedsInput)
	assert.NotNil(t, ws.PendingQuestion)
	assert.Len(t, ws.PendingQuestion.Questions, 2)
	assert.Equal(t, "coordinator", ws.PendingQuestion.AgentName)
}

func TestHandleAPIWorkspaceStatePendingQuestionUsesPromptToken(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "pq-token")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\n---\n# Goal"), 0o644))

	coord, errCh, cancel := startWaitingSessionQuestion(t, srv, wsDir, multiChoiceQuestionWith(func(question *state.MultiChoiceQuestion) {
		question.Questions = []state.QuestionItem{
			questionItemWith(func(item *state.QuestionItem) {
				item.Question = "Pick one"
				item.Choices = []string{"A", "B"}
			}),
		}
	}), "Pick one")
	defer cancel()
	promptToken := waitForSessionPromptToken(t, coord)

	w := serveHTTP(srv, "GET", "/api/v1/workspaces/pq-token/state", "")
	assert.Equal(t, http.StatusOK, w.Code)

	var workspace apiWorkspaceFullState
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &workspace))
	require.NotNil(t, workspace.PendingQuestion)
	assert.Equal(t, promptToken, workspace.PendingQuestion.PromptToken)

	var rawResp struct {
		PendingQuestion map[string]json.RawMessage `json:"pendingQuestion"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rawResp))
	_, hasLegacyField := rawResp.PendingQuestion["questionId"]
	assert.False(t, hasLegacyField)

	cancel()
	require.ErrorIs(t, <-errCh, context.Canceled)
}

func TestRespondViaCoordinatorFullPath(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "respond-full")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\n---\n# Goal"), 0o644))

	coord, errCh, cancel := startWaitingSessionQuestion(t, srv, wsDir, multiChoiceQuestionWith(func(question *state.MultiChoiceQuestion) {
		question.Questions = []state.QuestionItem{
			questionItemWith(func(item *state.QuestionItem) {
				item.Question = "Pick one"
				item.Choices = []string{"A", "B"}
			}),
		}
	}), "Pick an option")
	defer cancel()
	body := `{"answer":"go with A","selectedChoices":["A"]}`
	w := serveHTTP(srv, "POST", "/api/v1/workspaces/respond-full/respond", body)
	assert.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, <-errCh)
	assert.Empty(t, coord.State().HumanMessage)
}

func TestRespondViaCoordinatorWithoutActiveToolCall(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "respond-wrong")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\n---\n# Goal"), 0o644))

	attachSessionCoordinator(t, srv, wsDir, workflowRef(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.HumanMessage = "Pick an option"
		workflow.MultiChoiceQuestion = multiChoiceQuestionWith(func(question *state.MultiChoiceQuestion) {
			question.Questions = []state.QuestionItem{
				questionItemWith(func(item *state.QuestionItem) {
					item.Question = "Pick one"
					item.Choices = []string{"A", "B"}
				}),
			}
		})
	}))

	body := `{"answer":"go with A","selectedChoices":["A"]}`
	w := serveHTTP(srv, "POST", "/api/v1/workspaces/respond-wrong/respond", body)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestHandleAPIWorkspaceStateWithCaching(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "cache-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\n---\n# Goal"), 0o644))

	w1 := serveHTTP(srv, "GET", "/api/v1/workspaces/cache-ws/state", "")
	assert.Equal(t, http.StatusOK, w1.Code)

	w2 := serveHTTP(srv, "GET", "/api/v1/workspaces/cache-ws/state", "")
	assert.Equal(t, http.StatusOK, w2.Code)
}

func TestBuildAdhocArgsWithVariantAddsFlag(t *testing.T) {
	args := buildAdhocArgs("anthropic/claude-sonnet-4-6 (thinking)")
	assert.Contains(t, args, "--variant")
	assert.Contains(t, args, "thinking")
}

func TestBuildAdhocArgsWithoutVariantNoFlag(t *testing.T) {
	args := buildAdhocArgs("anthropic/claude-sonnet-4-6")
	for _, arg := range args {
		assert.NotEqual(t, "--variant", arg)
	}
}

func TestBuildWorkspaceFullStateContinuousMode(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "cont-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\ncontinuousModePrompt: run tests every 5m\n---\n# Goal"), 0o644))

	ws := workspaceWith(func(workspace *workspaceInfo) {
		workspace.DirName = "cont-ws"
		workspace.Directory = wsDir
		workspace.HasWorkspace = true
	})
	result := srv.buildWorkspaceFullState(ws, nil)
	assert.True(t, result.ContinuousMode)
}

func TestBuildWorkspaceFullStateEmptyGoal(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "emptygoal-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\n---\n"), 0o644))

	ws := workspaceWith(func(workspace *workspaceInfo) {
		workspace.DirName = "emptygoal-ws"
		workspace.Directory = wsDir
		workspace.HasWorkspace = true
	})
	result := srv.buildWorkspaceFullState(ws, nil)
	assert.False(t, result.HasEditedGoal)
}

func TestBuildWorkspaceFullStateExternal(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "ext-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\n---\n# Goal"), 0o644))

	ws := workspaceWith(func(workspace *workspaceInfo) {
		workspace.DirName = "ext-ws"
		workspace.Directory = wsDir
		workspace.HasWorkspace = true
		workspace.External = true
	})
	result := srv.buildWorkspaceFullState(ws, nil)
	assert.True(t, result.IsExternal)
}

func TestBuildWorkspaceFullStateCanonicalTitle(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	goalContent := "---\ntitle: Canonical Repository Title\nflow: |\n  \"a\" -> \"b\"\n---\n# Body Heading That Must Be Ignored\nSome body"
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte(goalContent), 0o644))

	ws := workspaceWith(func(workspace *workspaceInfo) {
		workspace.Directory = wsDir
		workspace.DirName = "test-ws"
	})
	result := server.buildWorkspaceFullState(ws, nil)
	assert.Equal(t, "Canonical Repository Title", result.Title)
	assert.Empty(t, result.ComputedTitle)
	assert.True(t, result.HasEditedGoal)
}

func TestBuildWorkspaceFullStateUsesComputedTitleForForkedRoot(t *testing.T) {
	server, rootDir := setupTestServer(t)
	rootWSDir := setupTestWorkspace(t, server, rootDir, "root-ws")
	forkWSDir := setupTestWorkspace(t, server, rootDir, "fork-ws")
	require.NoError(t, os.WriteFile(filepath.Join(rootWSDir, "GOAL.md"), []byte("---\ntitle: Root Goal Title\n---\n# Root Goal\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(forkWSDir, "GOAL.md"), []byte("---\ntitle: Fork Goal Title\n---\n# Fork Goal\n"), 0o644))

	groups := []workspaceGroup{workspaceGroupWith(func(group *workspaceGroup) {
		group.Root = workspaceWith(func(workspace *workspaceInfo) {
			workspace.Directory = rootWSDir
			workspace.DirName = "root-ws"
			workspace.IsRoot = true
			workspace.HasWorkspace = true
		})
		group.Forks = []workspaceInfo{workspaceWith(func(workspace *workspaceInfo) {
			workspace.Directory = forkWSDir
			workspace.DirName = "fork-ws"
			workspace.HasWorkspace = true
		})}
	})}

	result := server.buildWorkspaceFullState(groups[0].Root, groups)
	assert.Equal(t, "Root Goal Title", result.Title)
	assert.Equal(t, "root-ws", result.ComputedTitle)
}

func TestBuildWorkspaceFullStateUsesComputedTitleForForkInRootGroup(t *testing.T) {
	server, rootDir := setupTestServer(t)
	rootWSDir := setupTestWorkspace(t, server, rootDir, "root-ws")
	forkWSDir := setupTestWorkspace(t, server, rootDir, "fork-ws")
	require.NoError(t, os.WriteFile(filepath.Join(rootWSDir, "GOAL.md"), []byte("---\ntitle: Root Goal Title\n---\n# Root Goal\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(forkWSDir, "GOAL.md"), []byte("---\ntitle: Fork Goal Title\n---\n# Fork Goal\n"), 0o644))

	groups := []workspaceGroup{workspaceGroupWith(func(group *workspaceGroup) {
		group.Root = workspaceWith(func(workspace *workspaceInfo) {
			workspace.Directory = rootWSDir
			workspace.DirName = "root-ws"
			workspace.IsRoot = true
			workspace.HasWorkspace = true
		})
		group.Forks = []workspaceInfo{workspaceWith(func(workspace *workspaceInfo) {
			workspace.Directory = forkWSDir
			workspace.DirName = "fork-ws"
			workspace.HasWorkspace = true
		})}
	})}

	result := server.buildWorkspaceFullState(groups[0].Forks[0], groups)
	assert.Equal(t, "Fork Goal Title", result.Title)
	assert.Equal(t, "root-ws/Fork Goal Title", result.ComputedTitle)
}

func TestBuildWorkspaceFullStatePreservesLiteralForkGoalTitleText(t *testing.T) {
	server, rootDir := setupTestServer(t)
	rootWSDir := setupTestWorkspace(t, server, rootDir, "root-ws")
	forkWSDir := setupTestWorkspace(t, server, rootDir, "fork-ws")
	require.NoError(t, os.WriteFile(filepath.Join(rootWSDir, "GOAL.md"), []byte("---\ntitle: Root Goal Title\n---\n# Root Goal\n"), 0o644))

	groups := []workspaceGroup{workspaceGroupWith(func(group *workspaceGroup) {
		group.Root = workspaceWith(func(workspace *workspaceInfo) {
			workspace.Directory = rootWSDir
			workspace.DirName = "root-ws"
			workspace.IsRoot = true
			workspace.HasWorkspace = true
		})
		group.Forks = []workspaceInfo{workspaceWith(func(workspace *workspaceInfo) {
			workspace.Directory = forkWSDir
			workspace.DirName = "fork-ws"
			workspace.HasWorkspace = true
		})}
	})}

	tests := []struct {
		name      string
		goalTitle string
		want      string
	}{
		{name: "leadingSlash", goalTitle: "/Title", want: "root-ws//Title"},
		{name: "dotDotSegment", goalTitle: "../Title", want: "root-ws/../Title"},
		{name: "doubleSlash", goalTitle: "A//B", want: "root-ws/A//B"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, os.WriteFile(filepath.Join(forkWSDir, "GOAL.md"), []byte("---\ntitle: "+tt.goalTitle+"\n---\n# Fork Goal\n"), 0o644))

			result := server.buildWorkspaceFullState(groups[0].Forks[0], groups)

			assert.Equal(t, tt.goalTitle, result.Title)
			assert.Equal(t, tt.want, result.ComputedTitle)
		})
	}
}

func TestCollectForksForAPIFromGroupsUsesComputedTitleForForks(t *testing.T) {
	server, rootDir := setupTestServer(t)
	rootWSDir := setupTestWorkspace(t, server, rootDir, "root-ws")
	forkWSDir := setupTestWorkspace(t, server, rootDir, "fork-ws")
	require.NoError(t, os.WriteFile(filepath.Join(rootWSDir, "GOAL.md"), []byte("---\ntitle: Root Goal Title\n---\n# Root Goal\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(forkWSDir, "GOAL.md"), []byte("---\ntitle: Fork Goal Title\n---\n# Fork Goal\n"), 0o644))

	groups := []workspaceGroup{workspaceGroupWith(func(group *workspaceGroup) {
		group.Root = workspaceWith(func(workspace *workspaceInfo) {
			workspace.Directory = rootWSDir
			workspace.DirName = "root-ws"
			workspace.IsRoot = true
			workspace.HasWorkspace = true
		})
		group.Forks = []workspaceInfo{workspaceWith(func(workspace *workspaceInfo) {
			workspace.Directory = forkWSDir
			workspace.DirName = "fork-ws"
			workspace.HasWorkspace = true
		})}
	})}

	result := server.collectForksForAPIFromGroups(rootWSDir, groups)
	require.Len(t, result, 1)
	assert.Equal(t, "Fork Goal Title", result[0].Title)
	assert.Equal(t, "root-ws/Fork Goal Title", result[0].ComputedTitle)
}

func TestCollectForksForAPIFromGroupsMatchesSymlinkEquivalentRootDir(t *testing.T) {
	server, rootDir := setupTestServer(t)
	rootWSDir := setupTestWorkspace(t, server, rootDir, "root-ws")
	forkWSDir := setupTestWorkspace(t, server, rootDir, "fork-ws")
	aliasParent := t.TempDir()
	rootAlias := filepath.Join(aliasParent, "root-ws-link")
	require.NoError(t, os.Symlink(rootWSDir, rootAlias))

	groups := []workspaceGroup{workspaceGroupWith(func(group *workspaceGroup) {
		group.Root = workspaceWith(func(workspace *workspaceInfo) {
			workspace.Directory = rootWSDir
			workspace.DirName = "root-ws"
			workspace.IsRoot = true
			workspace.HasWorkspace = true
		})
		group.Forks = []workspaceInfo{workspaceWith(func(workspace *workspaceInfo) {
			workspace.Directory = forkWSDir
			workspace.DirName = "fork-ws"
			workspace.HasWorkspace = true
		})}
	})}

	result := server.collectForksForAPIFromGroups(rootAlias, groups)
	require.Len(t, result, 1)
	assert.Equal(t, forkWSDir, result[0].Dir)
}

func TestForkRootDirNameMatchesSymlinkEquivalentForkDir(t *testing.T) {
	_, rootDir := setupTestServer(t)
	rootWSDir := setupTestWorkspace(t, nil, rootDir, "root-ws")
	forkWSDir := setupTestWorkspace(t, nil, rootDir, "fork-ws")
	aliasParent := t.TempDir()
	forkAlias := filepath.Join(aliasParent, "fork-ws-link")
	require.NoError(t, os.Symlink(forkWSDir, forkAlias))

	groups := []workspaceGroup{workspaceGroupWith(func(group *workspaceGroup) {
		group.Root = workspaceWith(func(workspace *workspaceInfo) {
			workspace.Directory = rootWSDir
			workspace.DirName = "root-ws"
			workspace.IsRoot = true
			workspace.HasWorkspace = true
		})
		group.Forks = []workspaceInfo{workspaceWith(func(workspace *workspaceInfo) {
			workspace.Directory = forkWSDir
			workspace.DirName = "fork-ws"
			workspace.HasWorkspace = true
		})}
	})}

	rootDirName, ok := forkRootDirName(forkAlias, groups)
	assert.True(t, ok)
	assert.Equal(t, "root-ws", rootDirName)
}

func TestBuildWorkspaceFullStateNoGoal(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "nogoal-ws")

	ws := workspaceWith(func(workspace *workspaceInfo) {
		workspace.DirName = "nogoal-ws"
		workspace.Directory = wsDir
		workspace.HasWorkspace = true
	})
	result := srv.buildWorkspaceFullState(ws, nil)
	assert.Empty(t, result.Title)
	assert.Equal(t, "nogoal-ws", result.ComputedTitle)
	assert.False(t, result.HasEditedGoal)
}

func TestBuildWorkspaceFullStateNoFrontmatterUsesComputedTitle(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "nofm-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Plain Heading\n\nBody"), 0o644))

	ws := workspaceWith(func(workspace *workspaceInfo) {
		workspace.DirName = "nofm-ws"
		workspace.Directory = wsDir
		workspace.HasWorkspace = true
	})
	result := srv.buildWorkspaceFullState(ws, nil)
	assert.Empty(t, result.Title)
	assert.Equal(t, "nofm-ws", result.ComputedTitle)
}

func TestBuildWorkspaceFullStateRepairsMissingGoalTitle(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "repair-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\nflow: |\n  \"a\" -> \"b\"\n---\n# Improve Repository Titles\n\nBody"), 0o644))

	server.goalTitleComposer = func(_ string, _ []byte) (string, error) {
		return "Repaired Repository Title", nil
	}

	ws := workspaceWith(func(workspace *workspaceInfo) {
		workspace.DirName = "repair-ws"
		workspace.Directory = wsDir
		workspace.HasWorkspace = true
	})
	result := server.buildWorkspaceFullState(ws, nil)
	assert.Empty(t, result.Title)
	assert.Equal(t, "repair-ws", result.ComputedTitle)

	require.Eventually(t, func() bool {
		data, errRead := os.ReadFile(filepath.Join(wsDir, "GOAL.md"))
		if errRead != nil {
			return false
		}
		return strings.Contains(string(data), "title: Repaired Repository Title")
	}, time.Second, 10*time.Millisecond)

	result = server.buildWorkspaceFullState(ws, nil)
	assert.Equal(t, "Repaired Repository Title", result.Title)
	assert.Empty(t, result.ComputedTitle)
}

func TestBuildFullFactoryStateRepairsMissingTitlesSequentially(t *testing.T) {
	server, rootDir := setupTestServer(t)
	firstDir := setupTestWorkspace(t, server, rootDir, "first-ws")
	secondDir := setupTestWorkspace(t, server, rootDir, "second-ws")
	require.NoError(t, os.WriteFile(filepath.Join(firstDir, "GOAL.md"), []byte("---\nflow: test\n---\n# First Goal"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(secondDir, "GOAL.md"), []byte("---\nflow: test\n---\n# Second Goal"), 0o644))

	var mu sync.Mutex
	currentConcurrent := 0
	maxConcurrent := 0
	started := make(chan string, 2)
	finished := make(chan string, 2)
	release := make(chan struct{}, 2)
	server.goalTitleComposer = func(workspacePath string, _ []byte) (string, error) {
		mu.Lock()
		currentConcurrent++
		if currentConcurrent > maxConcurrent {
			maxConcurrent = currentConcurrent
		}
		mu.Unlock()

		started <- filepath.Base(workspacePath)
		<-release

		mu.Lock()
		currentConcurrent--
		mu.Unlock()
		finished <- filepath.Base(workspacePath)

		return strings.TrimSuffix(filepath.Base(workspacePath), "-ws") + " repaired", nil
	}

	result := server.buildWorkspaceListResponse()
	require.Len(t, result.Workspaces, 2)

	var firstStarted string
	require.Eventually(t, func() bool {
		select {
		case firstStarted = <-started:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)

	select {
	case secondStarted := <-started:
		t.Fatalf("second repair started before first was released: %s", secondStarted)
	default:
	}

	release <- struct{}{}

	var secondStarted string
	require.Eventually(t, func() bool {
		select {
		case secondStarted = <-started:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
	assert.NotEqual(t, firstStarted, secondStarted)

	release <- struct{}{}

	finishedCount := 0
	require.Eventually(t, func() bool {
		for {
			select {
			case <-finished:
				finishedCount++
			default:
				return finishedCount == 2
			}
		}
	}, time.Second, 10*time.Millisecond)

	require.Eventually(t, func() bool {
		firstData, errFirst := os.ReadFile(filepath.Join(firstDir, "GOAL.md"))
		secondData, errSecond := os.ReadFile(filepath.Join(secondDir, "GOAL.md"))
		if errFirst != nil || errSecond != nil {
			return false
		}
		return strings.Contains(string(firstData), "title: first repaired") && strings.Contains(string(secondData), "title: second repaired")
	}, 2*time.Second, 10*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, maxConcurrent)
}

func TestBuildWorkspaceFullStateRunningWithSession(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "running-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\n---\n# Goal"), 0o644))

	ws := workspaceWith(func(workspace *workspaceInfo) {
		workspace.DirName = "running-ws"
		workspace.Directory = wsDir
		workspace.HasWorkspace = true
		workspace.Running = true
	})
	result := srv.buildWorkspaceFullState(ws, nil)
	assert.True(t, result.Running)
}

func TestBuildWorkspaceFullStateSelfDriveModeFlag(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	sp := filepath.Join(wsDir, ".sgai", "state.json")
	_, errCoord := state.NewCoordinatorWith(sp, workflowWith(func(workflow *state.Workflow) {
		workflow.InteractionMode = state.ModeSelfDrive
	}))
	require.NoError(t, errCoord)

	ws := workspaceWith(func(workspace *workspaceInfo) {
		workspace.Directory = wsDir
		workspace.DirName = "test-ws"
	})
	result := server.buildWorkspaceFullState(ws, nil)
	assert.True(t, result.InteractiveAuto)
}

func TestBuildWorkspaceFullStateWithAgentSequence(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "seq-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\n---\n# Goal"), 0o644))
	statePath := filepath.Join(wsDir, ".sgai", "state.json")
	_, errCoord := state.NewCoordinatorWith(statePath, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
		workflow.AgentSequence = []state.AgentSequenceEntry{{Agent: "coordinator", StartTime: "2025-01-01T00:00:00Z", IsCurrent: false}}
	}))
	require.NoError(t, errCoord)

	ws := workspaceWith(func(workspace *workspaceInfo) {
		workspace.DirName = "seq-ws"
		workspace.Directory = wsDir
		workspace.HasWorkspace = true
	})
	result := srv.buildWorkspaceFullState(ws, nil)
	assert.NotEmpty(t, result.AgentSequence)
}

func TestBuildWorkspaceFullStateWithAutoMode(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "auto-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\n---\n# Goal"), 0o644))
	statePath := filepath.Join(wsDir, ".sgai", "state.json")
	_, errCoord := state.NewCoordinatorWith(statePath, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
		workflow.InteractionMode = state.ModeSelfDrive
	}))
	require.NoError(t, errCoord)

	ws := workspaceWith(func(workspace *workspaceInfo) {
		workspace.DirName = "auto-ws"
		workspace.Directory = wsDir
		workspace.HasWorkspace = true
	})
	result := srv.buildWorkspaceFullState(ws, nil)
	assert.True(t, result.InteractiveAuto)
}

func TestBuildWorkspaceFullStateWithCost(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "cost-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\n---\n# Goal"), 0o644))
	statePath := filepath.Join(wsDir, ".sgai", "state.json")
	_, errCoord := state.NewCoordinatorWith(statePath, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
		workflow.Cost = updated(newTestSessionCost(), func(cost *state.SessionCost) { cost.TotalCost = 1.50 })
	}))
	require.NoError(t, errCoord)

	ws := workspaceWith(func(workspace *workspaceInfo) {
		workspace.DirName = "cost-ws"
		workspace.Directory = wsDir
		workspace.HasWorkspace = true
	})
	result := srv.buildWorkspaceFullState(ws, nil)
	assert.InDelta(t, 1.50, result.Cost.TotalCost, 0.0001)
}

func TestBuildWorkspaceFullStateWithEditedGoal(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "edited-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\n---\n# Goal Content Here"), 0o644))

	ws := workspaceWith(func(workspace *workspaceInfo) {
		workspace.DirName = "edited-ws"
		workspace.Directory = wsDir
		workspace.HasWorkspace = true
	})
	result := srv.buildWorkspaceFullState(ws, nil)
	assert.True(t, result.HasEditedGoal)
}

func TestBuildWorkspaceFullStateWithFreeformPending(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	attachRunningSessionCoordinator(t, server, wsDir, workflowRef(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.HumanMessage = "What should I do next?"
		workflow.CurrentAgent = "builder"
	}))

	ws := workspaceWith(func(workspace *workspaceInfo) {
		workspace.Directory = wsDir
		workspace.DirName = "test-ws"
	})
	result := server.buildWorkspaceFullState(ws, nil)
	assert.NotNil(t, result.PendingQuestion)
	assert.Equal(t, "free-text", result.PendingQuestion.Type)
	assert.Equal(t, "builder", result.PendingQuestion.AgentName)
}

func TestBuildWorkspaceFullStateWithLogLines(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	sp := filepath.Join(wsDir, ".sgai", "state.json")
	_, errCoord := state.NewCoordinatorWith(sp, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.CurrentAgent = "coordinator"
		workflow.Task = "doing things"
	}))
	require.NoError(t, errCoord)

	ol := newCircularLogBuffer()
	ol.add(logLine{prefix: "[test]", text: "some log line"})

	server.mu.Lock()
	server.sessions[wsDir] = newTestServeSession(nil, true)
	server.sessions[wsDir].outputLog = ol
	server.mu.Unlock()

	ws := workspaceWith(func(workspace *workspaceInfo) {
		workspace.Directory = wsDir
		workspace.DirName = "test-ws"
		workspace.Running = true
	})
	result := server.buildWorkspaceFullState(ws, nil)
	assert.True(t, result.Running)
	assert.NotEmpty(t, result.Log)
}

func TestBuildWorkspaceFullStateWithMultiChoicePendingQuestion(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	attachRunningSessionCoordinator(t, server, wsDir, workflowRef(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.MultiChoiceQuestion = multiChoiceQuestionWith(func(question *state.MultiChoiceQuestion) {
			question.Questions = []state.QuestionItem{
				questionItemWith(func(item *state.QuestionItem) {
					item.Question = "Pick one"
					item.Choices = []string{"A", "B"}
					item.MultiSelect = true
				}),
			}
		})
		workflow.CurrentAgent = "coordinator"
	}))

	ws := workspaceWith(func(workspace *workspaceInfo) {
		workspace.Directory = wsDir
		workspace.DirName = "test-ws"
	})
	result := server.buildWorkspaceFullState(ws, nil)
	assert.NotNil(t, result.PendingQuestion)
	assert.Equal(t, "multi-choice", result.PendingQuestion.Type)
	require.Len(t, result.PendingQuestion.Questions, 1)
	assert.True(t, result.PendingQuestion.Questions[0].MultiSelect)
}

func TestBuildWorkspaceFullStateWithPMFile(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "pm-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\n---\n# Goal"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, ".sgai", "PROJECT_MANAGEMENT.md"), []byte("# PM Content"), 0o644))

	ws := workspaceWith(func(workspace *workspaceInfo) {
		workspace.DirName = "pm-ws"
		workspace.Directory = wsDir
		workspace.HasWorkspace = true
	})
	result := srv.buildWorkspaceFullState(ws, nil)
	assert.True(t, result.HasProjectMgmt)
	assert.NotEmpty(t, result.PMContent)
}

func TestBuildWorkspaceFullStateWithProgress(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "progress-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\n---\n# Goal"), 0o644))
	statePath := filepath.Join(wsDir, ".sgai", "state.json")
	_, errCoord := state.NewCoordinatorWith(statePath, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
		workflow.Progress = []state.ProgressEntry{
			{Agent: "", Timestamp: time.Now().UTC().Format(time.RFC3339), Description: "step 1"},
			{Agent: "", Timestamp: time.Now().UTC().Format(time.RFC3339), Description: "step 2"},
		}
	}))
	require.NoError(t, errCoord)

	ws := workspaceWith(func(workspace *workspaceInfo) {
		workspace.DirName = "progress-ws"
		workspace.Directory = wsDir
		workspace.HasWorkspace = true
	})
	result := srv.buildWorkspaceFullState(ws, nil)
	assert.NotEmpty(t, result.Events)
	assert.NotEmpty(t, result.LatestProgress)
}

func TestBuildWorkspaceFullStateWithWorkGatePending(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	attachRunningSessionCoordinator(t, server, wsDir, workflowRef(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.MultiChoiceQuestion = multiChoiceQuestionWith(func(question *state.MultiChoiceQuestion) {
			question.IsWorkGate = true
			question.Questions = []state.QuestionItem{
				questionItemWith(func(item *state.QuestionItem) {
					item.Question = "Approve?"
					item.Choices = []string{"Yes", "No"}
				}),
			}
		})
		workflow.CurrentAgent = "coordinator"
	}))

	ws := workspaceWith(func(workspace *workspaceInfo) {
		workspace.Directory = wsDir
		workspace.DirName = "test-ws"
	})
	result := server.buildWorkspaceFullState(ws, nil)
	assert.NotNil(t, result.PendingQuestion)
	assert.Equal(t, "work-gate", result.PendingQuestion.Type)
}

func TestCollectAgentModels(t *testing.T) {
	t.Run("noGoalFile", func(t *testing.T) {
		dir := t.TempDir()
		result := collectAgentModels(dir)
		assert.Nil(t, result)
	})

	t.Run("noModels", func(t *testing.T) {
		dir := t.TempDir()
		goalPath := filepath.Join(dir, "GOAL.md")
		require.NoError(t, os.WriteFile(goalPath, []byte("# No frontmatter"), 0o644))

		result := collectAgentModels(dir)
		assert.Nil(t, result)
	})

	t.Run("withModels", func(t *testing.T) {
		dir := t.TempDir()
		goalPath := filepath.Join(dir, "GOAL.md")
		content := "---\nmodels:\n  coordinator: claude-opus-4\n  developer: gpt-4\n---\n# Goal"
		require.NoError(t, os.WriteFile(goalPath, []byte(content), 0o644))

		result := collectAgentModels(dir)
		require.Len(t, result, 2)
		assert.Equal(t, "coordinator", result[0].Agent)
		assert.Equal(t, []string{"claude-opus-4"}, result[0].Models)
		assert.Equal(t, "developer", result[1].Agent)
		assert.Equal(t, []string{"gpt-4"}, result[1].Models)
	})
}

func TestCollectAgentModelsNoAgents(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai", "agent"), 0o755))
	result := collectAgentModels(dir)
	assert.Empty(t, result)
}

func TestCollectAgentModelsNoGoalReturnsNil(t *testing.T) {
	result := collectAgentModels(t.TempDir())
	assert.Nil(t, result)
}

func TestCollectAgentModelsWithGoal(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "GOAL.md"),
		[]byte("---\nmodels:\n  coordinator: anthropic/claude-opus-4-6\n---\n# Goal"), 0o644))

	result := collectAgentModels(dir)
	assert.NotNil(t, result)
}

func TestHandleAPIAdhocAlreadyRunningReturnsOutput(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	st := server.getAdhocState(wsDir)
	st.mu.Lock()
	st.running = true
	st.output.WriteString("already running output")
	st.mu.Unlock()
	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/adhoc", `{"prompt":"test","model":"anthropic/claude-sonnet-4-6"}`)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp apiAdhocResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.True(t, resp.Running)
	assert.Contains(t, resp.Output, "already running output")
}

func TestHandleAPIAttachWorkspaceValidDirectory(t *testing.T) {
	server, _ := setupTestServer(t)
	attachDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(attachDir, ".sgai"), 0o755))
	body := `{"path":"` + attachDir + `"}`
	w := serveHTTP(server, "POST", "/api/v1/workspaces/attach", body)
	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestHandleAPIBrowseDirectoriesWithPath(t *testing.T) {
	srv, _ := setupTestServer(t)
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub1"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub2"), 0o755))

	w := serveHTTP(srv, "GET", "/api/v1/browse-directories?path="+dir, "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPIBrowseDirsWithPathParam(t *testing.T) {
	server, rootDir := setupTestServer(t)
	w := serveHTTP(server, "GET", "/api/v1/browse-directories?path="+rootDir, "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPIDeleteForkConfirmedNoFork(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "delfork-conf-nofork")
	w := serveHTTP(srv, "POST", "/api/v1/workspaces/delfork-conf-nofork/delete-fork", `{"confirm":true,"forkDir":"/nonexistent"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIDeleteForkInvalidBodyJSON(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/delete-fork", "not-json")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIDeleteForkNoConfirmRequired(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/delete-fork", `{"confirm":false}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIDeleteForkNotRootOrForkWorkspace(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/delete-fork", `{"confirm":true}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIDeleteMessageInvalidID(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "delmsg-badid")

	w := serveHTTP(srv, "DELETE", "/api/v1/workspaces/delmsg-badid/messages/notanumber", "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIDeleteMessageInvalidIDPath(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "delmsg-path")
	w := serveHTTP(srv, "DELETE", "/api/v1/workspaces/delmsg-path/messages/abc", "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIDeleteMessageValidViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "delmsg-valid")
	statePath := filepath.Join(wsDir, ".sgai", "state.json")
	_, errCoord := state.NewCoordinatorWith(statePath, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
		workflow.Messages = []state.Message{messageWith(func(message *state.Message) {
			message.ID = 1
			message.FromAgent = "Human Partner"
			message.ToAgent = "coordinator"
			message.Body = "test"
			message.CreatedAt = "2025-01-01T00:00:00Z"
		})}
	}))
	require.NoError(t, errCoord)

	w := serveHTTP(srv, "DELETE", "/api/v1/workspaces/delmsg-valid/messages/1", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPIDeleteWorkspaceConfirmedStandalone(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "del-standalone-full")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Goal"), 0o644))

	w := serveHTTP(srv, "POST", "/api/v1/workspaces/del-standalone-full/delete", `{"confirm":true}`)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPIDeleteWorkspaceDirNotFoundError(t *testing.T) {
	server, _ := setupTestServer(t)
	w := serveHTTP(server, "POST", "/api/v1/workspaces/nonexistent/delete", `{"confirm":true}`)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAPIDeleteWorkspaceInvalidBodyJSON(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/delete", "not-json")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIDeleteWorkspaceNoConfirmRequired(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/delete", `{"confirm":false}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIDeleteWorkspaceStandaloneDelete(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/delete", `{"confirm":true}`)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp apiDeleteWorkspaceResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.Deleted)
	assert.True(t, resp.Detached)
}

func TestHandleAPIDetachWorkspaceNotAttached(t *testing.T) {
	srv, _ := setupTestServer(t)
	w := serveHTTP(srv, "POST", "/api/v1/workspaces/detach", `{"path":"/not/attached"}`)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAPIForkWorkspaceEmptyGoal(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "fork-empty")
	w := serveHTTP(srv, "POST", "/api/v1/workspaces/fork-empty/fork", `{"goalContent":""}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIForkWorkspaceInvalidBodyJSON(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/fork", "not-json")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIGetGoalSuccessReturnsContent(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# My Goal\nDo things"), 0o644))
	w := serveHTTP(server, "GET", "/api/v1/workspaces/test-ws/goal", "")
	assert.Equal(t, http.StatusOK, w.Code)
	var resp apiGoalResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Contains(t, resp.Content, "My Goal")
}

func TestHandleAPIListModelsViaHTTPReturnsModels(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "GET", "/api/v1/models?workspace=test-ws", "")
	assert.Equal(t, http.StatusOK, w.Code)
	var resp apiModelsResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.NotNil(t, resp.Models)
}

func TestHandleAPISkillDetailWithContent(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "skill-detail")
	skillPath := filepath.Join(wsDir, ".sgai", "skills", "coding-practices", "my-skill")
	require.NoError(t, os.MkdirAll(skillPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillPath, "SKILL.md"),
		[]byte("---\nname: my-skill\ndescription: Test skill\n---\n# My Skill\n\nDetailed instructions here."), 0o644))

	w := serveHTTP(srv, "GET", "/api/v1/skills/coding-practices/my-skill?workspace=skill-detail", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPISnippetDetailWithExtensionLookup(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	snippetDir := filepath.Join(wsDir, ".sgai", "snippets", "go")
	require.NoError(t, os.MkdirAll(snippetDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(snippetDir, "hello.go"), []byte("---\nname: Hello\ndescription: A hello snippet\n---\npackage main\nfunc main() {}"), 0o644))

	w := serveHTTP(server, "GET", "/api/v1/snippets/go/hello?workspace=test-ws", "")
	assert.Equal(t, http.StatusOK, w.Code)
	var resp apiSnippetDetailResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "Hello", resp.Name)
	assert.Equal(t, "go", resp.Language)
	assert.Contains(t, resp.Content, "package main")
}

func TestHandleAPISnippetDetailWithFile(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "snippet-detail")
	snippetDir := filepath.Join(wsDir, ".sgai", "snippets", "python")
	require.NoError(t, os.MkdirAll(snippetDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(snippetDir, "hello.py"),
		[]byte("# Python hello world\nprint('hello')"), 0o644))

	w := serveHTTP(srv, "GET", "/api/v1/snippets/python/hello.py?workspace=snippet-detail", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPISnippetsByLanguageFoundMatch(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	snippetDir := filepath.Join(wsDir, ".sgai", "snippets", "python")
	require.NoError(t, os.MkdirAll(snippetDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(snippetDir, "hello.py"), []byte("---\ndescription: py hello\n---\nprint('hi')"), 0o644))

	w := serveHTTP(server, "GET", "/api/v1/snippets/python?workspace=test-ws", "")
	assert.Equal(t, http.StatusOK, w.Code)
	var resp apiSnippetsByLanguageResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "python", resp.Language)
	assert.NotEmpty(t, resp.Snippets)
}

func TestHandleAPISnippetsByLanguageNoMatch(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "GET", "/api/v1/snippets/nonexistent?workspace=test-ws", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAPISnippetsByLanguageWithContent(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "sniplang-content")
	snippetDir := filepath.Join(wsDir, ".sgai", "snippets", "typescript")
	require.NoError(t, os.MkdirAll(snippetDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(snippetDir, "util.ts"),
		[]byte("// Utility functions\nexport const add = (a: number, b: number) => a + b;"), 0o644))

	w := serveHTTP(srv, "GET", "/api/v1/snippets/typescript?workspace=sniplang-content", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPIStartSessionNonExistent(t *testing.T) {
	srv, _ := setupTestServer(t)
	w := serveHTTP(srv, "POST", "/api/v1/workspaces/nonexistent-start/start", `{"auto":false}`)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAPISteerSuccessViaAPI(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	sp := filepath.Join(wsDir, ".sgai", "state.json")
	_, errCoord := state.NewCoordinatorWith(sp, newTestWorkflow())
	require.NoError(t, errCoord)
	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/steer", `{"message":"do something"}`)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPISteerWithState(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "steer-state")
	statePath := filepath.Join(wsDir, ".sgai", "state.json")
	_, errCoord := state.NewCoordinatorWith(statePath, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
		workflow.CurrentAgent = "coordinator"
	}))
	require.NoError(t, errCoord)

	w := serveHTTP(srv, "POST", "/api/v1/workspaces/steer-state/steer", `{"message":"focus on tests"}`)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPISteerWithValidSteer(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "steer-valid-full")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\nflow: |\n  digraph G {\n    \"coordinator\" -> \"builder\"\n  }\n---\n# Goal"), 0o644))
	statePath := filepath.Join(wsDir, ".sgai", "state.json")
	_, errCoord := state.NewCoordinatorWith(statePath, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
		workflow.CurrentAgent = "coordinator"
	}))
	require.NoError(t, errCoord)

	w := serveHTTP(srv, "POST", "/api/v1/workspaces/steer-valid-full/steer", `{"message":"focus on performance optimization"}`)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPIStopSessionAlreadyStopped(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "stop-already")
	statePath := filepath.Join(wsDir, ".sgai", "state.json")
	_, errCoord := state.NewCoordinatorWith(statePath, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
	}))
	require.NoError(t, errCoord)

	w := serveHTTP(srv, "POST", "/api/v1/workspaces/stop-already/stop", "{}")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPIStopSessionWithState(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "stop-state")
	statePath := filepath.Join(wsDir, ".sgai", "state.json")
	_, errCoord := state.NewCoordinatorWith(statePath, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
	}))
	require.NoError(t, errCoord)

	w := serveHTTP(srv, "POST", "/api/v1/workspaces/stop-state/stop", "{}")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPITogglePinAndUnpin(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "pin-toggle")
	srv.pinnedConfigDir = t.TempDir()

	w1 := serveHTTP(srv, "POST", "/api/v1/workspaces/pin-toggle/pin", "")
	assert.Equal(t, http.StatusOK, w1.Code)

	w2 := serveHTTP(srv, "POST", "/api/v1/workspaces/pin-toggle/pin", "")
	assert.Equal(t, http.StatusOK, w2.Code)
}

func TestHandleAPITogglePinNonExistent(t *testing.T) {
	srv, _ := setupTestServer(t)
	w := serveHTTP(srv, "POST", "/api/v1/workspaces/nonexistent-pin/pin", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAPITogglePinSuccessToggle(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/pin", "")
	assert.Equal(t, http.StatusOK, w.Code)
	var resp apiTogglePinResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.True(t, resp.Pinned)
}

func TestHandleAPIUpdateGoalInvalidJSONBody(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "PUT", "/api/v1/workspaces/test-ws/goal", "not-json")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIUpdateGoalSuccessWrites(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("old"), 0o644))
	w := serveHTTP(server, "PUT", "/api/v1/workspaces/test-ws/goal", `{"content":"# New Goal"}`)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp apiUpdateGoalResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.True(t, resp.Updated)
	data, errRead := os.ReadFile(filepath.Join(wsDir, "GOAL.md"))
	require.NoError(t, errRead)
	assert.Equal(t, "# New Goal", string(data))
}

func TestHandleAPIUpdateGoalWithContent(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "upgoal-content")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Old"), 0o644))

	w := serveHTTP(srv, "PUT", "/api/v1/workspaces/upgoal-content/goal", `{"content":"---\nflow: |\n  digraph G {\n    \"a\" -> \"b\"\n  }\n---\n# Updated Goal"}`)
	assert.Equal(t, http.StatusOK, w.Code)

	data, errRead := os.ReadFile(filepath.Join(wsDir, "GOAL.md"))
	require.NoError(t, errRead)
	assert.Contains(t, string(data), "Updated Goal")
}

func TestHandleAPIWorkflowSVGAvailableFromCache(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	server.svgCache.set(wsDir+"|Unknown", "<svg>test</svg>")
	w := serveHTTP(server, "GET", "/api/v1/workspaces/test-ws/workflow.svg", "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "image/svg+xml")
	assert.Contains(t, w.Body.String(), "<svg>test</svg>")
}

func TestHandleAPIWorkflowSVGNotAvailableError(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "GET", "/api/v1/workspaces/test-ws/workflow.svg", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestResolveCurrentModel(t *testing.T) {
	t.Run("fromState", func(t *testing.T) {
		wfState := workflowWith(func(workflow *state.Workflow) { workflow.CurrentModel = "claude-opus-4" })
		result := resolveCurrentModel("/some/path", &wfState)
		assert.Equal(t, "claude-opus-4", result)
	})

	t.Run("noAgent", func(t *testing.T) {
		wfState := newTestWorkflow()
		result := resolveCurrentModel("/some/path", &wfState)
		assert.Empty(t, result)
	})

	t.Run("fromGoalFile", func(t *testing.T) {
		dir := t.TempDir()
		goalPath := filepath.Join(dir, "GOAL.md")
		content := "---\nmodels:\n  coordinator: claude-opus-4\n---\n# Goal"
		require.NoError(t, os.WriteFile(goalPath, []byte(content), 0o644))

		wfState := workflowWith(func(workflow *state.Workflow) { workflow.CurrentAgent = "coordinator" })
		result := resolveCurrentModel(dir, &wfState)
		assert.Equal(t, "claude-opus-4", result)
	})

	t.Run("agentNotInGoal", func(t *testing.T) {
		dir := t.TempDir()
		goalPath := filepath.Join(dir, "GOAL.md")
		content := "---\nmodels:\n  coordinator: claude-opus-4\n---\n# Goal"
		require.NoError(t, os.WriteFile(goalPath, []byte(content), 0o644))

		wfState := workflowWith(func(workflow *state.Workflow) { workflow.CurrentAgent = "developer" })
		result := resolveCurrentModel(dir, &wfState)
		assert.Empty(t, result)
	})
}

func TestResolveCurrentModelNoAgentReturnsEmpty(t *testing.T) {
	wf := newTestWorkflow()
	result := resolveCurrentModel("/tmp", &wf)
	assert.Empty(t, result)
}

func TestResolveCurrentModelNoModel(t *testing.T) {
	dir := t.TempDir()
	wf := newTestWorkflow()
	result := resolveCurrentModel(dir, &wf)
	assert.Empty(t, result)
}

func TestResolveCurrentModelWithExplicitModel(t *testing.T) {
	wf := workflowWith(func(workflow *state.Workflow) { workflow.CurrentModel = "opus-4" })
	result := resolveCurrentModel("/tmp", &wf)
	assert.Equal(t, "opus-4", result)
}

func TestSPAMiddlewareStaticAssets(t *testing.T) {
	srv, _ := setupTestServer(t)
	mux := http.NewServeMux()
	srv.registerAPIRoutes(mux)
	handler := srv.spaMiddleware(mux)
	assert.NotNil(t, handler)
}

func TestSPAMiddlewareStaticAssetRequest(t *testing.T) {
	srv, _ := setupTestServer(t)
	mux := http.NewServeMux()
	srv.registerAPIRoutes(mux)
	handler := srv.spaMiddleware(mux)

	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", http.NoBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Contains(t, []int{http.StatusOK, http.StatusNotFound}, w.Code)
}

func TestSPAMiddlewareAPIRoute(t *testing.T) {
	srv, _ := setupTestServer(t)
	mux := http.NewServeMux()
	srv.registerAPIRoutes(mux)
	handler := srv.spaMiddleware(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces", http.NoBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestWriteJSON(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected string
	}{
		{
			name:     "simpleObject",
			value:    map[string]string{"key": "value"},
			expected: `{"key":"value"}`,
		},
		{
			name:     "emptyObject",
			value:    map[string]string{},
			expected: `{}`,
		},
		{
			name:     "array",
			value:    []string{"a", "b", "c"},
			expected: `["a","b","c"]`,
		},
		{
			name:     "string",
			value:    "hello",
			expected: `"hello"`,
		},
		{
			name:     "number",
			value:    42,
			expected: `42`,
		},
		{
			name:     "null",
			value:    nil,
			expected: `null`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeJSON(w, tt.value)

			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

			var result json.RawMessage
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
			assert.JSONEq(t, tt.expected, string(result))
		})
	}
}

func TestWriteJSONWithStruct(t *testing.T) {
	type testStruct struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	w := httptest.NewRecorder()
	writeJSON(w, testStruct{Name: "test", Value: 123})

	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"name":"test","value":123}`, w.Body.String())
}

func TestConvertMessagesForAPI(t *testing.T) {
	tests := []struct {
		name     string
		messages []state.Message
		expected []apiMessageEntry
	}{
		{
			name:     "empty",
			messages: []state.Message{},
			expected: []apiMessageEntry{},
		},
		{
			name: "singleMessage",
			messages: []state.Message{
				messageWith(func(message *state.Message) {
					message.ID = 1
					message.FromAgent = "agent1"
					message.ToAgent = "agent2"
					message.Body = "Hello"
					message.Read = true
					message.CreatedAt = "2024-01-15T10:30:00Z"
				}),
			},
			expected: []apiMessageEntry{
				{ID: 1, FromAgent: "agent1", ToAgent: "agent2", Body: "Hello", Subject: "Hello", Read: true, ReadAt: "", CreatedAt: "2024-01-15T10:30:00Z"},
			},
		},
		{
			name: "multipleMessages",
			messages: []state.Message{
				messageWith(func(message *state.Message) {
					message.ID = 1
					message.FromAgent = "agent1"
					message.ToAgent = "agent2"
					message.Body = "First"
					message.Read = true
					message.CreatedAt = "2024-01-15T10:00:00Z"
				}),
				messageWith(func(message *state.Message) {
					message.ID = 2
					message.FromAgent = "agent2"
					message.ToAgent = "agent1"
					message.Body = "Second"
					message.CreatedAt = "2024-01-15T11:00:00Z"
				}),
			},
			expected: []apiMessageEntry{
				{ID: 2, FromAgent: "agent2", ToAgent: "agent1", Body: "Second", Subject: "Second", Read: false, ReadAt: "", CreatedAt: "2024-01-15T11:00:00Z"},
				{ID: 1, FromAgent: "agent1", ToAgent: "agent2", Body: "First", Subject: "First", Read: true, ReadAt: "", CreatedAt: "2024-01-15T10:00:00Z"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertMessagesForAPI(tt.messages)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractSubject(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected string
	}{
		{
			name:     "empty",
			body:     "",
			expected: "",
		},
		{
			name:     "singleLine",
			body:     "This is a subject",
			expected: "This is a subject",
		},
		{
			name:     "multipleLines",
			body:     "Subject line\nSecond line\nThird line",
			expected: "Subject line",
		},
		{
			name:     "withNewline",
			body:     "Subject\n",
			expected: "Subject",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractSubject(tt.body)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertModelStatuses(t *testing.T) {
	tests := []struct {
		name     string
		displays []modelStatusDisplay
		expected []apiModelStatusEntry
	}{
		{
			name:     "empty",
			displays: []modelStatusDisplay{},
			expected: nil,
		},
		{
			name: "singleEntry",
			displays: []modelStatusDisplay{
				{ModelID: "model1", Status: "running"},
			},
			expected: []apiModelStatusEntry{
				{ModelID: "model1", Status: "running"},
			},
		},
		{
			name: "multipleEntries",
			displays: []modelStatusDisplay{
				{ModelID: "model1", Status: "running"},
				{ModelID: "model2", Status: "done"},
				{ModelID: "model3", Status: "error"},
			},
			expected: []apiModelStatusEntry{
				{ModelID: "model1", Status: "running"},
				{ModelID: "model2", Status: "done"},
				{ModelID: "model3", Status: "error"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertModelStatuses(tt.displays)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHandleRespondViaCoordinator(t *testing.T) {
	t.Run("noPendingQuestion", func(t *testing.T) {
		srv, _ := setupTestServer(t)
		dir := t.TempDir()
		sgaiDir := filepath.Join(dir, ".sgai")
		require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
		statePath := filepath.Join(sgaiDir, "state.json")
		coord, errCoord := state.NewCoordinatorWith(statePath, workflowWith(func(workflow *state.Workflow) {
			workflow.Status = state.StatusWorking
		}))
		require.NoError(t, errCoord)

		w := httptest.NewRecorder()
		srv.handleRespondViaCoordinator(w, dir, coord, respondRequestWith(func(request *apiRespondRequest) {
			request.Answer = "yes"
		}))
		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Contains(t, w.Body.String(), "no pending question")
	})

	t.Run("questionNotAvailable", func(t *testing.T) {
		srv, _ := setupTestServer(t)
		dir := t.TempDir()
		sgaiDir := filepath.Join(dir, ".sgai")
		require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
		statePath := filepath.Join(sgaiDir, "state.json")
		coord, errCoord := state.NewCoordinatorWith(statePath, workflowWith(func(workflow *state.Workflow) {
			workflow.Status = state.StatusWorking
			workflow.HumanMessage = "Pick one"
			workflow.MultiChoiceQuestion = multiChoiceQuestionWith(func(question *state.MultiChoiceQuestion) {
				question.Questions = []state.QuestionItem{questionItemWith(func(item *state.QuestionItem) {
					item.Question = "Pick one"
					item.Choices = []string{"A", "B"}
				})}
			})
		}))
		require.NoError(t, errCoord)

		w := httptest.NewRecorder()
		srv.handleRespondViaCoordinator(w, dir, coord, respondRequestWith(func(request *apiRespondRequest) {
			request.Answer = "yes"
		}))
		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Contains(t, w.Body.String(), "question not available")
	})

	t.Run("emptyResponse", func(t *testing.T) {
		srv, _ := setupTestServer(t)
		dir := t.TempDir()
		sgaiDir := filepath.Join(dir, ".sgai")
		require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
		statePath := filepath.Join(sgaiDir, "state.json")
		coord, errCoord := state.NewCoordinatorWith(statePath, workflowWith(func(workflow *state.Workflow) {
			workflow.Status = state.StatusWorking
			workflow.HumanMessage = "Choose"
			workflow.MultiChoiceQuestion = multiChoiceQuestionWith(func(question *state.MultiChoiceQuestion) {
				question.Questions = []state.QuestionItem{questionItemWith(func(item *state.QuestionItem) {
					item.Question = "Choose"
					item.Choices = []string{"X", "Y"}
				})}
			})
		}))
		require.NoError(t, errCoord)

		w := httptest.NewRecorder()
		srv.handleRespondViaCoordinator(w, dir, coord, newTestAPIRespondRequest())
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "response cannot be empty")
	})

	t.Run("stalePromptToken", func(t *testing.T) {
		srv, rootDir := setupTestServer(t)
		wsDir := setupTestWorkspace(t, srv, rootDir, "stale-token")
		coord := state.NewCoordinatorEmpty(filepath.Join(wsDir, ".sgai", "state.json"))
		firstErrCh, cancelFirst := startCoordinatorQuestion(t, coord, multiChoiceQuestionWith(func(question *state.MultiChoiceQuestion) {
			question.Questions = []state.QuestionItem{questionItemWith(func(item *state.QuestionItem) {
				item.Question = "Pick one"
				item.Choices = []string{"A", "B"}
			})}
		}), "Pick one")
		firstToken := waitForSessionPromptToken(t, coord)
		cancelFirst()
		require.ErrorIs(t, <-firstErrCh, context.Canceled)

		secondErrCh, cancelSecond := startCoordinatorQuestion(t, coord, multiChoiceQuestionWith(func(question *state.MultiChoiceQuestion) {
			question.Questions = []state.QuestionItem{questionItemWith(func(item *state.QuestionItem) {
				item.Question = "Pick one"
				item.Choices = []string{"A", "B"}
			})}
		}), "Pick one")
		defer cancelSecond()
		secondToken := waitForSessionPromptToken(t, coord)
		require.NotEqual(t, firstToken, secondToken)

		srv.mu.Lock()
		srv.sessions[wsDir] = newTestServeSession(coord, false)
		srv.mu.Unlock()

		staleBody, errMarshalStale := json.Marshal(respondRequestWith(func(request *apiRespondRequest) {
			request.PromptToken = firstToken
			request.Answer = "stale answer"
		}))
		require.NoError(t, errMarshalStale)
		w := serveHTTP(srv, "POST", "/api/v1/workspaces/stale-token/respond", string(staleBody))
		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Contains(t, w.Body.String(), "question not available")

		currentBody, errMarshalCurrent := json.Marshal(respondRequestWith(func(request *apiRespondRequest) {
			request.PromptToken = secondToken
			request.Answer = "current answer"
		}))
		require.NoError(t, errMarshalCurrent)
		w = serveHTTP(srv, "POST", "/api/v1/workspaces/stale-token/respond", string(currentBody))
		assert.Equal(t, http.StatusOK, w.Code)
		require.NoError(t, <-secondErrCh)
	})
}

func TestResolveForkDir(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "test-ws")

	t.Run("usesRequestForkDirWithinRoot", func(t *testing.T) {
		forkDir := filepath.Join(rootDir, "fork-1")
		require.NoError(t, os.MkdirAll(forkDir, 0o755))
		got := srv.resolveForkDir(forkDir, wsDir, rootDir)
		assert.NotEmpty(t, got)
	})

	t.Run("invalidRequestForkDir", func(t *testing.T) {
		got := srv.resolveForkDir("/nonexistent/12345", wsDir, rootDir)
		assert.Empty(t, got)
	})

	t.Run("workspaceNotRoot", func(t *testing.T) {
		got := srv.resolveForkDir("", wsDir, rootDir)
		assert.Equal(t, wsDir, got)
	})

	t.Run("workspaceIsRoot", func(t *testing.T) {
		got := srv.resolveForkDir("", rootDir, rootDir)
		assert.Empty(t, got)
	})
}

func TestServeReactIndex(t *testing.T) {
	t.Run("withIndex", func(t *testing.T) {
		webFS := fstest.MapFS{
			"index.html": &fstest.MapFile{Data: []byte("<html>React App</html>"), Mode: 0, ModTime: time.Time{}, Sys: nil},
		}
		w := httptest.NewRecorder()
		serveReactIndex(w, webFS)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
		assert.Contains(t, w.Body.String(), "React App")
	})

	t.Run("noIndex", func(t *testing.T) {
		webFS := fstest.MapFS{}
		w := httptest.NewRecorder()
		serveReactIndex(w, webFS)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestSPAMiddlewareRouting(t *testing.T) {
	srv, _ := setupTestServer(t)
	mux := http.NewServeMux()
	srv.registerAPIRoutes(mux)

	handler := srv.spaMiddleware(mux)

	t.Run("apiRoutePassesThrough", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces", http.NoBody)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("nonAPIRouteServesReact", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/dashboard", http.NoBody)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestHandleSignalStream(t *testing.T) {
	srv, _ := setupTestServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/signal", http.NoBody).WithContext(ctx)
	w := newLockedResponseRecorder()

	done := make(chan struct{})
	go func() {
		srv.handleSignalStream(w, req)
		close(done)
	}()

	cancel()
	<-done

	assert.Contains(t, w.Header().Get("Content-Type"), "text/event-stream")
}

func TestLockedWriterStripANSI(t *testing.T) {
	st := new(adhocPromptState)
	lw := &lockedWriter{mu: &st.mu, buf: &st.output}

	n, err := lw.Write([]byte("\x1b[31mhello\x1b[0m world"))
	require.NoError(t, err)
	assert.Equal(t, len("\x1b[31mhello\x1b[0m world"), n)

	st.mu.Lock()
	output := st.output.String()
	st.mu.Unlock()
	assert.Equal(t, "hello world", output)
}

func TestLockedWriterPlainText(t *testing.T) {
	st := new(adhocPromptState)
	lw := &lockedWriter{mu: &st.mu, buf: &st.output}

	n, err := lw.Write([]byte("plain text"))
	require.NoError(t, err)
	assert.Equal(t, len("plain text"), n)

	st.mu.Lock()
	output := st.output.String()
	st.mu.Unlock()
	assert.Equal(t, "plain text", output)
}

func TestGoalTitle(t *testing.T) {
	t.Run("emptyDirectory", func(t *testing.T) {
		result := goalTitleStateFromPath("", "fallback")
		assert.Empty(t, result.Title)
		assert.Equal(t, "fallback", result.ComputedTitle)
	})

	t.Run("noGoalFile", func(t *testing.T) {
		dir := t.TempDir()
		result := goalTitleStateFromPath(dir, "fallback")
		assert.Empty(t, result.Title)
		assert.Equal(t, "fallback", result.ComputedTitle)
	})

	t.Run("goalWithFrontmatterTitle", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "GOAL.md"), []byte("---\ntitle: Menu Title\n---\n# My Project\n\nSome description"), 0o644))
		result := goalTitleStateFromPath(dir, "fallback")
		assert.Equal(t, "Menu Title", result.Title)
		assert.Empty(t, result.ComputedTitle)
	})
}

func TestToMenuBarItem(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "GOAL.md"), []byte("---\ntitle: Test Title\n---\n# Test\nDesc"), 0o644))

	w := workspaceWith(func(workspace *workspaceInfo) {
		workspace.DirName = "test-ws"
		workspace.Directory = dir
		workspace.NeedsInput = true
		workspace.Running = true
		workspace.InProgress = true
	})

	item := toMenuBarItem(nil, w)
	assert.Equal(t, "test-ws", item.name)
	assert.Equal(t, "Test Title", item.title)
	assert.True(t, item.needsInput)
	assert.True(t, item.running)
	assert.False(t, item.stopped)
	assert.False(t, item.pinned)
}

func TestResolveForkTemplateContent(t *testing.T) {
	srv, _ := setupTestServer(t)
	content := srv.resolveForkTemplateContent("/nonexistent/root")
	assert.Equal(t, goalExampleContent, content)
}

var _ fs.FS = fstest.MapFS{}

func TestHandleAPIGetGoalViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "test-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# My Goal\nContent here"), 0o644))

	w := serveHTTP(srv, "GET", "/api/v1/workspaces/test-ws/goal", "")
	assert.Equal(t, http.StatusOK, w.Code)

	var resp apiGoalResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp.Content, "My Goal")
}

func TestHandleAPIForkTemplateNotRoot(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "standalone-ws")

	w := serveHTTP(srv, "GET", "/api/v1/workspaces/standalone-ws/fork-template", "")
	assert.Equal(t, http.StatusOK, w.Code)
	var resp apiForkTemplateResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, goalExampleContent, resp.Content)
}

func TestHandleAPIDeleteWorkspaceNoConfirm(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	_ = setupTestWorkspace(t, srv, rootDir, "ws")

	w := serveHTTP(srv, "POST", "/api/v1/workspaces/ws/delete", `{"confirm": false}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIOpenEditorFailureViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "editor-open-failure")
	srv.editorAvailable = true
	srv.editorName = "echo"
	editor := &recordingEditor{path: "", err: errors.New("boom")}
	srv.editor = editor

	w := serveHTTP(srv, "POST", "/api/v1/workspaces/editor-open-failure/open-editor", "")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "failed to open editor: boom")
	assert.Equal(t, wsDir, editor.path)
}

func TestHandleAPIOpenEditorViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "editor-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Goal"), 0o644))
	srv.editorAvailable = true
	srv.editorName = "echo"
	editor := &recordingEditor{path: "", err: nil}
	srv.editor = editor

	w := serveHTTP(srv, "POST", "/api/v1/workspaces/editor-ws/open-editor", "")
	assert.Equal(t, http.StatusOK, w.Code)
	var resp apiOpenEditorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.True(t, resp.Opened)
	assert.Equal(t, "echo", resp.Editor)
	assert.Equal(t, "opened in editor", resp.Message)
	assert.Equal(t, wsDir, editor.path)
}

func TestListModelsServiceFallback(t *testing.T) {
	srv, _ := setupTestServer(t)
	result := srv.listModelsService("nonexistent")
	assert.NotNil(t, result.Models)
}

func TestHandleAPIWorkspaceDiffRouteRemoved(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	setupTestWorkspace(t, srv, rootDir, "no-jj-ws")

	w := serveHTTP(srv, "GET", "/api/v1/workspaces/no-jj-ws/diff", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAPISteerViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "steer-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\n---\n# Original"), 0o644))

	statePath := filepath.Join(wsDir, ".sgai", "state.json")
	_, errCoord := state.NewCoordinatorWith(statePath, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
	}))
	require.NoError(t, errCoord)

	w := serveHTTP(srv, "POST", "/api/v1/workspaces/steer-ws/steer", `{"message":"Add logging"}`)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
}

func TestCheckWorkspaceState(t *testing.T) {
	srv, _ := setupTestServer(t)

	dir := t.TempDir()
	sgaiDir := filepath.Join(dir, ".sgai")
	require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
	statePath := filepath.Join(sgaiDir, "state.json")
	_, errCoord := state.NewCoordinatorWith(statePath, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
		workflow.Task = "done"
	}))
	require.NoError(t, errCoord)

	snapshots := make(map[string]workspaceStateSnapshot)
	activeWorkspaces := make(map[string]bool)

	srv.checkWorkspaceState(dir, snapshots, activeWorkspaces)
	assert.True(t, activeWorkspaces[dir])
	assert.Contains(t, snapshots, dir)
}

func TestCheckWorkspaceStateNoStateFile(t *testing.T) {
	srv, _ := setupTestServer(t)

	dir := t.TempDir()
	snapshots := make(map[string]workspaceStateSnapshot)
	activeWorkspaces := make(map[string]bool)

	srv.checkWorkspaceState(dir, snapshots, activeWorkspaces)
	assert.True(t, activeWorkspaces[dir])
	assert.NotContains(t, snapshots, dir)
}

func TestPollWorkspaceStatesEmpty(t *testing.T) {
	srv, _ := setupTestServer(t)
	snapshots := make(map[string]workspaceStateSnapshot)
	srv.pollWorkspaceStates(snapshots)
	assert.Empty(t, snapshots)
}

func TestGetWorkspaceStatusComplete(t *testing.T) {
	srv, _ := setupTestServer(t)
	dir := t.TempDir()
	sgaiDir := filepath.Join(dir, ".sgai")
	require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
	statePath := filepath.Join(sgaiDir, "state.json")
	_, errCoord := state.NewCoordinatorWith(statePath, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
		workflow.Task = "done"
	}))
	require.NoError(t, errCoord)

	running, needsInput := srv.getWorkspaceStatus(dir)
	assert.False(t, running)
	assert.False(t, needsInput)
}

func TestGetWorkspaceStatusPreservesWorkingStatusOnDisk(t *testing.T) {
	srv, _ := setupTestServer(t)
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".sgai"), 0o755))

	stopCachedSession(t, srv, dir, workflowRef(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
	}))
	writeWorkflowStateToDisk(t, dir, workflowRef(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.Task = "resume me"
	}))

	running, needsInput := srv.getWorkspaceStatus(dir)
	assert.False(t, running)
	assert.False(t, needsInput)
	assert.Equal(t, state.StatusWorking, workflowStateFromDisk(t, dir).Status)
}

func TestHandleAPIRespondViaHTTP(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "respond-ws")
	_, errCh, cancel := startWaitingSessionQuestion(t, srv, wsDir, multiChoiceQuestionWith(func(question *state.MultiChoiceQuestion) {
		question.Questions = []state.QuestionItem{questionItemWith(func(item *state.QuestionItem) {
			item.Question = "Pick one"
			item.Choices = []string{"A", "B"}
		})}
	}), "Pick one")
	defer cancel()
	body := `{"selectedChoices":["A"]}`
	w := serveHTTP(srv, "POST", "/api/v1/workspaces/respond-ws/respond", body)
	assert.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, <-errCh)
}

func TestCollectSkillCategories(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func(*testing.T, string)
		validate  func(*testing.T, []apiSkillCategory)
	}{
		{
			name: "noSkills",
			setupFunc: func(_ *testing.T, _ string) {
			},
			validate: func(t *testing.T, categories []apiSkillCategory) {
				t.Helper()
				assert.Empty(t, categories)
			},
		},
		{
			name: "singleSkillNoCategory",
			setupFunc: func(t *testing.T, dir string) {
				t.Helper()
				skillDir := filepath.Join(dir, ".sgai", "skills", "test-skill")
				require.NoError(t, os.MkdirAll(skillDir, 0o755))
				skillContent := `---
description: Test skill description
---
# Test Skill`
				require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644))
			},
			validate: func(t *testing.T, categories []apiSkillCategory) {
				t.Helper()
				assert.Len(t, categories, 1)
				assert.Equal(t, "General", categories[0].Name)
				assert.Len(t, categories[0].Skills, 1)
				assert.Equal(t, "test-skill", categories[0].Skills[0].Name)
			},
		},
		{
			name: "singleSkillWithCategory",
			setupFunc: func(t *testing.T, dir string) {
				t.Helper()
				skillDir := filepath.Join(dir, ".sgai", "skills", "coding", "test-skill")
				require.NoError(t, os.MkdirAll(skillDir, 0o755))
				skillContent := `---
description: Test skill description
---
# Test Skill`
				require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644))
			},
			validate: func(t *testing.T, categories []apiSkillCategory) {
				t.Helper()
				assert.Len(t, categories, 1)
				assert.Equal(t, "coding", categories[0].Name)
				assert.Len(t, categories[0].Skills, 1)
				assert.Equal(t, "test-skill", categories[0].Skills[0].Name)
			},
		},
		{
			name: "multipleSkillsMultipleCategories",
			setupFunc: func(t *testing.T, dir string) {
				t.Helper()
				for _, skill := range []struct {
					category string
					name     string
				}{
					{"coding", "skill1"},
					{"coding", "skill2"},
					{"testing", "skill3"},
				} {
					skillDir := filepath.Join(dir, ".sgai", "skills", skill.category, skill.name)
					require.NoError(t, os.MkdirAll(skillDir, 0o755))
					skillContent := `---
description: ` + skill.name + `
---
# ` + skill.name
					require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644))
				}
			},
			validate: func(t *testing.T, categories []apiSkillCategory) {
				t.Helper()
				assert.Len(t, categories, 2)
				assert.Equal(t, "coding", categories[0].Name)
				assert.Len(t, categories[0].Skills, 2)
				assert.Equal(t, "testing", categories[1].Name)
				assert.Len(t, categories[1].Skills, 1)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setupFunc(t, dir)
			result := collectSkillCategories(dir)
			if tt.validate != nil {
				tt.validate(t, result)
			}
		})
	}
}

func TestConvertSnippetLanguages(t *testing.T) {
	tests := []struct {
		name       string
		categories []languageCategory
		expected   []apiLanguageCategory
	}{
		{
			name:       "empty",
			categories: []languageCategory{},
			expected:   []apiLanguageCategory{},
		},
		{
			name: "singleCategory",
			categories: []languageCategory{
				{
					Name: "go",
					Snippets: []snippetData{
						{Name: "snippet1", FileName: "file1", FullPath: "", Description: "desc1", Language: ""},
					},
				},
			},
			expected: []apiLanguageCategory{
				{
					Name: "go",
					Snippets: []apiSnippetEntry{
						{Name: "snippet1", FileName: "file1", FullPath: "", Description: "desc1", Language: ""},
					},
				},
			},
		},
		{
			name: "multipleCategories",
			categories: []languageCategory{
				{
					Name: "go",
					Snippets: []snippetData{
						{Name: "go-snippet", FileName: "go-file", FullPath: "", Description: "", Language: ""},
					},
				},
				{
					Name: "python",
					Snippets: []snippetData{
						{Name: "py-snippet", FileName: "py-file", FullPath: "", Description: "", Language: ""},
					},
				},
			},
			expected: []apiLanguageCategory{
				{
					Name: "go",
					Snippets: []apiSnippetEntry{
						{Name: "go-snippet", FileName: "go-file", FullPath: "", Description: "", Language: ""},
					},
				},
				{
					Name: "python",
					Snippets: []apiSnippetEntry{
						{Name: "py-snippet", FileName: "py-file", FullPath: "", Description: "", Language: ""},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertSnippetLanguages(tt.categories)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertTodosForAPI(t *testing.T) {
	tests := []struct {
		name     string
		todos    []state.TodoItem
		expected []apiTodoEntry
	}{
		{
			name:     "empty",
			todos:    []state.TodoItem{},
			expected: []apiTodoEntry{},
		},
		{
			name: "singleTodo",
			todos: []state.TodoItem{
				{ID: "1", Content: "Task 1", Status: "pending", Priority: "high"},
			},
			expected: []apiTodoEntry{
				{ID: "1", Content: "Task 1", Status: "pending", Priority: "high"},
			},
		},
		{
			name: "multipleTodos",
			todos: []state.TodoItem{
				{ID: "1", Content: "Task 1", Status: "pending", Priority: "high"},
				{ID: "2", Content: "Task 2", Status: "completed", Priority: "medium"},
				{ID: "3", Content: "Task 3", Status: "in_progress", Priority: "low"},
			},
			expected: []apiTodoEntry{
				{ID: "1", Content: "Task 1", Status: "pending", Priority: "high"},
				{ID: "2", Content: "Task 2", Status: "completed", Priority: "medium"},
				{ID: "3", Content: "Task 3", Status: "in_progress", Priority: "low"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertTodosForAPI(tt.todos)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHandleAPIDeleteForkNoConfirm(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/delete-fork", `{"confirm":false}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIDeleteWorkspaceNotFound(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/nonexistent/delete", `{"confirm":true}`)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAPIDeleteWorkspaceStandalone(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	require.NoError(t, os.MkdirAll(filepath.Join(wsDir, ".jj"), 0o755))
	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/delete", `{"confirm":true}`)
	assert.Contains(t, []int{http.StatusOK, http.StatusInternalServerError, http.StatusNotFound}, w.Code)
}

func TestHandleAPIGetGoalMissing(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "GET", "/api/v1/workspaces/test-ws/goal", "")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleAPIUpdateGoalEmptyContent(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "PUT", "/api/v1/workspaces/test-ws/goal", `{"content":""}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIAdhocInvalidBody(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/adhoc", "{bad}")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIAdhocEmptyPrompt(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/adhoc", `{"prompt":"","model":"test"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIAdhocEmptyModel(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/adhoc", `{"prompt":"test","model":""}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIStartSessionRootWorkspace(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	canonicalDir := resolveSymlinks(wsDir)
	server.mu.Lock()
	server.externalDirs[canonicalDir] = true
	server.mu.Unlock()
	server.classifyCache.set(canonicalDir, workspaceRoot)
	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/start", `{}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "root workspace cannot start")
}

func TestHandleAPIStartSessionInvalidModelReturnsActionableError(t *testing.T) {
	if _, errLookPath := exec.LookPath("opencode"); errLookPath != nil {
		t.Skip("opencode not found in PATH")
	}

	if os.Getenv("SGAI_HELPER_INVALID_MODEL_START") == "1" {
		server, rootDir := setupTestServer(t)
		wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
		require.NoError(t, initializeWorkspace(wsDir))

		goalContent := strings.Join([]string{
			"---",
			"flow: |",
			`  "coordinator"`,
			"models:",
			"  coordinator: totally-fake-model-xyz",
			"---",
			"# Test Goal",
		}, "\n")
		require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte(goalContent), 0o644))

		w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/start", `{}`)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "invalid model(s) specified")
		assert.Contains(t, w.Body.String(), "totally-fake-model-xyz")
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run", "^TestHandleAPIStartSessionInvalidModelReturnsActionableError$")
	cmd.Env = append(os.Environ(), "SGAI_HELPER_INVALID_MODEL_START=1")
	output, errRun := cmd.CombinedOutput()
	require.NoError(t, errRun, "subprocess output:\n%s", string(output))
}

func TestHandleAPIForkWorkspaceStandalone(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/fork", `{"goalContent":"# Test"}`)
	assert.Contains(t, []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusCreated}, w.Code)
}

func TestHandleAPIAttachWorkspaceInvalidBody(t *testing.T) {
	server, _ := setupTestServer(t)
	w := serveHTTP(server, "POST", "/api/v1/workspaces/attach", "{bad}")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIDeleteMessageNumericNotFound(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "DELETE", "/api/v1/workspaces/test-ws/messages/99999", "")
	assert.Contains(t, []int{http.StatusOK, http.StatusNotFound, http.StatusInternalServerError}, w.Code)
}

func TestResolveWorkspaceFromPathNotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	w := serveHTTP(server, "GET", "/api/v1/workspaces/nonexistent-ws/goal", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestWriteJSONSuccess(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, map[string]string{"key": "value"})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Body.String(), `"key"`)
}

func TestSpaMiddleware(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := http.NewServeMux()
	handler := server.spaMiddleware(mux)
	assert.NotNil(t, handler)

	req := httptest.NewRequest(http.MethodGet, "/some-page", http.NoBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Contains(t, []int{http.StatusOK, http.StatusNotFound}, w.Code)
}

func TestResolveRootForDeleteFork(t *testing.T) {
	server, _ := setupTestServer(t)
	result := server.resolveRootForDeleteFork("/nonexistent")
	assert.Empty(t, result)
}

func TestHandleAPIDeleteWorkspaceRootBlocked(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/delete", `{"confirm":true}`)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "external workspace detached")
	server.mu.Lock()
	_, stillAttached := server.externalDirs[wsDir]
	server.mu.Unlock()
	assert.False(t, stillAttached)
}

func TestHandleAPIDeleteWorkspaceConfirmed(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Test"), 0o644))
	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/delete", `{"confirm":true}`)
	assert.Contains(t, []int{http.StatusOK, http.StatusInternalServerError}, w.Code)
}

func TestHandleAPIStopSessionNotFoundNewBatch(t *testing.T) {
	server, _ := setupTestServer(t)
	w := serveHTTP(server, "POST", "/api/v1/workspaces/nonexistent/stop", `{}`)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAPIForkTemplateRootNewBatch(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	server.classifyCache.set(wsDir, workspaceRoot)
	w := serveHTTP(server, "GET", "/api/v1/workspaces/test-ws/fork-template", "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "content")
}

func TestHandleAPIDeleteForkNotFoundNewBatch(t *testing.T) {
	server, _ := setupTestServer(t)
	w := serveHTTP(server, "POST", "/api/v1/workspaces/nonexistent/delete-fork", `{}`)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAPIDeleteForkStandaloneBlocked(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/delete-fork", `{}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "not a root or fork")
}

func TestHandleAPIDeleteForkInvalidForkDir(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	server.classifyCache.set(wsDir, workspaceRoot)
	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/delete-fork", `{"confirm":true,"forkDir":"../bad"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIAdhocInvalidJSON(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/adhoc", "{invalid}")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIAdhocMissingPrompt(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/adhoc", `{"prompt":"","model":"test"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "prompt and model are required")
}

func TestHandleAPIAdhocMissingModel(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/adhoc", `{"prompt":"test","model":""}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIAdhocStopNotRunning(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "DELETE", "/api/v1/workspaces/test-ws/adhoc", "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "stopped")
}

func TestHandleAPIUpdateGoalValidContent(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Old"), 0o644))
	w := serveHTTP(server, "PUT", "/api/v1/workspaces/test-ws/goal", `{"content":"# New Goal"}`)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPIRespondNoPendingQuestion(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/respond", `{"promptToken":"q1","answer":"test"}`)
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "no pending question")
}

func TestHandleAPIAttachWorkspaceNotAbsolute(t *testing.T) {
	server, _ := setupTestServer(t)
	w := serveHTTP(server, "POST", "/api/v1/workspaces/attach", `{"path":"relative/path"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIDetachNonexistentWorkspace(t *testing.T) {
	server, _ := setupTestServer(t)
	w := serveHTTP(server, "POST", "/api/v1/workspaces/detach", `{"path":"/nonexistent/path"}`)
	assert.Contains(t, []int{http.StatusOK, http.StatusBadRequest, http.StatusNotFound, http.StatusInternalServerError}, w.Code)
}

func TestHandleAPIForkEmptyGoal(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/test-ws/fork", `{"goalContent":""}`)
	assert.Contains(t, []int{http.StatusBadRequest, http.StatusInternalServerError}, w.Code)
}

func TestHandleAPIListModelsWithWorkspaceNewBatch(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "test-ws")
	w := serveHTTP(server, "GET", "/api/v1/models?workspace=test-ws", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestResolveForkTemplateContentNoGroupsNewBatch(t *testing.T) {
	server, _ := setupTestServer(t)
	result := server.resolveForkTemplateContent("/nonexistent")
	assert.NotEmpty(t, result)
}

func TestReadNewestForkGoalWithForksNewBatch(t *testing.T) {
	dir := t.TempDir()
	forkDir := filepath.Join(dir, "fork1")
	require.NoError(t, os.MkdirAll(forkDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(forkDir, "GOAL.md"), []byte("# Fork Goal"), 0o644))
	forks := []workspaceInfo{workspaceWith(func(workspace *workspaceInfo) {
		workspace.DirName = "fork1"
		workspace.Directory = forkDir
	})}
	result := readNewestForkGoal(forks)
	assert.Equal(t, "# Fork Goal", result)
}

func TestResolveCallerAgentNewBatch(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	coord, errCoord := state.NewCoordinatorWith(stateFile, workflowWith(func(workflow *state.Workflow) { workflow.CurrentAgent = "builder" }))
	require.NoError(t, errCoord)
	t.Run("notCoordinator", func(t *testing.T) {
		result := resolveCallerAgent("tester", coord)
		assert.Equal(t, "tester", result)
	})
	t.Run("coordinatorWithCurrent", func(t *testing.T) {
		result := resolveCallerAgent("coordinator", coord)
		assert.Equal(t, "builder", result)
	})
	t.Run("coordinatorNoCurrent", func(t *testing.T) {
		emptyFile := filepath.Join(t.TempDir(), "state.json")
		emptyCoord, errEmpty := state.NewCoordinatorWith(emptyFile, newTestWorkflow())
		require.NoError(t, errEmpty)
		result := resolveCallerAgent("coordinator", emptyCoord)
		assert.Equal(t, "coordinator", result)
	})
}

func TestParseAgentIdentityHeaderNewBatch(t *testing.T) {
	t.Run("emptyHeader", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		result := parseAgentIdentityHeader(r)
		assert.Empty(t, result)
	})
	t.Run("agentWithPipe", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		r.Header.Set(agentIdentityHeader, "builder|extra")
		result := parseAgentIdentityHeader(r)
		assert.Equal(t, "builder", result)
	})
	t.Run("emptyBeforePipe", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		r.Header.Set(agentIdentityHeader, "|extra")
		result := parseAgentIdentityHeader(r)
		assert.Empty(t, result)
	})
}

func TestStartSessionServiceClassification(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "classify-ws")
	kind := server.classifyWorkspaceCached(wsDir)
	assert.Equal(t, workspaceStandalone, kind)
}

func TestStopSessionServiceAlreadyStopped(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "stopped-ws")

	result := server.stopSessionService(wsDir)
	assert.Equal(t, "stopped", result.Status)
	assert.False(t, result.Running)
	assert.Contains(t, result.Message, "already stopped")
}

func TestRespondServiceNoPendingQuestion(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "respond-ws2")
	statePath := filepath.Join(wsDir, ".sgai", "state.json")
	require.NoError(t, os.WriteFile(statePath, []byte(`{"status":"working"}`), 0o644))

	_, err := server.respondService(wsDir, "q1", "answer", nil)
	require.Error(t, err)
}

func TestSteerServiceEmptyMessage(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "steer-ws2")

	_, err := server.steerService(wsDir, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestSteerServiceValidMessage(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "steer-ws3")
	goalContent := "---\nflow: |\n  \"a\" -> \"b\"\n---\n# Test"
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte(goalContent), 0o644))
	statePath := filepath.Join(wsDir, ".sgai", "state.json")
	require.NoError(t, os.WriteFile(statePath, []byte(`{"status":"working","messages":[]}`), 0o644))

	result, err := server.steerService(wsDir, "please fix the tests")
	require.NoError(t, err)
	assert.True(t, result.Success)
}

func TestHandleAPIDeleteForkNotConfirmedNew(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "fork-ws")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/fork-ws/delete-fork", `{"confirm":false}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIDeleteWorkspaceNotConfirmed(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "del-ws")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/del-ws/delete", `{"confirm":false}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIAdhocStatusNotRunning(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "adhoc-ws")
	w := serveHTTP(server, "GET", "/api/v1/workspaces/adhoc-ws/adhoc", "")
	assert.Equal(t, http.StatusOK, w.Code)

	var resp apiAdhocResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.Running)
}

func TestHandleAPIAdhocStopWhenNotRunning(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "adhoc-ws2")
	w := serveHTTP(server, "DELETE", "/api/v1/workspaces/adhoc-ws2/adhoc", "")
	assert.Equal(t, http.StatusOK, w.Code)

	var resp apiAdhocResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.Running)
}

func TestHandleAPIStopSessionNoSessionNew(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "stop-ws")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/stop-ws/stop", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPIBrowseDirectoriesEmptyPath(t *testing.T) {
	server, _ := setupTestServer(t)
	w := serveHTTP(server, "GET", "/api/v1/browse-directories?path=", "")
	assert.Contains(t, []int{http.StatusOK, http.StatusBadRequest}, w.Code)
}

func TestHandleAPIDeleteMessageNotFoundNew(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "msg-ws3")
	w := serveHTTP(server, "DELETE", "/api/v1/workspaces/msg-ws3/messages/999", "")
	assert.Contains(t, []int{http.StatusOK, http.StatusNotFound, http.StatusBadRequest}, w.Code)
}

func TestHandleAPISteerEmptyMessage(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "steer-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\nflow: |\n  \"a\" -> \"b\"\n---\n# Test"), 0o644))
	w := serveHTTP(server, "POST", "/api/v1/workspaces/steer-ws/steer", `{"message":""}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIStateOmitsForkCommitFields(t *testing.T) {
	server, rootDir := setupTestServer(t)
	rootWSDir := filepath.Join(rootDir, "root-ws")
	forkDir := filepath.Join(rootDir, "fork-ws")

	require.NoError(t, os.MkdirAll(rootWSDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(rootWSDir, ".sgai"), 0o755))

	runJJ := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("jj", args...)
		cmd.Dir = dir
		output, errCmd := cmd.CombinedOutput()
		require.NoErrorf(t, errCmd, "jj %s: %s", strings.Join(args, " "), output)
	}

	runJJ(rootWSDir, "git", "init", ".")
	require.NoError(t, os.WriteFile(filepath.Join(rootWSDir, "README.md"), []byte("root\n"), 0o644))
	runJJ(rootWSDir, "commit", "-m", "initial")
	runJJ(rootWSDir, "bookmark", "create", "main", "-r", "@-")
	runJJ(rootWSDir, "workspace", "add", forkDir)
	require.NoError(t, os.MkdirAll(filepath.Join(forkDir, ".sgai"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(forkDir, "README.md"), []byte("root\nfork\n"), 0o644))
	runJJ(forkDir, "commit", "-m", "fork change")

	rootCanonical := resolveSymlinks(rootWSDir)
	forkCanonical := resolveSymlinks(forkDir)
	server.mu.Lock()
	server.externalDirs[rootCanonical] = true
	server.externalDirs[forkCanonical] = true
	server.mu.Unlock()
	server.invalidateWorkspaceScanCache()

	w := serveHTTP(server, "GET", "/api/v1/workspaces", "")
	assert.Equal(t, http.StatusOK, w.Code)

	type forkState struct {
		Name        string           `json:"name"`
		CommitAhead *json.RawMessage `json:"commitAhead"`
		Commits     *json.RawMessage `json:"commits"`
	}
	type workspaceState struct {
		Name  string      `json:"name"`
		Forks []forkState `json:"forks"`
	}
	type workspaceListResponse struct {
		Workspaces []workspaceState `json:"workspaces"`
	}

	var resp workspaceListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	var rootWorkspace *workspaceState
	for i := range resp.Workspaces {
		if resp.Workspaces[i].Name == "root-ws" {
			rootWorkspace = &resp.Workspaces[i]
			break
		}
	}
	require.NotNil(t, rootWorkspace)

	require.Len(t, rootWorkspace.Forks, 1)
	assert.Equal(t, "fork-ws", rootWorkspace.Forks[0].Name)
	assert.Nil(t, rootWorkspace.Forks[0].CommitAhead)
	assert.Nil(t, rootWorkspace.Forks[0].Commits)
}

func TestDeleteWorkspaceServiceDirect(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "del-svc-api")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Goal"), 0o644))

	result, err := server.deleteWorkspaceService(wsDir)
	require.Error(t, err)
	assert.False(t, result.Deleted)

	_, errStat := os.Stat(wsDir)
	require.NoError(t, errStat)
}

func TestDeleteMessageServiceDirect(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "msg-svc-api")

	coord := server.workspaceCoordinator(wsDir)
	require.NoError(t, coord.UpdateState(func(wf *state.Workflow) {
		wf.Messages = append(wf.Messages, messageWith(func(message *state.Message) {
			message.ID = 42
			message.Body = "hello"
		}))
	}))

	result, err := server.deleteMessageService(wsDir, 42)
	require.NoError(t, err)
	assert.True(t, result.Deleted)
	assert.Equal(t, 42, result.ID)
}

func TestDeleteMessageServiceNotFoundDirect(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "msg-svc2-api")
	_, err := server.deleteMessageService(wsDir, 999)
	require.Error(t, err)
}

func TestResolveForkTemplateContentNoForks(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "template-svc-api")
	content := server.resolveForkTemplateContent(wsDir)
	assert.NotEmpty(t, content)
}

func TestHandleAPIBrowseDirectoriesNonexistentPath(t *testing.T) {
	server, _ := setupTestServer(t)
	w := serveHTTP(server, "GET", "/api/v1/browse-directories?path=/nonexistent/path", "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIBrowseDirectoriesRelativePath(t *testing.T) {
	server, _ := setupTestServer(t)
	w := serveHTTP(server, "GET", "/api/v1/browse-directories?path=.", "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "path must be absolute")
}

func TestHandleAPIAdhocStatusState(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "adhoc-state")
	w := serveHTTP(server, "GET", "/api/v1/workspaces/adhoc-state/adhoc", "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "adhoc status")
}

func TestHandleAPIAdhocStopWhenNotRunningDirect(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "adhoc-stop-direct")
	w := serveHTTP(server, "DELETE", "/api/v1/workspaces/adhoc-stop-direct/adhoc", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPISnippetsWithWorkspaceNew(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "snippets-ws-new")
	goDir := filepath.Join(wsDir, ".sgai", "snippets", "go")
	require.NoError(t, os.MkdirAll(goDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(goDir, "test.go"), []byte("---\nname: Test\ndescription: Test snippet\n---\npackage main"), 0o644))
	w := serveHTTP(server, "GET", "/api/v1/snippets?workspace=snippets-ws-new", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPISteerValidMessageNew(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "steer-valid-new")
	stateFile := filepath.Join(wsDir, ".sgai", "state.json")
	_, errCoord := state.NewCoordinatorWith(stateFile, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
	}))
	require.NoError(t, errCoord)
	w := serveHTTP(server, "POST", "/api/v1/workspaces/steer-valid-new/steer", `{"message":"new direction"}`)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPIAdhocPostNoPrompt(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "adhoc-noprompt")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/adhoc-noprompt/adhoc", `{"prompt":""}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIAdhocPostInvalidBody(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "adhoc-badbody")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/adhoc-badbody/adhoc", "not json")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestBrowseDirectoriesServiceHomeFallback(t *testing.T) {
	entries, err := browseDirectoriesService("")
	require.NoError(t, err)
	assert.NotNil(t, entries)
}

func TestBrowseDirectoriesServiceExistingDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "visible"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".hidden"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0o644))
	entries, err := browseDirectoriesService(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, "visible", entries[0].Name)
}

func TestBrowseDirectoriesServiceNonexistent(t *testing.T) {
	_, err := browseDirectoriesService("/nonexistent/path")
	require.Error(t, err)
}

func TestLoadExternalDirsNoFile(t *testing.T) {
	srv, _ := setupTestServer(t)
	srv.externalConfigDir = t.TempDir()
	err := srv.loadExternalDirs()
	require.NoError(t, err)
}

func TestSaveAndLoadExternalDirs(t *testing.T) {
	srv, _ := setupTestServer(t)
	dir := t.TempDir()
	srv.externalConfigDir = dir
	srv.externalDirs = map[string]bool{"/tmp/test-ws": true}
	err := srv.saveExternalDirs()
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(dir, "external.json"))

	srv2, _ := setupTestServer(t)
	srv2.externalConfigDir = dir
	err = srv2.loadExternalDirs()
	require.NoError(t, err)
}

func TestIsExternalWorkspaceNotAttached(t *testing.T) {
	srv, _ := setupTestServer(t)
	srv.externalDirs = map[string]bool{"/tmp/ext-ws": true}
	assert.False(t, srv.isExternalWorkspace("/tmp/not-ext"))
}

func TestDetachExternalWorkspaceNotAttached(t *testing.T) {
	srv, _ := setupTestServer(t)
	srv.externalConfigDir = t.TempDir()
	_, err := srv.detachExternalWorkspaceService("/tmp/not-attached")
	assert.ErrorIs(t, err, errNotAttached)
}

func TestAttachExternalWorkspaceNotAbsolute(t *testing.T) {
	srv, _ := setupTestServer(t)
	_, err := srv.attachExternalWorkspaceService("relative/path")
	assert.ErrorIs(t, err, errPathNotAbsolute)
}

func TestAttachExternalWorkspaceNotDir(t *testing.T) {
	srv, _ := setupTestServer(t)
	f := filepath.Join(t.TempDir(), "file.txt")
	require.NoError(t, os.WriteFile(f, []byte("hi"), 0o644))
	_, err := srv.attachExternalWorkspaceService(f)
	assert.ErrorIs(t, err, errNotADirectory)
}

func TestAttachExternalWorkspaceUnderRoot(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	srv.externalConfigDir = t.TempDir()
	subDir := filepath.Join(rootDir, "subworkspace")
	require.NoError(t, os.MkdirAll(subDir, 0o755))
	result, err := srv.attachExternalWorkspaceService(subDir)
	require.NoError(t, err)
	assert.Equal(t, subDir, result.Dir)
}

func TestValidateProjectConfigInvalidModel(t *testing.T) {
	cfg := updated(newTestProjectConfig(), func(config *projectConfig) {
		config.DefaultModel = "invalid model with space"
	})
	err := validateProjectConfig(&cfg)
	require.Error(t, err)
}

func TestServeReactIndexMissingIndex(t *testing.T) {
	emptyFS := fstest.MapFS{}
	w := httptest.NewRecorder()
	serveReactIndex(w, emptyFS)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "react app not found")
}

func TestServeReactIndexSuccess(t *testing.T) {
	testFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>test</html>"), Mode: 0, ModTime: time.Time{}, Sys: nil},
	}
	w := httptest.NewRecorder()
	serveReactIndex(w, testFS)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, w.Body.String(), "<html>test</html>")
}

func TestHandleAPIDeleteWorkspaceDirNotExist(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "ws-phantom")
	require.NoError(t, os.RemoveAll(filepath.Join(rootDir, "ws-phantom")))
	w := serveHTTP(server, "POST", "/api/v1/workspaces/ws-phantom/delete", `{"confirm": true}`)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAPISteerSuccess(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "ws-steer3")
	sgaiDir := filepath.Join(wsDir, ".sgai")
	require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
	statePath := filepath.Join(sgaiDir, "state.json")
	require.NoError(t, os.WriteFile(statePath, []byte(`{"status":"working"}`), 0o644))

	w := serveHTTP(server, "POST", "/api/v1/workspaces/ws-steer3/steer", `{"message": "change direction"}`)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp apiSteerResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.True(t, resp.Success)
}

func TestHandleAPIGetGoalSuccess(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "ws-goal")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# My Goal"), 0o644))
	w := serveHTTP(server, "GET", "/api/v1/workspaces/ws-goal/goal", "")
	assert.Equal(t, http.StatusOK, w.Code)
	var resp apiGoalResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "# My Goal", resp.Content)
}

func TestHandleAPIUpdateGoalSuccess(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "ws-upgoal3")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Old"), 0o644))
	w := serveHTTP(server, "PUT", "/api/v1/workspaces/ws-upgoal3/goal", `{"content": "# New Goal"}`)
	assert.Equal(t, http.StatusOK, w.Code)
	data, errRead := os.ReadFile(filepath.Join(wsDir, "GOAL.md"))
	require.NoError(t, errRead)
	assert.Equal(t, "# New Goal", string(data))
}

func TestHandleAPITogglePinSuccess(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "ws-pin")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Goal"), 0o644))
	server.pinnedConfigDir = t.TempDir()
	w := serveHTTP(server, "POST", "/api/v1/workspaces/ws-pin/pin", "")
	assert.Equal(t, http.StatusOK, w.Code)
	var resp apiTogglePinResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.True(t, resp.Pinned)
}

func TestHandleAPIDeleteForkNotRootOrFork(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "ws-delfork3")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/ws-delfork3/delete-fork", `{"confirm":true}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIDeleteMessageMissingID(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "ws-delmsg")
	mux := http.NewServeMux()
	server.registerAPIRoutes(mux)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/ws-delmsg/messages/", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Contains(t, []int{http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed}, w.Code)
}

func TestHandleAPIDeleteMessageNotFound(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "ws-delmsg3")
	sgaiDir := filepath.Join(wsDir, ".sgai")
	require.NoError(t, os.WriteFile(filepath.Join(sgaiDir, "state.json"), []byte(`{"status":"working","messages":[]}`), 0o644))
	mux := http.NewServeMux()
	server.registerAPIRoutes(mux)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/ws-delmsg3/messages/99", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAPIDeleteMessageSuccess(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "ws-delmsg4")
	sgaiDir := filepath.Join(wsDir, ".sgai")
	stateData := `{"status":"working","messages":[{"id":1,"fromAgent":"a","toAgent":"b","body":"msg","read":false}]}`
	require.NoError(t, os.WriteFile(filepath.Join(sgaiDir, "state.json"), []byte(stateData), 0o644))
	mux := http.NewServeMux()
	server.registerAPIRoutes(mux)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/ws-delmsg4/messages/1", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp apiDeleteMessageResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.True(t, resp.Deleted)
	assert.Equal(t, 1, resp.ID)
}

func TestHandleAPIOpenEditorNotAvailable(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "ws-editor")
	server.editorAvailable = false
	w := serveHTTP(server, "POST", "/api/v1/workspaces/ws-editor/open-editor", "")
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestDeleteMessageServiceSuccess(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "ws-delsvc")
	sgaiDir := filepath.Join(wsDir, ".sgai")
	stateData := `{"status":"working","messages":[{"id":5,"fromAgent":"a","toAgent":"b","body":"test","read":false}]}`
	require.NoError(t, os.WriteFile(filepath.Join(sgaiDir, "state.json"), []byte(stateData), 0o644))
	result, err := srv.deleteMessageService(wsDir, 5)
	require.NoError(t, err)
	assert.True(t, result.Deleted)
	assert.Equal(t, 5, result.ID)
}

func TestDeleteMessageServiceNotFound(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "ws-delsvc2")
	sgaiDir := filepath.Join(wsDir, ".sgai")
	require.NoError(t, os.WriteFile(filepath.Join(sgaiDir, "state.json"), []byte(`{"status":"working","messages":[]}`), 0o644))
	_, err := srv.deleteMessageService(wsDir, 99)
	assert.ErrorIs(t, err, errMessageNotFound)
}

func TestTogglePin(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	srv.pinnedConfigDir = t.TempDir()
	assert.False(t, srv.isPinned(rootDir))
	require.NoError(t, srv.togglePin(rootDir))
	assert.True(t, srv.isPinned(rootDir))
	require.NoError(t, srv.togglePin(rootDir))
	assert.False(t, srv.isPinned(rootDir))
}

func TestHandleAPIAdhocPostMissingModel(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "ws-adhoc-m")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/ws-adhoc-m/adhoc", `{"prompt":"test","model":""}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIAdhocStopSuccess(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "ws-adhocstop")
	_ = server.getAdhocState(wsDir)
	mux := http.NewServeMux()
	server.registerAPIRoutes(mux)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/ws-adhocstop/adhoc", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPIWorkflowSVGNotAvailable(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "ws-svg")
	w := serveHTTP(server, "GET", "/api/v1/workspaces/ws-svg/workflow.svg", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSessionCoordinatorNoSession(t *testing.T) {
	srv, _ := setupTestServer(t)
	assert.Nil(t, srv.sessionCoordinator("/nonexistent"))
}

func TestHandleAPISignalStream(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server, _ := setupTestServer(t)
		w, cancel, done := openSignalStream(t, server)
		defer func() {
			cancel()
			synctest.Wait()
			<-done
		}()

		server.notifyStateChange()
		synctest.Wait()

		assert.Contains(t, w.Header().Get("Content-Type"), "text/event-stream")
		assert.Contains(t, w.BodyString(), "event: reload")
	})
}

func TestHandleAPISignalStreamDeliversReloadAndWorkspaceWhenNotificationsRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		wsDir := setupTestWorkspace(t, server, rootDir, "ws-coalesce")
		w, cancel, done := openSignalStream(t, server)
		defer func() {
			cancel()
			synctest.Wait()
			<-done
		}()

		server.notifyStateChange()
		server.notifyWorkspaceListChange(wsDir)
		synctest.Wait()

		assert.Contains(t, w.BodyString(), "event: reload")
		assert.Contains(t, w.BodyString(), "event: workspace")
		assert.Contains(t, w.BodyString(), filepath.Base(wsDir))
	})
}

func TestHandleAPISignalStreamUsesWorkspaceEventForLogUpdates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		wsDir := setupTestWorkspace(t, server, rootDir, "ws-signal")

		sess := newTestServeSession(nil, true)
		sess.outputLog = newCircularLogBuffer()
		server.mu.Lock()
		server.sessions[wsDir] = sess
		server.mu.Unlock()

		w, cancel, done := openSignalStream(t, server)
		defer func() {
			cancel()
			synctest.Wait()
			<-done
		}()

		writer := newSessionLogWriter(sess, wsDir, server)
		writer.addLine("line 1")
		synctest.Wait()

		assert.Contains(t, w.BodyString(), "event: workspace")
		assert.Contains(t, w.BodyString(), filepath.Base(wsDir))
		assert.NotContains(t, w.BodyString(), "event: reload")
	})
}

func TestHandleAPISignalStreamUsesWorkspaceEventForProgressUpdates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		wsDir := setupTestWorkspace(t, server, rootDir, "ws-progress")
		require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Test"), 0o644))
		writeWorkflowStateToDisk(t, wsDir, workflowRef(func(workflow *state.Workflow) {
			workflow.Status = state.StatusWorking
			workflow.Progress = []state.ProgressEntry{{Timestamp: time.Now().UTC().Format(time.RFC3339), Agent: "coordinator", Description: "step 1"}}
		}))

		w, cancel, done := openSignalStream(t, server)
		defer func() {
			cancel()
			synctest.Wait()
			<-done
		}()

		snapshots := make(map[string]workspaceStateSnapshot)
		active := make(map[string]bool)
		server.checkWorkspaceState(wsDir, snapshots, active)
		server.loadWorkspaceListResponse()

		writeWorkflowStateWithLaterModTime(t, wsDir, workflowRef(func(workflow *state.Workflow) {
			workflow.Status = state.StatusWorking
			workflow.Progress = []state.ProgressEntry{
				{Timestamp: time.Now().UTC().Format(time.RFC3339), Agent: "coordinator", Description: "step 1"},
				{Timestamp: time.Now().UTC().Format(time.RFC3339), Agent: "coordinator", Description: "step 2"},
			}
		}))

		server.checkWorkspaceState(wsDir, snapshots, active)
		synctest.Wait()

		assert.Contains(t, w.BodyString(), "event: workspace")
		assert.Contains(t, w.BodyString(), filepath.Base(wsDir))
		assert.Contains(t, w.BodyString(), "event: reload")
	})
}

func TestHandleAPISignalStreamUsesWorkspaceEventForStatusUpdates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		wsDir := setupTestWorkspace(t, server, rootDir, "ws-status")
		require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Test"), 0o644))
		writeWorkflowStateToDisk(t, wsDir, workflowRef(func(workflow *state.Workflow) {
			workflow.Status = state.StatusWorking
		}))

		w, cancel, done := openSignalStream(t, server)
		defer func() {
			cancel()
			synctest.Wait()
			<-done
		}()

		snapshots := make(map[string]workspaceStateSnapshot)
		active := make(map[string]bool)
		server.checkWorkspaceState(wsDir, snapshots, active)
		server.loadWorkspaceListResponse()

		writeWorkflowStateWithLaterModTime(t, wsDir, workflowRef(func(workflow *state.Workflow) {
			workflow.Status = state.StatusComplete
		}))

		server.checkWorkspaceState(wsDir, snapshots, active)
		synctest.Wait()

		assert.Contains(t, w.BodyString(), "event: workspace")
		assert.Contains(t, w.BodyString(), filepath.Base(wsDir))
		assert.Contains(t, w.BodyString(), "event: reload")
	})
}

func TestHandleAPISignalStreamUsesWorkspaceEventForWorkspacePageSummaryChanges(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		wsDir := setupTestWorkspace(t, server, rootDir, "ws-summary")
		require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Test"), 0o644))
		writeWorkflowStateToDisk(t, wsDir, workflowRef(func(workflow *state.Workflow) {
			workflow.Status = state.StatusWorking
			workflow.CurrentAgent = "coordinator"
			workflow.CurrentModel = "coordinator:gpt-4.1"
			workflow.Task = "first task"
			workflow.AgentSequence = []state.AgentSequenceEntry{{Agent: "coordinator", StartTime: "2026-03-30T00:00:00Z", IsCurrent: true}}
			workflow.Cost = updated(newTestSessionCost(), func(cost *state.SessionCost) {
				cost.TotalCost = 1.5
			})
			workflow.ModelStatuses = map[string]string{"coordinator:gpt-4.1": "model-working"}
		}))

		w, cancel, done := openSignalStream(t, server)
		defer func() {
			cancel()
			synctest.Wait()
			<-done
		}()

		snapshots := make(map[string]workspaceStateSnapshot)
		active := make(map[string]bool)
		server.checkWorkspaceState(wsDir, snapshots, active)
		server.loadWorkspaceListResponse()

		writeWorkflowStateWithLaterModTime(t, wsDir, workflowRef(func(workflow *state.Workflow) {
			workflow.Status = state.StatusWorking
			workflow.CurrentAgent = "go-developer"
			workflow.CurrentModel = "go-developer:gpt-5.4"
			workflow.Task = "second task"
			workflow.AgentSequence = []state.AgentSequenceEntry{
				{Agent: "coordinator", StartTime: "2026-03-30T00:00:00Z", IsCurrent: false},
				{Agent: "go-developer", StartTime: "2026-03-30T00:01:00Z", IsCurrent: true},
			}
			workflow.Cost = updated(newTestSessionCost(), func(cost *state.SessionCost) {
				cost.TotalCost = 3.25
			})
			workflow.ModelStatuses = map[string]string{"go-developer:gpt-5.4": "model-done"}
		}))

		server.checkWorkspaceState(wsDir, snapshots, active)
		synctest.Wait()

		body := w.BodyString()
		assert.Contains(t, body, "event: workspace")
		assert.Contains(t, body, filepath.Base(wsDir))
		assert.Contains(t, body, "event: reload")
	})
}

func TestHandleAPISignalStreamUsesReloadForSummaryChangesAfterWorkspaceListCacheExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		wsDir := setupTestWorkspace(t, server, rootDir, "ws-summary-cache-expiry")
		attachRunningSessionCoordinator(t, server, wsDir, workflowRef(func(workflow *state.Workflow) {
			workflow.Status = state.StatusWorking
			workflow.CurrentAgent = "go-developer"
			workflow.Task = "before"
		}))

		_ = server.loadWorkspaceListResponse()
		_, errLoad := server.loadWorkspacePageState(wsDir)
		require.NoError(t, errLoad)
		server.workspaceListCache.delete("workspaces")

		w, cancel, done := openSignalStream(t, server)
		defer func() {
			cancel()
			synctest.Wait()
			<-done
		}()

		wfState := workflowWith(func(workflow *state.Workflow) {
			workflow.Status = state.StatusWorking
			workflow.CurrentAgent = "go-developer"
			workflow.Task = "after"
		})
		server.notifyWorkspaceChangeForState(wsDir, &wfState, true)
		synctest.Wait()

		body := w.BodyString()
		assert.Equal(t, 1, strings.Count(body, "event: reload"))
		assert.Equal(t, 1, strings.Count(body, "event: workspace"))
		assert.Contains(t, body, filepath.Base(wsDir))
	})
}

func TestHandleAPISignalStreamKeepsWorkspaceEventOnlyWhenWorkspaceListCacheExpiresAndSummaryMatches(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		wsDir := setupTestWorkspace(t, server, rootDir, "ws-summary-cache-stable")
		attachRunningSessionCoordinator(t, server, wsDir, workflowRef(func(workflow *state.Workflow) {
			workflow.Status = state.StatusWorking
			workflow.CurrentAgent = "go-developer"
			workflow.Task = "stable summary"
		}))

		_ = server.loadWorkspaceListResponse()
		_, errLoad := server.loadWorkspacePageState(wsDir)
		require.NoError(t, errLoad)
		server.workspaceListCache.delete("workspaces")

		w, cancel, done := openSignalStream(t, server)
		defer func() {
			cancel()
			synctest.Wait()
			<-done
		}()

		wfState := workflowWith(func(workflow *state.Workflow) {
			workflow.Status = state.StatusWorking
			workflow.CurrentAgent = "go-developer"
			workflow.Task = "stable summary"
			workflow.Messages = []state.Message{messageWith(func(message *state.Message) {
				message.ID = 1
				message.FromAgent = "go-developer"
				message.ToAgent = "coordinator"
				message.Body = "page-only change"
			})}
		})
		server.notifyWorkspaceChangeForState(wsDir, &wfState, true)
		synctest.Wait()

		body := w.BodyString()
		assert.Equal(t, 0, strings.Count(body, "event: reload"))
		assert.Equal(t, 1, strings.Count(body, "event: workspace"))
		assert.Contains(t, body, filepath.Base(wsDir))
	})
}

func TestStartSessionAlreadyRunning(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "ws-start-double")
	server.mu.Lock()
	server.sessions[wsDir] = newTestServeSession(nil, true)
	server.mu.Unlock()

	result := server.startSession(wsDir)
	assert.True(t, result.alreadyRunning)
}

func TestPollWorkspaceStates(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "ws-poll")
	sgaiDir := filepath.Join(wsDir, ".sgai")
	require.NoError(t, os.WriteFile(filepath.Join(sgaiDir, "state.json"), []byte(`{"status":"working"}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\nflow: |\n  \"a\" -> \"b\"\n---\n# Test"), 0o644))

	snapshots := make(map[string]workspaceStateSnapshot)
	server.pollWorkspaceStates(snapshots)
	assert.NotEmpty(t, snapshots)
}

func TestCheckWorkspaceStateNoState(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "ws-check-no-state")
	snapshots := make(map[string]workspaceStateSnapshot)
	active := make(map[string]bool)
	server.checkWorkspaceState(wsDir, snapshots, active)
	assert.True(t, active[wsDir])
	assert.Empty(t, snapshots)
}

func TestCheckWorkspaceStateWithState(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "ws-check-state")
	sgaiDir := filepath.Join(wsDir, ".sgai")
	require.NoError(t, os.WriteFile(filepath.Join(sgaiDir, "state.json"), []byte(`{"status":"working"}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Test"), 0o644))
	snapshots := make(map[string]workspaceStateSnapshot)
	active := make(map[string]bool)
	server.checkWorkspaceState(wsDir, snapshots, active)
	assert.True(t, active[wsDir])
}

func TestCheckWorkspaceStateChanged(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "ws-check-change")
	sgaiDir := filepath.Join(wsDir, ".sgai")
	require.NoError(t, os.WriteFile(filepath.Join(sgaiDir, "state.json"), []byte(`{"status":"working"}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Test"), 0o644))

	snapshots := make(map[string]workspaceStateSnapshot)
	active := make(map[string]bool)
	server.checkWorkspaceState(wsDir, snapshots, active)

	require.NoError(t, os.WriteFile(filepath.Join(sgaiDir, "state.json"), []byte(`{"status":"complete"}`), 0o644))
	server.checkWorkspaceState(wsDir, snapshots, active)
}

func TestInitializeWorkspaceFullPath(t *testing.T) {
	dir := t.TempDir()
	err := initializeWorkspace(dir)
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(dir, "GOAL.md"))
	assert.DirExists(t, filepath.Join(dir, ".sgai"))
}

func TestStopSessionNotRunning(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "ws-stop-norun")
	server.stopSession(wsDir)
}

func TestHandleAPIStartSessionAlreadyRunningViaHTTP(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "ws-start-api")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\nflow: |\n  \"a\" -> \"b\"\n---\n# Test"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, ".sgai", "state.json"), []byte(`{"status":"working"}`), 0o644))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	server.shutdownCtx = ctx
	server.mu.Lock()
	server.sessions[wsDir] = newTestServeSession(nil, true)
	server.mu.Unlock()

	w := serveHTTP(server, "POST", "/api/v1/workspaces/ws-start-api/start", `{"auto":false}`)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp apiSessionActionResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.True(t, resp.Running)
	assert.Equal(t, "session already running", resp.Message)
}

func TestHandleAPIListModelsWithWorkspaceParam(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "models-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\nmodels:\n  coordinator: [\"anthropic/claude-opus-4-6\"]\n---\n# Models Test"), 0o644))
	w := serveHTTP(server, "GET", "/api/v1/models?workspace=models-ws", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPIDeleteForkNotARoot(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "standalone-delfork")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/standalone-delfork/delete-fork", `{"forkDir":"/tmp/nope","confirm":true}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIDeleteForkNoConfirmation(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "noconfirm-delfork")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/noconfirm-delfork/delete-fork", `{"forkDir":"/tmp/nope","confirm":false}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIForkWorkspaceNotRootStandalone(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, server, rootDir, "not-root-ws")
	w := serveHTTP(server, "POST", "/api/v1/workspaces/not-root-ws/fork", `{"goalContent":"# Test Goal"}`)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleAPIStopSessionRunning(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "stop-session-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Goal"), 0o644))
	server.mu.Lock()
	server.sessions[wsDir] = newTestServeSession(nil, true)
	server.mu.Unlock()
	w := serveHTTP(server, "POST", "/api/v1/workspaces/stop-session-ws/stop", `{}`)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAPIDeleteWorkspaceStandaloneConfirmed(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "delete-standalone-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Goal"), 0o644))
	w := serveHTTP(server, "POST", "/api/v1/workspaces/delete-standalone-ws/delete", `{"confirm": true}`)
	assert.Equal(t, http.StatusOK, w.Code)
	_, errStat := os.Stat(wsDir)
	require.NoError(t, errStat)
	assert.Contains(t, w.Body.String(), "external workspace detached")
}

func TestHandleAPIWorkflowSVGForWorkspaceWithFlow(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "svg-flow-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\nflow: coordinator -> worker\n---\n# Goal"), 0o644))
	w := serveHTTP(server, "GET", "/api/v1/workspaces/svg-flow-ws/workflow.svg", "")
	assert.Contains(t, []int{http.StatusOK, http.StatusNotFound}, w.Code)
}

func TestHandleAPIDeleteWorkspaceNoConfirmField(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "delete-noconfirm2-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Goal"), 0o644))
	w := serveHTTP(server, "POST", "/api/v1/workspaces/delete-noconfirm2-ws/delete", `{"confirm": false}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIDeleteWorkspaceDirAlreadyRemoved(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "delete-removed-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Goal"), 0o644))
	require.NoError(t, os.RemoveAll(wsDir))
	w := serveHTTP(server, "POST", "/api/v1/workspaces/delete-removed-ws/delete", `{"confirm": true}`)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAPIDeleteWorkspaceExternalDetach(t *testing.T) {
	server, rootDir := setupTestServer(t)
	extDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(extDir, ".sgai"), 0o755))
	server.mu.Lock()
	server.externalDirs[extDir] = true
	server.mu.Unlock()
	require.NoError(t, os.MkdirAll(filepath.Join(extDir, ".sgai"), 0o755))
	_ = rootDir
	wsName := filepath.Base(extDir)
	w := serveHTTP(server, "POST", "/api/v1/workspaces/"+wsName+"/delete", `{"confirm": true}`)
	assert.Contains(t, []int{http.StatusOK, http.StatusNotFound, http.StatusBadRequest}, w.Code)
}

func TestHandleAPIDetachWorkspaceInvalidBody(t *testing.T) {
	server, _ := setupTestServer(t)
	w := serveHTTP(server, "POST", "/api/v1/workspaces/detach", `not json`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIAttachWorkspaceNotDirectory(t *testing.T) {
	server, _ := setupTestServer(t)
	tmpFile := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(tmpFile, []byte("a file"), 0o644))
	w := serveHTTP(server, "POST", "/api/v1/workspaces/attach", `{"path": "`+tmpFile+`"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAPIAttachWorkspaceAlreadyAttached(t *testing.T) {
	server, _ := setupTestServer(t)
	extDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(extDir, ".sgai"), 0o755))
	canonical := resolveSymlinks(extDir)
	server.mu.Lock()
	server.externalDirs[canonical] = true
	server.mu.Unlock()
	w := serveHTTP(server, "POST", "/api/v1/workspaces/attach", `{"path": "`+extDir+`"}`)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestHandleAPIAttachWorkspaceUnderRootDir(t *testing.T) {
	rootDir := t.TempDir()
	server := NewServer(rootDir, newTestServerPaths(), "")
	server.externalConfigDir = t.TempDir()
	subDir := filepath.Join(rootDir, "subproject")
	require.NoError(t, os.MkdirAll(subDir, 0o755))
	w := serveHTTP(server, "POST", "/api/v1/workspaces/attach", `{"path": "`+subDir+`"}`)
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"dir":"`+subDir+`"`)
}

func TestHandleAPIAttachWorkspaceWithGoal(t *testing.T) {
	server, _ := setupTestServer(t)
	extDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(extDir, ".sgai"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(extDir, "GOAL.md"), []byte("# External Goal"), 0o644))
	w := serveHTTP(server, "POST", "/api/v1/workspaces/attach", `{"path": "`+extDir+`"}`)
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"hasGoal":true`)
}

func TestHandleAPIResetSessionSuccess(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "reset-success-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Goal"), 0o644))

	sp := filepath.Join(wsDir, ".sgai", "state.json")
	coord, errCoord := state.NewCoordinatorWith(sp, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
	}))
	require.NoError(t, errCoord)

	server.mu.Lock()
	server.sessions[wsDir] = newTestServeSession(coord, false)
	server.mu.Unlock()

	w := serveHTTP(server, "POST", "/api/v1/workspaces/reset-success-ws/reset", "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"status":"complete"`)
	assert.Contains(t, w.Body.String(), `"message":"session reset successfully"`)

	wf := workflowStateFromDisk(t, wsDir)
	assert.Equal(t, state.StatusComplete, wf.Status)
}

func TestHandleAPIResetSessionRunning(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "reset-running-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Goal"), 0o644))

	sp := filepath.Join(wsDir, ".sgai", "state.json")
	coord, errCoord := state.NewCoordinatorWith(sp, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
	}))
	require.NoError(t, errCoord)

	server.mu.Lock()
	server.sessions[wsDir] = newTestServeSession(coord, true)
	server.mu.Unlock()

	w := serveHTTP(server, "POST", "/api/v1/workspaces/reset-running-ws/reset", "")
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "cannot reset while session is running")
}

func TestHandleAPIResetSessionNoState(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "reset-nostate-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Goal"), 0o644))

	w := serveHTTP(server, "POST", "/api/v1/workspaces/reset-nostate-ws/reset", "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"status":"complete"`)

	coord := server.workspaceCoordinator(wsDir)
	wf := coord.State()
	assert.Equal(t, state.StatusComplete, wf.Status)
}

func TestHandleAPIResetSessionAlreadyComplete(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "reset-complete-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Goal"), 0o644))

	sp := filepath.Join(wsDir, ".sgai", "state.json")
	coord, errCoord := state.NewCoordinatorWith(sp, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
	}))
	require.NoError(t, errCoord)

	server.mu.Lock()
	server.sessions[wsDir] = newTestServeSession(coord, false)
	server.mu.Unlock()

	w := serveHTTP(server, "POST", "/api/v1/workspaces/reset-complete-ws/reset", "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"status":"complete"`)

	wf := workflowStateFromDisk(t, wsDir)
	assert.Equal(t, state.StatusComplete, wf.Status)
}
