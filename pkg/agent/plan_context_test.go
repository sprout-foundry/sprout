//go:build !js

// plan_context_test.go — the SP-148 §148c item-148.5 acceptance tests for
// plan-context injection. When .sprout/plan.json exists for the project, the
// agent reads it at the start of a turn and injects a compact plan summary
// (goal + scope items with status) into the turn's context; when there is no
// plan, the context is unchanged (no summary injected).
//
// The tests cover three levels:
//
//   - renderPlanSummary (pure): the compact renderer's content and shape.
//   - planContextSummary (agent method): a fixture plan on disk renders, a
//     missing plan and an invalid/corrupt plan both yield "" (never fail).
//   - a scripted agent turn: the summary reaches the model's context (the
//     first "system" message), and with no plan the context is unchanged.

package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/planstore"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// pcFixturePlanJSON is a minimal valid SP-148 plan: two scope items, each
// covered by one acceptance item (the validator's "every scope item needs at
// least one acceptance item" rule). revision is 2 — a persisted plan (the
// store bumps on each write), so Load returns it as-is and the renderer is
// deterministic.
const pcFixturePlanJSON = `{
  "version": 1,
  "revision": 2,
  "created": "2026-10-03T12:00:00Z",
  "updated": "2026-10-03T12:00:00Z",
  "goal": "Add user authentication",
  "scope": [
    {"id": "s1", "title": "Auth API", "description": "Login and token endpoints"},
    {"id": "s2", "title": "Session UI"}
  ],
  "steps": [
    {"scope": "s1", "description": "Implement /login and /token endpoints"},
    {"scope": "s2", "description": "Build the session UI"}
  ],
  "acceptance": [
    {"id": "a1", "scope": "s1", "check": "make build", "kind": "build"},
    {"id": "a2", "scope": "s2", "check": "login flow renders", "kind": "page"}
  ],
  "out_of_scope": []
}`

// pcCorruptPlanJSON is valid JSON bytes? No — it is NOT valid JSON at all.
// It exercises the "unreadable" half of planContextSummary (a decode failure,
// not a validation failure).
const pcCorruptPlanJSON = `{"version": 1, "revision": 2, `

// pcInvalidPlanJSON is valid JSON but fails plancontract.Validate: the
// revision is < 1 and the goal is empty. It exercises the "unreadable
// (invalid)" half of planContextSummary.
const pcInvalidPlanJSON = `{"version": 1, "revision": 0, "goal": ""}`

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// pcWritePlanFile writes content to .sprout/plan.json under root (creating the
// .sprout directory), so the plan lives exactly where planstore looks.
func pcWritePlanFile(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Dir(planstore.PlanJSONPath(root))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create .sprout dir: %v", err)
	}
	if err := os.WriteFile(planstore.PlanJSONPath(root), []byte(content), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
}

// pcAgent wires client into a fresh agent whose workspace root is a project
// directory (the .sprout/ plan files land under it). It mirrors the e2e agent
// setup from plan_structured_e2e_test.go but accepts any api.ClientInterface
// so the scripted client's recorded requests can be inspected. The full
// context profile keeps the tools on the roster; SkipPrompt keeps the turn
// from ever blocking on an interactive prompt (stdin is closed in tests).
func pcAgent(t *testing.T, client api.ClientInterface, workspaceRoot string) *Agent {
	t.Helper()

	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.ContextMode = configuration.ContextModeFull
		cfg.SkipPrompt = true
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
	ag.SetWorkspaceRoot(workspaceRoot)
	return ag
}

// pcFindSystemMessageWith reports whether any "system" message sent to the
// model (across all recorded requests) contains marker. The seed core sends
// the composed system prompt as the first "system" message, so the plan
// summary — when injected — is found here.
func pcFindSystemMessageWith(client *ScriptedClient, marker string) bool {
	for _, msgs := range client.GetSentRequests() {
		for _, m := range msgs {
			if m.Role == "system" && strings.Contains(m.Content, marker) {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Pure renderer
// ---------------------------------------------------------------------------

// TestRenderPlanSummary_GoalAndScopeWithStatus pins the compact summary shape:
// the goal, then each scope item (id + title) with its status, in document
// order.
func TestRenderPlanSummary_GoalAndScopeWithStatus(t *testing.T) {
	plan, err := plancontract.ValidateJSON([]byte(pcFixturePlanJSON))
	if err != nil {
		t.Fatalf("fixture plan must validate: %v", err)
	}

	summary := renderPlanSummary(plan)
	for _, want := range []string{
		"Active plan (rev 2)",
		"Add user authentication",
		"[pending] s1: Auth API",
		"[pending] s2: Session UI",
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary missing %q, got:\n%s", want, summary)
		}
	}

	// The summary is compact (goal line + one line per scope item) and
	// deterministic.
	lines := strings.Split(strings.TrimSpace(summary), "\n")
	if len(lines) != 3 { // header + 2 scope items
		t.Errorf("summary should be 3 lines (header + 2 scope items), got %d:\n%s", len(lines), summary)
	}
}

// TestRenderPlanSummary_NilPlanIsEmpty guards the nil-plan edge: no plan, no
// summary.
func TestRenderPlanSummary_NilPlanIsEmpty(t *testing.T) {
	if got := renderPlanSummary(nil); got != "" {
		t.Errorf("nil plan -> empty summary, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Agent method: load + render from the project's .sprout/plan.json
// ---------------------------------------------------------------------------

// TestPlanContextSummary_PresentWhenPlanExists is the "fixture plan -> summary
// present" half of the item: a valid plan on disk renders a compact summary.
func TestPlanContextSummary_PresentWhenPlanExists(t *testing.T) {
	root := t.TempDir()
	pcWritePlanFile(t, root, pcFixturePlanJSON)

	ag := NewTestAgent()
	ag.SetWorkspaceRoot(root)

	summary := ag.planContextSummary()
	for _, want := range []string{
		"Add user authentication",
		"[pending] s1: Auth API",
		"pending",
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("planContextSummary missing %q, got:\n%s", want, summary)
		}
	}
}

// TestPlanContextSummary_AbsentWhenNoPlan is the "no plan -> no change" half:
// a project with no .sprout/plan.json yields an empty summary.
func TestPlanContextSummary_AbsentWhenNoPlan(t *testing.T) {
	root := t.TempDir() // no .sprout/plan.json

	ag := NewTestAgent()
	ag.SetWorkspaceRoot(root)

	if got := ag.planContextSummary(); got != "" {
		t.Errorf("no plan -> empty summary, got %q", got)
	}
}

// TestPlanContextSummary_CorruptPlanIsIgnored: a file that is not valid JSON
// is "unreadable" — the summary is empty and the turn is never failed.
func TestPlanContextSummary_CorruptPlanIsIgnored(t *testing.T) {
	root := t.TempDir()
	pcWritePlanFile(t, root, pcCorruptPlanJSON)

	ag := NewTestAgent()
	ag.SetWorkspaceRoot(root)

	if got := ag.planContextSummary(); got != "" {
		t.Errorf("corrupt plan -> empty summary (turn never fails), got %q", got)
	}
}

// TestPlanContextSummary_InvalidPlanIsIgnored: valid JSON that fails
// validation is "unreadable (invalid)" — the summary is empty and the turn is
// never failed.
func TestPlanContextSummary_InvalidPlanIsIgnored(t *testing.T) {
	root := t.TempDir()
	pcWritePlanFile(t, root, pcInvalidPlanJSON)

	ag := NewTestAgent()
	ag.SetWorkspaceRoot(root)

	if got := ag.planContextSummary(); got != "" {
		t.Errorf("invalid plan -> empty summary (turn never fails), got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Scripted agent turn: the summary reaches the model's context
// ---------------------------------------------------------------------------

// TestPlanContextSummary_InjectedIntoTurnContext drives a real (scripted) turn
// over a fixture plan and asserts the compact summary — goal plus a scope item
// with its status — reaches the model's context (the first "system" message).
func TestPlanContextSummary_InjectedIntoTurnContext(t *testing.T) {
	root := t.TempDir()
	pcWritePlanFile(t, root, pcFixturePlanJSON)

	client := NewScriptedClient(
		NewScriptedTextResponse("Continuing the auth work."),
	)
	ag := pcAgent(t, client, root)
	if _, err := ag.ProcessQuery("Continue with the plan."); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	if !pcFindSystemMessageWith(client, "Add user authentication") {
		t.Errorf("the plan goal must reach the model's context (system message); it was not injected")
	}
	if !pcFindSystemMessageWith(client, "[pending] s1: Auth API") {
		t.Errorf("a scope item with status must reach the model's context (system message); it was not injected")
	}
}

// TestPlanContextSummary_NoPlanLeavesContextUnchanged drives a real (scripted)
// turn with NO plan file and asserts the fixture's plan markers do not appear
// in the model's context — i.e. no summary is injected, so the context is
// unchanged.
func TestPlanContextSummary_NoPlanLeavesContextUnchanged(t *testing.T) {
	root := t.TempDir() // no .sprout/plan.json

	client := NewScriptedClient(
		NewScriptedTextResponse("Done."),
	)
	ag := pcAgent(t, client, root)
	if _, err := ag.ProcessQuery("Do the thing."); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	if pcFindSystemMessageWith(client, "Add user authentication") {
		t.Errorf("no plan: the plan goal must NOT appear in the model's context (no summary injected)")
	}
	if pcFindSystemMessageWith(client, "[pending] s1: Auth API") {
		t.Errorf("no plan: the plan scope marker must NOT appear in the model's context (no summary injected)")
	}
}
