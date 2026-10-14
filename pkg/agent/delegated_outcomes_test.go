package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// --- Task-type classification ---------------------------------------------

// TestClassifyTaskType pins the whole-word, category-priority classification:
// each category's keywords map to it, and the leading matching word in the
// prompt decides (a word that names several kinds of work takes the earlier
// category).
func TestClassifyTaskType(t *testing.T) {
	cases := []struct {
		prompt string
		want   string
	}{
		// read/search
		{"read the config file and tell me the port", TaskTypeReadSearch},
		{"search the repo for the auth middleware", TaskTypeReadSearch},
		{"grep the logs for the panic", TaskTypeReadSearch},
		{"find where the token is minted", TaskTypeReadSearch},
		// summarise
		{"summarise the test output", TaskTypeSummarise},
		{"give a summary of the changes", TaskTypeSummarise},
		{"condense the changelog to five lines", TaskTypeSummarise},
		// edit
		{"edit pkg/foo.go to add the guard", TaskTypeEdit},
		{"refactor the billing module", TaskTypeEdit},
		{"implement the retry backoff", TaskTypeEdit},
		{"rename the user model", TaskTypeEdit},
		// test (the test-category keyword is the first match in the prompt)
		{"run the unit tests and report", TaskTypeTest},
		{"assert the invariant holds", TaskTypeTest},
		{"the specs must cover the edge cases", TaskTypeTest},
		// other
		{"investigate the flake", TaskTypeOther},
		{"", TaskTypeOther},
	}
	for _, c := range cases {
		t.Run(strings.ReplaceAll(c.prompt, " ", "_"), func(t *testing.T) {
			if got := classifyTaskType(c.prompt); got != c.want {
				t.Errorf("classifyTaskType(%q) = %q, want %q", c.prompt, got, c.want)
			}
		})
	}
}

// TestClassifyTaskType_WordBoundaryGuards pins the false-positive guards: a
// keyword that is merely a substring of another word must not match, and the
// earlier category wins when several keywords are present.
func TestClassifyTaskType_WordBoundaryGuards(t *testing.T) {
	cases := []struct {
		prompt string
		want   string
	}{
		// "add" is a mutation verb but must not fire inside "address".
		{"address the reviewer's concern in a summary", TaskTypeSummarise},
		// "fix" must not fire inside "prefix".
		{"inspect the prefix handling", TaskTypeReadSearch},
		// "test" inside "protest" must not classify as test.
		{"summarise the protest coverage", TaskTypeSummarise},
		// Leading keyword decides: "read" beats the later "test".
		{"read the failing test output", TaskTypeReadSearch},
	}
	for _, c := range cases {
		if got := classifyTaskType(c.prompt); got != c.want {
			t.Errorf("classifyTaskType(%q) = %q, want %q", c.prompt, got, c.want)
		}
	}
}

// --- Bounded store + accessor ---------------------------------------------

// TestGetDelegatedTaskOutcomes_ReturnsCopy pins the accessor contract: a nil
// agent is safe, the store is empty to start, and the returned slice is a
// copy (mutating it does not disturb the store).
func TestGetDelegatedTaskOutcomes_ReturnsCopy(t *testing.T) {
	var nilAgent *Agent
	if got := nilAgent.GetDelegatedTaskOutcomes(); got != nil {
		t.Errorf("nil agent GetDelegatedTaskOutcomes = %v, want nil", got)
	}

	a := newMinimalTestAgent(t)
	if got := a.GetDelegatedTaskOutcomes(); len(got) != 0 {
		t.Fatalf("fresh agent outcomes = %d, want 0", len(got))
	}
	a.storeDelegatedOutcome(DelegatedTaskOutcome{Model: "m", Outcome: OutcomeVerified})

	out := a.GetDelegatedTaskOutcomes()
	if len(out) != 1 {
		t.Fatalf("len(out) = %d, want 1", len(out))
	}
	out[0].Model = "mutated"
	if got := a.GetDelegatedTaskOutcomes()[0].Model; got != "m" {
		t.Errorf("mutating the returned slice changed the store: got %q, want %q", got, "m")
	}
}

// TestDelegatedOutcomeStore_IsBounded pins the head-trim cap: past the cap the
// oldest records are dropped and the most recent are kept.
func TestDelegatedOutcomeStore_IsBounded(t *testing.T) {
	a := newMinimalTestAgent(t)
	for i := 0; i < delegatedOutcomeCap+50; i++ {
		a.storeDelegatedOutcome(DelegatedTaskOutcome{Model: "m", Outcome: OutcomeVerified})
	}
	if got := len(a.GetDelegatedTaskOutcomes()); got != delegatedOutcomeCap {
		t.Fatalf("len(out) = %d, want the cap %d", got, delegatedOutcomeCap)
	}
}

// --- Single path: outcome mapping (verified / escalated / failed) ---------

// newOutcomeRecordingAgent builds an agent with an event bus and a config
// manager whose coder/worker roles resolve to known models, so the outcome
// record can attribute the final model and the escalated coder model resolves
// deterministically.
func newOutcomeRecordingAgent(t *testing.T) (*Agent, *events.EventBus, *configuration.Manager) {
	t.Helper()
	a, bus, mgr := newWorkerEventAgent(t)
	setRoles(t, a, map[string]configuration.RoleConfig{
		configuration.RoleCoder:  {Provider: "openrouter", Model: "coder-model"},
		configuration.RoleWorker: {Provider: "zai", Model: "cheap-model"},
	})
	return a, bus, mgr
}

// TestRecordDelegatedOutcome_SinglePathVerified pins that a worker task that
// verified on the first pass is recorded under the worker model with the
// "verified" outcome and the task's tokens/cost.
func TestRecordDelegatedOutcome_SinglePathVerified(t *testing.T) {
	a, _, _ := newOutcomeRecordingAgent(t)
	spec := &subagentLaunchSpec{
		prompt:   "summarise the file",
		persona:  "worker",
		role:     configuration.RoleWorker,
		provider: "zai",
		model:    "cheap-model",
	}
	result := &SubagentResult{TokensUsed: 100, PromptTokens: 60, CompletionTokens: 40, Cost: 0.05}

	// No escalation: workerBefore == result (same pointer).
	a.recordDelegatedOutcome(spec, result, result, workerVerifyResult{Verified: true})

	out := a.GetDelegatedTaskOutcomes()
	if len(out) != 1 {
		t.Fatalf("len(out) = %d, want 1", len(out))
	}
	o := out[0]
	if o.Model != "cheap-model" || o.Provider != "zai" {
		t.Errorf("model/provider = %q/%q, want the worker model zai/cheap-model", o.Model, o.Provider)
	}
	if o.Role != configuration.RoleWorker {
		t.Errorf("role = %q, want %q", o.Role, configuration.RoleWorker)
	}
	if o.Outcome != OutcomeVerified {
		t.Errorf("outcome = %q, want %q", o.Outcome, OutcomeVerified)
	}
	if o.TaskType != TaskTypeSummarise {
		t.Errorf("task type = %q, want %q", o.TaskType, TaskTypeSummarise)
	}
	if o.Tokens != 100 || o.PromptTokens != 60 || o.CompletionTokens != 40 || o.Cost != 0.05 {
		t.Errorf("tokens/cost = %+v, want 100/60/40/0.05", o)
	}
}

// TestRecordDelegatedOutcome_SinglePathEscalated pins that a worker task whose
// escalation produced the final result is recorded under the CODER model with
// the "escalated" outcome — the model that actually produced the final result.
func TestRecordDelegatedOutcome_SinglePathEscalated(t *testing.T) {
	a, _, _ := newOutcomeRecordingAgent(t)
	spec := &subagentLaunchSpec{
		prompt:   "edit pkg/foo.go to add the guard",
		persona:  "worker",
		role:     configuration.RoleWorker,
		provider: "zai",
		model:    "cheap-model",
	}
	workerRes := &SubagentResult{TokensUsed: 100, Cost: 0.05}
	// The escalation ran: a distinct coder result is the final one.
	coderRes := &SubagentResult{TokensUsed: 200, PromptTokens: 150, CompletionTokens: 50, Cost: 0.30}

	a.recordDelegatedOutcome(spec, coderRes, workerRes, workerVerifyResult{Verified: true, Escalated: true})

	out := a.GetDelegatedTaskOutcomes()
	if len(out) != 1 {
		t.Fatalf("len(out) = %d, want 1", len(out))
	}
	o := out[0]
	if o.Outcome != OutcomeEscalated {
		t.Errorf("outcome = %q, want %q", o.Outcome, OutcomeEscalated)
	}
	if o.Role != configuration.RoleCoder {
		t.Errorf("role = %q, want the coder role (the escalation ran)", o.Role)
	}
	if o.Model != "coder-model" {
		t.Errorf("model = %q, want the coder role's model %q", o.Model, "coder-model")
	}
	if o.Tokens != 200 || o.Cost != 0.30 {
		t.Errorf("tokens/cost = %d/%.2f, want the escalated result's 200/0.30", o.Tokens, o.Cost)
	}

	// The worker's FIRST run produced no result (workerBefore nil) but the
	// escalation ran: the final is the distinct coder result. Even with
	// workerBefore nil, the record must be attributed to the coder model,
	// never to the worker.
	a.recordDelegatedOutcome(spec, &SubagentResult{TokensUsed: 250, Cost: 0.35}, nil, workerVerifyResult{Verified: true, Escalated: true})
	tail := a.GetDelegatedTaskOutcomes()
	last := tail[len(tail)-1]
	if last.Outcome != OutcomeEscalated || last.Role != configuration.RoleCoder {
		t.Errorf("nil-first-run escalation = %+v, want escalated/coder (not attributed to the worker)", last)
	}
	if last.Model != "coder-model" {
		t.Errorf("nil-first-run escalation model = %q, want the coder model %q", last.Model, "coder-model")
	}
}

// TestRecordDelegatedOutcome_SinglePathFailed pins the two failed shapes for a
// worker task: when the escalation ran but also failed, the coder model is
// recorded; when the escalation could not run, the worker's own result stands.
func TestRecordDelegatedOutcome_SinglePathFailed(t *testing.T) {
	a, _, _ := newOutcomeRecordingAgent(t)
	spec := &subagentLaunchSpec{
		prompt:   "edit pkg/foo.go to add the guard",
		persona:  "worker",
		role:     configuration.RoleWorker,
		provider: "zai",
		model:    "cheap-model",
	}

	// Escalation ran and also failed: final is a distinct coder result.
	coderFailed := &SubagentResult{TokensUsed: 200, Cost: 0.30}
	a.recordDelegatedOutcome(spec, coderFailed, &SubagentResult{}, workerVerifyResult{Verified: false, Escalated: true})
	out := a.GetDelegatedTaskOutcomes()
	if len(out) != 1 {
		t.Fatalf("len(out) = %d, want 1", len(out))
	}
	if out[0].Outcome != OutcomeFailed || out[0].Role != configuration.RoleCoder {
		t.Errorf("failed (escalated) record = %+v, want failed/coder", out[0])
	}

	// Escalation could not run: the worker's result stands (same pointer).
	workerRes := &SubagentResult{TokensUsed: 100, Cost: 0.05}
	a.recordDelegatedOutcome(spec, workerRes, workerRes, workerVerifyResult{Verified: false, Escalated: true})
	out = a.GetDelegatedTaskOutcomes()
	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want 2", len(out))
	}
	if out[1].Outcome != OutcomeFailed || out[1].Role != configuration.RoleWorker || out[1].Model != "cheap-model" {
		t.Errorf("failed (no escalation) record = %+v, want failed/worker/cheap-model", out[1])
	}
}

// TestRecordDelegatedOutcome_SinglePathNonWorker pins that a non-worker task
// (coder role) completes normally and is recorded under its own model with the
// "verified" outcome; a failed/aborted run is recorded "failed".
func TestRecordDelegatedOutcome_SinglePathNonWorker(t *testing.T) {
	a, _, _ := newOutcomeRecordingAgent(t)
	spec := &subagentLaunchSpec{
		prompt:   "implement the feature",
		persona:  "coder",
		role:     configuration.RoleCoder,
		provider: "openrouter",
		model:    "coder-model",
	}
	okRes := &SubagentResult{Output: "Implemented the feature and ran the tests.", TokensUsed: 80, Cost: 0.2}

	a.recordDelegatedOutcome(spec, okRes, nil, workerVerifyResult{})
	out := a.GetDelegatedTaskOutcomes()
	if len(out) != 1 {
		t.Fatalf("len(out) = %d, want 1", len(out))
	}
	if out[0].Outcome != OutcomeVerified || out[0].Role != configuration.RoleCoder || out[0].Model != "coder-model" {
		t.Errorf("completed non-worker = %+v, want verified/coder/coder-model", out[0])
	}

	failedRes := &SubagentResult{Error: context.DeadlineExceeded}
	a.recordDelegatedOutcome(spec, failedRes, nil, workerVerifyResult{})
	out = a.GetDelegatedTaskOutcomes()
	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want 2", len(out))
	}
	if out[1].Outcome != OutcomeFailed {
		t.Errorf("failed non-worker outcome = %q, want %q", out[1].Outcome, OutcomeFailed)
	}
}

// --- Parallel path: an outcome is recorded for each task -------------------

// TestRecordDelegatedOutcome_ParallelVerified pins that a worker task in a
// parallel batch that verifies is recorded under the worker model, alongside a
// non-worker task recorded under its own model.
func TestRecordDelegatedOutcome_ParallelVerified(t *testing.T) {
	parent, runner := newParallelRecordingAgent(t)
	tasks := []SubagentTask{
		{ID: "w1", Prompt: "summarise the file", Persona: "worker", Role: configuration.RoleWorker, Provider: "zai", Model: "cheap-model"},
		{ID: "c1", Prompt: "implement the feature", Persona: "coder", Role: configuration.RoleCoder, Provider: "openrouter", Model: "coder-role-model"},
	}
	results := []*SubagentResult{
		{ID: "w1", Output: "The file defines two helpers.", OutputComplete: true, TokensUsed: 50, Cost: 0.02},
		{ID: "c1", Output: "implemented the feature and ran the tests.", OutputComplete: true, TokensUsed: 40, Cost: 0.01},
	}

	verifyParallelWorkerResults(context.Background(), parent, tasks, results)

	out := parent.GetDelegatedTaskOutcomes()
	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want 2 (one per task)", len(out))
	}
	var w, c *DelegatedTaskOutcome
	for i := range out {
		switch {
		case out[i].Role == configuration.RoleWorker && out[i].Outcome == OutcomeVerified:
			w = &out[i]
		case out[i].Role == configuration.RoleCoder && out[i].Outcome == OutcomeVerified:
			c = &out[i]
		}
	}
	if w == nil || c == nil {
		t.Fatalf("records = %+v, want one worker-verified and one coder-verified", out)
	}
	if w.Model != "cheap-model" {
		t.Errorf("worker task model = %q, want cheap-model", w.Model)
	}
	if c.Model != "coder-role-model" {
		t.Errorf("coder task model = %q, want coder-role-model", c.Model)
	}
	_ = runner
}

// TestRecordDelegatedOutcome_ParallelEscalated pins that a worker task in a
// parallel batch that fails verification is escalated once on the coder model
// and — because the retry holds — recorded under that coder model with the
// "escalated" outcome.
func TestRecordDelegatedOutcome_ParallelEscalated(t *testing.T) {
	parent, runner := newParallelRecordingAgent(t)
	// The worker task requests a one-line summary but produces a multi-line
	// result, so the format check fails and the task escalates. The scripted
	// escalation produces a one-line result, so the retry holds.
	runner.testClientFactory = func(_ api.ClientType, model string) (api.ClientInterface, error) {
		return NewScriptedClient(NewScriptedResponseBuilder().Content("The build passed.").Build()), nil
	}
	tasks := []SubagentTask{
		{ID: "w1", Prompt: "summarise the file in one line", Persona: "worker", Role: configuration.RoleWorker, Provider: "zai", Model: "cheap-model", OutputFormat: "a one-line summary"},
	}
	results := []*SubagentResult{
		{ID: "w1", Output: "line one\nline two\nline three", OutputComplete: true, TokensUsed: 50, Cost: 0.02},
	}

	verifyParallelWorkerResults(context.Background(), parent, tasks, results)

	out := parent.GetDelegatedTaskOutcomes()
	if len(out) != 1 {
		t.Fatalf("len(out) = %d, want 1", len(out))
	}
	o := out[0]
	if o.Outcome != OutcomeEscalated {
		t.Errorf("outcome = %q, want %q (the retry held)", o.Outcome, OutcomeEscalated)
	}
	if o.Role != configuration.RoleCoder || o.Model != "coder-role-model" {
		t.Errorf("model/role = %q/%q, want the coder role model (coder-role-model/coder)", o.Model, o.Role)
	}
}

// --- Event: the outcome is published with the right payload ---------------

// TestDelegatedTaskOutcomeEvent_NoPromptText pins that the published event
// carries the model, provider, role, task type, outcome, tokens and cost, and
// carries NO prompt text.
func TestDelegatedTaskOutcomeEvent_NoPromptText(t *testing.T) {
	a, bus, _ := newOutcomeRecordingAgent(t)
	const prompt = "edit pkg/foo.go to add the guard"
	ch := bus.Subscribe("delegated-outcome-test")
	defer bus.Unsubscribe("delegated-outcome-test")

	spec := &subagentLaunchSpec{prompt: prompt, persona: "worker", role: configuration.RoleWorker, provider: "zai", model: "cheap-model"}
	result := &SubagentResult{TokensUsed: 100, PromptTokens: 60, CompletionTokens: 40, Cost: 0.05}
	a.recordDelegatedOutcome(spec, result, result, workerVerifyResult{Verified: true})

	ev, ok := waitForDelegatedOutcomeEvent(t, ch)
	if !ok {
		t.Fatalf("no %s event was published", events.EventTypeDelegatedTaskOutcome)
	}
	data, ok := ev.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("event data is %T, want a map", ev.Data)
	}
	for _, key := range []string{"model", "provider", "role", "task_type", "outcome", "tokens", "prompt_tokens", "completion_tokens", "cost"} {
		if _, present := data[key]; !present {
			t.Errorf("event payload missing key %q: %+v", key, data)
		}
	}
	if data["model"] != "cheap-model" || data["outcome"] != OutcomeVerified || data["task_type"] != TaskTypeEdit {
		t.Errorf("payload = %+v, want model=cheap-model outcome=verified task_type=edit", data)
	}
	// No prompt text anywhere in the payload.
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if strings.Contains(string(b), prompt) {
		t.Errorf("event payload leaked the prompt text: %s", string(b))
	}
}

// --- Usage-view summary: worker share + credits saved ---------------------

// TestDelegationSummary_WorkerShareAndCreditsSaved pins the summary math: the
// worker token share and the credits saved versus running the worker-model
// tasks on the coder model, computed from the coder model's catalog rates.
func TestDelegationSummary_WorkerShareAndCreditsSaved(t *testing.T) {
	a := newSummaryTestAgent(t)
	api.ResetPricingResolver()
	t.Cleanup(api.ResetPricingResolver)
	// Coder model rate: input $6.00/M, output $24.00/M.
	api.SeedPricingForTest("delegation-test", "coder-model", 6.0, 24.0, 0)

	// One worker-verified task: 100 prompt + 100 completion tokens, $0.001.
	// Coder cost for the same tokens = 100*6/1e6 + 100*24/1e6 = 0.003.
	// Savings = 0.003 - 0.001 = 0.002.
	a.storeDelegatedOutcome(DelegatedTaskOutcome{
		Role: configuration.RoleWorker, Outcome: OutcomeVerified,
		PromptTokens: 100, CompletionTokens: 100, Tokens: 200, Cost: 0.001,
	})
	// A coder-verified task (not worker work) and an escalated task (final
	// result on the coder model): neither counts as worker-model work.
	a.storeDelegatedOutcome(DelegatedTaskOutcome{
		Role: configuration.RoleCoder, Outcome: OutcomeVerified,
		PromptTokens: 150, CompletionTokens: 150, Tokens: 300, Cost: 0.1,
	})
	a.storeDelegatedOutcome(DelegatedTaskOutcome{
		Role: configuration.RoleCoder, Outcome: OutcomeEscalated,
		PromptTokens: 100, CompletionTokens: 100, Tokens: 200, Cost: 0.5,
	})

	s := a.GetDelegationSummary()
	if s.TaskCount != 3 {
		t.Errorf("TaskCount = %d, want 3", s.TaskCount)
	}
	if s.WorkerTaskCount != 1 {
		t.Errorf("WorkerTaskCount = %d, want 1", s.WorkerTaskCount)
	}
	if s.WorkerTokens != 200 || s.TotalTokens != 700 {
		t.Errorf("WorkerTokens/TotalTokens = %d/%d, want 200/700", s.WorkerTokens, s.TotalTokens)
	}
	wantShare := 200.0 / 700.0
	if s.WorkerTokenShare < wantShare-1e-9 || s.WorkerTokenShare > wantShare+1e-9 {
		t.Errorf("WorkerTokenShare = %f, want %f", s.WorkerTokenShare, wantShare)
	}
	if !s.CreditsSavedKnown {
		t.Fatalf("CreditsSavedKnown = false, want true (the coder rate is seeded)")
	}
	if s.CreditsSaved < 0.002-1e-9 || s.CreditsSaved > 0.002+1e-9 {
		t.Errorf("CreditsSaved = %f, want 0.002", s.CreditsSaved)
	}
}

// TestDelegationSummary_UnknownRateIsNotZero pins that when the coder model's
// rate cannot be resolved, the savings are reported as unknown (not $0) while
// the share is still computable.
func TestDelegationSummary_UnknownRateIsNotZero(t *testing.T) {
	a := newSummaryTestAgent(t)
	api.ResetPricingResolver()
	t.Cleanup(api.ResetPricingResolver)
	// No pricing seeded for the coder provider/model -> rate unknown.
	a.storeDelegatedOutcome(DelegatedTaskOutcome{
		Role: configuration.RoleWorker, Outcome: OutcomeVerified,
		PromptTokens: 100, CompletionTokens: 100, Tokens: 200, Cost: 0.001,
	})

	s := a.GetDelegationSummary()
	if s.WorkerTokenShare != 1.0 {
		t.Errorf("WorkerTokenShare = %f, want 1.0 (all delegated work is worker work)", s.WorkerTokenShare)
	}
	if s.CreditsSavedKnown {
		t.Errorf("CreditsSavedKnown = true, want false when the coder rate is unknown")
	}
	if s.CreditsSaved != 0 {
		t.Errorf("CreditsSaved = %f, want 0 (unknown, not fabricated)", s.CreditsSaved)
	}
}

// --- helpers --------------------------------------------------------------

// newParallelRecordingAgent builds a parent agent (with an isolated config and
// runner) whose coder/worker roles resolve to known models and whose runner
// test client factory drives the single escalation. The factory is set on
// the runner that escalateWorkerTask actually uses (the agent's cached
// subagent runner), so the escalation runs the scripted client.
func newParallelRecordingAgent(t *testing.T) (*Agent, *SubagentRunner) {
	t.Helper()
	parent, _ := newReviewTestRunner(t)
	setRoles(t, parent, map[string]configuration.RoleConfig{
		configuration.RoleCoder:  {Provider: "openrouter", Model: "coder-role-model"},
		configuration.RoleWorker: {Provider: "zai", Model: "cheap-model"},
	})
	runner := parent.GetSubagentRunner()
	runner.testClientFactory = func(_ api.ClientType, model string) (api.ClientInterface, error) {
		return NewScriptedClient(NewScriptedResponseBuilder().Content("escalated run finished the assigned work.").Build()), nil
	}
	return parent, runner
}

// newSummaryTestAgent builds a minimal agent with a config manager whose
// coder/worker roles resolve to a fake (unseeded) provider/model, so the
// summary's rate resolution is deterministic (unknown unless the test seeds
// the resolver).
func newSummaryTestAgent(t *testing.T) *Agent {
	t.Helper()
	a := newMinimalTestAgent(t)
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	a.configManager = mgr
	if err := mgr.UpdateConfigNoSave(func(c *configuration.Config) error {
		c.Roles = map[string]configuration.RoleConfig{
			configuration.RoleCoder:  {Provider: "delegation-test", Model: "coder-model"},
			configuration.RoleWorker: {Provider: "delegation-test", Model: "cheap-model"},
		}
		return nil
	}); err != nil {
		t.Fatalf("set roles: %v", err)
	}
	return a
}

// waitForDelegatedOutcomeEvent blocks until a delegated_task_outcome event
// arrives (or a timeout) and returns it.
func waitForDelegatedOutcomeEvent(t *testing.T, ch <-chan events.UIEvent) (events.UIEvent, bool) {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type == events.EventTypeDelegatedTaskOutcome {
				return ev, true
			}
		case <-timeout:
			return events.UIEvent{}, false
		}
	}
}
