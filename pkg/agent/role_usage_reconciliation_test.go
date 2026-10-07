package agent

// Per-role usage reconciliation. These tests pin
// the invariant that per-role totals sum to the overall total across the
// subagent/reviewer rollup and a state save/restore round-trip. The plan-agent
// role stamping is pinned in
// cmd/plan_roles_test.go; the role-aware cost-ledger booking is
// pinned in pkg/webui/cost_tracking_test.go.

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent/subagents"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// sumRoleUsage folds a per-role usage slice into aggregate prompt/completion
// tokens and charged cost.
func sumRoleUsage(usages []RoleUsage) (prompt, completion int, charged float64) {
	for _, ru := range usages {
		prompt += ru.PromptTokens
		completion += ru.CompletionTokens
		charged += ru.ChargedCost
	}
	return
}

// TestRollupSubagentUsage_AttributesToSubagentRole pins the sub-issue-1 fix:
// a completed subagent's usage is rolled up under the role that drove its
// model (not the parent's role) with its real prompt/completion token split,
// and the parent's per-role totals keep summing to the overall totals.
func TestRollupSubagentUsage_AttributesToSubagentRole(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	// The parent's own coder turn.
	addRoleTurn(t, a, configuration.RoleCoder, 100, 10, 0.01)

	// A reviewer subagent completes with its own role and token split.
	a.RollupSubagentUsage(&subagents.SubagentResult{
		Role:             configuration.RoleReviewer,
		TokensUsed:       30,
		Cost:             0.05,
		PromptTokens:     20,
		CompletionTokens: 10,
	})

	byRole := map[string]RoleUsage{}
	for _, e := range a.GetRoleUsage() {
		byRole[e.Role] = e
	}

	// The reviewer spend landed under the reviewer role (not the parent's
	// coder role) with its real token split.
	if got := byRole[configuration.RoleReviewer]; got.PromptTokens != 20 || got.CompletionTokens != 10 {
		t.Errorf("reviewer role = %+v, want 20 prompt / 10 completion", got)
	}
	// The parent's coder bucket is untouched by the subagent rollup.
	if got := byRole[configuration.RoleCoder]; got.PromptTokens != 100 || got.CompletionTokens != 10 {
		t.Errorf("coder role = %+v, want 100 prompt / 10 completion (parent's own turn only)", got)
	}

	// Per-role totals sum to the overall totals.
	ru := a.GetRoleUsage()
	rolePrompt, roleCompletion, _ := sumRoleUsage(ru)
	if rolePrompt != a.GetPromptTokens() {
		t.Errorf("per-role prompt sum = %d, want the overall %d", rolePrompt, a.GetPromptTokens())
	}
	if roleCompletion != a.GetCompletionTokens() {
		t.Errorf("per-role completion sum = %d, want the overall %d", roleCompletion, a.GetCompletionTokens())
	}
}

// TestRollupSubagentUsage_EmptyRoleFallsBackToCoder pins that a subagent
// result with no role is rolled up under the coder fallback (matching
// subagentRole's default resolution).
func TestRollupSubagentUsage_EmptyRoleFallsBackToCoder(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	a.RollupSubagentUsage(&subagents.SubagentResult{
		TokensUsed:       40,
		Cost:             0.02,
		PromptTokens:     30,
		CompletionTokens: 10,
	})

	byRole := map[string]RoleUsage{}
	for _, e := range a.GetRoleUsage() {
		byRole[e.Role] = e
	}
	if got := byRole[configuration.RoleCoder]; got.PromptTokens != 30 || got.CompletionTokens != 10 {
		t.Errorf("coder fallback role = %+v, want 30 prompt / 10 completion", got)
	}
}

// TestRoleUsageSurvivesStateRestore pins the sub-issue-4 fix: a
// conversation's per-role totals are persisted and restored, and after
// restore they still sum to the restored overall totals and are marked
// booked (so they are not re-booked).
func TestRoleUsageSurvivesStateRestore(t *testing.T) {
	source := newTestAgent(t)
	defer source.Shutdown()
	addRoleTurn(t, source, configuration.RoleCoder, 100, 10, 0.01)
	addRoleTurn(t, source, configuration.RoleReviewer, 50, 5, 0.02)

	snapshot, err := source.ExportState()
	if err != nil {
		t.Fatal(err)
	}

	a := newTestAgent(t)
	defer a.Shutdown()
	if err := a.ImportState(snapshot); err != nil {
		t.Fatal(err)
	}

	ru := a.GetRoleUsage()
	byRole := map[string]RoleUsage{}
	for _, e := range ru {
		byRole[e.Role] = e
	}
	if got := byRole[configuration.RoleCoder]; got.PromptTokens != 100 || got.CompletionTokens != 10 {
		t.Errorf("restored coder role = %+v, want 100 prompt / 10 completion", got)
	}
	if got := byRole[configuration.RoleReviewer]; got.PromptTokens != 50 || got.CompletionTokens != 5 {
		t.Errorf("restored reviewer role = %+v, want 50 prompt / 5 completion", got)
	}

	// Per-role totals still sum to the restored overall totals.
	rolePrompt, roleCompletion, _ := sumRoleUsage(ru)
	if rolePrompt != a.GetPromptTokens() {
		t.Errorf("restored per-role prompt sum = %d, want the overall %d", rolePrompt, a.GetPromptTokens())
	}
	if roleCompletion != a.GetCompletionTokens() {
		t.Errorf("restored per-role completion sum = %d, want the overall %d", roleCompletion, a.GetCompletionTokens())
	}

	// The restored per-role totals are marked booked: nothing is re-booked.
	for role, u := range a.TakeUnbookedUsageByRole() {
		if u != (Usage{Role: role}) {
			t.Errorf("restored history re-booked %s = %+v, want zero", role, u)
		}
	}
}
