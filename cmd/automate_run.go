//go:build !js

package cmd

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/automate"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// runAutomateRun runs a workflow by name or filename.
func runAutomateRun(name string) error {
	dir := getAutomateDir()

	// Resolve workflow path (includes path traversal protection from the shared package)
	wfPath, err := automate.ResolvePath(dir, name)
	if err != nil {
		// Check if this is a "not found" vs "directory doesn't exist" error
		if errors.Is(err, fs.ErrNotExist) {
			return handleNoAutomateDir(dir)
		}
		// For "no workflow matching" errors, show available workflows
		if strings.Contains(err.Error(), "no workflow matching") {
			console.GlyphWarning.Printf("%v", err)
			fmt.Println()
			return listAvailableWorkflows(dir)
		}
		if strings.Contains(err.Error(), "multiple workflows match") {
			console.GlyphWarning.Printf("%v", err)
			fmt.Println()
			return listAvailableWorkflows(dir)
		}
		if strings.Contains(err.Error(), "workflow path escapes") {
			console.GlyphWarning.Printf("Security: %v", err)
			return nil
		}
		return fmt.Errorf("failed to resolve workflow: %w", err)
	}

	return runWorkflowByPath(wfPath)
}

// handleNoAutomateDir handles the case where the automate/ directory doesn't exist.
func handleNoAutomateDir(dir string) error {
	console.GlyphWarning.Printf("No automate/ directory found.")
	fmt.Println()
	fmt.Println("Would you like to set up automated workflows?")
	fmt.Println("  This will activate the workflow-automation skill, which guides")
	fmt.Println("  you through creating workflows step by step.")
	fmt.Println()
	fmt.Print("Start setup? [y/N] ")

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		fmt.Println("Cancelled. You can set up workflows later with: activate_skill workflow-automation")
		return nil
	}
	response = strings.TrimSpace(strings.ToLower(response))

	if response != "y" && response != "yes" {
		fmt.Println("Cancelled. You can set up workflows later with: activate_skill workflow-automation")
		return nil
	}

	// Create the automate directory
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}
	console.GlyphSuccess.Printf("Created %s/", dir)
	fmt.Println()
	fmt.Println("To create workflows:")
	fmt.Println("  1. Start an agent session: sprout")
	fmt.Println("  2. Activate the skill: activate_skill workflow-automation")
	fmt.Println("  3. Follow the interactive setup")
	fmt.Println()
	fmt.Println("Once workflows are created, run them with: sprout automate")

	return nil
}

// runWorkflowByPath executes a workflow config file by invoking the agent command.
func runWorkflowByPath(path string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("workflow file not found: %s", path)
	}

	name := filepath.Base(path)

	// Parse the workflow once so we can reuse the summary for the overview
	// and for building subprocess args (max_iterations, subagent timeout).
	summary, err := automate.Summarize(path)
	if err != nil {
		// Failing to parse is unusual — fall back to basic display.
		desc, _ := automate.ExtractDescription(path)
		fmt.Println()
		console.GlyphAction.Printf("Running workflow: %s", name)
		if desc != "" {
			fmt.Printf("  %s\n", desc)
		}
		fmt.Println()
	} else {
		// Show an overview of the workflow before running so the user understands
		// what they are about to kick off (long-running, token-eating, background).
		if printErr := printWorkflowOverviewFromSummary(summary, name); printErr != nil {
			// Failing to render an overview is not fatal — fall back to the basic display.
			desc, _ := automate.ExtractDescription(path)
			fmt.Println()
			console.GlyphAction.Printf("Running workflow: %s", name)
			if desc != "" {
				fmt.Printf("  %s\n", desc)
			}
			fmt.Println()
		}
	}

	if !automateAssumeYes {
		if !confirmStartAutomation(name) {
			fmt.Println("Cancelled. The workflow was not started.")
			return nil
		}
	}

	// Invoke the agent command with the workflow config.
	// Use exec.Command to run as a subprocess so all initialization
	// (provider setup, config loading) happens correctly.
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to resolve sprout binary: %w", err)
	}

	args := buildAgentSubprocessArgs(path, summary)

	cmd := exec.Command(execPath, args...)
	// Close stdin and detach into a new process group so the workflow
	// survives the parent process exiting. Inherited std streams keep
	// a reference to the parent's terminal pipe — when that closes,
	// the child receives EOF/SIGPIPE and dies.
	cmd.Stdin = nil
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	setProcessGroup(cmd)

	// Apply subagent timeout override if the workflow specifies one.
	if summary != nil && summary.SubagentTimeoutSeconds != nil && *summary.SubagentTimeoutSeconds > 0 {
		cmd.Env = append(os.Environ(), fmt.Sprintf("SPROUT_TOOL_TIMEOUT=%d", *summary.SubagentTimeoutSeconds))
	}

	// Generate a session ID for PID file tracking
	randomHex := make([]byte, 8)
	if _, err := rand.Read(randomHex); err != nil {
		return fmt.Errorf("failed to generate session ID: %w", err)
	}
	sessionID := fmt.Sprintf("cli-automate-%s", hex.EncodeToString(randomHex))

	// Start the process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start workflow: %w", err)
	}

	// Resolve sprout directory relative to current working dir
	sproutDir, err := filepath.Abs(".sprout")
	if err != nil {
		return fmt.Errorf("resolve sprout directory: %w", err)
	}

	// Write PID file
	pidInfo := &automate.AutomateSessionInfo{
		Workflow:  filepath.Base(path),
		PID:       cmd.Process.Pid,
		StartedAt: time.Now(),
		Kind:      "automate",
	}
	if automateBudgetUSD > 0 {
		pidInfo.BudgetUSD = &automateBudgetUSD
	}
	if err := automate.WriteSessionFile(sproutDir, sessionID, pidInfo); err != nil {
		// Log warning but don't fail the workflow
		fmt.Fprintf(os.Stderr, "warn: failed to write PID file: %v\n", err)
	}

	// Remove PID file when process exits
	defer automate.RemoveSessionFile(sproutDir, sessionID)

	// Print session info
	fmt.Fprintf(os.Stderr, "\nWorkflow session: %s\n", sessionID)
	fmt.Fprintf(os.Stderr, "PID: %d\n", cmd.Process.Pid)
	fmt.Fprintf(os.Stderr, "PID file: %s/automate/%s.json\n", sproutDir, sessionID)
	fmt.Println()

	// Wait for the process to complete
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("workflow failed: %w", err)
	}
	return nil
}

// buildAgentSubprocessArgs constructs the argument list for the sprout agent
// subprocess that executes the workflow. Extracted for testability.
func buildAgentSubprocessArgs(path string, summary *automate.Summary) []string {
	args := []string{"agent", "--workflow-config", path, "--skip-prompt", "--no-web-ui"}

	// Plumb --max-iterations from the workflow JSON.
	// Non-zero values are passed explicitly; 0 (unlimited) is the default so
	// we don't pass the flag when it's 0 or nil.
	if summary != nil && summary.Initial != nil && summary.Initial.MaxIterations > 0 {
		args = append(args, "--max-iterations", strconv.Itoa(summary.Initial.MaxIterations))
	}

	if automateBudgetUSD > 0 {
		args = append(args, "--budget-usd", fmt.Sprintf("%g", automateBudgetUSD))
	}
	if strings.TrimSpace(automateBudgetWarn) != "" {
		args = append(args, "--budget-warn", automateBudgetWarn)
	}
	if automateHeartbeatSeconds > 0 {
		args = append(args, "--heartbeat", fmt.Sprintf("%d", automateHeartbeatSeconds))
	}

	return args
}

// printWorkflowOverviewFromSummary renders a human-readable summary of the
// workflow so the user can validate intent before kicking off a long-running
// automation run. Takes a pre-parsed summary to avoid re-reading the file.
func printWorkflowOverviewFromSummary(summary *automate.Summary, name string) error {
	fmt.Println()
	console.GlyphAction.Printf("Workflow: %s", name)
	if summary.Description != "" {
		fmt.Printf("  %s\n", summary.Description)
	}
	fmt.Println()

	fmt.Println("Overview:")
	if summary.Initial != nil {
		init := summary.Initial
		fmt.Printf("  • Initial run — persona=%s provider=%s model=%s\n",
			displayOrDefault(init.Persona, "default"),
			displayOrDefault(init.Provider, "config default"),
			displayOrDefault(init.Model, "config default"),
		)
		if init.MaxIterations > 0 {
			fmt.Printf("    max_iterations=%d\n", init.MaxIterations)
		} else {
			fmt.Printf("    max_iterations=0 (unlimited)\n")
		}
		if init.RiskProfile != "" {
			fmt.Printf("    risk_profile=%s\n", init.RiskProfile)
		}
		// Subagent overrides are the primary cost-control lever — surface
		// them so the user can see what providers/models will run for the
		// bulk of the workflow's work.
		if len(init.SubagentOverrides) > 0 {
			fmt.Println("    subagent_overrides:")
			for _, ov := range init.SubagentOverrides {
				fmt.Printf("      - %-18s provider=%s model=%s\n",
					ov.Persona,
					displayOrDefault(ov.Provider, "(inherit)"),
					displayOrDefault(ov.Model, "(inherit)"),
				)
			}
		}
	}

	if len(summary.Steps) > 0 {
		fmt.Printf("  • %d step(s):\n", len(summary.Steps))
		for i, step := range summary.Steps {
			stepName := step.Name
			if stepName == "" {
				stepName = fmt.Sprintf("step-%d", i+1)
			}
			fmt.Printf("    %2d. %-20s [%s] %s\n",
				i+1,
				stepName,
				step.Kind,
				stepDetail(step),
			)
		}
	}

	flags := []string{}
	if summary.ContinueOnError {
		flags = append(flags, "continue_on_error")
	}
	if summary.NoWebUI {
		flags = append(flags, "no_web_ui")
	}
	if len(flags) > 0 {
		fmt.Printf("  • Flags: %s\n", strings.Join(flags, ", "))
	}

	printPriceCard(summary)
	printBudgetLine(summary)
	printAllowedPaths(summary)

	// Surface auto-approval explicitly so a reader of the JSON sees the
	// security implication of requires_approval: false.
	if !summary.IsApprovalRequired() {
		fmt.Println()
		console.GlyphWarning.Printf("requires_approval: false — this workflow runs without a confirmation prompt when invoked by an agent.")
	}

	// Surface subagent timeout override if set.
	if summary.SubagentTimeoutSeconds != nil && *summary.SubagentTimeoutSeconds > 0 {
		fmt.Println()
		fmt.Printf("Subagent timeout: %d seconds\n", *summary.SubagentTimeoutSeconds)
	}

	fmt.Println()
	console.GlyphWarning.Printf("Heads up: workflows run autonomously in the background and consume tokens until they finish or are stopped.")
	for _, w := range summary.Warnings {
		fmt.Println()
		console.GlyphWarning.Printf("%s", w)
	}
	fmt.Println()
	return nil
}

// printPriceCard, printBudgetLine, printAllowedPaths, displayOrDefault, and
// stepDetail live in automate_run_overview_helpers.go — split out so this
// file stays under the AGENTS.md 500-line guideline.

// confirmStartAutomation asks the user to explicitly approve starting the run.
// This is intent validation, not security — long-running, token-eating
// background processes should not start by accident.
func confirmStartAutomation(name string) bool {
	fmt.Printf("Start workflow %q now? [y/N] ", name)

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}

