package agent

// Worker-result verification tests (ca.2). Pin the three pre-reliance checks
// (edit applied, stated tests pass, output format matches), the single
// escalation to the coder model, and the rule that a failed worker result is
// never handed to the primary as an unqualified success.

import (
	"context"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent/changes"
	"github.com/sprout-foundry/sprout/pkg/agent/subagents"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// --- Check 1: an edit applies cleanly -------------------------------------

func TestVerifyWorkerResult_EditCheck(t *testing.T) {
	tests := []struct {
		name     string
		prompt   string
		changes  []changesTrackedFile
		verified bool
	}{
		{
			name:     "mutating task with no changes fails",
			prompt:   "edit pkg/foo.go to add the missing guard",
			changes:  nil,
			verified: false,
		},
		{
			name:     "mutating task with changes passes",
			prompt:   "edit pkg/foo.go to add the missing guard",
			changes:  []changesTrackedFile{{FilePath: "pkg/foo.go", Operation: "edit"}},
			verified: true,
		},
		{
			name:     "read-only task with no changes passes (check not applicable)",
			prompt:   "summarise the contents of pkg/foo.go in two sentences",
			changes:  nil,
			verified: true,
		},
		{
			name:     "read task mentioning a past mutation is not treated as mutating",
			prompt:   "summarise what was created in the last commit and list the files",
			changes:  nil,
			verified: true,
		},
		{
			name:     "word-boundary: 'address' does not match the add verb",
			prompt:   "address the reviewer's concern in your summary, no code changes",
			changes:  nil,
			verified: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newIsolatedTestAgent(t)
			defer a.Shutdown()
			spec := &subagentLaunchSpec{prompt: tt.prompt}
			result := &SubagentResult{Output: "done", OutputComplete: true, FileChanges: tt.changes}
			got := verifyWorkerResult(a, spec, result)
			if got.Verified != tt.verified {
				t.Errorf("verified = %v, want %v (reason: %s)", got.Verified, tt.verified, got.Reason)
			}
		})
	}
}

// --- Check 2: the stated tests pass ---------------------------------------

func TestVerifyWorkerResult_StatedTestsCheck(t *testing.T) {
	tests := []struct {
		name     string
		history  map[string]subagents.ShellCommandRecord
		verified bool
	}{
		{
			name:     "no test command run: check not applicable",
			history:  map[string]subagents.ShellCommandRecord{"ls -la": {Command: "ls -la"}},
			verified: true,
		},
		{
			name:     "recorded passing test passes",
			history:  map[string]subagents.ShellCommandRecord{"go test ./pkg/foo/": {Command: "go test ./pkg/foo/"}},
			verified: true,
		},
		{
			name:     "recorded failing test fails verification",
			history:  map[string]subagents.ShellCommandRecord{"go test ./pkg/foo/": {Command: "go test ./pkg/foo/", Failed: true}},
			verified: false,
		},
		{
			name:     "a failing non-test command does not fail verification",
			history:  map[string]subagents.ShellCommandRecord{"ls /nope": {Command: "ls /nope", Failed: true}},
			verified: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newIsolatedTestAgent(t)
			defer a.Shutdown()
			spec := &subagentLaunchSpec{prompt: "summarise the test output"}
			result := &SubagentResult{Output: "summary", OutputComplete: true, ShellHistory: tt.history}
			got := verifyWorkerResult(a, spec, result)
			if got.Verified != tt.verified {
				t.Errorf("verified = %v, want %v (reason: %s)", got.Verified, tt.verified, got.Reason)
			}
		})
	}
}

// --- Check 3: the output matches the requested format ---------------------

func TestVerifyWorkerResult_OutputFormatCheck(t *testing.T) {
	tests := []struct {
		name     string
		format   string
		output   string
		verified bool
	}{
		{
			name:     "no format requested: check not applicable",
			format:   "",
			output:   "anything at all",
			verified: true,
		},
		{
			name:     "JSON requested and provided",
			format:   "a single JSON object with keys name and count",
			output:   `{"name": "x", "count": 2}`,
			verified: true,
		},
		{
			name:     "JSON requested but missing",
			format:   "a single JSON object with keys name and count",
			output:   "I could not produce that.",
			verified: false,
		},
		{
			name:     "one-line requested and provided",
			format:   "a one-line summary",
			output:   "The build passed.",
			verified: true,
		},
		{
			name:     "one-line requested but multi-line",
			format:   "a one-line summary",
			output:   "The build passed.\nNothing else to report.",
			verified: false,
		},
		{
			name:     "empty output fails when a format was requested",
			format:   "a one-line summary",
			output:   "   ",
			verified: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newIsolatedTestAgent(t)
			defer a.Shutdown()
			spec := &subagentLaunchSpec{prompt: "summarise", outputFormat: tt.format}
			result := &SubagentResult{Output: tt.output, OutputComplete: true}
			got := verifyWorkerResult(a, spec, result)
			if got.Verified != tt.verified {
				t.Errorf("verified = %v, want %v (reason: %s)", got.Verified, tt.verified, got.Reason)
			}
		})
	}
}

// --- Failed runs are never verified ---------------------------------------

func TestVerifyWorkerResult_FailedRunNeverVerified(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()
	spec := &subagentLaunchSpec{prompt: "summarise"}

	cases := []*SubagentResult{
		nil,
		{Error: context.DeadlineExceeded},
		{Cancelled: true},
		{BudgetExceeded: true},
		{Truncated: true},
	}
	for i, result := range cases {
		if got := verifyWorkerResult(a, spec, result); got.Verified {
			t.Errorf("case %d: a failed/aborted run must not verify (result=%+v)", i, result)
		}
	}
}

// --- The single escalation ------------------------------------------------

// TestEscalateWorkerRun_RetriesOnceOnCoderModel pins the escalation contract:
// a worker task that fails verification is retried exactly once on the coder
// model, and the escalation is recorded. The retry count is asserted by
// counting the client invocations.
func TestEscalateWorkerRun_RetriesOnceOnCoderModel(t *testing.T) {
	parent, _ := newReviewTestRunner(t)
	setRoles(t, parent, map[string]configuration.RoleConfig{
		configuration.RoleCoder:  {Provider: "openrouter", Model: "coder-role-model"},
		configuration.RoleWorker: {Provider: "zai", Model: "cheap-model"},
	})

	// escalateWorkerTask runs through the agent's own runner, so the test
	// client factory must be set there.
	runner := parent.GetSubagentRunner()
	var ranModels []string
	runner.testClientFactory = func(_ api.ClientType, model string) (api.ClientInterface, error) {
		ranModels = append(ranModels, model)
		return NewScriptedClient(NewScriptedResponseBuilder().Content("done; no issues found in the assigned work.").Build()), nil
	}

	// A worker task that asked for an edit but produced none fails check 1.
	spec := &subagentLaunchSpec{
		prompt:         "edit pkg/foo.go to add the guard",
		persona:        "worker",
		role:           configuration.RoleWorker,
		enhancedPrompt: "edit pkg/foo.go to add the guard",
	}
	failed := &SubagentResult{Output: "I edited the file.", OutputComplete: true}

	first := verifyWorkerResult(parent, spec, failed)
	if first.Verified {
		t.Fatalf("precondition: the failed worker result must not verify")
	}

	escalated, second := escalateWorkerRun(context.Background(), parent, spec, failed, first)
	if escalated == nil {
		t.Fatal("escalation returned no result")
	}
	if !second.Escalated {
		t.Error("the escalation must be recorded on the verification outcome")
	}
	if len(ranModels) != 1 {
		t.Fatalf("escalation ran %d model calls, want exactly 1", len(ranModels))
	}
	if ranModels[0] != "coder-role-model" {
		t.Errorf("escalation ran model %q, want the coder role's model %q", ranModels[0], "coder-role-model")
	}
}

// TestEscalateWorkerRun_SecondFailureNotRetriedAgain pins that the retry
// happens at most once: when the escalated run also fails verification, the
// outcome is a reported failure, not another retry.
func TestEscalateWorkerRun_SecondFailureNotRetriedAgain(t *testing.T) {
	parent, _ := newReviewTestRunner(t)
	setRoles(t, parent, map[string]configuration.RoleConfig{
		configuration.RoleCoder: {Provider: "openrouter", Model: "coder-role-model"},
	})

	runner := parent.GetSubagentRunner()
	calls := 0
	runner.testClientFactory = func(api.ClientType, string) (api.ClientInterface, error) {
		calls++
		return NewScriptedClient(NewScriptedResponseBuilder().Content("still nothing useful here, sorry.").Build()), nil
	}

	// A mutating task with no file changes fails check 1 both times.
	spec := &subagentLaunchSpec{
		prompt:         "edit pkg/foo.go to add the guard",
		persona:        "worker",
		role:           configuration.RoleWorker,
		enhancedPrompt: "edit pkg/foo.go to add the guard",
	}
	failed := &SubagentResult{Output: "I edited the file.", OutputComplete: true}

	first := verifyWorkerResult(parent, spec, failed)
	_, second := escalateWorkerRun(context.Background(), parent, spec, failed, first)

	if calls != 1 {
		t.Fatalf("escalation ran %d model calls, want exactly 1 (no second retry)", calls)
	}
	if second.Verified {
		t.Error("the escalated run also failed verification; it must not be reported as verified")
	}
	if !second.Escalated {
		t.Error("the escalation must be recorded even when it also fails")
	}
}

// --- Never let a failed worker result through silently --------------------

// TestFinishSubagentRun_FailedWorkerVerificationIsNotSilent pins that a
// worker result that fails verification (even after escalation) is surfaced
// as a failure in the envelope, never as an unqualified success.
func TestFinishSubagentRun_FailedWorkerVerificationIsNotSilent(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	spec := &subagentLaunchSpec{prompt: "edit pkg/foo.go", persona: "worker", role: configuration.RoleWorker}
	result := &SubagentResult{Output: "I edited the file.", OutputComplete: true}
	verification := workerVerifyResult{Verified: false, Reason: "task asked for a file change but the worker reported no modified files"}

	out, err := finishSubagentRun(context.Background(), a, spec, result, verification)
	if err != nil {
		t.Fatalf("finishSubagentRun: %v", err)
	}
	if !containsAll(out, `"exit_code": "1"`, "worker verification failed") {
		t.Errorf("failed worker verification must surface as a failure in the envelope:\n%s", out)
	}
	if !containsAll(out, `"verified": false`) {
		t.Errorf("envelope must record verified=false:\n%s", out)
	}
}

// TestFinishSubagentRun_NonWorkerUnaffected pins that a non-worker subagent
// is not subject to worker verification: its envelope carries no
// verified/escalated fields and a clean run stays a success.
func TestFinishSubagentRun_NonWorkerUnaffected(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	spec := &subagentLaunchSpec{prompt: "implement the feature", persona: "coder", role: configuration.RoleCoder}
	result := &SubagentResult{Output: "Implemented the feature and ran the tests.", OutputComplete: true}

	out, err := finishSubagentRun(context.Background(), a, spec, result, workerVerifyResult{})
	if err != nil {
		t.Fatalf("finishSubagentRun: %v", err)
	}
	if containsAll(out, `"verified"`) || containsAll(out, `"escalated"`) {
		t.Errorf("a non-worker result must not carry worker-verification fields:\n%s", out)
	}
	if !containsAll(out, `"exit_code": "0"`) {
		t.Errorf("a clean non-worker run must stay a success:\n%s", out)
	}
}

// --- Parallel path ---------------------------------------------------------

// TestVerifyParallelWorkerResults_EscalatesFailedWorker pins the parallel
// path: a worker task in a batch whose result fails verification is retried
// once on the coder model and its result entry is replaced; a non-worker task
// in the same batch is left untouched.
func TestVerifyParallelWorkerResults_EscalatesFailedWorker(t *testing.T) {
	parent, _ := newReviewTestRunner(t)
	setRoles(t, parent, map[string]configuration.RoleConfig{
		configuration.RoleCoder:  {Provider: "openrouter", Model: "coder-role-model"},
		configuration.RoleWorker: {Provider: "zai", Model: "cheap-model"},
	})

	runner := parent.GetSubagentRunner()
	var ranModels []string
	runner.testClientFactory = func(_ api.ClientType, model string) (api.ClientInterface, error) {
		ranModels = append(ranModels, model)
		return NewScriptedClient(NewScriptedResponseBuilder().Content("escalated run finished the assigned work.").Build()), nil
	}

	tasks := []SubagentTask{
		{ID: "w1", Prompt: "edit pkg/foo.go to add the guard", Persona: "worker", Role: configuration.RoleWorker},
		{ID: "c1", Prompt: "implement the feature", Persona: "coder", Role: configuration.RoleCoder},
	}
	// w1 produced no file changes despite a mutating prompt → fails check 1.
	results := []*SubagentResult{
		{ID: "w1", Output: "I edited the file.", OutputComplete: true},
		{ID: "c1", Output: "implemented the feature and ran the tests.", OutputComplete: true},
	}

	verifyParallelWorkerResults(context.Background(), parent, tasks, results)

	if len(ranModels) != 1 {
		t.Fatalf("parallel escalation ran %d model calls, want exactly 1", len(ranModels))
	}
	if ranModels[0] != "coder-role-model" {
		t.Errorf("parallel escalation ran model %q, want the coder role's model", ranModels[0])
	}
	if results[1].Output != "implemented the feature and ran the tests." {
		t.Errorf("non-worker task result must be untouched, got %q", results[1].Output)
	}
	if results[0].Output != "escalated run finished the assigned work." {
		t.Errorf("worker task result must be replaced by the escalated run, got %q", results[0].Output)
	}
}

// TestVerifyParallelWorkerResults_NonWorkerBatchUntouched pins that a batch
// with no worker tasks runs no escalation at all.
func TestVerifyParallelWorkerResults_NonWorkerBatchUntouched(t *testing.T) {
	parent, _ := newReviewTestRunner(t)
	runner := parent.GetSubagentRunner()
	calls := 0
	runner.testClientFactory = func(api.ClientType, string) (api.ClientInterface, error) {
		calls++
		return NewScriptedClient(NewScriptedResponseBuilder().Content("ok").Build()), nil
	}

	tasks := []SubagentTask{
		{ID: "c1", Prompt: "implement the feature", Persona: "coder", Role: configuration.RoleCoder},
	}
	results := []*SubagentResult{{ID: "c1", Output: "done", OutputComplete: true}}

	verifyParallelWorkerResults(context.Background(), parent, tasks, results)

	if calls != 0 {
		t.Errorf("a non-worker batch must not escalate; ran %d model calls", calls)
	}
}

// TestFinishSubagentRun_VerifiedWorkerCarriesVerifiedTrue pins that a worker
// result that passed verification carries verified=true on the envelope, so
// the primary can see the result was checked.
func TestFinishSubagentRun_VerifiedWorkerCarriesVerifiedTrue(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	spec := &subagentLaunchSpec{prompt: "summarise the file", persona: "worker", role: configuration.RoleWorker}
	result := &SubagentResult{Output: "The file defines two helpers.", OutputComplete: true}
	verification := workerVerifyResult{Verified: true}

	out, err := finishSubagentRun(context.Background(), a, spec, result, verification)
	if err != nil {
		t.Fatalf("finishSubagentRun: %v", err)
	}
	if !containsAll(out, `"verified": true`) {
		t.Errorf("a verified worker result must carry verified=true:\n%s", out)
	}
	if containsAll(out, `"exit_code": "1"`) {
		t.Errorf("a verified worker result must stay a success:\n%s", out)
	}
}

// changesTrackedFile is a local alias for the change-tracker entry shape used
// by the edit-check test.
type changesTrackedFile = changes.TrackedFileChange
