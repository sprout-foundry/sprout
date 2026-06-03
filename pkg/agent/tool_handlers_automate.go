package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/sprout-foundry/sprout/pkg/automate"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// backgroundProcessManagerOnce ensures thread-safe lazy initialization of
// the BackgroundProcessManager to prevent data races.
var backgroundProcessManagerOnce sync.Once

// handleRunAutomate runs a workflow from the automate/ directory as a background process.
// Always requires user approval (enforced by the security classifier).
// Background execution is enforced — foreground mode is disabled for safety.
func handleRunAutomate(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	workflowName, _ := getStringArg(args, "workflow")
	if workflowName == "" {
		return "", fmt.Errorf("workflow parameter is required")
	}

	// Resolve the automate directory
	dir := automate.Dir()

	// Find the workflow file (includes path traversal protection)
	wfPath, err := automate.ResolvePath(dir, workflowName)
	if err != nil {
		return "", err
	}

	// Read description for user context
	desc, _ := automate.ExtractDescription(wfPath)

	// Resolve the sprout binary
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to resolve sprout binary: %w", err)
	}

	// Build the command — filename is validated by the shared automate
	// package (IsValidFilename), preventing shell injection.
	cmdStr := execPath + " agent --workflow-config " + wfPath + " --skip-prompt --no-web-ui"

	result := map[string]interface{}{
		"workflow":    filepath.Base(wfPath),
		"description": desc,
		"command":     cmdStr,
		"background":  true,
	}

	// Background: use the background process manager
	bpm := tools.BackgroundProcessManagerFromContext(ctx)
	if bpm == nil {
		bpm = a.getOrCreateBackgroundProcessManager()
	}

	sessionID, err := bpm.Start(ctx, cmdStr, "")
	if err != nil {
		return "", fmt.Errorf("failed to start workflow: %w", err)
	}

	result["status"] = "started"
	result["session_id"] = sessionID

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return string(resultJSON), nil
}

// handleListAutomateWorkflows lists available workflows from the automate/ directory.
func handleListAutomateWorkflows(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	dir := automate.Dir()

	workflows, err := automate.Discover(dir)
	if err != nil {
		if automate.IsNotExists(err) {
			return "No automate/ directory found. Activate the workflow-automation skill to create one.", nil
		}
		return "", fmt.Errorf("failed to scan %s: %w", dir, err)
	}

	if len(workflows) == 0 {
		return fmt.Sprintf("No workflows found in %s/. Activate the workflow-automation skill to create one.", dir), nil
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

	result := map[string]interface{}{
		"directory": dir,
		"count":     len(items),
		"workflows": items,
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return string(resultJSON), nil
}

// getOrCreateBackgroundProcessManager lazily initializes the background process manager.
// Thread-safe via sync.Once to prevent data races.
func (a *Agent) getOrCreateBackgroundProcessManager() *tools.BackgroundProcessManager {
	backgroundProcessManagerOnce.Do(func() {
		if a.backgroundProcessManager == nil {
			a.backgroundProcessManager = tools.NewBackgroundProcessManager()
		}
	})
	return a.backgroundProcessManager
}
