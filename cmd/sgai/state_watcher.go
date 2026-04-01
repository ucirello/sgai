package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type workspaceStateSnapshot struct {
	modTime     time.Time
	goalModTime time.Time
	goalHash    string
}

func (s *Server) startStateWatcher() {
	go s.stateWatcherLoop(s.shutdownCtx)
}

func (s *Server) stateWatcherLoop(ctx context.Context) {
	snapshots := make(map[string]workspaceStateSnapshot)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.stopAllIDESessions(context.Background())
			return
		case <-ticker.C:
			s.pollWorkspaceStates(snapshots)
			s.cleanupIdleIDESessions(s.ideNow())
		}
	}
}

func (s *Server) pollWorkspaceStates(snapshots map[string]workspaceStateSnapshot) {
	groups, errScan := s.scanWorkspaceGroups()
	if errScan != nil {
		return
	}

	activeWorkspaces := make(map[string]bool)

	for _, grp := range groups {
		s.checkWorkspaceState(grp.Root.Directory, snapshots, activeWorkspaces)
		for _, fork := range grp.Forks {
			s.checkWorkspaceState(fork.Directory, snapshots, activeWorkspaces)
		}
	}

	for dir := range snapshots {
		if !activeWorkspaces[dir] {
			delete(snapshots, dir)
		}
	}
}

func (s *Server) checkWorkspaceState(dir string, snapshots map[string]workspaceStateSnapshot, activeWorkspaces map[string]bool) {
	activeWorkspaces[dir] = true
	stPath := statePath(dir)
	goalPath := filepath.Join(dir, "GOAL.md")

	info, errStat := os.Stat(stPath)
	if errStat != nil {
		delete(snapshots, dir)
		return
	}

	goalInfo, errGoalStat := os.Stat(goalPath)
	if errGoalStat != nil {
		goalInfo = nil
	}

	prev, hasPrev := snapshots[dir]
	if hasPrev && stateWatcherSnapshotUnchanged(prev, info.ModTime(), goalInfo) {
		return
	}

	current := buildStateSnapshot(info.ModTime(), goalInfo)

	if !hasPrev {
		snapshots[dir] = current
		return
	}

	s.emitStateChangeEvents(dir, s.sessionRunning(dir), prev, current)

	snapshots[dir] = current
}

func stateWatcherSnapshotUnchanged(prev workspaceStateSnapshot, modTime time.Time, goalInfo os.FileInfo) bool {
	if !modTime.Equal(prev.modTime) {
		return false
	}
	return goalSnapshotUnchanged(prev, goalInfo)
}

func goalSnapshotUnchanged(prev workspaceStateSnapshot, goalInfo os.FileInfo) bool {
	if goalInfo == nil {
		return prev.goalHash == ""
	}
	if !goalInfo.ModTime().Equal(prev.goalModTime) {
		return false
	}
	return hashGoalFile(goalInfo) == prev.goalHash
}

func buildStateSnapshot(modTime time.Time, goalInfo os.FileInfo) workspaceStateSnapshot {
	snapshot := workspaceStateSnapshot{
		modTime:     modTime,
		goalModTime: time.Time{},
		goalHash:    "",
	}
	if goalInfo != nil {
		snapshot.goalModTime = goalInfo.ModTime()
		snapshot.goalHash = hashGoalFile(goalInfo)
	}
	return snapshot
}

func (s *Server) emitStateChangeEvents(workspacePath string, running bool, prev, current workspaceStateSnapshot) {
	if prev.goalHash != current.goalHash {
		s.notifyWorkspaceListChange(workspacePath)
		return
	}
	if !running && !prev.modTime.Equal(current.modTime) {
		wfState := s.loadWorkspaceState(workspacePath)
		s.notifyWorkspaceChangeForState(workspacePath, &wfState, false)
	}
}

func hashGoalFile(goalInfo os.FileInfo) string {
	if goalInfo == nil {
		return ""
	}
	h := sha256.New()
	_, _ = fmt.Fprint(h, goalInfo.ModTime().String())
	_, _ = fmt.Fprintf(h, "%d", goalInfo.Size())
	return hex.EncodeToString(h.Sum(nil))[:16]
}
