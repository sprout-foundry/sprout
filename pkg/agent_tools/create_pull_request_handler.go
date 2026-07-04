package tools

import (
	"context"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// CreatePullRequestFunc is a function pointer set by pkg/agent at startup.
// It bridges the new ToolHandler interface with the legacy handleCreatePullRequest
// implementation that requires *Agent access.
//
// The function signature matches the legacy handler:
//
//	handleCreatePullRequest(ctx, args) → JSON string
//
// The agent sets this pointer during initialization, capturing the *Agent
// reference in a closure so the handler doesn't need direct access.
//
// Phase 4 of SP-109 will migrate the execute logic into this package,
// eliminating the need for this indirection.
var CreatePullRequestFunc func(ctx context.Context, args map[string]any) (string, error)

// createPullRequestHandler implements ToolHandler for the create_pull_request tool.
// It creates a pull request on GitHub after a feature branch has been pushed.
//
// This is a THIN WRAPPER that delegates Execute to the function pointer
// CreatePullRequestFunc. All metadata (Name, Definition, Validate, Aliases,
// Timeout, etc.) lives here.
type createPullRequestHandler struct{}

func (h *createPullRequestHandler) Name() string { return "create_pull_request" }

func (h *createPullRequestHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "create_pull_request",
		Description: "Create a pull request on GitHub after pushing a feature branch. " +
			"Gated as a git-write operation — requires git_write capability.",
		Required: []string{"title"},
		Parameters: []ParameterDef{
			{
				Name:        "title",
				Type:        "string",
				Required:    true,
				Description: "PR title (required)",
			},
			{
				Name:        "body",
				Type:        "string",
				Description: "PR body; synthesized from commits if omitted",
			},
			{
				Name:        "base",
				Type:        "string",
				Description: "Target branch (default: repo default branch)",
			},
			{
				Name:        "head",
				Type:        "string",
				Description: "Source branch (default: current HEAD)",
			},
			{
				Name:        "draft",
				Type:        "boolean",
				Description: "Create as draft PR (default false)",
			},
			{
				Name:        "repo_dir",
				Type:        "string",
				Description: "Repository root path (default: workspace root)",
			},
		},
	}
}

func (h *createPullRequestHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "title")
	return err
}

func (h *createPullRequestHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	toolName := h.Name()
	var hadError bool
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":   toolName,
			"params": args,
		})
		defer func() {
			env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
				"tool":  toolName,
				"error": hadError,
			})
		}()
	}

	if CreatePullRequestFunc == nil {
		hadError = true
		return ToolResult{
			Output:  "create_pull_request is not available: agent integration not initialized (CreatePullRequestFunc is nil)",
			IsError: true,
		}, nil
	}

	result, err := CreatePullRequestFunc(ctx, args)
	if err != nil {
		hadError = true
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}
	return ToolResult{Output: result}, nil
}

func (h *createPullRequestHandler) Aliases() []string         { return nil }
func (h *createPullRequestHandler) Timeout() time.Duration    { return 0 }
func (h *createPullRequestHandler) MaxResultSize() int        { return 0 }
func (h *createPullRequestHandler) SafeForParallel() bool     { return false }
func (h *createPullRequestHandler) Interactive() bool         { return false }
