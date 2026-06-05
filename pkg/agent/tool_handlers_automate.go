package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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

	sessionID, err := bpm.StartWithKind(ctx, cmdStr, "", "automate")
	if err != nil {
		return "", fmt.Errorf("failed to start workflow: %w", err)
	}

	// Write PID file for cross-process discoverability.
	// The error is non-fatal — the session is still tracked by BPM.
	if err := writeAutomatePIDFile(sessionID, bpm, wfPath); err != nil {
		// Log warning but don't fail the workflow.
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

// workflowRequiresApproval returns true unless the named workflow's JSON
// declares requires_approval: false. Used by the security gate to decide
// whether to bypass the intent-confirmation prompt for run_automate calls.
//
// FAIL-SAFE: any error resolving or parsing the workflow returns true so a
// missing file or malformed JSON can't be used to slip past the prompt.
func workflowRequiresApproval(workflowName string) bool {
	dir := automate.Dir()
	path, err := automate.ResolvePath(dir, workflowName)
	if err != nil {
		return true
	}
	summary, err := automate.Summarize(path)
	if err != nil {
		return true
	}
	return summary.IsApprovalRequired()
}

// normalizeWorkflowKey produces a stable cache key for the in-session approval
// cache. ResolvePath accepts the workflow name with or without the .json
// extension (case-insensitive) and both forms resolve to the same file, so the
// cache key must collapse them — otherwise approving "foo" wouldn't satisfy a
// follow-up "foo.json" call from the model and we'd re-prompt the user.
func normalizeWorkflowKey(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	base := strings.ToLower(filepath.Base(trimmed))
	return strings.TrimSuffix(base, ".json")
}

// IsWorkflowApprovedInSession reports whether the user has already approved
// running this workflow during the current chat session. The cache is scoped
// per-agent and is reset whenever the agent is reinitialized.
func (a *Agent) IsWorkflowApprovedInSession(workflow string) bool {
	if a == nil {
		return false
	}
	key := normalizeWorkflowKey(workflow)
	if key == "" {
		return false
	}
	a.automateApprovedMu.Lock()
	defer a.automateApprovedMu.Unlock()
	if a.automateApprovedWorkflows == nil {
		return false
	}
	_, ok := a.automateApprovedWorkflows[key]
	return ok
}

// MarkWorkflowApprovedInSession records that the user has approved this
// workflow for the remainder of the chat session. Called by the security
// gate after a successful interactive approval, and by handleRunAutomate
// after a CLI-side confirmation path.
func (a *Agent) MarkWorkflowApprovedInSession(workflow string) {
	if a == nil {
		return
	}
	key := normalizeWorkflowKey(workflow)
	if key == "" {
		return
	}
	a.automateApprovedMu.Lock()
	defer a.automateApprovedMu.Unlock()
	if a.automateApprovedWorkflows == nil {
		a.automateApprovedWorkflows = make(map[string]struct{})
	}
	a.automateApprovedWorkflows[key] = struct{}{}
}

// writeAutomatePIDFile creates a PID file in .sprout/automate/ for cross-process
// discoverability of agent-launched automate workflows.
func writeAutomatePIDFile(sessionID string, bpm *tools.BackgroundProcessManager, wfPath string) error {
	// Get the process info from BPM using public accessors
	proc, exists := bpm.GetProcess(sessionID)
	if !exists {
		return fmt.Errorf("session %s not found in BPM", sessionID)
	}
	pid := proc.GetPID()
	outputPath := proc.GetOutputPath()

	// Resolve sprout directory
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	sproutDir := filepath.Join(wd, ".sprout")

	info := &automate.AutomateSessionInfo{
		Workflow:       filepath.Base(wfPath),
		PID:            pid,
		StartedAt:      time.Now(),
		OutputFilePath: outputPath,
		Kind:           "automate",
	}

	return automate.WriteSessionFile(sproutDir, sessionID, info)
}
