package agent

import (
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// =============================================================================
// SP-079-1: GetVisionProcessor nil-safety tests
// =============================================================================

func TestAgent_GetVisionProcessor_ReturnsSetProcessor(t *testing.T) {
	a := NewTestAgent()

	// Manually set the vision processor to exercise the fast-path return.
	// We use the exported constructor to create a valid *VisionProcessor.
	mockProc := tools.NewVisionProcessor(nil, nil, false)
	a.visionProc = mockProc

	result := a.GetVisionProcessor()
	if result == nil {
		t.Fatal("GetVisionProcessor should return the pre-set processor")
	}
	if result != mockProc {
		t.Error("GetVisionProcessor should return the same processor instance that was set")
	}
}

func TestAgent_GetVisionProcessor_IdempotentWithPreSet(t *testing.T) {
	a := NewTestAgent()

	// Pre-set the processor, then call GetVisionProcessor multiple times.
	// The double-check pattern should always return the same cached instance
	// without triggering lazy re-initialization.
	mockProc := tools.NewVisionProcessor(nil, nil, false)
	a.visionProc = mockProc

	result1 := a.GetVisionProcessor()
	result2 := a.GetVisionProcessor()
	result3 := a.GetVisionProcessor()

	if result1 != mockProc {
		t.Error("first call should return the pre-set processor")
	}
	if result2 != mockProc {
		t.Error("second call should return the same pre-set processor")
	}
	if result3 != mockProc {
		t.Error("third call should return the same pre-set processor")
	}
}

func TestAgent_GetVisionProcessor_NilAgentSafety(t *testing.T) {
	t.Parallel()
	var a *Agent
	t.Run("nil_agent_returns_nil", func(t *testing.T) {
		got := a.GetVisionProcessor()
		if got != nil {
			t.Errorf("expected nil for nil agent, got %v", got)
		}
	})
}
