//go:build !js

package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/planstore"
)

// ---------------------------------------------------------------------------
// plan_add_scope conformance tests.
//
// The handler is the agent-facing scope write-back path: when scope changes
// during execution, the agent appends the new scope item (with its covering
// acceptance items) and persists the plan through the store — revision
// bumped, markdown view regenerated. These tests assert the on-disk
// outcomes (revision, plan contents, .sprout/plan.md), not just the tool
// result — a call that only *claims* to write the plan fails here.
// ---------------------------------------------------------------------------

// pasFixturePlanJSON is a minimal valid plan document: one scope item
// (s1) covered by one acceptance item (a1), revision 1 — the revision the
// fixture carries before a write (the store bumps it on every write).
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

// pasValidArgs is a valid plan_add_scope argument set: a new scope item (s2)
// with one covering page acceptance item, shaped exactly as the JSON-decoded
// arguments arrive from a tool call.
var pasValidArgs = map[string]any{
	"scope": map[string]any{
		"id":          "s2",
		"title":       "Session UI",
		"description": "Login and session pages",
	},
	"acceptance": []any{
		map[string]any{"id": "a2", "check": "/login renders", "kind": "page"},
	},
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// pasWritePlanFile writes the fixture plan to .sprout/plan.json under root
// (creating the .sprout directory) and renders the matching .sprout/plan.md
// view, so the on-disk state is what a real project has after a plan write
// (the same fixture convention as pkg/agent's plan_context_test.go).
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

// pasExecute runs the plan_add_scope handler in root and returns the tool
// result (failing the test on a Go error, which the handler never returns).
func pasExecute(t *testing.T, root string, args map[string]any) ToolResult {
	t.Helper()
	res, err := (&planAddScopeHandler{}).Execute(context.Background(), ToolEnv{WorkspaceRoot: root}, args)
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	return res
}

// pasStoredPlan loads the on-disk plan through the same entry point every
// reader uses (planstore.Load validates).
func pasStoredPlan(t *testing.T, root string) *plancontract.Plan {
	t.Helper()
	plan, err := planstore.New().Load(root)
	if err != nil {
		t.Fatalf("load stored plan: %v", err)
	}
	return plan
}

// ---------------------------------------------------------------------------
// Definition + Validate
// ---------------------------------------------------------------------------

func TestPlanAddScopeHandlerConformance_Definition(t *testing.T) {
	t.Parallel()
	h := &planAddScopeHandler{}

	if h.Name() != "plan_add_scope" {
		t.Errorf("Name() = %q, want plan_add_scope", h.Name())
	}

	d := h.Definition()
	if d.Name != "plan_add_scope" {
		t.Errorf("Definition().Name = %q, want plan_add_scope", d.Name)
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}
	if len(d.Required) != 2 || d.Required[0] != "scope" || d.Required[1] != "acceptance" {
		t.Errorf("Definition().Required = %v, want [scope acceptance]", d.Required)
	}

	byName := map[string]ParameterDef{}
	for _, p := range d.Parameters {
		byName[p.Name] = p
	}
	if p, ok := byName["scope"]; !ok {
		t.Error("Definition() missing the 'scope' parameter")
	} else if p.Type != "object" || !p.Required {
		t.Errorf("'scope' parameter = type %q required %v, want object/required", p.Type, p.Required)
	}
	if p, ok := byName["acceptance"]; !ok {
		t.Error("Definition() missing the 'acceptance' parameter")
	} else if p.Type != "array" || !p.Required {
		t.Errorf("'acceptance' parameter = type %q required %v, want array/required", p.Type, p.Required)
	} else if p.Items == nil {
		t.Error("'acceptance' (array) must declare Items (the item object schema)")
	}
	if p, ok := byName["goal"]; !ok {
		t.Error("Definition() missing the optional 'goal' parameter")
	} else if p.Type != "string" || p.Required {
		t.Errorf("'goal' parameter = type %q required %v, want string/optional", p.Type, p.Required)
	}
}

func TestPlanAddScopeHandlerConformance_Validate(t *testing.T) {
	t.Parallel()
	h := &planAddScopeHandler{}

	if err := h.Validate(pasValidArgs); err != nil {
		t.Errorf("Validate(valid args) = %v, want nil", err)
	}
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil args) should return an error")
	}
	if err := h.Validate(map[string]any{"acceptance": pasValidArgs["acceptance"]}); err == nil {
		t.Error("Validate(missing scope) should return an error")
	}
	if err := h.Validate(map[string]any{"scope": pasValidArgs["scope"]}); err == nil {
		t.Error("Validate(missing acceptance) should return an error")
	}
	if err := h.Validate(map[string]any{"scope": pasValidArgs["scope"], "acceptance": "not an array"}); err == nil {
		t.Error("Validate(non-array acceptance) should return an error")
	}
	if err := h.Validate(map[string]any{"scope": pasValidArgs["scope"], "acceptance": []any{}}); err == nil {
		t.Error("Validate(empty acceptance array) should return an error (a scope item needs a covering acceptance item)")
	}
	if err := h.Validate(map[string]any{"scope": pasValidArgs["scope"], "acceptance": []any{"not an object"}}); err == nil {
		t.Error("Validate(non-object acceptance item) should return an error")
	}
	if err := h.Validate(map[string]any{"scope": "not an object", "acceptance": pasValidArgs["acceptance"]}); err == nil {
		t.Error("Validate(non-object scope) should return an error")
	}
}

// ---------------------------------------------------------------------------
// The scope write-back: scope addition → revision bump + markdown view
// ---------------------------------------------------------------------------

// TestPlanAddScopeHandlerConformance_AddScopeBumpsRevisionAndMarkdown is the
// success half of the item: a valid scope addition is persisted through the
// store (revision bumped, markdown view regenerated), and the acceptance
// items are wired to the new scope id.
func TestPlanAddScopeHandlerConformance_AddScopeBumpsRevisionAndMarkdown(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pasWritePlanFile(t, dir, pasFixturePlanJSON)

	res := pasExecute(t, dir, pasValidArgs)
	if res.IsError {
		t.Fatalf("Execute IsError for a valid scope addition, output: %s", res.Output)
	}
	if !strings.Contains(res.Output, "revision 2") {
		t.Errorf("the success output must name the new revision, got: %s", res.Output)
	}

	plan := pasStoredPlan(t, dir)
	// The store bumps the revision on every write (fixture revision 1 → 2).
	if plan.Revision != 2 {
		t.Errorf("stored revision = %d, want 2 (the store bumps on every write)", plan.Revision)
	}
	// The new scope item and its covering acceptance item are present; the
	// existing scope item survives.
	if _, ok := plan.ScopeByID("s1"); !ok {
		t.Error("the existing scope item s1 must survive the write-back")
	}
	s2, ok := plan.ScopeByID("s2")
	if !ok {
		t.Fatalf("the new scope item s2 must be present")
	}
	if s2.Title != "Session UI" || s2.Description != "Login and session pages" {
		t.Errorf("stored s2 = %+v, want the scripted title/description", s2)
	}
	acceptance := plan.AcceptanceForScope("s2")
	if len(acceptance) != 1 {
		t.Fatalf("s2 must be covered by exactly one acceptance item, got %d", len(acceptance))
	}
	if acceptance[0].ID != "a2" || acceptance[0].Kind != plancontract.KindPage || acceptance[0].Check != "/login renders" {
		t.Errorf("stored a2 = %+v, want the scripted page check", acceptance[0])
	}
	// The acceptance item is wired to the new scope id.
	if acceptance[0].Scope != "s2" {
		t.Errorf("stored a2.scope = %q, want %q (wired to the new scope item)", acceptance[0].Scope, "s2")
	}
	// The original acceptance item survives.
	if len(plan.AcceptanceForScope("s1")) != 1 {
		t.Error("the existing acceptance item a1 must survive the write-back")
	}

	// The rendered markdown view was regenerated to include the new scope.
	md, err := os.ReadFile(planstore.PlanMarkdownPath(dir))
	if err != nil {
		t.Fatalf(".sprout/plan.md was not written: %v", err)
	}
	if !strings.Contains(string(md), "Session UI") {
		t.Errorf("plan.md must render the new scope, got:\n%s", md)
	}
	if !strings.Contains(string(md), "**Revision:** 2") {
		t.Errorf("plan.md must show the stored revision, got:\n%s", md)
	}

	// The structured output carries the machine-readable facts.
	so, ok := res.StructuredOut.(map[string]any)
	if !ok {
		t.Fatalf("StructuredOut = %T, want map[string]any", res.StructuredOut)
	}
	if so["scope_id"] != "s2" || so["revision"] != 2 {
		t.Errorf("StructuredOut scope_id/revision = %v/%v, want s2/2", so["scope_id"], so["revision"])
	}
}

// TestPlanAddScopeHandlerConformance_EmptyAcceptanceRejectedNothingWritten is
// the negative half of the item: a scope item without a covering acceptance
// item is rejected up front (the plan would be invalid), and nothing is
// written — the plan keeps its revision and the markdown view is untouched.
func TestPlanAddScopeHandlerConformance_EmptyAcceptanceRejectedNothingWritten(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pasWritePlanFile(t, dir, pasFixturePlanJSON)
	beforeJSON, err := os.ReadFile(planstore.PlanJSONPath(dir))
	if err != nil {
		t.Fatalf("read fixture plan: %v", err)
	}
	beforeMD, err := os.ReadFile(planstore.PlanMarkdownPath(dir))
	if err != nil {
		t.Fatalf("read fixture plan.md: %v", err)
	}

	args := clonePlanMap(t, pasValidArgs)
	args["acceptance"] = []any{}
	res := pasExecute(t, dir, args)
	if !res.IsError {
		t.Fatalf("Execute must be IsError for a scope without a covering acceptance item, output: %s", res.Output)
	}
	if !strings.Contains(res.Output, "at least one") {
		t.Errorf("the result must say a covering acceptance item is required, got: %s", res.Output)
	}

	// Nothing was written: the plan and its markdown view are byte-identical
	// and the revision was not incremented.
	afterJSON, err := os.ReadFile(planstore.PlanJSONPath(dir))
	if err != nil {
		t.Fatalf("the stored plan must still exist: %v", err)
	}
	if string(beforeJSON) != string(afterJSON) {
		t.Error("a rejected write must leave the stored plan byte-identical")
	}
	afterMD, err := os.ReadFile(planstore.PlanMarkdownPath(dir))
	if err != nil {
		t.Fatalf("the stored markdown view must still exist: %v", err)
	}
	if string(beforeMD) != string(afterMD) {
		t.Error("a rejected write must leave the markdown view byte-identical")
	}
	plan := pasStoredPlan(t, dir)
	if plan.Revision != 1 {
		t.Errorf("stored revision = %d, want 1 (a rejected write must not bump it)", plan.Revision)
	}
	if _, ok := plan.ScopeByID("s2"); ok {
		t.Error("the rejected scope item s2 must not be stored")
	}
}

// TestPlanAddScopeHandlerConformance_DuplicateScopeIDRejected pins the store
// validation half of the write-back: the input is well-formed, but the
// resulting plan is invalid (duplicate scope id), so the store rejects the
// write with the aggregated problem list and writes nothing.
func TestPlanAddScopeHandlerConformance_DuplicateScopeIDRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pasWritePlanFile(t, dir, pasFixturePlanJSON)
	before, _ := os.ReadFile(planstore.PlanJSONPath(dir))

	args := clonePlanMap(t, pasValidArgs)
	args["scope"] = map[string]any{"id": "s1", "title": "Something Else"} // collides with the fixture scope id
	res := pasExecute(t, dir, args)
	if !res.IsError {
		t.Fatalf("Execute must be IsError for a duplicate scope id, output: %s", res.Output)
	}
	if !strings.Contains(res.Output, `duplicate scope id "s1"`) {
		t.Errorf("the result must name the duplicate scope id, got: %s", res.Output)
	}
	if !strings.Contains(res.Output, "nothing was written") {
		t.Errorf("the result must report that nothing was written, got: %s", res.Output)
	}
	after, _ := os.ReadFile(planstore.PlanJSONPath(dir))
	if string(before) != string(after) {
		t.Error("a rejected write must leave the stored plan byte-identical")
	}
}

// TestPlanAddScopeHandlerConformance_UnknownKindRejected pins the validator's
// kind check through the store: an acceptance item with an unknown kind is
// rejected with the valid kinds named, and nothing is written.
func TestPlanAddScopeHandlerConformance_UnknownKindRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pasWritePlanFile(t, dir, pasFixturePlanJSON)
	before, _ := os.ReadFile(planstore.PlanJSONPath(dir))

	args := clonePlanMap(t, pasValidArgs)
	args["acceptance"] = []any{
		map[string]any{"id": "a2", "check": "smoke", "kind": "smoke"},
	}
	res := pasExecute(t, dir, args)
	if !res.IsError {
		t.Fatalf("Execute must be IsError for an unknown kind, output: %s", res.Output)
	}
	if !strings.Contains(res.Output, `unknown kind "smoke"`) {
		t.Errorf("the result must name the unknown kind, got: %s", res.Output)
	}
	after, _ := os.ReadFile(planstore.PlanJSONPath(dir))
	if string(before) != string(after) {
		t.Error("a rejected write must leave the stored plan byte-identical")
	}
}

// TestPlanAddScopeHandlerConformance_InteractionWithoutStepsRejected pins the
// browse-step rule through the write-back: an interaction acceptance item without
// steps is rejected by the store, and nothing is written.
func TestPlanAddScopeHandlerConformance_InteractionWithoutStepsRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pasWritePlanFile(t, dir, pasFixturePlanJSON)
	before, _ := os.ReadFile(planstore.PlanJSONPath(dir))

	args := clonePlanMap(t, pasValidArgs)
	args["acceptance"] = []any{
		map[string]any{"id": "a2", "check": "login flow", "kind": "interaction"},
	}
	res := pasExecute(t, dir, args)
	if !res.IsError {
		t.Fatalf("Execute must be IsError for an interaction item without steps, output: %s", res.Output)
	}
	if !strings.Contains(res.Output, "is missing steps") {
		t.Errorf("the result must name the missing steps, got: %s", res.Output)
	}
	after, _ := os.ReadFile(planstore.PlanJSONPath(dir))
	if string(before) != string(after) {
		t.Error("a rejected write must leave the stored plan byte-identical")
	}
}

// TestPlanAddScopeHandlerConformance_InteractionWithStepsSucceeds pins the
// browse-step happy path through the write-back: an interaction item with browse
// steps is stored, and the markdown view renders the steps.
func TestPlanAddScopeHandlerConformance_InteractionWithStepsSucceeds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pasWritePlanFile(t, dir, pasFixturePlanJSON)

	args := clonePlanMap(t, pasValidArgs)
	args["acceptance"] = []any{
		map[string]any{
			"id":   "a2",
			"kind": "interaction",
			"steps": []any{
				map[string]any{"action": "fill", "selector": "#email", "value": "a@b.c"},
				map[string]any{"action": "assert_text", "expect": "Welcome"},
			},
		},
	}
	res := pasExecute(t, dir, args)
	if res.IsError {
		t.Fatalf("Execute IsError for a well-formed interaction item, output: %s", res.Output)
	}

	plan := pasStoredPlan(t, dir)
	a2 := plan.AcceptanceForScope("s2")
	if len(a2) != 1 || a2[0].Kind != plancontract.KindInteraction {
		t.Fatalf("the stored interaction item = %+v, want one interaction item", a2)
	}
	if len(a2[0].Steps) != 2 {
		t.Fatalf("stored steps = %d, want 2", len(a2[0].Steps))
	}
	if a2[0].Steps[0].Action != "fill" || a2[0].Steps[0].Selector != "#email" || a2[0].Steps[0].Value != "a@b.c" {
		t.Errorf("stored step[0] = %+v, want the scripted fill step", a2[0].Steps[0])
	}
	if a2[0].Steps[1].Action != "assert_text" || a2[0].Steps[1].Expect != "Welcome" {
		t.Errorf("stored step[1] = %+v, want the scripted assert step", a2[0].Steps[1])
	}

	// The markdown view renders the steps under the acceptance item.
	md, err := os.ReadFile(planstore.PlanMarkdownPath(dir))
	if err != nil {
		t.Fatalf(".sprout/plan.md was not written: %v", err)
	}
	if !strings.Contains(string(md), "fill selector: #email value: a@b.c") {
		t.Errorf("plan.md must render the browse steps, got:\n%s", md)
	}
}

// TestPlanAddScopeHandlerConformance_CreatesPlanWhenAbsent pins the first-turn
// write-back: with no .sprout/plan.json yet, the call starts from a fresh
// plan (the goal from the optional 'goal' argument) and persists it.
func TestPlanAddScopeHandlerConformance_CreatesPlanWhenAbsent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir() // no .sprout/plan.json

	args := clonePlanMap(t, pasValidArgs)
	args["scope"] = map[string]any{"id": "s1", "title": "Auth API", "description": "Login and token endpoints"}
	args["acceptance"] = []any{map[string]any{"id": "a1", "check": "make build", "kind": "build"}}
	args["goal"] = "Add user authentication"
	res := pasExecute(t, dir, args)
	if res.IsError {
		t.Fatalf("Execute IsError for a first-turn write-back, output: %s", res.Output)
	}

	plan := pasStoredPlan(t, dir)
	if plan.Revision != 2 {
		t.Errorf("stored revision = %d, want 2 (a fresh plan is revision 1; the store bumps on the write)", plan.Revision)
	}
	if plan.Goal != "Add user authentication" {
		t.Errorf("stored goal = %q, want the scripted goal", plan.Goal)
	}
	if _, ok := plan.ScopeByID("s1"); !ok {
		t.Error("the scope item must be stored")
	}
	if len(plan.AcceptanceForScope("s1")) != 1 {
		t.Error("the acceptance item must be stored")
	}
	md, err := os.ReadFile(planstore.PlanMarkdownPath(dir))
	if err != nil {
		t.Fatalf(".sprout/plan.md was not written: %v", err)
	}
	if !strings.Contains(string(md), "Add user authentication") || !strings.Contains(string(md), "**Revision:** 2") {
		t.Errorf("plan.md must render the new plan, got:\n%s", md)
	}
}

// TestPlanAddScopeHandlerConformance_GoalFallsBackToScopeTitle pins the
// no-goal fallback: without the 'goal' argument, the new plan's goal is the
// scope item's title (a plan must carry a non-empty goal to validate).
func TestPlanAddScopeHandlerConformance_GoalFallsBackToScopeTitle(t *testing.T) {
	t.Parallel()
	dir := t.TempDir() // no .sprout/plan.json

	args := clonePlanMap(t, pasValidArgs)
	args["scope"] = map[string]any{"id": "s1", "title": "Auth API"}
	args["acceptance"] = []any{map[string]any{"id": "a1", "check": "make build", "kind": "build"}}
	// no "goal" key
	res := pasExecute(t, dir, args)
	if res.IsError {
		t.Fatalf("Execute IsError for a goal-less first-turn write-back, output: %s", res.Output)
	}
	plan := pasStoredPlan(t, dir)
	if plan.Goal != "Auth API" {
		t.Errorf("stored goal = %q, want the scope item's title", plan.Goal)
	}
}

// TestPlanAddScopeHandlerConformance_GoalIgnoredWhenPlanExists pins that the
// 'goal' argument only seeds a NEW plan: when a plan exists, its goal is
// preserved (a scope write-back never rewrites the plan's goal).
func TestPlanAddScopeHandlerConformance_GoalIgnoredWhenPlanExists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pasWritePlanFile(t, dir, pasFixturePlanJSON)

	args := clonePlanMap(t, pasValidArgs)
	args["goal"] = "Something Completely Different"
	res := pasExecute(t, dir, args)
	if res.IsError {
		t.Fatalf("Execute IsError with an existing plan, output: %s", res.Output)
	}
	plan := pasStoredPlan(t, dir)
	if plan.Goal != "Add user authentication" {
		t.Errorf("stored goal = %q, want the existing plan's goal (the 'goal' argument is ignored for existing plans)", plan.Goal)
	}
}

// TestPlanAddScopeHandlerConformance_NoWorkspaceRoot pins the write refusal
// when the handler has no workspace root (the plan files would have
// nowhere to land).
func TestPlanAddScopeHandlerConformance_NoWorkspaceRoot(t *testing.T) {
	t.Parallel()
	res, err := (&planAddScopeHandler{}).Execute(context.Background(), ToolEnv{}, pasValidArgs)
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Output, "no workspace root") {
		t.Errorf("without a workspace root the write must be refused, got: %s (IsError=%v)", res.Output, res.IsError)
	}
}
