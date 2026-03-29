package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"sync"
	"syscall"
	"time"
)

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\].*?\x07|\x1b[^[\]].?`)

type adhocPromptState struct {
	mu            sync.Mutex
	running       bool
	output        bytes.Buffer
	linePrefix    string
	cmd           *exec.Cmd
	waitDone      chan struct{}
	stopRequested bool
}

func (s *Server) getAdhocState(workspacePath string) *adhocPromptState {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.adhocStates[workspacePath]
	if st == nil {
		st = &adhocPromptState{mu: sync.Mutex{}, running: false, output: bytes.Buffer{}, linePrefix: "", cmd: nil, waitDone: nil, stopRequested: false}
		s.adhocStates[workspacePath] = st
	}
	return st
}

type lockedWriter struct {
	mu  *sync.Mutex
	buf *bytes.Buffer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	stripped := ansiEscapePattern.ReplaceAll(p, nil)
	w.buf.Write(stripped)
	return len(p), nil
}

func (st *adhocPromptState) stop() {
	st.mu.Lock()
	if !st.running {
		st.mu.Unlock()
		return
	}
	cmd := st.cmd
	waitDone := st.waitDone
	st.stopRequested = true
	st.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		pgid := -cmd.Process.Pid
		_ = syscall.Kill(pgid, syscall.SIGTERM)

		if waitDone != nil {
			select {
			case <-waitDone:
			case <-time.After(gracefulShutdownTimeout):
				_ = syscall.Kill(pgid, syscall.SIGKILL)
				<-waitDone
			}
		}
	}

	st.mu.Lock()
	if st.running {
		st.running = false
		st.cmd = nil
		st.waitDone = nil
		st.stopRequested = false
	}
	_, _ = fmt.Fprintln(newPrefixWriter(st.linePrefix, &st.output, time.Now), "[stopped by user]")
	st.mu.Unlock()
}
