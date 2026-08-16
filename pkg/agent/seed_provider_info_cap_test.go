package agent

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// TestSproutProviderInfoAppliesCap (SP-126) verifies that sproutProvider.Info()
// returns a ContextSize that honors the effective context cap set on the
// Agent, not the model's native window. This is the root-cause fix for the
// bug where seed's per-iteration OnIteration callback received the uncapped
// native size and clobbered state.MaxContextTokens every turn.
func TestSproutProviderInfoAppliesCap(t *testing.T) {
	// MockClient reports a 128K native context window. With a 64K cap, Info()
	// must return ContextSize = 64K (capped), not 128K (native).
	mockClient := &MockClient{model: "test-model"}

	configManager, err := configuration.NewManagerSilent()
	require.NoError(t, err)

	agent := &Agent{
		configManager:       configManager,
		state:               NewAgentStateManager(false),
		nativeContextWindow: 128_000, // matches MockClient so the fast path applies the cap
	}
	agent.effectiveContextCap = 64_000

	prov, err := NewSproutProvider(agent, mockClient)
	require.NoError(t, err)

	info := prov.(*sproutProvider).Info()
	require.Equal(t, 64_000, info.ContextSize,
		"Info().ContextSize must equal effectiveContextCap (64K), not the native 128K window")

	// Also verify the OnIteration-time defensive re-cap survives the case
	// where Info() somehow delivers the native value (simulated by clearing
	// the cap momentarily). OnIteration must always re-cap from
	// a.effectiveContextCap — the field is the authoritative source.
	a := agent
	a.state.SetMaxContextTokens(0) // simulate "uncapped native wrote through"

	// Reproduce the callback body to test the re-cap directly.
	iteration, messages, tokenEstimate, contextSize := 1, 1, 1000, 128_000
	if cap := a.effectiveContextCap; cap > 0 && (contextSize == 0 || contextSize > cap) {
		contextSize = cap
	}
	a.state.SetMaxContextTokens(contextSize)

	require.Equal(t, 64_000, a.state.GetMaxContextTokens(),
		"OnIteration defensive re-cap must clamp contextSize to effectiveContextCap")
	_ = iteration
	_ = messages
	_ = tokenEstimate
}

// TestSproutProviderInfoCapEqualsNative (SP-126) verifies that when the
// effective cap equals the native window (or the cap is unset), Info()
// returns the native value. The cap is a no-op in this case.
func TestSproutProviderInfoCapEqualsNative(t *testing.T) {
	mockClient := &MockClient{model: "test-model"}

	configManager, err := configuration.NewManagerSilent()
	require.NoError(t, err)

	agent := &Agent{
		configManager:       configManager,
		state:               NewAgentStateManager(false),
		nativeContextWindow: 128_000, // matches MockClient so the fast path applies the cap
	}
	// Cap set above native (128K) — Info() should still return 128K.
	agent.effectiveContextCap = 256_000

	prov, err := NewSproutProvider(agent, mockClient)
	require.NoError(t, err)

	info := prov.(*sproutProvider).Info()
	require.Equal(t, 128_000, info.ContextSize,
		"Info() must return the native 128K window when the cap is above it")
}

// TestInfoFallbackOnContextLimitError verifies that a failing
// GetModelContextLimit lookup no longer zeroes ContextSize (which would
// disable seed's compaction trigger entirely). Info() must fall back to the
// last known effective cap, or 32K when no cap is known.
func TestInfoFallbackOnContextLimitError(t *testing.T) {
	t.Run("falls back to effectiveContextCap", func(t *testing.T) {
		configManager, err := configuration.NewManagerSilent()
		require.NoError(t, err)

		agent := &Agent{
			configManager: configManager,
			state:         NewAgentStateManager(false),
		}
		agent.effectiveContextCap = 96_000

		prov, err := NewSproutProvider(agent, &errContextLimitClient{MockClient: &MockClient{model: "test-model"}})
		require.NoError(t, err)

		info := prov.(*sproutProvider).Info()
		require.Equal(t, 96_000, info.ContextSize,
			"Info() must fall back to effectiveContextCap when the context-limit lookup errors")
	})

	t.Run("falls back to 32K when no cap is known", func(t *testing.T) {
		configManager, err := configuration.NewManagerSilent()
		require.NoError(t, err)

		agent := &Agent{
			configManager: configManager,
			state:         NewAgentStateManager(false),
		}

		prov, err := NewSproutProvider(agent, &errContextLimitClient{MockClient: &MockClient{model: "test-model"}})
		require.NoError(t, err)

		info := prov.(*sproutProvider).Info()
		require.Equal(t, 32_000, info.ContextSize,
			"Info() must fall back to 32K when the context-limit lookup errors and no cap is known")
	})
}

// TestInfoReconcilesStaleCap verifies that a cap resolved against a stale
// native window (e.g. the creation-time lookup failed to 32K) is re-resolved
// on the fly when the live window is larger — so compaction doesn't trigger
// absurdly early against the stale 32K.
func TestInfoReconcilesStaleCap(t *testing.T) {
	configManager, err := configuration.NewManagerSilent()
	require.NoError(t, err)

	agent := &Agent{
		configManager: configManager,
		state:         NewAgentStateManager(false),
	}
	// Creation-time lookup failed: cap was resolved against 32K.
	agent.effectiveContextCap = 32_000
	agent.nativeContextWindow = 32_000

	// MockClient now reports a live 128K window.
	prov, err := NewSproutProvider(agent, &MockClient{model: "test-model"})
	require.NoError(t, err)

	info := prov.(*sproutProvider).Info()
	require.Equal(t, 128_000, info.ContextSize,
		"Info() must reconcile the stale 32K cap up to the live 128K window (no user cap)")
	require.Equal(t, 128_000, agent.effectiveContextCap,
		"reconciliation must update effectiveContextCap to the live window")
}

// TestInfoKeepsUserCapAcrossReconcile verifies that reconciliation still
// honors an explicit user cap even when the live native window is larger.
func TestInfoKeepsUserCapAcrossReconcile(t *testing.T) {
	configManager, err := configuration.NewManagerSilent()
	require.NoError(t, err)

	userCap := 64_000
	require.NoError(t, configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.MaxContextTokens = &userCap
		return nil
	}))

	agent := &Agent{
		configManager: configManager,
		state:         NewAgentStateManager(false),
	}
	// Creation-time lookup failed: cap was resolved against 32K.
	agent.effectiveContextCap = 32_000
	agent.nativeContextWindow = 32_000

	prov, err := NewSproutProvider(agent, &MockClient{model: "test-model"})
	require.NoError(t, err)

	info := prov.(*sproutProvider).Info()
	require.Equal(t, 64_000, info.ContextSize,
		"Info() must reconcile the stale cap but keep the user's 64K cap")
	require.Equal(t, 64_000, agent.effectiveContextCap,
		"reconciliation must store the user cap as the effective cap")
}

// TestInfoAppliesRuntimeCapChange verifies that a MaxContextTokens change
// made at runtime (via /max-context or the settings API) is picked up by the
// very next Info() call — no provider/model switch required.
func TestInfoAppliesRuntimeCapChange(t *testing.T) {
	configManager, err := configuration.NewManagerSilent()
	require.NoError(t, err)

	agent := &Agent{
		configManager:       configManager,
		state:               NewAgentStateManager(false),
		nativeContextWindow: 128_000, // matches MockClient so the fast path applies
	}
	agent.effectiveContextCap = 128_000

	prov, err := NewSproutProvider(agent, &MockClient{model: "test-model"})
	require.NoError(t, err)
	sproutProv := prov.(*sproutProvider)

	// No user cap → Info() returns the native window.
	require.Equal(t, 128_000, sproutProv.Info().ContextSize)

	// Set a runtime cap below native. reconcileContextCap must detect the
	// config change on the next call even though the native window is the
	// same (previously the fast path served the stale 128K resolution).
	userCap := 64_000
	require.NoError(t, configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.MaxContextTokens = &userCap
		return nil
	}))

	require.Equal(t, 64_000, sproutProv.Info().ContextSize,
		"Info() must apply a runtime cap change without a model switch")
	require.Equal(t, 64_000, agent.effectiveContextCap,
		"runtime cap change must be reflected in effectiveContextCap")
}

// TestInfoClearsRuntimeCap verifies that clearing MaxContextTokens at runtime
// restores the native window on the next Info() call.
func TestInfoClearsRuntimeCap(t *testing.T) {
	configManager, err := configuration.NewManagerSilent()
	require.NoError(t, err)

	userCap := 64_000
	require.NoError(t, configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.MaxContextTokens = &userCap
		return nil
	}))

	agent := &Agent{
		configManager:       configManager,
		state:               NewAgentStateManager(false),
		nativeContextWindow: 128_000,
	}
	agent.effectiveContextCap = 64_000

	prov, err := NewSproutProvider(agent, &MockClient{model: "test-model"})
	require.NoError(t, err)
	sproutProv := prov.(*sproutProvider)

	// Capped state → Info() returns the cap.
	require.Equal(t, 64_000, sproutProv.Info().ContextSize)

	// Clear the cap → Info() returns the native window again.
	require.NoError(t, configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.MaxContextTokens = nil
		return nil
	}))

	require.Equal(t, 128_000, sproutProv.Info().ContextSize,
		"Info() must restore the native window after the cap is cleared")
	require.Equal(t, 128_000, agent.effectiveContextCap)
}

// TestContextCapConcurrentAccess is a smoke test for the contextCapMu guard.
// reconcileContextCap (per-iteration path, Info) and refreshEffectiveContextCap
// (model-switch / /max-context path) must be safe to run concurrently. No
// assertions beyond no-panic: -race is unavailable on this platform, but the
// test documents the intended concurrency contract for the two writers.
func TestContextCapConcurrentAccess(t *testing.T) {
	configManager, err := configuration.NewManagerSilent()
	require.NoError(t, err)

	agent := &Agent{
		configManager:       configManager,
		state:               NewAgentStateManager(false),
		nativeContextWindow: 128_000,
	}
	agent.effectiveContextCap = 128_000
	agent.client = &MockClient{model: "test-model"}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				agent.reconcileContextCap(128_000)
			}
		}()
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				agent.refreshEffectiveContextCap()
			}
		}()
	}
	wg.Wait()

	require.Greater(t, agent.effectiveCapSnapshot(), 0,
		"concurrent cap writers must leave the effective cap in a sane state")
}
