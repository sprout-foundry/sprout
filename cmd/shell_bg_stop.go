//go:build !js

package cmd

// shell_bg_stop.go — the `sprout shell-bg stop` / `stop-all` subcommands:
// the stop flow (BPM kill + PID-file kill + file cleanup), the stop-all
// iteration, and their print helpers. Split out of shell_bg.go.
import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/automate"
	"github.com/sprout-foundry/sprout/pkg/console"
)

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
		console.GlyphWarning.Printf("Could not stop process %d: %v", pid, err)
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
		console.GlyphWarning.Printf("Could not stop session %s: %v", sessionID, err)
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
			console.GlyphWarning.Printf("Could not stop process %d: %v", pid, err)
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
