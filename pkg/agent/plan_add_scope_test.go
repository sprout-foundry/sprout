//go:build !js

// plan_add_scope_test.go — the scope write-back acceptance tests: when scope
// changes during execution, the agent updates
// the plan through the plan store (the plan_add_scope tool) with a new
// revision instead of silently diverging.
//
// The tests drive REAL agent turns with a scripted model (the same
// conventions as plan_structured_e2e_test.go): the fixture plan
// lives on disk under the project's .sprout/ directory, the scripted model
// calls plan_add_scope, and the assertions are on the on-disk state (the
// plan revision, the stored scope/acceptance items, and the regenerated
// .sprout/plan.md view) rather than on the scripted fixture. A rejected
// write leaves the plan byte-identical — no revision bump, no markdown
// change — which is the "never diverge" guarantee.

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

// pasFixturePlanJSON is a minimal valid plan document: one scope item
// (s1) covered by one acceptance item (a1). revision is 1 — the revision
// the fixture carries before the write-back (the store bumps it on the
// write, so the stored plan after a successful plan_add_scope call is
// revision 2).
const pasFixturePlanJSON = `{
  "version": 1,
  "revision": 1,
  "created": "2026-10-03T12:00:00Z",
  "updated": "2026-10-03T12:00:00Z",
  "goal": "Add user authentication",
  "scope": [
    {"id": "s1", "title": "Auth API", "description": "Login and token endpoints"}
  ],
  "steps": [
    {"scope": "s1", "description": "Implement /login and /token endpoints"}
  ],
  "acceptance": [
    {"id": "a1", "scope": "s1", "check": "make build", "kind": "build"}
  ],
  "out_of_scope": []
}`

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// pasWritePlanFile writes content to .sprout/plan.json under root (creating
// the .sprout directory) and renders the matching .sprout/plan.md view, so
// the plan lives exactly where the plan store looks (the same fixture
// convention as plan_context_test.go).
func pasWritePlanFile(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Dir(planstore.PlanJSONPath(root))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create .sprout dir: %v", err)
	}
	if err := os.WriteFile(planstore.PlanJSONPath(root), []byte(content), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	plan, err := plancontract.ValidateJSON([]byte(content))
	if err != nil {
		t.Fatalf("fixture plan must validate: %v", err)
	}
	if err := os.WriteFile(planstore.PlanMarkdownPath(root), []byte(planstore.RenderMarkdown(plan)), 0o644); err != nil {
		t.Fatalf("write plan.md: %v", err)
	}
}

// pasAgent wires client into a fresh agent whose workspace root is a
// project directory (the .sprout/ plan files land under it). It mirrors the
// agent setup from plan_structured_e2e_test.go: the full context profile
// keeps the plan tools on the roster; SkipPrompt keeps the turn from ever
// blocking on an interactive prompt (stdin is closed in tests).
func pasAgent(t *testing.T, client api.ClientInterface, workspaceRoot string) *Agent {
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

// pasToolMessage returns the tool result message content for callID, or
// fails the test when the call was never dispatched.
func pasToolMessage(t *testing.T, ag *Agent, callID string) string {
	t.Helper()
	for _, m := range ag.GetMessages() {
		if m.Role == "tool" && m.ToolCallID == callID {
			return m.Content
		}
	}
	t.Fatalf("no tool result message for call %q", callID)
	return ""
}

// ---------------------------------------------------------------------------
// The scripted write-back: scope addition → revision bump + markdown
// ---------------------------------------------------------------------------

// TestPlanAddScope_ScriptedScopeAdditionBumpsRevisionAndMarkdown is the
// positive acceptance test: a scripted model turn calls plan_add_scope to
// add a NEW scope item (s2) with a covering acceptance item, and the plan
// on disk now carries the incremented revision, the new scope/acceptance
// items, and the regenerated markdown view. The follow-up turn re-reads the
// plan: the updated summary reaches the model's context, so
// execution continues from the written-back plan instead of diverging.
func TestPlanAddScope_ScriptedScopeAdditionBumpsRevisionAndMarkdown(t *testing.T) {
	root := t.TempDir()
	pasWritePlanFile(t, root, pasFixturePlanJSON)

	client := NewScriptedClient(
		NewScriptedToolCallResponse("ps_1", "plan_add_scope",
			`{"scope":{"id":"s2","title":"Session UI","description":"Login and session pages"},`+
				`"acceptance":[{"id":"a2","check":"/login renders","kind":"page"}]}`,
			"Adding the new scope item to the plan."),
		NewScriptedTextResponse("Scope written back to the plan."),
		NewScriptedTextResponse("Continuing with the plan."),
	)
	ag := pasAgent(t, client, root)
	if !NewSeedToolRegistry(ag).HasTool("plan_add_scope") {
		t.Skip("plan_add_scope is not registered in this build")
	}

	if _, err := ag.ProcessQuery("Scope changed: add the session UI scope to the plan."); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	// The tool must have been dispatched and must have succeeded.
	out := pasToolMessage(t, ag, "ps_1")
	if strings.Contains(out, "unknown tool") || strings.Contains(out, "rejected") {
		t.Fatalf("plan_add_scope must succeed on a covering acceptance item, got: %s", out)
	}

	// The plan on disk now carries the bumped revision (fixture revision 1
	// + the store's write bump) and the new scope item, each acceptance item
	// wired to its scope.
	data, err := os.ReadFile(planstore.PlanJSONPath(root))
	if err != nil {
		t.Fatalf(".sprout/plan.json was not read: %v", err)
	}
	plan, err := plancontract.ValidateJSON(data)
	if err != nil {
		t.Fatalf("the stored plan must validate: %v", err)
	}
	if plan.Revision != 2 {
		t.Errorf("stored revision = %d, want 2 (the plan revision increments on the write-back)", plan.Revision)
	}
	if _, ok := plan.ScopeByID("s1"); !ok {
		t.Error("the existing scope item s1 must survive the write-back")
	}
	if _, ok := plan.ScopeByID("s2"); !ok {
		t.Error("the new scope item s2 must be present")
	}
	acceptance := plan.AcceptanceForScope("s2")
	if len(acceptance) != 1 || acceptance[0].ID != "a2" || acceptance[0].Kind != plancontract.KindPage {
		t.Errorf("the new scope item must be covered by the scripted acceptance item, got %+v", acceptance)
	}

	// The rendered markdown view was regenerated to include the new scope.
	md, err := os.ReadFile(planstore.PlanMarkdownPath(root))
	if err != nil {
		t.Fatalf(".sprout/plan.md was not written: %v", err)
	}
	if !strings.Contains(string(md), "Session UI") {
		t.Errorf("plan.md must render the new scope item, got:\n%s", md)
	}
	if !strings.Contains(string(md), "**Revision:** 2") {
		t.Errorf("plan.md must show the stored revision, got:\n%s", md)
	}

	// The next turn re-reads the plan: the written-back scope
	// item reaches the model's context, so execution continues from the
	// updated plan instead of diverging from it.
	if _, err := ag.ProcessQuery("Continue with the plan."); err != nil {
		t.Fatalf("ProcessQuery (second turn): %v", err)
	}
	if !pcFindSystemMessageWith(client, "[pending] s2: Session UI") {
		t.Error("the written-back scope item must reach the model's context on the next turn (the plan summary is re-read per turn)")
	}
}

// TestPlanAddScope_ScriptedScopeWithoutAcceptanceIsRejected is the negative
// acceptance test: a plan_add_scope call that adds a scope WITHOUT a
// covering acceptance item is rejected (the resulting plan would be
// invalid), and the plan is left untouched — no revision increment, no
// new scope item, no markdown change.
func TestPlanAddScope_ScriptedScopeWithoutAcceptanceIsRejected(t *testing.T) {
	root := t.TempDir()
	pasWritePlanFile(t, root, pasFixturePlanJSON)

	client := NewScriptedClient(
		NewScriptedToolCallResponse("ps_2", "plan_add_scope",
			`{"scope":{"id":"s3","title":"Reports"},"acceptance":[]}`,
			"Adding the reports scope."),
		NewScriptedTextResponse("The scope addition was rejected; adding a covering acceptance item."),
	)
	ag := pasAgent(t, client, root)
	if !NewSeedToolRegistry(ag).HasTool("plan_add_scope") {
		t.Skip("plan_add_scope is not registered in this build")
	}

	if _, err := ag.ProcessQuery("Add the reports scope to the plan."); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	// The tool result must tell the model that a covering acceptance item is
	// required (so it can fix and retry).
	out := pasToolMessage(t, ag, "ps_2")
	if !strings.Contains(out, "at least one") {
		t.Errorf("the tool result must say a covering acceptance item is required, got: %s", out)
	}

	// The plan is untouched: no revision increment, no new scope item, and
	// the markdown view does not mention the rejected scope.
	data, err := os.ReadFile(planstore.PlanJSONPath(root))
	if err != nil {
		t.Fatalf(".sprout/plan.json was not read: %v", err)
	}
	plan, err := plancontract.ValidateJSON(data)
	if err != nil {
		t.Fatalf("the stored plan must still validate: %v", err)
	}
	if plan.Revision != 1 {
		t.Errorf("stored revision = %d, want 1 (a rejected write must not increment the plan revision)", plan.Revision)
	}
	if _, ok := plan.ScopeByID("s3"); ok {
		t.Error("the rejected scope item s3 must not be stored")
	}
	md, err := os.ReadFile(planstore.PlanMarkdownPath(root))
	if err != nil {
		t.Fatalf(".sprout/plan.md was not read: %v", err)
	}
	if strings.Contains(string(md), "Reports") {
		t.Errorf("plan.md must not mention the rejected scope item, got:\n%s", md)
	}
}
