//go:build !js

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// currentBPM is an optional in-process BPM for richer output. Set in tests.
var currentBPM *tools.BackgroundProcessManager

// shellBgBaseDirOverride is a test-only hook to override the base directory.
// When non-empty, runShellBg* functions use this instead of the default.
var shellBgBaseDirOverride string

var (
	shellBgListJSON bool
	shellBgGrace    time.Duration
	shellBgTTL      time.Duration
)

var shellBgCmd = &cobra.Command{
	Use:   "shell-bg",
	Short: "Monitor and manage CLI-mode background processes",
	Long: `Inspect and control background shell processes started by sprout.

When sprout runs without the WebUI (e.g., 'sprout agent --no-web-ui'),
shell commands that exceed their timeout are promoted to background processes
tracked by the BackgroundProcessManager (BPM). This command provides a CLI
surface for listing, inspecting, and stopping those processes.

Each session is identified by a session ID of the form 'bg-<cmd>-<hex>'.
The ID, PID, command, and accumulated output are persisted in the
background-process directory (default: /tmp/sprout-bg/).

Subcommands:
  list        Show all sessions (PID file based discovery)
  status ID   Print accumulated output and runtime for one session
  stop ID     Stop one session via SIGINT->SIGTERM->SIGKILL cascade
  stop-all    Stop every active session`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	shellBgCmd.AddCommand(shellBgListCmd)
	shellBgCmd.AddCommand(shellBgStatusCmd)
	shellBgCmd.AddCommand(shellBgStopCmd)
	shellBgCmd.AddCommand(shellBgStopAllCmd)
	shellBgCmd.AddCommand(shellBgKeepaliveCmd)

	shellBgListCmd.Flags().BoolVar(&shellBgListJSON, "json", false, "Output in JSON format")
	shellBgStopCmd.Flags().DurationVar(&shellBgGrace, "grace", 10*time.Second, "Grace period between SIGINT and SIGTERM")
}

// ---------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------

// shellBgEntry is the JSON output structure for list --json.
type shellBgEntry struct {
	SessionID      string `json:"session_id"`
	PID            int    `json:"pid"`
	Command        string `json:"command,omitempty"`
	StartedAt      string `json:"started_at"`
	ElapsedSeconds int64  `json:"elapsed_seconds"`
	Status         string `json:"status"`
}

// isStdinTTY checks if stdin is connected to a terminal.
func isStdinTTY() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

// ---------------------------------------------------------------------------
// keepalive
// ---------------------------------------------------------------------------

var shellBgKeepaliveCmd = &cobra.Command{
	Use:   "keepalive <session_id>",
	Short: "Renew a background session's expiry timer",
	Long: `Touch a background session's activity timer so the cleanup pass treats
it as recently used.

Use this for long-lived watcher sessions (gh run watch, tail -f) that
are silent for hours by design: renewal prevents the session's TTL from
expiring while the process is still healthy. Safe to call repeatedly
(e.g. from a wait loop).

In-process sessions (same sprout invocation) are renewed directly.
Cross-process sessions discovered via PID files are renewed by touching
their .pid file, which updates the mtime used as the fallback activity
signal.

Examples:
  sprout shell-bg keepalive bg-sleep-abc12345`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runShellBgKeepalive(args[0])
	},
}

func runShellBgKeepalive(sessionID string) error {
	// In-process BPM renews directly.
	if currentBPM != nil {
		if _, ok := currentBPM.GetProcess(sessionID); ok {
			if err := currentBPM.KeepAlive(sessionID); err != nil {
				return err
			}
			console.GlyphSuccess.Printf("Renewed %s.", sessionID)
			return nil
		}
	}

	// Cross-process: touch the .pid file so its mtime (the fallback
	// activity signal used by list/status discovery) reflects the renewal.
	baseDir := getShellBgBaseDir()
	pidFile := filepath.Join(baseDir, sessionID+".pid")
	if _, err := os.Stat(pidFile); err != nil {
		return fmt.Errorf("session %q not found: %w", sessionID, err)
	}
	now := time.Now()
	if err := os.Chtimes(pidFile, now, now); err != nil {
		return fmt.Errorf("touch pid file: %w", err)
	}
	console.GlyphSuccess.Printf("Renewed %s.", sessionID)
	return nil
}

// getShellBgBaseDir returns the base directory for shell-bg files.
// Uses the override if set (for tests), otherwise the default.
func getShellBgBaseDir() string {
	if shellBgBaseDirOverride != "" {
		return shellBgBaseDirOverride
	}
	return tools.GetBackgroundOutputBaseDir()
}
