package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ucirello/sgai/pkg/state"
)

func TestNewCircularLogBuffer(t *testing.T) {
	buf := newCircularLogBuffer()
	assert.NotNil(t, buf)
	assert.NotNil(t, buf.ring)
	assert.Equal(t, 0, buf.size)
}

func TestCircularLogBufferAdd(t *testing.T) {
	buf := newCircularLogBuffer()

	buf.add(logLine{prefix: "test", text: "line1"})
	assert.Equal(t, 1, buf.size)

	buf.add(logLine{prefix: "test", text: "line2"})
	assert.Equal(t, 2, buf.size)
}

func TestCircularLogBufferLines(t *testing.T) {
	tests := []struct {
		name     string
		addLines []logLine
		expected []logLine
	}{
		{
			name:     "emptyBuffer",
			addLines: []logLine{},
			expected: nil,
		},
		{
			name: "singleLine",
			addLines: []logLine{
				{prefix: "test", text: "line1"},
			},
			expected: []logLine{
				{prefix: "test", text: "line1"},
			},
		},
		{
			name: "multipleLines",
			addLines: []logLine{
				{prefix: "test", text: "line1"},
				{prefix: "test", text: "line2"},
				{prefix: "test", text: "line3"},
			},
			expected: []logLine{
				{prefix: "test", text: "line1"},
				{prefix: "test", text: "line2"},
				{prefix: "test", text: "line3"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := newCircularLogBuffer()
			for _, line := range tt.addLines {
				buf.add(line)
			}

			lines := buf.lines()
			assert.Len(t, lines, len(tt.expected))
			for i, expected := range tt.expected {
				if i < len(lines) {
					assert.Equal(t, expected.text, lines[i].text)
				}
			}
		})
	}
}

func TestNewRingWriter(t *testing.T) {
	rw := newRingWriter()
	assert.NotNil(t, rw)
	assert.NotNil(t, rw.ring)
	assert.Equal(t, 0, rw.size)
}

func TestRingWriterWrite(t *testing.T) {
	rw := newRingWriter()

	n, err := rw.Write([]byte("line1\n"))
	require.NoError(t, err)
	assert.Equal(t, 6, n)
	assert.Equal(t, 1, rw.size)

	n, err = rw.Write([]byte("line2\n"))
	require.NoError(t, err)
	assert.Equal(t, 6, n)
	assert.Equal(t, 2, rw.size)
}

func TestRingWriterWritePartial(t *testing.T) {
	rw := newRingWriter()

	n, err := rw.Write([]byte("partial"))
	require.NoError(t, err)
	assert.Equal(t, 7, n)
	assert.Equal(t, 0, rw.size)
	assert.Equal(t, []byte("partial"), rw.partial)

	n, err = rw.Write([]byte(" line\n"))
	require.NoError(t, err)
	assert.Equal(t, 6, n)
	assert.Equal(t, 1, rw.size)
	assert.Nil(t, rw.partial)
}

func TestRingWriterDump(t *testing.T) {
	rw := newRingWriter()

	_, _ = rw.Write([]byte("line1\n"))
	_, _ = rw.Write([]byte("line2\n"))

	var buf bytes.Buffer
	rw.dump(&buf)

	assert.Equal(t, []string{"line1", "line2"}, stripTimestampedPayloads(t, buf.String()))
}

func TestRingWriterDumpEmpty(t *testing.T) {
	rw := newRingWriter()

	var buf bytes.Buffer
	rw.dump(&buf)

	assert.Empty(t, buf.String())
}

func TestRingWriterDumpPartial(t *testing.T) {
	rw := newRingWriter()

	_, _ = rw.Write([]byte("line1\n"))
	_, _ = rw.Write([]byte("partial"))

	var buf bytes.Buffer
	rw.dump(&buf)

	assert.Equal(t, []string{"line1", "partial"}, stripTimestampedPayloads(t, buf.String()))
	assert.Equal(t, 1, rw.size)
}

func TestRingWriterDumpTrimsTrailingCarriageReturnAtEOF(t *testing.T) {
	rw := newRingWriter()

	_, _ = rw.Write([]byte("partial\r"))

	var buf bytes.Buffer
	rw.dump(&buf)

	assert.Equal(t, []string{"partial"}, stripTimestampedPayloads(t, buf.String()))
}

func TestRingWriterDumpPreservesIntentionalBlankLines(t *testing.T) {
	rw := newRingWriter()

	_, _ = rw.Write([]byte("line1\n\nline3\n"))

	var buf bytes.Buffer
	rw.dump(&buf)

	assert.Equal(t, []string{"line1", "", "line3"}, stripTimestampedPayloads(t, buf.String()))
}

func TestScanBufferedLinesHandlesCRLF(t *testing.T) {
	lines, partial := scanBufferedLines([]byte("line1\r\nline2\r\nline3"), false)
	assert.Equal(t, []string{"line1", "line2"}, lines)
	assert.Equal(t, []byte("line3"), partial)
}

func TestBuildAgentOutputWriter(t *testing.T) {
	tests := []struct {
		name        string
		base        *bytes.Buffer
		extra       []io.Writer
		expectMulti bool
	}{
		{
			name:        "singleWriter",
			base:        &bytes.Buffer{},
			extra:       nil,
			expectMulti: false,
		},
		{
			name:        "multipleWriters",
			base:        &bytes.Buffer{},
			extra:       []io.Writer{&bytes.Buffer{}},
			expectMulti: true,
		},
		{
			name:        "nilExtraWriter",
			base:        &bytes.Buffer{},
			extra:       []io.Writer{nil},
			expectMulti: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildAgentOutputWriter(tt.base, tt.extra...)
			if tt.expectMulti {
				assert.NotNil(t, result)
			} else {
				assert.Equal(t, tt.base, result)
			}
		})
	}
}

func TestPrepareLogFile(t *testing.T) {
	t.Run("newFile", func(t *testing.T) {
		dir := t.TempDir()
		logPath := filepath.Join(dir, "output.log")

		f, err := prepareLogFile(logPath)
		require.NoError(t, err)
		t.Cleanup(func() { _ = f.Close() })

		_, errStat := os.Stat(logPath)
		require.NoError(t, errStat)
	})

	t.Run("rotatesExisting", func(t *testing.T) {
		dir := t.TempDir()
		logPath := filepath.Join(dir, "output.log")
		require.NoError(t, os.WriteFile(logPath, []byte("old content"), 0o644))

		f, err := prepareLogFile(logPath)
		require.NoError(t, err)
		t.Cleanup(func() { _ = f.Close() })

		oldContent, errOld := os.ReadFile(logPath + ".old")
		require.NoError(t, errOld)
		assert.Equal(t, "old content", string(oldContent))
	})
}

func TestRotateLogFile(t *testing.T) {
	t.Run("fileNotExists", func(t *testing.T) {
		dir := t.TempDir()
		err := rotateLogFile(filepath.Join(dir, "nonexistent.log"))
		require.NoError(t, err)
	})

	t.Run("fileExists", func(t *testing.T) {
		dir := t.TempDir()
		logPath := filepath.Join(dir, "output.log")
		require.NoError(t, os.WriteFile(logPath, []byte("content"), 0o644))

		err := rotateLogFile(logPath)
		require.NoError(t, err)

		_, errStat := os.Stat(logPath)
		assert.True(t, os.IsNotExist(errStat))

		data, errRead := os.ReadFile(logPath + ".old")
		require.NoError(t, errRead)
		assert.Equal(t, "content", string(data))
	})
}

func TestSessionLogWriter(t *testing.T) {
	sess := newTestSession()
	sess.outputLog = newCircularLogBuffer()
	srv, _ := setupTestServer(t)

	w := newSessionLogWriter(sess, "/test", srv)

	n, err := w.Write([]byte("hello world\n"))
	require.NoError(t, err)
	assert.Equal(t, 12, n)

	lines := sess.outputLog.lines()
	require.GreaterOrEqual(t, len(lines), 1)
	assert.Equal(t, "hello world", lines[0].text)
}

func TestSessionLogWriterKeepsWorkspaceListCacheForLogUpdates(t *testing.T) {
	srv, rootDir := setupTestServer(t)
	wsDir := setupTestWorkspace(t, srv, rootDir, "log-cache")
	attachRunningSessionCoordinator(t, srv, wsDir, workflowRef(func(workflow *state.Workflow) {
		workflow.Status = state.StatusWorking
		workflow.CurrentAgent = "go-developer"
		workflow.Task = "stream logs"
	}))

	srv.mu.Lock()
	sess := srv.sessions[wsDir]
	sess.outputLog = newCircularLogBuffer()
	srv.mu.Unlock()

	_ = srv.loadWorkspaceListResponse()
	_, errLoad := srv.loadWorkspacePageState(wsDir)
	require.NoError(t, errLoad)

	writer := newSessionLogWriter(sess, wsDir, srv)
	writer.addLine("line 1")

	_, okList := srv.workspaceListCache.get("workspaces")
	assert.True(t, okList)
	assert.False(t, hasCachedWorkspacePageState(srv, wsDir))
}

func TestSessionLogWriterMultipleLines(t *testing.T) {
	sess := newTestSession()
	sess.outputLog = newCircularLogBuffer()
	srv, _ := setupTestServer(t)

	w := newSessionLogWriter(sess, "/test", srv)

	_, _ = w.Write([]byte("line1\nline2\nline3\n"))

	lines := sess.outputLog.lines()
	require.GreaterOrEqual(t, len(lines), 3)
	assert.Equal(t, "line1", lines[0].text)
	assert.Equal(t, "line2", lines[1].text)
	assert.Equal(t, "line3", lines[2].text)
}

func TestSessionLogWriterPartialLine(t *testing.T) {
	sess := newTestSession()
	sess.outputLog = newCircularLogBuffer()
	srv, _ := setupTestServer(t)

	w := newSessionLogWriter(sess, "/test", srv)

	_, _ = w.Write([]byte("part"))
	assert.Empty(t, sess.outputLog.lines())

	_, _ = w.Write([]byte("ial\n"))
	lines := sess.outputLog.lines()
	require.GreaterOrEqual(t, len(lines), 1)
	assert.Equal(t, "partial", lines[0].text)
}

func stripTimestampedPayloads(t *testing.T, output string) []string {
	t.Helper()

	trimmed := strings.TrimSuffix(output, "\n")
	if trimmed == "" {
		return nil
	}

	lines := strings.Split(trimmed, "\n")
	payloads := make([]string, 0, len(lines))
	for _, line := range lines {
		require.GreaterOrEqual(t, len(line), len("[00:00:00] "))
		require.Equal(t, byte('['), line[0])
		require.Equal(t, byte(':'), line[3])
		require.Equal(t, byte(':'), line[6])
		require.Equal(t, byte(']'), line[9])
		require.Equal(t, byte(' '), line[10])
		payloads = append(payloads, line[len("[00:00:00] "):])
	}

	return payloads
}
