package tools

import (
	"context"
	"fmt"
	"time"
)

// reviewChangesHandler implements the review_changes tool: a code-owned
// review pipeline (context building, splitting across parallel reviewer
// subagents, merging findings) behind one call. Execute delegates to the
// agent through ToolFuncSet.ReviewChanges.
type reviewChangesHandler struct{}

func (h *reviewChangesHandler) Name() string { return "review_changes" }

func (h *reviewChangesHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "review_changes",
		Description: "Review a code change for real problems (correctness, security, concurrency, broken callers) using reviewer subagents. " +
			"Use this instead of spawning a `reviewer` with run_subagent: it pre-loads the diff, the code around each change, new files, " +
			"and repo conventions; splits large changes across parallel reviewers; and returns merged findings " +
			"(MUST_FIX / VERIFY / NOTE with file:line) plus a verdict (APPROVE, CHANGES_REQUIRED, or INCONCLUSIVE).\n\n" +
			"Run it after the build and tests pass. Fix MUST_FIX before committing; resolve VERIFY by confirming or fixing. " +
			"After substantial fixes, one re-review is enough.",
		Parameters: []ParameterDef{
			{
				Name:        "scope",
				Type:        "string",
				Description: "What to review: \"working_tree\" (default: staged + unstaged changes vs HEAD, plus new untracked files), \"staged\" (what the next commit contains), or \"range\" (a commit or branch range; set `range`).",
			},
			{
				Name:        "range",
				Type:        "string",
				Description: "Git revision range for scope \"range\", e.g. \"main...HEAD\" or \"abc123^!\" (one commit).",
			},
			{
				Name:        "background",
				Type:        "boolean",
				Description: "Run in the background (default true where supported): returns a task_id immediately and you are notified with the findings when the review finishes. Set false to wait for the result.",
			},
			{
				Name:        "focus",
				Type:        "string",
				Description: "Optional: the change's intent and the risks reviewers should focus on, in a few lines. Do not paste code or diffs.",
			},
		},
	}
}

func (h *reviewChangesHandler) Validate(args map[string]any) error {
	if scope, ok := args["scope"].(string); ok && scope == "range" {
		if r, _ := args["range"].(string); r == "" {
			return fmt.Errorf("parameter 'range' is required when scope is \"range\"")
		}
	}
	return nil
}

func (h *reviewChangesHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	fn := env.ResolveToolFuncs().ReviewChanges
	if fn == nil {
		return ToolResult{Output: "review_changes is not available: agent integration not initialized", IsError: true}, nil
	}
	result, err := fn(ctx, args)
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}
	return ToolResult{Output: result}, nil
}

func (h *reviewChangesHandler) Aliases() []string      { return nil }
func (h *reviewChangesHandler) Timeout() time.Duration { return subagentToolTimeout }
func (h *reviewChangesHandler) MaxResultSize() int     { return 0 }
func (h *reviewChangesHandler) SafeForParallel() bool  { return false }
func (h *reviewChangesHandler) Interactive() bool      { return false }
