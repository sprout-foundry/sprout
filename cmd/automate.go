//go:build !js

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/spf13/cobra"
)

var automateDir string

func init() {
	automateCmd.Flags().StringVar(&automateDir, "dir", "", "Workflow directory (default: ./automate)")
	automateCmd.PersistentFlags().StringVar(&automateDir, "dir", "", "Workflow directory (default: ./automate)")
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

// workflowEntry is a parsed workflow file with its metadata.
type workflowEntry struct {
	Filename    string
	FilePath    string
	Description string
}

func init() {
	automateCmd.AddCommand(automateListCmd)
	automateCmd.AddCommand(automateRunCmd)
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
	cwd, _ := os.Getwd()
	return filepath.Join(cwd, "automate")
}

// discoverWorkflows scans the automate directory for JSON workflow files.
func discoverWorkflows(dir string) ([]workflowEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var workflows []workflowEntry
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.ToLower(filepath.Ext(name)) != ".json" {
			continue
		}

		fullPath := filepath.Join(dir, name)
		desc, err := extractWorkflowDescription(fullPath)
		if err != nil {
			// Not a valid workflow JSON — skip silently
			continue
		}

		workflows = append(workflows, workflowEntry{
			Filename:    name,
			FilePath:    fullPath,
			Description: desc,
		})
	}

	return workflows, nil
}

// extractWorkflowDescription reads a JSON file and returns its description field.
// Returns empty string if the file is valid JSON but has no description.
func extractWorkflowDescription(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", err // Not valid JSON
	}

	// Check that it looks like a workflow config (has "initial" or "steps")
	hasInitial := false
	hasSteps := false
	if _, ok := raw["initial"]; ok {
		hasInitial = true
	}
	if _, ok := raw["steps"]; ok {
		hasSteps = true
	}
	if !hasInitial && !hasSteps {
		return "", fmt.Errorf("not a workflow config")
	}

	// Extract description
	var desc string
	if descRaw, ok := raw["description"]; ok {
		_ = json.Unmarshal(descRaw, &desc)
	}

	return desc, nil
}

// runAutomatePicker shows an interactive workflow picker and runs the selection.
func runAutomatePicker() error {
	dir := getAutomateDir()
	workflows, err := discoverWorkflows(dir)
	if err != nil {
		if os.IsNotExist(err) {
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
	workflows, err := discoverWorkflows(dir)
	if err != nil {
		if os.IsNotExist(err) {
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
	workflows, err := discoverWorkflows(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return handleNoAutomateDir(dir)
		}
		return fmt.Errorf("failed to scan %s: %w", dir, err)
	}

	// Try exact filename match first
	target := name
	if !strings.HasSuffix(strings.ToLower(name), ".json") {
		target = name + ".json"
	}

	for _, wf := range workflows {
		if wf.Filename == target {
			return runWorkflowByPath(wf.FilePath)
		}
	}

	// Try substring match
	var matches []workflowEntry
	for _, wf := range workflows {
		if strings.Contains(strings.ToLower(wf.Filename), strings.ToLower(name)) {
			matches = append(matches, wf)
		}
	}

	if len(matches) == 1 {
		return runWorkflowByPath(matches[0].FilePath)
	}

	if len(matches) > 1 {
		console.GlyphWarning.Printf("Multiple workflows match %q:", name)
		for _, m := range matches {
			fmt.Printf("  %s\n", m.Filename)
		}
		fmt.Println("Please specify the full filename.")
		return nil
	}

	console.GlyphWarning.Printf("No workflow matching %q found in %s/", name, dir)
	fmt.Println()
	fmt.Println("Available workflows:")
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

	var response string
	fmt.Scanln(&response)
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

	// Read the workflow to display info before running
	desc, _ := extractWorkflowDescription(path)
	name := filepath.Base(path)

	fmt.Println()
	console.GlyphAction.Printf("Running workflow: %s", name)
	if desc != "" {
		fmt.Printf("  %s\n", desc)
	}
	fmt.Println()

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
