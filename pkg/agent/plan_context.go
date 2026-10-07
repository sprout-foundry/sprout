// Package agent — plan-context injection.
//
// When a structured plan (.sprout/plan.json) exists for the project, the agent
// reads it at the start of a turn and injects a compact plan summary — the goal
// plus each scope item (id + title) with a status — into the turn's context so
// the model works scope item by scope item. When there is no
// plan, or the plan cannot be read or validated, nothing is injected and the
// turn is never failed: an absent or corrupt plan is "no plan", not an error.
//
// The per-scope status is deliberately self-contained. Completion tracking must
// not depend on the todo scope-ID work nor on scope write-back, so
// the plan carries no completion data and every scope item is reported as
// "pending" (not yet confirmed done) until a later change tracks progress.
package agent

import (
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/planstore"
)

// planScopeStatusPending is the per-scope status shown in the compact plan
// summary while completion tracking does not yet
// exist. The status is self-contained: the plan carries no completion data and
// this code must not read the todo scope IDs, so every scope item is reported
// as "pending" (not yet confirmed done). A later change will replace this value
// with a derived status (e.g. from todo scope IDs); the renderer stays the
// same.
const planScopeStatusPending = "pending"

// planContextSummary loads the project's structured plan and renders a compact
// summary for injection into the turn's context. It returns ""
// when the project has no plan (missing file) or when the plan is unreadable or
// invalid — an absent or corrupt plan never fails a turn.
//
// It is called from prepareQueryRun once per turn, so the summary always
// reflects the plan as it is on disk at the start of the turn.
func (a *Agent) planContextSummary() string {
	root := a.currentWorkspaceRoot()
	if root == "" {
		return ""
	}
	plan, err := planstore.New().Load(root)
	if err != nil {
		// planstore.ErrNoPlan (no plan yet) or a read/validate failure: treat
		// as "no plan" and inject nothing. Never surface an error here — the
		// absence of a valid plan is not a reason to fail a turn.
		return ""
	}
	return renderPlanSummary(plan)
}

// renderPlanSummary renders a compact, deterministic summary of plan for
// context injection: the goal, then each scope item (id +
// title) with its status. It is pure (no I/O) and short — a few lines of
// context, not the full plan document. It returns "" for a nil plan.
//
// The per-scope status is the plan's own default (planScopeStatusPending):
// completion is not tracked yet (that arrives with todo scope
// IDs and write-back), so every scope item renders as "pending" until a
// later change records progress.
func renderPlanSummary(plan *plancontract.Plan) string {
	if plan == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Active plan (rev %d): %s\n", plan.Revision, strings.TrimSpace(plan.Goal))
	for _, s := range plan.Scope {
		fmt.Fprintf(&b, "  [%s] %s: %s\n", planScopeStatusPending, s.ID, s.Title)
	}
	return strings.TrimRight(b.String(), "\n")
}
