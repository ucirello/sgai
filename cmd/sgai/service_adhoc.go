package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
)

type adhocStatusResult struct {
	Running bool
	Output  string
	Message string
}

func (s *Server) adhocStatusService(workspacePath string) adhocStatusResult {
	st := s.getAdhocState(workspacePath)
	st.mu.Lock()
	running := st.running
	output := st.output.String()
	st.mu.Unlock()

	return adhocStatusResult{Running: running, Output: output, Message: "adhoc status"}
}

type adhocStartResult struct {
	Running bool
	Output  string
	Message string
	Error   error
}

func adhocStartError(err error) adhocStartResult {
	return adhocStartResult{Running: false, Output: "", Message: "", Error: err}
}

func adhocStartRunning(output, message string) adhocStartResult {
	return adhocStartResult{Running: true, Output: output, Message: message, Error: nil}
}

type commandStartSpec struct {
	command               string
	args                  []string
	stdin                 string
	env                   []string
	logLabel              string
	headerLines           []string
	startedMessage        string
	alreadyRunningMessage string
}

type actionCommandSpec struct {
	command string
	args    []string
	stdin   string
	env     []string
}

func buildPromptActionCommandSpec(workspacePath, prompt, model string) actionCommandSpec {
	return actionCommandSpec{
		command: "opencode",
		args:    buildAdhocArgs(model),
		stdin:   prompt,
		env:     []string{"OPENCODE_CONFIG_DIR=" + filepath.Join(workspacePath, ".sgai")},
	}
}

func buildScriptActionCommandSpec(argv []string) (actionCommandSpec, error) {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return actionCommandSpec{}, errors.New("rendered an empty command")
	}
	return actionCommandSpec{command: argv[0], args: argv[1:], stdin: "", env: nil}, nil
}

func (s *Server) adhocStartService(workspacePath, prompt, model string) adhocStartResult {
	prompt = strings.TrimSpace(prompt)
	model = strings.TrimSpace(model)
	if prompt == "" || model == "" {
		return adhocStartError(errors.New("prompt and model are required"))
	}

	actionSpec := buildPromptActionCommandSpec(workspacePath, prompt, model)
	return s.startCommandService(workspacePath, &commandStartSpec{
		command:               actionSpec.command,
		args:                  actionSpec.args,
		stdin:                 actionSpec.stdin,
		env:                   actionSpec.env,
		logLabel:              "adhoc",
		headerLines:           []string{"$ opencode " + strings.Join(actionSpec.args, " "), "prompt: " + prompt},
		startedMessage:        "ad-hoc prompt started",
		alreadyRunningMessage: "ad-hoc prompt already running",
	})
}

func (s *Server) runPromptAction(workspacePath, prompt, model string) adhocStartResult {
	if s.promptActionRunner != nil {
		return s.promptActionRunner(workspacePath, prompt, model)
	}
	return s.adhocStartService(workspacePath, prompt, model)
}

func (s *Server) runScriptAction(workspacePath, actionName string, argv []string) adhocStartResult {
	if s.scriptActionRunner != nil {
		return s.scriptActionRunner(workspacePath, actionName, argv)
	}
	actionSpec, errSpec := buildScriptActionCommandSpec(argv)
	if errSpec != nil {
		return adhocStartError(fmt.Errorf("action %q %w", actionName, errSpec))
	}
	return s.startCommandService(workspacePath, &commandStartSpec{
		command:               actionSpec.command,
		args:                  actionSpec.args,
		stdin:                 actionSpec.stdin,
		env:                   actionSpec.env,
		logLabel:              "action",
		headerLines:           []string{"action: " + actionName, formatCommandForLog(argv)},
		startedMessage:        "action started",
		alreadyRunningMessage: "action already running",
	})
}

func formatCommandForLog(argv []string) string {
	if len(argv) == 0 {
		return "$"
	}
	parts := make([]string, 0, len(argv))
	for _, arg := range argv {
		parts = append(parts, formatCommandArgForLog(arg))
	}
	return "$ " + strings.Join(parts, " ")
}

func formatCommandArgForLog(arg string) string {
	if arg == "" {
		return strconv.Quote(arg)
	}
	if strings.IndexFunc(arg, func(r rune) bool {
		return unicode.IsSpace(r) || !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._/:=+,%@", r)
	}) != -1 {
		return strconv.Quote(arg)
	}
	return arg
}

func (s *Server) startCommandService(workspacePath string, spec *commandStartSpec) adhocStartResult {
	st := s.getAdhocState(workspacePath)
	st.mu.Lock()
	if st.running {
		output := st.output.String()
		st.mu.Unlock()
		return adhocStartRunning(output, spec.alreadyRunningMessage)
	}

	st.running = true
	st.output.Reset()
	st.stopRequested = false

	ctx := s.shutdownCtx
	if ctx == nil {
		ctx = context.Background()
	}

	cmd := exec.CommandContext(ctx, spec.command, spec.args...)
	cmd.Dir = workspacePath
	sysProcAttr := new(syscall.SysProcAttr)
	sysProcAttr.Setpgid = true
	cmd.SysProcAttr = sysProcAttr
	if len(spec.env) > 0 {
		cmd.Env = opencodeEnv(spec.env...)
	}
	if spec.stdin != "" {
		cmd.Stdin = strings.NewReader(spec.stdin)
	}
	writer := &lockedWriter{mu: &st.mu, buf: &st.output}
	prefix := fmt.Sprintf("[%s][%s:0000]", filepath.Base(workspacePath), spec.logLabel)
	st.linePrefix = prefix + " "
	stdoutPW := newPrefixWriter(prefix+" ", os.Stdout, time.Now)
	stderrPW := newPrefixWriter(prefix+" ", os.Stderr, time.Now)
	stdoutBufferPW := newPrefixWriter(prefix+" ", writer, time.Now)
	stderrBufferPW := newPrefixWriter(prefix+" ", writer, time.Now)
	statePW := newPrefixWriter(prefix+" ", &st.output, time.Now)
	cmd.Stdout = io.MultiWriter(stdoutPW, stdoutBufferPW)
	cmd.Stderr = io.MultiWriter(stderrPW, stderrBufferPW)
	for _, line := range spec.headerLines {
		_, _ = fmt.Fprintln(stderrPW, line)
		_, _ = fmt.Fprintln(statePW, line)
	}

	if errStart := cmd.Start(); errStart != nil {
		st.running = false
		st.mu.Unlock()
		return adhocStartError(fmt.Errorf("failed to start command: %w", errStart))
	}

	st.cmd = cmd
	waitDone := make(chan struct{})
	st.waitDone = waitDone
	st.mu.Unlock()

	go func() {
		errWait := cmd.Wait()
		flushPrefixWriterWithLog("command stdout", stdoutPW)
		flushPrefixWriterWithLog("command stderr", stderrPW)
		flushPrefixWriterWithLog("command stdout buffer", stdoutBufferPW)
		flushPrefixWriterWithLog("command stderr buffer", stderrBufferPW)
		st.mu.Lock()
		stoppedByUser := st.stopRequested
		st.running = false
		st.cmd = nil
		st.waitDone = nil
		st.stopRequested = false
		close(waitDone)
		st.mu.Unlock()
		if errWait != nil && !stoppedByUser {
			_, _ = fmt.Fprintln(stderrPW, "command exited with error:", errWait)
			_, _ = fmt.Fprintln(stderrBufferPW, "command exited with error:", errWait)
		}
		s.notifyStateChange()
	}()

	s.notifyStateChange()

	return adhocStartRunning("", spec.startedMessage)
}

type adhocStopResult struct {
	Running bool
	Output  string
	Message string
}

func (s *Server) adhocStopService(workspacePath string) adhocStopResult {
	st := s.getAdhocState(workspacePath)
	st.stop()
	s.notifyStateChange()

	st.mu.Lock()
	output := st.output.String()
	st.mu.Unlock()

	return adhocStopResult{Running: false, Output: output, Message: "ad-hoc stopped"}
}
