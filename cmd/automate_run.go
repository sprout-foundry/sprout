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
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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

	// Generate session ID up front so the log path (detach mode) and the
	// PID file both exist before the child starts.
	randomHex := make([]byte, 8)
	if _, err := rand.Read(randomHex); err != nil {
		return fmt.Errorf("failed to generate session ID: %w", err)
	}
	sessionID := fmt.Sprintf("cli-automate-%s", hex.EncodeToString(randomHex))

	// Resolve the session root before start so the detach log path exists.
	sproutDir, err := automateSessionRoot()
	if err != nil {
		return err
	}

	args := buildAgentSubprocessArgs(path, summary)

	if floorErr := automate.CheckMemoryFloor(); floorErr != nil {
		return fmt.Errorf("not starting workflow: %w", floorErr)
	}

	cmd := buildAgentCommandFn(execPath, args)
	cmd.Stdin = nil
	setProcessGroup(cmd)

	// In detach mode, redirect the child's output to a session log file
	// instead of inheriting this process's stdio. Inherited stdio is a
	// pipe (or TTY) owned by the launcher — when the launcher dies, the
	// read end closes and the child receives SIGPIPE on its next write.
	// A file has no such lifetime coupling: the workflow survives the
	// launcher, the terminal, and even this CLI exiting.
	var detachLogPath string
	if automateDetach {
		var f *os.File
		f, detachLogPath, err = openDetachLogFile(sproutDir, sessionID)
		if err != nil {
			return err
		}
		cmd.Stdout = f
		cmd.Stderr = f
		defer func() {
			// The child holds its own dup'd descriptors once started;
			// the launcher's copy closes on return either way.
			f.Close()
		}()
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	// Apply subagent timeout override if the workflow specifies one.
	if summary != nil && summary.SubagentTimeoutSeconds != nil && *summary.SubagentTimeoutSeconds > 0 {
		cmd.Env = append(os.Environ(), fmt.Sprintf("SPROUT_TOOL_TIMEOUT=%d", *summary.SubagentTimeoutSeconds))
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		// Don't leave a 0-byte orphan log with no session record.
		if detachLogPath != "" {
			_ = os.Remove(detachLogPath)
		}
		return fmt.Errorf("start workflow: %w", err)
	}

	// Write PID file
	pidInfo := &automate.AutomateSessionInfo{
		Workflow:  filepath.Base(path),
		PID:       cmd.Process.Pid,
		StartedAt: time.Now(),
		Kind:      "automate",
	}
	if detachLogPath != "" {
		pidInfo.OutputFilePath = detachLogPath
	}
	if automateBudgetUSD > 0 {
		pidInfo.BudgetUSD = &automateBudgetUSD
	}
	if err := automate.WriteSessionFile(sproutDir, sessionID, pidInfo); err != nil {
		// Log warning but don't fail the workflow
		fmt.Fprintf(os.Stderr, "warn: failed to write PID file: %v\n", err)
	}

	// Print session info
	fmt.Fprintf(os.Stderr, "\nWorkflow session: %s\n", sessionID)
	fmt.Fprintf(os.Stderr, "PID: %d\n", cmd.Process.Pid)
	fmt.Fprintf(os.Stderr, "PID file: %s/automate/%s.json\n", sproutDir, sessionID)
	if detachLogPath != "" {
		fmt.Fprintf(os.Stderr, "Log file: %s\n", detachLogPath)
		fmt.Fprintln(os.Stderr, "\nDetached: workflow runs in the background; use 'sprout automate status' and 'sprout automate logs' to monitor.")
	}
	fmt.Println()

	if automateDetach {
		// No waiter, no signal forwarding, no finalizer. The child owns
		// its log file; session end-state falls back to PID-liveness per
		// the AutomateSessionInfo schema (pkg/automate/pid_file.go).
		return nil
	}

	// Wait for the process to complete with signal forwarding.
	// The child is in its own session (setProcessGroup), so terminal
	// Ctrl+C never reaches it — we must forward SIGINT/SIGTERM.
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	waitDone := make(chan error, 1)
	// The real wait error is captured exactly once here; a second cmd.Wait()
	// call returns "Wait was already called" (a plain error), so the deferred
	// finalizer must read this variable, not re-Wait. waitDone is buffered so
	// the goroutine always completes even on return paths that don't drain it.
	var finalWaitErr error
	waitCompleted := make(chan struct{})
	go func() {
		defer close(waitCompleted)
		finalWaitErr = cmd.Wait()
		waitDone <- finalWaitErr
	}()
	defer func() {
		<-waitCompleted
		if finErr := automate.FinalizeSessionFile(sproutDir, sessionID, exitCodeFromWaitErr(finalWaitErr)); finErr != nil {
			fmt.Fprintf(os.Stderr, "warn: %v\n", finErr)
		}
	}()

	for {
		select {
		case waitErr := <-waitDone:
			// Child exited on its own.
			if waitErr != nil {
				return fmt.Errorf("workflow failed: %w", waitErr)
			}
			return nil
		case sig := <-sigCh:
			// First signal: forward to the child and keep waiting.
			console.GlyphWarning.Printf("Received %v, forwarding to workflow (PID %d)...", sig, cmd.Process.Pid)
			if err := cmd.Process.Signal(sig); err != nil {
				if err == syscall.ESRCH {
					// Child already exited — report its real result.
					waitErr := <-waitDone
					if waitErr != nil {
						return fmt.Errorf("workflow failed: %w", waitErr)
					}
					return nil
				}
				console.GlyphWarning.Printf("Signal failed: %v — force quitting workflow (PID %d)...", err, cmd.Process.Pid)
				cmd.Process.Kill()
				<-waitDone
				return fmt.Errorf("workflow force-quit")
			}
			// Wait for a second signal or the child to exit.
			select {
			case waitErr := <-waitDone:
				if waitErr != nil {
					return fmt.Errorf("workflow failed: %w", waitErr)
				}
				return nil
			case <-sigCh:
				// Second signal: force quit.
				console.GlyphStopped.Printf("Force quitting workflow (PID %d)...", cmd.Process.Pid)
				cmd.Process.Kill()
				<-waitDone
				return fmt.Errorf("workflow force-quit")
			}
		}
	}
}

// exitCodeFromWaitErr maps a cmd.Wait error to a process exit code.
// nil → 0; *exec.ExitError carries the real code (signal deaths report -1);
// any other error (wait-system failure) is conservatively -1 so the session
// record still shows a non-success outcome.
func exitCodeFromWaitErr(waitErr error) int {
	if waitErr == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
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

// openDetachLogFile creates the session log directory under sproutDir and
// opens the per-session log file the detached workflow child will write to.
// Returned path is recorded in the session PID file (OutputFilePath) so
// `sprout automate logs` can find it.
func openDetachLogFile(sproutDir, sessionID string) (*os.File, string, error) {
	// 0o700 dir / 0o600 file match the session-dir convention
	// (WriteSessionFile/GetAutomateSessionDir): workflow output can
	// contain source and secrets, so no group/world access.
	logDir := filepath.Join(sproutDir, "automate", "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, "", fmt.Errorf("create automate log directory: %w", err)
	}
	logPath := filepath.Join(logDir, sessionID+".log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, "", fmt.Errorf("open detach log file: %w", err)
	}
	return f, logPath, nil
}

// buildAgentCommandFn is a test seam over child-process construction.
// Production behavior execs the sprout agent subprocess; tests swap it to
// launch a stand-in child so the launch machinery (stdio wiring, PID file,
// immediate-return contract) can be exercised without a real agent.
var buildAgentCommandFn = func(execPath string, args []string) *exec.Cmd {
	return exec.Command(execPath, args...)
}
