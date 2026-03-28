package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type externalMCPContext struct {
	server  *Server
	request *http.Request
}

type externalTargetArgs struct {
	Target string `json:"target" jsonschema:"Repository or workspace handle, basename, or absolute path returned by factory_info."`
}

type externalForkArgs struct {
	Target      string `json:"target" jsonschema:"Repository handle, basename, or absolute path returned by factory_info."`
	GoalContent string `json:"goalContent" jsonschema:"GOAL.md content for the new fork. Provide the full document, including frontmatter when needed."`
}

type externalAttachArgs struct {
	Path string `json:"path" jsonschema:"Absolute on-disk path to an existing repository or workspace directory."`
}

type externalRepositoryInfo struct {
	Handle        string `json:"handle"`
	DirectoryName string `json:"directoryName"`
	Label         string `json:"label"`
	Title         string `json:"title,omitempty"`
	Path          string `json:"path"`
	Mode          string `json:"mode"`
	RootHandle    string `json:"rootHandle,omitempty"`
	RootPath      string `json:"rootPath,omitempty"`
	ForkCount     int    `json:"forkCount,omitempty"`
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
}

type externalGoalEditLinkResult struct {
	Workspace externalRepositoryInfo `json:"workspace"`
	URL       string                 `json:"url"`
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

	mcp.AddTool(server, newMCPTool("factory_info", "Describe this factory, including hostname, start directory, and attached repositories with hybrid handles and modes.", factoryInfoSchema), mcpCtx.factoryInfoHandler)
	mcp.AddTool(server, newMCPTool("start_self_drive", "Start an attached repository or workspace in Self-Drive Mode.", startSelfDriveSchema), mcpCtx.startSelfDriveHandler)
	mcp.AddTool(server, newMCPTool("stop_workspace", "Stop an attached workspace. This is idempotent for already stopped workspaces.", stopWorkspaceSchema), mcpCtx.stopWorkspaceHandler)
	mcp.AddTool(server, newMCPTool("reset_workspace", "Reset a stopped workspace state. Running workspaces must be stopped before reset.", resetWorkspaceSchema), mcpCtx.resetWorkspaceHandler)
	mcp.AddTool(server, newMCPTool("goal_edit_link", "Return a browser URL for editing GOAL.md for the selected workspace, based on the host used for this MCP connection.", goalEditLinkSchema), mcpCtx.goalEditLinkHandler)
	mcp.AddTool(server, newMCPTool("fork_repository", "Fork a standalone repository or root repository and create the new fork with the supplied GOAL.md content.", forkRepositorySchema), mcpCtx.forkRepositoryHandler)
	mcp.AddTool(server, newMCPTool("attach_repository", "Attach an existing on-disk repository or workspace by absolute path.", attachRepositorySchema), mcpCtx.attachRepositoryHandler)

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

	result, errStart := c.server.startSessionService(workspacePath, true)
	if errStart != nil {
		return nil, externalSessionActionResult{}, humanizeStartSelfDriveError(&workspace, errStart)
	}

	return nil, externalSessionActionResult{
		Workspace:      workspace,
		Status:         result.Status,
		Running:        result.Running,
		Message:        result.Message,
		AlreadyRunning: result.AlreadyRunning,
	}, nil
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
	}, nil
}

func (c *externalMCPContext) goalEditLinkHandler(_ context.Context, _ *mcp.CallToolRequest, args externalTargetArgs) (*mcp.CallToolResult, externalGoalEditLinkResult, error) {
	workspace, _, errResolve := c.resolveTarget(args.Target)
	if errResolve != nil {
		return nil, externalGoalEditLinkResult{}, errResolve
	}

	baseURL := externalBaseURL(c.request)
	workspaceName, errWorkspaceName := c.goalEditWorkspaceName(&workspace)
	if errWorkspaceName != nil {
		return nil, externalGoalEditLinkResult{}, errWorkspaceName
	}
	goalURL, errGoalURL := externalGoalEditURL(baseURL, workspaceName)
	if errGoalURL != nil {
		return nil, externalGoalEditLinkResult{}, errGoalURL
	}

	return nil, externalGoalEditLinkResult{Workspace: workspace, URL: goalURL}, nil
}

func (c *externalMCPContext) goalEditWorkspaceName(workspace *externalRepositoryInfo) (string, error) {
	matches := c.server.resolveWorkspaceNameToPaths(workspace.DirectoryName)
	if len(matches) == 1 && sameWorkspacePath(matches[0], workspace.Path) {
		return workspace.DirectoryName, nil
	}
	return "", fmt.Errorf("cannot build goal edit link for %q because workspace basename %q is ambiguous across attached repositories", workspace.Handle, workspace.DirectoryName)
}

func (c *externalMCPContext) forkRepositoryHandler(_ context.Context, _ *mcp.CallToolRequest, args externalForkArgs) (*mcp.CallToolResult, externalForkResult, error) {
	workspace, workspacePath, errResolve := c.resolveTarget(args.Target)
	if errResolve != nil {
		return nil, externalForkResult{}, errResolve
	}

	result, errFork := c.server.forkWorkspaceService(workspacePath, args.GoalContent)
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

func (c *externalMCPContext) repositories() ([]externalRepositoryInfo, error) {
	groups, errGroups := c.workspaceGroups()
	if errGroups != nil {
		return nil, errGroups
	}

	var repositories []externalRepositoryInfo
	for _, group := range groups {
		repositories = append(repositories, c.repositoryInfo(group.Root, groups))
		for _, fork := range group.Forks {
			repositories = append(repositories, c.repositoryInfo(fork, groups))
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
		return externalRepositoryInfo{}, "", errors.New("target is required. Call factory_info and use a returned handle, basename, or absolute path")
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
		return externalRepositoryInfo{}, "", fmt.Errorf("repository %q is not attached. Call factory_info and use one of the returned handle, basename, or absolute path values", target)
	}

	var matches []externalRepositoryInfo
	for i := range repositories {
		repository := repositories[i]
		if repository.Handle == target || repository.DirectoryName == target {
			matches = append(matches, repository)
		}
	}

	switch len(matches) {
	case 0:
		return externalRepositoryInfo{}, "", fmt.Errorf("repository %q not found. Call factory_info and use one of the returned handle, basename, or absolute path values", target)
	case 1:
		return matches[0], matches[0].Path, nil
	default:
		return externalRepositoryInfo{}, "", fmt.Errorf("target %q matches multiple attached repositories. Use the exact handle or absolute path returned by factory_info", target)
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

func (c *externalMCPContext) repositoryInfo(workspace workspaceInfo, groups []workspaceGroup) externalRepositoryInfo {
	label := repositoryLabel(workspace, groups)
	title := goalTitleStateFromPath(workspace.Directory, workspace.DirName).Title
	repository := externalRepositoryInfo{
		Handle:        repositoryHandle(label, workspace.DirName),
		DirectoryName: workspace.DirName,
		Label:         label,
		Title:         title,
		Path:          workspace.Directory,
		Mode:          repositoryMode(workspace, groups),
		RootHandle:    "",
		RootPath:      "",
		ForkCount:     0,
	}

	if repository.Mode == string(workspaceRoot) {
		repository.ForkCount = forkCount(workspace.Directory, groups)
	}

	if rootWorkspace, ok := rootWorkspaceForFork(workspace.Directory, groups); ok {
		rootLabel := repositoryLabel(rootWorkspace, groups)
		repository.RootHandle = repositoryHandle(rootLabel, rootWorkspace.DirName)
		repository.RootPath = rootWorkspace.Directory
	}

	return repository
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

func repositoryLabel(workspace workspaceInfo, groups []workspaceGroup) string {
	label := titleLabelForWorkspace(workspace)
	if rootWorkspace, ok := rootWorkspaceForFork(workspace.Directory, groups); ok {
		return titleLabelForWorkspace(rootWorkspace) + "/" + label
	}
	return label
}

func titleLabelForWorkspace(workspace workspaceInfo) string {
	label := goalTitleStateFromPath(workspace.Directory, workspace.DirName).label()
	if label == "" {
		return workspace.DirName
	}
	return label
}

func repositoryHandle(label, dirName string) string {
	if label == "" || label == dirName {
		return dirName
	}
	return label + " [" + dirName + "]"
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
	parsedURL, errParse := url.Parse(baseURL)
	if errParse != nil {
		return "", fmt.Errorf("building goal edit link: %w", errParse)
	}
	parsedURL.Path = "/workspaces/" + workspaceName + "/goal/edit"
	parsedURL.RawPath = "/workspaces/" + url.PathEscape(workspaceName) + "/goal/edit"
	parsedURL.RawQuery = ""
	return parsedURL.String(), nil
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
