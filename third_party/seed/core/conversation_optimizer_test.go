package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// --- ConversationOptimizer Unit Tests ---

func TestHashContent(t *testing.T) {
	// Same content produces same hash.
	h1 := hashContent("file contents here")
	h2 := hashContent("file contents here")
	if h1 != h2 {
		t.Errorf("same content should produce same hash: %q vs %q", h1, h2)
	}

	// Different content produces different hash.
	h3 := hashContent("different content")
	if h1 == h3 {
		t.Errorf("different content should produce different hash: %q", h1)
	}

	// Hash length is 16 hex chars (8 bytes).
	if len(h1) != 16 {
		t.Errorf("hash length should be 16, got %d", len(h1))
	}

	// Empty content produces a valid hash.
	emptyHash := hashContent("")
	if len(emptyHash) != 16 {
		t.Errorf("empty content hash length should be 16, got %d", len(emptyHash))
	}

	// Large content produces a valid hash (memory efficiency test).
	large := strings.Repeat("x", 100000)
	largeHash := hashContent(large)
	if len(largeHash) != 16 {
		t.Errorf("large content hash length should be 16, got %d", len(largeHash))
	}
}

func TestConversationOptimizer_Disabled(t *testing.T) {
	opt := NewConversationOptimizer(ConversationOptimizerOptions{
		Enabled:     false,
		KnownToolFn: func(name string) ToolCategory { return ToolCategoryFileRead },
	})

	messages := []Message{
		{Role: "user", Content: "read this"},
		{Role: "assistant", Content: "reading", ToolCalls: []ToolCall{
			{ID: "c1", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
		}},
		{Role: "tool", Content: "same content", ToolCallID: "c1"},
	}

	result := opt.OptimizeConversation(messages)
	// Should be unchanged
	if result[2].Content != "same content" {
		t.Errorf("disabled optimizer should not modify content, got %q", result[2].Content)
	}
}

func TestConversationOptimizer_NilKnownToolFn(t *testing.T) {
	opt := NewConversationOptimizer(ConversationOptimizerOptions{
		Enabled: true,
		// KnownToolFn is nil
	})

	messages := []Message{
		{Role: "user", Content: "read this"},
		{Role: "assistant", Content: "reading", ToolCalls: []ToolCall{
			{ID: "c1", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
		}},
		{Role: "tool", Content: "same content", ToolCallID: "c1"},
	}

	result := opt.OptimizeConversation(messages)
	// Should be unchanged (nil callback = skip all)
	if result[2].Content != "same content" {
		t.Errorf("nil KnownToolFn should not modify content, got %q", result[2].Content)
	}
}

func TestConversationOptimizer_FileReadDedup_SameContent(t *testing.T) {
	opt := NewConversationOptimizer(ConversationOptimizerOptions{
		Enabled: true,
		KnownToolFn: func(name string) ToolCategory {
			if name == "read_file" {
				return ToolCategoryFileRead
			}
			return ToolCategoryUnknown
		},
	})

	// Two reads of the same file with identical content.
	messages := []Message{
		{Role: "user", Content: "read a.txt twice"},
		{Role: "assistant", Content: "reading first time", ToolCalls: []ToolCall{
			{ID: "c1", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
		}},
		{Role: "tool", Content: "file contents here", ToolCallID: "c1"},
		{Role: "assistant", Content: "reading again", ToolCalls: []ToolCall{
			{ID: "c2", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
		}},
		{Role: "tool", Content: "file contents here", ToolCallID: "c2"},
		{Role: "assistant", Content: "done"},
	}

	result := opt.OptimizeConversation(messages)

	// First read should be replaced with placeholder
	if !strings.Contains(result[2].Content, "[Earlier file read:") {
		t.Errorf("first read should be replaced with placeholder, got %q", result[2].Content)
	}
	if !strings.Contains(result[2].Content, "a.txt") {
		t.Errorf("placeholder should contain filepath, got %q", result[2].Content)
	}

	// Second (latest) read should be kept
	if result[4].Content != "file contents here" {
		t.Errorf("latest read should be kept, got %q", result[4].Content)
	}
}

func TestConversationOptimizer_FileReadDedup_DifferentContent(t *testing.T) {
	opt := NewConversationOptimizer(ConversationOptimizerOptions{
		Enabled: true,
		KnownToolFn: func(name string) ToolCategory {
			if name == "read_file" {
				return ToolCategoryFileRead
			}
			return ToolCategoryUnknown
		},
	})

	// Two reads of the same file with different content (e.g., file was modified).
	messages := []Message{
		{Role: "user", Content: "read a.txt"},
		{Role: "assistant", Content: "reading v1", ToolCalls: []ToolCall{
			{ID: "c1", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
		}},
		{Role: "tool", Content: "original content", ToolCallID: "c1"},
		{Role: "assistant", Content: "reading v2", ToolCalls: []ToolCall{
			{ID: "c2", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
		}},
		{Role: "tool", Content: "modified content", ToolCallID: "c2"},
	}

	result := opt.OptimizeConversation(messages)

	// Different content → no dedup; both reads should be preserved
	if result[2].Content != "original content" {
		t.Errorf("first read should be kept (different content), got %q", result[2].Content)
	}
	if result[4].Content != "modified content" {
		t.Errorf("second read should be kept, got %q", result[4].Content)
	}
}

func TestConversationOptimizer_FileReadDedup_DifferentFiles(t *testing.T) {
	opt := NewConversationOptimizer(ConversationOptimizerOptions{
		Enabled: true,
		KnownToolFn: func(name string) ToolCategory {
			if name == "read_file" {
				return ToolCategoryFileRead
			}
			return ToolCategoryUnknown
		},
	})

	// Reads of different files with same content.
	messages := []Message{
		{Role: "user", Content: "read files"},
		{Role: "assistant", Content: "reading a", ToolCalls: []ToolCall{
			{ID: "c1", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
		}},
		{Role: "tool", Content: "same content", ToolCallID: "c1"},
		{Role: "assistant", Content: "reading b", ToolCalls: []ToolCall{
			{ID: "c2", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"b.txt"}`}},
		}},
		{Role: "tool", Content: "same content", ToolCallID: "c2"},
	}

	result := opt.OptimizeConversation(messages)

	// Different files → no dedup (even if content is same)
	if result[2].Content != "same content" {
		t.Errorf("read of a.txt should be kept, got %q", result[2].Content)
	}
	if result[4].Content != "same content" {
		t.Errorf("read of b.txt should be kept, got %q", result[4].Content)
	}
}

func TestConversationOptimizer_FileReadDedup_ThreeReads(t *testing.T) {
	opt := NewConversationOptimizer(ConversationOptimizerOptions{
		Enabled: true,
		KnownToolFn: func(name string) ToolCategory {
			if name == "read_file" {
				return ToolCategoryFileRead
			}
			return ToolCategoryUnknown
		},
	})

	// Three reads: first and third have same content, second is different.
	messages := []Message{
		{Role: "user", Content: "read"},
		{Role: "assistant", Content: "read1", ToolCalls: []ToolCall{
			{ID: "c1", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"f.txt"}`}},
		}},
		{Role: "tool", Content: "content A", ToolCallID: "c1"},
		{Role: "assistant", Content: "read2", ToolCalls: []ToolCall{
			{ID: "c2", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"f.txt"}`}},
		}},
		{Role: "tool", Content: "content B", ToolCallID: "c2"},
		{Role: "assistant", Content: "read3", ToolCalls: []ToolCall{
			{ID: "c3", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"f.txt"}`}},
		}},
		{Role: "tool", Content: "content A", ToolCallID: "c3"},
	}

	result := opt.OptimizeConversation(messages)

	// First read (content A) → replaced (later read has same content)
	if !strings.Contains(result[2].Content, "[Earlier file read:") {
		t.Errorf("first read should be replaced, got %q", result[2].Content)
	}
	// Second read (content B) → kept (unique content)
	if result[4].Content != "content B" {
		t.Errorf("second read should be kept, got %q", result[4].Content)
	}
	// Third read (content A) → kept (latest)
	if result[6].Content != "content A" {
		t.Errorf("third read should be kept, got %q", result[6].Content)
	}
}

func TestConversationOptimizer_ShellCommandDedup(t *testing.T) {
	opt := NewConversationOptimizer(ConversationOptimizerOptions{
		Enabled: true,
		KnownToolFn: func(name string) ToolCategory {
			if name == "shell" {
				return ToolCategoryShellCommand
			}
			return ToolCategoryUnknown
		},
	})

	// Two identical transient commands with identical output.
	messages := []Message{
		{Role: "user", Content: "list dir"},
		{Role: "assistant", Content: "running ls", ToolCalls: []ToolCall{
			{ID: "c1", Function: ToolCallFunction{Name: "shell", Arguments: `{"cmd":"ls -la"}`}},
		}},
		{Role: "tool", Content: "file1\nfile2\n", ToolCallID: "c1"},
		{Role: "assistant", Content: "running ls again", ToolCalls: []ToolCall{
			{ID: "c2", Function: ToolCallFunction{Name: "shell", Arguments: `{"cmd":"ls -la"}`}},
		}},
		{Role: "tool", Content: "file1\nfile2\n", ToolCallID: "c2"},
	}

	result := opt.OptimizeConversation(messages)

	// First ls output → replaced (same command, same content)
	if !strings.Contains(result[2].Content, "[Earlier command output:") {
		t.Errorf("first command should be replaced, got %q", result[2].Content)
	}
	if !strings.Contains(result[2].Content, "ls -la") {
		t.Errorf("placeholder should contain command, got %q", result[2].Content)
	}

	// Second ls output → kept (latest)
	if result[4].Content != "file1\nfile2\n" {
		t.Errorf("latest command output should be kept, got %q", result[4].Content)
	}
}

func TestConversationOptimizer_ShellCommandDedup_DifferentOutput(t *testing.T) {
	opt := NewConversationOptimizer(ConversationOptimizerOptions{
		Enabled: true,
		KnownToolFn: func(name string) ToolCategory {
			if name == "shell" {
				return ToolCategoryShellCommand
			}
			return ToolCategoryUnknown
		},
	})

	// Same transient command but different output — should NOT dedup.
	messages := []Message{
		{Role: "user", Content: "list dir"},
		{Role: "assistant", Content: "running ls", ToolCalls: []ToolCall{
			{ID: "c1", Function: ToolCallFunction{Name: "shell", Arguments: `{"cmd":"ls"}`}},
		}},
		{Role: "tool", Content: "file1\n", ToolCallID: "c1"},
		{Role: "assistant", Content: "running ls again", ToolCalls: []ToolCall{
			{ID: "c2", Function: ToolCallFunction{Name: "shell", Arguments: `{"cmd":"ls"}`}},
		}},
		{Role: "tool", Content: "file1\nfile2\n", ToolCallID: "c2"},
	}

	result := opt.OptimizeConversation(messages)

	// Different output → no dedup; both should be preserved
	if result[2].Content != "file1\n" {
		t.Errorf("first ls should be kept (different output), got %q", result[2].Content)
	}
	if result[4].Content != "file1\nfile2\n" {
		t.Errorf("second ls should be kept, got %q", result[4].Content)
	}
}

func TestConversationOptimizer_ShellCommand_NonTransient(t *testing.T) {
	opt := NewConversationOptimizer(ConversationOptimizerOptions{
		Enabled: true,
		KnownToolFn: func(name string) ToolCategory {
			if name == "shell" {
				return ToolCategoryShellCommand
			}
			return ToolCategoryUnknown
		},
	})

	// Non-transient commands (e.g., git commit) should NOT be deduplicated.
	messages := []Message{
		{Role: "user", Content: "commit"},
		{Role: "assistant", Content: "committing", ToolCalls: []ToolCall{
			{ID: "c1", Function: ToolCallFunction{Name: "shell", Arguments: `{"cmd":"git commit -m 'fix'"}`}},
		}},
		{Role: "tool", Content: "committed", ToolCallID: "c1"},
		{Role: "assistant", Content: "committing again", ToolCalls: []ToolCall{
			{ID: "c2", Function: ToolCallFunction{Name: "shell", Arguments: `{"cmd":"git commit -m 'fix'"}`}},
		}},
		{Role: "tool", Content: "committed again", ToolCallID: "c2"},
	}

	result := opt.OptimizeConversation(messages)

	// Both outputs should be preserved
	if result[2].Content != "committed" {
		t.Errorf("first git commit should be kept, got %q", result[2].Content)
	}
	if result[4].Content != "committed again" {
		t.Errorf("second git commit should be kept, got %q", result[4].Content)
	}
}

func TestConversationOptimizer_ShellCommand_DifferentCommands(t *testing.T) {
	opt := NewConversationOptimizer(ConversationOptimizerOptions{
		Enabled: true,
		KnownToolFn: func(name string) ToolCategory {
			if name == "shell" {
				return ToolCategoryShellCommand
			}
			return ToolCategoryUnknown
		},
	})

	// Different transient commands should not be deduplicated.
	messages := []Message{
		{Role: "user", Content: "run commands"},
		{Role: "assistant", Content: "ls", ToolCalls: []ToolCall{
			{ID: "c1", Function: ToolCallFunction{Name: "shell", Arguments: `{"cmd":"ls"}`}},
		}},
		{Role: "tool", Content: "file1", ToolCallID: "c1"},
		{Role: "assistant", Content: "pwd", ToolCalls: []ToolCall{
			{ID: "c2", Function: ToolCallFunction{Name: "shell", Arguments: `{"cmd":"pwd"}`}},
		}},
		{Role: "tool", Content: "/home", ToolCallID: "c2"},
	}

	result := opt.OptimizeConversation(messages)

	// Both should be preserved (different commands)
	if result[2].Content != "file1" {
		t.Errorf("ls output should be kept, got %q", result[2].Content)
	}
	if result[4].Content != "/home" {
		t.Errorf("pwd output should be kept, got %q", result[4].Content)
	}
}

func TestConversationOptimizer_UnknownToolSkipped(t *testing.T) {
	opt := NewConversationOptimizer(ConversationOptimizerOptions{
		Enabled: true,
		KnownToolFn: func(name string) ToolCategory {
			// Unknown tool → skip
			return ToolCategoryUnknown
		},
	})

	messages := []Message{
		{Role: "user", Content: "do something"},
		{Role: "assistant", Content: "doing", ToolCalls: []ToolCall{
			{ID: "c1", Function: ToolCallFunction{Name: "unknown_tool", Arguments: `{}`}},
		}},
		{Role: "tool", Content: "result", ToolCallID: "c1"},
	}

	result := opt.OptimizeConversation(messages)
	if result[2].Content != "result" {
		t.Errorf("unknown tool should not be modified, got %q", result[2].Content)
	}
}

func TestConversationOptimizer_MixedTools(t *testing.T) {
	opt := NewConversationOptimizer(ConversationOptimizerOptions{
		Enabled: true,
		KnownToolFn: func(name string) ToolCategory {
			switch name {
			case "read_file":
				return ToolCategoryFileRead
			case "shell":
				return ToolCategoryShellCommand
			default:
				return ToolCategoryUnknown
			}
		},
	})

	// Mixed: file read (dedup), shell (dedup), unknown (skip).
	messages := []Message{
		{Role: "user", Content: "do stuff"},
		{Role: "assistant", Content: "read", ToolCalls: []ToolCall{
			{ID: "c1", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
		}},
		{Role: "tool", Content: "content", ToolCallID: "c1"},
		{Role: "assistant", Content: "ls", ToolCalls: []ToolCall{
			{ID: "c2", Function: ToolCallFunction{Name: "shell", Arguments: `{"cmd":"ls"}`}},
		}},
		{Role: "tool", Content: "file1", ToolCallID: "c2"},
		{Role: "assistant", Content: "unknown", ToolCalls: []ToolCall{
			{ID: "c3", Function: ToolCallFunction{Name: "write_file", Arguments: `{}`}},
		}},
		{Role: "tool", Content: "wrote", ToolCallID: "c3"},
		{Role: "assistant", Content: "read again", ToolCalls: []ToolCall{
			{ID: "c4", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
		}},
		{Role: "tool", Content: "content", ToolCallID: "c4"},
		{Role: "assistant", Content: "ls again", ToolCalls: []ToolCall{
			{ID: "c5", Function: ToolCallFunction{Name: "shell", Arguments: `{"cmd":"ls"}`}},
		}},
		{Role: "tool", Content: "file1", ToolCallID: "c5"}, // same output as before
	}

	result := opt.OptimizeConversation(messages)

	// First file read → replaced (same content as later read)
	if !strings.Contains(result[2].Content, "[Earlier file read:") {
		t.Errorf("first file read should be replaced, got %q", result[2].Content)
	}

	// First ls → replaced (same command, same output)
	if !strings.Contains(result[4].Content, "[Earlier command output:") {
		t.Errorf("first ls should be replaced, got %q", result[4].Content)
	}

	// Unknown tool → unchanged
	if result[6].Content != "wrote" {
		t.Errorf("unknown tool result should be unchanged, got %q", result[6].Content)
	}

	// Latest file read → kept
	if result[8].Content != "content" {
		t.Errorf("latest file read should be kept, got %q", result[8].Content)
	}

	// Latest ls → kept
	if result[10].Content != "file1" {
		t.Errorf("latest ls output should be kept, got %q", result[10].Content)
	}
}

func TestConversationOptimizer_PreservesNonToolMessages(t *testing.T) {
	opt := NewConversationOptimizer(ConversationOptimizerOptions{
		Enabled: true,
		KnownToolFn: func(name string) ToolCategory {
			if name == "read_file" {
				return ToolCategoryFileRead
			}
			return ToolCategoryUnknown
		},
	})

	messages := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}

	result := opt.OptimizeConversation(messages)

	if result[0].Content != "You are helpful." {
		t.Errorf("system message changed, got %q", result[0].Content)
	}
	if result[1].Content != "hello" {
		t.Errorf("user message changed, got %q", result[1].Content)
	}
	if result[2].Content != "hi there" {
		t.Errorf("assistant message changed, got %q", result[2].Content)
	}
}

func TestConversationOptimizer_EmptyMessages(t *testing.T) {
	opt := NewConversationOptimizer(ConversationOptimizerOptions{
		Enabled:     true,
		KnownToolFn: func(name string) ToolCategory { return ToolCategoryFileRead },
	})

	result := opt.OptimizeConversation(nil)
	if result != nil {
		t.Error("nil input should return nil")
	}

	result = opt.OptimizeConversation([]Message{})
	if len(result) != 0 {
		t.Error("empty input should return empty")
	}
}

func TestConversationOptimizer_MissingPathArg(t *testing.T) {
	opt := NewConversationOptimizer(ConversationOptimizerOptions{
		Enabled: true,
		KnownToolFn: func(name string) ToolCategory {
			return ToolCategoryFileRead
		},
	})

	// Tool call with no "path" argument → should be skipped gracefully.
	messages := []Message{
		{Role: "assistant", Content: "reading", ToolCalls: []ToolCall{
			{ID: "c1", Function: ToolCallFunction{Name: "read_file", Arguments: `{}`}},
		}},
		{Role: "tool", Content: "content", ToolCallID: "c1"},
	}

	result := opt.OptimizeConversation(messages)
	if result[1].Content != "content" {
		t.Errorf("should skip when path arg missing, got %q", result[1].Content)
	}
}

func TestConversationOptimizer_MissingCmdArg(t *testing.T) {
	opt := NewConversationOptimizer(ConversationOptimizerOptions{
		Enabled: true,
		KnownToolFn: func(name string) ToolCategory {
			return ToolCategoryShellCommand
		},
	})

	// Tool call with no "cmd" argument → should be skipped gracefully.
	messages := []Message{
		{Role: "assistant", Content: "running", ToolCalls: []ToolCall{
			{ID: "c1", Function: ToolCallFunction{Name: "shell", Arguments: `{}`}},
		}},
		{Role: "tool", Content: "output", ToolCallID: "c1"},
	}

	result := opt.OptimizeConversation(messages)
	if result[1].Content != "output" {
		t.Errorf("should skip when cmd arg missing, got %q", result[1].Content)
	}
}

func TestConversationOptimizer_InvalidJSONArgs(t *testing.T) {
	opt := NewConversationOptimizer(ConversationOptimizerOptions{
		Enabled: true,
		KnownToolFn: func(name string) ToolCategory {
			return ToolCategoryFileRead
		},
	})

	// Malformed JSON arguments → should be handled gracefully.
	messages := []Message{
		{Role: "assistant", Content: "reading", ToolCalls: []ToolCall{
			{ID: "c1", Function: ToolCallFunction{Name: "read_file", Arguments: `not json`}},
		}},
		{Role: "tool", Content: "content", ToolCallID: "c1"},
	}

	result := opt.OptimizeConversation(messages)
	if result[1].Content != "content" {
		t.Errorf("should skip invalid JSON, got %q", result[1].Content)
	}
}

func TestConversationOptimizer_ArgsNotString(t *testing.T) {
	opt := NewConversationOptimizer(ConversationOptimizerOptions{
		Enabled: true,
		KnownToolFn: func(name string) ToolCategory {
			return ToolCategoryFileRead
		},
	})

	// "path" is a number, not a string → should be skipped gracefully.
	args := map[string]interface{}{"path": 123}
	argsJSON, _ := json.Marshal(args)

	messages := []Message{
		{Role: "assistant", Content: "reading", ToolCalls: []ToolCall{
			{ID: "c1", Function: ToolCallFunction{Name: "read_file", Arguments: string(argsJSON)}}},
		},
		{Role: "tool", Content: "content", ToolCallID: "c1"},
	}

	result := opt.OptimizeConversation(messages)
	if result[1].Content != "content" {
		t.Errorf("should skip non-string path arg, got %q", result[1].Content)
	}
}

func TestIsTransientCommand(t *testing.T) {
	transientCmds := []string{"ls", "pwd", "echo", "find", "cat", "head", "tail", "wc"}
	for _, cmd := range transientCmds {
		if !isTransientCommand(cmd) {
			t.Errorf("expected %q to be transient", cmd)
		}
	}

	nonTransient := []string{"git", "make", "npm", "python", "go", "rm", "mv"}
	for _, cmd := range nonTransient {
		if isTransientCommand(cmd) {
			t.Errorf("expected %q to NOT be transient", cmd)
		}
	}
}

func TestBaseCommand(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{"ls -la", "ls"},
		{"pwd", "pwd"},
		{"find . -name '*.go'", "find"},
		{"", ""},
		{"  cat file.txt  ", "cat"},
	}
	for _, tt := range tests {
		got := baseCommand(tt.input)
		if got != tt.expect {
			t.Errorf("baseCommand(%q) = %q, want %q", tt.input, got, tt.expect)
		}
	}
}

func TestExtractStringArg(t *testing.T) {
	// Valid string arg
	got := extractStringArg(`{"path":"foo.txt"}`, "path")
	if got != "foo.txt" {
		t.Errorf("expected 'foo.txt', got %q", got)
	}

	// Missing key
	got = extractStringArg(`{"path":"foo.txt"}`, "cmd")
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}

	// Invalid JSON
	got = extractStringArg(`not json`, "path")
	if got != "" {
		t.Errorf("expected empty for invalid JSON, got %q", got)
	}

	// Non-string value
	got = extractStringArg(`{"path":123}`, "path")
	if got != "" {
		t.Errorf("expected empty for non-string, got %q", got)
	}
}

func TestConversationOptimizer_ShellCommandCap(t *testing.T) {
	opt := NewConversationOptimizer(ConversationOptimizerOptions{
		Enabled: true,
		KnownToolFn: func(name string) ToolCategory {
			if name == "shell" {
				return ToolCategoryShellCommand
			}
			return ToolCategoryUnknown
		},
	})

	// Run `ls` many times with unique output to exceed maxShellCmdRecords.
	// Each unique output should be tracked, but the map should be capped.
	var messages []Message
	messages = append(messages, Message{Role: "user", Content: "run ls"})
	for i := 0; i < maxShellCmdRecords+5; i++ {
		messages = append(messages, Message{
			Role:    "assistant",
			Content: fmt.Sprintf("ls run %d", i),
			ToolCalls: []ToolCall{
				{ID: fmt.Sprintf("c%d", i), Function: ToolCallFunction{Name: "shell", Arguments: `{"cmd":"ls"}`}},
			},
		})
		messages = append(messages, Message{
			Role:       "tool",
			Content:    fmt.Sprintf("output %d", i),
			ToolCallID: fmt.Sprintf("c%d", i),
		})
	}

	result := opt.OptimizeConversation(messages)

	// All outputs are unique, so none should be replaced.
	// The important thing is that it doesn't crash or hang.
	for i := 0; i < maxShellCmdRecords+5; i++ {
		idx := 1 + i*2 + 1 // tool result index
		if result[idx].Content != fmt.Sprintf("output %d", i) {
			t.Errorf("unique output %d should not be replaced, got %q", i, result[idx].Content)
		}
	}
}

func TestConversationOptimizer_FileReadCap(t *testing.T) {
	opt := NewConversationOptimizer(ConversationOptimizerOptions{
		Enabled: true,
		KnownToolFn: func(name string) ToolCategory {
			if name == "read_file" {
				return ToolCategoryFileRead
			}
			return ToolCategoryUnknown
		},
	})

	// Read the same file many times with identical content to exceed maxFileReadRecords.
	var messages []Message
	messages = append(messages, Message{Role: "user", Content: "read many times"})
	for i := 0; i < maxFileReadRecords+3; i++ {
		messages = append(messages, Message{
			Role:    "assistant",
			Content: fmt.Sprintf("read %d", i),
			ToolCalls: []ToolCall{
				{ID: fmt.Sprintf("c%d", i), Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"f.txt"}`}},
			},
		})
		messages = append(messages, Message{
			Role:       "tool",
			Content:    "same content every time",
			ToolCallID: fmt.Sprintf("c%d", i),
		})
	}

	result := opt.OptimizeConversation(messages)

	// All but the last read should be replaced with placeholder.
	// The last read should be kept.
	lastIdx := len(result) - 1
	if result[lastIdx].Content != "same content every time" {
		t.Errorf("last read should be kept, got %q", result[lastIdx].Content)
	}

	// Count how many reads were replaced.
	replaced := 0
	for i := 0; i < maxFileReadRecords+3; i++ {
		idx := 1 + i*2 + 1 // tool result index
		if strings.Contains(result[idx].Content, "[Earlier file read:") {
			replaced++
		}
	}
	// All but the last should be replaced
	if replaced != maxFileReadRecords+2 {
		t.Errorf("expected %d replaced reads, got %d", maxFileReadRecords+2, replaced)
	}
}

// --- OptimizeConversationWithOptions: gate on observation masking ---

// optimizerMaskFixture builds a message list with N+1 large consumed tool
// results: N old results that the masking pass would replace, plus a final
// result inside observationMaskKeepLast that should always survive. Returns
// the list and the index of the first old result for assertion convenience.
func optimizerMaskFixture(t *testing.T) ([]Message, int) {
	t.Helper()

	bigBody := strings.Repeat("x", observationMaskMaxChars*2)
	msgs := []Message{
		{Role: "user", Content: "do many things"},
	}
	// Eight tool-call/tool-result pairs followed by a "consuming" assistant
	// message at the end. With observationMaskKeepLast=5, the first three
	// tool results are eligible to be masked when the mask pass runs.
	const pairs = 8
	for i := 0; i < pairs; i++ {
		callID := fmt.Sprintf("c%d", i)
		msgs = append(msgs,
			Message{Role: "assistant", ToolCalls: []ToolCall{
				{ID: callID, Function: ToolCallFunction{Name: "read_file", Arguments: fmt.Sprintf(`{"path":"f%d.txt"}`, i)}},
			}},
			Message{Role: "tool", ToolCallID: callID, Content: bigBody},
		)
	}
	// Trailing assistant message marks the prior results as "consumed".
	msgs = append(msgs, Message{Role: "assistant", Content: "ok"})

	// First tool-result index after the user prompt is 2 (0=user, 1=assistant).
	return msgs, 2
}

// TestOptimizeConversationWithOptions_MaskOff_KeepsBigConsumedResults verifies
// that calling the new entry point with MaskConsumedToolResults=false leaves
// the raw tool-result bodies in place. This is the "below pressure" path —
// without it the gate in prepareMessages would do nothing.
func TestOptimizeConversationWithOptions_MaskOff_KeepsBigConsumedResults(t *testing.T) {
	opt := NewConversationOptimizer(ConversationOptimizerOptions{
		Enabled:     true,
		KnownToolFn: func(name string) ToolCategory { return ToolCategoryFileRead },
	})

	msgs, firstToolIdx := optimizerMaskFixture(t)
	originalLen := len(msgs[firstToolIdx].Content)

	result := opt.OptimizeConversationWithOptions(msgs, OptimizeOptions{MaskConsumedToolResults: false})

	// Every tool-result content should be unchanged (no placeholder).
	for i, m := range result {
		if m.Role != "tool" {
			continue
		}
		if strings.Contains(m.Content, "[PREVIOUS RESULT:") || strings.Contains(m.Content, "consumed") {
			t.Errorf("message[%d] looks masked when MaskConsumedToolResults=false: %q", i, m.Content)
		}
		if len(m.Content) != originalLen {
			t.Errorf("message[%d] content was modified: got %d chars, want %d", i, len(m.Content), originalLen)
		}
	}
}

// TestOptimizeConversationWithOptions_MaskOn_ReplacesOldConsumedResults verifies
// that the new entry point with MaskConsumedToolResults=true preserves the
// historical behavior (old big consumed tool results get placeholders). The
// last observationMaskKeepLast results must survive even with the mask on.
func TestOptimizeConversationWithOptions_MaskOn_ReplacesOldConsumedResults(t *testing.T) {
	opt := NewConversationOptimizer(ConversationOptimizerOptions{
		Enabled:     true,
		KnownToolFn: func(name string) ToolCategory { return ToolCategoryFileRead },
	})

	msgs, _ := optimizerMaskFixture(t)

	result := opt.OptimizeConversationWithOptions(msgs, OptimizeOptions{MaskConsumedToolResults: true})

	// Collect tool-result indices and bodies, in order.
	var bodies []string
	for _, m := range result {
		if m.Role == "tool" {
			bodies = append(bodies, m.Content)
		}
	}
	if len(bodies) != 8 {
		t.Fatalf("expected 8 tool results, got %d", len(bodies))
	}

	// First 3 (8 - observationMaskKeepLast=5) should be placeholders;
	// last 5 should keep their original size.
	for i := 0; i < 3; i++ {
		if !strings.Contains(bodies[i], "[PREVIOUS RESULT:") {
			t.Errorf("tool result %d expected to be masked, got: %q", i, truncateForLog(bodies[i]))
		}
	}
	for i := 3; i < 8; i++ {
		if strings.Contains(bodies[i], "[PREVIOUS RESULT:") {
			t.Errorf("tool result %d (in keep-last window) should not be masked: %q", i, truncateForLog(bodies[i]))
		}
	}
}

// TestOptimizeConversation_LegacyEntryPointMasksByDefault confirms the
// backward-compatible alias still applies observation masking — important
// for callers that haven't migrated to OptimizeConversationWithOptions.
func TestOptimizeConversation_LegacyEntryPointMasksByDefault(t *testing.T) {
	opt := NewConversationOptimizer(ConversationOptimizerOptions{
		Enabled:     true,
		KnownToolFn: func(name string) ToolCategory { return ToolCategoryFileRead },
	})

	msgs, _ := optimizerMaskFixture(t)

	result := opt.OptimizeConversation(msgs)

	masked := 0
	for _, m := range result {
		if m.Role == "tool" && strings.Contains(m.Content, "[PREVIOUS RESULT:") {
			masked++
		}
	}
	if masked == 0 {
		t.Errorf("legacy OptimizeConversation should mask consumed tool results; none were masked")
	}
}

func truncateForLog(s string) string {
	if len(s) > 80 {
		return s[:77] + "..."
	}
	return s
}
