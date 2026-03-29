package main

import (
	"errors"
	"fmt"
	"maps"
	"strings"
)

type actionExecutionPlan struct {
	workspacePath string
	actionName    string
	parsed        parsedAction
	rendered      string
	argv          []string
}

func (s *Server) actionRunService(workspacePath, actionName string, values map[string]string) adhocStartResult {
	plan, errPrepare := prepareActionExecution(workspacePath, "", actionName, values, nil)
	if errPrepare != nil {
		return adhocStartError(errPrepare)
	}

	if plan.parsed.kind == actionKindPrompt {
		return s.runPromptAction(plan.workspacePath, plan.rendered, plan.parsed.model)
	}
	return s.runScriptAction(plan.workspacePath, plan.actionName, plan.argv)
}

func findActionConfigByNameWithConfigPath(workspacePath, configPath, actionName string) (actionConfig, error) {
	configs, errLoad := loadActionConfigsFromConfigPath(workspacePath, configPath)
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

func prepareActionExecution(workspacePath, configPath, actionName string, values map[string]string, promptForMissing func(string) (string, error)) (actionExecutionPlan, error) {
	actionName = strings.TrimSpace(actionName)
	if actionName == "" {
		return actionExecutionPlan{}, errors.New("action name is required")
	}

	config, errFind := findActionConfigByNameWithConfigPath(workspacePath, configPath, actionName)
	if errFind != nil {
		return actionExecutionPlan{}, errFind
	}

	parsed, errValidate := validateAndParseAction(&config)
	if errValidate != nil {
		return actionExecutionPlan{}, errValidate
	}

	resolvedValues, errResolve := resolveActionValues(&parsed, values, promptForMissing)
	if errResolve != nil {
		return actionExecutionPlan{}, errResolve
	}

	rendered, errRender := renderParsedAction(&parsed, resolvedValues)
	if errRender != nil {
		return actionExecutionPlan{}, fmt.Errorf("action %q %w", actionName, errRender)
	}

	plan := actionExecutionPlan{
		workspacePath: workspacePath,
		actionName:    actionName,
		parsed:        parsed,
		rendered:      rendered,
		argv:          nil,
	}

	if parsed.kind != actionKindScript {
		return plan, nil
	}

	argv, errSplit := splitActionCommand(rendered)
	if errSplit != nil {
		return actionExecutionPlan{}, fmt.Errorf("action %q rendered an invalid command: %w", actionName, errSplit)
	}
	plan.argv = argv
	return plan, nil
}

func resolveActionValues(parsed *parsedAction, values map[string]string, promptForMissing func(string) (string, error)) (map[string]string, error) {
	resolvedValues := cloneActionValues(values)
	if promptForMissing == nil {
		return resolvedValues, nil
	}
	for _, name := range parsed.variables {
		if _, exists := resolvedValues[name]; exists {
			continue
		}
		value, errPrompt := promptForMissing(name)
		if errPrompt != nil {
			return nil, fmt.Errorf("prompting for %s: %w", name, errPrompt)
		}
		resolvedValues[name] = value
	}
	return resolvedValues, nil
}

func cloneActionValues(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(values))
	maps.Copy(cloned, values)
	return cloned
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
