//go:build !js

package cmd

// shell_bg_status.go — the `sprout shell-bg status` subcommand: status
// display for a named session (BPM + PID-file sources) and its print
// helpers. Split out of shell_bg.go.
import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/automate"
)

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
