package commands

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestRewindCommand_Name(t *testing.T) {
	cmd := &RewindCommand{}
	if got := cmd.Name(); got != "rewind" {
		t.Errorf("RewindCommand.Name() = %q, want %q", got, "rewind")
	}
}

func TestRewindCommand_Description(t *testing.T) {
	cmd := &RewindCommand{}
	got := cmd.Description()
	if got == "" {
		t.Error("RewindCommand.Description() returned empty string")
	}
}

func TestRewindCommand_Execute_NilAgent(t *testing.T) {
	cmd := &RewindCommand{}
	err := cmd.Execute(nil, nil)
	if err == nil {
		t.Error("Execute with nil agent should return error")
	}
}

func TestRewindCommand_Execute_NoCheckpoints(t *testing.T) {
	a := agent.NewTestAgent()
	cmd := &RewindCommand{}
	err := cmd.Execute(nil, a)
	if err == nil {
		t.Error("Execute with no checkpoints should return error")
	}
}

func TestRewindCommand_Execute_InvalidArg(t *testing.T) {
	a := agent.NewTestAgent()
	a.AddMessage(api.Message{Role: "user", Content: "hello"})
	a.RecordTurnCheckpoint(0, 0)

	cmd := &RewindCommand{}
	err := cmd.Execute([]string{"abc"}, a)
	if err == nil {
		t.Error("Execute with invalid arg should return error")
	}
	if !strings.Contains(err.Error(), "invalid turn number") {
		t.Errorf("Error should mention invalid turn number, got: %v", err)
	}
}

func TestRewindCommand_Execute_OutOfRangeIndex(t *testing.T) {
	a := agent.NewTestAgent()
	a.AddMessage(api.Message{Role: "user", Content: "hello"})
	a.RecordTurnCheckpoint(0, 0)

	cmd := &RewindCommand{}
	// One checkpoint → valid range is [0, 0]. Index 1 is out of range.
	err := cmd.Execute([]string{"1"}, a)
	if err == nil {
		t.Error("Execute with out-of-range index should return error")
	}
}

func TestRewindCommand_Execute_NegativeIndex(t *testing.T) {
	a := agent.NewTestAgent()
	a.AddMessage(api.Message{Role: "user", Content: "hello"})
	a.RecordTurnCheckpoint(0, 0)

	cmd := &RewindCommand{}
	err := cmd.Execute([]string{"-1"}, a)
	if err == nil {
		t.Error("Execute with negative index should return error")
	}
}

func TestRewindCommand_Execute_Success(t *testing.T) {
	a := agent.NewTestAgent()

	// Add 4 messages across two turns
	a.AddMessage(api.Message{Role: "user", Content: "msg-0"})
	a.AddMessage(api.Message{Role: "assistant", Content: "msg-1"})
	a.AddMessage(api.Message{Role: "user", Content: "msg-2"})
	a.AddMessage(api.Message{Role: "assistant", Content: "msg-3"})

	// Record two checkpoints: turn 0 covers messages 0-1, turn 1 covers 2-3
	a.RecordTurnCheckpoint(0, 1)
	a.RecordTurnCheckpoint(2, 3)

	// Rewind to turn 1 (StartIndex=2) — should keep first 2 messages
	cmd := &RewindCommand{}
	err := cmd.Execute([]string{"1"}, a)
	if err != nil {
		t.Fatalf("Execute should succeed, got error: %v", err)
	}

	msgs := a.GetMessages()
	if len(msgs) != 2 {
		t.Errorf("After rewind to turn 1, expected 2 messages, got %d", len(msgs))
	}
}

func TestRewindCommand_Execute_Success_TurnZero(t *testing.T) {
	a := agent.NewTestAgent()

	// Add 4 messages across two turns
	a.AddMessage(api.Message{Role: "user", Content: "msg-0"})
	a.AddMessage(api.Message{Role: "assistant", Content: "msg-1"})
	a.AddMessage(api.Message{Role: "user", Content: "msg-2"})
	a.AddMessage(api.Message{Role: "assistant", Content: "msg-3"})

	// Record two checkpoints
	a.RecordTurnCheckpoint(0, 1)
	a.RecordTurnCheckpoint(2, 3)

	// Rewind to turn 0 (StartIndex=0) — should truncate all messages
	cmd := &RewindCommand{}
	err := cmd.Execute([]string{"0"}, a)
	if err != nil {
		t.Fatalf("Execute should succeed, got error: %v", err)
	}

	msgs := a.GetMessages()
	if len(msgs) != 0 {
		t.Errorf("After rewind to turn 0, expected 0 messages, got %d", len(msgs))
	}
}

func TestRewindCommand_Execute_NoRevertFlag(t *testing.T) {
	a := agent.NewTestAgent()

	a.AddMessage(api.Message{Role: "user", Content: "msg-0"})
	a.AddMessage(api.Message{Role: "assistant", Content: "msg-1"})
	a.AddMessage(api.Message{Role: "user", Content: "msg-2"})
	a.AddMessage(api.Message{Role: "assistant", Content: "msg-3"})

	a.RecordTurnCheckpoint(0, 1)
	a.RecordTurnCheckpoint(2, 3)

	// --no-revert after the turn number
	cmd := &RewindCommand{}
	err := cmd.Execute([]string{"1", "--no-revert"}, a)
	if err != nil {
		t.Fatalf("Execute with --no-revert should succeed, got error: %v", err)
	}

	msgs := a.GetMessages()
	if len(msgs) != 2 {
		t.Errorf("After rewind to turn 1 with --no-revert, expected 2 messages, got %d", len(msgs))
	}
}

func TestRewindCommand_Execute_NoRevertFlagBeforeTurn(t *testing.T) {
	a := agent.NewTestAgent()

	a.AddMessage(api.Message{Role: "user", Content: "msg-0"})
	a.AddMessage(api.Message{Role: "assistant", Content: "msg-1"})
	a.AddMessage(api.Message{Role: "user", Content: "msg-2"})
	a.AddMessage(api.Message{Role: "assistant", Content: "msg-3"})

	a.RecordTurnCheckpoint(0, 1)
	a.RecordTurnCheckpoint(2, 3)

	// --no-revert before the turn number
	cmd := &RewindCommand{}
	err := cmd.Execute([]string{"--no-revert", "1"}, a)
	if err != nil {
		t.Fatalf("Execute with --no-revert before turn should succeed, got error: %v", err)
	}

	msgs := a.GetMessages()
	if len(msgs) != 2 {
		t.Errorf("After rewind to turn 1 with --no-revert before turn, expected 2 messages, got %d", len(msgs))
	}
}

func TestRewindCommand_Usage(t *testing.T) {
	cmd := &RewindCommand{}

	// Verify RewindCommand implements UsageProvider (defined in commands.go)
	var _ UsageProvider = cmd

	usage := cmd.Usage()
	if usage == "" {
		t.Error("Usage() returned empty string")
	}
	if !strings.Contains(usage, "rewind") {
		t.Errorf("Usage() should contain 'rewind', got: %q", usage)
	}
}
