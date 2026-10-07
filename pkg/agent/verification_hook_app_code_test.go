//go:build !js

// verification_hook_app_code_test.go — the verification gate's
// application-code filter, end to end through a full agent turn: a turn
// that changed only documentation or .sprout bookkeeping must be a
// complete no-op for the turn-end hook (no verify run, no repair round,
// no stored result, no reply attachment), and a turn that changed an
// application-code file alongside docs must still run. Fixtures mirror
// verification_hook_test.go (vhAgent, vhWriteStarterManifest, the
// scripted client); the broken build command proves the hook either ran
// (the reply carries the failure attachment) or did not (the reply is
// byte-identical to the model's answer).

package agent

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/starterstore"
)

// vhacWriteToolCall is vhWriteToolCall generalized: a scripted write_file
// tool call for any workspace path, so a test can script the exact mix of
// docs and application-code writes its gate case needs.
func vhacWriteToolCall(t *testing.T, root, path string) *ScriptedResponse {
	t.Helper()
	args := fmt.Sprintf(`{"path":%q,"content":"changed"}`, filepath.Join(root, path))
	return NewScriptedToolCallResponse("vhac_wf_1", "write_file", args, "Writing the file.")
}

// TestVerificationHook_DocsOnlyTurnSkipsVerification pins the docs
// half of the app-code rule: a turn that changed only markdown never starts a build, even
// though the starter manifest carries a failing one — the reply is
// byte-identical to the model's answer, no extra model call happens, and
// no result is stored or attached.
func TestVerificationHook_DocsOnlyTurnSkipsVerification(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, "echo fixture-broken-build; exit 1")

	const turnAnswer = "Docs updated."
	client := NewScriptedClient(
		vhacWriteToolCall(t, root, "README.md"),
		NewScriptedTextResponse(turnAnswer),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: 2})

	result, err := ag.ProcessQuery("Update the README.")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if result != turnAnswer {
		t.Errorf("result = %q, want the untouched turn answer %q (a docs-only turn must not run verification)", result, turnAnswer)
	}
	if calls := len(client.GetSentRequests()); calls != 2 {
		t.Errorf("model calls = %d, want 2 (the turn only: no verify run, no repair round)", calls)
	}
	if reports := vhReportMessages(ag); len(reports) != 0 {
		t.Errorf("verification-report messages = %d, want 0 (nothing ran)", len(reports))
	}
	if res := ag.LastVerificationResult(); res != nil {
		t.Errorf("LastVerificationResult = %+v, want nil (the hook never ran)", res)
	}
	if tv := ag.currentTurnVerification(); tv.result != nil {
		t.Errorf("stored verification state = %+v, want empty (no attachment may exist)", tv)
	}
}

// TestVerificationHook_SproutBookkeepingOnlyTurnSkipsVerification pins
// the bookkeeping half of the app-code rule: a turn that changed only
// .sprout paths (the plan here; the starter manifest is off-limits to the
// model by the write guard) is also a no-op for the hook.
func TestVerificationHook_SproutBookkeepingOnlyTurnSkipsVerification(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, "echo fixture-broken-build; exit 1")

	const turnAnswer = "Plan bookkeeping updated."
	client := NewScriptedClient(
		vhacWriteToolCall(t, root, filepath.Join(".sprout", "plan.json")),
		NewScriptedTextResponse(turnAnswer),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: 2})

	result, err := ag.ProcessQuery("Refresh the plan bookkeeping.")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if result != turnAnswer {
		t.Errorf("result = %q, want the untouched turn answer %q (a .sprout-only turn must not run verification)", result, turnAnswer)
	}
	if calls := len(client.GetSentRequests()); calls != 2 {
		t.Errorf("model calls = %d, want 2 (the turn only: no verify run, no repair round)", calls)
	}
	if reports := vhReportMessages(ag); len(reports) != 0 {
		t.Errorf("verification-report messages = %d, want 0 (nothing ran)", len(reports))
	}
	if res := ag.LastVerificationResult(); res != nil {
		t.Errorf("LastVerificationResult = %+v, want nil (the hook never ran)", res)
	}
	if tv := ag.currentTurnVerification(); tv.result != nil {
		t.Errorf("stored verification state = %+v, want empty (no attachment may exist)", tv)
	}
}

// TestVerificationHook_DocsFileDoesNotSuppressVerification pins the
// filter's fail-open direction: one application-code change alongside
// docs changes still opens the gate — the docs path must not suppress
// the run. The manifest's build fails, so the reply must carry the
// failure attachment exactly as a code-only turn would.
func TestVerificationHook_DocsFileDoesNotSuppressVerification(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, "echo fixture-broken-build; exit 1")

	const turnAnswer = "Code and docs changed."
	client := NewScriptedClient(
		vhacWriteToolCall(t, root, "README.md"),
		vhacWriteToolCall(t, root, "src/app.js"),
		NewScriptedTextResponse(turnAnswer),
		NewScriptedTextResponse("Repair one: I looked into the build."),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: 1})

	result, err := ag.ProcessQuery("Update the code and the README.")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if !strings.Contains(result, "Verification: FAILED") {
		t.Errorf("result = %q, want the verification attachment (the code change must still run verification)", result)
	}
	if !strings.Contains(result, "Failed: build") {
		t.Errorf("result = %q, want the failing build check in the attachment", result)
	}
	// The build's own output landed in the repair-round report: the run
	// executed the manifest's (broken) build command.
	reports := vhReportMessages(ag)
	if len(reports) != 1 {
		t.Fatalf("verification-report messages = %d, want 1 (one repair round)", len(reports))
	}
	if !strings.Contains(reports[0], "fixture-broken-build") {
		t.Errorf("report = %q, want the failing build's evidence", reports[0])
	}
	if res := ag.LastVerificationResult(); res == nil || !res.Failed() {
		t.Errorf("LastVerificationResult = %+v, want the stored failing run", res)
	}
	// The turn's two writes + answer, plus the one repair round.
	if calls := len(client.GetSentRequests()); calls != 4 {
		t.Errorf("model calls = %d, want 4 (the turn + one repair round)", calls)
	}
}

// TestVerificationHook_DocsOnlyTurnReportsNoCodeChangesReason pins the
// not-verified reason on the same fixture: with verification enabled, a
// docs-only turn's progress_complete carries "no code changes this turn"
// — not "verification did not run this turn".
func TestVerificationHook_DocsOnlyTurnReportsNoCodeChangesReason(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, "echo fixture-broken-build; exit 1")

	client := NewScriptedClient(
		vhacWriteToolCall(t, root, "docs/guide.md"),
		NewScriptedTextResponse("Docs updated."),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: 2})

	if _, err := ag.ProcessQuery("Update the docs guide."); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if got := ag.notVerifiedReason(); got != "no code changes this turn" {
		t.Errorf("notVerifiedReason = %q, want %q (docs-only reads as no code changes)", got, "no code changes this turn")
	}
	// The raw window still holds the docs path — the reason reads the
	// filtered one.
	if got := ag.TurnChangedPaths(); len(got) == 0 {
		t.Fatalf("TurnChangedPaths = %v, want the docs path (the raw window is unfiltered)", got)
	}
}

// TestNotVerifiedReason_DocsOnlyBranch pins the filtered-window read of
// the not-verified reason at unit level: verification enabled, the turn
// window holds docs and .sprout paths only → "no code changes this
// turn"; adding one application-code path flips it to "verification did
// not run this turn".
func TestNotVerifiedReason_DocsOnlyBranch(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.Verification = &configuration.VerificationConfig{Enabled: true}
		return nil
	}); err != nil {
		t.Fatalf("configure verification: %v", err)
	}

	newAgentWithTracker := func() *Agent {
		ag := NewTestAgent()
		ag.configManager = mgr
		tracker := NewChangeTracker(nil, "vhac-reason")
		tracker.MarkTurnStart()
		ag.changeTracker = tracker
		return ag
	}

	agDocs := newAgentWithTracker()
	for _, p := range []string{"/ws/README.md", "/ws/docs/guide.md", "/ws/.sprout/plan.json"} {
		if err := agDocs.changeTracker.TrackFileWriteState(p, "old", "new", true); err != nil {
			t.Fatalf("TrackFileWriteState(%q): %v", p, err)
		}
	}
	if got := agDocs.notVerifiedReason(); got != "no code changes this turn" {
		t.Errorf("docs-only reason = %q, want %q", got, "no code changes this turn")
	}

	agMixed := newAgentWithTracker()
	for _, p := range []string{"/ws/README.md", "/ws/src/app.go"} {
		if err := agMixed.changeTracker.TrackFileWriteState(p, "old", "new", true); err != nil {
			t.Fatalf("TrackFileWriteState(%q): %v", p, err)
		}
	}
	if got := agMixed.notVerifiedReason(); got != "verification did not run this turn" {
		t.Errorf("mixed reason = %q, want %q (the code path must still read as a code turn)", got, "verification did not run this turn")
	}
}

// TestVerificationHook_ManifestPathInsideProjectStillSkips pins the
// real-world path shape: the manifest lives at <root>/.sprout/starter.json
// and the tracker records absolute paths, so the filter's segment match
// must hold on absolute paths (the fixture's own manifest write is not
// model-made, so it never enters the turn window — this test drives the
// classification directly).
func TestVerificationHook_ManifestPathInsideProjectStillSkips(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, "echo fixture-broken-build; exit 1")

	if IsApplicationCodePath(starterstore.StarterManifestPath(root)) {
		t.Errorf("IsApplicationCodePath(%q) = true, want false (the manifest is bookkeeping)", starterstore.StarterManifestPath(root))
	}
}
