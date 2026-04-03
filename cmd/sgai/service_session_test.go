package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ucirello/sgai/pkg/state"
)

func openSignalStream(t *testing.T, server *Server) (*lockedResponseRecorder, context.CancelFunc, <-chan struct{}) {
	t.Helper()
	mux := http.NewServeMux()
	server.registerAPIRoutes(mux)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/signal", http.NoBody).WithContext(ctx)
	w := newLockedResponseRecorder()

	done := make(chan struct{})
	go func() {
		mux.ServeHTTP(w, req)
		close(done)
	}()

	synctest.Wait()
	server.signals.mu.Lock()
	defer server.signals.mu.Unlock()
	require.Len(t, server.signals.subscribers, 1)

	return w, cancel, done
}

func completeStartedSessionForTest(t *testing.T, server *Server, workspacePath string, sess *session, coord *state.Coordinator) {
	t.Helper()
	server.finishSessionRun(workspacePath, sess, coord)
}

func startCoordinatorQuestion(t *testing.T, coord *state.Coordinator, question *state.MultiChoiceQuestion, humanMessage string) (<-chan error, context.CancelFunc) {
	t.Helper()
	ready := make(chan struct{}, 1)
	coord.OnUpdate(func() {
		if coord.State().NeedsHumanInput() {
			select {
			case ready <- struct{}{}:
			default:
			}
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := coord.AskAndWait(ctx, question, humanMessage, "coordinator")
		errCh <- err
	}()
	<-ready
	require.True(t, coord.State().NeedsHumanInput())
	return errCh, cancel
}

func waitForSessionPromptToken(t *testing.T, coord *state.Coordinator) string {
	t.Helper()
	promptToken := coord.CurrentPromptToken()
	require.NotEmpty(t, promptToken)
	return promptToken
}

func TestStopSessionService(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func(*testing.T, string, *Server)
		validate  func(*testing.T, stopSessionResult)
	}{
		{
			name: "stopRunningSession",
			setupFunc: func(t *testing.T, workspacePath string, _ *Server) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".sgai"), 0o755))
			},
			validate: func(t *testing.T, result stopSessionResult) {
				t.Helper()
				assert.Equal(t, "stopped", result.Status)
				assert.False(t, result.Running)
				assert.Contains(t, result.Message, "session")
			},
		},
		{
			name: "stopAlreadyStoppedSession",
			setupFunc: func(t *testing.T, workspacePath string, _ *Server) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".sgai"), 0o755))
			},
			validate: func(t *testing.T, result stopSessionResult) {
				t.Helper()
				assert.Equal(t, "stopped", result.Status)
				assert.False(t, result.Running)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootDir := t.TempDir()
			server := NewServer(rootDir, newTestServerPaths(), "")

			workspacePath := filepath.Join(rootDir, "test-workspace")
			require.NoError(t, os.MkdirAll(workspacePath, 0o755))
			tt.setupFunc(t, workspacePath, server)

			result := server.stopSessionService(workspacePath)

			if tt.validate != nil {
				tt.validate(t, result)
			}
		})
	}
}

func TestRespondService(t *testing.T) {
	tests := []struct {
		name            string
		promptToken     string
		answer          string
		selectedChoices []string
		setupFunc       func(*testing.T, string)
		wantErr         bool
		expectedErr     error
		errContains     string
		validate        func(*testing.T, respondResult)
	}{
		{
			name:            "respondToQuestion",
			promptToken:     "test-question-1",
			answer:          "Test answer",
			selectedChoices: []string{},
			setupFunc: func(t *testing.T, workspacePath string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".sgai"), 0o755))
			},
			wantErr:     true,
			expectedErr: errNoPendingQuestion,
			errContains: "no pending question",
			validate:    nil,
		},
		{
			name:            "respondWithEmptyAnswer",
			promptToken:     "test-question-1",
			answer:          "",
			selectedChoices: []string{},
			setupFunc: func(t *testing.T, workspacePath string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".sgai"), 0o755))
			},
			wantErr:     true,
			expectedErr: errNoPendingQuestion,
			errContains: "no pending question",
			validate:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootDir := t.TempDir()
			server := NewServer(rootDir, newTestServerPaths(), "")

			workspacePath := filepath.Join(rootDir, "test-workspace")
			require.NoError(t, os.MkdirAll(workspacePath, 0o755))
			tt.setupFunc(t, workspacePath)

			result, err := server.respondService(workspacePath, tt.promptToken, tt.answer, tt.selectedChoices)

			if tt.wantErr {
				require.Error(t, err)
				if tt.expectedErr != nil {
					require.ErrorIs(t, err, tt.expectedErr)
				}
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, result)
			}
		})
	}
}

func TestSteerService(t *testing.T) {
	tests := []struct {
		name        string
		message     string
		setupFunc   func(*testing.T, string)
		wantErr     bool
		expectedErr error
		errContains string
		validate    func(*testing.T, steerResult)
	}{
		{
			name:    "steerWithValidMessage",
			message: "Please focus on the database implementation",
			setupFunc: func(t *testing.T, workspacePath string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".sgai"), 0o755))
			},
			wantErr:     false,
			expectedErr: nil,
			errContains: "",
			validate: func(t *testing.T, result steerResult) {
				t.Helper()
				assert.True(t, result.Success)
				assert.Equal(t, "steering instruction added", result.Message)
			},
		},
		{
			name:    "steerWithEmptyMessage",
			message: "",
			setupFunc: func(t *testing.T, workspacePath string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".sgai"), 0o755))
			},
			wantErr:     true,
			expectedErr: errSteerMessageEmpty,
			errContains: "message cannot be empty",
			validate:    nil,
		},
		{
			name:    "steerWithWhitespaceOnlyMessage",
			message: "   \t\n  ",
			setupFunc: func(t *testing.T, workspacePath string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".sgai"), 0o755))
			},
			wantErr:     true,
			expectedErr: errSteerMessageEmpty,
			errContains: "message cannot be empty",
			validate:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootDir := t.TempDir()
			server := NewServer(rootDir, newTestServerPaths(), "")

			workspacePath := filepath.Join(rootDir, "test-workspace")
			require.NoError(t, os.MkdirAll(workspacePath, 0o755))
			tt.setupFunc(t, workspacePath)

			result, err := server.steerService(workspacePath, tt.message)

			if tt.wantErr {
				require.Error(t, err)
				if tt.expectedErr != nil {
					require.ErrorIs(t, err, tt.expectedErr)
				}
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, result)
			}
		})
	}
}

func TestStopSessionServiceIdempotency(t *testing.T) {
	rootDir := t.TempDir()
	server := NewServer(rootDir, newTestServerPaths(), "")

	workspacePath := filepath.Join(rootDir, "test-workspace")
	require.NoError(t, os.MkdirAll(workspacePath, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".sgai"), 0o755))

	result1 := server.stopSessionService(workspacePath)
	assert.Equal(t, "stopped", result1.Status)
	assert.False(t, result1.Running)
	assert.Contains(t, result1.Message, "session already stopped")

	result2 := server.stopSessionService(workspacePath)
	assert.Equal(t, "stopped", result2.Status)
	assert.False(t, result2.Running)
	assert.Contains(t, result2.Message, "session already stopped")
}

func TestRespondServiceValidation(t *testing.T) {
	tests := []struct {
		name            string
		promptToken     string
		answer          string
		selectedChoices []string
		setupFunc       func(*testing.T, string)
		wantErr         bool
		expectedErr     error
		errContains     string
	}{
		{
			name:            "respondWithEmptyPromptToken",
			promptToken:     "",
			answer:          "Test answer",
			selectedChoices: []string{},
			setupFunc: func(t *testing.T, workspacePath string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".sgai"), 0o755))
			},
			wantErr:     true,
			expectedErr: errNoPendingQuestion,
			errContains: "no pending question",
		},
		{
			name:            "respondWithEmptyAnswerAndChoices",
			promptToken:     "test-question-1",
			answer:          "",
			selectedChoices: []string{},
			setupFunc: func(t *testing.T, workspacePath string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".sgai"), 0o755))
			},
			wantErr:     true,
			expectedErr: errNoPendingQuestion,
			errContains: "no pending question",
		},
		{
			name:            "respondWithOnlyChoices",
			promptToken:     "test-question-1",
			answer:          "",
			selectedChoices: []string{"Option A", "Option B"},
			setupFunc: func(t *testing.T, workspacePath string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".sgai"), 0o755))
			},
			wantErr:     true,
			expectedErr: errNoPendingQuestion,
			errContains: "no pending question",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootDir := t.TempDir()
			server := NewServer(rootDir, newTestServerPaths(), "")

			workspacePath := filepath.Join(rootDir, "test-workspace")
			require.NoError(t, os.MkdirAll(workspacePath, 0o755))
			tt.setupFunc(t, workspacePath)

			result, err := server.respondService(workspacePath, tt.promptToken, tt.answer, tt.selectedChoices)

			if tt.wantErr {
				require.Error(t, err)
				if tt.expectedErr != nil {
					require.ErrorIs(t, err, tt.expectedErr)
				}
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			assert.True(t, result.Success)
		})
	}
}

func TestRespondViaCoordinatorServiceNoQuestion(t *testing.T) {
	rootDir := t.TempDir()
	server := NewServer(rootDir, newTestServerPaths(), "")

	workspacePath := filepath.Join(rootDir, "test-workspace")
	require.NoError(t, os.MkdirAll(workspacePath, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".sgai"), 0o755))

	coord := state.NewCoordinatorEmpty(statePath(workspacePath))
	req := respondRequestWith(func(request *apiRespondRequest) {
		request.PromptToken = "test-question-1"
		request.Answer = "Test answer"
	})

	_, err := server.respondViaCoordinatorService(workspacePath, coord, req)
	require.Error(t, err)
	require.ErrorIs(t, err, errNoPendingQuestion)
	assert.Contains(t, err.Error(), "no pending question")
}

func TestRespondViaCoordinatorServiceEmptyResponse(t *testing.T) {
	rootDir := t.TempDir()
	server := NewServer(rootDir, newTestServerPaths(), "")

	workspacePath := filepath.Join(rootDir, "test-workspace")
	require.NoError(t, os.MkdirAll(workspacePath, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".sgai"), 0o755))

	coord := state.NewCoordinatorEmpty(statePath(workspacePath))
	require.NoError(t, coord.UpdateState(func(wf *state.Workflow) {
		wf.HumanMessage = "What should I do?"
	}))

	req := respondRequestWith(func(request *apiRespondRequest) {
		request.Answer = ""
	})

	_, err := server.respondViaCoordinatorService(workspacePath, coord, req)
	require.Error(t, err)
	require.ErrorIs(t, err, errResponseCannotBeEmpty)
	assert.Contains(t, err.Error(), "response cannot be empty")
}

func TestRespondViaCoordinatorServiceWorkGateApproval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rootDir := t.TempDir()
		server := NewServer(rootDir, newTestServerPaths(), "")

		workspacePath := filepath.Join(rootDir, "test-workspace")
		require.NoError(t, os.MkdirAll(workspacePath, 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".sgai"), 0o755))

		coord := state.NewCoordinatorEmpty(statePath(workspacePath))
		require.NoError(t, coord.UpdateState(func(wf *state.Workflow) {
			wf.InteractionMode = state.ModeBrainstorming
		}))
		errCh, cancel := startCoordinatorQuestion(t, coord, multiChoiceQuestionWith(func(question *state.MultiChoiceQuestion) {
			question.Questions = []state.QuestionItem{questionItemWith(func(item *state.QuestionItem) {
				item.Question = "Approve this definition?"
				item.Choices = []string{workGateApprovalText, "Not ready"}
			})}
			question.IsWorkGate = true
		}), "Approve this definition?")
		defer cancel()
		promptToken := waitForSessionPromptToken(t, coord)

		req := respondRequestWith(func(request *apiRespondRequest) {
			request.PromptToken = promptToken
			request.SelectedChoices = []string{workGateApprovalText}
		})

		result, err := server.respondViaCoordinatorService(workspacePath, coord, req)
		require.NoError(t, err)
		assert.True(t, result.Success)
		synctest.Wait()
		require.NoError(t, <-errCh)

		wfState := coord.State()
		assert.Equal(t, state.ModeBuilding, wfState.InteractionMode)
	})
}

func TestRespondViaCoordinatorServiceRequiresPromptToken(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rootDir := t.TempDir()
		server := NewServer(rootDir, newTestServerPaths(), "")
		workspacePath := filepath.Join(rootDir, "test-workspace")
		require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".sgai"), 0o755))

		coord := state.NewCoordinatorEmpty(statePath(workspacePath))
		errCh, cancel := startCoordinatorQuestion(t, coord, multiChoiceQuestionWith(func(question *state.MultiChoiceQuestion) {
			question.Questions = []state.QuestionItem{questionItemWith(func(item *state.QuestionItem) {
				item.Question = "Pick one"
				item.Choices = []string{"A", "B"}
			})}
		}), "Pick one")

		_, err := server.respondViaCoordinatorService(workspacePath, coord, respondRequestWith(func(request *apiRespondRequest) {
			request.Answer = "current answer"
		}))
		require.Error(t, err)
		require.ErrorIs(t, err, errPromptTokenRequired)
		assert.Contains(t, err.Error(), "prompt token is required")
		assert.True(t, coord.State().NeedsHumanInput())

		cancel()
		synctest.Wait()
		require.ErrorIs(t, <-errCh, context.Canceled)
	})
}

func TestRespondViaCoordinatorServiceRejectsStalePromptToken(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rootDir := t.TempDir()
		server := NewServer(rootDir, newTestServerPaths(), "")
		workspacePath := filepath.Join(rootDir, "test-workspace")
		require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".sgai"), 0o755))

		coord := state.NewCoordinatorEmpty(statePath(workspacePath))
		firstErrCh, cancelFirst := startCoordinatorQuestion(t, coord, multiChoiceQuestionWith(func(question *state.MultiChoiceQuestion) {
			question.Questions = []state.QuestionItem{questionItemWith(func(item *state.QuestionItem) {
				item.Question = "Pick one"
				item.Choices = []string{"A", "B"}
			})}
		}), "Pick one")
		firstToken := waitForSessionPromptToken(t, coord)
		cancelFirst()
		synctest.Wait()
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

		_, err := server.respondViaCoordinatorService(workspacePath, coord, respondRequestWith(func(request *apiRespondRequest) {
			request.PromptToken = firstToken
			request.Answer = "stale answer"
		}))
		require.Error(t, err)
		require.ErrorIs(t, err, errQuestionNotAvailable)
		assert.Contains(t, err.Error(), "question not available")
		assert.True(t, coord.State().NeedsHumanInput())

		result, err := server.respondViaCoordinatorService(workspacePath, coord, respondRequestWith(func(request *apiRespondRequest) {
			request.PromptToken = secondToken
			request.Answer = "current answer"
		}))
		require.NoError(t, err)
		assert.True(t, result.Success)
		synctest.Wait()
		require.NoError(t, <-secondErrCh)
	})
}

func TestHandleRespondViaCoordinatorNoQuestion(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "respond-noq")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\n---\n# Goal"), 0o644))

	statePath := filepath.Join(wsDir, ".sgai", "state.json")
	coord, errCoord := state.NewCoordinatorWith(statePath, workflowWith(func(workflow *state.Workflow) {
		workflow.Status = state.StatusComplete
	}))
	require.NoError(t, errCoord)

	srv.mu.Lock()
	srv.sessions[wsDir] = newTestServeSession(coord, false)
	srv.mu.Unlock()

	body := `{"answer":"test"}`
	w := serveHTTP(srv, "POST", "/api/v1/workspaces/respond-noq/respond", body)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestRespondViaCoordinatorEmptyResponse(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "respond-empty")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\n---\n# Goal"), 0o644))

	statePath := filepath.Join(wsDir, ".sgai", "state.json")
	coord, errCoord := state.NewCoordinatorWith(statePath, workflowWith(func(workflow *state.Workflow) {
		workflow.HumanMessage = "Pick an option"
		workflow.MultiChoiceQuestion = multiChoiceQuestionWith(func(question *state.MultiChoiceQuestion) {
			question.Questions = []state.QuestionItem{questionItemWith(func(item *state.QuestionItem) {
				item.Question = "Pick one"
				item.Choices = []string{"A", "B"}
			})}
		})
	}))
	require.NoError(t, errCoord)

	srv.mu.Lock()
	srv.sessions[wsDir] = newTestServeSession(coord, false)
	srv.mu.Unlock()

	body := `{"answer":""}`
	w := serveHTTP(srv, "POST", "/api/v1/workspaces/respond-empty/respond", body)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRespondViaCoordinatorWorkGateApproval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv, rootDir := setupTestServer(t)
		wsDir := setupTestWorkspace(t, srv, rootDir, "respond-gate")
		require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("---\n---\n# Goal"), 0o644))

		statePath := filepath.Join(wsDir, ".sgai", "state.json")
		coord, errCoord := state.NewCoordinatorWith(statePath, workflowWith(func(workflow *state.Workflow) {
			workflow.InteractionMode = state.ModeBrainstorming
		}))
		require.NoError(t, errCoord)
		errCh, cancel := startCoordinatorQuestion(t, coord, multiChoiceQuestionWith(func(question *state.MultiChoiceQuestion) {
			question.Questions = []state.QuestionItem{questionItemWith(func(item *state.QuestionItem) {
				item.Question = "Is ready?"
				item.Choices = []string{workGateApprovalText, "Not ready"}
			})}
			question.IsWorkGate = true
		}), "Is this ready?")
		defer cancel()
		promptToken := waitForSessionPromptToken(t, coord)

		srv.mu.Lock()
		srv.sessions[wsDir] = newTestServeSession(coord, false)
		srv.mu.Unlock()

		body := `{"promptToken":"` + promptToken + `","answer":"","selectedChoices":["` + workGateApprovalText + `"]}`
		w := serveHTTP(srv, "POST", "/api/v1/workspaces/respond-gate/respond", body)
		assert.Equal(t, http.StatusOK, w.Code)
		synctest.Wait()
		require.NoError(t, <-errCh)

		updatedState := coord.State()
		assert.Equal(t, state.ModeBuilding, updatedState.InteractionMode)
	})
}

func TestStopSessionServiceRunningSession(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	server.mu.Lock()
	server.sessions[wsDir] = newTestServeSession(nil, true)
	server.mu.Unlock()
	result := server.stopSessionService(wsDir)
	assert.Equal(t, "session stopped", result.Message)
	assert.False(t, result.Running)
}

func TestStopSessionPublishesSingleReloadAndWorkspaceSignals(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server, rootDir := setupTestServer(t)
		wsDir := setupTestWorkspace(t, server, rootDir, "stop-signal-ws")

		coord := state.NewCoordinatorEmpty(statePath(wsDir))
		coord.OnUpdate(func() {
			wfState := coord.State()
			server.notifyWorkspaceChangeForState(wsDir, &wfState, server.sessionRunning(wsDir))
		})

		ctx, cancelWait := context.WithCancel(context.Background())
		sess := newTestServeSession(coord, true)
		sess.cancel = cancelWait
		sess.mcpCloseFn = func() {}

		server.mu.Lock()
		server.sessions[wsDir] = sess
		server.mu.Unlock()

		errCh := make(chan error, 1)
		go func() {
			_, err := coord.AskAndWait(ctx, nil, "question?", "coordinator")
			errCh <- err
		}()

		synctest.Wait()
		require.True(t, coord.State().NeedsHumanInput())

		_ = server.loadWorkspaceListResponse()
		_, errLoad := server.loadWorkspacePageState(wsDir)
		require.NoError(t, errLoad)

		w, cancelStream, done := openSignalStream(t, server)
		server.stopSession(wsDir)
		synctest.Wait()
		require.ErrorIs(t, <-errCh, context.Canceled)

		completeStartedSessionForTest(t, server, wsDir, sess, coord)
		synctest.Wait()

		cancelStream()
		synctest.Wait()
		<-done

		body := w.BodyString()
		assert.Equal(t, 1, strings.Count(body, "event: reload"))
		assert.Equal(t, 1, strings.Count(body, "event: workspace"))
		assert.Contains(t, body, filepath.Base(wsDir))

		wfState := coord.State()
		assert.Empty(t, wfState.HumanMessage)
		assert.Nil(t, wfState.MultiChoiceQuestion)
	})
}

func TestRespondServiceNoSession(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	sp := filepath.Join(wsDir, ".sgai", "state.json")
	_, errCoord := state.NewCoordinatorWith(sp, workflowWith(func(workflow *state.Workflow) {
		workflow.HumanMessage = "What?"
	}))
	require.NoError(t, errCoord)
	_, errRespond := server.respondService(wsDir, "wrong-id", "answer", nil)
	require.Error(t, errRespond)
	require.ErrorIs(t, errRespond, errNoPendingQuestion)
}

func TestSteerServiceSuccessful(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	sp := filepath.Join(wsDir, ".sgai", "state.json")
	_, errCoord := state.NewCoordinatorWith(sp, workflowWith(func(*state.Workflow) {}))
	require.NoError(t, errCoord)
	result, errSteer := server.steerService(wsDir, "do something different")
	require.NoError(t, errSteer)
	assert.True(t, result.Success)
}

func TestSteerServiceEmptyMessageFails(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	_, errSteer := server.steerService(wsDir, "  ")
	require.Error(t, errSteer)
	require.ErrorIs(t, errSteer, errSteerMessageEmpty)
}

func TestStopSessionServiceNotRunning(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	result := server.stopSessionService(wsDir)
	assert.False(t, result.Running)
}

func TestRespondServiceInvalidBody(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")
	_, err := server.respondService(wsDir, "test response", "", nil)
	require.Error(t, err)
}
