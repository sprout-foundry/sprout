package agent

import (
	"fmt"
	"strings"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

func TestCollapseSystemMessagesToFrontMovesLateSystemMessages(t *testing.T) {
	messages := []api.Message{
		{Role: "system", Content: "base instructions"},
		{Role: "user", Content: "first request"},
		{Role: "assistant", Content: "working"},
		{Role: "system", Content: "[Skill Activated: test]\n\nextra instructions"},
		{Role: "user", Content: "continue"},
	}

	got := collapseSystemMessagesToFront(messages)

	if len(got) != 4 {
		t.Fatalf("expected 4 messages after collapsing systems, got %d", len(got))
	}
	if got[0].Role != "system" {
		t.Fatalf("expected first message to remain system, got %q", got[0].Role)
	}
	wantContent := "base instructions\n\n[Skill Activated: test]\n\nextra instructions"
	if got[0].Content != wantContent {
		t.Fatalf("unexpected merged system content:\nwant: %q\ngot:  %q", wantContent, got[0].Content)
	}
	if got[1].Role != "user" || got[1].Content != "first request" {
		t.Fatalf("expected first non-system message to be preserved, got %#v", got[1])
	}
	if got[2].Role != "assistant" || got[2].Content != "working" {
		t.Fatalf("expected assistant message to be preserved, got %#v", got[2])
	}
	if got[3].Role != "user" || got[3].Content != "continue" {
		t.Fatalf("expected trailing user message to be preserved, got %#v", got[3])
	}
}

func TestCollapseSystemMessagesToFrontLeavesValidMessagesUntouched(t *testing.T) {
	messages := []api.Message{
		{Role: "system", Content: "base instructions"},
		{Role: "user", Content: "request"},
	}

	got := collapseSystemMessagesToFront(messages)

	if len(got) != len(messages) {
		t.Fatalf("expected message count to remain %d, got %d", len(messages), len(got))
	}
	for i := range messages {
		if got[i].Role != messages[i].Role || got[i].Content != messages[i].Content {
			t.Fatalf("message %d changed unexpectedly: want %#v got %#v", i, messages[i], got[i])
		}
	}
}

// TestPrepareMessagesStripsAllSystemFromHistory verifies that system messages in conversation
// history are always stripped before the fresh system prompt is prepended. This covers:
//   - Exact duplicates of the current system prompt
//   - Stale system messages from previous personas after a persona transition
//   - System messages imported from a prior session with a different date/AGENTS.md
func TestPrepareMessagesStripsAllSystemFromHistory(t *testing.T) {
	agent := newTestAgent(t)

	agent.systemPrompt = "new system prompt"
	agent.baseSystemPrompt = "new system prompt"

	// Seed history with system messages: one matching the current prompt (old exact dup)
	// and one that was the prior persona's prompt (should also be stripped).
	agent.messages = []api.Message{
		{Role: "system", Content: "new system prompt"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
		{Role: "system", Content: "old persona prompt from a previous session"},
		{Role: "user", Content: "another message"},
	}

	prepared, err := newTestConversationHandler(t, agent).prepareMessagesForTest()
	if err != nil {
		t.Fatalf("prepareMessages: %v", err)
	}

	// There must be exactly one system message, at position 0, containing the current prompt.
	systemCount := 0
	for _, m := range prepared {
		if m.Role == "system" {
			systemCount++
		}
	}
	if systemCount != 1 {
		t.Errorf("expected exactly 1 system message in prepared messages, got %d", systemCount)
	}
	if len(prepared) == 0 || prepared[0].Role != "system" {
		t.Fatalf("expected system message at position 0, got %v", prepared[0].Role)
	}
	if prepared[0].Content != "new system prompt" {
		t.Errorf("unexpected system content: %q", prepared[0].Content)
	}
}

// TestSkillActivationFoldsIntoSystemPrompt verifies that activating a skill appends its
// instructions to a.systemPrompt rather than injecting a system message into a.messages.
// This ensures skill instructions are always present in the outgoing system prompt and
// are never accidentally stripped by the history-filtering step in prepareMessages.
func TestSkillActivationFoldsIntoSystemPrompt(t *testing.T) {
	agent := newTestAgent(t)
	basePrompt := agent.systemPrompt

	// Directly simulate what handleActivateSkill does after loading a skill.
	skillContent := "Always respond in haiku form."
	skillMessage := fmt.Sprintf("[Skill Activated: %s]\n\n%s", "haiku", skillContent)
	if strings.TrimSpace(agent.systemPrompt) != "" {
		agent.systemPrompt = agent.systemPrompt + "\n\n---\n\n" + skillMessage
	} else {
		agent.systemPrompt = skillMessage
	}

	// The system prompt must now contain the skill instructions.
	if !strings.Contains(agent.systemPrompt, skillContent) {
		t.Fatalf("expected systemPrompt to contain skill content, got: %q", agent.systemPrompt)
	}
	if !strings.Contains(agent.systemPrompt, basePrompt) {
		t.Fatalf("expected systemPrompt to still contain the base prompt")
	}

	// No system message should be injected into a.messages.
	for _, m := range agent.messages {
		if m.Role == "system" {
			t.Errorf("unexpected system message in a.messages after skill activation: %q", m.Content)
		}
	}

	// prepareMessages must emit exactly one system message containing skill content.
	agent.messages = []api.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	prepared, err := newTestConversationHandler(t, agent).prepareMessagesForTest()
	if err != nil {
		t.Fatalf("prepareMessages: %v", err)
	}
	if prepared[0].Role != "system" {
		t.Fatalf("expected first prepared message to be system, got %q", prepared[0].Role)
	}
	if !strings.Contains(prepared[0].Content, skillContent) {
		t.Errorf("prepared system message does not contain skill content:\n%q", prepared[0].Content)
	}
	systemCount := 0
	for _, m := range prepared {
		if m.Role == "system" {
			systemCount++
		}
	}
	if systemCount != 1 {
		t.Errorf("expected exactly 1 system message in prepared output, got %d", systemCount)
	}
}

// TestSessionNameSystemMessageIsStrippedFromPrepared verifies that the [SESSION_NAME:]
// metadata sidecar injected by SetSessionName into a.messages does NOT reach the model.
// generateSessionName reads from a.messages directly and does not require LLM visibility.
func TestSessionNameSystemMessageIsStrippedFromPrepared(t *testing.T) {
	agent := newTestAgent(t)
	agent.SetSessionName("my test session")

	// Confirm the message is in a.messages for generateSessionName to read.
	found := false
	for _, m := range agent.messages {
		if m.Role == "system" && strings.HasPrefix(m.Content, "[SESSION_NAME:]") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected [SESSION_NAME:] sidecar to be present in a.messages")
	}

	// Confirm generateSessionName can still read it.
	if name := agent.generateSessionName(); name != "my test session" {
		t.Errorf("generateSessionName = %q, want %q", name, "my test session")
	}

	// Confirm prepareMessages does NOT forward it to the model.
	agent.messages = append(agent.messages, api.Message{Role: "user", Content: "hello"})
	prepared, err := newTestConversationHandler(t, agent).prepareMessagesForTest()
	if err != nil {
		t.Fatalf("prepareMessages: %v", err)
	}
	for _, m := range prepared {
		if m.Role == "system" && strings.HasPrefix(m.Content, "[SESSION_NAME:]") {
			t.Errorf("session name sidecar was forwarded to the model as a system message")
		}
	}
}
