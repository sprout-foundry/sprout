//go:build !js

package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/history"
	"github.com/sprout-foundry/sprout/pkg/testutil"
)

// ---------------------------------------------------------------------------
// parseDuration
// ---------------------------------------------------------------------------

func TestParseDuration_Days(t *testing.T) {
	tests := []struct {
		input     string
		wantHours int
		wantErr   bool
	}{
		{"30d", 720, false},
		{"7d", 168, false},
		{"1d", 24, false},
		{"0d", 0, false},
		{" 7d ", 168, false}, // trimmed
		{"100d", 2400, false},
		{"-1d", 0, true}, // negative
		{"ad", 0, true},  // non-numeric
		{"", 0, true},    // empty
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseDuration(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("parseDuration(%q) expected error, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Errorf("parseDuration(%q) unexpected error: %v", tc.input, err)
				return
			}
			want := time.Duration(tc.wantHours) * time.Hour
			if got != want {
				t.Errorf("parseDuration(%q) = %v, want %v", tc.input, got, want)
			}
		})
	}
}

func TestParseDuration_StandardDurations(t *testing.T) {
	tests := []struct {
		input     string
		wantHours int
		wantErr   bool
	}{
		{"24h", 24, false},
		{"1h", 1, false},
		{"120m", 2, false},
		{"0h", 0, false},
		{"48h", 48, false},
		{"abc", 0, true}, // unparseable
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseDuration(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("parseDuration(%q) expected error, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Errorf("parseDuration(%q) unexpected error: %v", tc.input, err)
				return
			}
			want := time.Duration(tc.wantHours) * time.Hour
			if got != want {
				t.Errorf("parseDuration(%q) = %v, want %v", tc.input, got, want)
			}
		})
	}
}

func TestParseDuration_ExactSeconds(t *testing.T) {
	got, err := parseDuration("1800s")
	if err != nil {
		t.Fatalf("parseDuration(\"1800s\") unexpected error: %v", err)
	}
	want := 30 * time.Minute
	if got != want {
		t.Errorf("parseDuration(\"1800s\") = %v, want %v", got, want)
	}
}

func TestParseDuration_ExactMinutes(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"120m", 2 * time.Hour},
		{"60m", 1 * time.Hour},
		{"30m", 30 * time.Minute},
		{"0m", 0},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseDuration(tc.input)
			if err != nil {
				t.Fatalf("parseDuration(%q) unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("parseDuration(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// TestParseDuration_NegativeDuration verifies that parseDuration rejects
// negative durations (e.g. "-1h").
func TestParseDuration_NegativeDuration(t *testing.T) {
	_, err := parseDuration("-1h")
	if err == nil {
		t.Fatal("parseDuration(\"-1h\") expected error, got nil")
	}
	if !strings.Contains(err.Error(), "duration must be non-negative") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// fileHash computes a SHA256 hash from "filename:code" for creating change dirs.
func fileHash(filename, code string) string {
	data := []byte(filename + ":" + code)
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// setupTestWorkspace creates a temp workspace with .sprout/changes, .sprout/revisions,
// .sprout/runlogs directories. The runHistoryClear function will chdir to this
// workspace before calling history.ClearAll/ClearOlderThan, so the relative
// paths in the history package will resolve correctly.
func setupTestWorkspace(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()

	for _, dir := range []string{
		filepath.Join(workspace, ".sprout", "changes"),
		filepath.Join(workspace, ".sprout", "revisions"),
		filepath.Join(workspace, ".sprout", "runlogs"),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("failed to create dir %s: %v", dir, err)
		}
	}
	return workspace
}

// createChange creates a change entry in the history package's configured dirs.
// It writes metadata.json and the .original/.updated base64-encoded code files.
func createChange(t *testing.T, workspace, revisionID, filename string, timestamp time.Time, originalCode, newCode string) string {
	t.Helper()
	hash := fileHash(filename, newCode)
	changeDir := filepath.Join(workspace, ".sprout", "changes", hash)
	if err := os.MkdirAll(changeDir, 0755); err != nil {
		t.Fatalf("failed to create change dir: %v", err)
	}

	metadata := history.ChangeMetadata{
		Version:          1,
		Filename:         filename,
		FileRevisionHash: hash,
		RequestHash:      revisionID,
		Timestamp:        timestamp,
		Status:           "active",
		Description:      "test change",
	}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(changeDir, "metadata.json"), data, 0644); err != nil {
		t.Fatalf("failed to write metadata: %v", err)
	}
	return hash
}

// createRevision creates a revision directory with instructions and response files.
func createRevision(t *testing.T, workspace, revisionID string) {
	t.Helper()
	revDir := filepath.Join(workspace, ".sprout", "revisions", revisionID)
	if err := os.MkdirAll(revDir, 0755); err != nil {
		t.Fatalf("failed to create revision dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(revDir, "instructions.txt"), []byte("instructions"), 0644); err != nil {
		t.Fatalf("failed to write instructions: %v", err)
	}
	if err := os.WriteFile(filepath.Join(revDir, "llm_response.txt"), []byte("response"), 0644); err != nil {
		t.Fatalf("failed to write response: %v", err)
	}
}

// createRunlog creates a .jsonl runlog file with optional modification time.
func createRunlog(t *testing.T, workspace, name string, modTime time.Time) {
	t.Helper()
	runlogsDir := filepath.Join(workspace, ".sprout", "runlogs")
	path := filepath.Join(runlogsDir, name)
	if err := os.WriteFile(path, []byte(`{"msg":"test"}`), 0644); err != nil {
		t.Fatalf("failed to write runlog: %v", err)
	}
	if !modTime.IsZero() {
		os.Chtimes(path, modTime, modTime)
	}
}

// countDirEntries counts subdirectories in a directory.
func countDirEntries(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("failed to read dir %s: %v", dir, err)
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			count++
		}
	}
	return count
}

// countFiles counts regular files in a directory.
func countFiles(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("failed to read dir %s: %v", dir, err)
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			count++
		}
	}
	return count
}

// resetClearFlags resets the global flag variables to defaults.
func resetClearFlags() {
	clearOlderThan = ""
	clearWorkspace = ""
	clearYes = false
	clearDryRun = false
}

// ---------------------------------------------------------------------------
// pipeStdin — redirect stdin to a pipe with the given input
// ---------------------------------------------------------------------------

// pipeStdin replaces os.Stdin with a pipe that sends the given input.
// It restores the original stdin when the returned cleanup function is called.
func pipeStdin(t *testing.T, input string) func() {
	t.Helper()
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdin = r
	go func() {
		defer w.Close()
		w.WriteString(input)
	}()
	return func() {
		os.Stdin = oldStdin
		r.Close()
	}
}

// ---------------------------------------------------------------------------
// clearOldRunlogs
// ---------------------------------------------------------------------------

func TestClearOldRunlogs_NoDirectory(t *testing.T) {
	workspace := t.TempDir()
	// No .sprout/runlogs directory exists
	count, err := clearOldRunlogs(workspace, time.Now(), true)
	if err != nil {
		t.Fatalf("clearOldRunlogs() unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("clearOldRunlogs() = %d, want 0", count)
	}
}

func TestClearOldRunlogs_ClearAll(t *testing.T) {
	workspace := setupTestWorkspace(t)

	now := time.Now()
	createRunlog(t, workspace, "run1.jsonl", now)
	createRunlog(t, workspace, "run2.jsonl", now)
	createRunlog(t, workspace, "run3.jsonl", now)

	count, err := clearOldRunlogs(workspace, time.Time{}, true)
	if err != nil {
		t.Fatalf("clearOldRunlogs() error = %v", err)
	}
	if count != 3 {
		t.Errorf("clearOldRunlogs() = %d, want 3", count)
	}

	// Verify files are gone
	remaining := countFiles(t, filepath.Join(workspace, ".sprout", "runlogs"))
	if remaining != 0 {
		t.Errorf("expected 0 remaining runlog files, got %d", remaining)
	}
}

func TestClearOldRunlogs_OnlyRemovesJSONL(t *testing.T) {
	workspace := setupTestWorkspace(t)

	createRunlog(t, workspace, "run1.jsonl", time.Now())
	// Create a non-.jsonl file that should NOT be removed
	runlogsDir := filepath.Join(workspace, ".sprout", "runlogs")
	os.WriteFile(filepath.Join(runlogsDir, "notes.txt"), []byte("not a runlog"), 0644)

	count, err := clearOldRunlogs(workspace, time.Time{}, true)
	if err != nil {
		t.Fatalf("clearOldRunlogs() error = %v", err)
	}
	if count != 1 {
		t.Errorf("clearOldRunlogs() = %d, want 1 (only .jsonl)", count)
	}

	// notes.txt should still exist
	if _, err := os.Stat(filepath.Join(runlogsDir, "notes.txt")); os.IsNotExist(err) {
		t.Error("notes.txt should not have been removed")
	}
}

func TestClearOldRunlogs_OlderThan(t *testing.T) {
	workspace := setupTestWorkspace(t)

	cutoff := time.Now().Add(-7 * 24 * time.Hour) // 7 days ago
	oldTime := cutoff.Add(-24 * time.Hour)        // 8 days ago (should be cleared)
	recentTime := cutoff.Add(24 * time.Hour)      // 6 days ago (should be kept)

	createRunlog(t, workspace, "old.jsonl", oldTime)
	createRunlog(t, workspace, "recent.jsonl", recentTime)
	createRunlog(t, workspace, "very-old.jsonl", oldTime.Add(-24*time.Hour))

	count, err := clearOldRunlogs(workspace, cutoff, false)
	if err != nil {
		t.Fatalf("clearOldRunlogs() error = %v", err)
	}
	if count != 2 {
		t.Errorf("clearOldRunlogs() = %d, want 2 (two old files)", count)
	}

	// recent.jsonl should still exist
	if _, err := os.Stat(filepath.Join(workspace, ".sprout", "runlogs", "recent.jsonl")); os.IsNotExist(err) {
		t.Error("recent.jsonl should still exist")
	}
}

func TestClearOldRunlogs_IgnoresSubdirectories(t *testing.T) {
	workspace := setupTestWorkspace(t)

	runlogsDir := filepath.Join(workspace, ".sprout", "runlogs")
	os.MkdirAll(filepath.Join(runlogsDir, "subdir"), 0755)
	createRunlog(t, workspace, "run1.jsonl", time.Now())

	count, err := clearOldRunlogs(workspace, time.Time{}, true)
	if err != nil {
		t.Fatalf("clearOldRunlogs() error = %v", err)
	}
	if count != 1 {
		t.Errorf("clearOldRunlogs() = %d, want 1 (subdir should be ignored)", count)
	}

	// Subdir should still exist
	if _, err := os.Stat(filepath.Join(runlogsDir, "subdir")); os.IsNotExist(err) {
		t.Error("subdir should still exist")
	}
}

// ---------------------------------------------------------------------------
// runHistoryClear — integration tests
// ---------------------------------------------------------------------------

func TestRunHistoryClear_ClearAll(t *testing.T) {
	workspace := setupTestWorkspace(t)

	// Save and restore working directory
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)

	// Save and restore flags
	defer resetClearFlags()

	// Create test data
	createRevision(t, workspace, "rev-1")
	createChange(t, workspace, "rev-1", "file1.go", time.Now().Add(-30*24*time.Hour), "old1", "new1")
	createChange(t, workspace, "rev-1", "file2.go", time.Now().Add(-30*24*time.Hour), "old2", "new2")
	createRevision(t, workspace, "rev-2")
	createChange(t, workspace, "rev-2", "file3.go", time.Now().Add(-30*24*time.Hour), "old3", "new3")
	createRunlog(t, workspace, "old-run.jsonl", time.Now().Add(-30*24*time.Hour))

	changesBefore := countDirEntries(t, filepath.Join(workspace, ".sprout", "changes"))
	revisionsBefore := countDirEntries(t, filepath.Join(workspace, ".sprout", "revisions"))
	runlogsBefore := countFiles(t, filepath.Join(workspace, ".sprout", "runlogs"))

	if changesBefore != 3 {
		t.Fatalf("expected 3 change dirs before clear, got %d", changesBefore)
	}
	if revisionsBefore != 2 {
		t.Fatalf("expected 2 revision dirs before clear, got %d", revisionsBefore)
	}
	if runlogsBefore != 1 {
		t.Fatalf("expected 1 runlog before clear, got %d", runlogsBefore)
	}

	// Set flags and run
	clearOlderThan = ""
	clearWorkspace = workspace
	clearYes = true

	if err := runHistoryClear(); err != nil {
		t.Fatalf("runHistoryClear() error = %v", err)
	}

	// Verify everything is cleared
	changesAfter := countDirEntries(t, filepath.Join(workspace, ".sprout", "changes"))
	revisionsAfter := countDirEntries(t, filepath.Join(workspace, ".sprout", "revisions"))
	runlogsAfter := countFiles(t, filepath.Join(workspace, ".sprout", "runlogs"))

	if changesAfter != 0 {
		t.Errorf("expected 0 change dirs after clear, got %d", changesAfter)
	}
	if revisionsAfter != 0 {
		t.Errorf("expected 0 revision dirs after clear, got %d", revisionsAfter)
	}
	if runlogsAfter != 0 {
		t.Errorf("expected 0 runlogs after clear, got %d", runlogsAfter)
	}
}

func TestRunHistoryClear_OlderThan(t *testing.T) {
	workspace := setupTestWorkspace(t)

	// Save and restore working directory
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)

	// Save and restore flags
	defer resetClearFlags()

	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	oldTime := cutoff.Add(-24 * time.Hour)   // 8 days ago
	recentTime := cutoff.Add(24 * time.Hour) // 6 days ago

	// Create old data
	createRevision(t, workspace, "old-rev")
	createChange(t, workspace, "old-rev", "old-file.go", oldTime, "old", "new")
	createRunlog(t, workspace, "old-run.jsonl", oldTime)

	// Create recent data
	createRevision(t, workspace, "recent-rev")
	createChange(t, workspace, "recent-rev", "recent-file.go", recentTime, "old", "new")
	createRunlog(t, workspace, "recent-run.jsonl", recentTime)

	// Set flags and run
	clearOlderThan = "7d"
	clearWorkspace = workspace

	if err := runHistoryClear(); err != nil {
		t.Fatalf("runHistoryClear() error = %v", err)
	}

	// Old change should be gone
	changesAfter := countDirEntries(t, filepath.Join(workspace, ".sprout", "changes"))
	if changesAfter != 1 {
		t.Errorf("expected 1 change dir after older-than clear, got %d", changesAfter)
	}

	// Old revision should be gone (orphaned), recent should remain
	revisionsAfter := countDirEntries(t, filepath.Join(workspace, ".sprout", "revisions"))
	if revisionsAfter != 1 {
		t.Errorf("expected 1 revision dir after older-than clear, got %d", revisionsAfter)
	}

	// Old runlog should be gone, recent should remain
	runlogsAfter := countFiles(t, filepath.Join(workspace, ".sprout", "runlogs"))
	if runlogsAfter != 1 {
		t.Errorf("expected 1 runlog after older-than clear, got %d", runlogsAfter)
	}

	// Verify the remaining runlog is the recent one
	if _, err := os.Stat(filepath.Join(workspace, ".sprout", "runlogs", "recent-run.jsonl")); os.IsNotExist(err) {
		t.Error("recent-run.jsonl should still exist")
	}
}

func TestRunHistoryClear_RequiresYes(t *testing.T) {
	workspace := setupTestWorkspace(t)

	defer resetClearFlags()

	// No --yes and no --older-than should fail because stdin is not a TTY
	// (StdinIsTerminal() returns false in tests).
	clearOlderThan = ""
	clearWorkspace = workspace
	clearYes = false

	err := runHistoryClear()
	if err == nil {
		t.Fatal("expected error when --yes is not provided and --older-than is not set, got nil")
	}
	if !strings.Contains(err.Error(), "this command requires confirmation") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRunHistoryClear_NoHistory(t *testing.T) {
	workspace := setupTestWorkspace(t)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	defer resetClearFlags()

	// Directories exist but are empty
	clearOlderThan = ""
	clearWorkspace = workspace
	clearYes = true

	// Should not error, just print "No history to clear."
	if err := runHistoryClear(); err != nil {
		t.Fatalf("runHistoryClear() error = %v", err)
	}
}

func TestRunHistoryClear_NonExistentWorkspace(t *testing.T) {
	defer resetClearFlags()

	clearOlderThan = ""
	clearWorkspace = "/tmp/nonexistent-workspace-" + filepath.Base(os.TempDir())
	clearYes = true

	// Non-existent workspace is handled gracefully (no .sprout dirs exist)
	if err := runHistoryClear(); err != nil {
		t.Fatalf("runHistoryClear() should not error on non-existent workspace: %v", err)
	}
}

func TestRunHistoryClear_InvalidOlderThan(t *testing.T) {
	workspace := setupTestWorkspace(t)

	defer resetClearFlags()

	clearOlderThan = "invalid"
	clearWorkspace = workspace

	err := runHistoryClear()
	if err == nil {
		t.Fatal("expected error for invalid --older-than, got nil")
	}
	if !strings.Contains(err.Error(), "invalid --older-than") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRunHistoryClear_NonExistentHistoryDirs(t *testing.T) {
	workspace := t.TempDir()

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	defer resetClearFlags()

	// Don't create any .sprout dirs — runHistoryClear should handle this gracefully
	clearOlderThan = ""
	clearWorkspace = workspace
	clearYes = true

	if err := runHistoryClear(); err != nil {
		t.Fatalf("runHistoryClear() should not error on non-existent history dirs: %v", err)
	}
}

func TestRunHistoryClear_UsingCurrentDir(t *testing.T) {
	workspace := setupTestWorkspace(t)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	defer resetClearFlags()

	// Change into the workspace
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	// Create test data
	createRevision(t, workspace, "rev-1")
	createChange(t, workspace, "rev-1", "file1.go", time.Now(), "old", "new")
	createRunlog(t, workspace, "run.jsonl", time.Now())

	// Run without --workspace (should use current dir)
	clearOlderThan = ""
	clearWorkspace = ""
	clearYes = true

	if err := runHistoryClear(); err != nil {
		t.Fatalf("runHistoryClear() error = %v", err)
	}

	// Everything should be cleared
	changesAfter := countDirEntries(t, filepath.Join(workspace, ".sprout", "changes"))
	revisionsAfter := countDirEntries(t, filepath.Join(workspace, ".sprout", "revisions"))
	runlogsAfter := countFiles(t, filepath.Join(workspace, ".sprout", "runlogs"))

	if changesAfter != 0 {
		t.Errorf("expected 0 changes, got %d", changesAfter)
	}
	if revisionsAfter != 0 {
		t.Errorf("expected 0 revisions, got %d", revisionsAfter)
	}
	if runlogsAfter != 0 {
		t.Errorf("expected 0 runlogs, got %d", runlogsAfter)
	}
}

func TestRunHistoryClear_OlderThan_Hours(t *testing.T) {
	workspace := setupTestWorkspace(t)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	defer resetClearFlags()

	cutoff := time.Now().Add(-24 * time.Hour)
	oldTime := cutoff.Add(-1 * time.Hour)
	recentTime := cutoff.Add(1 * time.Hour)

	createRevision(t, workspace, "old-rev")
	createChange(t, workspace, "old-rev", "old.go", oldTime, "old", "new")
	createRevision(t, workspace, "recent-rev")
	createChange(t, workspace, "recent-rev", "recent.go", recentTime, "old", "new")

	clearOlderThan = "24h"
	clearWorkspace = workspace

	if err := runHistoryClear(); err != nil {
		t.Fatalf("runHistoryClear() error = %v", err)
	}

	changesAfter := countDirEntries(t, filepath.Join(workspace, ".sprout", "changes"))
	if changesAfter != 1 {
		t.Errorf("expected 1 change dir after 24h clear, got %d", changesAfter)
	}
}

func TestRunHistoryClear_Revisions_ClearedAsOrphans(t *testing.T) {
	workspace := setupTestWorkspace(t)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	defer resetClearFlags()

	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	oldTime := cutoff.Add(-24 * time.Hour)

	// Create an old revision with an old change
	createRevision(t, workspace, "old-rev")
	createChange(t, workspace, "old-rev", "old.go", oldTime, "old", "new")

	// Create a revision with NO changes (orphaned from the start)
	createRevision(t, workspace, "orphan-rev")

	changesBefore := countDirEntries(t, filepath.Join(workspace, ".sprout", "changes"))
	revisionsBefore := countDirEntries(t, filepath.Join(workspace, ".sprout", "revisions"))
	if changesBefore != 1 {
		t.Fatalf("expected 1 change dir before, got %d", changesBefore)
	}
	if revisionsBefore != 2 {
		t.Fatalf("expected 2 revision dirs before, got %d", revisionsBefore)
	}

	clearOlderThan = "7d"
	clearWorkspace = workspace

	if err := runHistoryClear(); err != nil {
		t.Fatalf("runHistoryClear() error = %v", err)
	}

	// Old change gone, old-rev is orphaned, orphan-rev was always orphaned — both should be cleared
	revisionsAfter := countDirEntries(t, filepath.Join(workspace, ".sprout", "revisions"))
	if revisionsAfter != 0 {
		t.Errorf("expected 0 revision dirs after clear (orphans should be removed), got %d", revisionsAfter)
	}
}

// ---------------------------------------------------------------------------
// historyClearCmd flag validation
// ---------------------------------------------------------------------------

func TestHistoryClearCmd_FlagsRegistered(t *testing.T) {
	// Verify the --older-than flag is registered
	flag := historyClearCmd.Flags().Lookup("older-than")
	if flag == nil {
		t.Fatal("expected --older-than flag to be registered")
	}
	if flag.Usage != "Duration threshold (e.g. 30d, 7d, 24h). Entries older than this are cleared. Empty means clear ALL." {
		t.Errorf("unexpected --older-than usage: %s", flag.Usage)
	}

	// Verify the --workspace flag is registered
	flag = historyClearCmd.Flags().Lookup("workspace")
	if flag == nil {
		t.Fatal("expected --workspace flag to be registered")
	}
	if flag.Usage != "Workspace path to clear history from (default: current directory)" {
		t.Errorf("unexpected --workspace usage: %s", flag.Usage)
	}
}

func TestHistoryCmd_Subcommands(t *testing.T) {
	// Verify historyCmd has the clear subcommand
	found := false
	for _, sub := range historyCmd.Commands() {
		if sub.Use == "clear" {
			found = true
			break
		}
	}
	if !found {
		t.Error("historyCmd should have a 'clear' subcommand")
	}
}

// ---------------------------------------------------------------------------
// Dry-run tests
// ---------------------------------------------------------------------------

func TestClearDryRun_ClearAll(t *testing.T) {
	workspace := setupTestWorkspace(t)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	defer resetClearFlags()

	// Create test data
	createRevision(t, workspace, "rev-1")
	createChange(t, workspace, "rev-1", "file1.go", time.Now(), "old", "new")
	createChange(t, workspace, "rev-1", "file2.go", time.Now(), "old", "new")
	createRunlog(t, workspace, "run1.jsonl", time.Now())
	createRunlog(t, workspace, "run2.jsonl", time.Now())

	// Set dry-run flags
	clearOlderThan = ""
	clearWorkspace = workspace
	clearDryRun = true

	out := testutil.CaptureStdout(t, func() {
		if err := runHistoryClear(); err != nil {
			t.Fatalf("runHistoryClear() error = %v", err)
		}
	})

	// Should say "Would clear" not "Cleared"
	if !strings.Contains(out, "Would clear") {
		t.Errorf("expected output to contain 'Would clear', got: %q", out)
	}
	if strings.Contains(out, "Cleared") {
		t.Errorf("output should not contain 'Cleared' in dry-run mode, got: %q", out)
	}

	// Verify nothing was actually deleted
	changesAfter := countDirEntries(t, filepath.Join(workspace, ".sprout", "changes"))
	revisionsAfter := countDirEntries(t, filepath.Join(workspace, ".sprout", "revisions"))
	runlogsAfter := countFiles(t, filepath.Join(workspace, ".sprout", "runlogs"))

	if changesAfter != 2 {
		t.Errorf("expected 2 change dirs after dry-run, got %d", changesAfter)
	}
	if revisionsAfter != 1 {
		t.Errorf("expected 1 revision dir after dry-run, got %d", revisionsAfter)
	}
	if runlogsAfter != 2 {
		t.Errorf("expected 2 runlogs after dry-run, got %d", runlogsAfter)
	}
}

func TestClearDryRun_OlderThan(t *testing.T) {
	workspace := setupTestWorkspace(t)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	defer resetClearFlags()

	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	oldTime := cutoff.Add(-24 * time.Hour)
	recentTime := cutoff.Add(24 * time.Hour)

	// Create old and recent data
	createRevision(t, workspace, "old-rev")
	createChange(t, workspace, "old-rev", "old.go", oldTime, "old", "new")
	createRunlog(t, workspace, "old-run.jsonl", oldTime)

	createRevision(t, workspace, "recent-rev")
	createChange(t, workspace, "recent-rev", "recent.go", recentTime, "old", "new")
	createRunlog(t, workspace, "recent-run.jsonl", recentTime)

	// Set dry-run with --older-than
	clearOlderThan = "7d"
	clearWorkspace = workspace
	clearDryRun = true

	out := testutil.CaptureStdout(t, func() {
		if err := runHistoryClear(); err != nil {
			t.Fatalf("runHistoryClear() error = %v", err)
		}
	})

	// Should say "Would clear" with older-than message
	if !strings.Contains(out, "Would clear") {
		t.Errorf("expected output to contain 'Would clear', got: %q", out)
	}
	if !strings.Contains(out, "older than 7d") {
		t.Errorf("expected output to contain 'older than 7d', got: %q", out)
	}

	// Verify nothing was actually deleted
	changesAfter := countDirEntries(t, filepath.Join(workspace, ".sprout", "changes"))
	revisionsAfter := countDirEntries(t, filepath.Join(workspace, ".sprout", "revisions"))
	runlogsAfter := countFiles(t, filepath.Join(workspace, ".sprout", "runlogs"))

	// Everything should still be there
	if changesAfter != 2 {
		t.Errorf("expected 2 change dirs after dry-run, got %d", changesAfter)
	}
	if revisionsAfter != 2 {
		t.Errorf("expected 2 revision dirs after dry-run, got %d", revisionsAfter)
	}
	if runlogsAfter != 2 {
		t.Errorf("expected 2 runlogs after dry-run, got %d", runlogsAfter)
	}
}

func TestClearDryRun_NoHistory(t *testing.T) {
	workspace := setupTestWorkspace(t)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	defer resetClearFlags()

	clearOlderThan = ""
	clearWorkspace = workspace
	clearDryRun = true

	out := testutil.CaptureStdout(t, func() {
		if err := runHistoryClear(); err != nil {
			t.Fatalf("runHistoryClear() error = %v", err)
		}
	})

	if !strings.Contains(out, "Would clear 0 revision(s)") {
		t.Errorf("expected output to contain 'Would clear 0 revision(s)', got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// ConfirmPrompt tests
// ---------------------------------------------------------------------------

func TestConfirmPrompt_AcceptsY(t *testing.T) {
	cleanup := pipeStdin(t, "y\n")
	defer cleanup()

	if !ConfirmPrompt("Continue?") {
		t.Error("ConfirmPrompt should return true for 'y'")
	}
}

func TestConfirmPrompt_AcceptsYes(t *testing.T) {
	cleanup := pipeStdin(t, "yes\n")
	defer cleanup()

	if !ConfirmPrompt("Continue?") {
		t.Error("ConfirmPrompt should return true for 'yes'")
	}
}

func TestConfirmPrompt_RejectsN(t *testing.T) {
	cleanup := pipeStdin(t, "n\n")
	defer cleanup()

	if ConfirmPrompt("Continue?") {
		t.Error("ConfirmPrompt should return false for 'n'")
	}
}

func TestConfirmPrompt_RejectsNo(t *testing.T) {
	cleanup := pipeStdin(t, "no\n")
	defer cleanup()

	if ConfirmPrompt("Continue?") {
		t.Error("ConfirmPrompt should return false for 'no'")
	}
}

func TestConfirmPrompt_RejectsEmpty(t *testing.T) {
	cleanup := pipeStdin(t, "\n")
	defer cleanup()

	if ConfirmPrompt("Continue?") {
		t.Error("ConfirmPrompt should return false for empty input")
	}
}

func TestConfirmPrompt_RejectsRandom(t *testing.T) {
	cleanup := pipeStdin(t, "abc\n")
	defer cleanup()

	if ConfirmPrompt("Continue?") {
		t.Error("ConfirmPrompt should return false for random input")
	}
}

func TestConfirmPrompt_CaseInsensitive(t *testing.T) {
	tests := []struct {
		input  string
		expect bool
	}{
		{"Y\n", true},
		{"YEs\n", true},
		{"YES\n", true},
		{"yES\n", true},
		{"N\n", false},
		{"NO\n", false},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			cleanup := pipeStdin(t, tc.input)
			defer cleanup()

			got := ConfirmPrompt("Continue?")
			if got != tc.expect {
				t.Errorf("ConfirmPrompt(%q) = %v, want %v", tc.input, got, tc.expect)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Dry-run skips confirmation
// ---------------------------------------------------------------------------

func TestRunHistoryClear_DryRun_NoConfirm(t *testing.T) {
	workspace := setupTestWorkspace(t)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	defer resetClearFlags()

	// Create test data
	createRevision(t, workspace, "rev-1")
	createChange(t, workspace, "rev-1", "file1.go", time.Now(), "old", "new")
	createRunlog(t, workspace, "run.jsonl", time.Now())

	// Set dry-run WITHOUT --yes. Should NOT prompt for confirmation.
	clearOlderThan = ""
	clearWorkspace = workspace
	clearDryRun = true
	clearYes = false

	out := testutil.CaptureStdout(t, func() {
		if err := runHistoryClear(); err != nil {
			t.Fatalf("runHistoryClear() error = %v", err)
		}
	})

	// Should NOT contain a confirmation prompt
	if strings.Contains(out, "[y/N]") {
		t.Errorf("dry-run should not show confirmation prompt, got: %q", out)
	}

	// Should show "Would clear" output
	if !strings.Contains(out, "Would clear") {
		t.Errorf("expected output to contain 'Would clear', got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// --force alias for --yes
// ---------------------------------------------------------------------------

func TestRunHistoryClear_ForceAlias(t *testing.T) {
	workspace := setupTestWorkspace(t)

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	defer resetClearFlags()

	// Create test data
	createRevision(t, workspace, "rev-1")
	createChange(t, workspace, "rev-1", "file1.go", time.Now(), "old", "new")
	createRunlog(t, workspace, "run.jsonl", time.Now())

	// Setting clearYes=true is how the --force flag works (both bound to the same var)
	clearOlderThan = ""
	clearWorkspace = workspace
	clearYes = true

	if err := runHistoryClear(); err != nil {
		t.Fatalf("runHistoryClear() error = %v", err)
	}

	// Verify everything was cleared (same behavior as --yes)
	changesAfter := countDirEntries(t, filepath.Join(workspace, ".sprout", "changes"))
	revisionsAfter := countDirEntries(t, filepath.Join(workspace, ".sprout", "revisions"))
	runlogsAfter := countFiles(t, filepath.Join(workspace, ".sprout", "runlogs"))

	if changesAfter != 0 {
		t.Errorf("expected 0 changes after clear, got %d", changesAfter)
	}
	if revisionsAfter != 0 {
		t.Errorf("expected 0 revisions after clear, got %d", revisionsAfter)
	}
	if runlogsAfter != 0 {
		t.Errorf("expected 0 runlogs after clear, got %d", runlogsAfter)
	}
}
