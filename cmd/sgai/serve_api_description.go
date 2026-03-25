package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

type goalTitleState struct {
	Title         string
	ComputedTitle string
	NeedsRepair   bool
}

func (s goalTitleState) label() string {
	if s.Title != "" {
		return s.Title
	}
	return s.ComputedTitle
}

func goalTitleStateFromPath(dir, dirName string) goalTitleState {
	if dir == "" {
		return goalTitleState{Title: "", ComputedTitle: dirName, NeedsRepair: false}
	}
	data, errRead := os.ReadFile(filepath.Join(dir, "GOAL.md"))
	if errRead != nil {
		return goalTitleState{Title: "", ComputedTitle: dirName, NeedsRepair: false}
	}
	return goalTitleStateFromContent(data, dirName)
}

func goalTitleStateFromContent(content []byte, dirName string) goalTitleState {
	fallback := dirName
	if len(content) == 0 {
		return goalTitleState{Title: "", ComputedTitle: fallback, NeedsRepair: false}
	}
	if _, ok := splitFrontmatter(content); !ok {
		return goalTitleState{Title: "", ComputedTitle: fallback, NeedsRepair: false}
	}
	metadata, errParse := parseYAMLFrontmatter(content)
	if errParse != nil {
		return goalTitleState{Title: "", ComputedTitle: fallback, NeedsRepair: false}
	}
	title := strings.TrimSpace(metadata.Title)
	if title != "" {
		return goalTitleState{Title: title, ComputedTitle: "", NeedsRepair: false}
	}
	return goalTitleState{Title: "", ComputedTitle: fallback, NeedsRepair: true}
}

func composeGoalTitleFromContent(content []byte, fallback string) string {
	return composeGoalTitleFromText(string(extractBody(content)), fallback)
}

func composeGoalTitleFromText(text, fallback string) string {
	for line := range strings.SplitSeq(text, "\n") {
		candidate := normalizedGoalTitle(line)
		if candidate != "" {
			return candidate
		}
	}
	return fallback
}

func normalizedGoalTitle(input string) string {
	candidate := strings.Join(strings.Fields(strings.TrimSpace(input)), " ")
	return strings.TrimFunc(candidate, func(r rune) bool {
		return unicode.IsPunct(r) || unicode.IsSpace(r) || unicode.IsSymbol(r)
	})
}

func sanitizedPersistedGoalTitle(title string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(title)), " ")
}

type goalFrontmatterSections struct {
	frontmatter []byte
	after       []byte
	lineEnding  []byte
}

func splitGoalFrontmatterSections(content []byte) (goalFrontmatterSections, error) {
	sections, errSplit := splitFrontmatterSections(content)
	if errSplit != nil {
		if !bytes.HasPrefix(content, []byte("---")) {
			return goalFrontmatterSections{}, errors.New("GOAL.md has no frontmatter")
		}
		return goalFrontmatterSections{}, fmt.Errorf("GOAL.md %w", errSplit)
	}
	return goalFrontmatterSections{
		frontmatter: sections.yamlContent,
		after:       sections.after,
		lineEnding:  sections.lineEnding,
	}, nil
}

func updatedGoalFrontmatter(content []byte, title string) ([]byte, error) {
	var doc yaml.Node
	if len(bytes.TrimSpace(content)) == 0 {
		doc.Kind = yaml.DocumentNode
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
	} else if errUnmarshal := yaml.Unmarshal(content, &doc); errUnmarshal != nil {
		return nil, fmt.Errorf("parse GOAL.md frontmatter: %w", errUnmarshal)
	}
	if len(doc.Content) == 0 {
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
	}
	mapping := doc.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return nil, errors.New("GOAL.md frontmatter must be a mapping")
	}
	if !setGoalFrontmatterTitle(mapping, title) {
		prependGoalFrontmatterTitle(mapping, title)
	}
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	if errEncode := encoder.Encode(&doc); errEncode != nil {
		return nil, fmt.Errorf("marshal GOAL.md frontmatter: %w", errEncode)
	}
	if errClose := encoder.Close(); errClose != nil {
		return nil, fmt.Errorf("close GOAL.md frontmatter encoder: %w", errClose)
	}
	return buf.Bytes(), nil
}

func setGoalFrontmatterTitle(mapping *yaml.Node, title string) bool {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value != "title" {
			continue
		}
		value := mapping.Content[i+1]
		value.Kind = yaml.ScalarNode
		value.Tag = "!!str"
		value.Style = 0
		value.Value = title
		value.Content = nil
		value.Alias = nil
		value.Anchor = ""
		return true
	}
	return false
}

func prependGoalFrontmatterTitle(mapping *yaml.Node, title string) {
	mapping.Content = append([]*yaml.Node{
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: "title"},
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: title},
	}, mapping.Content...)
}

func frontmatterWithLineEnding(content, lineEnding []byte) []byte {
	if !bytes.Equal(lineEnding, []byte("\r\n")) {
		return content
	}
	return bytes.ReplaceAll(content, []byte("\n"), lineEnding)
}

func contentWithInsertedGoalTitle(content []byte, title string) ([]byte, error) {
	sections, errSplit := splitGoalFrontmatterSections(content)
	if errSplit != nil {
		return nil, errSplit
	}
	frontmatter, errUpdate := updatedGoalFrontmatter(sections.frontmatter, title)
	if errUpdate != nil {
		return nil, errUpdate
	}
	frontmatter = frontmatterWithLineEnding(frontmatter, sections.lineEnding)
	delimiter := []byte("---")
	var buf bytes.Buffer
	buf.Write(delimiter)
	buf.Write(sections.lineEnding)
	buf.Write(frontmatter)
	if len(frontmatter) == 0 || !bytes.HasSuffix(frontmatter, sections.lineEnding) {
		buf.Write(sections.lineEnding)
	}
	buf.Write(delimiter)
	buf.Write(sections.after)
	return buf.Bytes(), nil
}

func defaultGoalTitleComposer(workspacePath string, goalContent []byte) (string, error) {
	return composeGoalTitleFromContent(goalContent, filepath.Base(workspacePath)), nil
}

func canonicalGoalTitleRepairPath(workspacePath string) string {
	if workspacePath == "" {
		return ""
	}
	return resolveSymlinks(workspacePath)
}

func overwriteExistingFile(path string, data []byte) error {
	f, errOpen := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if errOpen != nil {
		return fmt.Errorf("opening %s: %w", path, errOpen)
	}
	if _, errWrite := f.Write(data); errWrite != nil {
		_ = f.Close()
		return fmt.Errorf("writing %s: %w", path, errWrite)
	}
	if errClose := f.Close(); errClose != nil {
		return fmt.Errorf("closing %s: %w", path, errClose)
	}
	return nil
}

func (s *Server) enqueueGoalTitleRepair(workspacePath string) {
	workspacePath = canonicalGoalTitleRepairPath(workspacePath)
	if workspacePath == "" {
		return
	}
	s.goalTitleRepairMu.Lock()
	if _, ok := s.goalTitleRepairQueued[workspacePath]; ok {
		s.goalTitleRepairMu.Unlock()
		return
	}
	s.goalTitleRepairQueued[workspacePath] = struct{}{}
	s.goalTitleRepairQueue = append(s.goalTitleRepairQueue, workspacePath)
	if s.goalTitleRepairRunning {
		s.goalTitleRepairMu.Unlock()
		return
	}
	s.goalTitleRepairRunning = true
	s.goalTitleRepairMu.Unlock()
	go s.runGoalTitleRepairLoop()
}

func (s *Server) runGoalTitleRepairLoop() {
	for {
		workspacePath, ok := s.nextGoalTitleRepair()
		if !ok {
			return
		}
		if errRepair := s.repairGoalTitle(workspacePath); errRepair != nil {
			log.Println("failed to repair GOAL.md title:", errRepair)
		}
		s.finishGoalTitleRepair(workspacePath)
	}
}

func (s *Server) nextGoalTitleRepair() (string, bool) {
	s.goalTitleRepairMu.Lock()
	defer s.goalTitleRepairMu.Unlock()
	if len(s.goalTitleRepairQueue) == 0 {
		s.goalTitleRepairRunning = false
		return "", false
	}
	workspacePath := s.goalTitleRepairQueue[0]
	s.goalTitleRepairQueue = s.goalTitleRepairQueue[1:]
	return workspacePath, true
}

func (s *Server) finishGoalTitleRepair(workspacePath string) {
	workspacePath = canonicalGoalTitleRepairPath(workspacePath)
	s.goalTitleRepairMu.Lock()
	delete(s.goalTitleRepairQueued, workspacePath)
	s.goalTitleRepairMu.Unlock()
}

func (s *Server) repairGoalTitle(workspacePath string) error {
	workspacePath = canonicalGoalTitleRepairPath(workspacePath)
	goalPath := filepath.Join(workspacePath, "GOAL.md")
	goalContent, errRead := s.goalTitleReadFile(goalPath)
	if errRead != nil {
		if errors.Is(errRead, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read GOAL.md: %w", errRead)
	}
	titleState := goalTitleStateFromContent(goalContent, filepath.Base(workspacePath))
	if !titleState.NeedsRepair {
		return nil
	}
	latestGoalContent, errReadLatest := s.goalTitleReadFile(goalPath)
	if errReadLatest != nil {
		if errors.Is(errReadLatest, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("re-read GOAL.md: %w", errReadLatest)
	}
	latestTitleState := goalTitleStateFromContent(latestGoalContent, filepath.Base(workspacePath))
	if !latestTitleState.NeedsRepair {
		return nil
	}
	title, errCompose := s.goalTitleComposer(workspacePath, latestGoalContent)
	if errCompose != nil {
		return fmt.Errorf("compose title: %w", errCompose)
	}
	title = sanitizedPersistedGoalTitle(title)
	if title == "" {
		return errors.New("compose title: empty title")
	}
	updatedContent, errUpdate := contentWithInsertedGoalTitle(latestGoalContent, title)
	if errUpdate != nil {
		return fmt.Errorf("insert title: %w", errUpdate)
	}
	if errWrite := overwriteExistingFile(goalPath, updatedContent); errWrite != nil {
		if errors.Is(errWrite, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("write GOAL.md: %w", errWrite)
	}
	s.notifyWorkspaceListChange(workspacePath)
	return nil
}
