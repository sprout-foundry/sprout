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

	// Show an overview of the workflow before running so the user understands
	// what they are about to kick off (long-running, token-eating, background).
	if err := printWorkflowOverview(path, name); err != nil {
		// Failing to render an overview is not fatal — fall back to the basic display.
		desc, _ := automate.ExtractDescription(path)
		fmt.Println()
		console.GlyphAction.Printf("Running workflow: %s", name)
		if desc != "" {
			fmt.Printf("  %s\n", desc)
		}
		fmt.Println()
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

	args := []string{"agent", "--workflow-config", path, "--skip-prompt", "--no-web-ui"}
	if automateBudgetUSD > 0 {
		args = append(args, "--budget-usd", fmt.Sprintf("%g", automateBudgetUSD))
	}
	if strings.TrimSpace(automateBudgetWarn) != "" {
		args = append(args, "--budget-warn", automateBudgetWarn)
	}
	if automateHeartbeatSeconds > 0 {
		args = append(args, "--heartbeat", fmt.Sprintf("%d", automateHeartbeatSeconds))
	}

	cmd := exec.Command(execPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

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

// printWorkflowOverview renders a human-readable summary of the workflow so
// the user can validate intent before kicking off a long-running automation run.
func printWorkflowOverview(path, name string) error {
	summary, err := automate.Summarize(path)
	if err != nil {
		return err
	}

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

	// Surface auto-approval explicitly so a reader of the JSON sees the
	// security implication of requires_approval: false.
	if !summary.IsApprovalRequired() {
		fmt.Println()
		console.GlyphWarning.Printf("requires_approval: false — this workflow runs without a confirmation prompt when invoked by an agent.")
	}

	fmt.Println()
	console.GlyphWarning.Printf("Heads up: workflows run autonomously in the background and consume tokens until they finish or are stopped.")
	fmt.Println()
	return nil
}

// printPriceCard renders the provider/model rates for the initial agent and
// each subagent persona that will run. It walks pricing for every model
// named in the workflow so the user sees the actual rate card before
// approving the run. Unknown rates are shown explicitly as "unknown" — we
// never fabricate a price. Followed by a footer when any row is incomplete.
func printPriceCard(summary *automate.Summary) {
	if summary == nil || summary.Initial == nil {
		return
	}

	type row struct {
		Role        string
		Persona     string
		Provider    string
		Model       string
		InputUsd    float64
		OutputUsd   float64
		HasPricing  bool
		IsInherited bool
	}

	rows := []row{}
	primaryProvider := summary.Initial.Provider
	primaryModel := summary.Initial.Model
	if primaryProvider != "" && primaryModel != "" {
		p := lookupModelPricing(primaryProvider, primaryModel)
		rows = append(rows, row{
			Role:       "Initial",
			Persona:    displayOrDefault(summary.Initial.Persona, "default"),
			Provider:   primaryProvider,
			Model:      primaryModel,
			InputUsd:   p.InputUsdPerM,
			OutputUsd:  p.OutputUsdPerM,
			HasPricing: p.HasPricing,
		})
	}

	for _, ov := range summary.Initial.SubagentOverrides {
		provider := ov.Provider
		model := ov.Model
		inherited := false
		if provider == "" {
			provider = primaryProvider
			inherited = true
		}
		if model == "" {
			model = primaryModel
			inherited = true
		}
		if provider == "" || model == "" {
			rows = append(rows, row{
				Role:        "Subagent",
				Persona:     ov.Persona,
				Provider:    displayOrDefault(provider, "(inherit)"),
				Model:       displayOrDefault(model, "(inherit)"),
				IsInherited: inherited,
			})
			continue
		}
		p := lookupModelPricing(provider, model)
		rows = append(rows, row{
			Role:        "Subagent",
			Persona:     ov.Persona,
			Provider:    provider,
			Model:       model,
			InputUsd:    p.InputUsdPerM,
			OutputUsd:   p.OutputUsdPerM,
			HasPricing:  p.HasPricing,
			IsInherited: inherited,
		})
	}

	if len(rows) == 0 {
		return
	}

	fmt.Println()
	fmt.Println("Models that will run:")
	missing := 0
	for _, r := range rows {
		priceCol := "      pricing: unknown"
		if r.HasPricing {
			priceCol = fmt.Sprintf("$%6.2f / $%6.2f per Mtok", r.InputUsd, r.OutputUsd)
		} else {
			missing++
		}
		inheritedTag := ""
		if r.IsInherited {
			inheritedTag = " (inherited)"
		}
		fmt.Printf("  %-9s %-20s %-13s %-30s %s%s\n",
			r.Role, r.Persona, r.Provider, r.Model, priceCol, inheritedTag,
		)
	}
	if missing > 0 {
		console.GlyphWarning.Printf("Pricing data incomplete for %d of %d models — actual cost may exceed what's shown.",
			missing, len(rows))
	}
}

// printBudgetLine renders the configured USD budget if set, including warn
// thresholds expressed in dollars (not just fractions) so the user sees the
// concrete numbers they'll be billed against.
func printBudgetLine(summary *automate.Summary) {
	if summary == nil || summary.Budget == nil || summary.Budget.USD <= 0 {
		return
	}
	parts := []string{fmt.Sprintf("$%.2f USD cap", summary.Budget.USD)}
	if len(summary.Budget.WarnAt) > 0 {
		dollars := make([]string, 0, len(summary.Budget.WarnAt))
		for _, t := range summary.Budget.WarnAt {
			dollars = append(dollars, fmt.Sprintf("$%.2f", t*summary.Budget.USD))
		}
		parts = append(parts, "warn at "+strings.Join(dollars, ", "))
	}
	fmt.Println()
	fmt.Printf("Budget: %s\n", strings.Join(parts, ", "))
}

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

func displayOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func stepDetail(step automate.StepSummary) string {
	switch step.Kind {
	case "shell":
		if step.CommandPreview != "" {
			return step.CommandPreview
		}
		return "(shell command)"
	default:
		details := []string{}
		if step.Persona != "" {
			details = append(details, "persona="+step.Persona)
		}
		if step.Provider != "" {
			details = append(details, "provider="+step.Provider)
		}
		if step.Model != "" {
			details = append(details, "model="+step.Model)
		}
		if step.When != "" && step.When != "always" {
			details = append(details, "when="+step.When)
		}
		if len(details) == 0 {
			return "(inference)"
		}
		return strings.Join(details, " ")
	}
}
