package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sprout-foundry/sprout/pkg/automate"
)

// listAutomateWorkflowsHandler implements ToolHandler for the
// list_automate_workflows tool. It lists available workflow files
// from the project's automate/ directory.
//
// Unlike the Agent-dependent tools in this batch, this handler is
// fully standalone — it uses package-level functions from
// pkg/automate (automate.Dir, automate.Discover) that are accessible
// from pkg/agent_tools without creating an import cycle.
type listAutomateWorkflowsHandler struct{}

func (h *listAutomateWorkflowsHandler) Name() string { return "list_automate_workflows" }

func (h *listAutomateWorkflowsHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "list_automate_workflows",
		Description: "List available automated workflows from the project's automate/ directory. " +
			"Returns workflow filenames and descriptions. Use this before run_automate to show the user what's available.",
		Parameters: []ParameterDef{},
	}
}

func (h *listAutomateWorkflowsHandler) Validate(args map[string]any) error {
	// No required parameters — all args are optional.
	return nil
}

func (h *listAutomateWorkflowsHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	dir := automate.Dir()

	workflows, err := automate.Discover(dir)
	if err != nil {
		if automate.IsNotExists(err) {
			return ToolResult{
				Output: "No automate/ directory found. Activate the workflow-automation skill to create one.",
			}, nil
		}
		return ToolResult{
			Output:  fmt.Sprintf("failed to scan %s: %v", dir, err),
			IsError: true,
		}, nil
	}

	if len(workflows) == 0 {
		return ToolResult{
			Output: fmt.Sprintf("No workflows found in %s/. Activate the workflow-automation skill to create one.", dir),
		}, nil
	}

	type workflowInfo struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}

	items := make([]workflowInfo, 0, len(workflows))
	for _, wf := range workflows {
		items = append(items, workflowInfo{
			Name:        wf.Filename,
			Description: wf.Description,
		})
	}

	result := map[string]any{
		"directory": dir,
		"count":     len(items),
		"workflows": items,
	}

	resultJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("failed to marshal result: %v", err), IsError: true}, nil
	}

	return ToolResult{Output: string(resultJSON)}, nil
}

func (h *listAutomateWorkflowsHandler) Aliases() []string      { return nil }
func (h *listAutomateWorkflowsHandler) Timeout() time.Duration { return 0 }
func (h *listAutomateWorkflowsHandler) MaxResultSize() int     { return 0 }
func (h *listAutomateWorkflowsHandler) SafeForParallel() bool  { return false }
func (h *listAutomateWorkflowsHandler) Interactive() bool      { return false }
