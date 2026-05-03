package agent

import (
	"context"
	"strings"
	"testing"
)

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
