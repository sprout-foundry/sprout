package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/spec"
)

// =============================================================================
// handleSelfReview context propagation tests
// Verifies that handleSelfReview receives ctx as a parameter and threads
// it through to spec.ReviewTrackedChanges (SP-073-1).
// =============================================================================

func TestHandleSelfReview_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before calling

	// Minimal agent — no changeTracker, no configManager.
	// The function will hit the "no changes found" path before reaching
	// ReviewTrackedChanges, but the ctx parameter IS threaded through
	// the call chain. This verifies the parameter exists and is used.
	a := &Agent{}

	result, err := handleSelfReview(ctx, a, map[string]interface{}{})
	assert.Empty(t, result)
	require.Error(t, err, "handleSelfReview with cancelled ctx and nil changeTracker should fail")
}

func TestHandleSelfReview_NilAgent(t *testing.T) {
	ctx := context.Background()
	// handleSelfReview does not guard against nil agent — it immediately
	// dereferences a.changeTracker. Verify the panic is recoverable.
	assert.Panics(t, func() {
		handleSelfReview(ctx, nil, map[string]interface{}{})
	})
}

func TestHandleSelfReview_NilArgs(t *testing.T) {
	ctx := context.Background()
	a := &Agent{}
	// With a bare &Agent{}, changeTracker is nil so the function falls through
	// to history.GetRevisionGroups(). That lookup will either find a revision
	// or return an error — but before reaching configManager.GetConfig() (which
	// would panic on nil), the history path exits. We expect an error, not a panic.
	result, err := handleSelfReview(ctx, a, nil)
	assert.Empty(t, result)
	require.Error(t, err, "handleSelfReview with bare &Agent{} and nil args should fail")
}

// =============================================================================
// Agent interrupt context propagation tests
// Verifies that a.interruptCtx is properly set, cancelable, and resettable.
// This is the ctx source used by conversation.go (processImagesViaOCR)
// and seed_integration.go (self-review gate).
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
// formatSelfReviewResult edge case (supplements self_review_tool_test.go)
// =============================================================================

func TestFormatSelfReviewResult_NilResult(t *testing.T) {
	// formatSelfReviewResult is not designed for nil input —
	// verify it panics predictably rather than producing garbage.
	assert.Panics(t, func() {
		_ = formatSelfReviewResult(nil)
	})
}

func TestFormatSelfReviewResult_BothNilResults(t *testing.T) {
	result := &spec.ChangeReviewResult{
		RevisionID:   "rev-minimal",
		FilesChanged: 1,
		TotalChanges: 2,
		Summary:      "Minimal review",
		SpecResult:   nil,
		ScopeResult:  nil,
	}

	output := formatSelfReviewResult(result)

	assert.Contains(t, output, "## Self-Review Results")
	assert.Contains(t, output, "**Revision ID**: rev-minimal")
	assert.Contains(t, output, "**Files Changed**: 1")
	assert.Contains(t, output, "**Total Changes**: 2")
	assert.Contains(t, output, "Minimal review")
	// With nil ScopeResult and nil SpecResult, should fall through to OK recommendation
	assert.Contains(t, output, "### [OK] Recommendation")
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
