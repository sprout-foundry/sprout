package agent

// Item 150.5 (SP-150 §150c): metering — every model call carries its role.
// These tests pin the per-role dimension on the cost model, the metrics
// manager, the usage ledger, the agent/subagent role stamping, and the
// language-guard metric (the per-model metric gains a role axis).

import (
	"context"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// addRoleTurn books one model call for a role the way the real build sites
// do: a cost entry (which feeds the per-role accumulator) plus the aggregate
// prompt/completion totals (SetPromptTokens/SetCompletionTokens), so the
// overall and per-role views stay in agreement.
func addRoleTurn(t *testing.T, a *Agent, role string, prompt, completion int, charged float64) {
	t.Helper()
	a.state.AddCostEntry(CostEntry{
		Role:             role,
		BillingType:      BillingPayPerToken,
		ChargedCost:      charged,
		PromptTokens:     prompt,
		CompletionTokens: completion,
	})
	a.state.SetPromptTokens(a.state.GetPromptTokens() + prompt)
	a.state.SetCompletionTokens(a.state.GetCompletionTokens() + completion)
}

// TestCostEntryRoleStampedByBuildSites pins that each of the three cost build
// sites stamps the agent's own role on the CostEntry (SP-150 §150c,
// item 150.5). The primary agent's role is the coder role.
func TestCostEntryRoleStampedByBuildSites(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()
	if got := a.GetRole(); got != configuration.RoleCoder {
		t.Fatalf("primary agent role = %q, want %q", got, configuration.RoleCoder)
	}

	// Agent runtime path (agent_runtime.go accumulateResponseCost).
	a.accumulateResponseCost(chatResponseWithUsage(10, 5))
	// Seed provider path (seed_provider_cost.go accumulateResponseCost).
	sp := &sproutProvider{agent: a}
	sp.accumulateResponseCost(chatResponseWithUsage(20, 10))
	// Metrics path (metrics.go TrackMetricsFromResponse).
	a.TrackMetricsFromResponse(30, 15, 45, 0.001, 0, 0, 0, 0)

	ru := a.GetRoleUsage()
	if len(ru) != 1 || ru[0].Role != configuration.RoleCoder {
		t.Fatalf("GetRoleUsage = %+v, want a single %q entry", ru, configuration.RoleCoder)
	}
	// All three build sites landed in the coder bucket: 10+20+30 prompt,
	// 5+10+15 completion.
	if ru[0].PromptTokens != 60 || ru[0].CompletionTokens != 30 {
		t.Errorf("coder tokens = %d/%d, want 60/30 (all build sites stamped the role)", ru[0].PromptTokens, ru[0].CompletionTokens)
	}
	if ru[0].Calls != 3 {
		t.Errorf("coder Calls = %d, want 3 (one per build site)", ru[0].Calls)
	}
}

// chatResponseWithUsage builds a minimal chat response for the cost build
// sites.
func chatResponseWithUsage(prompt, completion int) *api.ChatResponse {
	return &api.ChatResponse{
		Usage: api.ChatUsage{
			PromptTokens:     prompt,
			CompletionTokens: completion,
			TotalTokens:      prompt + completion,
		},
	}
}

// TestMetricsManagerPerRoleAggregation pins that AddCostEntry accumulates
// per-role totals (GetRoleUsage) while leaving the aggregate totals exactly
// as they were, and that the per-role sums agree with the aggregate sums.
func TestMetricsManagerPerRoleAggregation(t *testing.T) {
	m := NewAgentMetricsManager()

	m.AddCostEntry(CostEntry{Role: configuration.RoleCoder, BillingType: BillingPayPerToken, ChargedCost: 0.01, PromptTokens: 100, CompletionTokens: 10})
	m.AddCostEntry(CostEntry{Role: configuration.RoleCoder, BillingType: BillingPayPerToken, ChargedCost: 0.02, PromptTokens: 50, CompletionTokens: 5})
	m.AddCostEntry(CostEntry{Role: configuration.RoleReviewer, BillingType: BillingPayPerToken, ChargedCost: 0.05, PromptTokens: 200, CompletionTokens: 20})
	m.AddCostEntry(CostEntry{Role: "", BillingType: BillingPayPerToken, ChargedCost: 0.03, PromptTokens: 40, CompletionTokens: 4})

	byRole := map[string]RoleUsage{}
	for _, ru := range m.GetRoleUsage() {
		byRole[ru.Role] = ru
	}
	if got := byRole[configuration.RoleCoder]; got.PromptTokens != 150 || got.CompletionTokens != 15 || got.Calls != 2 {
		t.Errorf("coder role = %+v, want 150 prompt / 15 completion / 2 calls", got)
	}
	if got := byRole[configuration.RoleReviewer]; got.PromptTokens != 200 || got.CompletionTokens != 20 {
		t.Errorf("reviewer role = %+v, want 200 prompt / 20 completion", got)
	}
	// Empty role is bucketed under "unknown".
	if got := byRole["unknown"]; got.PromptTokens != 40 || got.CompletionTokens != 4 {
		t.Errorf("unknown role = %+v, want 40 prompt / 4 completion", got)
	}

	// Per-role charged-cost sums agree with the aggregate.
	var roleCharged float64
	for _, ru := range m.GetRoleUsage() {
		roleCharged += ru.ChargedCost
	}
	if roleCharged != m.GetChargedCostTotal() {
		t.Errorf("per-role charged sum = %v, want the aggregate %v", roleCharged, m.GetChargedCostTotal())
	}
	if m.GetChargedCostTotal() != 0.11 {
		t.Errorf("GetChargedCostTotal = %v, want 0.11 (unchanged aggregate accounting)", m.GetChargedCostTotal())
	}
}

// TestPrimaryAgentRoleIsCoder pins that a primary (main-loop) agent is
// attributed to the coder role (SP-150 §150c, item 150.5).
func TestPrimaryAgentRoleIsCoder(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()
	if got := a.GetRole(); got != configuration.RoleCoder {
		t.Errorf("primary agent role = %q, want %q", got, configuration.RoleCoder)
	}
}

// TestSubagentRoleFollowsOptions pins that a subagent created with
// SubagentOptions.Role carries that role, and an unset role falls back to
// the coder role (the default subagent resolution).
func TestSubagentRoleFollowsOptions(t *testing.T) {
	_, runner := newReviewTestRunner(t)

	sub, err := runner.createSubagent(SubagentOptions{Persona: "reviewer", Role: configuration.RoleReviewer}, context.Background())
	if err != nil {
		t.Fatalf("createSubagent (reviewer): %v", err)
	}
	defer sub.Shutdown()
	if got := sub.GetRole(); got != configuration.RoleReviewer {
		t.Errorf("reviewer subagent role = %q, want %q", got, configuration.RoleReviewer)
	}

	plain, err := runner.createSubagent(SubagentOptions{Persona: "coder"}, context.Background())
	if err != nil {
		t.Fatalf("createSubagent (coder, no role): %v", err)
	}
	defer plain.Shutdown()
	if got := plain.GetRole(); got != configuration.RoleCoder {
		t.Errorf("unset-role subagent role = %q, want the coder fallback %q", got, configuration.RoleCoder)
	}
}

// TestResolveSubagentProviderModelReturnsRole pins the spawn handler's role
// resolution: the reviewer persona resolves through the reviewer role, and
// the default (no-persona) path resolves through the coder role.
func TestResolveSubagentProviderModelReturnsRole(t *testing.T) {
	parent, _ := newReviewTestRunner(t)
	setRoles(t, parent, map[string]configuration.RoleConfig{
		configuration.RoleReviewer: {Provider: "openai", Model: "review-model"},
	})

	// Reviewer persona → the reviewer role drives the model.
	if _, _, role, _, err := resolveSubagentProviderModel(parent, "reviewer", true, t.TempDir()); err != nil {
		t.Fatal(err)
	} else if role != configuration.RoleReviewer {
		t.Errorf("reviewer persona role = %q, want %q", role, configuration.RoleReviewer)
	}

	// No persona → the default subagent resolution is the coder role.
	if _, _, role, _, err := resolveSubagentProviderModel(parent, "", false, t.TempDir()); err != nil {
		t.Fatal(err)
	} else if role != configuration.RoleCoder {
		t.Errorf("default subagent role = %q, want %q", role, configuration.RoleCoder)
	}
}

// TestUsageLedgerPerRoleDeltas pins that the ledger produces correct
// per-role deltas and that the overall and per-role views agree (the sum of
// the per-role deltas equals the overall delta).
func TestUsageLedgerPerRoleDeltas(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	// Turn 1: a coder turn and a reviewer turn.
	addRoleTurn(t, a, configuration.RoleCoder, 100, 10, 0.01)
	addRoleTurn(t, a, configuration.RoleReviewer, 50, 5, 0.02)

	byRole := a.TakeUnbookedUsageByRole()
	if got := byRole[configuration.RoleCoder]; got.PromptTokens != 100 || got.CompletionTokens != 10 {
		t.Errorf("turn-1 coder delta = %+v, want 100 prompt / 10 completion", got)
	}
	if got := byRole[configuration.RoleReviewer]; got.PromptTokens != 50 || got.CompletionTokens != 5 {
		t.Errorf("turn-1 reviewer delta = %+v, want 50 prompt / 5 completion", got)
	}
	overall := a.TakeUnbookedUsage()
	if overall.PromptTokens != 150 || overall.CompletionTokens != 15 {
		t.Errorf("turn-1 overall delta = %+v, want 150 prompt / 15 completion", overall)
	}
	// The sum of the per-role deltas equals the overall delta.
	if sum := sumPerRole(byRole); sum.PromptTokens != overall.PromptTokens || sum.CompletionTokens != overall.CompletionTokens {
		t.Errorf("per-role sum = %+v does not equal overall %+v", sum, overall)
	}

	// Turn 2: only the coder role accrues new usage.
	addRoleTurn(t, a, configuration.RoleCoder, 40, 4, 0.01)
	byRole = a.TakeUnbookedUsageByRole()
	if got := byRole[configuration.RoleCoder]; got.PromptTokens != 40 {
		t.Errorf("turn-2 coder delta = %+v, want only the new 40 prompt tokens", got)
	}
	if got := byRole[configuration.RoleReviewer]; got.PromptTokens != 0 {
		t.Errorf("turn-2 reviewer delta = %+v, want 0 (no new usage)", got)
	}
	overall = a.TakeUnbookedUsage()
	if overall.PromptTokens != 40 {
		t.Errorf("turn-2 overall delta = %+v, want 40 prompt", overall)
	}
	if sum := sumPerRole(byRole); sum.PromptTokens != overall.PromptTokens {
		t.Errorf("turn-2 per-role sum = %+v does not equal overall %+v", sum, overall)
	}

	// Nothing new: both views are empty.
	if again := a.TakeUnbookedUsageByRole(); len(again) != 0 {
		for role, u := range again {
			if u != (Usage{Role: role}) {
				t.Errorf("no-op per-role booking %s = %+v, want zero", role, u)
			}
		}
	}
	if again := a.TakeUnbookedUsage(); again != (Usage{}) {
		t.Errorf("no-op overall booking = %+v, want zero", again)
	}
}

// sumPerRole folds a per-role delta map into a single Usage (the sum the
// overall view reports).
func sumPerRole(m map[string]Usage) Usage {
	var sum Usage
	for role, u := range m {
		sum.PromptTokens += u.PromptTokens
		sum.CompletionTokens += u.CompletionTokens
		sum.ChargedCost += u.ChargedCost
		sum.TokenCost += u.TokenCost
		sum.Role = role
	}
	return sum
}

// TestLanguageGuardMetricsRoleDimension pins that the language-guard metric
// buckets by (model, role): the same model under two roles yields two
// entries, Snapshot exposes the role, and the per-model OverallRate is
// unchanged by the split.
func TestLanguageGuardMetricsRoleDimension(t *testing.T) {
	m := NewLanguageGuardMetrics()
	// model-a under two roles: one mismatch in each.
	m.Record("model-a", configuration.RoleCoder, true)
	m.Record("model-a", configuration.RoleReviewer, false)
	// model-b under a single role.
	m.Record("model-b", configuration.RoleCoder, true)

	snap := m.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("Snapshot = %d cells, want 3 (model-a/coder, model-a/reviewer, model-b/coder)", len(snap))
	}
	byCell := map[string]LanguageGuardModelStat{}
	for _, s := range snap {
		byCell[s.ModelID+"/"+s.Role] = s
	}
	if got := byCell["model-a/"+configuration.RoleCoder]; got.Checks != 1 || got.Mismatches != 1 {
		t.Errorf("model-a/coder = %+v, want Checks=1 Mismatches=1", got)
	}
	if got := byCell["model-a/"+configuration.RoleReviewer]; got.Checks != 1 || got.Mismatches != 0 {
		t.Errorf("model-a/reviewer = %+v, want Checks=1 Mismatches=0", got)
	}
	if got := byCell["model-b/"+configuration.RoleCoder]; got.Checks != 1 || got.Mismatches != 1 {
		t.Errorf("model-b/coder = %+v, want Checks=1 Mismatches=1", got)
	}

	// The role split must not change the overall rate: 2 mismatches / 3 checks.
	if got := m.OverallRate(); got != 2.0/3.0 {
		t.Errorf("OverallRate = %v, want %v (2/3 across all roles)", got, 2.0/3.0)
	}

	// An empty role is bucketed under "unknown".
	m.Record("model-c", "", true)
	snap2 := m.Snapshot()
	if !cellExists(snap2, "model-c", "unknown") {
		t.Errorf("empty role not bucketed under \"unknown\" in %+v", snap2)
	}
}

// cellExists reports whether a (model, role) cell is present in a snapshot.
func cellExists(snap []LanguageGuardModelStat, model, role string) bool {
	for _, s := range snap {
		if s.ModelID == model && s.Role == role {
			return true
		}
	}
	return false
}

// TestBookRoleUsageMetersExternalRoleCall pins the metering seam a
// role-serving capability uses to attribute its own model call:
// BookRoleUsage books the usage into the named role's bucket and
// advances the overall token totals, so the per-role totals keep summing
// to the overall totals. A zero usage is a no-op.
func TestBookRoleUsageMetersExternalRoleCall(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	a.BookRoleUsage(configuration.RoleSummarizer, 120, 20, 0.002)

	ru := a.GetRoleUsage()
	if len(ru) != 1 || ru[0].Role != configuration.RoleSummarizer {
		t.Fatalf("GetRoleUsage = %+v, want a single %q entry", ru, configuration.RoleSummarizer)
	}
	if ru[0].PromptTokens != 120 || ru[0].CompletionTokens != 20 || ru[0].Calls != 1 {
		t.Errorf("summarizer role = %+v, want 120/20/1", ru[0])
	}
	if a.GetPromptTokens() != 120 || a.GetCompletionTokens() != 20 || a.GetTotalTokens() != 140 {
		t.Errorf("overall tokens = %d/%d/%d, want 120/20/140", a.GetPromptTokens(), a.GetCompletionTokens(), a.GetTotalTokens())
	}

	// A fully-zero booking is a no-op (no phantom "calls").
	a.BookRoleUsage(configuration.RoleSummarizer, 0, 0, 0)
	if got := a.GetRoleUsage()[0].Calls; got != 1 {
		t.Errorf("Calls after a zero booking = %d, want 1", got)
	}
}
