//go:build !js

package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/planstore"
)

// ---------------------------------------------------------------------------
// write_plan (SP-148 §148b) conformance tests.
//
// The handler is the agent-facing write path for the structured plan:
// model document → plancontract.ValidateJSON → planstore.Save. The store
// re-validates, bumps the revision, stamps `updated`, and regenerates the
// .sprout/plan.md view, so these tests assert the on-disk outcomes (not
// just the tool result) — a turn that only *claims* to write the plan
// fails here.
// ---------------------------------------------------------------------------

// wphValidPlan is a minimal valid SP-148 plan document (one scope item
// covered by one acceptance item), shaped exactly as the JSON-decoded
// `plan` argument arrives from a tool call (numbers as the values
// json.Unmarshal produces are fine — the handler re-marshals to bytes).
var wphValidPlan = map[string]any{
	"version":  float64(1),
	"revision": float64(1),
	"created":  "2026-10-03T12:00:00Z",
	"updated":  "2026-10-03T12:00:00Z",
	"goal":     "Add user authentication",
	"scope": []any{
		map[string]any{"id": "s1", "title": "Auth API", "description": "Login and token endpoints"},
	},
	"steps": []any{
		map[string]any{"scope": "s1", "description": "Implement /login and /token endpoints"},
	},
	"acceptance": []any{
		map[string]any{"id": "a1", "scope": "s1", "check": "make build", "kind": "build"},
	},
	"out_of_scope": []any{},
}

// wphInvalidPlan is the same document with a second scope item that no
// acceptance item covers — the validator's "every scope item needs at
// least one acceptance item" rule (SP-148 §148b).
var wphInvalidPlan = map[string]any{
	"version":  float64(1),
	"revision": float64(1),
	"created":  "2026-10-03T12:00:00Z",
	"updated":  "2026-10-03T12:00:00Z",
	"goal":     "Add user authentication",
	"scope": []any{
		map[string]any{"id": "s1", "title": "Auth API"},
		map[string]any{"id": "s2", "title": "Session UI"},
	},
	"acceptance": []any{
		map[string]any{"id": "a1", "scope": "s1", "check": "make build", "kind": "build"},
	},
	"out_of_scope": []any{},
}

func TestWritePlanHandlerConformance_Definition(t *testing.T) {
	t.Parallel()
	h := &writePlanHandler{}

	if h.Name() != "write_plan" {
		t.Errorf("Name() = %q, want write_plan", h.Name())
	}

	d := h.Definition()
	if d.Name != "write_plan" {
		t.Errorf("Definition().Name = %q, want write_plan", d.Name)
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}
	for _, r := range d.Required {
		if r != "plan" {
			t.Errorf("Definition().Required = %v, want [plan]", d.Required)
		}
	}
	found := false
	for _, p := range d.Parameters {
		if p.Name == "plan" {
			found = true
			if p.Type != "object" {
				t.Errorf("plan parameter Type = %q, want object", p.Type)
			}
			if !p.Required {
				t.Error("plan parameter must be required")
			}
		}
	}
	if !found {
		t.Error("Definition() missing the 'plan' parameter")
	}
}

func TestWritePlanHandlerConformance_Validate(t *testing.T) {
	t.Parallel()
	h := &writePlanHandler{}

	if err := h.Validate(map[string]any{"plan": wphValidPlan}); err != nil {
		t.Errorf("Validate(valid plan object) = %v, want nil", err)
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(missing plan) should return an error")
	}
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil args) should return an error")
	}
	if err := h.Validate(map[string]any{"plan": "not an object"}); err == nil {
		t.Error("Validate(non-object plan) should return an error")
	}
}

// TestWritePlanHandlerConformance_ValidPlanWritesBothFiles drives the full
// success path: a valid document is validated, persisted through the plan
// store, and the markdown view is regenerated from the JSON.
func TestWritePlanHandlerConformance_ValidPlanWritesBothFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writePlanHandler{}
	env := ToolEnv{WorkspaceRoot: dir}

	res, err := h.Execute(context.Background(), env, map[string]any{"plan": wphValidPlan})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if res.IsError {
		t.Fatalf("Execute IsError for a valid plan, output: %s", res.Output)
	}

	// The JSON document (the source of truth) must be on disk and validate.
	data, err := os.ReadFile(planstore.PlanJSONPath(dir))
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
		t.Errorf("stored revision = %d, want 2", plan.Revision)
	}
	if plan.Created.IsZero() || plan.Updated.IsZero() {
		t.Errorf("timestamps must survive the round trip: created=%v updated=%v", plan.Created, plan.Updated)
	}

	// The rendered markdown view must exist and reflect the stored plan.
	md, err := os.ReadFile(planstore.PlanMarkdownPath(dir))
	if err != nil {
		t.Fatalf(".sprout/plan.md was not written: %v", err)
	}
	if !strings.Contains(string(md), "Add user authentication") {
		t.Errorf("plan.md must render the goal, got:\n%s", md)
	}
	if !strings.Contains(string(md), "**Revision:** 2") {
		t.Errorf("plan.md must show the stored revision, got:\n%s", md)
	}

	// The success result names the paths and the new revision.
	if !strings.Contains(res.Output, "plan.json") || !strings.Contains(res.Output, "revision 2") {
		t.Errorf("success output must name the paths and revision, got: %s", res.Output)
	}
}

// TestWritePlanHandlerConformance_EditingBumpsRevisionAgain pins the
// write-back semantics (SP-148 §148c): editing an existing plan and saving
// through the handler produces the next revision, with the markdown view
// regenerated on the edit.
func TestWritePlanHandlerConformance_EditingBumpsRevisionAgain(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writePlanHandler{}
	env := ToolEnv{WorkspaceRoot: dir}

	if res, err := h.Execute(context.Background(), env, map[string]any{"plan": wphValidPlan}); err != nil || res.IsError {
		t.Fatalf("first write failed: %v / %s", err, res.Output)
	}

	// Load the stored plan (as the agent would) and extend the scope with a
	// matching acceptance item.
	plan, err := planstore.New().Load(dir)
	if err != nil {
		t.Fatalf("load stored plan: %v", err)
	}
	plan.Scope = append(plan.Scope, plancontract.ScopeItem{ID: "s2", Title: "Session UI"})
	plan.Acceptance = append(plan.Acceptance, plancontract.Acceptance{
		ID:    "a2",
		Scope: "s2",
		Check: "/login renders",
		Kind:  plancontract.KindPage,
	})

	// Re-marshalling through the handler's own contract (map → bytes →
	// ValidateJSON → Save) is what the model path does.
	planBytes, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal edited plan: %v", err)
	}
	res, err := h.Execute(context.Background(), env, map[string]any{"plan": decodeToMap(t, planBytes)})
	if err != nil {
		t.Fatalf("Execute (edit) returned error: %v", err)
	}
	if res.IsError {
		t.Fatalf("Execute IsError for a valid edit, output: %s", res.Output)
	}

	stored, err := planstore.New().Load(dir)
	if err != nil {
		t.Fatalf("reload plan after edit: %v", err)
	}
	if stored.Revision != 3 {
		t.Errorf("stored revision after edit = %d, want 3 (the store bumps on every write)", stored.Revision)
	}
	if len(stored.AcceptanceForScope("s2")) == 0 {
		t.Error("the edit's acceptance item must be stored")
	}
	md, err := os.ReadFile(planstore.PlanMarkdownPath(dir))
	if err != nil {
		t.Fatalf("plan.md missing after edit: %v", err)
	}
	if !strings.Contains(string(md), "Session UI") || !strings.Contains(string(md), "**Revision:** 3") {
		t.Errorf("plan.md must reflect the edit, got:\n%s", md)
	}
}

// TestWritePlanHandlerConformance_InvalidPlanRejectedNamesEveryProblem
// covers the failure half of the SP-148 §148b acceptance: an invalid
// document is rejected with the aggregated validation problems, so the
// model knows exactly what to fix, and nothing is written.
func TestWritePlanHandlerConformance_InvalidPlanRejectedNamesEveryProblem(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writePlanHandler{}
	env := ToolEnv{WorkspaceRoot: dir}

	res, err := h.Execute(context.Background(), env, map[string]any{"plan": wphInvalidPlan})
	if err != nil {
		t.Fatalf("Execute returned a Go error (should be a tool error result): %v", err)
	}
	if !res.IsError {
		t.Fatalf("Execute must be IsError for an invalid plan, output: %s", res.Output)
	}
	if !strings.Contains(res.Output, "rejected") {
		t.Errorf("the result must report the rejection, got: %s", res.Output)
	}
	if !strings.Contains(res.Output, `scope item "s2" has no acceptance item`) {
		t.Errorf("the result must name the problem, got: %s", res.Output)
	}

	// Nothing may have been written.
	if _, statErr := os.Stat(planstore.PlanJSONPath(dir)); !os.IsNotExist(statErr) {
		t.Error(".sprout/plan.json must not exist after a rejected write")
	}
	if _, statErr := os.Stat(planstore.PlanMarkdownPath(dir)); !os.IsNotExist(statErr) {
		t.Error(".sprout/plan.md must not exist after a rejected write")
	}
}

// TestWritePlanHandlerConformance_RejectedWriteLeavesExistingPlanUntouched
// pins the store's atomicity through the handler: a rejected write leaves
// the previously stored plan byte-identical.
func TestWritePlanHandlerConformance_RejectedWriteLeavesExistingPlanUntouched(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writePlanHandler{}
	env := ToolEnv{WorkspaceRoot: dir}

	if res, err := h.Execute(context.Background(), env, map[string]any{"plan": wphValidPlan}); err != nil || res.IsError {
		t.Fatalf("first write failed: %v / %s", err, res.Output)
	}
	before, err := os.ReadFile(planstore.PlanJSONPath(dir))
	if err != nil {
		t.Fatalf("read stored plan: %v", err)
	}

	res, _ := h.Execute(context.Background(), env, map[string]any{"plan": wphInvalidPlan})
	if !res.IsError {
		t.Fatalf("invalid plan must be rejected, output: %s", res.Output)
	}
	after, err := os.ReadFile(planstore.PlanJSONPath(dir))
	if err != nil {
		t.Fatalf("the stored plan must still exist: %v", err)
	}
	if string(before) != string(after) {
		t.Error("a rejected write must leave the stored plan byte-identical")
	}
}

// TestWritePlanHandlerConformance_InteractionKindRules pin the §148d rules
// through the handler: an interaction item needs non-empty steps, and a
// non-interaction item may not carry steps.
func TestWritePlanHandlerConformance_InteractionKindRules(t *testing.T) {
	t.Parallel()
	h := &writePlanHandler{}

	missingSteps := clonePlanMap(t, wphValidPlan)
	missingSteps["acceptance"] = []any{
		map[string]any{"id": "a1", "scope": "s1", "check": "login flow", "kind": "interaction"},
	}
	res := executeWithPlan(t, h, t.TempDir(), missingSteps)
	if !res.IsError || !strings.Contains(res.Output, "is missing steps") {
		t.Errorf("an interaction item without steps must be rejected, got: %s (IsError=%v)", res.Output, res.IsError)
	}

	stepsOnBuild := clonePlanMap(t, wphValidPlan)
	stepsOnBuild["acceptance"] = []any{
		map[string]any{
			"id": "a1", "scope": "s1", "check": "make build", "kind": "build",
			"steps": []any{map[string]any{"action": "click", "selector": "#b"}},
		},
	}
	res = executeWithPlan(t, h, t.TempDir(), stepsOnBuild)
	if !res.IsError || !strings.Contains(res.Output, "only kind \"interaction\" may carry steps") {
		t.Errorf("a non-interaction item with steps must be rejected, got: %s (IsError=%v)", res.Output, res.IsError)
	}

	// A well-formed interaction item passes.
	goodInteraction := clonePlanMap(t, wphValidPlan)
	goodInteraction["acceptance"] = []any{
		map[string]any{
			"id": "a1", "scope": "s1", "check": "login flow", "kind": "interaction",
			"steps": []any{
				map[string]any{"action": "fill", "selector": "#email", "value": "a@b.c"},
				map[string]any{"action": "assert_text", "expect": "Welcome"},
			},
		},
	}
	res = executeWithPlan(t, h, t.TempDir(), goodInteraction)
	if res.IsError {
		t.Errorf("a well-formed interaction item must pass, got: %s", res.Output)
	}
}

func TestWritePlanHandlerConformance_NoWorkspaceRoot(t *testing.T) {
	t.Parallel()
	h := &writePlanHandler{}

	res, err := h.Execute(context.Background(), ToolEnv{}, map[string]any{"plan": wphValidPlan})
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Output, "no workspace root") {
		t.Errorf("without a workspace root the write must be refused, got: %s (IsError=%v)", res.Output, res.IsError)
	}
}

func TestWritePlanHandlerConformance_NonObjectPlanArgument(t *testing.T) {
	t.Parallel()
	h := &writePlanHandler{}
	env := ToolEnv{WorkspaceRoot: t.TempDir()}

	res, err := h.Execute(context.Background(), env, map[string]any{"plan": "not an object"})
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Output, "must be an object") {
		t.Errorf("a non-object plan argument must be refused, got: %s (IsError=%v)", res.Output, res.IsError)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// executeWithPlan runs the handler with planDoc and returns the tool result.
func executeWithPlan(t *testing.T, h *writePlanHandler, root string, planDoc map[string]any) (res ToolResult) {
	t.Helper()
	var err error
	res, err = h.Execute(context.Background(), ToolEnv{WorkspaceRoot: root}, map[string]any{"plan": planDoc})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return res
}

// decodeToMap re-decodes plan JSON bytes the way a tool call's arguments
// arrive (json.Unmarshal into map[string]any), so the handler's own
// re-marshal path is exercised.
func decodeToMap(t *testing.T, planBytes []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(planBytes, &m); err != nil {
		t.Fatalf("unmarshal plan bytes: %v", err)
	}
	return m
}

// clonePlanMap deep-copies a plan document map (via JSON) so table-driven
// mutations cannot leak between subtests.
func clonePlanMap(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal plan map: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal plan map: %v", err)
	}
	return out
}
