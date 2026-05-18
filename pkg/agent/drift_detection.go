package agent

import (
	"context"
	"log"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// DriftConfig holds configuration for session intent drift detection.
// These defaults will later be backed by PersistentContextConfig in
// pkg/configuration (SP-027-3e).
type DriftConfig struct {
	// Enabled determines whether drift detection is active. Default: true.
	// Uses a pointer so the zero value (nil) maps to the default (true)
	// while still allowing explicit Disable via Enabled: func() *bool { b := false; return &b }().
	Enabled *bool

	// Threshold is the minimum cosine similarity required to consider
	// the current prompt aligned with the session's original intent.
	// Values below this indicate potential drift. Default: 0.60.
	Threshold float32

	// CheckInterval is the number of turns between drift checks.
	// Only every Nth turn is checked. Default: 5 (check every 5th turn).
	CheckInterval int
}

// DefaultDriftConfig returns a DriftConfig with the standard defaults
// specified in SP-027.
func DefaultDriftConfig() DriftConfig {
	return DriftConfig{
		Enabled:       func() *bool { b := true; return &b }(),
		Threshold:     0.60,
		CheckInterval: 5,
	}
}

// resolve fills zero/nil fields with defaults and returns the resolved copy.
// Only nil/zero/negative values are replaced — any positive override is preserved.
func (c DriftConfig) resolve() DriftConfig {
	d := DefaultDriftConfig()
	resolved := DriftConfig{
		Threshold:     c.Threshold,
		CheckInterval: c.CheckInterval,
	}
	// Enabled: nil means "use default" (true). Non-nil preserves the user's choice.
	if c.Enabled != nil {
		resolved.Enabled = c.Enabled
	} else {
		resolved.Enabled = d.Enabled
	}
	if resolved.Threshold <= 0 {
		resolved.Threshold = d.Threshold
	}
	if resolved.CheckInterval <= 0 {
		resolved.CheckInterval = d.CheckInterval
	}
	return resolved
}

// isEnabled is a convenience to dereference the Enabled pointer after resolve.
func (c DriftConfig) isEnabled() bool {
	if c.Enabled == nil {
		return false // fallback; resolve() should have set it
	}
	return *c.Enabled
}

// DriftResult holds the result of a drift detection check.
type DriftResult struct {
	// Drifted is true if the current prompt's similarity to the session
	// intent embedding falls below the configured threshold.
	Drifted bool

	// Similarity is the cosine similarity score between the session intent
	// embedding and the current prompt embedding. Values range from -1 to 1,
	// with 1 indicating identical direction.
	Similarity float32

	// IntentEmbedding is the session's original intent embedding (captured
	// from the first turn's user prompt). A defensive copy is returned.
	IntentEmbedding []float32

	// CurrentEmbedding is the embedding of the current user prompt being
	// checked. A defensive copy is returned.
	CurrentEmbedding []float32

	// TurnNumber is the turn number at which this drift check was performed.
	TurnNumber int
}

// CheckDrift detects whether the conversation has drifted from its original
// intent by comparing the current user prompt's embedding against the session's
// intent embedding (captured from the first turn).
//
// The check runs every Nth turn (configurable via CheckInterval) and compares
// cosine similarity between embeddings. If similarity falls below the threshold,
// the result's Drifted field is set to true.
//
// Setup (first turn): The SessionIntentEmbedding is set from the first turn's
// user prompt embedding in turn_checkpoints.go (see SetSessionIntentEmbeddingIfNil).
//
// Check (every Nth turn): Embed the current user prompt, compute cosine similarity
// with SessionIntentEmbedding, flag if below threshold.
//
// Graceful degradation: all errors are logged and nil/empty is returned.
// The agent should never be blocked by a drift detection failure.
func CheckDrift(
	ctx context.Context,
	mgr *embedding.EmbeddingManager,
	stateMgr StateManager,
	prompt string,
	turnNumber int,
	config DriftConfig,
) (*DriftResult, error) {
	config = config.resolve()

	// Nil-safe input validation
	if mgr == nil {
		log.Printf("[drift-detection] skipping: embedding manager is nil")
		return nil, nil
	}
	if stateMgr == nil {
		log.Printf("[drift-detection] skipping: state manager is nil")
		return nil, nil
	}
	if ctx == nil {
		log.Printf("[drift-detection] skipping: context is nil")
		return nil, nil
	}
	if prompt == "" {
		log.Printf("[drift-detection] skipping: empty prompt")
		return nil, nil
	}

	// If not enabled, skip silently (no log spam for intentional disable)
	if !config.isEnabled() {
		return nil, nil
	}

	// Only check on positive turn numbers
	if turnNumber <= 0 {
		return nil, nil
	}

	// Only check every Nth turn
	if turnNumber%config.CheckInterval != 0 {
		return nil, nil
	}

	// Get the session intent embedding (captured from first turn)
	intentEmb := stateMgr.GetSessionIntentEmbedding()
	if intentEmb == nil {
		log.Printf("[drift-detection] skipping: no session intent embedding available")
		return nil, nil
	}

	// Ensure the embedding manager is initialized
	if err := mgr.Init(ctx); err != nil {
		log.Printf("[drift-detection] init failed: %v", err)
		return nil, nil
	}

	// Acquire the conversation store (lazy-created by the manager)
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		log.Printf("[drift-detection] conversation store unavailable: %v", err)
		return nil, nil
	}

	provider := store.Provider()
	if provider == nil {
		log.Printf("[drift-detection] provider unexpectedly nil")
		return nil, nil
	}

	// Embed the current prompt
	currentEmb, err := provider.Embed(ctx, prompt)
	if err != nil {
		if ctx.Err() != nil {
			log.Printf("[drift-detection] embedding cancelled: %v", ctx.Err())
		} else {
			log.Printf("[drift-detection] prompt embedding failed: %v", err)
		}
		return nil, nil
	}
	if len(currentEmb) == 0 {
		log.Printf("[drift-detection] prompt embedding returned empty vector")
		return nil, nil
	}

	// Dimension mismatch check: if the embedding model changed between sessions,
	// intent and current embeddings may have different lengths. CosineSimilarity
	// silently returns 0 for mismatched dimensions, which would be a false
	// positive drift. Gracefully skip instead.
	if len(intentEmb) != len(currentEmb) {
		log.Printf("[drift-detection] dimension mismatch: intent=%d, current=%d; skipping",
			len(intentEmb), len(currentEmb))
		return nil, nil
	}

	// Compute cosine similarity between session intent and current prompt
	similarity := embedding.CosineSimilarity(intentEmb, currentEmb)

	// Flag as drifted if similarity falls below threshold
	drifted := similarity < config.Threshold

	// Create defensive copies of embeddings for the result
	intentCopy := make([]float32, len(intentEmb))
	copy(intentCopy, intentEmb)
	currentCopy := make([]float32, len(currentEmb))
	copy(currentCopy, currentEmb)

	result := &DriftResult{
		Drifted:          drifted,
		Similarity:       similarity,
		IntentEmbedding:  intentCopy,
		CurrentEmbedding: currentCopy,
		TurnNumber:       turnNumber,
	}

	if drifted {
		log.Printf("[drift-detection] drift detected at turn %d: similarity %.3f < threshold %.2f",
			turnNumber, similarity, config.Threshold)
	} else {
		log.Printf("[drift-detection] no drift at turn %d: similarity %.3f >= threshold %.2f",
			turnNumber, similarity, config.Threshold)
	}

	return result, nil
}
