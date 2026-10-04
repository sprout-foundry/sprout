//go:build !js

// todo_plan_scope_test.go — the SP-148 §148c item-148.6 acceptance tests:
// todo items carry an optional plan scope ID (the id of a scope[] entry in
// .sprout/plan.json, owned by pkg/plancontract). The scope ID links a todo
// back to the structured plan so progress maps to the plan and survives
// across sessions.
//
// The tests cover:
//
//   - a scripted agent turn: the model calls todo_write with todos that
//     carry a "scope" field; the TodoManager items emitted by the turn carry
//     the scope IDs ("emits todos with scope IDs"), and the scope field
//     survives a JSON marshal→unmarshal round-trip (the "persisted across
//     sessions" guarantee — the struct tag is the carrier).
//   - a focused unit test of coerceTodoItem: a raw todo map with "scope" (and
//     its common aliases) yields a TodoItem with Scope set, while a todo with
//     no scope yields an empty Scope.
//
// The scripted run mirrors the conventions in plan_structured_e2e_test.go
// (148.4) and plan_context_test.go (148.5): a fixture plan on disk, a scripted
// model client, and assertions on the on-struct state rather than the
// scripted fixture.

package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/planstore"
)

// ---------------------------------------------------------------------------
// Fixture
// ---------------------------------------------------------------------------

// tpsFixturePlanJSON is a minimal valid SP-148 plan with two scope items (s1,
// s2), each covered by one acceptance item (the validator's "every scope item
// needs at least one acceptance item" rule). revision is 2 — a persisted plan,
// so planstore returns it as-is. The scope ids are the ids a todo's "scope"
// field references.
const tpsFixturePlanJSON = `{
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

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// tpsWritePlanFile writes content to .sprout/plan.json under root (creating the
// .sprout directory), so the plan lives exactly where planstore looks.
func tpsWritePlanFile(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Dir(planstore.PlanJSONPath(root))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create .sprout dir: %v", err)
	}
	if err := os.WriteFile(planstore.PlanJSONPath(root), []byte(content), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
}

// tpsAgent wires client into a fresh agent whose workspace root is a temp
// project directory (the .sprout/ plan files land under it). It mirrors the
// agent setup from plan_context_test.go but accepts any api.ClientInterface so
// the scripted client's recorded requests can be inspected. The full context
// profile keeps the todo_write tool on the roster; SkipPrompt keeps the turn
// from ever blocking on an interactive prompt (stdin is closed in tests).
func tpsAgent(t *testing.T, client api.ClientInterface, workspaceRoot string) *Agent {
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

// tpsToolMessage returns the tool result message content for callID, or fails
// the test when the call was never dispatched.
func tpsToolMessage(t *testing.T, ag *Agent, callID string) string {
	t.Helper()
	for _, m := range ag.GetMessages() {
		if m.Role == "tool" && m.ToolCallID == callID {
			return m.Content
		}
	}
	t.Fatalf("no tool result message for call %q", callID)
	return ""
}

// tpsRoundTripTodos marshals todos to JSON and unmarshals them back, returning
// the restored slice. It is the "persisted across sessions" carrier check: the
// TodoItem struct tag (json:"scope,omitempty") must round-trip the scope field.
func tpsRoundTripTodos(t *testing.T, todos []tools.TodoItem) []tools.TodoItem {
	t.Helper()
	data, err := json.Marshal(todos)
	if err != nil {
		t.Fatalf("marshal todos: %v", err)
	}
	var restored []tools.TodoItem
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal todos: %v", err)
	}
	if len(restored) != len(todos) {
		t.Fatalf("round-trip changed length: got %d, want %d", len(restored), len(todos))
	}
	return restored
}

// ---------------------------------------------------------------------------
// The §148c scripted run: todos carry plan scope IDs
// ---------------------------------------------------------------------------

// TestTodoWriteEmitsTodosWithScopeIDs is the item's acceptance test: a scripted
// model turn calls todo_write with todos that include a "scope" field, and the
// TodoManager items emitted by the turn carry those scope IDs. The fixture
// plan provides the scope ids (s1, s2) so the link is meaningful.
func TestTodoWriteEmitsTodosWithScopeIDs(t *testing.T) {
	root := t.TempDir()
	tpsWritePlanFile(t, root, tpsFixturePlanJSON)

	const callID = "td_1"
	const args = `{"todos":[
		{"content":"Implement login","status":"in_progress","scope":"s1"},
		{"content":"Build session UI","status":"pending","scope":"s2"}
	]}`

	client := NewScriptedClient(
		NewScriptedToolCallResponse(callID, "todo_write", args, "Tracking the plan scopes."),
		NewScriptedTextResponse("Todos now carry their plan scope IDs."),
	)
	ag := tpsAgent(t, client, root)
	if !NewSeedToolRegistry(ag).HasTool("todo_write") {
		t.Skip("todo_write is not registered in this build")
	}

	if _, err := ag.ProcessQuery("Track the plan scope items."); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	// The tool must have been dispatched (a tool result exists and is not an
	// unknown-tool error).
	out := tpsToolMessage(t, ag, callID)
	if strings.Contains(out, "unknown tool") || strings.Contains(out, "not found") {
		t.Fatalf("todo_write was not dispatched, got: %s", out)
	}

	// The emitted todos carry their plan scope IDs.
	todos := ag.GetTodoManager().Read()
	if len(todos) != 2 {
		t.Fatalf("expected 2 todos in the manager, got %d", len(todos))
	}
	if todos[0].Content != "Implement login" {
		t.Errorf("todo[0].Content = %q, want %q", todos[0].Content, "Implement login")
	}
	if todos[0].Scope != "s1" {
		t.Errorf("todo[0].Scope = %q, want %q (the plan scope id)", todos[0].Scope, "s1")
	}
	if todos[1].Scope != "s2" {
		t.Errorf("todo[1].Scope = %q, want %q (the plan scope id)", todos[1].Scope, "s2")
	}

	// The "persisted across sessions" guarantee: the scope field survives a
	// JSON marshal→unmarshal round-trip (the struct tag is the carrier).
	restored := tpsRoundTripTodos(t, todos)
	for i := range todos {
		if restored[i].Scope != todos[i].Scope {
			t.Errorf("round-trip todo[%d].Scope = %q, want %q (scope must survive (de)serialization)",
				i, restored[i].Scope, todos[i].Scope)
		}
		if restored[i].Content != todos[i].Content {
			t.Errorf("round-trip todo[%d].Content = %q, want %q", i, restored[i].Content, todos[i].Content)
		}
	}
}

// ---------------------------------------------------------------------------
// Focused unit test: coerceTodoItem extracts scope (canonical key + aliases)
// ---------------------------------------------------------------------------

// TestCoerceTodoItem_Scope pins the scope-extraction half of the item: a raw
// todo map with "scope" (and its common aliases) yields a TodoItem with Scope
// set, while a todo with no scope key yields an empty Scope (unlinked).
func TestCoerceTodoItem_Scope(t *testing.T) {
	// The canonical "scope" key and the common aliases all map to
	// TodoItem.Scope (see coerceTodoItem's extractStringField call).
	aliasCases := []struct {
		name string
		key  string
	}{
		{"scope", "scope"},
		{"scopeId", "scopeId"},
		{"scope_id", "scope_id"},
		{"planScope", "planScope"},
		{"plan_scope", "plan_scope"},
	}
	for _, tc := range aliasCases {
		t.Run(tc.name, func(t *testing.T) {
			raw := map[string]interface{}{
				"content": "Implement login",
				"status":  "in_progress",
				tc.key:    "s1",
			}
			todo, err := coerceTodoItem(raw)
			if err != nil {
				t.Fatalf("coerceTodoItem: %v", err)
			}
			if todo.Scope != "s1" {
				t.Errorf("Scope from key %q = %q, want %q", tc.key, todo.Scope, "s1")
			}
			// The canonical fields must still be extracted alongside scope.
			if todo.Content != "Implement login" {
				t.Errorf("Content = %q, want %q", todo.Content, "Implement login")
			}
			if todo.Status != "in_progress" {
				t.Errorf("Status = %q, want %q", todo.Status, "in_progress")
			}
		})
	}

	// A todo with no scope key yields an empty Scope (the todo is not linked
	// to any plan scope).
	todo, err := coerceTodoItem(map[string]interface{}{
		"content": "Just a task",
		"status":  "pending",
	})
	if err != nil {
		t.Fatalf("coerceTodoItem: %v", err)
	}
	if todo.Scope != "" {
		t.Errorf("todo without a scope key: Scope = %q, want empty (unlinked)", todo.Scope)
	}
}
