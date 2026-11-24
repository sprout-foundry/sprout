package core

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Handlers capture what the context exposed, then return a trivial result.
type capturedCallContext struct {
	callID   string
	toolName string
	intent   int
}

func TestToolCallMetadata_Sequential(t *testing.T) {
	var got capturedCallContext
	reg := NewToolRegistry(ToolRegistryOptions{})
	reg.Register(ToolConfig{
		Name: "ctx_probe",
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			got.callID = ToolCallIDFromContext(ctx)
			got.toolName = ToolNameFromContext(ctx)
			got.intent = ToolCallIndexFromContext(ctx)
			return "ok", nil
		},
	})

	results := reg.Execute(context.Background(), []ToolCall{
		{ID: "call-abc", Type: "function", Function: ToolCallFunction{Name: "ctx_probe", Arguments: "{}"}},
	})

	if len(results) != 1 || results[0].Content != "ok" {
		t.Fatalf("expected ok result, got: %+v", results)
	}
	if got.callID != "call-abc" {
		t.Errorf("call ID: want %q, got %q", "call-abc", got.callID)
	}
	if got.toolName != "ctx_probe" {
		t.Errorf("tool name: want %q, got %q", "ctx_probe", got.toolName)
	}
	if got.intent != 0 {
		t.Errorf("call index: want 0, got %d", got.intent)
	}
}

func TestToolCallMetadata_ParallelDistinct(t *testing.T) {
	// Two parallel-safe tools; each handler must observe its OWN call ID,
	// not a sibling's (the registry runs parallel tools on goroutines that
	// share the parent ctx).
	var mu sync.Mutex
	seen := map[string]string{} // callID -> toolName

	reg := NewToolRegistry(ToolRegistryOptions{})
	for _, name := range []string{"probe_a", "probe_b"} {
		toolName := name
		reg.Register(ToolConfig{
			Name:            toolName,
			SafeForParallel: true,
			Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
				mu.Lock()
				seen[ToolCallIDFromContext(ctx)] = ToolNameFromContext(ctx)
				mu.Unlock()
				return "ok", nil
			},
		})
	}

	reg.Execute(context.Background(), []ToolCall{
		{ID: "call-a", Type: "function", Function: ToolCallFunction{Name: "probe_a", Arguments: "{}"}},
		{ID: "call-b", Type: "function", Function: ToolCallFunction{Name: "probe_b", Arguments: "{}"}},
	})

	if seen["call-a"] != "probe_a" || seen["call-b"] != "probe_b" {
		t.Errorf("parallel handlers saw mismatched metadata: %v", seen)
	}
}

func TestToolCallMetadata_SurvivesTimeoutWrapper(t *testing.T) {
	// The timeout context wraps the metadata context; values must still be
	// readable inside the handler.
	var got string
	reg := NewToolRegistry(ToolRegistryOptions{DefaultTimeout: 5 * time.Second})
	reg.Register(ToolConfig{
		Name: "slow_probe",
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			got = ToolCallIDFromContext(ctx)
			return "ok", nil
		},
	})

	reg.Execute(context.Background(), []ToolCall{
		{ID: "call-timeout", Type: "function", Function: ToolCallFunction{Name: "slow_probe", Arguments: "{}"}},
	})

	if got != "call-timeout" {
		t.Errorf("call ID lost through timeout wrapper: got %q", got)
	}
}

func TestToolCallMetadata_EmptyContext(t *testing.T) {
	if got := ToolCallIDFromContext(context.Background()); got != "" {
		t.Errorf("expected empty call ID from bare context, got %q", got)
	}
	if got := ToolCallIDFromContext(nil); got != "" {
		t.Errorf("expected empty call ID from nil context, got %q", got)
	}
	if got := ToolNameFromContext(context.Background()); got != "" {
		t.Errorf("expected empty tool name from bare context, got %q", got)
	}
	if got := ToolCallIndexFromContext(context.Background()); got != -1 {
		t.Errorf("expected -1 call index from bare context, got %d", got)
	}
}
