package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

type composerState struct {
	Title          string              `json:"title"`
	Description    string              `json:"description"`
	CompletionGate string              `json:"completionGate"`
	Agents         []composerAgentConf `json:"agents"`
	Flow           string              `json:"flow"`
	Tasks          string              `json:"tasks"`
}

type composerAgentConf struct {
	Name     string `json:"name"`
	Selected bool   `json:"selected"`
	Model    string `json:"model"`
}

type composerSession struct {
	mu          sync.Mutex
	state       composerState
	wizard      wizardState
	loadedTitle string
}

func refreshComposerSession(workspacePath string, cs *composerSession, latestTitle string) (composerState, wizardState) {
	syncComposerSessionTitle(cs, latestTitle)
	cs.state = normalizeComposerState(workspacePath, cs.state)
	cs.wizard = syncWizardState(cs.wizard, cs.state)
	return cs.state, cs.wizard
}

func (srv *Server) getComposerSession(workspacePath string) *composerSession {
	srv.composerSessionsMu.Lock()
	defer srv.composerSessionsMu.Unlock()

	if existing, ok := srv.composerSessions[workspacePath]; ok {
		return existing
	}

	state := loadComposerStateFromDisk(workspacePath)
	cs := &composerSession{
		state:       state,
		wizard:      defaultWizardState(),
		loadedTitle: state.Title,
	}
	srv.composerSessions[workspacePath] = cs
	return cs
}

func (srv *Server) loadComposerStateForInterface(dir string) composerState {
	st := loadComposerStateFromDisk(dir)
	st.Title = srv.resolveRepositoryTitleForInterface(dir, filepath.Base(dir))
	return st
}

func loadComposerStateFromDisk(dir string) composerState {
	goalPath := filepath.Join(dir, "GOAL.md")
	content, errRead := os.ReadFile(goalPath)
	if errRead != nil {
		return defaultComposerState(dir)
	}

	metadata, errParse := parseYAMLFrontmatter(content)
	if errParse != nil && strings.TrimSpace(metadata.Title) == "" {
		return defaultComposerState(dir)
	}

	bodyContent := string(extractBody(content))
	resolvedTitle := resolveComposerTitle(dir, metadata.Title)

	st := composerState{
		Title:          resolvedTitle,
		Description:    extractDescriptionFromBody(bodyContent, resolvedTitle),
		CompletionGate: metadata.CompletionGateScript,
		Flow:           metadata.Flow,
		Tasks:          extractTasksFromBody(bodyContent),
	}
	if errParse != nil {
		return st
	}

	for agentName, modelVal := range metadata.Models {
		model := ""
		if s, ok := modelVal.(string); ok {
			model = s
		}
		st.Agents = append(st.Agents, composerAgentConf{
			Name:     agentName,
			Selected: true,
			Model:    model,
		})
	}

	slices.SortFunc(st.Agents, func(a, b composerAgentConf) int {
		return strings.Compare(a.Name, b.Name)
	})

	return st
}

func defaultComposerState(dir string) composerState {
	return composerState{
		Title: resolveComposerTitle(dir, ""),
		Agents: []composerAgentConf{
			{Name: "coordinator", Selected: true, Model: defaultAgentModel},
		},
	}
}

func resolveComposerTitle(dir string, candidates ...string) string {
	for _, candidate := range candidates {
		trimmed := strings.TrimSpace(candidate)
		if trimmed != "" {
			return trimmed
		}
	}
	return filepath.Base(dir)
}

func normalizeComposerState(dir string, st composerState) composerState {
	st.Title = resolveComposerTitle(dir, st.Title)
	return st
}

func composerTitleFollowsLoadedTitle(current, loaded string) bool {
	current = strings.TrimSpace(current)
	loaded = strings.TrimSpace(loaded)
	return current == "" || current == loaded
}

func syncComposerSessionTitle(cs *composerSession, latestTitle string) {
	latestTitle = strings.TrimSpace(latestTitle)
	if latestTitle == "" {
		return
	}

	if composerTitleFollowsLoadedTitle(cs.state.Title, cs.loadedTitle) {
		cs.state.Title = latestTitle
	}
	if composerTitleFollowsLoadedTitle(cs.wizard.Title, cs.loadedTitle) {
		cs.wizard.Title = latestTitle
	}

	cs.loadedTitle = latestTitle
}

func (srv *Server) currentComposerSession(workspacePath string) (composerState, wizardState) {
	cs := srv.getComposerSession(workspacePath)
	cs.mu.Lock()
	defer cs.mu.Unlock()

	latestTitle := srv.loadComposerStateForInterface(workspacePath).Title
	return refreshComposerSession(workspacePath, cs, latestTitle)
}

func (srv *Server) updateComposerSession(workspacePath string, st composerState, wizard wizardState) (composerState, wizardState) {
	cs := srv.getComposerSession(workspacePath)
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.state = st
	cs.wizard = wizard
	latestTitle := loadComposerStateFromDisk(workspacePath).Title
	return refreshComposerSession(workspacePath, cs, latestTitle)
}

func (srv *Server) loadComposerGoalContentForBuild(workspacePath string) ([]byte, string, error) {
	goalPath := filepath.Join(workspacePath, "GOAL.md")
	goalContent, errRead := os.ReadFile(goalPath)
	if errRead != nil {
		if os.IsNotExist(errRead) {
			return nil, filepath.Base(workspacePath), nil
		}
		return nil, "", fmt.Errorf("failed to read current GOAL.md: %w", errRead)
	}

	latestTitle, needsGeneration, errTitle := repositoryTitleFromContent(goalContent, filepath.Base(workspacePath))
	if errTitle != nil {
		return goalContent, filepath.Base(workspacePath), nil
	}
	if !needsGeneration {
		return goalContent, latestTitle, nil
	}

	latestTitle, errGenerate := srv.generateRepositoryTitle(workspacePath, filepath.Base(workspacePath))
	if errGenerate != nil {
		if errors.Is(errGenerate, errGeneratedRepositoryTitleMissingHeading) {
			return goalContent, latestTitle, nil
		}
		return nil, "", fmt.Errorf("failed to backfill current GOAL.md title: %w", errGenerate)
	}

	goalContent, errRead = os.ReadFile(goalPath)
	if errRead != nil {
		return nil, "", fmt.Errorf("failed to read current GOAL.md: %w", errRead)
	}

	return goalContent, latestTitle, nil
}

func (srv *Server) composerStateForBuild(workspacePath string) (composerState, []byte, error) {
	cs := srv.getComposerSession(workspacePath)
	cs.mu.Lock()
	defer cs.mu.Unlock()

	goalContent, latestTitle, errLoad := srv.loadComposerGoalContentForBuild(workspacePath)
	if errLoad != nil {
		return composerState{}, nil, errLoad
	}

	state, _ := refreshComposerSession(workspacePath, cs, latestTitle)
	return state, goalContent, nil
}

func stripLeadingComposerTitle(body string, resolvedTitle ...string) string {
	title := strings.TrimSpace(strings.Join(resolvedTitle, ""))
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		candidate := repositoryTitleCandidateFromLine(line)
		if candidate == "" {
			return body
		}
		if title != "" && strings.Join(strings.Fields(candidate), " ") == strings.Join(strings.Fields(title), " ") {
			return strings.TrimLeft(strings.Join(lines[i+1:], "\n"), "\n")
		}
		return body
	}
	return body
}

func quoteComposerYAMLString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func extractDescriptionFromBody(body string, resolvedTitle ...string) string {
	body = stripLeadingComposerTitle(body, resolvedTitle...)
	lines := strings.Split(body, "\n")
	var descLines []string
	inTasks := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## Tasks") || strings.HasPrefix(trimmed, "## Task") {
			inTasks = true
			continue
		}
		if strings.HasPrefix(trimmed, "## ") && inTasks {
			inTasks = false
		}
		if !inTasks {
			descLines = append(descLines, line)
		}
	}

	return strings.TrimSpace(strings.Join(descLines, "\n"))
}

func extractTasksFromBody(body string) string {
	lines := strings.Split(body, "\n")
	var taskLines []string
	inTasks := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## Tasks") || strings.HasPrefix(trimmed, "## Task") {
			inTasks = true
			continue
		}
		if strings.HasPrefix(trimmed, "## ") && inTasks {
			break
		}
		if inTasks {
			taskLines = append(taskLines, line)
		}
	}

	return strings.TrimSpace(strings.Join(taskLines, "\n"))
}

func buildGOALContent(st composerState) string {
	st.Title = strings.TrimSpace(st.Title)

	var buf bytes.Buffer

	buf.WriteString("---\n")
	buf.WriteString("title: ")
	buf.WriteString(quoteComposerYAMLString(st.Title))
	buf.WriteString("\n")

	if st.Flow != "" {
		buf.WriteString("flow: |\n")
		for line := range strings.SplitSeq(st.Flow, "\n") {
			buf.WriteString("  ")
			buf.WriteString(line)
			buf.WriteString("\n")
		}
	}

	hasSelectedAgents := false
	for _, a := range st.Agents {
		if a.Selected {
			hasSelectedAgents = true
			break
		}
	}

	if hasSelectedAgents {
		buf.WriteString("models:\n")
		for _, a := range st.Agents {
			if !a.Selected {
				continue
			}
			buf.WriteString("  \"")
			buf.WriteString(a.Name)
			buf.WriteString("\": \"")
			buf.WriteString(a.Model)
			buf.WriteString("\"\n")
		}
	}

	if st.CompletionGate != "" {
		buf.WriteString("completionGateScript: ")
		buf.WriteString(st.CompletionGate)
		buf.WriteString("\n")
	}

	buf.WriteString("---\n\n")

	if st.Description != "" {
		buf.WriteString(st.Description)
		buf.WriteString("\n\n")
	}

	if st.Tasks != "" {
		buf.WriteString("## Tasks\n\n")
		buf.WriteString(st.Tasks)
		buf.WriteString("\n")
	}

	return buf.String()
}
