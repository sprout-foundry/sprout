//go:build !js

package cmd

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/automate"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/spf13/cobra"
)

var (
	automateDir              string
	automateAssumeYes        bool
	automateBudgetUSD        float64
	automateBudgetWarn       string
	automateHeartbeatSeconds int
)

func init() {
	automateCmd.AddCommand(automateListCmd)
	automateCmd.AddCommand(automateRunCmd)
	automateCmd.AddCommand(automateStatusCmd)
	automateCmd.AddCommand(automateStopCmd)
	automateCmd.AddCommand(automateLogsCmd)

	automateCmd.PersistentFlags().StringVar(&automateDir, "dir", "", "Workflow directory (default: ./automate)")
	automateCmd.PersistentFlags().BoolVarP(&automateAssumeYes, "yes", "y", false, "Skip the confirmation prompt before starting the workflow")
	automateCmd.PersistentFlags().Float64Var(&automateBudgetUSD, "budget-usd", 0, "Hard cap on workflow USD spend (overrides workflow JSON budget.usd; 0 = no cap)")
	automateCmd.PersistentFlags().StringVar(&automateBudgetWarn, "budget-warn", "", "Comma-separated warning thresholds as fractions of the budget, e.g. '0.5,0.8'")
	automateCmd.PersistentFlags().IntVar(&automateHeartbeatSeconds, "heartbeat", 0, "Print [budget] progress every N seconds during the run (overrides workflow JSON progress.heartbeat_seconds)")
}

var automateCmd = &cobra.Command{
	Use:   "automate",
	Short: "Discover and run automated agent workflows",
	Long: `Discover and run automated agent workflows from your project's automate/ directory.

Workflows are JSON configuration files that define automated agent behavior —
building, testing, reviewing, and committing code without manual intervention.

Use 'sprout automate run <name>' to run a workflow.
Use 'sprout automate status' to see running sessions.
Use 'sprout automate stop <session>' to stop a running session.
Use 'sprout automate logs <session>' to view session output.

To create workflows, activate the workflow-automation skill in an agent session
or see: sprout skill list`,
	Args: cobra.NoArgs,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
		sproutDir := filepath.Join(cwd, ".sprout")
		removed, err := automate.SweepStaleSessions(sproutDir)
		if err != nil {
			// Log warning but don't fail
			fmt.Fprintf(os.Stderr, "warn: stale session sweep: %v\n", err)
		} else if removed > 0 {
			fmt.Fprintf(os.Stderr, "Cleaned up %d stale session(s)\n", removed)
		}
		return nil
	},
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
		Workflow:    filepath.Base(path),
		PID:         cmd.Process.Pid,
		StartedAt:   time.Now(),
		Kind:        "automate",
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

// -----------------------------------------------------------------------
// Status subcommand
// -----------------------------------------------------------------------

var (
	automateStatusAll  bool
	automateStatusJSON bool
)

var automateStatusCmd = &cobra.Command{
	Use:   "status [--all] [--json]",
	Short: "Show running automate sessions",
	Long: `Show currently running automate workflow sessions.

By default only shows running (alive) sessions. Use --all to include
exited sessions as well. Use --json for machine-readable output.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAutomateStatus()
	},
}

func runAutomateStatus() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	sproutDir := filepath.Join(cwd, ".sprout")

	sessions, err := readAllSessions(sproutDir)
	if err != nil {
		return err
	}

	if len(sessions) == 0 {
		if automateStatusJSON {
			fmt.Println("[]")
		} else {
			console.GlyphInfo.Printf("No automate sessions found.")
		}
		return nil
	}

	// Filter to running only unless --all
	if !automateStatusAll {
		filtered := make([]sessionEntry, 0, len(sessions))
		for _, s := range sessions {
			if automate.IsProcessAlive(s.PID) {
				filtered = append(filtered, s)
			}
		}
		sessions = filtered
	}

	if len(sessions) == 0 {
		if automateStatusJSON {
			fmt.Println("[]")
		} else {
			console.GlyphInfo.Printf("No running automate sessions.")
		}
		return nil
	}

	if automateStatusJSON {
		return printStatusJSON(sessions)
	}

	printStatusTable(sessions)
	return nil
}

type sessionEntry struct {
	SessionID string
	automate.AutomateSessionInfo
}

func readAllSessions(sproutDir string) ([]sessionEntry, error) {
	dir := filepath.Join(sproutDir, "automate")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read automate session directory: %w", err)
	}

	var sessions []sessionEntry
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}
		if len(name) <= 5 {
			continue // skip filenames too short to have a meaningful session ID
		}
		sessionID := name[:len(name)-5] // strip ".json"
		info, err := automate.ReadSessionFile(sproutDir, sessionID)
		if err != nil {
			continue
		}
		sessions = append(sessions, sessionEntry{
			SessionID:          sessionID,
			AutomateSessionInfo: *info,
		})
	}
	return sessions, nil
}

func printStatusTable(sessions []sessionEntry) {
	fmt.Println()
	fmt.Printf("  %-30s %-25s %-10s %-8s %-10s %s\n",
		"SESSION", "WORKFLOW", "STATUS", "PID", "STARTED", "ELAPSED")
	fmt.Println()
	for _, s := range sessions {
		status := "exited"
		if automate.IsProcessAlive(s.PID) {
			status = "running"
		}
		fmt.Printf("  %-30s %-25s %-10s %-8d %-10s %s\n",
			s.SessionID,
			s.Workflow,
			status,
			s.PID,
			s.StartedAt.Format("15:04:05"),
			time.Since(s.StartedAt).Round(time.Second),
		)
	}
	fmt.Println()
}

func printStatusJSON(sessions []sessionEntry) error {
	type statusEntry struct {
		SessionID       string `json:"session_id"`
		Workflow        string `json:"workflow"`
		Status          string `json:"status"`
		PID             int    `json:"pid"`
		StartedAt       string `json:"started_at"`
		ElapsedSeconds  int64  `json:"elapsed_seconds"`
	}

	entries := make([]statusEntry, 0, len(sessions))
	for _, s := range sessions {
		status := "exited"
		if automate.IsProcessAlive(s.PID) {
			status = "running"
		}
		entries = append(entries, statusEntry{
			SessionID:      s.SessionID,
			Workflow:       s.Workflow,
			Status:         status,
			PID:            s.PID,
			StartedAt:      s.StartedAt.Format(time.RFC3339),
			ElapsedSeconds: int64(time.Since(s.StartedAt).Seconds()),
		})
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal status JSON: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

// -----------------------------------------------------------------------
// Stop subcommand
// -----------------------------------------------------------------------

var automateStopAll bool

var automateStopCmd = &cobra.Command{
	Use:   "stop <session_id> [--all]",
	Short: "Stop a running automate session",
	Long: `Stop a running automate workflow session by session ID.

The process is stopped via signal escalation: SIGINT, then SIGTERM,
then SIGKILL if the process persists. The PID file is removed after
the process is confirmed dead.

Use --all to stop all running sessions.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if automateStopAll {
			return runAutomateStopAll()
		}
		if len(args) == 0 {
			return fmt.Errorf("session ID is required (or use --all to stop all sessions)")
		}
		return runAutomateStop(args[0])
	},
}

func runAutomateStop(sessionID string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	sproutDir := filepath.Join(cwd, ".sprout")

	info, err := automate.ReadSessionFile(sproutDir, sessionID)
	if err != nil {
		return err
	}

	if !automate.IsProcessAlive(info.PID) {
		// Already dead — just clean up the PID file
		if err := automate.RemoveSessionFile(sproutDir, sessionID); err != nil {
			fmt.Fprintf(os.Stderr, "warn: %v\n", err)
		}
		console.GlyphInfo.Printf("Session %s (PID %d) was already stopped. PID file cleaned up.", sessionID, info.PID)
		return nil
	}

	console.GlyphAction.Printf("Stopping session %s (PID %d, workflow: %s)...", sessionID, info.PID, info.Workflow)

	ok, err := automate.StopProcess(info.PID)
	if err != nil {
		console.GlyphWarning.Printf("Error stopping process %d: %v", info.PID, err)
	}

	// Remove PID file regardless
	if err := automate.RemoveSessionFile(sproutDir, sessionID); err != nil {
		fmt.Fprintf(os.Stderr, "warn: %v\n", err)
	}

	if ok {
		console.GlyphSuccess.Printf("Stopped session %s (PID %d).", sessionID, info.PID)
	} else {
		console.GlyphWarning.Printf("Session %s (PID %d) may still be running — verify manually.", sessionID, info.PID)
	}
	return nil
}

func runAutomateStopAll() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	sproutDir := filepath.Join(cwd, ".sprout")

	sessions, err := readAllSessions(sproutDir)
	if err != nil {
		return err
	}

	stopped := 0
	for _, s := range sessions {
		if !automate.IsProcessAlive(s.PID) {
			// Already dead, just clean up
			_ = automate.RemoveSessionFile(sproutDir, s.SessionID)
			continue
		}
		console.GlyphAction.Printf("Stopping session %s (PID %d)...", s.SessionID, s.PID)
		ok, err := automate.StopProcess(s.PID)
		if err != nil {
			console.GlyphWarning.Printf("Error stopping PID %d: %v", s.PID, err)
		}
		_ = automate.RemoveSessionFile(sproutDir, s.SessionID)
		if ok {
			stopped++
		}
	}

	if stopped == 0 {
		console.GlyphInfo.Printf("No running sessions to stop.")
	} else {
		console.GlyphSuccess.Printf("Stopped %d session(s).", stopped)
	}
	return nil
}

// -----------------------------------------------------------------------
// Logs subcommand
// -----------------------------------------------------------------------

var (
	automateLogsFollow bool
	automateLogsLines  int
)

var automateLogsCmd = &cobra.Command{
	Use:   "logs <session_id> [-f] [-n N]",
	Short: "View output from an automate session",
	Long: `View the captured output from an automate workflow session.

Use -f to follow the output in real time (stops when the process exits).
Use -n N to show only the last N lines.

Note: CLI sessions that pipe to terminal do not have captured output files.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAutomateLogs(args[0])
	},
}

func runAutomateLogs(sessionID string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	sproutDir := filepath.Join(cwd, ".sprout")

	info, err := automate.ReadSessionFile(sproutDir, sessionID)
	if err != nil {
		return err
	}

	if info.OutputFilePath == "" {
		fmt.Printf("No captured output for session %s (CLI sessions pipe to terminal)\n", sessionID)
		return nil
	}

	data, err := os.ReadFile(info.OutputFilePath)
	if err != nil {
		return fmt.Errorf("read output file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	// Remove trailing empty element from trailing newline.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if automateLogsLines > 0 && automateLogsLines < len(lines) {
		lines = lines[len(lines)-automateLogsLines:]
	}

	for _, line := range lines {
		fmt.Println(line)
	}

	if automateLogsFollow {
		return followLogFile(info.OutputFilePath, info.PID)
	}
	return nil
}

func followLogFile(path string, pid int) error {
	// Get the initial file size
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat output file: %w", err)
	}
	offset := fi.Size()

	for {
		if !automate.IsProcessAlive(pid) {
			// Process is gone — do one final read then exit
			break
		}
		time.Sleep(500 * time.Millisecond)

		// Check if the file was truncated (e.g. log rotation).
		fi, err := os.Stat(path)
		if err == nil && fi.Size() < offset {
			offset = 0 // file was truncated, restart from beginning
		}

		f, err := os.Open(path)
		if err != nil {
			continue
		}

		_, err = f.Seek(offset, io.SeekStart)
		if err != nil {
			f.Close()
			continue
		}

		newData, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			continue
		}

		if len(newData) > 0 {
			fmt.Print(string(newData))
			offset += int64(len(newData))
		}
	}

	// Final read after process exits
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	_, err = f.Seek(offset, io.SeekStart)
	if err != nil {
		return nil
	}

	newData, err := io.ReadAll(f)
	if err != nil || len(newData) == 0 {
		return nil
	}
	fmt.Print(string(newData))
	return nil
}
