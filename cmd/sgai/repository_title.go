package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sigs.k8s.io/yaml"
)

var errGeneratedRepositoryTitleMissingHeading = errors.New("failed to derive generated GOAL.md title from markdown heading")

type repositoryTitleResult struct {
	title           string
	needsGeneration bool
}

func resolveRepositoryTitle(directory, dirName string) repositoryTitleResult {
	if directory == "" {
		return repositoryTitleResult{title: dirName}
	}

	goalPath := filepath.Join(directory, "GOAL.md")
	data, errRead := os.ReadFile(goalPath)
	if errRead != nil {
		return repositoryTitleResult{title: dirName}
	}

	title, needsGeneration, errTitle := repositoryTitleFromContent(data, dirName)
	if errTitle != nil {
		return repositoryTitleResult{title: dirName}
	}

	return repositoryTitleResult{title: title, needsGeneration: needsGeneration}
}

func repositoryTitleFromContent(content []byte, dirName string) (string, bool, error) {
	metadata, errParse := parseYAMLFrontmatter(content)
	title := strings.TrimSpace(metadata.Title)
	if title != "" {
		return title, false, nil
	}
	if errParse != nil {
		return dirName, false, fmt.Errorf("failed to parse GOAL.md frontmatter: %w", errParse)
	}

	if !goalBodyHasContent(content) {
		return dirName, false, nil
	}

	return dirName, true, nil
}

func (s *Server) resolveRepositoryTitleForInterface(directory, dirName string) string {
	result := resolveRepositoryTitle(directory, dirName)
	if result.needsGeneration {
		s.queueRepositoryTitleGeneration(directory, dirName)
	}
	return result.title
}

func (s *Server) queueRepositoryTitleGeneration(directory, dirName string) {
	if s == nil || directory == "" {
		return
	}

	go func() {
		_, _ = s.repositoryTitleFlight.do(directory, func() (string, error) {
			return s.generateRepositoryTitle(directory, dirName)
		})
	}()
}

func (s *Server) generateRepositoryTitle(directory, dirName string) (string, error) {
	goalPath := filepath.Join(directory, "GOAL.md")
	content, errRead := os.ReadFile(goalPath)
	if errRead != nil {
		return "", fmt.Errorf("failed to read GOAL.md: %w", errRead)
	}

	title, needsGeneration, errTitle := repositoryTitleFromContent(content, dirName)
	if errTitle != nil {
		return "", errTitle
	}
	if !needsGeneration {
		return title, nil
	}

	title, errWrite := writeGoalTitle(goalPath, dirName)
	if errWrite != nil {
		return "", errWrite
	}

	prefix := directory + "|"
	s.svgCache.deleteFunc(func(k string) bool {
		return strings.HasPrefix(k, prefix)
	})
	s.notifyStateChange()

	return title, nil
}

func writeGoalTitle(goalPath, dirName string) (string, error) {
	content, errRead := os.ReadFile(goalPath)
	if errRead != nil {
		return "", fmt.Errorf("failed to read GOAL.md: %w", errRead)
	}

	title, needsGeneration, errTitle := repositoryTitleFromContent(content, dirName)
	if errTitle != nil {
		return "", errTitle
	}
	if !needsGeneration {
		return title, nil
	}

	normalizedTitle, errGenerate := generatedRepositoryTitleFromContent(content)
	if errGenerate != nil {
		return "", errGenerate
	}

	updatedContent, errPatch := patchGoalTitle(content, normalizedTitle)
	if errPatch != nil {
		return "", errPatch
	}
	if bytes.Equal(updatedContent, content) {
		return normalizedTitle, nil
	}

	if errWrite := os.WriteFile(goalPath, updatedContent, 0o644); errWrite != nil {
		return "", fmt.Errorf("failed to write GOAL.md: %w", errWrite)
	}

	return normalizedTitle, nil
}

func goalBodyHasContent(content []byte) bool {
	body := string(extractBody(content))
	for line := range strings.SplitSeq(body, "\n") {
		if strings.TrimSpace(line) != "" {
			return true
		}
	}
	return false
}

func generatedRepositoryTitleFromContent(content []byte) (string, error) {
	title := repositoryTitleCandidateFromContent(content)
	if title == "" {
		return "", fmt.Errorf("%w", errGeneratedRepositoryTitleMissingHeading)
	}
	return normalizeGeneratedRepositoryTitle(title)
}

func patchGoalTitle(content []byte, title string) ([]byte, error) {
	normalizedTitle, errNormalize := normalizeGeneratedRepositoryTitle(title)
	if errNormalize != nil {
		return nil, errNormalize
	}

	titleLine, errTitleLine := renderGoalTitleLine(normalizedTitle)
	if errTitleLine != nil {
		return nil, errTitleLine
	}

	lineEnding := detectLineEnding(content)
	text := string(content)
	openingDelimiter := "---"
	openingFrontmatter := openingDelimiter + lineEnding
	if !strings.HasPrefix(text, openingFrontmatter) {
		var buf strings.Builder
		buf.WriteString(openingFrontmatter)
		buf.WriteString(titleLine)
		buf.WriteString(lineEnding)
		buf.WriteString(openingDelimiter)
		buf.WriteString(lineEnding)
		buf.Write(content)
		return []byte(buf.String()), nil
	}

	yamlStart := len(openingFrontmatter)
	frontmatterStart, frontmatterEnd, bodyStart, ok, errFrontmatter := frontmatterBounds(content)
	if errFrontmatter != nil {
		return nil, errFrontmatter
	}
	if !ok || frontmatterStart != yamlStart {
		return nil, fmt.Errorf("failed to find closing GOAL.md frontmatter delimiter")
	}

	frontmatter := strings.TrimSuffix(text[yamlStart:frontmatterEnd], lineEnding)
	updatedFrontmatter := replaceGoalFrontmatterTitle(frontmatter, titleLine, lineEnding)

	return []byte(text[:yamlStart] + updatedFrontmatter + lineEnding + text[frontmatterEnd:bodyStart] + text[bodyStart:]), nil
}

func normalizeGeneratedRepositoryTitle(title string) (string, error) {
	normalizedTitle := strings.Join(strings.Fields(title), " ")
	if normalizedTitle == "" {
		return "", fmt.Errorf("title is required")
	}
	return normalizedTitle, nil
}

func renderGoalTitleLine(title string) (string, error) {
	encodedTitle, errMarshal := yaml.Marshal(struct {
		Title string `json:"title" yaml:"title"`
	}{Title: title})
	if errMarshal != nil {
		return "", fmt.Errorf("failed to encode GOAL.md title: %w", errMarshal)
	}
	return strings.TrimSuffix(string(encodedTitle), "\n"), nil
}

func replaceGoalFrontmatterTitle(frontmatter, titleLine, lineEnding string) string {
	if frontmatter == "" {
		return titleLine
	}

	lines := strings.Split(frontmatter, lineEnding)
	for i, line := range lines {
		if isGoalTitleLine(line) {
			lines[i] = titleLine
			return strings.Join(lines, lineEnding)
		}
	}

	return titleLine + lineEnding + frontmatter
}

func isGoalTitleLine(line string) bool {
	return strings.HasPrefix(strings.TrimLeft(line, " \t"), "title:")
}

func repositoryTitleCandidateFromContent(content []byte) string {
	body := string(extractBody(content))
	for line := range strings.SplitSeq(body, "\n") {
		if strings.TrimSpace(line) != "" {
			return repositoryTitleCandidateFromLine(line)
		}
	}
	return ""
}

func repositoryTitleCandidateFromLine(line string) string {
	trimmed, ok := trimAllowedRepositoryTitleHeadingIndentation(line)
	if !ok || !isSingleATXHeadingMarker(trimmed) {
		return ""
	}

	trimmed = strings.TrimSpace(trimmed[1:])
	trimmed = trimOptionalATXClosingHashes(trimmed)
	if trimmed == "" {
		return ""
	}

	return strings.Join(strings.Fields(trimmed), " ")
}

func trimAllowedRepositoryTitleHeadingIndentation(line string) (string, bool) {
	idx := 0
	for idx < len(line) && line[idx] == ' ' {
		idx++
	}
	if idx > 3 {
		return "", false
	}
	if idx < len(line) && line[idx] == '\t' {
		return "", false
	}
	return line[idx:], true
}

func isSingleATXHeadingMarker(line string) bool {
	if line == "" || line[0] != '#' {
		return false
	}
	if len(line) == 1 {
		return true
	}
	if line[1] == '#' {
		return false
	}
	return line[1] == ' ' || line[1] == '\t'
}

func trimOptionalATXClosingHashes(line string) string {
	trimmedRight := strings.TrimRight(line, " \t")
	idx := len(trimmedRight)
	for idx > 0 && trimmedRight[idx-1] == '#' {
		idx--
	}
	if idx == len(trimmedRight) {
		return trimmedRight
	}
	if idx == 0 {
		return ""
	}
	if trimmedRight[idx-1] != ' ' && trimmedRight[idx-1] != '\t' {
		return trimmedRight
	}
	return strings.TrimRight(trimmedRight[:idx-1], " \t")
}

func detectLineEnding(content []byte) string {
	if bytes.Contains(content, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}
