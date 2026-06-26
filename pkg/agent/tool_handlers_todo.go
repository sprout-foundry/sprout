package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// Tool handler implementations for todo and build validation operations

// statusNormalizationMap maps canonical lookup keys to canonical status
// values. Keys are produced by normalizeKey: lower-cased, non-alphanumeric
// bytes stripped. So "in-progress", "inProgress", "In Progress", and
// "in_progress" all collapse to the same key "inprogress".
var statusNormalizationMap = map[string]string{
	// → pending
	"todo":       "pending",
	"notstarted": "pending",
	"new":        "pending",
	"pending":    "pending",
	// → in_progress
	"inprogress": "in_progress",
	"started":    "in_progress",
	"active":     "in_progress",
	"doing":      "in_progress",
	"in progress": "in_progress",
	// → completed
	"done":      "completed",
	"complete":  "completed",
	"completed": "completed",
	"finished":  "completed",
	// → cancelled
	"cancelled": "cancelled",
	"canceled":  "cancelled",
	"skipped":   "cancelled",
	"cancel":    "cancelled",
}

// priorityNormalizationMap maps canonical lookup keys to canonical priority
// values. Same key normalization as statusNormalizationMap.
var priorityNormalizationMap = map[string]string{
	"high":     "high",
	"hi":       "high",
	"medium":   "medium",
	"med":      "medium",
	"normal":   "medium",
	"default":  "medium",
	"low":      "low",
	"lo":       "low",
}

// normalizeStatus converts common status variants to canonical values.
// Returns the original string unchanged if no normalization applies.
func normalizeStatus(status string) string {
	if status == "" {
		return status
	}
	if canonical, ok := statusNormalizationMap[normalizeKey(status)]; ok {
		return canonical
	}
	return status
}

// normalizePriority converts common priority variants to canonical values.
// Returns empty string if input is empty; returns the original string
// unchanged if no normalization applies (IsValidPriority will reject
// non-canonical values downstream, which is the desired behavior — we
// don't want to silently coerce unknown priorities).
func normalizePriority(priority string) string {
	if priority == "" {
		return ""
	}
	if canonical, ok := priorityNormalizationMap[normalizeKey(priority)]; ok {
		return canonical
	}
	return priority
}

// normalizeKey produces a canonical lookup key from a free-form status or
// priority string: lower-cased with all non-alphanumeric bytes stripped.
// "in-progress" → "inprogress", "In Progress" → "inprogress",
// "inProgress" → "inprogress". Used so the normalization tables tolerate
// common punctuation and casing variations without enumerating every form.
func normalizeKey(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		case r >= '0' && r <= '9':
			return r
		default:
			return -1
		}
	}, s)
}

// extractStringField extracts a string field from a map, trying the primary
// key first, then each alternative key. Returns empty string if not found
// or if the value is nil. Non-string values (numbers, bools) are stringified
// via fmt — nil is explicitly treated as absent so {"content": null} does
// not become the literal "<nil>".
func extractStringField(m map[string]interface{}, primary string, alternatives ...string) string {
	lookup := func(v interface{}) string {
		if v == nil {
			return ""
		}
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
	if v, ok := m[primary]; ok {
		if s := lookup(v); s != "" {
			return s
		}
	}
	for _, alt := range alternatives {
		if v, ok := m[alt]; ok {
			if s := lookup(v); s != "" {
				return s
			}
		}
	}
	return ""
}

// coerceTodoItem converts a single todo entry (map or bare string) into a TodoItem.
func coerceTodoItem(raw interface{}) (tools.TodoItem, error) {
	var todo tools.TodoItem

	// Bare-string item: treat the whole string as content with pending status
	if s, ok := raw.(string); ok {
		return tools.TodoItem{
			Content: s,
			Status:  "pending",
		}, nil
	}

	todoMap, ok := raw.(map[string]interface{})
	if !ok {
		return todo, agenterrors.NewInvalidInputError("each todo must be an object or string", nil)
	}

	// Extract content with fallbacks
	todo.Content = extractStringField(todoMap, "content", "text", "title", "description", "task", "name")
	if todo.Content == "" {
		return todo, agenterrors.NewInvalidInputError("each todo requires content", nil)
	}

	// Extract status with fallbacks and normalization
	status := extractStringField(todoMap, "status", "state", "STATUS", "Status")
	if status != "" {
		status = normalizeStatus(status)
	}
	if status == "" {
		return todo, agenterrors.NewInvalidInputError("each todo requires status", nil)
	}
	// Validate normalized status
	if !tools.IsValidStatus(status) {
		return todo, agenterrors.NewInvalidInputError(
			fmt.Sprintf("todo %q: %s", todo.Content, tools.FormatTodoStatusError(status)), nil)
	}
	todo.Status = status

	// Extract priority with fallbacks and normalization
	priority := extractStringField(todoMap, "priority", "Priority")
	if priority != "" {
		priority = normalizePriority(priority)
	}
	// Validate priority (empty is allowed)
	if !tools.IsValidPriority(priority) {
		return todo, agenterrors.NewInvalidInputError(
			fmt.Sprintf("todo %q: %s", todo.Content, tools.FormatTodoPriorityError(priority)), nil)
	}
	todo.Priority = priority

	// Extract id with fallbacks
	id := extractStringField(todoMap, "id", "task_id")
	if id != "" {
		todo.ID = tools.NormalizeTodoID(id)
	}

	// Extract activeForm with fallbacks
	todo.ActiveForm = extractStringField(todoMap, "activeForm", "active_form")

	return todo, nil
}

// coerceTodosFromArgs extracts and normalizes the todos array from tool args,
// handling common malformed inputs from models that don't follow the schema exactly.
//
// Two layers of fallback cover the realistic MiniMax failure modes:
//
//   1. Top-level key fallback. Seed core's resolveAlternativeNames (seed/core/
//      tool_registry_args.go:97) already remaps args["tasks"] / args["items"] /
//      args["task_list"] / args["todo_list"] → args["todos"] before the handler
//      runs — so the primary-key lookup below catches everything that layer
//      missed plus the canonical name. The explicit alternatives here are a
//      belt-and-suspenders second line of defense if the schema alternatives
//      are ever stripped.
//
//   2. Per-item coercion. seed core does NOT remap field names inside array
//      items — it only looks at top-level keys. So when MiniMax sends
//      {"todos": [{"text": "...", "state": "done"}]} we still need to coerce
//      the item fields ourselves (see coerceTodoItem).
//
//   3. String-encoded JSON. If the model JSON-encodes the value of "todos"
//      as a string instead of an inline array, we decode it back to an array.
func coerceTodosFromArgs(args map[string]interface{}) ([]tools.TodoItem, error) {
	todosRaw, ok := args["todos"]
	if !ok {
		// Fallback to alternative top-level keys. Seed core's
		// resolveAlternativeNames (seed/core/tool_registry_args.go:97)
		// remaps these at parse time, so this branch only fires for
		// direct (non-seed) callers — tests, alternative dispatchers.
		for _, key := range todoTopLevelAlternatives {
			if v, altOk := args[key]; altOk {
				todosRaw = v
				ok = true
				break
			}
		}
	}
	if !ok {
		return nil, agenterrors.NewInvalidInputError("missing todos argument (expected key \"todos\", array of {content, status, ...})", nil)
	}

	// If the value is a string, attempt to JSON-decode it (some models
	// double-encode the array). Only fall back to the type error if the
	// string is genuinely not a JSON array — that preserves the original
	// "todos must be an array" error for malformed inputs that the LLM
	// can learn from.
	if s, isString := todosRaw.(string); isString {
		var decoded []interface{}
		if err := json.Unmarshal([]byte(s), &decoded); err == nil {
			todosRaw = decoded
		}
	}

	todosSlice, ok := todosRaw.([]interface{})
	if !ok {
		return nil, agenterrors.NewInvalidInputError("todos must be an array", nil)
	}

	todos := make([]tools.TodoItem, 0, len(todosSlice))
	for _, raw := range todosSlice {
		todo, err := coerceTodoItem(raw)
		if err != nil {
			return nil, err
		}
		todos = append(todos, todo)
	}

	return todos, nil
}

// todoTopLevelAlternatives is the handler-level fallback for the schema's
// parameter alternatives on "todos". Seed core's resolveAlternativeNames
// already remaps these at parse time, but keeping the list here means the
// handler is self-contained if it's ever called directly (tests, alternative
// dispatchers).
var todoTopLevelAlternatives = []string{"tasks", "items", "task_list", "todo_list"}

// handleTodoWrite creates and manages a structured task list.
// Accepts malformed inputs from models that don't follow the schema exactly,
// including alternative parameter names, alternative field names, status/priority
// normalization, and bare-string items. See coerceTodosFromArgs and
// coerceTodoItem for the full coercion surface.
func handleTodoWrite(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Coerce the todos array from args (handles alternative keys, JSON strings, etc.)
	todos, err := coerceTodosFromArgs(args)
	if err != nil {
		return "", err
	}

	a.Logger().Debug("TodoWrite: processing %d todos\n", len(todos))
	result := a.GetTodoManager().Write(todos)
	a.Logger().Debug("TodoWrite result: %s\n", result)

	// CLI rendering: when there's no active browser and stdin is a TTY,
	// surface a bar-wrapped progress block on stdout. The webui path
	// renders the same data via the todo_update event.
	if !a.HasActiveWebUIClients() && !isNonInteractive() {
		tools.RenderTodosForCLI(os.Stdout, todos)
	}

	return result, nil
}

func handleTodoRead(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	a.Logger().Debug("TodoRead: returning current todo list\n")
	todos := a.GetTodoManager().Read()
	if len(todos) == 0 {
		return "No todos", nil
	}

	var result strings.Builder
	for _, todo := range todos {
		status := todo.Status
		if status == "in_progress" {
			status = "active"
		}
		result.WriteString(fmt.Sprintf("- [%s] %s\n", status[:1], todo.Content))
	}
	return result.String(), nil
}
