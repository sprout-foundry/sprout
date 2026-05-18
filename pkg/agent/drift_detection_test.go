package agent

import (
	"context"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// driftTestStateManager — configurable mock for drift detection tests
//
// Embeds mockStateManager for all no-op methods, but allows the caller to
// set the session intent embedding used by CheckDrift.
// ---------------------------------------------------------------------------

type driftTestStateManager struct {
	mockStateManager
	sessionIntentEmbedding []float32
}

func (m *driftTestStateManager) GetSessionIntentEmbedding() []float32 {
	return m.sessionIntentEmbedding
}

func (m *driftTestStateManager) SetSessionIntentEmbedding(emb []float32) {
	m.sessionIntentEmbedding = emb
}

func (m *driftTestStateManager) SetSessionIntentEmbeddingIfNil(emb []float32) bool {
	if m.sessionIntentEmbedding == nil {
		m.sessionIntentEmbedding = emb
		return true
	}
	return false
}

// boolPtr is a test helper for creating *bool values.
func boolPtr(b bool) *bool { return &b }

// ---------------------------------------------------------------------------
// setupDriftManager — creates an EmbeddingManager in a temp dir, initialized.
// Returns the manager and a cleanup function.
//
// NOTE: Uses t.Setenv, so cannot be called in t.Parallel() tests (Go 1.25).
// ---------------------------------------------------------------------------

func setupDriftManager(t *testing.T) (*embedding.EmbeddingManager, func()) {
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
// TestDefaultDriftConfig
// ---------------------------------------------------------------------------

func TestDefaultDriftConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultDriftConfig()

	assert.NotNil(t, cfg.Enabled, "Enabled should not be nil")
	assert.True(t, *cfg.Enabled, "Enabled should default to true")
	assert.Equal(t, float32(0.60), cfg.Threshold, "Threshold should default to 0.60")
	assert.Equal(t, 5, cfg.CheckInterval, "CheckInterval should default to 5")
}

// ---------------------------------------------------------------------------
// TestDriftConfig_Resolve
// ---------------------------------------------------------------------------

func TestDriftConfig_Resolve(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input DriftConfig
		want  DriftConfig
	}{
		{
			name:  "nil_enabled_resolves_to_true",
			input: DriftConfig{Enabled: nil, Threshold: 0, CheckInterval: 0},
			want:  DriftConfig{Enabled: boolPtr(true), Threshold: 0.60, CheckInterval: 5},
		},
		{
			name:  "explicit_false_stays_false",
			input: DriftConfig{Enabled: boolPtr(false), Threshold: 0.50, CheckInterval: 3},
			want:  DriftConfig{Enabled: boolPtr(false), Threshold: 0.50, CheckInterval: 3},
		},
		{
			name:  "explicit_true_stays_true",
			input: DriftConfig{Enabled: boolPtr(true), Threshold: 0.80, CheckInterval: 10},
			want:  DriftConfig{Enabled: boolPtr(true), Threshold: 0.80, CheckInterval: 10},
		},
		{
			name:  "negative_threshold_uses_default",
			input: DriftConfig{Enabled: boolPtr(true), Threshold: -1.0, CheckInterval: 3},
			want:  DriftConfig{Enabled: boolPtr(true), Threshold: 0.60, CheckInterval: 3},
		},
		{
			name:  "zero_threshold_uses_default",
			input: DriftConfig{Enabled: boolPtr(true), Threshold: 0, CheckInterval: 10},
			want:  DriftConfig{Enabled: boolPtr(true), Threshold: 0.60, CheckInterval: 10},
		},
		{
			name:  "negative_check_interval_uses_default",
			input: DriftConfig{Enabled: boolPtr(true), Threshold: 0.80, CheckInterval: -5},
			want:  DriftConfig{Enabled: boolPtr(true), Threshold: 0.80, CheckInterval: 5},
		},
		{
			name:  "partial_override_preserves_positive_values",
			input: DriftConfig{Enabled: boolPtr(true), Threshold: 0.75, CheckInterval: 0},
			want:  DriftConfig{Enabled: boolPtr(true), Threshold: 0.75, CheckInterval: 5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.input.resolve()
			if tt.want.Enabled != nil {
				assert.NotNil(t, got.Enabled, "Enabled should not be nil")
				assert.Equal(t, *tt.want.Enabled, *got.Enabled, "Enabled mismatch")
			} else {
				assert.Nil(t, got.Enabled, "Enabled should be nil")
			}
			assert.Equal(t, tt.want.Threshold, got.Threshold, "Threshold mismatch")
			assert.Equal(t, tt.want.CheckInterval, got.CheckInterval, "CheckInterval mismatch")
		})
	}
}

// ---------------------------------------------------------------------------
// TestDriftConfig_Resolve_DoesNotMutateOriginal
// ---------------------------------------------------------------------------

func TestDriftConfig_Resolve_DoesNotMutateOriginal(t *testing.T) {
	t.Parallel()

	cfg := DriftConfig{Enabled: boolPtr(false), Threshold: 0, CheckInterval: 0}
	_ = cfg.resolve()

	assert.NotNil(t, cfg.Enabled, "Enabled should still be non-nil")
	assert.False(t, *cfg.Enabled, "Enabled should remain false")
	assert.Equal(t, float32(0), cfg.Threshold, "Threshold should remain 0")
	assert.Equal(t, 0, cfg.CheckInterval, "CheckInterval should remain 0")
}

// ---------------------------------------------------------------------------
// TestCheckDrift_SkipsNilManager
//
// No embedding manager needed — this test only verifies nil input handling.
// ---------------------------------------------------------------------------

func TestCheckDrift_SkipsNilManager(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stateMgr := &driftTestStateManager{}
	config := DefaultDriftConfig()

	result, err := CheckDrift(ctx, nil, stateMgr, "test prompt", 5, config)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

// ---------------------------------------------------------------------------
// TestCheckDrift_SkipsNilStateManager
// ---------------------------------------------------------------------------

func TestCheckDrift_SkipsNilStateManager(t *testing.T) {
	ctx := context.Background()
	mgr, cleanup := setupDriftManager(t)
	defer cleanup()

	result, err := CheckDrift(ctx, mgr, nil, "test prompt", 5, DefaultDriftConfig())
	assert.NoError(t, err)
	assert.Nil(t, result)
}

// ---------------------------------------------------------------------------
// TestCheckDrift_SkipsNilContext
// ---------------------------------------------------------------------------

func TestCheckDrift_SkipsNilContext(t *testing.T) {
	mgr, cleanup := setupDriftManager(t)
	defer cleanup()

	stateMgr := &driftTestStateManager{}
	config := DefaultDriftConfig()

	result, err := CheckDrift(nil, mgr, stateMgr, "test prompt", 5, config)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

// ---------------------------------------------------------------------------
// TestCheckDrift_SkipsEmptyPrompt
// ---------------------------------------------------------------------------

func TestCheckDrift_SkipsEmptyPrompt(t *testing.T) {
	ctx := context.Background()
	mgr, cleanup := setupDriftManager(t)
	defer cleanup()

	stateMgr := &driftTestStateManager{}
	config := DefaultDriftConfig()

	result, err := CheckDrift(ctx, mgr, stateMgr, "", 5, config)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

// ---------------------------------------------------------------------------
// TestCheckDrift_SkipsDisabled
// ---------------------------------------------------------------------------

func TestCheckDrift_SkipsDisabled(t *testing.T) {
	ctx := context.Background()
	mgr, cleanup := setupDriftManager(t)
	defer cleanup()

	stateMgr := &driftTestStateManager{}
	config := DriftConfig{Enabled: boolPtr(false)}

	result, err := CheckDrift(ctx, mgr, stateMgr, "test prompt", 5, config)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

// ---------------------------------------------------------------------------
// TestCheckDrift_SkipsNonIntervalTurn
// ---------------------------------------------------------------------------

func TestCheckDrift_SkipsNonIntervalTurn(t *testing.T) {
	ctx := context.Background()
	mgr, cleanup := setupDriftManager(t)
	defer cleanup()

	stateMgr := &driftTestStateManager{}
	config := DefaultDriftConfig() // CheckInterval = 5

	// Turn 3 is not a multiple of 5
	result, err := CheckDrift(ctx, mgr, stateMgr, "test prompt", 3, config)
	assert.NoError(t, err)
	assert.Nil(t, result)

	// Turn 7 is not a multiple of 5
	result, err = CheckDrift(ctx, mgr, stateMgr, "test prompt", 7, config)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

// ---------------------------------------------------------------------------
// TestCheckDrift_SkipsNegativeTurn
// ---------------------------------------------------------------------------

func TestCheckDrift_SkipsNegativeTurn(t *testing.T) {
	ctx := context.Background()
	mgr, cleanup := setupDriftManager(t)
	defer cleanup()

	stateMgr := &driftTestStateManager{}
	config := DefaultDriftConfig()

	// Turn 0
	result, err := CheckDrift(ctx, mgr, stateMgr, "test prompt", 0, config)
	assert.NoError(t, err)
	assert.Nil(t, result)

	// Turn -1
	result, err = CheckDrift(ctx, mgr, stateMgr, "test prompt", -1, config)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

// ---------------------------------------------------------------------------
// TestCheckDrift_SkipsNoIntentEmbedding
// ---------------------------------------------------------------------------

func TestCheckDrift_SkipsNoIntentEmbedding(t *testing.T) {
	ctx := context.Background()
	mgr, cleanup := setupDriftManager(t)
	defer cleanup()

	// State manager with nil intent embedding (default)
	stateMgr := &driftTestStateManager{}
	config := DefaultDriftConfig()

	// Turn 5 is on the interval but no embedding
	result, err := CheckDrift(ctx, mgr, stateMgr, "test prompt", 5, config)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

// ---------------------------------------------------------------------------
// TestCheckDrift_SkipsDimensionMismatch
//
// Verifies graceful skip when intent and current embeddings have different
// lengths (e.g., embedding model changed between sessions).
// ---------------------------------------------------------------------------

func TestCheckDrift_SkipsDimensionMismatch(t *testing.T) {
	ctx := context.Background()
	mgr, cleanup := setupDriftManager(t)
	defer cleanup()

	// Intent embedding has only 2 dimensions; real provider outputs more
	stateMgr := &driftTestStateManager{
		sessionIntentEmbedding: []float32{1.0, 0.0},
	}
	config := DefaultDriftConfig()

	result, err := CheckDrift(ctx, mgr, stateMgr, "test prompt at turn 5", 5, config)
	assert.NoError(t, err)
	assert.Nil(t, result, "should skip when embedding dimensions don't match")
}

// ---------------------------------------------------------------------------
// TestCheckDrift_Detected
//
// Uses a negated embedding to guarantee cosine similarity of -1.0,
// well below the threshold → drift detected.
// ---------------------------------------------------------------------------

func TestCheckDrift_Detected(t *testing.T) {
	ctx := context.Background()
	mgr, cleanup := setupDriftManager(t)
	defer cleanup()

	store, err := mgr.GetConversationStore(ctx)
	assert.NoError(t, err)
	provider := store.Provider()
	assert.NotNil(t, provider)

	// Get embedding for the intent
	intentEmbedding, err := provider.Embed(ctx, "How do I implement a REST API in Go?")
	assert.NoError(t, err)
	assert.NotEmpty(t, intentEmbedding)

	// Create an orthogonal embedding by negating — cosine similarity will be -1.0
	orthogonalEmb := make([]float32, len(intentEmbedding))
	for i, v := range intentEmbedding {
		orthogonalEmb[i] = -v
	}

	stateMgr := &driftTestStateManager{
		sessionIntentEmbedding: orthogonalEmb,
	}

	config := DefaultDriftConfig()

	result, err := CheckDrift(ctx, mgr, stateMgr, "How do I implement a REST API in Go?", 5, config)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Drifted, "should detect drift when embeddings are opposed")
	assert.Less(t, result.Similarity, config.Threshold, "similarity should be below threshold")
	assert.Equal(t, 5, result.TurnNumber)
	assert.NotEmpty(t, result.IntentEmbedding)
	assert.NotEmpty(t, result.CurrentEmbedding)
}

// ---------------------------------------------------------------------------
// TestCheckDrift_NotDetected
//
// Same prompt → deterministic embedding → cosine similarity = 1.0 → no drift.
// ---------------------------------------------------------------------------

func TestCheckDrift_NotDetected(t *testing.T) {
	ctx := context.Background()
	mgr, cleanup := setupDriftManager(t)
	defer cleanup()

	store, err := mgr.GetConversationStore(ctx)
	assert.NoError(t, err)
	provider := store.Provider()
	assert.NotNil(t, provider)

	intentEmbedding, err := provider.Embed(ctx, "How do I implement a REST API in Go?")
	assert.NoError(t, err)
	assert.NotEmpty(t, intentEmbedding)

	stateMgr := &driftTestStateManager{
		sessionIntentEmbedding: intentEmbedding,
	}

	config := DefaultDriftConfig()

	result, err := CheckDrift(ctx, mgr, stateMgr, "How do I implement a REST API in Go?", 5, config)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.Drifted, "should not detect drift for same prompt")
	assert.GreaterOrEqual(t, result.Similarity, config.Threshold, "similarity should be at or above threshold")
	assert.Equal(t, 5, result.TurnNumber)
	assert.NotEmpty(t, result.IntentEmbedding)
	assert.NotEmpty(t, result.CurrentEmbedding)
}

// ---------------------------------------------------------------------------
// TestCheckDrift_CustomThreshold
//
// Verifies that custom threshold values are respected.
// ---------------------------------------------------------------------------

func TestCheckDrift_CustomThreshold(t *testing.T) {
	ctx := context.Background()
	mgr, cleanup := setupDriftManager(t)
	defer cleanup()

	store, err := mgr.GetConversationStore(ctx)
	assert.NoError(t, err)
	provider := store.Provider()
	assert.NotNil(t, provider)

	intentEmbedding, err := provider.Embed(ctx, "How do I implement a REST API in Go?")
	assert.NoError(t, err)

	stateMgr := &driftTestStateManager{
		sessionIntentEmbedding: intentEmbedding,
	}

	// Set threshold very high (0.99) — same prompt should still pass (similarity ≈ 1.0)
	config := DriftConfig{Enabled: boolPtr(true), Threshold: 0.99, CheckInterval: 1}

	result, err := CheckDrift(ctx, mgr, stateMgr, "How do I implement a REST API in Go?", 1, config)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.Drifted, "same prompt with high threshold should not drift")

	// Different prompt with high threshold should drift
	result, err = CheckDrift(ctx, mgr, stateMgr, "How do I bake cookies?", 2, config)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Drifted, "different prompt with high threshold should drift")
}

// ---------------------------------------------------------------------------
// TestCheckDrift_CheckIntervalCustom
//
// Verifies that CheckInterval controls which turns are actually checked.
// ---------------------------------------------------------------------------

func TestCheckDrift_CheckIntervalCustom(t *testing.T) {
	ctx := context.Background()
	mgr, cleanup := setupDriftManager(t)
	defer cleanup()

	store, err := mgr.GetConversationStore(ctx)
	assert.NoError(t, err)
	provider := store.Provider()
	assert.NotNil(t, provider)

	intentEmbedding, err := provider.Embed(ctx, "How do I implement a REST API in Go?")
	assert.NoError(t, err)

	stateMgr := &driftTestStateManager{
		sessionIntentEmbedding: intentEmbedding,
	}

	// CheckInterval = 3: only turns 3, 6, 9, ... get checked
	config := DriftConfig{Enabled: boolPtr(true), Threshold: 0.60, CheckInterval: 3}

	// Turn 1 — skipped (not a multiple of 3)
	result, err := CheckDrift(ctx, mgr, stateMgr, "How do I implement a REST API in Go?", 1, config)
	assert.NoError(t, err)
	assert.Nil(t, result)

	// Turn 3 — checked
	result, err = CheckDrift(ctx, mgr, stateMgr, "How do I implement a REST API in Go?", 3, config)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.Drifted)
	assert.Equal(t, 3, result.TurnNumber)
}

// ---------------------------------------------------------------------------
// TestCheckDrift_DefensiveCopy
//
// Verifies that returned embeddings are defensive copies, not aliases.
// Modifying the result's embedding should not affect the original.
// ---------------------------------------------------------------------------

func TestCheckDrift_DefensiveCopy(t *testing.T) {
	ctx := context.Background()
	mgr, cleanup := setupDriftManager(t)
	defer cleanup()

	store, err := mgr.GetConversationStore(ctx)
	assert.NoError(t, err)
	provider := store.Provider()
	assert.NotNil(t, provider)

	intentEmbedding, err := provider.Embed(ctx, "How do I implement a REST API in Go?")
	assert.NoError(t, err)
	assert.NotEmpty(t, intentEmbedding)

	stateMgr := &driftTestStateManager{
		sessionIntentEmbedding: intentEmbedding,
	}

	config := DefaultDriftConfig()

	result, err := CheckDrift(ctx, mgr, stateMgr, "How do I implement a REST API in Go?", 5, config)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// Defensive copy verification: modify result.IntentEmbedding and
	// confirm the original intentEmbedding is untouched.
	origFirst := intentEmbedding[0]
	result.IntentEmbedding[0] = -999.0
	assert.Equal(t, origFirst, intentEmbedding[0],
		"modifying result.IntentEmbedding should not affect the original")

	// Defensive copy verification: modify result.CurrentEmbedding and
	// then re-embed to confirm they are different backing arrays.
	origCurrentFirst := result.CurrentEmbedding[0]
	result.CurrentEmbedding[0] = -888.0

	// Re-embed the same prompt to get a fresh copy and verify the
	// provider's output wasn't affected by our modification.
	freshEmb, err := provider.Embed(ctx, "How do I implement a REST API in Go?")
	assert.NoError(t, err)
	assert.Equal(t, origCurrentFirst, freshEmb[0],
		"modifying result.CurrentEmbedding should not affect provider output")

	// Verify CurrentEmbedding has correct dimensionality
	assert.NotEmpty(t, result.CurrentEmbedding)
	assert.Equal(t, len(intentEmbedding), len(result.CurrentEmbedding),
		"both embeddings should have the same dimensionality")
}

// ---------------------------------------------------------------------------
// TestCheckDrift_ResultFields
//
// Verifies all fields of the returned DriftResult are correct.
// ---------------------------------------------------------------------------

func TestCheckDrift_ResultFields(t *testing.T) {
	ctx := context.Background()
	mgr, cleanup := setupDriftManager(t)
	defer cleanup()

	store, err := mgr.GetConversationStore(ctx)
	assert.NoError(t, err)
	provider := store.Provider()
	assert.NotNil(t, provider)

	intentEmbedding, err := provider.Embed(ctx, "How do I implement a REST API in Go?")
	assert.NoError(t, err)

	stateMgr := &driftTestStateManager{
		sessionIntentEmbedding: intentEmbedding,
	}

	config := DefaultDriftConfig()

	result, err := CheckDrift(ctx, mgr, stateMgr, "How do I implement a REST API in Go?", 10, config)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// TurnNumber should match
	assert.Equal(t, 10, result.TurnNumber)

	// Similarity for identical prompt should be 1.0 (deterministic provider)
	assert.InDelta(t, 1.0, result.Similarity, 0.001,
		"identical prompt should have similarity ≈ 1.0")

	// Drifted should be false for high similarity
	assert.False(t, result.Drifted)

	// Embeddings should be non-empty
	assert.NotEmpty(t, result.IntentEmbedding)
	assert.NotEmpty(t, result.CurrentEmbedding)
}

// ---------------------------------------------------------------------------
// TestCheckDrift_ContextCancellation
//
// Verifies graceful degradation when context is cancelled.
// ---------------------------------------------------------------------------

func TestCheckDrift_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	mgr, cleanup := setupDriftManager(t)
	defer cleanup()

	stateMgr := &driftTestStateManager{
		sessionIntentEmbedding: []float32{1.0, 0.0}, // dummy embedding (wrong dimensionality)
	}

	config := DefaultDriftConfig()

	// Cancel before calling
	cancel()

	result, err := CheckDrift(ctx, mgr, stateMgr, "test prompt", 5, config)
	// Should return gracefully, no panic
	assert.NoError(t, err)
	_ = result
}
