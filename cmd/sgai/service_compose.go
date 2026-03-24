package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var errComposerGoalModified = errors.New("GOAL.md has been modified by another session")

type composeStateResult struct {
	Workspace      string
	State          composerState
	Wizard         apiWizardState
	TechStackItems []apiTechStackItem
	FlowError      string
}

func (s *Server) composeStateService(workspacePath string) composeStateResult {
	currentState, wizard := s.currentComposerSession(workspacePath)

	var flowErr string
	if currentState.Flow != "" {
		if _, errParse := parseFlow(currentState.Flow, workspacePath); errParse != nil {
			flowErr = errParse.Error()
		}
	}

	return composeStateResult{
		Workspace:      filepath.Base(workspacePath),
		State:          currentState,
		Wizard:         apiWizardState(wizard),
		TechStackItems: buildAPITechStackItems(wizard.TechStack),
		FlowError:      flowErr,
	}
}

type composeSaveResult struct {
	Saved     bool
	Workspace string
}

func (s *Server) composeSaveService(workspacePath, ifMatch string) (composeSaveResult, error) {
	currentState, goalContent, errBuild := s.composerStateForBuild(workspacePath)
	if errBuild != nil {
		return composeSaveResult{}, errBuild
	}

	if ifMatch != "" {
		currentEtag := computeEtag(goalContent)
		if ifMatch != currentEtag {
			return composeSaveResult{}, errComposerGoalModified
		}
	}

	goalPath := filepath.Join(workspacePath, "GOAL.md")
	goalContent = []byte(buildGOALContent(currentState))

	if errWrite := os.WriteFile(goalPath, goalContent, 0644); errWrite != nil {
		return composeSaveResult{}, fmt.Errorf("failed to save GOAL.md: %w", errWrite)
	}

	s.notifyStateChange()

	return composeSaveResult{Saved: true, Workspace: filepath.Base(workspacePath)}, nil
}

type composeTemplatesResult struct {
	Templates []apiComposeTemplateEntry
}

func (s *Server) composeTemplatesService() composeTemplatesResult {
	entries := make([]apiComposeTemplateEntry, len(workflowTemplates))
	for i, tmpl := range workflowTemplates {
		entries[i] = apiComposeTemplateEntry(tmpl)
	}
	return composeTemplatesResult{Templates: entries}
}

type composePreviewResult struct {
	Content   string
	FlowError string
	Etag      string
}

func (s *Server) composePreviewService(workspacePath string) (composePreviewResult, error) {
	currentState, goalContent, errBuild := s.composerStateForBuild(workspacePath)
	if errBuild != nil {
		return composePreviewResult{}, errBuild
	}

	preview := buildGOALContent(currentState)

	var flowErr string
	if currentState.Flow != "" {
		if _, errParse := parseFlow(currentState.Flow, workspacePath); errParse != nil {
			flowErr = errParse.Error()
		}
	}

	etag := computeEtag(goalContent)

	return composePreviewResult{Content: preview, FlowError: flowErr, Etag: etag}, nil
}

type composeDraftResult struct {
	Saved bool
}

func (s *Server) composeDraftService(workspacePath string, state composerState, wizard wizardState) composeDraftResult {
	s.updateComposerSession(workspacePath, state, wizard)

	s.notifyStateChange()

	return composeDraftResult{Saved: true}
}
