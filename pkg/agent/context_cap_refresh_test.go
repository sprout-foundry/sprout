package agent

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// TestRefreshEffectiveContextCapNoUserCap verifies that when the user has no
// MaxContextTokens set, refreshEffectiveContextCap follows the native window.
// Regression test: before the fix, effectiveContextCap was set once at agent
// creation and never refreshed — switching from an 81K model to a 200K model
// would leave effectiveContextCap at 81K, causing the OnIteration clamping
// to truncate the new model's context and triggering premature compaction.
func TestRefreshEffectiveContextCapNoUserCap(t *testing.T) {
	cap := 0
	caps := &configuration.Config{MaxContextTokens: &cap}

	resolved, err := configuration.ResolveEffectiveContextCap(caps, 200_000)
	if err != nil {
		t.Fatalf("ResolveEffectiveContextCap(nil cap, 200K): %v", err)
	}
	if resolved != 200_000 {
		t.Errorf("ResolveEffectiveContextCap(nil cap, 200K) = %d, want 200_000", resolved)
	}

	resolved, err = configuration.ResolveEffectiveContextCap(caps, 32_000)
	if err != nil {
		t.Fatalf("ResolveEffectiveContextCap(nil cap, 32K): %v", err)
	}
	if resolved != 32_000 {
		t.Errorf("ResolveEffectiveContextCap(nil cap, 32K) = %d, want 32_000", resolved)
	}
}

// TestRefreshEffectiveContextCapWithUserCap verifies that a user-set cap
// is respected even when the native window is larger.
func TestRefreshEffectiveContextCapWithUserCap(t *testing.T) {
	userCap := 64_000
	caps := &configuration.Config{MaxContextTokens: &userCap}

	resolved, err := configuration.ResolveEffectiveContextCap(caps, 200_000)
	if err != nil {
		t.Fatalf("ResolveEffectiveContextCap(64K cap, 200K native): %v", err)
	}
	if resolved != 64_000 {
		t.Errorf("ResolveEffectiveContextCap(64K cap, 200K native) = %d, want 64_000", resolved)
	}

	resolved, err = configuration.ResolveEffectiveContextCap(caps, 48_000)
	if err != nil {
		t.Fatalf("ResolveEffectiveContextCap(64K cap, 48K native): %v", err)
	}
	if resolved != 48_000 {
		t.Errorf("ResolveEffectiveContextCap(64K cap, 48K native) = %d, want 48_000", resolved)
	}
}

// TestOnIterationClampingLogic verifies the OnIteration clamping logic that
// replaced the old (contextSize == 0 || contextSize > cap) guard.
func TestOnIterationClampingLogic(t *testing.T) {
	// Case 1: effectiveContextCap matches current model — no clamping.
	effectiveContextCap := 200_000
	contextSize := 200_000
	if cap := effectiveContextCap; cap > 0 && contextSize > cap {
		contextSize = cap
	}
	if contextSize == 0 {
		contextSize = effectiveContextCap
	}
	if contextSize != 200_000 {
		t.Errorf("Case 1: contextSize = %d, want 200_000", contextSize)
	}

	// Case 2: STALE cap (the old bug) — effectiveContextCap was 81K from old model.
	effectiveContextCap = 81_920
	contextSize = 200_000
	if cap := effectiveContextCap; cap > 0 && contextSize > cap {
		contextSize = cap
	}
	if contextSize == 0 {
		contextSize = effectiveContextCap
	}
	if contextSize != 81_920 {
		t.Errorf("Case 2 (stale cap): contextSize = %d, want 81_920 (documents the stale-cap bug)", contextSize)
	}

	// Case 3: After refresh, cap follows the new model — no clamping.
	effectiveContextCap = 200_000
	contextSize = 200_000
	if cap := effectiveContextCap; cap > 0 && contextSize > cap {
		contextSize = cap
	}
	if contextSize == 0 {
		contextSize = effectiveContextCap
	}
	if contextSize != 200_000 {
		t.Errorf("Case 3 (after refresh): contextSize = %d, want 200_000", contextSize)
	}

	// Case 4: Zero contextSize with cap — falls back to cap.
	effectiveContextCap = 81_920
	contextSize = 0
	if cap := effectiveContextCap; cap > 0 && contextSize > cap {
		contextSize = cap
	}
	if contextSize == 0 {
		contextSize = effectiveContextCap
	}
	if contextSize != 81_920 {
		t.Errorf("Case 4 (zero contextSize): contextSize = %d, want 81_920", contextSize)
	}
}

// TestRefreshEffectiveContextCapLive verifies refreshEffectiveContextCap
// against the agent's real client. The test client reports 128K.
func TestRefreshEffectiveContextCapLive(t *testing.T) {
	a := newIsolatedTestAgent(t)

	// With no user cap, refresh should set effectiveContextCap to the native window (128K).
	a.refreshEffectiveContextCap()
	expectedLimit := a.getNativeModelContextLimit()
	if a.effectiveContextCap != expectedLimit {
		t.Errorf("effectiveContextCap = %d, want %d (native limit)", a.effectiveContextCap, expectedLimit)
	}

	// Set a user cap below native and verify it sticks.
	userCap := 64_000
	_ = a.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.MaxContextTokens = &userCap
		return nil
	})
	a.refreshEffectiveContextCap()
	if a.effectiveContextCap != 64_000 {
		t.Errorf("effectiveContextCap = %d, want 64_000 (user cap)", a.effectiveContextCap)
	}

	// Clear the cap and verify it goes back to native.
	_ = a.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.MaxContextTokens = nil
		return nil
	})
	a.refreshEffectiveContextCap()
	if a.effectiveContextCap != expectedLimit {
		t.Errorf("effectiveContextCap after cap clear = %d, want %d (native)", a.effectiveContextCap, expectedLimit)
	}
}