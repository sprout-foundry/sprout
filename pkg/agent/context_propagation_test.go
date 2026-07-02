package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// =============================================================================
// Agent interrupt context propagation tests
// Verifies that a.interruptCtx is properly set, cancelable, and resettable.
// This is the ctx source used by conversation.go (processImagesViaOCR).
// =============================================================================

func TestInterruptCtx_Propagation(t *testing.T) {
	a, err := NewAgentWithModel("")
	require.NoError(t, err)

	// Fresh agent has nil interruptCtx — initialize it via ClearInterrupt
	a.ClearInterrupt()

	ctx := a.InterruptCtx()
	require.NotNil(t, ctx, "InterruptCtx after ClearInterrupt should return non-nil")
	assert.NoError(t, ctx.Err(), "fresh interruptCtx should not be cancelled")

	// Trigger interrupt — this cancels the current context
	a.TriggerInterrupt()

	ctx2 := a.InterruptCtx()
	require.NotNil(t, ctx2)
	assert.ErrorIs(t, ctx2.Err(), context.Canceled, "interruptCtx should be cancelled after TriggerInterrupt")
}

func TestInterruptCtx_ClearInterrupt(t *testing.T) {
	a, err := NewAgentWithModel("")
	require.NoError(t, err)

	// Initialize then cancel
	a.ClearInterrupt()
	a.TriggerInterrupt()

	ctx1 := a.InterruptCtx()
	require.NotNil(t, ctx1)
	assert.ErrorIs(t, ctx1.Err(), context.Canceled, "context should be cancelled after TriggerInterrupt")

	// ClearInterrupt should reset to a fresh, non-cancelled context
	a.ClearInterrupt()
	ctx2 := a.InterruptCtx()
	require.NotNil(t, ctx2)
	assert.NoError(t, ctx2.Err(), "context should be fresh after ClearInterrupt")
}

func TestInterruptCtx_NilAgent(t *testing.T) {
	// InterruptCtx on nil agent calls snapshotInterrupt which locks a mutex —
	// this panics. Document the behavior rather than trying to work around it.
	assert.Panics(t, func() {
		var a *Agent
		_ = a.InterruptCtx()
	})
}

func TestInterruptCtx_DerivedContext(t *testing.T) {
	a, err := NewAgentWithModel("")
	require.NoError(t, err)

	// Initialize the interrupt context first
	a.ClearInterrupt()

	// Snapshot the current (live) context before triggering
	parent := a.InterruptCtx()
	require.NotNil(t, parent)

	derived, cancel := context.WithCancel(parent)
	defer cancel()

	// Trigger interrupt on the agent
	a.TriggerInterrupt()

	// The derived context should also be done (parent was cancelled)
	assert.ErrorIs(t, derived.Err(), context.Canceled, "derived context should be cancelled when parent is cancelled")

	// The agent's new InterruptCtx() returns the cancelled snapshot
	agentCtx := a.InterruptCtx()
	require.NotNil(t, agentCtx)
	assert.ErrorIs(t, agentCtx.Err(), context.Canceled)
}

// =============================================================================
// Vision processor context propagation
// Verifies that ProcessImagesInText (called from conversation.go with
// a.interruptCtx) respects context cancellation.
// =============================================================================

func TestVisionProcessor_ProcessImagesInText_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before calling

	// Create a vision processor (may fail if no provider is configured)
	proc, err := tools.NewVisionProcessorWithProvider(false, "")
	if err != nil {
		// Vision provider may not be available in test env — skip the call
		t.Skip("vision provider not available")
	}

	// Should not panic with cancelled context — either returns an error
	// or returns the input unchanged.
	result, analyses, resultErr := proc.ProcessImagesInText(ctx, "plain text with no images")
	if resultErr == nil {
		// No error — should return input unchanged when there are no images
		assert.Equal(t, "plain text with no images", result)
		assert.Empty(t, analyses)
	}
	// If there IS an error, that's also fine — cancelled context may cause failure.
}
