package main

import (
	"errors"
	"fmt"
	"strings"
)

func (s *Server) actionRunService(workspacePath, actionName string, values map[string]string) adhocStartResult {
	actionName = strings.TrimSpace(actionName)
	if actionName == "" {
		return adhocStartError(errors.New("action name is required"))
	}

	config, errFind := findActionConfigByName(workspacePath, actionName)
	if errFind != nil {
		return adhocStartError(errFind)
	}

	parsed, errValidate := validateAndParseAction(&config)
	if errValidate != nil {
		return adhocStartError(errValidate)
	}

	rendered, errRender := renderParsedAction(&parsed, values)
	if errRender != nil {
		return adhocStartError(fmt.Errorf("action %q %w", actionName, errRender))
	}

	if parsed.kind == actionKindPrompt {
		return s.runPromptAction(workspacePath, rendered, parsed.model)
	}

	argv, errSplit := splitActionCommand(rendered)
	if errSplit != nil {
		return adhocStartError(fmt.Errorf("action %q rendered an invalid command: %w", actionName, errSplit))
	}
	return s.runScriptAction(workspacePath, actionName, argv)
}

func findActionConfigByName(workspacePath, actionName string) (actionConfig, error) {
	configs, errLoad := loadActionConfigs(workspacePath)
	if errLoad != nil {
		return actionConfig{}, errLoad
	}
	nameErrors := actionIdentityErrors(configs)
	for i, config := range configs {
		if strings.TrimSpace(config.Name) == actionName {
			if nameErrors[i] != nil {
				return actionConfig{}, nameErrors[i]
			}
			return config, nil
		}
	}
	return actionConfig{}, fmt.Errorf("action %q not found", actionName)
}

func splitActionCommand(command string) ([]string, error) {
	if strings.TrimSpace(command) == "" {
		return nil, errors.New("empty command")
	}

	runes := []rune(command)
	args := []string{}
	var current strings.Builder
	started := false
	inSingle := false
	inDouble := false

	flushCurrent := func() {
		if !started {
			return
		}
		args = append(args, current.String())
		current.Reset()
		started = false
	}

	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case inSingle:
			if r == '\'' {
				inSingle = false
				continue
			}
			current.WriteRune(r)
			started = true
		case inDouble:
			switch r {
			case '"':
				inDouble = false
			case '\\':
				if i+1 >= len(runes) {
					return nil, errors.New("unterminated escape sequence")
				}
				if runes[i+1] == '"' {
					current.WriteRune('"')
					started = true
					i++
					continue
				}
				current.WriteRune(r)
				started = true
			default:
				current.WriteRune(r)
				started = true
			}
		default:
			switch r {
			case '\\':
				if i+1 >= len(runes) {
					return nil, errors.New("unterminated escape sequence")
				}
				next := runes[i+1]
				if isSplitActionWhitespace(next) || next == '\'' || next == '"' || next == '\\' || isUnsupportedShellOperator(next) {
					current.WriteRune(next)
					started = true
					i++
					continue
				}
				current.WriteRune(r)
				started = true
			case '\'':
				inSingle = true
				started = true
			case '"':
				inDouble = true
				started = true
			case ' ', '\t', '\n', '\r':
				flushCurrent()
			default:
				if isUnsupportedShellOperator(r) {
					return nil, fmt.Errorf("unsupported shell operator %q", string(r))
				}
				current.WriteRune(r)
				started = true
			}
		}
	}

	if inSingle || inDouble {
		return nil, errors.New("unterminated quoted string")
	}

	flushCurrent()
	if len(args) == 0 {
		return nil, errors.New("empty command")
	}
	return args, nil
}

func isSplitActionWhitespace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r'
}

func isUnsupportedShellOperator(r rune) bool {
	switch r {
	case '|', '&', ';', '<', '>':
		return true
	default:
		return false
	}
}
