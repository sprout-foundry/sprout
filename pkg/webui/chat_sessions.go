//go:build !js

// Package webui provides React web server with embedded assets
package webui

// chat_sessions.go — the chatSession object: the type, its accessors,
// constructors, and the summary/message snapshots. The agent-wiring path
// lives in chat_sessions_agent.go; the webClientContext management surface
// (and session handoff) in chat_sessions_manager.go.

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

const (
	defaultChatID = "default"
)

// normalizeChatMode maps a chat's mode to its lane: "" (legacy/unassigned)
// reads as "code" (SP-142 §1) so pre-existing sessions keep today's
// behavior; unknown values also read as code — the wire never rejects a
// mode it doesn't know.
func normalizeChatMode(mode string) string {
	if strings.TrimSpace(strings.ToLower(mode)) == "design" {
		return "design"
	}
	return "code"
}

// chatSession stores per-chat state within a single browser tab context.
//
// @ts-generated  webui/src/types/generated.ts::ChatSession
// SP-034-5b: the JSON-tagged exported fields below are the canonical
// wire shape mirrored on the TS side. Adding a new persisted field
// here means updating types/generated.ts too (until SP-034-5a wires
// up the generator).
type chatSession struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	CreatedAt        time.Time `json:"created_at"`
	LastActiveAt     time.Time `json:"last_active_at"`
	AgentState       []byte    `json:"-"`
	CurrentSessionID string    `json:"current_session_id"`
	ActiveQuery      bool      `json:"active_query"`
	CurrentQuery     string    `json:"current_query"`
	IsPinned         bool      `json:"is_pinned"`
	Provider         string    `json:"provider"`
	Model            string    `json:"model"`
	WorktreePath     string    `json:"worktree_path"`

	// Mode is the workspace mode lane this chat belongs to: "code" or
	// "design" (SP-142). "" is a legacy/unassigned chat — the read side
	// treats it as code so pre-existing sessions keep today's behavior.
	Mode string `json:"mode"`

	// ConfigOverrides stores session-scoped configuration overrides that differ
	// from the global/workspace config. Populated when settings change during a session.
	ConfigOverrides map[string]interface{} `json:"config_overrides,omitempty"`

	// HandoffContext stores context from a previous chat session to inject
	// into the new session's system prompt. Set by CreateSessionWithHandoff.
	HandoffContext string `json:"handoff_context,omitempty"`

	Agent *agent.Agent `json:"-"`

	// mu serializes ALL field access on this chatSession. Promoted from
	// sync.Mutex to sync.RWMutex in SP-034-3d: pure-read paths
	// (messageCount, agentSessionID, snapshot-style readers used by the
	// multi-tab fan-out path) take RLock so concurrent readers don't
	// queue behind each other. Writes still take the full Lock — every
	// existing callsite that called `cs.mu.Lock()` keeps working
	// unchanged because RWMutex.Lock is the exclusive (writer) variant.
	mu sync.RWMutex

	// runBuffer holds the last N stream events for reattach replay when
	// a browser tab reconnects mid-query. Constructed lazily so chats
	// that never see a query don't pay the allocation. See
	// pkg/webui/chat_run_buffer.go and SP-034-2a.
	runBuffer *chatRunRingBuffer `json:"-"`

	// runBufferResetTimer schedules a Reset of runBuffer N seconds after
	// the last query_completed event. Cancelled and re-scheduled as new
	// queries start/complete on this chat. nil when no reset is pending.
	// SP-034-2f.
	runBufferResetTimer *time.Timer `json:"-"`
}

// messageCount returns the number of messages in the chat's agent state.
// Read-only: takes RLock so concurrent readers don't serialize.
func (cs *chatSession) messageCount() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.messageCountLocked()
}

// messageCountLocked is the lock-free helper for messageCount.
func (cs *chatSession) messageCountLocked() int {
	if len(cs.AgentState) == 0 {
		return 0
	}
	var state agent.AgentState
	if err := json.Unmarshal(cs.AgentState, &state); err != nil {
		return 0
	}
	return len(state.Messages)
}

// agentSessionID parses the session ID from the serialized agent state.
// Read-only: takes RLock.
func (cs *chatSession) agentSessionID() string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.agentSessionIDLocked()
}

// agentSessionIDLocked is the lock-free helper for agentSessionID.
func (cs *chatSession) agentSessionIDLocked() string {
	if len(cs.AgentState) == 0 {
		return ""
	}
	var state agent.AgentState
	if err := json.Unmarshal(cs.AgentState, &state); err != nil {
		return ""
	}
	return strings.TrimSpace(state.SessionID)
}

// touch updates the LastActiveAt timestamp.
func (cs *chatSession) touch() {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.LastActiveAt = time.Now()
}

// setQueryActive atomically sets the ActiveQuery flag and optional CurrentQuery.
func (cs *chatSession) setQueryActive(active bool, query string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.ActiveQuery = active
	if active {
		cs.CurrentQuery = query
	} else {
		cs.CurrentQuery = ""
	}
	cs.LastActiveAt = time.Now()
}

// setWorktreePath sets the worktree path for this chat session.
func (cs *chatSession) setWorktreePath(path string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.WorktreePath = path
	cs.LastActiveAt = time.Now()
}

// getWorktreePath returns the worktree path for this chat session.
// Read-only: takes RLock.
func (cs *chatSession) getWorktreePath() string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.WorktreePath
}

// newChatSession creates a new chat session with a unique ID and name.
func newChatSession(id, name string) *chatSession {
	if id == "" {
		id = generateChatID()
	}
	now := time.Now()
	return &chatSession{
		ID:           id,
		Name:         name,
		CreatedAt:    now,
		LastActiveAt: now,
		AgentState:   emptyAgentStateSnapshot(),
		IsPinned:     false,
		Mode:         "",
	}
}

// newChatSessionInMode creates a chat pinned to a workspace-mode lane
// (SP-142 §1): "code" or "design". Any other value normalizes to "" (the
// legacy read-as-code lane) — the wire never rejects a mode it doesn't
// know so forward-compatible clients keep working.
func newChatSessionInMode(id, name, mode string) *chatSession {
	cs := newChatSession(id, name)
	if mode == "code" || mode == "design" {
		cs.Mode = mode
	}
	return cs
}

// newDefaultChatSession creates the "default" chat session.
func newDefaultChatSession() *chatSession {
	return newChatSession(defaultChatID, "Chat")
}

// --- webClientContext chat session methods ---

// toInfo copies the public fields from cs under cs.mu.
func (cs *chatSession) toInfo() chatSessionInfo {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return chatSessionInfo{
		ID:               cs.ID,
		Name:             cs.Name,
		CreatedAt:        cs.CreatedAt,
		LastActiveAt:     cs.LastActiveAt,
		CurrentSessionID: cs.CurrentSessionID,
		ActiveQuery:      cs.ActiveQuery,
		CurrentQuery:     cs.CurrentQuery,
		MessageCount:     cs.messageCountLocked(),
		Provider:         cs.Provider,
		Model:            cs.Model,
		WorktreePath:     cs.WorktreePath,
		IsPinned:         cs.IsPinned,
		Mode:             cs.Mode,
	}
}

// chatSessionSummary produces a JSON-safe map with metadata for an API response.
func (cs *chatSession) chatSessionSummary(isDefault bool) map[string]interface{} {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	summary := map[string]interface{}{
		"id":                 cs.ID,
		"name":               cs.Name,
		"created_at":         cs.CreatedAt.UTC().Format(time.RFC3339),
		"last_active_at":     cs.LastActiveAt.UTC().Format(time.RFC3339),
		"message_count":      cs.messageCountLocked(),
		"current_session_id": cs.agentSessionIDLocked(),
		"active_query":       cs.ActiveQuery,
		"is_default":         isDefault,
		"is_pinned":          cs.IsPinned,
	}
	if cs.Provider != "" {
		summary["provider"] = cs.Provider
	}
	if cs.Model != "" {
		summary["model"] = cs.Model
	}
	if cs.WorktreePath != "" {
		summary["worktree_path"] = cs.WorktreePath
	}
	if cs.ActiveQuery && cs.CurrentQuery != "" {
		summary["current_query"] = cs.CurrentQuery
	}
	// The lane this chat belongs to (SP-142): "" reads as code client-side,
	// so legacy sessions surface in the Code tab list unchanged.
	if cs.Mode == "design" {
		summary["mode"] = cs.Mode
	}
	return summary
}

// chatSessionWithMessages produces a response map including the serialized
// agent state for a chat switch response.
func (cs *chatSession) chatSessionWithMessages() map[string]interface{} {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	summary := map[string]interface{}{
		"id":                 cs.ID,
		"name":               cs.Name,
		"created_at":         cs.CreatedAt.UTC().Format(time.RFC3339),
		"last_active_at":     cs.LastActiveAt.UTC().Format(time.RFC3339),
		"message_count":      cs.messageCountLocked(),
		"current_session_id": cs.agentSessionIDLocked(),
		"active_query":       cs.ActiveQuery,
		"is_default":         cs.ID == defaultChatID,
		"is_pinned":          cs.IsPinned,
	}
	if cs.Provider != "" {
		summary["provider"] = cs.Provider
	}
	if cs.Model != "" {
		summary["model"] = cs.Model
	}
	if cs.WorktreePath != "" {
		summary["worktree_path"] = cs.WorktreePath
	}
	if cs.ActiveQuery && cs.CurrentQuery != "" {
		summary["current_query"] = cs.CurrentQuery
	}
	// The lane this chat belongs to (SP-142); see chatSessionSummary.
	if cs.Mode == "design" {
		summary["mode"] = cs.Mode
	}

	// Decode agent state to extract messages for the frontend
	if len(cs.AgentState) > 0 {
		var state agent.AgentState
		if err := json.Unmarshal(cs.AgentState, &state); err == nil {
			// Build enriched messages with per-message timestamps.
			// The core.Message type (seed dependency) doesn't carry a
			// timestamp field, so we build a parallel array here so the
			// frontend can display accurate per-message times.
			timestamps := state.MessageTimestamps
			rawMsgs := state.Messages
			enriched := make([]map[string]interface{}, 0, len(rawMsgs))
			for i, msg := range rawMsgs {
				content := msg.Content
				if msg.Role == "user" {
					content = agent.StripUserMessageTimestamp(content)
				}
				m := map[string]interface{}{
					"role":    msg.Role,
					"content": content,
				}
				if msg.ReasoningContent != "" {
					m["reasoning_content"] = msg.ReasoningContent
				}
				if i < len(timestamps) {
					m["timestamp"] = timestamps[i].Format(time.RFC3339)
				}
				enriched = append(enriched, m)
			}
			summary["messages"] = enriched
			summary["total_tokens"] = state.TotalTokens
			summary["total_cost"] = state.TotalCost
			summary["session_id"] = state.SessionID
		}
	}

	// NOTE: the raw agent_state blob is deliberately NOT included. The agent
	// state lives server-side in cs.AgentState — the frontend only renders
	// the decoded `messages` above, and shipping the full serialized state
	// (which contains every message again plus reasoning) roughly doubles
	// the payload of every chat-switch / worktree response for no consumer.
	return summary
}
