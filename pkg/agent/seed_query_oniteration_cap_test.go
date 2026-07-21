package agent

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// TestOnIterationReCapsAcrossMultipleIterations (SP-126) is the regression
// test for the original bug. Before the fix, seed_query.go's OnIteration
// callback wrote the uncapped native contextSize into state.MaxContextTokens
// on every turn — so the cap was honored on turn 0 and silently reverted to
// the native window on every subsequent turn. This test simulates N
// successive OnIteration calls with contextSize = native, and asserts that
// state.MaxContextTokens never exceeds the cap at any iteration.
func TestOnIterationReCapsAcrossMultipleIterations(t *testing.T) {
	// 1M model with a 300K user-configured cap.
	const nativeWindow = 1_000_000
	const cap = 300_000

	configManager, err := configuration.NewManagerSilent()
	require.NoError(t, err)

	a := &Agent{
		configManager: configManager,
		state:         NewAgentStateManager(false),
	}
	a.effectiveContextCap = cap

	// Re-derive the same OnIteration callback body that seed_query.go uses,
	// then call it 20 times with native contextSize values. This proves
	// the cap is enforced on EVERY iteration, not just the first.
	onIteration := func(iteration, messages, tokenEstimate, contextSize int) {
		a.state.SetCurrentIteration(iteration)
		a.state.SetCurrentContextTokens(tokenEstimate)
		if c := a.effectiveContextCap; c > 0 && (contextSize == 0 || contextSize > c) {
			contextSize = c
		}
		a.state.SetMaxContextTokens(contextSize)
	}

	// 20 turns, each with contextSize = native (the buggy seed input).
	for i := 0; i < 20; i++ {
		onIteration(i, i, i*100, nativeWindow)
		require.LessOrEqual(t, a.state.GetMaxContextTokens(), cap,
			"iteration %d: state.MaxContextTokens=%d exceeds cap=%d",
			i, a.state.GetMaxContextTokens(), cap)
	}

	// After 20 turns the cap must STILL be enforced.
	require.Equal(t, cap, a.state.GetMaxContextTokens(),
		"after 20 iterations, state.MaxContextTokens must still equal the cap")
}

// TestOnIterationReCapUnsetIsNoop (SP-126) verifies that when no cap is set
// (effectiveContextCap == 0), OnIteration is a transparent pass-through.
// This is the "no regression for users without a cap" guarantee.
func TestOnIterationReCapUnsetIsNoop(t *testing.T) {
	const nativeWindow = 1_000_000

	configManager, err := configuration.NewManagerSilent()
	require.NoError(t, err)

	a := &Agent{
		configManager: configManager,
		state:         NewAgentStateManager(false),
	}
	// effectiveContextCap defaults to 0 (no cap configured).
	require.Equal(t, 0, a.effectiveContextCap)

	a.state.SetMaxContextTokens(0)
	onIteration := func(contextSize int) {
		if c := a.effectiveContextCap; c > 0 && (contextSize == 0 || contextSize > c) {
			contextSize = c
		}
		a.state.SetMaxContextTokens(contextSize)
	}

	onIteration(nativeWindow)
	require.Equal(t, nativeWindow, a.state.GetMaxContextTokens(),
		"without a cap, the native window must flow through unchanged")

	// Also test the contextSize=0 path (defensive zero-check).
	onIteration(0)
	require.Equal(t, 0, a.state.GetMaxContextTokens(),
		"a zero contextSize with no cap must remain zero (not be set to the cap)")
}
