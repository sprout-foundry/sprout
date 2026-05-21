package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// mockReadFileHandler implements ToolHandler for testing registry dispatch.
type mockReadFileHandler struct{}

func (h *mockReadFileHandler) Name() string { return "read_file" }

func (h *mockReadFileHandler) Definition() api.Tool {
	return api.Tool{Type: "function", Function: api.ToolFunction{Name: "read_file", Description: "mock read_file for testing"}}
}

func (h *mockReadFileHandler) Validate(_ map[string]any) error { return nil }

func (h *mockReadFileHandler) Execute(_ context.Context, _ *tools.ToolEnv, _ map[string]any) (*tools.ToolResult, error) {
	return &tools.ToolResult{Output: "MOCK_REGISTRY_READ_FILE_RESULT"}, nil
}

// TestDualDispatch_RegistryWins verifies that when a handlerRegistry is set and
// contains a handler for the requested tool, the registry dispatch path is taken
// instead of the legacy dispatch path. This is the core SP-038-2b contract.
func TestDualDispatch_RegistryWins(t *testing.T) {
	// 1. Create a mock handler and register it
	reg := tools.NewToolRegistry()
	reg.Register(&mockReadFileHandler{})

	// 2. Create a minimal agent with the fields needed by executeSingleToolWithIndex
	agent := &Agent{
		mcpSub:       NewAgentMCPManager(),
		interruptCtx: context.Background(),
		output:       NewAgentOutputManager(),
		security:     NewAgentSecurityManager(),
	}
	agent.output.SetOutputMutex(&sync.Mutex{})

	tmpDir := t.TempDir()
	agent.SetWorkspaceRoot(tmpDir)

	// 3. Create executor and inject the registry
	executor := NewToolExecutor(agent)
	executor.SetHandlerRegistry(reg)

	// 4. Create a tool call for "read_file" with a path that definitely
	// doesn't exist — if legacy dispatch were used, it would error.
	tc := api.ToolCall{ID: "call_registry_wins", Type: "function"}
	tc.Function.Name = "read_file"
	tc.Function.Arguments = `{"path":"/tmp/nonexistent_test_file.txt"}`

	// 5. Execute
	msg := executor.executeSingleToolWithIndex(tc, 0)

	// 6. Assert: registry handler output should be present (proves registry wins)
	if !strings.Contains(msg.Content, "MOCK_REGISTRY_READ_FILE_RESULT") {
		t.Fatalf("expected registry dispatch to produce mock output, got: %q", msg.Content)
	}
}
