package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ucirello/sgai/pkg/state"
)

var (
	errRootWorkspaceCannotStart                 = errors.New("root workspace cannot start agentic work")
	errSessionResetWhileRunning                 = errors.New("cannot reset while session is running")
	errInteractiveStartRequiresContinuousConfig = errors.New("interactive start requires continuous configuration")
	errContinuousModeNotConfigured              = errors.New("continuous mode is not configured")
	errNoPendingQuestion                        = errors.New("no pending question")
	errPromptTokenRequired                      = errors.New("prompt token is required")
	errResponseCannotBeEmpty                    = errors.New("response cannot be empty")
	errQuestionNotAvailable                     = errors.New("question not available")
	errSteerMessageEmpty                        = errors.New("message cannot be empty")
)

type sessionStartMode string

const (
	sessionStartModeSelfDrive   sessionStartMode = "self-drive"
	sessionStartModeInteractive sessionStartMode = "interactive"
	sessionStartModeContinuous  sessionStartMode = "continuous"
)

type sessionStartResult struct {
	Name           string
	Status         string
	Running        bool
	Message        string
	AlreadyRunning bool
	RunningMode    string
}

func (s *Server) startSessionService(workspacePath string, auto bool) (sessionStartResult, error) {
	requestedMode := requestedSessionStartMode(auto, readContinuousModePrompt(workspacePath) != "")
	return s.startSessionInModeService(workspacePath, requestedMode)
}

func requestedSessionStartMode(auto, continuousConfigured bool) sessionStartMode {
	switch {
	case continuousConfigured:
		return sessionStartModeContinuous
	case auto:
		return sessionStartModeSelfDrive
	default:
		return sessionStartModeInteractive
	}
}

func (s *Server) startSessionInModeService(workspacePath string, requestedMode sessionStartMode) (sessionStartResult, error) {
	if s.classifyWorkspaceCached(workspacePath) == workspaceRoot {
		return sessionStartResult{}, errRootWorkspaceCannotStart
	}

	name := filepath.Base(workspacePath)

	if runningMode, okRunningMode := s.runningSessionMode(workspacePath); okRunningMode {
		return sessionStartResult{
			Name:           name,
			Status:         "running",
			Running:        true,
			Message:        "session already running",
			AlreadyRunning: true,
			RunningMode:    string(runningMode),
		}, nil
	}

	if errValidateStartMode := validateStartModeRequest(workspacePath, requestedMode); errValidateStartMode != nil {
		return sessionStartResult{}, errValidateStartMode
	}

	if errValidateStart := validateStartSessionWorkspace(workspacePath); errValidateStart != nil {
		return sessionStartResult{}, errValidateStart
	}

	coord := s.workspaceCoordinator(workspacePath)
	interactionMode := interactionModeForSessionStart(requestedMode)

	if errUpdate := coord.UpdateState(func(wf *state.Workflow) {
		wf.InteractionMode = interactionMode
	}); errUpdate != nil {
		return sessionStartResult{}, fmt.Errorf("failed to save workflow state: %w", errUpdate)
	}

	result := s.startSession(workspacePath)

	if result.alreadyRunning {
		runningMode, okRunningMode := s.runningSessionMode(workspacePath)
		if !okRunningMode {
			runningMode = requestedMode
		}
		return sessionStartResult{
			Name:           name,
			Status:         "running",
			Running:        true,
			Message:        "session already running",
			AlreadyRunning: true,
			RunningMode:    string(runningMode),
		}, nil
	}

	if result.startError != nil {
		return sessionStartResult{}, result.startError
	}

	return sessionStartResult{
		Name:           name,
		Status:         "running",
		Running:        true,
		Message:        "session started",
		AlreadyRunning: false,
		RunningMode:    string(requestedMode),
	}, nil
}

func validateStartModeRequest(workspacePath string, requestedMode sessionStartMode) error {
	continuousConfigured := readContinuousModePrompt(workspacePath) != ""
	switch requestedMode {
	case sessionStartModeSelfDrive:
		return nil
	case sessionStartModeInteractive:
		if continuousConfigured {
			return errInteractiveStartRequiresContinuousConfig
		}
	case sessionStartModeContinuous:
		if !continuousConfigured {
			return errContinuousModeNotConfigured
		}
	}
	return nil
}

func interactionModeForSessionStart(requestedMode sessionStartMode) string {
	switch requestedMode {
	case sessionStartModeInteractive:
		return state.ModeBrainstorming
	case sessionStartModeSelfDrive:
		return state.ModeSelfDrive
	case sessionStartModeContinuous:
		return state.ModeContinuous
	default:
		return state.ModeBrainstorming
	}
}

func runningModeFromInteractionMode(interactionMode string) sessionStartMode {
	switch interactionMode {
	case state.ModeSelfDrive:
		return sessionStartModeSelfDrive
	case state.ModeContinuous:
		return sessionStartModeContinuous
	default:
		return sessionStartModeInteractive
	}
}

func (s *Server) runningSessionMode(workspacePath string) (sessionStartMode, bool) {
	s.mu.Lock()
	sess := s.sessions[workspacePath]
	s.mu.Unlock()
	if sess == nil {
		return "", false
	}

	sess.mu.Lock()
	running := sess.running
	coord := sess.coord
	sess.mu.Unlock()
	if !running {
		return "", false
	}
	if coord != nil {
		return runningModeFromInteractionMode(coord.State().InteractionMode), true
	}
	return runningModeFromInteractionMode(s.loadWorkspaceState(workspacePath).InteractionMode), true
}

func (s *Server) sessionRunning(workspacePath string) bool {
	s.mu.Lock()
	sess := s.sessions[workspacePath]
	s.mu.Unlock()
	if sess == nil {
		return false
	}
	sess.mu.Lock()
	running := sess.running
	sess.mu.Unlock()
	return running
}

func validateStartSessionWorkspace(workspacePath string) error {
	goalPath := filepath.Join(workspacePath, "GOAL.md")
	goalContent, errRead := os.ReadFile(goalPath)
	if errRead != nil {
		if os.IsNotExist(errRead) {
			return fmt.Errorf("GOAL.md not found in %s", workspacePath)
		}
		return fmt.Errorf("reading GOAL.md: %w", errRead)
	}

	metadata, errParse := parseYAMLFrontmatter(goalContent)
	if errParse != nil {
		return fmt.Errorf("failed to parse GOAL.md frontmatter: %w", errParse)
	}

	projectConfig, errConfig := loadProjectConfig(workspacePath)
	if errConfig != nil {
		return fmt.Errorf("failed to load sgai.json: %w", errConfig)
	}

	if errValidate := validateProjectConfig(projectConfig); errValidate != nil {
		return errValidate
	}

	applyConfigDefaults(projectConfig, &metadata)

	if errInit := initializeWorkspaceDir(workspacePath); errInit != nil {
		return fmt.Errorf("failed to initialize workspace directory: %w", errInit)
	}

	flowDag, errFlow := parseFlow(metadata.Flow, workspacePath)
	if errFlow != nil {
		return fmt.Errorf("failed to parse flow: %w", errFlow)
	}

	if retrospectiveEnabled(metadata.Retrospective) {
		flowDag.injectRetrospectiveEdge()
	}

	ensureImplicitAgentModel(flowDag, &metadata, "project-critic-council")
	ensureImplicitAgentModel(flowDag, &metadata, "retrospective")

	return validateModels(metadata.Models)
}

type stopSessionResult struct {
	Name    string
	Status  string
	Running bool
	Message string
}

func (s *Server) stopSessionService(workspacePath string) stopSessionResult {
	s.mu.Lock()
	sess := s.sessions[workspacePath]
	s.mu.Unlock()

	var alreadyStopped bool
	if sess == nil {
		alreadyStopped = true
	} else {
		sess.mu.Lock()
		alreadyStopped = !sess.running
		sess.mu.Unlock()
	}

	s.stopSession(workspacePath)

	message := "session stopped"
	if alreadyStopped {
		message = "session already stopped"
	}

	return stopSessionResult{
		Name:    filepath.Base(workspacePath),
		Status:  "stopped",
		Running: false,
		Message: message,
	}
}

type resetSessionResult struct {
	Name    string
	Status  string
	Running bool
	Message string
}

func (s *Server) resetSessionService(workspacePath string) (resetSessionResult, error) {
	s.mu.Lock()
	sess := s.sessions[workspacePath]
	s.mu.Unlock()

	if sess != nil {
		sess.mu.Lock()
		running := sess.running
		sess.mu.Unlock()
		if running {
			return resetSessionResult{}, errSessionResetWhileRunning
		}
	}

	coord := s.workspaceCoordinator(workspacePath)
	if errUpdate := coord.UpdateState(func(wf *state.Workflow) {
		wf.Status = state.StatusComplete
		wf.Task = ""
		wf.CurrentAgent = ""
		wf.CurrentModel = ""
		wf.AgentStates = nil
	}); errUpdate != nil {
		return resetSessionResult{}, fmt.Errorf("failed to reset state: %w", errUpdate)
	}

	s.notifyWorkspaceChangeAfterCoordinatorUpdate(workspacePath, coord)

	return resetSessionResult{
		Name:    filepath.Base(workspacePath),
		Status:  state.StatusComplete,
		Running: false,
		Message: "session reset successfully",
	}, nil
}

type respondResult struct {
	Success bool
	Message string
}

func requiredPromptToken(promptToken string) (string, error) {
	promptToken = strings.TrimSpace(promptToken)
	if promptToken == "" {
		return "", errPromptTokenRequired
	}
	return promptToken, nil
}

func (s *Server) respondService(workspacePath, promptToken, answer string, selectedChoices []string) (respondResult, error) {
	req := apiRespondRequest{
		PromptToken:     promptToken,
		Answer:          answer,
		SelectedChoices: selectedChoices,
	}

	wsName := filepath.Base(workspacePath)
	coord := s.sessionCoordinator(workspacePath)
	if coord != nil {
		log.Println("respond-service:", wsName, "delivering via session coordinator")
		return s.respondViaCoordinatorService(workspacePath, coord, req)
	}

	log.Println("respond-service:", wsName, "rejected, no session coordinator found")
	return respondResult{}, errNoPendingQuestion
}

func (s *Server) respondViaCoordinatorService(workspacePath string, coord *state.Coordinator, req apiRespondRequest) (respondResult, error) {
	wfState := coord.State()

	if !wfState.NeedsHumanInput() {
		log.Println("respond-service: coordinator path rejected, no pending question, status:", wfState.Status)
		return respondResult{}, errNoPendingQuestion
	}

	responseText := buildAPIResponseText(req)
	if responseText == "" {
		return respondResult{}, errResponseCannotBeEmpty
	}

	promptToken, errPromptToken := requiredPromptToken(req.PromptToken)
	if errPromptToken != nil {
		return respondResult{}, errPromptToken
	}

	if !coord.RespondIfCurrent(promptToken, responseText) {
		return respondResult{}, errQuestionNotAvailable
	}

	if wfState.MultiChoiceQuestion != nil && wfState.MultiChoiceQuestion.IsWorkGate {
		approvedViaSelection := slices.Contains(req.SelectedChoices, workGateApprovalText)
		if approvedViaSelection {
			if errUpdate := coord.UpdateState(func(wf *state.Workflow) {
				if wf.InteractionMode == state.ModeBrainstorming {
					wf.InteractionMode = state.ModeBuilding
				}
			}); errUpdate != nil {
				return respondResult{}, fmt.Errorf("failed to save work gate approval: %w", errUpdate)
			}
		}
	}
	s.notifyWorkspaceChangeAfterCoordinatorUpdate(workspacePath, coord)

	return respondResult{Success: true, Message: "response submitted"}, nil
}

type steerResult struct {
	Success bool
	Message string
}

func (s *Server) steerService(workspacePath, message string) (steerResult, error) {
	if strings.TrimSpace(message) == "" {
		return steerResult{}, errSteerMessageEmpty
	}

	coord := s.workspaceCoordinator(workspacePath)
	steerBody := "Re-steering instruction: " + strings.TrimSpace(message)
	steerCreatedAt := time.Now().UTC().Format(time.RFC3339)

	if errUpdate := coord.UpdateState(func(wf *state.Workflow) {
		newMsg := state.Message{
			ID:        nextMessageID(wf.Messages),
			FromAgent: "Human Partner",
			ToAgent:   "coordinator",
			Body:      steerBody,
			Read:      false,
			ReadAt:    "",
			ReadBy:    "",
			CreatedAt: steerCreatedAt,
		}
		insertIdx := findSteerInsertPosition(wf.Messages)
		wf.Messages = slices.Insert(wf.Messages, insertIdx, newMsg)
	}); errUpdate != nil {
		return steerResult{}, fmt.Errorf("failed to save state: %w", errUpdate)
	}

	s.notifyWorkspaceChangeAfterCoordinatorUpdate(workspacePath, coord)

	return steerResult{Success: true, Message: "steering instruction added"}, nil
}
