//go:build !js

package webui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestNewChatSessionWorktree(t *testing.T) {
	cs := newChatSession("test-id", "Test Chat")
	if cs.ID != "test-id" {
		t.Errorf("expected ID test-id, got %q", cs.ID)
	}
	if cs.Name != "Test Chat" {
		t.Errorf("expected name Test Chat, got %q", cs.Name)
	}
	if cs.IsPinned {
		t.Error("expected IsPinned to be false")
	}
	if cs.AgentState == nil {
		t.Error("expected non-nil AgentState")
	}
}

func TestNewChatSessionAutoID(t *testing.T) {
	cs := newChatSession("", "Test Chat")
	if cs.ID == "" {
		t.Error("expected auto-generated ID")
	}
	if len(cs.ID) < 5 || cs.ID[:5] != "chat-" {
		t.Errorf("expected ID to start with 'chat-', got %q", cs.ID)
	}
}

func TestNewDefaultChatSessionWorktree(t *testing.T) {
	cs := newDefaultChatSession()
	if cs.ID != "default" {
		t.Errorf("expected ID default, got %q", cs.ID)
	}
	if cs.Name != "Chat" {
		t.Errorf("expected name Chat, got %q", cs.Name)
	}
}

func TestChatSessionTouch(t *testing.T) {
	cs := newDefaultChatSession()
	oldTime := cs.LastActiveAt
	time.Sleep(1 * time.Millisecond)
	cs.touch()
	if !cs.LastActiveAt.After(oldTime) {
		t.Error("expected LastActiveAt to be updated after touch")
	}
}

func TestChatSessionSetQueryActive(t *testing.T) {
	cs := newDefaultChatSession()
	cs.setQueryActive(true, "test query")
	if !cs.ActiveQuery {
		t.Error("expected ActiveQuery to be true")
	}
	if cs.CurrentQuery != "test query" {
		t.Errorf("expected CurrentQuery 'test query', got %q", cs.CurrentQuery)
	}

	cs.setQueryActive(false, "")
	if cs.ActiveQuery {
		t.Error("expected ActiveQuery to be false")
	}
	if cs.CurrentQuery != "" {
		t.Errorf("expected CurrentQuery to be empty, got %q", cs.CurrentQuery)
	}
}

func TestChatSessionSetGetWorktreePath(t *testing.T) {
	cs := newDefaultChatSession()
	cs.setWorktreePath("/path/to/worktree")
	got := cs.getWorktreePath()
	if got != "/path/to/worktree" {
		t.Errorf("expected /path/to/worktree, got %q", got)
	}

	cs.setWorktreePath("")
	got = cs.getWorktreePath()
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestChatSessionMessageCount(t *testing.T) {
	cs := newDefaultChatSession()
	count := cs.messageCount()
	if count != 0 {
		t.Errorf("expected 0 messages, got %d", count)
	}
}

func TestChatSessionAgentSessionID(t *testing.T) {
	cs := newDefaultChatSession()
	id := cs.agentSessionID()
	// Empty agent state should return empty session ID
	if id != "" {
		t.Errorf("expected empty session ID, got %q", id)
	}
}

func TestChatSessionToInfo(t *testing.T) {
	cs := newChatSession("test-id", "Test Chat")
	info := cs.toInfo()
	if info.ID != "test-id" {
		t.Errorf("expected ID test-id, got %q", info.ID)
	}
	if info.Name != "Test Chat" {
		t.Errorf("expected name Test Chat, got %q", info.Name)
	}
	if info.MessageCount != 0 {
		t.Errorf("expected 0 messages, got %d", info.MessageCount)
	}
}

func TestChatSessionChatSessionSummary(t *testing.T) {
	cs := newChatSession("test-id", "Test Chat")
	summary := cs.chatSessionSummary(true)
	if summary["id"] != "test-id" {
		t.Errorf("expected id test-id, got %v", summary["id"])
	}
	if summary["is_default"] != true {
		t.Error("expected is_default to be true")
	}
	if summary["is_pinned"] != false {
		t.Error("expected is_pinned to be false")
	}
}

func TestChatSessionChatSessionWithMessages(t *testing.T) {
	cs := newChatSession("test-id", "Test Chat")
	result := cs.chatSessionWithMessages()
	if result["id"] != "test-id" {
		t.Errorf("expected id test-id, got %v", result["id"])
	}
	if result["is_default"] != false {
		t.Error("expected is_default to be false for non-default session")
	}
}

func TestChatSessionWithMessagesStripsUserTimestamp(t *testing.T) {
	cs := newChatSession("test-id", "Test Chat")
	state := agent.AgentState{Messages: []api.Message{
		{Role: "user", Content: "<current-time>2026-07-22T12:37:28Z</current-time>\n\nhello"},
		{Role: "assistant", Content: "response", ReasoningContent: "reasoning"},
	}}
	var err error
	cs.AgentState, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	result := cs.chatSessionWithMessages()
	messages := result["messages"].([]map[string]interface{})
	if got := messages[0]["content"]; got != "hello" {
		t.Fatalf("user content = %q, want hello", got)
	}
	if got := messages[1]["reasoning_content"]; got != "reasoning" {
		t.Fatalf("reasoning content changed: %q", got)
	}
}

func TestChatSessionChatSessionWithProvider(t *testing.T) {
	cs := newChatSession("test-id", "Test Chat")
	cs.Provider = "openai"
	cs.Model = "gpt-4"
	result := cs.chatSessionSummary(false)
	if result["provider"] != "openai" {
		t.Errorf("expected provider openai, got %v", result["provider"])
	}
	if result["model"] != "gpt-4" {
		t.Errorf("expected model gpt-4, got %v", result["model"])
	}
}

func TestGenerateChatID(t *testing.T) {
	id1 := generateChatID()
	id2 := generateChatID()
	if id1 == "" || id2 == "" {
		t.Error("expected non-empty IDs")
	}
	if id1 == id2 {
		// Very unlikely to collide, but possible with time-based IDs
	}
	if id1[:5] != "chat-" {
		t.Errorf("expected ID to start with 'chat-', got %q", id1[:5])
	}
}

func TestRandomSuffix(t *testing.T) {
	s1 := randomSuffix(4)
	s2 := randomSuffix(4)
	if len(s1) != 8 {
		t.Errorf("expected 8 chars for 4 bytes, got %d", len(s1))
	}
	if len(s2) != 8 {
		t.Errorf("expected 8 chars for 4 bytes, got %d", len(s2))
	}
}

func TestCreateSessionWithHandoff(t *testing.T) {
	cc := &webClientContext{
		ChatSessions:   make(map[string]*chatSession),
		nextChatNumber: 1,
	}

	// Create handoff with summary
	newID := cc.CreateSessionWithHandoff("source-id", "Implementing user authentication")
	if newID == "" {
		t.Fatal("expected non-empty new ID")
	}

	newSession := cc.getChatSession(newID)
	if newSession == nil {
		t.Fatal("expected new session to exist")
	}
	if newSession.HandoffContext != "Implementing user authentication" {
		t.Errorf("HandoffContext = %q, want %q", newSession.HandoffContext, "Implementing user authentication")
	}
}

func TestCreateSessionWithHandoff_EmptySummary(t *testing.T) {
	cc := &webClientContext{
		ChatSessions:   make(map[string]*chatSession),
		nextChatNumber: 1,
	}

	// Empty summary should create a regular session without handoff
	newID := cc.CreateSessionWithHandoff("source-id", "")
	if newID == "" {
		t.Fatal("expected non-empty new ID")
	}

	newSession := cc.getChatSession(newID)
	if newSession == nil {
		t.Fatal("expected new session to exist")
	}
	if newSession.HandoffContext != "" {
		t.Errorf("HandoffContext should be empty, got %q", newSession.HandoffContext)
	}
}

func TestFormatHandoffSystemPrompt(t *testing.T) {
	result := formatHandoffSystemPrompt("building a REST API")
	if !strings.Contains(result, "Context from Previous Chat") {
		t.Error("expected header in handoff prompt")
	}
	if !strings.Contains(result, "building a REST API") {
		t.Error("expected summary in handoff prompt")
	}
}

func TestFormatHandoffSystemPrompt_Empty(t *testing.T) {
	result := formatHandoffSystemPrompt("")
	if result != "" {
		t.Errorf("expected empty string for empty summary, got %q", result)
	}
}
