package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

type workspaceOperation string

const (
	workspaceOperationDelete workspaceOperation = "delete"
	workspaceOperationDetach workspaceOperation = "detach"
	disabledReasonRunning    string             = "running"
	disabledReasonForks      string             = "forks-attached"
	disabledReasonTopology   string             = "topology-unavailable"
)

type workspaceActionEntryPoint string

const (
	workspaceActionEntryPointChoose  workspaceActionEntryPoint = "choose"
	workspaceActionEntryPointConfirm workspaceActionEntryPoint = "confirm"
	workspaceActionEntryPointHidden  workspaceActionEntryPoint = "hidden"
)

type workspaceActionIcon string

const (
	workspaceActionIconChoose workspaceActionIcon = "choose"
	workspaceActionIconDetach workspaceActionIcon = "detach"
	workspaceActionIconDelete workspaceActionIcon = "delete"
)

type workspaceActionTone string

const (
	workspaceActionToneNeutral     workspaceActionTone = "neutral"
	workspaceActionToneDestructive workspaceActionTone = "destructive"
)

var (
	errWorkspaceActionInvalidOperation  = errors.New("invalid workspace operation")
	errWorkspaceActionOperationRequired = errors.New("workspace operation is required")
	errWorkspaceActionRunning           = errors.New("workspace is running")
	errWorkspaceActionForksAttached     = errors.New("root workspace still has attached forks")
	errWorkspaceActionNotAllowed        = errors.New("workspace operation is not allowed")
)

type workspaceActionPolicy struct {
	repositoryMode   workspaceKind
	entryPoint       workspaceActionEntryPoint
	defaultOperation workspaceOperation
	allowedOps       []workspaceOperation
	disabledReason   string
	attachedForks    int
	running          bool
}

type apiRepositoryAction struct {
	RepositoryMode string                          `json:"repositoryMode"`
	EntryPoint     string                          `json:"entryPoint"`
	AllowedOps     []string                        `json:"allowedOperations"`
	DefaultOp      string                          `json:"defaultOperation,omitempty"`
	DisabledReason string                          `json:"disabledReason,omitempty"`
	AttachedForks  int                             `json:"attachedForkCount"`
	Running        bool                            `json:"running"`
	Presentation   apiRepositoryActionPresentation `json:"presentation"`
}

type apiRepositoryActionPresentation struct {
	DetailTriggerLabel  string                                     `json:"detailTriggerLabel"`
	TreeTriggerLabel    string                                     `json:"treeTriggerLabel"`
	ForkRowTriggerLabel string                                     `json:"forkRowTriggerLabel"`
	DialogTitle         string                                     `json:"dialogTitle"`
	DialogDescription   string                                     `json:"dialogDescription"`
	Icon                string                                     `json:"icon"`
	Tone                string                                     `json:"tone"`
	Operations          []apiRepositoryActionOperationPresentation `json:"operations"`
}

type apiRepositoryActionOperationPresentation struct {
	Operation string `json:"operation"`
	Label     string `json:"label"`
	Icon      string `json:"icon"`
	Tone      string `json:"tone"`
}

type workspaceActionResult struct {
	Message  string
	Deleted  bool
	Detached bool
}

type workspaceListState struct {
	externalDirs map[string]bool
	pinnedDirs   map[string]bool
}

type pendingFileReplacement struct {
	tempPath   string
	targetPath string
	label      string
}

type committedFileReplacement struct {
	targetPath   string
	backupPath   string
	label        string
	hadOriginal  bool
	parentConfig string
}

type committedFileReplacements struct {
	replacements []committedFileReplacement
}

func (s *Server) workspaceActionPolicy(workspacePath string) workspaceActionPolicy {
	canonicalPath := resolveSymlinks(workspacePath)
	kind := s.classifyWorkspaceCached(canonicalPath)
	running, _ := s.getWorkspaceStatus(canonicalPath)

	switch kind {
	case workspaceFork:
		if running {
			return workspaceActionPolicy{
				repositoryMode: workspaceFork,
				entryPoint:     workspaceActionEntryPointHidden,
				disabledReason: disabledReasonRunning,
				running:        true,
			}
		}
		return workspaceActionPolicy{
			repositoryMode: workspaceFork,
			entryPoint:     workspaceActionEntryPointChoose,
			allowedOps:     []workspaceOperation{workspaceOperationDetach, workspaceOperationDelete},
		}
	case workspaceRoot:
		if running {
			return workspaceActionPolicy{
				repositoryMode: workspaceRoot,
				entryPoint:     workspaceActionEntryPointHidden,
				disabledReason: disabledReasonRunning,
				running:        true,
			}
		}
		forkCount, errCount := s.countWorkspaceChildren(canonicalPath)
		if errCount != nil {
			attachedForks := s.countAttachedForks(canonicalPath)
			if attachedForks > 0 {
				return workspaceActionPolicy{
					repositoryMode: workspaceRoot,
					entryPoint:     workspaceActionEntryPointHidden,
					disabledReason: disabledReasonForks,
					attachedForks:  attachedForks,
				}
			}
			return workspaceActionPolicy{
				repositoryMode: workspaceRoot,
				entryPoint:     workspaceActionEntryPointHidden,
				disabledReason: disabledReasonTopology,
			}
		}
		if forkCount > 0 {
			return workspaceActionPolicy{
				repositoryMode: workspaceRoot,
				entryPoint:     workspaceActionEntryPointHidden,
				disabledReason: disabledReasonForks,
				attachedForks:  forkCount,
			}
		}
		return workspaceActionPolicy{
			repositoryMode:   workspaceRoot,
			entryPoint:       workspaceActionEntryPointConfirm,
			defaultOperation: workspaceOperationDetach,
			allowedOps:       []workspaceOperation{workspaceOperationDetach},
		}
	default:
		if running {
			return workspaceActionPolicy{
				repositoryMode: workspaceStandalone,
				entryPoint:     workspaceActionEntryPointHidden,
				disabledReason: disabledReasonRunning,
				running:        true,
			}
		}
		return workspaceActionPolicy{
			repositoryMode:   workspaceStandalone,
			entryPoint:       workspaceActionEntryPointConfirm,
			defaultOperation: workspaceOperationDetach,
			allowedOps:       []workspaceOperation{workspaceOperationDetach},
		}
	}
}

func (p workspaceActionPolicy) allows(op workspaceOperation) bool {
	return slices.Contains(p.allowedOps, op)
}

func (p workspaceActionPolicy) api(workspaceName string) apiRepositoryAction {
	allowedOps := make([]string, 0, len(p.allowedOps))
	for _, op := range p.allowedOps {
		allowedOps = append(allowedOps, string(op))
	}
	result := apiRepositoryAction{
		RepositoryMode: string(p.repositoryMode),
		EntryPoint:     string(p.entryPoint),
		AllowedOps:     allowedOps,
		AttachedForks:  p.attachedForks,
		Running:        p.running,
		Presentation:   p.apiPresentation(workspaceName),
	}
	if p.defaultOperation != "" {
		result.DefaultOp = string(p.defaultOperation)
	}
	if p.disabledReason != "" {
		result.DisabledReason = p.disabledReason
	}
	return result
}

func (p workspaceActionPolicy) apiPresentation(workspaceName string) apiRepositoryActionPresentation {
	operations := p.apiOperationPresentations()
	if p.entryPoint == workspaceActionEntryPointHidden {
		return apiRepositoryActionPresentation{Operations: operations}
	}

	if p.entryPoint == workspaceActionEntryPointChoose {
		subject := p.repositorySubject(workspaceName)
		dialogTitle := "Choose workspace action"
		if p.repositoryMode == workspaceFork {
			dialogTitle = "Choose fork action"
		}
		return apiRepositoryActionPresentation{
			DetailTriggerLabel:  "Choose action",
			TreeTriggerLabel:    fmt.Sprintf("Choose action for %s", subject),
			ForkRowTriggerLabel: fmt.Sprintf("Choose action for %s", subject),
			DialogTitle:         dialogTitle,
			DialogDescription:   p.chooseDialogDescription(workspaceName),
			Icon:                string(workspaceActionIconChoose),
			Tone:                string(workspaceActionToneNeutral),
			Operations:          operations,
		}
	}

	confirmOperation := p.defaultOperation
	if !p.allows(confirmOperation) && len(p.allowedOps) > 0 {
		confirmOperation = p.allowedOps[0]
	}
	confirmLabel := p.operationLabel(confirmOperation)
	confirmTone := string(p.operationTone(confirmOperation))
	confirmIcon := string(p.operationIcon(confirmOperation))
	return apiRepositoryActionPresentation{
		DetailTriggerLabel:  confirmLabel,
		TreeTriggerLabel:    fmt.Sprintf("%s %s", confirmLabel, workspaceName),
		ForkRowTriggerLabel: fmt.Sprintf("%s %s", confirmLabel, workspaceName),
		DialogTitle:         p.confirmDialogTitle(confirmOperation),
		DialogDescription:   p.confirmDialogDescription(workspaceName, confirmOperation),
		Icon:                confirmIcon,
		Tone:                confirmTone,
		Operations:          operations,
	}
}

func (p workspaceActionPolicy) apiOperationPresentations() []apiRepositoryActionOperationPresentation {
	operations := make([]apiRepositoryActionOperationPresentation, 0, len(p.allowedOps))
	for _, op := range p.allowedOps {
		operations = append(operations, apiRepositoryActionOperationPresentation{
			Operation: string(op),
			Label:     p.operationLabel(op),
			Icon:      string(p.operationIcon(op)),
			Tone:      string(p.operationTone(op)),
		})
	}
	return operations
}

func (p workspaceActionPolicy) repositoryNoun() string {
	if p.repositoryMode == workspaceFork {
		return "fork"
	}
	return "workspace"
}

func (p workspaceActionPolicy) repositorySubject(workspaceName string) string {
	if p.repositoryMode == workspaceFork {
		return fmt.Sprintf("fork %s", workspaceName)
	}
	return workspaceName
}

func (p workspaceActionPolicy) operationLabel(op workspaceOperation) string {
	if op == workspaceOperationDelete {
		return "Delete"
	}
	return "Detach"
}

func (p workspaceActionPolicy) operationIcon(op workspaceOperation) workspaceActionIcon {
	if op == workspaceOperationDelete {
		return workspaceActionIconDelete
	}
	return workspaceActionIconDetach
}

func (p workspaceActionPolicy) operationTone(op workspaceOperation) workspaceActionTone {
	if op == workspaceOperationDelete {
		return workspaceActionToneDestructive
	}
	return workspaceActionToneNeutral
}

func (p workspaceActionPolicy) confirmDialogTitle(op workspaceOperation) string {
	if op == workspaceOperationDelete {
		return "Delete workspace"
	}
	return "Detach workspace"
}

func (p workspaceActionPolicy) confirmDialogDescription(workspaceName string, op workspaceOperation) string {
	if op == workspaceOperationDelete {
		return fmt.Sprintf("This will permanently delete '%s' from disk. This action cannot be undone.", workspaceName)
	}
	return fmt.Sprintf("This will remove '%s' from the SGAI workspace list. The files on disk will NOT be deleted.", workspaceName)
}

func (p workspaceActionPolicy) chooseDialogDescription(workspaceName string) string {
	repositoryNoun := p.repositoryNoun()
	parts := make([]string, 0, len(p.allowedOps))
	for _, op := range p.allowedOps {
		parts = append(parts, p.chooseOperationDescription(repositoryNoun, op))
	}
	return fmt.Sprintf("Choose what to do with %s '%s'. %s", repositoryNoun, workspaceName, strings.Join(parts, " "))
}

func (p workspaceActionPolicy) chooseOperationDescription(repositoryNoun string, op workspaceOperation) string {
	if op == workspaceOperationDelete {
		return fmt.Sprintf("Delete permanently removes the %s from disk.", repositoryNoun)
	}
	return fmt.Sprintf("Detach removes the %s from the SGAI workspace list and keeps the files on disk.", repositoryNoun)
}

func (s *Server) countWorkspaceChildren(rootPath string) (int, error) {
	count, errCount := workspaceCount(rootPath)
	if errCount != nil {
		return 0, errCount
	}
	if count <= 1 {
		return 0, nil
	}
	return count - 1, nil
}

func (s *Server) countAttachedForks(rootPath string) int {
	canonicalRoot := resolveSymlinks(rootPath)
	state := s.currentWorkspaceListState()
	count := 0
	for dir := range state.externalDirs {
		if s.classifyWorkspaceCached(dir) != workspaceFork {
			continue
		}
		if resolveSymlinks(getRootWorkspacePath(dir)) == canonicalRoot {
			count++
		}
	}
	return count
}

func (s *Server) resolveWorkspaceOperation(workspacePath, requestedOperation string) (workspaceActionPolicy, workspaceOperation, error) {
	policy := s.workspaceActionPolicy(workspacePath)
	if policy.disabledReason == disabledReasonRunning {
		return workspaceActionPolicy{}, "", errWorkspaceActionRunning
	}
	if policy.disabledReason == disabledReasonForks {
		return workspaceActionPolicy{}, "", errWorkspaceActionForksAttached
	}
	if policy.entryPoint == workspaceActionEntryPointHidden {
		return workspaceActionPolicy{}, "", errWorkspaceActionNotAllowed
	}

	requested := workspaceOperation(requestedOperation)
	if requested == "" {
		if policy.defaultOperation == "" {
			return workspaceActionPolicy{}, "", errWorkspaceActionOperationRequired
		}
		requested = policy.defaultOperation
	}

	switch requested {
	case workspaceOperationDelete, workspaceOperationDetach:
	default:
		return workspaceActionPolicy{}, "", errWorkspaceActionInvalidOperation
	}

	if !policy.allows(requested) {
		return workspaceActionPolicy{}, "", errWorkspaceActionNotAllowed
	}

	return policy, requested, nil
}

func (s *Server) executeWorkspaceAction(workspacePath string, op workspaceOperation) (workspaceActionResult, error) {
	policy, resolvedOp, errResolve := s.resolveWorkspaceOperation(workspacePath, string(op))
	if errResolve != nil {
		return workspaceActionResult{}, errResolve
	}

	switch resolvedOp {
	case workspaceOperationDetach:
		result, errDetach := s.detachWorkspaceService(workspacePath)
		if errDetach != nil {
			return workspaceActionResult{}, errDetach
		}
		return workspaceActionResult{Message: result.Message, Detached: result.Detached}, nil
	case workspaceOperationDelete:
		if policy.repositoryMode != workspaceFork {
			return workspaceActionResult{}, errWorkspaceActionNotAllowed
		}
		result, errDelete := s.deleteForkWorkspaceService(workspacePath)
		if errDelete != nil {
			return workspaceActionResult{}, errDelete
		}
		return workspaceActionResult{Message: result.Message, Deleted: result.Deleted}, nil
	default:
		return workspaceActionResult{}, errWorkspaceActionInvalidOperation
	}
}

func (s *Server) detachWorkspaceService(workspacePath string) (detachExternalResult, error) {
	canonicalPath := resolveSymlinks(workspacePath)
	policy := s.workspaceActionPolicy(canonicalPath)
	if policy.running {
		return detachExternalResult{}, errWorkspaceActionRunning
	}
	if !policy.allows(workspaceOperationDetach) {
		if policy.repositoryMode == workspaceRoot && policy.attachedForks > 0 {
			return detachExternalResult{}, errWorkspaceActionForksAttached
		}
		return detachExternalResult{}, errWorkspaceActionNotAllowed
	}

	state := s.currentWorkspaceListState()
	pinnedChanged := directorySetContains(state.pinnedDirs, workspacePath)
	if !directorySetContains(state.externalDirs, workspacePath) {
		return detachExternalResult{}, errNotAttached
	}

	deleteDirectorySetEntry(state.externalDirs, workspacePath)
	deleteDirectorySetEntry(state.pinnedDirs, workspacePath)

	if errSave := s.saveWorkspaceListState(state, true, pinnedChanged); errSave != nil {
		return detachExternalResult{}, fmt.Errorf("saving workspace lists: %w", errSave)
	}

	s.commitWorkspaceListState(state)
	s.forgetWorkspaceRuntimeState(canonicalPath)
	s.invalidateWorkspaceScanCache()
	s.classifyCache.delete(canonicalPath)
	rootPath := resolveSymlinks(getRootWorkspacePath(canonicalPath))
	if rootPath != "" {
		s.classifyCache.delete(rootPath)
	}
	s.notifyStateChange()

	return detachExternalResult{Detached: true, Message: "external workspace detached"}, nil
}

func (s *Server) deleteForkWorkspaceService(workspacePath string) (deleteForkResult, error) {
	canonicalPath := resolveSymlinks(workspacePath)
	policy := s.workspaceActionPolicy(canonicalPath)
	if policy.running {
		return deleteForkResult{}, errWorkspaceActionRunning
	}
	if policy.repositoryMode != workspaceFork {
		return deleteForkResult{}, errWorkspaceActionNotAllowed
	}

	rootPath := resolveSymlinks(getRootWorkspacePath(canonicalPath))
	if rootPath == "" {
		return deleteForkResult{}, fmt.Errorf("could not determine root workspace for fork")
	}

	state := s.currentWorkspaceListState()
	pinnedChanged := directorySetContains(state.pinnedDirs, workspacePath)
	deleteDirectorySetEntry(state.externalDirs, workspacePath)
	deleteDirectorySetEntry(state.pinnedDirs, workspacePath)
	replacements, errPrepare := s.prepareWorkspaceListState(state, true, pinnedChanged)
	if errPrepare != nil {
		return deleteForkResult{}, fmt.Errorf("saving workspace lists: %w", errPrepare)
	}
	defer cleanupFileReplacements(replacements)

	stagedPath, errStage := stageWorkspaceDirectoryForDeletion(canonicalPath)
	if errStage != nil {
		return deleteForkResult{}, fmt.Errorf("failed to stage fork directory for deletion: %w", errStage)
	}

	forkName := filepath.Base(canonicalPath)
	rollbackOpID, errRollbackOp := currentOperationID(rootPath)
	if errRollbackOp != nil {
		if errRestore := restoreStagedWorkspaceDirectory(stagedPath, canonicalPath); errRestore != nil {
			return deleteForkResult{}, errors.Join(
				fmt.Errorf("capturing rollback operation: %w", errRollbackOp),
				fmt.Errorf("failed to restore staged fork directory: %w", errRestore),
			)
		}
		return deleteForkResult{}, fmt.Errorf("capturing rollback operation: %w", errRollbackOp)
	}

	if errForget := forgetWorkspace(rootPath, forkName); errForget != nil {
		if errRestore := restoreStagedWorkspaceDirectory(stagedPath, canonicalPath); errRestore != nil {
			return deleteForkResult{}, errors.Join(
				fmt.Errorf("failed to forget fork workspace: %w", errForget),
				fmt.Errorf("failed to restore staged fork directory: %w", errRestore),
			)
		}
		return deleteForkResult{}, fmt.Errorf("failed to forget fork workspace: %w", errForget)
	}

	committedReplacements, errCommit := commitFileReplacements(replacements)
	if errCommit != nil {
		if errRollback := rollbackDeletedForkWorkspace(rootPath, rollbackOpID, stagedPath, canonicalPath); errRollback != nil {
			return deleteForkResult{}, errors.Join(
				fmt.Errorf("saving workspace lists: %w", errCommit),
				fmt.Errorf("failed to rollback fork deletion: %w", errRollback),
			)
		}
		return deleteForkResult{}, fmt.Errorf("saving workspace lists: %w", errCommit)
	}
	defer committedReplacements.cleanup()

	if errRemove := os.RemoveAll(stagedPath); errRemove != nil {
		errRollbackState := committedReplacements.rollback()
		errRollbackWorkspace := rollbackDeletedForkWorkspace(rootPath, rollbackOpID, stagedPath, canonicalPath)
		if errRollbackState != nil || errRollbackWorkspace != nil {
			rollbackErrs := []error{fmt.Errorf("failed to remove fork directory: %w", errRemove)}
			if errRollbackState != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("failed to rollback workspace lists: %w", errRollbackState))
			}
			if errRollbackWorkspace != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("failed to rollback fork workspace: %w", errRollbackWorkspace))
			}
			return deleteForkResult{}, errors.Join(rollbackErrs...)
		}
		return deleteForkResult{}, fmt.Errorf("failed to remove fork directory: %w", errRemove)
	}

	s.commitWorkspaceListState(state)
	s.forgetWorkspaceRuntimeState(canonicalPath)
	s.invalidateWorkspaceScanCache()
	s.classifyCache.delete(rootPath)
	s.classifyCache.delete(canonicalPath)
	s.notifyStateChange()

	return deleteForkResult{Deleted: true, Message: "fork deleted successfully"}, nil
}

func (s *Server) forgetWorkspaceRuntimeState(workspacePath string) {
	s.mu.Lock()
	delete(s.sessions, workspacePath)
	delete(s.everStartedDirs, workspacePath)
	s.mu.Unlock()
}

func (s *Server) currentWorkspaceListState() workspaceListState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return workspaceListState{
		externalDirs: maps.Clone(s.externalDirs),
		pinnedDirs:   maps.Clone(s.pinnedDirs),
	}
}

func (s *Server) commitWorkspaceListState(state workspaceListState) {
	s.mu.Lock()
	s.externalDirs = state.externalDirs
	s.pinnedDirs = state.pinnedDirs
	s.mu.Unlock()
}

func (s *Server) saveWorkspaceListState(state workspaceListState, includeExternal, includePinned bool) error {
	replacements, errPrepare := s.prepareWorkspaceListState(state, includeExternal, includePinned)
	if errPrepare != nil {
		return errPrepare
	}
	defer cleanupFileReplacements(replacements)
	committedReplacements, errCommit := commitFileReplacements(replacements)
	if errCommit != nil {
		return errCommit
	}
	committedReplacements.cleanup()
	return nil
}

func (s *Server) prepareWorkspaceListState(state workspaceListState, includeExternal, includePinned bool) ([]pendingFileReplacement, error) {
	var replacements []pendingFileReplacement
	if includeExternal {
		replacement, errPrepare := s.prepareExternalDirsReplacement(state.externalDirs)
		if errPrepare != nil {
			cleanupFileReplacements(replacements)
			return nil, errPrepare
		}
		replacements = append(replacements, replacement)
	}
	if includePinned {
		replacement, errPrepare := s.preparePinnedProjectsReplacement(state.pinnedDirs)
		if errPrepare != nil {
			cleanupFileReplacements(replacements)
			return nil, errPrepare
		}
		replacements = append(replacements, replacement)
	}
	return replacements, nil
}

func (s *Server) prepareExternalDirsReplacement(externalDirs map[string]bool) (pendingFileReplacement, error) {
	data, errMarshal := marshalDirectoryMap(externalDirs)
	if errMarshal != nil {
		return pendingFileReplacement{}, fmt.Errorf("encoding external dirs: %w", errMarshal)
	}
	replacement, errPrepare := prepareFileReplacement(s.externalConfigDir, "external.json", data, "external dirs")
	if errPrepare != nil {
		return pendingFileReplacement{}, errPrepare
	}
	return replacement, nil
}

func (s *Server) preparePinnedProjectsReplacement(pinnedDirs map[string]bool) (pendingFileReplacement, error) {
	data, errMarshal := marshalDirectoryMap(pinnedDirs)
	if errMarshal != nil {
		return pendingFileReplacement{}, fmt.Errorf("encoding pinned projects: %w", errMarshal)
	}
	replacement, errPrepare := prepareFileReplacement(s.pinnedConfigDir, "pinned.json", data, "pinned projects")
	if errPrepare != nil {
		return pendingFileReplacement{}, errPrepare
	}
	return replacement, nil
}

func marshalDirectoryMap(dirs map[string]bool) ([]byte, error) {
	values := slices.Collect(maps.Keys(dirs))
	if values == nil {
		values = []string{}
	}
	slices.Sort(values)
	return json.Marshal(values)
}

func prepareFileReplacement(configDir, fileName string, data []byte, label string) (pendingFileReplacement, error) {
	if errDir := os.MkdirAll(configDir, 0o755); errDir != nil {
		return pendingFileReplacement{}, fmt.Errorf("creating %s config directory: %w", label, errDir)
	}
	tempFile, errTemp := os.CreateTemp(configDir, fileName+".tmp-*")
	if errTemp != nil {
		return pendingFileReplacement{}, fmt.Errorf("writing %s: %w", label, errTemp)
	}
	tempPath := tempFile.Name()
	if _, errWrite := tempFile.Write(data); errWrite != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return pendingFileReplacement{}, fmt.Errorf("writing %s: %w", label, errWrite)
	}
	if errSync := tempFile.Sync(); errSync != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return pendingFileReplacement{}, fmt.Errorf("writing %s: %w", label, errSync)
	}
	if errClose := tempFile.Close(); errClose != nil {
		_ = os.Remove(tempPath)
		return pendingFileReplacement{}, fmt.Errorf("writing %s: %w", label, errClose)
	}
	return pendingFileReplacement{
		tempPath:   tempPath,
		targetPath: filepath.Join(configDir, fileName),
		label:      label,
	}, nil
}

func commitFileReplacements(replacements []pendingFileReplacement) (*committedFileReplacements, error) {
	committed := &committedFileReplacements{}
	for _, replacement := range replacements {
		info, errStat := os.Stat(replacement.targetPath)
		if errStat == nil && info.IsDir() {
			return nil, fmt.Errorf("writing %s: target path is a directory", replacement.label)
		}
		if errStat != nil && !os.IsNotExist(errStat) {
			return nil, fmt.Errorf("writing %s: %w", replacement.label, errStat)
		}
	}
	for _, replacement := range replacements {
		_, errStat := os.Stat(replacement.targetPath)

		committedReplacement := committedFileReplacement{
			targetPath:   replacement.targetPath,
			label:        replacement.label,
			parentConfig: filepath.Dir(replacement.targetPath),
		}
		if errStat == nil {
			backupPath, errBackup := prepareBackupPath(replacement.targetPath, replacement.label)
			if errBackup != nil {
				errRollback := committed.rollback()
				if errRollback != nil {
					return nil, errors.Join(errBackup, errRollback)
				}
				return nil, errBackup
			}
			if errRename := os.Rename(replacement.targetPath, backupPath); errRename != nil {
				errWrite := fmt.Errorf("writing %s: %w", replacement.label, errRename)
				errRollback := committed.rollback()
				if errRollback != nil {
					return nil, errors.Join(errWrite, errRollback)
				}
				return nil, errWrite
			}
			committedReplacement.backupPath = backupPath
			committedReplacement.hadOriginal = true
		}

		if errRename := os.Rename(replacement.tempPath, replacement.targetPath); errRename != nil {
			errRollbackCurrent := rollbackCommittedFileReplacement(committedReplacement)
			errRollbackCommitted := committed.rollback()
			if errRollbackCurrent != nil || errRollbackCommitted != nil {
				return nil, errors.Join(
					fmt.Errorf("writing %s: %w", replacement.label, errRename),
					errRollbackCurrent,
					errRollbackCommitted,
				)
			}
			return nil, fmt.Errorf("writing %s: %w", replacement.label, errRename)
		}

		committed.replacements = append(committed.replacements, committedReplacement)
	}
	if errSync := syncParentDirs(parentDirsForPendingReplacements(replacements)); errSync != nil {
		errRollback := committed.rollback()
		if errRollback != nil {
			return nil, errors.Join(errSync, errRollback)
		}
		return nil, errSync
	}
	return committed, nil
}

func cleanupFileReplacements(replacements []pendingFileReplacement) {
	for _, replacement := range replacements {
		_ = os.Remove(replacement.tempPath)
	}
}

func (r *committedFileReplacements) cleanup() {
	if r == nil {
		return
	}
	for _, replacement := range r.replacements {
		if replacement.backupPath == "" {
			continue
		}
		_ = os.Remove(replacement.backupPath)
	}
}

func (r *committedFileReplacements) rollback() error {
	if r == nil {
		return nil
	}
	var rollbackErrs []error
	for i := len(r.replacements) - 1; i >= 0; i-- {
		if errRollback := rollbackCommittedFileReplacement(r.replacements[i]); errRollback != nil {
			rollbackErrs = append(rollbackErrs, errRollback)
		}
	}
	if errSync := syncParentDirs(parentDirsForCommittedReplacements(r.replacements)); errSync != nil {
		rollbackErrs = append(rollbackErrs, errSync)
	}
	r.cleanup()
	return errors.Join(rollbackErrs...)
}

func rollbackCommittedFileReplacement(replacement committedFileReplacement) error {
	if replacement.hadOriginal {
		if errRemove := removeFileIfExists(replacement.targetPath, replacement.label); errRemove != nil {
			return errRemove
		}
		if errRename := os.Rename(replacement.backupPath, replacement.targetPath); errRename != nil {
			return fmt.Errorf("restoring %s: %w", replacement.label, errRename)
		}
		return nil
	}
	return removeFileIfExists(replacement.targetPath, replacement.label)
}

func removeFileIfExists(path, label string) error {
	info, errStat := os.Stat(path)
	if os.IsNotExist(errStat) {
		return nil
	}
	if errStat != nil {
		return fmt.Errorf("restoring %s: %w", label, errStat)
	}
	if info.IsDir() {
		return fmt.Errorf("restoring %s: target path is a directory", label)
	}
	if errRemove := os.Remove(path); errRemove != nil {
		return fmt.Errorf("restoring %s: %w", label, errRemove)
	}
	return nil
}

func prepareBackupPath(targetPath, label string) (string, error) {
	backupFile, errBackup := os.CreateTemp(filepath.Dir(targetPath), filepath.Base(targetPath)+".bak-*")
	if errBackup != nil {
		return "", fmt.Errorf("writing %s: %w", label, errBackup)
	}
	backupPath := backupFile.Name()
	if errClose := backupFile.Close(); errClose != nil {
		_ = os.Remove(backupPath)
		return "", fmt.Errorf("writing %s: %w", label, errClose)
	}
	if errRemove := os.Remove(backupPath); errRemove != nil {
		return "", fmt.Errorf("writing %s: %w", label, errRemove)
	}
	return backupPath, nil
}

func parentDirsForPendingReplacements(replacements []pendingFileReplacement) []string {
	parents := make([]string, 0, len(replacements))
	for _, replacement := range replacements {
		parents = append(parents, filepath.Dir(replacement.targetPath))
	}
	return parents
}

func parentDirsForCommittedReplacements(replacements []committedFileReplacement) []string {
	parents := make([]string, 0, len(replacements))
	for _, replacement := range replacements {
		parents = append(parents, replacement.parentConfig)
	}
	return parents
}

func syncParentDirs(parentDirs []string) error {
	seen := make(map[string]bool, len(parentDirs))
	var syncErrs []error
	for _, parentDir := range parentDirs {
		if parentDir == "" || seen[parentDir] {
			continue
		}
		seen[parentDir] = true
		dirHandle, errOpen := os.Open(parentDir)
		if errOpen != nil {
			syncErrs = append(syncErrs, fmt.Errorf("syncing %s: %w", parentDir, errOpen))
			continue
		}
		if errSync := dirHandle.Sync(); errSync != nil {
			syncErrs = append(syncErrs, fmt.Errorf("syncing %s: %w", parentDir, errSync))
		}
		if errClose := dirHandle.Close(); errClose != nil {
			syncErrs = append(syncErrs, fmt.Errorf("syncing %s: %w", parentDir, errClose))
		}
	}
	return errors.Join(syncErrs...)
}

func stageWorkspaceDirectoryForDeletion(workspacePath string) (string, error) {
	stagedPath, errStagedPath := os.MkdirTemp(filepath.Dir(workspacePath), filepath.Base(workspacePath)+".delete-*")
	if errStagedPath != nil {
		return "", errStagedPath
	}
	if errRemove := os.Remove(stagedPath); errRemove != nil {
		return "", errRemove
	}
	if errRename := os.Rename(workspacePath, stagedPath); errRename != nil {
		return "", errRename
	}
	return stagedPath, nil
}

func restoreStagedWorkspaceDirectory(stagedPath, workspacePath string) error {
	return os.Rename(stagedPath, workspacePath)
}

func forgetWorkspace(rootPath, forkName string) error {
	forgetCmd := exec.Command("jj", "workspace", "forget", forkName)
	forgetCmd.Dir = rootPath
	output, errForget := forgetCmd.CombinedOutput()
	if errForget != nil {
		return fmt.Errorf("%w: %s", errForget, output)
	}
	return nil
}

func currentOperationID(rootPath string) (string, error) {
	cmd := exec.Command("jj", "op", "log", "--limit", "1", "--no-graph")
	cmd.Dir = rootPath
	output, errOutput := cmd.Output()
	if errOutput != nil {
		return "", errOutput
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 {
		return "", fmt.Errorf("operation log is empty")
	}
	fields := strings.Fields(lines[0])
	if len(fields) == 0 {
		return "", fmt.Errorf("operation log did not contain an operation id")
	}
	return fields[0], nil
}

func restoreWorkspaceOperation(rootPath, operationID string) error {
	cmd := exec.Command("jj", "op", "restore", operationID)
	cmd.Dir = rootPath
	output, errRestore := cmd.CombinedOutput()
	if errRestore != nil {
		return fmt.Errorf("%w: %s", errRestore, output)
	}
	return nil
}

func rollbackDeletedForkWorkspace(rootPath, rollbackOpID, stagedPath, workspacePath string) error {
	errRestore := restoreStagedWorkspaceDirectory(stagedPath, workspacePath)
	if errRestore != nil {
		return errRestore
	}
	if errRestoreOp := restoreWorkspaceOperation(rootPath, rollbackOpID); errRestoreOp != nil {
		return errRestoreOp
	}
	return nil
}

func workspacePathCandidates(workspacePath string) []string {
	if workspacePath == "" {
		return nil
	}
	seen := make(map[string]bool)
	var candidates []string
	appendCandidate := func(value string) {
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		candidates = append(candidates, value)
	}
	appendCandidate(workspacePath)
	appendCandidate(filepath.Clean(workspacePath))
	if absPath, errAbs := filepath.Abs(workspacePath); errAbs == nil {
		appendCandidate(absPath)
		appendCandidate(resolveSymlinks(absPath))
	}
	appendCandidate(resolveSymlinks(workspacePath))
	return candidates
}

func directorySetContains(values map[string]bool, workspacePath string) bool {
	for _, candidate := range workspacePathCandidates(workspacePath) {
		if values[candidate] {
			return true
		}
	}
	return false
}

func deleteDirectorySetEntry(values map[string]bool, workspacePath string) {
	for _, candidate := range workspacePathCandidates(workspacePath) {
		delete(values, candidate)
	}
}
