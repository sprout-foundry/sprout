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
	automateDetach           bool
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

	automateStatusCmd.Flags().StringVar(&automateSessionDir, "dir", "", "Path to the .sprout session root (default: nearest .sprout/automate/ found walking up from the current directory)")
	automateStopCmd.Flags().StringVar(&automateSessionDir, "dir", "", "Path to the .sprout session root (default: nearest .sprout/automate/ found walking up from the current directory)")
	automateLogsCmd.Flags().StringVar(&automateSessionDir, "dir", "", "Path to the .sprout session root (default: nearest .sprout/automate/ found walking up from the current directory)")

	// AUTOM-6: these globals were always read by the run functions and
	// advertised in the Use/Long help text, but never bound to cobra —
	// `sprout automate status --all`, `stop --all`, and `logs -f`/`-n`
	// all failed with "unknown flag". In-process callers (agent tool
	// layer, tests) set the globals directly and are unaffected.
	automateStatusCmd.Flags().BoolVar(&automateStatusAll, "all", false, "Include exited sessions (default: running sessions plus those that ended within the last 24h)")
	automateStatusCmd.Flags().BoolVar(&automateStatusJSON, "json", false, "Output sessions as a JSON array (machine-readable)")
	automateStopCmd.Flags().BoolVar(&automateStopAll, "all", false, "Stop all running sessions (session ID not required)")
	automateLogsCmd.Flags().BoolVarP(&automateLogsFollow, "follow", "f", false, "Follow output in real time (stops when the process exits)")
	automateLogsCmd.Flags().IntVarP(&automateLogsLines, "lines", "n", 0, "Show only the last N lines (0 = all)")

	automateCmd.PersistentFlags().StringVar(&automateDir, "dir", "", "Workflow directory (default: ./automate)")
	automateCmd.PersistentFlags().BoolVarP(&automateAssumeYes, "yes", "y", false, "Skip the confirmation prompt before starting the workflow")
	automateCmd.PersistentFlags().BoolVar(&automateDetach, "detach", false, "Run the workflow in the background: child stdio is redirected to .sprout/automate/logs/<sessionID>.log under the discovered sprout root (recorded in the session file) and the command returns immediately instead of streaming to the terminal")
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
		sproutDir, err := automateSessionRoot()
		if err != nil {
			return err
		}
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
	Use:   "run <workflow> [--detach]",
	Short: "Run a workflow by name or filename",
	Long: `Run a workflow configuration directly by name or filename.

The workflow name can be specified with or without the .json extension.
If an exact match isn't found, it searches for any JSON file containing
the given name.

By default the workflow runs in the foreground: output streams to this
terminal and the command waits for it to finish. With --detach it runs in
the background instead — child stdio is redirected to
.sprout/automate/logs/<sessionID>.log under the discovered sprout root
(the nearest .sprout/automate/ found walking up from the current
directory), the log path is recorded in the session file
(output_file_path), and the command returns immediately. Monitor it with
'sprout automate status' and 'sprout automate logs <session>'.

Examples:
  sprout automate run full_autonomous
  sprout automate run full_autonomous.json
  sprout automate run review
  sprout automate run review --detach`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAutomateRun(args[0])
	},
}

var automateStatusCmd = &cobra.Command{
	Use:   "status [--all] [--json] [--dir PATH]",
	Short: "Show running automate sessions",
	Long: `Show currently running automate workflow sessions.

By default only shows running (alive) sessions. Use --all to include
exited sessions as well. Use --json for machine-readable output.

Sessions are found by walking up from the current directory to the nearest
.sprout/automate/ (then the central registry); --dir overrides that root.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAutomateStatus()
	},
}

var automateStopCmd = &cobra.Command{
	Use:   "stop <session_id> [--all] [--dir PATH]",
	Short: "Stop a running automate session",
	Long: `Stop a running automate workflow session by session ID.

The process is stopped via signal escalation: SIGINT, then SIGTERM,
then SIGKILL if the process persists. The PID file is removed after
the process is confirmed dead.

Sessions are found by walking up from the current directory to the nearest
.sprout/automate/ (then the central registry); --dir overrides that root.

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
	Use:   "logs <session_id> [-f] [-n N] [--dir PATH]",
	Short: "View output from an automate session",
	Long: `View the captured output from an automate workflow session.

Use -f/--follow to follow the output in real time — it keeps tailing
the log file until the process exits.

Use -n N to show only the last N lines.

Sessions launched with 'sprout automate run --detach' write their output
to .sprout/automate/logs/<sessionID>.log under the session root; this
command reads that file. Sessions are found by walking up from the current
directory to the nearest .sprout/automate/ (then the central registry);
--dir overrides that root.

Note: attached (foreground) CLI sessions stream to the terminal and have
no captured output file.`,
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
