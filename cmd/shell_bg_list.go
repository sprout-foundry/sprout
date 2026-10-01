//go:build !js

package cmd

// shell_bg_list.go — the `sprout shell-bg list` subcommand: list display
// (table + JSON), session discovery (BPM current session + PID-file
// fallback), and the per-entry print helpers. Split out of shell_bg.go.
import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/automate"
	"github.com/sprout-foundry/sprout/pkg/console"
)

var shellBgListCmd = &cobra.Command{
	Use:   "list [--json]",
	Short: "List active background sessions",
	Long: `List all background shell sessions tracked by the BackgroundProcessManager.

Discovers sessions by scanning the background-process directory for .pid files,
so sessions started by previous sprout invocations are visible.

Examples:
  sprout shell-bg list
  sprout shell-bg list --json

Output columns:
  SESSION    The session ID (e.g. bg-sleep-abc12345)
  PID        The OS process ID
  COMMAND    The shell command that was started
  STARTED    The wall-clock time the session was created
  ELAPSED    Time since the session was created
  STATUS     'running' or 'exited' (based on kill(pid, 0) probe)`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runShellBgList()
	},
}

func runShellBgList() error {
	baseDir := getShellBgBaseDir()
	entries, err := discoverShellBgSessions(baseDir)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		if shellBgListJSON {
			fmt.Println("[]")
		} else {
			console.GlyphInfo.Printf("No active background shell sessions.")
		}
		return nil
	}

	if shellBgListJSON {
		return printShellBgListJSON(entries)
	}

	printShellBgListTable(entries)
	return nil
}

func discoverShellBgSessions(baseDir string) ([]shellBgEntry, error) {
	// If we have an in-process BPM, use it for richer data.
	if currentBPM != nil {
		return discoverFromBPM(currentBPM)
	}

	// Otherwise, scan .pid files on disk.
	return discoverFromPIDFiles(baseDir)
}

func discoverFromBPM(bpm *tools.BackgroundProcessManager) ([]shellBgEntry, error) {
	ids := bpm.SessionIDs()
	entries := make([]shellBgEntry, 0, len(ids))

	for _, id := range ids {
		proc, ok := bpm.GetProcess(id)
		if !ok {
			continue
		}
		pid := proc.GetPID()
		if pid == 0 {
			continue // exited, no PID available
		}

		status := "running"
		if !automate.IsProcessAlive(pid) {
			status = "exited"
		}

		entries = append(entries, shellBgEntry{
			SessionID:      id,
			PID:            pid,
			Command:        proc.Command,
			StartedAt:      proc.StartedAt.Format(time.RFC3339),
			ElapsedSeconds: int64(time.Since(proc.StartedAt).Seconds()),
			Status:         status,
		})
	}

	return entries, nil
}

func discoverFromPIDFiles(baseDir string) ([]shellBgEntry, error) {
	pidPattern := filepath.Join(baseDir, "*.pid")
	pidFiles, _ := filepath.Glob(pidPattern)
	if pidFiles == nil {
		pidFiles = []string{}
	}

	entries := make([]shellBgEntry, 0, len(pidFiles))
	for _, pidFile := range pidFiles {
		sessionID, pid, startedAt, err := loadProcessFromPIDFile(pidFile)
		if err != nil {
			continue // skip unparseable files
		}

		status := "running"
		if !automate.IsProcessAlive(pid) {
			status = "exited"
		}

		entries = append(entries, shellBgEntry{
			SessionID:      sessionID,
			PID:            pid,
			StartedAt:      startedAt.Format(time.RFC3339),
			ElapsedSeconds: int64(time.Since(startedAt).Seconds()),
			Status:         status,
		})
	}

	return entries, nil
}

// loadProcessFromPIDFile reads the PID file and returns session ID, PID, and started-at time.
// Accepts both the owner-aware format ("<child-pid> <owner-pid>") and the
// legacy single-PID format.
func loadProcessFromPIDFile(pidFile string) (string, int, time.Time, error) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return "", 0, time.Time{}, fmt.Errorf("read pid file %s: %w", pidFile, err)
	}

	pidStr := strings.TrimSpace(string(data))
	if fields := strings.Fields(pidStr); len(fields) > 1 {
		pidStr = fields[0]
	}
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return "", 0, time.Time{}, fmt.Errorf("parse pid from %s: %w", pidFile, err)
	}

	info, err := os.Stat(pidFile)
	if err != nil {
		return "", 0, time.Time{}, fmt.Errorf("stat pid file %s: %w", pidFile, err)
	}

	base := filepath.Base(pidFile)
	sessionID := strings.TrimSuffix(base, ".pid")

	return sessionID, pid, info.ModTime(), nil
}

func printShellBgListTable(entries []shellBgEntry) {
	fmt.Println()
	fmt.Printf("  %-30s %-8s %-25s %-10s %-10s %s\n",
		"SESSION", "PID", "COMMAND", "STARTED", "ELAPSED", "STATUS")
	fmt.Println()
	for _, e := range entries {
		cmdPreview := e.Command
		if len(cmdPreview) > 25 {
			cmdPreview = cmdPreview[:22] + "..."
		}
		if cmdPreview == "" {
			cmdPreview = "(unknown)"
		}
		startedTime, err := time.Parse(time.RFC3339, e.StartedAt)
		if err != nil {
			startedTime = time.Time{}
		}
		fmt.Printf("  %-30s %-8d %-25s %-10s %-10s %s\n",
			e.SessionID,
			e.PID,
			cmdPreview,
			startedTime.Format("15:04:05"),
			time.Duration(e.ElapsedSeconds)*time.Second,
			e.Status,
		)
	}
	fmt.Println()
}

func printShellBgListJSON(entries []shellBgEntry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal list JSON: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

// ---------------------------------------------------------------------------
// status
// ---------------------------------------------------------------------------
