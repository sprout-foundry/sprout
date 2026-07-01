//go:build !js

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/automate"
	"github.com/sprout-foundry/sprout/pkg/console"
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
