//go:build !js

// verification_hook_test.go — the turn-end verification acceptance tests: the
// turn-end verification hook drives a full agent turn (scripted model,
// real workspace fixture, real shell execution of the manifest's build
// command) and pins the gate, the repair loop, and the final-reply
// contract:
//
//   - a turn that changed a file runs the project's verification;
//   - a broken build → the structured report continues the turn, the
//     loop stops after N repair attempts on the same check (N repair
//     rounds, a third never happens), and the final reply carries the
//     failure report (what passes, what fails, what was tried);
//   - a passing build needs no repair round and the final reply carries
//     the passing result;
//   - an all-skipped run verifies nothing, so the reply states that no
//     passing result exists;
//   - disabled verification (the default) and a no-change turn are
//     complete no-ops (no verify run, no extra model call, no stored
//     result, no attachment — the reply is byte-identical);
//   - a previous turn's stored verification state never attaches to a
//     later turn's reply (the per-turn reset);
//   - a runner setup error never gates the turn; a provider error in a
//     repair round is propagated to handleQueryResult's classification
//     (and never carries the attachment).
//
// The fixture runs the build command through the default executor
// (sh -c), so every test here skips rather than fails where sh is
// absent (mirroring shAvailable in pkg/verify). The pure report-builder,
// attempt-key, and attachment-renderer tests live in
// verification_hook_unit_test.go.

package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/starterstore"
)

// ---------------------------------------------------------------------------
// Fixtures and helpers
// ---------------------------------------------------------------------------

// vhShAvailable mirrors pkg/verify's shAvailable: the fixture's build
// command runs through the default executor (sh -c), so the tests skip
// where sh does not exist (non-UNIX dev machines).
func vhShAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh available on this platform (the fixture build command runs through sh -c)")
	}
}

// vhWriteStarterManifest writes the project's starter manifest fixture
// with the given build command and no test command, so a
// baseline verification run executes the build and skips the test check.
func vhWriteStarterManifest(t *testing.T, root, buildCommand string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".sprout"), 0o755); err != nil {
		t.Fatalf("mkdir .sprout: %v", err)
	}
	manifest := fmt.Sprintf(`{"starter":{"id":"fixture","version":"1.0.0"},"build":%q}`, buildCommand)
	if err := os.WriteFile(starterstore.StarterManifestPath(root), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write starter manifest: %v", err)
	}
}

// vhWriteBareStarterManifest writes a starter manifest with no commands at
// all: a baseline verification run skips every check (no trusted command
// anywhere), so the run verifies nothing and reports no passing result.
func vhWriteBareStarterManifest(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".sprout"), 0o755); err != nil {
		t.Fatalf("mkdir .sprout: %v", err)
	}
	manifest := `{"starter":{"id":"fixture","version":"1.0.0"}}`
	if err := os.WriteFile(starterstore.StarterManifestPath(root), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write starter manifest: %v", err)
	}
}

// vhAgent wires a scripted client into a fresh agent whose workspace root
// is the given project directory ("" pins an empty root for the runner
// setup-error test). ver (nil = unset, the default-off path) sets the
// verification section. Full context mode keeps the file tools on the
// roster; SkipPrompt keeps the turn from blocking on an interactive
// prompt (stdin is closed in tests).
func vhAgent(t *testing.T, client *ScriptedClient, root string, ver *configuration.VerificationConfig) *Agent {
	t.Helper()

	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.ContextMode = configuration.ContextModeFull
		cfg.SkipPrompt = true
		cfg.Verification = ver
		return nil
	}); err != nil {
		t.Fatalf("configure test agent: %v", err)
	}

	ag, err := NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient: %v", err)
	}
	t.Cleanup(ag.Shutdown)
	ag.SetMaxIterations(10)
	ag.SetWorkspaceRoot(root)
	return ag
}

// vhWriteToolCall is the scripted tool call that changes a file in the
// workspace (so the change tracker records the turn's change and the
// hook's gate opens): write_file on a new file under the project root.
func vhWriteToolCall(t *testing.T, root, path string) *ScriptedResponse {
	t.Helper()
	args := fmt.Sprintf(`{"path":%q,"content":"function main() { return 1; }"}`, filepath.Join(root, path))
	return NewScriptedToolCallResponse("vh_wf_1", "write_file", args, "Writing the file.")
}

// vhReportMessages returns the user-role transcript messages carrying a
// <verification-report> envelope, in order (one per repair round).
func vhReportMessages(ag *Agent) []string {
	var reports []string
	for _, m := range ag.GetMessages() {
		if m.Role == "user" && strings.Contains(m.Content, "<verification-report>") {
			reports = append(reports, m.Content)
		}
	}
	return reports
}

// ---------------------------------------------------------------------------
// (a) Broken build, N=2 → the loop runs and stops at N
// ---------------------------------------------------------------------------

func TestVerificationHook_BrokenBuildLoopRunsAndStopsAtN(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, "echo fixture-broken-build; exit 1")

	const (
		turnAnswer     = "I made the change."
		repairOne      = "Repair one: I tried to fix the build."
		repairTwoFinal = "The build still fails; the remaining failure is fixture-broken-build."
	)
	client := NewScriptedClient(
		vhWriteToolCall(t, root, "src/app.js"),
		NewScriptedTextResponse(turnAnswer),
		NewScriptedTextResponse(repairOne),
		NewScriptedTextResponse(repairTwoFinal),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: 2})

	result, err := ag.ProcessQuery("Implement the app entry point.")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	// The stopping rule fired (N=2): the final reply states
	// plainly what passes (nothing), what fails (the build), and what
	// was tried (2/2 repair attempts). Partial success
	// is never reported as success.
	const want = repairTwoFinal + "\n\n" + "Verification: FAILED after the stopping rule (2 repair attempts)\n" +
		"Passed: none\n" +
		"Failed: build — command failed\n" +
		"Tried: build: 2/2 repair attempts"
	if result != want {
		t.Errorf("result = %q,\nwant %q (the last repair-round answer with the failure report)", result, want)
	}

	// Model calls: the turn's tool-call iteration + the turn's answer,
	// plus exactly two repair rounds. A third repair round would consume
	// a fifth call.
	if calls := len(client.GetSentRequests()); calls != 4 {
		t.Errorf("model calls = %d, want 4 (the turn + 2 repair rounds)", calls)
	}

	// The transcript carries exactly two <verification-report> user
	// messages — one per repair round — with the failing check, the
	// attempts used, and (in the second) the DONE instruction.
	reports := vhReportMessages(ag)
	if len(reports) != 2 {
		t.Fatalf("verification-report user messages = %d, want 2 (one per repair round)", len(reports))
	}
	for _, want := range []string{"<verification-report>", "build", "1/2", "fixture-broken-build"} {
		if !strings.Contains(reports[0], want) {
			t.Errorf("first report missing %q:\n%s", want, reports[0])
		}
	}
	for _, want := range []string{"<verification-report>", "2/2", "DONE"} {
		if !strings.Contains(reports[1], want) {
			t.Errorf("second report missing %q:\n%s", want, reports[1])
		}
	}

	// The stored result is the last (still failing) verification run:
	// the build check failed, the baseline test check skipped itself.
	res := ag.LastVerificationResult()
	if res == nil {
		t.Fatal("LastVerificationResult = nil, want the last failing run")
	}
	if !res.Failed() {
		t.Error("LastVerificationResult().Failed() = false, want true (the build never passed)")
	}
	if len(res.Checks) != 2 {
		t.Fatalf("stored result has %d checks, want 2 (build + baseline test)", len(res.Checks))
	}
	if res.Checks[0].Kind != plancontract.KindBuild || res.Checks[0].Passed {
		t.Errorf("stored build check = %+v, want failed", res.Checks[0])
	}
	if res.Checks[1].Kind != plancontract.KindTest || !res.Checks[1].Skipped {
		t.Errorf("stored test check = %+v, want skipped (no test command)", res.Checks[1])
	}
}

// ---------------------------------------------------------------------------
// (b) Passing build → verification runs once and passes, no repair
// ---------------------------------------------------------------------------

func TestVerificationHook_PassingBuildNeedsNoRepair(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, "echo fixture-build-ok")

	const turnAnswer = "Done, the build passes."
	client := NewScriptedClient(
		vhWriteToolCall(t, root, "src/app.js"),
		NewScriptedTextResponse(turnAnswer),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: 2})

	result, err := ag.ProcessQuery("Implement the app entry point.")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	// The final reply carries the passing verification result —
	// success may be reported only with a passing result attached.
	// The attachment is the stable block: a blank line,
	// then the "passed" line with the run's summary.
	const want = turnAnswer + "\n\n" + "Verification: passed — baseline: build: passed (echo fixture-build-ok); test: skipped — no test command available: set the starter manifest or the project's explicit verification configuration"
	if result != want {
		t.Errorf("result = %q,\nwant %q (the turn answer with the passing attachment)", result, want)
	}

	// No repair round: only the turn's tool-call iteration + answer.
	if calls := len(client.GetSentRequests()); calls != 2 {
		t.Errorf("model calls = %d, want 2 (the turn only, no repair round)", calls)
	}
	if reports := vhReportMessages(ag); len(reports) != 0 {
		t.Errorf("verification-report messages = %d, want 0 (nothing failed)", len(reports))
	}

	res := ag.LastVerificationResult()
	if res == nil {
		t.Fatal("LastVerificationResult = nil, want the passing run")
	}
	if !res.Passed() {
		t.Errorf("LastVerificationResult().Passed() = false, want true (the build ran and passed)")
	}
}

// ---------------------------------------------------------------------------
// (c) Disabled (the default) → a complete no-op
// ---------------------------------------------------------------------------

func TestVerificationHook_DisabledByDefaultIsNoOp(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, "echo fixture-broken-build; exit 1")

	const turnAnswer = "Done."
	client := NewScriptedClient(
		vhWriteToolCall(t, root, "src/app.js"),
		NewScriptedTextResponse(turnAnswer),
	)
	// No verification section: the default-off path — a
	// broken build must be invisible to the hook.
	ag := vhAgent(t, client, root, nil)

	result, err := ag.ProcessQuery("Implement the app entry point.")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if result != turnAnswer {
		t.Errorf("result = %q, want the untouched turn answer %q", result, turnAnswer)
	}
	if calls := len(client.GetSentRequests()); calls != 2 {
		t.Errorf("model calls = %d, want 2 (verification disabled: no repair round)", calls)
	}
	if reports := vhReportMessages(ag); len(reports) != 0 {
		t.Errorf("verification-report messages = %d, want 0 (the hook is a no-op)", len(reports))
	}
	if res := ag.LastVerificationResult(); res != nil {
		t.Errorf("LastVerificationResult = %+v, want nil (the hook never ran)", res)
	}
	if tv := ag.currentTurnVerification(); tv.result != nil {
		t.Errorf("stored verification state = %+v, want empty (verification disabled: no attachment may exist)", tv)
	}
}

// ---------------------------------------------------------------------------
// (d) No file change → the hook does not run
// ---------------------------------------------------------------------------

func TestVerificationHook_NoFileChangeSkipsVerification(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, "echo fixture-broken-build; exit 1")

	const turnAnswer = "Nothing to change here."
	client := NewScriptedClient(NewScriptedTextResponse(turnAnswer))
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: 2})

	result, err := ag.ProcessQuery("Is there anything to fix?")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if result != turnAnswer {
		t.Errorf("result = %q, want the untouched turn answer %q", result, turnAnswer)
	}

	// The text-only turn recorded no change, so the hook's gate
	// (TurnChangedPaths) is empty: one model call, no verify run, no
	// stored result.
	if calls := len(client.GetSentRequests()); calls != 1 {
		t.Errorf("model calls = %d, want 1 (the hook is a no-op: the turn changed no code)", calls)
	}
	if reports := vhReportMessages(ag); len(reports) != 0 {
		t.Errorf("verification-report messages = %d, want 0 (no code change, no verify run)", len(reports))
	}
	if res := ag.LastVerificationResult(); res != nil {
		t.Errorf("LastVerificationResult = %+v, want nil (the hook never ran)", res)
	}
	if tv := ag.currentTurnVerification(); tv.result != nil {
		t.Errorf("stored verification state = %+v, want empty (no code change: no attachment may exist)", tv)
	}
}

// ---------------------------------------------------------------------------
// (e) Stop rule → exactly N repair rounds, never a third
// ---------------------------------------------------------------------------

func TestVerificationHook_StopRuleCapsRepairRoundsAtN(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, "echo fixture-broken-build; exit 1")

	client := NewScriptedClient(
		vhWriteToolCall(t, root, "src/app.js"),
		NewScriptedTextResponse("Turn answer."),
		NewScriptedTextResponse("Repair one."),
		NewScriptedTextResponse("Repair two."),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: 2})

	if _, err := ag.ProcessQuery("Implement the app entry point."); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	// The stopping rule: after N=2 repair attempts on the
	// same failing check the loop stops — exactly two repair rounds,
	// never a third. The scripted client carries exactly the four
	// responses a correct run consumes: a third repair round would send
	// a fifth request and the assertions below would catch it.
	if calls := len(client.GetSentRequests()); calls != 4 {
		t.Errorf("model calls = %d, want 4 (exactly two repair rounds; a third must not happen)", calls)
	}
	if reports := vhReportMessages(ag); len(reports) != 2 {
		t.Errorf("verification-report messages = %d, want 2 (one per repair round)", len(reports))
	}

	// The stored result is the last verification run — still failing.
	res := ag.LastVerificationResult()
	if res == nil || !res.Failed() {
		t.Fatalf("LastVerificationResult = %+v, want the last failing run", res)
	}
}

// ---------------------------------------------------------------------------
// The final-reply contract
// ---------------------------------------------------------------------------

// TestVerificationHook_AllSkippedRunReportsNoPassingResult pins the all-skipped
// rule: a run that verified nothing (every check skipped, no trusted command
// anywhere) is neither a pass nor a failure — the reply states that no
// passing result exists, and success is never reported without one.
func TestVerificationHook_AllSkippedRunReportsNoPassingResult(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteBareStarterManifest(t, root)

	const turnAnswer = "I made the change."
	client := NewScriptedClient(
		vhWriteToolCall(t, root, "src/app.js"),
		NewScriptedTextResponse(turnAnswer),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: 2})

	result, err := ag.ProcessQuery("Implement the app entry point.")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	// The run stored itself (it ran, it verified nothing): neither
	// Passed() nor Failed().
	res := ag.LastVerificationResult()
	if res == nil {
		t.Fatal("LastVerificationResult = nil, want the all-skipped run")
	}
	if res.Passed() || res.Failed() {
		t.Fatalf("all-skipped run reports Passed()=%v Failed()=%v, want both false", res.Passed(), res.Failed())
	}

	// The reply carries the no-passing-result attachment — not the
	// passing one — listing what could not be verified.
	const want = turnAnswer + "\n\n" + "Verification: ran, but no checks applied (all skipped) — no passing result\n" +
		"Skipped: build — no build command available: set the starter manifest or the project's explicit verification configuration\n" +
		"Skipped: test — no test command available: set the starter manifest or the project's explicit verification configuration"
	if result != want {
		t.Errorf("result = %q,\nwant %q (the no-passing-result attachment)", result, want)
	}
}
