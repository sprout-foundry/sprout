package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupMemoryHandlers creates an isolated memory directory for testing
// by setting SPROUT_CONFIG and SPROUT_CONFIG to a temp dir.
func setupMemoryHandlers(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()

	// Set config dir to our temp dir
	t.Setenv("SPROUT_CONFIG", tmp)

	// Create the memories subdirectory
	memDir := filepath.Join(tmp, "memories")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatalf("failed to create temp memory dir: %v", err)
	}
	return memDir
}

// --- handleAddMemory ---

func TestHandleAddMemoryMissingArgs(t *testing.T) {
	t.Parallel()

	// Missing "name"
	_, err := handleAddMemory(nil, nil, map[string]interface{}{
		"content": "some content",
	})
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("wrong error: %v", err)
	}

	// Missing "content"
	_, err = handleAddMemory(nil, nil, map[string]interface{}{
		"name": "test-memory",
	})
	if err == nil {
		t.Fatal("expected error for missing content")
	}
	if !strings.Contains(err.Error(), "content is required") {
		t.Errorf("wrong error: %v", err)
	}

	// Neither arg
	_, err = handleAddMemory(nil, nil, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for missing args")
	}
}

func TestHandleAddMemorySuccess(t *testing.T) {
	// Not parallel — uses env vars
	memDir := setupMemoryHandlers(t)

	result, err := handleAddMemory(nil, nil, map[string]interface{}{
		"name":    "test-mem",
		"content": "# Test Memory\n\nSome content here.",
	})
	if err != nil {
		t.Fatalf("handleAddMemory failed: %v", err)
	}

	// Verify file was written
	memFile := filepath.Join(memDir, "test-mem.md")
	data, err := os.ReadFile(memFile)
	if err != nil {
		t.Fatalf("memory file not found at %s: %v", memFile, err)
	}
	if string(data) != "# Test Memory\n\nSome content here." {
		t.Errorf("file content = %q; want original content", string(data))
	}

	// Verify result message mentions the memory name
	if !strings.Contains(result, "test-mem") {
		t.Errorf("result should mention memory name: %s", result)
	}
}

func TestHandleAddMemorySanitized(t *testing.T) {
	// Not parallel — uses env vars
	memDir := setupMemoryHandlers(t)

	result, err := handleAddMemory(nil, nil, map[string]interface{}{
		"name":    "My Memory! @#$%&*()",
		"content": "content",
	})
	if err != nil {
		t.Fatalf("handleAddMemory failed: %v", err)
	}

	// Name should be sanitized to "my-memory"
	memFile := filepath.Join(memDir, "my-memory.md")
	if _, err := os.Stat(memFile); os.IsNotExist(err) {
		t.Errorf("sanitized memory file not found at %s", memFile)
	}

	if !strings.Contains(result, "my-memory") {
		t.Errorf("result should mention sanitized name 'my-memory': %s", result)
	}
}

// --- handleReadMemory ---

func TestHandleReadMemoryMissingArgs(t *testing.T) {
	t.Parallel()

	_, err := handleReadMemory(nil, nil, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("wrong error: %v", err)
	}
}

func TestHandleReadMemoryNotFound(t *testing.T) {
	// Not parallel — uses env vars
	setupMemoryHandlers(t)

	_, err := handleReadMemory(nil, nil, map[string]interface{}{
		"name": "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent memory")
	}
}

func TestHandleReadMemorySuccess(t *testing.T) {
	// Not parallel — uses env vars
	memDir := setupMemoryHandlers(t)

	// Write a memory file first
	content := "# ReadMe\n\nThis is test content."
	memFile := filepath.Join(memDir, "readme.md")
	if err := os.WriteFile(memFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test memory: %v", err)
	}

	result, err := handleReadMemory(nil, nil, map[string]interface{}{
		"name": "readme",
	})
	if err != nil {
		t.Fatalf("handleReadMemory failed: %v", err)
	}

	if !strings.Contains(result, "## Memory: readme") {
		t.Error("result should contain '## Memory: readme'")
	}
	if !strings.Contains(result, "# ReadMe") {
		t.Error("result should contain memory content")
	}
}

// --- handleListMemories ---

func TestHandleListMemoriesEmpty(t *testing.T) {
	// Not parallel — uses env vars
	setupMemoryHandlers(t)

	result, err := handleListMemories(nil, nil, map[string]interface{}{})
	if err != nil {
		t.Fatalf("handleListMemories failed: %v", err)
	}

	if !strings.Contains(result, "No memories saved") {
		t.Errorf("should say 'No memories saved': %s", result)
	}
}

func TestHandleListMemoriesSuccess(t *testing.T) {
	// Not parallel — uses env vars
	memDir := setupMemoryHandlers(t)

	// Write two memory files
	os.WriteFile(filepath.Join(memDir, "alpha.md"), []byte("# Alpha Memory\nDetails..."), 0644)
	os.WriteFile(filepath.Join(memDir, "beta.md"), []byte("# Beta Memory\nOther details..."), 0644)

	result, err := handleListMemories(nil, nil, map[string]interface{}{})
	if err != nil {
		t.Fatalf("handleListMemories failed: %v", err)
	}

	if !strings.Contains(result, "alpha") {
		t.Error("should list alpha memory")
	}
	if !strings.Contains(result, "beta") {
		t.Error("should list beta memory")
	}
	if !strings.Contains(result, "2") {
		t.Error("should show count of 2")
	}
}

func TestHandleListMemoriesTruncation(t *testing.T) {
	// Not parallel — uses env vars
	memDir := setupMemoryHandlers(t)

	// Write a memory with a very long title line
	longTitle := "# " + strings.Repeat("A", 200)
	os.WriteFile(filepath.Join(memDir, "long.md"), []byte(longTitle+"\nBody"), 0644)

	result, err := handleListMemories(nil, nil, map[string]interface{}{})
	if err != nil {
		t.Fatalf("handleListMemories failed: %v", err)
	}

	// Should be truncated (title > 120 chars gets "...")
	if !strings.Contains(result, "...") {
		t.Error("long title should be truncated with ...")
	}
}

// --- handleDeleteMemory ---

func TestHandleDeleteMemoryMissingArgs(t *testing.T) {
	t.Parallel()

	_, err := handleDeleteMemory(nil, nil, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("wrong error: %v", err)
	}
}

func TestHandleDeleteMemoryNotFound(t *testing.T) {
	// Not parallel — uses env vars
	setupMemoryHandlers(t)

	_, err := handleDeleteMemory(nil, nil, map[string]interface{}{
		"name": "no-such-memory",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent memory")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("wrong error: %v", err)
	}
}

func TestHandleDeleteMemorySuccess(t *testing.T) {
	// Not parallel — uses env vars
	memDir := setupMemoryHandlers(t)

	// Write a memory file
	memFile := filepath.Join(memDir, "bye.md")
	if err := os.WriteFile(memFile, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to write test memory: %v", err)
	}

	result, err := handleDeleteMemory(nil, nil, map[string]interface{}{
		"name": "bye",
	})
	if err != nil {
		t.Fatalf("handleDeleteMemory failed: %v", err)
	}

	// Verify file was deleted
	if _, err := os.Stat(memFile); !os.IsNotExist(err) {
		t.Error("memory file should be deleted")
	}

	if !strings.Contains(result, "Memory 'bye' deleted") {
		t.Errorf("result should confirm deletion: %s", result)
	}
}

func TestHandleDeleteMemoryWithSuffix(t *testing.T) {
	// Not parallel — uses env vars
	memDir := setupMemoryHandlers(t)

	memFile := filepath.Join(memDir, "del-me.md")
	if err := os.WriteFile(memFile, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to write test memory: %v", err)
	}

	result, err := handleDeleteMemory(nil, nil, map[string]interface{}{
		"name": "del-me.md", // .md suffix should be stripped
	})
	if err != nil {
		t.Fatalf("handleDeleteMemory with .md suffix failed: %v", err)
	}

	if _, err := os.Stat(memFile); !os.IsNotExist(err) {
		t.Error("memory file should be deleted even when name has .md suffix")
	}

	if !strings.Contains(result, "del-me") {
		t.Errorf("result should mention name without suffix: %s", result)
	}
}

func TestGetStringArg(t *testing.T) {
	t.Run("valid string", func(t *testing.T) {
		args := map[string]interface{}{
			"name":    "test-memory",
			"content": "some content",
		}
		val, err := getStringArg(args, "name")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != "test-memory" {
			t.Errorf("expected 'test-memory', got %q", val)
		}
	})

	t.Run("missing key", func(t *testing.T) {
		args := map[string]interface{}{
			"content": "some content",
		}
		_, err := getStringArg(args, "name")
		if err == nil {
			t.Fatal("expected error for missing key")
		}
		if !strings.Contains(err.Error(), "missing required argument") {
			t.Errorf("expected 'missing required argument', got %q", err.Error())
		}
	})

	t.Run("wrong type", func(t *testing.T) {
		args := map[string]interface{}{
			"name": 42,
		}
		_, err := getStringArg(args, "name")
		if err == nil {
			t.Fatal("expected error for wrong type")
		}
		if !strings.Contains(err.Error(), "must be a string") {
			t.Errorf("expected 'must be a string', got %q", err.Error())
		}
	})

	t.Run("nil map", func(t *testing.T) {
		_, err := getStringArg(nil, "name")
		if err == nil {
			t.Fatal("expected error for nil map")
		}
	})
}

func TestHandleAddMemory_MissingArgs(t *testing.T) {
	ctx := context.Background()
	a := &Agent{}
	a.initSubManagers()

	// Missing name
	_, err := handleAddMemory(ctx, a, map[string]interface{}{
		"content": "some content",
	})
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("expected 'name is required', got %q", err.Error())
	}

	// Missing content
	_, err = handleAddMemory(ctx, a, map[string]interface{}{
		"name": "test-memory",
	})
	if err == nil {
		t.Fatal("expected error for missing content")
	}
	if !strings.Contains(err.Error(), "content is required") {
		t.Errorf("expected 'content is required', got %q", err.Error())
	}
}

func TestHandleReadMemory_MissingArgs(t *testing.T) {
	ctx := context.Background()
	a := &Agent{}
	a.initSubManagers()

	_, err := handleReadMemory(ctx, a, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("expected 'name is required', got %q", err.Error())
	}
}

func TestHandleReadMemory_NonExistent(t *testing.T) {
	ctx := context.Background()
	a := &Agent{}
	a.initSubManagers()

	_, err := handleReadMemory(ctx, a, map[string]interface{}{
		"name": "nonexistent-memory-12345",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent memory")
	}
}

func TestHandleListMemories(t *testing.T) {
	ctx := context.Background()
	a := &Agent{}
	a.initSubManagers()

	result, err := handleListMemories(ctx, a, map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return either a list or the "no memories" message
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	// Either it says "No memories saved" or "Saved Memories"
	hasNoMemoriesMsg := strings.Contains(result, "No memories saved")
	hasSavedMemoriesMsg := strings.Contains(result, "Saved Memories")
	if !hasNoMemoriesMsg && !hasSavedMemoriesMsg {
		t.Errorf("expected 'No memories saved' or 'Saved Memories', got: %s", result)
	}
}

func TestHandleDeleteMemory_MissingArgs(t *testing.T) {
	ctx := context.Background()
	a := &Agent{}
	a.initSubManagers()

	_, err := handleDeleteMemory(ctx, a, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("expected 'name is required', got %q", err.Error())
	}
}

func TestHandleDeleteMemory_NonExistent(t *testing.T) {
	ctx := context.Background()
	a := &Agent{}
	a.initSubManagers()

	_, err := handleDeleteMemory(ctx, a, map[string]interface{}{
		"name": "nonexistent-memory-12345",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent memory")
	}
}

func TestHandleDeleteMemory_WithMdExtension(t *testing.T) {
	ctx := context.Background()
	a := &Agent{}
	a.initSubManagers()

	// Should strip .md extension before trying to delete
	_, err := handleDeleteMemory(ctx, a, map[string]interface{}{
		"name": "nonexistent-memory.md",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent memory (after stripping .md)")
	}
}

func TestSanitizeMemoryNameV2(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"my-memory", "my-memory"},
		{"My Memory", "my-memory"},
		{"  spaces  ", "spaces"},
		{"special!@#$%chars", "specialchars"},
		{"UPPERCASE", "uppercase"},
		{"has_underscore", "has_underscore"},
		{"has-hyphen", "has-hyphen"},
		{"mixed_My-Memory_Name", "mixed_my-memory_name"},
		{".md", "md"},
		{"...---___", "untitled"},
		{"", "untitled"},
		{"---leading-trailing---", "leading-trailing"},
		{"_leading_trailing_", "leading_trailing"},
		{"test.md", "testmd"},
		{"a b c", "a-b-c"},
	}

	for _, tt := range tests {
		result := sanitizeMemoryName(tt.input)
		if result != tt.expected {
			t.Errorf("sanitizeMemoryName(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
