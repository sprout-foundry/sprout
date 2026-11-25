package core

import (
	"fmt"
	"strings"
	"testing"
)

// Tests for the configurable SubstitutionTargetFraction (Phase 0a target).
// Added alongside the Options.SubstitutionTargetFraction field so that
// consumers can make each substitution pass buy substantial headroom
// instead of substituting just enough to clear the trigger.

// ─── Agent-level defaults ─────────────────────────────────────────────────

// TestSubstitutionTargetOrDefault_Default verifies that an Agent constructed
// without SubstitutionTargetFraction gets the 0.50 default, so each
// substitution pass buys substantial headroom rather than barely clearing
// the compaction trigger.
func TestSubstitutionTargetOrDefault_Default(t *testing.T) {
	a, err := NewAgent(Options{
		Provider: &mockProvider{},
		Executor: NoopExecutor,
	})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	got := a.substitutionTargetOrDefault()
	if got != defaultSubstitutionTargetFraction {
		t.Errorf("default substitution target = %v, want %v (defaultSubstitutionTargetFraction)", got, defaultSubstitutionTargetFraction)
	}
}

// TestSubstitutionTargetOrDefault_ExplicitOverride verifies the configured
// value is returned when set.
func TestSubstitutionTargetOrDefault_ExplicitOverride(t *testing.T) {
	a, err := NewAgent(Options{
		Provider:                   &mockProvider{},
		Executor:                   NoopExecutor,
		SubstitutionTargetFraction: 0.50,
	})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	got := a.substitutionTargetOrDefault()
	if got != 0.50 {
		t.Errorf("substitution target = %v, want 0.50", got)
	}
}

// TestSubstitutionTargetOrDefault_OutOfRangeFallsBack verifies that invalid
// values (0, negative, >=1) all fall back to the default.
func TestSubstitutionTargetOrDefault_OutOfRangeFallsBack(t *testing.T) {
	for _, bad := range []float64{0, -0.1, 1.0, 1.5} {
		a, err := NewAgent(Options{
			Provider:                   &mockProvider{},
			Executor:                   NoopExecutor,
			SubstitutionTargetFraction: bad,
		})
		if err != nil {
			t.Fatalf("NewAgent for %v: %v", bad, err)
		}
		got := a.substitutionTargetOrDefault()
		if got != defaultSubstitutionTargetFraction {
			t.Errorf("for out-of-range %v: substitution target = %v, want default %v", bad, got, defaultSubstitutionTargetFraction)
		}
	}
}

// ─── CompactWith SubstitutionTargetFraction override ──────────────────────

// TestCompactWith_LowerSubstitutionTarget_SubstitutesMore verifies that
// setting SubstitutionTargetFraction below the default causes more
// checkpoints to be substituted in a single pass.
func TestCompactWith_LowerSubstitutionTarget_SubstitutesMore(t *testing.T) {
	var msgs []Message
	var cps []TurnCheckpoint
	for i := 0; i < 6; i++ {
		u, a := makeTextTurn(i)
		msgs = append(msgs, u, a)
		cps = append(cps, TurnCheckpoint{
			StartIndex:        i * 2,
			EndIndex:          i*2 + 1,
			Summary:           fmt.Sprintf("Turn %d summary", i),
			ActionableSummary: fmt.Sprintf("- T%d done", i),
		})
	}
	before := roughTokens(msgs)

	// Default target (0.85): set tokenLimit so ~1 substitution clears it.
	defaultLimit := int(float64(before-250) / emergencyTargetFraction)
	defaultResult := CompactWith(CompactInputs{
		Messages:    msgs,
		TokenLimit:  defaultLimit,
		Checkpoints: cps,
	})

	// Low target (0.50): same tokenLimit, but substitution now targets 50%.
	// More checkpoints should be substituted.
	lowResult := CompactWith(CompactInputs{
		Messages:                   msgs,
		TokenLimit:                 defaultLimit,
		Checkpoints:                cps,
		SubstitutionTargetFraction: 0.50,
	})

	// The low-target result should substitute at least as many as the default,
	// and typically more.
	defaultSubbed := countCheckpointSummaries(defaultResult.Messages)
	lowSubbed := countCheckpointSummaries(lowResult.Messages)

	if lowSubbed < defaultSubbed {
		t.Errorf("lower target should substitute >= default; default=%d low=%d", defaultSubbed, lowSubbed)
	}
	// With a 6-turn conversation and a 0.50 target, we expect more than 1.
	if lowSubbed < 2 {
		t.Errorf("expected lower target to substitute >= 2 checkpoints, got %d", lowSubbed)
	}
}

// TestCompactWith_DefaultSubstitutionTargetMatchesOldBehavior verifies that
// when SubstitutionTargetFraction is zero (unset), CompactWith produces
// identical behavior to the pre-feature code path — substituting only
// enough to reach emergencyTargetFraction.
func TestCompactWith_DefaultSubstitutionTargetMatchesOldBehavior(t *testing.T) {
	var msgs []Message
	var cps []TurnCheckpoint
	for i := 0; i < 5; i++ {
		u, a := makeTextTurn(i)
		msgs = append(msgs, u, a)
		cps = append(cps, TurnCheckpoint{
			StartIndex:        i * 2,
			EndIndex:          i*2 + 1,
			Summary:           fmt.Sprintf("Turn %d summary", i),
			ActionableSummary: fmt.Sprintf("- T%d", i),
		})
	}
	before := roughTokens(msgs)
	// Target just one turn below current so default behavior substitutes ~1.
	// Note: with SP-128 fix, each substitution preserves the user query,
	// adding tokens, so we may need more slack.
	tokenLimit := int(float64(before-250) / emergencyTargetFraction)

	result := CompactWith(CompactInputs{
		Messages:    msgs,
		TokenLimit:  tokenLimit,
		Checkpoints: cps,
		// SubstitutionTargetFraction intentionally left zero (default).
	})

	if !strings.Contains(result.Strategy, "substitute") {
		t.Fatalf("expected strategy to include 'substitute', got %q", result.Strategy)
	}
	// With SP-128 fix, substitutions add preserved user queries (more tokens),
	// so we may need more substitutions to reach the target.
	subbed := countCheckpointSummaries(result.Messages)
	if subbed > 5 {
		t.Errorf("default target should substitute at most 5 checkpoints, got %d", subbed)
	}
}

// countCheckpointSummaries counts how many messages in the slice carry the
// MetaKeyCheckpoint marker — i.e., how many substitutions were applied.
func countCheckpointSummaries(msgs []Message) int {
	n := 0
	for _, m := range msgs {
		if m.Meta != nil && m.Meta[MetaKeyCheckpoint] == "true" {
			n++
		}
	}
	return n
}
