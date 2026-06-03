package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// handleRunAutomate runs a workflow from the automate/ directory as a background process.
// Always requires user approval (enforced by the security classifier).
func handleRunAutomate(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	workflowName, _ := getStringArg(args, "workflow")
	if workflowName == "" {
		return "", fmt.Errorf("workflow parameter is required")
	}

	useBackground := true
	if bgVal, ok := args["background"]; ok {
		if bgBool, ok := bgVal.(bool); ok {
			useBackground = bgBool
		}
	}

	// Resolve the automate directory
	dir := resolveAutomateDir()

	// Find the workflow file
	wfPath, err := resolveWorkflowPath(dir, workflowName)
	if err != nil {
		return "", err
	}

	// Read description for user context
	desc, _ := extractAutomateWorkflowDescription(wfPath)

	// Resolve the sprout binary
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to resolve sprout binary: %w", err)
	}

	// Build the command
	cmdArgs := []string{execPath, "agent", "--workflow-config", wfPath, "--skip-prompt", "--no-web-ui"}

	result := map[string]interface{}{
		"workflow":     filepath.Base(wfPath),
		"description":  desc,
		"command":      strings.Join(cmdArgs, " "),
		"background":   useBackground,
	}

	if !useBackground {
		// Foreground: run synchronously (blocks until complete)
		result["status"] = "running_foreground"
		resultJSON, _ := json.MarshalIndent(result, "", "  ")

		// Execute via shell_command infrastructure
		shellArgs := map[string]interface{}{
			"command": strings.Join(cmdArgs, " "),
		}
		out, err := handleShellCommand(ctx, a, shellArgs)
		if err != nil {
			return "", fmt.Errorf("workflow failed: %w", err)
		}
		return string(resultJSON) + "\n" + out, nil
	}

	// Background: use the background process manager
	bpm := tools.BackgroundProcessManagerFromContext(ctx)
	if bpm == nil {
		bpm = a.getOrCreateBackgroundProcessManager()
	}

	cmdStr := strings.Join(cmdArgs[1:], " ") // skip binary name, shell_command adds it
	shellArgs := map[string]interface{}{
		"command":    cmdStr,
		"background": true,
	}

	// Execute via shell_command with background=true
	out, err := handleShellCommand(ctx, a, shellArgs)
	if err != nil {
		return "", fmt.Errorf("failed to start workflow: %w", err)
	}

	result["status"] = "started"
	result["output"] = out

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return string(resultJSON), nil
}

// handleListAutomateWorkflows lists available workflows from the automate/ directory.
func handleListAutomateWorkflows(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	dir := resolveAutomateDir()

	workflows, err := discoverAutomateWorkflows(dir)
	if err != nil {
		if os.IsNotExist(err) {
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

// automateWorkflowEntry is a parsed workflow file with metadata.
type automateWorkflowEntry struct {
	Filename    string
	FilePath    string
	Description string
}

// resolveAutomateDir returns the automate directory path.
func resolveAutomateDir() string {
	cwd, _ := os.Getwd()
	return filepath.Join(cwd, "automate")
}

// discoverAutomateWorkflows scans the automate directory for workflow JSON files.
func discoverAutomateWorkflows(dir string) ([]automateWorkflowEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var workflows []automateWorkflowEntry
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.ToLower(filepath.Ext(name)) != ".json" {
			continue
		}

		fullPath := filepath.Join(dir, name)
		desc, err := extractAutomateWorkflowDescription(fullPath)
		if err != nil {
			continue // Not a valid workflow JSON
		}

		workflows = append(workflows, automateWorkflowEntry{
			Filename:    name,
			FilePath:    fullPath,
			Description: desc,
		})
	}

	return workflows, nil
}

// extractAutomateWorkflowDescription reads a JSON file and returns its description field.
func extractAutomateWorkflowDescription(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", err
	}

	// Must have "initial" or "steps" to be a workflow
	if _, ok := raw["initial"]; !ok {
		if _, ok := raw["steps"]; !ok {
			return "", fmt.Errorf("not a workflow config")
		}
	}

	var desc string
	if descRaw, ok := raw["description"]; ok {
		_ = json.Unmarshal(descRaw, &desc)
	}

	return desc, nil
}

// resolveWorkflowPath finds a workflow file by name, with or without .json extension.
func resolveWorkflowPath(dir string, name string) (string, error) {
	// Try exact filename match first
	target := name
	if !strings.HasSuffix(strings.ToLower(name), ".json") {
		target = name + ".json"
	}

	candidate := filepath.Join(dir, target)
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}

	// Try substring match
	workflows, err := discoverAutomateWorkflows(dir)
	if err != nil {
		return "", fmt.Errorf("no automate/ directory found")
	}

	var matches []automateWorkflowEntry
	for _, wf := range workflows {
		if strings.Contains(strings.ToLower(wf.Filename), strings.ToLower(name)) {
			matches = append(matches, wf)
		}
	}

	if len(matches) == 1 {
		return matches[0].FilePath, nil
	}

	if len(matches) > 1 {
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.Filename
		}
		return "", fmt.Errorf("multiple workflows match %q: %v — please specify the full filename", name, names)
	}

	return "", fmt.Errorf("no workflow matching %q found in %s/", name, dir)
}

// getOrCreateBackgroundProcessManager lazily initializes the background process manager.
func (a *Agent) getOrCreateBackgroundProcessManager() *tools.BackgroundProcessManager {
	if a.backgroundProcessManager != nil {
		return a.backgroundProcessManager
	}
	a.backgroundProcessManager = tools.NewBackgroundProcessManager()
	return a.backgroundProcessManager
}
