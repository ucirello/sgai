package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"maps"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ucirello/sgai/pkg/state"
)

type signalSubscriber struct {
	ch   chan struct{}
	done chan struct{}
}

type signalBroker struct {
	mu          sync.Mutex
	subscribers map[*signalSubscriber]struct{}
}

func newSignalBroker() *signalBroker {
	var broker signalBroker
	broker.subscribers = make(map[*signalSubscriber]struct{})
	return &broker
}

func (b *signalBroker) subscribe() *signalSubscriber {
	s := &signalSubscriber{
		ch:   make(chan struct{}, 1),
		done: make(chan struct{}),
	}
	b.mu.Lock()
	b.subscribers[s] = struct{}{}
	b.mu.Unlock()
	return s
}

func (b *signalBroker) unsubscribe(s *signalSubscriber) {
	b.mu.Lock()
	delete(b.subscribers, s)
	b.mu.Unlock()
	close(s.done)
}

func (b *signalBroker) notify() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.subscribers {
		select {
		case s.ch <- struct{}{}:
		default:
		}
	}
}

func (s *Server) registerAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/state", s.handleAPIState)
	mux.HandleFunc("GET /api/v1/signal", s.handleSignalStream)
	mux.HandleFunc("GET /api/v1/agents", s.handleAPIAgents)
	mux.HandleFunc("GET /api/v1/skills", s.handleAPISkills)
	mux.HandleFunc("GET /api/v1/skills/{name...}", s.handleAPISkillDetail)
	mux.HandleFunc("GET /api/v1/snippets", s.handleAPISnippets)
	mux.HandleFunc("GET /api/v1/snippets/{lang}", s.handleAPISnippetsByLanguage)
	mux.HandleFunc("GET /api/v1/snippets/{lang}/{fileName}", s.handleAPISnippetDetail)

	mux.HandleFunc("POST /api/v1/workspaces/{name}/respond", s.handleAPIRespond)
	mux.HandleFunc("POST /api/v1/workspaces/{name}/start", s.handleAPIStartSession)
	mux.HandleFunc("POST /api/v1/workspaces/{name}/stop", s.handleAPIStopSession)
	mux.HandleFunc("POST /api/v1/workspaces/{name}/reset", s.handleAPIResetSession)
	mux.HandleFunc("POST /api/v1/workspaces/{name}/fork", s.handleAPIForkWorkspace)
	mux.HandleFunc("POST /api/v1/workspaces/{name}/delete-fork", s.handleAPIDeleteFork)
	mux.HandleFunc("POST /api/v1/workspaces/{name}/delete", s.handleAPIDeleteWorkspace)
	mux.HandleFunc("GET /api/v1/workspaces/{name}/goal", s.handleAPIGetGoal)
	mux.HandleFunc("GET /api/v1/workspaces/{name}/fork-template", s.handleAPIForkTemplate)
	mux.HandleFunc("PUT /api/v1/workspaces/{name}/goal", s.handleAPIUpdateGoal)
	mux.HandleFunc("GET /api/v1/workspaces/{name}/adhoc", s.handleAPIAdhocStatus)
	mux.HandleFunc("POST /api/v1/workspaces/{name}/adhoc", s.handleAPIAdhoc)
	mux.HandleFunc("DELETE /api/v1/workspaces/{name}/adhoc", s.handleAPIAdhocStop)
	mux.HandleFunc("POST /api/v1/workspaces/{name}/actions/run", s.handleAPIActionRun)
	mux.HandleFunc("DELETE /api/v1/workspaces/{name}/messages/{id}", s.handleAPIDeleteMessage)

	mux.HandleFunc("GET /api/v1/workspaces/{name}/workflow.svg", s.handleAPIWorkflowSVG)
	mux.HandleFunc("POST /api/v1/workspaces/{name}/steer", s.handleAPISteer)
	mux.HandleFunc("POST /api/v1/workspaces/{name}/pin", s.handleAPITogglePin)
	mux.HandleFunc("POST /api/v1/workspaces/{name}/open-editor", s.handleAPIOpenEditor)
	mux.HandleFunc("POST /api/v1/workspaces/{name}/open-editor/goal", s.handleAPIOpenEditorGoal)
	mux.HandleFunc("POST /api/v1/workspaces/{name}/open-editor/project-management", s.handleAPIOpenEditorProjectManagement)
	mux.HandleFunc("GET /api/v1/models", s.handleAPIListModels)

	mux.HandleFunc("GET /api/v1/browse-directories", s.handleAPIBrowseDirectories)
	mux.HandleFunc("POST /api/v1/workspaces/attach", s.handleAPIAttachWorkspace)
	mux.HandleFunc("POST /api/v1/workspaces/detach", s.handleAPIDetachWorkspace)
}

func (s *Server) handleSignalStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	sub := s.signals.subscribe()
	defer s.signals.unsubscribe(sub)

	for {
		select {
		case <-r.Context().Done():
			return
		case <-sub.done:
			return
		case <-sub.ch:
			if _, errWrite := fmt.Fprintf(w, "event: reload\ndata: {}\n\n"); errWrite != nil {
				return
			}
			flusher.Flush()
		}
	}
}

type apiFactoryState struct {
	Workspaces []apiWorkspaceFullState `json:"workspaces"`
}

type apiWorkspaceFullState struct {
	Name              string                      `json:"name"`
	Dir               string                      `json:"dir"`
	Running           bool                        `json:"running"`
	NeedsInput        bool                        `json:"needsInput"`
	InProgress        bool                        `json:"inProgress"`
	Pinned            bool                        `json:"pinned"`
	IsRoot            bool                        `json:"isRoot"`
	IsFork            bool                        `json:"isFork"`
	IsExternal        bool                        `json:"isExternal"`
	External          bool                        `json:"external"`
	HasSGAI           bool                        `json:"hasSgai"`
	Status            string                      `json:"status"`
	BadgeClass        string                      `json:"badgeClass"`
	BadgeText         string                      `json:"badgeText"`
	HasEditedGoal     bool                        `json:"hasEditedGoal"`
	InteractiveAuto   bool                        `json:"interactiveAuto"`
	ContinuousMode    bool                        `json:"continuousMode"`
	CurrentAgent      string                      `json:"currentAgent"`
	CurrentModel      string                      `json:"currentModel"`
	Task              string                      `json:"task"`
	GoalContent       string                      `json:"goalContent"`
	Title             string                      `json:"title"`
	ComputedTitle     string                      `json:"computedTitle,omitempty"`
	RawGoalContent    string                      `json:"rawGoalContent"`
	FullGoalContent   string                      `json:"fullGoalContent"`
	PMContent         string                      `json:"pmContent"`
	HasProjectMgmt    bool                        `json:"hasProjectMgmt"`
	SVGHash           string                      `json:"svgHash"`
	TotalExecTime     string                      `json:"totalExecTime"`
	LatestProgress    string                      `json:"latestProgress"`
	HumanMessage      string                      `json:"humanMessage"`
	AgentSequence     []apiAgentSequenceEntry     `json:"agentSequence"`
	Cost              state.SessionCost           `json:"cost"`
	ModelStatuses     []apiModelStatusEntry       `json:"modelStatuses,omitempty"`
	AgentModels       []apiAgentModelEntry        `json:"agentModels,omitempty"`
	Events            []apiEventEntry             `json:"events"`
	Messages          []apiMessageEntry           `json:"messages"`
	ProjectTodos      []apiTodoEntry              `json:"projectTodos"`
	AgentTodos        []apiTodoEntry              `json:"agentTodos"`
	Forks             []apiForkEntry              `json:"forks,omitempty"`
	Log               []apiLogEntry               `json:"log"`
	PendingQuestion   *apiPendingQuestionResponse `json:"pendingQuestion,omitempty"`
	Actions           []apiActionEntry            `json:"actions,omitempty"`
	ActionConfigError string                      `json:"actionConfigError,omitempty"`
	RepositoryAction  apiRepositoryAction         `json:"repositoryAction"`
}

func (s *Server) handleAPIState(w http.ResponseWriter, _ *http.Request) {
	if cached, ok := s.stateCache.get("state"); ok {
		writeJSON(w, cached)
		return
	}
	factoryState, _ := s.stateFlight.do("state", func() (apiFactoryState, error) {
		if cached, ok := s.stateCache.get("state"); ok {
			return cached, nil
		}
		s.mu.Lock()
		genBefore := s.stateGeneration
		s.mu.Unlock()
		result := s.buildFullFactoryState()
		s.mu.Lock()
		genAfter := s.stateGeneration
		s.mu.Unlock()
		if genBefore == genAfter {
			s.stateCache.set("state", result)
		}
		return result, nil
	})
	writeJSON(w, factoryState)
}

func (s *Server) warmStateCache() {
	s.mu.Lock()
	genBefore := s.stateGeneration
	s.mu.Unlock()
	result := s.buildFullFactoryState()
	s.mu.Lock()
	genAfter := s.stateGeneration
	s.mu.Unlock()
	if genBefore == genAfter {
		s.stateCache.set("state", result)
	}
}

func (s *Server) loadWorkspaceState(dir string) state.Workflow {
	var emptyState state.Workflow
	stPath := statePath(dir)
	info, errStat := os.Stat(stPath)
	if errStat != nil {
		return emptyState
	}
	if info.Size() > maxStateSizeBytes {
		return emptyState
	}
	return s.workspaceCoordinator(dir).State()
}

func (s *Server) buildFullFactoryState() apiFactoryState {
	groups, errScan := s.scanWorkspaceGroups()
	if errScan != nil {
		return apiFactoryState{Workspaces: nil}
	}

	var allWorkspaces []workspaceInfo
	for _, grp := range groups {
		allWorkspaces = append(allWorkspaces, grp.Root)
		allWorkspaces = append(allWorkspaces, grp.Forks...)
	}

	workspaces := make([]apiWorkspaceFullState, len(allWorkspaces))
	var wg sync.WaitGroup
	for i, ws := range allWorkspaces {
		wg.Go(func() {
			workspaces[i] = s.buildWorkspaceFullState(ws, groups)
		})
	}
	wg.Wait()

	return apiFactoryState{Workspaces: workspaces}
}

const maxStateSizeBytes = 10 * 1024 * 1024

func (s *Server) buildWorkspaceFullState(ws workspaceInfo, groups []workspaceGroup) apiWorkspaceFullState {
	wfState := s.loadWorkspaceState(ws.Directory)
	kind := s.classifyWorkspaceCached(ws.Directory)

	interactiveAuto := wfState.InteractionMode == state.ModeSelfDrive || wfState.InteractionMode == state.ModeContinuous
	badgeClass, badgeText := badgeStatus(&wfState, ws.Running)
	needsInput := wfState.NeedsHumanInput()

	currentAgent := wfState.CurrentAgent
	if currentAgent == "" {
		currentAgent = "Unknown"
	}

	status := wfState.Status
	if status == "" {
		status = "-"
	}

	goalContent, rawGoalContent, fullGoalContent, pmContent, hasProjectMgmt := readGoalAndPMForAPI(ws.Directory)
	titleState := goalTitleStateFromContent([]byte(fullGoalContent), ws.DirName)
	if titleState.NeedsRepair {
		s.enqueueGoalTitleRepair(ws.Directory)
	}

	hasEditedGoal := false
	if data, errRead := os.ReadFile(filepath.Join(ws.Directory, "GOAL.md")); errRead == nil {
		body := extractBody(data)
		hasEditedGoal = strings.TrimSpace(string(body)) != ""
	}

	agentSeq := convertAgentSequence(
		prepareAgentSequenceDisplay(wfState.AgentSequence, ws.Running, getLastActivityTime(wfState.Progress), ws.Directory),
	)
	totalExecTime := calculateTotalExecutionTime(wfState.AgentSequence, ws.Running, getLastActivityTime(wfState.Progress))

	modelStatuses := convertModelStatuses(orderedModelStatuses(ws.Directory, wfState.ModelStatuses))

	reversedProgress := slices.Clone(wfState.Progress)
	slices.Reverse(reversedProgress)
	events := convertEventsForAPI(formatProgressForDisplay(reversedProgress))

	messages := convertMessagesForAPI(wfState.Messages)

	var logLines []apiLogEntry
	s.mu.Lock()
	sess := s.sessions[ws.Directory]
	s.mu.Unlock()
	if sess != nil && sess.outputLog != nil {
		for _, line := range sess.outputLog.lines() {
			logLines = append(logLines, apiLogEntry{Prefix: line.prefix, Text: line.text})
		}
	}

	var pendingQuestion *apiPendingQuestionResponse
	if wfState.NeedsHumanInput() {
		coord := s.sessionCoordinator(ws.Directory)
		agentName := currentAgent
		var questions []apiQuestionItem
		if wfState.MultiChoiceQuestion != nil {
			questions = make([]apiQuestionItem, 0, len(wfState.MultiChoiceQuestion.Questions))
			for _, q := range wfState.MultiChoiceQuestion.Questions {
				questions = append(questions, apiQuestionItem{
					Question:    q.Question,
					Choices:     q.Choices,
					MultiSelect: q.MultiSelect,
				})
			}
		}
		pendingQuestion = &apiPendingQuestionResponse{
			PromptToken: promptTokenForState(coord, &wfState),
			Type:        questionType(&wfState),
			AgentName:   agentName,
			Message:     wfState.HumanMessage,
			Questions:   questions,
		}
	}

	actionState := loadActionsForAPI(ws.Directory)
	repositoryAction := s.workspaceActionPolicy(ws.Directory)

	full := apiWorkspaceFullState{
		Name:              ws.DirName,
		Dir:               ws.Directory,
		Forks:             nil,
		Running:           ws.Running,
		NeedsInput:        needsInput,
		InProgress:        ws.InProgress,
		Pinned:            ws.Pinned,
		IsRoot:            ws.IsRoot,
		IsFork:            kind == workspaceFork,
		IsExternal:        ws.External,
		External:          ws.External,
		HasSGAI:           ws.HasWorkspace,
		Status:            status,
		BadgeClass:        badgeClass,
		BadgeText:         badgeText,
		HasEditedGoal:     hasEditedGoal,
		InteractiveAuto:   interactiveAuto,
		ContinuousMode:    readContinuousModePrompt(ws.Directory) != "",
		CurrentAgent:      currentAgent,
		CurrentModel:      resolveCurrentModel(ws.Directory, &wfState),
		Task:              wfState.Task,
		GoalContent:       goalContent,
		Title:             titleState.Title,
		ComputedTitle:     workspaceComputedTitle(ws, groups, titleState),
		RawGoalContent:    rawGoalContent,
		FullGoalContent:   fullGoalContent,
		PMContent:         pmContent,
		HasProjectMgmt:    hasProjectMgmt,
		SVGHash:           s.getWorkflowSVGHashCached(ws.Directory, currentAgent),
		TotalExecTime:     totalExecTime,
		LatestProgress:    getLatestProgress(wfState.Progress),
		HumanMessage:      wfState.HumanMessage,
		AgentSequence:     agentSeq,
		Cost:              wfState.Cost,
		ModelStatuses:     modelStatuses,
		AgentModels:       collectAgentModels(ws.Directory),
		Events:            events,
		Messages:          messages,
		ProjectTodos:      convertTodosForAPI(wfState.ProjectTodos),
		AgentTodos:        convertTodosForAPI(wfState.Todos),
		Log:               logLines,
		PendingQuestion:   pendingQuestion,
		Actions:           actionState.Actions,
		ActionConfigError: actionState.ConfigError,
		RepositoryAction:  repositoryAction.api(ws.DirName),
	}

	if ws.IsRoot {
		full.Forks = s.collectForksForAPIFromGroups(ws.Directory, groups)
	}

	return full
}

func workspaceComputedTitle(ws workspaceInfo, groups []workspaceGroup, titleState goalTitleState) string {
	if rootDirName, ok := forkRootDirName(ws.Directory, groups); ok {
		forkLabel := titleState.label()
		if forkLabel == "" {
			return rootDirName
		}
		return rootDirName + "/" + forkLabel
	}
	if rootHasForks(ws, groups) {
		return ws.DirName
	}
	return titleState.ComputedTitle
}

func rootHasForks(ws workspaceInfo, groups []workspaceGroup) bool {
	if !ws.IsRoot {
		return false
	}
	for _, grp := range groups {
		if sameWorkspacePath(grp.Root.Directory, ws.Directory) {
			return len(grp.Forks) > 0
		}
	}
	return false
}

func forkRootDirName(workspaceDir string, groups []workspaceGroup) (string, bool) {
	for _, grp := range groups {
		for _, fork := range grp.Forks {
			if sameWorkspacePath(fork.Directory, workspaceDir) {
				return grp.Root.DirName, true
			}
		}
	}
	return "", false
}

func (s *Server) collectForksForAPIFromGroups(rootDir string, groups []workspaceGroup) []apiForkEntry {
	for _, grp := range groups {
		if !sameWorkspacePath(grp.Root.Directory, rootDir) {
			continue
		}
		forks := make([]apiForkEntry, len(grp.Forks))
		var wg sync.WaitGroup
		for i, fork := range grp.Forks {
			wg.Go(func() {
				wfState := s.loadWorkspaceState(fork.Directory)
				titleState := goalTitleStateFromPath(fork.Directory, fork.DirName)
				if titleState.NeedsRepair {
					s.enqueueGoalTitleRepair(fork.Directory)
				}
				forks[i] = apiForkEntry{
					Name:          fork.DirName,
					Dir:           fork.Directory,
					Running:       fork.Running,
					NeedsInput:    wfState.NeedsHumanInput(),
					InProgress:    fork.InProgress,
					Pinned:        fork.Pinned,
					Title:         titleState.Title,
					ComputedTitle: workspaceComputedTitle(fork, groups, titleState),
				}
			})
		}
		wg.Wait()
		return forks
	}
	return nil
}

func (s *Server) spaMiddleware(mux *http.ServeMux) http.Handler {
	webappFS, errSub := fs.Sub(webappDist, "webapp/dist")
	if errSub != nil {
		log.Println("failed to create webapp sub-filesystem:", errSub)
	}

	var staticHandler http.Handler
	if webappFS != nil {
		staticHandler = http.FileServerFS(webappFS)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIRoute(r.URL.Path) {
			mux.ServeHTTP(w, r)
			return
		}

		if webappFS == nil {
			http.Error(w, "react app not available", http.StatusInternalServerError)
			return
		}

		if isStaticAsset(r.URL.Path) {
			staticHandler.ServeHTTP(w, r)
			return
		}

		serveReactIndex(w, webappFS)
	})
}

func isAPIRoute(urlPath string) bool {
	return strings.HasPrefix(urlPath, "/api/") || strings.HasPrefix(urlPath, "/mcp/")
}

func isStaticAsset(urlPath string) bool {
	ext := path.Ext(urlPath)
	switch ext {
	case ".js", ".css", ".map", ".png", ".svg", ".ico", ".woff", ".woff2", ".ttf", ".json":
		return true
	}
	return false
}

func serveReactIndex(w http.ResponseWriter, webappFS fs.FS) {
	indexHTML, errRead := fs.ReadFile(webappFS, "index.html")
	if errRead != nil {
		http.Error(w, "react app not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, errWrite := w.Write(indexHTML); errWrite != nil {
		log.Println("failed to serve react index:", errWrite)
	}
}

func (s *Server) resolveAPIWorkspace(r *http.Request) (workspacePath string, statusCode int, errMessage string) {
	if name := r.URL.Query().Get("workspace"); name != "" {
		return s.resolveSingleWorkspacePath(name)
	}
	groups, errScan := s.scanWorkspaceGroups()
	if errScan != nil || len(groups) == 0 {
		return "", http.StatusNotFound, "workspace not found"
	}
	return groups[0].Root.Directory, 0, ""
}

type apiAgentEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type apiAgentsResponse struct {
	Agents []apiAgentEntry `json:"agents"`
}

func (s *Server) handleAPIAgents(w http.ResponseWriter, r *http.Request) {
	workspacePath, statusCode, errMessage := s.resolveAPIWorkspace(r)
	if statusCode != 0 {
		http.Error(w, errMessage, statusCode)
		return
	}

	agents := collectAgents(workspacePath)
	writeJSON(w, apiAgentsResponse{Agents: agents})
}

func collectAgents(workspacePath string) []apiAgentEntry {
	agentsDir := filepath.Join(workspacePath, ".sgai", "agent")
	agentsFS := os.DirFS(agentsDir)

	var agents []apiAgentEntry
	errWalk := fs.WalkDir(agentsFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		name := strings.TrimSuffix(p, ".md")
		content, errRead := fs.ReadFile(agentsFS, p)
		if errRead != nil {
			return nil
		}
		desc := extractFrontmatterDescription(string(content))
		agents = append(agents, apiAgentEntry{
			Name:        name,
			Description: desc,
		})
		return nil
	})
	if errWalk != nil {
		return nil
	}

	slices.SortFunc(agents, func(a, b apiAgentEntry) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})

	return agents
}

type apiSkillEntry struct {
	Name        string `json:"name"`
	FullPath    string `json:"fullPath"`
	Description string `json:"description"`
}

type apiSkillCategory struct {
	Name   string          `json:"name"`
	Skills []apiSkillEntry `json:"skills"`
}

type apiSkillsResponse struct {
	Categories []apiSkillCategory `json:"categories"`
}

func (s *Server) handleAPISkills(w http.ResponseWriter, r *http.Request) {
	workspacePath, statusCode, errMessage := s.resolveAPIWorkspace(r)
	if statusCode != 0 {
		http.Error(w, errMessage, statusCode)
		return
	}

	categories := collectSkillCategories(workspacePath)
	writeJSON(w, apiSkillsResponse{Categories: categories})
}

func collectSkillCategories(workspacePath string) []apiSkillCategory {
	skillsDir := filepath.Join(workspacePath, ".sgai", "skills")
	skillsFS := os.DirFS(skillsDir)

	grouped := make(map[string][]apiSkillEntry)

	errWalk := fs.WalkDir(skillsFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != "SKILL.md" {
			return nil
		}
		content, errRead := fs.ReadFile(skillsFS, p)
		if errRead != nil {
			return nil
		}
		skillPath := strings.TrimSuffix(p, "/SKILL.md")
		parts := strings.Split(skillPath, "/")
		var category, name string
		if len(parts) > 1 {
			category = parts[0]
			name = strings.Join(parts[1:], "/")
		} else {
			name = skillPath
		}
		desc := extractFrontmatterDescription(string(content))
		grouped[category] = append(grouped[category], apiSkillEntry{
			Name:        name,
			FullPath:    skillPath,
			Description: desc,
		})
		return nil
	})
	if errWalk != nil {
		return nil
	}

	var categories []apiSkillCategory
	for _, categoryName := range slices.Sorted(maps.Keys(grouped)) {
		skills := grouped[categoryName]
		slices.SortFunc(skills, func(a, b apiSkillEntry) int {
			return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		})
		displayName := categoryName
		if displayName == "" {
			displayName = "General"
		}
		categories = append(categories, apiSkillCategory{
			Name:   displayName,
			Skills: skills,
		})
	}

	return categories
}

type apiSkillDetailResponse struct {
	Name       string `json:"name"`
	FullPath   string `json:"fullPath"`
	Content    string `json:"content"`
	RawContent string `json:"rawContent"`
}

func (s *Server) handleAPISkillDetail(w http.ResponseWriter, r *http.Request) {
	workspacePath, statusCode, errMessage := s.resolveAPIWorkspace(r)
	if statusCode != 0 {
		http.Error(w, errMessage, statusCode)
		return
	}

	skillName := r.PathValue("name")
	if skillName == "" {
		http.Error(w, "skill name is required", http.StatusBadRequest)
		return
	}

	skillsDir := filepath.Join(workspacePath, ".sgai", "skills")
	skillsFS := os.DirFS(skillsDir)

	skillFilePath := skillName + "/SKILL.md"
	content, errRead := fs.ReadFile(skillsFS, skillFilePath)
	if errRead != nil {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}

	stripped := stripFrontmatter(string(content))
	rendered, errRender := renderMarkdown([]byte(stripped))
	if errRender != nil {
		rendered = stripped
	}

	writeJSON(w, apiSkillDetailResponse{
		Name:       path.Base(skillName),
		FullPath:   skillName,
		Content:    rendered,
		RawContent: stripped,
	})
}

type apiSnippetEntry struct {
	Name        string `json:"name"`
	FileName    string `json:"fileName"`
	FullPath    string `json:"fullPath"`
	Description string `json:"description"`
	Language    string `json:"language"`
}

type apiLanguageCategory struct {
	Name     string            `json:"name"`
	Snippets []apiSnippetEntry `json:"snippets"`
}

type apiSnippetsResponse struct {
	Languages []apiLanguageCategory `json:"languages"`
}

func (s *Server) handleAPISnippets(w http.ResponseWriter, r *http.Request) {
	workspacePath, statusCode, errMessage := s.resolveAPIWorkspace(r)
	if statusCode != 0 {
		http.Error(w, errMessage, statusCode)
		return
	}

	languages := convertSnippetLanguages(gatherSnippetsByLanguage(workspacePath))
	writeJSON(w, apiSnippetsResponse{Languages: languages})
}

func convertSnippetLanguages(categories []languageCategory) []apiLanguageCategory {
	result := make([]apiLanguageCategory, 0, len(categories))
	for _, lc := range categories {
		snippets := make([]apiSnippetEntry, 0, len(lc.Snippets))
		for _, s := range lc.Snippets {
			snippets = append(snippets, apiSnippetEntry(s))
		}
		result = append(result, apiLanguageCategory{
			Name:     lc.Name,
			Snippets: snippets,
		})
	}
	return result
}

type apiSnippetsByLanguageResponse struct {
	Language string            `json:"language"`
	Snippets []apiSnippetEntry `json:"snippets"`
}

func (s *Server) handleAPISnippetsByLanguage(w http.ResponseWriter, r *http.Request) {
	workspacePath, statusCode, errMessage := s.resolveAPIWorkspace(r)
	if statusCode != 0 {
		http.Error(w, errMessage, statusCode)
		return
	}

	lang := r.PathValue("lang")
	if lang == "" {
		http.Error(w, "language is required", http.StatusBadRequest)
		return
	}

	allLanguages := convertSnippetLanguages(gatherSnippetsByLanguage(workspacePath))
	for _, lc := range allLanguages {
		if lc.Name == lang {
			writeJSON(w, apiSnippetsByLanguageResponse{
				Language: lc.Name,
				Snippets: lc.Snippets,
			})
			return
		}
	}

	http.Error(w, "language not found", http.StatusNotFound)
}

type apiSnippetDetailResponse struct {
	Name        string `json:"name"`
	FileName    string `json:"fileName"`
	Language    string `json:"language"`
	Description string `json:"description"`
	WhenToUse   string `json:"whenToUse"`
	Content     string `json:"content"`
}

func (s *Server) handleAPISnippetDetail(w http.ResponseWriter, r *http.Request) {
	workspacePath, statusCode, errMessage := s.resolveAPIWorkspace(r)
	if statusCode != 0 {
		http.Error(w, errMessage, statusCode)
		return
	}

	lang := r.PathValue("lang")
	fileName := r.PathValue("fileName")
	if lang == "" || fileName == "" {
		http.Error(w, "language and fileName are required", http.StatusBadRequest)
		return
	}

	snippetsDir := filepath.Join(workspacePath, ".sgai", "snippets")
	snippetsFS := os.DirFS(snippetsDir)

	var content []byte
	extensions := []string{".go", ".html", ".css", ".js", ".ts", ".py", ".sh", ".yaml", ".yml", ".json", ".md", ".sql", ".txt", ""}
	for _, ext := range extensions {
		filePath := lang + "/" + fileName + ext
		var errRead error
		content, errRead = fs.ReadFile(snippetsFS, filePath)
		if errRead == nil {
			break
		}
	}

	if content == nil {
		http.Error(w, "snippet not found", http.StatusNotFound)
		return
	}

	fm := parseFrontmatterMap(content)
	name := fm["name"]
	if name == "" {
		name = fileName
	}

	writeJSON(w, apiSnippetDetailResponse{
		Name:        name,
		FileName:    fileName,
		Language:    lang,
		Description: fm["description"],
		WhenToUse:   fm["when_to_use"],
		Content:     stripFrontmatter(string(content)),
	})
}

type apiActionEntry struct {
	Name            string   `json:"name"`
	Model           string   `json:"model"`
	Prompt          string   `json:"prompt"`
	Script          string   `json:"script,omitempty"`
	Description     string   `json:"description,omitempty"`
	Kind            string   `json:"kind,omitempty"`
	Variables       []string `json:"variables,omitempty"`
	ValidationError string   `json:"validationError,omitempty"`
}

type apiAgentSequenceEntry struct {
	Agent       string `json:"agent"`
	Model       string `json:"model"`
	ElapsedTime string `json:"elapsedTime"`
	IsCurrent   bool   `json:"isCurrent"`
}

func loadActionsForAPI(workspacePath string) actionAPIState {
	configs, errLoad := loadActionConfigs(workspacePath)
	if errLoad != nil {
		return actionAPIState{Actions: nil, ConfigError: errLoad.Error()}
	}
	return actionAPIState{Actions: convertActionsForAPI(configs), ConfigError: ""}
}

func convertActionsForAPI(configs []actionConfig) []apiActionEntry {
	nameErrors := actionIdentityErrors(configs)
	actions := make([]apiActionEntry, 0, len(configs))
	for i := range configs {
		actions = append(actions, convertActionForAPIWithIdentityError(&configs[i], nameErrors[i]))
	}
	return actions
}

func readGoalAndPMForAPI(dir string) (goalContent, rawGoalContent, fullGoalContent, pmContent string, hasProjectMgmt bool) {
	if data, errRead := os.ReadFile(filepath.Join(dir, "GOAL.md")); errRead == nil {
		fullGoalContent = string(data)
		stripped := stripFrontmatter(fullGoalContent)
		rawGoalContent = stripped
		if rendered, errRender := renderMarkdown([]byte(stripped)); errRender == nil {
			goalContent = rendered
		} else {
			goalContent = stripped
		}
	}

	if data, errRead := os.ReadFile(filepath.Join(dir, ".sgai", "PROJECT_MANAGEMENT.md")); errRead == nil {
		hasProjectMgmt = true
		stripped := stripFrontmatter(string(data))
		if rendered, errRender := renderMarkdown([]byte(stripped)); errRender == nil {
			pmContent = rendered
		} else {
			pmContent = stripped
		}
	}

	return goalContent, rawGoalContent, fullGoalContent, pmContent, hasProjectMgmt
}

func convertAgentSequence(displays []agentSequenceDisplay) []apiAgentSequenceEntry {
	result := make([]apiAgentSequenceEntry, 0, len(displays))
	for _, d := range displays {
		result = append(result, apiAgentSequenceEntry(d))
	}
	return result
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Println("failed to encode json response:", err)
	}
}

func (s *Server) resolveWorkspaceFromPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	workspaceName := r.PathValue("name")
	if workspaceName == "" {
		http.Error(w, "workspace name is required", http.StatusBadRequest)
		return "", false
	}
	workspacePath, statusCode, errMessage := s.resolveSingleWorkspacePath(workspaceName)
	if statusCode != 0 {
		http.Error(w, errMessage, statusCode)
		return "", false
	}
	return workspacePath, true
}

func (s *Server) resolveGoalWorkspaceFromPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	workspaceName := r.PathValue("name")
	if workspaceName == "" {
		http.Error(w, "workspace name is required", http.StatusBadRequest)
		return "", false
	}
	workspacePath, statusCode, errMessage := s.resolveSingleWorkspacePath(workspaceName)
	if statusCode != 0 {
		http.Error(w, errMessage, statusCode)
		return "", false
	}
	return workspacePath, true
}

func (s *Server) resolveWorkspaceForAction(workspaceName string) (workspacePath string, statusCode int, errMessage string) {
	return s.resolveSingleWorkspacePath(workspaceName)
}

func sameWorkspacePath(leftPath, rightPath string) bool {
	leftCandidates := workspacePathCandidates(leftPath)
	rightCandidates := workspacePathCandidates(rightPath)
	if len(leftCandidates) == 0 || len(rightCandidates) == 0 {
		return false
	}

	rightSet := make(map[string]bool, len(rightCandidates))
	for _, candidate := range rightCandidates {
		rightSet[candidate] = true
	}
	for _, candidate := range leftCandidates {
		if rightSet[candidate] {
			return true
		}
	}
	return false
}

type apiModelStatusEntry struct {
	ModelID string `json:"modelId"`
	Status  string `json:"status"`
}

func convertModelStatuses(displays []modelStatusDisplay) []apiModelStatusEntry {
	if len(displays) == 0 {
		return nil
	}
	result := make([]apiModelStatusEntry, 0, len(displays))
	for _, d := range displays {
		result = append(result, apiModelStatusEntry(d))
	}
	return result
}

type apiMessageEntry struct {
	ID        int    `json:"id"`
	FromAgent string `json:"fromAgent"`
	ToAgent   string `json:"toAgent"`
	Body      string `json:"body"`
	Subject   string `json:"subject"`
	Read      bool   `json:"read"`
	ReadAt    string `json:"readAt,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}

const maxMessageBodyBytes = 10 * 1024

func convertMessagesForAPI(messages []state.Message) []apiMessageEntry {
	result := make([]apiMessageEntry, 0, len(messages))
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		subject := extractSubject(msg.Body)
		body := msg.Body
		if len(body) > maxMessageBodyBytes {
			body = body[:maxMessageBodyBytes] + "...[truncated]"
		}
		result = append(result, apiMessageEntry{
			ID:        msg.ID,
			FromAgent: msg.FromAgent,
			ToAgent:   msg.ToAgent,
			Body:      body,
			Subject:   subject,
			Read:      msg.Read,
			ReadAt:    msg.ReadAt,
			CreatedAt: msg.CreatedAt,
		})
	}
	return result
}

type apiTodoEntry struct {
	ID       string `json:"id"`
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}

func convertTodosForAPI(todos []state.TodoItem) []apiTodoEntry {
	result := make([]apiTodoEntry, 0, len(todos))
	for _, t := range todos {
		result = append(result, apiTodoEntry{
			ID:       t.ID,
			Content:  t.Content,
			Status:   t.Status,
			Priority: t.Priority,
		})
	}
	return result
}

type apiLogEntry struct {
	Prefix string `json:"prefix"`
	Text   string `json:"text"`
}

type apiEventEntry struct {
	Timestamp       string `json:"timestamp"`
	FormattedTime   string `json:"formattedTime"`
	Agent           string `json:"agent"`
	Description     string `json:"description"`
	ShowDateDivider bool   `json:"showDateDivider"`
	DateDivider     string `json:"dateDivider"`
}

type apiAgentModelEntry struct {
	Agent  string   `json:"agent"`
	Models []string `json:"models"`
}

func convertEventsForAPI(displays []eventsProgressDisplay) []apiEventEntry {
	result := make([]apiEventEntry, 0, len(displays))
	for _, d := range displays {
		result = append(result, apiEventEntry(d))
	}
	return result
}

type apiForkEntry struct {
	Name          string `json:"name"`
	Dir           string `json:"dir"`
	Running       bool   `json:"running"`
	NeedsInput    bool   `json:"needsInput"`
	InProgress    bool   `json:"inProgress"`
	Pinned        bool   `json:"pinned"`
	Title         string `json:"title"`
	ComputedTitle string `json:"computedTitle,omitempty"`
}

func promptTokenForState(coord *state.Coordinator, wfState *state.Workflow) string {
	if coord == nil || !wfState.NeedsHumanInput() {
		return ""
	}
	return coord.CurrentPromptToken()
}

func questionType(wfState *state.Workflow) string {
	if wfState.MultiChoiceQuestion != nil {
		if wfState.MultiChoiceQuestion.IsWorkGate {
			return "work-gate"
		}
		return "multi-choice"
	}
	if wfState.HumanMessage != "" {
		return "free-text"
	}
	return ""
}

type apiQuestionItem struct {
	Question    string   `json:"question"`
	Choices     []string `json:"choices"`
	MultiSelect bool     `json:"multiSelect"`
}

type apiPendingQuestionResponse struct {
	PromptToken string            `json:"promptToken,omitempty"`
	Type        string            `json:"type"`
	AgentName   string            `json:"agentName"`
	Message     string            `json:"message"`
	Questions   []apiQuestionItem `json:"questions,omitempty"`
}

type apiRespondRequest struct {
	PromptToken     string   `json:"promptToken,omitempty"`
	Answer          string   `json:"answer"`
	SelectedChoices []string `json:"selectedChoices"`
}

type apiRespondResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func (s *Server) handleAPIRespond(w http.ResponseWriter, r *http.Request) {
	workspacePath, ok := s.resolveWorkspaceFromPath(w, r)
	if !ok {
		return
	}

	var req apiRespondRequest
	if errDecode := json.NewDecoder(r.Body).Decode(&req); errDecode != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	coord := s.sessionCoordinator(workspacePath)
	if coord == nil {
		http.Error(w, "no pending question", http.StatusConflict)
		return
	}

	s.handleRespondViaCoordinator(w, coord, req)
}

func (s *Server) sessionCoordinator(workspacePath string) *state.Coordinator {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[workspacePath]
	if sess == nil {
		return nil
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.coord
}

func (s *Server) handleRespondViaCoordinator(w http.ResponseWriter, coord *state.Coordinator, req apiRespondRequest) {
	wfState := coord.State()

	if !wfState.NeedsHumanInput() {
		http.Error(w, "no pending question", http.StatusConflict)
		return
	}

	responseText := buildAPIResponseText(req)
	if responseText == "" {
		http.Error(w, "response cannot be empty", http.StatusBadRequest)
		return
	}

	if !coord.RespondIfCurrent(req.PromptToken, responseText) {
		http.Error(w, "question not available", http.StatusConflict)
		return
	}

	if wfState.MultiChoiceQuestion != nil && wfState.MultiChoiceQuestion.IsWorkGate {
		approvedViaSelection := slices.Contains(req.SelectedChoices, workGateApprovalText)
		if approvedViaSelection {
			if err := coord.UpdateState(func(wf *state.Workflow) {
				if wf.InteractionMode == state.ModeBrainstorming {
					wf.InteractionMode = state.ModeBuilding
				}
			}); err != nil {
				http.Error(w, "failed to save work gate approval", http.StatusInternalServerError)
				return
			}
		}
	}

	s.notifyStateChange()

	writeJSON(w, apiRespondResponse{Success: true, Message: "response submitted"})
}

func buildAPIResponseText(req apiRespondRequest) string {
	var parts []string
	if len(req.SelectedChoices) > 0 {
		parts = append(parts, "Selected: "+strings.Join(req.SelectedChoices, ", "))
	}
	if req.Answer != "" {
		parts = append(parts, req.Answer)
	}
	return strings.Join(parts, "\n")
}

type apiStartSessionRequest struct {
	Auto bool `json:"auto"`
}

type apiSessionActionResponse struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Running bool   `json:"running"`
	Message string `json:"message"`
}

func (s *Server) handleAPIStartSession(w http.ResponseWriter, r *http.Request) {
	workspacePath, ok := s.resolveWorkspaceFromPath(w, r)
	if !ok {
		return
	}

	var req apiStartSessionRequest
	if errDecode := json.NewDecoder(r.Body).Decode(&req); errDecode != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	result, errStart := s.startSessionService(workspacePath, req.Auto)
	if errStart != nil {
		statusCode := http.StatusInternalServerError
		if errors.Is(errStart, errRootWorkspaceCannotStart) {
			statusCode = http.StatusBadRequest
		}
		http.Error(w, errStart.Error(), statusCode)
		return
	}

	writeJSON(w, apiSessionActionResponse{
		Name:    result.Name,
		Status:  result.Status,
		Running: result.Running,
		Message: result.Message,
	})
}

func (s *Server) handleAPIStopSession(w http.ResponseWriter, r *http.Request) {
	workspacePath, ok := s.resolveWorkspaceFromPath(w, r)
	if !ok {
		return
	}

	result := s.stopSessionService(workspacePath)

	writeJSON(w, apiSessionActionResponse(result))
}

func (s *Server) handleAPIResetSession(w http.ResponseWriter, r *http.Request) {
	workspacePath, ok := s.resolveWorkspaceFromPath(w, r)
	if !ok {
		return
	}

	result, errReset := s.resetSessionService(workspacePath)
	if errReset != nil {
		statusCode := http.StatusInternalServerError
		if errors.Is(errReset, errSessionResetWhileRunning) {
			statusCode = http.StatusConflict
		}
		http.Error(w, errReset.Error(), statusCode)
		return
	}

	writeJSON(w, apiSessionActionResponse(result))
}

type apiForkRequest struct {
	GoalContent string `json:"goalContent"`
}

type apiForkResponse struct {
	Name      string `json:"name"`
	Dir       string `json:"dir"`
	Parent    string `json:"parent"`
	CreatedAt string `json:"createdAt"`
}

func (s *Server) handleAPIForkWorkspace(w http.ResponseWriter, r *http.Request) {
	workspacePath, ok := s.resolveWorkspaceFromPath(w, r)
	if !ok {
		return
	}

	var req apiForkRequest
	if errDecode := json.NewDecoder(r.Body).Decode(&req); errDecode != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	result, errFork := s.forkWorkspaceService(workspacePath, req.GoalContent)
	if errFork != nil {
		statusCode := http.StatusInternalServerError
		switch {
		case errors.Is(errFork, errForkOfFork):
			statusCode = http.StatusBadRequest
		case errors.Is(errFork, errGoalContentEmpty):
			statusCode = http.StatusBadRequest
		case errors.Is(errFork, errDirectoryExists):
			statusCode = http.StatusConflict
		}
		http.Error(w, errFork.Error(), statusCode)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(apiForkResponse(result)); err != nil {
		log.Println("failed to encode json response:", err)
	}
}

type apiDeleteForkRequest struct {
	ForkDir string `json:"forkDir"`
	Confirm bool   `json:"confirm"`
}

type apiDeleteForkResponse struct {
	Deleted bool   `json:"deleted"`
	Message string `json:"message"`
}

func (s *Server) handleAPIDeleteFork(w http.ResponseWriter, r *http.Request) {
	var req apiDeleteForkRequest
	if errDecode := json.NewDecoder(r.Body).Decode(&req); errDecode != nil && !errors.Is(errDecode, io.EOF) {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	workspacePath, statusCode, errMessage := s.resolveWorkspaceForAction(r.PathValue("name"))
	if statusCode != 0 {
		http.Error(w, errMessage, statusCode)
		return
	}

	rootPath := s.resolveRootForDeleteFork(workspacePath)
	if rootPath == "" {
		http.Error(w, "workspace is not a root or fork", http.StatusBadRequest)
		return
	}

	if !req.Confirm {
		http.Error(w, "confirmation required to delete fork", http.StatusBadRequest)
		return
	}

	forkDir := s.resolveForkDir(req.ForkDir, workspacePath, rootPath)
	if forkDir == "" {
		http.Error(w, "invalid fork directory", http.StatusBadRequest)
		return
	}

	if _, errStat := os.Stat(forkDir); errStat != nil {
		http.Error(w, "fork workspace not found", http.StatusBadRequest)
		return
	}

	if s.classifyWorkspaceCached(forkDir) != workspaceFork {
		http.Error(w, "fork workspace not found", http.StatusBadRequest)
		return
	}

	if resolveSymlinks(getRootWorkspacePath(forkDir)) != rootPath {
		http.Error(w, "fork does not belong to root", http.StatusBadRequest)
		return
	}

	result, errAction := s.executeWorkspaceAction(forkDir, workspaceOperationDelete)
	if errAction != nil {
		http.Error(w, errAction.Error(), statusForWorkspaceActionError(errAction))
		return
	}

	writeJSON(w, apiDeleteForkResponse{Deleted: result.Deleted, Message: result.Message})
}

func (s *Server) resolveRootForDeleteFork(workspacePath string) string {
	classification := s.classifyWorkspaceCached(workspacePath)
	switch classification {
	case workspaceRoot:
		return resolveSymlinks(workspacePath)
	case workspaceFork:
		return resolveSymlinks(getRootWorkspacePath(workspacePath))
	case workspaceStandalone:
		return ""
	default:
		return ""
	}
}

func (s *Server) resolveForkDir(requestForkDir, workspacePath, rootPath string) string {
	if requestForkDir != "" {
		if !filepath.IsAbs(requestForkDir) {
			return ""
		}
		absPath, errAbs := filepath.Abs(requestForkDir)
		if errAbs != nil {
			return ""
		}
		if _, errStat := os.Stat(absPath); errStat != nil {
			return ""
		}
		return filepath.Clean(absPath)
	}
	if workspacePath != rootPath {
		return workspacePath
	}
	return ""
}

type apiDeleteWorkspaceRequest struct {
	Confirm   bool   `json:"confirm"`
	Operation string `json:"operation,omitempty"`
}

type apiDeleteWorkspaceResponse struct {
	Deleted  bool   `json:"deleted"`
	Detached bool   `json:"detached"`
	Message  string `json:"message"`
}

func (s *Server) handleAPIDeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	var req apiDeleteWorkspaceRequest
	if errDecode := json.NewDecoder(r.Body).Decode(&req); errDecode != nil && !errors.Is(errDecode, io.EOF) {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	workspacePath, statusCode, errMessage := s.resolveWorkspaceForAction(r.PathValue("name"))
	if statusCode != 0 {
		http.Error(w, errMessage, statusCode)
		return
	}

	if !req.Confirm {
		http.Error(w, "confirmation required to delete workspace", http.StatusBadRequest)
		return
	}

	if _, errStat := os.Stat(workspacePath); errStat != nil {
		http.Error(w, "workspace directory not found", http.StatusNotFound)
		return
	}

	_, op, errResolve := s.resolveWorkspaceOperation(workspacePath, req.Operation)
	if errResolve != nil {
		http.Error(w, errResolve.Error(), statusForWorkspaceActionError(errResolve))
		return
	}

	result, errAction := s.executeWorkspaceAction(workspacePath, op)
	if errAction != nil {
		http.Error(w, errAction.Error(), statusForWorkspaceActionError(errAction))
		return
	}

	writeJSON(w, apiDeleteWorkspaceResponse{Deleted: result.Deleted, Detached: result.Detached, Message: result.Message})
}

type apiGoalResponse struct {
	Content string `json:"content"`
}

func (s *Server) handleAPIGetGoal(w http.ResponseWriter, r *http.Request) {
	workspacePath, ok := s.resolveGoalWorkspaceFromPath(w, r)
	if !ok {
		return
	}

	data, errRead := os.ReadFile(filepath.Join(workspacePath, "GOAL.md"))
	if errRead != nil {
		http.Error(w, "failed to read GOAL.md", http.StatusInternalServerError)
		return
	}

	writeJSON(w, apiGoalResponse{Content: string(data)})
}

type apiForkTemplateResponse struct {
	Content string `json:"content"`
}

func (s *Server) handleAPIForkTemplate(w http.ResponseWriter, r *http.Request) {
	workspacePath, ok := s.resolveWorkspaceFromPath(w, r)
	if !ok {
		return
	}

	if s.classifyWorkspaceCached(workspacePath) == workspaceFork {
		http.Error(w, "workspace is a fork workspace", http.StatusBadRequest)
		return
	}

	content := s.resolveForkTemplateContent(workspacePath)
	writeJSON(w, apiForkTemplateResponse{Content: content})
}

func (s *Server) resolveForkTemplateContent(workspaceDir string) string {
	groups, errScan := s.scanWorkspaceGroups()
	if errScan != nil {
		return goalExampleContent
	}

	for _, grp := range groups {
		if grp.Root.Directory != workspaceDir {
			continue
		}
		if len(grp.Forks) == 0 {
			return goalExampleContent
		}
		content := readNewestForkGoal(grp.Forks)
		if content != "" {
			return content
		}
		return goalExampleContent
	}
	return goalExampleContent
}

func readNewestForkGoal(forks []workspaceInfo) string {
	type forkWithTime struct {
		dir     string
		modTime time.Time
	}
	candidates := make([]forkWithTime, 0, len(forks))
	for _, fork := range forks {
		goalPath := filepath.Join(fork.Directory, "GOAL.md")
		info, errStat := os.Stat(goalPath)
		if errStat != nil {
			continue
		}
		candidates = append(candidates, forkWithTime{dir: fork.Directory, modTime: info.ModTime()})
	}
	slices.SortFunc(candidates, func(a, b forkWithTime) int {
		return b.modTime.Compare(a.modTime)
	})
	for _, c := range candidates {
		data, errRead := os.ReadFile(filepath.Join(c.dir, "GOAL.md"))
		if errRead != nil {
			continue
		}
		content := string(data)
		if strings.TrimSpace(content) != "" {
			return content
		}
	}
	return ""
}

type apiUpdateGoalRequest struct {
	Content string `json:"content"`
}

type apiUpdateGoalResponse struct {
	Updated   bool   `json:"updated"`
	Workspace string `json:"workspace"`
}

func (s *Server) handleAPIUpdateGoal(w http.ResponseWriter, r *http.Request) {
	workspacePath, ok := s.resolveGoalWorkspaceFromPath(w, r)
	if !ok {
		return
	}

	var req apiUpdateGoalRequest
	if errDecode := json.NewDecoder(r.Body).Decode(&req); errDecode != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Content == "" {
		http.Error(w, "content cannot be empty", http.StatusBadRequest)
		return
	}

	result, errUpdateGoal := s.updateGoalService(workspacePath, req.Content)
	if errUpdateGoal != nil {
		http.Error(w, "failed to write GOAL.md", http.StatusInternalServerError)
		return
	}

	writeJSON(w, apiUpdateGoalResponse(result))
}

type apiAdhocRequest struct {
	Prompt string `json:"prompt"`
	Model  string `json:"model"`
}

type apiAdhocResponse struct {
	Running bool   `json:"running"`
	Output  string `json:"output"`
	Message string `json:"message"`
}

func (s *Server) handleAPIAdhocStatus(w http.ResponseWriter, r *http.Request) {
	workspacePath, ok := s.resolveWorkspaceFromPath(w, r)
	if !ok {
		return
	}

	result := s.adhocStatusService(workspacePath)
	writeJSON(w, apiAdhocResponse(result))
}

func (s *Server) handleAPIAdhoc(w http.ResponseWriter, r *http.Request) {
	workspacePath, ok := s.resolveWorkspaceFromPath(w, r)
	if !ok {
		return
	}

	var req apiAdhocRequest
	if errDecode := json.NewDecoder(r.Body).Decode(&req); errDecode != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	result := s.adhocStartService(workspacePath, req.Prompt, req.Model)
	if result.Error != nil {
		http.Error(w, result.Error.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, apiAdhocResponse{
		Running: result.Running,
		Output:  result.Output,
		Message: result.Message,
	})
}

func buildAdhocArgs(modelSpec string) []string {
	baseModel, variant := parseModelAndVariant(modelSpec)
	args := []string{"run", "-m", baseModel, "--agent", "build", "--title", "adhoc [" + modelSpec + "]"}
	if variant != "" {
		args = append(args, "--variant", variant)
	}
	return args
}

func (s *Server) handleAPIAdhocStop(w http.ResponseWriter, r *http.Request) {
	workspacePath, ok := s.resolveWorkspaceFromPath(w, r)
	if !ok {
		return
	}

	result := s.adhocStopService(workspacePath)
	writeJSON(w, apiAdhocResponse(result))
}

func (s *Server) handleAPIWorkflowSVG(w http.ResponseWriter, r *http.Request) {
	workspacePath, ok := s.resolveWorkspaceFromPath(w, r)
	if !ok {
		return
	}

	wfState := s.workspaceCoordinator(workspacePath).State()
	currentAgent := wfState.CurrentAgent
	if currentAgent == "" {
		currentAgent = "Unknown"
	}

	svg := s.getWorkflowSVGCached(workspacePath, currentAgent)
	if svg == "" {
		http.Error(w, "workflow SVG not available", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "no-cache")
	if _, errWrite := w.Write([]byte(svg)); errWrite != nil {
		log.Println("failed to write workflow SVG:", errWrite)
	}
}

type apiSteerRequest struct {
	Message string `json:"message"`
}

type apiSteerResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func (s *Server) handleAPISteer(w http.ResponseWriter, r *http.Request) {
	workspacePath, ok := s.resolveWorkspaceFromPath(w, r)
	if !ok {
		return
	}

	var req apiSteerRequest
	if errDecode := json.NewDecoder(r.Body).Decode(&req); errDecode != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Message) == "" {
		http.Error(w, "message cannot be empty", http.StatusBadRequest)
		return
	}

	coord := s.workspaceCoordinator(workspacePath)
	steerBody := "Re-steering instruction: " + strings.TrimSpace(req.Message)
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
		http.Error(w, "failed to save state", http.StatusInternalServerError)
		return
	}

	s.notifyStateChange()

	writeJSON(w, apiSteerResponse{Success: true, Message: "steering instruction added"})
}

func findSteerInsertPosition(messages []state.Message) int {
	for i, msg := range messages {
		if !msg.Read {
			return i
		}
	}
	return 0
}

type apiTogglePinResponse struct {
	Pinned  bool   `json:"pinned"`
	Message string `json:"message"`
}

func (s *Server) handleAPITogglePin(w http.ResponseWriter, r *http.Request) {
	workspacePath, ok := s.resolveWorkspaceFromPath(w, r)
	if !ok {
		return
	}

	if errToggle := s.togglePin(workspacePath); errToggle != nil {
		http.Error(w, "failed to toggle pin", http.StatusInternalServerError)
		return
	}

	s.invalidateWorkspaceScanCache()
	s.notifyStateChange()

	pinned := s.isPinned(workspacePath)

	writeJSON(w, apiTogglePinResponse{
		Pinned:  pinned,
		Message: "pin toggled",
	})
}

type apiOpenEditorResponse struct {
	Opened  bool   `json:"opened"`
	Editor  string `json:"editor"`
	Message string `json:"message"`
}

func (s *Server) handleAPIOpenEditor(w http.ResponseWriter, r *http.Request) {
	workspacePath, ok := s.resolveWorkspaceFromPath(w, r)
	if !ok {
		return
	}

	if !s.editorAvailable {
		http.Error(w, "no editor available", http.StatusServiceUnavailable)
		return
	}

	if errOpen := s.editor.open(workspacePath); errOpen != nil {
		http.Error(w, fmt.Sprintf("failed to open editor: %v", errOpen), http.StatusInternalServerError)
		return
	}

	writeJSON(w, apiOpenEditorResponse{
		Opened:  true,
		Editor:  s.editorName,
		Message: "opened in editor",
	})
}

func (s *Server) handleAPIOpenEditorGoal(w http.ResponseWriter, r *http.Request) {
	s.openEditorForFile(w, r, "GOAL.md")
}

func (s *Server) handleAPIOpenEditorProjectManagement(w http.ResponseWriter, r *http.Request) {
	s.openEditorForFile(w, r, filepath.Join(".sgai", "PROJECT_MANAGEMENT.md"))
}

func (s *Server) openEditorForFile(w http.ResponseWriter, r *http.Request, relPath string) {
	workspacePath, ok := s.resolveWorkspaceFromPath(w, r)
	if !ok {
		return
	}

	if !s.editorAvailable {
		http.Error(w, "no editor available", http.StatusServiceUnavailable)
		return
	}

	targetPath := filepath.Join(workspacePath, relPath)
	if _, errStat := os.Stat(targetPath); errStat != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	if errOpen := s.editor.open(targetPath); errOpen != nil {
		http.Error(w, fmt.Sprintf("failed to open editor: %v", errOpen), http.StatusInternalServerError)
		return
	}

	writeJSON(w, apiOpenEditorResponse{
		Opened:  true,
		Editor:  s.editorName,
		Message: "opened in editor",
	})
}

type apiModelEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type apiModelsResponse struct {
	Models       []apiModelEntry `json:"models"`
	DefaultModel string          `json:"defaultModel,omitempty"`
}

type apiDeleteMessageResponse struct {
	Deleted bool   `json:"deleted"`
	ID      int    `json:"id"`
	Message string `json:"message"`
}

func (s *Server) handleAPIListModels(w http.ResponseWriter, r *http.Request) {
	workspaceName := r.URL.Query().Get("workspace")
	if workspaceName != "" {
		_, statusCode, errMessage := s.resolveSingleWorkspacePath(workspaceName)
		if statusCode == http.StatusConflict {
			http.Error(w, errMessage, statusCode)
			return
		}
	}

	validModels, errFetch := fetchValidModels()
	if errFetch != nil {
		log.Println("cannot fetch models:", errFetch)
		writeJSON(w, apiModelsResponse{Models: []apiModelEntry{}, DefaultModel: ""})
		return
	}

	modelNames := slices.Sorted(maps.Keys(validModels))
	entries := make([]apiModelEntry, 0, len(modelNames))
	for _, name := range modelNames {
		entries = append(entries, apiModelEntry{
			ID:   name,
			Name: name,
		})
	}

	defaultModel := s.coordinatorModelFromWorkspace(workspaceName)
	writeJSON(w, apiModelsResponse{Models: entries, DefaultModel: defaultModel})
}

func (s *Server) handleAPIDeleteMessage(w http.ResponseWriter, r *http.Request) {
	workspacePath, ok := s.resolveWorkspaceFromPath(w, r)
	if !ok {
		return
	}

	idStr := r.PathValue("id")
	if idStr == "" {
		http.Error(w, "message id is required", http.StatusBadRequest)
		return
	}

	messageID, errParseID := strconv.Atoi(idStr)
	if errParseID != nil {
		http.Error(w, "invalid message id", http.StatusBadRequest)
		return
	}

	deleteResult, errDelete := s.deleteMessageService(workspacePath, messageID)
	if errDelete != nil {
		if errors.Is(errDelete, errMessageNotFound) {
			http.Error(w, "message not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to save workspace state", http.StatusInternalServerError)
		return
	}

	writeJSON(w, apiDeleteMessageResponse(deleteResult))
}

func (s *Server) coordinatorModelFromWorkspace(workspace string) string {
	if workspace == "" {
		return ""
	}
	workspacePath := s.resolveWorkspaceNameToPath(workspace)
	if workspacePath == "" {
		return ""
	}
	models := modelsForAgentFromGoal(workspacePath, "coordinator")
	if len(models) == 0 {
		return ""
	}
	baseModel, _ := parseModelAndVariant(models[0])
	return baseModel
}

func resolveCurrentModel(workspacePath string, wfState *state.Workflow) string {
	if wfState.CurrentModel != "" {
		return wfState.CurrentModel
	}
	agent := wfState.CurrentAgent
	if agent == "" {
		return ""
	}
	models := modelsForAgentFromGoal(workspacePath, agent)
	if len(models) == 0 {
		return ""
	}
	return models[0]
}

func collectAgentModels(workspacePath string) []apiAgentModelEntry {
	goalPath := filepath.Join(workspacePath, "GOAL.md")
	goalData, errRead := os.ReadFile(goalPath)
	if errRead != nil {
		return nil
	}
	metadata, errParse := parseYAMLFrontmatter(goalData)
	if errParse != nil || len(metadata.Models) == 0 {
		return nil
	}
	agents := slices.Sorted(maps.Keys(metadata.Models))
	result := make([]apiAgentModelEntry, 0, len(agents))
	for _, agent := range agents {
		models := getModelsForAgent(metadata.Models, agent)
		if len(models) == 0 {
			continue
		}
		result = append(result, apiAgentModelEntry{
			Agent:  agent,
			Models: models,
		})
	}
	return result
}

type apiBrowseDirectoriesResponse struct {
	Path    string           `json:"path"`
	Entries []directoryEntry `json:"entries"`
}

func (s *Server) handleAPIBrowseDirectories(w http.ResponseWriter, r *http.Request) {
	browsePath := r.URL.Query().Get("path")
	entries, errBrowse := browseDirectoriesService(browsePath)
	if errBrowse != nil {
		http.Error(w, errBrowse.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, apiBrowseDirectoriesResponse{Path: browsePath, Entries: entries})
}

type apiAttachWorkspaceRequest struct {
	Path string `json:"path"`
}

type apiAttachWorkspaceResponse struct {
	Name    string `json:"name"`
	Dir     string `json:"dir"`
	HasGoal bool   `json:"hasGoal"`
}

func (s *Server) handleAPIAttachWorkspace(w http.ResponseWriter, r *http.Request) {
	var req apiAttachWorkspaceRequest
	if errDecode := json.NewDecoder(r.Body).Decode(&req); errDecode != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	result, errAttach := s.attachExternalWorkspaceService(req.Path)
	if errAttach != nil {
		statusCode := http.StatusInternalServerError
		switch {
		case errors.Is(errAttach, errPathNotAbsolute):
			statusCode = http.StatusBadRequest
		case errors.Is(errAttach, errNotADirectory):
			statusCode = http.StatusBadRequest
		case errors.Is(errAttach, errAlreadyAttached):
			statusCode = http.StatusConflict
		}
		http.Error(w, errAttach.Error(), statusCode)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(apiAttachWorkspaceResponse(result)); err != nil {
		log.Println("failed to encode json response:", err)
	}
}

type apiDetachWorkspaceRequest struct {
	Path string `json:"path"`
}

type apiDetachWorkspaceResponse struct {
	Detached bool   `json:"detached"`
	Message  string `json:"message"`
}

func (s *Server) handleAPIDetachWorkspace(w http.ResponseWriter, r *http.Request) {
	var req apiDetachWorkspaceRequest
	if errDecode := json.NewDecoder(r.Body).Decode(&req); errDecode != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	result, errDetach := s.detachExternalWorkspaceService(req.Path)
	if errDetach != nil {
		statusCode := statusForWorkspaceActionError(errDetach)
		http.Error(w, errDetach.Error(), statusCode)
		return
	}

	writeJSON(w, apiDetachWorkspaceResponse(result))
}

func statusForWorkspaceActionError(err error) int {
	switch {
	case errors.Is(err, errNotAttached):
		return http.StatusNotFound
	case errors.Is(err, errWorkspaceActionInvalidOperation), errors.Is(err, errWorkspaceActionOperationRequired):
		return http.StatusBadRequest
	case errors.Is(err, errWorkspaceActionRunning), errors.Is(err, errWorkspaceActionForksAttached), errors.Is(err, errWorkspaceActionNotAllowed):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
