//go:build !js

// agent_verification_exit_test.go — the SP-149 §149e tests: a
// non-interactive `sprout agent` run exits non-zero when verification
// is enabled and fails; disabled verification changes no behavior.
//
// The pure core (verificationExitError) is table-tested directly
// against constructed *verify.Result values. The exit mapping is pinned
// against the real exit-code function (exitCodeFor). The acceptance
// cases drive the real RunAgent direct-mode path with a scripted model
// and a real workspace fixture whose manifest build command runs
// through the default executor (sh -c), mirroring the pkg/agent
// turn-end hook fixtures (149.5/149.6):
//
//   - a broken build → RunAgent returns the verification-failure
//     error (exit code 1) and the stored result is the failing run;
//   - a passing build → RunAgent returns nil;
//   - verification disabled (the default) with a broken build → nil
//     (behavior unchanged: the hook never runs, nothing is stored);
//   - a text-only turn (no file change) with verification enabled →
//     nil (the hook's change gate is empty, so it never runs).

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/history"
	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/starterstore"
	"github.com/sprout-foundry/sprout/pkg/verify"
)

// ---------------------------------------------------------------------------
// Pure core: verificationExitError
// ---------------------------------------------------------------------------

func vaFailingResult() *verify.Result {
	return &verify.Result{
		Baseline: true,
		Checks: []verify.Check{
			{Kind: plancontract.KindBuild, Command: "make build", Excerpt: "fixture-broken\n"},
			{Kind: plancontract.KindTest, Skipped: true, Reason: "no test command available"},
		},
	}
}

func vaPassingResult() *verify.Result {
	return &verify.Result{
		Baseline: true,
		Checks: []verify.Check{
			{Kind: plancontract.KindBuild, Command: "make build", Passed: true},
			{Kind: plancontract.KindTest, Skipped: true, Reason: "no test command available"},
		},
	}
}

func TestVerificationExitError(t *testing.T) {
	allSkipped := &verify.Result{
		Checks: []verify.Check{
			{Kind: plancontract.KindBuild, Skipped: true, Reason: "no build command available"},
			{Kind: plancontract.KindTest, Skipped: true, Reason: "no test command available"},
		},
	}
	runError := &verify.Result{
		Checks: []verify.Check{
			{Kind: plancontract.KindBuild, Command: "make build", Passed: true},
		},
		Errors: []string{"plan: corrupt"},
	}

	tests := []struct {
		name    string
		res     *verify.Result
		wantErr bool
	}{
		{
			name:    "nil result (verification never ran) is no error",
			res:     nil,
			wantErr: false,
		},
		{
			name:    "passing result is no error",
			res:     vaPassingResult(),
			wantErr: false,
		},
		{
			name:    "all-skipped result (verified nothing, not a failure) is no error",
			res:     allSkipped,
			wantErr: false,
		},
		{
			name:    "failed check is an error",
			res:     vaFailingResult(),
			wantErr: true,
		},
		{
			name:    "run-level error is an error",
			res:     runError,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verificationExitError(tt.res)
			if (err != nil) != tt.wantErr {
				t.Fatalf("verificationExitError() = %v, wantErr = %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				return
			}
			if !strings.Contains(err.Error(), "verification failed") {
				t.Errorf("error %q does not name the verification failure", err)
			}
			// The message carries the run's summary, so the single
			// clean line (renderExecuteError) says what failed.
			if !strings.Contains(err.Error(), tt.res.Summary()) {
				t.Errorf("error %q does not carry the run's summary %q", err, tt.res.Summary())
			}
		})
	}
}

func TestVerificationRunExitErrorNilAgent(t *testing.T) {
	if err := verificationRunExitError(nil); err != nil {
		t.Fatalf("verificationRunExitError(nil) = %v, want nil (a nil agent never gates the exit)", err)
	}
}

// TestVerificationExitErrorExitCode pins "exits non-zero" against the
// real CLI exit-code function: a failing result maps to exit code 1,
// a nil/passing result to 0.
func TestVerificationExitErrorExitCode(t *testing.T) {
	if got := exitCodeFor(verificationExitError(vaFailingResult())); got != exitFailure {
		t.Errorf("exitCodeFor(failing result) = %d, want %d (non-zero)", got, exitFailure)
	}
	if got := exitCodeFor(verificationExitError(nil)); got != exitOK {
		t.Errorf("exitCodeFor(nil result) = %d, want %d", got, exitOK)
	}
	if got := exitCodeFor(verificationExitError(vaPassingResult())); got != exitOK {
		t.Errorf("exitCodeFor(passing result) = %d, want %d", got, exitOK)
	}
}

// ---------------------------------------------------------------------------
// Fixtures and helpers (the RunAgent end-to-end cases)
// ---------------------------------------------------------------------------

// vaShAvailable mirrors pkg/verify's shAvailable: the fixture's build
// command runs through the default executor (sh -c), so the tests skip
// rather than fail where sh is absent (non-UNIX dev machines).
func vaShAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh available on this platform (the fixture build command runs through sh -c)")
	}
}

// vaWriteStarterManifest writes the project's starter manifest fixture
// (SP-153) with the given build command and no test command, so a
// baseline verification run executes the build and skips the test check.
func vaWriteStarterManifest(t *testing.T, root, buildCommand string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".sprout"), 0o755); err != nil {
		t.Fatalf("mkdir .sprout: %v", err)
	}
	manifest := fmt.Sprintf(`{"starter":{"id":"fixture","version":"1.0.0"},"build":%q}`, buildCommand)
	if err := os.WriteFile(starterstore.StarterManifestPath(root), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write starter manifest: %v", err)
	}
}

// vaAgent wires a scripted client into a fresh agent whose workspace
// root is the given project directory. ver (nil = unset, the
// default-off path) sets the verification section; full context mode
// keeps the file tools on the roster; SkipPrompt keeps the turn from
// blocking on an interactive prompt. History I/O (the direct-mode
// turn's deferred change commit) is redirected to a temp dir — the
// project-scoped history default is the process CWD's .sprout/, which
// must never be written by a test.
func vaAgent(t *testing.T, client *agent.ScriptedClient, root string, ver *configuration.VerificationConfig) *agent.Agent {
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

	prevChanges, prevRevisions := history.GetPathsForTesting()
	histDir := t.TempDir()
	history.SetPathsForTesting(filepath.Join(histDir, "changes"), filepath.Join(histDir, "revisions"))
	t.Cleanup(func() { history.SetPathsForTesting(prevChanges, prevRevisions) })

	ag, err := agent.NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient: %v", err)
	}
	t.Cleanup(func() { ag.Shutdown() })
	ag.SetMaxIterations(10)
	ag.SetWorkspaceRoot(root)
	return ag
}

// vaWriteToolCall is the scripted tool call that changes a file in the
// workspace (so the change tracker records the turn's change and the
// turn-end hook's gate opens): write_file on a new file under the
// project root.
func vaWriteToolCall(t *testing.T, root, path string) *agent.ScriptedResponse {
	t.Helper()
	args := fmt.Sprintf(`{"path":%q,"content":"function main() { return 1; }"}`, filepath.Join(root, path))
	return agent.NewToolCallResponse("write_file", args)
}

// vaRunAgentNonInteractive runs the direct-mode path end to end:
// RunAgent on the scripted agent with a one-line query. SPROUT_DAEMON_AGENT=0
// keeps the one-shot daemon routing off (the query must run in-process
// against THIS agent, never through a developer's running daemon).
func vaRunAgentNonInteractive(t *testing.T, ag *agent.Agent, query string) error {
	t.Helper()
	t.Setenv("SPROUT_DAEMON_AGENT", "0")
	return RunAgent(ag, false, strings.Fields(query))
}

// ---------------------------------------------------------------------------
// (a) Broken build → RunAgent returns the verification-failure error
// ---------------------------------------------------------------------------

func TestRunAgentVerificationFailureExitsNonZero(t *testing.T) {
	vaShAvailable(t)
	root := t.TempDir()
	vaWriteStarterManifest(t, root, "echo fixture-broken; exit 1")

	// The scripted turn: a file write (opens the hook's gate), the
	// turn's answer, then exactly one repair round (RepairAttempts=1).
	client := agent.NewScriptedClient(
		vaWriteToolCall(t, root, "src/app.js"),
		agent.NewStopResponse("done"),
		agent.NewStopResponse("I tried to fix the build; it still fails."),
	)
	ag := vaAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: 1})

	err := vaRunAgentNonInteractive(t, ag, "make the change")
	if err == nil {
		t.Fatal("RunAgent = nil error, want the verification-failure error (non-interactive run exits non-zero when verification fails, SP-149 §149e)")
	}
	if !strings.Contains(err.Error(), "verification failed") {
		t.Errorf("RunAgent error = %q, want it to name the verification failure", err)
	}
	// Pin the exit code against the real CLI exit function: a plain
	// error maps to exit code 1.
	if got := exitCodeFor(err); got != exitFailure {
		t.Errorf("exitCodeFor(RunAgent error) = %d, want %d (non-zero)", got, exitFailure)
	}

	// The stored result is the last (still failing) verification run.
	res := ag.LastVerificationResult()
	if res == nil {
		t.Fatal("LastVerificationResult = nil, want the last failing run")
	}
	if !res.Failed() {
		t.Error("LastVerificationResult().Failed() = false, want true (the build never passed)")
	}
}

// ---------------------------------------------------------------------------
// (b) Passing build → RunAgent returns nil
// ---------------------------------------------------------------------------

func TestRunAgentVerificationPassExitsZero(t *testing.T) {
	vaShAvailable(t)
	root := t.TempDir()
	vaWriteStarterManifest(t, root, "echo fixture-ok")

	client := agent.NewScriptedClient(
		vaWriteToolCall(t, root, "src/app.js"),
		agent.NewStopResponse("done"),
	)
	ag := vaAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: 1})

	if err := vaRunAgentNonInteractive(t, ag, "make the change"); err != nil {
		t.Fatalf("RunAgent = %v, want nil (a passing verification run exits zero)", err)
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
// (c) Verification disabled (the default) with a broken build → nil:
//     behavior unchanged, the hook never runs, nothing is stored
// ---------------------------------------------------------------------------

func TestRunAgentVerificationDisabledIsNoOp(t *testing.T) {
	vaShAvailable(t)
	root := t.TempDir()
	vaWriteStarterManifest(t, root, "echo fixture-broken; exit 1")

	client := agent.NewScriptedClient(
		vaWriteToolCall(t, root, "src/app.js"),
		agent.NewStopResponse("done"),
	)
	// No verification section: the default-off path (SP-149 §149e) —
	// a broken build must not change the exit behavior at all.
	ag := vaAgent(t, client, root, nil)

	if err := vaRunAgentNonInteractive(t, ag, "make the change"); err != nil {
		t.Fatalf("RunAgent = %v, want nil (verification disabled: behavior unchanged)", err)
	}
	if res := ag.LastVerificationResult(); res != nil {
		t.Errorf("LastVerificationResult = %+v, want nil (the hook never ran)", res)
	}
}

// ---------------------------------------------------------------------------
// (d) Text-only turn (no file change) with verification enabled → nil:
//     the hook's change gate is empty, so it never runs
// ---------------------------------------------------------------------------

func TestRunAgentTextOnlyTurnSkipsVerification(t *testing.T) {
	vaShAvailable(t)
	root := t.TempDir()
	vaWriteStarterManifest(t, root, "echo fixture-broken; exit 1")

	client := agent.NewScriptedClient(
		agent.NewStopResponse("nothing to change here"),
	)
	ag := vaAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: 1})

	if err := vaRunAgentNonInteractive(t, ag, "is there anything to fix?"); err != nil {
		t.Fatalf("RunAgent = %v, want nil (a text-only turn changed no code: the hook never ran)", err)
	}
	if res := ag.LastVerificationResult(); res != nil {
		t.Errorf("LastVerificationResult = %+v, want nil (the hook never ran)", res)
	}
}
