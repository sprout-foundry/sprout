//go:build !js

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/history"
	"github.com/spf13/cobra"
)

var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "Manage project history",
	Long: `Manage project revision and change history.

Subcommands:
  clear  Remove old revisions, changes, and runlogs`,
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

var clearOlderThan string
var clearWorkspace string
var clearForce bool

var historyClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear old history (revisions, changes, runlogs)",
	Long: `Clear old revisions, change entries, and runlog files from project history.

By default, this removes ALL history. Use --older-than to keep recent entries.
Use --workspace to target a specific project directory.

When clearing all history (no --older-than), you must pass --force to confirm.
When using --older-than, --force is not required.

Examples:
  sprout history clear --force                     # Clear ALL history
  sprout history clear --older-than 30d            # Clear history older than 30 days
  sprout history clear --older-than 7d --workspace /path/to/project  # Target specific project`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := runHistoryClear(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	historyClearCmd.Flags().StringVar(&clearOlderThan, "older-than", "", "Duration threshold (e.g. 30d, 7d, 24h). Entries older than this are cleared. Empty means clear ALL.")
	historyClearCmd.Flags().StringVar(&clearWorkspace, "workspace", "", "Workspace path to clear history from (default: current directory)")
	historyClearCmd.Flags().BoolVar(&clearForce, "force", false, "Required to clear ALL history (when --older-than is not set)")

	historyCmd.AddCommand(historyClearCmd)
}

// runHistoryClear implements the history clear subcommand.
func runHistoryClear() error {
	// 1. Resolve the workspace path
	workspace := clearWorkspace
	if workspace == "" {
		var err error
		workspace, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current working directory: %w", err)
		}
	}

	// 2. Check --force requirement when clearing all history
	if clearOlderThan == "" && !clearForce {
		return fmt.Errorf("use --force to clear all history, or use --older-than to clear only old entries")
	}

	// 3. Parse duration threshold
	var since time.Time
	if clearOlderThan != "" {
		duration, err := parseDuration(clearOlderThan)
		if err != nil {
			return fmt.Errorf("invalid --older-than value %q: %w", clearOlderThan, err)
		}
		since = time.Now().Add(-duration)
	}

	// 4. Clear changes and revisions via history package (pass workspace directly)
	var changesCleared, revisionsCleared int
	var err error
	if clearOlderThan != "" {
		changesCleared, revisionsCleared, err = history.ClearOlderThan(workspace, since)
	} else {
		changesCleared, revisionsCleared, err = history.ClearAll(workspace)
	}
	if err != nil {
		return fmt.Errorf("failed to clear history: %w", err)
	}

	// 5. Clear runlogs from the workspace
	runlogCleared, err := clearOldRunlogs(workspace, since, clearOlderThan == "")
	if err != nil {
		return fmt.Errorf("failed to clear runlogs: %w", err)
	}

	// 6. Check if there was any history at all
	if changesCleared == 0 && revisionsCleared == 0 && runlogCleared == 0 {
		fmt.Println("No history to clear.")
		return nil
	}

	// 7. Print summary
	if clearOlderThan != "" {
		fmt.Printf("Cleared %d revision(s), %d change(s), %d runlog(s) older than %s from %s\n",
			revisionsCleared, changesCleared, runlogCleared, clearOlderThan, workspace)
	} else {
		fmt.Printf("Cleared %d revision(s), %d change(s), %d runlog(s) from %s\n",
			revisionsCleared, changesCleared, runlogCleared, workspace)
	}

	return nil
}

// parseDuration converts a duration string like "30d", "7d", "24h" into a time.Duration.
// The "d" suffix is treated as days (24 hours each).
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration string")
	}

	// Handle day suffix ("d") by converting to hours
	if strings.HasSuffix(s, "d") {
		daysStr := strings.TrimSuffix(s, "d")
		days, err := strconv.Atoi(daysStr)
		if err != nil {
			return 0, fmt.Errorf("invalid day count %q: %w", daysStr, err)
		}
		if days < 0 {
			return 0, fmt.Errorf("day count must be non-negative")
		}
		return time.Duration(days*24) * time.Hour, nil
	}

	// Direct parse for h, m, s, etc.
	return time.ParseDuration(s)
}

// clearOldRunlogs removes runlog files from the workspace's .sprout/runlogs/ directory.
// If clearAll is true, all runlogs are removed regardless of age.
// Returns the number of runlogs cleared.
func clearOldRunlogs(workspace string, since time.Time, clearAll bool) (int, error) {
	runlogsDir := filepath.Join(workspace, ".sprout", "runlogs")

	// Check if the directory exists
	entries, err := os.ReadDir(runlogsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // No runlogs directory, nothing to clear
		}
		return 0, fmt.Errorf("failed to read runlogs directory %s: %w", runlogsDir, err)
	}

	var toDelete []os.DirEntry
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		if clearAll {
			toDelete = append(toDelete, entry)
			continue
		}

		// Check modification time
		info, err := entry.Info()
		if err != nil {
			continue // Skip files we can't stat
		}
		if info.ModTime().Before(since) {
			toDelete = append(toDelete, entry)
		}
	}

	if len(toDelete) == 0 {
		return 0, nil
	}

	// Sort for deterministic output
	sort.Slice(toDelete, func(i, j int) bool {
		return toDelete[i].Name() < toDelete[j].Name()
	})

	cleared := 0
	for _, entry := range toDelete {
		path := filepath.Join(runlogsDir, entry.Name())
		if err := os.Remove(path); err != nil {
			// Don't fail entirely on individual file errors, just skip
			fmt.Fprintf(os.Stderr, "Warning: failed to remove runlog %s: %v\n", entry.Name(), err)
			continue
		}
		cleared++
	}

	return cleared, nil
}
