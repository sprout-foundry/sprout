package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// CleanCommitMessage — edge cases for uncovered paths
// =============================================================================

func TestCleanCommitMessage_EmptyString(t *testing.T) {
	got := CleanCommitMessage("")
	assert.Equal(t, "", got)
}

func TestCleanCommitMessage_WhitespaceOnly(t *testing.T) {
	// Whitespace-only input doesn't match JSON/markdown patterns, so passes through unchanged
	got := CleanCommitMessage("   \t  \n  ")
	// The multiline block: len(lines) > 2, but descriptionStart search finds no non-empty line
	// so descriptionStart stays -1 and no normalization happens, returns original
	assert.Contains(t, got, "\t")
}

func TestCleanCommitMessage_WhitespaceOnlyWithNewlines(t *testing.T) {
	// Empty/whitespace input is not JSON/mardown — passes through
	got := CleanCommitMessage("\n\n\n  \t  \n\n")
	// Has multiple lines, but none are non-empty, so no normalization occurs
	assert.NotEmpty(t, got)
}

func TestCleanCommitMessage_JSONFunctionCallNonMapParams(t *testing.T) {
	// Function call format where parameters is not a map (e.g., a string)
	input := `{"type": "function", "name": "generateCommitMessage", "parameters": "not a map"}`
	got := CleanCommitMessage(input)
	// Fails type assertion for map[string]interface{}, falls through to multi-key JSON fallback
	assert.True(t, len(got) > 10)
}

func TestCleanCommitMessage_JSONFunctionCallParamsIsNil(t *testing.T) {
	// Function call format where parameters key exists but is null
	input := `{"type": "function", "name": "generateCommitMessage", "parameters": null}`
	got := CleanCommitMessage(input)
	// parameters is nil, type assertion fails, falls to multi-key fallback
	assert.True(t, len(got) > 10)
}

func TestCleanCommitMessage_JSONFunctionCallNoParamsKey(t *testing.T) {
	// Function call format but missing "parameters" key entirely
	input := `{"type": "function", "name": "generateCommitMessage"}`
	got := CleanCommitMessage(input)
	// params doesn't exist, type assertion fails, falls to multi-key fallback
	assert.True(t, len(got) > 10)
}

func TestCleanCommitMessage_JSONSingleKeyNonStringValue(t *testing.T) {
	// Single key with non-string value (number)
	input := `{"Feature": 123}`
	got := CleanCommitMessage(input)
	// descStr type assertion to string fails, falls to JSON fallback
	assert.True(t, len(got) > 10)
}

func TestCleanCommitMessage_MultipleBlankLinesWithEmptyLinesBetween(t *testing.T) {
	input := "feat: title\n\n\n\n\ndescription here"
	got := CleanCommitMessage(input)
	parts := strings.SplitN(got, "\n\n", 2)
	assert.Equal(t, 2, len(parts))
	assert.Equal(t, "feat: title", parts[0])
	assert.Equal(t, "description here", parts[1])
}

func TestCleanCommitMessage_MultilineWithOnlyTitle(t *testing.T) {
	// 2 lines: title + one blank line (no description after)
	input := "feat: title\n\n"
	got := CleanCommitMessage(input)
	// len(lines) = 2, not > 2, so the normalization block is skipped
	assert.Equal(t, "feat: title\n\n", got)
}

func TestCleanCommitMessage_MarkdownFencesGitLangOnSingleLine(t *testing.T) {
	// Fences with "git\n" on same line as opening backticks but newlines before content
	input := "```git\nfeat: add feature\n\ndescription\n```"
	got := CleanCommitMessage(input)
	assert.NotContains(t, got, "```")
	assert.NotContains(t, got, "git\n")
	assert.Contains(t, got, "feat: add feature")
}

func TestCleanCommitMessage_JSONSingleKeyImprove(t *testing.T) {
	// "improve" matches the "enhance" check
	input := `{"Improve caching": "Add Redis caching layer"}`
	got := CleanCommitMessage(input)
	assert.Contains(t, got, "[*]")
	assert.Contains(t, got, "enhance:")
	assert.Contains(t, got, "Improve caching")
}

func TestCleanCommitMessage_JSONBracesButNotJSON(t *testing.T) {
	// Starts with '{' but does NOT end with '}', so the JSON branch is skipped.
	input := `{something} and more text`
	got := CleanCommitMessage(input)
	assert.Equal(t, "{something} and more text", got)
}

// =============================================================================
// AddAndCommitFile — add succeeds but commit fails (already tracked unchanged file)
// =============================================================================

func TestAddAndCommitFile_AlreadyCommittedUnchanged(t *testing.T) {
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Add init.go which is already committed and unchanged
	err = AddAndCommitFile("init.go", "should fail - nothing to commit")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error committing changes to git")
}

func TestAddAndCommitFile_CommitSuccess(t *testing.T) {
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	fp := filepath.Join(dir, "new_commit.go")
	require.NoError(t, os.WriteFile(fp, []byte("package new\n"), 0644))

	err = AddAndCommitFile("new_commit.go", "add new_commit.go")
	assert.NoError(t, err)

	out, _ := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%s").CombinedOutput()
	assert.Equal(t, "add new_commit.go\n", string(out))
}

// =============================================================================
// AddAllAndCommit — short timeout success
// =============================================================================

func TestAddAllAndCommit_ShortTimeoutSuccess(t *testing.T) {
	// Verifies that AddAllAndCommit succeeds normally when git completes
	// well within the timeout. A true timeout/kill test would require
	// injecting a slow operation (e.g., a git hook that sleeps).
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "to.go"), []byte("package to\n"), 0644))
	gitRun(t, dir, "add", "to.go")

	err = AddAllAndCommit("timeout test", 1)
	assert.NoError(t, err)
}

// =============================================================================
// GetGitRemoteURL — non-origin remote with broken URL
// =============================================================================

func TestGetGitRemoteURL_RemoteWithURL(t *testing.T) {
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	gitRun(t, dir, "remote", "add", "myremote", "https://github.com/example/repo.git")

	url, err := GetGitRemoteURL()
	assert.NoError(t, err)
	assert.Equal(t, "https://github.com/example/repo.git", url)
}

func TestGetGitRemoteURL_MultipleRemotes(t *testing.T) {
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	gitRun(t, dir, "remote", "add", "origin", "https://github.com/origin/repo.git")
	gitRun(t, dir, "remote", "add", "upstream", "https://github.com/upstream/repo.git")

	url, err := GetGitRemoteURL()
	assert.NoError(t, err)
	// Should get origin's URL
	assert.Equal(t, "https://github.com/origin/repo.git", url)
}

// =============================================================================
// GetGitStatus — untracked files
// =============================================================================

func TestGetGitStatus_UntrackedFiles(t *testing.T) {
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Create untracked file — should not appear as staged or uncommitted
	require.NoError(t, os.WriteFile(filepath.Join(dir, "untracked.go"), []byte("package untracked\n"), 0644))

	branch, uncommitted, staged, err := GetGitStatus()
	require.NoError(t, err)
	assert.Equal(t, "main", branch)
	assert.Equal(t, 0, uncommitted)
	assert.Equal(t, 0, staged)
}

func TestGetGitStatus_OnlyStagedChanges(t *testing.T) {
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Stage a new file — it's staged but not "uncommitted" (no working tree modification)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "staged_only.go"), []byte("package staged\n"), 0644))
	gitRun(t, dir, "add", "staged_only.go")

	branch, uncommitted, staged, err := GetGitStatus()
	require.NoError(t, err)
	assert.Equal(t, "main", branch)
	assert.Equal(t, 0, uncommitted)
	assert.GreaterOrEqual(t, staged, 1)
}

// =============================================================================
// GetStagedChanges truncation for large staged diffs
// =============================================================================

func TestGetStagedChanges_Truncation(t *testing.T) {
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Create a large file, commit it, then stage a completely new version
	var lines []string
	for i := 0; i < 300; i++ {
		lines = append(lines, fmt.Sprintf("line %03d: original content for the staged diff truncation test\n", i))
	}
	bigContent := strings.Join(lines, "")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big_stage.go"), []byte(bigContent), 0644))
	gitRun(t, dir, "add", "big_stage.go")
	gitRun(t, dir, "commit", "-m", "add big_stage.go")

	// Now modify all lines to create a large staged diff
	var modifiedLines []string
	for i := 0; i < 300; i++ {
		modifiedLines = append(modifiedLines, fmt.Sprintf("LINE %03d: MODIFIED content for the staged diff truncation test\n", i))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big_stage.go"), []byte(strings.Join(modifiedLines, "")), 0644))
	gitRun(t, dir, "add", "big_stage.go")

	diff, err := GetStagedChanges()
	require.NoError(t, err)
	assert.Contains(t, diff, "truncated for brevity")
	assert.LessOrEqual(t, len(diff), 5100) // 5000 + truncation message
}

func TestGetStagedChanges_EmptyStaged(t *testing.T) {
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	diff, err := GetStagedChanges()
	require.NoError(t, err)
	assert.Equal(t, "", diff)
}

// =============================================================================
// GetUncommittedChanges — large diffs
// =============================================================================

func TestGetUncommittedChanges_VeryLargeDiff(t *testing.T) {
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Create a very large file to guarantee truncation
	var lines []string
	for i := 0; i < 500; i++ {
		lines = append(lines, fmt.Sprintf("original line %04d of content for uncommitted changes test\n", i))
	}
	bigContent := strings.Join(lines, "")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "huge.go"), []byte(bigContent), 0644))
	gitRun(t, dir, "add", "huge.go")
	gitRun(t, dir, "commit", "-m", "add huge.go")

	// Replace every line entirely
	var modifiedLines []string
	for i := 0; i < 500; i++ {
		modifiedLines = append(modifiedLines, fmt.Sprintf("MODIFIED LINE %04d of replacement content for uncommitted test\n", i))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "huge.go"), []byte(strings.Join(modifiedLines, "")), 0644))

	diff, err := GetUncommittedChanges()
	require.NoError(t, err)
	assert.Contains(t, diff, "truncated for brevity")
}

// =============================================================================
// GetRecentFileLog — file with no commits (no log output at all)
// =============================================================================

func TestGetRecentFileLog_EmptyLog(t *testing.T) {
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Ask about a file that exists but has no commits touching it
	log, err := GetRecentFileLog("nonexistent_ever.go", 3)
	require.NoError(t, err)
	assert.Equal(t, "(no recent commits)", log)
}

func TestGetRecentFileLog_NegativeLimit(t *testing.T) {
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Negative limit should be normalized to 3
	log, err := GetRecentFileLog("init.go", -5)
	require.NoError(t, err)
	assert.NotEmpty(t, log)
}

// =============================================================================
// GetRecentTouchedFiles —
// =============================================================================

func TestGetRecentTouchedFiles_NegativeCount(t *testing.T) {
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Negative count should be normalized to 5
	files, err := GetRecentTouchedFiles(-1)
	require.NoError(t, err)
	assert.NotNil(t, files)
}

func TestGetRecentTouchedFiles_DeDuplication(t *testing.T) {
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Commit the same file twice — should only appear once
	for i := 0; i < 3; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "dup.go"),
			[]byte(fmt.Sprintf("package dup\nvar v=%d\n", i)), 0644))
		gitRun(t, dir, "add", "dup.go")
		gitRun(t, dir, "commit", "-m", fmt.Sprintf("update dup %d", i))
	}

	files, err := GetRecentTouchedFiles(3)
	require.NoError(t, err)
	// dup.go should appear only once despite being in multiple commits
	count := 0
	for _, f := range files {
		if f == "dup.go" {
			count++
		}
	}
	assert.Equal(t, 1, count, "dup.go should appear exactly once due to de-duplication")
}

func TestGetRecentTouchedFiles_EmptyRepo(t *testing.T) {
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// The test repo has one commit with init.go, so this should succeed
	files, err := GetRecentTouchedFiles(1)
	require.NoError(t, err)
	assert.Contains(t, files, "init.go")
}

// =============================================================================
// ParseCommitMessage — extra edge cases
// =============================================================================

func TestParseCommitMessage_SingleLineOnly(t *testing.T) {
	_, _, err := ParseCommitMessage("just a title")
	assert.Error(t, err)
}

func TestParseCommitMessage_EmptyString(t *testing.T) {
	_, _, err := ParseCommitMessage("")
	assert.Error(t, err)
}

func TestParseCommitMessage_TitleAndEmptyDescription(t *testing.T) {
	note, desc, err := ParseCommitMessage("feat: title\n\n")
	require.NoError(t, err)
	assert.Equal(t, "feat: title", note)
	assert.Equal(t, "", desc)
}

func TestParseCommitMessage_MultilineDescription(t *testing.T) {
	input := "feat: add auth\n\nLine 1\nLine 2\nLine 3"
	note, desc, err := ParseCommitMessage(input)
	require.NoError(t, err)
	assert.Equal(t, "feat: add auth", note)
	assert.Equal(t, "Line 1\nLine 2\nLine 3", desc)
}

// =============================================================================
// ExecuteCommit — fresh repo with no prior commits (HEAD fallback path)
// =============================================================================

func TestExecuteCommit_BranchFallback(t *testing.T) {
	// Create a bare git repo without any commits
	dir, err := os.MkdirTemp("", "ledit-no-commit-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	// Init without the initial commit
	cmd := exec.Command("git", "-C", dir, "init")
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "-C", dir, "config", "user.email", "test@test.com")
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "-C", dir, "config", "user.name", "Test")
	require.NoError(t, cmd.Run())

	// Don't create any commits — HEAD doesn't exist
	// Stage a file
	require.NoError(t, os.WriteFile(filepath.Join(dir, "first.go"), []byte("package first\n"), 0644))
	gitRun(t, dir, "add", "first.go")

	executor := NewCommitExecutorInDir(nil, "first commit", "", dir)
	hash, err := executor.ExecuteCommit()
	// This should succeed since the fallback branch logic handles missing HEAD
	// But actually git commit will create HEAD, and the branch fallback uses
	// symbolic-ref which also fails on fresh repos, falling back to "main"
	require.NoError(t, err)
	assert.NotEmpty(t, hash)

	// Verify the commit was created
	cmd = exec.Command("git", "-C", dir, "log", "-1", "--pretty=%s")
	out, _ := cmd.CombinedOutput()
	assert.Contains(t, string(out), "first commit")
}

// =============================================================================
// WrapText — line length exactly at boundaries
// =============================================================================

func TestWrapText_ExactBoundary(t *testing.T) {
	// "hello" is 5 chars, lineLength=5 → fits
	got := WrapText("hello", 5)
	assert.Equal(t, "hello", got)

	// "hello world" with lineLength=5 → "hello" (5) fits, " world" (6) doesn't fit
	got = WrapText("hello world", 5)
	assert.Equal(t, "hello\nworld", got)
}

func TestWrapText_TwoWordsExactLength(t *testing.T) {
	// "hi go" = 2+1+2 = 5, lineLength=5 → fits
	got := WrapText("hi go", 5)
	assert.Equal(t, "hi go", got)
}

func TestWrapText_LineLengthExactlyMatchesWord(t *testing.T) {
	got := WrapText("hello", 5)
	assert.Equal(t, "hello", got)
}

func TestWrapText_EmptyParagraphsBetween(t *testing.T) {
	// "First\n\n\nThird" splits on "\n\n" → ["First", "\nThird"]
	// The "\nThird" paragraph: TrimSpace gets "Third" → wrapped single word
	// Join with "\n\n" → "First\n\nThird"
	got := WrapText("First\n\n\nThird", 10)
	assert.Equal(t, "First\n\nThird", got)
}

func TestWrapText_TabAndNewlineInParagraph(t *testing.T) {
	// Tabs and newlines inside a paragraph are treated as whitespace by Fields
	got := WrapText("hello\tworld\nagain", 72)
	assert.Equal(t, "hello world again", got)
}

// =============================================================================
// TruncateRunes — additional edge cases
// =============================================================================

func TestTruncateRunes_Max1(t *testing.T) {
	// max=1 → max<=3, returns first rune
	got := TruncateRunes("abcde", 1)
	assert.Equal(t, "a", got)
}

func TestTruncateRunes_Max2(t *testing.T) {
	// max=2 → max<=3, returns first 2 runes
	got := TruncateRunes("abcde", 2)
	assert.Equal(t, "ab", got)
}

func TestTruncateRunes_Max3Exact(t *testing.T) {
	// max=3 → max<=3, returns first 3 runes (no ellipsis)
	got := TruncateRunes("abcde", 3)
	assert.Equal(t, "abc", got)
}

func TestTruncateRunes_EmptyString(t *testing.T) {
	got := TruncateRunes("", 5)
	assert.Equal(t, "", got)
}

func TestTruncateRunes_WhitespaceTrimBeforeEllipsis(t *testing.T) {
	// "ab cd" max=4 → runes[:1]="a" → TrimSpace→"a" + "..." → "a..."
	got := TruncateRunes("ab cd", 4)
	assert.Equal(t, "a...", got)
}

// =============================================================================
// NormalizeShortTitle — more edge cases
// =============================================================================

func TestNormalizeShortTitle_TwoNewlines(t *testing.T) {
	got := NormalizeShortTitle("Title\nSecond line\nThird")
	assert.Equal(t, "Title", got)
}

func TestNormalizeShortTitle_TitleAndBacktickPrefix(t *testing.T) {
	// "title: `Hello`" → TrimPrefix("title:") → "`Hello`" → Trim(backtick) → "`Hello"
	// The trailing backtick is stripped but the leading one (in the middle) is not
	got := NormalizeShortTitle("title: `Hello`")
	assert.Equal(t, "`Hello", got)
}

func TestNormalizeShortTitle_EmptyLines(t *testing.T) {
	got := NormalizeShortTitle("\n\n\n")
	assert.Equal(t, "", got)
}

// =============================================================================
// isDefaultBranch — more edge cases
// =============================================================================

func TestIsDefaultBranch_Empty(t *testing.T) {
	assert.False(t, isDefaultBranch(""))
}

func TestIsDefaultBranch_MainWithNewline(t *testing.T) {
	assert.True(t, isDefaultBranch("main\n"))
}

func TestIsDefaultBranch_MasterWithNewline(t *testing.T) {
	assert.True(t, isDefaultBranch("master\n"))
}

func TestIsDefaultBranch_WhitespaceMain(t *testing.T) {
	assert.True(t, isDefaultBranch("  main\n  "))
}

func TestIsDefaultBranch_MainDev(t *testing.T) {
	assert.False(t, isDefaultBranch("main-dev"))
}

func TestIsDefaultBranch_MainInName(t *testing.T) {
	assert.False(t, isDefaultBranch("my-main-branch"))
}

func TestIsDefaultBranch_DevExact(t *testing.T) {
	assert.True(t, isDefaultBranch("dev"))
}

// =============================================================================
// actionFromStatus — whitespace edge cases
// =============================================================================

func TestActionFromStatus_TabPrefix(t *testing.T) {
	assert.Equal(t, "Adds", actionFromStatus("\tA"))
}

func TestActionFromStatus_MultipleWhitespace(t *testing.T) {
	assert.Equal(t, "Deletes", actionFromStatus("  \t D  \t "))
}

func TestActionFromStatus_Empty(t *testing.T) {
	assert.Equal(t, "Updates", actionFromStatus(""))
}

func TestActionFromStatus_StatusR(t *testing.T) {
	assert.Equal(t, "Renames", actionFromStatus("R"))
}

// =============================================================================
// parseStagedFileChanges — path with spaces, edge cases
// =============================================================================

func TestParseStagedFileChanges_PathWithSpacesTab(t *testing.T) {
	input := "M\tpath with spaces/file.go"
	result := parseStagedFileChanges(input)
	require.Len(t, result, 1)
	assert.Equal(t, "M", result[0].Status)
	assert.Equal(t, "path with spaces/file.go", result[0].Path)
}

func TestParseStagedFileChanges_NonStandardStatus(t *testing.T) {
	// Unmerged status codes
	input := "DU\tconflict.go\nUD\tother.go"
	result := parseStagedFileChanges(input)
	require.Len(t, result, 2)
	assert.Equal(t, "DU", result[0].Status)
	assert.Equal(t, "UD", result[1].Status)
}

// =============================================================================
// generateFallbackCommitMessage — unknown statuses
// =============================================================================

func TestGenerateFallbackCommitMessage_CopyAndTypeChange(t *testing.T) {
	// C and T are not A or D, so they fall to the default "modified" bucket
	changes := []CommitFileChange{
		{Status: "C", Path: "copied.go"},
		{Status: "T", Path: "typechange.go"},
	}
	result := generateFallbackCommitMessage(changes)
	assert.Contains(t, result, "Update 2 Files")
}

func TestGenerateFallbackCommitMessage_SingleDeletedFile(t *testing.T) {
	changes := []CommitFileChange{{Status: "D", Path: "removed.go"}}
	result := generateFallbackCommitMessage(changes)
	assert.Equal(t, "Update removed.go", result)
}

func TestGenerateFallbackCommitMessage_SingleAddedFile(t *testing.T) {
	changes := []CommitFileChange{{Status: "A", Path: "new.go"}}
	result := generateFallbackCommitMessage(changes)
	assert.Equal(t, "Update new.go", result)
}

// =============================================================================
// NewCommitExecutor — constructors
// =============================================================================

func TestNewCommitExecutor_Fields(t *testing.T) {
	e := NewCommitExecutor(nil, "msg", "instr")
	assert.NotNil(t, e)
	assert.Equal(t, "msg", e.UserMessage)
	assert.Equal(t, "instr", e.UserInstructions)
	assert.Equal(t, "", e.Dir)
	assert.Nil(t, e.Client)
}

func TestNewCommitExecutorInDir_Fields(t *testing.T) {
	e := NewCommitExecutorInDir(nil, "m", "i", "/tmp")
	assert.NotNil(t, e)
	assert.Equal(t, "m", e.UserMessage)
	assert.Equal(t, "i", e.UserInstructions)
	assert.Equal(t, "/tmp", e.Dir)
}

// =============================================================================
// PerformGitCommit — success path verification
// =============================================================================

func TestPerformGitCommit_Success(t *testing.T) {
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "perf.go"), []byte("package perf\n"), 0644))
	gitRun(t, dir, "add", "perf.go")

	err = PerformGitCommit("perform test success")
	assert.NoError(t, err)

	out, _ := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%s").CombinedOutput()
	assert.Equal(t, "perform test success\n", string(out))
}

// =============================================================================
// CheckStagedDiff — success and empty paths
// =============================================================================

func TestGetStagedDiff_Success(t *testing.T) {
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "sd.go"), []byte("package sd\n"), 0644))
	gitRun(t, dir, "add", "sd.go")

	diff, err := GetStagedDiff()
	require.NoError(t, err)
	assert.NotEmpty(t, diff)
	assert.Contains(t, diff, "sd.go")
}

// =============================================================================
// Compile-time accessibility checks
// =============================================================================

var (
	_ func(string) string                                     = CleanCommitMessage
	_ func(string) (string, string, error)                    = ParseCommitMessage
	_ func() (string, error)                                  = GetGitRootDir
	_ func(string) (string, error)                            = GetFileGitPath
	_ func() (string, error)                                  = GetGitRemoteURL
	_ func() (string, int, int, error)                        = GetGitStatus
	_ func(int) ([]string, error)                             = GetRecentTouchedFiles
	_ func(string, int) (string, error)                       = GetRecentFileLog
	_ func(string, string) error                              = AddAndCommitFile
	_ func(string, int) error                                 = AddAllAndCommit
	_ func() (string, error)                                  = GetUncommittedChanges
	_ func() (string, error)                                  = GetStagedChanges
	_ func() error                                            = CheckStagedChanges
	_ func() (string, error)                                  = GetStagedDiff
	_ func(string) error                                      = PerformGitCommit
	_ func([]CommitFileChange) string                         = generateFallbackCommitMessage
	_ func(string) string                                     = actionFromStatus
	_ func(string) bool                                       = isDefaultBranch
	_ func(string) string                                     = NormalizeShortTitle
	_ func(string, int) string                                = TruncateRunes
	_ func(string, int) string                                = WrapText
	_ func(api.ClientInterface, CommitMessageOptions) (*CommitMessageResult, error) = GenerateCommitMessageFromStagedDiff
)
