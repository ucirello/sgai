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

	"github.com/sandgardenhq/sgai/pkg/state"
)

var errRootWorkspaceCannotStart = errors.New("root workspace cannot start agentic work")

type startSessionResult2 struct {
	Name           string
	Status         string
	Running        bool
	Message        string
	AlreadyRunning bool
}

func (s *Server) startSessionService(workspacePath string, auto bool) (startSessionResult2, error) {
	if s.classifyWorkspaceCached(workspacePath) == workspaceRoot {
		return startSessionResult2{}, errRootWorkspaceCannotStart
	}

	name := filepath.Base(workspacePath)

	if s.sessionRunning(workspacePath) {
		return startSessionResult2{
			Name:           name,
			Status:         "running",
			Running:        true,
			Message:        "session already running",
			AlreadyRunning: true,
		}, nil
	}

	if errValidateStart := validateStartSessionWorkspace(workspacePath); errValidateStart != nil {
		return startSessionResult2{}, errValidateStart
	}

	coord := s.workspaceCoordinator(workspacePath)
	continuousPrompt := readContinuousModePrompt(workspacePath)

	var interactionMode string
	switch {
	case continuousPrompt != "":
		interactionMode = state.ModeContinuous
	case auto:
		interactionMode = state.ModeSelfDrive
	default:
		interactionMode = state.ModeBrainstorming
	}

	if errUpdate := coord.UpdateState(func(wf *state.Workflow) {
		wf.InteractionMode = interactionMode
	}); errUpdate != nil {
		return startSessionResult2{}, fmt.Errorf("failed to save workflow state: %w", errUpdate)
	}

	result := s.startSession(workspacePath)

	if result.alreadyRunning {
		return startSessionResult2{
			Name:           name,
			Status:         "running",
			Running:        true,
			Message:        "session already running",
			AlreadyRunning: true,
		}, nil
	}

	if result.startError != nil {
		return startSessionResult2{}, result.startError
	}

	s.notifyStateChange()

	return startSessionResult2{
		Name:    name,
		Status:  "running",
		Running: true,
		Message: "session started",
	}, nil
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
		return errRead
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

	if retrospectiveEnabled(metadata) {
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

	s.notifyStateChange()

	return stopSessionResult{
		Name:    filepath.Base(workspacePath),
		Status:  "stopped",
		Running: false,
		Message: message,
	}
}

type respondResult struct {
	Success bool
	Message string
}

func (s *Server) respondService(workspacePath, promptToken string, answer string, selectedChoices []string) (respondResult, error) {
	req := apiRespondRequest{
		PromptToken:     promptToken,
		Answer:          answer,
		SelectedChoices: selectedChoices,
	}

	wsName := filepath.Base(workspacePath)
	coord := s.sessionCoordinator(workspacePath)
	if coord != nil {
		log.Println("respond-service:", wsName, "delivering via session coordinator")
		return s.respondViaCoordinatorService(coord, req)
	}

	log.Println("respond-service:", wsName, "rejected, no session coordinator found")
	return respondResult{}, fmt.Errorf("no pending question")
}

func (s *Server) respondViaCoordinatorService(coord *state.Coordinator, req apiRespondRequest) (respondResult, error) {
	wfState := coord.State()

	if !wfState.NeedsHumanInput() {
		log.Println("respond-service: coordinator path rejected, no pending question, status:", wfState.Status)
		return respondResult{}, fmt.Errorf("no pending question")
	}

	responseText := buildAPIResponseText(req)
	if responseText == "" {
		return respondResult{}, fmt.Errorf("response cannot be empty")
	}

	if !coord.RespondIfCurrent(req.PromptToken, responseText) {
		return respondResult{}, fmt.Errorf("question not available")
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
	s.notifyStateChange()

	return respondResult{Success: true, Message: "response submitted"}, nil
}

type steerResult struct {
	Success bool
	Message string
}

func (s *Server) steerService(workspacePath, message string) (steerResult, error) {
	if strings.TrimSpace(message) == "" {
		return steerResult{}, fmt.Errorf("message cannot be empty")
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
			CreatedAt: steerCreatedAt,
		}
		insertIdx := findSteerInsertPosition(wf.Messages)
		wf.Messages = slices.Insert(wf.Messages, insertIdx, newMsg)
	}); errUpdate != nil {
		return steerResult{}, fmt.Errorf("failed to save state: %w", errUpdate)
	}

	s.notifyStateChange()

	return steerResult{Success: true, Message: "steering instruction added"}, nil
}
