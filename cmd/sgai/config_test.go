package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newConfigTestActionConfig() actionConfig {
	return actionConfig{
		Name:        "",
		Model:       "",
		Prompt:      "",
		Script:      "",
		Description: "",
	}
}

func newConfigTestProjectConfig() projectConfig {
	return projectConfig{
		DefaultModel: "",
		MCP:          nil,
		Editor:       "",
		Actions:      nil,
	}
}

func newConfigTestGoalMetadata() GoalMetadata {
	return GoalMetadata{
		Title:                "",
		Flow:                 "",
		Models:               nil,
		Alias:                nil,
		CompletionGateScript: "",
		ContinuousModePrompt: "",
		ContinuousModeAuto:   "",
		ContinuousModeCron:   "",
		Retrospective:        "",
	}
}

func TestDefaultActionConfigs(t *testing.T) {
	configs := defaultActionConfigs()
	assert.Len(t, configs, 3)
	assert.Equal(t, "Create PR", configs[0].Name)
	assert.Equal(t, "Upstream Sync", configs[1].Name)
	assert.Equal(t, "Start Application", configs[2].Name)
}

func TestLoadProjectConfig(t *testing.T) {
	tests := []struct {
		name        string
		setupFunc   func(*testing.T, string)
		wantErr     bool
		errContains string
		validate    func(*testing.T, *projectConfig)
	}{
		{
			name: "validConfig",
			setupFunc: func(t *testing.T, dir string) {
				t.Helper()
				config := newConfigTestProjectConfig()
				config.DefaultModel = "opencode/claude-opus-4-6"
				config.Editor = "code"
				data, err := json.Marshal(config)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(dir, configFileName), data, 0o644))
			},
			wantErr:     false,
			errContains: "",
			validate: func(t *testing.T, config *projectConfig) {
				t.Helper()
				require.NotNil(t, config)
				assert.Equal(t, "opencode/claude-opus-4-6", config.DefaultModel)
				assert.Equal(t, "code", config.Editor)
			},
		},
		{
			name: "noConfigFile",
			setupFunc: func(_ *testing.T, _ string) {
			},
			wantErr:     false,
			errContains: "",
			validate: func(t *testing.T, config *projectConfig) {
				t.Helper()
				assert.Nil(t, config)
			},
		},
		{
			name: "invalidJSON",
			setupFunc: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(dir, configFileName), []byte("not valid json"), 0o644))
			},
			wantErr:     true,
			errContains: "invalid JSON syntax",
			validate:    nil,
		},
		{
			name: "configWithActions",
			setupFunc: func(t *testing.T, dir string) {
				t.Helper()
				config := newConfigTestProjectConfig()
				config.DefaultModel = "opencode/claude-opus-4-6"
				action := newConfigTestActionConfig()
				action.Name = "Test Action"
				action.Model = "test-model"
				action.Prompt = "test prompt"
				config.Actions = []actionConfig{action}
				data, err := json.Marshal(config)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(dir, configFileName), data, 0o644))
			},
			wantErr:     false,
			errContains: "",
			validate: func(t *testing.T, config *projectConfig) {
				t.Helper()
				require.NotNil(t, config)
				assert.Len(t, config.Actions, 1)
				assert.Equal(t, "Test Action", config.Actions[0].Name)
			},
		},
		{
			name: "configWithMCP",
			setupFunc: func(t *testing.T, dir string) {
				t.Helper()
				config := newConfigTestProjectConfig()
				config.DefaultModel = "opencode/claude-opus-4-6"
				config.MCP = map[string]json.RawMessage{
					"test-server": json.RawMessage(`{"command": "test"}`),
				}
				data, err := json.Marshal(config)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(dir, configFileName), data, 0o644))
			},
			wantErr:     false,
			errContains: "",
			validate: func(t *testing.T, config *projectConfig) {
				t.Helper()
				require.NotNil(t, config)
				assert.NotNil(t, config.MCP)
				assert.Contains(t, config.MCP, "test-server")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setupFunc(t, dir)

			config, err := loadProjectConfig(dir)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, config)
			}
		})
	}
}

func TestValidateProjectConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      *projectConfig
		wantErr     bool
		errContains string
	}{
		{
			name:        "nilConfig",
			config:      nil,
			wantErr:     false,
			errContains: "",
		},
		{
			name: "emptyDefaultModel",
			config: func() *projectConfig {
				config := newConfigTestProjectConfig()
				return &config
			}(),
			wantErr:     false,
			errContains: "",
		},
		{
			name: "validDefaultModel",
			config: func() *projectConfig {
				config := newConfigTestProjectConfig()
				config.DefaultModel = "opencode/claude-opus-4-6"
				return &config
			}(),
			wantErr:     false,
			errContains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.config != nil && tt.config.DefaultModel != "" {
				if _, err := exec.LookPath("opencode"); err != nil {
					t.Skip("opencode not found in PATH")
				}
			}

			err := validateProjectConfig(tt.config)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestApplyConfigDefaults(t *testing.T) {
	tests := []struct {
		name     string
		config   *projectConfig
		metadata *GoalMetadata
		validate func(*testing.T, *GoalMetadata)
	}{
		{
			name:   "nilConfig",
			config: nil,
			metadata: func() *GoalMetadata {
				metadata := newConfigTestGoalMetadata()
				metadata.Models = map[string]any{"agent1": "model1"}
				return &metadata
			}(),
			validate: func(t *testing.T, m *GoalMetadata) {
				t.Helper()
				assert.Equal(t, "model1", m.Models["agent1"])
			},
		},
		{
			name: "emptyDefaultModel",
			config: func() *projectConfig {
				config := newConfigTestProjectConfig()
				return &config
			}(),
			metadata: func() *GoalMetadata {
				metadata := newConfigTestGoalMetadata()
				metadata.Models = map[string]any{"agent1": "model1"}
				return &metadata
			}(),
			validate: func(t *testing.T, m *GoalMetadata) {
				t.Helper()
				assert.Equal(t, "model1", m.Models["agent1"])
			},
		},
		{
			name: "applyDefaultToEmptyAgent",
			config: func() *projectConfig {
				config := newConfigTestProjectConfig()
				config.DefaultModel = "default-model"
				return &config
			}(),
			metadata: func() *GoalMetadata {
				metadata := newConfigTestGoalMetadata()
				metadata.Models = map[string]any{
					"agent1": "model1",
					"agent2": "",
				}
				return &metadata
			}(),
			validate: func(t *testing.T, m *GoalMetadata) {
				t.Helper()
				assert.Equal(t, "model1", m.Models["agent1"])
			},
		},
		{
			name: "nilModelsMap",
			config: func() *projectConfig {
				config := newConfigTestProjectConfig()
				config.DefaultModel = "default-model"
				return &config
			}(),
			metadata: func() *GoalMetadata {
				metadata := newConfigTestGoalMetadata()
				return &metadata
			}(),
			validate: func(t *testing.T, m *GoalMetadata) {
				t.Helper()
				assert.NotNil(t, m.Models)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applyConfigDefaults(tt.config, tt.metadata)
			if tt.validate != nil {
				tt.validate(t, tt.metadata)
			}
		})
	}
}

func TestExtractMCPSection(t *testing.T) {
	tests := []struct {
		name        string
		oc          map[string]json.RawMessage
		wantErr     bool
		errContains string
		validate    func(*testing.T, map[string]json.RawMessage)
	}{
		{
			name:        "noMCPSection",
			oc:          map[string]json.RawMessage{},
			wantErr:     false,
			errContains: "",
			validate: func(t *testing.T, mcp map[string]json.RawMessage) {
				t.Helper()
				assert.Empty(t, mcp)
			},
		},
		{
			name: "validMCPSection",
			oc: map[string]json.RawMessage{
				"mcp": json.RawMessage(`{"server1": {"command": "test"}}`),
			},
			wantErr:     false,
			errContains: "",
			validate: func(t *testing.T, mcp map[string]json.RawMessage) {
				t.Helper()
				assert.Contains(t, mcp, "server1")
			},
		},
		{
			name: "invalidMCPSection",
			oc: map[string]json.RawMessage{
				"mcp": json.RawMessage(`not valid json`),
			},
			wantErr:     true,
			errContains: "",
			validate:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mcp, err := extractMCPSection(tt.oc)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, mcp)
			}
		})
	}
}

func TestApplyCustomMCPs(t *testing.T) {
	tests := []struct {
		name      string
		config    *projectConfig
		setupFunc func(*testing.T, string)
		wantErr   bool
		validate  func(*testing.T, string)
	}{
		{
			name:   "nilConfig",
			config: nil,
			setupFunc: func(_ *testing.T, _ string) {
			},
			wantErr:  false,
			validate: nil,
		},
		{
			name: "emptyMCP",
			config: func() *projectConfig {
				config := newConfigTestProjectConfig()
				config.MCP = map[string]json.RawMessage{}
				return &config
			}(),
			setupFunc: func(_ *testing.T, _ string) {
			},
			wantErr:  false,
			validate: nil,
		},
		{
			name: "noOpencodeFile",
			config: func() *projectConfig {
				config := newConfigTestProjectConfig()
				config.MCP = map[string]json.RawMessage{
					"test-server": json.RawMessage(`{"command": "test"}`),
				}
				return &config
			}(),
			setupFunc: func(_ *testing.T, _ string) {
			},
			wantErr:  true,
			validate: nil,
		},
		{
			name: "addNewMCP",
			config: func() *projectConfig {
				config := newConfigTestProjectConfig()
				config.MCP = map[string]json.RawMessage{
					"new-server": json.RawMessage(`{"command": "new"}`),
				}
				return &config
			}(),
			setupFunc: func(t *testing.T, dir string) {
				t.Helper()
				sgaiDir := filepath.Join(dir, ".sgai")
				require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
				opencodeContent := `{"mcp": {}}`
				require.NoError(t, os.WriteFile(filepath.Join(sgaiDir, "opencode.jsonc"), []byte(opencodeContent), 0o644))
			},
			wantErr: false,
			validate: func(t *testing.T, dir string) {
				t.Helper()
				data, err := os.ReadFile(filepath.Join(dir, ".sgai", "opencode.jsonc"))
				require.NoError(t, err)
				var oc map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(data, &oc))
				assert.Contains(t, oc, "mcp")
			},
		},
		{
			name: "existingMCP",
			config: func() *projectConfig {
				config := newConfigTestProjectConfig()
				config.MCP = map[string]json.RawMessage{
					"existing-server": json.RawMessage(`{"command": "updated"}`),
				}
				return &config
			}(),
			setupFunc: func(t *testing.T, dir string) {
				t.Helper()
				sgaiDir := filepath.Join(dir, ".sgai")
				require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
				opencodeContent := `{"mcp": {"existing-server": {"command": "original"}}}`
				require.NoError(t, os.WriteFile(filepath.Join(sgaiDir, "opencode.jsonc"), []byte(opencodeContent), 0o644))
			},
			wantErr: false,
			validate: func(t *testing.T, dir string) {
				t.Helper()
				data, err := os.ReadFile(filepath.Join(dir, ".sgai", "opencode.jsonc"))
				require.NoError(t, err)
				var oc map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(data, &oc))
				mcpRaw := oc["mcp"]
				var mcp map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(mcpRaw, &mcp))
				assert.Contains(t, mcp, "existing-server")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setupFunc(t, dir)
			err := applyCustomMCPs(dir, tt.config)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, dir)
			}
		})
	}
}

func TestLoadProjectConfigTypeError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, configFileName), []byte(`{"editor": 12345}`), 0o644))
	_, err := loadProjectConfig(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid JSON type")
}

func TestLoadProjectConfigPermissionDenied(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, configFileName)
	require.NoError(t, os.WriteFile(configPath, []byte(`{}`), 0o000))
	t.Cleanup(func() { _ = os.Chmod(configPath, 0o644) })
	_, err := loadProjectConfig(dir)
	if err != nil {
		assert.Contains(t, err.Error(), "permission denied")
	}
}

func TestApplyCustomMCPsInvalidOpencodeJSON(t *testing.T) {
	dir := t.TempDir()
	sgaiDir := filepath.Join(dir, ".sgai")
	require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sgaiDir, "opencode.jsonc"), []byte("not valid json"), 0o644))
	cfg := newConfigTestProjectConfig()
	cfg.MCP = map[string]json.RawMessage{
		"test-server": json.RawMessage(`{"command": "test"}`),
	}
	err := applyCustomMCPs(dir, &cfg)
	require.Error(t, err)
}

func TestApplyCustomMCPsInvalidMCPSection(t *testing.T) {
	dir := t.TempDir()
	sgaiDir := filepath.Join(dir, ".sgai")
	require.NoError(t, os.MkdirAll(sgaiDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sgaiDir, "opencode.jsonc"), []byte(`{"mcp": "not a map"}`), 0o644))
	cfg := newConfigTestProjectConfig()
	cfg.MCP = map[string]json.RawMessage{
		"test-server": json.RawMessage(`{"command": "test"}`),
	}
	err := applyCustomMCPs(dir, &cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "extracting mcp section")
}
