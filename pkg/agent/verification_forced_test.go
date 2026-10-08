//go:build !js

// verification_forced_test.go — the forced-verification seam: the escape
// hatch a caller uses when it has independent evidence the turn changed
// the workspace but the tracker's window stayed empty, so the turn-end
// hook's gate never opened. The seam runs the same trusted checks the
// hook runs and stores the result as the turn's verification state, and
// it honors the verification switch (a disabled run never verifies).

package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/plancontract"
)

// TestRunForcedTurnEndVerification_RunsAndStores proves the seam runs the
// project's trusted checks and stores the result, so the agent's existing
// LastVerificationResult / LastTurnVerification reads see it exactly as
// they see a hook-run result — even though the turn's change window is
// empty (the hook's gate would have stayed closed).
func TestRunForcedTurnEndVerification_RunsAndStores(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, "echo forced-verify-ok")
	rtWritePlan(t, root, []plancontract.Acceptance{
		{ID: "build-passes", Scope: "s1", Check: "npm run build", Kind: plancontract.KindBuild},
	})

	client := NewScriptedClient()
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: 1})

	// The turn changed nothing the tracker recorded: the hook's gate is
	// closed.
	require.Empty(t, ag.TurnChangedApplicationPaths(), "precondition: the change window is empty")

	res, err := ag.RunForcedTurnEndVerification(context.Background())
	require.NoError(t, err)
	require.NotNil(t, res, "forced verification must produce a result")
	require.True(t, res.Passed(), "the manifest's build command succeeds")

	// The result is stored: the runner's LastVerificationResult read (and
	// the per-turn metrics) pick it up exactly as a hook-run result.
	require.NotNil(t, ag.LastVerificationResult(), "the forced result must be stored on the agent")
	require.Same(t, res, ag.LastVerificationResult(), "LastVerificationResult must return the forced result")
	tv := ag.LastTurnVerification()
	require.NotNil(t, tv, "the forced run must store the per-turn verification state")
	require.Equal(t, 1, tv.Limit, "the stored limit must be the configured repair limit")
}

// TestRunForcedTurnEndVerification_DisabledIsNoOp proves the seam honors
// the verification switch: with verification disabled it runs nothing and
// stores nothing, so a disabled configuration can never be verified by the
// backstop.
func TestRunForcedTurnEndVerification_DisabledIsNoOp(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, "echo forced-verify-should-not-run")

	client := NewScriptedClient()
	ag := vhAgent(t, client, root, nil) // no verification section: default off

	res, err := ag.RunForcedTurnEndVerification(context.Background())
	require.NoError(t, err)
	require.Nil(t, res, "a disabled run must produce no result")
	require.Nil(t, ag.LastVerificationResult(), "a disabled run must store nothing")
	require.Nil(t, ag.LastTurnVerification(), "a disabled run must store no per-turn state")
}

// TestRunForcedTurnEndVerification_NilReceiverIsSafe pins the nil-receiver
// guard: the seam is callable on a nil agent (the benchmark's defensive
// wiring) and returns no result rather than panicking.
func TestRunForcedTurnEndVerification_NilReceiverIsSafe(t *testing.T) {
	var ag *Agent
	res, err := ag.RunForcedTurnEndVerification(context.Background())
	require.NoError(t, err)
	require.Nil(t, res)
}
