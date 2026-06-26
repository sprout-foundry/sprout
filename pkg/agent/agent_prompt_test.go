package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePromptPath(t *testing.T) {
	t.Run("empty path returns error", func(t *testing.T) {
		_, err := resolvePromptPath("")
		if err == nil {
			t.Fatal("expected error for empty path")
		}
		if !strings.Contains(err.Error(), "empty") {
			t.Errorf("expected 'empty' in error, got: %v", err)
		}
	})

	t.Run("whitespace-only path returns error", func(t *testing.T) {
		_, err := resolvePromptPath("   ")
		if err == nil {
			t.Fatal("expected error for whitespace-only path")
		}
		if !strings.Contains(err.Error(), "empty") {
			t.Errorf("expected 'empty' in error, got: %v", err)
		}
	})

	t.Run("existing relative path in cwd is returned as-is", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "prompt.txt")
		if err := os.WriteFile(f, []byte("test prompt"), 0o644); err != nil {
			t.Fatal(err)
		}

		origCwd, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origCwd)

		got, err := resolvePromptPath("prompt.txt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "prompt.txt" {
			t.Errorf("got %q, want %q", got, "prompt.txt")
		}
	})

	t.Run("absolute path is returned even if not found", func(t *testing.T) {
		got, err := resolvePromptPath("/nonexistent/path/prompt.txt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "/nonexistent/path/prompt.txt" {
			t.Errorf("got %q, want %q", got, "/nonexistent/path/prompt.txt")
		}
	})

	t.Run("relative path not found returns original trimmed path", func(t *testing.T) {
		origCwd, _ := os.Getwd()
		os.Chdir(t.TempDir())
		defer os.Chdir(origCwd)

		got, err := resolvePromptPath("no_such_file.txt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "no_such_file.txt" {
			t.Errorf("got %q, want %q", got, "no_such_file.txt")
		}
	})

	t.Run("path with leading/trailing whitespace is trimmed", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "prompt.txt")
		if err := os.WriteFile(f, []byte("test prompt"), 0o644); err != nil {
			t.Fatal(err)
		}

		origCwd, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origCwd)

		got, err := resolvePromptPath("  prompt.txt  ")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "prompt.txt" {
			t.Errorf("got %q, want %q", got, "prompt.txt")
		}
	})
}

func TestResolvePromptPathRepoRelative(t *testing.T) {
	// Test repo-relative path resolution using go.mod search
	// Since we're running from the repo root (or a subdirectory),
	// a path like "prompts/something.txt" should be resolved relative to repo root

	origCwd, _ := os.Getwd()
	defer os.Chdir(origCwd)

	// From within the repo, a path under a known directory should resolve
	// If we are in a nested dir and there's a go.mod above, it should use that
	// Since we're in the repo, findRepoRootFromCWD should succeed
	repoRoot, err := findRepoRootFromCWD()
	if err != nil {
		t.Skipf("not in a Go module directory: %v", err)
	}

	// Create a prompt file under the repo root
	promptPath := filepath.Join(repoRoot, "test_prompt_file.txt")
	if err := os.WriteFile(promptPath, []byte("test prompt content"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(promptPath) })

	// Change to a subdirectory and verify repo-relative resolution
	subDir := filepath.Join(repoRoot, "sub", "dir")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Join(repoRoot, "sub")) })
	os.Chdir(subDir)

	got, err := resolvePromptPath("test_prompt_file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != promptPath {
		t.Errorf("got %q, want %q", got, promptPath)
	}
}

func TestFindRepoRootFromCWD(t *testing.T) {
	origCwd, _ := os.Getwd()
	defer os.Chdir(origCwd)

	t.Run("finds repo root from within repo", func(t *testing.T) {
		repoRoot, err := findRepoRootFromCWD()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err != nil {
			t.Errorf("repo root %q should contain go.mod", repoRoot)
		}
	})

	t.Run("finds repo root from nested directory", func(t *testing.T) {
		repoRoot, err := findRepoRootFromCWD()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		nestedDir := filepath.Join(repoRoot, "pkg", "agent")
		os.Chdir(nestedDir)

		repoRoot2, err := findRepoRootFromCWD()
		if err != nil {
			t.Fatalf("unexpected error from nested dir: %v", err)
		}
		if repoRoot2 != repoRoot {
			t.Errorf("got %q, want %q", repoRoot2, repoRoot)
		}
	})

	t.Run("returns error when no go.mod found", func(t *testing.T) {
		dir := t.TempDir()
		os.Chdir(dir)

		_, err := findRepoRootFromCWD()
		if err == nil {
			t.Fatal("expected error when no go.mod found")
		}
		if !strings.Contains(err.Error(), "go.mod not found") {
			t.Errorf("expected 'go.mod not found' in error, got: %v", err)
		}
	})
}

func TestSetSystemPromptFromFile(t *testing.T) {
	t.Run("reads and sets prompt from valid file", func(t *testing.T) {
		a := newTestAgent(t)
		defer a.Shutdown()

		dir := t.TempDir()
		promptFile := filepath.Join(dir, "prompt.txt")
		content := "You are a custom assistant."
		if err := os.WriteFile(promptFile, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		err := a.SetSystemPromptFromFile(promptFile)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if a.systemPrompt != content {
			t.Errorf("got %q, want %q", a.systemPrompt, content)
		}
	})

	t.Run("rejects empty file", func(t *testing.T) {
		a := newTestAgent(t)
		defer a.Shutdown()

		dir := t.TempDir()
		promptFile := filepath.Join(dir, "empty.txt")
		if err := os.WriteFile(promptFile, []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}

		err := a.SetSystemPromptFromFile(promptFile)
		if err == nil {
			t.Fatal("expected error for empty file")
		}
		if !strings.Contains(err.Error(), "empty") {
			t.Errorf("expected 'empty' in error, got: %v", err)
		}
	})

	t.Run("rejects whitespace-only file", func(t *testing.T) {
		a := newTestAgent(t)
		defer a.Shutdown()

		dir := t.TempDir()
		promptFile := filepath.Join(dir, "whitespace.txt")
		if err := os.WriteFile(promptFile, []byte("   \n  \n  "), 0o644); err != nil {
			t.Fatal(err)
		}

		err := a.SetSystemPromptFromFile(promptFile)
		if err == nil {
			t.Fatal("expected error for whitespace-only file")
		}
	})

	t.Run("returns error for non-existent file", func(t *testing.T) {
		a := newTestAgent(t)
		defer a.Shutdown()

		err := a.SetSystemPromptFromFile("/tmp/__nonexistent_prompt_file.txt")
		if err == nil {
			t.Fatal("expected error for non-existent file")
		}
	})

	t.Run("trims whitespace from file content", func(t *testing.T) {
		a := newTestAgent(t)
		defer a.Shutdown()

		dir := t.TempDir()
		promptFile := filepath.Join(dir, "prompt.txt")
		content := "  Trimmed prompt  "
		if err := os.WriteFile(promptFile, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		err := a.SetSystemPromptFromFile(promptFile)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if a.systemPrompt != "Trimmed prompt" {
			t.Errorf("got %q, want %q", a.systemPrompt, "Trimmed prompt")
		}
	})
}

func TestEnsureStopInformation(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	prompt := "Your custom prompt text"
	result := a.ensureStopInformation(prompt)
	if result != prompt {
		t.Errorf("ensureStopInformation(%q) = %q, want %q", prompt, result, prompt)
	}

	// Empty string
	result = a.ensureStopInformation("")
	if result != "" {
		t.Errorf("ensureStopInformation(\"\") = %q, want empty", result)
	}
}

func TestResolvePromptPathReturnsAgentError(t *testing.T) {
	_, err := resolvePromptPath("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	// Just verify error is returned with correct message
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected 'empty' in error, got: %v", err)
	}
}
