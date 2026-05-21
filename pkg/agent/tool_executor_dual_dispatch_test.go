package agent

import (
	"context"
	"fmt"
	"testing"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

type mockDualDispatchHandler struct {
	name string
}

func (m mockDualDispatchHandler) Name() string {
	return m.name
}

func (m mockDualDispatchHandler) Definition() tools.ToolDefinition {
	return tools.ToolDefinition{
		Name:        m.name,
		Description: "Mock tool for dual-dispatch testing",
	}
}

func (m mockDualDispatchHandler) Validate(args map[string]any) error {
	return nil
}

func (m mockDualDispatchHandler) Execute(ctx context.Context, env tools.ToolEnv, args map[string]any) (tools.ToolResult, error) {
	return tools.ToolResult{
		Output: "DISPATCHED_VIA_NEW_REGISTRY",
	}, nil
}

func TestDualDispatch_RegistryWins(t *testing.T) {
	// Use a unique tool name to avoid race conditions with parallel tests
	toolName := fmt.Sprintf("mock_dual_dispatch_tool_%d", time.Now().UnixNano())

	// Register mock handler in the new registry (not in the legacy registry)
	handler := mockDualDispatchHandler{name: toolName}
	tools.GetNewToolRegistry().Register(handler)
	defer tools.GetNewToolRegistry().Unregister(toolName)

	// Use the legacy registry — ExecuteTool will dual-dispatch via new registry first
	legacyRegistry := newDefaultToolRegistry()

	images, result, err := legacyRegistry.ExecuteTool(context.Background(), toolName, nil, nil)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result != "DISPATCHED_VIA_NEW_REGISTRY" {
		t.Errorf("expected result %q, got %q", "DISPATCHED_VIA_NEW_REGISTRY", result)
	}
	if images != nil && len(images) > 0 {
		t.Errorf("expected no images, got %d", len(images))
	}
}
