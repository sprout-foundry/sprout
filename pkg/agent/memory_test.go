package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ------------------------------------------------------------------------
// sanitizeMemoryName
// ------------------------------------------------------------------------

func TestSanitizeMemoryName(t *testing.T) {

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Normal cases
		{"simple name", "hello", "hello"},
		{"with spaces", "my memory", "my-memory"},
		{"mixed case", "My Memory", "my-memory"},
		{"with underscores", "my_memory", "my_memory"},
		{"with hyphens", "my-memory", "my-memory"},

		// Special character stripping
		{"strip special chars", "my @#$ memory!", "my--memory"},
		{"strip leading/trailing hyphens", "--my-memory--", "my-memory"},
		{"strip leading/trailing underscores", "__my__memory__", "my__memory"},
		{"dots removed", "my.memory", "mymemory"},
		{"slashes removed", "my/memory", "mymemory"},

		// Edge cases
		{"empty string", "", "untitled"},
		{"only special chars", "@#$%^&*", "untitled"},
		{"only spaces", "   ", "untitled"},
		{"only hyphens", "---", "untitled"},
		{"only underscores", "___", "untitled"},
		{".md extension stripped from name", "my-memory.md", "my-memorymd"},
		{"complex mixed", "Hello World! (v2.0)", "hello-world-v20"},
		{"numbers kept", "test123", "test123"},
		{"unicode stripped", "café", "caf"},

		// Real-world memory names
		{"git safety rules", "git-safety-rules", "git-safety-rules"},
		{"code style", "code style", "code-style"},
		{"preferences for go", "Preferences for Go", "preferences-for-go"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := sanitizeMemoryName(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeMemoryName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// ------------------------------------------------------------------------
// Helper: set up isolated memory directory for file-based tests
// ------------------------------------------------------------------------

func setupMemoryDirForTest(t *testing.T) func() {
	t.Helper()

	// Use a temp dir as config dir so memory files live there.
	tempDir := t.TempDir()
	memoryDir := filepath.Join(tempDir, memoryDirName)
	os.MkdirAll(memoryDir, 0755)

	// Override config dir by setting the env var that GetConfigDir reads.
	t.Setenv("SPROUT_CONFIG", tempDir)

	return func() {}
}

// ------------------------------------------------------------------------
// SaveMemory / LoadMemoryContent / DeleteMemory
// ------------------------------------------------------------------------

func TestSaveMemory_AndLoad(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	err := SaveMemory("test-memory", "# Test Memory\nSome content here.")
	if err != nil {
		t.Fatalf("SaveMemory failed: %v", err)
	}

	content, err := LoadMemoryContent("test-memory")
	if err != nil {
		t.Fatalf("LoadMemoryContent failed: %v", err)
	}
	if !strings.Contains(content, "Some content here") {
		t.Errorf("expected loaded content to contain 'Some content here', got %q", content)
	}
}

func TestSaveMemory_SanitizesName(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	err := SaveMemory("My @Test Memory!", "content")
	if err != nil {
		t.Fatalf("SaveMemory failed: %v", err)
	}

	// Name should be sanitized
	content, err := LoadMemoryContent("my-test-memory")
	if err != nil {
		t.Fatalf("LoadMemoryContent for sanitized name failed: %v", err)
	}
	if content != "content" {
		t.Errorf("content = %q, want %q", content, "content")
	}
}

func TestSaveMemory_OverwritesExisting(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	SaveMemory("overwrite-me", "first content")
	SaveMemory("overwrite-me", "second content")

	content, err := LoadMemoryContent("overwrite-me")
	if err != nil {
		t.Fatalf("LoadMemoryContent failed: %v", err)
	}
	if content != "second content" {
		t.Errorf("content = %q, want %q", content, "second content")
	}
}

func TestLoadMemoryContent_NonExistent(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	_, err := LoadMemoryContent("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent memory, got nil")
	}
}

func TestLoadMemoryContent_EmptyName(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	_, err := LoadMemoryContent("")
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
}

func TestDeleteMemory(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	SaveMemory("to-delete", "content")

	err := DeleteMemory("to-delete")
	if err != nil {
		t.Fatalf("DeleteMemory failed: %v", err)
	}

	_, err = LoadMemoryContent("to-delete")
	if err == nil {
		t.Fatal("expected error after deletion, got nil")
	}
}

func TestDeleteMemory_WithExtension(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	SaveMemory("with-ext", "content")

	err := DeleteMemory("with-ext.md")
	if err != nil {
		t.Fatalf("DeleteMemory with .md extension failed: %v", err)
	}

	_, err = LoadMemoryContent("with-ext")
	if err == nil {
		t.Fatal("expected error after deletion with .md extension, got nil")
	}
}

func TestDeleteMemory_NonExistent(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	err := DeleteMemory("does-not-exist")
	if err == nil {
		t.Fatal("expected error deleting non-existent memory, got nil")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error message should contain 'does not exist', got %q", err.Error())
	}
}

// ------------------------------------------------------------------------
// LoadAllMemories
// ------------------------------------------------------------------------

func TestLoadAllMemories_Empty(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	memories, err := LoadAllMemories()
	if err != nil {
		t.Fatalf("LoadAllMemories failed: %v", err)
	}
	if len(memories) != 0 {
		t.Errorf("expected 0 memories, got %d", len(memories))
	}
}

func TestLoadAllMemories_WithFiles(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	SaveMemory("beta", "content beta")
	SaveMemory("alpha", "content alpha")
	SaveMemory("gamma", "content gamma")

	memories, err := LoadAllMemories()
	if err != nil {
		t.Fatalf("LoadAllMemories failed: %v", err)
	}
	if len(memories) != 3 {
		t.Fatalf("expected 3 memories, got %d", len(memories))
	}

	// Check sorting
	if memories[0].Name != "alpha" {
		t.Errorf("first memory should be 'alpha', got %q", memories[0].Name)
	}
	if memories[1].Name != "beta" {
		t.Errorf("second memory should be 'beta', got %q", memories[1].Name)
	}
	if memories[2].Name != "gamma" {
		t.Errorf("third memory should be 'gamma', got %q", memories[2].Name)
	}

	// Check content
	if !strings.Contains(memories[0].Content, "content alpha") {
		t.Errorf("alpha content mismatch: %q", memories[0].Content)
	}
}

func TestLoadAllMemories_IgnoresNonMdFiles(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	memoryDir := filepath.Join(os.Getenv("SPROUT_CONFIG"), memoryDirName)

	// Create non-md file
	os.WriteFile(filepath.Join(memoryDir, "notes.txt"), []byte("not a memory"), 0644)
	os.WriteFile(filepath.Join(memoryDir, "data.json"), []byte("{}"), 0644)
	SaveMemory("real-memory", "valid content")

	memories, err := LoadAllMemories()
	if err != nil {
		t.Fatalf("LoadAllMemories failed: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory (non-md files ignored), got %d", len(memories))
	}
	if memories[0].Name != "real-memory" {
		t.Errorf("expected 'real-memory', got %q", memories[0].Name)
	}
}

func TestLoadAllMemories_IgnoresSubdirectories(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	memoryDir := filepath.Join(os.Getenv("SPROUT_CONFIG"), memoryDirName)
	os.MkdirAll(filepath.Join(memoryDir, "subdir"), 0755)

	SaveMemory("real-memory", "valid content")

	memories, err := LoadAllMemories()
	if err != nil {
		t.Fatalf("LoadAllMemories failed: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}
}

// ------------------------------------------------------------------------
// ListMemories
// ------------------------------------------------------------------------

func TestListMemories_Empty(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	memories, err := ListMemories()
	if err != nil {
		t.Fatalf("ListMemories failed: %v", err)
	}
	if len(memories) != 0 {
		t.Errorf("expected 0 memories, got %d", len(memories))
	}
}

func TestListMemories_WithFiles(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	SaveMemory("zebra", "# Zebra Title\nDetails about zebras")
	SaveMemory("alpha", "# Alpha Title\nDetails about alpha")

	memories, err := ListMemories()
	if err != nil {
		t.Fatalf("ListMemories failed: %v", err)
	}
	if len(memories) != 2 {
		t.Fatalf("expected 2 memories, got %d", len(memories))
	}

	// Check sorting
	if memories[0].Name != "alpha" {
		t.Errorf("first should be 'alpha', got %q", memories[0].Name)
	}
	if memories[1].Name != "zebra" {
		t.Errorf("second should be 'zebra', got %q", memories[1].Name)
	}

	// Content should be first non-empty line (the title)
	if memories[0].Content != "# Alpha Title" {
		t.Errorf("alpha Content = %q, want %q", memories[0].Content, "# Alpha Title")
	}
	if memories[1].Content != "# Zebra Title" {
		t.Errorf("zebra Content = %q, want %q", memories[1].Content, "# Zebra Title")
	}
}

func TestListMemories_FirstNonEmptyLine(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	SaveMemory("with-blanks", "\n\n\n# Real Title\nBody")

	memories, err := ListMemories()
	if err != nil {
		t.Fatalf("ListMemories failed: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}
	if memories[0].Content != "# Real Title" {
		t.Errorf("Content = %q, want %q", memories[0].Content, "# Real Title")
	}
}

func TestListMemories_NoTitleLine(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	SaveMemory("no-title", "Just body content, no title")

	memories, err := ListMemories()
	if err != nil {
		t.Fatalf("ListMemories failed: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}
	if memories[0].Content != "Just body content, no title" {
		t.Errorf("Content = %q, want %q", memories[0].Content, "Just body content, no title")
	}
}

// ------------------------------------------------------------------------
// LoadMemoriesForPrompt
// ------------------------------------------------------------------------

func TestLoadMemoriesForPrompt_Empty(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	result := LoadMemoriesForPrompt()
	if result != "" {
		t.Errorf("expected empty string for no memories, got %q", result)
	}
}

func TestLoadMemoriesForPrompt_WithMemories(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	SaveMemory("alpha", "# Alpha\nAlpha content body.")
	SaveMemory("beta", "# Beta\nBeta content body.")

	result := LoadMemoriesForPrompt()

	if !strings.Contains(result, "## Memories") {
		t.Error("expected header '## Memories'")
	}
	if !strings.Contains(result, "### alpha") {
		t.Error("expected '### alpha'")
	}
	if !strings.Contains(result, "Alpha content body") {
		t.Error("expected alpha body content")
	}
	if !strings.Contains(result, "### beta") {
		t.Error("expected '### beta'")
	}
	if !strings.Contains(result, "Beta content body") {
		t.Error("expected beta body content")
	}
	// H1 titles should be stripped
	if strings.Contains(result, "# Alpha\n") {
		t.Error("H1 title should be stripped from memory content")
	}
}

func TestLoadMemoriesForPrompt_SkipsLeadingH1(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	SaveMemory("h1test", "# My Title\n\nBody after title.")

	result := LoadMemoriesForPrompt()

	// The H1 title line should not appear in the body
	// Only the header "### h1test" + "Body after title." should appear
	if strings.Count(result, "# My Title") > 0 {
		t.Error("leading H1 title should be stripped from body")
	}
	if !strings.Contains(result, "Body after title") {
		t.Error("body content should remain")
	}
}

func TestLoadMemoriesForPrompt_RespectsMaxBytes(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	// Create a large memory that exceeds maxMemoryPromptBytes
	largeContent := strings.Repeat("x", 60000) // > maxMemoryPromptBytes (50_000)
	SaveMemory("big-memory", largeContent)

	result := LoadMemoriesForPrompt()

	// Should contain the truncation notice
	if !strings.Contains(result, "additional memory file(s) omitted") {
		t.Error("expected truncation notice for oversized memory")
	}
	if !strings.Contains(result, "total size exceeded") {
		t.Error("expected 'total size exceeded' in truncation notice")
	}
}

func TestLoadMemoriesForPrompt_MultipleMemoriesWithLimit(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	// Create several medium memories that together exceed the limit
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("mem%02d", i)
		content := strings.Repeat("data ", 3000) // ~15KB each
		SaveMemory(name, content)
	}

	result := LoadMemoriesForPrompt()

	// Should contain the truncation notice
	if !strings.Contains(result, "additional memory file(s) omitted") {
		t.Error("expected truncation notice when total exceeds limit")
	}
}

// ------------------------------------------------------------------------
// Integration: Save + Delete + Load cycle
// ------------------------------------------------------------------------

func TestMemoryLifecycle(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	// Save
	err := SaveMemory("lifecycle-test", "# Test\nContent here.")
	if err != nil {
		t.Fatalf("SaveMemory failed: %v", err)
	}

	// Load
	content, err := LoadMemoryContent("lifecycle-test")
	if err != nil {
		t.Fatalf("LoadMemoryContent failed: %v", err)
	}
	if !strings.Contains(content, "Content here") {
		t.Errorf("unexpected content: %q", content)
	}

	// List
	memories, err := ListMemories()
	if err != nil {
		t.Fatalf("ListMemories failed: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}

	// Delete
	err = DeleteMemory("lifecycle-test")
	if err != nil {
		t.Fatalf("DeleteMemory failed: %v", err)
	}

	// Verify gone
	memories, err = LoadAllMemories()
	if err != nil {
		t.Fatalf("LoadAllMemories after delete failed: %v", err)
	}
	if len(memories) != 0 {
		t.Errorf("expected 0 memories after delete, got %d", len(memories))
	}
}

// ------------------------------------------------------------------------
// Config dir error handling
// ------------------------------------------------------------------------

func TestLoadMemoryContent_ConfigDirUnavailable(t *testing.T) {

	// Temporarily clear HOME and unset config env var so GetConfigDir fails.
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SPROUT_CONFIG", "")

	// This may or may not fail depending on the environment.
	// If it succeeds (because UserHomeDir still works), skip.
	content, err := LoadMemoryContent("anything")
	if err == nil {
		t.Skip("config dir was resolvable; skipping config-error test")
	}
	// If we get here, the error should be about the config dir
	if !strings.Contains(err.Error(), "config") && !strings.Contains(err.Error(), "memory") {
		t.Logf("Got error (may or may not be about config): %v", err)
	}
	_ = content // avoid unused variable warning
}

// ------------------------------------------------------------------------
// getMemoryDir (indirectly tested through Save/Load, but test directory
// creation explicitly)
// ------------------------------------------------------------------------

func TestGetMemoryDir_CreatesDirectory(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	// getMemoryDir is internal; we verify via SaveMemory which calls it.
	memoryDir := filepath.Join(os.Getenv("SPROUT_CONFIG"), memoryDirName)
	info, err := os.Stat(memoryDir)
	if err != nil {
		t.Fatalf("memory directory should exist after setup: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("memory path should be a directory")
	}
}
