// Shared test fixtures for MCP manager fakes.
//
// Several test files (utils_test.go, formerly tool_executor_test.go) need a
// fake mcp.Manager. After removing the legacy ToolExecutor (and its
// dedicated test file that originally declared fakeMCPManager), this fake
// was relocated here so live tests like
// TestSuggestCorrectToolNameResolvesLegacyMCPName can still reference it.
//
// Package-private since it's only used in-package.
package agent

import (
	"context"

	"github.com/sprout-foundry/sprout/pkg/mcp"
)

type fakeMCPManager struct {
	tools         []mcp.MCPTool
	callResult    *mcp.MCPToolCallResult
	lastServer    string
	lastTool      string
	lastArguments map[string]interface{}
}

func (f *fakeMCPManager) AddServer(config mcp.MCPServerConfig) error  { return nil }
func (f *fakeMCPManager) RemoveServer(name string) error              { return nil }
func (f *fakeMCPManager) GetServer(name string) (mcp.MCPServer, bool) { return nil, false }
func (f *fakeMCPManager) ListServers() []mcp.MCPServer                { return nil }
func (f *fakeMCPManager) StartAll(ctx context.Context) error          { return nil }
func (f *fakeMCPManager) StopAll(ctx context.Context) error           { return nil }

func (f *fakeMCPManager) GetAllTools(ctx context.Context) ([]mcp.MCPTool, error) {
	return f.tools, nil
}

func (f *fakeMCPManager) CallTool(ctx context.Context, serverName, toolName string, args map[string]interface{}) (*mcp.MCPToolCallResult, error) {
	f.lastServer = serverName
	f.lastTool = toolName
	f.lastArguments = args

	if f.callResult != nil {
		return f.callResult, nil
	}

	return &mcp.MCPToolCallResult{
		Content: []mcp.MCPContent{{Type: "text", Text: "ok"}},
		IsError: false,
	}, nil
}
