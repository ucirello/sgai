package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ucirello/sgai/pkg/state"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestPrintUsageOmitsInternalMCPCommand(t *testing.T) {
	output := captureStdout(t, printUsage)
	assert.NotContains(t, output, "internal-mcp")
}

func TestRequiresOpencode(t *testing.T) {
	tests := []struct {
		name       string
		subcommand string
		want       bool
	}{
		{name: "help", subcommand: "help", want: false},
		{name: "shortHelp", subcommand: "-h", want: false},
		{name: "longHelp", subcommand: "--help", want: false},
		{name: "hiddenInternalMCP", subcommand: "internal-mcp", want: false},
		{name: "serve", subcommand: "serve", want: true},
		{name: "defaultServe", subcommand: "", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, requiresOpencode(tt.subcommand))
		})
	}
}

func TestRunInternalMCPBridgeExposesCoordinatorToolsWhenIdentityForwarded(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.json")
	coord, errCoord := state.NewCoordinatorWith(stateFile, state.NewWorkflow())
	require.NoError(t, errCoord)

	mcpURL, closeFn, errStart := startMCPHTTPServer(t.TempDir(), coord, []string{"builder", "coordinator"})
	require.NoError(t, errStart)
	t.Cleanup(closeFn)

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errBridgeCh := make(chan error, 1)
	go func() {
		errBridgeCh <- runInternalMCPBridge(ctx, mcpURL, "coordinator|model-a", serverTransport, nil)
	}()

	client := mcp.NewClient(&mcp.Implementation{
		Name:       "test-client",
		Title:      "",
		Version:    "test-version",
		WebsiteURL: "",
		Icons:      nil,
	}, nil)
	cs, errClient := client.Connect(context.Background(), clientTransport, nil)
	require.NoError(t, errClient)
	t.Cleanup(func() {
		_ = cs.Close()
	})

	result, errList := cs.ListTools(context.Background(), &mcp.ListToolsParams{Meta: mcp.Meta{}, Cursor: ""})
	require.NoError(t, errList)
	assert.True(t, slices.Contains(mcpToolNames(result.Tools), "delete_unread_messages"))

	require.NoError(t, cs.Close())
	cancel()
	require.NoError(t, <-errBridgeCh)
}

func TestInternalMCPHeaderRoundTripperUsesSharedAgentIdentityHeader(t *testing.T) {
	var gotRequest *http.Request
	roundTripper := internalMCPHeaderRoundTripper{
		baseTransport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			gotRequest = r.Clone(r.Context())
			recorder := httptest.NewRecorder()
			recorder.WriteHeader(http.StatusNoContent)
			return recorder.Result(), nil
		}),
		agentIdentity: "builder|model-a",
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.com/mcp", http.NoBody)
	resp, errRoundTrip := roundTripper.RoundTrip(req)
	require.NoError(t, errRoundTrip)
	require.NoError(t, resp.Body.Close())
	require.NotNil(t, gotRequest)
	assert.Equal(t, "builder|model-a", gotRequest.Header.Get(agentIdentityHeader))
	assert.Equal(t, "builder", parseAgentIdentityHeader(gotRequest))
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	originalStdout := os.Stdout
	reader, writer, errPipe := os.Pipe()
	require.NoError(t, errPipe)
	os.Stdout = writer
	t.Cleanup(func() {
		os.Stdout = originalStdout
	})

	fn()
	require.NoError(t, writer.Close())

	output, errRead := io.ReadAll(reader)
	require.NoError(t, errRead)
	require.NoError(t, reader.Close())
	return string(output)
}
