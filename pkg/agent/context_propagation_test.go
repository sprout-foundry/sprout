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

	// Trigger interrupt — this cancels the context the turn was handed
	a.TriggerInterrupt()

	assert.ErrorIs(t, ctx.Err(), context.Canceled, "the handed-out context should be cancelled after TriggerInterrupt")
	assert.True(t, a.CheckForInterrupt(), "interrupt should be visible to CheckForInterrupt")

	// A NEW InterruptCtx() call returns a healed, live context. Slash
	// commands fetch InterruptCtx() after a stopped turn and must not
	// receive the dead context — that is the /compact "context canceled"
	// failure mode.
	ctx2 := a.InterruptCtx()
	require.NotNil(t, ctx2)
	assert.NoError(t, ctx2.Err(), "InterruptCtx should heal the stale-cancelled context when no query is running")
}

func TestInterruptCtx_ClearInterrupt(t *testing.T) {
	a, err := NewAgentWithModel("")
	require.NoError(t, err)

	// Initialize then cancel
	a.ClearInterrupt()
	ctx1 := a.InterruptCtx()
	a.TriggerInterrupt()

	require.NotNil(t, ctx1)
	assert.ErrorIs(t, ctx1.Err(), context.Canceled, "context should be cancelled after TriggerInterrupt")

	// ClearInterrupt should reset to a fresh, non-cancelled context
	a.ClearInterrupt()
	ctx2 := a.InterruptCtx()
	require.NotNil(t, ctx2)
	assert.NoError(t, ctx2.Err(), "context should be fresh after ClearInterrupt")
}

// TestInterruptCtx_HealsStaleCancelledContext is the regression guard for
// the /compact "context canceled" bug: after a stopped turn the stored
// interrupt context stays cancelled (reset points are the next normal
// query's resetInterruptForNewQuery and ClearInterrupt, which has no
// production callers), and slash commands that make LLM calls — /compact,
// /review, /review-deep, /shell — read InterruptCtx() directly. The call
// must heal the stale context instead of handing out a dead one.
func TestInterruptCtx_HealsStaleCancelledContext(t *testing.T) {
	a, err := NewAgentWithModel("")
	require.NoError(t, err)

	a.ClearInterrupt()
	a.TriggerInterrupt()

	require.True(t, a.CheckForInterrupt(), "interrupt should be recorded")

	// No query holds the guard: InterruptCtx heals the dead context.
	fresh := a.InterruptCtx()
	require.NotNil(t, fresh)
	assert.NoError(t, fresh.Err(), "InterruptCtx must not return a stale-cancelled context when no query is running")
}

// TestInterruptCtx_NoHealWhileQueryInProgress pins the guard behaviour: a
// running query owns the cancellation, so InterruptCtx returns the
// cancelled context as-is (in-flight calls must keep observing the
// interrupt). Once the guard releases, the same call heals.
func TestInterruptCtx_NoHealWhileQueryInProgress(t *testing.T) {
	a, err := NewAgentWithModel("")
	require.NoError(t, err)

	a.ClearInterrupt()
	a.TriggerInterrupt()

	require.NoError(t, a.TryBeginQueryAs(QuerySourceCLI))

	ctx := a.InterruptCtx()
	require.NotNil(t, ctx)
	assert.ErrorIs(t, ctx.Err(), context.Canceled, "InterruptCtx must return the cancelled context while a query holds the guard")

	a.EndQuery()
	assert.NoError(t, a.InterruptCtx().Err(), "InterruptCtx should heal once the query guard is released")
}

func TestInterruptCtx_NilAgent(t *testing.T) {
	// InterruptCtx on nil agent calls ensureLiveInterruptCtx which locks a
	// mutex — this panics. Document the behavior rather than trying to work
	// around it.
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

	// A previously handed-out snapshot still observes the cancellation,
	// but a NEW InterruptCtx() call heals to a fresh live context.
	agentCtx := a.InterruptCtx()
	require.NotNil(t, agentCtx)
	assert.NoError(t, agentCtx.Err(), "InterruptCtx should heal the stale-cancelled context when no query is running")
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
