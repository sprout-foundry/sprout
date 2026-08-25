package commands

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/history"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIsSlashCommand tests that both / and ! are recognized as slash commands
func TestIsSlashCommand(t *testing.T) {
	registry := NewCommandRegistry()

	testCases := []struct {
		name     string
		input    string
		expected bool
	}{
		{"forward slash command", "/exec ls", true},
		{"bang prefix as command", "!ls -la", true},
		{"bang with exec", "!exec ls", true},
		{"unix path", "/tmp/test.log", false},
		{"nested unix path with spaces later", "/tmp/test.log more output", false},
		{"windows style path after slash", "/tmp\\test.log", false},
		{"unknown slash command", "/unknown", true},
		{"regular text", "hello world", false},
		{"empty string", "", false},
		{"whitespace only", "   ", false},
		{"slash with whitespace", "   /exec ls   ", true},
		{"bang with whitespace", "   !ls -la   ", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := registry.IsSlashCommand(tc.input)
			if result != tc.expected {
				t.Errorf("IsSlashCommand(%q) = %v, expected %v", tc.input, result, tc.expected)
			}
		})
	}
}

// TestBangPrefixRouting tests that ! prefixes route to exec command
func TestBangPrefixRouting(t *testing.T) {
	registry := NewCommandRegistry()

	testCases := []struct {
		name         string
		input        string
		expectedCmd  string
		expectedArgs []string
	}{
		{"bang simple", "!ls", "exec", []string{"ls"}},
		{"bang with flags", "!ls -la", "exec", []string{"ls -la"}},
		{"bang with args", "!git status", "exec", []string{"git status"}},
		{"bang with quoted args", "!echo 'hello world'", "exec", []string{"echo 'hello world'"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// We can't easily test Execute because it requires a full Agent
			// But we can verify that ! prefixes are recognized as slash commands
			if !registry.IsSlashCommand(tc.input) {
				t.Errorf("Expected %q to be recognized as slash command", tc.input)
			}
		})
	}
}

// TestSlashPrefixStillWorks ensures / prefix still works as before
func TestSlashPrefixStillWorks(t *testing.T) {
	registry := NewCommandRegistry()

	testCases := []struct {
		name     string
		input    string
		expected bool
	}{
		{"slash help", "/help", true},
		{"slash models", "/models", true},
		{"slash exec", "/exec ls", true},
		{"slash commit", "/commit", true},
		{"slash path is not command", "/var/log/system.log", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := registry.IsSlashCommand(tc.input)
			if result != tc.expected {
				t.Errorf("IsSlashCommand(%q) = %v, expected %v", tc.input, result, tc.expected)
			}
		})
	}
}

// =====================================================================
// CommitCommand parseFlags Tests (0% coverage)
// =====================================================================

func TestBoost_ParseFlags_NoFlags(t *testing.T) {
	c := &CommitCommand{}
	result := c.parseFlags([]string{"fix", "bug"})
	assert.Equal(t, []string{"fix", "bug"}, result)
	assert.False(t, c.skipPrompt)
	assert.False(t, c.dryRun)
	assert.False(t, c.allowSecrets)
}

func TestBoost_ParseFlags_SkipPrompt(t *testing.T) {
	c := &CommitCommand{}
	result := c.parseFlags([]string{"--skip-prompt", "fix"})
	assert.Equal(t, []string{"fix"}, result)
	assert.True(t, c.skipPrompt)
}

func TestBoost_ParseFlags_DryRun(t *testing.T) {
	c := &CommitCommand{}
	result := c.parseFlags([]string{"--dry-run"})
	assert.Equal(t, []string(nil), result)
	assert.True(t, c.dryRun)
}

func TestBoost_ParseFlags_AllowSecrets(t *testing.T) {
	c := &CommitCommand{}
	result := c.parseFlags([]string{"--allow-secrets", "msg"})
	assert.Equal(t, []string{"msg"}, result)
	assert.True(t, c.allowSecrets)
}

func TestBoost_ParseFlags_AllFlagsCombined(t *testing.T) {
	c := &CommitCommand{}
	result := c.parseFlags([]string{"--skip-prompt", "--dry-run", "--allow-secrets", "user", "msg"})
	assert.Equal(t, []string{"user", "msg"}, result)
	assert.True(t, c.skipPrompt)
	assert.True(t, c.dryRun)
	assert.True(t, c.allowSecrets)
}

func TestBoost_ParseFlags_UnknownPassesThrough(t *testing.T) {
	c := &CommitCommand{}
	result := c.parseFlags([]string{"--unknown", "arg"})
	assert.Equal(t, []string{"--unknown", "arg"}, result)
}

// =====================================================================
// CommitCommand SetAgentError (0% coverage)
// =====================================================================

func TestBoost_SetAgentError(t *testing.T) {
	c := &CommitCommand{}
	err := fmt.Errorf("test error")
	c.SetAgentError(err)
	assert.Equal(t, err, c.agentError)
}

// =====================================================================
// CommitCommand showHelp (0% coverage)
// =====================================================================

func TestBoost_ShowHelp(t *testing.T) {
	c := &CommitCommand{}
	output := captureOutput(func() {
		err := c.showHelp()
		assert.NoError(t, err)
	})
	assert.Contains(t, output, "Commit Command Usage")
	assert.Contains(t, output, "/commit")
}

// =====================================================================
// CommitFlow Constructors (partially covered)
// =====================================================================

func TestBoost_NewCommitFlow_NilAgent(t *testing.T) {
	cf := NewCommitFlow(nil)
	assert.NotNil(t, cf)
	assert.Nil(t, cf.agent)
	assert.NotNil(t, cf.optimizer)
	assert.False(t, cf.skipPrompt)
	assert.False(t, cf.dryRun)
	assert.False(t, cf.allowSecrets)
}

func TestBoost_NewCommitFlowWithFlags(t *testing.T) {
	a := agent.NewTestAgent()
	cf := NewCommitFlowWithFlags(a, true, true, true)
	assert.NotNil(t, cf)
	assert.True(t, cf.skipPrompt)
	assert.True(t, cf.dryRun)
	assert.True(t, cf.allowSecrets)
	assert.Equal(t, a, cf.agent)
}

func TestBoost_SetUserInstructions(t *testing.T) {
	cf := &CommitFlow{}
	cf.SetUserInstructions("keep it brief")
	assert.Equal(t, "keep it brief", cf.userInstructions)
}

// =====================================================================
// CommitFlow printf/println (0% coverage)
// =====================================================================

func TestBoost_CommitFlow_Printf(t *testing.T) {
	cf := &CommitFlow{}
	output := captureOutput(func() {
		cf.printf("hello %s", "world")
	})
	assert.Contains(t, output, "hello world")
}

func TestBoost_CommitFlow_Println(t *testing.T) {
	cf := &CommitFlow{}
	output := captureOutput(func() {
		cf.println("test line")
	})
	assert.Contains(t, output, "test line")
}

// =====================================================================
// CommitCommand printf/println (0% coverage)
// =====================================================================

func TestBoost_CommitCommand_Printf(t *testing.T) {
	c := &CommitCommand{}
	output := captureOutput(func() {
		c.printf("value: %d", 42)
	})
	assert.Contains(t, output, "value: 42")
}

func TestBoost_CommitCommand_Println(t *testing.T) {
	c := &CommitCommand{}
	output := captureOutput(func() {
		c.println("a line")
	})
	assert.Contains(t, output, "a line")
}

// =====================================================================
// CommitCommand Execute with help arg (0% coverage path)
// =====================================================================

func TestBoost_CommitCommand_Execute_HelpArg(t *testing.T) {
	c := &CommitCommand{}
	output := captureOutput(func() {
		err := c.Execute([]string{"help"}, nil)
		assert.NoError(t, err)
	})
	assert.Contains(t, output, "Commit Command Usage")
}

func TestBoost_CommitCommand_Execute_HelpFlag(t *testing.T) {
	c := &CommitCommand{}
	output := captureOutput(func() {
		err := c.Execute([]string{"--help"}, nil)
		assert.NoError(t, err)
	})
	assert.Contains(t, output, "Commit Command Usage")
}

func TestBoost_CommitCommand_Execute_MinusH(t *testing.T) {
	c := &CommitCommand{}
	output := captureOutput(func() {
		err := c.Execute([]string{"-h"}, nil)
		assert.NoError(t, err)
	})
	assert.Contains(t, output, "Commit Command Usage")
}

// =====================================================================
// SetGitDir whitespace trimming (partially covered)
// =====================================================================

func TestBoost_SetGitDir_WhitespaceTrimmed(t *testing.T) {
	SetGitDir("  /tmp/test  ")
	defer SetGitDir("")
	cmd := gitCommand("status")
	assert.Equal(t, "/tmp/test", cmd.Dir)
}

func TestBoost_SetGitDir_EmptyClears(t *testing.T) {
	SetGitDir("/tmp/testdir")
	SetGitDir("")
	cmd := gitCommand("status")
	assert.Equal(t, "", cmd.Dir)
}

// =====================================================================
// Git helpers in temp repo (0% coverage: getGitCommitHash, getGitBranchName, getStagedFiles, etc.)
// =====================================================================

func TestBoost_GitHelpers_InTempRepo(t *testing.T) {
	// Shared setup: create an initialized git repo with one committed file
	tmpDir := t.TempDir()
	SetGitDir(tmpDir)
	defer SetGitDir("")

	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = tmpDir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.name", "Test")
	cmd.Dir = tmpDir
	require.NoError(t, cmd.Run())

	testFile := tmpDir + "/test.txt"
	require.NoError(t, os.WriteFile(testFile, []byte("hello"), 0644))

	cmd = exec.Command("git", "add", "test.txt")
	cmd.Dir = tmpDir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "initial")
	cmd.Dir = tmpDir
	require.NoError(t, cmd.Run())

	t.Run("getGitCommitHash", func(t *testing.T) {
		hash, err := getGitCommitHash()
		assert.NoError(t, err)
		assert.Len(t, hash, 40)
	})

	t.Run("getGitBranchName", func(t *testing.T) {
		branch, err := getGitBranchName()
		assert.NoError(t, err)
		assert.NotEmpty(t, branch)
	})

	t.Run("getStagedFiles_empty", func(t *testing.T) {
		files, err := getStagedFiles()
		assert.NoError(t, err)
		assert.Empty(t, files)
	})

	t.Run("getStagedFiles_withStaged", func(t *testing.T) {
		// Self-contained: modify and stage, then verify
		require.NoError(t, os.WriteFile(testFile, []byte("modified"), 0644))
		stageCmd := exec.Command("git", "add", "test.txt")
		stageCmd.Dir = tmpDir
		require.NoError(t, stageCmd.Run())

		files, err := getStagedFiles()
		assert.NoError(t, err)
		assert.Contains(t, files, "test.txt")

		// Cleanup: commit so the staging area is clean for subsequent tests
		commitCmd := exec.Command("git", "commit", "-m", "stage test")
		commitCmd.Dir = tmpDir
		require.NoError(t, commitCmd.Run())
	})

	t.Run("getPorcelainStatusLines", func(t *testing.T) {
		// Self-contained: create untracked file, verify it appears
		newFile := tmpDir + "/new.txt"
		require.NoError(t, os.WriteFile(newFile, []byte("new"), 0644))

		lines, err := getPorcelainStatusLines()
		assert.NoError(t, err)
		assert.NotEmpty(t, lines)

		// Cleanup: remove the untracked file so it doesn't affect later tests
		require.NoError(t, os.Remove(newFile))
	})

	t.Run("CommitFlow_getGitStatus", func(t *testing.T) {
		cf := &CommitFlow{}
		staged, unstaged, err := cf.getGitStatus()
		assert.NoError(t, err)
		// Staging area should be clean after prior cleanup
		assert.Empty(t, staged)
		_ = unstaged
	})
}

// =====================================================================
// CommitFlow GetGitStatus error (0% coverage, non-git dir)
// =====================================================================

func TestBoost_CommitFlow_GetGitStatus_NoGitRepo(t *testing.T) {
	tmpDir := t.TempDir()
	SetGitDir(tmpDir)
	defer SetGitDir("")

	cf := &CommitFlow{}
	_, _, err := cf.getGitStatus()
	assert.Error(t, err)
}

// =====================================================================
// doHeuristicReview additional patterns (0% coverage paths)
// =====================================================================

func TestBoost_DoHeuristicReview_SecretPatterns(t *testing.T) {
	secretPatterns := []string{
		"password", "secret", "api_key", "apikey", "token",
		"private_key", "bearer", "authorization", "credential",
		"passwd", "pwd", "aws_access_key", "aws_secret_key",
		"slack_token", "github_token", "database_url",
	}
	for _, pattern := range secretPatterns {
		t.Run(pattern, func(t *testing.T) {
			diff := fmt.Sprintf("const x = \"%s_value\"", pattern)
			result := doHeuristicReview(diff, []string{"config.go"})
			assert.Contains(t, result, "SECRET", "expected detection for: %s", pattern)
		})
	}
}

func TestBoost_DoHeuristicReview_RiskyFiles(t *testing.T) {
	riskyFiles := []string{".env", ".env.production", "secret.yaml", "credential.json", "private_key.pem", "cert.key"}
	for _, file := range riskyFiles {
		t.Run(file, func(t *testing.T) {
			result := doHeuristicReview("normal code", []string{file})
			assert.Contains(t, result, "RISKY FILE")
		})
	}
}

func TestBoost_DoHeuristicReview_DebugPatterns(t *testing.T) {
	debugPatterns := []string{"console.log", "fmt.println", "debug=true"}
	for _, pattern := range debugPatterns {
		t.Run(pattern, func(t *testing.T) {
			diff := "// code\n" + pattern + "\n// more"
			result := doHeuristicReview(diff, []string{"main.go"})
			assert.Contains(t, result, "DEBUG CODE")
		})
	}
}

func TestBoost_DoHeuristicReview_LargeCommentedBlocks(t *testing.T) {
	var b strings.Builder
	b.WriteString("/* header */\n")
	for i := 0; i < 11; i++ {
		b.WriteString("// comment line\n")
	}
	result := doHeuristicReview(b.String(), []string{"main.go"})
	assert.Contains(t, result, "COMMENTED CODE")
}

func TestBoost_DoHeuristicReview_NoConcerns(t *testing.T) {
	result := doHeuristicReview("func add(a, b int) int { return a + b }", []string{"main.go"})
	assert.Equal(t, "No critical concerns found.", result)
}

// =====================================================================
// parseDiffForContent additional cases (partially covered at 91.9%)
// =====================================================================

func TestBoost_ParseDiffForContent_Empty(t *testing.T) {
	old, new_ := parseDiffForContent("", "main.go")
	assert.Equal(t, "\n", old)
	assert.Equal(t, "\n", new_)
}

func TestBoost_ParseDiffForContent_MultiHunk(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,3 @@
-old1
+new1
@@ -10,3 +10,3 @@
-old2
+new2
`
	old, new_ := parseDiffForContent(diff, "main.go")
	assert.Contains(t, old, "old1")
	assert.Contains(t, old, "old2")
	assert.Contains(t, new_, "new1")
	assert.Contains(t, new_, "new2")
}

func TestBoost_ParseDiffForContent_ContextLines(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,5 +1,5 @@
 package main
 import "fmt"
-removed line
+added line
 context line
`
	old, new_ := parseDiffForContent(diff, "main.go")
	assert.Contains(t, old, "removed line")
	assert.Contains(t, new_, "added line")
}

// =====================================================================
// IndexCommand Execute (0% coverage paths)
// =====================================================================

func TestBoost_IndexCommand_NilAgent(t *testing.T) {
	c := &IndexCommand{}
	err := c.Execute(nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "agent not available")
}

func TestBoost_IndexCommand_NilAgent_WithAction(t *testing.T) {
	c := &IndexCommand{}
	err := c.Execute([]string{"on"}, nil)
	assert.Error(t, err)
}

func TestBoost_IndexCommand_UnknownAction(t *testing.T) {
	c := &IndexCommand{}
	a := agent.NewTestAgent()
	err := c.Execute([]string{"badaction"}, a)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown action")
}

func TestBoost_IndexCommand_Enable(t *testing.T) {
	c := &IndexCommand{}
	a := agent.NewTestAgent()
	output := captureOutput(func() {
		err := c.Execute([]string{"enable"}, a)
		if err != nil {
			// TestAgent may not support full embedding index setup
			assert.Contains(t, err.Error(), "indexing", "error should relate to indexing")
		}
	})
	// On success path, output mentions "index"
	if len(output) > 0 {
		assert.Contains(t, output, "index", "output should mention index-related text")
	}
}

func TestBoost_IndexCommand_Status(t *testing.T) {
	c := &IndexCommand{}
	a := agent.NewTestAgent()
	output := captureOutput(func() {
		err := c.Execute([]string{"status"}, a)
		assert.NoError(t, err)
	})
	assert.Contains(t, output, "Status")
}

// =====================================================================
// CommitMessageHandler constructor (0% coverage)
// =====================================================================

func TestBoost_NewCommitMessageHandler_WithAgent(t *testing.T) {
	a := agent.NewTestAgent()
	h := NewCommitMessageHandler(a, nil)
	assert.NotNil(t, h)
	assert.Equal(t, a, h.chatAgent)
}

func TestBoost_NewCommitMessageHandler_NilAgent(t *testing.T) {
	h := NewCommitMessageHandler(nil, nil)
	assert.NotNil(t, h)
	assert.Nil(t, h.chatAgent)
}

// =====================================================================
// RollbackCommand Execute with invalid revision (0% coverage)
// =====================================================================

func TestBoost_RollbackCommand_Execute_InvalidRevision(t *testing.T) {
	r := &RollbackCommand{}
	err := r.Execute([]string{"nonexistent_revision_id"}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rollback failed")
}

// =====================================================================
// stageFiles with mock (0% coverage for non-existent files)
// =====================================================================

type mockPrintfHelper struct {
	output strings.Builder
}

func (m *mockPrintfHelper) printf(format string, args ...interface{}) {
	m.output.WriteString(fmt.Sprintf(format, args...))
}

func (m *mockPrintfHelper) println(text string) {
	m.output.WriteString(text + "\n")
}

func TestBoost_StageFiles_NonExistentFiles(t *testing.T) {
	tmpDir := t.TempDir()
	SetGitDir(tmpDir)
	defer SetGitDir("")

	mock := &mockPrintfHelper{}
	stageFiles(mock, []string{"nonexistent.go"})
	// Failure rendering migrated from "[FAIL]" to GlyphError ("✗");
	// assert against the readable suffix so the test stays stable
	// across color/no-color environments.
	assert.Contains(t, mock.output.String(), "Failed to stage")
}

// =====================================================================
// CommitFlow executeConsoleFlow (0% coverage)
// Testing the path where no git repo exists
// =====================================================================

func TestBoost_CommitFlow_ExecuteConsoleFlow_NoGitRepo(t *testing.T) {
	tmpDir := t.TempDir()
	SetGitDir(tmpDir)
	defer SetGitDir("")

	cf := &CommitFlow{}
	err := cf.executeConsoleFlow()
	assert.Error(t, err)
}

// =====================================================================
// CommitFlow CommitStagedWithMessage (0% coverage)
// Testing no staged changes path
// =====================================================================

// TestBoost_CommitStagedWithMessage_NilAgentRefuses verifies the new
// contract: CommitStagedWithMessage rejects a nil-agent CommitFlow rather
// than falling back to SetGitDir(""). Background — the prior behavior
// produced two real "test" commits on the host repo when a leaked
// api.TestClientType="test" sentinel routed the commit message LLM to
// the mock client, which returned "test". The defense-in-depth at the
// gitCommand layer (commit_git_safety_test.go) and the sentinel scrub
// at config load (pkg/configuration/testing_isolation.go) are the other
// two layers; this is the call-site refusal that closes the loop.
func TestBoost_CommitStagedWithMessage_NilAgentRefuses(t *testing.T) {
	cf := &CommitFlow{}
	err := cf.CommitStagedWithMessage()
	if err == nil {
		t.Fatal("expected error from nil-agent CommitStagedWithMessage")
	}
	if !strings.Contains(err.Error(), "requires an agent") {
		t.Errorf("error message should explain the contract; got: %v", err)
	}
}

// =====================================================================
// CommitFlow buildCommitActions edge cases (partially covered)
// =====================================================================

func TestBoost_BuildCommitActions_EdgeCases(t *testing.T) {
	cf := &CommitFlow{}

	t.Run("empty both lists", func(t *testing.T) {
		actions := cf.buildCommitActions(nil, nil)
		assert.Empty(t, actions)
	})

	t.Run("many staged files shows count", func(t *testing.T) {
		staged := []string{"a.go", "b.go", "c.go", "d.go"}
		actions := cf.buildCommitActions(staged, []string{"e.go"})
		assert.Contains(t, actions[0].Description, "4 staged file(s)")
		assert.NotContains(t, actions[0].Description, "a.go")
	})

	t.Run("3 or fewer staged shows file list", func(t *testing.T) {
		staged := []string{"a.go", "b.go"}
		actions := cf.buildCommitActions(staged, []string{"c.go"})
		assert.Contains(t, actions[0].Description, "a.go, b.go")
	})

	t.Run("single staged file no unstaged", func(t *testing.T) {
		actions := cf.buildCommitActions([]string{"only.go"}, nil)
		assert.Len(t, actions, 1) // Only commit_staged
		assert.Equal(t, "commit_staged", actions[0].ID)
	})

	t.Run("single file option only when total > 1", func(t *testing.T) {
		// Only 1 staged + 0 unstaged = total 1, should NOT have single_file
		actions := cf.buildCommitActions([]string{"one.go"}, nil)
		for _, a := range actions {
			assert.NotEqual(t, "single_file", a.ID)
		}
	})
}

// =====================================================================
// BuildInitPrompt (covered but let's exercise the constructor)
// =====================================================================

func TestBoost_InitCommand_Constructors(t *testing.T) {
	cmd := &InitCommand{}
	assert.Equal(t, "init", cmd.Name())
	assert.Contains(t, cmd.Description(), "AGENTS.md")
}

// =====================================================================
// HelpCommand Tests
// =====================================================================

func TestHelpCommand_Name(t *testing.T) {
	h := &HelpCommand{}
	assert.Equal(t, "help", h.Name())
}

func TestHelpCommand_Description(t *testing.T) {
	h := &HelpCommand{}
	assert.Equal(t, "Show help information and available slash commands", h.Description())
}

func TestHelpCommand_Execute_Output(t *testing.T) {
	registry := NewCommandRegistry()
	h := &HelpCommand{registry: registry}

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := h.Execute(nil, nil)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	assert.NoError(t, err)
	assert.Contains(t, output, "Sprout")
	assert.Contains(t, output, "AVAILABLE SLASH COMMANDS")
	assert.Contains(t, output, "/help")
}

// =====================================================================
// LogActionItem Tests
// =====================================================================
// LogActionItem Tests
// =====================================================================

func TestLogActionItem_Display(t *testing.T) {
	item := LogActionItem{
		ID:          "view_log",
		DisplayName: "[list] View Change Log",
		Description: "Display complete change history",
	}
	assert.Equal(t, "[list] View Change Log", item.Display())
}

func TestLogActionItem_SearchText(t *testing.T) {
	item := LogActionItem{
		ID:          "view_log",
		DisplayName: "[list] View Change Log",
		Description: "Display complete change history",
	}
	assert.Equal(t, "[list] View Change Log Display complete change history", item.SearchText())
}

func TestLogActionItem_Value(t *testing.T) {
	item := LogActionItem{
		ID:          "rollback_select",
		DisplayName: "[|<] Select Revision",
		Description: "Choose from available revisions",
	}
	val := item.Value()
	assert.Equal(t, "rollback_select", val)
}

// =====================================================================
// RevisionItem Tests
// =====================================================================

func TestRevisionItem_Display(t *testing.T) {
	item := RevisionItem{
		RevisionID:  "rev_abc123",
		Description: "Fixed login bug",
		Timestamp:   "2024-01-15",
	}
	assert.Equal(t, "rev_abc123 - Fixed login bug", item.Display())
}

func TestRevisionItem_SearchText(t *testing.T) {
	item := RevisionItem{
		RevisionID:  "rev_def456",
		Description: "Added auth module",
		Timestamp:   "2024-01-16 10:00:00",
	}
	assert.Equal(t, "rev_def456 Added auth module 2024-01-16 10:00:00", item.SearchText())
}

func TestRevisionItem_Value(t *testing.T) {
	item := RevisionItem{
		RevisionID:  "rev_ghi789",
		Description: "Updated README",
		Timestamp:   "2024-01-17",
	}
	val := item.Value()
	assert.Equal(t, "rev_ghi789", val)
}

// =====================================================================
// CompactCommand Tests
// =====================================================================

func TestCompactCommand_Name(t *testing.T) {
	c := &CompactCommand{}
	assert.Equal(t, "compact", c.Name())
}

func TestCompactCommand_Description(t *testing.T) {
	c := &CompactCommand{}
	assert.Equal(t, "LLM-summarize the middle of the conversation, preserving the opening task anchor and the recent causal chain", c.Description())
}

func TestCompactCommand_Execute_NilAgent(t *testing.T) {
	c := &CompactCommand{}
	err := c.Execute(nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "agent not available")
}

func TestCompactCommand_Execute_MiddleTooSmallShortCircuits(t *testing.T) {
	// 0:  system                                (anchor start)
	// 1:  user "u0"                             (anchor user)
	// 2:  assistant "a0" plain                  (anchor assistant, anchorEnd=3)
	// 3..29: all assistant-tc                   (every slot triggers branch-2 walk-back)
	//
	// anchorEnd = 3. raw recentStart = 30 - 12 = 18. adjustRecentBoundary
	// walks back via branch 2 repeatedly: 18 → 17 → 16 → ... → 4 → 3.
	// At recentStart=3, branch 2 checks messages[2] (assistant plain, no
	// tool calls) → skip → break. got=3. middle = 3-3 = 0 < 6 → middle-
	// too-small branch fires. ✓
	a := agent.NewTestAgent()
	a.SetMessages(makeMiddleTooSmallHistory())
	c := &CompactCommand{}

	output := captureOutput(func() {
		err := c.Execute(nil, a)
		assert.NoError(t, err)
	})

	assert.Contains(t, output, "Middle segment too small to be worth summarizing")
}

// =====================================================================
// ModelsCommand Pure Function Tests
// =====================================================================

func TestModelsCommand_CommonPrefix(t *testing.T) {
	m := &ModelsCommand{}

	tests := []struct {
		name     string
		a        string
		b        string
		expected string
	}{
		{"common prefix", "gpt-4-turbo", "gpt-4-32k", "gpt-4-"},
		{"no common prefix", "gpt-4", "claude-3", ""},
		{"identical strings", "gpt-4", "gpt-4", "gpt-4"},
		{"one is prefix of other", "gpt", "gpt-4-turbo", "gpt"},
		{"empty a", "", "gpt-4", ""},
		{"empty b", "gpt-4", "", ""},
		{"both empty", "", "", ""},
		{"case insensitive", "GPT-4", "gpt-3", "GPT-"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.commonPrefix(tt.a, tt.b)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestModelsCommand_FindExactModel(t *testing.T) {
	m := &ModelsCommand{}

	tests := []struct {
		name     string
		models   []api.ModelInfo
		query    string
		expected string // empty string means nil expected
	}{
		{
			name: "exact match",
			models: []api.ModelInfo{
				{ID: "gpt-4", Description: "OpenAI GPT-4"},
				{ID: "gpt-3.5-turbo", Description: "OpenAI GPT-3.5"},
			},
			query:    "gpt-4",
			expected: "gpt-4",
		},
		{
			name: "case insensitive match",
			models: []api.ModelInfo{
				{ID: "GPT-4", Description: "OpenAI GPT-4"},
			},
			query:    "gpt-4",
			expected: "GPT-4",
		},
		{
			name: "no match",
			models: []api.ModelInfo{
				{ID: "gpt-4", Description: "OpenAI GPT-4"},
			},
			query:    "claude-3",
			expected: "",
		},
		{
			name:     "empty models list",
			models:   []api.ModelInfo{},
			query:    "gpt-4",
			expected: "",
		},
		{
			name:     "nil models list",
			models:   nil,
			query:    "gpt-4",
			expected: "",
		},
		{
			name:     "empty query",
			models:   []api.ModelInfo{{ID: "gpt-4", Description: "OpenAI GPT-4"}},
			query:    "",
			expected: "",
		},
		{
			name: "partial match should not match",
			models: []api.ModelInfo{
				{ID: "gpt-4-turbo", Description: "OpenAI GPT-4 Turbo"},
				{ID: "gpt-4", Description: "OpenAI GPT-4"},
			},
			query:    "gpt",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.findExactModel(tt.models, tt.query)
			if tt.expected == "" {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				assert.Equal(t, tt.expected, result.ID)
			}
		})
	}
}

func TestModelsCommand_FindCommonPrefix(t *testing.T) {
	m := &ModelsCommand{}

	tests := []struct {
		name     string
		matches  []api.ModelInfo
		input    string
		expected string
	}{
		{
			name:     "no matches",
			matches:  []api.ModelInfo{},
			input:    "gpt",
			expected: "",
		},
		{
			name: "single match",
			matches: []api.ModelInfo{
				{ID: "gpt-4-turbo"},
			},
			input:    "gpt",
			expected: "gpt-4-", // finds stop chars '-' and extends
		},
		{
			name: "two matches with meaningful common extension",
			matches: []api.ModelInfo{
				{ID: "openrouter/anthropic/claude-3-opus"},
				{ID: "openrouter/anthropic/claude-3-sonnet"},
			},
			input:    "openrouter",
			expected: "openrouter/anthropic/claude-",
		},
		{
			name: "two matches with long common prefix after input",
			matches: []api.ModelInfo{
				{ID: "openrouter/anthropic/claude-3-opus"},
				{ID: "openrouter/anthropic/claude-3-sonnet"},
			},
			input:    "openrouter/an",
			expected: "openrouter/anthropic/claude-",
		},
		{
			name: "matches diverge after input",
			matches: []api.ModelInfo{
				{ID: "gpt-4"},
				{ID: "gpt-3.5"},
			},
			input:    "gpt",
			expected: "", // common prefix "gpt" is not > len(input)+1
		},
		{
			name: "three matches with common prefix",
			matches: []api.ModelInfo{
				{ID: "openrouter/anthropic/claude-3-opus"},
				{ID: "openrouter/anthropic/claude-3-sonnet"},
				{ID: "openrouter/anthropic/claude-3-haiku"},
			},
			input:    "openrouter",
			expected: "openrouter/anthropic/claude-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.findCommonPrefix(tt.matches, tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestModelsCommand_CalculateFuzzyScore(t *testing.T) {
	m := &ModelsCommand{}

	tests := []struct {
		name     string
		model    api.ModelInfo
		query    string
		expected int
	}{
		{
			name: "substring at start gets bonus",
			model: api.ModelInfo{
				ID:          "gpt-4-turbo",
				Description: "OpenAI GPT-4 Turbo",
			},
			query:    "gpt",
			expected: 190, // 100 contain + 50 prefix + 30 word "gpt" in ID + 10 word "gpt" in description
		},
		{
			name: "no match returns 0",
			model: api.ModelInfo{
				ID:          "gpt-4-turbo",
				Description: "OpenAI GPT-4 Turbo",
			},
			query:    "claude",
			expected: 0,
		},
		{
			name: "multi-part query with slash",
			model: api.ModelInfo{
				ID:          "openrouter/gpt-4",
				Description: "OpenRouter GPT-4",
			},
			query:    "openrouter/gpt",
			expected: 100 + 50 + 80, // contain + prefix + both parts match
		},
		{
			name: "word match in ID",
			model: api.ModelInfo{
				ID:          "gpt-4-turbo",
				Description: "OpenAI GPT-4 Turbo",
			},
			query:    "turbo",
			expected: 140, // 100 contain in ID + 30 word "turbo" in ID + 10 word "turbo" in description
		},
		{
			name: "word match in description only",
			model: api.ModelInfo{
				ID:          "gpt-4",
				Description: "A smart model for coding",
			},
			query:    "smart",
			expected: 10, // word match in description only
		},
		{
			name: "short word ignored (less than 3 chars)",
			model: api.ModelInfo{
				ID:          "gpt-4",
				Description: "An ai model",
			},
			query:    "an",
			expected: 0, // "an" is only 2 chars
		},
		{
			name: "empty query",
			model: api.ModelInfo{
				ID:          "gpt-4",
				Description: "OpenAI GPT-4",
			},
			query:    "",
			expected: 150, // empty string is contained in every string (100) + prefix (50)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := m.calculateFuzzyScore(tt.model, tt.query)
			assert.Equal(t, tt.expected, score)
		})
	}
}

func TestModelsCommand_FuzzySearchModels(t *testing.T) {
	m := &ModelsCommand{}

	models := []api.ModelInfo{
		{ID: "gpt-4-turbo", Description: "OpenAI GPT-4 Turbo"},
		{ID: "gpt-3.5-turbo", Description: "OpenAI GPT-3.5"},
		{ID: "claude-3-opus", Description: "Anthropic Claude 3 Opus"},
		{ID: "claude-3-sonnet", Description: "Anthropic Claude 3 Sonnet"},
		{ID: "llama-3-70b", Description: "Meta Llama 3 70B"},
	}

	tests := []struct {
		name      string
		query     string
		wantCount int
		wantFirst string // ID of top result
	}{
		{
			name:      "search gpt",
			query:     "gpt",
			wantCount: 2,
			wantFirst: "gpt-4-turbo", // higher score (prefix bonus)
		},
		{
			name:      "search claude",
			query:     "claude",
			wantCount: 2,
			wantFirst: "claude-3-opus",
		},
		{
			name:      "search non-existent",
			query:     "nonexistent",
			wantCount: 0,
			wantFirst: "",
		},
		{
			name:      "empty query returns all results",
			query:     "",
			wantCount: 5,  // empty string matches all (score 150 each)
			wantFirst: "", // order not guaranteed for equal scores
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := m.fuzzySearchModels(models, tt.query)
			assert.Len(t, results, tt.wantCount)
			if tt.wantCount > 0 && tt.wantFirst != "" {
				assert.Equal(t, tt.wantFirst, results[0].ID)
			}
		})
	}
}

func TestModelsCommand_FuzzySearchModels_LimitsTo10(t *testing.T) {
	m := &ModelsCommand{}

	// Create 15 models that all match "gpt"
	models := make([]api.ModelInfo, 0, 15)
	for i := 0; i < 15; i++ {
		models = append(models, api.ModelInfo{ID: fmt.Sprintf("gpt-%d", i)})
	}

	results := m.fuzzySearchModels(models, "gpt")
	assert.LessOrEqual(t, len(results), 10)
}

// =====================================================================
// Helper for capturing stdout in tests
// =====================================================================

func captureOutput(f func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Read from pipe in background goroutine to prevent deadlock between
	// w.Close() and io.Copy.
	var buf bytes.Buffer
	copyDone := make(chan struct{})
	go func() {
		io.Copy(&buf, r)
		close(copyDone)
	}()

	f()
	w.Close()
	os.Stdout = old
	<-copyDone
	return buf.String()
}

// =====================================================================
// CommandRegistry Execute Tests
// =====================================================================

func TestCommandRegistry_Execute(t *testing.T) {
	registry := NewCommandRegistry()

	t.Run("help command succeeds", func(t *testing.T) {
		var err error
		captureOutput(func() {
			err = registry.Execute("/help", nil)
		})
		assert.NoError(t, err)
	})

	t.Run("unknown command returns error", func(t *testing.T) {
		err := registry.Execute("/unknown", nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown command")
	})

	t.Run("empty input returns error", func(t *testing.T) {
		err := registry.Execute("", nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not a valid command")
	})

	t.Run("slash only returns error", func(t *testing.T) {
		err := registry.Execute("/", nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty command")
	})

	t.Run("bang command routes to exec", func(t *testing.T) {
		var err error
		captureOutput(func() {
			err = registry.Execute("!echo test", nil)
		})
		// Should NOT be "unknown command" - it routes to exec
		if err != nil {
			assert.NotContains(t, err.Error(), "unknown command")
		}
	})
}

// =====================================================================
// ExecCommand Execute Tests
// =====================================================================

func TestExecCommand_Execute_NilAgent(t *testing.T) {
	c := &ExecCommand{}

	t.Run("no args returns usage error", func(t *testing.T) {
		err := c.Execute(nil, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "usage: /exec")
	})

	t.Run("git checkout blocked", func(t *testing.T) {
		err := c.Execute([]string{"git", "checkout", "main"}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "checkout")
	})

	t.Run("git restore blocked", func(t *testing.T) {
		err := c.Execute([]string{"git", "restore", "file"}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "restore")
	})
}

// =====================================================================
// WriteToOutput Tests
// =====================================================================

func TestWriteToOutput(t *testing.T) {
	output := captureOutput(func() {
		WriteToOutput("hello world")
	})
	assert.Contains(t, output, "hello world")
}

// =====================================================================
// WriteJSONToOutput Tests
// =====================================================================

func TestWriteJSONToOutput(t *testing.T) {
	output := captureOutput(func() {
		err := WriteJSONToOutput(map[string]string{"key": "value"})
		assert.NoError(t, err)
	})
	assert.Contains(t, output, `"key"`)
	assert.Contains(t, output, `"value"`)
}

// =====================================================================
// getChangeTrackingStatus Tests
// =====================================================================

func TestGetChangeTrackingStatus_WithTestAgent(t *testing.T) {
	a := agent.NewTestAgent()
	status := getChangeTrackingStatus(a)
	// NewTestAgent() doesn't have change tracking enabled
	// GetChangeTracker() returns nil → "[i] Idle (no tracked session yet)"
	assert.Contains(t, status, "Idle")
}

func TestGetChangeTrackingStatus_NilAgent(t *testing.T) {
	status := getChangeTrackingStatus(nil)
	// Glyph-prefixed (SP-057 Phase 1); assert on the text suffix.
	assert.Contains(t, status, "Disabled")
}

// =====================================================================
// Review Context Tests
// =====================================================================

func TestExtractStagedChangesSummary_NoGitRepo(t *testing.T) {
	// Create a temp dir that is not a git repo, using t.Chdir for auto-restore
	t.Chdir(t.TempDir())

	// extractStagedChangesSummary should return "" when not in a git repo
	result := extractStagedChangesSummary()
	assert.Equal(t, "", result)
}

// =====================================================================
// BuildLogActions Tests
// =====================================================================

func TestBuildLogActions(t *testing.T) {
	lf := &LogFlow{agent: nil}

	t.Run("nil revisions returns basic actions", func(t *testing.T) {
		actions := lf.buildLogActions(nil)
		assert.Len(t, actions, 2) // view_log and current_changes only
		assert.Equal(t, "view_log", actions[0].ID)
		assert.Equal(t, "current_changes", actions[1].ID)
	})

	t.Run("empty slice returns basic actions", func(t *testing.T) {
		actions := lf.buildLogActions([]history.RevisionGroup{})
		assert.Len(t, actions, 2)
		assert.Equal(t, "view_log", actions[0].ID)
		assert.Equal(t, "current_changes", actions[1].ID)
	})

	t.Run("non-empty revisions returns all actions", func(t *testing.T) {
		actions := lf.buildLogActions([]history.RevisionGroup{{RevisionID: "rev1"}})
		assert.Len(t, actions, 5) // all 5 actions
		assert.Equal(t, "view_log", actions[0].ID)
		assert.Equal(t, "rollback_select", actions[1].ID)
		assert.Equal(t, "current_changes", actions[2].ID)
		assert.Equal(t, "change_stats", actions[3].ID)
		assert.Equal(t, "export_log", actions[4].ID)
	})
}

// =====================================================================
// ChangesCommand Execute Tests
// =====================================================================

func TestChangesCommand_Execute_NilAgent(t *testing.T) {
	c := &ChangesCommand{}
	output := captureOutput(func() {
		err := c.Execute(nil, nil)
		assert.NoError(t, err)
	})
	assert.Contains(t, output, "No active tracked session")
}

func TestChangesCommand_Execute_WithTestAgent(t *testing.T) {
	c := &ChangesCommand{}
	a := agent.NewTestAgent()
	output := captureOutput(func() {
		err := c.Execute(nil, a)
		assert.NoError(t, err)
	})
	// NewTestAgent has no change tracker, so should say "No tracked session has started yet"
	assert.Contains(t, output, "No tracked session")
}

// =====================================================================
// StatusCommand Execute Tests
// =====================================================================

func TestStatusCommand_Execute_NilAgent(t *testing.T) {
	s := &StatusCommand{}
	output := captureOutput(func() {
		err := s.Execute(nil, nil)
		assert.NoError(t, err)
	})
	assert.Contains(t, output, "Session Status")
	assert.Contains(t, output, "Change Tracking")
}

func TestStatusCommand_Execute_WithTestAgent(t *testing.T) {
	s := &StatusCommand{}
	a := agent.NewTestAgent()
	output := captureOutput(func() {
		err := s.Execute(nil, a)
		assert.NoError(t, err)
	})
	assert.Contains(t, output, "Session Status")
}

// =====================================================================
// RollbackCommand Execute Tests
// =====================================================================

func TestRollbackCommand_Execute_NoArgs(t *testing.T) {
	r := &RollbackCommand{}
	output := captureOutput(func() {
		err := r.Execute(nil, nil)
		assert.NoError(t, err)
	})
	assert.Contains(t, output, "Available revisions")
}

// =====================================================================
// CommandRegistry IsSlashCommand Tests
// =====================================================================

func TestCommandRegistry_IsSlashCommand(t *testing.T) {
	r := NewCommandRegistry()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid slash command", "/help", true},
		{"valid bang command", "!ls", true},
		{"no prefix", "help", false},
		{"empty input", "", false},
		{"slash only", "/", false},
		{"bang only", "!", false},
		{"slash with path", "/path/to/file", false},
		{"slash with backslash", "\\path", false},
		{"special chars in name", "/help!", false},
		{"valid command with numbers", "/stats", true},
		{"bang with command", "!echo hello", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.IsSlashCommand(tt.input)
			assert.Equal(t, tt.want, got, "IsSlashCommand(%q)", tt.input)
		})
	}
}

// =====================================================================
// CommandRegistry GetCommand Tests
// =====================================================================

func TestCommandRegistry_GetCommand(t *testing.T) {
	r := NewCommandRegistry()

	cmd, ok := r.GetCommand("help")
	assert.True(t, ok)
	assert.NotNil(t, cmd)
	assert.Equal(t, "help", cmd.Name())

	_, ok = r.GetCommand("nonexistent")
	assert.False(t, ok)
}

// =====================================================================
// CommandRegistry ListCommands Tests
// =====================================================================

func TestCommandRegistry_ListCommands(t *testing.T) {
	r := NewCommandRegistry()
	commands := r.ListCommands()
	assert.NotEmpty(t, commands)
	// Should have at least the built-in commands
	assert.GreaterOrEqual(t, len(commands), 10)
}

// =====================================================================
// CompactCommand anchor/boundary helper tests
// =====================================================================
//
// These tests pin the boundary logic that /compact uses to keep the
// opening task anchor and the recent causal chain intact. They mirror
// seed's unexported compactionAnchorEnd / adjustCompactionBoundary so
// changes to one side or the other are caught by the suite.

func TestCompactAnchorEnd_Empty(t *testing.T) {
	got := compactAnchorEnd(nil)
	assert.Equal(t, 0, got)
	got = compactAnchorEnd([]api.Message{})
	assert.Equal(t, 0, got)
}

func TestCompactAnchorEnd_SystemOnly(t *testing.T) {
	// Only a system message → anchorEnd = 1. There is no user message
	// so the fallback `anchorEnd = 1` kicks in.
	msgs := []api.Message{{Role: "system", Content: "you are helpful"}}
	got := compactAnchorEnd(msgs)
	assert.Equal(t, 1, got)
}

func TestCompactAnchorEnd_SystemPlusFirstUser(t *testing.T) {
	// system + first user → anchorEnd = 2. The follow-up assistant has
	// no tool calls so it would be included too, but it's not present
	// here so anchorEnd stops at 2.
	msgs := []api.Message{
		{Role: "system", Content: "you are helpful"},
		{Role: "user", Content: "u0"},
	}
	got := compactAnchorEnd(msgs)
	assert.Equal(t, 2, got)
}

func TestCompactAnchorEnd_SystemUserImmediateAssistantPlain(t *testing.T) {
	// system + user + immediate plain assistant (no tool calls) →
	// anchorEnd = 3 (the assistant is anchored because it has no tool
	// calls, so the model can still see the opening greeting in place).
	msgs := []api.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "u0"},
		{Role: "assistant", Content: "hi"},
	}
	got := compactAnchorEnd(msgs)
	assert.Equal(t, 3, got)
}

func TestCompactAnchorEnd_SystemUserImmediateAssistantToolCalls(t *testing.T) {
	// system + user + immediate assistant WITH tool calls → anchorEnd
	// stays at 2 (the assistant is left in the middle so its tool
	// results can stay paired with it).
	msgs := []api.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "u0"},
		{Role: "assistant", Content: "ok", ToolCalls: []api.ToolCall{{ID: "c1"}}},
	}
	got := compactAnchorEnd(msgs)
	assert.Equal(t, 2, got)
}

func TestCompactAnchorEnd_NoUserMessage(t *testing.T) {
	// No user message anywhere — fallback to anchorEnd = 1 (just the
	// system message is anchored).
	msgs := []api.Message{
		{Role: "system", Content: "sys"},
		{Role: "assistant", Content: "a"},
	}
	got := compactAnchorEnd(msgs)
	assert.Equal(t, 1, got)
}

func TestCompactAnchorEnd_NoSystemNoUser(t *testing.T) {
	// No system, no user → fallback to anchorEnd = 1.
	msgs := []api.Message{
		{Role: "assistant", Content: "a"},
	}
	got := compactAnchorEnd(msgs)
	assert.Equal(t, 1, got)
}

func TestCompactAnchorEnd_SkipsLeadingNonUserUntilFirstUser(t *testing.T) {
	// system + assistant + user → anchorEnd = 3 (the assistant between
	// system and the first user is skipped over).
	msgs := []api.Message{
		{Role: "system", Content: "sys"},
		{Role: "assistant", Content: "prelude"},
		{Role: "user", Content: "u0"},
		{Role: "assistant", Content: "plain reply"},
	}
	got := compactAnchorEnd(msgs)
	assert.Equal(t, 4, got)
}

func TestAdjustRecentBoundary_WalksBackPastTrailingTool(t *testing.T) {
	// The first-branch check fires when messages[recentStart] is a
	// tool message. We construct a layout where recentStart points at
	// a tool and the slot at recentStart-1 is NOT an assistant-tc
	// (so only branch 1 fires).
	msgs := []api.Message{
		{Role: "system", Content: "s"},
		{Role: "user", Content: "u"}, // anchorEnd = 2
		{Role: "assistant", Content: "plain"},
		{Role: "tool", Content: "result", ToolCallID: "c1"}, // 3 — recentStart points here
	}
	// recentStart=3 → messages[3] is tool → walk to 2.
	// recentStart=2 → loop guard 2 > 2 fails → break. got=2.
	got := adjustRecentBoundary(msgs, 3, 2)
	assert.Equal(t, 2, got)
}

func TestAdjustRecentBoundary_WalksBackPastAssistantWithToolCalls(t *testing.T) {
	// The second-branch check fires when messages[recentStart] is NOT
	// a tool but messages[recentStart-1] is an assistant-with-tool-
	// calls. We construct a layout where that pattern holds and the
	// walk-back stops after one iteration (slot below is not tool, not
	// assistant-tc).
	msgs := []api.Message{
		{Role: "system", Content: "s"},
		{Role: "user", Content: "u"}, // anchorEnd = 2
		{Role: "assistant", Content: "plain"},
		{Role: "assistant", Content: "tc", ToolCalls: []api.ToolCall{{ID: "c1"}}}, // 3
		{Role: "user", Content: "u2"},                                             // 4
	}
	// recentStart=4 → messages[4] is user (not tool) → first branch
	// skip. messages[3] is assistant-tc → second branch fires → walk
	// to 3. recentStart=3 → messages[3] is assistant-tc (not tool) →
	// first branch skip. messages[2] is assistant plain (no tool
	// calls) → second branch skip. break → got=3.
	got := adjustRecentBoundary(msgs, 4, 2)
	assert.Equal(t, 3, got)
}

func TestAdjustRecentBoundary_StopsAtAnchorEnd(t *testing.T) {
	// The loop guard `recentStart > anchorEnd` must hold — the
	// boundary can never be moved below anchorEnd. We construct a
	// list where the walk-back would WANT to keep going but anchorEnd
	// blocks it.
	//
	// Layout: msgs[0]=system (anchor start), msgs[1]=user (anchor
	// user, anchorEnd=2). msgs[2]=plain assistant (anchor assistant,
	// anchorEnd=3). msgs[3..10]=assistant-tc.
	//
	// adjustRecentBoundary(msgs, 10, 3): walk-back goes 10 → 9 → ...
	// → 4 → 3 (via second branch). At recentStart=3, second branch
	// checks messages[2] which is plain assistant (no ToolCalls) →
	// skip → break. got=3.
	msgs := []api.Message{
		{Role: "system", Content: "s"},
		{Role: "user", Content: "u"}, // anchorEnd = 2; then messages[2] is plain assistant → anchorEnd=3
		{Role: "assistant", Content: "plain-anchor"},
	}
	for i := 3; i <= 10; i++ {
		msgs = append(msgs, api.Message{
			Role:      "assistant",
			Content:   fmt.Sprintf("tc-%d", i),
			ToolCalls: []api.ToolCall{{ID: fmt.Sprintf("c%d", i)}},
		})
	}
	got := adjustRecentBoundary(msgs, 10, 3)
	assert.Equal(t, 3, got)

	// To prove the loop guard stops at anchorEnd (not just at the
	// first non-assistant-tc slot), use a tighter anchorEnd where
	// EVERY slot from anchorEnd+1 onward is assistant-tc. The loop
	// must still stop at anchorEnd without dropping below it.
	//
	// Layout: msgs[0]=system only → anchorEnd=1. msgs[1..5]=assistant-tc.
	// Walk-back: 5 → 4 → 3 → 2 → 1 (loop guard 1 > 1 false → break).
	msgs2 := []api.Message{
		{Role: "system", Content: "s"}, // anchorEnd = 1
	}
	for i := 1; i <= 5; i++ {
		msgs2 = append(msgs2, api.Message{
			Role:      "assistant",
			Content:   fmt.Sprintf("tc-%d", i),
			ToolCalls: []api.ToolCall{{ID: fmt.Sprintf("c%d", i)}},
		})
	}
	got = adjustRecentBoundary(msgs2, 5, 1)
	assert.Equal(t, 1, got, "loop must stop at anchorEnd (1), not drop below it")
}

// =====================================================================
// CompactCommand Execute tests — threshold and overlap branches
// =====================================================================
//
// These exercise the early-exit paths that don't reach the LLM call.
// The test agent has no LLM client bound, so any test that DOES hit
// SummarizeViaLLM would error out — we deliberately stay on the
// short-circuit branches so the suite doesn't need an HTTP mock.

func TestCompactCommand_Execute_BelowMinMessagesThreshold(t *testing.T) {
	// 29 messages — one below the threshold — must short-circuit with
	// the "Need at least 30" informational message and not modify the
	// message list.
	a := agent.NewTestAgent()
	original := makeMessages(29)
	a.SetMessages(original)
	c := &CompactCommand{}

	output := captureOutput(func() {
		err := c.Execute(nil, a)
		assert.NoError(t, err)
	})

	assert.Contains(t, output, "Need at least 30 messages to compact")
	assert.Contains(t, output, "(have 29)")
	// Messages should be unchanged after a no-op short-circuit.
	assert.Equal(t, 29, len(a.GetMessages()))
}

func TestCompactCommand_Execute_AnchorRecentOverlapShortCircuits(t *testing.T) {
	// 30 messages where anchorEnd >= len-recentToKeep. We make
	// anchorEnd large by stacking many "first user" candidates. With
	// the makeOverlappingHistory builder, the first user at index 1
	// has a plain assistant reply at index 2, but then we attach many
	// user/assistant plain pairs that are NOT anchor-included — the
	// anchor is system + first user + immediate plain assistant = 3.
	// recentStart = 30-12 = 18. middle = 18-3 = 15, which is fine. To
	// force overlap we need a long opening assistant-with-tool-calls
	// chain such that adjustRecentBoundary walks recentStart back to
	// <= anchorEnd. The makeOverlappingHistory builder places a tool
	// result at index 30-12 = 18 to trigger the walk-back.
	a := agent.NewTestAgent()
	a.SetMessages(makeOverlappingHistory(30))
	c := &CompactCommand{}

	output := captureOutput(func() {
		err := c.Execute(nil, a)
		assert.NoError(t, err)
	})

	// Either the overlap branch or the middle-too-small branch fires;
	// both are valid early-exits. We accept either informative
	// message.
	assert.True(t,
		strings.Contains(output, "Not enough distinct history beyond anchor + recent window to compact") ||
			strings.Contains(output, "Middle segment too small to be worth summarizing"),
		"expected an early-exit message; got: %q", output)
}

// =====================================================================
// Test message builders
// =====================================================================
//
// These produce deterministic message lists shaped to exercise the
// specific boundary branches above. Each builder is paired with a
// docstring explaining the layout.

func makeMessages(n int) []api.Message {
	msgs := make([]api.Message, n)
	for i := 0; i < n; i++ {
		switch i % 3 {
		case 0:
			msgs[i] = api.Message{Role: "user", Content: fmt.Sprintf("u%d", i)}
		case 1:
			msgs[i] = api.Message{Role: "assistant", Content: fmt.Sprintf("a%d", i)}
		case 2:
			msgs[i] = api.Message{Role: "user", Content: fmt.Sprintf("u%d-extra", i)}
		}
	}
	return msgs
}

// makeOverlappingHistory returns a 30-message list shaped so that the
// raw recentStart (= len - 12 = 18) is <= anchorEnd, forcing the
// overlap short-circuit ("Not enough distinct history beyond anchor +
// recent window to compact") to fire without touching the LLM.
//
// Layout (indices):
//
//	0:  system                                (anchor start)
//	1..17: alternating assistant-tc and tool  (skipped by compactAnchorEnd)
//	18: user "u18"                            (first user → anchorEnd = 19)
//	19: assistant "a19" plain                 (anchor extends to 20)
//	20..29: filler user/assistant pairs        (recent window)
//
// anchorEnd = 20. raw recentStart = 30 - 12 = 18. recentStart (18) <=
// anchorEnd (20) → overlap short-circuit fires.
func makeOverlappingHistory(n int) []api.Message {
	if n < 30 {
		n = 30
	}
	msgs := make([]api.Message, n)
	msgs[0] = api.Message{Role: "system", Content: "sys"}
	// Indices 1..17: alternating assistant-tc and tool (neither is a
	// user, so compactAnchorEnd scans past them).
	for i := 1; i <= 17; i++ {
		if i%2 == 1 {
			msgs[i] = api.Message{
				Role:      "assistant",
				Content:   fmt.Sprintf("prelude-tc-%d", i),
				ToolCalls: []api.ToolCall{{ID: fmt.Sprintf("c%d", i)}},
			}
		} else {
			msgs[i] = api.Message{
				Role:       "tool",
				Content:    fmt.Sprintf("prelude-tr-%d", i),
				ToolCallID: fmt.Sprintf("c%d", i-1),
			}
		}
	}
	// Index 18: first user → anchorEnd becomes 19.
	msgs[18] = api.Message{Role: "user", Content: "first-real-user"}
	// Index 19: immediate plain assistant → anchorEnd becomes 20.
	msgs[19] = api.Message{Role: "assistant", Content: "first-real-assistant"}
	// Indices 20..29: filler so the message list reaches n=30.
	for i := 20; i < n; i++ {
		switch i % 2 {
		case 0:
			msgs[i] = api.Message{Role: "user", Content: fmt.Sprintf("fill-u%d", i)}
		case 1:
			msgs[i] = api.Message{Role: "assistant", Content: fmt.Sprintf("fill-a%d", i)}
		}
	}
	return msgs
}

// makeMiddleTooSmallHistory returns a 30-message list where
// adjustRecentBoundary walks recentStart all the way down to the
// anchor, leaving a middle segment of size 0 and triggering the
// "Middle segment too small" early-exit.
//
// Layout (indices):
//
//	0:  system                                (anchor start)
//	1:  user "u0"                             (anchor user)
//	2:  assistant "a0" plain                  (anchor assistant, anchorEnd=3)
//	3..29: all assistant-tc                   (every slot triggers branch-2 walk-back)
//
// anchorEnd = 3. raw recentStart = 30 - 12 = 18. adjustRecentBoundary
// walks back via branch 2 (each iteration: messages[recentStart-1] is
// assistant-tc → walk). Walks: 18 → 17 → 16 → ... → 4 → 3. At
// recentStart=3, branch 2 checks messages[2] (assistant plain, no tool
// calls) → skip → break. got=3. middle = 3-3 = 0 < 6 → middle-too-
// small branch fires.
func makeMiddleTooSmallHistory() []api.Message {
	msgs := make([]api.Message, 30)
	msgs[0] = api.Message{Role: "system", Content: "sys"}
	msgs[1] = api.Message{Role: "user", Content: "u0"}
	msgs[2] = api.Message{Role: "assistant", Content: "a0"}
	// Indices 3..29: every slot is an assistant-with-tool-calls so
	// branch 2 keeps firing on each iteration.
	for i := 3; i < 30; i++ {
		msgs[i] = api.Message{
			Role:      "assistant",
			Content:   fmt.Sprintf("tc-%d", i),
			ToolCalls: []api.ToolCall{{ID: fmt.Sprintf("c%d", i)}},
		}
	}
	return msgs
}

func TestContains(t *testing.T) {
	tests := []struct {
		name  string
		slice []string
		item  string
		want  bool
	}{
		{
			name:  "item present",
			slice: []string{"apple", "banana", "cherry"},
			item:  "banana",
			want:  true,
		},
		{
			name:  "item absent",
			slice: []string{"apple", "banana", "cherry"},
			item:  "durian",
			want:  false,
		},
		{
			name:  "empty slice",
			slice: []string{},
			item:  "apple",
			want:  false,
		},
		{
			name:  "nil slice",
			slice: nil,
			item:  "apple",
			want:  false,
		},
		{
			name:  "item at start",
			slice: []string{"apple", "banana", "cherry"},
			item:  "apple",
			want:  true,
		},
		{
			name:  "item at end",
			slice: []string{"apple", "banana", "cherry"},
			item:  "cherry",
			want:  true,
		},
		{
			name:  "duplicate item",
			slice: []string{"apple", "apple", "banana"},
			item:  "apple",
			want:  true,
		},
		{
			name:  "empty string item",
			slice: []string{"apple", "", "banana"},
			item:  "",
			want:  true,
		},
		{
			name:  "case sensitive match",
			slice: []string{"Apple", "Banana"},
			item:  "apple",
			want:  false,
		},
		{
			name:  "special characters",
			slice: []string{"--flag", "-f", "output"},
			item:  "--flag",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contains(tt.slice, tt.item)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFilterArgs(t *testing.T) {
	tests := []struct {
		name  string
		slice []string
		item  string
		want  []string
	}{
		{
			name:  "filter one item",
			slice: []string{"apple", "banana", "cherry"},
			item:  "banana",
			want:  []string{"apple", "cherry"},
		},
		{
			name:  "filter item not present",
			slice: []string{"apple", "banana", "cherry"},
			item:  "durian",
			want:  []string{"apple", "banana", "cherry"},
		},
		{
			name:  "empty slice",
			slice: []string{},
			item:  "banana",
			want:  []string{},
		},
		{
			name:  "nil slice",
			slice: nil,
			item:  "banana",
			want:  []string{},
		},
		{
			name:  "filter all items",
			slice: []string{"banana", "banana", "banana"},
			item:  "banana",
			want:  []string{},
		},
		{
			name:  "filter duplicates",
			slice: []string{"apple", "banana", "apple", "cherry"},
			item:  "apple",
			want:  []string{"banana", "cherry"},
		},
		{
			name:  "filter empty string",
			slice: []string{"", "apple", "", "banana"},
			item:  "",
			want:  []string{"apple", "banana"},
		},
		{
			name:  "filter flag",
			slice: []string{"--json", "--verbose", "output"},
			item:  "--json",
			want:  []string{"--verbose", "output"},
		},
		{
			name:  "single item filtered",
			slice: []string{"banana"},
			item:  "banana",
			want:  []string{},
		},
		{
			name:  "single item kept",
			slice: []string{"apple"},
			item:  "banana",
			want:  []string{"apple"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterArgs(tt.slice, tt.item)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsLikelySlashCommandName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		// Valid names
		{
			name:  "simple lowercase",
			input: "help",
			want:  true,
		},
		{
			name:  "simple uppercase",
			input: "HELP",
			want:  true,
		},
		{
			name:  "mixed case",
			input: "HeLp",
			want:  true,
		},
		{
			name:  "with numbers",
			input: "model3",
			want:  true,
		},
		{
			name:  "with hyphen",
			input: "git-commit",
			want:  true,
		},
		{
			name:  "with underscore",
			input: "git_commit",
			want:  true,
		},
		{
			name:  "with all three",
			input: "git_commit-v2",
			want:  true,
		},
		{
			name:  "single letter",
			input: "a",
			want:  true,
		},
		{
			name:  "single number",
			input: "1",
			want:  true,
		},
		// Invalid names
		{
			name:  "with space",
			input: "git commit",
			want:  false,
		},
		{
			name:  "with slash",
			input: "git/commit",
			want:  false,
		},
		{
			name:  "with backslash",
			input: "git\\commit",
			want:  false,
		},
		{
			name:  "with pipe",
			input: "git|commit",
			want:  false,
		},
		{
			name:  "with semicolon",
			input: "git;commit",
			want:  false,
		},
		{
			name:  "with comma",
			input: "git,commit",
			want:  false,
		},
		{
			name:  "with period",
			input: "git.commit",
			want:  false,
		},
		{
			name:  "with exclamation",
			input: "git!",
			want:  false,
		},
		{
			name:  "with question",
			input: "git?",
			want:  false,
		},
		{
			name:  "with at",
			input: "git@",
			want:  false,
		},
		{
			name:  "with hash",
			input: "git#",
			want:  false,
		},
		{
			name:  "with dollar",
			input: "git$",
			want:  false,
		},
		{
			name:  "with percent",
			input: "git%",
			want:  false,
		},
		{
			name:  "with ampersand",
			input: "git&",
			want:  false,
		},
		{
			name:  "with asterisk",
			input: "git*",
			want:  false,
		},
		{
			name:  "with parentheses",
			input: "git()",
			want:  false,
		},
		{
			name:  "with brackets",
			input: "git[]",
			want:  false,
		},
		{
			name:  "with braces",
			input: "git{}",
			want:  false,
		},
		{
			name:  "with angle brackets",
			input: "git<>",
			want:  false,
		},
		// Edge cases
		{
			name:  "empty string",
			input: "",
			want:  true, // Loop doesn't execute, returns true by default
		},
		{
			name:  "only hyphen",
			input: "-",
			want:  true, // Single hyphen is valid
		},
		{
			name:  "only underscore",
			input: "_",
			want:  true, // Single underscore is valid
		},
		{
			name:  "starts with number",
			input: "3dmodel",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isLikelySlashCommandName(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestOutputWriter(t *testing.T) {
	tests := []struct {
		name    string
		writes  []string
		wantStr string
	}{
		{
			name:    "single write",
			writes:  []string{"hello"},
			wantStr: "hello",
		},
		{
			name:    "multiple writes",
			writes:  []string{"hello", " ", "world"},
			wantStr: "hello world",
		},
		{
			name:    "writes with newlines",
			writes:  []string{"line1\n", "line2\n"},
			wantStr: "line1\nline2\n",
		},
		{
			name:    "empty write",
			writes:  []string{""},
			wantStr: "",
		},
		{
			name:    "write bytes",
			writes:  []string{"hello", "\n", "world"},
			wantStr: "hello\nworld",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ow := &OutputWriter{}

			// Perform writes
			for _, w := range tt.writes {
				n, err := ow.Write([]byte(w))
				assert.NoError(t, err)
				assert.Equal(t, len(w), n)
			}

			// Check the string representation
			got := ow.String()
			assert.Equal(t, tt.wantStr, got)
		})
	}
}

func TestOutputWriterWrite(t *testing.T) {
	t.Run("Write returns byte count", func(t *testing.T) {
		ow := &OutputWriter{}
		testStr := "hello, world!"

		n, err := ow.Write([]byte(testStr))

		assert.NoError(t, err)
		assert.Equal(t, len(testStr), n)
	})

	t.Run("Write appends to buffer", func(t *testing.T) {
		ow := &OutputWriter{}

		ow.Write([]byte("first"))
		ow.Write([]byte(" "))
		ow.Write([]byte("second"))

		assert.Equal(t, "first second", ow.String())
	})

	t.Run("Write empty byte slice", func(t *testing.T) {
		ow := &OutputWriter{}

		n, err := ow.Write([]byte{})

		assert.NoError(t, err)
		assert.Equal(t, 0, n)
		assert.Equal(t, "", ow.String())
	})

	t.Run("Write to already initialized buffer", func(t *testing.T) {
		ow := &OutputWriter{Buffer: bytes.Buffer{}}
		ow.Buffer.WriteString("existing")

		ow.Write([]byte(" appended"))

		assert.Equal(t, "existing appended", ow.String())
	})
}

func TestOutputWriterString(t *testing.T) {
	t.Run("String returns buffer content", func(t *testing.T) {
		ow := &OutputWriter{}
		ow.Buffer.WriteString("test content")

		got := ow.String()
		assert.Equal(t, "test content", got)
	})

	t.Run("String on empty buffer", func(t *testing.T) {
		ow := &OutputWriter{}

		got := ow.String()
		assert.Equal(t, "", got)
	})

	t.Run("String returns copy, doesn't affect buffer", func(t *testing.T) {
		ow := &OutputWriter{}
		ow.Buffer.WriteString("original")

		str := ow.String()
		assert.Equal(t, "original", str)

		// Modify the returned string shouldn't affect buffer
		ow.Buffer.WriteString(" added")
		assert.Equal(t, "original", str)
		assert.Equal(t, "original added", ow.String())
	})
}

func TestCommandRegistryGetCommand(t *testing.T) {
	registry := NewCommandRegistry()

	tests := []struct {
		name        string
		cmdName     string
		wantCmdName string
		wantFound   bool
	}{
		{
			name:        "existing command help",
			cmdName:     "help",
			wantCmdName: "help",
			wantFound:   true,
		},
		{
			name:        "existing command model",
			cmdName:     "model",
			wantCmdName: "model",
			wantFound:   true,
		},
		{
			name:        "existing command provider",
			cmdName:     "provider",
			wantCmdName: "provider",
			wantFound:   true,
		},
		{
			name:        "existing command exec",
			cmdName:     "exec",
			wantCmdName: "exec",
			wantFound:   true,
		},
		{
			name:        "existing command commit",
			cmdName:     "commit",
			wantCmdName: "commit",
			wantFound:   true,
		},
		{
			name:        "non-existing command",
			cmdName:     "notreal",
			wantCmdName: "",
			wantFound:   false,
		},
		{
			name:        "empty command name",
			cmdName:     "",
			wantCmdName: "",
			wantFound:   false,
		},
		{
			name:        "command with invalid chars",
			cmdName:     "invalid/command",
			wantCmdName: "",
			wantFound:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, found := registry.GetCommand(tt.cmdName)
			if tt.wantFound {
				assert.True(t, found)
				assert.NotNil(t, cmd)
				assert.Equal(t, tt.wantCmdName, cmd.Name())
			} else {
				assert.False(t, found)
				assert.Nil(t, cmd)
			}
		})
	}
}

func TestCommandRegistryListCommands(t *testing.T) {
	registry := NewCommandRegistry()

	// Get all commands
	commands := registry.ListCommands()

	// Verify we got commands
	assert.NotNil(t, commands)
	assert.Greater(t, len(commands), 0)

	// Verify all commands are valid
	for _, cmd := range commands {
		assert.NotNil(t, cmd)
		name := cmd.Name()
		assert.NotEmpty(t, name)
		assert.NotEmpty(t, cmd.Description())
	}

	// Check for some expected commands
	commandNames := make(map[string]bool)
	for _, cmd := range commands {
		commandNames[cmd.Name()] = true
	}

	// Verify some expected commands exist
	expectedCommands := []string{
		"help", "model", "provider", "sessions", "clear",
		"exec", "shell", "commit", "changes", "status",
	}

	for _, expected := range expectedCommands {
		assert.True(t, commandNames[expected], "expected command %q to be in list", expected)
	}
}

func TestCommandRegistryListCommandsConsistency(t *testing.T) {
	registry := NewCommandRegistry()

	// List commands multiple times to ensure consistency
	commands1 := registry.ListCommands()
	commands2 := registry.ListCommands()

	assert.Equal(t, len(commands1), len(commands2), "command count should be consistent")

	// Create maps for comparison
	cmdMap1 := make(map[string]Command)
	for _, cmd := range commands1 {
		cmdMap1[cmd.Name()] = cmd
	}

	cmdMap2 := make(map[string]Command)
	for _, cmd := range commands2 {
		cmdMap2[cmd.Name()] = cmd
	}

	// Check that the same commands are present
	for name := range cmdMap1 {
		_, exists := cmdMap2[name]
		assert.True(t, exists, "command %q should be present in both lists", name)
	}

	for name := range cmdMap2 {
		_, exists := cmdMap1[name]
		assert.True(t, exists, "command %q should be present in both lists", name)
	}
}
