package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeIDERuntime struct {
	statusResult   ideRuntimeStatus
	startTarget    ideRuntimeTarget
	inspectTarget  ideRuntimeTarget
	startErr       error
	inspectErr     error
	stopErr        error
	beforeStop     func()
	startCalls     int
	inspectCalls   int
	stopCalls      int
	lastStartReq   ideStartRequest
	lastStopTarget ideRuntimeTarget
	lastInspectID  string
}

type writeErrorResponseWriter struct {
	header     http.Header
	statusCode int
	writeErr   error
	onWrite    func()
}

func newFakeIDERuntime() *fakeIDERuntime {
	return &fakeIDERuntime{
		statusResult:   newIDERuntimeStatus(false, ""),
		startTarget:    newIDERuntimeTarget("", "", 0),
		inspectTarget:  newIDERuntimeTarget("", "", 0),
		startErr:       nil,
		inspectErr:     nil,
		stopErr:        nil,
		beforeStop:     nil,
		startCalls:     0,
		inspectCalls:   0,
		stopCalls:      0,
		lastStartReq:   ideStartRequest{WorkspacePath: "", ContainerName: ""},
		lastStopTarget: newIDERuntimeTarget("", "", 0),
		lastInspectID:  "",
	}
}

func newWriteErrorResponseWriter(writeErr error, onWrite func()) *writeErrorResponseWriter {
	return &writeErrorResponseWriter{
		header:     make(http.Header),
		statusCode: 0,
		writeErr:   writeErr,
		onWrite:    onWrite,
	}
}

func (w *writeErrorResponseWriter) Header() http.Header {
	return w.header
}

func (w *writeErrorResponseWriter) WriteHeader(statusCode int) {
	if w.statusCode != 0 {
		return
	}
	w.statusCode = statusCode
}

func (w *writeErrorResponseWriter) Write(p []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	if w.onWrite != nil {
		w.onWrite()
	}
	if w.writeErr == nil {
		return len(p), nil
	}
	return 0, w.writeErr
}

func (r *fakeIDERuntime) status(context.Context) ideRuntimeStatus {
	return r.statusResult
}

func (r *fakeIDERuntime) start(_ context.Context, req ideStartRequest) (ideRuntimeTarget, error) {
	r.startCalls++
	r.lastStartReq = req
	return r.startTarget, r.startErr
}

func (r *fakeIDERuntime) inspect(_ context.Context, target ideRuntimeTarget) (ideRuntimeTarget, error) {
	r.inspectCalls++
	r.lastInspectID = target.ID
	if r.inspectErr != nil {
		return newIDERuntimeTarget("", "", 0), r.inspectErr
	}
	if r.inspectTarget.ID != "" || r.inspectTarget.Host != "" || r.inspectTarget.Port != 0 {
		return r.inspectTarget, nil
	}
	if target.Host == "" && target.Port == 0 {
		return newIDERuntimeTarget("", "", 0), errors.New("not found")
	}
	return target, nil
}

func (r *fakeIDERuntime) stop(_ context.Context, target ideRuntimeTarget) error {
	if r.beforeStop != nil {
		r.beforeStop()
	}
	r.stopCalls++
	r.lastStopTarget = target
	return r.stopErr
}

func newIDEProxyHandler(server *Server) http.Handler {
	mux := http.NewServeMux()
	server.registerAPIRoutes(mux)
	server.registerIDERoutes(mux)
	return server.spaMiddleware(mux)
}

func targetFromHTTPServer(t *testing.T, rawURL string) ideRuntimeTarget {
	t.Helper()
	parsedURL, errParse := url.Parse(rawURL)
	require.NoError(t, errParse)
	host, portText, errSplit := net.SplitHostPort(parsedURL.Host)
	require.NoError(t, errSplit)
	port, errPort := net.LookupPort("tcp", portText)
	require.NoError(t, errPort)
	return newIDERuntimeTarget("target", host, port)
}

func issueIDEAccessCookies(t *testing.T, server *Server, workspaceID string) (browserSessionCookie, ideCookie *http.Cookie) {
	t.Helper()
	browserSessionCookie = issueBrowserSessionCookie(t, server)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+workspaceID+"/ide/access", http.NoBody)
	req.AddCookie(browserSessionCookie)
	w := httptest.NewRecorder()
	newIDEProxyHandler(server).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	result := w.Result()
	require.NoError(t, result.Body.Close())
	return requireCookieNamed(t, result.Cookies(), browserSessionCookieName), requireCookieNamed(t, result.Cookies(), ideAccessCookieName)
}

func issueIDEAccessCookie(t *testing.T, server *Server, workspaceID string) *http.Cookie {
	t.Helper()
	_, ideCookie := issueIDEAccessCookies(t, server, workspaceID)
	return ideCookie
}

func issueBrowserSessionCookie(t *testing.T, server *Server) *http.Cookie {
	t.Helper()
	w := serveHTTP(server, http.MethodGet, "/api/v1/state", "")
	require.Equal(t, http.StatusOK, w.Code)
	result := w.Result()
	require.NoError(t, result.Body.Close())
	return requireCookieNamed(t, result.Cookies(), browserSessionCookieName)
}

func newTestCookie(name, value string) *http.Cookie {
	return &http.Cookie{
		Name:        name,
		Value:       value,
		Quoted:      false,
		Path:        "",
		Domain:      "",
		Expires:     time.Time{},
		RawExpires:  "",
		MaxAge:      0,
		Secure:      false,
		HttpOnly:    false,
		SameSite:    0,
		Partitioned: false,
		Raw:         "",
		Unparsed:    nil,
	}
}

func requireCookieNamed(t *testing.T, cookies []*http.Cookie, name string) *http.Cookie {
	t.Helper()
	var match *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name != name {
			continue
		}
		if cookie.Value != "" {
			match = cookie
			continue
		}
		if match == nil {
			match = cookie
		}
	}
	if match != nil {
		return match
	}
	require.Failf(t, "cookie not found", "expected cookie %q", name)
	return nil
}

func TestShouldIgnoreIDEProxyErrorRequiresCanceledRequestForClosedConnection(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/workspaces/test/ide-proxy/static/app.js", http.NoBody)
	assert.False(t, shouldIgnoreIDEProxyError(req, net.ErrClosed))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	canceledReq := httptest.NewRequest(http.MethodGet, "/workspaces/test/ide-proxy/static/app.js", http.NoBody).WithContext(ctx)
	assert.True(t, shouldIgnoreIDEProxyError(canceledReq, net.ErrClosed))
	assert.True(t, shouldIgnoreIDEProxyError(canceledReq, context.Canceled))
	assert.False(t, shouldIgnoreIDEProxyError(nil, net.ErrClosed))
}

func setupNestedTestWorkspace(t *testing.T, server *Server, rootDir string, pathParts ...string) string {
	t.Helper()
	wsDir := filepath.Join(append([]string{rootDir}, pathParts...)...)
	require.NoError(t, os.MkdirAll(filepath.Join(wsDir, ".sgai"), 0o755))
	canonicalDir := resolveSymlinks(wsDir)
	server.mu.Lock()
	server.externalDirs[canonicalDir] = true
	server.mu.Unlock()
	server.invalidateWorkspaceScanCache()
	return canonicalDir
}

func TestBuildWorkspaceFullStateIncludesIDEAvailability(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "ide-state-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "GOAL.md"), []byte("# Goal"), 0o644))
	runtime := newFakeIDERuntime()
	runtime.statusResult = newIDERuntimeStatus(false, "docker unavailable")
	srv.ideRuntime = runtime

	ws := workspaceWith(func(workspace *workspaceInfo) {
		workspace.DirName = "ide-state-ws"
		workspace.Directory = wsDir
		workspace.HasWorkspace = true
	})

	result := srv.buildWorkspaceFullState(ws, nil)

	assert.False(t, result.IDE.Available)
	assert.False(t, result.IDE.Running)
	assert.Equal(t, "docker unavailable", result.IDE.Reason)
	assert.Equal(t, workspaceIDEProxyPath(workspaceRouteID(wsDir)), result.IDE.ProxyPath)
	assert.Equal(t, "/api/v1/workspaces/"+workspaceRouteID(wsDir)+"/ide/access", result.IDE.AccessPath)
}

func TestHandleAPIIDEAccessStartsSessionAndSetsCookie(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "ide-access-ws")
	runtime := newFakeIDERuntime()
	runtime.statusResult = newIDERuntimeStatus(true, "")
	runtime.startTarget = newIDERuntimeTarget("container-1", "127.0.0.1", 12345)
	srv.ideRuntime = runtime
	srv.ideNow = func() time.Time {
		return time.Date(2026, time.March, 31, 12, 0, 0, 0, time.UTC)
	}
	workspaceID := workspaceRouteID(wsDir)
	browserSessionCookie := issueBrowserSessionCookie(t, srv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+workspaceID+"/ide/access", http.NoBody)
	req.AddCookie(browserSessionCookie)
	w := httptest.NewRecorder()
	newIDEProxyHandler(srv).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp apiIDEStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Available)
	assert.True(t, resp.Running)
	assert.False(t, resp.Reused)
	assert.Equal(t, workspaceIDEProxyPath(workspaceID), resp.ProxyPath)
	assert.NotNil(t, resp.Session)
	assert.Equal(t, wsDir, runtime.lastStartReq.WorkspacePath)
	assert.Equal(t, ideContainerName(wsDir), runtime.lastStartReq.ContainerName)
	assert.Equal(t, 1, runtime.startCalls)

	result := w.Result()
	require.NoError(t, result.Body.Close())
	cookies := result.Cookies()
	ideCookie := requireCookieNamed(t, cookies, ideAccessCookieName)
	assert.Equal(t, ideAccessCookiePath(), ideCookie.Path)
	assert.True(t, ideCookie.HttpOnly)
	assert.Equal(t, http.SameSiteStrictMode, ideCookie.SameSite)
}

func TestHandleAPIIDEAccessRequiresBrowserSession(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "ide-browser-auth-ws")
	runtime := newFakeIDERuntime()
	runtime.statusResult = newIDERuntimeStatus(true, "")
	runtime.startTarget = newIDERuntimeTarget("container-1", "127.0.0.1", 12345)
	srv.ideRuntime = runtime
	workspaceID := workspaceRouteID(wsDir)

	w := serveHTTP(srv, http.MethodPost, "/api/v1/workspaces/"+workspaceID+"/ide/access", "")

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, 0, runtime.startCalls)
	assert.Contains(t, w.Body.String(), "browser session")
}

func TestHandleAPIIDEAccessIgnoresForwardedIdentityHeaders(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "ide-forwarded-header-access-ws")
	runtime := newFakeIDERuntime()
	runtime.statusResult = newIDERuntimeStatus(true, "")
	runtime.startTarget = newIDERuntimeTarget("container-1", "127.0.0.1", 12345)
	srv.ideRuntime = runtime
	workspaceID := workspaceRouteID(wsDir)
	browserSessionCookie := issueBrowserSessionCookie(t, srv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+workspaceID+"/ide/access", http.NoBody)
	req.AddCookie(browserSessionCookie)
	req.Header.Set("X-Forwarded-User", "spoofed-user")
	w := httptest.NewRecorder()
	newIDEProxyHandler(srv).ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 1, runtime.startCalls)
}

func TestHandleAPIIDEAccessReusesRunningSession(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "ide-reuse-ws")
	runtime := newFakeIDERuntime()
	runtime.statusResult = newIDERuntimeStatus(true, "")
	runtime.startTarget = newIDERuntimeTarget("container-1", "127.0.0.1", 12345)
	srv.ideRuntime = runtime

	workspaceID := workspaceRouteID(wsDir)
	browserSessionCookie := issueBrowserSessionCookie(t, srv)
	firstReq := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+workspaceID+"/ide/access", http.NoBody)
	firstReq.AddCookie(browserSessionCookie)
	first := httptest.NewRecorder()
	newIDEProxyHandler(srv).ServeHTTP(first, firstReq)
	require.Equal(t, http.StatusOK, first.Code)
	secondReq := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+workspaceID+"/ide/access", http.NoBody)
	secondReq.AddCookie(browserSessionCookie)
	second := httptest.NewRecorder()
	newIDEProxyHandler(srv).ServeHTTP(second, secondReq)
	require.Equal(t, http.StatusOK, second.Code)

	var resp apiIDEStatusResponse
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &resp))
	assert.True(t, resp.Reused)
	assert.Equal(t, 1, runtime.startCalls)
	assert.GreaterOrEqual(t, runtime.inspectCalls, 1)
}

func TestHandleAPIIDEAccessRejectsNonLoopbackTarget(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "ide-loopback-ws")
	runtime := newFakeIDERuntime()
	runtime.statusResult = newIDERuntimeStatus(true, "")
	runtime.startTarget = newIDERuntimeTarget("container-unsafe", "0.0.0.0", 12345)
	srv.ideRuntime = runtime

	workspaceID := workspaceRouteID(wsDir)
	browserSessionCookie := issueBrowserSessionCookie(t, srv)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+workspaceID+"/ide/access", http.NoBody)
	req.AddCookie(browserSessionCookie)
	w := httptest.NewRecorder()
	newIDEProxyHandler(srv).ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "loopback")
	assert.Equal(t, 1, runtime.stopCalls)
	assert.Equal(t, "container-unsafe", runtime.lastStopTarget.ID)
}

func TestIDEProxyRequiresAuthorization(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()

	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "ide-proxy-auth-ws")
	runtime := newFakeIDERuntime()
	runtime.statusResult = newIDERuntimeStatus(true, "")
	runtime.startTarget = targetFromHTTPServer(t, backend.URL)
	srv.ideRuntime = runtime
	workspaceID := workspaceRouteID(wsDir)
	_, ideCookie := issueIDEAccessCookies(t, srv, workspaceID)
	require.NotNil(t, ideCookie)

	req := httptest.NewRequest(http.MethodGet, workspaceIDEProxyPath(workspaceID), http.NoBody)
	w := httptest.NewRecorder()
	newIDEProxyHandler(srv).ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestIDEProxyRequiresMatchingBrowserSession(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()

	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "ide-proxy-session-ws")
	runtime := newFakeIDERuntime()
	runtime.statusResult = newIDERuntimeStatus(true, "")
	runtime.startTarget = targetFromHTTPServer(t, backend.URL)
	srv.ideRuntime = runtime
	workspaceID := workspaceRouteID(wsDir)
	ideCookie := issueIDEAccessCookie(t, srv, workspaceID)

	req := httptest.NewRequest(http.MethodGet, workspaceIDEProxyPath(workspaceID), http.NoBody)
	req.AddCookie(ideCookie)
	w := httptest.NewRecorder()
	newIDEProxyHandler(srv).ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "browser session")
}

func TestIDEProxyIgnoresForwardedIdentityHeaders(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()

	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "ide-proxy-forwarded-header-ws")
	runtime := newFakeIDERuntime()
	runtime.statusResult = newIDERuntimeStatus(true, "")
	runtime.startTarget = targetFromHTTPServer(t, backend.URL)
	srv.ideRuntime = runtime
	workspaceID := workspaceRouteID(wsDir)
	browserSessionCookie, ideCookie := issueIDEAccessCookies(t, srv, workspaceID)

	req := httptest.NewRequest(http.MethodGet, workspaceIDEProxyPath(workspaceID), http.NoBody)
	req.AddCookie(browserSessionCookie)
	req.AddCookie(ideCookie)
	req.Header.Set("X-Forwarded-User", "spoofed-user")
	w := httptest.NewRecorder()
	newIDEProxyHandler(srv).ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestIDEAccessClearsLegacyProxyScopedCookie(t *testing.T) {
	tests := []struct {
		name       string
		cookiePath func(workspaceID string) string
	}{
		{
			name:       "base path",
			cookiePath: workspaceIDEBasePath,
		},
		{
			name:       "proxy path",
			cookiePath: workspaceIDEProxyPath,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			defer backend.Close()

			srv, rootDir := setupTestServer(t)
			wsDir := setupTestWorkspace(t, srv, rootDir, "ide-legacy-cookie-ws")
			runtime := newFakeIDERuntime()
			runtime.statusResult = newIDERuntimeStatus(true, "")
			runtime.startTarget = targetFromHTTPServer(t, backend.URL)
			srv.ideRuntime = runtime
			workspaceID := workspaceRouteID(wsDir)
			front := httptest.NewServer(newIDEProxyHandler(srv))
			defer front.Close()
			jar, errJar := cookiejar.New(nil)
			require.NoError(t, errJar)
			client := &http.Client{Transport: nil, CheckRedirect: nil, Jar: jar, Timeout: 0}

			stateResp, errState := client.Get(front.URL + "/api/v1/state")
			require.NoError(t, errState)
			require.NoError(t, stateResp.Body.Close())

			frontURL, errParse := url.Parse(front.URL)
			require.NoError(t, errParse)
			jar.SetCookies(frontURL, []*http.Cookie{{
				Name:    ideAccessCookieName,
				Value:   "legacy-token",
				Path:    tt.cookiePath(workspaceID),
				Expires: time.Now().Add(time.Hour),
			}})

			accessReq, errAccessReq := http.NewRequest(http.MethodPost, front.URL+"/api/v1/workspaces/"+workspaceID+"/ide/access", http.NoBody)
			require.NoError(t, errAccessReq)
			accessResp, errAccess := client.Do(accessReq)
			require.NoError(t, errAccess)
			require.Equal(t, http.StatusOK, accessResp.StatusCode)
			require.NoError(t, accessResp.Body.Close())

			proxyResp, errProxy := client.Get(front.URL + workspaceIDEProxyPath(workspaceID))
			require.NoError(t, errProxy)
			defer func() {
				assert.NoError(t, proxyResp.Body.Close())
			}()
			assert.Equal(t, http.StatusNoContent, proxyResp.StatusCode)
		})
	}
}

func TestIDEProxyAllowsFirstRequestAfterAccessWhenRequestStillCarriesStaleCookie(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()

	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "ide-first-request-stale-cookie-ws")
	runtime := newFakeIDERuntime()
	runtime.statusResult = newIDERuntimeStatus(true, "")
	runtime.startTarget = targetFromHTTPServer(t, backend.URL)
	srv.ideRuntime = runtime
	workspaceID := workspaceRouteID(wsDir)
	browserSessionCookie := issueBrowserSessionCookie(t, srv)

	accessReq := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+workspaceID+"/ide/access", http.NoBody)
	accessReq.AddCookie(browserSessionCookie)
	accessResp := httptest.NewRecorder()
	newIDEProxyHandler(srv).ServeHTTP(accessResp, accessReq)
	require.Equal(t, http.StatusOK, accessResp.Code)

	proxyReq := httptest.NewRequest(http.MethodGet, workspaceIDEProxyPath(workspaceID), http.NoBody)
	proxyReq.AddCookie(browserSessionCookie)
	proxyReq.AddCookie(newIDEAccessCookie("stale-token", workspaceIDEProxyPath(workspaceID), false, time.Time{}, 0))
	proxyResp := httptest.NewRecorder()
	newIDEProxyHandler(srv).ServeHTTP(proxyResp, proxyReq)

	assert.Equal(t, http.StatusNoContent, proxyResp.Code)
	result := proxyResp.Result()
	defer func() {
		assert.NoError(t, result.Body.Close())
	}()
	refreshedIDECookie := requireCookieNamed(t, result.Cookies(), ideAccessCookieName)
	assert.NotEmpty(t, refreshedIDECookie.Value)
	assert.Equal(t, ideAccessCookiePath(), refreshedIDECookie.Path)
}

func TestIDEProxyAllowsFirstRequestAfterAccessWhenRequestHasNoIDECookieYet(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()

	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "ide-first-request-no-cookie-ws")
	runtime := newFakeIDERuntime()
	runtime.statusResult = newIDERuntimeStatus(true, "")
	runtime.startTarget = targetFromHTTPServer(t, backend.URL)
	srv.ideRuntime = runtime
	workspaceID := workspaceRouteID(wsDir)
	front := httptest.NewServer(newIDEProxyHandler(srv))
	defer front.Close()
	jar, errJar := cookiejar.New(nil)
	require.NoError(t, errJar)
	authorizedClient := &http.Client{Transport: nil, CheckRedirect: nil, Jar: jar, Timeout: 0}

	stateResp, errState := authorizedClient.Get(front.URL + "/api/v1/state")
	require.NoError(t, errState)
	require.NoError(t, stateResp.Body.Close())

	accessReq, errAccessReq := http.NewRequest(http.MethodPost, front.URL+"/api/v1/workspaces/"+workspaceID+"/ide/access", http.NoBody)
	require.NoError(t, errAccessReq)
	accessResp, errAccess := authorizedClient.Do(accessReq)
	require.NoError(t, errAccess)
	require.Equal(t, http.StatusOK, accessResp.StatusCode)
	browserSessionCookie := requireCookieNamed(t, accessResp.Cookies(), browserSessionCookieName)
	require.NoError(t, accessResp.Body.Close())

	proxyReq, errProxyReq := http.NewRequest(http.MethodGet, front.URL+workspaceIDEProxyPath(workspaceID), http.NoBody)
	require.NoError(t, errProxyReq)
	proxyReq.AddCookie(browserSessionCookie)
	proxyClient := &http.Client{Transport: nil, CheckRedirect: nil, Jar: nil, Timeout: 0}
	proxyResp, errProxy := proxyClient.Do(proxyReq)
	require.NoError(t, errProxy)
	defer func() {
		assert.NoError(t, proxyResp.Body.Close())
	}()
	assert.Equal(t, http.StatusNoContent, proxyResp.StatusCode)
	refreshedIDECookie := requireCookieNamed(t, proxyResp.Cookies(), ideAccessCookieName)
	assert.NotEmpty(t, refreshedIDECookie.Value)
	assert.Equal(t, ideAccessCookiePath(), refreshedIDECookie.Path)
}

func TestIDEProxyForwardsHTTPRequests(t *testing.T) {
	var gotPath string
	var gotQuery string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()

	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "ide-proxy-ws")
	runtime := newFakeIDERuntime()
	runtime.statusResult = newIDERuntimeStatus(true, "")
	runtime.startTarget = targetFromHTTPServer(t, backend.URL)
	srv.ideRuntime = runtime
	workspaceID := workspaceRouteID(wsDir)
	browserSessionCookie, ideCookie := issueIDEAccessCookies(t, srv, workspaceID)

	req := httptest.NewRequest(http.MethodGet, workspaceIDEProxyPath(workspaceID)+"static/app.js?theme=dark", http.NoBody)
	req.AddCookie(browserSessionCookie)
	req.AddCookie(ideCookie)
	w := httptest.NewRecorder()
	newIDEProxyHandler(srv).ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "/static/app.js", gotPath)
	assert.Equal(t, "theme=dark", gotQuery)
}

func TestIDEProxyDoesNotForwardSgaiAuthCookies(t *testing.T) {
	var gotCookieHeader string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookieHeader = r.Header.Get("Cookie")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()

	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "ide-proxy-cookie-ws")
	runtime := newFakeIDERuntime()
	runtime.statusResult = newIDERuntimeStatus(true, "")
	runtime.startTarget = targetFromHTTPServer(t, backend.URL)
	srv.ideRuntime = runtime
	workspaceID := workspaceRouteID(wsDir)
	browserSessionCookie, ideCookie := issueIDEAccessCookies(t, srv, workspaceID)

	req := httptest.NewRequest(http.MethodGet, workspaceIDEProxyPath(workspaceID)+"static/app.js", http.NoBody)
	req.AddCookie(browserSessionCookie)
	req.AddCookie(ideCookie)
	req.AddCookie(newTestCookie("theme", "dark"))
	w := httptest.NewRecorder()
	newIDEProxyHandler(srv).ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
	assert.Contains(t, gotCookieHeader, "theme=dark")
	assert.NotContains(t, gotCookieHeader, browserSessionCookieName+"=")
	assert.NotContains(t, gotCookieHeader, ideAccessCookieName+"=")
}

func TestIDEProxySupportsWebSocketUpgrade(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Connection"), "Upgrade") || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "upgrade required", http.StatusBadRequest)
			return
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("response writer does not support hijacking")
			http.Error(w, "hijack unavailable", http.StatusInternalServerError)
			return
		}
		conn, writer, errHijack := hijacker.Hijack()
		if errHijack != nil {
			t.Errorf("hijack failed: %v", errHijack)
			return
		}
		defer func() {
			assert.NoError(t, conn.Close())
		}()
		_, errWrite := writer.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		if errWrite != nil {
			t.Errorf("write failed: %v", errWrite)
			return
		}
		if errFlush := writer.Flush(); errFlush != nil {
			t.Errorf("flush failed: %v", errFlush)
		}
	}))
	defer backend.Close()

	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "ide-ws-upgrade")
	runtime := newFakeIDERuntime()
	runtime.statusResult = newIDERuntimeStatus(true, "")
	runtime.startTarget = targetFromHTTPServer(t, backend.URL)
	srv.ideRuntime = runtime
	workspaceID := workspaceRouteID(wsDir)
	browserSessionCookie, ideCookie := issueIDEAccessCookies(t, srv, workspaceID)

	front := httptest.NewServer(newIDEProxyHandler(srv))
	defer front.Close()

	parsedFrontURL, errParse := url.Parse(front.URL)
	require.NoError(t, errParse)
	conn, errDial := net.Dial("tcp", parsedFrontURL.Host)
	require.NoError(t, errDial)
	t.Cleanup(func() {
		assert.NoError(t, conn.Close())
	})

	_, errWrite := fmt.Fprintf(conn, "GET %ssocket HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nCookie: %s=%s; %s=%s\r\n\r\n", workspaceIDEProxyPath(workspaceID), parsedFrontURL.Host, browserSessionCookie.Name, browserSessionCookie.Value, ideCookie.Name, ideCookie.Value)
	require.NoError(t, errWrite)

	line, errRead := bufio.NewReader(conn).ReadString('\n')
	require.NoError(t, errRead)
	assert.Contains(t, line, "101 Switching Protocols")
}

func TestClientDisconnectedIDEProxyRequestDoesNotMarkSessionFailed(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, errWrite := w.Write([]byte("console.log('ready')"))
		if errWrite != nil {
			t.Errorf("write failed: %v", errWrite)
		}
	}))
	defer backend.Close()

	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "ide-proxy-cancel-ws")
	runtime := newFakeIDERuntime()
	runtime.statusResult = newIDERuntimeStatus(true, "")
	runtime.startTarget = targetFromHTTPServer(t, backend.URL)
	srv.ideRuntime = runtime
	workspaceID := workspaceRouteID(wsDir)
	browserSessionCookie, ideCookie := issueIDEAccessCookies(t, srv, workspaceID)

	ctx, cancel := context.WithCancel(context.Background())
	proxyResp := newWriteErrorResponseWriter(net.ErrClosed, cancel)
	proxyReq := httptest.NewRequest(http.MethodGet, workspaceIDEProxyPath(workspaceID)+"static/app.js", http.NoBody).WithContext(ctx)
	proxyReq.AddCookie(browserSessionCookie)
	proxyReq.AddCookie(ideCookie)
	newIDEProxyHandler(srv).ServeHTTP(proxyResp, proxyReq)

	statusReq := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/"+workspaceID+"/ide", http.NoBody)
	statusReq.AddCookie(browserSessionCookie)
	statusReq.AddCookie(ideCookie)
	statusResp := httptest.NewRecorder()
	newIDEProxyHandler(srv).ServeHTTP(statusResp, statusReq)
	require.Equal(t, http.StatusOK, statusResp.Code)

	var resp apiIDEStatusResponse
	require.NoError(t, json.Unmarshal(statusResp.Body.Bytes(), &resp))
	require.NotNil(t, resp.Session)
	assert.True(t, resp.Running)
	assert.Equal(t, "created", resp.LastEvent)
	assert.Equal(t, "created", resp.Session.LastEvent)
	assert.Empty(t, resp.LastError)
}

func TestIDEProxyFailureMarksSessionFailedAndReturnsBadGateway(t *testing.T) {
	listener, errListen := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, errListen)
	targetAddr := listener.Addr().String()
	require.NoError(t, listener.Close())

	targetURL, errParse := url.Parse("http://" + targetAddr)
	require.NoError(t, errParse)
	host, portText, errSplit := net.SplitHostPort(targetURL.Host)
	require.NoError(t, errSplit)
	port, errPort := net.LookupPort("tcp", portText)
	require.NoError(t, errPort)

	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "ide-proxy-failed-ws")
	runtime := newFakeIDERuntime()
	runtime.statusResult = newIDERuntimeStatus(true, "")
	runtime.startTarget = newIDERuntimeTarget("target", host, port)
	srv.ideRuntime = runtime
	workspaceID := workspaceRouteID(wsDir)
	browserSessionCookie, ideCookie := issueIDEAccessCookies(t, srv, workspaceID)

	proxyReq := httptest.NewRequest(http.MethodGet, workspaceIDEProxyPath(workspaceID)+"static/app.js", http.NoBody)
	proxyReq.AddCookie(browserSessionCookie)
	proxyReq.AddCookie(ideCookie)
	proxyResp := httptest.NewRecorder()
	newIDEProxyHandler(srv).ServeHTTP(proxyResp, proxyReq)
	require.Equal(t, http.StatusBadGateway, proxyResp.Code)
	assert.Contains(t, proxyResp.Body.String(), "ide proxy failed")

	srv.mu.Lock()
	sess := srv.ideSessions[wsDir]
	srv.mu.Unlock()
	require.NotNil(t, sess)
	assert.Equal(t, "failed", sess.lastEvent)
	assert.Contains(t, sess.lastError, "ide proxy failed")

	statusReq := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/"+workspaceID+"/ide", http.NoBody)
	statusReq.AddCookie(browserSessionCookie)
	statusReq.AddCookie(ideCookie)
	statusResp := httptest.NewRecorder()
	newIDEProxyHandler(srv).ServeHTTP(statusResp, statusReq)
	require.Equal(t, http.StatusOK, statusResp.Code)

	var resp apiIDEStatusResponse
	require.NoError(t, json.Unmarshal(statusResp.Body.Bytes(), &resp))
	require.NotNil(t, resp.Session)
	assert.True(t, resp.Running)
	assert.Equal(t, "failed", resp.LastEvent)
	assert.Equal(t, "failed", resp.Session.LastEvent)
}

func TestCleanupIdleIDESessionsStopsExpiredSession(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "ide-cleanup-ws")
	runtime := newFakeIDERuntime()
	srv.ideRuntime = runtime
	now := time.Date(2026, time.March, 31, 14, 0, 0, 0, time.UTC)
	srv.ideIdleTimeout = time.Minute
	srv.ideNow = func() time.Time {
		return now
	}

	srv.mu.Lock()
	srv.ideSessions[wsDir] = &ideSession{
		workspacePath: wsDir,
		workspaceName: "ide-cleanup-ws",
		target:        newIDERuntimeTarget("container-cleanup", "127.0.0.1", 12345),
		createdAt:     time.Time{},
		lastActivity:  now.Add(-2 * time.Minute),
		lastError:     "",
		lastEvent:     "",
		accessGrants: map[string]ideAccessGrant{
			"token": {
				Token:               "token",
				BrowserSessionToken: "",
				ExpiresAt:           time.Time{},
				LastActivity:        now.Add(-2 * time.Minute),
			},
		},
	}
	srv.mu.Unlock()

	srv.cleanupIdleIDESessions(now)

	assert.Equal(t, 1, runtime.stopCalls)
	srv.mu.Lock()
	_, ok := srv.ideSessions[wsDir]
	srv.mu.Unlock()
	assert.False(t, ok)
}

func TestCleanupIdleIDESessionsKeepsSessionRefreshedDuringStop(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "ide-cleanup-refresh-ws")
	runtime := newFakeIDERuntime()
	stopStarted := make(chan struct{}, 1)
	allowStop := make(chan struct{})
	runtime.beforeStop = func() {
		stopStarted <- struct{}{}
		<-allowStop
	}
	srv.ideRuntime = runtime
	now := time.Date(2026, time.March, 31, 14, 0, 0, 0, time.UTC)
	srv.ideIdleTimeout = time.Minute

	srv.mu.Lock()
	srv.ideSessions[wsDir] = &ideSession{
		workspacePath: wsDir,
		workspaceName: "ide-cleanup-refresh-ws",
		target:        newIDERuntimeTarget("container-refresh", "127.0.0.1", 12345),
		createdAt:     time.Time{},
		lastActivity:  now.Add(-2 * time.Minute),
		lastError:     "",
		lastEvent:     "",
		accessGrants: map[string]ideAccessGrant{
			"token": {
				Token:               "token",
				BrowserSessionToken: "",
				ExpiresAt:           time.Time{},
				LastActivity:        now.Add(-2 * time.Minute),
			},
		},
	}
	srv.mu.Unlock()

	done := make(chan struct{})
	go func() {
		srv.cleanupIdleIDESessions(now)
		close(done)
	}()

	<-stopStarted
	srv.mu.Lock()
	sess := srv.ideSessions[wsDir]
	require.NotNil(t, sess)
	sess.lastActivity = now
	grant := sess.accessGrants["token"]
	grant.LastActivity = now
	sess.accessGrants["token"] = grant
	srv.mu.Unlock()
	close(allowStop)
	<-done

	srv.mu.Lock()
	refreshed := srv.ideSessions[wsDir]
	srv.mu.Unlock()
	require.NotNil(t, refreshed)
	assert.Equal(t, "container-refresh", refreshed.target.ID)
	assert.Equal(t, now, refreshed.lastActivity)
}

func TestCleanupIdleIDESessionsRemovesExpiredBrowserSessions(t *testing.T) {
	srv, _ := setupTestServer(t)
	now := time.Date(2026, time.March, 31, 14, 0, 0, 0, time.UTC)

	srv.mu.Lock()
	srv.browserSessions["expired"] = &browserSession{
		Token:     "expired",
		ExpiresAt: now.Add(-time.Minute),
		LastSeen:  now.Add(-2 * time.Minute),
	}
	srv.browserSessions["active"] = &browserSession{
		Token:     "active",
		ExpiresAt: now.Add(time.Minute),
		LastSeen:  now,
	}
	srv.mu.Unlock()

	srv.cleanupIdleIDESessions(now)

	srv.mu.Lock()
	_, expiredOK := srv.browserSessions["expired"]
	_, activeOK := srv.browserSessions["active"]
	srv.mu.Unlock()
	assert.False(t, expiredOK)
	assert.True(t, activeOK)
}

func TestHandleAPIIDEAccessSupportsDuplicateBasenameWorkspaceIDs(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	firstDir := setupNestedTestWorkspace(t, srv, rootDir, "one", "shared-ws")
	secondDir := setupNestedTestWorkspace(t, srv, rootDir, "two", "shared-ws")
	runtime := newFakeIDERuntime()
	runtime.statusResult = newIDERuntimeStatus(true, "")
	runtime.startTarget = newIDERuntimeTarget("container-1", "127.0.0.1", 12345)
	srv.ideRuntime = runtime
	browserSessionCookie := issueBrowserSessionCookie(t, srv)

	firstReq := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+workspaceRouteID(firstDir)+"/ide/access", http.NoBody)
	firstReq.AddCookie(browserSessionCookie)
	first := httptest.NewRecorder()
	newIDEProxyHandler(srv).ServeHTTP(first, firstReq)
	require.Equal(t, http.StatusOK, first.Code)

	secondReq := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+workspaceRouteID(secondDir)+"/ide/access", http.NoBody)
	secondReq.AddCookie(browserSessionCookie)
	second := httptest.NewRecorder()
	newIDEProxyHandler(srv).ServeHTTP(second, secondReq)
	require.Equal(t, http.StatusOK, second.Code)

	var firstResp apiIDEStatusResponse
	var secondResp apiIDEStatusResponse
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &firstResp))
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &secondResp))
	assert.Equal(t, workspaceIDEProxyPath(workspaceRouteID(firstDir)), firstResp.ProxyPath)
	assert.Equal(t, workspaceIDEProxyPath(workspaceRouteID(secondDir)), secondResp.ProxyPath)
	assert.NotEqual(t, firstResp.ProxyPath, secondResp.ProxyPath)
	assert.Equal(t, 2, runtime.startCalls)
}

func TestBuildDockerRunArgsKeepWritablePathsInsideWorkspace(t *testing.T) {
	req := ideStartRequest{
		WorkspacePath: "/tmp/test-workspace",
		ContainerName: "sgai-ide-test",
	}

	args := buildDockerRunArgs(req)

	assert.Contains(t, ideDockerImage, "@sha256:")
	assert.True(t, slices.Contains(args, "--read-only"))
	assert.True(t, slices.Contains(args, "--tmpfs"))
	assert.Contains(t, args, "/var/run")
	assert.True(t, slices.Contains(args, "--env"))
	assert.Contains(t, args, "HOME=/workspace/.sgai/code-server/home")
	assert.Contains(t, args, "TMPDIR=/workspace/.sgai/code-server/tmp")
	assert.Contains(t, args, "ENTRYPOINTD=/workspace/.sgai/code-server/entrypoint.d")
	assert.Contains(t, args, "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	assert.NotContains(t, args, "PATH=/workspace/.sgai/code-server/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	assert.Contains(t, args, "/workspace/.sgai/code-server/user-data")
	assert.Contains(t, args, "/workspace/.sgai/code-server/extensions")
	assert.NotContains(t, args, "--base-path")
	assert.NotContains(t, args, "--entrypoint")
	assert.NotContains(t, args, "/tmp/sgai-code-server-data")
	assert.NotContains(t, args, "/tmp/sgai-code-server-extensions")
}

func TestPrepareIDEWorkspaceStateWritesJJBootstrapScript(t *testing.T) {
	workspacePath := t.TempDir()
	errorPath := filepath.Join(workspacePath, ".sgai", "code-server", "bootstrap-error")
	require.NoError(t, os.MkdirAll(filepath.Dir(errorPath), 0o755))
	require.NoError(t, os.WriteFile(errorPath, []byte("stale failure"), 0o644))

	require.NoError(t, prepareIDEWorkspaceState(workspacePath))

	_, errStatError := os.Stat(errorPath)
	require.ErrorIs(t, errStatError, os.ErrNotExist)

	scriptPath := filepath.Join(workspacePath, ".sgai", "code-server", "entrypoint.d", "10-install-jj")
	scriptInfo, errStatScript := os.Stat(scriptPath)
	require.NoError(t, errStatScript)
	assert.NotZero(t, scriptInfo.Mode()&0o111)

	script, errRead := os.ReadFile(scriptPath)
	require.NoError(t, errRead)
	scriptText := string(script)
	assert.Contains(t, scriptText, "error_file=\"/workspace/.sgai/code-server/bootstrap-error\"")
	assert.Contains(t, scriptText, "bin_dir=\"/workspace/.sgai/code-server/bin\"")
	assert.Contains(t, scriptText, "cache_root=\"/workspace/.sgai/code-server/cache\"")
	assert.Contains(t, scriptText, "version=\"0.39.0\"")
	assert.Contains(t, scriptText, "archive_name=\"jj-v$version-x86_64-unknown-linux-musl.tar.gz\"")
	assert.Contains(t, scriptText, "archive_name=\"jj-v$version-aarch64-unknown-linux-musl.tar.gz\"")
	assert.Contains(t, scriptText, "archive_sha256=\"8da8d96e9c8696c21ad47847a63d533e249acb0449d9af0f0562b5ea7b024f04\"")
	assert.Contains(t, scriptText, "archive_sha256=\"15bbb0199adf57929d1e3cd90ae0b47356858cbe374814769815a1fb87d5ad1d\"")
	assert.Contains(t, scriptText, "binary_sha256=\"4bf2da7b36705dc9f5c0df98e62789efa7ce8ee3de8d8667c6d50ce52a72f306\"")
	assert.Contains(t, scriptText, "binary_sha256=\"ccda0d659adc1f0b72da83b907d0905a1b4ba2a4bb47d917d2815680a73a79e3\"")
	assert.Contains(t, scriptText, "command -v sha256sum")
	assert.Contains(t, scriptText, "command -v shasum")
	assert.Contains(t, scriptText, "if [ ! -x \"$cached_jj\" ]; then")
	assert.Contains(t, scriptText, "if ! matches_sha256 \"$cached_jj\" \"$binary_sha256\"; then")
	assert.Contains(t, scriptText, "verify_sha256 \"$archive_path\" \"$archive_sha256\"")
	assert.Contains(t, scriptText, "jj_candidate=\"$tmp_dir/jj\"")
	assert.Contains(t, scriptText, "jj_candidate=\"$extracted_dir/jj\"")
	assert.Contains(t, scriptText, "verify_sha256 \"$jj_candidate\" \"$binary_sha256\"")
	assert.Contains(t, scriptText, "verify_sha256 \"$cached_jj\" \"$binary_sha256\"")
	assert.Contains(t, scriptText, "cp \"$cached_jj\" \"$bin_dir/jj.tmp\"")
	assert.Contains(t, scriptText, "jj bootstrap failed")
	assert.Contains(t, scriptText, "curl -fsSL")
	assert.Contains(t, scriptText, "wget -qO")

	bashrcPath := filepath.Join(workspacePath, ".sgai", "code-server", "home", ".bashrc")
	bashrc, errReadBashrc := os.ReadFile(bashrcPath)
	require.NoError(t, errReadBashrc)
	assert.Contains(t, string(bashrc), "export PATH=\"$PATH:/workspace/.sgai/code-server/bin\"")
	assert.NotContains(t, string(bashrc), "export PATH=\"/workspace/.sgai/code-server/bin:$PATH\"")

	profilePath := filepath.Join(workspacePath, ".sgai", "code-server", "home", ".profile")
	profile, errReadProfile := os.ReadFile(profilePath)
	require.NoError(t, errReadProfile)
	assert.Contains(t, string(profile), ". \"$HOME/.bashrc\"")

	bashProfilePath := filepath.Join(workspacePath, ".sgai", "code-server", "home", ".bash_profile")
	bashProfile, errReadBashProfile := os.ReadFile(bashProfilePath)
	require.NoError(t, errReadBashProfile)
	assert.Contains(t, string(bashProfile), ". \"$HOME/.bashrc\"")
}

func TestPrepareIDEWorkspaceStateRewritesUnsafeBashrcPathBlock(t *testing.T) {
	workspacePath := t.TempDir()
	bashrcPath := filepath.Join(workspacePath, ".sgai", "code-server", "home", ".bashrc")
	require.NoError(t, os.MkdirAll(filepath.Dir(bashrcPath), 0o755))
	legacyBlock := strings.Join([]string{
		`case ":$PATH:" in`,
		`  *":/workspace/.sgai/code-server/bin:"*) ;;`,
		`  *) export PATH="/workspace/.sgai/code-server/bin:$PATH" ;;`,
		"esac",
	}, "\n")
	original := "export EDITOR=vim\n\n" + legacyBlock + "\n"
	require.NoError(t, os.WriteFile(bashrcPath, []byte(original), 0o644))

	require.NoError(t, prepareIDEWorkspaceState(workspacePath))

	bashrc, errRead := os.ReadFile(bashrcPath)
	require.NoError(t, errRead)
	text := string(bashrc)
	assert.Contains(t, text, "export EDITOR=vim")
	assert.Contains(t, text, "export PATH=\"$PATH:/workspace/.sgai/code-server/bin\"")
	assert.NotContains(t, text, "export PATH=\"/workspace/.sgai/code-server/bin:$PATH\"")
	assert.Equal(t, 0, strings.Count(text, legacyBlock))
	assert.Equal(t, 1, strings.Count(text, buildIDEBashRCBlock()))
}

func TestResolveIDEStartErrorPrefersBootstrapFailure(t *testing.T) {
	workspacePath := t.TempDir()
	fallbackErr := errors.New("waiting for ide runtime: context deadline exceeded")
	want := "jj bootstrap failed: downloading https://example.invalid/jj"
	errorPath := filepath.Join(workspacePath, ".sgai", "code-server", "bootstrap-error")
	require.NoError(t, os.MkdirAll(filepath.Dir(errorPath), 0o755))
	require.NoError(t, os.WriteFile(errorPath, []byte(want+"\n"), 0o644))

	errResolved := resolveIDEStartError(workspacePath, fallbackErr)
	require.EqualError(t, errResolved, want)

	errFallback := resolveIDEStartError(t.TempDir(), fallbackErr)
	assert.ErrorIs(t, errFallback, fallbackErr)
}

func TestWaitForStartedTargetReturnsBootstrapErrorImmediatelyWhenContainerStops(t *testing.T) {
	workspacePath := t.TempDir()
	errorPath := filepath.Join(workspacePath, ".sgai", "code-server", "bootstrap-error")
	require.NoError(t, os.MkdirAll(filepath.Dir(errorPath), 0o755))
	want := "jj bootstrap failed: extracted archive missing jj binary"
	require.NoError(t, os.WriteFile(errorPath, []byte(want+"\n"), 0o644))

	runtime := &dockerIDERuntime{
		inspectOverride: func(context.Context, ideRuntimeTarget) (ideRuntimeTarget, error) {
			return newIDERuntimeTarget("", "", 0), errors.New("ide runtime is not running")
		},
		probeOverride: nil,
	}

	startedAt := time.Now()
	_, errWait := runtime.waitForStartedTarget(context.Background(), newIDERuntimeTarget("container-1", "", 0), workspacePath)
	require.EqualError(t, errWait, want)
	assert.Less(t, time.Since(startedAt), time.Second)
}

func TestIsIDEProxyRouteSkipsWorkspaceDetailIDERoute(t *testing.T) {
	assert.False(t, isIDEProxyRoute("/workspaces/sgai-pure-navy-ii76/ide"))
	assert.True(t, isIDEProxyRoute("/workspaces/workspace-id/ide-proxy/"))
}

func TestHandleAPIIDEStatusRequiresIDEAuthorization(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "ide-status-auth-ws")
	runtime := newFakeIDERuntime()
	runtime.statusResult = newIDERuntimeStatus(true, "")
	runtime.startTarget = newIDERuntimeTarget("container-1", "127.0.0.1", 12345)
	srv.ideRuntime = runtime
	workspaceID := workspaceRouteID(wsDir)

	w := serveHTTP(srv, http.MethodGet, "/api/v1/workspaces/"+workspaceID+"/ide", "")

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleAPIIDEStatusReturnsMetadataForAuthorizedRequest(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "ide-status-authorized-ws")
	runtime := newFakeIDERuntime()
	runtime.statusResult = newIDERuntimeStatus(true, "")
	runtime.startTarget = newIDERuntimeTarget("container-1", "127.0.0.1", 12345)
	srv.ideRuntime = runtime
	workspaceID := workspaceRouteID(wsDir)
	front := httptest.NewServer(newIDEProxyHandler(srv))
	defer front.Close()
	jar, errJar := cookiejar.New(nil)
	require.NoError(t, errJar)
	client := &http.Client{Transport: nil, CheckRedirect: nil, Jar: jar, Timeout: 0}

	stateResp, errState := client.Get(front.URL + "/api/v1/state")
	require.NoError(t, errState)
	require.NoError(t, stateResp.Body.Close())

	accessReq, errAccessReq := http.NewRequest(http.MethodPost, front.URL+"/api/v1/workspaces/"+workspaceID+"/ide/access", http.NoBody)
	require.NoError(t, errAccessReq)
	accessResp, errAccess := client.Do(accessReq)
	require.NoError(t, errAccess)
	require.Equal(t, http.StatusOK, accessResp.StatusCode)
	require.NoError(t, accessResp.Body.Close())

	statusResp, errStatus := client.Get(front.URL + "/api/v1/workspaces/" + workspaceID + "/ide")
	require.NoError(t, errStatus)
	defer func() {
		assert.NoError(t, statusResp.Body.Close())
	}()
	assert.Equal(t, http.StatusOK, statusResp.StatusCode)

	var resp apiIDEStatusResponse
	require.NoError(t, json.NewDecoder(statusResp.Body).Decode(&resp))
	assert.True(t, resp.Available)
	assert.True(t, resp.Running)
	assert.NotNil(t, resp.Session)
	assert.Equal(t, "container-1", resp.Session.ID)
}

func TestAuthorizedIDERequestsRefreshIDEAccessCookie(t *testing.T) {
	tests := []struct {
		name        string
		requestPath func(workspaceID string) string
		wantStatus  int
	}{
		{
			name: "status route",
			requestPath: func(workspaceID string) string {
				return "/api/v1/workspaces/" + workspaceID + "/ide"
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "proxy route",
			requestPath: func(workspaceID string) string {
				return workspaceIDEProxyPath(workspaceID) + "static/app.js"
			},
			wantStatus: http.StatusNoContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			defer backend.Close()

			srv, rootDir := setupTestServer(t)
			wsDir := setupTestWorkspace(t, srv, rootDir, strings.ReplaceAll(tt.name, " ", "-"))
			runtime := newFakeIDERuntime()
			runtime.statusResult = newIDERuntimeStatus(true, "")
			runtime.startTarget = targetFromHTTPServer(t, backend.URL)
			srv.ideRuntime = runtime

			now := time.Date(2026, time.March, 31, 12, 0, 0, 0, time.UTC)
			srv.ideNow = func() time.Time {
				return now
			}

			workspaceID := workspaceRouteID(wsDir)
			browserSessionCookie, ideCookie := issueIDEAccessCookies(t, srv, workspaceID)
			initialIDEExpiry := ideCookie.Expires

			now = now.Add(12 * time.Hour)
			request := httptest.NewRequest(http.MethodGet, tt.requestPath(workspaceID), http.NoBody)
			request.AddCookie(browserSessionCookie)
			request.AddCookie(ideCookie)
			response := httptest.NewRecorder()
			newIDEProxyHandler(srv).ServeHTTP(response, request)
			require.Equal(t, tt.wantStatus, response.Code)

			result := response.Result()
			defer func() {
				assert.NoError(t, result.Body.Close())
			}()

			refreshedBrowserSessionCookie := requireCookieNamed(t, result.Cookies(), browserSessionCookieName)
			refreshedIDECookie := requireCookieNamed(t, result.Cookies(), ideAccessCookieName)
			assert.Equal(t, ideCookie.Value, refreshedIDECookie.Value)
			assert.Equal(t, ideAccessCookiePath(), refreshedIDECookie.Path)
			assert.WithinDuration(t, now.Add(srv.ideAccessTTL), refreshedIDECookie.Expires, time.Second)
			assert.True(t, refreshedIDECookie.Expires.After(initialIDEExpiry))

			now = initialIDEExpiry.Add(time.Minute)
			followUpRequest := httptest.NewRequest(http.MethodGet, tt.requestPath(workspaceID), http.NoBody)
			followUpRequest.AddCookie(refreshedBrowserSessionCookie)
			followUpRequest.AddCookie(refreshedIDECookie)
			followUpResponse := httptest.NewRecorder()
			newIDEProxyHandler(srv).ServeHTTP(followUpResponse, followUpRequest)
			assert.Equal(t, tt.wantStatus, followUpResponse.Code)
		})
	}
}

func TestHandleAPIIDEAccessSurfacesStartFailures(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "ide-failure-ws")
	runtime := newFakeIDERuntime()
	runtime.statusResult = newIDERuntimeStatus(true, "")
	runtime.startErr = errors.New("docker run failed")
	srv.ideRuntime = runtime
	workspaceID := workspaceRouteID(wsDir)

	browserSessionCookie := issueBrowserSessionCookie(t, srv)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+workspaceID+"/ide/access", http.NoBody)
	req.AddCookie(browserSessionCookie)
	w := httptest.NewRecorder()
	newIDEProxyHandler(srv).ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "docker run failed")
}
