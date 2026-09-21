package agent

// Tests for the mcp_tools meta-tool's action=call path — the schema
// advertises it, so dispatch must actually work (a call that falls through
// to "unknown action" strands any agent-mediated MCP flow, e.g. connecting
// a Figma server from the design empty state).

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/mcp"
)

// callRecordingManager satisfies mcp.MCPManager and records the last CallTool
// invocation so the test can assert the meta-tool parsed and forwarded
// server/tool/arguments intact.
type callRecordingManager struct {
	mu         sync.Mutex
	lastServer string
	lastTool   string
	lastArgs   map[string]interface{}
	callCount  int
	result     *mcp.MCPToolCallResult
	err        error
}

func (m *callRecordingManager) AddServer(config mcp.MCPServerConfig) error  { return nil }
func (m *callRecordingManager) RemoveServer(name string) error              { return nil }
func (m *callRecordingManager) GetServer(name string) (mcp.MCPServer, bool) { return nil, false }
func (m *callRecordingManager) ListServers() []mcp.MCPServer                { return nil }
func (m *callRecordingManager) StartAll(ctx context.Context) error          { return nil }
func (m *callRecordingManager) StopAll(ctx context.Context) error           { return nil }
func (m *callRecordingManager) GetAllTools(ctx context.Context) ([]mcp.MCPTool, error) {
	return nil, nil
}

func (m *callRecordingManager) CallTool(ctx context.Context, serverName, toolName string, args map[string]interface{}) (*mcp.MCPToolCallResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastServer = serverName
	m.lastTool = toolName
	m.lastArgs = args
	m.callCount++
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func callTestAgent(mgr mcp.MCPManager) *Agent {
	a := &Agent{mcpSub: NewAgentMCPManager()}
	a.mcpSub.SetManager(mgr)
	return a
}

func TestMCPToolsCall_ForwardsServerToolArguments(t *testing.T) {
	mgr := &callRecordingManager{result: &mcp.MCPToolCallResult{
		Content: []mcp.MCPContent{{Type: "text", Text: "frame imported"}},
	}}
	a := callTestAgent(mgr)

	out, err := a.handleMCPToolsCommand(map[string]interface{}{
		"action":    "call",
		"server":    "figma",
		"tool":      "get_file",
		"arguments": map[string]interface{}{"fileKey": "abc123"},
	})
	if err != nil {
		t.Fatalf("expected call to succeed, got error: %v", err)
	}
	if !strings.Contains(out, "frame imported") {
		t.Errorf("expected tool output in result, got %q", out)
	}
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if mgr.callCount != 1 {
		t.Errorf("expected exactly one CallTool, got %d", mgr.callCount)
	}
	if mgr.lastServer != "figma" || mgr.lastTool != "get_file" {
		t.Errorf("expected figma/get_file, got %s/%s", mgr.lastServer, mgr.lastTool)
	}
	if mgr.lastArgs["fileKey"] != "abc123" {
		t.Errorf("expected arguments forwarded intact, got %v", mgr.lastArgs)
	}
}

func TestMCPToolsCall_MissingServerOrTool(t *testing.T) {
	a := callTestAgent(&callRecordingManager{})

	for _, args := range []map[string]interface{}{
		{"action": "call", "tool": "get_file"},
		{"action": "call", "server": "figma"},
		{"action": "call"},
	} {
		if _, err := a.handleMCPToolsCommand(args); err == nil {
			t.Errorf("expected error for args %v, got nil", args)
		}
	}
}

func TestMCPToolsCall_ManagerErrorSurfaces(t *testing.T) {
	mgr := &callRecordingManager{err: context.DeadlineExceeded}
	a := callTestAgent(mgr)

	if _, err := a.handleMCPToolsCommand(map[string]interface{}{
		"action": "call", "server": "figma", "tool": "get_file",
	}); err == nil {
		t.Error("expected the manager error to surface, got nil")
	}
}
