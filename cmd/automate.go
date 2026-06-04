//go:build !js

package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/automate"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/spf13/cobra"
)

var (
	automateDir       string
	automateAssumeYes bool
)

func init() {
	automateCmd.AddCommand(automateListCmd)
	automateCmd.AddCommand(automateRunCmd)

	automateCmd.PersistentFlags().StringVar(&automateDir, "dir", "", "Workflow directory (default: ./automate)")
	automateCmd.PersistentFlags().BoolVarP(&automateAssumeYes, "yes", "y", false, "Skip the confirmation prompt before starting the workflow")
}

var automateCmd = &cobra.Command{
	Use:   "automate",
	Short: "Discover and run automated agent workflows",
	Long: `Discover and run automated agent workflows from your project's automate/ directory.

Workflows are JSON configuration files that define automated agent behavior —
building, testing, reviewing, and committing code without manual intervention.

Use 'sprout automate' to interactively pick a workflow, or specify one directly:
  sprout automate run full_autonomous
  sprout automate run full_autonomous.json

To create workflows, activate the workflow-automation skill in an agent session
or see: sprout skill list`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAutomatePicker()
	},
}

var automateListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available workflows",
	Long:  `List all workflow configurations found in the automate/ directory.`,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAutomateList()
	},
}

var automateRunCmd = &cobra.Command{
	Use:   "run <workflow>",
	Short: "Run a workflow by name or filename",
	Long: `Run a workflow configuration directly by name or filename.

The workflow name can be specified with or without the .json extension.
If an exact match isn't found, it searches for any JSON file containing
the given name.

Examples:
  sprout automate run full_autonomous
  sprout automate run full_autonomous.json
  sprout automate run review`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAutomateRun(args[0])
	},
}

// getAutomateDir returns the workflow directory path.
func getAutomateDir() string {
	if automateDir != "" {
		if filepath.IsAbs(automateDir) {
			return automateDir
		}
		cwd, _ := os.Getwd()
		return filepath.Join(cwd, automateDir)
	}
	return automate.Dir()
}

// runAutomatePicker shows an interactive workflow picker and runs the selection.
func runAutomatePicker() error {
	dir := getAutomateDir()
	workflows, err := automate.Discover(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return handleNoAutomateDir(dir)
		}
		return fmt.Errorf("failed to scan %s: %w", dir, err)
	}

	if len(workflows) == 0 {
		console.GlyphWarning.Printf("No workflow JSON files found in %s/", dir)
		fmt.Println()
		fmt.Println("To create workflows:")
		fmt.Println("  1. Start an agent session: sprout")
		fmt.Println("  2. Activate the skill: activate_skill workflow-automation")
		fmt.Println("  3. Follow the interactive setup")
		return nil
	}

	items := make([]console.SelectItem, 0, len(workflows))
	for _, wf := range workflows {
		detail := wf.Description
		if detail == "" {
			detail = "(no description)"
		}
		items = append(items, console.SelectItem{
			Label:  wf.Filename,
			Detail: detail,
			Value:  wf.FilePath,
		})
	}

	ctx := context.Background()
	selected, ok, err := console.NewSelectList(console.SelectListOptions{
		Title:      "Select a workflow to run",
		Items:      items,
		Searchable: true,
	}).Run(ctx)
	if err != nil {
		return fmt.Errorf("selection failed: %w", err)
	}
	if !ok {
		fmt.Println("Cancelled.")
		return nil
	}

	return runWorkflowByPath(selected)
}

// runAutomateList prints all discovered workflows.
func runAutomateList() error {
	dir := getAutomateDir()
	workflows, err := automate.Discover(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			console.GlyphWarning.Printf("No automate/ directory found at %s/", dir)
			return nil
		}
		return fmt.Errorf("failed to scan %s: %w", dir, err)
	}

	if len(workflows) == 0 {
		console.GlyphInfo.Printf("No workflows found in %s/", dir)
		return nil
	}

	fmt.Println()
	for _, wf := range workflows {
		desc := wf.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Printf("  %-30s %s\n", wf.Filename, desc)
	}
	fmt.Println()
	return nil
}

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

// listAvailableWorkflows shows available workflow names for the user.
func listAvailableWorkflows(dir string) error {
	fmt.Println("Available workflows:")
	workflows, err := automate.Discover(dir)
	if err != nil {
		return nil
	}
	for _, wf := range workflows {
		fmt.Printf("  %s\n", wf.Filename)
	}
	return nil
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
	if err := os.MkdirAll(dir, 0755); err != nil {
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

	cmd := exec.Command(execPath, "agent", "--workflow-config", path, "--skip-prompt", "--no-web-ui")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
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

	fmt.Println()
	console.GlyphWarning.Printf("Heads up: workflows run autonomously in the background and consume tokens until they finish or are stopped.")
	fmt.Println()
	return nil
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
