package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/mcp"
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

func TestToolExecutorHandlesMCPMetaList(t *testing.T) {
	manager := &fakeMCPManager{
		tools: []mcp.MCPTool{{
			Name:        "hello",
			Description: "say hello",
			ServerName:  "test",
		}},
	}

	agent := &Agent{
		mcpManager:   manager,
		interruptCtx: context.Background(),
		outputMutex:  &sync.Mutex{},
	}

	executor := NewToolExecutor(agent)

	tc := api.ToolCall{ID: "call_1", Type: "function"}
	tc.Function.Name = "mcp_tools"
	args := map[string]interface{}{"action": "list"}
	payload, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	tc.Function.Arguments = string(payload)

	msg := executor.executeSingleTool(tc)

	if !strings.Contains(msg.Content, "mcp_test_hello") {
		t.Fatalf("expected list output to include MCP tool name, got: %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "Available MCP tools (1)") {
		t.Fatalf("expected count in output, got: %q", msg.Content)
	}
}

func TestToolExecutorFallbacksToMCPExecution(t *testing.T) {
	manager := &fakeMCPManager{
		tools: []mcp.MCPTool{{
			Name:        "hello",
			Description: "say hello",
			ServerName:  "test",
		}},
		callResult: &mcp.MCPToolCallResult{
			Content: []mcp.MCPContent{{Type: "text", Text: "hi"}},
			IsError: false,
		},
	}

	agent := &Agent{
		mcpManager:   manager,
		interruptCtx: context.Background(),
		outputMutex:  &sync.Mutex{},
	}

	executor := NewToolExecutor(agent)

	tc := api.ToolCall{ID: "call_2", Type: "function"}
	tc.Function.Name = "mcp_test_hello"
	tc.Function.Arguments = "{}"

	msg := executor.executeSingleTool(tc)

	if msg.Content != "hi" {
		t.Fatalf("expected MCP call result 'hi', got: %q", msg.Content)
	}
	if manager.lastServer != "test" || manager.lastTool != "hello" {
		t.Fatalf("unexpected MCP call routing: server=%q tool=%q", manager.lastServer, manager.lastTool)
	}
}

func TestToolExecutorDoesNotTranslateLegacyNames(t *testing.T) {
	manager := &fakeMCPManager{}

	agent := &Agent{
		mcpManager:   manager,
		interruptCtx: context.Background(),
		outputMutex:  &sync.Mutex{},
	}

	executor := NewToolExecutor(agent)

	tc := api.ToolCall{ID: "call_legacy", Type: "function"}
	tc.Function.Name = "github:search"
	tc.Function.Arguments = "{}"

	msg := executor.executeSingleTool(tc)

	if !strings.Contains(msg.Content, "unknown tool 'github:search'") {
		t.Fatalf("expected unknown tool error, got: %q", msg.Content)
	}
	if manager.lastServer != "" {
		t.Fatalf("expected MCP manager not to be invoked, but CallTool captured server=%q", manager.lastServer)
	}
}
