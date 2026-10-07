//go:build !js

// quality_hook_test.go — the quality-after-edits acceptance tests: the
// turn-end quality hook drives a full agent turn (scripted model, real
// workspace fixture, real shell execution of the manifest's formatter and
// linter) and pins the gate and the repair loop:
//
//   - a turn that changed a file runs the project's formatter and linter; a
//     seeded lint violation is detected and the repair round fixes it (the
//     scripted test with a seeded lint violation);
//   - a formatter that rewrites a file is a runtime repair (the format check
//     reports the change) and the linter still runs;
//   - quality disabled (the default) is a complete no-op (no command runs, no
//     extra model call, no stored result);
//   - a formatter failure does not crash the turn: the failure is reported,
//     the linter still runs, and the turn completes.
//
// The fixture runs the commands through the default executor (sh -c), so
// every test here skips rather than fails where sh is absent (mirroring
// shAvailable in pkg/verify).

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
	"github.com/sprout-foundry/sprout/pkg/verify"
)

// ---------------------------------------------------------------------------
// Fixtures and helpers
// ---------------------------------------------------------------------------

// qhShAvailable mirrors pkg/verify's shAvailable: the fixture commands run
// through the default executor (sh -c), so the tests skip where sh does not
// exist.
func qhShAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh available on this platform (the fixture commands run through sh -c)")
	}
}

// qhWriteStarterManifest writes a starter manifest fixture that declares the
// given formatter and linter commands ("" omits the field).
func qhWriteStarterManifest(t *testing.T, root, format, lint string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".sprout"), 0o755); err != nil {
		t.Fatalf("mkdir .sprout: %v", err)
	}
	manifest := fmt.Sprintf(`{"starter":{"id":"fixture","version":"1.0.0"},"format":%q,"lint":%q}`, format, lint)
	if err := os.WriteFile(filepath.Join(root, ".sprout", "starter.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write starter manifest: %v", err)
	}
}

// qhAgent wires a scripted client into a fresh agent whose workspace root is
// the given project directory, with the quality section set (nil = unset, the
// default-off path). Verification is left off so the two turn-end hooks are
// independent.
func qhAgent(t *testing.T, client *ScriptedClient, root string, quality *configuration.QualityConfig) *Agent {
	t.Helper()

	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.ContextMode = configuration.ContextModeFull
		cfg.SkipPrompt = true
		cfg.Quality = quality
		// The quality loop reuses the verification repair limits; pin one
		// repair round so the tests are deterministic (verification itself
		// stays off: Enabled is false).
		cfg.Verification = &configuration.VerificationConfig{RepairAttempts: 1}
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

// qhWriteFileCall is a scripted write_file tool call that writes content to a
// path under the project root (so the change tracker records the turn's
// change and the hook's gate opens).
func qhWriteFileCall(t *testing.T, root, path, content string) *ScriptedResponse {
	t.Helper()
	args := fmt.Sprintf(`{"path":%q,"content":%q}`, filepath.Join(root, path), content)
	return NewScriptedToolCallResponse("qh_wf_1", "write_file", args, "Writing the file.")
}

// qhReportMessages returns the user-role transcript messages carrying a
// <quality-report> envelope, in order (one per repair round).
func qhReportMessages(ag *Agent) []string {
	var reports []string
	for _, m := range ag.GetMessages() {
		if m.Role == "user" && strings.Contains(m.Content, "<quality-report>") {
			reports = append(reports, m.Content)
		}
	}
	return reports
}

// ---------------------------------------------------------------------------
// (a) Seeded lint violation → detected, repaired in the same turn
// ---------------------------------------------------------------------------

func TestQualityHook_SeededLintViolationIsRepaired(t *testing.T) {
	qhShAvailable(t)
	root := t.TempDir()

	// The linter fails while the violation marker is present: `! grep -q BAD
	// app.js`. The formatter is a no-op that always passes.
	qhWriteStarterManifest(t, root, "exit 0", "! grep -q BAD app.js")

	const (
		turnAnswer  = "I added the file."
		repairFinal = "Fixed the lint violation."
	)
	// The turn writes a file carrying the seeded violation; the repair round
	// rewrites it clean.
	client := NewScriptedClient(
		qhWriteFileCall(t, root, "app.js", "var x = 1; // BAD\n"),
		NewScriptedTextResponse(turnAnswer),
		qhWriteFileCall(t, root, "app.js", "var x = 1;\n"),
		NewScriptedTextResponse(repairFinal),
	)
	ag := qhAgent(t, client, root, &configuration.QualityConfig{Enabled: true})

	result, err := ag.ProcessQuery("Add app.js.")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if result != repairFinal {
		t.Errorf("result = %q, want the repair round's answer %q", result, repairFinal)
	}

	// The turn + its answer + one repair round (the tool call and answer) is
	// the whole run: the linter passed after the repair, so no second repair
	// round.
	if calls := len(client.GetSentRequests()); calls != 4 {
		t.Errorf("model calls = %d, want 4 (the turn plus one repair round)", calls)
	}

	// One <quality-report> user message, naming the failing lint step and the
	// violation evidence.
	reports := qhReportMessages(ag)
	if len(reports) != 1 {
		t.Fatalf("quality-report messages = %d, want 1 (one repair round)", len(reports))
	}
	for _, want := range []string{"<quality-report>", "lint", "grep -q BAD"} {
		if !strings.Contains(reports[0], want) {
			t.Errorf("quality report missing %q:\n%s", want, reports[0])
		}
	}

	// The stored result is the last (passing) quality run: the linter passed
	// after the repair.
	res := ag.LastQualityResult()
	if res == nil {
		t.Fatal("LastQualityResult = nil, want the last run")
	}
	if !res.Passed() {
		t.Errorf("LastQualityResult().Passed() = false, want true; checks: %+v", res.Checks)
	}

	// The seeded violation is gone from disk: the repair actually fixed it.
	data, err := os.ReadFile(filepath.Join(root, "app.js"))
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	if strings.Contains(string(data), "BAD") {
		t.Errorf("app.js still contains the violation marker: %q", data)
	}
}

// ---------------------------------------------------------------------------
// (b) Formatter rewrites a file → a runtime repair, linter still runs
// ---------------------------------------------------------------------------

func TestQualityHook_FormatterChangeIsRuntimeRepair(t *testing.T) {
	qhShAvailable(t)
	root := t.TempDir()

	// The formatter rewrites BADFMT away (a real in-place repair); the linter
	// always passes.
	qhWriteStarterManifest(t, root, "printf 'formatted\\n' > app.js", "exit 0")

	client := NewScriptedClient(
		qhWriteFileCall(t, root, "app.js", "BADFMT\n"),
		NewScriptedTextResponse("Turn answer."),
	)
	ag := qhAgent(t, client, root, &configuration.QualityConfig{Enabled: true})

	result, err := ag.ProcessQuery("Add app.js.")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if result != "Turn answer." {
		t.Errorf("result = %q, want the untouched turn answer (the formatter repair needs no round)", result)
	}
	if calls := len(client.GetSentRequests()); calls != 2 {
		t.Errorf("model calls = %d, want 2 (the runtime repair needs no repair round)", calls)
	}

	res := ag.LastQualityResult()
	if res == nil {
		t.Fatal("LastQualityResult = nil, want the run")
	}
	requireFormatChanged(t, res)

	data, err := os.ReadFile(filepath.Join(root, "app.js"))
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	if string(data) != "formatted\n" {
		t.Errorf("app.js = %q, want the formatted content", data)
	}
}

// requireFormatChanged asserts the run's format check reports an in-place
// change and the linter ran.
func requireFormatChanged(t *testing.T, res *verify.QualityResult) {
	t.Helper()
	for _, c := range res.Checks {
		if c.Kind == verify.QualityFormat {
			if !c.Changed {
				t.Errorf("format check Changed = false, want true (the formatter rewrote a file)")
			}
			return
		}
	}
	t.Errorf("no format check in the result: %+v", res.Checks)
}

// ---------------------------------------------------------------------------
// (c) Disabled (the default) → a complete no-op
// ---------------------------------------------------------------------------

func TestQualityHook_DisabledByDefaultIsNoOp(t *testing.T) {
	qhShAvailable(t)
	root := t.TempDir()

	// The linter would fail if run — proving the hook ran nothing.
	qhWriteStarterManifest(t, root, "exit 0", "exit 1")

	const turnAnswer = "Done."
	client := NewScriptedClient(
		qhWriteFileCall(t, root, "app.js", "var x = 1;\n"),
		NewScriptedTextResponse(turnAnswer),
	)
	// No quality section: the default-off path — a failing linter must be
	// invisible to the hook.
	ag := qhAgent(t, client, root, nil)

	result, err := ag.ProcessQuery("Add app.js.")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if result != turnAnswer {
		t.Errorf("result = %q, want the untouched turn answer %q", result, turnAnswer)
	}
	if calls := len(client.GetSentRequests()); calls != 2 {
		t.Errorf("model calls = %d, want 2 (quality disabled: no repair round)", calls)
	}
	if reports := qhReportMessages(ag); len(reports) != 0 {
		t.Errorf("quality-report messages = %d, want 0 (the hook is a no-op)", len(reports))
	}
	if res := ag.LastQualityResult(); res != nil {
		t.Errorf("LastQualityResult = %+v, want nil (the hook never ran)", res)
	}
}

// ---------------------------------------------------------------------------
// (d) Formatter failure does not crash the turn
// ---------------------------------------------------------------------------

func TestQualityHook_FormatterFailureDoesNotCrashTurn(t *testing.T) {
	qhShAvailable(t)
	root := t.TempDir()

	// The formatter fails (a broken command); the linter passes. The turn
	// must complete, reporting the formatter failure without trying to repair
	// it forever: the failing format step is fed back once, the model replies,
	// and the (still failing) format step then stands.
	qhWriteStarterManifest(t, root, "echo formatter blew up; exit 1", "exit 0")

	const (
		turnAnswer   = "Turn answer."
		repairAnswer = "Formatter still fails; nothing more to do."
	)
	client := NewScriptedClient(
		qhWriteFileCall(t, root, "app.js", "var x = 1;\n"),
		NewScriptedTextResponse(turnAnswer),
		NewScriptedTextResponse(repairAnswer),
	)
	ag := qhAgent(t, client, root, &configuration.QualityConfig{Enabled: true})

	result, err := ag.ProcessQuery("Add app.js.")
	if err != nil {
		t.Fatalf("ProcessQuery: %v (a formatter failure must not gate the turn)", err)
	}
	if result != repairAnswer {
		t.Errorf("result = %q, want the repair-round answer %q", result, repairAnswer)
	}

	// The stored result reports the failing format step (with its output) and
	// the passing lint step — neither crashed the turn.
	res := ag.LastQualityResult()
	if res == nil {
		t.Fatal("LastQualityResult = nil, want the run")
	}
	if !res.Failed() {
		t.Errorf("LastQualityResult().Failed() = false, want true (the formatter failed)")
	}
	var sawFormat bool
	for _, c := range res.Checks {
		if c.Kind == verify.QualityFormat {
			sawFormat = true
			if c.Passed {
				t.Error("format check Passed = true, want false (the formatter failed)")
			}
			if !strings.Contains(c.Excerpt, "formatter blew up") {
				t.Errorf("format excerpt = %q, want the formatter's output", c.Excerpt)
			}
		}
	}
	if !sawFormat {
		t.Error("no format check in the stored result")
	}
}

// ---------------------------------------------------------------------------
// (e) No code change / subagent → the hook does not run
// ---------------------------------------------------------------------------

func TestQualityHook_NoFileChangeSkips(t *testing.T) {
	qhShAvailable(t)
	root := t.TempDir()
	qhWriteStarterManifest(t, root, "exit 0", "exit 1")

	const turnAnswer = "Nothing to change here."
	client := NewScriptedClient(NewScriptedTextResponse(turnAnswer))
	ag := qhAgent(t, client, root, &configuration.QualityConfig{Enabled: true})

	result, err := ag.ProcessQuery("Is there anything to fix?")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if result != turnAnswer {
		t.Errorf("result = %q, want the untouched turn answer %q", result, turnAnswer)
	}
	if calls := len(client.GetSentRequests()); calls != 1 {
		t.Errorf("model calls = %d, want 1 (no code change: the hook is a no-op)", calls)
	}
	if res := ag.LastQualityResult(); res != nil {
		t.Errorf("LastQualityResult = %+v, want nil (no code change)", res)
	}
}

func TestQualityHook_SkipsSubagents(t *testing.T) {
	qhShAvailable(t)
	root := t.TempDir()
	qhWriteStarterManifest(t, root, "exit 0", "exit 1")

	const turnAnswer = "Subagent done."
	client := NewScriptedClient(
		qhWriteFileCall(t, root, "app.js", "var x = 1;\n"),
		NewScriptedTextResponse(turnAnswer),
	)
	ag := qhAgent(t, client, root, &configuration.QualityConfig{Enabled: true})
	ag.subagentDepth = 1

	result, err := ag.ProcessQuery("Add app.js.")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if result != turnAnswer {
		t.Errorf("result = %q, want the untouched subagent answer %q", result, turnAnswer)
	}
	if res := ag.LastQualityResult(); res != nil {
		t.Errorf("LastQualityResult = %+v, want nil (subagent turns skip the hook)", res)
	}
}
