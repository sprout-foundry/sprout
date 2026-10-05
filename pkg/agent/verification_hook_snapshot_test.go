//go:build !js

// verification_hook_snapshot_test.go — SP-149 §149b: the turn-start
// verification snapshot and the starter-manifest write guard. Together they
// close the hole where the model could change what "passing" means mid-turn:
//
//   - the model's file tools (write_file / edit_file) refuse a mid-turn write
//     to the starter manifest while verification is enabled (the manifest is
//     the project's trusted source for the verification commands);
//   - a write to .sprout/plan.json is NOT refused (the plan is the semantic
//     layer, not a verification source), but the turn-start acceptance
//     snapshot — the manifest's commands AND the plan's acceptance — is what
//     gates the turn, so a mid-turn plan edit cannot loosen the gate.
//
// The guard tests exercise the agent's tool-handling path directly; the two
// full-turn tests (the manifest anchor and the plan-side defense) drive a
// scripted turn end to end (the live tool path) and pin the snapshot contract.

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/planstore"
	"github.com/sprout-foundry/sprout/pkg/starterstore"
)

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// snapWriteStarterManifest writes the project's starter manifest (build
// command = buildCommand, no test command so the baseline test check is
// skipped) to the project directory.
func snapWriteStarterManifest(t *testing.T, root, buildCommand string) string {
	t.Helper()
	path := starterstore.StarterManifestPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir .sprout: %v", err)
	}
	manifest := fmt.Sprintf(`{"starter":{"id":"fixture","version":"1.0.0"},"build":%q}`, buildCommand)
	if err := os.WriteFile(path, []byte(manifest), 0o644); err != nil {
		t.Fatalf("write starter manifest: %v", err)
	}
	return path
}

// snapStarterManifestBuild returns the manifest's current build command.
func snapStarterManifestBuild(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(starterstore.StarterManifestPath(root))
	require.NoError(t, err)
	var m struct {
		Build string `json:"build"`
	}
	require.NoError(t, json.Unmarshal(data, &m))
	return m.Build
}

// snapGuardAgent wires a fresh agent (workspace root = root, verification
// section = ver; nil means the default-off path) for the guard tests, which
// call the file-tool handlers directly (the agent's tool-handling path). The
// scripted client is never invoked (the handlers are called directly).
func snapGuardAgent(t *testing.T, root string, ver *configuration.VerificationConfig) *Agent {
	t.Helper()
	return vhAgent(t, NewScriptedClient(NewScriptedTextResponse("Done.")), root, ver)
}

// ---------------------------------------------------------------------------
// Guard unit tests (the per-agent refusal)
// ---------------------------------------------------------------------------

// TestRefuseStarterManifestWrite_EnabledRefuses verifies the guard refuses a
// manifest write while verification is enabled.
func TestRefuseStarterManifestWrite_EnabledRefuses(t *testing.T) {
	root := t.TempDir()
	manifest := snapWriteStarterManifest(t, root, "echo ok")
	ag := snapGuardAgent(t, root, &configuration.VerificationConfig{Enabled: true})

	err := ag.refuseStarterManifestWrite(manifest)
	require.Error(t, err, "the guard must refuse a manifest write while verification is enabled")
	assert.Contains(t, err.Error(), "trusted source")
	assert.Contains(t, err.Error(), "cannot be modified")
	assert.Contains(t, err.Error(), "tell the user")
}

// TestRefuseStarterManifestWrite_DisabledAllows verifies the guard is a
// no-op when verification is disabled (the default).
func TestRefuseStarterManifestWrite_DisabledAllows(t *testing.T) {
	root := t.TempDir()
	manifest := snapWriteStarterManifest(t, root, "echo ok")
	ag := snapGuardAgent(t, root, nil)

	require.NoError(t, ag.refuseStarterManifestWrite(manifest), "the guard must be a no-op when verification is disabled")
}

// TestRefuseStarterManifestWrite_NonManifestPath verifies the guard allows a
// non-manifest write even when verification is enabled.
func TestRefuseStarterManifestWrite_NonManifestPath(t *testing.T) {
	root := t.TempDir()
	snapWriteStarterManifest(t, root, "echo ok")
	ag := snapGuardAgent(t, root, &configuration.VerificationConfig{Enabled: true})

	other := filepath.Join(root, "src", "app.js")
	require.NoError(t, ag.refuseStarterManifestWrite(other), "the guard must allow non-manifest writes")
}

// TestRefuseStarterManifestWrite_PlanPathAllowed verifies the guard does NOT
// refuse a write to .sprout/plan.json (the plan is the semantic layer, not a
// verification source; the snapshot, not the guard, is the plan's gate).
func TestRefuseStarterManifestWrite_PlanPathAllowed(t *testing.T) {
	root := t.TempDir()
	snapWriteStarterManifest(t, root, "echo ok")
	ag := snapGuardAgent(t, root, &configuration.VerificationConfig{Enabled: true})

	planPath := filepath.Join(root, ".sprout", "plan.json")
	require.NoError(t, ag.refuseStarterManifestWrite(planPath), "the guard must allow plan writes (the plan is not a verification source)")
}

// TestPathResolvesToStarterManifest pins the guard's path-resolution helper:
// an absolute manifest path, a relative manifest path (resolved against the
// workspace root), and an unrelated path.
func TestPathResolvesToStarterManifest(t *testing.T) {
	root := t.TempDir()
	manifest := snapWriteStarterManifest(t, root, "echo ok")
	ag := snapGuardAgent(t, root, &configuration.VerificationConfig{Enabled: true})

	assert.True(t, pathResolvesToStarterManifest(ag, manifest), "the absolute manifest path must match")
	assert.True(t, pathResolvesToStarterManifest(ag, ".sprout/starter.json"), "a relative manifest path must resolve against the workspace root")
	assert.False(t, pathResolvesToStarterManifest(ag, filepath.Join(root, "src", "app.js")), "an unrelated path must not match")
}

// ---------------------------------------------------------------------------
// Guard integration tests (the agent's tool-handling path)
// ---------------------------------------------------------------------------

// TestHandleWriteFileRefusesStarterManifest verifies the agent's write-file
// handler refuses a mid-turn write to the starter manifest while verification
// is enabled, leaving the file untouched (the plain and .json routes are both
// guarded, because the guard fires before the JSON routing).
func TestHandleWriteFileRefusesStarterManifest(t *testing.T) {
	root := t.TempDir()
	manifest := snapWriteStarterManifest(t, root, "echo fixture-build")
	ag := snapGuardAgent(t, root, &configuration.VerificationConfig{Enabled: true})

	_, err := handleWriteFile(context.Background(), ag, map[string]interface{}{
		"path":    manifest,
		"content": `{"starter":{"id":"fixture","version":"1.0.0"},"build":"true"}`,
	})
	require.Error(t, err, "the write-file handler must refuse a manifest write while verification is enabled")
	assert.Contains(t, err.Error(), "trusted source")
	// The manifest on disk is unchanged.
	assert.Equal(t, "echo fixture-build", snapStarterManifestBuild(t, root), "the manifest must be left untouched")
}

// TestHandleEditFileRefusesStarterManifest verifies the agent's edit-file
// handler refuses a mid-turn edit to the starter manifest while verification
// is enabled, leaving the file untouched.
func TestHandleEditFileRefusesStarterManifest(t *testing.T) {
	root := t.TempDir()
	manifest := snapWriteStarterManifest(t, root, "echo fixture-build")
	ag := snapGuardAgent(t, root, &configuration.VerificationConfig{Enabled: true})

	_, err := handleEditFile(context.Background(), ag, map[string]interface{}{
		"path":    manifest,
		"old_str": `"echo fixture-build"`,
		"new_str": `"true"`,
	})
	require.Error(t, err, "the edit-file handler must refuse a manifest edit while verification is enabled")
	assert.Contains(t, err.Error(), "trusted source")
	assert.Equal(t, "echo fixture-build", snapStarterManifestBuild(t, root), "the manifest must be left untouched")
}

// TestHandleWriteFileAllowsStarterManifestWhenDisabled verifies the guard is a
// no-op when verification is disabled: the identical manifest write succeeds
// as before.
func TestHandleWriteFileAllowsStarterManifestWhenDisabled(t *testing.T) {
	root := t.TempDir()
	manifest := snapWriteStarterManifest(t, root, "echo fixture-build")
	ag := snapGuardAgent(t, root, nil)

	// Pre-read the manifest so the write passes the (unrelated) staleness rule.
	ag.RecordFileReadThisTurn(manifest)

	_, err := handleWriteFile(context.Background(), ag, map[string]interface{}{
		"path":    manifest,
		"content": `{"starter":{"id":"fixture","version":"1.0.0"},"build":"echo now-passing"}`,
	})
	require.NoError(t, err, "the manifest write must succeed when verification is disabled")
	assert.Equal(t, "echo now-passing", snapStarterManifestBuild(t, root), "the manifest must be updated")
}

// ---------------------------------------------------------------------------
// The anchor test (the full turn, the live tool path)
// ---------------------------------------------------------------------------

// TestVerificationSnapshot_ModelCannotChangeCommands is the §149b anchor: a
// scripted turn whose repair round tries to rewrite the manifest's build
// command to a trivially-passing one. The guard refuses the write and the
// turn-start snapshot keeps the gate — the turn ends FAILED on the original
// command, and the manifest on disk is untouched.
func TestVerificationSnapshot_ModelCannotChangeCommands(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	snapWriteStarterManifest(t, root, "echo fixture-broken-build; exit 1")

	const (
		turnAnswer = "I made the change."
		repairOne  = "Repair one: I tried to change the build command."
	)
	// The repair round's tool call rewrites the manifest's build to a
	// trivially-passing command — the "cheat" the guard must refuse.
	manifestPath := starterstore.StarterManifestPath(root)
	repairToolCall := NewScriptedToolCallResponse(
		"vh_wf_snap_manifest",
		"write_file",
		fmt.Sprintf(`{"path":%q,"content":%q}`, manifestPath, `{"starter":{"id":"fixture","version":"1.0.0"},"build":"true"}`),
		"Changing the build command so it passes.",
	)
	client := NewScriptedClient(
		vhWriteToolCall(t, root, "src/app.js"),
		NewScriptedTextResponse(turnAnswer),
		repairToolCall,
		NewScriptedTextResponse(repairOne),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: 1})

	result, err := ag.ProcessQuery("Fix the app so the build passes.")
	require.NoError(t, err)

	// The guard refused the manifest write: the manifest on disk still has
	// the original failing build command.
	assert.Equal(t, "echo fixture-broken-build; exit 1", snapStarterManifestBuild(t, root),
		"the manifest must be left untouched (the guard refuses the mid-turn rewrite)")

	// The stored verification used the turn-start snapshot (the original
	// failing command), not the model's "true": the build check ran the
	// original command and failed.
	res := ag.LastVerificationResult()
	require.NotNil(t, res, "a verification result must be stored")
	require.True(t, res.Baseline, "no plan is present, so the run is baseline mode (gated on the manifest's commands)")
	foundBuild := false
	for i := range res.Checks {
		if res.Checks[i].Kind == plancontract.KindBuild {
			foundBuild = true
			if res.Checks[i].Passed {
				t.Errorf("the build check passed, want it to fail (the snapshot's command is the failing one)")
			}
		}
	}
	require.True(t, foundBuild, "the stored result must carry the build check")

	// 149.6: the turn ended FAILED — the final reply carries the §149d
	// failure report (what fails, what was tried), not a clean success.
	const want = repairOne + "\n\n" + "Verification: FAILED after the stopping rule (1 repair attempts)\n" +
		"Passed: none\n" +
		"Failed: build — command failed\n" +
		"Tried: build: 1/1 repair attempts"
	assert.Equal(t, want, result, "the final reply must carry the FAILED verification attachment")

	// Model calls: the turn's tool-call iteration + answer, plus exactly one
	// repair round (the manifest write and its answer).
	if calls := len(client.GetSentRequests()); calls != 4 {
		t.Errorf("model calls = %d, want 4 (the turn + 1 repair round)", calls)
	}
}

// ---------------------------------------------------------------------------
// Defense-in-depth, plan side (the full turn, the live tool path)
// ---------------------------------------------------------------------------

// TestVerificationSnapshot_PlanTamperStillGates verifies the plan-side
// defense: a scripted repair round that overwrites .sprout/plan.json (the
// plan is NOT refused by the guard) must still leave the turn-start
// acceptance snapshot as the gate — the verification runs the snapshot's plan
// acceptance (the build item), not the model's rewritten plan.
func TestVerificationSnapshot_PlanTamperStillGates(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	snapWriteStarterManifest(t, root, "echo fixture-broken-build; exit 1")

	// The plan's acceptance is a build item (a1): in plan mode the build
	// check covers a1 and runs the manifest's (failing) build command.
	plan := plancontract.New("Add login", time.Now())
	plan.Scope = []plancontract.ScopeItem{{ID: "s1", Title: "Auth"}}
	plan.Steps = []plancontract.Step{{Scope: "s1", Description: "Implement login"}}
	plan.Acceptance = []plancontract.Acceptance{{ID: "a1", Scope: "s1", Kind: plancontract.KindBuild}}
	storedPlan, err := planstore.New().Save(root, plan)
	require.NoError(t, err, "save the plan fixture")
	planRevision := storedPlan.Revision

	const (
		turnAnswer = "I made the change."
		repairOne  = "Repair one: I rewrote the plan to drop the build gate."
	)
	// The repair round's tool call overwrites the plan, replacing the build
	// item (a1) with a manual item (a2) — loosening the gate. The plan is
	// NOT guarded, so the write succeeds; the snapshot is what still gates.
	planPath := filepath.Join(root, ".sprout", "plan.json")
	tamperedPlan := `{"version":1,"revision":2,"created":"2024-01-01T00:00:00Z","updated":"2024-01-01T00:00:00Z","goal":"Add login","scope":[{"id":"s1","title":"Auth"}],"steps":[{"scope":"s1","description":"Implement login"}],"acceptance":[{"id":"a2","scope":"s1","kind":"manual","check":"verify session persists"}],"out_of_scope":[]}`
	repairToolCall := NewScriptedToolCallResponse(
		"vh_wf_snap_plan",
		"write_file",
		fmt.Sprintf(`{"path":%q,"content":%q}`, planPath, tamperedPlan),
		"Rewriting the plan to drop the build gate.",
	)
	client := NewScriptedClient(
		vhWriteToolCall(t, root, "src/app.js"),
		NewScriptedTextResponse(turnAnswer),
		repairToolCall,
		NewScriptedTextResponse(repairOne),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: 1})

	result, err := ag.ProcessQuery("Add the login flow.")
	require.NoError(t, err)

	// The plan write succeeded (the plan is not guarded): the plan on disk
	// now carries the model's manual item, not the original build item.
	planData, err := os.ReadFile(planPath)
	require.NoError(t, err)
	assert.Contains(t, string(planData), `"kind":"manual"`, "the plan write must succeed (the plan is not a verification source)")
	assert.NotContains(t, string(planData), `"kind":"build"`, "the plan on disk must be the model's rewritten plan")

	// But the verification still gates on the turn-start snapshot's plan
	// acceptance (the build item a1): plan mode, the original revision, and
	// the build check covering a1 still runs and fails.
	res := ag.LastVerificationResult()
	require.NotNil(t, res, "a verification result must be stored")
	require.False(t, res.Baseline, "the run must be plan mode (gated on the snapshot's plan)")
	assert.Equal(t, planRevision, res.PlanRevision, "the run must use the turn-start plan revision, not the model's rewrite")
	require.Len(t, res.Checks, 1, "the snapshot's plan declares only the build check")
	assert.Equal(t, plancontract.KindBuild, res.Checks[0].Kind, "the build check must still run (covering the snapshot's a1)")
	assert.Equal(t, []string{"a1"}, res.Checks[0].Items, "the build check must cover the snapshot's a1")
	assert.False(t, res.Checks[0].Passed, "the build check (the snapshot's command) must still fail")
	assert.True(t, res.Failed(), "the run must fail (the gate holds on the snapshot)")

	// 149.6: the turn ended FAILED.
	assert.Contains(t, result, "Verification: FAILED", "the final reply must carry the FAILED verification attachment")
}
