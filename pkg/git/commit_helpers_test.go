package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// parseStagedFileChanges tests

func TestParseStagedFileChanges_TabSeparated(t *testing.T) {
	input := "A\tnewfile.go\nM\tmodified.go\nD\tdeleted.go"
	result := parseStagedFileChanges(input)

	if len(result) != 3 {
		t.Fatalf("expected 3 changes, got %d", len(result))
	}
	assertChange(t, result[0], "A", "newfile.go")
	assertChange(t, result[1], "M", "modified.go")
	assertChange(t, result[2], "D", "deleted.go")
}

func TestParseStagedFileChanges_SpaceSeparated(t *testing.T) {
	input := "A newfile.go\nM modified.go\nD deleted.go"
	result := parseStagedFileChanges(input)

	if len(result) != 3 {
		t.Fatalf("expected 3 changes, got %d", len(result))
	}
	assertChange(t, result[0], "A", "newfile.go")
	assertChange(t, result[1], "M", "modified.go")
	assertChange(t, result[2], "D", "deleted.go")
}

func TestParseStagedFileChanges_Empty(t *testing.T) {
	result := parseStagedFileChanges("")
	if len(result) != 0 {
		t.Errorf("expected 0 changes for empty input, got %d", len(result))
	}
}

func TestParseStagedFileChanges_EmptyLines(t *testing.T) {
	input := "A\tfile.go\n\n\nM\tother.go"
	result := parseStagedFileChanges(input)

	if len(result) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(result))
	}
	assertChange(t, result[0], "A", "file.go")
	assertChange(t, result[1], "M", "other.go")
}

func TestParseStagedFileChanges_SingleFile(t *testing.T) {
	input := "M\tmain.go"
	result := parseStagedFileChanges(input)

	if len(result) != 1 {
		t.Fatalf("expected 1 change, got %d", len(result))
	}
	assertChange(t, result[0], "M", "main.go")
}

func TestParseStagedFileChanges_MultipleFiles(t *testing.T) {
	input := "A\tpkg/auth/login.go\nM\tpkg/auth/handler.go\nM\tpkg/api/router.go\nD\tpkg/legacy/old.go\nA\tpkg/utils/helper.go"
	result := parseStagedFileChanges(input)

	if len(result) != 5 {
		t.Fatalf("expected 5 changes, got %d", len(result))
	}
	assertChange(t, result[0], "A", "pkg/auth/login.go")
	assertChange(t, result[1], "M", "pkg/auth/handler.go")
	assertChange(t, result[2], "M", "pkg/api/router.go")
	assertChange(t, result[3], "D", "pkg/legacy/old.go")
	assertChange(t, result[4], "A", "pkg/utils/helper.go")
}

func TestParseStagedFileChanges_Rename(t *testing.T) {
	// Git rename format: R{similarity}\told_path\tnew_path
	input := "R100\told_name.go\tnew_name.go"
	result := parseStagedFileChanges(input)

	if len(result) != 1 {
		t.Fatalf("expected 1 change, got %d", len(result))
	}
	// For rename, the path is the remaining fields joined: "old_name.go new_name.go"
	if result[0].Status != "R100" {
		t.Errorf("expected status %q, got %q", "R100", result[0].Status)
	}
	expectedPath := "old_name.go new_name.go"
	if result[0].Path != expectedPath {
		t.Errorf("expected path %q, got %q", expectedPath, result[0].Path)
	}
}

func TestParseStagedFileChanges_RenameLowSimilarity(t *testing.T) {
	input := "R050\told.go\tnew.go"
	result := parseStagedFileChanges(input)

	if len(result) != 1 {
		t.Fatalf("expected 1 change, got %d", len(result))
	}
	if result[0].Status != "R050" {
		t.Errorf("expected status %q, got %q", "R050", result[0].Status)
	}
}

func TestParseStagedFileChanges_CopiedFile(t *testing.T) {
	input := "C100\toriginal.go\tcopy.go"
	result := parseStagedFileChanges(input)

	if len(result) != 1 {
		t.Fatalf("expected 1 change, got %d", len(result))
	}
	if result[0].Status != "C100" {
		t.Errorf("expected status %q, got %q", "C100", result[0].Status)
	}
}

func TestParseStagedFileChanges_LineWithOnlyStatus(t *testing.T) {
	// A line with just a status and no path should be skipped
	input := "M\t\nA\tfile.go"
	result := parseStagedFileChanges(input)

	if len(result) != 1 {
		t.Fatalf("expected 1 change, got %d", len(result))
	}
	assertChange(t, result[0], "A", "file.go")
}

func TestParseStagedFileChanges_UnmergedStatus(t *testing.T) {
	// Git shows unmerged files with various status codes like UU, AA, DD
	input := "UU\tconflict.go\nAA\tduplicate.go"
	result := parseStagedFileChanges(input)

	if len(result) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(result))
	}
	assertChange(t, result[0], "UU", "conflict.go")
	assertChange(t, result[1], "AA", "duplicate.go")
}

// generateFallbackCommitMessage tests

func TestGenerateFallbackCommitMessage_Empty(t *testing.T) {
	result := generateFallbackCommitMessage(nil)
	if result != "Update files" {
		t.Errorf("expected %q, got %q", "Update files", result)
	}

	result = generateFallbackCommitMessage([]CommitFileChange{})
	if result != "Update files" {
		t.Errorf("expected %q for empty slice, got %q", "Update files", result)
	}
}

func TestGenerateFallbackCommitMessage_SingleFile(t *testing.T) {
	tests := []struct {
		name  string
		change CommitFileChange
		want  string
	}{
		{"single added", CommitFileChange{Status: "A", Path: "main.go"}, "Update main.go"},
		{"single modified", CommitFileChange{Status: "M", Path: "utils.go"}, "Update utils.go"},
		{"single deleted", CommitFileChange{Status: "D", Path: "old.go"}, "Update old.go"},
		{"single renamed", CommitFileChange{Status: "R100", Path: "old.go new.go"}, "Update old.go new.go"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateFallbackCommitMessage([]CommitFileChange{tt.change})
			if result != tt.want {
				t.Errorf("expected %q, got %q", tt.want, result)
			}
		})
	}
}

func TestGenerateFallbackCommitMessage_MultipleSameType(t *testing.T) {
	tests := []struct {
		name  string
		changes []CommitFileChange
		want  string
	}{
		{
			"multiple added",
			[]CommitFileChange{{Status: "A", Path: "a.go"}, {Status: "A", Path: "b.go"}, {Status: "A", Path: "c.go"}},
			"Add 3 Files",
		},
		{
			"multiple modified",
			[]CommitFileChange{{Status: "M", Path: "a.go"}, {Status: "M", Path: "b.go"}},
			"Update 2 Files",
		},
		{
			"multiple deleted",
			[]CommitFileChange{{Status: "D", Path: "a.go"}, {Status: "D", Path: "b.go"}, {Status: "D", Path: "c.go"}, {Status: "D", Path: "d.go"}},
			"Delete 4 Files",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateFallbackCommitMessage(tt.changes)
			if result != tt.want {
				t.Errorf("expected %q, got %q", tt.want, result)
			}
		})
	}
}

func TestGenerateFallbackCommitMessage_Mixed(t *testing.T) {
	changes := []CommitFileChange{
		{Status: "A", Path: "new.go"},
		{Status: "M", Path: "existing.go"},
		{Status: "D", Path: "removed.go"},
	}

	result := generateFallbackCommitMessage(changes)

	// Should contain parts for add, update, and delete (using strings.Title)
	if !strings.Contains(result, "Add") {
		t.Errorf("expected mixed message to contain 'Add', got %q", result)
	}
	if !strings.Contains(result, "Update") {
		t.Errorf("expected mixed message to contain 'Update', got %q", result)
	}
	if !strings.Contains(result, "Delete") {
		t.Errorf("expected mixed message to contain 'Delete', got %q", result)
	}
}

func TestGenerateFallbackCommitMessage_MixedPartial(t *testing.T) {
	// Only added + modified, no deleted
	changes := []CommitFileChange{
		{Status: "A", Path: "a.go"},
		{Status: "A", Path: "b.go"},
		{Status: "M", Path: "c.go"},
	}

	result := generateFallbackCommitMessage(changes)
	if !strings.Contains(result, "Add 2 Files") {
		t.Errorf("expected 'Add 2 Files' in %q", result)
	}
	if !strings.Contains(result, "Update 1 Files") {
		t.Errorf("expected 'Update 1 Files' in %q", result)
	}
}

// CommitExecutor integration tests using temp git repos

func newTestGitRepo(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ledit-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")

	// Create an initial commit so HEAD exists (required by many git operations).
	initPath := filepath.Join(dir, "init.go")
	if err := os.WriteFile(initPath, []byte("package x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", "init.go")
	run("commit", "-m", "initial commit")

	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestCommitExecutor_ExecutesCommitInTempRepo(t *testing.T) {
	dir := newTestGitRepo(t)

	// Create and stage a file
	if err := os.WriteFile(dir+"/test.txt", []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if _, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "test.txt")

	executor := NewCommitExecutorInDir(nil, "Test commit message", "", dir)
	hash, err := executor.ExecuteCommit()
	if err != nil {
		t.Fatalf("ExecuteCommit failed: %v", err)
	}
	if hash == "" {
		t.Error("expected non-empty commit hash")
	}
}

func TestCommitExecutor_NoStagedChanges(t *testing.T) {
	dir := newTestGitRepo(t)

	executor := NewCommitExecutorInDir(nil, "Should fail", "", dir)
	_, err := executor.ExecuteCommit()
	if err == nil {
		t.Fatal("expected error when no staged changes")
	}
	if !strings.Contains(err.Error(), "no staged changes") {
		t.Errorf("expected 'no staged changes' error, got: %v", err)
	}
}

func TestCommitExecutor_UserMessagePriority(t *testing.T) {
	dir := newTestGitRepo(t)

	os.WriteFile(dir+"/file.go", []byte("package main\n"), 0644)
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.CombinedOutput()
	}
	run("add", "file.go")

	executor := NewCommitExecutorInDir(nil, "user message", "instructions", dir)
	hash, err := executor.ExecuteCommit()
	if err != nil {
		t.Fatalf("ExecuteCommit failed: %v", err)
	}
	if hash == "" {
		t.Error("expected non-empty hash")
	}

	// Verify the commit uses the user message, not instructions
	cmd := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%B")
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "user message") {
		t.Errorf("expected commit message to contain 'user message', got: %s", string(out))
	}
}

func TestCommitExecutor_UserInstructionsPriority(t *testing.T) {
	dir := newTestGitRepo(t)

	// Create initial commit so branch exists
	os.WriteFile(dir+"/init.go", []byte("package init\n"), 0644)
	run := func(args ...string) {
		exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	}
	run("add", "init.go")
	run("commit", "-m", "init")

	// Now add another file for our test commit
	os.WriteFile(dir+"/file.go", []byte("package main\n"), 0644)
	run("add", "file.go")

	executor := NewCommitExecutorInDir(nil, "", "my instructions", dir)
	hash, err := executor.ExecuteCommit()
	if err != nil {
		t.Fatalf("ExecuteCommit failed: %v", err)
	}
	if hash == "" {
		t.Error("expected non-empty hash")
	}

	cmd := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%B")
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "my instructions") {
		t.Errorf("expected commit message to contain 'my instructions', got: %s", string(out))
	}
}

// helper
func assertChange(t *testing.T, got CommitFileChange, wantStatus, wantPath string) {
	t.Helper()
	if got.Status != wantStatus {
		t.Errorf("expected status %q, got %q", wantStatus, got.Status)
	}
	if got.Path != wantPath {
		t.Errorf("expected path %q, got %q", wantPath, got.Path)
	}
}
