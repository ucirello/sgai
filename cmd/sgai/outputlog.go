package main

import (
	"bufio"
	"container/ring"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"
)

const (
	outputBufferSize = 100
)

type logLine struct {
	prefix string
	text   string
}

type circularLogBuffer struct {
	mu   sync.RWMutex
	ring *ring.Ring
	size int
}

func newCircularLogBuffer() *circularLogBuffer {
	return &circularLogBuffer{
		mu:   sync.RWMutex{},
		ring: ring.New(outputBufferSize),
		size: 0,
	}
}

func (c *circularLogBuffer) add(line logLine) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ring.Value = line
	c.ring = c.ring.Next()
	if c.size < outputBufferSize {
		c.size++
	}
}

func (c *circularLogBuffer) lines() []logLine {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.size == 0 {
		return nil
	}

	result := make([]logLine, 0, c.size)
	startRing := c.ring
	if c.size < outputBufferSize {
		startRing = c.ring.Move(-c.size)
	}

	startRing.Do(func(v any) {
		if v != nil {
			result = append(result, v.(logLine))
		}
	})

	return result[:c.size]
}

type ringWriter struct {
	mu      sync.Mutex
	ring    *ring.Ring
	size    int
	partial []byte
}

func newRingWriter() *ringWriter {
	return &ringWriter{
		mu:      sync.Mutex{},
		ring:    ring.New(outputBufferSize),
		size:    0,
		partial: nil,
	}
}

func (r *ringWriter) Write(data []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	n := len(data)
	combined := r.partial
	combined = append(combined, data...)
	lines, partial := scanBufferedLines(combined, false)

	for _, line := range lines {
		r.addLine(line)
	}

	r.partial = partial

	return n, nil
}

func (r *ringWriter) dump(w io.Writer) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.size == 0 && len(r.partial) == 0 {
		return
	}

	pw := newPrefixWriter("", w, time.Now)

	startRing := r.ring
	if r.size < outputBufferSize {
		startRing = r.ring.Move(-r.size)
	}

	startRing.Do(func(v any) {
		if v != nil {
			if _, err := pw.Write([]byte(v.(string) + "\n")); err != nil {
				log.Println("write failed:", err)
			}
		}
	})

	lines, _ := scanBufferedLines(r.partial, true)
	for _, line := range lines {
		if _, err := pw.Write([]byte(line + "\n")); err != nil {
			log.Println("write failed:", err)
		}
	}
}

func (r *ringWriter) addLine(line string) {
	r.ring.Value = line
	r.ring = r.ring.Next()
	if r.size < outputBufferSize {
		r.size++
	}
}

func scanBufferedLines(data []byte, atEOF bool) (lines []string, partial []byte) {
	if len(data) == 0 {
		return nil, nil
	}

	remaining := data
	for len(remaining) > 0 {
		advance, token, errScan := bufio.ScanLines(remaining, atEOF)
		if errScan != nil {
			return lines, append([]byte{}, remaining...)
		}
		if advance == 0 {
			return lines, append([]byte{}, remaining...)
		}
		lines = append(lines, string(token))
		remaining = remaining[advance:]
	}

	return lines, nil
}

func prepareLogFile(logPath string) (*os.File, error) {
	if err := rotateLogFile(logPath); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening log file: %w", err)
	}

	return f, nil
}

func rotateLogFile(logPath string) error {
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		return nil
	}
	if errRename := os.Rename(logPath, logPath+".old"); errRename != nil {
		return fmt.Errorf("rotating log file %s: %w", logPath, errRename)
	}
	return nil
}

type sessionLogWriter struct {
	mu            sync.Mutex
	partial       []byte
	sess          *session
	workspacePath string
	srv           *Server
}

func newSessionLogWriter(sess *session, workspacePath string, srv *Server) *sessionLogWriter {
	return &sessionLogWriter{
		mu:            sync.Mutex{},
		partial:       nil,
		sess:          sess,
		workspacePath: workspacePath,
		srv:           srv,
	}
}

func (w *sessionLogWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	n := len(data)
	combined := w.partial
	combined = append(combined, data...)
	lines, partial := scanBufferedLines(combined, false)

	for _, line := range lines {
		w.addLine(line)
	}

	w.partial = partial

	return n, nil
}

func (w *sessionLogWriter) addLine(text string) {
	w.sess.outputLog.add(logLine{prefix: "", text: text})
	w.srv.notifyWorkspacePageChange(w.workspacePath)
}

func buildAgentOutputWriter(base io.Writer, extra ...io.Writer) io.Writer {
	writers := []io.Writer{base}
	for _, w := range extra {
		if w != nil {
			writers = append(writers, w)
		}
	}
	if len(writers) == 1 {
		return base
	}
	return io.MultiWriter(writers...)
}
