package agent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// notifierTestStateManager — test state manager that tracks drift rejection
// count in addition to session intent embedding.
//
// Embeds mockStateManager for all no-op methods; overrides only the methods
// needed by DriftNotifier and CheckDrift.
// ---------------------------------------------------------------------------

type notifierTestStateManager struct {
	mockStateManager
	sessionIntentEmbedding []float32
	driftRejectionCount    int
}

func (m *notifierTestStateManager) GetSessionIntentEmbedding() []float32 {
	return m.sessionIntentEmbedding
}

func (m *notifierTestStateManager) SetSessionIntentEmbedding(emb []float32) {
	m.sessionIntentEmbedding = emb
}

func (m *notifierTestStateManager) SetSessionIntentEmbeddingIfNil(emb []float32) bool {
	if m.sessionIntentEmbedding == nil {
		m.sessionIntentEmbedding = emb
		return true
	}
	return false
}

func (m *notifierTestStateManager) GetDriftRejectionCount() int {
	return m.driftRejectionCount
}

func (m *notifierTestStateManager) IncrementDriftRejectionCount() {
	m.driftRejectionCount++
}

func (m *notifierTestStateManager) ResetDriftRejectionCount() {
	m.driftRejectionCount = 0
}

// ---------------------------------------------------------------------------
// setupNotifierManager — creates an EmbeddingManager in a temp dir,
// initialized. Returns the manager and a cleanup function.
//
// NOTE: Uses t.Setenv, so cannot be called in t.Parallel() tests (Go 1.25).
// ---------------------------------------------------------------------------

func setupNotifierManager(t *testing.T) (*embedding.EmbeddingManager, func()) {
	t.Helper()
	ctx := context.Background()
	tempDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("LEDIT_CONFIG", tempDir)

	cfg := &configuration.EmbeddingIndexConfig{IndexDir: tempDir}
	mgr := embedding.NewEmbeddingManager(cfg, tempDir)

	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("failed to init embedding manager: %v", err)
	}

	return mgr, func() { mgr.Close() }
}

// ---------------------------------------------------------------------------
// newTestAgentWithNotifier — creates a minimal Agent wired for notifier
// tests. Allows injecting a custom state manager and optionally an event bus.
// ---------------------------------------------------------------------------

func newTestAgentWithNotifier(t *testing.T, stateMgr StateManager, eventBus *events.EventBus) *Agent {
	t.Helper()
	a := &Agent{}
	a.initSubManagers()
	a.state = stateMgr
	if eventBus != nil {
		a.eventBus = eventBus
	}
	return a
}

// ---------------------------------------------------------------------------
// TestCheckAndNotify_NilAgent
//
// When the notifier's agent field is nil, CheckAndNotify returns false
// without panic or side effects.
// ---------------------------------------------------------------------------

func TestCheckAndNotify_NilAgent(t *testing.T) {
	t.Parallel()

	notifier := &DriftNotifier{agent: nil}

	result := notifier.CheckAndNotify(context.Background(), "test prompt", 5)
	assert.False(t, result, "should return false when agent is nil")
}

// ---------------------------------------------------------------------------
// TestCheckAndNotify_NilEventBus_NoDrift
//
// When there is no event bus and CheckDrift returns nil (e.g., no embeddings),
// CheckAndNotify returns false and does not panic.
// ---------------------------------------------------------------------------

func TestCheckAndNotify_NilEventBus_NoDrift(t *testing.T) {
	t.Parallel()

	stateMgr := &notifierTestStateManager{}
	a := newTestAgentWithNotifier(t, stateMgr, nil) // no event bus
	a.embeddingMgr = nil // no embedding manager → CheckDrift returns nil

	notifier := newDriftNotifier(a)

	result := notifier.CheckAndNotify(context.Background(), "test prompt", 5)
	assert.False(t, result, "should return false when CheckDrift returns nil")
}

// ---------------------------------------------------------------------------
// TestCheckAndNotify_NoDrift
//
// When CheckDrift returns a result with Drifted=false, CheckAndNotify
// returns false and does not publish any event.
// ---------------------------------------------------------------------------

func TestCheckAndNotify_NoDrift(t *testing.T) {
	ctx := context.Background()
	mgr, cleanup := setupNotifierManager(t)
	defer cleanup()

	store, err := mgr.GetConversationStore(ctx)
	assert.NoError(t, err)
	provider := store.Provider()
	assert.NotNil(t, provider)

	// Same prompt → identical embeddings → no drift
	intentEmb, err := provider.Embed(ctx, "How do I implement a REST API in Go?")
	assert.NoError(t, err)

	stateMgr := &notifierTestStateManager{
		sessionIntentEmbedding: intentEmb,
	}

	eventBus := events.NewEventBus()
	a := newTestAgentWithNotifier(t, stateMgr, eventBus)
	a.embeddingMgr = mgr

	// Subscribe before the call to ensure we capture any events
	sub := eventBus.Subscribe("test-drift-no-drift")

	notifier := newDriftNotifier(a)
	result := notifier.CheckAndNotify(ctx, "How do I implement a REST API in Go?", 5)

	// Should not detect drift
	assert.False(t, result, "should return false when no drift is detected")

	// DriftDetected flag should NOT be set
	assert.False(t, a.GetAndClearDriftDetected(), "drift flag should not be set")

	// No event should be published
	select {
	case <-sub:
		t.Error("expected no event to be published when drift is not detected")
	case <-time.After(100 * time.Millisecond):
		// OK — no event received
	}
}

// ---------------------------------------------------------------------------
// TestCheckAndNotify_DriftDetected
//
// When CheckDrift detects drift, CheckAndNotify:
//   - returns true
//   - sets the DriftDetected flag on the agent
//   - publishes a drift_detected event to the event bus
// ---------------------------------------------------------------------------

func TestCheckAndNotify_DriftDetected(t *testing.T) {
	ctx := context.Background()
	mgr, cleanup := setupNotifierManager(t)
	defer cleanup()

	store, err := mgr.GetConversationStore(ctx)
	assert.NoError(t, err)
	provider := store.Provider()
	assert.NotNil(t, provider)

	// Get embedding for the prompt
	intentEmb, err := provider.Embed(ctx, "How do I implement a REST API in Go?")
	assert.NoError(t, err)

	// Create opposed embedding (negated) to guarantee drift
	opposedEmb := make([]float32, len(intentEmb))
	for i, v := range intentEmb {
		opposedEmb[i] = -v
	}

	stateMgr := &notifierTestStateManager{
		sessionIntentEmbedding: opposedEmb,
	}

	eventBus := events.NewEventBus()
	a := newTestAgentWithNotifier(t, stateMgr, eventBus)
	a.embeddingMgr = mgr

	// Subscribe to capture the drift event
	sub := eventBus.Subscribe("test-drift-detected")

	// Clear any pre-existing flag state
	_ = a.GetAndClearDriftDetected()

	notifier := newDriftNotifier(a)
	result := notifier.CheckAndNotify(ctx, "How do I implement a REST API in Go?", 5)

	// Should detect drift
	assert.True(t, result, "should return true when drift is detected")

	// DriftDetected flag should be set
	assert.True(t, a.GetAndClearDriftDetected(), "drift flag should be set after drift detection")

	// Should have published a drift_detected event
	select {
	case event := <-sub:
		assert.Equal(t, events.EventTypeDriftDetected, event.Type, "should publish drift_detected event")

		data, ok := event.Data.(map[string]interface{})
		assert.True(t, ok, "event data should be a map")
		assert.Contains(t, data, "similarity", "should include similarity")
		assert.Contains(t, data, "threshold", "should include threshold")
		assert.Contains(t, data, "turn_number", "should include turn_number")

	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected drift_detected event to be published")
	}
}

// ---------------------------------------------------------------------------
// TestCheckAndNotify_DriftDetected_NoEventBus
//
// When drift is detected but there is no event bus, the function should still
// return true and set the flag, but not panic on the nil event bus.
// ---------------------------------------------------------------------------

func TestCheckAndNotify_DriftDetected_NoEventBus(t *testing.T) {
	ctx := context.Background()
	mgr, cleanup := setupNotifierManager(t)
	defer cleanup()

	store, err := mgr.GetConversationStore(ctx)
	assert.NoError(t, err)
	provider := store.Provider()
	assert.NotNil(t, provider)

	intentEmb, err := provider.Embed(ctx, "How do I implement a REST API in Go?")
	assert.NoError(t, err)

	// Create opposed embedding
	opposedEmb := make([]float32, len(intentEmb))
	for i, v := range intentEmb {
		opposedEmb[i] = -v
	}

	stateMgr := &notifierTestStateManager{
		sessionIntentEmbedding: opposedEmb,
	}

	// No event bus — nil
	a := newTestAgentWithNotifier(t, stateMgr, nil)
	a.embeddingMgr = mgr

	_ = a.GetAndClearDriftDetected()

	notifier := newDriftNotifier(a)

	// Should not panic and should detect drift
	result := notifier.CheckAndNotify(ctx, "How do I implement a REST API in Go?", 5)
	assert.True(t, result, "should return true when drift is detected even without event bus")
	assert.True(t, a.GetAndClearDriftDetected(), "drift flag should still be set")
}

// ---------------------------------------------------------------------------
// TestCheckAndNotify_Suppressed
//
// After maxDriftRejections (3) consecutive rejections, CheckAndNotify
// returns false silently without calling CheckDrift at all.
// ---------------------------------------------------------------------------

func TestCheckAndNotify_Suppressed(t *testing.T) {
	t.Parallel()

	stateMgr := &notifierTestStateManager{
		driftRejectionCount: maxDriftRejections, // 3 rejections
	}

	a := newTestAgentWithNotifier(t, stateMgr, nil)
	a.embeddingMgr = nil // doesn't matter — should short-circuit

	notifier := newDriftNotifier(a)

	result := notifier.CheckAndNotify(context.Background(), "test prompt", 5)
	assert.False(t, result, "should return false when suppressed (3+ rejections)")
}

// ---------------------------------------------------------------------------
// TestCheckAndNotify_NotSuppressed_At_2_Rejections
//
// With exactly 2 rejections (below the threshold of 3), CheckAndNotify
// should still proceed. Here we verify it's not suppressed prematurely.
// The result depends on CheckDrift (returns false because embeddingMgr is nil).
// ---------------------------------------------------------------------------

func TestCheckAndNotify_NotSuppressed_At_2_Rejections(t *testing.T) {
	t.Parallel()

	stateMgr := &notifierTestStateManager{
		driftRejectionCount: 2,
	}

	a := newTestAgentWithNotifier(t, stateMgr, nil)
	a.embeddingMgr = nil // CheckDrift will return nil

	notifier := newDriftNotifier(a)

	result := notifier.CheckAndNotify(context.Background(), "test prompt", 5)
	// Returns false because CheckDrift returns nil (no embedding manager),
	// NOT because it's suppressed
	assert.False(t, result)
}

// ---------------------------------------------------------------------------
// TestCheckAndNotify_CheckDriftError
//
// When CheckDrift returns an error, CheckAndNotify returns false without
// publishing events or setting flags.
// ---------------------------------------------------------------------------

func TestCheckAndNotify_CheckDriftError(t *testing.T) {
	t.Parallel()

	// nil embedding manager → CheckDrift logs error and returns nil, nil
	stateMgr := &notifierTestStateManager{}

	a := newTestAgentWithNotifier(t, stateMgr, events.NewEventBus())
	a.embeddingMgr = nil

	notifier := newDriftNotifier(a)
	result := notifier.CheckAndNotify(context.Background(), "test prompt", 5)
	assert.False(t, result)
	assert.False(t, a.GetAndClearDriftDetected(), "flag should not be set on error")
}

// ---------------------------------------------------------------------------
// TestRecordUserResponse_Continue
//
// Recording a "continue" response (startedNewChat=false) should increment
// the drift rejection count.
// ---------------------------------------------------------------------------

func TestRecordUserResponse_Continue(t *testing.T) {
	t.Parallel()

	stateMgr := &notifierTestStateManager{
		driftRejectionCount: 0,
	}

	a := newTestAgentWithNotifier(t, stateMgr, nil)
	notifier := newDriftNotifier(a)

	notifier.RecordUserResponse(false) // user chose to continue

	assert.Equal(t, 1, stateMgr.GetDriftRejectionCount(), "rejection count should be incremented")
}

// ---------------------------------------------------------------------------
// TestRecordUserResponse_NewChat
//
// Recording a "new chat" response (startedNewChat=true) should reset
// the drift rejection count to zero.
// ---------------------------------------------------------------------------

func TestRecordUserResponse_NewChat(t *testing.T) {
	t.Parallel()

	stateMgr := &notifierTestStateManager{
		driftRejectionCount: 5, // simulate multiple prior rejections
	}

	a := newTestAgentWithNotifier(t, stateMgr, nil)
	notifier := newDriftNotifier(a)

	notifier.RecordUserResponse(true) // user chose new chat

	assert.Equal(t, 0, stateMgr.GetDriftRejectionCount(), "rejection count should be reset to zero")
}

// ---------------------------------------------------------------------------
// TestRecordUserResponse_NilAgent
//
// When the notifier has no agent, RecordUserResponse should not panic.
// ---------------------------------------------------------------------------

func TestRecordUserResponse_NilAgent(t *testing.T) {
	t.Parallel()

	notifier := &DriftNotifier{agent: nil}

	// Should not panic
	notifier.RecordUserResponse(false)
	notifier.RecordUserResponse(true)
}

// ---------------------------------------------------------------------------
// TestShouldSuppressDrift
//
// Suppression should kick in at exactly maxDriftRejections (3) and remain
// true for any count above that threshold.
// ---------------------------------------------------------------------------

func TestShouldSuppressDrift(t *testing.T) {
	t.Parallel()

	tests := []struct {
		count int
		suppress bool
	}{
		{0, false},
		{1, false},
		{2, false},
		{3, true},   // exactly at threshold
		{4, true},   // above threshold
		{100, true}, // well above
	}

	for _, tt := range tests {
		tt := tt // capture range var
		t.Run("", func(t *testing.T) {
			t.Parallel()

			stateMgr := &notifierTestStateManager{
				driftRejectionCount: tt.count,
			}

			a := newTestAgentWithNotifier(t, stateMgr, nil)
			notifier := newDriftNotifier(a)

			got := notifier.ShouldSuppressDrift()
			assert.Equal(t, tt.suppress, got,
				"ShouldSuppressDrift with count=%d", tt.count)
		})
	}
}

// ---------------------------------------------------------------------------
// TestShouldSuppressDrift_NilAgent
//
// When the agent is nil, ShouldSuppressDrift should return true
// (conservative: suppress if we can't check).
// ---------------------------------------------------------------------------

func TestShouldSuppressDrift_NilAgent(t *testing.T) {
	t.Parallel()

	notifier := &DriftNotifier{agent: nil}

	assert.True(t, notifier.ShouldSuppressDrift(), "should suppress when agent is nil")
}

// ---------------------------------------------------------------------------
// TestCheckDriftAsync_NonBlocking
//
// checkDriftAsync runs in a goroutine and must never block the caller.
// Even with a nil embedding manager (which causes CheckDrift to return nil),
// the call should complete immediately.
// ---------------------------------------------------------------------------

func TestCheckDriftAsync_NonBlocking(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.state = &notifierTestStateManager{}
	a.embeddingMgr = nil // no-ops gracefully

	// Should return immediately
	done := make(chan struct{})
	go func() {
		a.checkDriftAsync("test prompt", 5)
		close(done)
	}()

	select {
	case <-done:
		// OK — completed immediately
	case <-time.After(500 * time.Millisecond):
		t.Fatal("checkDriftAsync should not block the caller")
	}
}

// ---------------------------------------------------------------------------
// TestCheckDriftAsync_NilAgent
//
// When called on a nil agent, checkDriftAsync should not panic.
// ---------------------------------------------------------------------------

func TestCheckDriftAsync_NilAgent(t *testing.T) {
	t.Parallel()

	// Should not panic
	var a *Agent
	a.checkDriftAsync("test prompt", 5)
}

// ---------------------------------------------------------------------------
// TestCheckDriftAsync_NilState
//
// When the agent's state is nil, checkDriftAsync should not panic.
// ---------------------------------------------------------------------------

func TestCheckDriftAsync_NilState(t *testing.T) {
	t.Parallel()

	a := &Agent{}
	// state is nil

	// Should not panic
	a.checkDriftAsync("test prompt", 5)
}

// ---------------------------------------------------------------------------
// TestCheckDriftAsync_Suppressed
//
// When drift is suppressed (3+ rejections), the async goroutine should
// return immediately without spawning a new goroutine.
// ---------------------------------------------------------------------------

func TestCheckDriftAsync_Suppressed(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	stateMgr := &notifierTestStateManager{
		driftRejectionCount: maxDriftRejections,
	}
	a.state = stateMgr
	a.embeddingMgr = nil

	// Should return immediately (suppressed, no goroutine spawned)
	done := make(chan struct{})
	go func() {
		a.checkDriftAsync("test prompt", 5)
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(500 * time.Millisecond):
		t.Fatal("checkDriftAsync should return immediately when suppressed")
	}
}

// ---------------------------------------------------------------------------
// TestCheckDriftAsync_DriftDetected_FlagSet
//
// When drift is detected in the async path, the drift flag should
// be set on the agent.
// ---------------------------------------------------------------------------

func TestCheckDriftAsync_DriftDetected_FlagSet(t *testing.T) {
	ctx := context.Background()
	mgr, cleanup := setupNotifierManager(t)
	defer cleanup()

	store, err := mgr.GetConversationStore(ctx)
	assert.NoError(t, err)
	provider := store.Provider()
	assert.NotNil(t, provider)

	intentEmb, err := provider.Embed(ctx, "How do I implement a REST API in Go?")
	assert.NoError(t, err)

	// Opposed embedding to trigger drift
	opposedEmb := make([]float32, len(intentEmb))
	for i, v := range intentEmb {
		opposedEmb[i] = -v
	}

	stateMgr := &notifierTestStateManager{
		sessionIntentEmbedding: opposedEmb,
	}

	a := &Agent{}
	a.initSubManagers()
	a.state = stateMgr
	a.embeddingMgr = mgr
	a.eventBus = events.NewEventBus()

	// Clear any pre-existing flag
	_ = a.GetAndClearDriftDetected()

	// Call async drift check
	a.checkDriftAsync("How do I implement a REST API in Go?", 5)

	// Wait for the goroutine to complete
	// Give it up to 3 seconds (embedding + similarity computation)
	flagSet := false
	for i := 0; i < 30; i++ {
		if a.GetAndClearDriftDetected() {
			flagSet = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	assert.True(t, flagSet, "drift flag should be set by checkDriftAsync")
}

// ---------------------------------------------------------------------------
// TestRecordDriftUserResponse_AgentAPI_NewChat
//
// The public Agent.RecordDriftUserResponse API should reset the counter
// when the user chooses a new chat.
// ---------------------------------------------------------------------------

func TestRecordDriftUserResponse_AgentAPI_NewChat(t *testing.T) {
	t.Parallel()

	stateMgr := &notifierTestStateManager{
		driftRejectionCount: 3,
	}

	a := &Agent{}
	a.initSubManagers()
	a.state = stateMgr

	a.RecordDriftUserResponse(true) // new chat

	assert.Equal(t, 0, stateMgr.GetDriftRejectionCount(), "counter should be reset on new chat")
}

// ---------------------------------------------------------------------------
// TestRecordDriftUserResponse_AgentAPI_Continue
//
// The public Agent.RecordDriftUserResponse API should increment the counter
// when the user chooses to continue.
// ---------------------------------------------------------------------------

func TestRecordDriftUserResponse_AgentAPI_Continue(t *testing.T) {
	t.Parallel()

	stateMgr := &notifierTestStateManager{
		driftRejectionCount: 1,
	}

	a := &Agent{}
	a.initSubManagers()
	a.state = stateMgr

	a.RecordDriftUserResponse(false) // continue

	assert.Equal(t, 2, stateMgr.GetDriftRejectionCount(), "counter should be incremented on continue")
}

// ---------------------------------------------------------------------------
// TestRecordDriftUserResponse_AgentAPI_NilAgent
// ---------------------------------------------------------------------------

func TestRecordDriftUserResponse_AgentAPI_NilAgent(t *testing.T) {
	t.Parallel()

	// Should not panic
	var a *Agent
	a.RecordDriftUserResponse(true)
	a.RecordDriftUserResponse(false)
}

// ---------------------------------------------------------------------------
// TestShouldSuppressDriftDetection_AgentAPI
//
// The public Agent.ShouldSuppressDriftDetection API should return true
// when rejection count reaches the threshold.
// ---------------------------------------------------------------------------

func TestShouldSuppressDriftDetection_AgentAPI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		count    int
		suppress bool
	}{
		{0, false},
		{2, false},
		{3, true},
		{5, true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run("", func(t *testing.T) {
			t.Parallel()

			stateMgr := &notifierTestStateManager{
				driftRejectionCount: tt.count,
			}

			a := &Agent{}
			a.initSubManagers()
			a.state = stateMgr

			assert.Equal(t, tt.suppress, a.ShouldSuppressDriftDetection(),
				"ShouldSuppressDriftDetection with count=%d", tt.count)
		})
	}
}

// ---------------------------------------------------------------------------
// TestShouldSuppressDriftDetection_AgentAPI_NilAgent
// ---------------------------------------------------------------------------

func TestShouldSuppressDriftDetection_AgentAPI_NilAgent(t *testing.T) {
	t.Parallel()

	var a *Agent
	assert.True(t, a.ShouldSuppressDriftDetection(), "should suppress when agent is nil")
}

// ---------------------------------------------------------------------------
// TestShouldSuppressDriftDetection_AgentAPI_NilState
// ---------------------------------------------------------------------------

func TestShouldSuppressDriftDetection_AgentAPI_NilState(t *testing.T) {
	t.Parallel()

	a := &Agent{}
	assert.True(t, a.ShouldSuppressDriftDetection(), "should suppress when state is nil")
}

// ---------------------------------------------------------------------------
// TestGetDriftRejectionCount_AgentAPI
// ---------------------------------------------------------------------------

func TestGetDriftRejectionCount_AgentAPI(t *testing.T) {
	t.Parallel()

	stateMgr := &notifierTestStateManager{
		driftRejectionCount: 42,
	}

	a := &Agent{}
	a.initSubManagers()
	a.state = stateMgr

	assert.Equal(t, 42, a.GetDriftRejectionCount())
}

// ---------------------------------------------------------------------------
// TestGetDriftRejectionCount_AgentAPI_NilAgent
// ---------------------------------------------------------------------------

func TestGetDriftRejectionCount_AgentAPI_NilAgent(t *testing.T) {
	t.Parallel()

	var a *Agent
	assert.Equal(t, 0, a.GetDriftRejectionCount())
}

// ---------------------------------------------------------------------------
// TestGetDriftRejectionCount_AgentAPI_NilState
// ---------------------------------------------------------------------------

func TestGetDriftRejectionCount_AgentAPI_NilState(t *testing.T) {
	t.Parallel()

	a := &Agent{}
	assert.Equal(t, 0, a.GetDriftRejectionCount())
}

// ---------------------------------------------------------------------------
// TestDriftNotifierIntegration_SuppressionAfterRejections
//
// End-to-end: detect drift → user continues (rejects) 3 times → suppression
// kicks in → subsequent drift checks are silently ignored.
// ---------------------------------------------------------------------------

func TestDriftNotifierIntegration_SuppressionAfterRejections(t *testing.T) {
	ctx := context.Background()
	mgr, cleanup := setupNotifierManager(t)
	defer cleanup()

	store, err := mgr.GetConversationStore(ctx)
	assert.NoError(t, err)
	provider := store.Provider()
	assert.NotNil(t, provider)

	intentEmb, err := provider.Embed(ctx, "How do I implement a REST API in Go?")
	assert.NoError(t, err)

	// Opposed embedding to trigger drift every time
	opposedEmb := make([]float32, len(intentEmb))
	for i, v := range intentEmb {
		opposedEmb[i] = -v
	}

	stateMgr := &notifierTestStateManager{
		sessionIntentEmbedding: opposedEmb,
	}

	eventBus := events.NewEventBus()
	a := newTestAgentWithNotifier(t, stateMgr, eventBus)
	a.embeddingMgr = mgr

	notifier := newDriftNotifier(a)

	// First drift detection — should succeed
	// Note: CheckAndNotify does NOT increment rejection count itself;
	// that happens when the user responds. So we need to simulate
	// the full cycle: detect → respond(continue) → detect → respond...

	// Cycle 1: detect
	assert.True(t, notifier.CheckAndNotify(ctx, "How do I implement a REST API in Go?", 5))
	a.GetAndClearDriftDetected() // clear for next

	// Cycle 1: user responds "continue"
	notifier.RecordUserResponse(false)
	assert.Equal(t, 1, stateMgr.GetDriftRejectionCount())

	// Cycle 2: detect
	assert.True(t, notifier.CheckAndNotify(ctx, "How do I implement a REST API in Go?", 10))
	a.GetAndClearDriftDetected()

	// Cycle 2: user responds "continue"
	notifier.RecordUserResponse(false)
	assert.Equal(t, 2, stateMgr.GetDriftRejectionCount())

	// Cycle 3: detect
	assert.True(t, notifier.CheckAndNotify(ctx, "How do I implement a REST API in Go?", 15))
	a.GetAndClearDriftDetected()

	// Cycle 3: user responds "continue"
	notifier.RecordUserResponse(false)
	assert.Equal(t, 3, stateMgr.GetDriftRejectionCount())

	// Now suppression should kick in
	assert.True(t, notifier.ShouldSuppressDrift(), "should suppress after 3 rejections")

	// Next detection should be suppressed
	assert.False(t, notifier.CheckAndNotify(ctx, "How do I implement a REST API in Go?", 20),
		"should return false when suppressed")
	assert.False(t, a.GetAndClearDriftDetected(), "flag should not be set when suppressed")

	// New chat should reset suppression
	notifier.RecordUserResponse(true)
	assert.Equal(t, 0, stateMgr.GetDriftRejectionCount(), "counter should reset on new chat")
	assert.False(t, notifier.ShouldSuppressDrift(), "should not suppress after reset")

	// Detection should work again
	assert.True(t, notifier.CheckAndNotify(ctx, "How do I implement a REST API in Go?", 25),
		"should detect again after suppression reset")
}

// ---------------------------------------------------------------------------
// TestDriftNotifierIntegration_SuppressionBoundary_At_2_Rejections
//
// With exactly 2 rejections, detection should still proceed (not suppressed).
// ---------------------------------------------------------------------------

func TestDriftNotifierIntegration_SuppressionBoundary_At_2_Rejections(t *testing.T) {
	t.Parallel()

	stateMgr := &notifierTestStateManager{
		driftRejectionCount: 2,
	}

	a := newTestAgentWithNotifier(t, stateMgr, nil)
	a.embeddingMgr = nil // CheckDrift returns nil

	notifier := newDriftNotifier(a)

	assert.False(t, notifier.ShouldSuppressDrift(), "should not suppress with 2 rejections")

	// Even though detection won't fire (no embedding mgr), it should
	// NOT be suppressed — the false return is from CheckDrift, not suppression
	notifier.CheckAndNotify(context.Background(), "test", 5)
}

// ---------------------------------------------------------------------------
// TestDriftNotifierIntegration_NewChatResetsSuppression
//
// After suppression kicks in, choosing "new chat" should fully reset
// and allow detection to resume.
// ---------------------------------------------------------------------------

func TestDriftNotifierIntegration_NewChatResetsSuppression(t *testing.T) {
	t.Parallel()

	stateMgr := &notifierTestStateManager{
		driftRejectionCount: maxDriftRejections + 5, // way over threshold
	}

	a := newTestAgentWithNotifier(t, stateMgr, nil)
	notifier := newDriftNotifier(a)

	assert.True(t, notifier.ShouldSuppressDrift(), "should be suppressed")

	// Record new chat — resets counter
	notifier.RecordUserResponse(true)

	assert.Equal(t, 0, stateMgr.GetDriftRejectionCount(), "counter should be zero")
	assert.False(t, notifier.ShouldSuppressDrift(), "should not be suppressed after new chat")
}

// ---------------------------------------------------------------------------
// TestCheckAndNotify_EventBusReceivesCorrectPayload
//
// Verifies that the event published to the event bus contains the correct
// similarity, threshold, and turn_number fields with proper types.
// ---------------------------------------------------------------------------

func TestCheckAndNotify_EventBusReceivesCorrectPayload(t *testing.T) {
	ctx := context.Background()
	mgr, cleanup := setupNotifierManager(t)
	defer cleanup()

	store, err := mgr.GetConversationStore(ctx)
	assert.NoError(t, err)
	provider := store.Provider()
	assert.NotNil(t, provider)

	// Use a prompt that's similar but not identical — we'll use a
	// partial match to get a similarity between 0 and 1
	intentEmb, err := provider.Embed(ctx, "How do I implement a REST API in Go?")
	assert.NoError(t, err)

	stateMgr := &notifierTestStateManager{
		sessionIntentEmbedding: intentEmb, // placeholder; overridden below
	}

	eventBus := events.NewEventBus()
	a := newTestAgentWithNotifier(t, stateMgr, eventBus)
	a.embeddingMgr = mgr

	// We can't override the config in CheckAndNotify (it uses DefaultDriftConfig),
	// so we'll use the opposed approach instead for reliable drift detection.

	// Override state to use fully opposed embedding
	stateMgr.sessionIntentEmbedding = make([]float32, len(intentEmb))
	for i, v := range intentEmb {
		stateMgr.sessionIntentEmbedding[i] = -v
	}

	sub := eventBus.Subscribe("test-payload")

	_ = a.GetAndClearDriftDetected()

	notifier := newDriftNotifier(a)
	result := notifier.CheckAndNotify(ctx, "How do I implement a REST API in Go?", 5)

	assert.True(t, result, "should detect drift")

	select {
	case event := <-sub:
		assert.Equal(t, events.EventTypeDriftDetected, event.Type)

		data, ok := event.Data.(map[string]interface{})
		assert.True(t, ok, "event data should be a map")

		// Verify similarity is present and is a valid number
		sim, ok := data["similarity"].(float64)
		assert.True(t, ok, "similarity should be a float64")
		assert.GreaterOrEqual(t, sim, -1.0, "similarity should be >= -1.0")
		assert.LessOrEqual(t, sim, 1.0, "similarity should be <= 1.0")

		// Verify threshold matches default (use InDelta for float32→float64 conversion tolerance)
		threshold, ok := data["threshold"].(float64)
		assert.True(t, ok, "threshold should be a float64")
		assert.InDelta(t, 0.60, threshold, 0.001, "threshold should match default config")

		// Verify turn_number matches
		turnNum, ok := data["turn_number"].(int)
		assert.True(t, ok, "turn_number should be an int")
		assert.Equal(t, 5, turnNum, "turn_number should match the call")

		// Verify similarity is below threshold (drift condition)
		assert.Less(t, sim, threshold, "similarity should be below threshold for drift")

	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected drift_detected event")
	}
}

// ---------------------------------------------------------------------------
// TestCheckAndNotify_DriftFlagAtomicSafety
//
// Verifies that SetDriftDetected and GetAndClearDriftDetected work
// correctly in the notifier flow.
// ---------------------------------------------------------------------------

func TestCheckAndNotify_DriftFlagAtomicSafety(t *testing.T) {
	t.Parallel()

	a := &Agent{}
	a.initSubManagers()

	// Initially false
	assert.False(t, a.GetAndClearDriftDetected(), "initial flag should be false")

	// Set it
	a.SetDriftDetected()

	// Should be true
	assert.True(t, a.GetAndClearDriftDetected(), "flag should be true after SetDriftDetected")

	// After clearing, should be false again
	assert.False(t, a.GetAndClearDriftDetected(), "flag should be false after clearing")

	// Multiple clears should all return false
	assert.False(t, a.GetAndClearDriftDetected(), "second clear should return false")
	assert.False(t, a.GetAndClearDriftDetected(), "third clear should return false")

	// Set again and verify
	a.SetDriftDetected()
	a.SetDriftDetected() // double-set should be fine
	assert.True(t, a.GetAndClearDriftDetected(), "flag should be true after re-set")
}

// ---------------------------------------------------------------------------
// TestCheckAndNotify_TurnNumberZero
//
// When turn number is 0 (or negative), CheckDrift returns nil (skipped).
// CheckAndNotify should handle this gracefully and return false.
// ---------------------------------------------------------------------------

func TestCheckAndNotify_TurnNumberZero(t *testing.T) {
	ctx := context.Background()
	mgr, cleanup := setupNotifierManager(t)
	defer cleanup()

	store, err := mgr.GetConversationStore(ctx)
	assert.NoError(t, err)
	provider := store.Provider()
	assert.NotNil(t, provider)

	intentEmb, err := provider.Embed(ctx, "test")
	assert.NoError(t, err)

	stateMgr := &notifierTestStateManager{
		sessionIntentEmbedding: intentEmb,
	}

	a := newTestAgentWithNotifier(t, stateMgr, events.NewEventBus())
	a.embeddingMgr = mgr

	notifier := newDriftNotifier(a)

	// Turn 0 should be skipped
	result := notifier.CheckAndNotify(ctx, "test", 0)
	assert.False(t, result, "should return false for turn 0")
	assert.False(t, a.GetAndClearDriftDetected(), "flag should not be set")

	// Turn -1 should also be skipped
	result = notifier.CheckAndNotify(ctx, "test", -1)
	assert.False(t, result, "should return false for negative turn")
}

// ---------------------------------------------------------------------------
// TestCheckAndNotify_ContextCancellation
//
// When the context is cancelled, CheckDrift should gracefully return nil
// and CheckAndNotify should return false.
// ---------------------------------------------------------------------------

func TestCheckAndNotify_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before calling

	mgr, cleanup := setupNotifierManager(t)
	defer cleanup()

	// Use dummy embedding with wrong dimensionality so even if context
	// cancellation is handled, the dimension check may also trigger
	stateMgr := &notifierTestStateManager{
		sessionIntentEmbedding: []float32{1.0, 0.0},
	}

	a := newTestAgentWithNotifier(t, stateMgr, events.NewEventBus())
	a.embeddingMgr = mgr

	notifier := newDriftNotifier(a)

	result := notifier.CheckAndNotify(ctx, "test prompt", 5)
	assert.False(t, result, "should return false on cancelled context")
	assert.False(t, a.GetAndClearDriftDetected(), "flag should not be set")
}

// ---------------------------------------------------------------------------
// TestCheckAndNotify_RaceSafety
//
// Verifies that concurrent CheckAndNotify calls don't cause data races.
// The atomic drift flag and event bus should handle concurrent access.
// ---------------------------------------------------------------------------

func TestCheckAndNotify_RaceSafety(t *testing.T) {
	// NOTE: Cannot use t.Parallel() — setupNotifierManager uses t.Setenv
	// which is incompatible with parallel tests in Go 1.25.

	ctx := context.Background()
	mgr, cleanup := setupNotifierManager(t)
	defer cleanup()

	store, err := mgr.GetConversationStore(ctx)
	assert.NoError(t, err)
	provider := store.Provider()
	assert.NotNil(t, provider)

	intentEmb, err := provider.Embed(ctx, "test")
	assert.NoError(t, err)

	stateMgr := &notifierTestStateManager{
		sessionIntentEmbedding: intentEmb,
	}

	eventBus := events.NewEventBus()
	a := newTestAgentWithNotifier(t, stateMgr, eventBus)
	a.embeddingMgr = mgr

	notifier := newDriftNotifier(a)

	// Run multiple concurrent checks
	var wg atomic.Int32
	wg.Store(10)

	for i := 0; i < 10; i++ {
		go func(turn int) {
			defer wg.Add(-1)
			notifier.CheckAndNotify(ctx, "test prompt", turn)
		}((i+1) * 5) // use interval turns (5, 10, 15, ...)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 100 && wg.Load() != 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}

	assert.Equal(t, int32(0), wg.Load(), "all goroutines should have completed")
}

// ---------------------------------------------------------------------------
// TestCheckAndNotify_ClearedFlagAfterDrift
//
// After drift is detected and the flag is cleared via GetAndClearDriftDetected,
// subsequent calls that detect drift again should set the flag fresh.
// ---------------------------------------------------------------------------

func TestCheckAndNotify_ClearedFlagAfterDrift(t *testing.T) {
	ctx := context.Background()
	mgr, cleanup := setupNotifierManager(t)
	defer cleanup()

	store, err := mgr.GetConversationStore(ctx)
	assert.NoError(t, err)
	provider := store.Provider()
	assert.NotNil(t, provider)

	intentEmb, err := provider.Embed(ctx, "How do I implement a REST API in Go?")
	assert.NoError(t, err)

	opposedEmb := make([]float32, len(intentEmb))
	for i, v := range intentEmb {
		opposedEmb[i] = -v
	}

	stateMgr := &notifierTestStateManager{
		sessionIntentEmbedding: opposedEmb,
	}

	eventBus := events.NewEventBus()
	a := newTestAgentWithNotifier(t, stateMgr, eventBus)
	a.embeddingMgr = mgr

	notifier := newDriftNotifier(a)

	// First drift detection
	assert.True(t, notifier.CheckAndNotify(ctx, "How do I implement a REST API in Go?", 5))
	assert.True(t, a.GetAndClearDriftDetected(), "flag should be set")
	assert.False(t, a.GetAndClearDriftDetected(), "flag should be cleared after reading")

	// Second drift detection — flag should be set fresh
	assert.True(t, notifier.CheckAndNotify(ctx, "How do I implement a REST API in Go?", 10))
	assert.True(t, a.GetAndClearDriftDetected(), "flag should be set again on second drift")
}
