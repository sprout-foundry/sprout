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

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/automate"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// backgroundProcessManagerOnce ensures thread-safe lazy initialization of
// the BackgroundProcessManager to prevent data races.
var backgroundProcessManagerOnce sync.Once

// completionMessageTailLimit is the maximum number of output bytes included
// in an automate completion injection message when the workflow fails.
const completionMessageTailLimit = 2048

// buildAutomateCompletionMessage builds the self-contained completion injection
// message for an automate workflow that has finished. It is extracted from the
// proc.Done() goroutine in handleRunAutomate so it can be unit-tested without
// spinning up real background processes.
func buildAutomateCompletionMessage(wfName, wfDesc, sessionID, status string, exitCode int, outputPath string) string {
	// On failure, include the output tail for diagnostics.
	if exitCode != 0 {
		tail := readOutputTail(outputPath, completionMessageTailLimit)
		if tail != "" {
			return fmt.Sprintf(
				"[automate] Background workflow completed:\n"+
					"  Workflow: %s\n"+
					"  Description: %s\n"+
					"  Session: %s\n"+
					"  Status: %s (exit code %d)\n"+
					"  Output (last 2KB):\n%s",
				wfName, wfDesc, sessionID, status, exitCode, tail,
			)
		}
	}
	return fmt.Sprintf(
		"[automate] Background workflow completed:\n"+
			"  Workflow: %s\n"+
			"  Description: %s\n"+
			"  Session: %s\n"+
			"  Status: %s (exit code %d)",
		wfName, wfDesc, sessionID, status, exitCode,
	)
}

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

	sessionID, err := bpm.StartWithOptions(ctx, cmdStr, "", "automate", &tools.StartOptions{EventBus: a.eventBus})
	if err != nil {
		return "", fmt.Errorf("failed to start workflow: %w", err)
	}

	// Write PID file for cross-process discoverability.
	// The error is non-fatal — the session is still tracked by BPM.
	if err := writeAutomatePIDFile(sessionID, bpm, wfPath); err != nil {
		// Log warning but don't fail the workflow.
	}

	// SP-065-2b: Publish session_started event
	a.publishEvent(events.EventTypeAutomateSessionStarted, events.AutomateSessionStartedEvent(
		sessionID, filepath.Base(wfPath), "automate",
	))

	// SP-065-2d: Watch for process exit and publish session_ended
	if proc, exists := bpm.GetProcess(sessionID); exists {
		// Capture variables for goroutine closure
		wfName := filepath.Base(wfPath)
		wfDesc := desc

		go func() {
			select {
			case <-proc.Done():
				exitCode := proc.GetExitCode()
				status := "success"
				if exitCode != 0 {
					status = "error"
				}
				a.publishEvent(events.EventTypeAutomateSessionEnded, events.AutomateSessionEndedEvent(
					sessionID, wfName, status, 0,
				))

				// SP-067: Inject self-contained completion message back to the model
				// so it can act autonomously (e.g., retry on failure) without polling.
				injectMsg := buildAutomateCompletionMessage(wfName, wfDesc, sessionID, status, exitCode, proc.GetOutputPath())
				_ = a.InjectInputContext(injectMsg)
			case <-ctx.Done():
				// Agent shutting down; skip injection.
			}
		}()
	}

	result["status"] = "started"
	result["session_id"] = sessionID
	// SP-065-5a: Include a message with session ID and link to Automations panel
	result["message"] = fmt.Sprintf("Workflow started: session `%s` — view in [Automations panel](sprout://automations/session/%s)", sessionID, sessionID)

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return string(resultJSON), nil
}

// RunAutomateWorkflow executes a named workflow and returns the JSON result.
// This is the public entry point for the WebUI automate API.
func (a *Agent) RunAutomateWorkflow(ctx context.Context, workflow string) (string, error) {
	args := map[string]interface{}{"workflow": workflow}
	return handleRunAutomate(ctx, a, args)
}

// WorkflowRequiresApproval reports whether the named workflow needs user
// confirmation before launching. This wraps the unexported workflowRequiresApproval
// so the WebUI layer can enforce the same policy as the CLI tool path.
func WorkflowRequiresApproval(workflowName string) bool {
	return workflowRequiresApproval(workflowName)
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

// readOutputTail reads the last maxBytes bytes of the file at path.
// Returns empty string on any error (file missing, read error, etc).
func readOutputTail(path string, maxBytes int) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	if info.IsDir() {
		return ""
	}

	fileSize := info.Size()
	if fileSize <= 0 {
		return ""
	}

	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	var offset int64
	if fileSize > int64(maxBytes) {
		offset = fileSize - int64(maxBytes)
	}

	buf := make([]byte, fileSize-offset)
	if _, err := f.ReadAt(buf, offset); err != nil {
		return ""
	}

	// Strip control characters except common whitespace (newline, tab, carriage return).
	var b strings.Builder
	b.Grow(len(buf))
	for _, r := range string(buf) {
		if r == '\n' || r == '\t' || r == '\r' || (r >= 32 && r < 127) || r >= 128 {
			b.WriteRune(r)
		}
	}
	return b.String()
}
