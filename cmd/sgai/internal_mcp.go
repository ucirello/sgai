package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const agentIdentityHeader = "X-Sgai-Agent-Identity"

type internalMCPArgs struct {
	mcpURL        string
	agentIdentity string
}

func parseInternalMCPArgs(args []string) (internalMCPArgs, error) {
	if len(args) != 2 {
		return internalMCPArgs{}, errors.New("usage: sgai internal-mcp <mcp-url> <agent-identity>")
	}

	mcpURL := strings.TrimSpace(args[0])
	if mcpURL == "" {
		return internalMCPArgs{}, errors.New("mcp URL is required")
	}
	if _, errParse := url.ParseRequestURI(mcpURL); errParse != nil {
		return internalMCPArgs{}, fmt.Errorf("invalid MCP URL: %w", errParse)
	}

	agentIdentity := strings.TrimSpace(args[1])
	if agentIdentity == "" {
		return internalMCPArgs{}, errors.New("agent identity is required")
	}

	return internalMCPArgs{mcpURL: mcpURL, agentIdentity: agentIdentity}, nil
}

func runInternalMCP(ctx context.Context, args []string, stdin io.ReadCloser, stdout io.WriteCloser) error {
	parsedArgs, errParse := parseInternalMCPArgs(args)
	if errParse != nil {
		return errParse
	}

	return runInternalMCPBridge(ctx, parsedArgs.mcpURL, parsedArgs.agentIdentity, &mcp.IOTransport{
		Reader: stdin,
		Writer: stdout,
	}, nil)
}

func runInternalMCPBridge(ctx context.Context, mcpURL, agentIdentity string, localTransport mcp.Transport, httpClient *http.Client) error {
	localConn, errLocal := localTransport.Connect(ctx)
	if errLocal != nil {
		return fmt.Errorf("connecting stdio MCP transport: %w", errLocal)
	}

	if httpClient == nil {
		httpClient = newInternalMCPHTTPClient(agentIdentity)
	}

	remoteTransport := &mcp.StreamableClientTransport{
		Endpoint:             mcpURL,
		HTTPClient:           httpClient,
		MaxRetries:           5,
		DisableStandaloneSSE: false,
		OAuthHandler:         nil,
	}
	remoteConn, errRemote := remoteTransport.Connect(ctx)
	if errRemote != nil {
		_ = localConn.Close()
		return fmt.Errorf("connecting HTTP MCP transport: %w", errRemote)
	}

	return bridgeMCPConnections(ctx, localConn, remoteConn)
}

func bridgeMCPConnections(ctx context.Context, left, right mcp.Connection) error {
	bridgeCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var closeOnce sync.Once
	closeBoth := func() {
		closeOnce.Do(func() {
			_ = left.Close()
			_ = right.Close()
		})
	}

	errCh := make(chan error, 2)
	go func() {
		errCh <- copyMCPMessages(bridgeCtx, left, right)
		closeBoth()
	}()
	go func() {
		errCh <- copyMCPMessages(bridgeCtx, right, left)
		closeBoth()
	}()

	errFirst := <-errCh
	cancel()
	closeBoth()
	errSecond := <-errCh

	if errUnexpected := firstUnexpectedBridgeError(errFirst, errSecond); errUnexpected != nil {
		return errUnexpected
	}
	return nil
}

func copyMCPMessages(ctx context.Context, src, dst mcp.Connection) error {
	for {
		msg, errRead := src.Read(ctx)
		if errRead != nil {
			return fmt.Errorf("reading MCP message: %w", errRead)
		}
		if errWrite := dst.Write(ctx, msg); errWrite != nil {
			return fmt.Errorf("writing MCP message: %w", errWrite)
		}
	}
}

func firstUnexpectedBridgeError(errs ...error) error {
	for _, errCurrent := range errs {
		if isExpectedBridgeShutdown(errCurrent) {
			continue
		}
		return errCurrent
	}
	return nil
}

func isExpectedBridgeShutdown(err error) bool {
	return err == nil ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, mcp.ErrConnectionClosed)
}

func newInternalMCPHTTPClient(agentIdentity string) *http.Client {
	return &http.Client{
		Transport: internalMCPHeaderRoundTripper{
			baseTransport: nil,
			agentIdentity: agentIdentity,
		},
		CheckRedirect: nil,
		Jar:           nil,
		Timeout:       0,
	}
}

type internalMCPHeaderRoundTripper struct {
	baseTransport http.RoundTripper
	agentIdentity string
}

func (t internalMCPHeaderRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	request := r.Clone(r.Context())
	request.Header = r.Header.Clone()
	request.Header.Set(agentIdentityHeader, t.agentIdentity)
	response, errRoundTrip := t.transport().RoundTrip(request)
	if errRoundTrip != nil {
		return nil, fmt.Errorf("sending MCP request: %w", errRoundTrip)
	}
	return response, nil
}

func (t internalMCPHeaderRoundTripper) transport() http.RoundTripper {
	if t.baseTransport != nil {
		return t.baseTransport
	}
	return http.DefaultTransport
}
