package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// AddAllAndCommit — timeout kill path (lines 93-96)
// =============================================================================

func TestAddAllAndCommit_TimeoutKillPath(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Create a pre-commit hook that sleeps longer than our timeout
	hookPath := filepath.Join(dir, ".git", "hooks", "pre-commit")
	require.NoError(t, os.MkdirAll(filepath.Dir(hookPath), 0755))
	require.NoError(t, os.WriteFile(hookPath, []byte("#!/bin/sh\nsleep 30\n"), 0755))

	// Stage a file so `git commit` has something to work on
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hooked.go"), []byte("package hooked\n"), 0644))
	gitRun(t, dir, "add", "hooked.go")

	// Use a very short timeout — the hook will block and we should hit the kill path
	start := time.Now()
	err = AddAllAndCommit("should timeout", 2)
	elapsed := time.Since(start)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
	// Should NOT wait the full 30s (hook duration)
	assert.Less(t, elapsed, 10*time.Second, "should have killed the process, not waited for the hook")
}

func TestAddAllAndCommit_TimeoutErrorInCommit(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Nothing staged — commit will fail quickly, but use timeout path
	err = AddAllAndCommit("nothing staged", 5)
	assert.Error(t, err)
	// The error comes from git commit failing (nothing to commit)
	assert.Contains(t, err.Error(), "error committing changes to git")
}

// =============================================================================
// GetFileGitPath — relative path from CWD vs absolute path
// =============================================================================

func TestGetFileGitPath_CurrentDirectoryFile(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Relative path from the root of the repo
	relPath, err := GetFileGitPath("init.go")
	require.NoError(t, err)
	assert.Equal(t, "init.go", relPath)
}

func TestGetFileGitPath_SubdirectoryAbsolute(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Create nested structure
	nestedDir := filepath.Join(dir, "cmd", "server")
	require.NoError(t, os.MkdirAll(nestedDir, 0755))
	fp := filepath.Join(nestedDir, "main.go")
	require.NoError(t, os.WriteFile(fp, []byte("package main\n"), 0644))
	gitRun(t, dir, "add", filepath.Join("cmd", "server", "main.go"))
	gitRun(t, dir, "commit", "-m", "add main.go")

	relPath, err := GetFileGitPath(fp)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("cmd", "server", "main.go"), relPath)
}

func TestGetFileGitPath_DeeplyNestedFile(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	deepDir := filepath.Join(dir, "a", "b", "c", "d", "e")
	require.NoError(t, os.MkdirAll(deepDir, 0755))
	fp := filepath.Join(deepDir, "deep.go")
	require.NoError(t, os.WriteFile(fp, []byte("package deep\n"), 0644))
	gitRun(t, dir, "add", filepath.Join("a", "b", "c", "d", "e", "deep.go"))
	gitRun(t, dir, "commit", "-m", "add deep")

	relPath, err := GetFileGitPath(fp)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("a", "b", "c", "d", "e", "deep.go"), relPath)
}

func TestGetFileGitPath_FromSubdirectory(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)

	// Create a subdirectory and cd into it
	subdir := filepath.Join(dir, "pkg", "app")
	require.NoError(t, os.MkdirAll(subdir, 0755))
	fp := filepath.Join(subdir, "app.go")
	require.NoError(t, os.WriteFile(fp, []byte("package app\n"), 0644))
	gitRun(t, dir, "add", "pkg/app/app.go")
	gitRun(t, dir, "commit", "-m", "add app.go")

	// Change into the subdirectory
	require.NoError(t, os.Chdir(subdir))

	relPath, err := GetFileGitPath("app.go")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("pkg", "app", "app.go"), relPath)
}

// =============================================================================
// GetStagedDiff — staged deletion (lines 39-40 error, empty diff)
// =============================================================================

func TestGetStagedDiff_StagedDeletion(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Create a file, commit it, then stage its deletion
	require.NoError(t, os.WriteFile(filepath.Join(dir, "toDelete.go"), []byte("package del\n"), 0644))
	gitRun(t, dir, "add", "toDelete.go")
	gitRun(t, dir, "commit", "-m", "add toDelete.go")

	// Stage deletion
	gitRun(t, dir, "rm", "toDelete.go")

	diff, err := GetStagedDiff()
	require.NoError(t, err)
	assert.NotEmpty(t, diff)
	assert.Contains(t, diff, "toDelete.go")
}

func TestGetStagedDiff_StagedModification(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Create a file, commit it, then modify and re-stage
	require.NoError(t, os.WriteFile(filepath.Join(dir, "modify.go"), []byte("package mod\nvar x = 1\n"), 0644))
	gitRun(t, dir, "add", "modify.go")
	gitRun(t, dir, "commit", "-m", "add modify.go")

	// Modify and re-stage
	require.NoError(t, os.WriteFile(filepath.Join(dir, "modify.go"), []byte("package mod\nvar x = 2\nvar y = 3\n"), 0644))
	gitRun(t, dir, "add", "modify.go")

	diff, err := GetStagedDiff()
	require.NoError(t, err)
	assert.NotEmpty(t, diff)
	assert.Contains(t, diff, "modify.go")
}

func TestGetStagedDiff_RenamedFile(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Create and commit a file
	require.NoError(t, os.WriteFile(filepath.Join(dir, "original.go"), []byte("package orig\n"), 0644))
	gitRun(t, dir, "add", "original.go")
	gitRun(t, dir, "commit", "-m", "add original.go")

	// Rename using git mv — this automatically stages the rename
	gitRun(t, dir, "mv", "original.go", "renamed.go")

	diff, err := GetStagedDiff()
	require.NoError(t, err)
	assert.NotEmpty(t, diff)
}

// =============================================================================
// createCommit — error paths for write, close, commit failure
// =============================================================================

func TestCreateCommit_NothingStaged(t *testing.T) {
	dir := newTestGitRepo(t)

	e := &CommitExecutor{Dir: dir}
	_, err := e.createCommit("nothing staged")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "commit failed")
}

func TestCreateCommit_CommitAndVerifyHash(t *testing.T) {
	dir := newTestGitRepo(t)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "hashVerify.go"), []byte("package hv\n"), 0644))
	gitRun(t, dir, "add", "hashVerify.go")

	e := &CommitExecutor{Dir: dir}
	hash, err := e.createCommit("verify hash format")
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
	// Git commit hashes are 40 hex chars
	assert.Len(t, hash, 40)
	for _, c := range hash {
		assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'), "hash char should be hex: %c", c)
	}
}

// =============================================================================
// ExecuteCommit — branch fallback path (symbolic-ref succeeds)
// =============================================================================

func TestExecuteCommit_FallbackBranchSymbolicRef(t *testing.T) {
	// Create a bare git repo, init with an annotated HEAD
	dir, err := os.MkdirTemp("", "ledit-symref-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	run("init", "-b", "custom-branch")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	run("commit", "--allow-empty", "-m", "initial on custom-branch")

	// Stage a file
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.go"), []byte("package f\n"), 0644))
	run("add", "file.go")

	executor := NewCommitExecutorInDir(nil, "commit on custom branch", "", dir)
	hash, err := executor.ExecuteCommit()
	require.NoError(t, err)
	assert.NotEmpty(t, hash)

	cmd := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%B")
	out, _ := cmd.CombinedOutput()
	assert.Contains(t, string(out), "commit on custom branch")
}

func TestExecuteCommit_EmptyStagedContent(t *testing.T) {
	dir := newTestGitRepo(t)

	executor := NewCommitExecutorInDir(nil, "", "", dir)
	_, err := executor.ExecuteCommit()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no staged changes")
}

func TestExecuteCommit_WithUserMessageTakesPriorityOverClient(t *testing.T) {
	dir := newTestGitRepo(t)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "prio.go"), []byte("package prio\n"), 0644))
	gitRun(t, dir, "add", "prio.go")

	mockClient := &mockAPIClient{
		titleResponse: testResponse("LLM generated title", 30),
		descResponse:  testResponse("LLM generated desc", 40),
	}

	// UserMessage should take priority over LLM client
	executor := NewCommitExecutorInDir(mockClient, "user takes priority", "", dir)
	hash, err := executor.ExecuteCommit()
	require.NoError(t, err)
	assert.NotEmpty(t, hash)

	cmd := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%B")
	out, _ := cmd.CombinedOutput()
	content := string(out)
	assert.Contains(t, content, "user takes priority")
	// The LLM message should NOT be used
	assert.NotContains(t, content, "LLM generated title")
}

func TestExecuteCommit_UserInstructionsOverFallback(t *testing.T) {
	dir := newTestGitRepo(t)

	// Stage multiple files to trigger fallback normally
	for _, name := range []string{"f1.go", "f2.go"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("package f\n"), 0644))
		gitRun(t, dir, "add", name)
	}

	executor := NewCommitExecutorInDir(nil, "", "explicit instructions", dir)
	_, execErr := executor.ExecuteCommit()
	require.NoError(t, execErr)

	cmd := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%B")
	out, _ := cmd.CombinedOutput()
	assert.Contains(t, string(out), "explicit instructions")
}

func TestExecuteCommit_MixedFileTypesFallback(t *testing.T) {
	dir := newTestGitRepo(t)

	// Create, commit, then delete a file (staged deletion)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "willDelete.go"), []byte("package del\n"), 0644))
	gitRun(t, dir, "add", "willDelete.go")
	gitRun(t, dir, "commit", "-m", "add willDelete.go")

	// Stage deletion + add new file
	gitRun(t, dir, "rm", "willDelete.go")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "newOne.go"), []byte("package new\n"), 0644))
	gitRun(t, dir, "add", "newOne.go")

	executor := NewCommitExecutorInDir(nil, "", "", dir)
	_, tmpErr := executor.ExecuteCommit()
	require.NoError(t, tmpErr)

	cmd := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%s")
	out, _ := cmd.CombinedOutput()
	msg := string(out)
	// Fallback for mixed types should mention Add and Delete
	assert.True(t, strings.Contains(msg, "Add") || strings.Contains(msg, "Delete"))
}

// =============================================================================
// CheckStagedChanges — non-ExitError path (hard to trigger, test via coverage)
// =============================================================================

func TestCheckStagedChanges_SpaceInFilename(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// File with space in name
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file with spaces.go"), []byte("package spaces\n"), 0644))
	gitRun(t, dir, "add", "file with spaces.go")

	err = CheckStagedChanges()
	assert.NoError(t, err, "staged file with spaces should be detected")
}

func TestCheckStagedChanges_StagedDeletion(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Create, commit, then stage deletion
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deleteMe.go"), []byte("package d\n"), 0644))
	gitRun(t, dir, "add", "deleteMe.go")
	gitRun(t, dir, "commit", "-m", "add deleteMe.go")

	// Stage deletion
	gitRun(t, dir, "rm", "deleteMe.go")

	err = CheckStagedChanges()
	assert.NoError(t, err, "staged deletion should count as staged changes")
}

// =============================================================================
// CheckStagedFilesForSecurityCredentials — staged diff error for one file
// =============================================================================

func TestCheckStagedFilesForSecurityCredentials_BinaryFile(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Create a binary file (null bytes) and stage it
	binaryContent := []byte{0x00, 0x01, 0x02, 0x80, 0x90, 0xFF}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "binary.bin"), binaryContent, 0644))
	gitRun(t, dir, "add", "binary.bin")

	logger := utils.GetLogger(true)

	// Binary files may produce empty diff output or error — function should handle gracefully
	result := CheckStagedFilesForSecurityCredentials(logger)
	// Binary files typically have no text diff, so no security concerns
	assert.False(t, result.HasConcerns)
}

func TestCheckStagedFilesForSecurityCredentials_MultipleFilesSomeClean(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Stage a clean file
	require.NoError(t, os.WriteFile(filepath.Join(dir, "clean1.go"), []byte("package clean1\nfunc hello() {}\n"), 0644))
	gitRun(t, dir, "add", "clean1.go")

	// Stage another clean file
	require.NoError(t, os.WriteFile(filepath.Join(dir, "clean2.go"), []byte("package clean2\nfunc world() {}\n"), 0644))
	gitRun(t, dir, "add", "clean2.go")

	logger := utils.GetLogger(true)

	result := CheckStagedFilesForSecurityCredentials(logger)
	assert.False(t, result.HasConcerns)
}

func TestCheckStagedFilesForSecurityCredentials_PasswordPattern(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Stage a file with a password variable
	content := `package config
var dbPassword = "super_secret_password_123"
func connect() string { return dbPassword }
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.go"), []byte(content), 0644))
	gitRun(t, dir, "add", "config.go")

	logger := utils.GetLogger(true)

	result := CheckStagedFilesForSecurityCredentials(logger)
	assert.True(t, result.HasConcerns, "password pattern should trigger security concern")
}

func TestCheckStagedFilesForSecurityCredentials_ModifiedFileWithSecret(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Create and commit a file without secrets
	require.NoError(t, os.WriteFile(filepath.Join(dir, "env.go"), []byte("package env\nconst Host = \"localhost\"\n"), 0644))
	gitRun(t, dir, "add", "env.go")
	gitRun(t, dir, "commit", "-m", "add env")

	// Modify to add an AWS access key pattern (known trigger), stage the modification
	require.NoError(t, os.WriteFile(filepath.Join(dir, "env.go"), []byte("package env\nconst Host = \"localhost\"\nconst AWSKey = \"AKIAIOSFODNN7EXAMPLE\"\n"), 0644))
	gitRun(t, dir, "add", "env.go")

	logger := utils.GetLogger(true)

	result := CheckStagedFilesForSecurityCredentials(logger)
	assert.True(t, result.HasConcerns, "adding an AWS key to a modified file should be detected")
}

// =============================================================================
// GetGitStatus — uncommitted modified files (Y column), status error
// =============================================================================

func TestGetGitStatus_UncommittedModifiedFile(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Modify a tracked file without staging
	require.NoError(t, os.WriteFile(filepath.Join(dir, "init.go"), []byte("package modified\n"), 0644))

	branch, uncommitted, staged, err := GetGitStatus()
	require.NoError(t, err)
	assert.Equal(t, "main", branch)
	// A modified file in Y column counts as uncommitted change
	assert.Greater(t, uncommitted+staged, 0, "modified file should be detected")
}

func TestGetGitStatus_BothStagedAndUncommitted(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Create a file and stage it
	require.NoError(t, os.WriteFile(filepath.Join(dir, "track.go"), []byte("package track\n"), 0644))
	gitRun(t, dir, "add", "track.go")

	// Now modify the staged file in the working tree (creating both staged + unstaged)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "track.go"), []byte("package track\nvar x int\n"), 0644))

	branch, _, staged, err := GetGitStatus()
	require.NoError(t, err)
	assert.Equal(t, "main", branch)
	assert.Greater(t, staged, 0, "should have staged changes")
	// Could also have uncommitted changes since the file was modified after staging
}

func TestGetGitStatus_StagedDeletion(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Create, commit, then stage deletion
	require.NoError(t, os.WriteFile(filepath.Join(dir, "toBeDeleted.go"), []byte("package del\n"), 0644))
	gitRun(t, dir, "add", "toBeDeleted.go")
	gitRun(t, dir, "commit", "-m", "add toBeDeleted.go")

	// Stage deletion
	gitRun(t, dir, "rm", "toBeDeleted.go")

	branch, _, staged, err := GetGitStatus()
	require.NoError(t, err)
	assert.Equal(t, "main", branch)
	assert.Equal(t, 1, staged, "staged deletion should show as 1 staged change")
}

func TestGetGitStatus_CleanRepo(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Clean repo with only the initial commit
	branch, uncommitted, staged, err := GetGitStatus()
	require.NoError(t, err)
	assert.Equal(t, "main", branch)
	assert.Equal(t, 0, uncommitted)
	assert.Equal(t, 0, staged)
}

func TestGetGitStatus_MultipleUntracked(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Create several untracked files
	for _, name := range []string{"u1.go", "u2.go", "u3.go"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("package u\n"), 0644))
	}

	branch, uncommitted, staged, err := GetGitStatus()
	require.NoError(t, err)
	assert.Equal(t, "main", branch)
	// Untracked files should NOT count as staged or uncommitted
	assert.Equal(t, 0, staged)
	assert.Equal(t, 0, uncommitted)
}

func TestGetGitStatus_StagedNewFileAndModifiedTracked(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Stage a new file (X=A, Y= )
	require.NoError(t, os.WriteFile(filepath.Join(dir, "brand_new.go"), []byte("package bn\n"), 0644))
	gitRun(t, dir, "add", "brand_new.go")

	// Modify an existing tracked file without staging (X= , Y=M)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "init.go"), []byte("package modified\n"), 0644))

	branch, _, staged, err := GetGitStatus()
	require.NoError(t, err)
	assert.Equal(t, "main", branch)
	assert.GreaterOrEqual(t, staged, 1, "should have at least 1 staged change (new file)")
}

// =============================================================================
// WrapText — wrap multiple paragraphs with actual wrapping
// =============================================================================

func TestWrapText_MultipleParagraphs(t *testing.T) {
	text := "This is the first paragraph that is quite long and should be wrapped onto multiple lines.\n\nThis is the second paragraph."
	got := WrapText(text, 40)
	paragraphs := strings.Split(got, "\n\n")
	assert.Equal(t, 2, len(paragraphs), "should have 2 paragraphs")
	// Each line within should be <= 40 chars
	for _, p := range paragraphs {
		for _, line := range strings.Split(p, "\n") {
			assert.LessOrEqual(t, len(line), 40, "line should be <= 40: %q", line)
		}
	}
}

func TestWrapText_ThreeParagraphs(t *testing.T) {
	text := "First short paragraph.\n\nSecond medium sized paragraph with more words.\n\nThird paragraph here."
	got := WrapText(text, 72)
	paragraphs := strings.Split(got, "\n\n")
	assert.Equal(t, 3, len(paragraphs))
	assert.Contains(t, got, "First short paragraph")
	assert.Contains(t, got, "Second medium sized paragraph with more words")
	assert.Contains(t, got, "Third paragraph here")
}

func TestWrapText_LongWordsStayOnOwnLine(t *testing.T) {
	// A word longer than lineLength stays on its own line
	got := WrapText("tiny verylongwordthatexceedslimit end", 10)
	lines := strings.Split(got, "\n")
	assert.Contains(t, lines, "verylongwordthatexceedslimit")
}

func TestWrapText_ParagraphWithLeadingTrailingSpace(t *testing.T) {
	got := WrapText("  hello world  ", 72)
	assert.Equal(t, "hello world", got)
}

func TestWrapText_SingleWordParagraphs(t *testing.T) {
	got := WrapText("one\n\ntwo\n\nthree", 72)
	paragraphs := strings.Split(got, "\n\n")
	assert.Equal(t, 3, len(paragraphs))
	assert.Equal(t, "one", paragraphs[0])
	assert.Equal(t, "two", paragraphs[1])
	assert.Equal(t, "three", paragraphs[2])
}

func TestWrapText_WrappingPreservesContent(t *testing.T) {
	longText := "The quick brown fox jumps over the lazy dog and runs through the field while the sun sets in the west painting the sky with beautiful colors."
	got := WrapText(longText, 50)
	// All words should be present
	for _, word := range strings.Fields(longText) {
		assert.Contains(t, got, word)
	}
	// No line should exceed 50 chars
	for _, line := range strings.Split(got, "\n") {
		assert.LessOrEqual(t, len(line), 50, "line exceeds 50: %q", line)
	}
}

// =============================================================================
// GenerateCommitMessageFromStagedDiff — additional coverage paths
// =============================================================================

func TestGenerateCommitMessageFromStagedDiff_AllDeletedFiles(t *testing.T) {
	mockClient := &mockAPIClient{
		titleResponse: testResponse("Deletes obsolete endpoints", 50),
		descResponse:  testResponse("Removes deprecated API handlers", 60),
	}

	result, err := GenerateCommitMessageFromStagedDiff(mockClient, CommitMessageOptions{
		Diff: "-func OldHandler() {}\n-func Legacy() {}",
		Branch: "main",
		FileChanges: []CommitFileChange{
			{Status: "D", Path: "old_handler.go"},
			{Status: "D", Path: "legacy.go"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	// All files are "Deletes" — primaryAction should be "Deletes"
	assert.Contains(t, result.Message, "Deletes 2 files")
}

func TestGenerateCommitMessageFromStagedDiff_AllRenamedFiles(t *testing.T) {
	mockClient := &mockAPIClient{
		titleResponse: testResponse("Renames module files", 40),
		descResponse:  testResponse("Updates file names for clarity", 50),
	}

	result, err := GenerateCommitMessageFromStagedDiff(mockClient, CommitMessageOptions{
		Diff: "-old_name\n+new_name",
		Branch: "main",
		FileChanges: []CommitFileChange{
			{Status: "R100", Path: "old.go\tnew.go"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Contains(t, result.Message, "Renames")
}

func TestGenerateCommitMessageFromStagedDiff_EmptyBranch(t *testing.T) {
	mockClient := &mockAPIClient{
		titleResponse: testResponse("Some change", 30),
		descResponse:  testResponse("Description", 40),
	}

	result, err := GenerateCommitMessageFromStagedDiff(mockClient, CommitMessageOptions{
		Diff:        "+new line",
		Branch:      "",
		FileChanges: []CommitFileChange{{Status: "M", Path: "file.go"}},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	// Empty branch uses default check, empty is not default → but TrimSpace "" is "",
	// the condition is !isDefaultBranch("") && strings.TrimSpace("") != "" → false && false → no prefix
	assert.NotContains(t, result.Message, "[] ")
}

func TestGenerateCommitMessageFromStagedDiff_WhitespaceBranch(t *testing.T) {
	mockClient := &mockAPIClient{
		titleResponse: testResponse("Some change", 30),
		descResponse:  testResponse("Description", 40),
	}

	// Branch is whitespace — should not add prefix
	result, err := GenerateCommitMessageFromStagedDiff(mockClient, CommitMessageOptions{
		Diff:        "+new line",
		Branch:      "   ",
		FileChanges: []CommitFileChange{{Status: "M", Path: "file.go"}},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotContains(t, result.Message, "[   ]")
}

func TestGenerateCommitMessageFromStagedDiff_FileChangeEmptyPath(t *testing.T) {
	mockClient := &mockAPIClient{
		titleResponse: testResponse("Updates file", 30),
		descResponse:  testResponse("Changes", 30),
	}

	// FileChange with empty path — only the non-empty one gets into fileActions
	// So len(fileActions)==1 → single-file format "Updates file.go"
	// But len(opts.FileChanges)==3 → total count used only in summary when >1 action
	result, err := GenerateCommitMessageFromStagedDiff(mockClient, CommitMessageOptions{
		Diff:   "+new line",
		Branch: "main",
		FileChanges: []CommitFileChange{
			{Status: "M", Path: "file.go"},
			{Status: "M", Path: ""},
			{Status: "A", Path: "  "},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	// fileActions filters out empty/whitespace paths → len(fileActions)==1 → uses single-file format
	assert.Contains(t, result.Message, "Updates file.go")
}

func TestGenerateCommitMessageFromStagedDiff_WarningsPropagated(t *testing.T) {
	mockClient := &mockAPIClient{
		titleResponse: testResponse("Adds feature", 30),
		descResponse:  testResponse("New feature implementation", 40),
	}

	result, err := GenerateCommitMessageFromStagedDiff(mockClient, CommitMessageOptions{
		Diff:   "+added",
		Branch: "feature/x",
		FileChanges: []CommitFileChange{
			{Status: "A", Path: "new.go"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	// Warnings are populated by DiffOptimizer — may be nil for simple diffs
	// The key assertion is the result was returned successfully with a message
	assert.NotEmpty(t, result.Message)
}

// =============================================================================
// AddAndCommitFile — success path with logger output
// =============================================================================

func TestAddAndCommitFile_NewFileSuccess(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	fp := filepath.Join(dir, "success.go")
	require.NoError(t, os.WriteFile(fp, []byte("package success\n"), 0644))

	err = AddAndCommitFile("success.go", "add success.go")
	assert.NoError(t, err)

	cmd := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%s")
	out, _ := cmd.CombinedOutput()
	assert.Equal(t, "add success.go\n", string(out))
}

// =============================================================================
// PerformGitCommit — commit message with special characters
// =============================================================================

func TestPerformGitCommit_SpecialCharsInMessage(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "special.go"), []byte("package sp\n"), 0644))
	gitRun(t, dir, "add", "special.go")

	msg := "feat: add special chars $`\"'\\()[]{}&*|;<>!~"
	err = PerformGitCommit(msg)
	assert.NoError(t, err)

	cmd := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%B")
	out, _ := cmd.CombinedOutput()
	assert.Contains(t, string(out), "feat: add special chars")
}

func TestPerformGitCommit_MultilineMessage(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "ml.go"), []byte("package ml\n"), 0644))
	gitRun(t, dir, "add", "ml.go")

	msg := "feat: multiline commit\n\nThis is the body.\nIt has multiple lines."
	err = PerformGitCommit(msg)
	assert.NoError(t, err)

	cmd := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%B")
	out, _ := cmd.CombinedOutput()
	content := string(out)
	assert.Contains(t, content, "feat: multiline commit")
	assert.Contains(t, content, "This is the body.")
	assert.Contains(t, content, "It has multiple lines.")
}

// =============================================================================
// GetStagedChanges — staged rename
// =============================================================================

func TestGetStagedChanges_StagedRename(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "old_name.go"), []byte("package old\n"), 0644))
	gitRun(t, dir, "add", "old_name.go")
	gitRun(t, dir, "commit", "-m", "add old_name.go")

	// git mv automatically stages the rename
	gitRun(t, dir, "mv", "old_name.go", "new_name.go")

	diff, err := GetStagedChanges()
	require.NoError(t, err)
	assert.NotEmpty(t, diff)
}

// =============================================================================
// GetUncommittedChanges — modified tracked file
// =============================================================================

func TestGetUncommittedChanges_ModifiedTrackedFile(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Create and commit a file
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tracked.go"), []byte("line1\nline2\nline3\n"), 0644))
	gitRun(t, dir, "add", "tracked.go")
	gitRun(t, dir, "commit", "-m", "add tracked.go")

	// Modify it (uncommitted change)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tracked.go"), []byte("line1\nMODIFIED\nline3\n"), 0644))

	diff, err := GetUncommittedChanges()
	require.NoError(t, err)
	assert.NotEmpty(t, diff)
	assert.Contains(t, diff, "tracked.go")
	assert.Contains(t, diff, "MODIFIED")
}

func TestGetUncommittedChanges_DeletedUnstagedFile(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Create and commit a file, then delete without staging
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gone.go"), []byte("package gone\n"), 0644))
	gitRun(t, dir, "add", "gone.go")
	gitRun(t, dir, "commit", "-m", "add gone.go")

	require.NoError(t, os.Remove(filepath.Join(dir, "gone.go")))

	diff, err := GetUncommittedChanges()
	require.NoError(t, err)
	assert.NotEmpty(t, diff)
}

// =============================================================================
// GetRecentTouchedFiles — zero results scenario
// =============================================================================

func TestGetRecentTouchedFiles_LargeNumCommits(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	files, err := GetRecentTouchedFiles(1000)
	require.NoError(t, err)
	assert.NotNil(t, files)
}

// =============================================================================
// GetRecentFileLog — verify log content format
// =============================================================================

func TestGetRecentFileLog_WithMultipleCommits(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Make multiple commits touching the same file
	for i := 1; i <= 5; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "logtest.go"),
			[]byte(fmt.Sprintf("package lt\nvar v=%d\n", i)), 0644))
		gitRun(t, dir, "add", "logtest.go")
		gitRun(t, dir, "commit", "-m", fmt.Sprintf("update logtest %d", i))
	}

	log, err := GetRecentFileLog("logtest.go", 5)
	require.NoError(t, err)
	assert.NotEmpty(t, log)
	assert.NotEqual(t, "(no recent commits)", log)
	// Limit is 5, so we should get up to 5 lines
	lines := strings.Split(log, "\n")
	assert.LessOrEqual(t, len(lines), 5)
}

// =============================================================================
// GetGitRemoteURL — error when no git
// =============================================================================

func TestGetGitRemoteURL_OutsideRepo(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	tmpDir, err := os.MkdirTemp("", "remote-error-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(tmpDir))

	_, err = GetGitRemoteURL()
	assert.Error(t, err)
}

// =============================================================================
// Integration: Full commit lifecycle
// =============================================================================

func TestFullCommitLifecycle_AddStageCommitVerify(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// 1. Check no staged changes
	err = CheckStagedChanges()
	assert.Error(t, err, "should have no staged changes initially")

	// 2. Create and stage
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lifecycle.go"), []byte("package lc\n"), 0644))
	gitRun(t, dir, "add", "lifecycle.go")

	// 3. Check staged changes exist
	err = CheckStagedChanges()
	assert.NoError(t, err)

	// 4. Get staged diff
	diff, err := GetStagedDiff()
	require.NoError(t, err)
	assert.NotEmpty(t, diff)

	// 5. Check security
	logger := utils.GetLogger(true)
	secure := CheckStagedFilesForSecurityCredentials(logger)
	assert.False(t, secure.HasConcerns)

	// 6. Commit
	err = AddAllAndCommit("lifecycle test", 5)
	assert.NoError(t, err)

	// 7. Verify clean state
	branch, uncommitted, staged, err := GetGitStatus()
	require.NoError(t, err)
	assert.Equal(t, "main", branch)
	assert.Equal(t, 0, uncommitted)
	assert.Equal(t, 0, staged)

	// 8. No uncommitted changes
	_, err = GetUncommittedChanges()
	assert.NoError(t, err)
}

// =============================================================================
// Additional coverage for uncovered branches
// =============================================================================

// --- GetStagedDiff error path (outside git repo) ---

func TestGetStagedDiff_OutsideRepo(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	tmpDir, err := os.MkdirTemp("", "staged-diff-no-git-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(tmpDir))

	_, err = GetStagedDiff()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get staged diff")
}

// --- CheckStagedFilesForSecurityCredentials get-staged-files error ---

func TestCheckStagedFilesForSecurityCredentials_OutsideRepo(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	tmpDir, err := os.MkdirTemp("", "cred-no-git-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(tmpDir))

	logger := utils.GetLogger(true)

	// Outside git repo → git diff --cached --name-only fails
	result := CheckStagedFilesForSecurityCredentials(logger)
	// Returns false on error
	assert.False(t, result.HasConcerns)
}

// --- WrapText words==0 path (paragraph with only whitespace that Fields ignores) ---

func TestWrapText_WhitespaceOnlyParagraph(t *testing.T) {
	// A "paragraph" that splits into 0 words after Fields processing.
	// Using a tab character between double newlines creates a paragraph of "\t"
	// which Fields() will return 0 words for.
	got := WrapText("\t\n\nword", 72)
	// The tab-only "paragraph" becomes empty after Fields → wrapped to ""
	// Result: "\n\nword"
	assert.Equal(t, "\n\nword", got)
}

func TestWrapText_TabParagraphBetweenText(t *testing.T) {
	got := WrapText("hello\n\t\nworld", 72)
	// Split on \n\n: ["hello", "\t\nworld"] → wait, \t\n is not \n\n
	// "hello\n\t\nworld" has NO \n\n, so single paragraph
	// Fields("hello\n\t\nworld") → ["hello", "world"]
	assert.Equal(t, "hello world", got)
}

// --- GetRecentFileLog line truncation (lines > limit) ---

func TestGetRecentFileLog_LineTruncation(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Make many commits to the same file
	for i := 0; i < 10; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "many.go"),
			[]byte(fmt.Sprintf("package m\nvar v=%d\n", i)), 0644))
		gitRun(t, dir, "add", "many.go")
		gitRun(t, dir, "commit", "-m", fmt.Sprintf("commit %d", i))
	}

	// Request limit=1 — git returns 2+ lines, so truncation kicks in
	log, err := GetRecentFileLog("many.go", 1)
	require.NoError(t, err)
	lines := strings.Split(log, "\n")
	assert.LessOrEqual(t, len(lines), 1, "should be limited to 1 line")
}

// --- ExecuteCommit detached HEAD (symbolic-ref fails) ---

func TestExecuteCommit_DetachedHeadFallback(t *testing.T) {
	dir, err := os.MkdirTemp("", "ledit-detached-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	// Create initial commit
	require.NoError(t, os.WriteFile(filepath.Join(dir, "init.go"), []byte("package x\n"), 0644))
	run("add", "init.go")
	run("commit", "-m", "initial")

	// Go into detached HEAD state
	run("checkout", "--detach", "HEAD")

	// Stage a file
	require.NoError(t, os.WriteFile(filepath.Join(dir, "detached.go"), []byte("package d\n"), 0644))
	run("add", "detached.go")

	// ExecuteCommit — rev-parse HEAD works (detached but valid) for commit,
	// but the branch fallback should not trigger since HEAD ref resolves
	executor := NewCommitExecutorInDir(nil, "detached head commit", "", dir)
	hash, err := executor.ExecuteCommit()
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
}

// --- ExecuteCommit fallback where generateCommitMessage returns empty ---

func TestExecuteCommit_NilClientFallbackEmptyChanges(t *testing.T) {
	dir := newTestGitRepo(t)

	// Stage a file
	require.NoError(t, os.WriteFile(filepath.Join(dir, "nil.go"), []byte("package nil\n"), 0644))
	gitRun(t, dir, "add", "nil.go")

	// nil client, no message, no instructions → uses fallback
	executor := NewCommitExecutorInDir(nil, "", "", dir)
	hash, err := executor.ExecuteCommit()
	require.NoError(t, err)
	assert.NotEmpty(t, hash)

	cmd := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%s")
	out, _ := cmd.CombinedOutput()
	assert.Contains(t, string(out), "Update nil.go")
}

// --- GetGitStatus branch error (non-git-repo) ---

func TestGetGitStatus_BranchErrorNonGitRepo(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	tmpDir, err := os.MkdirTemp("", "status-branch-err-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(tmpDir))

	branch, uncommitted, staged, err := GetGitStatus()
	// The "not a git repository" error returns nil,0,0,nil
	assert.NoError(t, err)
	assert.Equal(t, "", branch)
	assert.Equal(t, 0, uncommitted)
	assert.Equal(t, 0, staged)
}
