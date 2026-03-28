package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertActionForAPIWithIdentityError(t *testing.T) {
	tests := []struct {
		name     string
		config   actionConfig
		expected apiActionEntry
	}{
		{
			name: "promptWithVariables",
			config: actionConfig{
				Name:        "Summarize",
				Model:       "openai/gpt-5.4",
				Prompt:      "hello {{ .Name }}",
				Script:      "",
				Description: "Summarize something",
			},
			expected: apiActionEntry{
				Name:            "Summarize",
				Model:           "openai/gpt-5.4",
				Prompt:          "hello {{ .Name }}",
				Script:          "",
				Description:     "Summarize something",
				Kind:            "prompt",
				Variables:       []string{"Name"},
				ValidationError: "",
			},
		},
		{
			name: "scriptIgnoresModel",
			config: actionConfig{
				Name:        "Print",
				Model:       "ignored-model",
				Prompt:      "",
				Script:      `printf "%s" "{{ .Message }}"`,
				Description: "",
			},
			expected: apiActionEntry{
				Name:            "Print",
				Model:           "",
				Prompt:          "",
				Script:          `printf "%s" "{{ .Message }}"`,
				Description:     "",
				Kind:            "script",
				Variables:       []string{"Message"},
				ValidationError: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, convertActionForAPIWithIdentityError(&tt.config, nil))
		})
	}
}

func TestConvertActionForAPIWithIdentityErrorValidationErrors(t *testing.T) {
	tests := []struct {
		name        string
		config      actionConfig
		wantKind    string
		wantErrPart string
	}{
		{
			name: "blankName",
			config: actionConfig{
				Name:        "   ",
				Model:       "openai/gpt-5.4",
				Prompt:      "hello",
				Script:      "",
				Description: "",
			},
			wantKind:    "prompt",
			wantErrPart: "name is required",
		},
		{
			name: "promptAndScript",
			config: actionConfig{
				Name:        "Broken",
				Model:       "openai/gpt-5.4",
				Prompt:      "hello",
				Script:      "printf test",
				Description: "",
			},
			wantKind:    "",
			wantErrPart: "exactly one of prompt or script",
		},
		{
			name: "promptMissingModel",
			config: actionConfig{
				Name:        "Prompt",
				Model:       "",
				Prompt:      "hello",
				Script:      "",
				Description: "",
			},
			wantKind:    "prompt",
			wantErrPart: "model is required",
		},
		{
			name: "unsupportedTemplateStructure",
			config: actionConfig{
				Name:        "Complex",
				Model:       "openai/gpt-5.4",
				Prompt:      `{{ if .Name }}hello{{ end }}`,
				Script:      "",
				Description: "",
			},
			wantKind:    "prompt",
			wantErrPart: "unsupported template",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := convertActionForAPIWithIdentityError(&tt.config, nil)
			assert.Equal(t, tt.wantKind, entry.Kind)
			assert.Contains(t, entry.ValidationError, tt.wantErrPart)
		})
	}
}

func TestConvertActionsForAPIDuplicateNames(t *testing.T) {
	entries := convertActionsForAPI([]actionConfig{
		{
			Name:        "Repeat",
			Model:       "openai/gpt-5.4",
			Prompt:      "hello",
			Script:      "",
			Description: "",
		},
		{
			Name:        " Repeat ",
			Model:       "",
			Prompt:      "",
			Script:      `printf "%s" "hello"`,
			Description: "",
		},
	})

	require.Len(t, entries, 2)
	assert.Contains(t, entries[0].ValidationError, "must be unique")
	assert.Contains(t, entries[1].ValidationError, "must be unique")
}

func TestValidateAndParseActionRenderRequiresVariables(t *testing.T) {
	config := actionConfig{
		Name:        "Summarize",
		Model:       "openai/gpt-5.4",
		Prompt:      "hello {{ .Name }}",
		Script:      "",
		Description: "",
	}
	parsed, errValidate := validateAndParseAction(&config)
	require.NoError(t, errValidate)

	_, errRender := renderParsedAction(&parsed, nil)
	require.Error(t, errRender)
	assert.Contains(t, errRender.Error(), "Name")
}

func TestSplitActionCommand(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		want        []string
		wantErrPart string
	}{
		{
			name:        "quotedArguments",
			command:     `printf "%s" "hello world"`,
			want:        []string{"printf", "%s", "hello world"},
			wantErrPart: "",
		},
		{
			name:        "mixedQuotesAndEscapes",
			command:     `cmd 'two words' three\ words`,
			want:        []string{"cmd", "two words", "three words"},
			wantErrPart: "",
		},
		{
			name:        "doubleQuotedBackslashesArePreserved",
			command:     `printf "%s" "C:\tmp\logs"`,
			want:        []string{"printf", "%s", `C:\tmp\logs`},
			wantErrPart: "",
		},
		{
			name:        "emptyCommand",
			command:     "   ",
			want:        nil,
			wantErrPart: "empty command",
		},
		{
			name:        "pipeOperatorRejected",
			command:     `printf "%s" hello | cat`,
			want:        nil,
			wantErrPart: `unsupported shell operator "|"`,
		},
		{
			name:        "redirectionRejected",
			command:     `printf "%s" hello > out.txt`,
			want:        nil,
			wantErrPart: `unsupported shell operator ">"`,
		},
		{
			name:        "sequencingRejected",
			command:     `printf "%s" hello ; cat`,
			want:        nil,
			wantErrPart: `unsupported shell operator ";"`,
		},
		{
			name:        "unterminatedQuote",
			command:     `printf "unterminated`,
			want:        nil,
			wantErrPart: "unterminated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, errSplit := splitActionCommand(tt.command)
			if tt.wantErrPart != "" {
				require.Error(t, errSplit)
				assert.Contains(t, errSplit.Error(), tt.wantErrPart)
				return
			}

			require.NoError(t, errSplit)
			assert.Equal(t, tt.want, got)
		})
	}
}
