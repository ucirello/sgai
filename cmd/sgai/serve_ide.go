package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	browserSessionCookieName = "sgai_browser_session"
	ideAccessCookieName      = "sgai_ide_access"
	ideDockerImage           = "codercom/code-server:latest"
	ideDockerPort            = "8080/tcp"
	ideDockerPortFlag        = "8080"
	ideWorkspaceMountPath    = "/workspace"
	ideStatusCacheKey        = "ide-status"
	defaultBrowserSessionTTL = 24 * time.Hour
	defaultIDEAccessTTL      = 24 * time.Hour
	defaultIDEIdleTimeout    = 30 * time.Minute
	defaultIDEStatusTTL      = 5 * time.Second
	defaultIDERuntimeWait    = 30 * time.Second
	defaultIDERuntimeProbe   = 200 * time.Millisecond
)

var errIDEUnavailable = errors.New("ide unavailable")

type ideRuntimeStatus struct {
	Available bool
	Reason    string
}

type ideRuntimeTarget struct {
	ID   string
	Host string
	Port int
}

type ideStartRequest struct {
	WorkspacePath string
	ContainerName string
}

type ideRuntime interface {
	status(ctx context.Context) ideRuntimeStatus
	start(ctx context.Context, req ideStartRequest) (ideRuntimeTarget, error)
	inspect(ctx context.Context, target ideRuntimeTarget) (ideRuntimeTarget, error)
	stop(ctx context.Context, target ideRuntimeTarget) error
}

type ideAccessGrant struct {
	Token               string
	BrowserSessionToken string
	ExpiresAt           time.Time
	LastActivity        time.Time
}

type browserSession struct {
	Token     string
	ExpiresAt time.Time
	LastSeen  time.Time
}

type ideSession struct {
	workspacePath string
	workspaceName string
	target        ideRuntimeTarget
	createdAt     time.Time
	lastActivity  time.Time
	lastError     string
	lastEvent     string
	accessGrants  map[string]ideAccessGrant
}

type ideSessionCleanupCandidate struct {
	workspacePath string
	targetID      string
}

type ideWorkspaceRef struct {
	Path string
	Name string
	ID   string
}

type ideSessionStartResult struct {
	session *ideSession
	reused  bool
}

type apiWorkspaceIDEState struct {
	Available    bool   `json:"available"`
	Running      bool   `json:"running"`
	Reason       string `json:"reason,omitempty"`
	LastError    string `json:"lastError,omitempty"`
	LastEvent    string `json:"lastEvent,omitempty"`
	LastActivity string `json:"lastActivity,omitempty"`
	AccessPath   string `json:"accessPath,omitempty"`
	ProxyPath    string `json:"proxyPath,omitempty"`
}

type apiIDESessionInfo struct {
	ID           string `json:"id"`
	CreatedAt    string `json:"createdAt,omitempty"`
	LastActivity string `json:"lastActivity,omitempty"`
	LastEvent    string `json:"lastEvent,omitempty"`
}

type apiIDEStatusResponse struct {
	Available    bool               `json:"available"`
	Running      bool               `json:"running"`
	Reason       string             `json:"reason,omitempty"`
	LastError    string             `json:"lastError,omitempty"`
	LastEvent    string             `json:"lastEvent,omitempty"`
	LastActivity string             `json:"lastActivity,omitempty"`
	AccessPath   string             `json:"accessPath,omitempty"`
	ProxyPath    string             `json:"proxyPath,omitempty"`
	Reused       bool               `json:"reused,omitempty"`
	Session      *apiIDESessionInfo `json:"session,omitempty"`
}

type dockerIDERuntime struct{}

func newDockerIDERuntime() *dockerIDERuntime {
	return &dockerIDERuntime{}
}

func newIDERuntimeStatus(available bool, reason string) ideRuntimeStatus {
	return ideRuntimeStatus{Available: available, Reason: reason}
}

func newIDERuntimeTarget(id, host string, port int) ideRuntimeTarget {
	return ideRuntimeTarget{ID: id, Host: host, Port: port}
}

func emptyIDESessionStartResult() ideSessionStartResult {
	return ideSessionStartResult{session: nil, reused: false}
}

func (r *dockerIDERuntime) status(ctx context.Context) ideRuntimeStatus {
	dockerPath, errDockerPath := exec.LookPath("docker")
	if errDockerPath != nil {
		return newIDERuntimeStatus(false, "docker unavailable")
	}
	commandCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, dockerPath, "info", "--format", "{{.ServerVersion}}")
	output, errInfo := cmd.CombinedOutput()
	if errInfo != nil {
		return newIDERuntimeStatus(false, trimDockerOutput("docker unavailable", output, errInfo))
	}
	if strings.TrimSpace(string(output)) == "" {
		return newIDERuntimeStatus(false, "docker unavailable")
	}
	return newIDERuntimeStatus(true, "")
}

func (r *dockerIDERuntime) start(ctx context.Context, req ideStartRequest) (ideRuntimeTarget, error) {
	dockerPath, errDockerPath := exec.LookPath("docker")
	if errDockerPath != nil {
		return newIDERuntimeTarget("", "", 0), fmt.Errorf("looking up docker executable: %w", errDockerPath)
	}
	commandCtx, cancel := context.WithTimeout(ctx, defaultIDERuntimeWait)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, dockerPath, buildDockerRunArgs(req)...)
	output, errRun := cmd.CombinedOutput()
	if errRun != nil {
		return newIDERuntimeTarget("", "", 0), errors.New(trimDockerOutput("docker run failed", output, errRun))
	}
	containerID := strings.TrimSpace(string(output))
	if containerID == "" {
		containerID = req.ContainerName
	}
	target, errWait := r.waitForTarget(commandCtx, newIDERuntimeTarget(containerID, "", 0))
	if errWait != nil {
		_ = r.stop(context.Background(), newIDERuntimeTarget(containerID, "", 0))
		return newIDERuntimeTarget("", "", 0), errWait
	}
	return target, nil
}

func buildDockerRunArgs(req ideStartRequest) []string {
	workspaceStateRoot := ideWorkspaceMountPath + "/.sgai/code-server"
	workspaceHomePath := workspaceStateRoot + "/home"
	workspaceTempPath := workspaceStateRoot + "/tmp"
	workspaceConfigPath := workspaceStateRoot + "/config"
	workspaceDataPath := workspaceStateRoot + "/data"
	workspaceUserDataPath := workspaceStateRoot + "/user-data"
	workspaceExtensionsPath := workspaceStateRoot + "/extensions"
	return []string{
		"run",
		"--detach",
		"--rm",
		"--read-only",
		"--tmpfs", "/var/run",
		"--name", req.ContainerName,
		"--publish", "127.0.0.1::" + ideDockerPortFlag,
		"--workdir", ideWorkspaceMountPath,
		"--volume", req.WorkspacePath + ":" + ideWorkspaceMountPath,
		"--env", "HOME=" + workspaceHomePath,
		"--env", "TMPDIR=" + workspaceTempPath,
		"--env", "XDG_CONFIG_HOME=" + workspaceConfigPath,
		"--env", "XDG_DATA_HOME=" + workspaceDataPath,
		ideDockerImage,
		"--auth", "none",
		"--disable-telemetry",
		"--disable-update-check",
		"--user-data-dir", workspaceUserDataPath,
		"--extensions-dir", workspaceExtensionsPath,
		ideWorkspaceMountPath,
	}
}

func (r *dockerIDERuntime) waitForTarget(ctx context.Context, target ideRuntimeTarget) (ideRuntimeTarget, error) {
	client := new(http.Client)
	client.Timeout = time.Second
	ticker := time.NewTicker(defaultIDERuntimeProbe)
	defer ticker.Stop()
	for {
		inspectedTarget, errInspect := r.inspect(ctx, target)
		if errInspect == nil {
			var probeURL url.URL
			probeURL.Scheme = "http"
			probeURL.Host = net.JoinHostPort(inspectedTarget.Host, strconv.Itoa(inspectedTarget.Port))
			probeURL.Path = "/"
			resp, errRequest := client.Get(probeURL.String())
			if errRequest == nil {
				if errClose := resp.Body.Close(); errClose != nil {
					return newIDERuntimeTarget("", "", 0), fmt.Errorf("closing ide probe response: %w", errClose)
				}
				return inspectedTarget, nil
			}
		}
		select {
		case <-ctx.Done():
			return newIDERuntimeTarget("", "", 0), fmt.Errorf("waiting for ide runtime: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (r *dockerIDERuntime) inspect(ctx context.Context, target ideRuntimeTarget) (ideRuntimeTarget, error) {
	dockerPath, errDockerPath := exec.LookPath("docker")
	if errDockerPath != nil {
		return newIDERuntimeTarget("", "", 0), fmt.Errorf("looking up docker executable: %w", errDockerPath)
	}
	commandCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	runningCmd := exec.CommandContext(commandCtx, dockerPath, "inspect", "--format", "{{.State.Running}}", target.ID)
	runningOutput, errRunning := runningCmd.CombinedOutput()
	if errRunning != nil {
		return newIDERuntimeTarget("", "", 0), errors.New(trimDockerOutput("docker inspect failed", runningOutput, errRunning))
	}
	if strings.TrimSpace(string(runningOutput)) != "true" {
		return newIDERuntimeTarget("", "", 0), errors.New("ide runtime is not running")
	}
	portCmd := exec.CommandContext(commandCtx, dockerPath, "port", target.ID, ideDockerPort)
	portOutput, errPort := portCmd.CombinedOutput()
	if errPort != nil {
		return newIDERuntimeTarget("", "", 0), errors.New(trimDockerOutput("docker port lookup failed", portOutput, errPort))
	}
	host, port, errParsePort := parseDockerPortOutput(portOutput)
	if errParsePort != nil {
		return newIDERuntimeTarget("", "", 0), errParsePort
	}
	return newIDERuntimeTarget(target.ID, host, port), nil
}

func (r *dockerIDERuntime) stop(ctx context.Context, target ideRuntimeTarget) error {
	if target.ID == "" {
		return nil
	}
	dockerPath, errDockerPath := exec.LookPath("docker")
	if errDockerPath != nil {
		return fmt.Errorf("looking up docker executable: %w", errDockerPath)
	}
	commandCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, dockerPath, "stop", "--time", "1", target.ID)
	output, errStop := cmd.CombinedOutput()
	if errStop != nil {
		trimmed := strings.ToLower(strings.TrimSpace(string(output)))
		if strings.Contains(trimmed, "no such container") {
			return nil
		}
		return errors.New(trimDockerOutput("docker stop failed", output, errStop))
	}
	return nil
}

func trimDockerOutput(prefix string, output []byte, errCause error) string {
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return fmt.Sprintf("%s: %v", prefix, errCause)
	}
	return fmt.Sprintf("%s: %s", prefix, trimmed)
}

func parseDockerPortOutput(output []byte) (host string, port int, err error) {
	line := strings.TrimSpace(string(output))
	if line == "" {
		return "", 0, errors.New("docker port lookup returned empty output")
	}
	line = strings.Split(line, "\n")[0]
	host, portText, errSplit := net.SplitHostPort(line)
	if errSplit != nil {
		return "", 0, fmt.Errorf("parsing docker port output: %w", errSplit)
	}
	port, errPort := strconv.Atoi(portText)
	if errPort != nil {
		return "", 0, fmt.Errorf("parsing docker port: %w", errPort)
	}
	return host, port, nil
}

func (s *Server) registerIDERoutes(mux *http.ServeMux) {
	mux.HandleFunc("/workspaces/{id}/ide-proxy", s.handleIDEProxyEntry)
	mux.HandleFunc("/workspaces/{id}/ide-proxy/{$}", s.handleIDEProxy)
	mux.HandleFunc("/workspaces/{id}/ide-proxy/{path...}", s.handleIDEProxy)
}

func workspaceRouteID(workspacePath string) string {
	sum := sha256.Sum256([]byte(resolveSymlinks(workspacePath)))
	return hex.EncodeToString(sum[:])
}

func workspaceIDEBasePath(workspaceID string) string {
	return "/workspaces/" + url.PathEscape(workspaceID) + "/ide-proxy"
}

func workspaceIDEProxyPath(workspaceID string) string {
	return workspaceIDEBasePath(workspaceID) + "/"
}

func workspaceIDEAccessPath(workspaceID string) string {
	return "/api/v1/workspaces/" + url.PathEscape(workspaceID) + "/ide/access"
}

func ideAccessCookiePath() string {
	return "/"
}

func legacyIDEAccessCookiePath(workspaceID string) string {
	return workspaceIDEBasePath(workspaceID)
}

func legacyIDEAccessCookiePaths(workspaceID string) []string {
	return []string{legacyIDEAccessCookiePath(workspaceID), workspaceIDEProxyPath(workspaceID)}
}

func ideContainerName(workspacePath string) string {
	sum := sha256.Sum256([]byte(resolveSymlinks(workspacePath)))
	return "sgai-ide-" + hex.EncodeToString(sum[:6])
}

func (s *Server) ideAvailability(ctx context.Context) ideRuntimeStatus {
	if s.ideRuntime == nil {
		return newIDERuntimeStatus(false, "docker unavailable")
	}
	if cached, ok := s.ideStatusCache.get(ideStatusCacheKey); ok {
		return cached
	}
	status, _ := s.ideStatusFlight.do(ideStatusCacheKey, func() (ideRuntimeStatus, error) {
		if cached, ok := s.ideStatusCache.get(ideStatusCacheKey); ok {
			return cached, nil
		}
		status := s.ideRuntime.status(ctx)
		s.ideStatusCache.set(ideStatusCacheKey, status)
		return status, nil
	})
	return status
}

func (s *Server) buildWorkspaceIDEState(ctx context.Context, workspacePath string) apiWorkspaceIDEState {
	status := s.ideAvailability(ctx)
	workspaceID := workspaceRouteID(workspacePath)
	state := apiWorkspaceIDEState{
		Available:    status.Available,
		Running:      false,
		Reason:       status.Reason,
		LastError:    "",
		LastEvent:    "",
		LastActivity: "",
		AccessPath:   workspaceIDEAccessPath(workspaceID),
		ProxyPath:    workspaceIDEProxyPath(workspaceID),
	}
	s.mu.Lock()
	sess := s.ideSessions[workspacePath]
	if sess != nil {
		if sess.target.ID != "" {
			state.Available = true
			state.Running = true
		}
		state.LastError = sess.lastError
		state.LastEvent = sess.lastEvent
		state.LastActivity = formatOptionalTime(sess.lastActivity)
	}
	s.mu.Unlock()
	if sess == nil {
		return state
	}
	if state.Reason != "" && state.Available {
		state.Reason = ""
	}
	return state
}

func (s *Server) buildIDEStatusResponse(ctx context.Context, workspacePath string) apiIDEStatusResponse {
	state := s.buildWorkspaceIDEState(ctx, workspacePath)
	resp := apiIDEStatusResponse{
		Available:    state.Available,
		Running:      state.Running,
		Reason:       state.Reason,
		LastError:    state.LastError,
		LastEvent:    state.LastEvent,
		LastActivity: state.LastActivity,
		AccessPath:   state.AccessPath,
		ProxyPath:    state.ProxyPath,
		Reused:       false,
		Session:      nil,
	}
	s.mu.Lock()
	sess := s.ideSessions[workspacePath]
	if sess != nil && sess.target.ID != "" {
		resp.Session = &apiIDESessionInfo{
			ID:           sess.target.ID,
			CreatedAt:    formatOptionalTime(sess.createdAt),
			LastActivity: formatOptionalTime(sess.lastActivity),
			LastEvent:    sess.lastEvent,
		}
	}
	s.mu.Unlock()
	return resp
}

func (s *Server) handleAPIIDEStatus(w http.ResponseWriter, r *http.Request) {
	workspace, ok := s.resolveIDEWorkspaceFromPath(w, r)
	if !ok {
		return
	}
	if _, _, ok := s.authorizeIDERequest(w, r, workspace); !ok {
		return
	}
	writeJSON(w, s.buildIDEStatusResponse(r.Context(), workspace.Path))
}

func (s *Server) handleAPIIDEAccess(w http.ResponseWriter, r *http.Request) {
	workspace, ok := s.resolveIDEWorkspaceFromPath(w, r)
	if !ok {
		return
	}
	browserSession, ok := s.requireBrowserSession(w, r)
	if !ok {
		return
	}
	statusResp, cookie, errAccess := s.ensureIDEAccess(r.Context(), workspace.Path, workspace.Name, requestIsHTTPS(r), browserSession)
	if errAccess != nil {
		statusCode := http.StatusInternalServerError
		if errors.Is(errAccess, errIDEUnavailable) {
			statusCode = http.StatusServiceUnavailable
		}
		http.Error(w, errAccess.Error(), statusCode)
		return
	}
	setIDEAccessResponseCookies(w, workspace.ID, requestIsHTTPS(r), cookie)
	writeJSON(w, statusResp)
}

func (s *Server) ensureIDEAccess(ctx context.Context, workspacePath, workspaceName string, secureCookie bool, browserSession *browserSession) (apiIDEStatusResponse, *http.Cookie, error) {
	startResult, errEnsure := s.ensureIDESession(ctx, workspacePath, workspaceName)
	if errEnsure != nil {
		return apiIDEStatusResponse{}, nil, errEnsure
	}
	grant, errGrant := s.newIDEAccessGrant(startResult.session, browserSession.Token)
	if errGrant != nil {
		return apiIDEStatusResponse{}, nil, errGrant
	}
	resp := s.buildIDEStatusResponse(ctx, workspacePath)
	resp.Reused = startResult.reused
	cookie := newIDEAccessCookie(grant.Token, ideAccessCookiePath(), secureCookie, grant.ExpiresAt, 0)
	return resp, cookie, nil
}

func newIDEAccessCookie(token, path string, secureCookie bool, expiresAt time.Time, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:        ideAccessCookieName,
		Value:       token,
		Quoted:      false,
		Path:        path,
		Domain:      "",
		Expires:     expiresAt,
		RawExpires:  "",
		MaxAge:      maxAge,
		Secure:      secureCookie,
		HttpOnly:    true,
		SameSite:    http.SameSiteStrictMode,
		Partitioned: false,
		Raw:         "",
		Unparsed:    nil,
	}
}

func expiredLegacyIDEAccessCookies(workspaceID string, secureCookie bool) []*http.Cookie {
	paths := legacyIDEAccessCookiePaths(workspaceID)
	cookies := make([]*http.Cookie, 0, len(paths))
	for _, path := range paths {
		cookies = append(cookies, newIDEAccessCookie("", path, secureCookie, time.Unix(0, 0), -1))
	}
	return cookies
}

func setIDEAccessResponseCookies(w http.ResponseWriter, workspaceID string, secureCookie bool, accessCookie *http.Cookie) {
	for _, cookie := range expiredLegacyIDEAccessCookies(workspaceID, secureCookie) {
		http.SetCookie(w, cookie)
	}
	if accessCookie != nil {
		http.SetCookie(w, accessCookie)
	}
}

func (s *Server) newIDEAccessGrant(sess *ideSession, browserSessionToken string) (ideAccessGrant, error) {
	now := s.ideNow()
	token, errToken := newIDEAccessToken()
	if errToken != nil {
		return ideAccessGrant{Token: "", BrowserSessionToken: "", ExpiresAt: time.Time{}, LastActivity: time.Time{}}, errToken
	}
	grant := ideAccessGrant{Token: token, BrowserSessionToken: browserSessionToken, ExpiresAt: now.Add(s.ideAccessTTL), LastActivity: now}
	s.mu.Lock()
	if sess.accessGrants == nil {
		sess.accessGrants = make(map[string]ideAccessGrant)
	}
	sess.accessGrants[token] = grant
	sess.lastActivity = now
	s.mu.Unlock()
	return grant, nil
}

func (s *Server) ensureBrowserSession(w http.ResponseWriter, r *http.Request) bool {
	if browserSession, ok := s.lookupBrowserSession(r); ok {
		s.setBrowserSessionCookie(w, requestIsHTTPS(r), browserSession)
		return true
	}
	browserSession, errBrowserSession := s.newBrowserSession()
	if errBrowserSession != nil {
		http.Error(w, fmt.Sprintf("creating browser session: %v", errBrowserSession), http.StatusInternalServerError)
		return false
	}
	s.setBrowserSessionCookie(w, requestIsHTTPS(r), browserSession)
	return true
}

func (s *Server) requireBrowserSession(w http.ResponseWriter, r *http.Request) (*browserSession, bool) {
	browserSession, ok := s.lookupBrowserSession(r)
	if !ok {
		s.clearBrowserSessionCookie(w, requestIsHTTPS(r))
		http.Error(w, "browser session required", http.StatusUnauthorized)
		return nil, false
	}
	s.setBrowserSessionCookie(w, requestIsHTTPS(r), browserSession)
	return browserSession, true
}

func (s *Server) lookupBrowserSession(r *http.Request) (*browserSession, bool) {
	cookie, errCookie := r.Cookie(browserSessionCookieName)
	if errCookie != nil {
		return nil, false
	}
	now := s.ideNow()
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.browserSessions[cookie.Value]
	if current == nil {
		return nil, false
	}
	if !current.ExpiresAt.IsZero() && now.After(current.ExpiresAt) {
		delete(s.browserSessions, cookie.Value)
		return nil, false
	}
	current.LastSeen = now
	current.ExpiresAt = now.Add(s.browserSessionTTL)
	refreshed := *current
	return &refreshed, true
}

func (s *Server) newBrowserSession() (*browserSession, error) {
	token, errToken := newIDEAccessToken()
	if errToken != nil {
		return nil, errToken
	}
	now := s.ideNow()
	browserSession := &browserSession{
		Token:     token,
		ExpiresAt: now.Add(s.browserSessionTTL),
		LastSeen:  now,
	}
	s.mu.Lock()
	s.browserSessions[token] = browserSession
	s.mu.Unlock()
	cloned := *browserSession
	return &cloned, nil
}

func newBrowserSessionCookie(token string, secureCookie bool, expiresAt time.Time, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:        browserSessionCookieName,
		Value:       token,
		Quoted:      false,
		Path:        "/",
		Domain:      "",
		Expires:     expiresAt,
		RawExpires:  "",
		MaxAge:      maxAge,
		Secure:      secureCookie,
		HttpOnly:    true,
		SameSite:    http.SameSiteStrictMode,
		Partitioned: false,
		Raw:         "",
		Unparsed:    nil,
	}
}

func (s *Server) setBrowserSessionCookie(w http.ResponseWriter, secureCookie bool, browserSession *browserSession) {
	http.SetCookie(w, newBrowserSessionCookie(browserSession.Token, secureCookie, browserSession.ExpiresAt, 0))
}

func (s *Server) clearBrowserSessionCookie(w http.ResponseWriter, secureCookie bool) {
	http.SetCookie(w, newBrowserSessionCookie("", secureCookie, time.Unix(0, 0), -1))
}

func newIDEAccessToken() (string, error) {
	buf := make([]byte, 32)
	_, errRead := rand.Read(buf)
	if errRead != nil {
		return "", fmt.Errorf("reading ide access token bytes: %w", errRead)
	}
	return hex.EncodeToString(buf), nil
}

func (s *Server) ensureIDESession(ctx context.Context, workspacePath, workspaceName string) (ideSessionStartResult, error) {
	availability := s.ideAvailability(ctx)
	if !availability.Available {
		s.recordIDEFailure(workspacePath, workspaceName, availability.Reason)
		return emptyIDESessionStartResult(), fmt.Errorf("%w: %s", errIDEUnavailable, availability.Reason)
	}
	if result, ok := s.reuseRunningIDESession(ctx, workspacePath); ok {
		return result, nil
	}
	return s.ideStartFlight.do(workspacePath, func() (ideSessionStartResult, error) {
		if result, ok := s.reuseRunningIDESession(ctx, workspacePath); ok {
			return result, nil
		}
		containerName := ideContainerName(workspacePath)
		existingTarget, errInspect := s.ideRuntime.inspect(ctx, newIDERuntimeTarget(containerName, "", 0))
		if errInspect == nil {
			if errValidate := validateIDETarget(existingTarget); errValidate == nil {
				sess := s.storeIDESession(workspacePath, workspaceName, existingTarget, "reused")
				log.Printf("ide session reused workspace=%s container=%s", workspaceName, existingTarget.ID)
				return ideSessionStartResult{session: sess, reused: true}, nil
			}
			if errStop := s.ideRuntime.stop(ctx, existingTarget); errStop != nil {
				log.Printf("ide session cleanup failed workspace=%s container=%s err=%v", workspaceName, existingTarget.ID, errStop)
			}
		}
		request := ideStartRequest{
			WorkspacePath: workspacePath,
			ContainerName: containerName,
		}
		target, errStart := s.ideRuntime.start(ctx, request)
		if errStart != nil {
			s.recordIDEFailure(workspacePath, workspaceName, errStart.Error())
			return emptyIDESessionStartResult(), fmt.Errorf("starting ide session: %w", errStart)
		}
		if errValidate := validateIDETarget(target); errValidate != nil {
			if errStop := s.ideRuntime.stop(ctx, target); errStop != nil {
				log.Printf("ide session cleanup failed workspace=%s container=%s err=%v", workspaceName, target.ID, errStop)
			}
			s.recordIDEFailure(workspacePath, workspaceName, errValidate.Error())
			return emptyIDESessionStartResult(), errValidate
		}
		log.Printf("ide session created workspace=%s container=%s", workspaceName, target.ID)
		return ideSessionStartResult{session: s.storeIDESession(workspacePath, workspaceName, target, "created"), reused: false}, nil
	})
}

func (s *Server) reuseRunningIDESession(ctx context.Context, workspacePath string) (ideSessionStartResult, bool) {
	s.mu.Lock()
	sess := s.ideSessions[workspacePath]
	s.mu.Unlock()
	if sess == nil || sess.target.ID == "" {
		return emptyIDESessionStartResult(), false
	}
	target, errInspect := s.ideRuntime.inspect(ctx, sess.target)
	if errInspect != nil {
		s.mu.Lock()
		if current := s.ideSessions[workspacePath]; current != nil {
			current.target = newIDERuntimeTarget("", "", 0)
			current.accessGrants = make(map[string]ideAccessGrant)
			current.lastError = fmt.Sprintf("ide session inspect failed: %v", errInspect)
			current.lastEvent = "failed"
		}
		s.mu.Unlock()
		go s.notifyStateChange()
		return emptyIDESessionStartResult(), false
	}
	if errValidate := validateIDETarget(target); errValidate != nil {
		if errStop := s.ideRuntime.stop(ctx, target); errStop != nil {
			log.Printf("ide session cleanup failed workspace=%s container=%s err=%v", sess.workspaceName, target.ID, errStop)
		}
		s.recordIDEFailure(workspacePath, sess.workspaceName, errValidate.Error())
		return emptyIDESessionStartResult(), false
	}
	s.mu.Lock()
	if current := s.ideSessions[workspacePath]; current != nil {
		current.target = target
		current.lastEvent = "reused"
		current.lastError = ""
		sess = current
	}
	s.mu.Unlock()
	log.Printf("ide session reused workspace=%s container=%s", sess.workspaceName, target.ID)
	return ideSessionStartResult{session: sess, reused: true}, true
}

func (s *Server) storeIDESession(workspacePath, workspaceName string, target ideRuntimeTarget, event string) *ideSession {
	now := s.ideNow()
	s.mu.Lock()
	sess := &ideSession{
		workspacePath: workspacePath,
		workspaceName: workspaceName,
		target:        target,
		createdAt:     now,
		lastActivity:  now,
		lastError:     "",
		lastEvent:     event,
		accessGrants:  make(map[string]ideAccessGrant),
	}
	s.ideSessions[workspacePath] = sess
	s.ideStatusCache.delete(ideStatusCacheKey)
	s.mu.Unlock()
	s.notifyStateChange()
	return sess
}

func (s *Server) recordIDEFailure(workspacePath, workspaceName, message string) {
	s.mu.Lock()
	s.ideSessions[workspacePath] = &ideSession{
		workspacePath: workspacePath,
		workspaceName: workspaceName,
		target:        newIDERuntimeTarget("", "", 0),
		createdAt:     time.Time{},
		lastActivity:  time.Time{},
		lastError:     message,
		lastEvent:     "failed",
		accessGrants:  make(map[string]ideAccessGrant),
	}
	s.ideStatusCache.delete(ideStatusCacheKey)
	s.mu.Unlock()
	s.notifyStateChange()
}

func (s *Server) setIDESessionError(workspacePath, message, event string) {
	s.mu.Lock()
	if sess := s.ideSessions[workspacePath]; sess != nil {
		sess.lastError = message
		sess.lastEvent = event
	}
	s.mu.Unlock()
	s.notifyStateChange()
}

func (s *Server) handleIDEProxyEntry(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		workspace, ok := s.resolveIDEWorkspaceFromPath(w, r)
		if !ok {
			return
		}
		http.Redirect(w, r, workspaceIDEProxyPath(workspace.ID), http.StatusTemporaryRedirect)
		return
	}
	s.handleIDEProxy(w, r)
}

func (s *Server) handleIDEProxy(w http.ResponseWriter, r *http.Request) {
	workspace, ok := s.resolveIDEWorkspaceFromPath(w, r)
	if !ok {
		return
	}
	target, workspaceName, ok := s.authorizeIDERequest(w, r, workspace)
	if !ok {
		return
	}
	if target.ID == "" {
		http.Error(w, "ide session not found", http.StatusNotFound)
		return
	}
	targetURL := new(url.URL)
	targetURL.Scheme = "http"
	targetURL.Host = net.JoinHostPort(target.Host, strconv.Itoa(target.Port))
	proxy := new(httputil.ReverseProxy)
	proxy.Rewrite = func(proxyRequest *httputil.ProxyRequest) {
		proxyRequest.SetURL(targetURL)
		proxyRequest.SetXForwarded()
		stripIDEProxyAuthCookies(proxyRequest.Out)
		basePath := workspaceIDEBasePath(workspace.ID)
		proxyRequest.Out.URL.Path = trimIDEProxyRequestPath(proxyRequest.In.URL.Path, basePath)
		if proxyRequest.In.URL.RawPath != "" {
			proxyRequest.Out.URL.RawPath = trimIDEProxyRequestPath(proxyRequest.In.URL.RawPath, basePath)
		}
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, errProxy error) {
		if shouldIgnoreIDEProxyError(r, errProxy) {
			return
		}
		message := fmt.Sprintf("ide proxy failed: %v", errProxy)
		log.Printf("ide proxy failed workspace=%s err=%v", workspaceName, errProxy)
		s.setIDESessionError(workspace.Path, message, "failed")
		http.Error(w, message, http.StatusBadGateway)
	}
	proxy.ServeHTTP(w, r)
}

func stripIDEProxyAuthCookies(r *http.Request) {
	if r == nil || r.Header == nil {
		return
	}
	cookies := r.Cookies()
	if len(cookies) == 0 {
		return
	}
	r.Header.Del("Cookie")
	for _, cookie := range cookies {
		if cookie.Name == browserSessionCookieName || cookie.Name == ideAccessCookieName {
			continue
		}
		r.AddCookie(cookie)
	}
}

func trimIDEProxyRequestPath(urlPath, basePath string) string {
	if urlPath == basePath {
		return "/"
	}
	if strings.HasPrefix(urlPath, basePath+"/") {
		trimmed := strings.TrimPrefix(urlPath, basePath)
		if trimmed == "" {
			return "/"
		}
		return trimmed
	}
	if urlPath == "" {
		return "/"
	}
	return urlPath
}

func shouldIgnoreIDEProxyError(r *http.Request, errProxy error) bool {
	if errors.Is(errProxy, context.Canceled) {
		return true
	}
	return requestWasCanceled(r)
}

func requestWasCanceled(r *http.Request) bool {
	if r == nil {
		return false
	}
	return errors.Is(r.Context().Err(), context.Canceled)
}

func (s *Server) resolveIDEWorkspaceFromPath(w http.ResponseWriter, r *http.Request) (ideWorkspaceRef, bool) {
	workspaceID := strings.TrimSpace(r.PathValue("id"))
	if workspaceID == "" {
		http.Error(w, "workspace id is required", http.StatusBadRequest)
		return ideWorkspaceRef{Path: "", Name: "", ID: ""}, false
	}
	groups, errGroups := s.scanWorkspaceGroups()
	if errGroups != nil {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return ideWorkspaceRef{Path: "", Name: "", ID: ""}, false
	}
	var matches []ideWorkspaceRef
	for _, workspace := range workspaceInfos(groups) {
		if workspaceRouteID(workspace.Directory) != workspaceID {
			continue
		}
		matches = append(matches, ideWorkspaceRef{Path: workspace.Directory, Name: workspace.DirName, ID: workspaceID})
	}
	switch len(matches) {
	case 0:
		http.Error(w, "workspace not found", http.StatusNotFound)
		return ideWorkspaceRef{Path: "", Name: "", ID: ""}, false
	case 1:
		return matches[0], true
	default:
		http.Error(w, "workspace id is ambiguous", http.StatusConflict)
		return ideWorkspaceRef{Path: "", Name: "", ID: ""}, false
	}
}

func ideAccessCookieValues(r *http.Request) []string {
	if r == nil {
		return nil
	}
	cookies := r.Cookies()
	values := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie.Name != ideAccessCookieName || cookie.Value == "" {
			continue
		}
		values = append(values, cookie.Value)
	}
	return values
}

func refreshMatchingIDEAccessGrant(sess *ideSession, cookieValues []string, browserSessionToken string, now time.Time, ttl time.Duration) (ideAccessGrant, bool, bool) {
	hasBrowserSessionMismatch := false
	for _, cookieValue := range cookieValues {
		grant, ok := sess.accessGrants[cookieValue]
		if !ok {
			continue
		}
		if !grant.ExpiresAt.IsZero() && now.After(grant.ExpiresAt) {
			delete(sess.accessGrants, cookieValue)
			continue
		}
		if grant.BrowserSessionToken != browserSessionToken {
			delete(sess.accessGrants, cookieValue)
			hasBrowserSessionMismatch = true
			continue
		}
		grant.LastActivity = now
		grant.ExpiresAt = now.Add(ttl)
		sess.accessGrants[cookieValue] = grant
		sess.lastActivity = now
		sess.lastError = ""
		return grant, true, false
	}
	return ideAccessGrant{Token: "", BrowserSessionToken: "", ExpiresAt: time.Time{}, LastActivity: time.Time{}}, false, hasBrowserSessionMismatch
}

func refreshLatestIDEAccessGrantForBrowserSession(sess *ideSession, browserSessionToken string, now time.Time, ttl time.Duration) (ideAccessGrant, bool) {
	bestToken := ""
	bestGrant := ideAccessGrant{Token: "", BrowserSessionToken: "", ExpiresAt: time.Time{}, LastActivity: time.Time{}}
	for token, grant := range sess.accessGrants {
		if !grant.ExpiresAt.IsZero() && now.After(grant.ExpiresAt) {
			delete(sess.accessGrants, token)
			continue
		}
		if grant.BrowserSessionToken != browserSessionToken {
			continue
		}
		if bestToken == "" || grant.ExpiresAt.After(bestGrant.ExpiresAt) || (grant.ExpiresAt.Equal(bestGrant.ExpiresAt) && grant.LastActivity.After(bestGrant.LastActivity)) {
			bestToken = token
			bestGrant = grant
		}
	}
	if bestToken == "" {
		return ideAccessGrant{Token: "", BrowserSessionToken: "", ExpiresAt: time.Time{}, LastActivity: time.Time{}}, false
	}
	bestGrant.LastActivity = now
	bestGrant.ExpiresAt = now.Add(ttl)
	sess.accessGrants[bestToken] = bestGrant
	sess.lastActivity = now
	sess.lastError = ""
	return bestGrant, true
}

func (s *Server) authorizeIDERequest(w http.ResponseWriter, r *http.Request, workspace ideWorkspaceRef) (ideRuntimeTarget, string, bool) {
	browserSession, ok := s.lookupBrowserSession(r)
	if !ok {
		s.clearBrowserSessionCookie(w, requestIsHTTPS(r))
		http.Error(w, "ide access forbidden: browser session required", http.StatusForbidden)
		return newIDERuntimeTarget("", "", 0), "", false
	}
	secureCookie := requestIsHTTPS(r)
	s.setBrowserSessionCookie(w, secureCookie, browserSession)
	cookieValues := ideAccessCookieValues(r)
	now := s.ideNow()
	s.mu.Lock()
	current := s.ideSessions[workspace.Path]
	if current == nil || current.target.ID == "" {
		s.mu.Unlock()
		http.Error(w, "ide session not found", http.StatusNotFound)
		return newIDERuntimeTarget("", "", 0), "", false
	}
	grant, ok, hasBrowserSessionMismatch := refreshMatchingIDEAccessGrant(current, cookieValues, browserSession.Token, now, s.ideAccessTTL)
	if !ok && !hasBrowserSessionMismatch {
		grant, ok = refreshLatestIDEAccessGrantForBrowserSession(current, browserSession.Token, now, s.ideAccessTTL)
	}
	if !ok {
		s.mu.Unlock()
		if hasBrowserSessionMismatch {
			http.Error(w, "ide access forbidden: browser session mismatch", http.StatusForbidden)
			return newIDERuntimeTarget("", "", 0), "", false
		}
		http.Error(w, "ide access forbidden", http.StatusForbidden)
		return newIDERuntimeTarget("", "", 0), "", false
	}
	target := current.target
	workspaceName := current.workspaceName
	s.mu.Unlock()
	setIDEAccessResponseCookies(w, workspace.ID, secureCookie, newIDEAccessCookie(grant.Token, ideAccessCookiePath(), secureCookie, grant.ExpiresAt, 0))
	return target, workspaceName, true
}

func (s *Server) cleanupIdleIDESessions(now time.Time) {
	s.mu.Lock()
	for token, browserSession := range s.browserSessions {
		if !browserSession.ExpiresAt.IsZero() && now.After(browserSession.ExpiresAt) {
			delete(s.browserSessions, token)
		}
	}
	idleCandidates := make([]ideSessionCleanupCandidate, 0, len(s.ideSessions))
	for workspacePath, sess := range s.ideSessions {
		for token, grant := range sess.accessGrants {
			if !grant.ExpiresAt.IsZero() && now.After(grant.ExpiresAt) {
				delete(sess.accessGrants, token)
			}
		}
		if sess.target.ID == "" {
			continue
		}
		if ideSessionIsIdle(sess, now, s.ideIdleTimeout) {
			idleCandidates = append(idleCandidates, ideSessionCleanupCandidate{workspacePath: workspacePath, targetID: sess.target.ID})
		}
	}
	s.mu.Unlock()
	for _, candidate := range idleCandidates {
		if errStop := s.stopIdleIDESession(context.Background(), candidate, now, "destroyed"); errStop != nil {
			log.Printf("ide session cleanup failed workspace=%s err=%v", candidate.workspacePath, errStop)
		}
	}
}

func (s *Server) stopAllIDESessions(ctx context.Context) {
	s.mu.Lock()
	workspacePaths := make([]string, 0, len(s.ideSessions))
	for workspacePath, sess := range s.ideSessions {
		if sess.target.ID != "" {
			workspacePaths = append(workspacePaths, workspacePath)
		}
	}
	s.mu.Unlock()
	for _, workspacePath := range workspacePaths {
		if errStop := s.stopIDESession(ctx, workspacePath, "destroyed"); errStop != nil {
			log.Printf("ide session shutdown failed workspace=%s err=%v", workspacePath, errStop)
		}
	}
}

func (s *Server) stopIDESession(ctx context.Context, workspacePath, event string) error {
	return s.stopIDESessionIf(ctx, workspacePath, event, func(*ideSession) bool {
		return true
	})
}

func ideSessionIsIdle(sess *ideSession, now time.Time, idleTimeout time.Duration) bool {
	if sess == nil {
		return false
	}
	return now.Sub(sess.lastActivity) > idleTimeout
}

func (s *Server) stopIdleIDESession(ctx context.Context, candidate ideSessionCleanupCandidate, now time.Time, event string) error {
	return s.stopIDESessionIf(ctx, candidate.workspacePath, event, func(sess *ideSession) bool {
		return sess.target.ID == candidate.targetID && ideSessionIsIdle(sess, now, s.ideIdleTimeout)
	})
}

func (s *Server) stopIDESessionIf(ctx context.Context, workspacePath, event string, shouldStop func(*ideSession) bool) error {
	s.mu.Lock()
	current := s.ideSessions[workspacePath]
	if current == nil || current.target.ID == "" || !shouldStop(current) {
		s.mu.Unlock()
		return nil
	}
	target := current.target
	targetID := current.target.ID
	s.mu.Unlock()
	errStop := s.ideRuntime.stop(ctx, target)
	s.mu.Lock()
	current = s.ideSessions[workspacePath]
	if current == nil || current.target.ID != targetID || !shouldStop(current) {
		s.mu.Unlock()
		return errStop
	}
	if errStop != nil {
		current.lastError = errStop.Error()
		current.lastEvent = "failed"
		s.mu.Unlock()
		return errStop
	}
	log.Printf("ide session %s workspace=%s container=%s", event, current.workspaceName, current.target.ID)
	delete(s.ideSessions, workspacePath)
	s.ideStatusCache.delete(ideStatusCacheKey)
	s.mu.Unlock()
	s.notifyStateChange()
	return nil
}

func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func validateIDETarget(target ideRuntimeTarget) error {
	if target.ID == "" {
		return errors.New("ide target id is required")
	}
	if target.Port <= 0 {
		return errors.New("ide target port is required")
	}
	if !isLoopbackTargetHost(target.Host) {
		return fmt.Errorf("ide target must use loopback host: %s", target.Host)
	}
	return nil
}

func isLoopbackTargetHost(host string) bool {
	if host == "localhost" {
		return true
	}
	trimmed := strings.Trim(host, "[]")
	ip := net.ParseIP(trimmed)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func isIDEProxyRoute(urlPath string) bool {
	trimmed := strings.Trim(strings.TrimSpace(urlPath), "/")
	parts := strings.Split(trimmed, "/")
	return len(parts) >= 3 && parts[0] == "workspaces" && parts[2] == "ide-proxy"
}
