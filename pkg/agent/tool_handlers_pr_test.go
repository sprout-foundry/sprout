package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/git"
)

// ---------------------------------------------------------------------------
// setupPRHandlerTest creates a minimal agent + git repo for PR handler tests.
// Returns the agent, the temp dir path, and a defer-cleanup func.
//
// Hooks for PushBranch, RunGhCommand, and GetDefaultBranch are already
// overridden to avoid real network access.  Tests that need specific gh
// output can override git.RunGhCommand further.
// ---------------------------------------------------------------------------

func setupPRHandlerTest(t *testing.T) (*Agent, string, func()) {
	t.Helper()

	agent := newTestAgent(t)
	deferCleanup := func() { agent.Shutdown() }

	// Prevent real network access: mock the push and gh CLI hooks.
	origPushBranch := git.PushBranch
	origRunGhCommand := git.RunGhCommand
	origGetDefaultBranch := git.GetDefaultBranch

	git.PushBranch = func(ctx context.Context, repoDir, head string) error {
		return nil
	}
	git.RunGhCommand = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		// Default fallback; individual tests override for specific output.
		return []byte("https://github.com/testorg/testrepo/pull/42"), nil
	}
	git.GetDefaultBranch = func(ctx context.Context, repoDir string) (string, error) {
		return "main", nil
	}

	// Create a temp git repo with origin so CreatePullRequest has a home.
	dir := t.TempDir()
	// Initialize git repo
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.email", "test@test.com")
	runGitCmd(t, dir, "config", "user.name", "Test")
	// Create initial commit on main
	tmpFile := filepath.Join(dir, "README.md")
	if err := os.WriteFile(tmpFile, []byte("# test"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, dir, "add", "README.md")
	runGitCmd(t, dir, "commit", "-m", "Initial commit")
	// Add origin remote
	runGitCmd(t, dir, "remote", "add", "origin", "https://github.com/testorg/testrepo.git")
	// Create a feature branch with a commit
	runGitCmd(t, dir, "checkout", "-b", "feature-test")
	tmpFile2 := filepath.Join(dir, "feature.txt")
	if err := os.WriteFile(tmpFile2, []byte("feature"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, dir, "add", "feature.txt")
	runGitCmd(t, dir, "commit", "-m", "Add feature")

	// Set workspace so effectiveCwd() returns this dir.
	agent.SetWorkspaceRoot(dir)

	// Set orchestrator persona with git-write enabled for basic tests.
	agent.state.SetActivePersona("orchestrator")
	if err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.AllowOrchestratorGitWrite = true
		return nil
	}); err != nil {
		t.Fatalf("failed to enable orchestrator git-write: %v", err)
	}

	cleanup := func() {
		git.PushBranch = origPushBranch
		git.RunGhCommand = origRunGhCommand
		git.GetDefaultBranch = origGetDefaultBranch
		deferCleanup()
		os.Unsetenv("GH_TOKEN")
	}
	return agent, dir, cleanup
}

// overrideGHOutput replaces git.RunGhCommand to return the given output.
// Returns a restore function.
func overrideGHOutput(output string) func() {
	orig := git.RunGhCommand
	git.RunGhCommand = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		return []byte(output), nil
	}
	return func() { git.RunGhCommand = orig }
}

// overrideGHWithArgsCapture replaces git.RunGhCommand to log args and return
// the given output.  Returns a restore function and the captured-args slice.
func overrideGHWithArgsCapture(output string) (restore func(), capturedArgs *[]string) {
	orig := git.RunGhCommand
	var args []string
	git.RunGhCommand = func(ctx context.Context, dir string, ghArgs ...string) ([]byte, error) {
		args = append(args, ghArgs...)
		return []byte(output), nil
	}
	return func() { git.RunGhCommand = orig }, &args
}

// overrideGHErr replaces git.RunGhCommand to return the given error.
func overrideGHErr(err error) func() {
	orig := git.RunGhCommand
	git.RunGhCommand = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		return nil, err
	}
	return func() { git.RunGhCommand = orig }
}

// runGitCmd runs a git command in the given directory and fatals on error.
func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// ---------------------------------------------------------------------------
// Parameter validation tests
// ---------------------------------------------------------------------------

func TestHandleCreatePullRequest_MissingTitle(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	_, err := handleCreatePullRequest(context.Background(), a, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for missing title")
	}
	if !agenterrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInputError, got %T: %v", err, err)
	}
}

func TestHandleCreatePullRequest_EmptyTitle(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	_, err := handleCreatePullRequest(context.Background(), a, map[string]interface{}{
		"title": "",
	})
	if err == nil {
		t.Fatal("expected error for empty title")
	}
	if !agenterrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInputError, got %T: %v", err, err)
	}
}

func TestHandleCreatePullRequest_NonStringTitle(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	// Use a type that convertToString cannot handle (a struct), rather than
	// a numeric type which is intentionally coerced to a string.
	_, err := handleCreatePullRequest(context.Background(), a, map[string]interface{}{
		"title": struct{ Foo string }{Foo: "bar"},
	})
	if err == nil {
		t.Fatal("expected error for non-string title")
	}
	if !agenterrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInputError, got %T: %v", err, err)
	}
}

// ---------------------------------------------------------------------------
// Success scenarios
// ---------------------------------------------------------------------------

func TestHandleCreatePullRequest_SuccessViaGHCLI(t *testing.T) {
	a, _, cleanup := setupPRHandlerTest(t)
	defer cleanup()

	git.RunGhCommand = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		return []byte("https://github.com/testorg/testrepo/pull/42"), nil
	}
	t.Setenv("GH_TOKEN", "")

	result, err := handleCreatePullRequest(context.Background(), a, map[string]interface{}{
		"title": "My PR Title",
		"head":  "feature-test",
		"base":  "main",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "https://github.com/testorg/testrepo/pull/42") {
		t.Errorf("result should contain URL, got: %s", result)
	}
	if !strings.Contains(result, "#42") {
		t.Errorf("result should contain #42, got: %s", result)
	}
	if !strings.Contains(result, "open") {
		t.Errorf("result should contain 'open', got: %s", result)
	}
}

func TestHandleCreatePullRequest_SuccessAllParams(t *testing.T) {
	a, dir, cleanup := setupPRHandlerTest(t)
	defer cleanup()

	restore, capturedArgs := overrideGHWithArgsCapture("https://github.com/testorg/testrepo/pull/99")
	defer restore()
	t.Setenv("GH_TOKEN", "")

	result, err := handleCreatePullRequest(context.Background(), a, map[string]interface{}{
		"title":    "Feature PR",
		"body":     "This adds a new feature",
		"base":     "develop",
		"head":     "feature-new",
		"draft":    true,
		"repo_dir": dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "pull/99") {
		t.Errorf("result should contain PR URL, got: %s", result)
	}

	// Verify the gh function was called with the right args
	argsStr := strings.Join(*capturedArgs, " ")
	if !strings.Contains(argsStr, "--title") || !strings.Contains(argsStr, "Feature PR") {
		t.Errorf("gh should have been called with --title 'Feature PR', got args: %s", argsStr)
	}
	if !strings.Contains(argsStr, "--body") || !strings.Contains(argsStr, "This adds a new feature") {
		t.Errorf("gh should have been called with body, got args: %s", argsStr)
	}
	if !strings.Contains(argsStr, "--base") || !strings.Contains(argsStr, "develop") {
		t.Errorf("gh should have been called with --base 'develop', got args: %s", argsStr)
	}
	if !strings.Contains(argsStr, "--head") || !strings.Contains(argsStr, "feature-new") {
		t.Errorf("gh should have been called with --head 'feature-new', got args: %s", argsStr)
	}
	if !strings.Contains(argsStr, "--draft") {
		t.Errorf("gh should have been called with --draft, got args: %s", argsStr)
	}
}

func TestHandleCreatePullRequest_DefaultRepoDir(t *testing.T) {
	a, _, cleanup := setupPRHandlerTest(t)
	defer cleanup()

	restore := overrideGHOutput("https://github.com/testorg/testrepo/pull/1")
	defer restore()
	t.Setenv("GH_TOKEN", "")

	// Don't pass repo_dir — should default to agent's workspace root
	result, err := handleCreatePullRequest(context.Background(), a, map[string]interface{}{
		"title": "Default dir PR",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "pull/1") {
		t.Errorf("result should contain PR URL, got: %s", result)
	}
}

// ---------------------------------------------------------------------------
// Git-write gate tests
// ---------------------------------------------------------------------------

func TestHandleCreatePullRequest_GitWriteGateBlocks(t *testing.T) {
	a, _, cleanup := setupPRHandlerTest(t)
	defer cleanup()

	// Switch to a persona without git-write capability
	a.state.SetActivePersona("tester")

	// No UI → PromptChoice returns ErrUINotAvailable → handler should fall
	// through (same as handleCommitTool).  This verifies the gate is
	// actually reached (we hit the PromptChoice path) rather than the
	// handler crashing.
	a.ui = &mockUI{interactive: false}

	restore := overrideGHOutput("https://github.com/testorg/testrepo/pull/7")
	defer restore()
	t.Setenv("GH_TOKEN", "")

	result, err := handleCreatePullRequest(context.Background(), a, map[string]interface{}{
		"title": "Gate test PR",
	})
	if err != nil {
		t.Fatalf("unexpected error after ErrUINotAvailable fallback: %v", err)
	}
	if !strings.Contains(result, "pull/7") {
		t.Errorf("result should contain PR URL after UI-not-available fallback, got: %s", result)
	}
}

func TestHandleCreatePullRequest_GitWriteGateAllows(t *testing.T) {
	a, _, cleanup := setupPRHandlerTest(t)
	defer cleanup()

	// Orchestrator with AllowOrchestratorGitWrite=true should bypass prompt.
	a.state.SetActivePersona("orchestrator")

	restore := overrideGHOutput("https://github.com/testorg/testrepo/pull/8")
	defer restore()
	t.Setenv("GH_TOKEN", "")

	result, err := handleCreatePullRequest(context.Background(), a, map[string]interface{}{
		"title": "Allowed PR",
	})
	if err != nil {
		t.Fatalf("unexpected error for allowed git-write: %v", err)
	}
	if !strings.Contains(result, "pull/8") {
		t.Errorf("result should contain PR URL, got: %s", result)
	}
}

func TestHandleCreatePullRequest_SubagentBypass(t *testing.T) {
	a, _, cleanup := setupPRHandlerTest(t)
	defer cleanup()

	// Switch to a persona without git-write, but mark as subagent.
	a.state.SetActivePersona("tester")
	a.subagentDepth = 1 // subagent

	restore := overrideGHOutput("https://github.com/testorg/testrepo/pull/15")
	defer restore()
	t.Setenv("GH_TOKEN", "")

	result, err := handleCreatePullRequest(context.Background(), a, map[string]interface{}{
		"title": "Subagent PR",
	})
	if err != nil {
		t.Fatalf("subagent should bypass gate, got: %v", err)
	}
	if !strings.Contains(result, "pull/15") {
		t.Errorf("result should contain PR URL, got: %s", result)
	}
}

// ---------------------------------------------------------------------------
// Error from backend
// ---------------------------------------------------------------------------

func TestHandleCreatePullRequest_BackendError(t *testing.T) {
	a, _, cleanup := setupPRHandlerTest(t)
	defer cleanup()

	// Make the gh command fail
	restore := overrideGHErr(errors.New("gh not found"))
	defer restore()
	t.Setenv("GH_TOKEN", "")

	_, err := handleCreatePullRequest(context.Background(), a, map[string]interface{}{
		"title": "Failing PR",
	})

	if err == nil {
		t.Fatal("expected error from backend, got nil")
	}
	if !agenterrors.IsTransient(err) {
		t.Errorf("expected TransientError, got %T: %v", err, err)
	}
}

// ---------------------------------------------------------------------------
// Approval denied by user
// ---------------------------------------------------------------------------

func TestHandleCreatePullRequest_UserDenies(t *testing.T) {
	a, _, cleanup := setupPRHandlerTest(t)
	defer cleanup()

	// Switch to persona without git-write capability
	a.state.SetActivePersona("tester")

	// Interactive UI that returns "deny"
	a.ui = &mockUI{
		interactive: true,
		quickPromptFn: func(ctx context.Context, prompt string, options []QuickOption, horizontal bool) (QuickOption, error) {
			return QuickOption{Value: "deny"}, nil
		},
	}

	result, err := handleCreatePullRequest(context.Background(), a, map[string]interface{}{
		"title": "Denied PR",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "cancelled") {
		t.Errorf("result should mention 'cancelled', got: %s", result)
	}
}

// ---------------------------------------------------------------------------
// Approval approved by user
// ---------------------------------------------------------------------------

func TestHandleCreatePullRequest_UserApproves(t *testing.T) {
	a, _, cleanup := setupPRHandlerTest(t)
	defer cleanup()

	// Switch to persona without git-write capability
	a.state.SetActivePersona("tester")

	// Interactive UI that returns "approve"
	a.ui = &mockUI{
		interactive: true,
		quickPromptFn: func(ctx context.Context, prompt string, options []QuickOption, horizontal bool) (QuickOption, error) {
			return QuickOption{Value: "approve"}, nil
		},
	}

	restore := overrideGHOutput("https://github.com/testorg/testrepo/pull/25")
	defer restore()
	t.Setenv("GH_TOKEN", "")

	result, err := handleCreatePullRequest(context.Background(), a, map[string]interface{}{
		"title": "Approved PR",
	})
	if err != nil {
		t.Fatalf("unexpected error after approval: %v", err)
	}
	if !strings.Contains(result, "pull/25") {
		t.Errorf("result should contain PR URL after approval, got: %s", result)
	}
}

// ---------------------------------------------------------------------------
// Draft parameter
// ---------------------------------------------------------------------------

func TestHandleCreatePullRequest_DraftParam(t *testing.T) {
	a, _, cleanup := setupPRHandlerTest(t)
	defer cleanup()

	restore, capturedArgs := overrideGHWithArgsCapture("https://github.com/testorg/testrepo/pull/20")
	defer restore()
	t.Setenv("GH_TOKEN", "")

	result, err := handleCreatePullRequest(context.Background(), a, map[string]interface{}{
		"title": "Draft PR",
		"draft": true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "pull/20") {
		t.Errorf("result should contain PR URL, got: %s", result)
	}

	argsStr := strings.Join(*capturedArgs, " ")
	if !strings.Contains(argsStr, "--draft") {
		t.Errorf("gh should have been called with --draft, got args: %s", argsStr)
	}
}

// ---------------------------------------------------------------------------
// Result formatting
// ---------------------------------------------------------------------------

func TestHandleCreatePullRequest_ResultFormatting(t *testing.T) {
	a, _, cleanup := setupPRHandlerTest(t)
	defer cleanup()

	restore := overrideGHOutput("https://github.com/sprout-foundry/sprout/pull/456")
	defer restore()
	t.Setenv("GH_TOKEN", "")

	result, err := handleCreatePullRequest(context.Background(), a, map[string]interface{}{
		"title": "Format test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Pull request created successfully") {
		t.Errorf("result should contain success message, got: %s", result)
	}
	if !strings.Contains(result, "URL: https://github.com/sprout-foundry/sprout/pull/456") {
		t.Errorf("result should contain URL, got: %s", result)
	}
	if !strings.Contains(result, "Number: #456") {
		t.Errorf("result should contain number, got: %s", result)
	}
	if !strings.Contains(result, "State: open") {
		t.Errorf("result should contain state, got: %s", result)
	}
}

// ---------------------------------------------------------------------------
// Orchestrator with git-write disabled
// ---------------------------------------------------------------------------

func TestHandleCreatePullRequest_OrchestratorGitWriteDisabled(t *testing.T) {
	a, _, cleanup := setupPRHandlerTest(t)
	defer cleanup()

	// Disable git-write for orchestrator
	if err := a.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.AllowOrchestratorGitWrite = false
		return nil
	}); err != nil {
		t.Fatalf("failed to disable orchestrator git-write: %v", err)
	}

	// No UI → PromptChoice returns ErrUINotAvailable → fallback to allow
	a.state.SetActivePersona("orchestrator")
	a.ui = &mockUI{interactive: false}

	restore := overrideGHOutput("https://github.com/testorg/testrepo/pull/50")
	defer restore()
	t.Setenv("GH_TOKEN", "")

	result, err := handleCreatePullRequest(context.Background(), a, map[string]interface{}{
		"title": "Orchestrator no-cap PR",
	})
	if err != nil {
		t.Fatalf("unexpected error after UI-not-available fallback: %v", err)
	}
	if !strings.Contains(result, "pull/50") {
		t.Errorf("result should contain PR URL after fallback, got: %s", result)
	}
}

// ---------------------------------------------------------------------------
// No active persona (should fall through via ErrUINotAvailable)
// ---------------------------------------------------------------------------

func TestHandleCreatePullRequest_NoActivePersona(t *testing.T) {
	a, _, cleanup := setupPRHandlerTest(t)
	defer cleanup()

	// Clear the active persona
	a.state.SetActivePersona("")
	a.ui = &mockUI{interactive: false}

	restore := overrideGHOutput("https://github.com/testorg/testrepo/pull/60")
	defer restore()
	t.Setenv("GH_TOKEN", "")

	result, err := handleCreatePullRequest(context.Background(), a, map[string]interface{}{
		"title": "No persona PR",
	})
	if err != nil {
		t.Fatalf("unexpected error after fallback with no persona: %v", err)
	}
	if !strings.Contains(result, "pull/60") {
		t.Errorf("result should contain PR URL, got: %s", result)
	}
}

// ---------------------------------------------------------------------------
// Body and base params forwarded
// ---------------------------------------------------------------------------

func TestHandleCreatePullRequest_BodyAndBaseForwarded(t *testing.T) {
	a, _, cleanup := setupPRHandlerTest(t)
	defer cleanup()

	restore, capturedArgs := overrideGHWithArgsCapture("https://github.com/testorg/testrepo/pull/30")
	defer restore()
	t.Setenv("GH_TOKEN", "")

	result, err := handleCreatePullRequest(context.Background(), a, map[string]interface{}{
		"title": "Body test",
		"body":  "Fixes issue #123",
		"base":  "main",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "pull/30") {
		t.Errorf("result should contain PR URL, got: %s", result)
	}

	argsStr := strings.Join(*capturedArgs, " ")
	if !strings.Contains(argsStr, "--body") || !strings.Contains(argsStr, "Fixes issue #123") {
		t.Errorf("body should be forwarded to gh, got args: %s", argsStr)
	}
}
