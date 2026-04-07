package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ucirello/sgai/pkg/state"
)

type externalMCPContext struct {
	server  *Server
	request *http.Request
}

type externalTargetArgs struct {
	Target string `json:"target" jsonschema:"Repository or workspace label, basename, or absolute path returned by factory_info."`
}

type externalForkArgs struct {
	Target      string `json:"target" jsonschema:"Repository label, basename, or absolute path returned by factory_info."`
	GoalContent string `json:"goalContent" jsonschema:"Body-only GOAL.md content for the new fork. The backend copies the parent frontmatter and appends this body text after it."`
	Title       string `json:"title" jsonschema:"Required title for external fork creation. This overrides only the copied title field in the new fork GOAL.md frontmatter."`
}

type externalAttachArgs struct {
	Path string `json:"path" jsonschema:"Absolute on-disk path to an existing repository or workspace directory."`
}

type externalWorkspaceArgs struct {
	Workspace string `json:"workspace" jsonschema:"Workspace label, basename, or absolute path returned by factory_info."`
}

type externalAnswerPendingQuestionArgs struct {
	Workspace       string   `json:"workspace" jsonschema:"Workspace label, basename, or absolute path returned by factory_info."`
	PromptToken     string   `json:"promptToken" jsonschema:"Current prompt token returned by get_pending_question."`
	Answer          string   `json:"answer" jsonschema:"Optional free-text answer for the current pending question. Provide answer text, selected choices, or both."`
	SelectedChoices []string `json:"selectedChoices" jsonschema:"Optional selected choices for the current pending question. Provide selected choices, answer text, or both."`
}

type externalSteerNextTurnArgs struct {
	Workspace string `json:"workspace" jsonschema:"Workspace label, basename, or absolute path returned by factory_info."`
	Message   string `json:"message" jsonschema:"Free-text re-steering instruction for the next turn."`
}

type externalRepositoryInfo struct {
	Handle        string   `json:"handle"`
	DirectoryName string   `json:"directoryName"`
	Label         string   `json:"label"`
	Title         string   `json:"title,omitempty"`
	Path          string   `json:"path"`
	Mode          string   `json:"mode"`
	RootHandle    string   `json:"rootHandle,omitempty"`
	RootPath      string   `json:"rootPath,omitempty"`
	ForkCount     int      `json:"forkCount,omitempty"`
	ActiveAgents  []string `json:"activeAgents"`
}

type externalFactoryInfoResult struct {
	Hostname       string                   `json:"hostname"`
	StartDirectory string                   `json:"startDirectory"`
	Repositories   []externalRepositoryInfo `json:"repositories"`
}

type externalSessionActionResult struct {
	Workspace      externalRepositoryInfo `json:"workspace"`
	Status         string                 `json:"status"`
	Running        bool                   `json:"running"`
	Message        string                 `json:"message"`
	AlreadyRunning bool                   `json:"alreadyRunning,omitempty"`
	RunningMode    string                 `json:"runningMode,omitempty"`
}

type externalGoalEditLinkResult struct {
	Workspace externalRepositoryInfo `json:"workspace"`
	URL       string                 `json:"url"`
}

type externalWorkflowLinks struct {
	Progress string `json:"progress"`
	GoalEdit string `json:"goalEdit"`
	Respond  string `json:"respond,omitempty"`
}

type externalWorkflowLinksResult struct {
	Workspace externalRepositoryInfo `json:"workspace"`
	Links     externalWorkflowLinks  `json:"links"`
}

type externalAttachResult struct {
	Workspace externalRepositoryInfo `json:"workspace"`
	HasGoal   bool                   `json:"hasGoal"`
	Message   string                 `json:"message"`
}

type externalForkResult struct {
	Workspace externalRepositoryInfo `json:"workspace"`
	Parent    externalRepositoryInfo `json:"parent"`
	CreatedAt string                 `json:"createdAt"`
	Message   string                 `json:"message"`
}

type externalPendingQuestionListResult struct {
	Workspaces []externalRepositoryInfo `json:"workspaces"`
}

type externalPendingQuestionResult struct {
	Workspace       externalRepositoryInfo     `json:"workspace"`
	PendingQuestion apiPendingQuestionResponse `json:"pendingQuestion"`
}

type externalAnswerPendingQuestionResult struct {
	Workspace externalRepositoryInfo `json:"workspace"`
	Success   bool                   `json:"success"`
	Message   string                 `json:"message"`
}

type externalSteerNextTurnResult struct {
	Workspace externalRepositoryInfo `json:"workspace"`
	Success   bool                   `json:"success"`
	Message   string                 `json:"message"`
}

type externalWorkspaceLogResult struct {
	Workspace externalRepositoryInfo `json:"workspace"`
	Log       []apiLogEntry          `json:"log"`
}

type externalAgentTodoSection struct {
	Agent string         `json:"agent"`
	Todos []apiTodoEntry `json:"todos"`
}

type externalWorkspaceMessagesAndTodosResult struct {
	Workspace               externalRepositoryInfo     `json:"workspace"`
	Messages                []state.Message            `json:"messages"`
	ProjectTodos            []apiTodoEntry             `json:"projectTodos"`
	ActiveAgentTodoSections []externalAgentTodoSection `json:"activeAgentTodoSections"`
}

func buildExternalMCPHandler(server *Server) (http.Handler, error) {
	if _, errBuild := buildExternalMCPServer(server, nil); errBuild != nil {
		return nil, errBuild
	}
	return mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		mcpServer, errBuild := buildExternalMCPServer(server, r)
		if errBuild != nil {
			log.Println("failed to build external MCP server:", errBuild)
			return mcp.NewServer(newMCPImplementation("sgai-external"), nil)
		}
		return mcpServer
	}, nil), nil
}

func buildExternalMCPServer(server *Server, r *http.Request) (*mcp.Server, error) {
	mcpServer := mcp.NewServer(newMCPImplementation("sgai-external"), nil)
	mcpCtx := &externalMCPContext{server: server, request: r}
	if errRegister := registerExternalTools(mcpServer, mcpCtx); errRegister != nil {
		return nil, errRegister
	}
	return mcpServer, nil
}

func registerExternalTools(server *mcp.Server, mcpCtx *externalMCPContext) error {
	factoryInfoSchema, errSchema := buildToolSchema[struct{}]("factory_info")
	if errSchema != nil {
		return errSchema
	}
	startSelfDriveSchema, errSchema := buildToolSchema[externalTargetArgs]("start_self_drive")
	if errSchema != nil {
		return errSchema
	}
	startInteractiveSchema, errSchema := buildToolSchema[externalWorkspaceArgs]("start_interactive")
	if errSchema != nil {
		return errSchema
	}
	startContinuousSchema, errSchema := buildToolSchema[externalWorkspaceArgs]("start_continuous")
	if errSchema != nil {
		return errSchema
	}
	stopWorkspaceSchema, errSchema := buildToolSchema[externalTargetArgs]("stop_workspace")
	if errSchema != nil {
		return errSchema
	}
	resetWorkspaceSchema, errSchema := buildToolSchema[externalTargetArgs]("reset_workspace")
	if errSchema != nil {
		return errSchema
	}
	goalEditLinkSchema, errSchema := buildToolSchema[externalTargetArgs]("goal_edit_link")
	if errSchema != nil {
		return errSchema
	}
	forkRepositorySchema, errSchema := buildToolSchema[externalForkArgs]("fork_repository")
	if errSchema != nil {
		return errSchema
	}
	attachRepositorySchema, errSchema := buildToolSchema[externalAttachArgs]("attach_repository")
	if errSchema != nil {
		return errSchema
	}
	listPendingQuestionsSchema, errSchema := buildToolSchema[struct{}]("list_pending_questions")
	if errSchema != nil {
		return errSchema
	}
	getPendingQuestionSchema, errSchema := buildToolSchema[externalWorkspaceArgs]("get_pending_question")
	if errSchema != nil {
		return errSchema
	}
	answerPendingQuestionSchema, errSchema := buildToolSchema[externalAnswerPendingQuestionArgs]("answer_pending_question")
	if errSchema != nil {
		return errSchema
	}
	steerNextTurnSchema, errSchema := buildToolSchema[externalSteerNextTurnArgs]("steer_next_turn")
	if errSchema != nil {
		return errSchema
	}
	peekWorkspaceLogSchema, errSchema := buildToolSchema[externalWorkspaceArgs]("peek_workspace_log")
	if errSchema != nil {
		return errSchema
	}
	workflowLinksSchema, errSchema := buildToolSchema[externalWorkspaceArgs]("workflow_links")
	if errSchema != nil {
		return errSchema
	}
	workspaceMessagesAndTodosSchema, errSchema := buildToolSchema[externalWorkspaceArgs]("workspace_messages_and_todos")
	if errSchema != nil {
		return errSchema
	}

	mcp.AddTool(server, newMCPTool("factory_info", "Describe this factory, including hostname, start directory, and attached repositories with authoritative workspace labels and modes.", factoryInfoSchema), mcpCtx.factoryInfoHandler)
	mcp.AddTool(server, newMCPTool("start_self_drive", "Start an attached repository or workspace in Self-Drive Mode.", startSelfDriveSchema), mcpCtx.startSelfDriveHandler)
	mcp.AddTool(server, newMCPTool("start_interactive", "Start an attached workspace in interactive mode. Continuous-configured workspaces are rejected and re-steered to start_continuous.", startInteractiveSchema), mcpCtx.startInteractiveHandler)
	mcp.AddTool(server, newMCPTool("start_continuous", "Start an attached workspace in continuous mode. Workspaces without continuous configuration are rejected and re-steered.", startContinuousSchema), mcpCtx.startContinuousHandler)
	mcp.AddTool(server, newMCPTool("stop_workspace", "Stop an attached workspace. This is idempotent for already stopped workspaces.", stopWorkspaceSchema), mcpCtx.stopWorkspaceHandler)
	mcp.AddTool(server, newMCPTool("reset_workspace", "Reset a stopped workspace state. Running workspaces must be stopped before reset.", resetWorkspaceSchema), mcpCtx.resetWorkspaceHandler)
	mcp.AddTool(server, newMCPTool("goal_edit_link", "Return a browser URL for editing GOAL.md for the selected workspace, based on the host used for this MCP connection.", goalEditLinkSchema), mcpCtx.goalEditLinkHandler)
	mcp.AddTool(server, newMCPTool("fork_repository", "Fork a standalone repository or root repository. The new fork copies the parent GOAL frontmatter, appends the supplied body-only goal content, and requires an explicit title override.", forkRepositorySchema), mcpCtx.forkRepositoryHandler)
	mcp.AddTool(server, newMCPTool("attach_repository", "Attach an existing on-disk repository or workspace by absolute path.", attachRepositorySchema), mcpCtx.attachRepositoryHandler)
	mcp.AddTool(server, newMCPTool("list_pending_questions", "List attached workspaces that currently have live pending questions.", listPendingQuestionsSchema), mcpCtx.listPendingQuestionsHandler)
	mcp.AddTool(server, newMCPTool("get_pending_question", "Return the full pending-question payload for the selected workspace.", getPendingQuestionSchema), mcpCtx.getPendingQuestionHandler)
	mcp.AddTool(server, newMCPTool("answer_pending_question", "Answer the current pending question for a workspace using the current prompt token, free-text answer, selected choices, or both.", answerPendingQuestionSchema), mcpCtx.answerPendingQuestionHandler)
	mcp.AddTool(server, newMCPTool("steer_next_turn", "Add a Session-tab-style re-steering instruction for the selected workspace.", steerNextTurnSchema), mcpCtx.steerNextTurnHandler)
	mcp.AddTool(server, newMCPTool("peek_workspace_log", "Peek the live Log-tab-equivalent buffer for the selected workspace.", peekWorkspaceLogSchema), mcpCtx.peekWorkspaceLogHandler)
	mcp.AddTool(server, newMCPTool("workflow_links", "Return the minimal browser workflow links for the selected workspace: progress, goal-edit, and respond when the workspace currently needs input.", workflowLinksSchema), mcpCtx.workflowLinksHandler)
	mcp.AddTool(server, newMCPTool("workspace_messages_and_todos", "Export workspace messages in internal MCP order plus project TODOs and the currently active agents' TODOs.", workspaceMessagesAndTodosSchema), mcpCtx.workspaceMessagesAndTodosHandler)

	return nil
}

func (c *externalMCPContext) factoryInfoHandler(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, externalFactoryInfoResult, error) {
	hostname, errHostname := os.Hostname()
	if errHostname != nil {
		return nil, externalFactoryInfoResult{}, fmt.Errorf("resolving hostname: %w", errHostname)
	}

	repositories, errRepositories := c.repositories()
	if errRepositories != nil {
		return nil, externalFactoryInfoResult{}, errRepositories
	}

	return nil, externalFactoryInfoResult{
		Hostname:       hostname,
		StartDirectory: c.server.rootDir,
		Repositories:   repositories,
	}, nil
}

func (c *externalMCPContext) startSelfDriveHandler(_ context.Context, _ *mcp.CallToolRequest, args externalTargetArgs) (*mcp.CallToolResult, externalSessionActionResult, error) {
	workspace, workspacePath, errResolve := c.resolveTarget(args.Target)
	if errResolve != nil {
		return nil, externalSessionActionResult{}, errResolve
	}

	result, errStart := c.server.startSessionInModeService(workspacePath, sessionStartModeSelfDrive)
	if errStart != nil {
		return nil, externalSessionActionResult{}, humanizeStartSelfDriveError(&workspace, errStart)
	}

	return nil, externalStartSessionActionResult(&workspace, &result), nil
}

func (c *externalMCPContext) startInteractiveHandler(_ context.Context, _ *mcp.CallToolRequest, args externalWorkspaceArgs) (*mcp.CallToolResult, externalSessionActionResult, error) {
	workspace, workspacePath, errResolve := c.resolveTarget(args.Workspace)
	if errResolve != nil {
		return nil, externalSessionActionResult{}, errResolve
	}

	result, errStart := c.server.startSessionInModeService(workspacePath, sessionStartModeInteractive)
	if errStart != nil {
		return nil, externalSessionActionResult{}, humanizeStartInteractiveError(&workspace, errStart)
	}

	return nil, externalStartSessionActionResult(&workspace, &result), nil
}

func (c *externalMCPContext) startContinuousHandler(_ context.Context, _ *mcp.CallToolRequest, args externalWorkspaceArgs) (*mcp.CallToolResult, externalSessionActionResult, error) {
	workspace, workspacePath, errResolve := c.resolveTarget(args.Workspace)
	if errResolve != nil {
		return nil, externalSessionActionResult{}, errResolve
	}

	result, errStart := c.server.startSessionInModeService(workspacePath, sessionStartModeContinuous)
	if errStart != nil {
		return nil, externalSessionActionResult{}, humanizeStartContinuousError(&workspace, errStart)
	}

	return nil, externalStartSessionActionResult(&workspace, &result), nil
}

func (c *externalMCPContext) stopWorkspaceHandler(_ context.Context, _ *mcp.CallToolRequest, args externalTargetArgs) (*mcp.CallToolResult, externalSessionActionResult, error) {
	workspace, workspacePath, errResolve := c.resolveTarget(args.Target)
	if errResolve != nil {
		return nil, externalSessionActionResult{}, errResolve
	}

	result := c.server.stopSessionService(workspacePath)

	return nil, externalSessionActionResult{
		Workspace:      workspace,
		Status:         result.Status,
		Running:        result.Running,
		Message:        result.Message,
		AlreadyRunning: false,
		RunningMode:    "",
	}, nil
}

func (c *externalMCPContext) resetWorkspaceHandler(_ context.Context, _ *mcp.CallToolRequest, args externalTargetArgs) (*mcp.CallToolResult, externalSessionActionResult, error) {
	workspace, workspacePath, errResolve := c.resolveTarget(args.Target)
	if errResolve != nil {
		return nil, externalSessionActionResult{}, errResolve
	}

	result, errReset := c.server.resetSessionService(workspacePath)
	if errReset != nil {
		return nil, externalSessionActionResult{}, humanizeResetWorkspaceError(&workspace, errReset)
	}

	return nil, externalSessionActionResult{
		Workspace:      workspace,
		Status:         result.Status,
		Running:        result.Running,
		Message:        result.Message,
		AlreadyRunning: false,
		RunningMode:    "",
	}, nil
}

func (c *externalMCPContext) goalEditLinkHandler(_ context.Context, _ *mcp.CallToolRequest, args externalTargetArgs) (*mcp.CallToolResult, externalGoalEditLinkResult, error) {
	workspace, _, errResolve := c.resolveTarget(args.Target)
	if errResolve != nil {
		return nil, externalGoalEditLinkResult{}, errResolve
	}

	workspaceName, errWorkspaceName := c.workflowLinkWorkspaceName(&workspace, "goal edit link")
	if errWorkspaceName != nil {
		return nil, externalGoalEditLinkResult{}, errWorkspaceName
	}
	goalEditURL, errGoalEditURL := externalGoalEditURL(externalBaseURL(c.request), workspaceName)
	if errGoalEditURL != nil {
		return nil, externalGoalEditLinkResult{}, errGoalEditURL
	}

	return nil, externalGoalEditLinkResult{Workspace: workspace, URL: goalEditURL}, nil
}

func (c *externalMCPContext) workflowLinkWorkspaceName(workspace *externalRepositoryInfo, linkSet string) (string, error) {
	matches := c.server.resolveWorkspaceNameToPaths(workspace.DirectoryName)
	if len(matches) == 1 && sameWorkspacePath(matches[0], workspace.Path) {
		return workspace.DirectoryName, nil
	}
	return "", fmt.Errorf("cannot build %s for %q because workspace basename %q is ambiguous across attached repositories", linkSet, workspace.Handle, workspace.DirectoryName)
}

func (c *externalMCPContext) forkRepositoryHandler(_ context.Context, _ *mcp.CallToolRequest, args externalForkArgs) (*mcp.CallToolResult, externalForkResult, error) {
	workspace, workspacePath, errResolve := c.resolveTarget(args.Target)
	if errResolve != nil {
		return nil, externalForkResult{}, errResolve
	}

	result, errFork := c.server.forkWorkspaceServiceWithOptions(workspacePath, args.GoalContent, forkWorkspaceOptions{title: args.Title, requireTitle: true})
	if errFork != nil {
		return nil, externalForkResult{}, humanizeForkRepositoryError(&workspace, errFork)
	}

	forkWorkspace, errForkWorkspace := c.repositoryByPath(result.Dir)
	if errForkWorkspace != nil {
		return nil, externalForkResult{}, errForkWorkspace
	}

	return nil, externalForkResult{
		Workspace: forkWorkspace,
		Parent:    workspace,
		CreatedAt: result.CreatedAt,
		Message:   "repository fork created",
	}, nil
}

func (c *externalMCPContext) attachRepositoryHandler(_ context.Context, _ *mcp.CallToolRequest, args externalAttachArgs) (*mcp.CallToolResult, externalAttachResult, error) {
	result, errAttach := c.server.attachExternalWorkspaceService(args.Path)
	if errAttach != nil {
		return nil, externalAttachResult{}, humanizeAttachRepositoryError(args.Path, errAttach)
	}

	workspace, errWorkspace := c.repositoryByPath(result.Dir)
	if errWorkspace != nil {
		return nil, externalAttachResult{}, errWorkspace
	}

	return nil, externalAttachResult{
		Workspace: workspace,
		HasGoal:   result.HasGoal,
		Message:   "repository attached",
	}, nil
}

func (c *externalMCPContext) listPendingQuestionsHandler(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, externalPendingQuestionListResult, error) {
	repositories, errRepositories := c.repositories()
	if errRepositories != nil {
		return nil, externalPendingQuestionListResult{}, errRepositories
	}

	workspaces := make([]externalRepositoryInfo, 0)
	for i := range repositories {
		repository := repositories[i]
		if _, errPendingQuestion := c.livePendingQuestion(repository.Path); errPendingQuestion == nil {
			workspaces = append(workspaces, repository)
		}
	}

	return nil, externalPendingQuestionListResult{Workspaces: workspaces}, nil
}

func (c *externalMCPContext) getPendingQuestionHandler(_ context.Context, _ *mcp.CallToolRequest, args externalWorkspaceArgs) (*mcp.CallToolResult, externalPendingQuestionResult, error) {
	workspace, workspacePath, errResolve := c.resolveTarget(args.Workspace)
	if errResolve != nil {
		return nil, externalPendingQuestionResult{}, errResolve
	}

	pendingQuestion, errPendingQuestion := c.livePendingQuestion(workspacePath)
	if errPendingQuestion != nil {
		return nil, externalPendingQuestionResult{}, humanizeGetPendingQuestionError(&workspace, errPendingQuestion)
	}

	return nil, externalPendingQuestionResult{Workspace: workspace, PendingQuestion: *pendingQuestion}, nil
}

func (c *externalMCPContext) answerPendingQuestionHandler(_ context.Context, _ *mcp.CallToolRequest, args externalAnswerPendingQuestionArgs) (*mcp.CallToolResult, externalAnswerPendingQuestionResult, error) {
	workspace, workspacePath, errResolve := c.resolveTarget(args.Workspace)
	if errResolve != nil {
		return nil, externalAnswerPendingQuestionResult{}, errResolve
	}

	result, errRespond := c.server.respondService(workspacePath, args.PromptToken, args.Answer, args.SelectedChoices)
	if errRespond != nil {
		return nil, externalAnswerPendingQuestionResult{}, humanizeAnswerPendingQuestionError(&workspace, errRespond)
	}

	return nil, externalAnswerPendingQuestionResult{Workspace: workspace, Success: result.Success, Message: result.Message}, nil
}

func (c *externalMCPContext) steerNextTurnHandler(_ context.Context, _ *mcp.CallToolRequest, args externalSteerNextTurnArgs) (*mcp.CallToolResult, externalSteerNextTurnResult, error) {
	workspace, workspacePath, errResolve := c.resolveTarget(args.Workspace)
	if errResolve != nil {
		return nil, externalSteerNextTurnResult{}, errResolve
	}

	result, errSteer := c.server.steerService(workspacePath, args.Message)
	if errSteer != nil {
		return nil, externalSteerNextTurnResult{}, humanizeSteerNextTurnError(&workspace, errSteer)
	}

	return nil, externalSteerNextTurnResult{Workspace: workspace, Success: result.Success, Message: result.Message}, nil
}

func (c *externalMCPContext) workflowLinksHandler(_ context.Context, _ *mcp.CallToolRequest, args externalWorkspaceArgs) (*mcp.CallToolResult, externalWorkflowLinksResult, error) {
	workspace, workspacePath, errResolve := c.resolveTarget(args.Workspace)
	if errResolve != nil {
		return nil, externalWorkflowLinksResult{}, errResolve
	}

	links, errWorkflowLinks := c.workflowLinks(&workspace, workspacePath)
	if errWorkflowLinks != nil {
		return nil, externalWorkflowLinksResult{}, errWorkflowLinks
	}

	return nil, externalWorkflowLinksResult{Workspace: workspace, Links: links}, nil
}

func (c *externalMCPContext) peekWorkspaceLogHandler(_ context.Context, _ *mcp.CallToolRequest, args externalWorkspaceArgs) (*mcp.CallToolResult, externalWorkspaceLogResult, error) {
	workspace, workspacePath, errResolve := c.resolveTarget(args.Workspace)
	if errResolve != nil {
		return nil, externalWorkspaceLogResult{}, errResolve
	}

	return nil, externalWorkspaceLogResult{Workspace: workspace, Log: c.workspaceLogEntries(workspacePath)}, nil
}

func (c *externalMCPContext) workspaceMessagesAndTodosHandler(_ context.Context, _ *mcp.CallToolRequest, args externalWorkspaceArgs) (*mcp.CallToolResult, externalWorkspaceMessagesAndTodosResult, error) {
	workspace, workspacePath, errResolve := c.resolveTarget(args.Workspace)
	if errResolve != nil {
		return nil, externalWorkspaceMessagesAndTodosResult{}, errResolve
	}

	wfState := c.server.workflowStateSnapshot(workspacePath)
	activeAgents := c.server.activeAgentsForWorkspace(workspacePath)
	sections := make([]externalAgentTodoSection, 0, len(activeAgents))
	for _, agent := range activeAgents {
		sections = append(sections, externalAgentTodoSection{
			Agent: agent,
			Todos: convertTodosForAPI(wfState.TodosByAgent[agent]),
		})
	}

	return nil, externalWorkspaceMessagesAndTodosResult{
		Workspace:               workspace,
		Messages:                slices.Clone(wfState.Messages),
		ProjectTodos:            convertTodosForAPI(wfState.ProjectTodos),
		ActiveAgentTodoSections: sections,
	}, nil
}

func externalStartSessionActionResult(workspace *externalRepositoryInfo, result *sessionStartResult) externalSessionActionResult {
	message := result.Message
	if result.RunningMode != "" {
		if result.AlreadyRunning {
			message = "workspace already running in " + result.RunningMode + " mode"
		} else {
			message = "workspace started in " + result.RunningMode + " mode"
		}
	}
	return externalSessionActionResult{
		Workspace:      *workspace,
		Status:         result.Status,
		Running:        result.Running,
		Message:        message,
		AlreadyRunning: result.AlreadyRunning,
		RunningMode:    result.RunningMode,
	}
}

func (c *externalMCPContext) livePendingQuestion(workspacePath string) (*apiPendingQuestionResponse, error) {
	coord := c.server.sessionCoordinator(workspacePath)
	if coord == nil {
		return nil, errNoPendingQuestion
	}
	humanInput := currentHumanInputSnapshot(coord)
	if !humanInput.needsInput() {
		return nil, errNoPendingQuestion
	}
	return humanInput.pendingQuestion(coord.State().CurrentAgent), nil
}

func (c *externalMCPContext) workspaceLogEntries(workspacePath string) []apiLogEntry {
	c.server.mu.Lock()
	sess := c.server.sessions[workspacePath]
	c.server.mu.Unlock()
	if sess == nil || sess.outputLog == nil {
		return nil
	}
	entries := make([]apiLogEntry, 0)
	for _, line := range sess.outputLog.lines() {
		entries = append(entries, apiLogEntry{Prefix: line.prefix, Text: line.text})
	}
	return entries
}

func (s *Server) workflowStateSnapshot(workspacePath string) state.Workflow {
	if coord := s.sessionCoordinator(workspacePath); coord != nil {
		return coord.State()
	}
	return s.loadWorkspaceState(workspacePath)
}

func (s *Server) activeAgentsForWorkspace(workspacePath string) []string {
	if !s.sessionRunning(workspacePath) {
		return []string{}
	}
	activeAgents := splitCurrentAgents(s.workflowStateSnapshot(workspacePath).CurrentAgent)
	if activeAgents == nil {
		return []string{}
	}
	return activeAgents
}

func (c *externalMCPContext) repositories() ([]externalRepositoryInfo, error) {
	groups, errGroups := c.workspaceGroups()
	if errGroups != nil {
		return nil, errGroups
	}
	displayLabels := c.displayLabelsForGroups(groups)

	var repositories []externalRepositoryInfo
	for _, group := range groups {
		repositories = append(repositories, c.repositoryInfo(group.Root, groups, displayLabels))
		for _, fork := range group.Forks {
			repositories = append(repositories, c.repositoryInfo(fork, groups, displayLabels))
		}
	}

	return repositories, nil
}

func (c *externalMCPContext) workspaceGroups() ([]workspaceGroup, error) {
	groups, errGroups := c.server.scanWorkspaceGroups()
	if errGroups != nil {
		return nil, fmt.Errorf("scanning attached repositories: %w", errGroups)
	}
	if groups == nil {
		groups = []workspaceGroup{}
	}
	return groups, nil
}

func (c *externalMCPContext) resolveTarget(target string) (externalRepositoryInfo, string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return externalRepositoryInfo{}, "", errors.New("target is required. Call factory_info and use a returned label, basename, or absolute path")
	}

	repositories, errRepositories := c.repositories()
	if errRepositories != nil {
		return externalRepositoryInfo{}, "", errRepositories
	}

	if filepath.IsAbs(target) {
		for i := range repositories {
			repository := repositories[i]
			if sameWorkspacePath(repository.Path, target) {
				return repository, repository.Path, nil
			}
		}
		return externalRepositoryInfo{}, "", fmt.Errorf("repository %q is not attached. Call factory_info and use one of the returned label, basename, or absolute path values", target)
	}

	var matches []externalRepositoryInfo
	for i := range repositories {
		repository := repositories[i]
		if repository.Handle == target || repository.Label == target || repository.DirectoryName == target {
			matches = append(matches, repository)
		}
	}

	switch len(matches) {
	case 0:
		return externalRepositoryInfo{}, "", fmt.Errorf("repository %q not found. Call factory_info and use one of the returned label, basename, or absolute path values", target)
	case 1:
		return matches[0], matches[0].Path, nil
	default:
		return externalRepositoryInfo{}, "", fmt.Errorf("target %q matches multiple attached repositories. Use the exact label, basename, or absolute path returned by factory_info", target)
	}
}

func (c *externalMCPContext) repositoryByPath(workspacePath string) (externalRepositoryInfo, error) {
	repositories, errRepositories := c.repositories()
	if errRepositories != nil {
		return externalRepositoryInfo{}, errRepositories
	}

	for i := range repositories {
		repository := repositories[i]
		if sameWorkspacePath(repository.Path, workspacePath) {
			return repository, nil
		}
	}

	return externalRepositoryInfo{}, fmt.Errorf("attached repository %q not found after update", workspacePath)
}

func (c *externalMCPContext) repositoryInfo(workspace workspaceInfo, groups []workspaceGroup, displayLabels map[string]string) externalRepositoryInfo {
	label := repositoryDisplayLabel(workspace, displayLabels)
	title := goalTitleStateFromPath(workspace.Directory, workspace.DirName).Title
	repository := externalRepositoryInfo{
		Handle:        label,
		DirectoryName: workspace.DirName,
		Label:         label,
		Title:         title,
		Path:          workspace.Directory,
		Mode:          repositoryMode(workspace, groups),
		RootHandle:    "",
		RootPath:      "",
		ForkCount:     0,
		ActiveAgents:  c.server.activeAgentsForWorkspace(workspace.Directory),
	}

	if repository.Mode == string(workspaceRoot) {
		repository.ForkCount = forkCount(workspace.Directory, groups)
	}

	if rootWorkspace, ok := rootWorkspaceForFork(workspace.Directory, groups); ok {
		repository.RootHandle = repositoryDisplayLabel(rootWorkspace, displayLabels)
		repository.RootPath = rootWorkspace.Directory
	}

	return repository
}

type externalWorkspaceDisplayIdentity struct {
	Name          string
	Dir           string
	Title         string
	ComputedTitle string
}

func (c *externalMCPContext) displayLabelsForGroups(groups []workspaceGroup) map[string]string {
	identities := make([]externalWorkspaceDisplayIdentity, 0)
	for _, group := range groups {
		identities = append(identities, c.displayIdentity(group.Root, groups))
		for _, fork := range group.Forks {
			identities = append(identities, c.displayIdentity(fork, groups))
		}
	}
	disambiguators := externalWorkspaceNameDisambiguators(identities)
	labels := make(map[string]string, len(identities))
	for _, identity := range identities {
		labels[identity.Dir] = externalWorkspaceDisplayLabel(identity, disambiguators)
	}
	return labels
}

func (c *externalMCPContext) displayIdentity(workspace workspaceInfo, groups []workspaceGroup) externalWorkspaceDisplayIdentity {
	entry := c.server.buildWorkspaceListEntry(workspace, groups)
	return externalWorkspaceDisplayIdentity{
		Name:          entry.Name,
		Dir:           entry.Dir,
		Title:         entry.Title,
		ComputedTitle: entry.ComputedTitle,
	}
}

func externalWorkspaceDisplayLabel(identity externalWorkspaceDisplayIdentity, disambiguators map[string]string) string {
	baseLabel := externalWorkspaceBaseLabel(identity)
	disambiguator := disambiguators[identity.Dir]
	if disambiguator == "" {
		return baseLabel
	}
	return baseLabel + " · " + disambiguator
}

func externalWorkspaceBaseLabel(identity externalWorkspaceDisplayIdentity) string {
	computedTitle := strings.TrimSpace(identity.ComputedTitle)
	if computedTitle != "" {
		return computedTitle
	}
	title := strings.TrimSpace(identity.Title)
	if title != "" {
		return title
	}
	return identity.Name
}

func repositoryDisplayLabel(workspace workspaceInfo, displayLabels map[string]string) string {
	if label := displayLabels[workspace.Directory]; label != "" {
		return label
	}
	return workspace.DirName
}

func externalWorkspaceNameDisambiguators(identities []externalWorkspaceDisplayIdentity) map[string]string {
	grouped := make(map[string][]externalWorkspaceDisplayIdentity)
	for _, identity := range identities {
		grouped[identity.Name] = append(grouped[identity.Name], identity)
	}
	disambiguators := make(map[string]string)
	for _, group := range grouped {
		if len(group) < 2 {
			continue
		}
		maps.Copy(disambiguators, externalGroupDisambiguators(group))
	}
	return disambiguators
}

func externalGroupDisambiguators(identities []externalWorkspaceDisplayIdentity) map[string]string {
	type parentSegments struct {
		dir      string
		segments []string
	}
	parents := make([]parentSegments, 0, len(identities))
	maxDepth := 0
	for _, identity := range identities {
		segments := externalSplitPathSegments(identity.Dir)
		if len(segments) > 0 {
			segments = segments[:len(segments)-1]
		}
		parents = append(parents, parentSegments{dir: identity.Dir, segments: segments})
		if len(segments) > maxDepth {
			maxDepth = len(segments)
		}
	}
	if maxDepth == 0 {
		maxDepth = 1
	}
	disambiguators := make(map[string]string, len(parents))
	for _, current := range parents {
		resolved := current.dir
		for depth := 1; depth <= maxDepth; depth++ {
			candidate := strings.Join(lastPathSegments(current.segments, depth), "/")
			normalizedCandidate := candidate
			if normalizedCandidate == "" {
				normalizedCandidate = current.dir
			}
			unique := true
			for _, other := range parents {
				if other.dir == current.dir {
					continue
				}
				otherCandidate := strings.Join(lastPathSegments(other.segments, depth), "/")
				if otherCandidate == "" {
					otherCandidate = other.dir
				}
				if otherCandidate == normalizedCandidate {
					unique = false
					break
				}
			}
			if unique {
				resolved = normalizedCandidate
				break
			}
		}
		disambiguators[current.dir] = resolved
	}
	return disambiguators
}

func externalSplitPathSegments(path string) []string {
	return strings.FieldsFunc(path, func(r rune) bool {
		return r == '/' || r == '\\'
	})
}

func lastPathSegments(segments []string, count int) []string {
	if count >= len(segments) {
		return segments
	}
	return segments[len(segments)-count:]
}

func repositoryMode(workspace workspaceInfo, groups []workspaceGroup) string {
	if workspace.Kind != "" {
		return string(workspace.Kind)
	}
	if _, ok := rootWorkspaceForFork(workspace.Directory, groups); ok {
		return string(workspaceFork)
	}
	if workspace.IsRoot {
		return string(workspaceRoot)
	}
	return string(workspaceStandalone)
}

func forkCount(rootPath string, groups []workspaceGroup) int {
	for _, group := range groups {
		if sameWorkspacePath(group.Root.Directory, rootPath) {
			return len(group.Forks)
		}
	}
	return 0
}

func rootWorkspaceForFork(forkPath string, groups []workspaceGroup) (workspaceInfo, bool) {
	for _, group := range groups {
		for _, fork := range group.Forks {
			if sameWorkspacePath(fork.Directory, forkPath) {
				return group.Root, true
			}
		}
	}
	var workspace workspaceInfo
	return workspace, false
}

func humanizeStartSelfDriveError(workspace *externalRepositoryInfo, err error) error {
	if errors.Is(err, errRootWorkspaceCannotStart) {
		return fmt.Errorf("cannot start %q in Self-Drive Mode because it is a root repository. Choose a standalone repository or a fork workspace instead", workspace.Handle)
	}
	return err
}

func humanizeStartInteractiveError(workspace *externalRepositoryInfo, err error) error {
	switch {
	case errors.Is(err, errRootWorkspaceCannotStart):
		return fmt.Errorf("cannot start %q in interactive mode because it is a root repository. Choose a standalone repository or a fork workspace instead", workspace.Handle)
	case errors.Is(err, errInteractiveStartRequiresContinuousConfig):
		return fmt.Errorf("cannot start %q in interactive mode because it is configured for continuous mode. Call start_continuous instead", workspace.Handle)
	default:
		return err
	}
}

func humanizeStartContinuousError(workspace *externalRepositoryInfo, err error) error {
	switch {
	case errors.Is(err, errRootWorkspaceCannotStart):
		return fmt.Errorf("cannot start %q in continuous mode because it is a root repository. Choose a standalone repository or a fork workspace instead", workspace.Handle)
	case errors.Is(err, errContinuousModeNotConfigured):
		return fmt.Errorf("cannot start %q because continuous mode is not configured. Call start_interactive or start_self_drive instead", workspace.Handle)
	default:
		return err
	}
}

func humanizeGetPendingQuestionError(workspace *externalRepositoryInfo, err error) error {
	if errors.Is(err, errNoPendingQuestion) {
		return fmt.Errorf("workspace %q has no pending question. Call list_pending_questions to find a workspace that currently needs input", workspace.Handle)
	}
	return err
}

func humanizeAnswerPendingQuestionError(workspace *externalRepositoryInfo, err error) error {
	switch {
	case errors.Is(err, errNoPendingQuestion):
		return fmt.Errorf("workspace %q has no pending question. Call list_pending_questions to find a workspace that currently needs input", workspace.Handle)
	case errors.Is(err, errPromptTokenRequired):
		return fmt.Errorf("pending question answer for %q requires the current prompt token. Call get_pending_question and use the returned promptToken", workspace.Handle)
	case errors.Is(err, errQuestionNotAvailable):
		return fmt.Errorf("question is no longer current for %q. Call get_pending_question to fetch the latest prompt token and answer the current question", workspace.Handle)
	case errors.Is(err, errResponseCannotBeEmpty):
		return fmt.Errorf("pending question answer for %q is incomplete: answer text or selected choices are required. Call get_pending_question and provide answer text, selected choices, or both", workspace.Handle)
	default:
		return err
	}
}

func humanizeSteerNextTurnError(workspace *externalRepositoryInfo, err error) error {
	if errors.Is(err, errSteerMessageEmpty) {
		return fmt.Errorf("re-steering instruction for %q cannot be empty. Provide a non-empty message", workspace.Handle)
	}
	return err
}

func humanizeResetWorkspaceError(workspace *externalRepositoryInfo, err error) error {
	if errors.Is(err, errSessionResetWhileRunning) {
		return fmt.Errorf("cannot reset %q while it is running. Stop the workspace first", workspace.Handle)
	}
	return err
}

func humanizeForkRepositoryError(workspace *externalRepositoryInfo, err error) error {
	switch {
	case errors.Is(err, errForkOfFork):
		return fmt.Errorf("cannot fork %q because it is already a fork workspace. Choose a standalone repository or a root repository from factory_info instead", workspace.Handle)
	case errors.Is(err, errGoalContentEmpty):
		return fmt.Errorf("cannot fork %q without GOAL.md content for the new fork", workspace.Handle)
	case errors.Is(err, errForkTitleRequired):
		return fmt.Errorf("cannot fork %q because title is required for the new fork", workspace.Handle)
	default:
		return err
	}
}

func humanizeAttachRepositoryError(path string, err error) error {
	switch {
	case errors.Is(err, errPathNotAbsolute):
		return fmt.Errorf("cannot attach %q. Provide an absolute on-disk path to an existing repository or workspace", path)
	case errors.Is(err, errNotADirectory):
		return fmt.Errorf("cannot attach %q because it is not a directory", path)
	case errors.Is(err, errAlreadyAttached):
		return fmt.Errorf("repository %q is already attached", path)
	default:
		return err
	}
}

func externalBaseURL(r *http.Request) string {
	scheme := forwardedHeaderValue(r, "X-Forwarded-Proto")
	if scheme == "" {
		scheme = "http"
		if r != nil && r.TLS != nil {
			scheme = "https"
		}
	}

	host := forwardedHeaderValue(r, "X-Forwarded-Host")
	if host == "" && r != nil {
		host = r.Host
	}
	host = resolvedExternalHost(host, r)

	return scheme + "://" + externalURLHost(host)
}

func externalGoalEditURL(baseURL, workspaceName string) (string, error) {
	goalURL, errGoalURL := externalWorkspaceURL(baseURL, workspaceName, "goal/edit")
	if errGoalURL != nil {
		return "", fmt.Errorf("building goal edit link: %w", errGoalURL)
	}
	return goalURL, nil
}

func externalWorkspaceURL(baseURL, workspaceName, subpath string) (string, error) {
	parsedURL, errParse := url.Parse(baseURL)
	if errParse != nil {
		return "", fmt.Errorf("parse base url: %w", errParse)
	}
	pathSegments := []string{"workspaces", workspaceName}
	escapedPathSegments := []string{"workspaces", url.PathEscape(workspaceName)}
	trimmedSubpath := strings.Trim(subpath, "/")
	if trimmedSubpath != "" {
		for segment := range strings.SplitSeq(trimmedSubpath, "/") {
			pathSegments = append(pathSegments, segment)
			escapedPathSegments = append(escapedPathSegments, url.PathEscape(segment))
		}
	}
	parsedURL.Path = "/" + strings.Join(pathSegments, "/")
	parsedURL.RawPath = "/" + strings.Join(escapedPathSegments, "/")
	parsedURL.RawQuery = ""
	return parsedURL.String(), nil
}

func (c *externalMCPContext) workflowLinks(workspace *externalRepositoryInfo, workspacePath string) (externalWorkflowLinks, error) {
	workspaceName, errWorkspaceName := c.workflowLinkWorkspaceName(workspace, "workflow links")
	if errWorkspaceName != nil {
		return externalWorkflowLinks{}, errWorkspaceName
	}
	baseURL := externalBaseURL(c.request)
	progressURL, errProgressURL := externalWorkspaceURL(baseURL, workspaceName, "progress")
	if errProgressURL != nil {
		return externalWorkflowLinks{}, fmt.Errorf("building workflow links: progress link: %w", errProgressURL)
	}
	goalEditURL, errGoalEditURL := externalWorkspaceURL(baseURL, workspaceName, "goal/edit")
	if errGoalEditURL != nil {
		return externalWorkflowLinks{}, fmt.Errorf("building workflow links: goal edit link: %w", errGoalEditURL)
	}

	links := externalWorkflowLinks{
		Progress: progressURL,
		GoalEdit: goalEditURL,
		Respond:  "",
	}
	if !c.server.workflowStateSnapshot(workspacePath).NeedsHumanInput() {
		return links, nil
	}
	respondURL, errRespondURL := externalWorkspaceURL(baseURL, workspaceName, "respond")
	if errRespondURL != nil {
		return externalWorkflowLinks{}, fmt.Errorf("building workflow links: respond link: %w", errRespondURL)
	}
	links.Respond = respondURL
	return links, nil
}

func resolvedExternalHost(host string, r *http.Request) string {
	hostName, port := splitExternalHostPort(host)
	if hostName == "" {
		hostName = host
	}

	if hostName == "" || hostName == "0.0.0.0" || hostName == "::" {
		hostName = forwardedHeaderValue(r, "X-Forwarded-For")
		if hostName == "" && r != nil {
			remoteHost, _, errSplit := net.SplitHostPort(r.RemoteAddr)
			if errSplit == nil {
				hostName = remoteHost
			}
		}
	}

	if hostName == "" {
		hostName = "127.0.0.1"
	}
	if port == "" {
		return hostName
	}
	return net.JoinHostPort(hostName, port)
}

func externalURLHost(host string) string {
	if host == "" || strings.HasPrefix(host, "[") || !strings.Contains(host, ":") {
		return host
	}
	if net.ParseIP(host) == nil {
		return host
	}
	return "[" + host + "]"
}

func splitExternalHostPort(host string) (hostName, port string) {
	hostName, port, errSplit := net.SplitHostPort(host)
	if errSplit == nil {
		return hostName, port
	}
	return host, ""
}

func forwardedHeaderValue(r *http.Request, name string) string {
	if r == nil {
		return ""
	}
	value := strings.TrimSpace(r.Header.Get(name))
	if value == "" {
		return ""
	}
	parts := strings.Split(value, ",")
	return strings.TrimSpace(parts[0])
}
