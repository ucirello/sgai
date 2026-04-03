package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/ucirello/sgai/pkg/state"
)

type workflowRunner struct {
	dir              string
	goalPath         string
	coord            *state.Coordinator
	metadata         GoalMetadata
	flowDag          *dag
	wfState          state.Workflow
	retroDir         string
	paddedsgai       string
	longestNameLen   int
	mcpURL           string
	logWriter        io.Writer
	retroLogs        retroLogWriters
	iterationCounter int
	iterationMu      sync.Mutex
	previousAgent    string
	runAgentFn       func(context.Context, string) state.Workflow
}

type retroLogWriters struct {
	stdout io.WriteCloser
	stderr io.WriteCloser
}

type runResult int

const (
	resultContinue runResult = iota
	resultComplete
	resultInterrupt
)

func nextRunnableAgents(messages []state.Message) []string {
	hasUnreadMessages := false
	var recipients []string
	for _, msg := range messages {
		if msg.Read {
			continue
		}
		hasUnreadMessages = true
		recipient := extractAgentFromModelID(msg.ToAgent)
		if recipient == "coordinator" {
			return []string{"coordinator"}
		}
		if !slices.Contains(recipients, recipient) {
			recipients = append(recipients, recipient)
		}
	}
	if !hasUnreadMessages {
		return []string{"coordinator"}
	}
	return recipients
}

func hasUnreadMessages(messages []state.Message) bool {
	for _, msg := range messages {
		if !msg.Read {
			return true
		}
	}
	return false
}

func (r *workflowRunner) run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			log.Println("["+r.paddedsgai+"]", "interrupted, stopping workflow...")
			return
		}

		var errReload error
		r.metadata, errReload = tryReloadGoalMetadata(r.goalPath, &r.metadata, r.flowDag)
		if errReload != nil {
			log.Println("failed to reload GOAL.md frontmatter:", errReload)
			return
		}

		currentAgents := nextRunnableAgents(r.coord.State().Messages)
		if errPrepare := r.prepareAgents(currentAgents); errPrepare != nil {
			log.Println("failed to prepare agent batch:", errPrepare)
			return
		}
		result := r.runAgents(ctx, currentAgents)

		switch result {
		case resultInterrupt:
			return
		case resultComplete:
			return
		case resultContinue:
		}
	}
}

func (r *workflowRunner) nextIteration() int {
	r.iterationMu.Lock()
	defer r.iterationMu.Unlock()
	r.iterationCounter++
	return r.iterationCounter
}

func (r *workflowRunner) prepareAgents(currentAgents []string) error {
	displayAgent := formatCurrentAgents(currentAgents)
	r.wfState = r.coord.State()

	if r.previousAgent != "" && r.previousAgent != displayAgent {
		log.Println("["+r.paddedsgai+"]", r.previousAgent, "->", displayAgent)
		r.wfState.Todos = nil
		if errOverlay := applyLayerFolderOverlay(r.dir); errOverlay != nil {
			return fmt.Errorf("apply overlay on agent transition: %w", errOverlay)
		}
	}
	r.previousAgent = displayAgent

	if r.wfState.VisitCounts == nil {
		r.wfState.VisitCounts = map[string]int{}
	}
	r.wfState.CurrentAgent = displayAgent
	if len(currentAgents) != 1 || extractAgentFromModelID(r.wfState.CurrentModel) != currentAgents[0] {
		r.wfState.CurrentModel = ""
	}
	if len(currentAgents) == 1 && currentAgents[0] != "coordinator" {
		setVisibleAgentTodos(&r.wfState, currentAgents[0])
	} else {
		r.wfState.Todos = nil
	}
	prepareCurrentBatchState(&r.wfState, currentAgents)
	for _, currentAgent := range currentAgents {
		r.wfState.VisitCounts[currentAgent]++
		addAgentHandoffProgress(&r.wfState, currentAgent)
	}
	markCurrentAgentsInSequence(&r.wfState, currentAgents)

	if errReplace := r.coord.ReplaceState(&r.wfState); errReplace != nil {
		return fmt.Errorf("save state: %w", errReplace)
	}

	return nil
}

func (r *workflowRunner) runAgents(ctx context.Context, currentAgents []string) runResult {
	if len(currentAgents) == 1 {
		r.wfState = r.executeCurrentAgent(ctx, currentAgents[0])
		return r.finishCurrentBatch(ctx)
	}

	var wg sync.WaitGroup
	for _, currentAgent := range currentAgents {
		wg.Go(func() {
			r.executeCurrentAgent(ctx, currentAgent)
		})
	}
	wg.Wait()
	r.wfState = r.coord.State()
	return r.finishCurrentBatch(ctx)
}

func (r *workflowRunner) executeCurrentAgent(ctx context.Context, currentAgent string) state.Workflow {
	if r.runAgentFn != nil {
		return r.runAgentFn(ctx, currentAgent)
	}
	return r.executeAgent(ctx, currentAgent)
}

func (r *workflowRunner) finishCurrentBatch(ctx context.Context) runResult {
	if ctx.Err() != nil {
		return resultInterrupt
	}
	r.wfState = r.coord.State()
	if r.wfState.Status != state.StatusComplete {
		return resultContinue
	}
	if hasUnreadMessages(r.wfState.Messages) {
		if errUpdate := r.coord.UpdateState(func(wf *state.Workflow) {
			wf.Status = state.StatusWorking
		}); errUpdate != nil {
			log.Println("failed to continue workflow after complete with unread messages:", errUpdate)
			return resultInterrupt
		}
		r.wfState = r.coord.State()
		return resultContinue
	}
	log.Println("["+r.paddedsgai+"]", "complete:", r.wfState.Task)
	return resultComplete
}

func (r *workflowRunner) executeAgent(ctx context.Context, currentAgent string) state.Workflow {
	metadata := r.metadata
	cfg := multiModelConfig{
		dir:              r.dir,
		goalPath:         r.goalPath,
		agent:            currentAgent,
		flowDag:          r.flowDag,
		statePath:        filepath.Join(r.dir, ".sgai", "state.json"),
		coord:            r.coord,
		retrospectiveDir: r.retroDir,
		longestNameLen:   r.longestNameLen,
		paddedsgai:       r.paddedsgai,
		mcpURL:           r.mcpURL,
		logWriter:        r.logWriter,
		stdoutLog:        r.retroLogs.stdout,
		stderrLog:        r.retroLogs.stderr,
		nextIteration:    r.nextIteration,
	}
	currentState := r.coord.State()
	if errUnlock := unlockInteractiveForRetrospective(&currentState, currentAgent, r.coord, r.paddedsgai); errUnlock != nil {
		return failWorkflowState(&cfg, &currentState, "failed to unlock retrospective interaction mode: %v", errUnlock)
	}
	return runMultiModelAgent(ctx, &cfg, &currentState, &metadata)
}

func (r *workflowRunner) runContinuous(ctx context.Context, continuousPrompt string) {
	goalPath := filepath.Join(r.dir, "GOAL.md")
	stateJSONPath := filepath.Join(r.dir, ".sgai", "state.json")

	for {
		if ctx.Err() != nil {
			return
		}

		runWorkflow(ctx, r.dir, r.mcpURL, r.logWriter, r.coord)

		freshCoord, errCoord := state.NewCoordinator(stateJSONPath)
		if errCoord != nil {
			freshCoord = state.NewCoordinatorEmpty(stateJSONPath)
		}
		r.coord = freshCoord

		if ctx.Err() != nil {
			return
		}

		runContinuousModePrompt(ctx, r.dir, continuousPrompt, r.mcpURL, r.coord)

		if ctx.Err() != nil {
			return
		}

		autoDuration, cronExpr := readContinuousModeAutoCron(r.dir)

		trigger := watchForTrigger(ctx, r.dir, r.coord, autoDuration, cronExpr)
		if trigger == triggerNone {
			return
		}

		reloadedCoord, errFresh := state.NewCoordinator(stateJSONPath)
		if errFresh == nil {
			r.coord = reloadedCoord
		}

		r.handleTrigger(trigger, goalPath)
		resetWorkflowForNextCycle(r.coord)
	}
}

func (r *workflowRunner) handleTrigger(trigger triggerKind, goalPath string) {
	if trigger != triggerSteering {
		return
	}
	wfState := r.coord.State()
	found, msg := hasHumanPartnerMessage(wfState.Messages)
	if !found || msg == nil {
		return
	}
	if errPrepend := prependSteeringMessage(goalPath, msg.Body); errPrepend != nil {
		log.Println("failed to prepend steering message:", errPrepend)
	}
	markMessageAsRead(r.coord, msg.ID)
}

func buildWorkflowRunner(dir, mcpURL string, logWriter io.Writer, sessionCoord *state.Coordinator) (*workflowRunner, func(), error) {
	goalPath := filepath.Join(dir, "GOAL.md")
	goalContent, errRead := os.ReadFile(goalPath)
	if errRead != nil {
		if os.IsNotExist(errRead) {
			return nil, nil, fmt.Errorf("GOAL.md not found in %s", dir)
		}
		return nil, nil, fmt.Errorf("reading GOAL.md: %w", errRead)
	}

	metadata, errParse := parseYAMLFrontmatter(goalContent)
	if errParse != nil {
		return nil, nil, fmt.Errorf("parse GOAL.md frontmatter: %w", errParse)
	}

	projectConfig, errConfig := loadProjectConfig(dir)
	if errConfig != nil {
		return nil, nil, fmt.Errorf("load sgai.json: %w", errConfig)
	}

	if errValidate := validateProjectConfig(projectConfig); errValidate != nil {
		return nil, nil, errValidate
	}

	applyConfigDefaults(projectConfig, &metadata)

	if errInit := initializeWorkspaceDir(dir); errInit != nil {
		return nil, nil, fmt.Errorf("initialize workspace directory: %w", errInit)
	}

	if errMCP := applyCustomMCPs(dir, projectConfig); errMCP != nil {
		return nil, nil, fmt.Errorf("apply custom MCPs: %w", errMCP)
	}

	flowDag, errFlow := parseFlow(metadata.Flow, dir)
	if errFlow != nil {
		return nil, nil, fmt.Errorf("parse flow: %w", errFlow)
	}

	if retrospectiveEnabled(metadata.Retrospective) {
		flowDag.injectRetrospectiveEdge()
	}

	ensureImplicitAgentModel(flowDag, &metadata, "project-critic-council")
	ensureImplicitAgentModel(flowDag, &metadata, "retrospective")

	if errModels := validateModels(metadata.Models); errModels != nil {
		return nil, nil, errModels
	}

	stateJSONPath := filepath.Join(dir, ".sgai", "state.json")
	coord := sessionCoord
	if coord == nil {
		var errCoord error
		coord, errCoord = state.NewCoordinator(stateJSONPath)
		if errCoord != nil && !os.IsNotExist(errCoord) {
			return nil, nil, fmt.Errorf("read state.json: %w", errCoord)
		}
		if errCoord != nil {
			coord = state.NewCoordinatorEmpty(stateJSONPath)
		}
	}

	wfState := coord.State()

	dagAgents := flowDag.allAgents()
	allAgents := buildAllAgents(dagAgents)

	workspaceName := filepath.Base(dir)
	longestNameLen := computeLongestNameLen(allAgents)
	paddedsgai := workspaceName + "][" + "sgai" + strings.Repeat(" ", max(0, longestNameLen-len("sgai")))

	pmPath := filepath.Join(dir, ".sgai", "PROJECT_MANAGEMENT.md")
	retrospectivesBaseDir := filepath.Join(dir, ".sgai", "retrospectives")

	resuming := canResumeWorkflow(&wfState)

	retroDir, resuming := prepareRetrospectiveDir(resuming, dir, retrospectivesBaseDir, pmPath, stateJSONPath, goalPath)
	if retroDir == "" {
		return nil, func() {}, errors.New("prepare retrospective directory")
	}

	retroStdoutLog, retroStderrLog, errRetroLogs := openRetrospectiveLogs(retroDir)
	if errRetroLogs != nil {
		return nil, nil, fmt.Errorf("open retrospective logs: %w", errRetroLogs)
	}

	cleanup := func() {
		if errClose := retroStdoutLog.Close(); errClose != nil {
			log.Println("failed to close stdout log:", errClose)
		}
		if errClose := retroStderrLog.Close(); errClose != nil {
			log.Println("failed to close stderr log:", errClose)
		}
		if retroDir != "" {
			if errCopy := copyFinalStateToRetrospective(dir, retroDir); errCopy != nil {
				log.Println("[sgai] warning: failed to copy final state:", errCopy)
			}
		}
	}

	if !resuming {
		preservedMode := wfState.InteractionMode
		freshState := freshWorkflowState(allAgents, preservedMode)
		if errUpdate := coord.UpdateState(func(wf *state.Workflow) {
			*wf = freshState
		}); errUpdate != nil {
			log.Println("failed to initialize state.json:", errUpdate)
			cleanup()
			return nil, func() {}, fmt.Errorf("initialize state.json: %w", errUpdate)
		}
		wfState = coord.State()
	}

	retroLogs := retroLogWriters{stdout: retroStdoutLog, stderr: retroStderrLog}
	runner := &workflowRunner{
		dir:              dir,
		goalPath:         goalPath,
		coord:            coord,
		metadata:         metadata,
		flowDag:          flowDag,
		wfState:          wfState,
		retroDir:         retroDir,
		paddedsgai:       paddedsgai,
		longestNameLen:   longestNameLen,
		mcpURL:           mcpURL,
		logWriter:        logWriter,
		retroLogs:        retroLogs,
		iterationCounter: 0,
		iterationMu:      sync.Mutex{},
		previousAgent:    "",
		runAgentFn:       nil,
	}
	return runner, cleanup, nil
}

func freshWorkflowState(allAgents []string, preservedMode string) state.Workflow {
	wf := state.NewWorkflow()
	wf.Status = state.StatusWorking
	wf.VisitCounts = initVisitCounts(allAgents)
	wf.InteractionMode = preservedMode
	return wf
}

func buildAllAgents(dagAgents []string) []string {
	if slices.Contains(dagAgents, "coordinator") {
		return dagAgents
	}
	return append([]string{"coordinator"}, dagAgents...)
}

func computeLongestNameLen(agents []string) int {
	longest := len("sgai")
	for _, agent := range agents {
		longest = max(longest, len(agent))
	}
	return longest
}

func prepareRetrospectiveDir(resuming bool, dir, retrospectivesBaseDir, pmPath, stateJSONPath, goalPath string) (string, bool) {
	retroDir, errResolve := resolveRetrospectiveDir(resuming, dir, retrospectivesBaseDir, pmPath, stateJSONPath, goalPath)
	if errResolve == nil {
		return retroDir, resuming
	}
	if !resuming {
		log.Println("failed to create fresh retrospective directory:", errResolve)
		return "", false
	}

	log.Println("[sgai] warning:", errResolve)

	retroDir, errResolve = resolveRetrospectiveDir(false, dir, retrospectivesBaseDir, pmPath, stateJSONPath, goalPath)
	if errResolve != nil {
		log.Println("failed to create fresh retrospective directory:", errResolve)
		return "", false
	}

	return retroDir, false
}

func resolveRetrospectiveDir(resuming bool, dir, retrospectivesBaseDir, pmPath, stateJSONPath, goalPath string) (string, error) {
	if resuming {
		retroDirRel, errExtract := extractRetrospectiveDirFromProjectManagement(pmPath)
		if errExtract != nil {
			return "", fmt.Errorf("failed to read retrospective directory from PROJECT_MANAGEMENT.md during resume: %w", errExtract)
		}
		retroDir := filepath.Join(dir, retroDirRel)
		fi, errStat := os.Stat(retroDir)
		if errStat != nil {
			if os.IsNotExist(errStat) {
				return "", fmt.Errorf("retrospective directory from PROJECT_MANAGEMENT.md does not exist: %s", retroDir)
			}
			return "", fmt.Errorf("failed to stat retrospective directory from PROJECT_MANAGEMENT.md: %w", errStat)
		}
		if !fi.IsDir() {
			return "", fmt.Errorf("retrospective directory from PROJECT_MANAGEMENT.md is not a directory: %s", retroDir)
		}
		return retroDir, nil
	}

	retroName, errName := generateRetrospectiveDirName()
	if errName != nil {
		return "", fmt.Errorf("generate retrospective directory name: %w", errName)
	}
	retroDir := filepath.Join(retrospectivesBaseDir, retroName)
	if errMkdir := os.MkdirAll(retroDir, 0o755); errMkdir != nil {
		return "", fmt.Errorf("failed to create retrospective directory: %w", errMkdir)
	}

	retroDirRel, errRel := filepath.Rel(dir, retroDir)
	if errRel != nil {
		return "", fmt.Errorf("failed to compute relative retrospective directory path: %w", errRel)
	}

	if errRemove := os.Remove(stateJSONPath); errRemove != nil && !os.IsNotExist(errRemove) {
		return "", fmt.Errorf("failed to truncate state.json on startup: %w", errRemove)
	}
	if errRemove := os.Remove(pmPath); errRemove != nil && !os.IsNotExist(errRemove) {
		return "", fmt.Errorf("failed to truncate PROJECT_MANAGEMENT.md on startup: %w", errRemove)
	}

	if errUpdate := updateProjectManagementWithRetrospectiveDir(pmPath, retroDirRel); errUpdate != nil {
		return "", fmt.Errorf("failed to update PROJECT_MANAGEMENT.md with retrospective directory: %w", errUpdate)
	}

	goalRetrospectivePath := filepath.Join(retroDir, "GOAL.md")
	if errCopy := copyFileAtomic(goalPath, goalRetrospectivePath); errCopy != nil {
		return "", fmt.Errorf("failed to copy GOAL.md to retrospective: %w", errCopy)
	}

	return retroDir, nil
}
