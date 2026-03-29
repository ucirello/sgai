package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

type cliRunDeps struct {
	workingDir      func() (string, error)
	runPromptAction func(context.Context, *actionExecutionPlan, io.Writer, io.Writer) error
	runScriptAction func(context.Context, *actionExecutionPlan, io.Writer, io.Writer) error
}

type actionVarFlagValues struct {
	values map[string]string
}

func (v *actionVarFlagValues) String() string {
	return ""
}

func (v *actionVarFlagValues) Set(value string) error {
	key, val, found := strings.Cut(value, "=")
	if !found || strings.TrimSpace(key) == "" {
		return errors.New("--var must use key=value")
	}
	if v.values == nil {
		v.values = make(map[string]string)
	}
	v.values[key] = val
	return nil
}

func (v *actionVarFlagValues) clone() map[string]string {
	return cloneActionValues(v.values)
}

func cmdRun(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runActionSubcommand(ctx, args, os.Stdin, os.Stdout, os.Stderr, defaultCLIRunDeps())
}

func defaultCLIRunDeps() cliRunDeps {
	return cliRunDeps{
		workingDir:      os.Getwd,
		runPromptAction: runPromptActionCLI,
		runScriptAction: runScriptActionCLI,
	}
}

func runActionSubcommand(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, deps cliRunDeps) int {
	stderr = normalizeCLIWriter(stderr)
	errRun := runActionCommand(ctx, args, stdin, stdout, stderr, deps)
	switch {
	case errRun == nil, errors.Is(errRun, flag.ErrHelp):
		return 0
	case errors.Is(errRun, context.Canceled):
		return 130
	default:
		_, _ = fmt.Fprintln(stderr, errRun)
		return 1
	}
}

func runActionCommand(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, deps cliRunDeps) error {
	deps = fillCLIRunDeps(deps)
	stderr = normalizeCLIWriter(stderr)
	stdout = normalizeCLIWriter(stdout)

	runFlags := flag.NewFlagSet("run", flag.ContinueOnError)
	runFlags.SetOutput(stderr)
	runFlags.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "Usage: sgai run [--config path/to/sgai.json] [--var key=value ...] <action-name>")
	}

	configPath := runFlags.String("config", "", "Path to sgai.json")
	var values actionVarFlagValues
	runFlags.Var(&values, "var", "Action variable as key=value; repeat as needed")

	if errParse := runFlags.Parse(args); errParse != nil {
		return fmt.Errorf("parse run flags: %w", errParse)
	}

	remaining := runFlags.Args()
	if len(remaining) != 1 {
		runFlags.Usage()
		return errors.New("run requires exactly one action name")
	}

	workspacePath, resolvedConfigPath, errPaths := resolveRunPaths(deps.workingDir, *configPath)
	if errPaths != nil {
		return errPaths
	}

	reader := bufio.NewReader(stdin)
	plan, errPrepare := prepareActionExecution(workspacePath, resolvedConfigPath, remaining[0], values.clone(), func(name string) (string, error) {
		return promptForActionVariable(reader, stderr, name)
	})
	if errPrepare != nil {
		return errPrepare
	}

	if plan.parsed.kind == actionKindPrompt {
		return deps.runPromptAction(ctx, &plan, stdout, stderr)
	}
	return deps.runScriptAction(ctx, &plan, stdout, stderr)
}

func fillCLIRunDeps(deps cliRunDeps) cliRunDeps {
	if deps.workingDir == nil {
		deps.workingDir = os.Getwd
	}
	if deps.runPromptAction == nil {
		deps.runPromptAction = runPromptActionCLI
	}
	if deps.runScriptAction == nil {
		deps.runScriptAction = runScriptActionCLI
	}
	return deps
}

func normalizeCLIWriter(w io.Writer) io.Writer {
	if w != nil {
		return w
	}
	return io.Discard
}

func resolveRunPaths(workingDir func() (string, error), configPath string) (workspacePath, resolvedConfigPath string, err error) {
	workspacePath, errWorkingDir := workingDir()
	if errWorkingDir != nil {
		return "", "", fmt.Errorf("get working directory: %w", errWorkingDir)
	}
	if strings.TrimSpace(configPath) == "" {
		return workspacePath, "", nil
	}

	resolvedConfigPath = configPath
	if !filepath.IsAbs(resolvedConfigPath) {
		resolvedConfigPath = filepath.Join(workspacePath, resolvedConfigPath)
	}
	resolvedConfigPath = filepath.Clean(resolvedConfigPath)
	return filepath.Dir(resolvedConfigPath), resolvedConfigPath, nil
}

func promptForActionVariable(reader *bufio.Reader, out io.Writer, name string) (string, error) {
	if _, errWrite := fmt.Fprintf(out, "%s: ", name); errWrite != nil {
		return "", fmt.Errorf("write prompt: %w", errWrite)
	}
	line, errRead := reader.ReadString('\n')
	if errRead != nil && !errors.Is(errRead, io.EOF) {
		return "", fmt.Errorf("read prompt: %w", errRead)
	}
	if errRead != nil && line == "" {
		return "", fmt.Errorf("read prompt: %w", errRead)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func runPromptActionCLI(ctx context.Context, plan *actionExecutionPlan, stdout, stderr io.Writer) error {
	spec := buildPromptActionCommandSpec(plan.workspacePath, plan.rendered, plan.parsed.model)
	return runCLIAction(ctx, plan.workspacePath, &spec, stdout, stderr)
}

func runScriptActionCLI(ctx context.Context, plan *actionExecutionPlan, stdout, stderr io.Writer) error {
	spec, errSpec := buildScriptActionCommandSpec(plan.argv)
	if errSpec != nil {
		return fmt.Errorf("action %q %w", plan.actionName, errSpec)
	}
	return runCLIAction(ctx, plan.workspacePath, &spec, stdout, stderr)
}

func runCLIAction(ctx context.Context, workspacePath string, spec *actionCommandSpec, stdout, stderr io.Writer) error {
	if errContext := ctx.Err(); errContext != nil {
		return fmt.Errorf("check command context: %w", errContext)
	}

	cmd := exec.Command(spec.command, spec.args...)
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
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if errStart := cmd.Start(); errStart != nil {
		return fmt.Errorf("running %s: %w", spec.command, errStart)
	}

	processExited := make(chan struct{})
	go terminateProcessGroupOnCancel(ctx, cmd, processExited)

	errRun := cmd.Wait()
	close(processExited)
	if errContext := ctx.Err(); errContext != nil {
		if errRun == nil {
			return fmt.Errorf("running %s: %w", spec.command, errContext)
		}
		return fmt.Errorf("running %s: %w", spec.command, errors.Join(errContext, errRun))
	}
	if errRun != nil {
		return fmt.Errorf("running %s: %w", spec.command, errRun)
	}
	return nil
}
