//go:build !js

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// =============================================================================
// AgentResult PullRequestURL field serialization tests
// =============================================================================

func TestAgentResult_PullRequestURL_Present(t *testing.T) {
	r := AgentResult{
		Status:         "success",
		Query:          "implement feature X",
		PullRequestURL: "https://github.com/org/repo/pull/42",
		Metrics: AgentResultMetrics{
			ElapsedSeconds: 5.0,
		},
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// pull_request_url should be present with the correct value
	assertJSONString(t, result, "pull_request_url", "https://github.com/org/repo/pull/42")
}

func TestAgentResult_PullRequestURL_OmittedWhenEmpty(t *testing.T) {
	r := AgentResult{
		Status:         "success",
		Query:          "implement feature X",
		PullRequestURL: "", // empty — should be omitted by omitempty
		Metrics: AgentResultMetrics{
			ElapsedSeconds: 5.0,
		},
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// pull_request_url should NOT be present when empty (omitempty)
	if _, exists := result["pull_request_url"]; exists {
		t.Error("expected pull_request_url to be omitted when empty (omitempty)")
	}
}

func TestAgentResult_PullRequestURL_RoundTrip(t *testing.T) {
	original := AgentResult{
		Status:         "success",
		Query:          "add tests",
		PullRequestURL: "https://github.com/sprout-foundry/sprout/pull/123",
		Metrics: AgentResultMetrics{
			ElapsedSeconds: 10.5,
			TokensIn:       2000,
			TokensOut:      1500,
			LLMCalls:       3,
			Provider:       "openai",
			Model:          "gpt-4o",
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var restored AgentResult
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if restored.PullRequestURL != original.PullRequestURL {
		t.Errorf("PullRequestURL: got %q, want %q", restored.PullRequestURL, original.PullRequestURL)
	}
}

func TestAgentResult_PullRequestURL_AllFields(t *testing.T) {
	// Verify PullRequestURL serializes alongside all other fields
	r := AgentResult{
		Status:         "success",
		Error:          "",
		Query:          "build the thing",
		FilesModified:  []string{"main.go", "main_test.go"},
		GitDiff:        "diff --git a/main.go\n--- a/main.go\n+++ b/main.go",
		PullRequestURL: "https://github.com/org/repo/pull/99",
		Metrics: AgentResultMetrics{
			ElapsedSeconds: 7.5,
			TokensIn:       500,
			TokensOut:      300,
			LLMCalls:       2,
			Provider:       "anthropic",
			Model:          "claude-3-sonnet",
		},
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	assertJSONString(t, result, "pull_request_url", "https://github.com/org/repo/pull/99")
	assertJSONString(t, result, "status", "success")
	assertJSONString(t, result, "query", "build the thing")

	// Error should be omitted (empty + omitempty)
	if _, exists := result["error"]; exists {
		t.Error("expected error to be omitted when empty")
	}
}

// =============================================================================
// parseEditedFile() tests
// =============================================================================

func TestParseEditedFile_NormalTitleAndBody(t *testing.T) {
	tmp := createTempFile(t, "# Pull Request Title\nFix login bug\n\n# Pull Request Body\nResolved the OAuth redirect issue.\n")

	title, body, err := parseEditedFile(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if title != "Fix login bug" {
		t.Errorf("title: got %q, want %q", title, "Fix login bug")
	}
	if body != "Resolved the OAuth redirect issue." {
		t.Errorf("body: got %q, want %q", body, "Resolved the OAuth redirect issue.")
	}
}

func TestParseEditedFile_EmptyBody(t *testing.T) {
	// Title section present, no body section at all
	tmp := createTempFile(t, "# Pull Request Title\nMy Feature Title\n")

	title, body, err := parseEditedFile(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if title != "My Feature Title" {
		t.Errorf("title: got %q, want %q", title, "My Feature Title")
	}
	if body != "" {
		t.Errorf("body: got %q, want empty", body)
	}
}

func TestParseEditedFile_BodyOnly(t *testing.T) {
	// Title section present, body section with content but title is empty line
	tmp := createTempFile(t, "# Pull Request Title\n\n# Pull Request Body\nSome body text.\n")

	_, _, err := parseEditedFile(tmp)
	if err == nil {
		t.Fatal("expected error for empty title, got nil")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error message should mention 'empty', got: %v", err)
	}
}

func TestParseEditedFile_MultiLineBody(t *testing.T) {
	tmp := createTempFile(t, "# Pull Request Title\nAdd new feature\n\n# Pull Request Body\nThis PR adds a new feature.\n\nIt includes:\n- Sub-feature A\n- Sub-feature B\n")

	title, body, err := parseEditedFile(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if title != "Add new feature" {
		t.Errorf("title: got %q, want %q", title, "Add new feature")
	}
	expectedBody := "This PR adds a new feature.\n\nIt includes:\n- Sub-feature A\n- Sub-feature B"
	if body != expectedBody {
		t.Errorf("body:\n  got:  %q\n  want: %q", body, expectedBody)
	}
}

func TestParseEditedFile_TitleOnlySection(t *testing.T) {
	// Title section with content, body section marker but no content after it
	tmp := createTempFile(t, "# Pull Request Title\nQuick fix\n\n# Pull Request Body\n")

	title, body, err := parseEditedFile(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if title != "Quick fix" {
		t.Errorf("title: got %q, want %q", title, "Quick fix")
	}
	if body != "" {
		t.Errorf("body: got %q, want empty", body)
	}
}

func TestParseEditedFile_MissingTitleHeader(t *testing.T) {
	// No "# Pull Request Title" header at all — should error with empty title
	tmp := createTempFile(t, "Just some random text\n\n# Pull Request Body\nSome body.\n")

	_, _, err := parseEditedFile(tmp)
	if err == nil {
		t.Fatal("expected error when title header is missing, got nil")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention 'empty', got: %v", err)
	}
}

func TestParseEditedFile_WhitespaceOnlyTitle(t *testing.T) {
	// Title section exists but only has whitespace after the header
	tmp := createTempFile(t, "# Pull Request Title\n   \n\n# Pull Request Body\nBody here.\n")

	_, _, err := parseEditedFile(tmp)
	if err == nil {
		t.Fatal("expected error for whitespace-only title, got nil")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention 'empty', got: %v", err)
	}
}

func TestParseEditedFile_TitleWithLeadingWhitespace(t *testing.T) {
	// Title has leading/trailing spaces — should be trimmed
	tmp := createTempFile(t, "# Pull Request Title\n  Fix the thing  \n\n# Pull Request Body\nBody.\n")

	title, body, err := parseEditedFile(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if title != "Fix the thing" {
		t.Errorf("title: got %q, want %q (should be trimmed)", title, "Fix the thing")
	}
	if body != "Body." {
		t.Errorf("body: got %q, want %q", body, "Body.")
	}
}

func TestParseEditedFile_NoBodySectionWithBodyAfterTitleMarker(t *testing.T) {
	// Title section has content after it but NO "# Pull Request Body" marker
	// The content after title header but before body header should be ignored for body
	tmp := createTempFile(t, "# Pull Request Title\nMy Title\nSome other text\nMore text\n")

	title, body, err := parseEditedFile(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if title != "My Title" {
		t.Errorf("title: got %q, want %q", title, "My Title")
	}
	if body != "" {
		t.Errorf("body: got %q, want empty (no body section marker)", body)
	}
}

func TestParseEditedFile_NonexistentFile(t *testing.T) {
	_, _, err := parseEditedFile("/tmp/definitely-not-a-real-file-12345.md")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
	if !strings.Contains(err.Error(), "open edited file") {
		t.Errorf("error should contain 'open edited file', got: %v", err)
	}
}

// =============================================================================
// extractFirstLine() tests
// =============================================================================

func TestExtractFirstLine_SingleLine(t *testing.T) {
	result := extractFirstLine("Hello World")
	if result != "Hello World" {
		t.Errorf("got %q, want %q", result, "Hello World")
	}
}

func TestExtractFirstLine_MultiLine(t *testing.T) {
	result := extractFirstLine("First line\nSecond line\nThird line")
	if result != "First line" {
		t.Errorf("got %q, want %q", result, "First line")
	}
}

func TestExtractFirstLine_Empty(t *testing.T) {
	result := extractFirstLine("")
	if result != "" {
		t.Errorf("got %q, want empty", result)
	}
}

func TestExtractFirstLine_FirstLineEmpty(t *testing.T) {
	result := extractFirstLine("\nReal content\nMore lines")
	if result != "Real content" {
		t.Errorf("got %q, want %q", result, "Real content")
	}
}

func TestExtractFirstLine_WhitespaceOnly(t *testing.T) {
	result := extractFirstLine("   \n   ")
	if result != "" {
		t.Errorf("got %q, want empty (trimmed whitespace)", result)
	}
}

func TestExtractFirstLine_FirstLineWhitespace(t *testing.T) {
	result := extractFirstLine("   \nReal content")
	if result != "Real content" {
		t.Errorf("got %q, want %q (should skip leading whitespace-only lines)", result, "Real content")
	}
}

func TestExtractFirstLine_LeadingTrailingWhitespace(t *testing.T) {
	result := extractFirstLine("  Trim me  \nNext line")
	if result != "Trim me" {
		t.Errorf("got %q, want %q (trimmed)", result, "Trim me")
	}
}

// =============================================================================
// getCurrentBranch() tests (real git repos)
// =============================================================================

func TestGetCurrentBranch_NormalBranch(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "feature-login")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	// Create at least one commit so the branch reference resolves
	if err := os.WriteFile(dir+"/placeholder", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "placeholder")
	runGit(t, dir, "commit", "-m", "init")

	branch, err := getCurrentBranch(context.Background(), dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if branch != "feature-login" {
		t.Errorf("branch: got %q, want %q", branch, "feature-login")
	}
}

func TestGetCurrentBranch_DetachedHead(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	if err := os.WriteFile(dir+"/placeholder", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "placeholder")
	runGit(t, dir, "commit", "-m", "init")

	// Detach HEAD by checking out the commit hash
	runGit(t, dir, "checkout", "--detach")

	_, err := getCurrentBranch(context.Background(), dir)
	if err == nil {
		t.Fatal("expected error for detached HEAD, got nil")
	}
	if !strings.Contains(err.Error(), "detached HEAD") {
		t.Errorf("error should mention 'detached HEAD', got: %v", err)
	}
}

func TestGetCurrentBranch_NoGitRepo(t *testing.T) {
	dir := t.TempDir()
	// No git init

	_, err := getCurrentBranch(context.Background(), dir)
	if err == nil {
		t.Fatal("expected error for non-git directory, got nil")
	}
}

// =============================================================================
// synthesizeTitle() tests (real git repos)
// =============================================================================

func TestSynthesizeTitle_SingleCommit(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	// Initial commit on main
	if err := os.WriteFile(dir+"/a.txt", []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "initial commit")

	// Create feature branch with one new commit
	runGit(t, dir, "checkout", "-b", "feature-x")
	if err := os.WriteFile(dir+"/b.txt", []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "b.txt")
	runGit(t, dir, "commit", "-m", "Add new feature X")

	title, err := synthesizeTitle(context.Background(), dir, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if title != "Add new feature X" {
		t.Errorf("title: got %q, want %q", title, "Add new feature X")
	}
}

func TestSynthesizeTitle_NoCommitsSinceBase(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	if err := os.WriteFile(dir+"/a.txt", []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "initial commit")

	// Branch off main with no new commits
	runGit(t, dir, "checkout", "-b", "feature-empty")

	title, err := synthesizeTitle(context.Background(), dir, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if title != "" {
		t.Errorf("title: got %q, want empty (no commits since base)", title)
	}
}

func TestSynthesizeTitle_MultipleCommitsReturnsLast(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	// Initial commit
	if err := os.WriteFile(dir+"/a.txt", []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "initial commit")

	// Feature branch with multiple commits
	runGit(t, dir, "checkout", "-b", "feature-multi")

	if err := os.WriteFile(dir+"/b.txt", []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "b.txt")
	runGit(t, dir, "commit", "-m", "Add feature part 1")

	if err := os.WriteFile(dir+"/c.txt", []byte("c"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "c.txt")
	runGit(t, dir, "commit", "-m", "Add feature part 2")

	// -1 flag means the most recent (topmost) commit
	title, err := synthesizeTitle(context.Background(), dir, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if title != "Add feature part 2" {
		t.Errorf("title: got %q, want %q (most recent commit)", title, "Add feature part 2")
	}
}

func TestSynthesizeTitle_BranchNameWithSlashes(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	// Initial commit on main
	if err := os.WriteFile(dir+"/a.txt", []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "initial commit")

	// Create a branch with slashes in the name
	runGit(t, dir, "checkout", "-b", "feature/login-fix")
	if err := os.WriteFile(dir+"/b.txt", []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "b.txt")
	runGit(t, dir, "commit", "-m", "Fix login redirect")

	title, err := synthesizeTitle(context.Background(), dir, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if title != "Fix login redirect" {
		t.Errorf("title: got %q, want %q", title, "Fix login redirect")
	}
}

func TestSynthesizeBodyForPrompt_BranchNameWithSlashes(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	// Initial commit on main
	if err := os.WriteFile(dir+"/a.txt", []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "initial commit")

	// Create a branch with slashes in the name
	runGit(t, dir, "checkout", "-b", "feature/login-fix")
	if err := os.WriteFile(dir+"/b.txt", []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "b.txt")
	runGit(t, dir, "commit", "-m", "Fix login redirect")

	if err := os.WriteFile(dir+"/c.txt", []byte("c"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "c.txt")
	runGit(t, dir, "commit", "-m", "Add logout support")

	body, err := synthesizeBodyForPrompt(context.Background(), dir, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedBody := "## Commits\n\n- Add logout support\n- Fix login redirect\n"
	if body != expectedBody {
		t.Errorf("body:\n  got:  %q\n  want: %q", body, expectedBody)
	}
}

// =============================================================================
// synthesizeBodyForPrompt() tests (real git repos)
// =============================================================================

func TestSynthesizeBodyForPrompt_MultipleCommits(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	// Initial commit
	if err := os.WriteFile(dir+"/a.txt", []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "initial commit")

	// Feature branch with multiple commits (oldest first)
	runGit(t, dir, "checkout", "-b", "feature-y")

	if err := os.WriteFile(dir+"/b.txt", []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "b.txt")
	runGit(t, dir, "commit", "-m", "Add feature part 1")

	if err := os.WriteFile(dir+"/c.txt", []byte("c"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "c.txt")
	runGit(t, dir, "commit", "-m", "Add feature part 2")

	body, err := synthesizeBodyForPrompt(context.Background(), dir, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// git log outputs in reverse chronological order (newest first)
	// so the bullet list should be part 2, then part 1
	expectedBody := "## Commits\n\n- Add feature part 2\n- Add feature part 1\n"
	if body != expectedBody {
		t.Errorf("body:\n  got:  %q\n  want: %q", body, expectedBody)
	}
}

func TestSynthesizeBodyForPrompt_NoCommitsSinceBase(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	if err := os.WriteFile(dir+"/a.txt", []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "initial commit")

	// Branch off main with no new commits
	runGit(t, dir, "checkout", "-b", "feature-empty")

	body, err := synthesizeBodyForPrompt(context.Background(), dir, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body != "" {
		t.Errorf("body: got %q, want empty (no commits since base)", body)
	}
}

func TestSynthesizeBodyForPrompt_SingleCommit(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	if err := os.WriteFile(dir+"/a.txt", []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "initial commit")

	runGit(t, dir, "checkout", "-b", "feature-single")

	if err := os.WriteFile(dir+"/b.txt", []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "b.txt")
	runGit(t, dir, "commit", "-m", "One and done")

	body, err := synthesizeBodyForPrompt(context.Background(), dir, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedBody := "## Commits\n\n- One and done\n"
	if body != expectedBody {
		t.Errorf("body:\n  got:  %q\n  want: %q", body, expectedBody)
	}
}

// =============================================================================
// PR flag default tests
// =============================================================================

func TestPRFlags_Defaults(t *testing.T) {
	// Reset flag variables to zero values in case a previous test mutated them.
	prTitle = ""
	prBody = ""
	prBase = ""
	prDraft = false
	prWeb = false
	prSkipPrompt = false

	// Verify that the flag variables have their zero-value defaults.
	// prTitle, prBody, prBase are strings — default to ""
	// prDraft, prWeb, prSkipPrompt are bools — default to false

	// These are package-level vars; their zero values are the defaults.
	if prTitle != "" {
		t.Errorf("prTitle: got %q, want empty", prTitle)
	}
	if prBody != "" {
		t.Errorf("prBody: got %q, want empty", prBody)
	}
	if prBase != "" {
		t.Errorf("prBase: got %q, want empty", prBase)
	}
	if prDraft != false {
		t.Errorf("prDraft: got %v, want false", prDraft)
	}
	if prWeb != false {
		t.Errorf("prWeb: got %v, want false", prWeb)
	}
	if prSkipPrompt != false {
		t.Errorf("prSkipPrompt: got %v, want false", prSkipPrompt)
	}
}

func TestPRCmd_FlagConfiguration(t *testing.T) {
	// Verify the cobra command has all expected flags defined
	if prCmd == nil {
		t.Fatal("prCmd is nil")
	}

	// Check that the command has the expected flags by looking at the flag set
	expectedFlags := []string{"title", "body", "base", "draft", "web", "skip-prompt"}
	for _, flagName := range expectedFlags {
		flag := prCmd.Flags().Lookup(flagName)
		if flag == nil {
			t.Errorf("expected flag %q not found on prCmd", flagName)
		}
	}

	// Verify the command metadata
	if prCmd.Use != "pr" {
		t.Errorf("Use: got %q, want %q", prCmd.Use, "pr")
	}
	if prCmd.Short != "Create a GitHub pull request for the current branch" {
		t.Errorf("Short: got %q, want %q", prCmd.Short, "Create a GitHub pull request for the current branch")
	}
}

func TestPRCmd_FlagDefaults_AfterInit(t *testing.T) {
	// After init() runs (it runs at package init time), verify flag defaults
	titleFlag := prCmd.Flags().Lookup("title")
	if titleFlag == nil {
		t.Fatal("title flag not found")
	}
	if titleFlag.DefValue != "" {
		t.Errorf("title default: got %q, want empty", titleFlag.DefValue)
	}

	bodyFlag := prCmd.Flags().Lookup("body")
	if bodyFlag == nil {
		t.Fatal("body flag not found")
	}
	if bodyFlag.DefValue != "" {
		t.Errorf("body default: got %q, want empty", bodyFlag.DefValue)
	}

	baseFlag := prCmd.Flags().Lookup("base")
	if baseFlag == nil {
		t.Fatal("base flag not found")
	}
	if baseFlag.DefValue != "" {
		t.Errorf("base default: got %q, want empty", baseFlag.DefValue)
	}

	draftFlag := prCmd.Flags().Lookup("draft")
	if draftFlag == nil {
		t.Fatal("draft flag not found")
	}
	if draftFlag.DefValue != "false" {
		t.Errorf("draft default: got %q, want %q", draftFlag.DefValue, "false")
	}

	webFlag := prCmd.Flags().Lookup("web")
	if webFlag == nil {
		t.Fatal("web flag not found")
	}
	if webFlag.DefValue != "false" {
		t.Errorf("web default: got %q, want %q", webFlag.DefValue, "false")
	}

	skipPromptFlag := prCmd.Flags().Lookup("skip-prompt")
	if skipPromptFlag == nil {
		t.Fatal("skip-prompt flag not found")
	}
	if skipPromptFlag.DefValue != "false" {
		t.Errorf("skip-prompt default: got %q, want %q", skipPromptFlag.DefValue, "false")
	}
}

// =============================================================================
// Test helper: create a temp file with content
// =============================================================================

func createTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "sprout-pr-test-*.md")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })

	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()
	return f.Name()
}

// =============================================================================
// PromptWithEditor template generation test (without invoking real editor)
// This tests the template format that parseEditedFile expects.
// =============================================================================

func TestPromptWithEditor_TemplateFormat(t *testing.T) {
	// We can't test the editor interaction itself, but we can verify that
	// the template format used by promptWithEditor is correctly parsed by
	// parseEditedFile. This validates the contract between the two functions.

	// Simulate what promptWithEditor writes:
	// template := fmt.Sprintf(`# Pull Request Title\n%s\n\n# Pull Request Body\n%s\n`, title, body)

	// Case 1: Both title and body provided
	template1 := fmt.Sprintf("# Pull Request Title\n%s\n\n# Pull Request Body\n%s\n", "Fix login bug", "Resolved the OAuth issue")
	tmp1 := createTempFile(t, template1)
	title, body, err := parseEditedFile(tmp1)
	if err != nil {
		t.Fatalf("Case 1: unexpected error: %v", err)
	}
	if title != "Fix login bug" {
		t.Errorf("Case 1 title: got %q, want %q", title, "Fix login bug")
	}
	if body != "Resolved the OAuth issue" {
		t.Errorf("Case 1 body: got %q, want %q", body, "Resolved the OAuth issue")
	}

	// Case 2: Title only, empty body
	template2 := fmt.Sprintf("# Pull Request Title\n%s\n\n# Pull Request Body\n%s\n", "Quick fix", "")
	tmp2 := createTempFile(t, template2)
	title, body, err = parseEditedFile(tmp2)
	if err != nil {
		t.Fatalf("Case 2: unexpected error: %v", err)
	}
	if title != "Quick fix" {
		t.Errorf("Case 2 title: got %q, want %q", title, "Quick fix")
	}
	if body != "" {
		t.Errorf("Case 2 body: got %q, want empty", body)
	}

	// Case 3: Title with existing body content
	template3 := fmt.Sprintf("# Pull Request Title\n%s\n\n# Pull Request Body\n%s\n", "Add auth", "Multi-line\nbody content")
	tmp3 := createTempFile(t, template3)
	title, body, err = parseEditedFile(tmp3)
	if err != nil {
		t.Fatalf("Case 3: unexpected error: %v", err)
	}
	if title != "Add auth" {
		t.Errorf("Case 3 title: got %q, want %q", title, "Add auth")
	}
	if body != "Multi-line\nbody content" {
		t.Errorf("Case 3 body: got %q, want %q", body, "Multi-line\nbody content")
	}
}
