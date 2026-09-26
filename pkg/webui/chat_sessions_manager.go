//go:build !js

// Package webui provides React web server with embedded assets
package webui

// chat_sessions_manager.go — the webClientContext chat-management surface
// (list/delete/rename/active, state + worktree, query-active tracking) and the
// session-handoff + ID helpers, split out of chat_sessions.go.

import (
	crypto_rand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// getChatSession returns the chat session with the given ID, or nil if not found.
// The caller does NOT need to hold the server mutex; this is called while the
// server lock is already held by the surrounding methods.
func (cc *webClientContext) getChatSession(chatID string) *chatSession {
	if cc.ChatSessions == nil {
		return nil
	}
	return cc.ChatSessions[chatID]
}

// markChatCreated clears the deleted-chat tombstone for chatID. Call after
// inserting a (re)created chat session into ChatSessions so it becomes
// queryable again.
func (cc *webClientContext) markChatCreated(chatID string) {
	if cc.DeletedChats == nil {
		cc.DeletedChats = map[string]struct{}{}
	}
	delete(cc.DeletedChats, chatID)
}

// getOrCreateChatSession returns the chat session with the given ID, creating
// one if necessary. The auto-generated name follows the "Chat N" pattern.
func (cc *webClientContext) getOrCreateChatSession(chatID string) *chatSession {
	if cc.ChatSessions == nil {
		cc.ChatSessions = make(map[string]*chatSession)
	}
	if chatID == "" {
		chatID = cc.DefaultChatID
	}
	if cs, ok := cc.ChatSessions[chatID]; ok {
		return cs
	}
	cc.nextChatNumber++
	name := "Chat"
	if cc.nextChatNumber > 1 {
		name = name + " " + strconv.Itoa(cc.nextChatNumber)
	}
	cs := newChatSession(chatID, name)
	cc.ChatSessions[chatID] = cs
	cc.markChatCreated(chatID)
	return cs
}

// chatSessionInfo is a JSON-safe copy of chat session metadata (no mutex).
type chatSessionInfo struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	CreatedAt        time.Time `json:"created_at"`
	LastActiveAt     time.Time `json:"last_active_at"`
	CurrentSessionID string    `json:"current_session_id"`
	ActiveQuery      bool      `json:"active_query"`
	CurrentQuery     string    `json:"current_query"`
	MessageCount     int       `json:"message_count"`
	Provider         string    `json:"provider"`
	Model            string    `json:"model"`
	WorktreePath     string    `json:"worktree_path"`
	IsPinned         bool      `json:"is_pinned"`
	Mode             string    `json:"mode"`
}

// listChatSessions returns snapshots of all sessions sorted by most recently active.
func (cc *webClientContext) listChatSessions() []chatSessionInfo {
	if cc.ChatSessions == nil || len(cc.ChatSessions) == 0 {
		return []chatSessionInfo{}
	}
	infos := make([]chatSessionInfo, 0, len(cc.ChatSessions))
	for _, cs := range cc.ChatSessions {
		infos = append(infos, cs.toInfo())
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].LastActiveAt.After(infos[j].LastActiveAt)
	})
	return infos
}

// deleteChatSession deletes a chat session. Returns false if it cannot be deleted
// (it's the default, or it's the currently active chat, or it has an active query).
func (cc *webClientContext) deleteChatSession(chatID string) bool {
	if chatID == defaultChatID {
		return false
	}
	if chatID == cc.DefaultChatID {
		return false
	}
	if cc.ChatSessions == nil {
		return false
	}
	cs, ok := cc.ChatSessions[chatID]
	if !ok {
		return false
	}
	cs.mu.Lock()
	active := cs.ActiveQuery
	cs.mu.Unlock()
	if active {
		return false
	}
	delete(cc.ChatSessions, chatID)
	return true
}

// renameChatSession renames a chat session. Returns false if the session doesn't exist.
func (cc *webClientContext) renameChatSession(chatID, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if cc.ChatSessions == nil {
		return false
	}
	cs, ok := cc.ChatSessions[chatID]
	if !ok {
		return false
	}
	cs.mu.Lock()
	cs.Name = name
	cs.mu.Unlock()
	return true
}

// activeChatSession returns the currently active (default) chat session.
func (cc *webClientContext) activeChatSession() *chatSession {
	if cc.DefaultChatID != "" && cc.ChatSessions != nil {
		if cs, ok := cc.ChatSessions[cc.DefaultChatID]; ok {
			return cs
		}
	}
	// Fallback: if DefaultChatID points to nothing, try "default"
	if cs, ok := cc.ChatSessions[defaultChatID]; ok {
		cc.DefaultChatID = defaultChatID
		return cs
	}
	return nil
}

// ensureDefaultChatSession ensures that at minimum a "default" chat session exists.
// This is called during context initialization.
func (cc *webClientContext) ensureDefaultChatSession() {
	if cc.ChatSessions == nil {
		cc.ChatSessions = make(map[string]*chatSession)
	}
	if _, ok := cc.ChatSessions[defaultChatID]; !ok {
		cc.ChatSessions[defaultChatID] = newDefaultChatSession()
	}
	if cc.DefaultChatID == "" {
		cc.DefaultChatID = defaultChatID
	}
	if cc.nextChatNumber < 1 {
		cc.nextChatNumber = 1
	}
}

// getChatSessionState returns the agent state snapshot for the given chat.
// Falls back to the top-level AgentState if ChatSessions is nil (backward compat).
func (cc *webClientContext) getChatSessionState(chatID string) []byte {
	if cc.ChatSessions == nil {
		return cc.AgentState
	}
	if chatID == "" {
		chatID = cc.DefaultChatID
	}
	if cs, ok := cc.ChatSessions[chatID]; ok {
		cs.mu.Lock()
		defer cs.mu.Unlock()
		return append([]byte(nil), cs.AgentState...)
	}
	return cc.AgentState
}

// setChatSessionState sets the agent state snapshot for the given chat.
// Also updates the top-level AgentState for backward compatibility.
func (cc *webClientContext) setChatSessionState(chatID string, snapshot []byte) {
	if len(snapshot) == 0 {
		snapshot = emptyAgentStateSnapshot()
	}

	// Always update top-level for backward compat
	cc.AgentState = append([]byte(nil), snapshot...)

	sessionID := ""
	var state agent.AgentState
	if err := json.Unmarshal(snapshot, &state); err == nil {
		sessionID = strings.TrimSpace(state.SessionID)
	}

	if cc.ChatSessions == nil {
		cc.CurrentSessionID = sessionID
		return
	}
	if chatID == "" {
		chatID = cc.DefaultChatID
	}
	if cs, ok := cc.ChatSessions[chatID]; ok {
		cs.mu.Lock()
		cs.AgentState = append([]byte(nil), snapshot...)
		cs.CurrentSessionID = sessionID
		cs.LastActiveAt = time.Now()
		cs.mu.Unlock()
	}
	// Also update top-level from chat session
	cc.CurrentSessionID = sessionID
}

// getActiveChatID returns the default chat ID, or "default" if not set.
func (cc *webClientContext) getActiveChatID() string {
	if cc.DefaultChatID != "" {
		return cc.DefaultChatID
	}
	return defaultChatID
}

// hasActiveQueryForChat checks whether the specified chat has a query running.
// If chatID is empty, checks the active (default) chat.
//
// A chat that is absent from ChatSessions BUT in DeletedChats is treated as
// having an active query — the delete handler removed it from the map and may
// still be recomputing the top-level ActiveQuery flag. Without the tombstone,
// a query arriving in that window would fall through to the (now false)
// top-level flag and start a query on a chat that was just deleted.
func (cc *webClientContext) hasActiveQueryForChat(chatID string) bool {
	if cc.ChatSessions == nil {
		return cc.ActiveQuery
	}
	if chatID == "" {
		chatID = cc.DefaultChatID
	}
	cs, ok := cc.ChatSessions[chatID]
	if !ok {
		if _, deleted := cc.DeletedChats[chatID]; deleted {
			return true // deleted chat — reject queries
		}
		return cc.ActiveQuery
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.ActiveQuery
}

// setChatQueryActive sets the active query state for a specific chat and
// keeps the top-level ActiveQuery in sync (backward compat).
func (cc *webClientContext) setChatQueryActive(chatID string, active bool, query string) {
	// Update top-level for backward compat
	cc.ActiveQuery = active
	if active {
		cc.CurrentQuery = query
	} else {
		cc.CurrentQuery = ""
	}

	if cc.ChatSessions == nil {
		return
	}
	if chatID == "" {
		chatID = cc.DefaultChatID
	}
	if cs, ok := cc.ChatSessions[chatID]; ok {
		cs.setQueryActive(active, query)
	}
}

// clearAllChatQueryState resets ActiveQuery and CurrentQuery for every chat
// session and the top-level context. Used during panic recovery to ensure no
// chat is left stuck in a "running" state.
func (cc *webClientContext) clearAllChatQueryState() {
	if cc.ChatSessions != nil {
		for _, cs := range cc.ChatSessions {
			if cs != nil {
				cs.setQueryActive(false, "")
			}
		}
	}
	cc.ActiveQuery = false
	cc.CurrentQuery = ""
}

// setChatSessionWorktree sets the worktree path for a chat session.
// The caller is responsible for validating the path before calling this function.
// Callers must provide an absolute path. This function trusts its caller and
// stores the path as-is without any normalization.
func (cc *webClientContext) setChatSessionWorktree(chatID, worktreePath string) error {
	if cc.ChatSessions == nil {
		return fmt.Errorf("chat sessions not initialized")
	}
	if chatID == "" {
		chatID = cc.DefaultChatID
	}
	cs, ok := cc.ChatSessions[chatID]
	if !ok {
		return fmt.Errorf("chat session not found")
	}

	cs.setWorktreePath(worktreePath)
	return nil
}

// getChatSessionWorktree returns the worktree path for a chat session.
// If the chat session doesn't exist or has no worktree set, returns empty string.
func (cc *webClientContext) getChatSessionWorktree(chatID string) string {
	if cc.ChatSessions == nil {
		return ""
	}
	if chatID == "" {
		chatID = cc.DefaultChatID
	}
	cs, ok := cc.ChatSessions[chatID]
	if !ok {
		return ""
	}
	return cs.getWorktreePath()
}

// generateChatID generates a unique chat session ID.
func generateChatID() string {
	return "chat-" + time.Now().Format("20060102-150405") + "-" + randomSuffix(4)
}

// randomSuffix generates a short random hex string for unique IDs.
func randomSuffix(n int) string {
	b := make([]byte, n)
	if _, err := crypto_rand.Read(b); err != nil {
		// Fallback to time-based suffix if crypto/rand is unavailable
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// emptyAgentStateSnapshot is already defined in client_context.go; at
// runtime it's the same zero-argument reference. We redeclare here so
// this file compiles independently (the linker resolves to the single
// definition). However, since we're in the same package, referencing
// the existing function directly is fine — no redeclaration needed.

// formatHandoffSystemPrompt formats a handoff context string into a
// system prompt section for injection into a new session.
func formatHandoffSystemPrompt(summary string) string {
	if summary == "" {
		return ""
	}
	return fmt.Sprintf("\n\n## Context from Previous Chat\n\nYou were working on: %s\nThe conversation has shifted to a new topic. Use the above context as background only.", summary)
}

// CreateSessionWithHandoff creates a new chat session with context handed off
// from the source session. The handoff context is injected into the new
// session's system prompt, providing continuity across sessions.
//
// Parameters:
//   - sourceChatID: the chat ID to extract handoff context from
//   - summary: the actionable summary from the previous session's last turn
//
// Returns: the new chat session's ID, or empty string on error.
func (cc *webClientContext) CreateSessionWithHandoff(sourceChatID, summary string) string {
	if summary == "" {
		// No summary — just create a regular new session
		newID := generateChatID()
		cc.getOrCreateChatSession(newID)
		return newID
	}

	newID := generateChatID()
	cc.nextChatNumber++
	name := "Chat " + strconv.Itoa(cc.nextChatNumber)

	cs := newChatSession(newID, name)
	cs.HandoffContext = summary
	if cc.ChatSessions == nil {
		cc.ChatSessions = make(map[string]*chatSession)
	}
	cc.ChatSessions[newID] = cs

	return newID
}
