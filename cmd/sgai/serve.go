package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"maps"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/adrg/xdg"
	"github.com/sandgardenhq/sgai/pkg/state"
	"github.com/yuin/goldmark"
	emoji "github.com/yuin/goldmark-emoji"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

//go:embed GOAL.example.md
var goalExampleContent string

var tmplFallbackSVG = template.Must(template.New("fallbackSVG").Parse(
	`<svg xmlns="http://www.w3.org/2000/svg" width="400" height="{{.Height}}" viewBox="0 0 400 {{.Height}}">
<rect width="100%" height="100%" fill="#f8fafc"/>
<text x="10" y="20" font-family="monospace" font-size="12" fill="#475569">{{range .Lines}}<tspan x="10" dy="{{.DY}}">{{.Text}}</tspan>{{end}}</text>
</svg>`))

type project struct {
	Directory    string
	DirName      string
	LastModified time.Time
	HasWorkspace bool
}

func scanForProjects(rootDir string) ([]project, error) {
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return nil, err
	}

	var projects []project
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		if name == "." || name == ".." {
			continue
		}

		dirPath := filepath.Join(rootDir, name)
		sgaiDir := filepath.Join(dirPath, ".sgai")
		goalPath := filepath.Join(dirPath, "GOAL.md")

		sgaiInfo, errStatSGAI := os.Stat(sgaiDir)
		hasWorkspace := errStatSGAI == nil && sgaiInfo.IsDir()

		_, errStatGoal := os.Stat(goalPath)
		hasGoalMD := errStatGoal == nil

		if !hasWorkspace && !hasGoalMD {
			continue
		}

		var modTime time.Time
		if hasWorkspace {
			modTime = sgaiInfo.ModTime()
		} else if hasGoalMD {
			if errMkdir := os.MkdirAll(sgaiDir, 0755); errMkdir == nil {
				hasWorkspace = true
			}
			entryInfo, errEntry := entry.Info()
			if errEntry == nil {
				modTime = entryInfo.ModTime()
			}
		}

		projects = append(projects, project{
			Directory:    dirPath,
			DirName:      entry.Name(),
			LastModified: modTime,
			HasWorkspace: hasWorkspace,
		})
	}

	slices.SortFunc(projects, func(a, b project) int {
		return strings.Compare(strings.ToLower(a.DirName), strings.ToLower(b.DirName))
	})

	return projects, nil
}

func stripFrontmatter(content string) string {
	sections, errSplit := splitFrontmatterSections([]byte(content))
	if errSplit != nil {
		return content
	}
	after := sections.after
	for bytes.HasPrefix(after, sections.lineEnding) {
		after = after[len(sections.lineEnding):]
	}
	return string(after)
}

type session struct {
	mu           sync.Mutex
	cancel       context.CancelFunc
	running      bool
	outputLog    *circularLogBuffer
	mcpCloseOnce sync.Once
	mcpCloseFn   func()
	coord        *state.Coordinator
}

type editorOpener interface {
	open(path string) error
}

type serverPaths struct {
	pinnedConfigDir   string
	externalConfigDir string
}

const defaultEditorPreset = "code"

// editorPreset defines a preset editor configuration with its command template
// and whether it runs in a terminal.
type editorPreset struct {
	command    string
	isTerminal bool
}

var editorPresets = map[string]editorPreset{
	"code":   {command: "code", isTerminal: false},
	"cursor": {command: "cursor", isTerminal: false},
	"zed":    {command: "zed", isTerminal: false},
	"subl":   {command: "subl", isTerminal: false},
	"idea":   {command: "idea", isTerminal: false},
	"emacs":  {command: "emacsclient -n", isTerminal: false},
	"nvim":   {command: "nvim", isTerminal: true},
	"vim":    {command: "vim", isTerminal: true},
	"atom":   {command: "atom", isTerminal: false},
}

// configurableEditor implements editorOpener with configurable editor support.
type configurableEditor struct {
	name       string
	command    string
	isTerminal bool
}

func (e *configurableEditor) open(path string) error {
	cmd := exec.Command(e.command, path)
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	if err := cmd.Run(); err == nil {
		return nil
	}
	fallback := exec.Command("code", path)
	fallback.Stderr = os.Stderr
	fallback.Stdin = os.Stdin
	fallback.Stdout = os.Stdout
	return fallback.Run()
}

func resolveEditor(configEditor string) (name, command string, isTerminal bool) {
	editorSpec := configEditor
	if editorSpec == "" {
		editorSpec = os.Getenv("VISUAL")
	}
	if editorSpec == "" {
		editorSpec = os.Getenv("EDITOR")
	}
	if editorSpec == "" {
		editorSpec = defaultEditorPreset
	}

	if preset, ok := editorPresets[editorSpec]; ok {
		return editorSpec, preset.command, preset.isTerminal
	}

	return editorSpec, editorSpec, false
}

func newConfigurableEditor(configEditor string) *configurableEditor {
	name, command, isTerminal := resolveEditor(configEditor)
	return &configurableEditor{
		name:       name,
		command:    command,
		isTerminal: isTerminal,
	}
}

func isEditorAvailable(command string) bool {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return false
	}
	_, err := exec.LookPath(parts[0])
	return err == nil
}

// Server handles HTTP requests for the sgai serve command.
type Server struct {
	mu                sync.Mutex
	sessions          map[string]*session
	everStartedDirs   map[string]bool
	pinnedDirs        map[string]bool
	pinnedConfigDir   string
	externalDirs      map[string]bool
	externalConfigDir string
	rootDir           string
	editorAvailable   bool
	isTerminalEditor  bool
	editorName        string
	editor            editorOpener
	shutdownCtx       context.Context

	signals *signalBroker

	adhocStates map[string]*adhocPromptState

	promptActionRunner func(workspacePath, prompt, model string) adhocStartResult
	scriptActionRunner func(workspacePath, actionName string, argv []string) adhocStartResult

	composerSessionsMu sync.Mutex
	composerSessions   map[string]*composerSession

	workspaceScanFlight singleflight[string, []workspaceGroup]
	workspaceScanCache  *ttlCache[string, []workspaceGroup]
	classifyFlight      singleflight[string, workspaceKind]
	classifyCache       *ttlCache[string, workspaceKind]
	svgFlight           singleflight[string, string]
	svgCache            *ttlCache[string, string]
	stateFlight         singleflight[string, apiFactoryState]
	stateCache          *ttlCache[string, apiFactoryState]
	stateGeneration     uint64

	goalTitleComposer      func(workspacePath string, goalContent []byte) (string, error)
	goalTitleReadFile      func(path string) ([]byte, error)
	goalTitleRepairMu      sync.Mutex
	goalTitleRepairQueue   []string
	goalTitleRepairQueued  map[string]struct{}
	goalTitleRepairRunning bool
}

// NewServer creates a new Server with the supplied config paths and editor configuration.
// NewServer fills any empty config path fields with the workspace-local rootDir/.sgai defaults before constructing the server.
func NewServer(rootDir string, paths serverPaths, editorConfig string) *Server {
	absRootDir, err := filepath.Abs(rootDir)
	if err != nil {
		absRootDir = rootDir
	}
	paths = normalizeServerPaths(absRootDir, paths)
	editor := newConfigurableEditor(editorConfig)
	editorAvail := isEditorAvailable(editor.command)
	if !editorAvail {
		fallback := newConfigurableEditor(defaultEditorPreset)
		if isEditorAvailable(fallback.command) {
			editor = fallback
			editorAvail = true
		}
	}
	return &Server{
		sessions:              make(map[string]*session),
		everStartedDirs:       make(map[string]bool),
		pinnedDirs:            make(map[string]bool),
		pinnedConfigDir:       paths.pinnedConfigDir,
		externalDirs:          make(map[string]bool),
		externalConfigDir:     paths.externalConfigDir,
		adhocStates:           make(map[string]*adhocPromptState),
		shutdownCtx:           context.Background(),
		signals:               newSignalBroker(),
		composerSessions:      make(map[string]*composerSession),
		rootDir:               absRootDir,
		editorAvailable:       editorAvail,
		isTerminalEditor:      editor.isTerminal,
		editorName:            editor.name,
		editor:                editor,
		workspaceScanCache:    newTTLCache[string, []workspaceGroup](3 * time.Second),
		classifyCache:         newTTLCache[string, workspaceKind](5 * time.Second),
		svgCache:              newTTLCache[string, string](10 * time.Second),
		stateCache:            newTTLCache[string, apiFactoryState](30 * time.Second),
		goalTitleComposer:     defaultGoalTitleComposer,
		goalTitleReadFile:     os.ReadFile,
		goalTitleRepairQueued: make(map[string]struct{}),
	}
}

func resolveServerPaths(configHome string) serverPaths {
	configDir := filepath.Join(configHome, "sgai")
	return serverPaths{
		pinnedConfigDir:   configDir,
		externalConfigDir: configDir,
	}
}

func workspaceServerPaths(rootDir string) serverPaths {
	configDir := filepath.Join(rootDir, ".sgai")
	return serverPaths{
		pinnedConfigDir:   configDir,
		externalConfigDir: configDir,
	}
}

func normalizeServerPaths(rootDir string, paths serverPaths) serverPaths {
	defaults := workspaceServerPaths(rootDir)
	if paths.pinnedConfigDir == "" {
		paths.pinnedConfigDir = defaults.pinnedConfigDir
	}
	if paths.externalConfigDir == "" {
		paths.externalConfigDir = defaults.externalConfigDir
	}
	return paths
}

func (s *Server) notifyStateChange() {
	s.mu.Lock()
	s.stateGeneration++
	s.mu.Unlock()
	s.stateCache.delete("state")
	s.signals.notify()
}

func (s *Server) validateDirectory(dir string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("directory is required")
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("invalid directory path: %w", err)
	}

	absRoot, err := filepath.Abs(s.rootDir)
	if err != nil {
		return "", fmt.Errorf("invalid root path: %w", err)
	}

	cleanDir := filepath.Clean(absDir)
	cleanRoot := filepath.Clean(absRoot)

	realRoot, err := filepath.EvalSymlinks(cleanRoot)
	if err != nil {
		realRoot = cleanRoot
	}

	realDir, err := filepath.EvalSymlinks(cleanDir)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("invalid directory path: %w", err)
		}
		parent := cleanDir
		var nonExistentParts []string
		for {
			parentDir := filepath.Dir(parent)
			if parentDir == parent {
				break
			}
			realParent, err := filepath.EvalSymlinks(parentDir)
			if err == nil {
				realDir = realParent
				for i := len(nonExistentParts) - 1; i >= 0; i-- {
					realDir = filepath.Join(realDir, nonExistentParts[i])
				}
				realDir = filepath.Join(realDir, filepath.Base(parent))
				break
			}
			nonExistentParts = append(nonExistentParts, filepath.Base(parent))
			parent = parentDir
		}
		if realDir == "" {
			realDir = cleanDir
		}
	}

	relPath, err := filepath.Rel(realRoot, realDir)
	if err != nil {
		return "", fmt.Errorf("path traversal denied")
	}

	if strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf("path traversal denied")
	}

	return cleanDir, nil
}

func statePath(dir string) string {
	return filepath.Join(dir, ".sgai", "state.json")
}

type startSessionResult struct {
	alreadyRunning bool
	sess           *session
	startError     error
}

func (s *Server) startSession(workspacePath string) startSessionResult {
	s.mu.Lock()
	sess := s.sessions[workspacePath]
	if sess != nil && sess.running {
		s.mu.Unlock()
		return startSessionResult{alreadyRunning: true, sess: sess}
	}

	sess = &session{running: true, outputLog: newCircularLogBuffer()}
	s.sessions[workspacePath] = sess
	s.everStartedDirs[workspacePath] = true
	s.mu.Unlock()

	coord, errCoord := state.NewCoordinator(statePath(workspacePath))
	if errCoord != nil && !os.IsNotExist(errCoord) {
		sess.mu.Lock()
		sess.running = false
		sess.mu.Unlock()
		return startSessionResult{startError: fmt.Errorf("creating coordinator: %w", errCoord)}
	}
	if errCoord != nil {
		coord = state.NewCoordinatorEmpty(statePath(workspacePath))
	}
	coord.OnUpdate(s.notifyStateChange)
	if errUpdate := coord.UpdateState(func(wf *state.Workflow) {
		wf.HumanMessage = ""
		wf.MultiChoiceQuestion = nil
	}); errUpdate != nil {
		sess.mu.Lock()
		sess.running = false
		sess.mu.Unlock()
		return startSessionResult{startError: fmt.Errorf("updating state: %w", errUpdate)}
	}
	sess.mu.Lock()
	sess.coord = coord
	sess.mu.Unlock()

	dagAgents := workspaceDagAgents(workspacePath)

	mcpURL, mcpCloseFn, errMCP := startMCPHTTPServer(workspacePath, coord, dagAgents)
	if errMCP != nil {
		sess.mu.Lock()
		sess.running = false
		sess.mu.Unlock()
		return startSessionResult{startError: errMCP}
	}
	sess.mu.Lock()
	sess.mcpCloseFn = mcpCloseFn
	sess.mu.Unlock()

	ctx, cancel := context.WithCancel(s.shutdownCtx)
	sess.mu.Lock()
	sess.cancel = cancel
	sess.mu.Unlock()

	logWriter := newSessionLogWriter(sess, workspacePath, s, filepath.Base(workspacePath))

	go func() {
		defer func() {
			sess.mcpCloseOnce.Do(mcpCloseFn)
			coord.Stop()
			sess.mu.Lock()
			sess.running = false
			sess.mu.Unlock()
			s.clearEverStartedOnCompletion(workspacePath)
			s.notifyStateChange()
		}()

		wfState := coord.State()
		switch wfState.InteractionMode {
		case state.ModeContinuous:
			continuousPrompt := readContinuousModePrompt(workspacePath)
			runContinuousWorkflow(ctx, workspacePath, continuousPrompt, mcpURL, logWriter, coord)
		default:
			runWorkflow(ctx, workspacePath, mcpURL, logWriter, coord)
		}
	}()

	return startSessionResult{sess: sess}
}

func (s *Server) stopSession(workspacePath string) {
	s.mu.Lock()
	sess := s.sessions[workspacePath]
	s.mu.Unlock()

	if sess != nil {
		sess.mu.Lock()
		if sess.cancel != nil {
			sess.cancel()
		}
		if sess.mcpCloseFn != nil {
			sess.mcpCloseOnce.Do(sess.mcpCloseFn)
		}
		sess.running = false
		sess.mu.Unlock()
	}

	s.resetHumanCommunication(workspacePath)
	s.notifyStateChange()
}

func badgeStatus(wfState state.Workflow, running bool) (class, text string) {
	if wfState.NeedsHumanInput() {
		return "badge-needs-input", "Needs Input"
	}
	if running || wfState.Status == state.StatusWorking || wfState.Status == state.StatusAgentDone {
		return "badge-running", "Running"
	}
	if !running && wfState.Status == state.StatusComplete {
		return "badge-complete", "Complete"
	}
	return "badge-stopped", "Stopped"
}

func dashboardBaseURL(listenAddr string) string {
	host, port, errSplit := net.SplitHostPort(listenAddr)
	if errSplit != nil {
		return "http://" + listenAddr
	}

	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}

	return "http://" + net.JoinHostPort(host, port)
}

func cmdServe(args []string) {
	serveFlags := flag.NewFlagSet("serve", flag.ExitOnError)
	listenAddr := serveFlags.String("listen-addr", "127.0.0.1:8080", "HTTP server listen address")
	serveFlags.Parse(args) //nolint:errcheck // ExitOnError FlagSet exits on error, never returns non-nil

	var rootDir string
	remainingArgs := serveFlags.Args()
	if len(remainingArgs) > 0 {
		rootDir = remainingArgs[0]
	} else {
		var err error
		rootDir, err = os.Getwd()
		if err != nil {
			log.Fatalf("failed to get working directory: %v", err)
		}
	}

	listener, errListen := net.Listen("tcp4", *listenAddr)
	if errListen != nil {
		log.Fatalln("failed to listen:", errListen)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	paths := resolveServerPaths(xdg.ConfigHome)
	srv := NewServer(rootDir, paths, "")
	srv.shutdownCtx = ctx
	if err := srv.loadPinnedProjects(); err != nil {
		log.Println("warning: failed to load pinned projects:", err)
	}
	if err := srv.loadExternalDirs(); err != nil {
		log.Println("warning: failed to load external dirs:", err)
	}
	srv.startStateWatcher()
	go srv.warmStateCache()

	mux := http.NewServeMux()
	srv.registerAPIRoutes(mux)
	mux.Handle("/mcp/external", buildExternalMCPHandler())
	handler := srv.spaMiddleware(mux)

	httpServer := &http.Server{Handler: handler}

	baseURL := dashboardBaseURL(listener.Addr().String())
	log.Println("sgai serve listening on", baseURL)

	go func() {
		if errServe := httpServer.Serve(listener); errServe != nil && !errors.Is(errServe, http.ErrServerClosed) {
			log.Fatalln("server error:", errServe)
		}
	}()

	startMenuBar(ctx, baseURL, srv, stop)
	if errClose := httpServer.Close(); errClose != nil {
		log.Println("http server close:", errClose)
	}
}

func renderDotToSVG(dotContent string) string {
	if dotContent == "" {
		return ""
	}
	dotPath, err := exec.LookPath("dot")
	if err != nil {
		return renderDotAsFallbackSVG(dotContent)
	}

	cmd := exec.Command(dotPath, "-Tsvg")
	cmd.Stdin = strings.NewReader(dotContent)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return renderDotAsFallbackSVG(dotContent)
	}
	return out.String()
}

func renderDotAsFallbackSVG(dotContent string) string {
	lines := linesWithTrailingEmpty(dotContent)
	height := max(20+len(lines)*16, 100)

	type lineData struct {
		DY   int
		Text string
	}
	var lineItems []lineData
	for i, line := range lines {
		dy := 16
		if i == 0 {
			dy = 0
		}
		lineItems = append(lineItems, lineData{DY: dy, Text: line})
	}

	var buf bytes.Buffer
	data := struct {
		Height int
		Lines  []lineData
	}{
		Height: height,
		Lines:  lineItems,
	}
	if err := tmplFallbackSVG.Execute(&buf, data); err != nil {
		return ""
	}
	return buf.String()
}

func linesWithTrailingEmpty(content string) []string {
	var lines []string
	for line := range strings.Lines(content) {
		lines = append(lines, strings.TrimSuffix(line, "\n"))
	}
	if content == "" || strings.HasSuffix(content, "\n") {
		lines = append(lines, "")
	}
	return lines
}

func getWorkflowSVG(dir string, currentAgent string) string {
	goalPath := filepath.Join(dir, "GOAL.md")
	goalData, err := os.ReadFile(goalPath)
	if err != nil {
		return ""
	}

	metadata, err := parseYAMLFrontmatter(goalData)
	if err != nil {
		return ""
	}

	d, err := parseFlow(metadata.Flow, dir)
	if err != nil {
		return ""
	}

	if retrospectiveEnabled(metadata) {
		d.injectRetrospectiveEdge()
	}

	dotContent := d.toDOT()

	if currentAgent != "" {
		dotContent = injectCurrentAgentStyle(dotContent, currentAgent)
	}
	dotContent = injectLightTheme(dotContent)

	return renderDotToSVG(dotContent)
}

func (s *Server) getWorkflowSVGCached(dir string, currentAgent string) string {
	cacheKey := dir + "|" + currentAgent
	if cached, ok := s.svgCache.get(cacheKey); ok {
		return cached
	}
	svg, _ := s.svgFlight.do(cacheKey, func() (string, error) {
		if cached, ok := s.svgCache.get(cacheKey); ok {
			return cached, nil
		}
		svg := getWorkflowSVG(dir, currentAgent)
		if svg != "" {
			s.svgCache.set(cacheKey, svg)
		}
		return svg, nil
	})
	return svg
}

func (s *Server) getWorkflowSVGHashCached(dir string, currentAgent string) string {
	svg := s.getWorkflowSVGCached(dir, currentAgent)
	if svg == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(svg))
	return hex.EncodeToString(hash[:8])
}

type eventsProgressDisplay struct {
	Timestamp       string
	FormattedTime   string
	Agent           string
	Description     string
	ShowDateDivider bool
	DateDivider     string
}

func formatProgressForDisplay(entries []state.ProgressEntry) []eventsProgressDisplay {
	result := make([]eventsProgressDisplay, 0, len(entries))
	var lastDateStr string

	for _, entry := range entries {
		parsedTime, err := time.Parse(time.RFC3339, entry.Timestamp)
		if err != nil {
			result = append(result, eventsProgressDisplay{
				Timestamp:     entry.Timestamp,
				FormattedTime: entry.Timestamp,
				Agent:         entry.Agent,
				Description:   entry.Description,
			})
			continue
		}

		formattedTime := parsedTime.Local().Format("3:04 PM")
		currentDateStr := parsedTime.Local().Format("Jan 2, 2006")

		showDateDivider := currentDateStr != lastDateStr
		if showDateDivider {
			lastDateStr = currentDateStr
		}

		result = append(result, eventsProgressDisplay{
			Timestamp:       entry.Timestamp,
			FormattedTime:   formattedTime,
			Agent:           entry.Agent,
			Description:     entry.Description,
			ShowDateDivider: showDateDivider,
			DateDivider:     currentDateStr,
		})
	}

	return result
}

func renderMarkdown(content []byte) (string, error) {
	var buf bytes.Buffer
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM, emoji.Emoji),
		goldmark.WithRendererOptions(html.WithHardWraps()),
	)
	if err := md.Convert(content, &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

type agentSequenceDisplay struct {
	Agent       string
	Model       string
	ElapsedTime string
	IsCurrent   bool
}

func prepareAgentSequenceDisplay(sequence []state.AgentSequenceEntry, running bool, lastActivityTime string, workspacePath string) []agentSequenceDisplay {
	now := time.Now().UTC()
	result := make([]agentSequenceDisplay, 0, len(sequence))

	var endTime time.Time
	if !running && lastActivityTime != "" {
		if parsed, err := time.Parse(time.RFC3339, lastActivityTime); err == nil {
			endTime = parsed
		}
	}

	for i, entry := range sequence {
		startTime, err := time.Parse(time.RFC3339, entry.StartTime)
		if err != nil {
			log.Println("prepareAgentSequenceDisplay: skipping entry with invalid timestamp:", entry.StartTime, err)
			continue
		}
		var elapsed time.Duration
		isLastEntry := i+1 >= len(sequence)
		switch {
		case entry.IsCurrent && running:
			elapsed = now.Sub(startTime)
		case !isLastEntry:
			nextStartTime, err := time.Parse(time.RFC3339, sequence[i+1].StartTime)
			if err != nil {
				elapsed = now.Sub(startTime)
			} else {
				elapsed = nextStartTime.Sub(startTime)
			}
		case running:
			elapsed = now.Sub(startTime)
		case !endTime.IsZero():
			elapsed = endTime.Sub(startTime)
		}
		elapsedStr := formatDuration(elapsed)
		var model string
		if workspacePath != "" {
			if models := modelsForAgentFromGoal(workspacePath, entry.Agent); len(models) > 0 {
				model = models[0]
			}
		}
		result = append(result, agentSequenceDisplay{
			Agent:       entry.Agent,
			Model:       model,
			ElapsedTime: elapsedStr,
			IsCurrent:   entry.IsCurrent,
		})
	}
	slices.Reverse(result)
	return result
}

func calculateTotalExecutionTime(sequence []state.AgentSequenceEntry, running bool, lastActivityTime string) string {
	if len(sequence) == 0 {
		return ""
	}

	startTime, err := time.Parse(time.RFC3339, sequence[0].StartTime)
	if err != nil {
		return ""
	}

	var endTime time.Time
	switch {
	case running:
		endTime = time.Now().UTC()
	case lastActivityTime != "":
		parsed, err := time.Parse(time.RFC3339, lastActivityTime)
		if err != nil {
			return ""
		}
		endTime = parsed
	default:
		return ""
	}

	elapsed := endTime.Sub(startTime)
	return formatDuration(elapsed)
}

func workspaceDagAgents(workspacePath string) []string {
	goalPath := filepath.Join(workspacePath, "GOAL.md")
	goalContent, errRead := os.ReadFile(goalPath)
	if errRead != nil {
		return nil
	}
	metadata, errParse := parseYAMLFrontmatter(goalContent)
	if errParse != nil {
		return nil
	}
	flowDag, errFlow := parseFlow(metadata.Flow, workspacePath)
	if errFlow != nil {
		return nil
	}
	if retrospectiveEnabled(metadata) {
		flowDag.injectRetrospectiveEdge()
	}
	return flowDag.allAgents()
}

func (s *Server) resetHumanCommunication(workspacePath string) {
	s.mu.Lock()
	sess := s.sessions[workspacePath]
	s.mu.Unlock()

	if sess == nil {
		return
	}
	sess.mu.Lock()
	coord := sess.coord
	sess.mu.Unlock()

	if coord == nil {
		return
	}
	if err := coord.UpdateState(func(wf *state.Workflow) {
		wf.HumanMessage = ""
		wf.MultiChoiceQuestion = nil
	}); err != nil {
		log.Println("failed to reset human communication state:", err)
	}
}

func extractSubject(body string) string {
	for _, line := range linesWithTrailingEmpty(body) {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return strings.TrimLeft(trimmed, "# ")
		}
	}
	return ""
}

func injectCurrentAgentStyle(dot, currentAgent string) string {
	agentLine := fmt.Sprintf(`    "%s"`, currentAgent)
	styledLine := fmt.Sprintf(`    "%s" [style=filled, fillcolor="#10b981", fontcolor=white]`, currentAgent)

	if !strings.Contains(dot, agentLine) {
		return dot
	}

	return strings.Replace(dot, agentLine, styledLine, 1)
}

func injectLightTheme(dot string) string {
	lightTheme := `    bgcolor="transparent"
    node [style=filled, fillcolor="#e2e8f0", fontcolor="#1e293b", color="#94a3b8"]
    edge [color="#64748b", fontcolor="#475569"]`

	braceIdx := strings.Index(dot, "{")
	if braceIdx == -1 {
		return dot
	}

	return dot[:braceIdx+1] + "\n" + lightTheme + dot[braceIdx+1:]
}

func getLatestProgress(progress []state.ProgressEntry) string {
	if len(progress) == 0 {
		return "-"
	}
	return progress[len(progress)-1].Description
}

func getLastActivityTime(progress []state.ProgressEntry) string {
	if len(progress) == 0 {
		return ""
	}
	return progress[len(progress)-1].Timestamp
}

type workspaceInfo struct {
	Directory    string
	DirName      string
	IsRoot       bool
	Running      bool
	NeedsInput   bool
	InProgress   bool
	Pinned       bool
	HasWorkspace bool
	External     bool
}

type workspaceGroup struct {
	Root  workspaceInfo
	Forks []workspaceInfo
}

type workspaceKind string

const (
	workspaceStandalone workspaceKind = "standalone"
	workspaceRoot       workspaceKind = "root"
	workspaceFork       workspaceKind = "fork"
)

func classifyWorkspace(dir string) workspaceKind {
	repoPath := filepath.Join(dir, ".jj", "repo")
	info, errStat := os.Stat(repoPath)
	if errStat != nil {
		return workspaceStandalone
	}
	if !info.IsDir() {
		return workspaceFork
	}
	cmd := exec.Command("jj", "workspace", "list")
	cmd.Dir = dir
	output, errExec := cmd.Output()
	if errExec != nil {
		return workspaceStandalone
	}
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return workspaceStandalone
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) > 1 {
		return workspaceRoot
	}
	return workspaceStandalone
}

func (s *Server) classifyWorkspaceCached(dir string) workspaceKind {
	canonicalDir := resolveSymlinks(dir)
	if kind, ok := s.classifyCache.get(dir); ok {
		return kind
	}
	if kind, ok := s.classifyCache.get(canonicalDir); ok {
		return kind
	}
	cacheKey := dir
	if canonicalDir != "" {
		cacheKey = canonicalDir
	}
	kind, _ := s.classifyFlight.do(cacheKey, func() (workspaceKind, error) {
		if kind, ok := s.classifyCache.get(dir); ok {
			return kind, nil
		}
		if kind, ok := s.classifyCache.get(canonicalDir); ok {
			return kind, nil
		}
		lookupDir := dir
		if canonicalDir != "" {
			lookupDir = canonicalDir
		}
		kind := classifyWorkspace(lookupDir)
		s.classifyCache.set(dir, kind)
		if canonicalDir != dir {
			s.classifyCache.set(canonicalDir, kind)
		}
		return kind, nil
	})
	return kind
}

func hassgaiDirectory(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".sgai"))
	return err == nil && info.IsDir()
}

func hasJJRepo(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".jj"))
	return err == nil && info.IsDir()
}

func getRootWorkspacePath(forkDir string) string {
	repoPath := filepath.Join(forkDir, ".jj", "repo")
	content, err := os.ReadFile(repoPath)
	if err != nil {
		return ""
	}
	rootPath := strings.TrimSpace(string(content))
	if rootPath == "" {
		return ""
	}
	if !filepath.IsAbs(rootPath) {
		absPath, err := filepath.Abs(filepath.Join(forkDir, ".jj", rootPath))
		if err != nil {
			return ""
		}
		rootPath = absPath
	}
	jjDir := filepath.Dir(rootPath)
	rootDir := filepath.Dir(jjDir)
	if _, err := os.Stat(jjDir); os.IsNotExist(err) {
		return ""
	}
	return rootDir
}

func (s *Server) workspaceCoordinator(workspacePath string) *state.Coordinator {
	s.mu.Lock()
	sess := s.sessions[workspacePath]
	s.mu.Unlock()
	if sess != nil {
		sess.mu.Lock()
		coord := sess.coord
		running := sess.running
		sess.mu.Unlock()
		if running && coord != nil {
			return coord
		}
	}
	coord, errCoord := state.NewCoordinator(statePath(workspacePath))
	if errCoord != nil {
		return state.NewCoordinatorEmpty(statePath(workspacePath))
	}
	return coord
}

func (s *Server) getWorkspaceStatus(dir string) (running bool, needsInput bool) {
	s.mu.Lock()
	sess := s.sessions[dir]
	s.mu.Unlock()
	if sess != nil {
		sess.mu.Lock()
		running = sess.running
		sess.mu.Unlock()
	}

	wfState := s.workspaceCoordinator(dir).State()
	needsInput = wfState.NeedsHumanInput()
	return running, needsInput
}

func (s *Server) createWorkspaceInfo(dir, dirName string, isRoot, hasWorkspace, external bool) workspaceInfo {
	running, needsInput := s.getWorkspaceStatus(dir)
	pinned := s.isPinned(dir)
	inProgress := running || needsInput || s.wasEverStarted(dir) || pinned

	return workspaceInfo{
		Directory:    dir,
		DirName:      dirName,
		IsRoot:       isRoot,
		Running:      running,
		NeedsInput:   needsInput,
		InProgress:   inProgress,
		Pinned:       pinned,
		HasWorkspace: hasWorkspace,
		External:     external,
	}
}

func (s *Server) wasEverStarted(dir string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.everStartedDirs[dir]
}

func (s *Server) clearEverStartedOnCompletion(dir string) {
	wfState := s.workspaceCoordinator(dir).State()
	if wfState.Status != state.StatusComplete {
		return
	}
	s.mu.Lock()
	delete(s.everStartedDirs, dir)
	s.mu.Unlock()
}

func (s *Server) pinnedFilePath() string {
	return filepath.Join(s.pinnedConfigDir, "pinned.json")
}

func resolveSymlinks(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

func (s *Server) loadPinnedProjects() error {
	log.Println("pinned repositories file path:", s.pinnedFilePath())
	data, errRead := os.ReadFile(s.pinnedFilePath())
	if errRead != nil {
		if os.IsNotExist(errRead) {
			return nil
		}
		return fmt.Errorf("reading pinned projects: %w", errRead)
	}
	var dirs []string
	if errJSON := json.Unmarshal(data, &dirs); errJSON != nil {
		return fmt.Errorf("parsing pinned projects: %w", errJSON)
	}
	existing := make(map[string]bool, len(dirs))
	for _, d := range dirs {
		if _, errStat := os.Stat(d); errStat == nil {
			existing[resolveSymlinks(d)] = true
		} else if os.IsNotExist(errStat) {
			log.Println("pruning stale pinned path:", d)
		} else {
			log.Println("warning: cannot verify pinned path:", d, errStat)
			existing[resolveSymlinks(d)] = true
		}
	}
	s.mu.Lock()
	s.pinnedDirs = existing
	s.mu.Unlock()
	if len(existing) < len(dirs) {
		return s.savePinnedProjects()
	}
	return nil
}

func (s *Server) savePinnedProjects() error {
	s.mu.Lock()
	dirs := slices.Collect(maps.Keys(s.pinnedDirs))
	s.mu.Unlock()
	if dirs == nil {
		dirs = []string{}
	}
	slices.Sort(dirs)
	if errDir := os.MkdirAll(s.pinnedConfigDir, 0o755); errDir != nil {
		return fmt.Errorf("creating pin config directory: %w", errDir)
	}
	data, errJSON := json.Marshal(dirs)
	if errJSON != nil {
		return fmt.Errorf("encoding pinned projects: %w", errJSON)
	}
	if errWrite := os.WriteFile(s.pinnedFilePath(), data, 0o644); errWrite != nil {
		return fmt.Errorf("writing pinned projects: %w", errWrite)
	}
	return nil
}

func (s *Server) isPinned(dir string) bool {
	canonical := resolveSymlinks(dir)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pinnedDirs[canonical]
}

func (s *Server) togglePin(dir string) error {
	canonical := resolveSymlinks(dir)
	s.mu.Lock()
	if s.pinnedDirs[canonical] {
		delete(s.pinnedDirs, canonical)
	} else {
		s.pinnedDirs[canonical] = true
	}
	s.mu.Unlock()
	return s.savePinnedProjects()
}

func (s *Server) scanWorkspaceGroups() ([]workspaceGroup, error) {
	if cached, ok := s.workspaceScanCache.get("scan"); ok {
		return cached, nil
	}
	return s.workspaceScanFlight.do("scan", func() ([]workspaceGroup, error) {
		if cached, ok := s.workspaceScanCache.get("scan"); ok {
			return cached, nil
		}
		result, err := s.doScanWorkspaceGroups()
		if err == nil {
			s.workspaceScanCache.set("scan", result)
		}
		return result, err
	})
}

func (s *Server) invalidateWorkspaceScanCache() {
	s.workspaceScanCache.delete("scan")
}

func (s *Server) doScanWorkspaceGroups() ([]workspaceGroup, error) {
	s.mu.Lock()
	externalDirsCopy := maps.Clone(s.externalDirs)
	s.mu.Unlock()

	if len(externalDirsCopy) == 0 {
		return nil, nil
	}

	type attachedWorkspace struct {
		directory    string
		resolvedDir  string
		dirName      string
		hasWorkspace bool
		kind         workspaceKind
	}

	attached := make([]attachedWorkspace, 0, len(externalDirsCopy))
	for dir := range externalDirsCopy {
		if _, errStat := os.Stat(dir); errStat != nil {
			return nil, fmt.Errorf("stat attached workspace: %w", errStat)
		}
		resolvedDir := resolveSymlinks(dir)
		attached = append(attached, attachedWorkspace{
			directory:    dir,
			resolvedDir:  resolvedDir,
			dirName:      filepath.Base(dir),
			hasWorkspace: hassgaiDirectory(dir),
			kind:         s.classifyWorkspaceCached(dir),
		})
	}

	slices.SortFunc(attached, func(a, b attachedWorkspace) int {
		return strings.Compare(strings.ToLower(a.dirName), strings.ToLower(b.dirName))
	})

	rootMap := make(map[string]*workspaceGroup)
	var standaloneGroups []workspaceGroup

	for _, ws := range attached {
		if ws.kind != workspaceRoot {
			continue
		}
		rootMap[ws.resolvedDir] = &workspaceGroup{
			Root: s.createWorkspaceInfo(ws.directory, ws.dirName, true, ws.hasWorkspace, true),
		}
	}

	for _, ws := range attached {
		switch ws.kind {
		case workspaceFork:
			rootPath := resolveSymlinks(getRootWorkspacePath(ws.directory))
			if rootPath == "" || !externalDirsCopy[rootPath] {
				standaloneGroups = append(standaloneGroups, workspaceGroup{
					Root: s.createWorkspaceInfo(ws.directory, ws.dirName, false, ws.hasWorkspace, true),
				})
				continue
			}
			grp, exists := rootMap[rootPath]
			if !exists {
				grp = &workspaceGroup{
					Root: s.createWorkspaceInfo(rootPath, filepath.Base(rootPath), true, hassgaiDirectory(rootPath), true),
				}
				rootMap[rootPath] = grp
			}
			grp.Forks = append(grp.Forks, s.createWorkspaceInfo(ws.directory, ws.dirName, false, ws.hasWorkspace, true))
		case workspaceStandalone:
			standaloneGroups = append(standaloneGroups, workspaceGroup{
				Root: s.createWorkspaceInfo(ws.directory, ws.dirName, false, ws.hasWorkspace, true),
			})
		}
	}

	for resolvedDir, grp := range rootMap {
		if len(grp.Forks) > 0 {
			continue
		}
		standaloneGroups = append(standaloneGroups, workspaceGroup{
			Root: s.createWorkspaceInfo(grp.Root.Directory, grp.Root.DirName, false, grp.Root.HasWorkspace, true),
		})
		delete(rootMap, resolvedDir)
	}

	var groups []workspaceGroup
	for _, grp := range rootMap {
		groups = append(groups, *grp)
	}
	groups = append(groups, standaloneGroups...)

	slices.SortFunc(groups, func(a, b workspaceGroup) int {
		return strings.Compare(strings.ToLower(a.Root.DirName), strings.ToLower(b.Root.DirName))
	})

	return groups, nil
}

func (s *Server) resolveWorkspaceNameToPath(workspaceName string) string {
	if workspaceName == "" {
		return ""
	}

	groups, err := s.scanWorkspaceGroups()
	if err != nil {
		return ""
	}

	for _, grp := range groups {
		if grp.Root.DirName == workspaceName {
			return grp.Root.Directory
		}
		for _, fork := range grp.Forks {
			if fork.DirName == workspaceName {
				return fork.Directory
			}
		}
	}

	return ""
}

type modelStatusDisplay struct {
	ModelID string
	Status  string
}

func orderedModelStatuses(dir string, modelStatuses map[string]string) []modelStatusDisplay {
	if len(modelStatuses) == 0 {
		return nil
	}

	modelOrder := modelsForAgentFromGoal(dir, "project-critic-council")
	ordered := make([]modelStatusDisplay, 0, len(modelStatuses))
	used := make(map[string]bool)

	for _, modelSpec := range modelOrder {
		modelID := formatModelID("project-critic-council", modelSpec)
		status, ok := modelStatuses[modelID]
		if !ok {
			continue
		}
		ordered = append(ordered, modelStatusDisplay{ModelID: modelID, Status: status})
		used[modelID] = true
	}

	remaining := make([]string, 0, len(modelStatuses))
	for modelID := range modelStatuses {
		if used[modelID] {
			continue
		}
		remaining = append(remaining, modelID)
	}
	if len(remaining) == 0 {
		return ordered
	}
	if len(ordered) == 0 {
		ordered = make([]modelStatusDisplay, 0, len(modelStatuses))
	}
	if len(remaining) > 1 {
		slices.Sort(remaining)
	}
	for _, modelID := range remaining {
		ordered = append(ordered, modelStatusDisplay{ModelID: modelID, Status: modelStatuses[modelID]})
	}
	return ordered
}

func modelsForAgentFromGoal(dir, agent string) []string {
	goalPath := filepath.Join(dir, "GOAL.md")
	goalData, errRead := os.ReadFile(goalPath)
	if errRead != nil {
		return nil
	}
	metadata, errParse := parseYAMLFrontmatter(goalData)
	if errParse != nil {
		return nil
	}
	return getModelsForAgent(metadata.Models, agent)
}

func initializeWorkspace(workspacePath string) error {
	if err := unpackSkeleton(workspacePath); err != nil {
		return fmt.Errorf("unpacking skeleton: %w", err)
	}
	if err := initializeJJ(workspacePath); err != nil {
		return fmt.Errorf("initializing jj: %w", err)
	}
	if err := addGitExclude(workspacePath); err != nil {
		return fmt.Errorf("adding git exclude: %w", err)
	}
	if err := writeGoalExample(workspacePath); err != nil {
		return fmt.Errorf("writing GOAL.md: %w", err)
	}
	return nil
}

func unpackSkeleton(workspacePath string) error {
	subFS, err := fs.Sub(skelFS, "skel")
	if err != nil {
		return fmt.Errorf("accessing skeleton subdirectory: %w", err)
	}
	return fs.WalkDir(subFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		outPath := filepath.Join(workspacePath, path)
		if err := rejectSymlinkedWorkspacePath(workspacePath, outPath); err != nil {
			return err
		}
		if d.IsDir() {
			if err := os.MkdirAll(outPath, 0755); err != nil {
				return err
			}
			return nil
		}
		data, errRead := fs.ReadFile(subFS, path)
		if errRead != nil {
			return errRead
		}
		return os.WriteFile(outPath, data, 0644)
	})
}

func gitMetadataDirForWorkspace(dir string) (string, error) {
	gitPath := filepath.Join(dir, ".git")
	gitInfo, errStat := os.Lstat(gitPath)
	if os.IsNotExist(errStat) {
		return "", nil
	}
	if errStat != nil {
		return "", fmt.Errorf("statting .git: %w", errStat)
	}
	if gitInfo.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("statting .git: symlinked .git entry is not allowed")
	}
	if gitInfo.IsDir() {
		return gitPath, nil
	}

	content, errRead := os.ReadFile(gitPath)
	if errRead != nil {
		return "", fmt.Errorf("reading .git file: %w", errRead)
	}

	gitDir, errGitDir := gitDirFromFile(dir, content)
	if errGitDir != nil {
		return "", errGitDir
	}
	return gitDir, nil
}

func gitDirFromFile(workspaceDir string, content []byte) (string, error) {
	line := strings.TrimSpace(string(content))
	if !strings.HasPrefix(line, "gitdir:") {
		return "", fmt.Errorf("parsing .git file: missing gitdir prefix")
	}

	rawGitDir := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
	if rawGitDir == "" {
		return "", fmt.Errorf("parsing .git file: missing gitdir path")
	}

	gitDir, errGitDir := resolveGitPointerPath(workspaceDir, rawGitDir)
	if errGitDir != nil {
		return "", fmt.Errorf("parsing .git file: %w", errGitDir)
	}
	resolvedGitDir, errResolvedGitDir := resolveGitMetadataPath(gitDir)
	if errResolvedGitDir != nil {
		return "", fmt.Errorf("parsing .git file: %w", errResolvedGitDir)
	}

	withinWorkspace, errWithinWorkspace := gitDirWithinWorkspace(workspaceDir, resolvedGitDir)
	if errWithinWorkspace != nil {
		return "", fmt.Errorf("parsing .git file: %w", errWithinWorkspace)
	}
	if withinWorkspace {
		return resolvedGitDir, nil
	}

	allowed, errAllowed := gitDirReferencesWorkspaceGitFile(workspaceDir, resolvedGitDir)
	if errAllowed != nil {
		return "", fmt.Errorf("parsing .git file: %w", errAllowed)
	}
	if allowed {
		return resolvedGitDir, nil
	}

	return "", fmt.Errorf("parsing .git file: gitdir path escapes repository metadata boundary")
}

func resolveGitPointerPath(baseDir, rawPath string) (string, error) {
	if filepath.IsAbs(rawPath) {
		return filepath.Clean(rawPath), nil
	}

	basePath, errBasePath := filepath.Abs(baseDir)
	if errBasePath != nil {
		return "", fmt.Errorf("resolving base path: %w", errBasePath)
	}
	return filepath.Clean(filepath.Join(basePath, rawPath)), nil
}

func resolveGitMetadataPath(path string) (string, error) {
	absPath, errAbsPath := filepath.Abs(path)
	if errAbsPath != nil {
		return "", fmt.Errorf("resolving metadata path: %w", errAbsPath)
	}

	resolvedPath, errResolvedPath := filepath.EvalSymlinks(absPath)
	if errResolvedPath == nil {
		return filepath.Clean(resolvedPath), nil
	}
	if !os.IsNotExist(errResolvedPath) {
		return "", fmt.Errorf("resolving metadata path: %w", errResolvedPath)
	}

	parentPath := filepath.Dir(absPath)
	resolvedParentPath, errResolvedParentPath := filepath.EvalSymlinks(parentPath)
	if errResolvedParentPath != nil {
		return "", fmt.Errorf("resolving metadata path: %w", errResolvedParentPath)
	}
	return filepath.Join(filepath.Clean(resolvedParentPath), filepath.Base(absPath)), nil
}

func gitDirWithinWorkspace(workspaceDir, gitDir string) (bool, error) {
	workspacePath, errWorkspacePath := resolveGitMetadataPath(workspaceDir)
	if errWorkspacePath != nil {
		return false, fmt.Errorf("resolving workspace path: %w", errWorkspacePath)
	}

	relPath, errRelPath := filepath.Rel(filepath.Clean(workspacePath), filepath.Clean(gitDir))
	if errRelPath != nil {
		return false, fmt.Errorf("checking workspace boundary: %w", errRelPath)
	}
	return relPath == "." || (!strings.HasPrefix(relPath, "..") && relPath != ".."), nil
}

func gitDirReferencesWorkspaceGitFile(workspaceDir, gitDir string) (bool, error) {
	metadataBoundary, ok := worktreeMetadataBoundary(gitDir)
	if !ok {
		return false, nil
	}

	backReference, errBackReference := gitWorktreeBackReferencePath(gitDir)
	if os.IsNotExist(errBackReference) {
		return false, nil
	}
	if errBackReference != nil {
		return false, fmt.Errorf("reading gitdir back-reference: %w", errBackReference)
	}

	workspaceGitFile, errWorkspaceGitFile := filepath.Abs(filepath.Join(workspaceDir, ".git"))
	if errWorkspaceGitFile != nil {
		return false, fmt.Errorf("resolving workspace git path: %w", errWorkspaceGitFile)
	}
	if filepath.Clean(backReference) != filepath.Clean(workspaceGitFile) {
		return false, nil
	}

	commonDir, errCommonDir := gitWorktreeCommonDir(gitDir)
	if os.IsNotExist(errCommonDir) {
		return false, nil
	}
	if errCommonDir != nil {
		return false, fmt.Errorf("reading commondir boundary: %w", errCommonDir)
	}

	if filepath.Clean(commonDir) != filepath.Clean(metadataBoundary) {
		return false, nil
	}

	validMetadataLayout, errValidMetadataLayout := gitCommonDirLooksValid(metadataBoundary)
	if errValidMetadataLayout != nil {
		return false, fmt.Errorf("checking common git metadata: %w", errValidMetadataLayout)
	}
	return validMetadataLayout, nil
}

func addGitExclude(dir string) error {
	gitDir, errGitDir := gitMetadataDirForWorkspace(dir)
	if errGitDir != nil {
		return errGitDir
	}
	if gitDir == "" {
		return nil
	}

	gitInfoDir := filepath.Join(gitDir, "info")
	if err := rejectSymlinkedGitMetadataPath(gitDir, gitInfoDir); err != nil {
		return fmt.Errorf("checking .git/info directory: %w", err)
	}
	if err := os.MkdirAll(gitInfoDir, 0755); err != nil {
		return fmt.Errorf("creating .git/info directory: %w", err)
	}

	excludePath := filepath.Join(gitInfoDir, "exclude")
	if err := rejectSymlinkedGitMetadataPath(gitDir, excludePath); err != nil {
		return fmt.Errorf("checking .git/info/exclude: %w", err)
	}
	existingContent, err := os.ReadFile(excludePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading .git/info/exclude: %w", err)
	}
	if dotSGAILinePresent(existingContent) {
		return nil
	}
	if len(existingContent) > 0 && existingContent[len(existingContent)-1] != '\n' {
		existingContent = append(existingContent, '\n')
	}
	existingContent = append(existingContent, []byte("/.sgai\n")...)
	if err := os.WriteFile(excludePath, existingContent, 0644); err != nil {
		return fmt.Errorf("writing .git/info/exclude: %w", err)
	}
	return nil
}

func gitWorktreeBackReferencePath(gitDir string) (string, error) {
	content, errRead := readGitMetadataFile(filepath.Join(gitDir, "gitdir"), "gitdir back-reference")
	if errRead != nil {
		return "", errRead
	}

	backReference, errBackReference := resolveGitPointerPath(gitDir, strings.TrimSpace(string(content)))
	if errBackReference != nil {
		return "", fmt.Errorf("resolving gitdir back-reference: %w", errBackReference)
	}
	return filepath.Clean(backReference), nil
}

func gitWorktreeCommonDir(gitDir string) (string, error) {
	content, errRead := readGitMetadataFile(filepath.Join(gitDir, "commondir"), "commondir")
	if errRead != nil {
		return "", errRead
	}

	commonDir, errCommonDir := resolveGitPointerPath(gitDir, strings.TrimSpace(string(content)))
	if errCommonDir != nil {
		return "", fmt.Errorf("resolving commondir: %w", errCommonDir)
	}

	resolvedCommonDir, errResolvedCommonDir := resolveGitMetadataPath(commonDir)
	if errResolvedCommonDir != nil {
		return "", fmt.Errorf("resolving commondir: %w", errResolvedCommonDir)
	}
	return resolvedCommonDir, nil
}

func worktreeMetadataBoundary(gitDir string) (string, bool) {
	worktreesDir := filepath.Dir(gitDir)
	if filepath.Base(worktreesDir) != "worktrees" {
		return "", false
	}

	metadataBoundary := filepath.Dir(worktreesDir)
	if filepath.Base(metadataBoundary) != ".git" {
		return "", false
	}

	return metadataBoundary, true
}

func gitCommonDirLooksValid(path string) (bool, error) {
	hasHeadFile, errHasHeadFile := gitMetadataRegularFileExists(filepath.Join(path, "HEAD"))
	if errHasHeadFile != nil {
		return false, errHasHeadFile
	}
	if !hasHeadFile {
		return false, nil
	}

	hasObjectsDir, errHasObjectsDir := gitMetadataDirectoryExists(filepath.Join(path, "objects"))
	if errHasObjectsDir != nil {
		return false, errHasObjectsDir
	}
	if !hasObjectsDir {
		return false, nil
	}

	hasRefsDir, errHasRefsDir := gitMetadataDirectoryExists(filepath.Join(path, "refs"))
	if errHasRefsDir != nil {
		return false, errHasRefsDir
	}
	if !hasRefsDir {
		return false, nil
	}

	return true, nil
}

func gitMetadataRegularFileExists(path string) (bool, error) {
	info, errLstat := os.Lstat(path)
	if os.IsNotExist(errLstat) {
		return false, nil
	}
	if errLstat != nil {
		return false, errLstat
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("symlinked path is not allowed: %s", path)
	}
	if info.IsDir() {
		return false, nil
	}
	return true, nil
}

func gitMetadataDirectoryExists(path string) (bool, error) {
	info, errLstat := os.Lstat(path)
	if os.IsNotExist(errLstat) {
		return false, nil
	}
	if errLstat != nil {
		return false, errLstat
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("symlinked path is not allowed: %s", path)
	}
	return info.IsDir(), nil
}

func readGitMetadataFile(path, name string) ([]byte, error) {
	info, errLstat := os.Lstat(path)
	if errLstat != nil {
		return nil, errLstat
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("symlinked %s is not allowed", name)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s must be a file", name)
	}

	content, errRead := os.ReadFile(path)
	if errRead != nil {
		return nil, errRead
	}
	return content, nil
}

func rejectSymlinkedGitMetadataPath(rootPath, dstPath string) error {
	return rejectSymlinkedPath(rootPath, dstPath, "repository metadata boundary", "destination")
}

func rejectSymlinkedPath(rootPath, dstPath, boundaryLabel, pathLabel string) error {
	absRootPath, errAbsRootPath := filepath.Abs(rootPath)
	if errAbsRootPath != nil {
		return fmt.Errorf("resolving root path: %w", errAbsRootPath)
	}

	absDstPath, errAbsDstPath := filepath.Abs(dstPath)
	if errAbsDstPath != nil {
		return fmt.Errorf("resolving %s path: %w", pathLabel, errAbsDstPath)
	}

	relPath, errRelPath := filepath.Rel(absRootPath, absDstPath)
	if errRelPath != nil {
		return fmt.Errorf("checking %s path: %w", pathLabel, errRelPath)
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("checking %s path: %s escapes %s", pathLabel, dstPath, boundaryLabel)
	}

	currentPath := absRootPath
	for _, pathPart := range strings.Split(relPath, string(os.PathSeparator)) {
		if pathPart == "." || pathPart == "" {
			continue
		}
		currentPath = filepath.Join(currentPath, pathPart)
		pathInfo, errLstat := os.Lstat(currentPath)
		if os.IsNotExist(errLstat) {
			return nil
		}
		if errLstat != nil {
			return fmt.Errorf("checking %s path: %w", pathLabel, errLstat)
		}
		if pathInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("checking %s path: symlinked path is not allowed: %s", pathLabel, currentPath)
		}
	}

	return nil
}

func writeGoalExample(dir string) error {
	goalPath := filepath.Join(dir, "GOAL.md")
	return os.WriteFile(goalPath, []byte(goalExampleContent), 0644)
}

func validateWorkspaceName(name string) string {
	if name == "" {
		return "workspace name is required"
	}
	if strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return "workspace name cannot contain path separators or '..'"
	}
	for _, ch := range name {
		isValid := (ch >= 'a' && ch <= 'z') ||
			(ch >= '0' && ch <= '9') ||
			ch == '-'
		if !isValid {
			return "workspace name can only contain lowercase letters, numbers, and dashes"
		}
	}
	return ""
}

type snippetData struct {
	Name        string
	FileName    string
	FullPath    string
	Description string
	Language    string
}

type languageCategory struct {
	Name     string
	Snippets []snippetData
}

func gatherSnippetsByLanguage(workspacePath string) []languageCategory {
	snippetsDir := filepath.Join(workspacePath, ".sgai", "snippets")
	snippetsFS := os.DirFS(snippetsDir)

	languages := make(map[string][]snippetData)

	err := fs.WalkDir(snippetsFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		content, errRead := fs.ReadFile(snippetsFS, path)
		if errRead != nil {
			return nil
		}

		parts := strings.Split(path, "/")
		if len(parts) < 2 {
			return nil
		}

		language := parts[0]
		filename := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))

		fm := parseFrontmatterMap(content)
		name := fm["name"]
		if name == "" {
			name = filename
		}
		description := fm["description"]

		languages[language] = append(languages[language], snippetData{
			Name:        name,
			FileName:    filename,
			FullPath:    language + "/" + filename,
			Description: description,
			Language:    language,
		})

		return nil
	})
	if err != nil {
		return nil
	}

	var result []languageCategory
	languageNames := slices.Sorted(maps.Keys(languages))

	for _, languageName := range languageNames {
		snippets := languages[languageName]
		slices.SortFunc(snippets, func(a, b snippetData) int {
			return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		})
		result = append(result, languageCategory{
			Name:     languageName,
			Snippets: snippets,
		})
	}

	return result
}
