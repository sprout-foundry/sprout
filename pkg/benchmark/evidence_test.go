//go:build !js

// evidence_test.go — the run-evidence tests: the keep-runs policy (failed
// by default, all, none), the evidence layout written for a selected run
// (the working-copy diff against the baseline — including a NEW file the
// model created — the agent transcript and the verification output), the
// filesystem-safe and collision-resistant directory naming, and the
// best-effort guarantee (a failed evidence write never changes a run's
// verdict). The fixtures and helpers live in runner_test.go (same
// package): benchTask, shapeManifest, scriptedFactory, shAvailable.
package benchmark

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// evidenceRunner builds a one-run benchmark runner that keeps evidence
// under evidenceDir with the given policy, its single check the
// deterministic echo build command the fixture manifest honors.
func evidenceRunner(mgr *configuration.Manager, keep KeepRuns, evidenceDir string, factory AgentFactory) *Runner {
	return &Runner{
		ConfigManager: mgr,
		AgentFactory:  factory,
		ShapeCopy:     shapeManifest("echo evidence-ok"),
		RunsPerTask:   1,
		KeepRuns:      keep,
		EvidenceDir:   evidenceDir,
	}
}

// evidenceRunDir returns the single run directory under evidenceDir/runs
// (the deterministic layout has exactly one for a one-run task).
func evidenceRunDir(t *testing.T, evidenceDir string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(evidenceDir, "runs"))
	if err != nil {
		t.Fatalf("read evidence root: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("evidence runs entries = %d, want 1", len(entries))
	}
	return filepath.Join(evidenceDir, "runs", entries[0].Name())
}

// writeFileResponse builds a scripted write_file tool call that creates a
// NEW file in the run copy (the model's most common kind of edit, and the
// one a plain `git diff` cannot see — an untracked file).
func writeFileResponse(runDir, rel, content string) *agent.ScriptedResponse {
	return agent.NewToolCallResponse("write_file", gateJSON(map[string]any{
		"path":    filepath.Join(runDir, filepath.FromSlash(rel)),
		"content": content,
	}))
}

// TestEvidence_FailedRunKeepsDiffTranscriptAndVerification pins the core
// contract for keep-runs=failed: a failed run writes its working-copy diff
// (which MUST include a NEW file the model created — a plain `git diff`
// would miss untracked files), the agent transcript and the verification
// output, all under <evidence>/runs/<task>-<model>-<n>/.
func TestEvidence_FailedRunKeepsDiffTranscriptAndVerification(t *testing.T) {
	shAvailable(t)
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	evidenceDir := t.TempDir()
	// The build fails, so the run is recorded as a failure — the case the
	// default policy keeps evidence for.
	runner := evidenceRunner(mgr, KeepRunsFailed, evidenceDir, gateFactory(t, mgr, func(runDir string) []*agent.ScriptedResponse {
		return []*agent.ScriptedResponse{
			writeFileResponse(runDir, "src/added-page.html", "<h1>Added page</h1>\n"),
			agent.NewStopResponse("Done, added the about page."),
			agent.NewStopResponse("I could not fix the build."),
		}
	}, nil))
	runner.ShapeCopy = shapeManifest("echo evidence-fail; exit 1")

	runs, err := runner.RunTask(context.Background(), benchTask(), ModelSpec{Model: "keep-model", Provider: "keep-provider"})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(runs) != 1 || runs[0].Passed {
		t.Fatalf("runs = %+v, want one failed run", runs)
	}

	dir := evidenceRunDir(t, evidenceDir)
	if base := filepath.Base(dir); base != "bench-test-keep-provider-keep-model-1" {
		t.Errorf("evidence dir = %q, want bench-test-keep-provider-keep-model-1", base)
	}

	// The diff must show the NEW file the model created (the reason the
	// capture stages with git add -A before diffing).
	diff, err := os.ReadFile(filepath.Join(dir, "diff.patch"))
	if err != nil {
		t.Fatalf("read diff.patch: %v", err)
	}
	if !strings.Contains(string(diff), "src/added-page.html") {
		t.Errorf("diff.patch does not mention the new file src/added-page.html:\n%s", diff)
	}
	if !strings.Contains(string(diff), "+<h1>Added page</h1>") {
		t.Errorf("diff.patch does not carry the new file's content:\n%s", diff)
	}
	if !strings.Contains(string(diff), "new file mode") {
		t.Errorf("diff.patch does not mark the file as new (untracked files must be staged into the diff):\n%s", diff)
	}

	// The transcript is the agent's conversation, as JSON.
	data, err := os.ReadFile(filepath.Join(dir, "transcript.json"))
	if err != nil {
		t.Fatalf("read transcript.json: %v", err)
	}
	var messages []map[string]any
	if err := json.Unmarshal(data, &messages); err != nil {
		t.Fatalf("transcript.json is not a JSON message array: %v\n%s", err, data)
	}
	if len(messages) == 0 {
		t.Error("transcript.json is empty, want the run's conversation")
	}

	// The verification output is the failing result as JSON.
	vdata, err := os.ReadFile(filepath.Join(dir, "verification.json"))
	if err != nil {
		t.Fatalf("read verification.json: %v", err)
	}
	var verification map[string]any
	if err := json.Unmarshal(vdata, &verification); err != nil {
		t.Fatalf("verification.json is not JSON: %v\n%s", err, vdata)
	}
	if _, ok := verification["checks"]; !ok {
		t.Errorf("verification.json = %s, want the verification result's checks", vdata)
	}

	// The human-readable summary states the run failed.
	summary, err := os.ReadFile(filepath.Join(dir, "summary.txt"))
	if err != nil {
		t.Fatalf("read summary.txt: %v", err)
	}
	for _, want := range []string{"passed: false", "verified: true", "verification passed: false"} {
		if !strings.Contains(string(summary), want) {
			t.Errorf("summary.txt missing %q:\n%s", want, summary)
		}
	}
}

// TestEvidence_PassedRunNotKeptByDefault pins the default policy's other
// half: a passed run with keep-runs=failed writes nothing.
func TestEvidence_PassedRunNotKeptByDefault(t *testing.T) {
	shAvailable(t)
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	evidenceDir := t.TempDir()
	runner := evidenceRunner(mgr, KeepRunsFailed, evidenceDir, scriptedFactory(t, mgr, true, "Done."))

	runs, err := runner.RunTask(context.Background(), benchTask(), ModelSpec{Model: "keep-model"})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(runs) != 1 || !runs[0].Passed {
		t.Fatalf("runs = %+v, want one passing run", runs)
	}
	if _, err := os.Stat(filepath.Join(evidenceDir, "runs")); !os.IsNotExist(err) {
		t.Errorf("runs/ exists (err=%v), want no evidence for a passed run under keep-runs=failed", err)
	}
}

// TestEvidence_AllKeepsPassedRun pins keep-runs=all: a passed run keeps
// evidence too.
func TestEvidence_AllKeepsPassedRun(t *testing.T) {
	shAvailable(t)
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	evidenceDir := t.TempDir()
	runner := evidenceRunner(mgr, KeepRunsAll, evidenceDir, scriptedFactory(t, mgr, true, "Done."))

	runs, err := runner.RunTask(context.Background(), benchTask(), ModelSpec{Model: "keep-model"})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(runs) != 1 || !runs[0].Passed {
		t.Fatalf("runs = %+v, want one passing run", runs)
	}
	dir := evidenceRunDir(t, evidenceDir)
	for _, name := range []string{"diff.patch", "transcript.json", "verification.json", "summary.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("keep-runs=all: %s missing from a passing run's evidence: %v", name, err)
		}
	}
	summary, err := os.ReadFile(filepath.Join(dir, "summary.txt"))
	if err != nil {
		t.Fatalf("read summary.txt: %v", err)
	}
	if !strings.Contains(string(summary), "passed: true") {
		t.Errorf("summary.txt = %q, want the passing verdict", summary)
	}
}

// TestEvidence_NoneKeepsNothing pins keep-runs=none: no evidence, even for
// a failed run.
func TestEvidence_NoneKeepsNothing(t *testing.T) {
	shAvailable(t)
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	evidenceDir := t.TempDir()
	runner := evidenceRunner(mgr, KeepRunsNone, evidenceDir, scriptedFactory(t, mgr, true,
		"Claimed success.",
		"Could not fix it."))
	runner.ShapeCopy = shapeManifest("echo evidence-fail; exit 1")

	runs, err := runner.RunTask(context.Background(), benchTask(), ModelSpec{Model: "keep-model"})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(runs) != 1 || runs[0].Passed {
		t.Fatalf("runs = %+v, want one failed run", runs)
	}
	if _, err := os.Stat(filepath.Join(evidenceDir, "runs")); !os.IsNotExist(err) {
		t.Errorf("runs/ exists (err=%v), want nothing written under keep-runs=none", err)
	}
}

// TestEvidence_TimedOutRunKeepsEvidence pins the timeout dimension: a run
// whose turn timed out still has its copy on disk at capture time, so the
// failed run keeps evidence.
func TestEvidence_TimedOutRunKeepsEvidence(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	evidenceDir := t.TempDir()
	runner := evidenceRunner(mgr, KeepRunsFailed, evidenceDir, hungFactory(t, mgr, 2*time.Minute))
	runner.Timeout = 250 * time.Millisecond

	runs, err := runner.RunTask(context.Background(), benchTask(), ModelSpec{Model: "keep-model", Provider: "prov"})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(runs) != 1 || runs[0].Passed {
		t.Fatalf("runs = %+v, want one failed (timed-out) run", runs)
	}
	dir := evidenceRunDir(t, evidenceDir)
	// The verification output records WHY the run was not verified.
	vdata, err := os.ReadFile(filepath.Join(dir, "verification.json"))
	if err != nil {
		t.Fatalf("read verification.json: %v", err)
	}
	var verification map[string]any
	if err := json.Unmarshal(vdata, &verification); err != nil {
		t.Fatalf("verification.json is not JSON: %v\n%s", err, vdata)
	}
	if verified, _ := verification["verified"].(bool); verified {
		t.Errorf("verification.json = %s, want verified:false for a timed-out run", vdata)
	}
	if reason, _ := verification["reason"].(string); reason != "verify timed out" {
		t.Errorf("verification.json reason = %q, want %q", reason, "verify timed out")
	}
}

// TestEvidence_NoEvidenceDirWritesNothing pins the "no root, no evidence"
// rule: a runner with an empty EvidenceDir keeps nothing even when the
// policy says all.
func TestEvidence_NoEvidenceDirWritesNothing(t *testing.T) {
	shAvailable(t)
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	runner := evidenceRunner(mgr, KeepRunsAll, "", scriptedFactory(t, mgr, true, "Done."))
	if runner.keepsRun(true) || runner.keepsRun(false) {
		t.Error("keepsRun returned true with an empty EvidenceDir (there is nowhere to write)")
	}

	runs, err := runner.RunTask(context.Background(), benchTask(), ModelSpec{Model: "keep-model"})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(runs) != 1 || !runs[0].Passed {
		t.Fatalf("runs = %+v, want the run to complete normally", runs)
	}
}

// TestEvidence_DirNameSanitizedAndProviderDisambiguated pins the naming:
// path separators and spaces in the model id become a single safe
// component, and two providers with the SAME model id produce two distinct
// run directories (no silent overwrite).
func TestEvidence_DirNameSanitizedAndProviderDisambiguated(t *testing.T) {
	spec := ModelSpec{Model: "meta-llama/Llama 3.1 8B", Provider: "local prov/x"}
	got := runEvidenceDirName("my task/x", spec, 2)
	if strings.ContainsAny(got, `/\`) {
		t.Errorf("runEvidenceDirName = %q, want no path separators", got)
	}
	if strings.Contains(got, " ") {
		t.Errorf("runEvidenceDirName = %q, want no spaces", got)
	}
	if !strings.HasSuffix(got, "-2") {
		t.Errorf("runEvidenceDirName = %q, want the run number suffix -2", got)
	}

	a := runEvidenceDirName("task", ModelSpec{Model: "shared-model", Provider: "prov-a"}, 1)
	b := runEvidenceDirName("task", ModelSpec{Model: "shared-model", Provider: "prov-b"}, 1)
	if a == b {
		t.Errorf("run dir names collide across providers (%q); the provider must disambiguate", a)
	}
	if a != "task-prov-a-shared-model-1" || b != "task-prov-b-shared-model-1" {
		t.Errorf("dir names = %q / %q, want the provider between task and model", a, b)
	}

	// A model with no provider still names a usable directory.
	bare := runEvidenceDirName("task", ModelSpec{Model: "gpt-5"}, 3)
	if bare != "task-gpt-5-3" {
		t.Errorf("bare model dir name = %q, want task-gpt-5-3", bare)
	}
}

// TestEvidence_ParseKeepRuns pins the policy parser: the three accepted
// values (case-insensitive, trimmed) and a clear error naming them for
// anything else.
func TestEvidence_ParseKeepRuns(t *testing.T) {
	for in, want := range map[string]KeepRuns{
		"failed":  KeepRunsFailed,
		"all":     KeepRunsAll,
		"none":    KeepRunsNone,
		"ALL":     KeepRunsAll,
		"  None ": KeepRunsNone,
	} {
		got, err := ParseKeepRuns(in)
		if err != nil {
			t.Errorf("ParseKeepRuns(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseKeepRuns(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := ParseKeepRuns("sometimes"); err == nil {
		t.Error("ParseKeepRuns(\"sometimes\"): err = nil, want the usage error")
	} else if !strings.Contains(err.Error(), "failed") || !strings.Contains(err.Error(), "all") || !strings.Contains(err.Error(), "none") {
		t.Errorf("ParseKeepRuns bad-value error = %q, want it to name the accepted values", err)
	}
	if got := KeepRuns("").resolves(); got != KeepRunsFailed {
		t.Errorf("zero-value policy resolves to %q, want failed (the default)", got)
	}
}

// TestEvidence_ReRunDoesNotOverwrite pins the collision fallback: a second
// capture for the same (task, provider, model, run) into the same root
// adds a new directory rather than overwriting the evidence already there.
func TestEvidence_ReRunDoesNotOverwrite(t *testing.T) {
	evidenceDir := t.TempDir()

	// Two failed captures for the same run identity: the second must not
	// clobber the first.
	for i := 0; i < 2; i++ {
		run := &Run{TaskID: "bench-test", Starter: "fixture", Model: "m", Provider: "p", RunNumber: 1}
		captureRunEvidence(
			&Runner{KeepRuns: KeepRunsFailed, EvidenceDir: evidenceDir},
			benchTask(), ModelSpec{Model: "m", Provider: "p"}, 1,
			// A nonexistent run dir: the diff write fails (and is logged),
			// but the directory itself is still reserved and the
			// verification output still lands.
			filepath.Join(t.TempDir(), "gone"),
			nil, run,
		)
	}

	entries, err := os.ReadDir(filepath.Join(evidenceDir, "runs"))
	if err != nil {
		t.Fatalf("read evidence root: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("evidence runs entries = %d, want 2 (a re-run must not overwrite the first)", len(entries))
	}
}

// TestEvidence_WriteFailureDoesNotChangeVerdict pins the best-effort
// guarantee: with the evidence root pointed at an unwritable location
// (a path under an existing FILE), a passing run still passes — evidence
// capture is diagnostic, never an input to the verdict.
func TestEvidence_WriteFailureDoesNotChangeVerdict(t *testing.T) {
	shAvailable(t)
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	// A regular file where the evidence root should be a directory: every
	// evidence write under it fails.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed blocker file: %v", err)
	}

	runner := evidenceRunner(mgr, KeepRunsAll, blocker, scriptedFactory(t, mgr, true, "Done."))
	runs, err := runner.RunTask(context.Background(), benchTask(), ModelSpec{Model: "keep-model"})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	if !runs[0].Passed {
		t.Error("Passed = false, want true: an evidence write failure must not flip the verdict")
	}
	if runs[0].Err != nil {
		t.Errorf("Err = %v, want nil: evidence failures never ride on the run", runs[0].Err)
	}
}
