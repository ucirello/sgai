package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRunTestProjectConfig(actions ...actionConfig) projectConfig {
	config := newTestProjectConfig()
	config.Actions = actions
	return config
}

func newRunTestScriptActionConfig(name, script string) actionConfig {
	config := newTestActionConfig()
	config.Name = name
	config.Script = script
	return config
}

func newRunTestPromptActionConfig(name, model, prompt string) actionConfig {
	config := newTestActionConfig()
	config.Name = name
	config.Model = model
	config.Prompt = prompt
	return config
}

func newRunTestDeps(workingDir func() (string, error)) cliRunDeps {
	deps := defaultCLIRunDeps()
	deps.workingDir = workingDir
	return deps
}

func TestRunActionCommandUsesExplicitConfigPath(t *testing.T) {
	configDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, ".sgai"), 0o755))
	writeActionTestConfig(t, configDir, newRunTestProjectConfig(newRunTestScriptActionConfig("Print", `printf "%s" "{{ .Message }}"`)))

	var gotPlan actionExecutionPlan
	deps := newRunTestDeps(func() (string, error) {
		return filepath.Join(t.TempDir(), "ignored-working-dir"), nil
	})
	deps.runPromptAction = func(context.Context, *actionExecutionPlan, io.Writer, io.Writer) error {
		t.Fatalf("unexpected prompt action dispatch")
		return nil
	}
	deps.runScriptAction = func(_ context.Context, plan *actionExecutionPlan, _, _ io.Writer) error {
		gotPlan = *plan
		return nil
	}

	errRun := runActionCommand(t.Context(), []string{"--config", filepath.Join(configDir, configFileName), "--var", "Message=hello world", "Print"}, bytes.NewBuffer(nil), io.Discard, io.Discard, deps)
	require.NoError(t, errRun)
	assert.Equal(t, configDir, gotPlan.workspacePath)
	assert.Equal(t, "Print", gotPlan.actionName)
	assert.Equal(t, actionKindScript, gotPlan.parsed.kind)
	assert.Equal(t, []string{"printf", "%s", "hello world"}, gotPlan.argv)
}

func TestRunActionCommandUsesWorkspaceConfigWhenConfigIsOmitted(t *testing.T) {
	workingDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workingDir, ".sgai"), 0o755))
	writeActionTestConfig(t, workingDir, newRunTestProjectConfig(newRunTestScriptActionConfig("Print", `printf "%s" "{{ .Message }}"`)))

	var gotPlan actionExecutionPlan
	deps := newRunTestDeps(func() (string, error) {
		return workingDir, nil
	})
	deps.runPromptAction = func(context.Context, *actionExecutionPlan, io.Writer, io.Writer) error {
		t.Fatalf("unexpected prompt action dispatch")
		return nil
	}
	deps.runScriptAction = func(_ context.Context, plan *actionExecutionPlan, _, _ io.Writer) error {
		gotPlan = *plan
		return nil
	}

	errRun := runActionCommand(t.Context(), []string{"--var", "Message=hello from workspace", "Print"}, bytes.NewBuffer(nil), io.Discard, io.Discard, deps)
	require.NoError(t, errRun)
	assert.Equal(t, workingDir, gotPlan.workspacePath)
	assert.Equal(t, "Print", gotPlan.actionName)
	assert.Equal(t, actionKindScript, gotPlan.parsed.kind)
	assert.Equal(t, []string{"printf", "%s", "hello from workspace"}, gotPlan.argv)
}

func TestRunActionCommandAcceptsRepeatedVarFlags(t *testing.T) {
	configDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, ".sgai"), 0o755))
	writeActionTestConfig(t, configDir, newRunTestProjectConfig(newRunTestScriptActionConfig("Print", `printf "%s/%s" "{{ .Greeting }}" "{{ .Target }}"`)))

	var gotPlan actionExecutionPlan
	deps := newRunTestDeps(func() (string, error) {
		return t.TempDir(), nil
	})
	deps.runPromptAction = func(context.Context, *actionExecutionPlan, io.Writer, io.Writer) error {
		t.Fatalf("unexpected prompt action dispatch")
		return nil
	}
	deps.runScriptAction = func(_ context.Context, plan *actionExecutionPlan, _, _ io.Writer) error {
		gotPlan = *plan
		return nil
	}

	errRun := runActionCommand(t.Context(), []string{"--config", filepath.Join(configDir, configFileName), "--var", "Greeting=hello", "--var", "Target=world", "Print"}, bytes.NewBuffer(nil), io.Discard, io.Discard, deps)
	require.NoError(t, errRun)
	assert.Equal(t, actionKindScript, gotPlan.parsed.kind)
	assert.Equal(t, []string{"printf", "%s/%s", "hello", "world"}, gotPlan.argv)
}

func TestRunActionCommandPromptsOnlyForMissingVariables(t *testing.T) {
	configDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, ".sgai"), 0o755))
	writeActionTestConfig(t, configDir, newRunTestProjectConfig(newRunTestPromptActionConfig("Summarize", "openai/gpt-5.4", "hello {{ .Name }} about {{ .Topic }}")))

	var gotPlan actionExecutionPlan
	deps := newRunTestDeps(func() (string, error) {
		return t.TempDir(), nil
	})
	deps.runPromptAction = func(_ context.Context, plan *actionExecutionPlan, _, _ io.Writer) error {
		gotPlan = *plan
		return nil
	}
	deps.runScriptAction = func(context.Context, *actionExecutionPlan, io.Writer, io.Writer) error {
		t.Fatalf("unexpected script action dispatch")
		return nil
	}

	var stderr bytes.Buffer
	errRun := runActionCommand(t.Context(), []string{"--config", filepath.Join(configDir, configFileName), "--var", "Name=Ada", "Summarize"}, bytes.NewBufferString("compiler design\n"), io.Discard, &stderr, deps)
	require.NoError(t, errRun)
	assert.Equal(t, actionKindPrompt, gotPlan.parsed.kind)
	assert.Equal(t, "hello Ada about compiler design", gotPlan.rendered)
	assert.Contains(t, stderr.String(), "Topic")
	assert.NotContains(t, stderr.String(), "Name")
}

func TestRunActionCommandFallsBackToDefaultActionsWhenConfigIsAbsent(t *testing.T) {
	workingDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workingDir, ".sgai"), 0o755))

	var gotPlan actionExecutionPlan
	deps := newRunTestDeps(func() (string, error) {
		return workingDir, nil
	})
	deps.runPromptAction = func(_ context.Context, plan *actionExecutionPlan, _, _ io.Writer) error {
		gotPlan = *plan
		return nil
	}
	deps.runScriptAction = func(context.Context, *actionExecutionPlan, io.Writer, io.Writer) error {
		t.Fatalf("unexpected script action dispatch")
		return nil
	}

	errRun := runActionCommand(t.Context(), []string{"Start Application"}, bytes.NewBuffer(nil), io.Discard, io.Discard, deps)
	require.NoError(t, errRun)
	assert.Equal(t, workingDir, gotPlan.workspacePath)
	assert.Equal(t, "Start Application", gotPlan.actionName)
	assert.Equal(t, actionKindPrompt, gotPlan.parsed.kind)
	assert.Contains(t, gotPlan.rendered, "start the application server")
}

func TestRunActionCommandRejectsUnknownAction(t *testing.T) {
	workingDir := t.TempDir()
	deps := newRunTestDeps(func() (string, error) {
		return workingDir, nil
	})

	errRun := runActionCommand(t.Context(), []string{"Unknown Action"}, bytes.NewBuffer(nil), io.Discard, io.Discard, deps)
	require.Error(t, errRun)
	assert.Contains(t, errRun.Error(), `action "Unknown Action" not found`)
}

func TestRunActionCommandRejectsInvalidConfig(t *testing.T) {
	configDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, ".sgai"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, configFileName), []byte("not valid json"), 0o644))

	deps := newRunTestDeps(func() (string, error) {
		return t.TempDir(), nil
	})

	errRun := runActionCommand(t.Context(), []string{"--config", filepath.Join(configDir, configFileName), "Anything"}, bytes.NewBuffer(nil), io.Discard, io.Discard, deps)
	require.Error(t, errRun)
	assert.Contains(t, errRun.Error(), "invalid JSON syntax")
}

func TestRunActionCommandRejectsInvalidVarFlag(t *testing.T) {
	deps := newRunTestDeps(func() (string, error) {
		return t.TempDir(), nil
	})

	errRun := runActionCommand(t.Context(), []string{"--var", "missing-separator", "Anything"}, bytes.NewBuffer(nil), io.Discard, io.Discard, deps)
	require.Error(t, errRun)
	assert.Contains(t, errRun.Error(), "must use key=value")
}

func TestRunActionSubcommandWritesFailureToStderrAndReturnsFailureExitCode(t *testing.T) {
	var stderr bytes.Buffer

	exitCode := runActionSubcommand(t.Context(), nil, bytes.NewBuffer(nil), io.Discard, &stderr, cliRunDeps{
		workingDir:      nil,
		runPromptAction: nil,
		runScriptAction: nil,
	})

	assert.Equal(t, 1, exitCode)
	assert.Contains(t, stderr.String(), "Usage: sgai run")
	assert.Contains(t, stderr.String(), "run requires exactly one action name")
}

func TestRunActionSubcommandReturnsCancellationExitCodeWithoutWritingError(t *testing.T) {
	configDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, ".sgai"), 0o755))
	writeActionTestConfig(t, configDir, newRunTestProjectConfig(newRunTestScriptActionConfig("Wait", `printf waiting`)))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	deps := newRunTestDeps(func() (string, error) {
		return t.TempDir(), nil
	})
	deps.runPromptAction = func(context.Context, *actionExecutionPlan, io.Writer, io.Writer) error {
		t.Fatalf("unexpected prompt action dispatch")
		return nil
	}
	deps.runScriptAction = func(ctx context.Context, _ *actionExecutionPlan, _, _ io.Writer) error {
		return ctx.Err()
	}

	var stderr bytes.Buffer
	exitCode := runActionSubcommand(ctx, []string{"--config", filepath.Join(configDir, configFileName), "Wait"}, bytes.NewBuffer(nil), io.Discard, &stderr, deps)

	assert.Equal(t, 130, exitCode)
	assert.Empty(t, stderr.String())
}

func TestRunCLIActionStopsProcessGroupOnCancel(t *testing.T) {
	workspacePath := t.TempDir()
	scriptPath := writeRunCLIActionCancellationFixture(t, workspacePath)
	groupPath := filepath.Join(workspacePath, "group.txt")

	ctx, cancel := context.WithCancel(t.Context())
	resultCh := make(chan error, 1)
	go func() {
		resultCh <- runCLIAction(ctx, workspacePath, &actionCommandSpec{
			command: scriptPath,
			args:    []string{groupPath},
			stdin:   "",
			env:     nil,
		}, io.Discard, io.Discard)
	}()

	waitForRunTestPath(t, groupPath)
	pgid := readRunTestProcessGroupID(t, groupPath)
	t.Cleanup(func() {
		killRunTestProcessGroup(pgid)
	})

	cancel()
	errRun := waitForRunCLIActionResult(t, resultCh)
	require.Error(t, errRun)
	waitForRunTestProcessGroupExit(t, pgid)
}

func TestRunActionCommandReturnsContextCanceledWhenScriptActionInterrupted(t *testing.T) {
	workspacePath := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".sgai"), 0o755))
	scriptPath := writeRunCLIActionCancellationFixture(t, workspacePath)
	groupPath := filepath.Join(workspacePath, "group.txt")
	writeActionTestConfig(t, workspacePath, newRunTestProjectConfig(newRunTestScriptActionConfig("Wait", strconv.Quote(scriptPath)+" "+strconv.Quote(groupPath))))

	ctx, cancel := context.WithCancel(t.Context())
	resultCh := make(chan error, 1)
	go func() {
		resultCh <- runActionCommand(ctx, []string{"--config", filepath.Join(workspacePath, configFileName), "Wait"}, bytes.NewBuffer(nil), io.Discard, io.Discard, defaultCLIRunDeps())
	}()

	waitForRunTestPath(t, groupPath)
	pgid := readRunTestProcessGroupID(t, groupPath)
	t.Cleanup(func() {
		killRunTestProcessGroup(pgid)
	})

	cancel()
	errRun := waitForRunCLIActionResult(t, resultCh)
	require.Error(t, errRun)
	require.ErrorIs(t, errRun, context.Canceled)
	waitForRunTestProcessGroupExit(t, pgid)
}

func writeRunCLIActionCancellationFixture(t *testing.T, dir string) string {
	t.Helper()

	scriptPath := filepath.Join(dir, "run-cli-action-cancel.sh")
	script := `#!/bin/sh
group_path="$1"

sh -c 'while :; do sleep 1; done' &
child=$!
printf %s "$$" > "$group_path"
wait "$child"
`
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))
	return scriptPath
}

func waitForRunTestPath(t *testing.T, path string) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, errStat := os.Stat(path)
		return errStat == nil
	}, 2*time.Second, 10*time.Millisecond)
}

func waitForRunCLIActionResult(t *testing.T, resultCh <-chan error) error {
	t.Helper()

	select {
	case errRun := <-resultCh:
		return errRun
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for runCLIAction to return")
		return nil
	}
}

func readRunTestProcessGroupID(t *testing.T, groupPath string) int {
	t.Helper()

	data, errRead := os.ReadFile(groupPath)
	require.NoError(t, errRead)

	pgid, errAtoi := strconv.Atoi(strings.TrimSpace(string(data)))
	require.NoError(t, errAtoi)
	require.NotZero(t, pgid)
	return pgid
}

func waitForRunTestProcessGroupExit(t *testing.T, pgid int) {
	t.Helper()
	require.Eventually(t, func() bool {
		errKill := syscall.Kill(-pgid, 0)
		return errKill == syscall.ESRCH
	}, 2*time.Second, 10*time.Millisecond)
}

func killRunTestProcessGroup(pgid int) {
	if pgid == 0 {
		return
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}
