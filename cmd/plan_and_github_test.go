package cmd

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
)

// =============================================================================
// cmd/plan.go — runPlanMode
// =============================================================================

func TestRunPlanMode_NonTTY_WithArgs(t *testing.T) {
	err := runPlanMode([]string{"build a thing"})
	if err == nil {
		t.Fatal("expected error for non-TTY stdin even with args, got nil")
	}
}

func TestRunPlanMode_Continue_NonExistFile(t *testing.T) {
	// Set planContinue so it enters the continue path.
	// Even if stdin were a TTY, the file doesn't exist.
	origContinue := planContinue
	origOutput := planOutputFile
	planContinue = true
	planOutputFile = ""
	defer func() {
		planContinue = origContinue
		planOutputFile = origOutput
	}()

	err := runPlanMode([]string{"/tmp/nonexistent_plan_file_12345.md"})
	if err == nil {
		t.Fatal("expected error for non-existent plan file, got nil")
	}
	// Should get either the TTY error (checked first) or the file-not-found error
	// depending on whether stdin is a terminal. In CI it won't be a TTY.
	if !strings.Contains(err.Error(), "terminal") && !strings.Contains(err.Error(), "plan file") {
		t.Errorf("expected file-not-found or TTY error, got: %v", err)
	}
}

func TestRunPlanMode_Continue_NonExistOutputFile(t *testing.T) {
	origContinue := planContinue
	origOutput := planOutputFile
	planContinue = true
	planOutputFile = "/tmp/nonexistent_output_plan_67890.md"
	defer func() {
		planContinue = origContinue
		planOutputFile = origOutput
	}()

	err := runPlanMode(nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// =============================================================================
// cmd/plan.go — createPlanningAgent
// =============================================================================

func TestCreatePlanningAgent_WithCustomModel(t *testing.T) {
	origModel := planModel
	origProvider := planProvider
	planModel = "claude-3-opus"
	planProvider = ""
	defer func() {
		planModel = origModel
		planProvider = origProvider
	}()

	a, err := createPlanningAgent()
	if err != nil {
		t.Fatalf("createPlanningAgent() with custom model unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}
	// Verify system prompt contains planning content
	prompt := a.GetSystemPrompt()
	if prompt == "" {
		t.Fatal("expected non-empty system prompt")
	}
	lower := strings.ToLower(prompt)
	if !strings.Contains(lower, "plan") {
		t.Error("expected planning system prompt to contain 'plan'")
	}
}

func TestCreatePlanningAgent_WithCustomProviderAndModel(t *testing.T) {
	origModel := planModel
	origProvider := planProvider
	planModel = "gpt-4o"
	planProvider = "openrouter"
	defer func() {
		planModel = origModel
		planProvider = origProvider
	}()

	a, err := createPlanningAgent()
	if err != nil {
		t.Fatalf("createPlanningAgent() with provider+model unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}
}

func TestCreatePlanningAgent_PlanCreateTodosFalse(t *testing.T) {
	origModel := planModel
	origProvider := planProvider
	origCreateTodos := planCreateTodos
	planModel = ""
	planProvider = ""
	planCreateTodos = false
	defer func() {
		planModel = origModel
		planProvider = origProvider
		planCreateTodos = origCreateTodos
	}()

	a, err := createPlanningAgent()
	if err != nil {
		t.Fatalf("createPlanningAgent() with todos=false returned error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}

	// Verify the planning prompt is loaded successfully with todos=false
	prompt := a.GetSystemPrompt()
	if prompt == "" {
		t.Fatal("expected non-empty system prompt with todos=false")
	}
}

// =============================================================================
// cmd/plan.go — runSeamlessPlanning
// =============================================================================

func TestRunSeamlessPlanning_ExitImmediate(t *testing.T) {
	// Provide "exit\n" as stdin so the loop terminates immediately.
	a, err := createPlanningAgent()
	if err != nil {
		t.Fatalf("createPlanningAgent() error: %v", err)
	}

	// Pipe "exit" into stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}

	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	// Write "exit\n" and close
	go func() {
		w.WriteString("exit\n")
		w.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = runSeamlessPlanning(ctx, a, "test query")
	if err != nil {
		t.Errorf("runSeamlessPlanning with 'exit' input returned error: %v", err)
	}
}

func TestRunSeamlessPlanning_QuitCommands(t *testing.T) {
	commands := []string{"quit", "q"}
	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			a, err := createPlanningAgent()
			if err != nil {
				t.Fatalf("createPlanningAgent() error: %v", err)
			}

			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("failed to create pipe: %v", err)
			}

			origStdin := os.Stdin
			os.Stdin = r
			defer func() { os.Stdin = origStdin }()

			go func() {
				w.WriteString(cmd + "\n")
				w.Close()
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			err = runSeamlessPlanning(ctx, a, "test query")
			if err != nil {
				t.Errorf("runSeamlessPlanning with %q returned error: %v", cmd, err)
			}
		})
	}
}

func TestRunSeamlessPlanning_ContextCancelled(t *testing.T) {
	a, err := createPlanningAgent()
	if err != nil {
		t.Fatalf("createPlanningAgent() error: %v", err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}

	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	// Keep stdin open (never write anything), but cancel context after short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		w.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a brief moment — context check happens at end of loop iteration
	// after reading from stdin. The ReadString will block until the pipe is closed.
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	err = runSeamlessPlanning(ctx, a, "test query")
	// Should return context.Canceled or nil (if exit was read)
	if err != nil && !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("expected context canceled error, got: %v", err)
	}
}

// =============================================================================
// cmd/plan.go — planCmd cobra command registration
// =============================================================================

func TestPlanCmd_Registered(t *testing.T) {
	if planCmd == nil {
		t.Fatal("planCmd is nil, not registered")
	}
	if planCmd.Use != "plan [initial_idea]" {
		t.Errorf("planCmd.Use = %q, want %q", planCmd.Use, "plan [initial_idea]")
	}
	if planCmd.Short == "" {
		t.Error("planCmd.Short should not be empty")
	}
}

func TestPlanCmd_HasExpectedFlags(t *testing.T) {
	expectedFlags := []string{"model", "provider", "output", "continue", "todos"}
	for _, name := range expectedFlags {
		f := planCmd.Flags().Lookup(name)
		if f == nil {
			t.Errorf("planCmd missing flag %q", name)
		}
	}
}

func TestPlanCmd_MaxNArgs(t *testing.T) {
	// planCmd should accept at most 1 arg
	if planCmd.Args == nil {
		t.Fatal("planCmd.Args is nil")
	}
	// Verify it rejects >1 args
	err := planCmd.Args(planCmd, []string{"a", "b"})
	if err == nil {
		t.Error("expected error for 2 args (MaximumNArgs(1)), got nil")
	}
	// Verify it accepts 0 args
	err = planCmd.Args(planCmd, []string{})
	if err != nil {
		t.Errorf("unexpected error for 0 args: %v", err)
	}
	// Verify it accepts 1 arg
	err = planCmd.Args(planCmd, []string{"my idea"})
	if err != nil {
		t.Errorf("unexpected error for 1 arg: %v", err)
	}
}

func TestPlanCmd_FlagShorthands(t *testing.T) {
	shorthandTests := []struct {
		name     string
		flag     string
		shorthand string
	}{
		{"model shorthand", "model", "m"},
		{"provider shorthand", "provider", "p"},
		{"output shorthand", "output", "o"},
		{"continue shorthand", "continue", "c"},
		{"todos shorthand", "todos", "t"},
	}
	for _, tt := range shorthandTests {
		t.Run(tt.name, func(t *testing.T) {
			f := planCmd.Flags().Lookup(tt.flag)
			if f == nil {
				t.Fatalf("flag %q not found", tt.flag)
			}
			if f.Shorthand != tt.shorthand {
				t.Errorf("flag %q shorthand = %q, want %q", tt.flag, f.Shorthand, tt.shorthand)
			}
		})
	}
}

// =============================================================================
// cmd/plan.go — flag defaults (already tested in cmd_coverage_test.go,
// but duplicated here to ensure the file is properly exercised)
// =============================================================================

func TestPlanFlagDefaults_Also(t *testing.T) {
	if planModel != "" {
		t.Errorf("planModel default should be empty, got %q", planModel)
	}
	if planProvider != "" {
		t.Errorf("planProvider default should be empty, got %q", planProvider)
	}
	if planOutputFile != "" {
		t.Errorf("planOutputFile default should be empty, got %q", planOutputFile)
	}
	if planContinue {
		t.Errorf("planContinue default should be false")
	}
	if !planCreateTodos {
		t.Errorf("planCreateTodos default should be true")
	}
}

// =============================================================================
// cmd/github_setup_prompt.go — promptGitHubMCPSetupIfNeeded
// =============================================================================

func TestPromptGitHubMCPSetupIfNeeded_SkipPromptDirect(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	cfg := a.GetConfigManager().GetConfig()
	cfg.SkipPrompt = true

	// Should return immediately without prompting
	done := make(chan struct{})
	go func() {
		promptGitHubMCPSetupIfNeeded(a)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("promptGitHubMCPSetupIfNeeded did not return with SkipPrompt=true")
	}
}

func TestPromptGitHubMCPSetupIfNeeded_DismissedPrompts(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	cfg := a.GetConfigManager().GetConfig()
	cfg.SkipPrompt = false
	if cfg.DismissedPrompts == nil {
		cfg.DismissedPrompts = make(map[string]bool)
	}
	cfg.DismissedPrompts["github_mcp_setup"] = true

	// ShouldPromptGitHubSetup should return false for dismissed prompt.
	// The function should return early.
	done := make(chan struct{})
	go func() {
		promptGitHubMCPSetupIfNeeded(a)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("promptGitHubMCPSetupIfNeeded did not return for dismissed prompt")
	}
}

func TestPromptGitHubMCPSetupIfNeeded_WithExitInput(t *testing.T) {
	// Provide stdin that would be read if we get past the ShouldPrompt check.
	// In practice, ShouldPromptGitHubSetup will return false in test env (no git repo
	// configured for GitHub), so the function returns early. This test verifies
	// no panic in any case.
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	cfg := a.GetConfigManager().GetConfig()
	cfg.SkipPrompt = false
	cfg.DismissedPrompts = make(map[string]bool)

	// Pipe input in case the function tries to read
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe error: %v", err)
	}

	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	go func() {
		w.WriteString("n\n")
		w.Close()
	}()

	done := make(chan struct{})
	go func() {
		promptGitHubMCPSetupIfNeeded(a)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("promptGitHubMCPSetupIfNeeded timed out")
	}
}
