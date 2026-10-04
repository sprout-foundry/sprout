//go:build !js

// plan_structured_e2e_test.go — the SP-148 §148b item-148.4 acceptance
// tests for `sprout plan --structured`: the planning agent's write path for
// the structured plan document.
//
// The structured planning prompt (GetStructuredPlanningPrompt, wired by the
// --structured flag in cmd/plan.go) teaches the model the plan schema; the
// model persists the plan with the write_plan tool, which validates the
// document and writes it through the plan store (.sprout/plan.json plus the
// regenerated .sprout/plan.md view). These tests drive REAL agent turns
// with a scripted model, so the full dispatch path is exercised: the
// write_plan tool call the model sends must reach the handler, and the
// on-disk state is asserted rather than the scripted fixture.

package agent

import (
	"os"
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

// pseValidPlanJSON is a minimal valid SP-148 plan document: one scope item
// covered by one acceptance item, with the fields and timestamps the
// validator requires. revision is 1 — a new plan per the plancontract.New
// convention (the store bumps it on the write).
const pseValidPlanJSON = `{
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

// pseInvalidPlanJSON is the same plan with a second scope item (s2) that no
// acceptance item covers — the validator's "every scope item needs at
// least one acceptance item" rule (SP-148 §148b) must reject it.
const pseInvalidPlanJSON = `{
  "version": 1,
  "revision": 1,
  "created": "2026-10-03T12:00:00Z",
  "updated": "2026-10-03T12:00:00Z",
  "goal": "Add user authentication",
  "scope": [
    {"id": "s1", "title": "Auth API"},
    {"id": "s2", "title": "Session UI"}
  ],
  "acceptance": [
    {"id": "a1", "scope": "s1", "check": "make build", "kind": "build"}
  ],
  "out_of_scope": []
}`

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// pseAgent wires a scripted client into a fresh agent whose workspace root is
// a temp project directory (the .sprout/ plan files land under it). The full
// context profile keeps the write_plan tool on the roster; SkipPrompt keeps
// the turn from ever blocking on an interactive prompt (stdin is closed in
// tests).
func pseAgent(t *testing.T, client *ScriptedClient, workspaceRoot string) *Agent {
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

// pseToolMessage returns the tool result message content for callID, or
// fails the test when the call was never dispatched.
func pseToolMessage(t *testing.T, ag *Agent, callID string) string {
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
// The §148b scripted write path
// ---------------------------------------------------------------------------

// TestWritePlanStructured_ValidPlanIsWrittenAndRendered is the positive
// acceptance test: a scripted model turn calls write_plan with a VALID plan
// document, and the plan store writes .sprout/plan.json (validated) plus the
// regenerated .sprout/plan.md view.
func TestWritePlanStructured_ValidPlanIsWrittenAndRendered(t *testing.T) {
	root := t.TempDir()

	client := NewScriptedClient(
		NewScriptedToolCallResponse("wp_1", "write_plan",
			`{"plan":`+pseValidPlanJSON+`}`,
			"Writing the structured plan."),
		NewScriptedTextResponse("Wrote the structured plan for the auth work."),
	)
	ag := pseAgent(t, client, root)
	if !NewSeedToolRegistry(ag).HasTool("write_plan") {
		t.Skip("write_plan is not registered in this build")
	}

	if _, err := ag.ProcessQuery("Plan the auth feature and write the structured plan."); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	// The tool result must be a success naming the saved revision.
	out := pseToolMessage(t, ag, "wp_1")
	if strings.Contains(out, "unknown tool") || strings.Contains(out, "rejected") {
		t.Fatalf("write_plan must succeed on a valid plan, got: %s", out)
	}

	// .sprout/plan.json must exist and validate through the same entry
	// point every reader uses.
	data, err := os.ReadFile(planstore.PlanJSONPath(root))
	if err != nil {
		t.Fatalf(".sprout/plan.json was not written: %v", err)
	}
	plan, err := plancontract.ValidateJSON(data)
	if err != nil {
		t.Fatalf("the written plan must validate: %v", err)
	}
	if plan.Goal != "Add user authentication" {
		t.Errorf("stored goal = %q, want the scripted goal", plan.Goal)
	}
	// The model's new plan carries revision 1; the store bumps on every
	// write, so the first persisted plan is revision 2 (pinned by the
	// planstore round-trip tests, SP-148 §148b).
	if plan.Revision != 2 {
		t.Errorf("stored revision = %d, want 2 (first write of a revision-1 plan)", plan.Revision)
	}

	// The rendered markdown view must exist and reflect the stored plan.
	md, err := os.ReadFile(planstore.PlanMarkdownPath(root))
	if err != nil {
		t.Fatalf(".sprout/plan.md was not written: %v", err)
	}
	if !strings.Contains(string(md), "Add user authentication") {
		t.Errorf("plan.md must render the goal, got:\n%s", md)
	}
	if !strings.Contains(string(md), "**Revision:** 2") {
		t.Errorf("plan.md must show the stored revision, got:\n%s", md)
	}
}

// TestWritePlanStructured_InvalidPlanIsRejectedAndNothingIsWritten is the
// failure half of the acceptance test: a plan whose scope item has no
// acceptance item is rejected with the aggregated validation error (the
// model is told exactly what to fix), and no plan file is created.
func TestWritePlanStructured_InvalidPlanIsRejectedAndNothingIsWritten(t *testing.T) {
	root := t.TempDir()

	client := NewScriptedClient(
		NewScriptedToolCallResponse("wp_1", "write_plan",
			`{"plan":`+pseInvalidPlanJSON+`}`,
			"Writing the structured plan."),
		NewScriptedTextResponse("The plan had problems; not written yet."),
	)
	ag := pseAgent(t, client, root)
	if !NewSeedToolRegistry(ag).HasTool("write_plan") {
		t.Skip("write_plan is not registered in this build")
	}

	if _, err := ag.ProcessQuery("Write the structured plan."); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	out := pseToolMessage(t, ag, "wp_1")
	if !strings.Contains(out, "rejected") {
		t.Errorf("the tool result must report the rejection, got: %s", out)
	}
	if !strings.Contains(out, `scope item "s2" has no acceptance item`) {
		t.Errorf("the tool result must name the problem so the model can fix it, got: %s", out)
	}

	// Nothing may have been written.
	if _, statErr := os.Stat(planstore.PlanJSONPath(root)); !os.IsNotExist(statErr) {
		t.Error(".sprout/plan.json must not exist after a rejected write")
	}
	if _, statErr := os.Stat(planstore.PlanMarkdownPath(root)); !os.IsNotExist(statErr) {
		t.Error(".sprout/plan.md must not exist after a rejected write")
	}
}

// TestWritePlanStructured_StructuredPromptCarriesTheSchemaSection pins the
// prompt half of the item: the structured planning prompt must carry the
// schema section (with the every-scope-item-needs-an-acceptance-item rule),
// while the base prompt must not.
func TestWritePlanStructured_StructuredPromptCarriesTheSchemaSection(t *testing.T) {
	structuredTrue, err := GetStructuredPlanningPrompt(true)
	if err != nil {
		t.Fatalf("GetStructuredPlanningPrompt(true): %v", err)
	}
	structuredFalse, err := GetStructuredPlanningPrompt(false)
	if err != nil {
		t.Fatalf("GetStructuredPlanningPrompt(false): %v", err)
	}
	for _, want := range []string{
		"Structured Plan Schema",
		"write_plan",
		".sprout/plan.json",
		"Every scope item needs at least one acceptance item",
		"build", "test", "page", "interaction", "manual",
	} {
		if !strings.Contains(structuredTrue, want) {
			t.Errorf("the structured prompt must mention %q", want)
		}
	}
	// The schema section comes after the base body and before the
	// todo-integration suffix.
	if !strings.Contains(structuredTrue, "# Todo Integration") {
		t.Error("the structured prompt must keep the todo-integration suffix")
	}
	idxSchema := strings.Index(structuredTrue, "Structured Plan Schema")
	idxTodos := strings.Index(structuredTrue, "# Todo Integration")
	if idxSchema == -1 || idxTodos == -1 || idxSchema > idxTodos {
		t.Errorf("schema section (at %d) must precede the todo suffix (at %d)", idxSchema, idxTodos)
	}

	// Base prompts (both todo variants) must NOT carry the schema section —
	// it is advertised only in structured mode, and the WASM planning path
	// (cmd/wasm/agent_funcs.go) stays on the base prompt.
	for _, base := range []string{
		mustGetBasePlanningPrompt(t, true),
		mustGetBasePlanningPrompt(t, false),
	} {
		if strings.Contains(base, "Structured Plan Schema") {
			t.Errorf("the base planning prompt must not carry the schema section")
		}
		if strings.Contains(base, "STRUCTURED_PLAN_SCHEMA") {
			t.Errorf("the base planning prompt must not leak the schema markers")
		}
	}

	// The todo suffix still behaves per flag.
	if !strings.Contains(structuredTrue, "use the TodoWrite tool") {
		t.Error("structured prompt with todos=true must enable the todo integration")
	}
	if !strings.Contains(structuredFalse, "Disabled (user is managing tasks separately)") {
		t.Error("structured prompt with todos=false must disable the todo integration")
	}
}

func mustGetBasePlanningPrompt(t *testing.T, createTodos bool) string {
	t.Helper()
	p, err := GetEmbeddedPlanningPrompt(createTodos)
	if err != nil {
		t.Fatalf("GetEmbeddedPlanningPrompt(%v): %v", createTodos, err)
	}
	return p
}
