package main

import (
	"context"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func buildExternalMCPHandler() http.Handler {
	return mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return buildExternalMCPServer()
	}, nil)
}

func buildExternalMCPServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "sgai-external"}, nil)
	registerExternalTools(server)
	return server
}

func registerExternalTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "hello world",
		Description: "Return hello world.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, emptyResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "hello world"}},
		}, emptyResult{}, nil
	})
}
