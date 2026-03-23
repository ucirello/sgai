package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

var (
	errPathNotAbsolute = errors.New("path must be absolute")
	errNotADirectory   = errors.New("path is not a directory")
	errAlreadyAttached = errors.New("directory is already attached as an external workspace")
	errNotAttached     = errors.New("directory is not attached as an external workspace")
)

func (s *Server) externalFilePath() string {
	return filepath.Join(s.externalConfigDir, "external.json")
}

func loadPersistedDirectorySet(filePath, label, pruneLogPrefix, warningLogPrefix string) (map[string]bool, bool, error) {
	data, errRead := os.ReadFile(filePath)
	if errRead != nil {
		if os.IsNotExist(errRead) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("reading %s: %w", label, errRead)
	}
	var dirs []string
	if errJSON := json.Unmarshal(data, &dirs); errJSON != nil {
		return nil, false, fmt.Errorf("parsing %s: %w", label, errJSON)
	}
	existing := make(map[string]bool, len(dirs))
	pruned := false
	for _, d := range dirs {
		if _, errStat := os.Stat(d); errStat == nil {
			existing[resolveSymlinks(d)] = true
		} else if os.IsNotExist(errStat) {
			log.Println(pruneLogPrefix, d)
			pruned = true
		} else {
			log.Println(warningLogPrefix, d, errStat)
			existing[resolveSymlinks(d)] = true
		}
	}
	return existing, pruned, nil
}

func (s *Server) loadExternalDirs() error {
	log.Println("attached directories path", s.externalFilePath())
	existing, pruned, errLoad := loadPersistedDirectorySet(
		s.externalFilePath(),
		"external dirs",
		"pruning stale attached path:",
		"warning: cannot verify attached path:",
	)
	if errLoad != nil {
		return errLoad
	}
	if existing == nil {
		return nil
	}
	if !pruned {
		s.mu.Lock()
		s.externalDirs = existing
		s.mu.Unlock()
		return nil
	}
	state := s.currentWorkspaceListState()
	state.externalDirs = existing
	if errSave := s.saveWorkspaceListState(state, true, false); errSave != nil {
		return errSave
	}
	s.commitWorkspaceListState(state)
	return nil
}

func (s *Server) saveExternalDirs() error {
	state := s.currentWorkspaceListState()
	return s.saveWorkspaceListState(state, true, false)
}

type attachExternalResult struct {
	Name    string
	Dir     string
	HasGoal bool
}

func (s *Server) attachExternalWorkspaceService(path string) (attachExternalResult, error) {
	if !filepath.IsAbs(path) {
		return attachExternalResult{}, errPathNotAbsolute
	}

	info, errStat := os.Stat(path)
	if errStat != nil {
		if os.IsNotExist(errStat) {
			return attachExternalResult{}, fmt.Errorf("directory does not exist: %s", path)
		}
		return attachExternalResult{}, fmt.Errorf("checking path: %w", errStat)
	}
	if !info.IsDir() {
		return attachExternalResult{}, errNotADirectory
	}

	canonical := resolveSymlinks(path)
	s.mu.Lock()
	if directorySetContains(s.externalDirs, path) {
		s.mu.Unlock()
		return attachExternalResult{}, errAlreadyAttached
	}
	s.mu.Unlock()

	hasGoal := false
	if _, errGoal := os.Stat(filepath.Join(path, "GOAL.md")); errGoal == nil {
		hasGoal = true
	}

	sgaiDir := filepath.Join(path, ".sgai")
	if _, errSGAI := os.Stat(sgaiDir); os.IsNotExist(errSGAI) {
		if errInit := initializeWorkspace(path); errInit != nil {
			return attachExternalResult{}, fmt.Errorf("initializing workspace: %w", errInit)
		}
	}

	s.mu.Lock()
	if directorySetContains(s.externalDirs, path) {
		s.mu.Unlock()
		return attachExternalResult{}, errAlreadyAttached
	}
	s.mu.Unlock()

	state := s.currentWorkspaceListState()
	state.externalDirs[canonical] = true

	if errSave := s.saveWorkspaceListState(state, true, false); errSave != nil {
		return attachExternalResult{}, fmt.Errorf("saving external dirs: %w", errSave)
	}
	s.commitWorkspaceListState(state)

	s.invalidateWorkspaceScanCache()
	s.notifyStateChange()

	return attachExternalResult{
		Name:    filepath.Base(path),
		Dir:     path,
		HasGoal: hasGoal,
	}, nil
}

type detachExternalResult struct {
	Detached bool
	Message  string
}

func (s *Server) detachExternalWorkspaceService(path string) (detachExternalResult, error) {
	return s.detachWorkspaceService(path)
}

func (s *Server) isExternalWorkspace(workspacePath string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return directorySetContains(s.externalDirs, workspacePath)
}

type deleteExternalForkResult struct {
	Deleted bool
	Message string
}

func (s *Server) deleteExternalForkService(forkDir string) (deleteExternalForkResult, error) {
	result, errDelete := s.deleteForkWorkspaceService(forkDir)
	if errDelete != nil {
		return deleteExternalForkResult{}, errDelete
	}
	return deleteExternalForkResult(result), nil
}

type directoryEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
}

func browseDirectoriesService(path string) ([]directoryEntry, error) {
	if path != "" && !filepath.IsAbs(path) {
		return nil, errPathNotAbsolute
	}

	if path == "" {
		home, errHome := os.UserHomeDir()
		if errHome != nil {
			path = "/"
		} else {
			path = home
		}
	}

	entries, errRead := os.ReadDir(path)
	if errRead != nil {
		if os.IsNotExist(errRead) {
			return nil, fmt.Errorf("directory does not exist: %s", path)
		}
		return nil, fmt.Errorf("reading directory: %w", errRead)
	}

	var result []directoryEntry
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		result = append(result, directoryEntry{
			Name:  entry.Name(),
			Path:  filepath.Join(path, entry.Name()),
			IsDir: true,
		})
	}

	return result, nil
}
