//go:build !js

package cmd

import (
	"bufio"
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

// currentBPM is an optional in-process BPM for richer output. Set in tests.
var currentBPM *tools.BackgroundProcessManager

// shellBgBaseDirOverride is a test-only hook to override the base directory.
// When non-empty, runShellBg* functions use this instead of the default.
var shellBgBaseDirOverride string

var (
	shellBgListJSON bool
	shellBgGrace    time.Duration
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

	shellBgListCmd.Flags().BoolVar(&shellBgListJSON, "json", false, "Output in JSON format")
	shellBgStopCmd.Flags().DurationVar(&shellBgGrace, "grace", 10*time.Second, "Grace period between SIGINT and SIGTERM")
}

// ---------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------

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

// shellBgEntry is the JSON output structure for list --json.
type shellBgEntry struct {
	SessionID      string `json:"session_id"`
	PID            int    `json:"pid"`
	Command        string `json:"command,omitempty"`
	StartedAt      string `json:"started_at"`
	ElapsedSeconds int64  `json:"elapsed_seconds"`
	Status         string `json:"status"`
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
func loadProcessFromPIDFile(pidFile string) (string, int, time.Time, error) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return "", 0, time.Time{}, fmt.Errorf("read pid file %s: %w", pidFile, err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
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

var shellBgStatusCmd = &cobra.Command{
	Use:   "status <session_id>",
	Short: "Show details for a background session",
	Long: `Print accumulated output and runtime information for a specific background session.

Reads the .pid and .output files from the background-process directory.
If the session is still running, the output file is read live.

Examples:
  sprout shell-bg status bg-sleep-abc12345`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runShellBgStatus(args[0])
	},
}

func runShellBgStatus(sessionID string) error {
	baseDir := getShellBgBaseDir()

	// Try BPM first for richer data
	if currentBPM != nil {
		proc, ok := currentBPM.GetProcess(sessionID)
		if ok {
			return printStatusFromBPM(proc, sessionID)
		}
	}

	// Fall back to .pid/.output files
	pidFile := filepath.Join(baseDir, sessionID+".pid")
	if _, err := os.Stat(pidFile); err != nil {
		return fmt.Errorf("session %q not found: %w", sessionID, err)
	}

	pid, startedAt, err := loadPIDFromFile(pidFile)
	if err != nil {
		return fmt.Errorf("session %q: %w", sessionID, err)
	}

	// Read output file
	outputFile := filepath.Join(baseDir, sessionID+".output")
	var output string
	if data, err := os.ReadFile(outputFile); err == nil {
		output = string(data)
	}

	status := "running"
	if !automate.IsProcessAlive(pid) {
		status = "exited"
	}

	printShellBgStatusTable(sessionID, pid, "", startedAt, status, output)
	return nil
}

func printStatusFromBPM(proc *tools.BackgroundProcess, sessionID string) error {
	pid := proc.GetPID()
	if pid == 0 {
		return fmt.Errorf("session %q not found or already exited", sessionID)
	}

	outputPath := proc.GetOutputPath()
	var output string
	if outputPath != "" {
		if data, err := os.ReadFile(outputPath); err == nil {
			output = string(data)
		}
	}

	status := "running"
	if !automate.IsProcessAlive(pid) {
		status = "exited"
	}

	printShellBgStatusTable(sessionID, pid, proc.Command, proc.StartedAt, status, output)
	return nil
}

// loadPIDFromFile reads a .pid file and returns the PID and start time.
// It delegates to loadProcessFromPIDFile and discards the session ID.
func loadPIDFromFile(pidFile string) (int, time.Time, error) {
	_, pid, startedAt, err := loadProcessFromPIDFile(pidFile)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("load pid file: %w", err)
	}
	return pid, startedAt, nil
}

func printShellBgStatusTable(sessionID string, pid int, command string, startedAt time.Time, status string, output string) {
	fmt.Println()
	fmt.Printf("Session:  %s\n", sessionID)
	fmt.Printf("PID:      %d\n", pid)
	if command != "" {
		fmt.Printf("Command:  %s\n", command)
	}
	fmt.Printf("Started:  %s\n", startedAt.Format(time.RFC3339))
	fmt.Printf("Elapsed:  %s\n", time.Since(startedAt).Round(time.Second))
	fmt.Printf("Status:   %s\n", status)

	if output != "" {
		fmt.Println()
		fmt.Println("--- Output ---")
		fmt.Print(output)
		if !strings.HasSuffix(output, "\n") {
			fmt.Println()
		}
		fmt.Println("--- End ---")
	}
	fmt.Println()
}

// ---------------------------------------------------------------------------
// stop
// ---------------------------------------------------------------------------

var shellBgStopCmd = &cobra.Command{
	Use:   "stop <session_id> [--grace=10s]",
	Short: "Stop a background session",
	Long: `Stop a background shell session by session ID.

The process is stopped via signal escalation: SIGINT, then SIGTERM,
then SIGKILL if the process persists. The .pid and .output files are
removed after the process is confirmed dead.

The --grace value applies to in-process sessions (managed by the
BackgroundProcessManager). Cross-process sessions discovered via PID
files use a fixed escalation timing (10s/5s/2s).

Examples:
  sprout shell-bg stop bg-sleep-abc12345
  sprout shell-bg stop bg-sleep-abc12345 --grace=5s`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runShellBgStop(args[0])
	},
}

func runShellBgStop(sessionID string) error {
	baseDir := getShellBgBaseDir()

	// Try BPM first
	if currentBPM != nil {
		proc, ok := currentBPM.GetProcess(sessionID)
		if ok {
			return stopFromBPM(currentBPM, proc, sessionID, baseDir)
		}
	}

	// Fall back to .pid file
	pidFile := filepath.Join(baseDir, sessionID+".pid")
	if _, err := os.Stat(pidFile); err != nil {
		return fmt.Errorf("session %q not found: %w", sessionID, err)
	}

	pid, startedAt, err := loadPIDFromFile(pidFile)
	if err != nil {
		return fmt.Errorf("session %q: %w", sessionID, err)
	}

	// Check if already dead
	if !automate.IsProcessAlive(pid) {
		// Already dead — just clean up files
		cleanupShellBgFiles(baseDir, sessionID)
		console.GlyphInfo.Printf("Session %s (PID %d) was already stopped. Files cleaned up.", sessionID, pid)
		return nil
	}

	// PID-reuse guard: ensure the current process at this PID started before
	// the recorded session start time (proxied by the .pid file's mtime).
	if !automate.VerifyProcessStartedBefore(pid, startedAt) {
		console.GlyphWarning.Printf(
			"Session %s recorded PID %d at %s, but the current process at that PID started later — possible PID reuse. Refusing to signal. Cleaned up PID file.",
			sessionID, pid, startedAt.Format(time.RFC3339),
		)
		cleanupShellBgFiles(baseDir, sessionID)
		return nil
	}

	console.GlyphAction.Printf("Stopping session %s (PID %d)...", sessionID, pid)

	// Use automate.StopProcess for the signal escalation
	ok, err := automate.StopProcess(pid)
	if err != nil {
		console.GlyphWarning.Printf("Error stopping process %d: %v", pid, err)
	}

	// Clean up files
	cleanupShellBgFiles(baseDir, sessionID)

	if ok {
		console.GlyphSuccess.Printf("Stopped session %s (PID %d).", sessionID, pid)
	} else {
		console.GlyphWarning.Printf("Session %s (PID %d) may still be running — verify manually.", sessionID, pid)
	}
	return nil
}

func stopFromBPM(bpm *tools.BackgroundProcessManager, proc *tools.BackgroundProcess, sessionID, baseDir string) error {
	pid := proc.GetPID()
	if pid == 0 {
		cleanupShellBgFiles(baseDir, sessionID)
		console.GlyphInfo.Printf("Session %s was already exited. Files cleaned up.", sessionID)
		return nil
	}

	if !automate.IsProcessAlive(pid) {
		cleanupShellBgFiles(baseDir, sessionID)
		console.GlyphInfo.Printf("Session %s (PID %d) was already stopped. Files cleaned up.", sessionID, pid)
		return nil
	}

	console.GlyphAction.Printf("Stopping session %s (PID %d)...", sessionID, pid)

	// Use BPM's Stop method (it has process group awareness)
	if err := bpm.Stop(sessionID, shellBgGrace); err != nil {
		console.GlyphWarning.Printf("Error stopping session %s: %v", sessionID, err)
	}

	// Clean up files
	cleanupShellBgFiles(baseDir, sessionID)

	// Check if it's actually dead now
	if automate.IsProcessAlive(pid) {
		console.GlyphWarning.Printf("Session %s (PID %d) may still be running — verify manually.", sessionID, pid)
	} else {
		console.GlyphSuccess.Printf("Stopped session %s (PID %d).", sessionID, pid)
	}
	return nil
}

func cleanupShellBgFiles(baseDir, sessionID string) {
	_ = os.Remove(filepath.Join(baseDir, sessionID+".pid"))
	_ = os.Remove(filepath.Join(baseDir, sessionID+".output"))
}

// ---------------------------------------------------------------------------
// stop-all
// ---------------------------------------------------------------------------

var shellBgStopAllCmd = &cobra.Command{
	Use:   "stop-all",
	Short: "Stop all active background sessions",
	Long: `Stop every active background shell session.

Enumerates all .pid files in the background-process directory and stops
each running process via signal escalation. Already-exited sessions have
their files cleaned up without signaling.

In non-interactive mode (CI, pipe), the confirmation prompt is skipped.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runShellBgStopAll()
	},
}

func runShellBgStopAll() error {
	baseDir := getShellBgBaseDir()

	// Check if stdin is a TTY for confirmation prompt
	isTTY := isStdinTTY()

	// If interactive, ask for confirmation
	if isTTY {
		fmt.Print("Stop all background shell sessions? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		response, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Cancelled.")
			return nil
		}
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	// Try BPM first for in-process sessions.
	// Collect BPM session IDs before stopping them, so we don't re-signal
	// via the PID-file loop below.
	var skipBPM map[string]bool
	if currentBPM != nil {
		skipBPM = make(map[string]bool, len(currentBPM.SessionIDs()))
		for _, id := range currentBPM.SessionIDs() {
			skipBPM[id] = true
		}
		currentBPM.StopAll()
	}

	// Scan .pid files on disk
	pidPattern := filepath.Join(baseDir, "*.pid")
	pidFiles, _ := filepath.Glob(pidPattern)
	if pidFiles == nil {
		pidFiles = []string{}
	}

	if len(pidFiles) == 0 {
		console.GlyphInfo.Printf("No background shell sessions found.")
		return nil
	}

	stopped := 0
	for _, pidFile := range pidFiles {
		sessionID, pid, startedAt, err := loadProcessFromPIDFile(pidFile)
		if err != nil {
			continue
		}

		// Skip sessions already stopped via BPM.
		if skipBPM[sessionID] {
			continue
		}

		if !automate.IsProcessAlive(pid) {
			// Already dead, just clean up
			cleanupShellBgFiles(baseDir, sessionID)
			continue
		}

		// PID-reuse guard: ensure the current process at this PID started
		// before the recorded session start time (proxied by .pid file mtime).
		if !automate.VerifyProcessStartedBefore(pid, startedAt) {
			console.GlyphWarning.Printf(
				"Session %s PID %d appears recycled — skipping and cleaning up.",
				sessionID, pid,
			)
			cleanupShellBgFiles(baseDir, sessionID)
			continue
		}

		console.GlyphAction.Printf("Stopping session %s (PID %d)...", sessionID, pid)
		ok, err := automate.StopProcess(pid)
		if err != nil {
			console.GlyphWarning.Printf("Error stopping PID %d: %v", pid, err)
		}
		cleanupShellBgFiles(baseDir, sessionID)
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

// isStdinTTY checks if stdin is connected to a terminal.
func isStdinTTY() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

// getShellBgBaseDir returns the base directory for shell-bg files.
// Uses the override if set (for tests), otherwise the default.
func getShellBgBaseDir() string {
	if shellBgBaseDirOverride != "" {
		return shellBgBaseDirOverride
	}
	return tools.GetBackgroundOutputBaseDir()
}
