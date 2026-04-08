// Package webui provides React web server with embedded assets
package webui

import (
	crypto_rand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/events"
)

const (
	defaultChatID = "default"
)

// chatSession stores per-chat state within a single browser tab context.
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
	Agent            *agent.Agent `json:"-"`
	mu               sync.Mutex
}

// messageCount returns the number of messages in the chat's agent state.
// The caller must hold cs.mu or this method will acquire it.
func (cs *chatSession) messageCount() int {
	cs.mu.Lock()
	defer cs.mu.Unlock()
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
// The caller must hold cs.mu or this method will acquire it.
func (cs *chatSession) agentSessionID() string {
	cs.mu.Lock()
	defer cs.mu.Unlock()
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
func (cs *chatSession) getWorktreePath() string {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.WorktreePath
}

// getOrCreateAgent returns the agent for this chat session, creating one
// lazily if needed. The agent is created outside the chatSession mutex to
// avoid holding it during potentially slow I/O (JSON deserialization, state
// import). If two goroutines race to create the agent, only one wins and the
// other's agent becomes unreferenced.
//
// When the session has a Provider/Model set, those are applied to the agent
// after creation, providing per-session provider/model scoping.
//
// The agent's workspace root is set to the chat's worktree path if set,
// otherwise it falls back to the provided workspaceRoot parameter.
func (cs *chatSession) getOrCreateAgent(workspaceRoot string, eventBus *events.EventBus, clientID string) (*agent.Agent, error) {
	cs.mu.Lock()
	if cs.Agent != nil {
		// Use chat's worktree path if set, otherwise use provided workspaceRoot
		agentWorkspace := cs.WorktreePath
		if agentWorkspace == "" {
			agentWorkspace = workspaceRoot
		}
		agentInst := cs.Agent
		agentInst.SetWorkspaceRoot(agentWorkspace)
		agentInst.SetEventMetadata(map[string]interface{}{"client_id": clientID, "chat_id": cs.ID})
		agentInst.EnableStreaming(func(string) {})
		cs.mu.Unlock()
		return agentInst, nil
	}
	// Capture session-scoped provider/model before releasing the lock
	sessionProvider := cs.Provider
	sessionModel := cs.Model
	sessionWorktree := cs.WorktreePath
	cs.mu.Unlock()

	// Use chat's worktree path if set, otherwise use provided workspaceRoot
	agentWorkspace := sessionWorktree
	if agentWorkspace == "" {
		agentWorkspace = workspaceRoot
	}

	// Create agent outside the lock.
	snapshot := append([]byte(nil), cs.AgentState...)
	created, err := agent.NewAgentWithModel("")
	if err != nil {
		return nil, fmt.Errorf("create chat agent: %w", err)
	}

	if eventBus != nil {
		created.SetEventBus(eventBus)
	}
	created.SetWorkspaceRoot(agentWorkspace)
	created.SetEventMetadata(map[string]interface{}{"client_id": clientID, "chat_id": cs.ID})
	created.EnableStreaming(func(string) {})
	if len(snapshot) > 0 {
		if err := created.ImportState(snapshot); err != nil {
			log.Printf("chatSession.getOrCreateAgent: warning: failed to import state: %v", err)
		}
	}

	// Apply session-scoped provider/model if set on the session.
	// This provides per-session provider/model scoping without affecting
	// other sessions or the global config.
	if sessionProvider != "" {
		providerType, err := created.GetConfigManager().MapStringToClientType(sessionProvider)
		if err != nil {
			log.Printf("chatSession.getOrCreateAgent: warning: invalid session provider %q: %v", sessionProvider, err)
		} else if err := created.SetProvider(providerType); err != nil {
			log.Printf("chatSession.getOrCreateAgent: warning: failed to set session provider %q: %v", sessionProvider, err)
		}
	}
	if sessionModel != "" {
		if err := created.SetModel(sessionModel); err != nil {
			log.Printf("chatSession.getOrCreateAgent: warning: failed to set session model %q: %v", sessionModel, err)
		}
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.Agent == nil {
		// We won the race — store our agent.
		cs.Agent = created
		cs.CurrentSessionID = strings.TrimSpace(created.GetSessionID())
	} else {
		// Another goroutine beat us — discard ours and return theirs.
		created = cs.Agent
		// Use chat's worktree path if set, otherwise use provided workspaceRoot
		agentWorkspace := cs.WorktreePath
		if agentWorkspace == "" {
			agentWorkspace = workspaceRoot
		}
		created.SetWorkspaceRoot(agentWorkspace)
		created.SetEventMetadata(map[string]interface{}{"client_id": clientID, "chat_id": cs.ID})
		created.EnableStreaming(func(string) {})
	}
	return created, nil
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
	}
}

// newDefaultChatSession creates the "default" chat session.
func newDefaultChatSession() *chatSession {
	return newChatSession(defaultChatID, "Chat")
}

// --- webClientContext chat session methods ---

// getChatSession returns the chat session with the given ID, or nil if not found.
// The caller does NOT need to hold the server mutex; this is called while the
// server lock is already held by the surrounding methods.
func (cc *webClientContext) getChatSession(chatID string) *chatSession {
	if cc.ChatSessions == nil {
		return nil
	}
	return cc.ChatSessions[chatID]
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
}

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
	}
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
func (cc *webClientContext) hasActiveQueryForChat(chatID string) bool {
	if cc.ChatSessions == nil {
		return cc.ActiveQuery
	}
	if chatID == "" {
		chatID = cc.DefaultChatID
	}
	cs, ok := cc.ChatSessions[chatID]
	if !ok {
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

	// Decode agent state to extract messages for the frontend
	if len(cs.AgentState) > 0 {
		var state agent.AgentState
		if err := json.Unmarshal(cs.AgentState, &state); err == nil {
			summary["messages"] = state.Messages
			summary["total_tokens"] = state.TotalTokens
			summary["total_cost"] = state.TotalCost
			summary["session_id"] = state.SessionID
		}
	}

	summary["agent_state"] = string(cs.AgentState)
	return summary
}
