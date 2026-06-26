//go:build !js

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/history"
)

var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "Manage project history",
	Long: `Manage project revision and change history.

Subcommands:
  clear  Remove old revisions, changes, and runlogs`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var clearOlderThan string
var clearWorkspace string
var clearYes bool
var clearDryRun bool

var historyClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear old history (revisions, changes, runlogs)",
	Long: `Clear old revisions, change entries, and runlog files from project history.

By default, this removes ALL history. Use --older-than to keep recent entries.
Use --workspace to target a specific project directory.

When clearing all history (no --older-than), you must pass --yes or confirm
at the prompt. When using --older-than, no confirmation is needed.
Use --dry-run to see what would be cleared without deleting anything.

Examples:
  sprout history clear --yes                        # Clear ALL history
  sprout history clear --older-than 30d             # Clear history older than 30 days
  sprout history clear --older-than 7d --workspace /path/to/project  # Target specific project
  sprout history clear --dry-run                    # Show what would be cleared`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runHistoryClear()
	},
}

func init() {
	historyClearCmd.Flags().StringVar(&clearOlderThan, "older-than", "", "Duration threshold (e.g. 30d, 7d, 24h). Entries older than this are cleared. Empty means clear ALL.")
	historyClearCmd.Flags().StringVar(&clearWorkspace, "workspace", "", "Workspace path to clear history from (default: current directory)")
	historyClearCmd.Flags().BoolVarP(&clearYes, "yes", "y", false, "Skip confirmation prompt")
	historyClearCmd.Flags().BoolVar(&clearDryRun, "dry-run", false, "Show what would be cleared without deleting anything")
	// Keep --force as a hidden alias for backward compatibility
	historyClearCmd.Flags().BoolVar(&clearYes, "force", false, "shorthand for --yes")
	historyClearCmd.Flags().SetAnnotation("force", "cobra.bash_comp_hidden", []string{"true"})

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

	// 2. Check --yes requirement when clearing all history (unless --dry-run)
	if clearOlderThan == "" && !clearYes && !clearDryRun {
		if !StdinIsTerminal() {
			return fmt.Errorf("this command requires confirmation. Pass --yes to skip confirmation or run interactively")
		}
		if !ConfirmPrompt("This will clear ALL history for " + workspace + ". Continue") {
			return fmt.Errorf("aborted by user")
		}
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

	// 4. Handle --dry-run
	if clearDryRun {
		return runHistoryDryRun(workspace, since)
	}

	// 5. Clear changes and revisions via history package (pass workspace directly)
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

	// 6. Clear runlogs from the workspace
	runlogCleared, err := clearOldRunlogs(workspace, since, clearOlderThan == "")
	if err != nil {
		return fmt.Errorf("failed to clear runlogs: %w", err)
	}

	// 7. Check if there was any history at all
	if changesCleared == 0 && revisionsCleared == 0 && runlogCleared == 0 {
		fmt.Println("No history to clear.")
		return nil
	}

	// 8. Print summary
	if clearOlderThan != "" {
		fmt.Printf("Cleared %d revision(s), %d change(s), %d runlog(s) older than %s from %s\n",
			revisionsCleared, changesCleared, runlogCleared, clearOlderThan, workspace)
	} else {
		fmt.Printf("Cleared %d revision(s), %d change(s), %d runlog(s) from %s\n",
			revisionsCleared, changesCleared, runlogCleared, workspace)
	}

	return nil
}

// runHistoryDryRun counts what would be deleted without actually deleting anything.
func runHistoryDryRun(workspace string, since time.Time) error {
	changesCount, err := countChangeDirs(workspace, since)
	if err != nil {
		return fmt.Errorf("failed to count changes: %w", err)
	}

	revisionsCount, err := countRevisionDirs(workspace, since)
	if err != nil {
		return fmt.Errorf("failed to count revisions: %w", err)
	}

	runlogsCount, err := countRunlogs(workspace, since, clearOlderThan == "")
	if err != nil {
		return fmt.Errorf("failed to count runlogs: %w", err)
	}

	if clearOlderThan != "" {
		fmt.Printf("Would clear %d revision(s), %d change(s), %d runlog(s) older than %s from %s\n",
			revisionsCount, changesCount, runlogsCount, clearOlderThan, workspace)
	} else {
		fmt.Printf("Would clear %d revision(s), %d change(s), %d runlog(s) from %s\n",
			revisionsCount, changesCount, runlogsCount, workspace)
	}

	return nil
}

// countChangeDirs counts change subdirectories in .sprout/changes.
// If since is non-zero, only counts entries whose metadata timestamp is before since.
func countChangeDirs(workspace string, since time.Time) (int, error) {
	changesDir := filepath.Join(workspace, ".sprout", "changes")
	entries, err := os.ReadDir(changesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if since.IsZero() {
			count++
			continue
		}
		// Read metadata to check timestamp
		metadataPath := filepath.Join(changesDir, entry.Name(), "metadata.json")
		if history.IsChangeOlderThan(metadataPath, since) {
			count++
		}
	}
	return count, nil
}

// countRevisionDirs counts revision subdirectories in .sprout/revisions.
// If since is non-zero, only count revisions that would be orphaned
// (i.e., no remaining changes reference them after filtering out old changes).
// This mirrors the orphaning logic in history.ClearOlderThan().
func countRevisionDirs(workspace string, since time.Time) (int, error) {
	revisionsDir := filepath.Join(workspace, ".sprout", "revisions")
	entries, err := os.ReadDir(revisionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	if since.IsZero() {
		// Clear-all mode: count all revision dirs
		count := 0
		for _, entry := range entries {
			if entry.IsDir() {
				count++
			}
		}
		return count, nil
	}

	// --older-than mode: only count revisions that have no remaining (non-old) changes.
	// First, collect revision IDs that have at least one non-old change.
	changesDir := filepath.Join(workspace, ".sprout", "changes")
	changeEntries, err := os.ReadDir(changesDir)
	if err != nil && !os.IsNotExist(err) {
		return 0, err
	}

	keptRevisions := make(map[string]bool)
	for _, entry := range changeEntries {
		if !entry.IsDir() {
			continue
		}
		metadataPath := filepath.Join(changesDir, entry.Name(), "metadata.json")
		data, err := os.ReadFile(metadataPath)
		if err != nil {
			continue
		}
		var meta history.ChangeMetadata
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}
		// If this change is NOT old (timestamp >= since), its revision is kept.
		if !meta.Timestamp.Before(since) {
			keptRevisions[meta.RequestHash] = true
		}
	}

	// Count revision dirs that are NOT in the kept set.
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if !keptRevisions[entry.Name()] {
			count++
		}
	}
	return count, nil
}

// countRunlogs counts .jsonl files in .sprout/runlogs.
// If clearAll is true, all .jsonl files are counted.
// If clearAll is false and since is non-zero, only files modified before since are counted.
func countRunlogs(workspace string, since time.Time, clearAll bool) (int, error) {
	runlogsDir := filepath.Join(workspace, ".sprout", "runlogs")
	entries, err := os.ReadDir(runlogsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		if clearAll {
			count++
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(since) {
			count++
		}
	}
	return count, nil
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
	dur, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if dur < 0 {
		return 0, fmt.Errorf("duration must be non-negative")
	}
	return dur, nil
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
