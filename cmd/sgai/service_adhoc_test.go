package main

import (
	"bytes"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdhocStatusService(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")

	result := server.adhocStatusService(wsDir)
	assert.False(t, result.Running)
	assert.Empty(t, result.Output)
	assert.Equal(t, "adhoc status", result.Message)
}

func TestAdhocStartServiceEmptyPrompt(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")

	result := server.adhocStartService(wsDir, "", "claude-opus-4")
	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "required")
}

func TestAdhocStartServiceEmptyModel(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")

	result := server.adhocStartService(wsDir, "do something", "")
	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "required")
}

func TestAdhocStopService(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws")

	result := server.adhocStopService(wsDir)
	assert.False(t, result.Running)
	assert.Equal(t, "ad-hoc stopped", result.Message)
}

func TestAdhocStartServiceAlreadyRunningReturnsExisting(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "test-ws-adhoc-running")
	st := server.getAdhocState(wsDir)
	st.mu.Lock()
	st.running = true
	st.output.WriteString("test output")
	st.mu.Unlock()
	result := server.adhocStartService(wsDir, "prompt", "model")
	require.NoError(t, result.Error)
	assert.True(t, result.Running)
	assert.Contains(t, result.Output, "test output")
}

func TestGetAdhocStateCreation(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "adhoc-create")
	st1 := srv.getAdhocState(wsDir)
	st2 := srv.getAdhocState(wsDir)
	assert.Same(t, st1, st2)
}

func TestAdhocStopNotRunning(t *testing.T) {
	st := new(adhocPromptState)
	st.stop()
	assert.False(t, st.running)
}

func TestLockedWriterStripsANSI(t *testing.T) {
	var mu sync.Mutex
	var buf bytes.Buffer
	w := &lockedWriter{mu: &mu, buf: &buf}
	_, err := w.Write([]byte("\x1b[31mred text\x1b[0m"))
	require.NoError(t, err)
	assert.Equal(t, "red text", buf.String())
}

func TestLockedWriterPlainTextPassthrough(t *testing.T) {
	var mu sync.Mutex
	var buf bytes.Buffer
	w := &lockedWriter{mu: &mu, buf: &buf}
	n, err := w.Write([]byte("plain text"))
	require.NoError(t, err)
	assert.Equal(t, 10, n)
	assert.Equal(t, "plain text", buf.String())
}

func TestAnsiEscapePatternMatches(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"colorCode", "\x1b[31mhello\x1b[0m", "hello"},
		{"boldCode", "\x1b[1mbold\x1b[22m", "bold"},
		{"noAnsi", "plain text", "plain text"},
		{"mixed", "before\x1b[32mgreen\x1b[0mafter", "beforegreenafter"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ansiEscapePattern.ReplaceAllString(tt.input, "")
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRunScriptActionLogsQuotedArguments(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "script-log-ws")

	result := server.runScriptAction(wsDir, "Print", []string{"printf", "%s", "hello world"})
	require.NoError(t, result.Error)

	require.Eventually(t, func() bool {
		st := server.getAdhocState(wsDir)
		st.mu.Lock()
		defer st.mu.Unlock()
		return !st.running
	}, time.Second, 10*time.Millisecond)

	st := server.getAdhocState(wsDir)
	st.mu.Lock()
	output := st.output.String()
	st.mu.Unlock()

	assert.Contains(t, output, `$ printf %s "hello world"`)
	assert.NotContains(t, output, "$ printf %s hello world")
}

func TestStartCommandServiceFlushesTrailingPartialOutput(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "partial-output-ws")

	result := server.startCommandService(wsDir, &commandStartSpec{
		command:               "sh",
		args:                  []string{"-c", "printf trailing-partial"},
		stdin:                 "",
		env:                   nil,
		logLabel:              "test",
		headerLines:           nil,
		startedMessage:        "started",
		alreadyRunningMessage: "already running",
	})
	require.NoError(t, result.Error)

	require.Eventually(t, func() bool {
		st := server.getAdhocState(wsDir)
		st.mu.Lock()
		defer st.mu.Unlock()
		return !st.running
	}, time.Second, 10*time.Millisecond)

	st := server.getAdhocState(wsDir)
	st.mu.Lock()
	output := st.output.String()
	st.mu.Unlock()

	assert.Regexp(t, `(?m)^\[\d{2}:\d{2}:\d{2}\]\[partial-output-ws\]\[test:0000\] trailing-partial$`, output)
}

func TestStartCommandServiceKeepsStdoutAndStderrPartialLinesSeparate(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "split-streams-ws")

	result := server.startCommandService(wsDir, &commandStartSpec{
		command:               "sh",
		args:                  []string{"-c", "printf stdout; sleep 0.05; printf stderr >&2; sleep 0.05; printf ' tail\\n'; sleep 0.05; printf ' tail\\n' >&2"},
		stdin:                 "",
		env:                   nil,
		logLabel:              "test",
		headerLines:           nil,
		startedMessage:        "started",
		alreadyRunningMessage: "already running",
	})
	require.NoError(t, result.Error)

	require.Eventually(t, func() bool {
		st := server.getAdhocState(wsDir)
		st.mu.Lock()
		defer st.mu.Unlock()
		return !st.running
	}, 2*time.Second, 10*time.Millisecond)

	st := server.getAdhocState(wsDir)
	st.mu.Lock()
	output := st.output.String()
	st.mu.Unlock()

	assert.Regexp(t, `(?m)^\[\d{2}:\d{2}:\d{2}\]\[split-streams-ws\]\[test:0000\] stdout tail$`, output)
	assert.Regexp(t, `(?m)^\[\d{2}:\d{2}:\d{2}\]\[split-streams-ws\]\[test:0000\] stderr tail$`, output)
	assert.NotContains(t, output, "stdoutstderr")
	assert.NotContains(t, output, "stderrstdout")
}

func TestAdhocStopServiceDoesNotReportStopAsCommandError(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, server, rootDir, "stop-coordination-ws")

	result := server.startCommandService(wsDir, &commandStartSpec{
		command:               "sleep",
		args:                  []string{"30"},
		stdin:                 "",
		env:                   nil,
		logLabel:              "test",
		headerLines:           nil,
		startedMessage:        "started",
		alreadyRunningMessage: "already running",
	})
	require.NoError(t, result.Error)

	require.Eventually(t, func() bool {
		st := server.getAdhocState(wsDir)
		st.mu.Lock()
		defer st.mu.Unlock()
		return st.running && st.cmd != nil
	}, time.Second, 10*time.Millisecond)

	stopResult := server.adhocStopService(wsDir)
	assert.Contains(t, stopResult.Output, "[stopped by user]")
	assert.Regexp(t, `(?m)^\[\d{2}:\d{2}:\d{2}\]\[stop-coordination-ws\]\[test:0000\] \[stopped by user\]$`, stopResult.Output)

	require.Eventually(t, func() bool {
		st := server.getAdhocState(wsDir)
		st.mu.Lock()
		defer st.mu.Unlock()
		return !st.running && st.cmd == nil
	}, time.Second, 10*time.Millisecond)

	st := server.getAdhocState(wsDir)
	st.mu.Lock()
	output := st.output.String()
	st.mu.Unlock()

	assert.NotContains(t, output, "[command exited with error:", output)
	assert.Regexp(t, `(?m)^\[\d{2}:\d{2}:\d{2}\]\[stop-coordination-ws\]\[test:0000\] \[stopped by user\]$`, output)
}
