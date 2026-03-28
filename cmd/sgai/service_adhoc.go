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

func (s *Server) adhocStartService(workspacePath, prompt, model string) adhocStartResult {
	prompt = strings.TrimSpace(prompt)
	model = strings.TrimSpace(model)
	if prompt == "" || model == "" {
		return adhocStartError(errors.New("prompt and model are required"))
	}

	args := buildAdhocArgs(model)
	return s.startCommandService(workspacePath, &commandStartSpec{
		command:               "opencode",
		args:                  args,
		stdin:                 prompt,
		env:                   []string{"OPENCODE_CONFIG_DIR=" + filepath.Join(workspacePath, ".sgai")},
		logLabel:              "adhoc",
		headerLines:           []string{"$ opencode " + strings.Join(args, " "), "prompt: " + prompt},
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
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return adhocStartError(fmt.Errorf("action %q rendered an empty command", actionName))
	}
	return s.startCommandService(workspacePath, &commandStartSpec{
		command:               argv[0],
		args:                  argv[1:],
		stdin:                 "",
		env:                   nil,
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
	stdoutPW := &prefixWriter{prefix: prefix + " ", w: os.Stdout, startTime: time.Now()}
	stderrPW := &prefixWriter{prefix: prefix + " ", w: os.Stderr, startTime: time.Now()}
	cmd.Stdout = io.MultiWriter(stdoutPW, writer)
	cmd.Stderr = io.MultiWriter(stderrPW, writer)
	for _, line := range spec.headerLines {
		_, _ = fmt.Fprintln(stderrPW, line)
		st.output.WriteString(line + "\n")
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
		st.mu.Lock()
		stoppedByUser := st.stopRequested
		if errWait != nil && !stoppedByUser {
			st.output.WriteString("\n[command exited with error: " + errWait.Error() + "]\n")
		}
		st.running = false
		st.cmd = nil
		st.waitDone = nil
		st.stopRequested = false
		close(waitDone)
		st.mu.Unlock()
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
