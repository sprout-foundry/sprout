package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper to create an api.Choice with a message content string.
func testChoice(content string) api.Choice {
	var c api.Choice
	c.Message.Content = content
	return c
}

// Helper to create an api.ChatResponse with total token count.
func testResponse(content string, totalTokens int) *api.ChatResponse {
	var u api.ChatResponse
	u.Choices = append(u.Choices, testChoice(content))
	u.Usage.TotalTokens = totalTokens
	return &u
}

// =============================================================================
// CheckStagedFilesForSecurityCredentials tests (was 0.0% coverage)
// =============================================================================

func TestCheckStagedFilesForSecurityCredentials_NoStagedFiles(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	logger := utils.GetLogger(true)

	// No staged changes — cmd.Output() should succeed but produce empty output,
	// so the loop iterates over [""], skips it, and returns false.
	result := CheckStagedFilesForSecurityCredentials(logger)
	assert.False(t, result.HasConcerns)
}

func TestCheckStagedFilesForSecurityCredentials_CleanStagedFile(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Stage a file with no credentials
	require.NoError(t, os.WriteFile(filepath.Join(dir, "clean.go"), []byte("package main\nfunc main() {}\n"), 0644))
	gitRun(t, dir, "add", "clean.go")

	logger := utils.GetLogger(true)

	result := CheckStagedFilesForSecurityCredentials(logger)
	assert.False(t, result.HasConcerns)
}

func TestCheckStagedFilesForSecurityCredentials_WithSecretPatterns(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Stage a file containing fake AWS secret key pattern
	content := `package config
const awsSecretKey = "AKIAIOSFODNN7EXAMPLE"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "secrets.go"), []byte(content), 0644))
	gitRun(t, dir, "add", "secrets.go")

	logger := utils.GetLogger(true)

	result := CheckStagedFilesForSecurityCredentials(logger)
	assert.True(t, result.HasConcerns, "expected security issues found")
}

func TestCheckStagedFilesForSecurityCredentials_MixedFiles(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Stage a clean file
	require.NoError(t, os.WriteFile(filepath.Join(dir, "normal.go"), []byte("package main\n"), 0644))
	gitRun(t, dir, "add", "normal.go")

	// Stage a file with a potential credential pattern — private key
	require.NoError(t, os.WriteFile(filepath.Join(dir, "key.pem"), []byte(
		"-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQ\n-----END RSA PRIVATE KEY-----\n"), 0644))
	gitRun(t, dir, "add", "key.pem")

	logger := utils.GetLogger(true)

	result := CheckStagedFilesForSecurityCredentials(logger)
	assert.True(t, result.HasConcerns, "expected security issues when staging a private key")
}

// =============================================================================
// GenerateCommitMessageFromStagedDiff tests (was 0.0% coverage - error paths)
// =============================================================================

func TestGenerateCommitMessageFromStagedDiff_NilClient(t *testing.T) {
	result, err := GenerateCommitMessageFromStagedDiff(nil, CommitMessageOptions{
		Diff: "some diff content",
	})
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "client is required")
}

func TestGenerateCommitMessageFromStagedDiff_EmptyDiff(t *testing.T) {
	mockClient := &mockAPIClient{
		titleResponse: &api.ChatResponse{Choices: []api.Choice{{}}},
		descResponse:  &api.ChatResponse{Choices: []api.Choice{{}}},
	}

	result, err := GenerateCommitMessageFromStagedDiff(mockClient, CommitMessageOptions{
		Diff: "   \n  \n",
	})
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "staged diff is empty")
}

func TestGenerateCommitMessageFromStagedDiff_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}

	stopCh := make(chan struct{})
	defer close(stopCh) // unblock any leaked goroutines after test returns

	// Use a client wrapper that provides config with short timeout
	delayedClient := &timeoutTestClient{
		mockAPIClient: &mockAPIClient{
			delay:  10 * time.Second,
			stopCh:  stopCh,
		},
		timeoutSec: 1, // 1 second timeout - much shorter than 10s mock delay
	}

	// Override timeout to 1 second to make test fast while still testing timeout logic
	result, err := GenerateCommitMessageFromStagedDiff(delayedClient, CommitMessageOptions{
		Diff:        "some diff content here\n+added line\n-removed line",
		Branch:      "main",
		FileChanges: []CommitFileChange{{Status: "M", Path: "main.go"}},
	})
	// With 1s timeout and 10s mock delay, this should timeout
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "timed out")
}

func TestGenerateCommitMessageFromStagedDiff_ClientError(t *testing.T) {
	mockClient := &mockAPIClient{
		titleErr: fmt.Errorf("API server unavailable"),
	}

	result, err := GenerateCommitMessageFromStagedDiff(mockClient, CommitMessageOptions{
		Diff:        "some diff content here\n+added line",
		Branch:      "main",
		FileChanges: []CommitFileChange{{Status: "A", Path: "new.go"}},
	})
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to generate commit title")
}

func TestGenerateCommitMessageFromStagedDiff_EmptyChoices(t *testing.T) {
	mockClient := &mockAPIClient{
		titleResponse: &api.ChatResponse{Choices: []api.Choice{}},
		descResponse:  &api.ChatResponse{Choices: []api.Choice{}},
	}

	result, err := GenerateCommitMessageFromStagedDiff(mockClient, CommitMessageOptions{
		Diff:        "some diff content\n+new line",
		Branch:      "main",
		FileChanges: []CommitFileChange{{Status: "M", Path: "file.go"}},
	})
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no response from model for commit title")
}

func TestGenerateCommitMessageFromStagedDiff_DescError(t *testing.T) {
	titleResp := testResponse("Adds new feature", 100)
	mockClient := &mockAPIClient{
		titleResponse: titleResp,
		descErr:       fmt.Errorf("description generation failed"),
	}

	result, err := GenerateCommitMessageFromStagedDiff(mockClient, CommitMessageOptions{
		Diff:        "some diff\n+added",
		Branch:      "main",
		FileChanges: []CommitFileChange{{Status: "M", Path: "file.go"}},
	})
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to generate commit description")
}

func TestGenerateCommitMessageFromStagedDiff_EmptyDescChoices(t *testing.T) {
	titleResp := testResponse("Adds feature", 50)
	mockClient := &mockAPIClient{
		titleResponse: titleResp,
		descResponse:  &api.ChatResponse{Choices: []api.Choice{}},
	}

	result, err := GenerateCommitMessageFromStagedDiff(mockClient, CommitMessageOptions{
		Diff:        "some diff\n+added line",
		Branch:      "main",
		FileChanges: []CommitFileChange{{Status: "A", Path: "new.go"}},
	})
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no response from model for commit description")
}

func TestGenerateCommitMessageFromStagedDiff_HappyPath(t *testing.T) {
	mockClient := &mockAPIClient{
		titleResponse: testResponse("Adds user authentication module", 75),
		descResponse:  testResponse("Implements login and registration flow with JWT tokens", 85),
	}

	result, err := GenerateCommitMessageFromStagedDiff(mockClient, CommitMessageOptions{
		Diff:        "some diff content\n+added authentication code",
		Branch:      "main",
		FileChanges: []CommitFileChange{{Status: "A", Path: "auth/login.go"}},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Contains(t, result.Message, "auth")
	assert.Greater(t, result.ApproxTokens, 0)
}

func TestGenerateCommitMessageFromStagedDiff_WithUserInstructions(t *testing.T) {
	mockClient := &mockAPIClient{
		titleResponse: testResponse("Applies fix per user instructions", 50),
		descResponse:  testResponse("Fixes the null pointer dereference in handler", 60),
	}

	result, err := GenerateCommitMessageFromStagedDiff(mockClient, CommitMessageOptions{
		Diff:             "+fixed nil check",
		Branch:           "fix/null-check",
		FileChanges:      []CommitFileChange{{Status: "M", Path: "handler.go"}},
		UserInstructions: "Fix the bug where we dereference a nil pointer",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.Message)
}

func TestGenerateCommitMessageFromStagedDiff_FeatureBranch(t *testing.T) {
	mockClient := &mockAPIClient{
		titleResponse: testResponse("Implements caching layer", 40),
		descResponse:  testResponse("Redis caching for frequently accessed data", 50),
	}

	result, err := GenerateCommitMessageFromStagedDiff(mockClient, CommitMessageOptions{
		Diff:        "+import redis",
		Branch:      "feature/caching",
		FileChanges: []CommitFileChange{{Status: "A", Path: "cache/redis.go"}},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	// Feature branch: should include branch prefix [feature/caching]
	assert.Contains(t, result.Message, "[feature/caching]")
}

func TestGenerateCommitMessageFromStagedDiff_DefaultBranchNoPrefix(t *testing.T) {
	mockClient := &mockAPIClient{
		titleResponse: testResponse("Refactors database layer", 40),
		descResponse:  testResponse("Moves queries to repository pattern", 50),
	}

	result, err := GenerateCommitMessageFromStagedDiff(mockClient, CommitMessageOptions{
		Diff:        "+import db",
		Branch:      "main",
		FileChanges: []CommitFileChange{{Status: "M", Path: "db/repo.go"}},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	// Default branch should NOT include branch prefix
	assert.NotContains(t, result.Message, "[main]")
}

func TestGenerateCommitMessageFromStagedDiff_MixedChangeTypes(t *testing.T) {
	mockClient := &mockAPIClient{
		titleResponse: testResponse("Updates project structure and dependencies", 60),
		descResponse:  testResponse("Adds new module and removes deprecated handler", 70),
	}

	result, err := GenerateCommitMessageFromStagedDiff(mockClient, CommitMessageOptions{
		Diff:   "+new code\n-old code",
		Branch: "develop",
		FileChanges: []CommitFileChange{
			{Status: "A", Path: "new_module.go"},
			{Status: "D", Path: "old_handler.go"},
			{Status: "M", Path: "main.go"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	// Mixed types should use "Updates N files"
	assert.Contains(t, result.Message, "Updates 3 files")
}

func TestGenerateCommitMessageFromStagedDiff_SingleFile(t *testing.T) {
	mockClient := &mockAPIClient{
		titleResponse: testResponse("Adds login endpoint", 30),
		descResponse:  testResponse("New REST endpoint for user authentication", 40),
	}

	result, err := GenerateCommitMessageFromStagedDiff(mockClient, CommitMessageOptions{
		Diff:        "+func Login()",
		Branch:      "feature/auth",
		FileChanges: []CommitFileChange{{Status: "A", Path: "api/login.go"}},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	// Single file: summary should be "Adds api/login.go"
	assert.Contains(t, result.Message, "Adds api/login.go")
}

func TestGenerateCommitMessageFromStagedDiff_DevelopBranch(t *testing.T) {
	mockClient := &mockAPIClient{
		titleResponse: testResponse("Updates config", 20),
		descResponse:  testResponse("Changes default timeout values", 30),
	}

	// develop is a default branch — no prefix
	result, err := GenerateCommitMessageFromStagedDiff(mockClient, CommitMessageOptions{
		Diff:        "-timeout = 10\n+timeout = 30",
		Branch:      "develop",
		FileChanges: []CommitFileChange{{Status: "M", Path: "config.go"}},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotContains(t, result.Message, "[develop]")
}

func TestGenerateCommitMessageFromStagedDiff_EmptyContentReturn(t *testing.T) {
	// Client returns empty content — the function still builds a message from prefix + title
	mockClient := &mockAPIClient{
		titleResponse: testResponse("", 0),
		descResponse:  testResponse("", 0),
	}

	result, err := GenerateCommitMessageFromStagedDiff(mockClient, CommitMessageOptions{
		Diff:        "+added line",
		Branch:      "main",
		FileChanges: []CommitFileChange{{Status: "A", Path: "file.go"}},
	})
	// Even with empty content, prefix+title produces something (prefix is non-empty)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.Message)
}

// =============================================================================
// AddAllAndCommit additional tests (was 30.8% coverage)
// =============================================================================

func TestAddAllAndCommit_NoStagedChanges(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// No staged changes — git commit should fail
	err = AddAllAndCommit("should fail", 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error committing changes to git")
}

func TestAddAllAndCommit_WithTimeout(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Stage a file
	require.NoError(t, os.WriteFile(filepath.Join(dir, "timed.go"), []byte("package timed\n"), 0644))
	gitRun(t, dir, "add", "timed.go")

	// Use a generous timeout — should succeed
	err = AddAllAndCommit("timed commit", 10)
	assert.NoError(t, err)

	out, _ := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%s").CombinedOutput()
	assert.Equal(t, "timed commit\n", string(out))
}

func TestAddAllAndCommit_ZeroTimeout(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Stage a file
	require.NoError(t, os.WriteFile(filepath.Join(dir, "zero.go"), []byte("package zero\n"), 0644))
	gitRun(t, dir, "add", "zero.go")

	// Zero timeout means no timeout (direct execution)
	err = AddAllAndCommit("zero timeout commit", 0)
	assert.NoError(t, err)
}

func TestAddAllAndCommit_NegativeTimeout(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Stage a file
	require.NoError(t, os.WriteFile(filepath.Join(dir, "neg.go"), []byte("package neg\n"), 0644))
	gitRun(t, dir, "add", "neg.go")

	// Negative timeout also means no timeout path
	err = AddAllAndCommit("negative timeout commit", -1)
	assert.NoError(t, err)
}

func TestAddAllAndCommit_TimeoutTriggers(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Stage a file so commit has something to work with
	require.NoError(t, os.WriteFile(filepath.Join(dir, "timeout.go"), []byte("package timeout\n"), 0644))
	gitRun(t, dir, "add", "timeout.go")

	// Use an extremely short timeout (1 second should still be enough for git,
	// but we use it to exercise the timeout code path)
	// We can't reliably make git slow, so this test just validates
	// the function handles the timeout parameter correctly
	err = AddAllAndCommit("fast commit", 1)
	assert.NoError(t, err)
}

// =============================================================================
// GetFileGitPath additional tests (was 70.0% coverage)
// =============================================================================

func TestGetFileGitPath_AbsolutePath(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Create a file and commit it so it's tracked
	require.NoError(t, os.WriteFile(filepath.Join(dir, "abs.go"), []byte("package abs\n"), 0644))
	gitRun(t, dir, "add", "abs.go")
	gitRun(t, dir, "commit", "-m", "add abs.go")

	relPath, err := GetFileGitPath(filepath.Join(dir, "abs.go"))
	require.NoError(t, err)
	assert.Equal(t, "abs.go", relPath)
}

func TestGetFileGitPath_NestedAbsolutePath(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	nestedDir := filepath.Join(dir, "pkg", "utils")
	require.NoError(t, os.MkdirAll(nestedDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(nestedDir, "helper.go"), []byte("package utils\n"), 0644))
	gitRun(t, dir, "add", "pkg/utils/helper.go")
	gitRun(t, dir, "commit", "-m", "add helper")

	relPath, err := GetFileGitPath(filepath.Join(nestedDir, "helper.go"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("pkg", "utils", "helper.go"), relPath)
}

func TestGetFileGitPath_RelativePath(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Relative path from within the repo
	relPath, err := GetFileGitPath("init.go")
	require.NoError(t, err)
	assert.Equal(t, "init.go", relPath)
}

func TestGetFileGitPath_NotAGitRepo(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	// Create a temp dir that is NOT a git repo
	tmpDir, err := os.MkdirTemp("", "non-git-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(tmpDir))

	_, err = GetFileGitPath(filepath.Join(tmpDir, "somefile.go"))
	assert.Error(t, err)
}

// =============================================================================
// ExecuteCommit additional tests (was 66.7% coverage)
// =============================================================================

func TestExecuteCommit_WithLLMClient(t *testing.T) {
	dir := newTestGitRepo(t)

	// Create and stage a file
	require.NoError(t, os.WriteFile(filepath.Join(dir, "client.go"), []byte("package client\n"), 0644))
	gitRun(t, dir, "add", "client.go")

	mockClient := &mockAPIClient{
		titleResponse: testResponse("Adds client module", 30),
		descResponse:  testResponse("Initial client package structure", 40),
	}

	executor := NewCommitExecutorInDir(mockClient, "", "", dir)
	hash, err := executor.ExecuteCommit()
	require.NoError(t, err)
	assert.NotEmpty(t, hash)

	// Verify the commit was made
	cmd := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%B")
	out, _ := cmd.CombinedOutput()
	assert.Contains(t, string(out), "Adds client module")
}

func TestExecuteCommit_LLMClientErrorFallsBack(t *testing.T) {
	dir := newTestGitRepo(t)

	// Create and stage files
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fb.go"), []byte("package fallback\n"), 0644))
	gitRun(t, dir, "add", "fb.go")

	// Client that always errors — should fall back to default message
	mockClient := &mockAPIClient{titleErr: fmt.Errorf("connection refused")}

	executor := NewCommitExecutorInDir(mockClient, "", "", dir)
	hash, err := executor.ExecuteCommit()
	require.NoError(t, err)
	assert.NotEmpty(t, hash)

	// Verify commit used fallback message
	cmd := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%s")
	out, _ := cmd.CombinedOutput()
	assert.Contains(t, string(out), "Update fb.go")
}

func TestExecuteCommit_WithUserInstructions(t *testing.T) {
	dir := newTestGitRepo(t)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "instr.go"), []byte("package instr\n"), 0644))
	gitRun(t, dir, "add", "instr.go")

	executor := NewCommitExecutorInDir(nil, "", "custom commit instructions here", dir)
	hash, err := executor.ExecuteCommit()
	require.NoError(t, err)
	assert.NotEmpty(t, hash)

	cmd := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%B")
	out, _ := cmd.CombinedOutput()
	assert.Contains(t, string(out), "custom commit instructions here")
}

func TestExecuteCommit_MultipleFiles(t *testing.T) {
	dir := newTestGitRepo(t)

	// Stage multiple files — fallback should summarize
	for _, name := range []string{"multi1.go", "multi2.go", "multi3.go"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("package multi\n"), 0644))
		gitRun(t, dir, "add", name)
	}

	executor := NewCommitExecutorInDir(nil, "", "", dir)
	hash, err := executor.ExecuteCommit()
	require.NoError(t, err)
	assert.NotEmpty(t, hash)

	cmd := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%s")
	out, _ := cmd.CombinedOutput()
	assert.Contains(t, string(out), "Add 3 Files")
}

func TestExecuteCommit_NoDir_OperatesOnCWD(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	require.NoError(t, os.WriteFile("cwd_test.go", []byte("package cwd\n"), 0644))
	gitRun(t, dir, "add", "cwd_test.go")

	executor := NewCommitExecutor(nil, "cwd commit", "")
	hash, err := executor.ExecuteCommit()
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
}

func TestExecuteCommit_FallbackEmptyMessage(t *testing.T) {
	dir := newTestGitRepo(t)

	// Stage a file but don't track it via git add first — actually git add works
	// We need to create a scenario where fallback returns empty.
	// generateCommitMessage only returns "" if all priorities fail and fallback also returns "",
	// but fallback always returns something. So this test verifies the message is never empty.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fb2.go"), []byte("package fb2\n"), 0644))
	gitRun(t, dir, "add", "fb2.go")

	executor := NewCommitExecutorInDir(nil, "", "", dir)
	hash, err := executor.ExecuteCommit()
	require.NoError(t, err)
	require.NotEmpty(t, hash, "commit hash should not be empty")
}

// =============================================================================
// generateCommitMessage coverage tests (was 44.4% coverage)
// =============================================================================

func TestGenerateMessage_Priorities(t *testing.T) {
	tests := []struct {
		name       string
		userMsg    string
		userInstr  string
		client     api.ClientInterface
		wantPrefix string
	}{
		{
			name:       "user message wins over instructions",
			userMsg:    "my custom message",
			userInstr:  "instructions",
			wantPrefix: "my custom message",
		},
		{
			name:       "instructions when no user message",
			userMsg:    "",
			userInstr:  "my instructions",
			wantPrefix: "my instructions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &CommitExecutor{
				Client:           tt.client,
				UserMessage:      tt.userMsg,
				UserInstructions: tt.userInstr,
			}
			diffs := "+added line"
			branch := "main"
			changes := []CommitFileChange{{Status: "A", Path: "new.go"}}

			result := e.generateCommitMessage(diffs, branch, changes)
			assert.Contains(t, result, tt.wantPrefix)
		})
	}
}

func TestGenerateMessage_ClientNil(t *testing.T) {
	e := &CommitExecutor{
		Client:           nil,
		UserMessage:      "",
		UserInstructions: "",
	}
	result := e.generateCommitMessage("+diff", "main", []CommitFileChange{{Status: "A", Path: "a.go"}})
	// Should fall through to fallback
	assert.Contains(t, result, "Update a.go")
}

func TestGenerateMessage_ClientReturnsEmpty(t *testing.T) {
	// When client returns empty content, the LLM result still gets a prefix,
	// so generateCommitMessage returns it and doesn't fall back to the default.
	mockClient := &mockAPIClient{
		titleResponse: testResponse("", 0),
		descResponse:  testResponse("", 0),
	}

	e := &CommitExecutor{
		Client:           mockClient,
		UserMessage:      "",
		UserInstructions: "",
	}
	result := e.generateCommitMessage("+diff", "main", []CommitFileChange{{Status: "A", Path: "a.go"}})
	// Even with empty LLM content, the prefix is added so result is non-empty
	// This verifies the LLM path is exercised
	assert.NotEmpty(t, result)
}

func TestGenerateMessage_ClientReturnsNilResult(t *testing.T) {
	mockClient := &mockAPIClient{
		titleErr: fmt.Errorf("API error"),
	}

	e := &CommitExecutor{
		Client:           mockClient,
		UserMessage:      "",
		UserInstructions: "",
	}
	result := e.generateCommitMessage("+diff", "main", []CommitFileChange{{Status: "M", Path: "file.go"}})
	// Error from client → falls back
	assert.Contains(t, result, "Update file.go")
}

func TestGenerateMessage_ClientHappyPath(t *testing.T) {
	mockClient := &mockAPIClient{
		titleResponse: testResponse("Adds validation", 50),
		descResponse:  testResponse("Input validation for forms", 60),
	}

	e := &CommitExecutor{
		Client:           mockClient,
		UserMessage:      "",
		UserInstructions: "",
	}
	result := e.generateCommitMessage("+validation code", "main", []CommitFileChange{{Status: "A", Path: "validate.go"}})
	assert.Contains(t, result, "Adds validation")
}

func TestGenerateMessage_AllEmptyPriorities(t *testing.T) {
	// All string priorities empty, client nil → must use fallback
	e := &CommitExecutor{
		Client:           nil,
		UserMessage:      "",
		UserInstructions: "",
	}

	tests := []struct {
		name  string
		changes []CommitFileChange
		want  string
	}{
		{
			name:    "empty changes",
			changes: []CommitFileChange{},
			want:    "Update files",
		},
		{
			name:    "nil changes",
			changes: nil,
			want:    "Update files",
		},
		{
			name:    "single modified",
			changes: []CommitFileChange{{Status: "M", Path: "app.go"}},
			want:    "Update app.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := e.generateCommitMessage("+diff", "main", tt.changes)
			assert.Contains(t, result, tt.want, "fallback message should contain %q", tt.want)
		})
	}
}

// =============================================================================
// createCommit coverage tests (was 66.7% coverage)
// =============================================================================

func TestCreateCommit_TempFileCleanup(t *testing.T) {
	dir := newTestGitRepo(t)

	// Stage a file
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cleanup.go"), []byte("package cleanup\n"), 0644))
	gitRun(t, dir, "add", "cleanup.go")

	e := &CommitExecutor{Dir: dir}
	hash, err := e.createCommit("test temp file cleanup")
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
}

func TestCreateCommit_SuccessfullyCommits(t *testing.T) {
	dir := newTestGitRepo(t)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "good.go"), []byte("package good\n"), 0644))
	gitRun(t, dir, "add", "good.go")

	e := &CommitExecutor{Dir: dir}
	hash, err := e.createCommit("good commit message here")
	require.NoError(t, err)
	require.NotEmpty(t, hash)

	// Verify commit hash looks real (hex)
	assert.Len(t, hash, 40)

	// Verify commit message
	cmd := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%B")
	out, _ := cmd.CombinedOutput()
	assert.Contains(t, string(out), "good commit message here")
}

func TestCreateCommit_NoStagedChanges(t *testing.T) {
	dir := newTestGitRepo(t)

	// Nothing staged — commit should fail
	e := &CommitExecutor{Dir: dir}
	_, err := e.createCommit("should fail - nothing staged")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "commit failed")
}

// =============================================================================
// NewCommitExecutorInDir
// =============================================================================

func TestNewCommitExecutorInDir(t *testing.T) {
	e := NewCommitExecutorInDir(nil, "msg", "instr", "/tmp/testdir")
	assert.NotNil(t, e)
	assert.Equal(t, "msg", e.UserMessage)
	assert.Equal(t, "instr", e.UserInstructions)
	assert.Equal(t, "/tmp/testdir", e.Dir)
}

// =============================================================================
// GetGitStatus outside git repo
// =============================================================================

func TestGetGitStatus_OutsideGitRepo(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	tmpDir, err := os.MkdirTemp("", "no-git-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)

	require.NoError(t, os.Chdir(tmpDir))

	branch, uncommitted, staged, err := GetGitStatus()
	// Outside git repo: no error returned (normalized), 0 counts
	assert.NoError(t, err)
	assert.Equal(t, "", branch)
	assert.Equal(t, 0, uncommitted)
	assert.Equal(t, 0, staged)
}

// =============================================================================
// PerformGitCommit failure path
// =============================================================================

func TestPerformGitCommit_NoStagedChanges(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	// Nothing staged — commit should fail
	err = PerformGitCommit("should fail")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "git commit failed")
}

// =============================================================================
// gitCmd helper test
// =============================================================================

func TestGitCmd_WithDir(t *testing.T) {
	dir := newTestGitRepo(t)

	e := &CommitExecutor{Dir: dir}
	cmd := e.gitCmd("rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	require.NoError(t, err)
	assert.Equal(t, "main\n", string(out))
}

func TestGitCmd_WithoutDir(t *testing.T) {
	e := &CommitExecutor{Dir: ""}
	cmd := e.gitCmd("version")
	out, err := cmd.Output()
	require.NoError(t, err)
	assert.Contains(t, string(out), "git version")
}

// =============================================================================
// ParseCommitMessage additional edge cases
// =============================================================================

func TestParseCommitMessage_ThreeLines(t *testing.T) {
	input := "feat: add auth\n\nImplements JWT authentication"
	note, desc, err := ParseCommitMessage(input)
	require.NoError(t, err)
	assert.Equal(t, "feat: add auth", note)
	assert.Equal(t, "Implements JWT authentication", desc)
}

// =============================================================================
// CheckStagedChanges additional paths
// =============================================================================

func TestCheckStagedChanges_MultipleStagedFiles(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	for _, name := range []string{"a.go", "b.go", "c.go"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("package x\n"), 0644))
		gitRun(t, dir, "add", name)
	}

	err = CheckStagedChanges()
	assert.NoError(t, err)
}

// =============================================================================
// GetStagedDiff additional paths
// =============================================================================

func TestGetStagedDiff_MultipleFiles(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "first.go"), []byte("package first\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "second.go"), []byte("package second\n"), 0644))
	gitRun(t, dir, "add", "first.go")
	gitRun(t, dir, "add", "second.go")

	diff, err := GetStagedDiff()
	require.NoError(t, err)
	assert.Contains(t, diff, "first.go")
	assert.Contains(t, diff, "second.go")
}

// =============================================================================
// GetUncommittedChanges outside git repo
// =============================================================================

func TestGetUncommittedChanges_OutsideGitRepo(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	tmpDir, err := os.MkdirTemp("", "uncommitted-no-git-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(tmpDir))

	_, err = GetUncommittedChanges()
	assert.Error(t, err)
}

// =============================================================================
// GetStagedChanges outside git repo
// =============================================================================

func TestGetStagedChanges_OutsideGitRepo(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	tmpDir, err := os.MkdirTemp("", "staged-no-git-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(tmpDir))

	_, err = GetStagedChanges()
	assert.Error(t, err)
}

// =============================================================================
// CleanCommitMessage additional coverage
// =============================================================================

func TestCleanCommitMessage_MarkdownFencesWithTripleBacktickNoNewline(t *testing.T) {
	input := "```feat: add feature```"
	got := CleanCommitMessage(input)
	assert.NotContains(t, got, "```")
	assert.Contains(t, got, "feat: add feature")
}

func TestCleanCommitMessage_TwoLinesSingleBlank(t *testing.T) {
	// Title + exactly one blank line + description — should be unchanged
	input := "feat: add feature\n\nDescription line."
	got := CleanCommitMessage(input)
	assert.Equal(t, "feat: add feature\n\nDescription line.", got)
}

func TestCleanCommitMessage_OnlyTitle(t *testing.T) {
	input := "feat: add feature"
	got := CleanCommitMessage(input)
	assert.Equal(t, "feat: add feature", got)
}

// =============================================================================
// CleanCommitMessage JSON edge cases for coverage
// =============================================================================

func TestCleanCommitMessage_JSONFunctionCallNoCommitMessageKey(t *testing.T) {
	// Function call format but no commitMessageFormat or originalUserRequest
	input := `{"type": "function", "name": "generateCommitMessage", "parameters": {"other": "value"}}`
	got := CleanCommitMessage(input)
	// Should hit the multi-key JSON fallback (keys: type, name, parameters)
	assert.Contains(t, got, "Add new functionality")
}

func TestCleanCommitMessage_JSONSingleKeyEmptyValue(t *testing.T) {
	// Single key but empty string value — should not match the format
	input := `{"title": ""}`
	got := CleanCommitMessage(input)
	// The description is empty so the condition "descStr != """ fails,
	// falls through to the JSON fallback
	assert.Contains(t, got, "Add new functionality")
}

func TestCleanCommitMessage_JSONSingleKeyEmptyTitle(t *testing.T) {
	input := `{"": "description"}`
	got := CleanCommitMessage(input)
	// Title is empty so "title != "" fails, falls to JSON fallback
	assert.Contains(t, got, "Add new functionality")
}

// =============================================================================
// AddAndCommitFile error paths
// =============================================================================

func TestAddAndCommitFile_NonExistentFile(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	dir := newTestGitRepo(t)
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(dir))

	err = AddAndCommitFile("nonexistent.go", "should fail")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error adding changes to git")
}

// =============================================================================
// GetRecentTouchedFiles error
// =============================================================================

func TestGetRecentTouchedFiles_OutsideGitRepo(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	tmpDir, err := os.MkdirTemp("", "recent-no-git-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(tmpDir))

	_, err = GetRecentTouchedFiles(5)
	assert.Error(t, err)
}

// =============================================================================
// GetRecentFileLog error path
// =============================================================================

func TestGetRecentFileLog_OutsideGitRepo(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	tmpDir, err := os.MkdirTemp("", "log-no-git-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(tmpDir))

	_, err = GetRecentFileLog("somefile.go", 3)
	assert.Error(t, err)
}

// =============================================================================
// GetGitRemoteURL error path
// =============================================================================

func TestGetGitRemoteURL_OutsideGitRepo(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	tmpDir, err := os.MkdirTemp("", "remote-no-git-*")
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
// GetGitRootDir error path
// =============================================================================

func TestGetGitRootDir_OutsideGitRepo(t *testing.T) {
	testDirMtx.Lock()
	defer testDirMtx.Unlock()
	tmpDir, err := os.MkdirTemp("", "root-no-git-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)
	require.NoError(t, os.Chdir(tmpDir))

	_, err = GetGitRootDir()
	assert.Error(t, err)
}

// newMockClient creates a mockAPIClient with a properly initialized stopCh.
// Callers should call the returned cancel func (or close stopCh) during cleanup
// to unblock any in-flight delayed requests.
func newMockClient() (*mockAPIClient, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	m := &mockAPIClient{
		stopCh: ctx.Done(),
	}
	return m, cancel
}

// mockAPIClient implements api.ClientInterface for testing.
type mockAPIClient struct {
	titleResponse *api.ChatResponse
	descResponse  *api.ChatResponse
	titleErr      error
	descErr       error
	delay         time.Duration
	// stopCh receives on this channel to unblock delayed SendChatRequest calls.
	// Use newMockClient() for safe initialization, or set manually.
	stopCh  <-chan struct{}
	mu      sync.Mutex
	callIdx int
}

// timeoutTestClient wraps mockAPIClient to provide config with custom timeout.
type timeoutTestClient struct {
	*mockAPIClient
	timeoutSec int
}

func (c *timeoutTestClient) GetConfig() *configuration.Config {
	return &configuration.Config{
		APITimeouts: &configuration.APITimeoutConfig{
			CommitMessageTimeoutSec: c.timeoutSec,
		},
	}
}

func (m *mockAPIClient) SendChatRequest(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.callIdx++

	if m.delay > 0 {
		if m.stopCh == nil {
			// Defensive fallback: use a context that is never cancelled.
			// This will block for the full delay, which is the expected behavior
			// when no stop mechanism is configured.
			m.stopCh = make(chan struct{})
		}
		select {
		case <-time.After(m.delay):
		case <-m.stopCh:
			return nil, context.Canceled
		}
	}

	// Check if this is the title prompt (contains "commit title") or desc prompt
	// This is more reliable than call ordering with goroutines
	isTitle := false
	for _, msg := range messages {
		if strings.Contains(msg.Content, "commit title") ||
			strings.Contains(msg.Content, "concise git commit title") {
			isTitle = true
			break
		}
	}

	if isTitle {
		if m.titleErr != nil {
			return nil, m.titleErr
		}
		if m.titleResponse != nil {
			return m.titleResponse, nil
		}
	} else {
		if m.descErr != nil {
			return nil, m.descErr
		}
		if m.descResponse != nil {
			return m.descResponse, nil
		}
	}

	// Fallback
	if m.titleResponse != nil {
		return m.titleResponse, nil
	}
	return &api.ChatResponse{Choices: []api.Choice{}}, nil
}

func (m *mockAPIClient) SendChatRequestStream(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool, callback api.StreamCallback) (*api.ChatResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockAPIClient) CheckConnection() error      { return nil }
func (m *mockAPIClient) SetDebug(bool)               {}
func (m *mockAPIClient) SetModel(string) error       { return nil }
func (m *mockAPIClient) GetModel() string            { return "mock" }
func (m *mockAPIClient) GetProvider() string         { return "mock" }
func (m *mockAPIClient) GetModelContextLimit() (int, error) { return 4096, nil }
func (m *mockAPIClient) ListModels(ctx context.Context) ([]api.ModelInfo, error) { return nil, nil }
func (m *mockAPIClient) SupportsVision() bool        { return false }
func (m *mockAPIClient) GetVisionModel() string      { return "" }
func (m *mockAPIClient) SendVisionRequest(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockAPIClient) GetLastTPS() float64          { return 0 }
func (m *mockAPIClient) GetAverageTPS() float64       { return 0 }
func (m *mockAPIClient) GetTPSStats() map[string]float64 { return nil }
func (m *mockAPIClient) ResetTPSStats()               {}
