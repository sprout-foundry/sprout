//go:build !js

package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/spf13/cobra"
)

// defaultAuditLogPath returns the default path for the shell audit log file
// (~/.sprout/shell-audit.jsonl).
func defaultAuditLogPath() string {
	dir, err := configuration.GetConfigDir()
	if err != nil {
		// Fallback: use home directory directly.
		home, _ := os.UserHomeDir()
		return filepath.Join(home, configuration.ConfigDirName, "shell-audit.jsonl")
	}
	return filepath.Join(dir, "shell-audit.jsonl")
}

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "View or clear the security audit log",
	Long: `Inspect and manage the shell security audit log.

The audit log (~/.sprout/shell-audit.jsonl) records every non-SAFE shell
classification decision, including the command (with secrets redacted),
risk level, outcome (blocked/approved/prompted), and source annotation.

Use 'audit tail' to review recent decisions and 'audit clear' to wipe the log.`,
}

var auditTailCmd = &cobra.Command{
	Use:   "tail [--lines=N]",
	Short: "Print recent audit log decisions (human-formatted)",
	Long: `Print the most recent decisions from the security audit log.

Each line shows the timestamp, tool, risk level, outcome, source, and a
(redacted) command snippet. Secrets in commands are already redacted in the
log file itself.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		lines, _ := cmd.Flags().GetInt("lines")
		if lines <= 0 {
			lines = 20
		}

		logPath := defaultAuditLogPath()

		entries, err := readAuditLogTail(logPath, lines)
		if err != nil {
			return fmt.Errorf("failed to read audit log: %w", err)
		}

		if len(entries) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No audit log entries found.")
			return nil
		}

		out := cmd.OutOrStdout()
		for _, e := range entries {
			fmt.Fprintln(out, formatAuditEntry(e))
		}

		return nil
	},
}

var auditClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Wipe the security audit log file",
	Long: `Delete all entries from the security audit log.

This removes ~/.sprout/shell-audit.jsonl and any rotated file (.jsonl.1).
This action cannot be undone.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		force, _ := cmd.Flags().GetBool("force")
		if !force {
			fmt.Fprint(cmd.OutOrStdout(), "This will permanently delete the audit log. Continue? [y/N] ")
			reader := bufio.NewReader(os.Stdin)
			response, _ := reader.ReadString('\n')
			response = strings.ToLower(strings.TrimSpace(response))
			if response != "y" && response != "yes" {
				fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
				return nil
			}
		}

		logPath := defaultAuditLogPath()
		rotatedPath := logPath + ".1"
		removed := 0

		// Remove the main log file.
		if err := os.Remove(logPath); err != nil {
			if !os.IsNotExist(err) {
				return fmt.Errorf("failed to remove audit log: %w", err)
			}
		} else {
			removed++
		}

		// Remove any rotated file.
		if err := os.Remove(rotatedPath); err != nil {
			if !os.IsNotExist(err) {
				return fmt.Errorf("failed to remove rotated audit log: %w", err)
			}
		} else {
			removed++
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Cleared %d audit log file(s).\n", removed)
		return nil
	},
}

// readAuditLogTail reads the last N entries from the audit log file. If the
// main file has fewer than N entries, it also reads from the rotated .1 file.
func readAuditLogTail(logPath string, n int) ([]tools.AuditEntry, error) {
	entries, err := readAuditLogFile(logPath, n)
	if err != nil {
		return nil, err
	}

	// If we don't have enough entries, try the rotated file.
	if len(entries) < n {
		rotatedPath := logPath + ".1"
		rotated, err := readAuditLogFile(rotatedPath, n-len(entries))
		if err == nil && len(rotated) > 0 {
			// Prepend rotated entries (they're older).
			entries = append(rotated, entries...)
		}
	}

	return entries, nil
}

// readAuditLogFile reads up to the last N entries from a single JSONL file.
func readAuditLogFile(path string, n int) ([]tools.AuditEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No file = no entries.
		}
		return nil, err
	}
	defer f.Close()

	// Read all lines, keep the last N. For very large files this is
	// inefficient, but audit logs rotate at 10MB so this is acceptable.
	var allEntries []tools.AuditEntry
	scanner := bufio.NewScanner(f)
	// Increase buffer size for large entries (default 64KB).
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry tools.AuditEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			// Skip malformed lines rather than failing entirely.
			continue
		}
		allEntries = append(allEntries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan audit log: %w", err)
	}

	// Return the last N entries.
	if len(allEntries) > n {
		allEntries = allEntries[len(allEntries)-n:]
	}
	return allEntries, nil
}

// formatAuditEntry formats an audit entry for human-readable display.
func formatAuditEntry(e tools.AuditEntry) string {
	ts := ""
	if !e.Timestamp.IsZero() {
		ts = e.Timestamp.Format("2006-01-02 15:04:05")
	}

	risk := e.RiskLevel
	if risk == "" {
		risk = "?"
	}

	outcome := e.Action
	if outcome == "" {
		outcome = "-"
	}

	source := e.Source
	if source == "" {
		source = "-"
	}

	cmd := e.Args
	if len(cmd) > 80 {
		cmd = cmd[:77] + "..."
	}

	return fmt.Sprintf("[%s] %s | %-9s | %-8s | %-22s | %s",
		ts, e.Tool, risk, outcome, source, cmd)
}

func init() {
	auditTailCmd.Flags().IntP("lines", "n", 20, "number of recent entries to show")
	auditClearCmd.Flags().BoolP("force", "f", false, "skip confirmation prompt")

	auditCmd.AddCommand(auditTailCmd)
	auditCmd.AddCommand(auditClearCmd)
	rootCmd.AddCommand(auditCmd)
}
