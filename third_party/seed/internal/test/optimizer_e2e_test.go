package test

import (
	"context"
	"strings"
	"testing"

	"github.com/sprout-foundry/seed/core"
)

// --- Conversation Optimizer E2E Tests ---

func TestE2E_Optimizer_FileReadDedup(t *testing.T) {
	// Scenario: agent reads the same file twice with identical content.
	// The optimizer should replace the first read with a placeholder.
	h := NewHarnessWithT(t)

	// First iteration: read_file tool call
	h.Provider().AddToolCallResponse(
		"Reading the file.",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"config.yaml"}`,
			},
		},
	)
	// Second iteration: read_file again (same file, same content)
	h.Provider().AddToolCallResponse(
		"Reading again to verify.",
		core.ToolCall{
			ID: "call_2",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"config.yaml"}`,
			},
		},
	)
	// Third iteration: final answer
	h.Provider().AddTextResponse("The file contains the expected configuration.")

	h.Executor().AddTool(core.Tool{Function: core.ToolFunction{Name: "read_file"}})
	h.Executor().AddToolResult("call_1", "host: localhost\nport: 5432")
	h.Executor().AddToolResult("call_2", "host: localhost\nport: 5432")

	optimizer := core.NewConversationOptimizer(core.ConversationOptimizerOptions{
		Enabled: true,
		KnownToolFn: func(name string) core.ToolCategory {
			if name == "read_file" {
				return core.ToolCategoryFileRead
			}
			return core.ToolCategoryUnknown
		},
	})

	agent := h.NewAgentWithOptions(core.Options{
		Optimizer: optimizer,
	})

	result, err := agent.Run(context.Background(), "Read config.yaml")
	h.AssertNoError(err)
	h.AssertEquals(result, "The file contains the expected configuration.")

	// Provider called 3 times
	h.AssertProviderCalledN(3)

	// Verify the last provider request had the first file read replaced
	lastReq := h.Provider().Calls[2]
	foundPlaceholder := false
	for _, msg := range lastReq.Messages {
		if strings.Contains(msg.Content, "[Earlier file read: config.yaml]") {
			foundPlaceholder = true
			break
		}
	}
	if !foundPlaceholder {
		t.Error("expected first file read to be replaced with placeholder in last provider request")
	}

	// Verify the latest file read is still present
	foundLatest := false
	for _, msg := range lastReq.Messages {
		if strings.Contains(msg.Content, "host: localhost") {
			foundLatest = true
			break
		}
	}
	if !foundLatest {
		t.Error("expected latest file read content to be present in last provider request")
	}
}

func TestE2E_Optimizer_ShellCommandDedup(t *testing.T) {
	// Scenario: agent runs the same transient shell command twice.
	// The optimizer should replace the first output with a placeholder.
	h := NewHarnessWithT(t)

	// First iteration: shell ls
	h.Provider().AddToolCallResponse(
		"Listing directory.",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "shell",
				Arguments: `{"cmd":"ls -la"}`,
			},
		},
	)
	// Second iteration: shell ls again
	h.Provider().AddToolCallResponse(
		"Listing again.",
		core.ToolCall{
			ID: "call_2",
			Function: core.ToolCallFunction{
				Name:      "shell",
				Arguments: `{"cmd":"ls -la"}`,
			},
		},
	)
	// Third iteration: final answer
	h.Provider().AddTextResponse("The directory has three files.")

	h.Executor().AddTool(core.Tool{Function: core.ToolFunction{Name: "shell"}})
	h.Executor().AddToolResult("call_1", "file1\nfile2\nfile3\n")
	h.Executor().AddToolResult("call_2", "file1\nfile2\nfile3\n")

	optimizer := core.NewConversationOptimizer(core.ConversationOptimizerOptions{
		Enabled: true,
		KnownToolFn: func(name string) core.ToolCategory {
			if name == "shell" {
				return core.ToolCategoryShellCommand
			}
			return core.ToolCategoryUnknown
		},
	})

	agent := h.NewAgentWithOptions(core.Options{
		Optimizer: optimizer,
	})

	result, err := agent.Run(context.Background(), "List directory")
	h.AssertNoError(err)
	h.AssertEquals(result, "The directory has three files.")

	h.AssertProviderCalledN(3)

	// Verify the last provider request had the first ls output replaced
	lastReq := h.Provider().Calls[2]
	foundPlaceholder := false
	for _, msg := range lastReq.Messages {
		if strings.Contains(msg.Content, "[Earlier command output: ls -la]") {
			foundPlaceholder = true
			break
		}
	}
	if !foundPlaceholder {
		t.Error("expected first ls output to be replaced with placeholder in last provider request")
	}
}

func TestE2E_Optimizer_Disabled_NoOptimization(t *testing.T) {
	// When optimizer is disabled, no deduplication should occur.
	h := NewHarnessWithT(t)

	h.Provider().AddToolCallResponse(
		"Reading.",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"a.txt"}`,
			},
		},
	)
	h.Provider().AddToolCallResponse(
		"Reading again.",
		core.ToolCall{
			ID: "call_2",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"a.txt"}`,
			},
		},
	)
	h.Provider().AddTextResponse("Done.")

	h.Executor().AddTool(core.Tool{Function: core.ToolFunction{Name: "read_file"}})
	h.Executor().AddToolResult("call_1", "same content")
	h.Executor().AddToolResult("call_2", "same content")

	optimizer := core.NewConversationOptimizer(core.ConversationOptimizerOptions{
		Enabled: false, // disabled
		KnownToolFn: func(name string) core.ToolCategory {
			return core.ToolCategoryFileRead
		},
	})

	agent := h.NewAgentWithOptions(core.Options{
		Optimizer: optimizer,
	})

	_, err := agent.Run(context.Background(), "Read file")
	h.AssertNoError(err)

	// With optimizer disabled, the first file read content should NOT be replaced
	lastReq := h.Provider().Calls[2]
	for _, msg := range lastReq.Messages {
		if msg.Role == "tool" && strings.Contains(msg.Content, "[Earlier file read:") {
			t.Error("disabled optimizer should not replace content")
		}
	}
}

func TestE2E_Optimizer_Nil_NoPanic(t *testing.T) {
	// When no optimizer is provided (nil), the conversation should work normally.
	h := NewHarnessWithT(t)

	h.Provider().AddToolCallResponse(
		"Reading.",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"a.txt"}`,
			},
		},
	)
	h.Provider().AddTextResponse("Done.")

	h.Executor().AddTool(core.Tool{Function: core.ToolFunction{Name: "read_file"}})
	h.Executor().AddToolResult("call_1", "content")

	// No optimizer provided (nil)
	agent := h.NewAgent()

	_, err := agent.Run(context.Background(), "Read file")
	h.AssertNoError(err)
}
