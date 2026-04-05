package main

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/ucirello/sgai/pkg/state"
)

var (
	errForkOfFork        = errors.New("forks cannot create new forks")
	errGoalContentEmpty  = errors.New("GOAL.md must have content describing the goal")
	errForkTitleRequired = errors.New("fork title is required")
	errDirectoryExists   = errors.New("a directory with this name already exists")
	errMessageNotFound   = errors.New("message not found")
)

func generateRandomForkName() string {
	adjectives := [20]string{
		"bold", "swift", "calm", "keen", "warm",
		"cool", "soft", "firm", "pure", "wild",
		"deep", "tall", "fair", "vast", "slim",
		"rich", "free", "rare", "true", "wise",
	}
	colors := [15]string{
		"red", "blue", "green", "gold", "gray",
		"teal", "pink", "jade", "ruby", "sage",
		"plum", "mint", "onyx", "navy", "lime",
	}
	const suffixChars = "0123456789aeiou"

	adjective := adjectives[rand.IntN(len(adjectives))]
	color := colors[rand.IntN(len(colors))]
	suffix := make([]byte, 4)
	for i := range suffix {
		suffix[i] = suffixChars[rand.IntN(len(suffixChars))]
	}
	return adjective + "-" + color + "-" + string(suffix)
}

type forkWorkspaceResult struct {
	Name      string
	Dir       string
	Parent    string
	CreatedAt string
}

func (s *Server) forkWorkspaceService(workspacePath, goalContent string) (forkWorkspaceResult, error) {
	return s.forkWorkspaceServiceWithOptions(workspacePath, goalContent, forkWorkspaceOptions{title: "", requireTitle: false})
}

type forkWorkspaceOptions struct {
	title        string
	requireTitle bool
}

func (s *Server) forkWorkspaceServiceWithOptions(workspacePath, goalContent string, options forkWorkspaceOptions) (forkWorkspaceResult, error) {
	if s.classifyWorkspaceCached(workspacePath) == workspaceFork {
		return forkWorkspaceResult{}, errForkOfFork
	}

	if options.requireTitle && sanitizedPersistedGoalTitle(options.title) == "" {
		return forkWorkspaceResult{}, errForkTitleRequired
	}

	if goalContentBodyIsEmpty(goalContent) {
		return forkWorkspaceResult{}, errGoalContentEmpty
	}

	name := filepath.Base(workspacePath)
	if rootPath := getRootWorkspacePath(workspacePath); rootPath != "" {
		name = filepath.Base(rootPath)
	}
	name += "-" + generateRandomForkName()

	parentDir := filepath.Dir(workspacePath)
	forkPath := filepath.Join(parentDir, name)
	if _, errStat := os.Stat(forkPath); errStat == nil {
		return forkWorkspaceResult{}, errDirectoryExists
	} else if !os.IsNotExist(errStat) {
		return forkWorkspaceResult{}, fmt.Errorf("failed to check workspace path: %w", errStat)
	}

	cmd := exec.Command("jj", "workspace", "add", forkPath)
	cmd.Dir = workspacePath
	output, errFork := cmd.CombinedOutput()
	if errFork != nil {
		return forkWorkspaceResult{}, fmt.Errorf("failed to fork workspace: %w: %s", errFork, output)
	}

	if errSkel := unpackSkeleton(forkPath); errSkel != nil {
		return forkWorkspaceResult{}, failForkWorkspaceSetup(workspacePath, forkPath, "failed to unpack skeleton", errSkel)
	}
	if errExclude := addGitExclude(forkPath); errExclude != nil {
		return forkWorkspaceResult{}, failForkWorkspaceSetup(workspacePath, forkPath, "failed to add git exclude", errExclude)
	}
	forkGoalContent, errForkGoalContent := s.buildForkGoalContent(workspacePath, goalContent, options)
	if errForkGoalContent != nil {
		return forkWorkspaceResult{}, failForkWorkspaceSetup(workspacePath, forkPath, "failed to prepare GOAL.md", errForkGoalContent)
	}
	if errGoal := writeGoalBytes(forkPath, forkGoalContent); errGoal != nil {
		return forkWorkspaceResult{}, failForkWorkspaceSetup(workspacePath, forkPath, "failed to create GOAL.md", errGoal)
	}

	s.invalidateWorkspaceScanCache()
	s.classifyCache.delete(workspacePath)

	forkCanonical := resolveSymlinks(forkPath)
	listState := s.currentWorkspaceListState()
	if listState.externalDirs[resolveSymlinks(workspacePath)] {
		listState.externalDirs[forkCanonical] = true
	}
	listState.pinnedDirs[forkCanonical] = true
	if errSave := s.saveWorkspaceListState(listState, true, true); errSave != nil {
		return forkWorkspaceResult{}, failForkWorkspaceSetup(workspacePath, forkPath, "failed to persist workspace lists", errSave)
	}
	s.commitWorkspaceListState(listState)

	s.notifyStateChange()

	return forkWorkspaceResult{
		Name:      name,
		Dir:       forkPath,
		Parent:    filepath.Base(workspacePath),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (s *Server) buildForkGoalContent(workspacePath, goalContent string, options forkWorkspaceOptions) ([]byte, error) {
	goalPath := filepath.Join(workspacePath, "GOAL.md")
	rootGoalContent, errRead := os.ReadFile(goalPath)
	if errRead != nil {
		return nil, fmt.Errorf("reading parent GOAL.md: %w", errRead)
	}

	body := submittedForkGoalBody(goalContent)
	sections, hasFrontmatter, errSections := parentGoalFrontmatterSections(rootGoalContent)
	if errSections != nil {
		return nil, errSections
	}
	if hasFrontmatter {
		if errValidate := validateParentGoalFrontmatter(rootGoalContent); errValidate != nil {
			return nil, errValidate
		}
	}
	if !hasFrontmatter {
		if sanitizedPersistedGoalTitle(options.title) == "" {
			return []byte(body), nil
		}
		frontmatter, errFrontmatter := frontmatterWithOverriddenTitle(nil, nil, options.title)
		if errFrontmatter != nil {
			return nil, errFrontmatter
		}
		return composeForkGoalContent(frontmatter, nil, body), nil
	}

	frontmatter := sections.frontmatter
	if sanitizedPersistedGoalTitle(options.title) != "" {
		var errFrontmatter error
		frontmatter, errFrontmatter = frontmatterWithOverriddenTitle(sections.frontmatter, sections.lineEnding, options.title)
		if errFrontmatter != nil {
			return nil, errFrontmatter
		}
	}

	return composeForkGoalContent(frontmatter, sections.lineEnding, body), nil
}

func parentGoalFrontmatterSections(content []byte) (goalFrontmatterSections, bool, error) {
	var zeroSections goalFrontmatterSections
	trimmedContent := bytes.TrimLeftFunc(content, unicode.IsSpace)
	if !bytes.HasPrefix(trimmedContent, []byte("---")) {
		return zeroSections, false, nil
	}
	if !bytes.HasPrefix(content, []byte("---")) {
		return zeroSections, false, errors.New("parent GOAL.md frontmatter must start at beginning of file")
	}
	sections, errSections := splitGoalFrontmatterSections(content)
	if errSections != nil {
		return zeroSections, false, fmt.Errorf("invalid parent GOAL.md frontmatter: %w", errSections)
	}
	return sections, true, nil
}

func validateParentGoalFrontmatter(content []byte) error {
	_, errParse := parseYAMLFrontmatter(content)
	if errParse != nil {
		return fmt.Errorf("invalid parent GOAL.md frontmatter: %w", errParse)
	}
	return nil
}

func submittedForkGoalBody(goalContent string) string {
	return goalContent
}

func frontmatterWithOverriddenTitle(frontmatter, lineEnding []byte, title string) ([]byte, error) {
	title = sanitizedPersistedGoalTitle(title)
	if title == "" {
		return frontmatter, nil
	}
	updatedFrontmatter, errUpdate := updatedGoalFrontmatter(frontmatter, title)
	if errUpdate != nil {
		return nil, errUpdate
	}
	return frontmatterWithLineEnding(updatedFrontmatter, normalizedLineEnding(lineEnding)), nil
}

func normalizedLineEnding(lineEnding []byte) []byte {
	if len(lineEnding) == 0 {
		return []byte("\n")
	}
	return lineEnding
}

func composeForkGoalContent(frontmatter, lineEnding []byte, body string) []byte {
	resolvedLineEnding := string(lineEnding)
	if resolvedLineEnding == "" {
		resolvedLineEnding = "\n"
	}

	content := "---" + resolvedLineEnding + string(frontmatter)
	if !strings.HasSuffix(content, resolvedLineEnding) {
		content += resolvedLineEnding
	}
	content += "---" + resolvedLineEnding + resolvedLineEnding + body
	return []byte(content)
}

func failForkWorkspaceSetup(workspacePath, forkPath, message string, errCause error) error {
	errSetup := fmt.Errorf("%s: %w", message, errCause)
	errRollback := rollbackForkWorkspaceCreation(workspacePath, forkPath)
	if errRollback != nil {
		return errors.Join(errSetup, fmt.Errorf("failed to rollback fork workspace creation: %w", errRollback))
	}
	return errSetup
}

func rollbackForkWorkspaceCreation(workspacePath, forkPath string) error {
	forkName := filepath.Base(forkPath)
	forgetCmd := exec.Command("jj", "workspace", "forget", forkName)
	forgetCmd.Dir = workspacePath
	output, errForget := forgetCmd.CombinedOutput()

	var errForgetWrapped error
	if errForget != nil {
		errForgetWrapped = fmt.Errorf("failed to forget fork workspace during rollback: %w: %s", errForget, output)
	}

	errRemove := os.RemoveAll(forkPath)
	if errRemove != nil {
		errRemove = fmt.Errorf("failed to remove fork workspace during rollback: %w", errRemove)
	}

	if errForgetWrapped != nil || errRemove != nil {
		return errors.Join(errForgetWrapped, errRemove)
	}
	return nil
}

func goalContentBodyIsEmpty(goalContent string) bool {
	body := submittedForkGoalBody(goalContent)
	return strings.TrimSpace(body) == ""
}

func writeGoalContent(dir, content string) error {
	return writeGoalBytes(dir, []byte(content))
}

func writeGoalBytes(dir string, content []byte) error {
	goalPath := filepath.Join(dir, "GOAL.md")
	if errWrite := os.WriteFile(goalPath, content, 0o644); errWrite != nil {
		return fmt.Errorf("writing GOAL.md: %w", errWrite)
	}
	return nil
}

type deleteForkResult struct {
	Deleted bool
	Message string
}

func (s *Server) deleteForkByPathService(forkDir string) (deleteForkResult, error) {
	return s.deleteForkWorkspaceService(forkDir)
}

func (s *Server) deleteForkService(workspacePath, forkDir string, confirm bool) (deleteForkResult, error) {
	if s.classifyWorkspaceCached(workspacePath) != workspaceRoot {
		return deleteForkResult{}, errors.New("workspace is not a root")
	}

	if !confirm {
		return deleteForkResult{}, errors.New("confirmation required to delete fork")
	}

	validatedForkDir, errValidate := s.validateDirectory(forkDir)
	if errValidate != nil {
		return deleteForkResult{}, errors.New("invalid fork directory")
	}

	if s.classifyWorkspaceCached(validatedForkDir) != workspaceFork {
		return deleteForkResult{}, errors.New("fork workspace not found")
	}

	if resolveSymlinks(getRootWorkspacePath(validatedForkDir)) != resolveSymlinks(workspacePath) {
		return deleteForkResult{}, errors.New("fork does not belong to root")
	}

	return s.deleteForkWorkspaceService(validatedForkDir)
}

type getGoalResult struct {
	Content string
}

func (s *Server) getGoalService(workspacePath string) (getGoalResult, error) {
	data, errRead := os.ReadFile(filepath.Join(workspacePath, "GOAL.md"))
	if errRead != nil {
		return getGoalResult{}, fmt.Errorf("failed to read GOAL.md: %w", errRead)
	}
	return getGoalResult{Content: string(data)}, nil
}

type updateGoalResult struct {
	Updated   bool
	Workspace string
}

func (s *Server) updateGoalService(workspacePath, content string) (updateGoalResult, error) {
	if content == "" {
		return updateGoalResult{}, errors.New("content cannot be empty")
	}

	goalPath := filepath.Join(workspacePath, "GOAL.md")
	if errWrite := os.WriteFile(goalPath, []byte(content), 0o644); errWrite != nil {
		return updateGoalResult{}, fmt.Errorf("failed to write GOAL.md: %w", errWrite)
	}

	prefix := workspacePath + "|"
	s.svgCache.deleteFunc(func(k string) bool {
		return strings.HasPrefix(k, prefix)
	})
	s.notifyWorkspaceListChange(workspacePath)

	return updateGoalResult{Updated: true, Workspace: filepath.Base(workspacePath)}, nil
}

type togglePinResult struct {
	Pinned  bool
	Message string
}

func (s *Server) togglePinService(workspacePath string) (togglePinResult, error) {
	if errToggle := s.togglePin(workspacePath); errToggle != nil {
		return togglePinResult{}, fmt.Errorf("failed to toggle pin: %w", errToggle)
	}

	s.notifyWorkspaceListChange(workspacePath)

	return togglePinResult{Pinned: s.isPinned(workspacePath), Message: "pin toggled"}, nil
}

type deleteWorkspaceResult struct {
	Deleted  bool
	Detached bool
	Message  string
}

func (s *Server) deleteWorkspaceService(workspacePath string) (deleteWorkspaceResult, error) {
	result, errDelete := s.executeWorkspaceAction(workspacePath, workspaceOperationDelete)
	if errDelete != nil {
		return deleteWorkspaceResult{}, errDelete
	}
	return deleteWorkspaceResult{Deleted: result.Deleted, Detached: result.Detached, Message: result.Message}, nil
}

type deleteMessageResult struct {
	Deleted bool
	ID      int
	Message string
}

func (s *Server) deleteMessageService(workspacePath string, messageID int) (deleteMessageResult, error) {
	coord := s.workspaceCoordinator(workspacePath)
	wfState := coord.State()

	if !slices.ContainsFunc(wfState.Messages, func(msg state.Message) bool {
		return msg.ID == messageID
	}) {
		return deleteMessageResult{}, errMessageNotFound
	}

	if errUpdate := coord.UpdateState(func(wf *state.Workflow) {
		i := slices.IndexFunc(wf.Messages, func(msg state.Message) bool {
			return msg.ID == messageID
		})
		if i >= 0 {
			wf.Messages = slices.Delete(wf.Messages, i, i+1)
		}
	}); errUpdate != nil {
		return deleteMessageResult{}, fmt.Errorf("failed to save workspace state: %w", errUpdate)
	}

	s.notifyWorkspaceChangeAfterCoordinatorUpdate(workspacePath, coord)

	return deleteMessageResult{Deleted: true, ID: messageID, Message: "message deleted successfully"}, nil
}
