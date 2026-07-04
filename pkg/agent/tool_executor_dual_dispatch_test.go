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

func (m mockDualDispatchHandler) Aliases() []string      { return nil }
func (m mockDualDispatchHandler) Timeout() time.Duration  { return 0 }
func (m mockDualDispatchHandler) MaxResultSize() int      { return 0 }
func (m mockDualDispatchHandler) SafeForParallel() bool   { return false }
func (m mockDualDispatchHandler) Interactive() bool       { return false }

func TestExecuteTool_NewRegistryUsed(t *testing.T) {
	// Use a unique tool name to avoid race conditions with parallel tests
	toolName := fmt.Sprintf("mock_dispatch_tool_%d", time.Now().UnixNano())

	// Register mock handler in the new registry
	handler := mockDualDispatchHandler{name: toolName}
	tools.GetNewToolRegistry().Register(handler)
	defer tools.GetNewToolRegistry().Unregister(toolName)

	// ExecuteTool should dispatch via the new registry
	images, result, err := ExecuteTool(context.Background(), toolName, nil, nil, "")

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
