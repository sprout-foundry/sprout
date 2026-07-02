package commands

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

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
