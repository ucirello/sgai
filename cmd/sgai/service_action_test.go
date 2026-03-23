package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActionRunServicePromptAction(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "prompt-action-ws")
	writeActionTestConfig(t, wsDir, projectConfig{
		Actions: []actionConfig{{
			Name:   "Summarize",
			Model:  "openai/gpt-5.4",
			Prompt: "hello {{ .Name }}",
		}},
	})

	var gotWorkspace string
	var gotPrompt string
	var gotModel string
	server.promptActionRunner = func(workspacePath, prompt, model string) adhocStartResult {
		gotWorkspace = workspacePath
		gotPrompt = prompt
		gotModel = model
		return adhocStartResult{Running: true, Message: "started"}
	}

	result := server.actionRunService(wsDir, "Summarize", map[string]string{"Name": "Ada"})
	require.NoError(t, result.Error)
	assert.True(t, result.Running)
	assert.Equal(t, wsDir, gotWorkspace)
	assert.Equal(t, "hello Ada", gotPrompt)
	assert.Equal(t, "openai/gpt-5.4", gotModel)
}

func TestActionRunServiceScriptAction(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "script-action-ws")
	writeActionTestConfig(t, wsDir, projectConfig{
		Actions: []actionConfig{{
			Name:   "Print",
			Script: `printf "%s" "{{ .Message }}"`,
		}},
	})

	var gotWorkspace string
	var gotName string
	var gotArgv []string
	server.scriptActionRunner = func(workspacePath, actionName string, argv []string) adhocStartResult {
		gotWorkspace = workspacePath
		gotName = actionName
		gotArgv = append([]string(nil), argv...)
		return adhocStartResult{Running: true, Message: "started"}
	}

	result := server.actionRunService(wsDir, "Print", map[string]string{"Message": "hello world"})
	require.NoError(t, result.Error)
	assert.True(t, result.Running)
	assert.Equal(t, wsDir, gotWorkspace)
	assert.Equal(t, "Print", gotName)
	assert.Equal(t, []string{"printf", "%s", "hello world"}, gotArgv)
}

func TestActionRunServiceScriptActionPreservesBackslashes(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "script-action-backslashes-ws")
	writeActionTestConfig(t, wsDir, projectConfig{
		Actions: []actionConfig{{
			Name:   "Print",
			Script: `printf "%s" "{{ .Message }}"`,
		}},
	})

	var gotArgv []string
	server.scriptActionRunner = func(_, _ string, argv []string) adhocStartResult {
		gotArgv = append([]string(nil), argv...)
		return adhocStartResult{Running: true, Message: "started"}
	}

	result := server.actionRunService(wsDir, "Print", map[string]string{"Message": `C:\tmp\logs`})
	require.NoError(t, result.Error)
	assert.Equal(t, []string{"printf", "%s", `C:\tmp\logs`}, gotArgv)
}

func TestActionRunServiceRejectsInvalidActionAtRunTime(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "invalid-action-ws")
	writeActionTestConfig(t, wsDir, projectConfig{
		Actions: []actionConfig{{
			Name:   "Broken",
			Model:  "openai/gpt-5.4",
			Prompt: "hello",
			Script: "printf test",
		}},
	})

	result := server.actionRunService(wsDir, "Broken", nil)
	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "exactly one of prompt or script")
}

func TestActionRunServiceRejectsDuplicateActionNames(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "duplicate-action-ws")
	writeActionTestConfig(t, wsDir, projectConfig{
		Actions: []actionConfig{
			{
				Name:   "Repeat",
				Model:  "openai/gpt-5.4",
				Prompt: "hello",
			},
			{
				Name:   " Repeat ",
				Script: `printf "%s" "hello"`,
			},
		},
	})

	result := server.actionRunService(wsDir, "Repeat", nil)
	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "must be unique")
}

func TestActionRunServicePropagatesConfigLoadErrors(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "broken-config-action-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, configFileName), []byte("not valid json"), 0o644))

	result := server.actionRunService(wsDir, "Anything", nil)
	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "invalid JSON syntax")
}

func TestActionRunServiceRejectsMissingVariables(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "missing-vars-ws")
	writeActionTestConfig(t, wsDir, projectConfig{
		Actions: []actionConfig{{
			Name:   "Summarize",
			Model:  "openai/gpt-5.4",
			Prompt: "hello {{ .Name }}",
		}},
	})

	result := server.actionRunService(wsDir, "Summarize", nil)
	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "Name")
}

func TestActionRunServiceRejectsShellOperators(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "shell-operator-action-ws")
	writeActionTestConfig(t, wsDir, projectConfig{
		Actions: []actionConfig{{
			Name:   "Broken",
			Script: `printf "%s" hello | cat`,
		}},
	})

	result := server.actionRunService(wsDir, "Broken", nil)
	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "rendered an invalid command")
	assert.Contains(t, result.Error.Error(), `unsupported shell operator "|"`)
}

func TestActionRunServiceRejectsInvalidRenderedCommand(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "rendered-shell-operator-action-ws")
	writeActionTestConfig(t, wsDir, projectConfig{
		Actions: []actionConfig{{
			Name:   "Print",
			Script: `printf "%s" {{ .Message }}`,
		}},
	})

	ran := false
	server.scriptActionRunner = func(_, _ string, _ []string) adhocStartResult {
		ran = true
		return adhocStartResult{Running: true, Message: "started"}
	}

	result := server.actionRunService(wsDir, "Print", map[string]string{"Message": "hello|cat"})
	require.Error(t, result.Error)
	assert.False(t, ran)
	assert.Contains(t, result.Error.Error(), "rendered an invalid command")
	assert.Contains(t, result.Error.Error(), `unsupported shell operator "|"`)
}

func TestHandleAPIActionRunInvalidBody(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, rootDir, "api-action-run-ws")

	w := serveHTTP(server, "POST", "/api/v1/workspaces/api-action-run-ws/actions/run", "{bad}")
	assert.Equal(t, 400, w.Code)
}

func TestHandleAPIActionRunMissingName(t *testing.T) {
	server, rootDir := setupTestServer(t)
	setupTestWorkspace(t, rootDir, "api-action-run-name-ws")

	w := serveHTTP(server, "POST", "/api/v1/workspaces/api-action-run-name-ws/actions/run", `{"variables":{"Name":"Ada"}}`)
	assert.Equal(t, 400, w.Code)
}

func TestHandleAPIActionRunPropagatesConfigLoadErrors(t *testing.T) {
	server, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, rootDir, "api-action-run-config-ws")
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, configFileName), []byte("not valid json"), 0o644))

	w := serveHTTP(server, "POST", "/api/v1/workspaces/api-action-run-config-ws/actions/run", `{"name":"Anything"}`)
	assert.Equal(t, 400, w.Code)
	assert.Contains(t, w.Body.String(), "invalid JSON syntax")
}

func writeActionTestConfig(t *testing.T, dir string, cfg projectConfig) {
	t.Helper()
	data, errMarshal := json.Marshal(cfg)
	require.NoError(t, errMarshal)
	require.NoError(t, os.WriteFile(filepath.Join(dir, configFileName), data, 0o644))
}
