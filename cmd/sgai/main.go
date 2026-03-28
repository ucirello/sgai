// Package main implements the sgai CLI and server workflows.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sandgardenhq/sgai/pkg/state"
	"sigs.k8s.io/yaml"
)

const workGateApprovalText = "DEFINITION IS COMPLETE, BUILD MAY BEGIN"

const maxConsecutiveWorkingIterations = 10

var modelVariantPattern = regexp.MustCompile(`^(.+?)\s*\(([^)]+)\)$`)

func parseModelAndVariant(modelSpec string) (model, variant string) {
	matches := modelVariantPattern.FindStringSubmatch(modelSpec)
	if len(matches) == 3 {
		return matches[1], matches[2]
	}
	return modelSpec, ""
}

func main() {
	runtime.LockOSThread()
	subcommand := ""
	if len(os.Args) >= 2 {
		subcommand = os.Args[1]
	}

	if requiresOpencode(subcommand) {
		if _, err := exec.LookPath("opencode"); err != nil {
			log.Println("opencode is required but not found in PATH")
			os.Exit(1)
		}
	}

	switch subcommand {
	case "help", "-h", "--help":
		printUsage()
		return
	case "serve":
		cmdServe(os.Args[2:])
		return
	case "internal-mcp":
		if errRun := runInternalMCP(context.Background(), os.Args[2:], os.Stdin, os.Stdout); errRun != nil {
			log.Println(errRun)
			os.Exit(1)
		}
		return
	default:
		cmdServe(os.Args[1:])
		return
	}
}

func requiresOpencode(subcommand string) bool {
	switch subcommand {
	case "help", "-h", "--help", "internal-mcp":
		return false
	default:
		return true
	}
}

func printUsage() {
	fmt.Println(`sgai - AI-powered software factory

Usage:
  sgai [--listen-addr addr]    Start web server (default)

Options:
  --listen-addr   HTTP server listen address (default: 127.0.0.1:8080)

Examples:
  sgai
      Start web UI on localhost:8080
  sgai --listen-addr 0.0.0.0:8080
      Start web UI accessible externally`)
}

// runWorkflow executes the main workflow loop for a target directory.
// It handles flow mode workflows, agent iteration, and human interaction.
// mcpURL is the HTTP URL of the MCP server for this workflow.
// logWriter, when non-nil, receives a copy of the agent output for the web UI log tab.
// sessionCoord is the coordinator already managing state for this session; pass nil when starting standalone.
func runWorkflow(ctx context.Context, dir string, mcpURL string, logWriter io.Writer, sessionCoord *state.Coordinator) {
	runner, cleanup, ok := buildWorkflowRunner(dir, mcpURL, logWriter, sessionCoord)
	if !ok {
		return
	}
	defer cleanup()
	runner.run(ctx)
}

func unlockInteractiveForRetrospective(wfState *state.Workflow, currentAgent string, coord *state.Coordinator, paddedsgai string) {
	if currentAgent != "retrospective" {
		return
	}
	if wfState.InteractionMode == state.ModeRetrospective {
		return
	}
	wfState.InteractionMode = state.ModeRetrospective
	snapshot := *wfState
	if errUpdate := coord.UpdateState(func(wf *state.Workflow) {
		*wf = snapshot
	}); errUpdate != nil {
		log.Fatalln("failed to save state for retrospective unlock:", errUpdate)
	}
	fmt.Println("["+paddedsgai+"]", "transitioning to retrospective mode")
}

func ensureImplicitAgentModel(flowDag *dag, metadata *GoalMetadata, agentName string) {
	if metadata.Models == nil {
		metadata.Models = make(map[string]any)
	}
	if _, existsInDag := flowDag.Nodes[agentName]; !existsInDag {
		return
	}
	if _, existsInModels := metadata.Models[agentName]; existsInModels {
		return
	}
	coordinatorModel, hasCoordinator := metadata.Models["coordinator"]
	if !hasCoordinator {
		return
	}
	metadata.Models[agentName] = coordinatorModel
}

func getModelsForAgent(models map[string]any, agent string) []string {
	val, exists := models[agent]
	if !exists {
		return nil
	}

	switch v := val.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}

func formatModelID(agent, modelSpec string) string {
	return agent + ":" + modelSpec
}

func extractAgentFromModelID(modelID string) string {
	agent, _, found := strings.Cut(modelID, ":")
	if found {
		return agent
	}
	return modelID
}

func allModelsDone(modelStatuses map[string]string) bool {
	if len(modelStatuses) == 0 {
		return true
	}
	for _, status := range modelStatuses {
		if status != "model-done" && status != "model-error" {
			return false
		}
	}
	return true
}

func hasMessagesForModel(messages []state.Message, modelID string) bool {
	agentName := extractAgentFromModelID(modelID)
	for _, msg := range messages {
		if msg.Read {
			continue
		}
		if msg.ToAgent == modelID || msg.ToAgent == agentName {
			return true
		}
	}
	return false
}

func hasPendingMessagesForAnyModel(messages []state.Message, models []string, agent string) bool {
	for _, modelSpec := range models {
		modelID := formatModelID(agent, modelSpec)
		if hasMessagesForModel(messages, modelID) {
			return true
		}
	}
	return false
}

func messageRecipientsForAgent(workingDir, toAgent string, metadata GoalMetadata) []string {
	if toAgent == "" || strings.Contains(toAgent, ":") {
		return []string{toAgent}
	}

	models := getModelsForAgent(metadata.Models, toAgent)
	if len(models) == 0 && workingDir != "" {
		goalPath := filepath.Join(workingDir, "GOAL.md")
		if content, errRead := os.ReadFile(goalPath); errRead == nil {
			if parsedMetadata, errParse := parseYAMLFrontmatter(content); errParse == nil {
				models = getModelsForAgent(parsedMetadata.Models, toAgent)
			}
		}
	}

	if len(models) <= 1 {
		return []string{toAgent}
	}

	recipients := make([]string, 0, len(models))
	for _, modelSpec := range models {
		recipients = append(recipients, formatModelID(toAgent, modelSpec))
	}
	return recipients
}

func syncModelStatuses(modelStatuses map[string]string, models []string, agent string) map[string]string {
	if modelStatuses == nil {
		modelStatuses = make(map[string]string)
	}

	currentModelSet := make(map[string]bool)
	for _, modelSpec := range models {
		modelID := formatModelID(agent, modelSpec)
		currentModelSet[modelID] = true
		if _, exists := modelStatuses[modelID]; !exists {
			modelStatuses[modelID] = "model-working"
		}
	}

	for modelID := range modelStatuses {
		if extractAgentFromModelID(modelID) != agent {
			continue
		}
		if !currentModelSet[modelID] {
			delete(modelStatuses, modelID)
		}
	}

	return modelStatuses
}

func cleanupModelStatuses(wfState *state.Workflow) {
	wfState.ModelStatuses = nil
	wfState.CurrentModel = ""
}

type multiModelConfig struct {
	dir              string
	goalPath         string
	agent            string
	flowDag          *dag
	statePath        string
	coord            *state.Coordinator
	retrospectiveDir string
	longestNameLen   int
	paddedsgai       string
	mcpURL           string
	logWriter        io.Writer
	stdoutLog        io.Writer
	stderrLog        io.Writer
}

func runMultiModelAgent(ctx context.Context, cfg multiModelConfig, wfState state.Workflow, metadata GoalMetadata, iterationCounter *int) state.Workflow {
	models := getModelsForAgent(metadata.Models, cfg.agent)
	if len(models) <= 1 {
		return runSingleModelIteration(ctx, cfg, wfState, metadata, iterationCounter, models)
	}

	wfState.ModelStatuses = syncModelStatuses(wfState.ModelStatuses, models, cfg.agent)
	if errUpdate := cfg.coord.UpdateState(func(wf *state.Workflow) {
		*wf = wfState
	}); errUpdate != nil {
		log.Fatalln("failed to save state before multi-model loop:", errUpdate)
	}

	for {
		if ctx.Err() != nil {
			fmt.Println("["+cfg.paddedsgai+"]", "interrupted, stopping multi-model agent...")
			return wfState
		}

		var errReloadGoalMetadata error
		metadata, errReloadGoalMetadata = tryReloadGoalMetadata(cfg.goalPath, metadata, cfg.flowDag)
		if errReloadGoalMetadata != nil {
			log.Fatalln("failed to reload GOAL.md frontmatter:", errReloadGoalMetadata)
		}
		newModels := getModelsForAgent(metadata.Models, cfg.agent)

		if len(newModels) <= 1 {
			fmt.Println("["+cfg.paddedsgai+"]", "switching to single-model mode for", cfg.agent)
			cleanupModelStatuses(&wfState)
			return runSingleModelIteration(ctx, cfg, wfState, metadata, iterationCounter, newModels)
		}

		wfState.ModelStatuses = syncModelStatuses(wfState.ModelStatuses, newModels, cfg.agent)
		models = newModels

		for _, modelSpec := range models {
			if ctx.Err() != nil {
				return wfState
			}

			modelID := formatModelID(cfg.agent, modelSpec)

			currentStatus := wfState.ModelStatuses[modelID]
			hasMessages := hasMessagesForModel(wfState.Messages, modelID)

			if currentStatus == "model-done" && hasMessages {
				wfState.ModelStatuses[modelID] = "model-working"
				currentStatus = "model-working"
				fmt.Println("["+cfg.paddedsgai+"]", "reverting", modelID, "to model-working due to pending messages")
			}

			if currentStatus == "model-done" || currentStatus == "model-error" {
				continue
			}

			wfState.CurrentModel = modelID
			if errUpdate := cfg.coord.UpdateState(func(wf *state.Workflow) {
				*wf = wfState
			}); errUpdate != nil {
				log.Fatalln("failed to save state before model iteration:", errUpdate)
			}

			fmt.Println("["+cfg.paddedsgai+"]", "running model:", modelID)
			wfState = runSingleModelIteration(ctx, cfg, wfState, metadata, iterationCounter, []string{modelSpec})

			newState := cfg.coord.State()

			switch newState.Status {
			case state.StatusAgentDone:
				wfState.ModelStatuses[modelID] = "model-done"
				wfState.Status = state.StatusWorking
				if errUpdate := cfg.coord.UpdateState(func(wf *state.Workflow) {
					*wf = wfState
				}); errUpdate != nil {
					log.Fatalln("failed to save state after model done:", errUpdate)
				}
			case state.StatusComplete:
				return newState
			}
		}

		if allModelsDone(wfState.ModelStatuses) && !hasPendingMessagesForAnyModel(wfState.Messages, models, cfg.agent) {
			fmt.Println("["+cfg.paddedsgai+"]", "multi-model consensus reached for", cfg.agent)
			cleanupModelStatuses(&wfState)
			wfState.Status = state.StatusAgentDone
			if errUpdate := cfg.coord.UpdateState(func(wf *state.Workflow) {
				*wf = wfState
			}); errUpdate != nil {
				log.Fatalln("failed to save state after consensus:", errUpdate)
			}
			return wfState
		}
	}
}

func runSingleModelIteration(ctx context.Context, cfg multiModelConfig, wfState state.Workflow, metadata GoalMetadata, iterationCounter *int, models []string) state.Workflow {
	modelSpec := ""
	if len(models) > 0 {
		modelSpec = models[0]
	}
	return runFlowAgentWithModel(ctx, cfg, wfState, metadata, iterationCounter, modelSpec)
}

func runFlowAgentWithModel(ctx context.Context, cfg multiModelConfig, wfState state.Workflow, metadata GoalMetadata, iterationCounter *int, modelSpec string) state.Workflow {
	paddedAgentName := cfg.agent + strings.Repeat(" ", max(0, cfg.longestNameLen-len(cfg.agent)))
	var capturedSessionID string
	var consecutiveWorkingIterations int
	outputCapture := newRingWriter()

	for {
		if ctx.Err() != nil {
			fmt.Println("["+cfg.paddedsgai+"]", "interrupted, stopping agent...")
			return wfState
		}

		*iterationCounter++
		prefix := buildAgentPrefix(cfg.dir, paddedAgentName, *iterationCounter)

		saveState(cfg.coord, wfState)
		copyProjectManagementToRetrospective(cfg.dir, cfg.retrospectiveDir)

		baseAgent := resolveBaseAgent(metadata.Alias, cfg.agent)
		agentArgs := buildAgentArgs(cfg.agent, baseAgent, modelSpec, capturedSessionID)
		agentMsg := buildAgentMessage(cfg, wfState, metadata)

		newState, capturedSessionID, errExec := executeAgentProcess(ctx, cfg, agentArgs, agentMsg, prefix, outputCapture, wfState)
		if errExec != nil {
			return *errExec
		}

		if cfg.retrospectiveDir != "" && capturedSessionID != "" && shouldLogAgent(cfg.dir, baseAgent) {
			exportAgentSession(cfg, capturedSessionID, *iterationCounter)
		}

		if newState.VisitCounts == nil {
			newState.VisitCounts = make(map[string]int)
		}

		switch newState.Status {
		case state.StatusComplete:
			return handleCompleteStatus(ctx, cfg, newState, wfState, metadata)

		case state.StatusAgentDone:
			saveState(cfg.coord, newState)
			fmt.Println("["+cfg.paddedsgai+"]", "agent", cfg.agent, "done:", newState.Task)
			return newState

		case state.StatusWorking:
			saveState(cfg.coord, newState)
			if agentHasUnreadOutgoingMessages(newState, cfg.agent) {
				fmt.Println("["+cfg.paddedsgai+"]", "agent", cfg.agent, "sent message(s), yielding control...")
				return newState
			}
			consecutiveWorkingIterations = handleWorkingLoop(cfg, &capturedSessionID, consecutiveWorkingIterations)
			wfState = newState
			continue

		default:
			log.Fatalln("["+cfg.paddedsgai+"]", "unexpected status:", newState.Status)
		}
	}
}

func buildAgentPrefix(dir, paddedAgentName string, iteration int) string {
	workspaceName := filepath.Base(dir)
	return fmt.Sprintf("[%s][%s:%04d]", workspaceName, paddedAgentName, iteration)
}

func saveState(coord *state.Coordinator, s state.Workflow) {
	if errUpdate := coord.UpdateState(func(wf *state.Workflow) {
		*wf = s
	}); errUpdate != nil {
		log.Fatalln("failed to save state:", errUpdate)
	}
}

func copyProjectManagementToRetrospective(dir, retrospectiveDir string) {
	if retrospectiveDir == "" {
		return
	}
	pmPath := filepath.Join(dir, ".sgai", "PROJECT_MANAGEMENT.md")
	if _, errStat := os.Stat(pmPath); errStat != nil {
		return
	}
	pmRetrospectivePath := filepath.Join(retrospectiveDir, "PROJECT_MANAGEMENT.md")
	if err := copyFileAtomic(pmPath, pmRetrospectivePath); err != nil {
		log.Fatalln("failed to copy PROJECT_MANAGEMENT.md to retrospective:", err)
	}
}

func buildAgentArgs(agent, baseAgent, modelSpec, sessionID string) []string {
	args := []string{"run", "--format=json", "--agent", baseAgent}
	if modelSpec != "" {
		model, variant := parseModelAndVariant(modelSpec)
		args = append(args, "--model", model)
		if variant != "" {
			args = append(args, "--variant", variant)
		}
	}
	if sessionID != "" {
		args = append(args, "--session", sessionID)
	}
	title := agent
	if modelSpec != "" {
		title = agent + " [" + modelSpec + "]"
	}
	args = append(args, "--title", title)
	return args
}

func buildAgentMessage(cfg multiModelConfig, wfState state.Workflow, metadata GoalMetadata) string {
	msg := buildFlowMessage(cfg.flowDag, cfg.agent, wfState.VisitCounts, cfg.dir, metadata.Alias)

	multiModelSection := buildMultiModelSection(wfState.CurrentModel, metadata.Models, cfg.agent)
	if multiModelSection != "" {
		msg += multiModelSection
	}

	pendingCount := 0
	for _, m := range wfState.Messages {
		if !m.Read && messageMatchesRecipient(m, cfg.agent, wfState.CurrentModel) {
			pendingCount++
		}
	}
	if pendingCount > 0 {
		msg = fmt.Sprintf("\nYOU HAVE %d PENDING MESSAGE(S). YOU MUST CALL `sgai_check_inbox()` TO READ THEM.\n", pendingCount) + msg
	}

	pendingTodosCount := countPendingTodos(wfState, cfg.agent)
	if pendingTodosCount > 0 {
		msg += fmt.Sprintf("\nYou have %d pending TODO items. Please complete them before marking agent-done.\n", pendingTodosCount)
	}

	if cfg.agent != "coordinator" {
		outboxPending := 0
		for _, m := range wfState.Messages {
			if !m.Read && messageMatchesSender(m, cfg.agent, wfState.CurrentModel) {
				outboxPending++
			}
		}
		if outboxPending > 0 {
			msg += "\nYou have sent messages that haven't been read yet. For the recipient agents to process them, you MUST yield control by calling sgai_update_workflow_state({status: 'agent-done'}). They cannot run while you hold control.\n"
		}
	}

	return msg
}

func opencodeEnv(overrides ...string) []string {
	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		if key == "OPENCODE_CONFIG_DIR" || strings.HasPrefix(key, "SGAI_") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, overrides...)
}

func buildAgentEnv(cfg multiModelConfig, modelSpec string) ([]string, error) {
	agentIdentity := cfg.agent
	if modelSpec != "" {
		model, variant := parseModelAndVariant(modelSpec)
		agentIdentity = cfg.agent + "|" + model + "|" + variant
	}

	executablePath, errExecutable := os.Executable()
	if errExecutable != nil {
		return nil, fmt.Errorf("find current executable: %w", errExecutable)
	}

	return opencodeEnv(
		"OPENCODE_CONFIG_DIR="+filepath.Join(cfg.dir, ".sgai"),
		"SGAI_BIN_PATH="+executablePath,
		"SGAI_MCP_URL="+cfg.mcpURL,
		"SGAI_AGENT_IDENTITY="+agentIdentity), nil
}

func executeAgentProcess(ctx context.Context, cfg multiModelConfig, agentArgs []string, agentMsg, prefix string, outputCapture *ringWriter, wfState state.Workflow) (state.Workflow, string, *state.Workflow) {
	stderrOut := buildAgentOutputWriter(os.Stderr, cfg.logWriter, cfg.stderrLog)
	stdoutOut := buildAgentOutputWriter(os.Stdout, cfg.logWriter, cfg.stdoutLog)
	stderrWriter := &prefixWriter{prefix: prefix + " ", w: stderrOut, startTime: time.Now()}
	jsonWriter := &jsonPrettyWriter{prefix: prefix + " ", w: stdoutOut, coord: cfg.coord, currentAgent: cfg.agent, startTime: time.Now()}

	cfg.coord.ResetAgentDoneWatchdog()
	agentEnv, errBuildAgentEnv := buildAgentEnv(cfg, extractModelFromArgs(agentArgs))
	if errBuildAgentEnv != nil {
		fmt.Fprintln(os.Stderr, "failed to prepare agent environment:", errBuildAgentEnv)
		if errUpdate := cfg.coord.UpdateState(func(wf *state.Workflow) {
			wf.Status = state.StatusAgentDone
		}); errUpdate != nil {
			log.Fatalln("failed to save state:", errUpdate)
		}
		fmt.Fprintln(os.Stderr, "agent", cfg.agent, "marked as agent-done due to setup failure")
		result := cfg.coord.State()
		return state.Workflow{}, "", &result
	}

	agentCtx, agentCancel := context.WithCancel(ctx)
	cfg.coord.SetAgentCancel(agentCancel)

	cmd := exec.CommandContext(agentCtx, "opencode", agentArgs...)
	cmd.Dir = cfg.dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = agentEnv
	cmd.Stdin = strings.NewReader(agentMsg)
	cmd.Stderr = io.MultiWriter(stderrWriter, outputCapture)
	cmd.Stdout = io.MultiWriter(jsonWriter, outputCapture)

	if errStart := cmd.Start(); errStart != nil {
		agentCancel()
		fmt.Fprintln(os.Stderr, "failed to start opencode:", errStart)
		if errUpdate := cfg.coord.UpdateState(func(wf *state.Workflow) {
			wf.Status = state.StatusAgentDone
		}); errUpdate != nil {
			log.Fatalln("failed to save state:", errUpdate)
		}
		fmt.Fprintln(os.Stderr, "agent", cfg.agent, "marked as agent-done due to start failure")
		result := cfg.coord.State()
		return state.Workflow{}, "", &result
	}

	processExited := make(chan struct{})
	go terminateProcessGroupOnCancel(agentCtx, cmd, processExited)

	errWait := cmd.Wait()
	close(processExited)
	cfg.coord.Stop()
	agentCancel()

	if errWait != nil {
		if ctx.Err() != nil {
			fmt.Println("["+cfg.paddedsgai+"]", "interrupted during agent execution")
			return state.Workflow{}, "", &wfState
		}
		fmt.Fprintln(os.Stderr, "\n=== RAW AGENT OUTPUT (last 1000 lines) ===")
		outputCapture.dump(os.Stderr)
		fmt.Fprintln(os.Stderr, "=== END RAW AGENT OUTPUT ===")
		if errUpdate := cfg.coord.UpdateState(func(wf *state.Workflow) {
			wf.Status = state.StatusAgentDone
		}); errUpdate != nil {
			log.Fatalln("failed to save state:", errUpdate)
		}
		fmt.Fprintln(os.Stderr, "agent", cfg.agent, "marked as agent-done due to error", errWait)
		result := cfg.coord.State()
		return state.Workflow{}, "", &result
	}

	jsonWriter.Flush()
	return cfg.coord.State(), jsonWriter.sessionID, nil
}

func extractModelFromArgs(args []string) string {
	model := ""
	variant := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--model":
			if i+1 < len(args) {
				model = args[i+1]
				i++
			}
		case "--variant":
			if i+1 < len(args) {
				variant = args[i+1]
				i++
			}
		}
	}

	if model == "" {
		return ""
	}
	if variant == "" {
		return model
	}
	return model + " (" + variant + ")"
}

func exportAgentSession(cfg multiModelConfig, sessionID string, iteration int) {
	timestamp := time.Now().Format("20060102150405")
	sessionFile := filepath.Join(cfg.retrospectiveDir, fmt.Sprintf("%04d-%s-%s.json", iteration, cfg.agent, timestamp))
	if err := exportSession(cfg.dir, sessionID, sessionFile); err != nil {
		log.Fatalln("failed to export session:", err)
	}
}

func handleCompleteStatus(ctx context.Context, cfg multiModelConfig, newState, wfState state.Workflow, metadata GoalMetadata) state.Workflow {
	if cfg.agent != "coordinator" {
		fmt.Println("["+cfg.paddedsgai+"]", "agent", cfg.agent, "set status=complete, only coordinator can complete workflow; treating as agent-done")
		newState.Status = state.StatusAgentDone
		saveState(cfg.coord, newState)
		return newState
	}

	if blocked := blockCompletionOnPendingTodos(cfg, newState, wfState); blocked != nil {
		return *blocked
	}

	if blocked := blockCompletionOnGateScript(ctx, cfg, newState, metadata); blocked != nil {
		return *blocked
	}

	if blocked := blockCompletionOnProjectCriticCouncil(cfg, newState, metadata); blocked != nil {
		return *blocked
	}

	if blocked := blockCompletionOnRetrospective(cfg, newState, metadata); blocked != nil {
		return *blocked
	}

	copyCompletionArtifactsToRetrospective(cfg)
	return newState
}

func blockCompletionOnProjectCriticCouncil(cfg multiModelConfig, newState state.Workflow, metadata GoalMetadata) *state.Workflow {
	if !retrospectiveEnabled(metadata) {
		return nil
	}
	if _, exists := cfg.flowDag.Nodes["project-critic-council"]; !exists {
		return nil
	}
	if newState.VisitCounts["project-critic-council"] > 0 {
		return nil
	}
	fmt.Println("["+cfg.paddedsgai+"]", "blocking completion: project-critic-council has not returned a verdict yet")
	newState.Status = state.StatusAgentDone
	addProjectCriticCouncilRedirectMessages(&newState, cfg.dir, cfg.agent, metadata)
	saveState(cfg.coord, newState)
	return &newState
}

func blockCompletionOnRetrospective(cfg multiModelConfig, newState state.Workflow, metadata GoalMetadata) *state.Workflow {
	if !retrospectiveEnabled(metadata) {
		return nil
	}
	if _, exists := cfg.flowDag.Nodes["retrospective"]; !exists {
		return nil
	}
	if newState.VisitCounts["retrospective"] > 0 {
		return nil
	}
	fmt.Println("["+cfg.paddedsgai+"]", "blocking completion: retrospective agent has not run yet")
	newState.Status = state.StatusAgentDone
	addRetrospectiveRedirectMessage(&newState, cfg.agent)
	saveState(cfg.coord, newState)
	return &newState
}

func addProjectCriticCouncilRedirectMessages(s *state.Workflow, workingDir, fromAgent string, metadata GoalMetadata) {
	if hasUnreadMessageForAgent(s.Messages, "project-critic-council") {
		return
	}
	recipients := messageRecipientsForAgent(workingDir, "project-critic-council", metadata)
	createdAt := time.Now().UTC().Format(time.RFC3339)
	for _, recipient := range recipients {
		s.Messages = append(s.Messages, state.Message{
			ID:        nextMessageID(s.Messages),
			FromAgent: fromAgent,
			ToAgent:   recipient,
			Body:      "All work is complete and verified. Please return your final completion verdict before retrospective begins.",
			Read:      false,
			CreatedAt: createdAt,
		})
	}
}

func addRetrospectiveRedirectMessage(s *state.Workflow, fromAgent string) {
	if hasUnreadMessageForAgent(s.Messages, "retrospective") {
		return
	}
	s.Messages = append(s.Messages, state.Message{
		ID:        nextMessageID(s.Messages),
		FromAgent: fromAgent,
		ToAgent:   "retrospective",
		Body:      "All work is complete and verified. Please run the retrospective analysis.",
		Read:      false,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
}

func hasUnreadMessageForAgent(messages []state.Message, toAgent string) bool {
	for _, msg := range messages {
		if !msg.Read && extractAgentFromModelID(msg.ToAgent) == toAgent {
			return true
		}
	}
	return false
}

func blockCompletionOnPendingTodos(cfg multiModelConfig, newState, wfState state.Workflow) *state.Workflow {
	count := 0
	for _, todo := range wfState.Todos {
		if todo.Status != "completed" && todo.Status != "cancelled" {
			count++
		}
	}
	if count == 0 {
		return nil
	}
	fmt.Println("["+cfg.paddedsgai+"]", "coordinator cannot complete workflow, there are pending TODO items")
	newState.Status = state.StatusWorking
	addEnvironmentMessage(&newState, cfg.agent, fmt.Sprintf("# Pending TODO items.\nYou have %d pending TODO items. Please complete them before marking workflow complete.\n", count))
	saveState(cfg.coord, newState)
	return &newState
}

func blockCompletionOnGateScript(ctx context.Context, cfg multiModelConfig, newState state.Workflow, metadata GoalMetadata) *state.Workflow {
	if metadata.CompletionGateScript == "" {
		return nil
	}
	fmt.Println("["+cfg.paddedsgai+"]", "running completionGateScript:", metadata.CompletionGateScript)
	newState.Task = "running completionGateScript: " + metadata.CompletionGateScript
	saveState(cfg.coord, newState)
	output, errScript := runCompletionGateScript(ctx, cfg.dir, metadata.CompletionGateScript)
	if errScript == nil {
		return nil
	}
	fmt.Println("["+cfg.paddedsgai+"]", "completionGateScript failed, blocking completion")
	newState.Status = state.StatusWorking
	addEnvironmentMessage(&newState, cfg.agent, formatCompletionGateScriptFailureMessage(metadata.CompletionGateScript, output))
	saveState(cfg.coord, newState)
	return &newState
}

func copyCompletionArtifactsToRetrospective(cfg multiModelConfig) {
	if cfg.retrospectiveDir == "" {
		return
	}
	goalRetrospectivePath := filepath.Join(cfg.retrospectiveDir, "GOAL.md")
	if err := copyFileAtomic(cfg.goalPath, goalRetrospectivePath); err != nil {
		log.Fatalln("failed to copy GOAL.md to retrospective:", err)
	}
	pmPath := filepath.Join(cfg.dir, ".sgai", "PROJECT_MANAGEMENT.md")
	if _, errStat := os.Stat(pmPath); errStat == nil {
		pmRetrospectivePath := filepath.Join(cfg.retrospectiveDir, "PROJECT_MANAGEMENT.md")
		if err := copyFileAtomic(pmPath, pmRetrospectivePath); err != nil {
			log.Fatalln("failed to copy PROJECT_MANAGEMENT.md to retrospective:", err)
		}
	}
}

func handleWorkingLoop(cfg multiModelConfig, capturedSessionID *string, consecutiveWorkingIterations int) int {
	consecutiveWorkingIterations++
	if consecutiveWorkingIterations >= maxConsecutiveWorkingIterations {
		fmt.Println("["+cfg.paddedsgai+"]", "agent", cfg.agent, "stuck in working loop after", consecutiveWorkingIterations, "iterations; discarding session to recover")
		*capturedSessionID = ""
		consecutiveWorkingIterations = 0
	}
	fmt.Println("["+cfg.paddedsgai+"]", "agent", cfg.agent, "still working, re-running...")
	return consecutiveWorkingIterations
}

func agentHasUnreadOutgoingMessages(s state.Workflow, agentName string) bool {
	for _, msg := range s.Messages {
		if !msg.Read && messageMatchesSender(msg, agentName, s.CurrentModel) {
			return true
		}
	}
	return false
}

func nextMessageID(messages []state.Message) int {
	nextID := 1
	for _, msg := range messages {
		if msg.ID >= nextID {
			nextID = msg.ID + 1
		}
	}
	return nextID
}

func addEnvironmentMessage(wfState *state.Workflow, toAgent, body string) {
	wfState.Messages = append(wfState.Messages, state.Message{
		ID:        nextMessageID(wfState.Messages),
		FromAgent: "environment",
		ToAgent:   toAgent,
		Body:      body,
		Read:      false,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
}

func addAgentHandoffProgress(wfState *state.Workflow, targetAgent string) {
	progressEntry := state.ProgressEntry{
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		Agent:       "sgai",
		Description: fmt.Sprintf("Handing off to %s", targetAgent),
	}
	wfState.Progress = append(wfState.Progress, progressEntry)
}

// markCurrentAgentInSequence updates the agent sequence to track the current agent
// with a timestamp. If the current agent is already the last entry, it just marks
// it as current; otherwise, it appends a new entry.
func markCurrentAgentInSequence(s *state.Workflow, currentAgent string) {
	now := time.Now().UTC().Format(time.RFC3339)
	lastIdx := len(s.AgentSequence) - 1
	if lastIdx >= 0 && s.AgentSequence[lastIdx].Agent == currentAgent {
		s.AgentSequence[lastIdx].IsCurrent = true
		return
	}
	for i := range s.AgentSequence {
		s.AgentSequence[i].IsCurrent = false
	}
	s.AgentSequence = append(s.AgentSequence, state.AgentSequenceEntry{
		Agent:     currentAgent,
		StartTime: now,
		IsCurrent: true,
	})
}

// countPendingTodos returns the count of non-completed, non-cancelled TODO items.
// For coordinator, it checks ProjectTodos; for other agents, it checks Todos.
func countPendingTodos(wfState state.Workflow, agent string) int {
	if agent == "coordinator" {
		return 0
	}
	count := 0
	for _, todo := range wfState.Todos {
		if todo.Status != "completed" && todo.Status != "cancelled" {
			count++
		}
	}
	return count
}

// GoalMetadata represents the YAML frontmatter in GOAL.md files.
// It configures workflow flow, per-agent models, agent aliases,
// and completion gate command. Models can be either a single string
// or an array of strings per agent (for multi-model support).
type GoalMetadata struct {
	Title                string            `json:"title,omitempty" yaml:"title,omitempty"`
	Flow                 string            `json:"flow,omitempty" yaml:"flow,omitempty"`
	Models               map[string]any    `json:"models,omitempty" yaml:"models,omitempty"`
	Alias                map[string]string `json:"alias,omitempty" yaml:"alias,omitempty"`
	CompletionGateScript string            `json:"completionGateScript,omitempty" yaml:"completionGateScript,omitempty"`
	ContinuousModePrompt string            `json:"continuousModePrompt,omitempty" yaml:"continuousModePrompt,omitempty"`
	ContinuousModeAuto   string            `json:"continuousModeAuto,omitempty" yaml:"continuousModeAuto,omitempty"`
	ContinuousModeCron   string            `json:"continuousModeCron,omitempty" yaml:"continuousModeCron,omitempty"`
	Retrospective        string            `json:"retrospective,omitempty" yaml:"retrospective,omitempty"`
}

func resolveBaseAgent(alias map[string]string, agentName string) string {
	if base, ok := alias[agentName]; ok {
		return base
	}
	return agentName
}

type agentMetadata struct {
	Log      bool     `json:"log" yaml:"log"`
	Snippets []string `json:"snippets" yaml:"snippets"`
}

func parseAgentFileMetadata(dir, agentName string) (agentMetadata, bool) {
	agentPath := filepath.Join(dir, ".sgai", "agent", agentName+".md")
	content, err := os.ReadFile(agentPath)
	if err != nil {
		return agentMetadata{}, false
	}
	yamlContent, ok := splitFrontmatter(content)
	if !ok {
		return agentMetadata{}, false
	}
	var metadata agentMetadata
	metadata.Log = true
	if err := yaml.Unmarshal(yamlContent, &metadata); err != nil {
		return agentMetadata{}, false
	}
	return metadata, true
}

func shouldLogAgent(dir, agentName string) bool {
	metadata, ok := parseAgentFileMetadata(dir, agentName)
	if !ok {
		return true
	}
	return metadata.Log
}

func parseAgentSnippets(dir, agentName string) []string {
	metadata, ok := parseAgentFileMetadata(dir, agentName)
	if !ok {
		return nil
	}
	return metadata.Snippets
}

type frontmatterSections struct {
	yamlContent []byte
	after       []byte
	lineEnding  []byte
}

func splitFrontmatterSections(content []byte) (frontmatterSections, error) {
	delimiter := []byte("---")
	if !bytes.HasPrefix(content, delimiter) {
		return frontmatterSections{}, fmt.Errorf("content has no frontmatter")
	}
	lineEnding, rest, errLineEnding := frontmatterLineEnding(content[len(delimiter):])
	if errLineEnding != nil {
		return frontmatterSections{}, errLineEnding
	}
	yamlContent, after, errSplit := splitFrontmatterBody(rest, lineEnding)
	if errSplit != nil {
		return frontmatterSections{}, errSplit
	}
	return frontmatterSections{yamlContent: yamlContent, after: after, lineEnding: lineEnding}, nil
}

func frontmatterLineEnding(content []byte) ([]byte, []byte, error) {
	if bytes.HasPrefix(content, []byte("\r\n")) {
		return []byte("\r\n"), content[2:], nil
	}
	if bytes.HasPrefix(content, []byte("\n")) {
		return []byte("\n"), content[1:], nil
	}
	return nil, nil, fmt.Errorf("frontmatter opening delimiter must end with newline")
}

func splitFrontmatterBody(content, lineEnding []byte) ([]byte, []byte, error) {
	delimiter := []byte("---")
	if bytes.HasPrefix(content, delimiter) {
		after := content[len(delimiter):]
		if len(after) > 0 && !bytes.HasPrefix(after, lineEnding) {
			return nil, nil, fmt.Errorf("frontmatter closing delimiter must be on its own line")
		}
		return nil, after, nil
	}
	scanOffset := 0
	for {
		nextLineOffset := bytes.Index(content[scanOffset:], lineEnding)
		if nextLineOffset < 0 {
			return nil, nil, fmt.Errorf("no closing '---' found for frontmatter")
		}
		nextLineOffset += scanOffset
		lineStart := nextLineOffset + len(lineEnding)
		if bytes.HasPrefix(content[lineStart:], delimiter) {
			after := content[lineStart+len(delimiter):]
			if len(after) == 0 || bytes.HasPrefix(after, lineEnding) {
				return content[:lineStart], after, nil
			}
		}
		scanOffset = lineStart
	}
}

func splitFrontmatter(content []byte) (yamlContent []byte, ok bool) {
	yamlStart, yamlEnd, _, ok, errFrontmatter := frontmatterBounds(content)
	if errFrontmatter != nil || !ok {
		return nil, false
	}
	return content[yamlStart:yamlEnd], true
}

func frontmatterBounds(content []byte) (int, int, int, bool, error) {
	line, next, ok := nextFrontmatterLine(content, 0)
	if !ok || !bytes.Equal(line, []byte("---")) {
		return 0, 0, 0, false, nil
	}

	yamlStart := next
	for lineStart := next; ; {
		line, next, ok = nextFrontmatterLine(content, lineStart)
		if !ok {
			return 0, 0, 0, false, fmt.Errorf("no closing '---' found for frontmatter")
		}
		if bytes.Equal(line, []byte("---")) {
			return yamlStart, lineStart, next, true, nil
		}
		lineStart = next
	}
}

func nextFrontmatterLine(content []byte, start int) ([]byte, int, bool) {
	if start >= len(content) {
		return nil, start, false
	}

	end := bytes.IndexByte(content[start:], '\n')
	if end == -1 {
		line := bytes.TrimSuffix(content[start:], []byte("\r"))
		return line, len(content), true
	}

	end += start
	line := bytes.TrimSuffix(content[start:end], []byte("\r"))
	return line, end + 1, true
}

func extractFrontmatterTitle(yamlContent []byte) (string, bool) {
	for start := 0; ; {
		line, next, ok := nextFrontmatterLine(yamlContent, start)
		if !ok {
			return "", false
		}
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' && bytes.HasPrefix(line, []byte("title:")) {
			var metadata struct {
				Title string `json:"title" yaml:"title"`
			}
			if errUnmarshal := yaml.Unmarshal(line, &metadata); errUnmarshal == nil {
				return strings.TrimSpace(metadata.Title), true
			}
			return "", false
		}
		start = next
	}
}

func parseFrontmatterMap(content []byte) map[string]string {
	result := make(map[string]string)
	yamlContent, ok := splitFrontmatter(content)
	if !ok {
		return result
	}
	for line := range bytes.SplitSeq(yamlContent, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if colonIdx := bytes.IndexByte(trimmed, ':'); colonIdx > 0 {
			key := string(bytes.TrimSpace(trimmed[:colonIdx]))
			value := string(bytes.TrimSpace(trimmed[colonIdx+1:]))
			result[key] = value
		}
	}
	return result
}

// validateModels checks that all agent models in the map are valid according to `opencode models`.
// Returns an error listing invalid models and agents if any are found.
// When model specs include variants (e.g., "model (variant)"), only the base model is validated.
// Supports both single string models and arrays of model strings.
func validateModels(models map[string]any) error {
	if len(models) == 0 {
		return nil
	}

	validModels, err := fetchValidModels()
	if err != nil {
		return fmt.Errorf("failed to fetch valid models: %w", err)
	}

	var invalidAgents []string
	var invalidModelNames []string
	seen := make(map[string]bool)

	for agent := range models {
		modelSpecs := getModelsForAgent(models, agent)
		for _, modelSpec := range modelSpecs {
			if modelSpec == "" {
				continue
			}
			baseModel, _ := parseModelAndVariant(modelSpec)
			if !validModels[baseModel] {
				invalidAgents = append(invalidAgents, agent)
				if !seen[baseModel] {
					invalidModelNames = append(invalidModelNames, baseModel)
					seen[baseModel] = true
				}
			}
		}
	}

	if len(invalidAgents) > 0 {
		slices.Sort(invalidAgents)
		slices.Sort(invalidModelNames)

		validModelList := slices.Sorted(maps.Keys(validModels))

		return fmt.Errorf("invalid model(s) specified:\n  agents: %s\n  invalid models: %s\n  valid models: %s",
			strings.Join(invalidAgents, ", "),
			strings.Join(invalidModelNames, ", "),
			strings.Join(validModelList, ", "))
	}

	return nil
}

func fetchValidModels() (map[string]bool, error) {
	cmd := exec.Command("opencode", "models")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("opencode models command failed: %w", err)
	}

	validModels := make(map[string]bool)
	for line := range strings.SplitSeq(string(output), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			validModels[trimmed] = true
		}
	}

	return validModels, nil
}

// tryReloadGoalMetadata attempts to reload GOAL.md frontmatter from disk.
// If the file is unavailable, it preserves current metadata.
func tryReloadGoalMetadata(goalPath string, current GoalMetadata, flowDag *dag) (GoalMetadata, error) {
	content, errRead := os.ReadFile(goalPath)
	if errRead != nil {
		if os.IsNotExist(errRead) {
			return current, nil
		}
		return current, fmt.Errorf("failed to read GOAL.md: %w", errRead)
	}

	newMetadata, errParse := parseYAMLFrontmatter(content)
	if errParse != nil {
		if strings.TrimSpace(newMetadata.Title) != "" {
			current.Title = newMetadata.Title
		}
		return current, errParse
	}

	ensureImplicitAgentModel(flowDag, &newMetadata, "retrospective")
	ensureImplicitAgentModel(flowDag, &newMetadata, "project-critic-council")

	return newMetadata, nil
}

// parseYAMLFrontmatter extracts YAML frontmatter from content delimited by "---".
// If no frontmatter is found, returns default metadata.
func parseYAMLFrontmatter(content []byte) (GoalMetadata, error) {
	sections, errSplit := splitFrontmatterSections(content)
	if errSplit != nil {
		if !bytes.HasPrefix(content, []byte("---")) {
			return GoalMetadata{}, nil
		}
		return GoalMetadata{}, errSplit
	}

	title, hasTitle := extractFrontmatterTitle(sections.yamlContent)
	metadata := GoalMetadata{}
	if hasTitle {
		metadata.Title = title
	}
	if errUnmarshal := yaml.Unmarshal(sections.yamlContent, &metadata); errUnmarshal != nil {
		return metadata, fmt.Errorf("failed to parse YAML frontmatter: %w", errUnmarshal)
	}
	if metadata.Title == "" && hasTitle {
		metadata.Title = title
	}

	return metadata, nil
}

//go:embed skel/**
var skelFS embed.FS

func findFirstPendingMessageAgent(s state.Workflow) string {
	if len(s.Messages) == 0 {
		return ""
	}
	for _, msg := range s.Messages {
		if !msg.Read {
			return extractAgentFromModelID(msg.ToAgent)
		}
	}
	return ""
}

func redirectToPendingMessageAgent(s *state.Workflow, coord *state.Coordinator, paddedsgai string) bool {
	pendingAgent := findFirstPendingMessageAgent(*s)
	if pendingAgent == "" {
		return false
	}
	fmt.Println("["+paddedsgai+"]", "pending messages for", pendingAgent, "- redirecting before completion")
	s.Status = state.StatusWorking
	s.CurrentAgent = pendingAgent
	s.VisitCounts[pendingAgent]++
	snapshot := *s
	if errUpdate := coord.UpdateState(func(wf *state.Workflow) {
		*wf = snapshot
	}); errUpdate != nil {
		log.Fatalln("failed to save state:", errUpdate)
	}
	return true
}

func runCompletionGateScript(ctx context.Context, dir, script string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	errStart := cmd.Start()
	if errStart != nil {
		return "", errStart
	}

	processExited := make(chan struct{})
	go terminateProcessGroupOnCancel(ctx, cmd, processExited)

	errWait := cmd.Wait()
	close(processExited)
	return buf.String(), errWait
}

func formatCompletionGateScriptFailureMessage(script, output string) string {
	return fmt.Sprintf(`From: environment
To: coordinator
Subject: computable definition of success has failed

The script %s has failed with this output:
<pre>
%s
</pre>
`, script, output)
}

// initVisitCounts initializes a visit counts map with all agents set to 0.
// This ensures send_message validation works before agents are visited.
func initVisitCounts(agents []string) map[string]int {
	counts := make(map[string]int)
	for _, agent := range agents {
		counts[agent] = 0
	}
	return counts
}

func dotSGAILinePresent(content []byte) bool {
	for line := range bytes.SplitSeq(content, []byte("\n")) {
		if bytes.Equal(bytes.TrimSpace(line), []byte("/.sgai")) {
			return true
		}
	}
	return false
}

func isExecNotFound(err error) bool {
	var errExec *exec.Error
	if errors.As(err, &errExec) {
		return errors.Is(errExec.Err, exec.ErrNotFound)
	}
	return false
}

func initializeWorkspaceDir(dir string) error {
	if err := unpackSkeleton(dir); err != nil {
		return fmt.Errorf("failed to unpack skeleton: %w", err)
	}

	if err := applyLayerFolderOverlay(dir); err != nil {
		return fmt.Errorf("failed to apply layer overlay: %w", err)
	}

	if err := initializeJJ(dir); err != nil {
		return fmt.Errorf("failed to initialize jj: %w", err)
	}

	if err := addGitExclude(dir); err != nil {
		return fmt.Errorf("failed to add git exclude: %w", err)
	}

	return nil
}

func initializeJJ(dir string) error {
	if classifyWorkspace(dir) == workspaceFork {
		return nil
	}
	cmd := exec.Command("jj", "status")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		if isExecNotFound(err) {
			return fmt.Errorf("jj is required but not found in PATH")
		}
		initCmd := exec.Command("jj", "git", "init", "--colocate")
		initCmd.Dir = dir
		if errInit := initCmd.Run(); errInit != nil {
			return fmt.Errorf("failed to run jj git init: %w", errInit)
		}
	}
	return nil
}

func extractBody(content []byte) []byte {
	sections, errSplit := splitFrontmatterSections(content)
	if errSplit != nil {
		return content
	}
	after := sections.after
	if len(after) == 0 {
		return []byte{}
	}
	if bytes.HasPrefix(after, sections.lineEnding) {
		return after[len(sections.lineEnding):]
	}
	return after
}

// generateRetrospectiveDirName generates a timestamp-based folder name in format YYYY-MM-DD-HH-II.XXXX
// where XXXX is 4 random lowercase alphanumeric characters [a-z0-9]
func generateRetrospectiveDirName() string {
	timestamp := time.Now().Format("2006-01-02-15-04")
	suffix := make([]byte, 4)
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	if _, err := rand.Read(suffix); err != nil {
		log.Fatalln("failed to generate random suffix:", err)
	}
	for i := range suffix {
		suffix[i] = chars[int(suffix[i])%len(chars)]
	}
	return timestamp + "." + string(suffix)
}

func openRetrospectiveLogs(retrospectiveDir string) (io.WriteCloser, io.WriteCloser, error) {
	stdoutLogPath := filepath.Join(retrospectiveDir, "stdout.log")
	stderrLogPath := filepath.Join(retrospectiveDir, "stderr.log")

	stdoutLog, errStdout := prepareLogFile(stdoutLogPath)
	if errStdout != nil {
		return nil, nil, fmt.Errorf("preparing stdout.log: %w", errStdout)
	}

	stderrLog, errStderr := prepareLogFile(stderrLogPath)
	if errStderr != nil {
		if errClose := stdoutLog.Close(); errClose != nil {
			log.Println("failed to close stdout log during error cleanup:", errClose)
		}
		return nil, nil, fmt.Errorf("preparing stderr.log: %w", errStderr)
	}

	return stdoutLog, stderrLog, nil
}

const gracefulShutdownTimeout = 5 * time.Second

func terminateProcessGroupOnCancel(ctx context.Context, cmd *exec.Cmd, processExited <-chan struct{}) {
	select {
	case <-ctx.Done():
	case <-processExited:
		return
	}
	pgid := -cmd.Process.Pid
	_ = syscall.Kill(pgid, syscall.SIGTERM)
	select {
	case <-time.After(gracefulShutdownTimeout):
		_ = syscall.Kill(pgid, syscall.SIGKILL)
	case <-processExited:
	}
}

func formatElapsed(start time.Time) string {
	elapsed := time.Since(start)
	hours := int(elapsed.Hours())
	mins := int(elapsed.Minutes()) % 60
	secs := int(elapsed.Seconds()) % 60
	millis := elapsed.Milliseconds() % 1000
	return fmt.Sprintf("[%02d:%02d:%02d.%03d]", hours, mins, secs, millis)
}

type prefixWriter struct {
	prefix    string
	w         io.Writer
	startTime time.Time
}

func (p *prefixWriter) Write(data []byte) (int, error) {
	lines := linesWithTrailingEmpty(string(data))
	for i, line := range lines {
		if i < len(lines)-1 || line != "" {
			timestamp := formatElapsed(p.startTime)
			if _, err := p.w.Write([]byte(timestamp + p.prefix + line + "\n")); err != nil {
				return 0, err
			}
		}
	}
	return len(data), nil
}

type streamEvent struct {
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp"`
	SessionID string `json:"sessionID"`
	Part      part   `json:"part"`
}

type part struct {
	Type   string     `json:"type"`
	Text   string     `json:"text,omitempty"`
	Tool   string     `json:"tool,omitempty"`
	State  *toolState `json:"state,omitempty"`
	Cost   float64    `json:"cost,omitempty"`
	Tokens partTokens `json:"tokens"`
}

type partTokens struct {
	Input     int        `json:"input"`
	Output    int        `json:"output"`
	Reasoning int        `json:"reasoning"`
	Cache     cacheStats `json:"cache"`
}

type cacheStats struct {
	Read  int `json:"read"`
	Write int `json:"write"`
}

type toolState struct {
	Status string         `json:"status"`
	Input  map[string]any `json:"input"`
	Title  string         `json:"title,omitempty"`
	Output string         `json:"output,omitempty"`
	Error  string         `json:"error,omitempty"`
}

type jsonPrettyWriter struct {
	prefix       string
	w            io.Writer
	buf          []byte
	currentText  strings.Builder
	sessionID    string
	coord        *state.Coordinator
	currentAgent string
	stepCounter  int
	startTime    time.Time
}

func (j *jsonPrettyWriter) tsPrefix() string {
	return formatElapsed(j.startTime) + j.prefix
}

func (j *jsonPrettyWriter) Write(data []byte) (int, error) {
	j.buf = append(j.buf, data...)
	j.processBuffer()
	return len(data), nil
}

func (j *jsonPrettyWriter) processBuffer() {
	for {
		idx := strings.Index(string(j.buf), "\n")
		if idx == -1 {
			return
		}

		line := j.buf[:idx]
		j.buf = j.buf[idx+1:]

		if len(line) == 0 {
			continue
		}

		var event streamEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}

		j.processEvent(event)
	}
}

func (j *jsonPrettyWriter) processEvent(event streamEvent) {
	if event.SessionID != "" {
		j.sessionID = event.SessionID
	}
	part := event.Part

	switch event.Type {
	case "text":
		if part.Text != "" {
			j.currentText.WriteString(part.Text)
		}

	case "tool", "tool_use":
		j.flushText()
		if part.State != nil {
			toolCall := formatToolCall(part.Tool, part.State.Input)
			switch part.State.Status {
			case "pending":
				if _, err := fmt.Fprintln(j.w, j.tsPrefix()+toolCall); err != nil {
					log.Println("write failed:", err)
				}
			case "running":
				if _, err := fmt.Fprintln(j.w, j.tsPrefix()+toolCall+" ..."); err != nil {
					log.Println("write failed:", err)
				}
			case "completed":
				if _, err := fmt.Fprintln(j.w, j.tsPrefix()+toolCall); err != nil {
					log.Println("write failed:", err)
				}
				if part.State.Output != "" {
					if isTodoTool(part.Tool) {
						j.formatTodoOutput(part.State.Output)
						j.updateAgentTodos(part.Tool, part.State.Output)
					} else {
						for line := range strings.SplitSeq(part.State.Output, "\n") {
							if _, err := fmt.Fprintln(j.w, j.tsPrefix()+"  → "+line); err != nil {
								log.Println("write failed:", err)
							}
						}
					}
				}
			case "error":
				if _, err := fmt.Fprintln(j.w, j.tsPrefix()+toolCall+" ERROR:", part.State.Error); err != nil {
					log.Println("write failed:", err)
				}
			}
		}

	case "step_start", "step-start":
		j.flushText()
		j.stepCounter++

	case "step_finish", "step-finish":
		j.flushText()
		j.recordStepCost(part, event.Timestamp)

	case "reasoning":
		j.flushText()
		if part.Text != "" {
			if _, err := fmt.Fprintln(j.w, j.tsPrefix()+"[thinking] ..."); err != nil {
				log.Println("write failed:", err)
			}
		}

	default:
		if event.Type != "" {
			if _, err := fmt.Fprintln(j.w, j.tsPrefix()+"["+event.Type+"]"); err != nil {
				log.Println("write failed:", err)
			}
		}
	}
}

func (j *jsonPrettyWriter) flushText() {
	if j.currentText.Len() > 0 {
		text := j.currentText.String()
		for line := range strings.SplitSeq(text, "\n") {
			if _, err := fmt.Fprintln(j.w, j.tsPrefix()+line); err != nil {
				log.Println("write failed:", err)
			}
		}
		j.currentText.Reset()
	}
}

func (j *jsonPrettyWriter) Flush() {
	j.processBuffer()
	j.flushText()
}

func dollarBreakdownForStep(cost float64, tokens state.TokenUsage) state.DollarBreakdown {
	breakdown := state.DollarBreakdown{Total: cost}
	totalTokens := tokens.Input + tokens.Output + tokens.Reasoning + tokens.CacheRead + tokens.CacheWrite
	if totalTokens == 0 || cost == 0 {
		return breakdown
	}

	allocate := func(count int) float64 {
		return cost * float64(count) / float64(totalTokens)
	}

	breakdown.Input = allocate(tokens.Input)
	breakdown.Output = allocate(tokens.Output)
	breakdown.Reasoning = allocate(tokens.Reasoning)
	breakdown.CacheRead = allocate(tokens.CacheRead)
	breakdown.CacheWrite = allocate(tokens.CacheWrite)
	return breakdown
}

func (j *jsonPrettyWriter) recordStepCost(p part, timestamp int64) {
	if j.coord == nil || j.currentAgent == "" {
		return
	}
	if p.Cost == 0 && p.Tokens.Input == 0 && p.Tokens.Output == 0 && p.Tokens.Reasoning == 0 && p.Tokens.Cache.Read == 0 && p.Tokens.Cache.Write == 0 {
		return
	}

	stepCost := state.StepCost{
		StepID: fmt.Sprintf("%s-step-%d", j.currentAgent, j.stepCounter),
		Agent:  j.currentAgent,
		Cost:   p.Cost,
		Dollars: dollarBreakdownForStep(p.Cost, state.TokenUsage{
			Input:      p.Tokens.Input,
			Output:     p.Tokens.Output,
			Reasoning:  p.Tokens.Reasoning,
			CacheRead:  p.Tokens.Cache.Read,
			CacheWrite: p.Tokens.Cache.Write,
		}),
		Tokens: state.TokenUsage{
			Input:      p.Tokens.Input,
			Output:     p.Tokens.Output,
			Reasoning:  p.Tokens.Reasoning,
			CacheRead:  p.Tokens.Cache.Read,
			CacheWrite: p.Tokens.Cache.Write,
		},
		Timestamp: time.Unix(0, timestamp*int64(time.Millisecond)).UTC().Format(time.RFC3339),
	}

	if errUpdate := j.coord.UpdateState(func(wf *state.Workflow) {
		wf.Cost.TotalCost += stepCost.Cost
		wf.Cost.Dollars.Add(stepCost.Dollars)
		wf.Cost.TotalTokens.Add(stepCost.Tokens)

		agentIdx := slices.IndexFunc(wf.Cost.ByAgent, func(ac state.AgentCost) bool {
			return ac.Agent == j.currentAgent
		})
		if agentIdx == -1 {
			wf.Cost.ByAgent = append(wf.Cost.ByAgent, state.AgentCost{
				Agent:   j.currentAgent,
				Cost:    stepCost.Cost,
				Dollars: stepCost.Dollars,
				Tokens:  stepCost.Tokens,
				Steps:   []state.StepCost{stepCost},
			})
		} else {
			wf.Cost.ByAgent[agentIdx].Cost += stepCost.Cost
			wf.Cost.ByAgent[agentIdx].Dollars.Add(stepCost.Dollars)
			wf.Cost.ByAgent[agentIdx].Tokens.Add(stepCost.Tokens)
			wf.Cost.ByAgent[agentIdx].Steps = append(wf.Cost.ByAgent[agentIdx].Steps, stepCost)
		}
	}); errUpdate != nil {
		log.Println("failed to save state:", errUpdate)
	}
}

func isTodoTool(tool string) bool {
	switch tool {
	case "todowrite", "todoread", "sgai_project_todowrite", "sgai_project_todoread":
		return true
	default:
		return false
	}
}

func (j *jsonPrettyWriter) formatTodoOutput(output string) {
	todos, ok := parseTodoOutput(output)
	if !ok {
		for line := range strings.SplitSeq(output, "\n") {
			if _, err := fmt.Fprintln(j.w, j.tsPrefix()+"  → "+line); err != nil {
				log.Println("write failed:", err)
			}
		}
		return
	}

	for _, t := range todos {
		symbol := todoStatusSymbol(t.Status)
		if _, err := fmt.Fprintf(j.w, "%s  → %s %s (%s)\n", j.tsPrefix(), symbol, t.Content, t.Priority); err != nil {
			log.Println("write failed:", err)
		}
	}
}

func (j *jsonPrettyWriter) updateAgentTodos(tool, output string) {
	if tool != "todowrite" || j.coord == nil || j.currentAgent == "" {
		return
	}

	todos, ok := parseTodoOutput(output)
	if !ok {
		return
	}

	if errUpdate := j.coord.UpdateState(func(wf *state.Workflow) {
		wf.Todos = slices.Clone(todos)
	}); errUpdate != nil {
		log.Println("failed to save todos:", errUpdate)
	}
}

func parseTodoOutput(output string) ([]state.TodoItem, bool) {
	jsonOutput := stripMCPTodoPrefix(output)

	var todos []state.TodoItem
	if errUnmarshal := json.Unmarshal([]byte(jsonOutput), &todos); errUnmarshal != nil {
		return nil, false
	}

	return todos, true
}

func stripMCPTodoPrefix(output string) string {
	idx := strings.Index(output, "\n[")
	if idx == -1 {
		return output
	}
	prefix := strings.TrimSpace(output[:idx])
	if strings.HasSuffix(prefix, "todos") || strings.HasSuffix(prefix, "todo") {
		return output[idx+1:]
	}
	return output
}

func todoStatusSymbol(status string) string {
	switch status {
	case "pending":
		return "○"
	case "in_progress":
		return "◐"
	case "completed":
		return "●"
	case "cancelled":
		return "✕"
	default:
		return "○"
	}
}

func formatToolCall(tool string, input map[string]any) string {
	if len(input) == 0 {
		return tool
	}
	escapeReplacer := strings.NewReplacer(
		"\n", "\\n",
		"\r", "\\r",
		"\t", "\\t",
	)
	var parts []string
	for k, v := range input {
		switch val := v.(type) {
		case string:
			val = escapeReplacer.Replace(val)
			if k != "filePath" && len(val) > 50 {
				val = val[:47] + "..."
			}
			parts = append(parts, k+": '"+val+"'")
		case bool:
			parts = append(parts, k+": "+strconv.FormatBool(val))
		case float64:
			parts = append(parts, k+": "+strconv.FormatFloat(val, 'f', -1, 64))
		default:
			parts = append(parts, k+": "+fmt.Sprint(val))
		}
	}
	return tool + "(" + strings.Join(parts, ", ") + ")"
}

func extractFrontmatterDescription(content string) string {
	fm := parseFrontmatterMap([]byte(content))
	return fm["description"]
}

func updateProjectManagementWithRetrospectiveDir(pmPath, retrospectiveDirRel string) error {
	const headerDelimiter = "---"
	const headerPrefix = "Retrospective Session: "

	var existingContent []byte
	existingContent, err := os.ReadFile(pmPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read PROJECT_MANAGEMENT.md: %w", err)
	}

	newHeader := fmt.Sprintf("%s\n%s%s\n%s\n", headerDelimiter, headerPrefix, retrospectiveDirRel, headerDelimiter)

	content := string(existingContent)
	lines := linesWithTrailingEmpty(content)

	if len(lines) >= 3 && strings.HasPrefix(lines[0], headerDelimiter) {
		endIdx := -1
		for i := 1; i < len(lines); i++ {
			if strings.HasPrefix(lines[i], headerDelimiter) {
				endIdx = i
				break
			}
		}

		if endIdx > 0 {
			for i := 1; i < endIdx; i++ {
				if strings.HasPrefix(lines[i], headerPrefix) {
					remainingLines := lines[endIdx+1:]
					if len(remainingLines) > 0 && remainingLines[0] == "" {
						remainingLines = remainingLines[1:]
					}
					content = strings.Join(remainingLines, "\n")
					break
				}
			}
		}
	}

	if content != "" && !strings.HasPrefix(content, "\n") {
		newHeader += "\n"
	}

	finalContent := newHeader + content

	if err := os.MkdirAll(filepath.Dir(pmPath), 0755); err != nil {
		return fmt.Errorf("failed to create .sgai directory: %w", err)
	}

	if err := os.WriteFile(pmPath, []byte(finalContent), 0644); err != nil {
		return fmt.Errorf("failed to write PROJECT_MANAGEMENT.md: %w", err)
	}

	return nil
}

// canResumeWorkflow determines if an existing workflow can be resumed
// based on the current workflow state.
func canResumeWorkflow(wfState state.Workflow) bool {
	return wfState.Status == state.StatusWorking ||
		wfState.Status == state.StatusAgentDone ||
		wfState.NeedsHumanInput()
}

// extractRetrospectiveDirFromProjectManagement parses the PROJECT_MANAGEMENT.md
// frontmatter to extract the Retrospective Session path.
func extractRetrospectiveDirFromProjectManagement(pmPath string) (string, error) {
	const headerPrefix = "Retrospective Session: "

	content, errRead := os.ReadFile(pmPath)
	if errRead != nil {
		return "", fmt.Errorf("read PROJECT_MANAGEMENT.md: %w", errRead)
	}

	lines := linesWithTrailingEmpty(string(content))
	if len(lines) == 0 {
		return "", fmt.Errorf("missing frontmatter header in PROJECT_MANAGEMENT.md")
	}

	if strings.TrimSpace(lines[0]) != "---" {
		return "", fmt.Errorf("missing frontmatter header in PROJECT_MANAGEMENT.md")
	}

	closingDelimiterIdx := slices.IndexFunc(lines[1:], func(line string) bool {
		return strings.TrimSpace(line) == "---"
	})
	if closingDelimiterIdx < 0 {
		return "", fmt.Errorf("missing closing frontmatter delimiter in PROJECT_MANAGEMENT.md")
	}

	for _, line := range lines[1 : 1+closingDelimiterIdx] {
		if after, ok := strings.CutPrefix(line, headerPrefix); ok {
			after = strings.TrimSpace(after)
			if after == "" {
				return "", fmt.Errorf("empty Retrospective Session in PROJECT_MANAGEMENT.md")
			}
			return after, nil
		}
	}

	return "", fmt.Errorf("missing Retrospective Session in PROJECT_MANAGEMENT.md")
}

func copyFileAtomic(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	tmpDst := dst + ".tmp"

	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		if err := srcFile.Close(); err != nil {
			log.Println("close failed:", err)
		}
	}()

	tmpFile, err := os.Create(tmpDst)
	if err != nil {
		return err
	}
	tmpClosed := false
	defer func() {
		if !tmpClosed {
			if errClose := tmpFile.Close(); errClose != nil {
				log.Println("close failed:", errClose)
			}
		}
		if err != nil {
			if errRemove := os.Remove(tmpDst); errRemove != nil {
				log.Println("cleanup failed:", errRemove)
			}
		}
	}()

	if _, err = io.Copy(tmpFile, srcFile); err != nil {
		return err
	}

	if err = tmpFile.Close(); err != nil {
		return err
	}
	tmpClosed = true

	if err = os.Rename(tmpDst, dst); err != nil {
		return err
	}

	return nil
}

func rejectSymlinkedWorkspacePath(workspacePath, dstPath string) error {
	return rejectSymlinkedPath(workspacePath, dstPath, "workspace", "destination")
}

func rejectSymlinkedOverlaySourcePath(workspacePath, srcPath string) error {
	return rejectSymlinkedPath(workspacePath, srcPath, "workspace", "overlay source")
}

func copyFinalStateToRetrospective(dir, retrospectiveDir string) error {
	statePath := filepath.Join(dir, ".sgai", "state.json")
	pmPath := filepath.Join(dir, ".sgai", "PROJECT_MANAGEMENT.md")

	if _, err := os.Stat(statePath); err == nil {
		stateDst := filepath.Join(retrospectiveDir, "state.json")
		if err := copyFileAtomic(statePath, stateDst); err != nil {
			return fmt.Errorf("failed to copy state.json: %w", err)
		}
	}

	if _, err := os.Stat(pmPath); err == nil {
		pmDst := filepath.Join(retrospectiveDir, "PROJECT_MANAGEMENT.md")
		if err := copyFileAtomic(pmPath, pmDst); err != nil {
			return fmt.Errorf("failed to copy PROJECT_MANAGEMENT.md: %w", err)
		}
	}

	return nil
}

func exportSession(dir, sessionID, outputPath string) error {
	cmd := exec.Command("opencode", "export", sessionID)
	cmd.Dir = dir
	cmd.Env = opencodeEnv("OPENCODE_CONFIG_DIR=" + filepath.Join(dir, ".sgai"))
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("opencode export failed: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(outputPath, output, 0644)
}

func formatDuration(d time.Duration) string {
	totalSeconds := int(d.Seconds())
	minutes := totalSeconds / 60
	seconds := totalSeconds % 60
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

func applyLayerFolderOverlay(dir string) error {
	layerDir := filepath.Join(dir, "sgai")
	if !isExistingDirectory(layerDir) {
		return nil
	}

	allowedSubfolders := []string{"agent", "skills", "snippets"}
	for _, subfolder := range allowedSubfolders {
		srcDir := filepath.Join(layerDir, subfolder)
		dstDir := filepath.Join(dir, ".sgai", subfolder)
		if err := copyLayerSubfolder(dir, srcDir, dstDir, subfolder); err != nil {
			return err
		}
	}

	return nil
}

func copyLayerSubfolder(workspaceDir, srcDir, dstDir, subfolder string) error {
	if errRejectSource := rejectSymlinkedOverlaySourcePath(workspaceDir, srcDir); errRejectSource != nil {
		return errRejectSource
	}
	if !isExistingDirectory(srcDir) {
		return nil
	}

	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("checking overlay source path: symlinked path is not allowed: %s", path)
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		if d.IsDir() {
			dstPath := filepath.Join(dstDir, relPath)
			if err := rejectSymlinkedWorkspacePath(workspaceDir, dstPath); err != nil {
				return err
			}
			return os.MkdirAll(dstPath, 0755)
		}

		if isProtectedFile(subfolder, relPath) {
			return nil
		}

		dstPath := filepath.Join(dstDir, relPath)
		if err := rejectSymlinkedWorkspacePath(workspaceDir, dstPath); err != nil {
			return err
		}

		return copyFileAtomic(path, dstPath)
	})
}

func isExistingDirectory(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return fi.IsDir()
}

func isProtectedFile(subfolder, relPath string) bool {
	return subfolder == "agent" && relPath == "coordinator.md"
}

func isFalsish(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "no", "false", "0", "off":
		return true
	default:
		return false
	}
}

func retrospectiveEnabled(metadata GoalMetadata) bool {
	return !isFalsish(metadata.Retrospective)
}
