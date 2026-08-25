package commands

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestForkCommand_Name(t *testing.T) {
	cmd := &ForkCommand{}
	if got := cmd.Name(); got != "fork" {
		t.Errorf("ForkCommand.Name() = %q, want %q", got, "fork")
	}
}

func TestForkCommand_Description(t *testing.T) {
	cmd := &ForkCommand{}
	if got := cmd.Description(); got == "" {
		t.Error("ForkCommand.Description() returned empty string")
	}
}

func TestForkCommand_Usage(t *testing.T) {
	cmd := &ForkCommand{}
	usage := cmd.Usage()
	if !strings.Contains(usage, "/fork") {
		t.Errorf("ForkCommand.Usage() = %q; expected to mention /fork", usage)
	}
}

func TestForkCommand_NoArgs_ListsBreakpoints(t *testing.T) {
	chatAgent, err := agent.NewAgentWithModel("")
	if err != nil {
		t.Fatalf("NewAgentWithModel failed: %v", err)
	}

	// Add some messages so there are breakpoints.
	chatAgent.AddMessage(api.Message{Role: "user", Content: "question one"})
	chatAgent.AddMessage(api.Message{Role: "assistant", Content: "answer one"})
	chatAgent.AddMessage(api.Message{Role: "user", Content: "question two"})

	priorID := chatAgent.GetSessionID()

	cmd := &ForkCommand{}
	// No args: should list breakpoints without modifying state.
	err = cmd.Execute(nil, chatAgent)
	if err != nil {
		t.Fatalf("ForkCommand.Execute() (no args) returned error: %v", err)
	}

	// Verify messages are NOT modified by listing.
	msgs := chatAgent.GetMessages()
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages after listing, got %d", len(msgs))
	}

	// Session ID must be unchanged.
	if sid := chatAgent.GetSessionID(); sid != priorID && priorID != "" {
		t.Errorf("session ID changed after listing: %q → %q", priorID, sid)
	}
}

func TestForkCommand_WithIndex_ForksAndPrintsID(t *testing.T) {
	chatAgent, err := agent.NewAgentWithModel("")
	if err != nil {
		t.Fatalf("NewAgentWithModel failed: %v", err)
	}

	priorID := "session_fork_command_test"
	chatAgent.SetSessionID(priorID)
	chatAgent.AddMessage(api.Message{Role: "user", Content: "Q1"})
	chatAgent.AddMessage(api.Message{Role: "assistant", Content: "A1"})
	chatAgent.AddMessage(api.Message{Role: "user", Content: "Q2"})
	chatAgent.AddMessage(api.Message{Role: "assistant", Content: "A2"})

	cmd := &ForkCommand{}
	err = cmd.Execute([]string{"1"}, chatAgent)
	if err != nil {
		t.Fatalf("ForkCommand.Execute([\"1\"]) returned error: %v", err)
	}

	// Session ID must be rotated.
	newID := chatAgent.GetSessionID()
	if newID == "" {
		t.Fatal("session ID was empty after fork")
	}
	if newID == priorID {
		t.Errorf("session ID did not change after fork: still %q", priorID)
	}
	if !strings.HasPrefix(newID, "session_") {
		t.Errorf("new ID %q does not start with 'session_'", newID)
	}

	// Messages must be truncated at breakpoint 1.
	msgs := chatAgent.GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message after fork at breakpoint 1, got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Content != "Q1" {
		t.Errorf("msg[0] = %q, want %q", msgs[0].Content, "Q1")
	}
}

func TestForkCommand_InvalidIndex_PrintsError(t *testing.T) {
	chatAgent, err := agent.NewAgentWithModel("")
	if err != nil {
		t.Fatalf("NewAgentWithModel failed: %v", err)
	}

	cmd := &ForkCommand{}

	// Non-numeric index.
	err = cmd.Execute([]string{"abc"}, chatAgent)
	if err == nil {
		t.Error("ForkCommand.Execute([\"abc\"]) should return error for non-numeric index")
	}

	// Out-of-range index (no messages).
	err = cmd.Execute([]string{"1"}, chatAgent)
	if err == nil {
		t.Error("ForkCommand.Execute([\"1\"]) should return error when no messages exist")
	}
}

func TestForkCommand_NilAgent(t *testing.T) {
	cmd := &ForkCommand{}

	err := cmd.Execute(nil, nil)
	if err == nil {
		t.Error("ForkCommand.Execute() with nil agent should return error")
	}

	err = cmd.Execute([]string{"1"}, nil)
	if err == nil {
		t.Error("ForkCommand.Execute([\"1\"]) with nil agent should return error")
	}
}
