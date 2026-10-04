package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/planstore"
)

// planAddScopeHandler implements ToolHandler for plan_add_scope (SP-148
// §148c): the agent-facing scope write-back path for the project's structured
// plan.
//
// When the scope of the work changes during execution, the agent calls this
// tool instead of silently diverging from the plan: the new scope item and
// its acceptance items are appended to the current plan and persisted through
// the plan store (planstore.Store.Save), which re-validates, bumps the
// revision, stamps `updated`, and regenerates the derived .sprout/plan.md
// view. The compact plan summary (item 148.5) is re-read at the start of the
// next turn, so the model immediately sees the written-back plan.
//
// Coverage is enforced end to end: every scope item must be covered by at
// least one acceptance item (SP-148 §148b), so the tool requires the
// acceptance items for the new scope (each is wired to the new scope id by
// the store, not by the model) and the store's validation is the final gate
// — a rejected write (duplicate scope id, unknown kind, an interaction item
// without steps, ...) lists every problem and writes nothing.
//
// When the project has no .sprout/plan.json yet, the call starts from a fresh
// plan (plancontract.New) so scope write-back works from the first turn: the
// plan's goal comes from the optional `goal` argument, or falls back to the
// scope item's title (a plan must carry a non-empty goal to validate).
//
// Pure Go: plancontract and planstore have no browser or vision dependencies,
// so the handler ships on every platform — it is registered in the shared
// AllTools list (no build tag, no WASM stub), like write_plan. Writes are
// confined to the project's .sprout/ directory; the handler never writes
// elsewhere.
type planAddScopeHandler struct{}

func (h *planAddScopeHandler) Name() string { return "plan_add_scope" }

func (h *planAddScopeHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "plan_add_scope",
		Description: "Add a scope item to the project's structured plan (.sprout/plan.json) and persist it " +
			"through the plan store — scope write-back (SP-148 §148c). Use it when the scope of the work " +
			"changes during execution, so the plan is updated with a new revision instead of silently " +
			"diverging. Every scope item must be covered by at least one acceptance item, so the call " +
			"always includes the acceptance items for the new scope. On success the plan is saved " +
			"(revision bumped, .sprout/plan.md regenerated); on rejection nothing is written and every " +
			"problem is listed so the call can be fixed and retried.",
		Required: []string{"scope", "acceptance"},
		Parameters: []ParameterDef{
			{
				Name:     "scope",
				Type:     "object",
				Required: true,
				Description: "The new scope item to add: {id, title, description?}. " +
					"id is a unique scope item ID that must not collide with an existing scope ID; " +
					"title is a short name; description is an optional longer description.",
			},
			{
				Name:     "acceptance",
				Type:     "array",
				Required: true,
				Description: "The acceptance items covering the new scope item — at least one (SP-148 §148b); " +
					"the store wires each item's scope to the new scope id, so an item's own scope field is " +
					"ignored. Each item: {id, check?, kind, steps?}. kind is one of build|test|page|" +
					"interaction|manual; an interaction item carries a non-empty steps[] (browse step format: " +
					"each step has a required action plus selector, value, key, millis, script, expect, " +
					"screenshot_path as applicable); no other kind carries steps; check is optional for " +
					"kind manual.",
				Items: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":    map[string]any{"type": "string", "description": "Unique acceptance item ID (must not collide with an existing acceptance ID)"},
						"check": map[string]any{"type": "string", "description": "The concrete check (command, route, or steps reference); optional for kind manual"},
						"kind":  map[string]any{"type": "string", "enum": []any{"build", "test", "page", "interaction", "manual"}, "description": "The acceptance kind"},
						"steps": map[string]any{
							"type":        "array",
							"description": "Scripted browse steps; required non-empty for kind interaction, forbidden for every other kind",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"action":          map[string]any{"type": "string", "description": "The step verb (e.g. click, fill, assert_text)"},
									"selector":        map[string]any{"type": "string", "description": "CSS selector the step targets"},
									"value":           map[string]any{"type": "string", "description": "The value the step types or fills"},
									"key":             map[string]any{"type": "string", "description": "The keyboard key for press-style steps"},
									"millis":          map[string]any{"type": "integer", "description": "Wait time in milliseconds for sleep-style steps"},
									"script":          map[string]any{"type": "string", "description": "JavaScript snippet for eval-style steps"},
									"expect":          map[string]any{"type": "string", "description": "The expected value or text for assert-style steps"},
									"screenshot_path": map[string]any{"type": "string", "description": "File path for a screenshot_selector step's cropped element screenshot"},
								},
								"required": []any{"action"},
							},
						},
					},
					"required": []any{"id", "kind"},
				},
			},
			{
				Name:     "goal",
				Type:     "string",
				Required: false,
				Description: "Optional plan goal (one or two sentences of what the work is for), used only " +
					"when the project has no .sprout/plan.json yet: it becomes the goal of the new plan " +
					"this call creates. Ignored when a plan already exists (its goal is preserved). When " +
					"omitted, the new plan's goal falls back to the scope item's title.",
			},
		},
	}
}

func (h *planAddScopeHandler) Validate(args map[string]any) error {
	if args == nil {
		return agenterrors.NewValidation("plan_add_scope: 'scope' and 'acceptance' are required", nil)
	}
	scopeRaw, ok := lookupKey(args, "scope")
	if !ok || scopeRaw == nil {
		return agenterrors.NewValidation("plan_add_scope: 'scope' is required (the new scope item to add)", nil)
	}
	if _, ok := scopeRaw.(map[string]any); !ok {
		return agenterrors.NewValidation("plan_add_scope: 'scope' must be an object {id, title, description?}", nil)
	}
	accRaw, ok := lookupKey(args, "acceptance")
	if !ok || accRaw == nil {
		return agenterrors.NewValidation("plan_add_scope: 'acceptance' is required (at least one acceptance item covering the new scope)", nil)
	}
	accArr, ok := accRaw.([]any)
	if !ok {
		return agenterrors.NewValidation("plan_add_scope: 'acceptance' must be an array of acceptance items", nil)
	}
	if len(accArr) == 0 {
		return agenterrors.NewValidation(
			"plan_add_scope: 'acceptance' must contain at least one item (every scope item needs at least one acceptance item)", nil)
	}
	for i, item := range accArr {
		if _, ok := item.(map[string]any); !ok {
			return agenterrors.NewValidation(
				fmt.Sprintf("plan_add_scope: acceptance[%d] must be an object {id, check?, kind, steps?}", i), nil)
		}
	}
	return nil
}

func (h *planAddScopeHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	root := env.WorkspaceRoot
	if root == "" {
		return ToolResult{
			Output:  "plan_add_scope: no workspace root available — cannot write .sprout/plan.json",
			IsError: true,
		}, nil
	}

	// Validate the inputs before touching anything: on failure the model
	// gets a clear, actionable message and the plan is left untouched.
	scope, err := pasScopeFromArgs(args)
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}
	acceptance, err := pasAcceptanceFromArgs(args, scope.ID)
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}

	store := planstore.New()
	plan, err := store.Load(root)
	if err != nil {
		if !errors.Is(err, planstore.ErrNoPlan) {
			return ToolResult{
				Output:  fmt.Sprintf("plan_add_scope: could not read the existing plan, nothing was written: %v", err),
				IsError: true,
			}, nil
		}
		// No plan yet: start from a fresh plan so scope write-back works
		// from the first turn. The plan must carry a non-empty goal to be
		// valid: take it from the optional 'goal' argument, or fall back to
		// the scope item's title (required non-empty by pasScopeFromArgs —
		// if the title ever becomes optional, this path must require the
		// 'goal' argument instead).
		goal := ""
		if g, ok := lookupKey(args, "goal"); ok {
			goal = strings.TrimSpace(pasString(g))
		}
		if goal == "" {
			goal = scope.Title
		}
		plan = plancontract.New(goal, time.Now())
	}

	// The scope write-back: append the new scope item and its acceptance
	// items (each wired to the new scope id), then persist through the store.
	plan.Scope = append(plan.Scope, scope)
	plan.Acceptance = append(plan.Acceptance, acceptance...)

	stored, err := store.Save(root, plan)
	if err != nil {
		// A rejected write lists every problem (the model can fix and retry);
		// the store validated before touching disk, so nothing is written.
		return ToolResult{Output: pasSaveError(err), IsError: true}, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Scope %q (%s) added to the structured plan: %s (revision %d, schema v%d)\n",
		scope.ID, scope.Title, planstore.PlanJSONPath(root), stored.Revision, stored.Version)
	acceptanceIDs := make([]string, 0, len(acceptance))
	for _, a := range acceptance {
		kindDesc := plancontract.KindDescription(a.Kind)
		if kindDesc == "" {
			kindDesc = string(a.Kind)
		}
		fmt.Fprintf(&b, "  + acceptance %s (%s, %s): %s\n", a.ID, a.Scope, a.Kind, orDash(a.Check, kindDesc))
		acceptanceIDs = append(acceptanceIDs, a.ID)
	}
	b.WriteString("Rendered view: " + planstore.PlanMarkdownPath(root))

	return ToolResult{
		Output: b.String(),
		StructuredOut: map[string]any{
			"plan_json":      planstore.PlanJSONPath(root),
			"plan_markdown":  planstore.PlanMarkdownPath(root),
			"revision":       stored.Revision,
			"version":        stored.Version,
			"scope_id":       scope.ID,
			"acceptance_ids": acceptanceIDs,
		},
	}, nil
}

func (h *planAddScopeHandler) Aliases() []string      { return nil }
func (h *planAddScopeHandler) Timeout() time.Duration { return 0 }
func (h *planAddScopeHandler) MaxResultSize() int     { return 0 }
func (h *planAddScopeHandler) SafeForParallel() bool  { return false }
func (h *planAddScopeHandler) Interactive() bool      { return false }

// ---------------------------------------------------------------------------
// Argument extraction
// ---------------------------------------------------------------------------

// pasScopeFromArgs extracts the new scope item from the tool arguments:
// `scope` must be an object {id, title, description?} with a non-empty id
// and title. It returns an error the model can act on (nothing is written).
func pasScopeFromArgs(args map[string]any) (plancontract.ScopeItem, error) {
	raw, ok := lookupKey(args, "scope")
	if !ok || raw == nil {
		return plancontract.ScopeItem{}, fmt.Errorf("plan_add_scope: 'scope' is required (the new scope item to add)")
	}
	scopeMap, ok := raw.(map[string]any)
	if !ok {
		return plancontract.ScopeItem{}, fmt.Errorf("plan_add_scope: 'scope' must be an object {id, title, description?}, got %T", raw)
	}
	item := plancontract.ScopeItem{
		ID:          strings.TrimSpace(pasString(scopeMap["id"])),
		Title:       strings.TrimSpace(pasString(scopeMap["title"])),
		Description: strings.TrimSpace(pasString(scopeMap["description"])),
	}
	if item.ID == "" {
		return item, fmt.Errorf("plan_add_scope: 'scope.id' is required (a unique scope item ID, e.g. \"s2\")")
	}
	if item.Title == "" {
		return item, fmt.Errorf("plan_add_scope: 'scope.title' is required (a short name for the scope item)")
	}
	return item, nil
}

// pasAcceptanceFromArgs extracts the acceptance items from the tool
// arguments: `acceptance` must be a non-empty array of objects
// {id, check?, kind, steps?}. Each item is wired to the new scope id
// (SP-148 §148c write-back: the items cover the new scope item, so the
// store's "every scope item needs at least one acceptance item" rule is
// satisfied by construction). A scope without any covering acceptance item
// is rejected up front with a clear error (nothing is written).
func pasAcceptanceFromArgs(args map[string]any, scopeID string) ([]plancontract.Acceptance, error) {
	raw, ok := lookupKey(args, "acceptance")
	if !ok || raw == nil {
		return nil, fmt.Errorf(
			"plan_add_scope: 'acceptance' is required (at least one acceptance item covering the new scope; a scope item without an acceptance item is an invalid plan, SP-148 §148b)")
	}
	accArr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("plan_add_scope: 'acceptance' must be an array of acceptance items, got %T", raw)
	}
	if len(accArr) == 0 {
		return nil, fmt.Errorf(
			"plan_add_scope: 'acceptance' must contain at least one item: every scope item needs at least one acceptance item (SP-148 §148b)")
	}

	out := make([]plancontract.Acceptance, 0, len(accArr))
	for i, itemRaw := range accArr {
		itemMap, ok := itemRaw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("plan_add_scope: acceptance[%d] must be an object {id, check?, kind, steps?}, got %T", i, itemRaw)
		}
		a := plancontract.Acceptance{
			ID:    strings.TrimSpace(pasString(itemMap["id"])),
			Scope: scopeID, // wired to the new scope item (SP-148 §148c)
			Check: strings.TrimSpace(pasString(itemMap["check"])),
			Kind:  plancontract.Kind(strings.TrimSpace(pasString(itemMap["kind"]))),
		}
		if a.ID == "" {
			return nil, fmt.Errorf("plan_add_scope: acceptance[%d].id is required (a unique acceptance item ID, e.g. \"a2\")", i)
		}
		if a.Kind == "" {
			return nil, fmt.Errorf(
				"plan_add_scope: acceptance[%d] (id %q) is missing kind (required: one of %s)",
				i, a.ID, pasJoinKinds())
		}
		if stepsRaw, present := itemMap["steps"]; present && stepsRaw != nil {
			steps, err := pasStepsFromValue(stepsRaw)
			if err != nil {
				return nil, fmt.Errorf("plan_add_scope: acceptance[%d] (id %q): %v", i, a.ID, err)
			}
			a.Steps = steps
		}
		out = append(out, a)
	}
	return out, nil
}

// pasStepsFromValue converts a raw steps array (as it arrives in a tool call)
// to the plancontract browse-step form (SP-148 §148d). The action set
// itself is not validated here: the plan validator requires non-empty
// actions on interaction items, and the browse executor is the source of
// truth for which actions exist.
func pasStepsFromValue(raw any) ([]plancontract.BrowseStep, error) {
	rawArr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("'steps' must be an array of browse steps, got %T", raw)
	}
	steps := make([]plancontract.BrowseStep, 0, len(rawArr))
	for i, stepRaw := range rawArr {
		stepMap, ok := stepRaw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("steps[%d] must be an object (browse step), got %T", i, stepRaw)
		}
		st := plancontract.BrowseStep{
			Action:         strings.TrimSpace(pasString(stepMap["action"])),
			Selector:       pasString(stepMap["selector"]),
			Value:          pasString(stepMap["value"]),
			Key:            pasString(stepMap["key"]),
			Script:         pasString(stepMap["script"]),
			Expect:         pasString(stepMap["expect"]),
			ScreenshotPath: pasString(stepMap["screenshot_path"]),
		}
		// JSON numbers decode as float64; a direct Go call may pass an int.
		switch m := stepMap["millis"].(type) {
		case float64:
			st.Millis = int(m)
		case int:
			st.Millis = m
		}
		steps = append(steps, st)
	}
	return steps, nil
}

// pasSaveError renders a planstore.Save failure as a tool result the model
// can act on: a validation failure lists every problem (the model fixes the
// plan document and retries), any other failure reports that nothing was
// written (the store validates before touching disk).
func pasSaveError(err error) string {
	var vErr *plancontract.ValidationError
	if errors.As(err, &vErr) {
		problems := make([]string, len(vErr.Problems))
		for i, p := range vErr.Problems {
			problems[i] = "  - " + p
		}
		return fmt.Sprintf(
			"plan_add_scope: the plan update was rejected and nothing was written (%d problem(s)):\n%s\n"+
				"Fix the scope/acceptance items and call plan_add_scope again.",
			len(vErr.Problems), strings.Join(problems, "\n"),
		)
	}
	return fmt.Sprintf("plan_add_scope: failed to save the plan, nothing was written: %v", err)
}

// ---------------------------------------------------------------------------
// small helpers
// ---------------------------------------------------------------------------

// pasString coerces a JSON-decoded value to a string ("" for non-strings),
// so a mistyped field fails as an empty value with a clear required-field
// message instead of a type-assertion crash.
func pasString(v any) string {
	s, _ := v.(string)
	return s
}

// pasJoinKinds renders the plancontract acceptance kinds as "a, b, c" for
// error messages (the canonical order of plancontract.AllKinds).
func pasJoinKinds() string {
	ks := plancontract.AllKinds()
	parts := make([]string, 0, len(ks))
	for _, k := range ks {
		parts = append(parts, string(k))
	}
	return strings.Join(parts, ", ")
}

// orDash renders check, or a short kind description when check is empty (a
// manual item is reported, not run).
func orDash(check, fallback string) string {
	if check == "" {
		return fallback
	}
	return check
}
