package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/planstore"
)

// writePlanHandler implements ToolHandler for write_plan (SP-148 §148b): the
// agent-facing write path for the project's structured plan document.
//
// The model hands over the full plan document (the plancontract schema) as a
// single object argument. The handler validates it (plancontract.ValidateJSON
// — decode plus the full invariant check) and then persists it through the
// plan store (planstore.Store.Save), which re-validates, bumps the revision,
// stamps `updated`, and regenerates the derived .sprout/plan.md view. An
// invalid document is rejected with every problem listed (the model can fix
// and retry) and nothing is written: the store validates before touching
// disk, so a rejected write never leaves a partial file behind.
//
// Pure Go: plancontract and planstore have no browser or vision dependencies,
// so the handler ships on every platform — it is registered in the shared
// AllTools list (no build tag, no WASM stub). Writes are confined to the
// project's .sprout/ directory; the handler never writes elsewhere.
type writePlanHandler struct{}

func (h *writePlanHandler) Name() string { return "write_plan" }

func (h *writePlanHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "write_plan",
		Description: "Write the project's structured plan (.sprout/plan.json plus the rendered .sprout/plan.md). " +
			"Use it when the planning phase produced a complete plan document (SP-148), or when scope " +
			"changed during execution and the plan must be updated instead of silently diverging. " +
			"The document is validated on every write: an invalid plan is rejected with the full " +
			"problem list and nothing is written, so fix the problems and call again. " +
			"Every successful write bumps the plan revision and regenerates the markdown view.",
		Required: []string{"plan"},
		Parameters: []ParameterDef{
			{
				Name:     "plan",
				Type:     "object",
				Required: true,
				Description: "The full structured plan document: {version, revision, created, updated, goal, " +
					"scope[{id,title,description}], steps[{scope,description}], design?, starter?, " +
					"acceptance[{id,scope,check,kind,steps?}], out_of_scope[{item,reason}]}. " +
					"version is 1; created/updated are RFC3339 timestamps (both required); " +
					"for a new plan revision is 1 (the store bumps it on every write), and when editing " +
					"an existing plan keep the revision you last read. Every scope item needs at least " +
					"one acceptance item referencing it; acceptance ids and scope ids are unique; every " +
					"acceptance/steps scope reference must name an existing scope id. " +
					"kind is one of build|test|page|interaction|manual; only kind \"interaction\" " +
					"carries a non-empty steps[] (browse step format: each step has a required action " +
					"plus selector, value, key, millis, script, expect, screenshot_path as applicable). " +
					"out_of_scope items need both item and reason.",
			},
		},
	}
}

func (h *writePlanHandler) Validate(args map[string]any) error {
	if args == nil {
		return agenterrors.NewValidation("write_plan: 'plan' is required", nil)
	}
	plan, ok := lookupKey(args, "plan")
	if !ok || plan == nil {
		return agenterrors.NewValidation("write_plan: 'plan' is required", nil)
	}
	if _, ok := plan.(map[string]any); !ok {
		return agenterrors.NewValidation("write_plan: 'plan' must be an object (the full plan document)", nil)
	}
	return nil
}

func (h *writePlanHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	planRaw, ok := lookupKey(args, "plan")
	if !ok {
		return ToolResult{Output: "write_plan: 'plan' is required", IsError: true}, nil
	}
	planMap, ok := planRaw.(map[string]any)
	if !ok {
		return ToolResult{
			Output:  fmt.Sprintf("write_plan: 'plan' must be an object (the full plan document), got %T", planRaw),
			IsError: true,
		}, nil
	}

	// The model's document, as bytes: the canonical form for decode +
	// validate. Re-marshalling the decoded map is deterministic enough for
	// this purpose (the schema has no maps that could randomize order).
	planBytes, err := json.Marshal(planMap)
	if err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("write_plan: could not encode the plan document: %v", err),
			IsError: true,
		}, nil
	}

	// Decode and validate before touching anything: on failure the model
	// gets the aggregated problem list and can fix and retry.
	plan, err := plancontract.ValidateJSON(planBytes)
	if err != nil {
		return ToolResult{Output: planRejectionMessage(err), IsError: true}, nil
	}

	root := env.WorkspaceRoot
	if root == "" {
		return ToolResult{
			Output:  "write_plan: no workspace root available — cannot write .sprout/plan.json",
			IsError: true,
		}, nil
	}

	// Persist through the plan store: validates again, bumps revision,
	// stamps updated, and regenerates .sprout/plan.md from the JSON.
	stored, err := planstore.New().Save(root, plan)
	if err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("write_plan: failed to save the plan, nothing was written: %v", err),
			IsError: true,
		}, nil
	}

	return ToolResult{
		Output: fmt.Sprintf(
			"Structured plan saved: %s (revision %d, schema v%d)\nRendered view: %s",
			planstore.PlanJSONPath(root), stored.Revision, stored.Version, planstore.PlanMarkdownPath(root),
		),
		StructuredOut: map[string]any{
			"plan_json":     planstore.PlanJSONPath(root),
			"plan_markdown": planstore.PlanMarkdownPath(root),
			"revision":      stored.Revision,
			"version":       stored.Version,
		},
	}, nil
}

// planRejectionMessage renders a plancontract failure (a JSON decode error or
// the aggregated *ValidationError) as a tool result the model can act on:
// every problem is listed and the retry instruction is explicit.
func planRejectionMessage(err error) string {
	var vErr *plancontract.ValidationError
	if errors.As(err, &vErr) {
		problems := make([]string, len(vErr.Problems))
		for i, p := range vErr.Problems {
			problems[i] = "  - " + p
		}
		return fmt.Sprintf(
			"write_plan: the plan was rejected and nothing was written (%d problem(s)):\n%s\n"+
				"Fix the plan document and call write_plan again.",
			len(vErr.Problems), strings.Join(problems, "\n"),
		)
	}
	return fmt.Sprintf(
		"write_plan: the plan document could not be read and nothing was written: %v\n"+
			"Fix the document and call write_plan again.",
		err,
	)
}

func (h *writePlanHandler) Aliases() []string      { return nil }
func (h *writePlanHandler) Timeout() time.Duration { return 0 }
func (h *writePlanHandler) MaxResultSize() int     { return 0 }
func (h *writePlanHandler) SafeForParallel() bool  { return false }
func (h *writePlanHandler) Interactive() bool      { return false }
