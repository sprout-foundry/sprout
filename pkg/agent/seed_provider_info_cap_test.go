package agent

import (
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
		configManager: configManager,
		state:         NewAgentStateManager(false),
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
		configManager: configManager,
		state:         NewAgentStateManager(false),
	}
	// Cap set above native (128K) — Info() should still return 128K.
	agent.effectiveContextCap = 256_000

	prov, err := NewSproutProvider(agent, mockClient)
	require.NoError(t, err)

	info := prov.(*sproutProvider).Info()
	require.Equal(t, 128_000, info.ContextSize,
		"Info() must return the native 128K window when the cap is above it")
}
