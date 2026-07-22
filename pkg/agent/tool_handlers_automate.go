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
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// backgroundProcessManagerOnce ensures thread-safe lazy initialization of
// the BackgroundProcessManager to prevent data races.
var backgroundProcessManagerOnce sync.Once

// automateSproutDirKey is a context key for the workspace-aware sprout
// directory. The WebUI API layer sets this before calling RunAutomateWorkflow
// so that writeAutomatePIDFile writes session files to the correct .sprout/
// directory instead of the process CWD.
type automateSproutDirKey struct{}

// SproutDirFromContext returns the workspace-aware sprout directory from ctx,
// or falls back to os.Getwd() (matching the legacy behavior for CLI-triggered
// workflows where CWD is the workspace root).
func SproutDirFromContext(ctx context.Context) string {
	if dir, ok := ctx.Value(automateSproutDirKey{}).(string); ok && dir != "" {
		return dir
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

// ContextWithSproutDir returns a context that carries the sprout directory.
func ContextWithSproutDir(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, automateSproutDirKey{}, dir)
}

// handleRunAutomate runs a workflow from the automate/ directory as a background process.
// Always requires user approval (enforced by the security classifier).
// Background execution is enforced — foreground mode is disabled for safety.
func handleRunAutomate(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	workflowName, _ := getStringArg(args, "workflow")
	if workflowName == "" {
		return "", agenterrors.NewValidation("workflow parameter is required", nil)
	}

	// Resolve the automate directory
	dir := automate.DirIn(a.GetWorkspaceRoot())

	// Find the workflow file (includes path traversal protection)
	wfPath, err := automate.ResolvePath(dir, workflowName)
	if err != nil {
		return "", err
	}

	// Build the summary once and read both the description and the
	// allowed_paths from it. Replaces the prior
	// automate.ExtractDescription(wfPath) call which re-parsed the
	// JSON just for `description`. Summarize also runs the
	// allowed_paths schema check (via workflow.AllowedPath.Validate
	// replicated in pkg/automate), so a malformed entry surfaces
	// here as a parse error rather than silently dropping the
	// whole field.
	summary, sumErr := automate.Summarize(wfPath)
	desc := ""
	if sumErr == nil && summary != nil {
		desc = summary.Description
	}

	// SP-128-1e: pre-seed the running agent's session allowlist
	// with every declared allowed_path, tagged with the declared
	// mode. This must happen BEFORE the in-process / BPM fork so
	// the launched workflow inherits the grants; the in-process
	// path inherits via SnapshotSessionAllowedFolders /
	// SnapshotSessionAllowedFolderModes (run inside the goroutine
	// — see subagent_creation.go), and the BPM subprocess path
	// inherits because the parent process is the one carrying the
	// session allowlist forward. We only act when Summarize
	// succeeded: a parse failure already aborts the launch below.
	if sumErr == nil && summary != nil {
		for _, ap := range summary.AllowedPaths {
			a.AddSessionAllowedFolder(ap.Path)
			a.SetSessionAllowedFolderMode(ap.Path, ap.Mode)
		}
	}

	// -----------------------------------------------------------------------
	// In-process path: detect loop workflows and run them as a goroutine
	// without spawning a subprocess. This eliminates the need for nohup
	// and avoids process-group/session detachment issues.
	// -----------------------------------------------------------------------
	if wfCfg, parseErr := parseWorkflowFile(wfPath); parseErr == nil && wfCfg.Loop != nil {
		sessionID := generateWorkflowSessionID()

		// Publish session_started event immediately.
		a.publishEvent(events.EventTypeAutomateSessionStarted, events.AutomateSessionStartedEvent(
			sessionID, filepath.Base(wfPath), "automate",
		))

		// Capture variables for the goroutine closure.
		wfName := filepath.Base(wfPath)
		wfDesc := desc

		// Launch the in-process workflow runner as a goroutine.
		go func() {
			// Use a background context so the goroutine survives the
			// parent agent's current query. Cancellation propagation
			// is handled internally by RunWorkflowLoopInProcess via
			// the interrupt context derived from the parent.
			result, runErr := RunWorkflowLoopInProcess(context.Background(), a, wfPath, a.eventBus)

			status := "success"
			if runErr != nil || (result != nil && result.Error != nil) {
				status = "error"
			}
			var totalCost float64
			if a.GetTotalCost() > 0 {
				totalCost = a.GetTotalCost()
			}

			a.publishEvent(events.EventTypeAutomateSessionEnded, events.AutomateSessionEndedEvent(
				sessionID, wfName, status, totalCost,
			))

			injectMsg := buildInProcessCompletionMessage(wfName, wfDesc, sessionID, status, result)
			a.QueueNotification(Notification{
				Content:   injectMsg,
				SessionID: sessionID,
				Kind:      NotifAutomate,
			})
		}()

		result := map[string]interface{}{
			"workflow":    filepath.Base(wfPath),
			"description": desc,
			"background":  true,
			"mode":        "in-process",
			"status":      "started",
			"session_id":  sessionID,
			"message":     fmt.Sprintf("Workflow started: session `%s` — view in [Automations panel](sprout://automations/session/%s)", sessionID, sessionID),
		}

		resultJSON, _ := json.MarshalIndent(result, "", "  ")
		return string(resultJSON), nil
	}

	// -----------------------------------------------------------------------
	// BPM subprocess path (fallback for steps-based workflows)
	// -----------------------------------------------------------------------

	// Resolve the sprout binary
	execPath, err := os.Executable()
	if err != nil {
		return "", agenterrors.NewTool("automate", "failed to resolve sprout binary", err)
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
		return "", agenterrors.NewTool("automate", "failed to start workflow", err)
	}

	// Write PID file for cross-process discoverability.
	// The error is non-fatal — the session is still tracked by BPM.
	sproutDir := filepath.Join(SproutDirFromContext(ctx), ".sprout")
	if err := writeAutomatePIDFile(sessionID, bpm, wfPath, sproutDir); err != nil {
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
			bgCtx := context.Background()
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
				injectMsg := buildAutomateCompletionMessage(wfName, wfDesc, sessionID, status, exitCode, proc.GetOutputPath())
				a.QueueNotification(Notification{
					Content:   injectMsg,
					SessionID: sessionID,
					Kind:      NotifAutomate,
				})
			case <-bgCtx.Done():
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

// WorkflowRequiresApprovalIn reports whether the named workflow needs
// user confirmation before launching, using the specified directory
// instead of the CWD-based automate.Dir().
//
// FAIL-SAFE: any error resolving or parsing the workflow returns true so a
// missing file or malformed JSON can't be used to slip past the prompt.
func WorkflowRequiresApprovalIn(dir, workflowName string) bool {
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

// WorkflowRequiresApproval reports whether the named workflow needs user
// confirmation before launching. This wraps WorkflowRequiresApprovalIn with
// the CWD-based automate.Dir() so the CLI tool path works correctly.
func WorkflowRequiresApproval(workflowName string) bool {
	return WorkflowRequiresApprovalIn(automate.Dir(), workflowName)
}

// handleListAutomateWorkflows lists available workflows from the automate/ directory.
func handleListAutomateWorkflows(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	dir := automate.DirIn(a.GetWorkspaceRoot())

	workflows, err := automate.Discover(dir)
	if err != nil {
		if automate.IsNotExists(err) {
			return "No automate/ directory found. Activate the workflow-automation skill to create one.", nil
		}
		return "", agenterrors.Wrapf(err, "failed to scan %s", dir)
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
// The agent is required (not optional) so the approval check resolves
// the automate/ directory against the agent's workspace root —
// `automate.Dir()` alone returns the daemon CWD in SPROUT_SERVICE mode,
// which would mismatch the directory the workflow itself will be loaded
// from. See SP-119 for the workspace-aware flow.
func workflowRequiresApproval(agent *Agent, workflowName string) bool {
	return WorkflowRequiresApprovalIn(automate.DirIn(agent.GetWorkspaceRoot()), workflowName)
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
func writeAutomatePIDFile(sessionID string, bpm *tools.BackgroundProcessManager, wfPath string, sproutDir string) error {
	// Get the process info from BPM using public accessors
	proc, exists := bpm.GetProcess(sessionID)
	if !exists {
		return fmt.Errorf("session %s not found in BPM", sessionID)
	}
	pid := proc.GetPID()
	outputPath := proc.GetOutputPath()

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
