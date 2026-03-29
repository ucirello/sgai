package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"text/template"
	"text/template/parse"
)

type actionAPIState struct {
	Actions     []apiActionEntry
	ConfigError string
}

type actionKind string

const (
	actionKindPrompt actionKind = "prompt"
	actionKindScript actionKind = "script"
)

type parsedAction struct {
	kind      actionKind
	model     string
	source    string
	variables []string
	tmpl      *template.Template
}

func loadActionConfigs(workspacePath string) ([]actionConfig, error) {
	return loadActionConfigsFromConfigPath(workspacePath, "")
}

func loadActionConfigsFromConfigPath(workspacePath, configPath string) ([]actionConfig, error) {
	var (
		config  *projectConfig
		errLoad error
	)
	if strings.TrimSpace(configPath) == "" {
		config, errLoad = loadProjectConfig(workspacePath)
	} else {
		config, errLoad = loadProjectConfigPath(configPath)
	}
	if errLoad != nil {
		return nil, errLoad
	}
	if config == nil || len(config.Actions) == 0 {
		return defaultActionConfigs(), nil
	}
	return config.Actions, nil
}

func convertActionForAPIWithIdentityError(config *actionConfig, errIdentity error) apiActionEntry {
	kind := actionKindFromConfig(config)
	var entry apiActionEntry
	entry.Name = config.Name
	entry.Model = actionModelForKind(kind, config.Model)
	entry.Prompt = config.Prompt
	entry.Script = config.Script
	entry.Description = config.Description
	entry.Kind = string(kind)
	if errIdentity != nil {
		entry.ValidationError = errIdentity.Error()
		return entry
	}

	parsed, errValidate := validateAndParseAction(config)
	if errValidate != nil {
		entry.ValidationError = errValidate.Error()
		return entry
	}

	entry.Kind = string(parsed.kind)
	entry.Model = parsed.model
	entry.Variables = parsed.variables
	return entry
}

func validateAndParseAction(config *actionConfig) (parsedAction, error) {
	if strings.TrimSpace(config.Name) == "" {
		return parsedAction{}, errors.New("action name is required")
	}

	kind := actionKindFromConfig(config)
	if kind == "" {
		return parsedAction{}, fmt.Errorf("action %q must set exactly one of prompt or script", actionNameForError(config))
	}

	var parsed parsedAction
	parsed.kind = kind
	if kind == actionKindPrompt {
		parsed.model = strings.TrimSpace(config.Model)
		if parsed.model == "" {
			return parsed, fmt.Errorf("action %q model is required for prompt actions", actionNameForError(config))
		}
		parsed.source = config.Prompt
	} else {
		parsed.source = config.Script
	}

	tmpl, variables, errParse := parseActionTemplate(config, parsed.source)
	if errParse != nil {
		return parsed, errParse
	}

	parsed.variables = variables
	parsed.tmpl = tmpl
	if errValidate := validateParsedActionCommand(config, &parsed); errValidate != nil {
		return parsed, errValidate
	}
	return parsed, nil
}

func actionIdentityErrors(configs []actionConfig) []error {
	errs := make([]error, len(configs))
	counts := map[string]int{}
	for _, config := range configs {
		name := strings.TrimSpace(config.Name)
		if name == "" {
			continue
		}
		counts[name]++
	}
	for i, config := range configs {
		name := strings.TrimSpace(config.Name)
		switch {
		case name == "":
			errs[i] = errors.New("action name is required")
		case counts[name] > 1:
			errs[i] = fmt.Errorf("action %q name must be unique", name)
		}
	}
	return errs
}

func renderParsedAction(parsed *parsedAction, values map[string]string) (string, error) {
	if values == nil {
		values = map[string]string{}
	}

	var buf bytes.Buffer
	if errExecute := parsed.tmpl.Execute(&buf, values); errExecute != nil {
		return "", fmt.Errorf("rendering action template: %w", errExecute)
	}
	return buf.String(), nil
}

func parseActionTemplate(config *actionConfig, source string) (*template.Template, []string, error) {
	tmpl, errParse := template.New(actionNameForError(config)).Option("missingkey=error").Parse(source)
	if errParse != nil {
		return nil, nil, fmt.Errorf("action %q has invalid template syntax: %w", actionNameForError(config), errParse)
	}

	variables, errCollect := collectActionVariables(tmpl.Root)
	if errCollect != nil {
		return nil, nil, fmt.Errorf("action %q has unsupported template usage: %w", actionNameForError(config), errCollect)
	}

	return tmpl, variables, nil
}

func validateParsedActionCommand(config *actionConfig, parsed *parsedAction) error {
	if parsed.kind != actionKindScript {
		return nil
	}

	rendered, errRender := renderParsedAction(parsed, actionValidationValues(parsed.variables))
	if errRender != nil {
		return fmt.Errorf("action %q rendering action template: %w", actionNameForError(config), errRender)
	}
	if _, errSplit := splitActionCommand(rendered); errSplit != nil {
		return fmt.Errorf("action %q rendered an invalid command: %w", actionNameForError(config), errSplit)
	}
	return nil
}

func actionValidationValues(variables []string) map[string]string {
	if len(variables) == 0 {
		return nil
	}

	values := make(map[string]string, len(variables))
	for _, name := range variables {
		values[name] = "sgai_validation_" + name
	}
	return values
}

func collectActionVariables(root parse.Node) ([]string, error) {
	var variables []string
	seen := map[string]struct{}{}
	if errCollect := collectActionVariablesInto(root, seen, &variables); errCollect != nil {
		return nil, errCollect
	}
	return variables, nil
}

func collectActionVariablesInto(node parse.Node, seen map[string]struct{}, variables *[]string) error {
	switch n := node.(type) {
	case nil:
		return nil
	case *parse.ListNode:
		for _, child := range n.Nodes {
			if errCollect := collectActionVariablesInto(child, seen, variables); errCollect != nil {
				return errCollect
			}
		}
		return nil
	case *parse.TextNode:
		return nil
	case *parse.ActionNode:
		name, errExtract := extractActionVariableName(n)
		if errExtract != nil {
			return errExtract
		}
		if _, exists := seen[name]; !exists {
			seen[name] = struct{}{}
			*variables = append(*variables, name)
		}
		return nil
	default:
		return fmt.Errorf("unsupported template node %q", node.String())
	}
}

func extractActionVariableName(node *parse.ActionNode) (string, error) {
	if node == nil || node.Pipe == nil || len(node.Pipe.Decl) != 0 || len(node.Pipe.Cmds) != 1 {
		return "", fmt.Errorf("unsupported template expression %q", node.String())
	}

	cmd := node.Pipe.Cmds[0]
	if len(cmd.Args) != 1 {
		return "", fmt.Errorf("unsupported template expression %q", node.String())
	}

	field, ok := cmd.Args[0].(*parse.FieldNode)
	if !ok || len(field.Ident) != 1 {
		return "", fmt.Errorf("unsupported template expression %q", node.String())
	}

	return field.Ident[0], nil
}

func actionKindFromConfig(config *actionConfig) actionKind {
	prompt := strings.TrimSpace(config.Prompt)
	script := strings.TrimSpace(config.Script)
	switch {
	case prompt != "" && script == "":
		return actionKindPrompt
	case prompt == "" && script != "":
		return actionKindScript
	default:
		return ""
	}
}

func actionModelForKind(kind actionKind, model string) string {
	if kind != actionKindPrompt {
		return ""
	}
	return strings.TrimSpace(model)
}

func actionNameForError(config *actionConfig) string {
	name := strings.TrimSpace(config.Name)
	if name == "" {
		return "unnamed action"
	}
	return name
}
