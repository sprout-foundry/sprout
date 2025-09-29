package tools

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"sync"
	"time"
)

// SessionTracker tracks tool calls per session to detect duplicates
type SessionTracker struct {
	mu           sync.RWMutex
	sessions     map[string]*SessionData
	maxSessions  int
	cleanupAfter time.Duration
}

// SessionData contains information about a session
type SessionData struct {
	SessionID    string
	ToolCalls    map[string]*ToolCallInfo
	CreatedAt    time.Time
	LastActivity time.Time
}

// ToolCallInfo contains information about a specific tool call
type ToolCallInfo struct {
	ToolName  string
	Arguments map[string]interface{}
	CallCount int
	FirstCall time.Time
	LastCall  time.Time
	Responses []string // Keep track of responses for cache purposes
}

// NewSessionTracker creates a new session tracker
func NewSessionTracker(maxSessions int, cleanupAfter time.Duration) *SessionTracker {
	tracker := &SessionTracker{
		sessions:     make(map[string]*SessionData),
		maxSessions:  maxSessions,
		cleanupAfter: cleanupAfter,
	}

	// Start cleanup goroutine
	go tracker.cleanupWorker()

	return tracker
}

// GenerateSessionID generates a unique session ID
func GenerateSessionID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// StartSession starts a new session and returns the session ID
func (st *SessionTracker) StartSession() string {
	st.mu.Lock()
	defer st.mu.Unlock()

	sessionID := GenerateSessionID()

	// Clean up old sessions if we're at the limit
	if len(st.sessions) >= st.maxSessions {
		st.cleanupOldSessions()
	}

	st.sessions[sessionID] = &SessionData{
		SessionID:    sessionID,
		ToolCalls:    make(map[string]*ToolCallInfo),
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
	}

	return sessionID
}

// EndSession ends a session and removes its data
func (st *SessionTracker) EndSession(sessionID string) {
	st.mu.Lock()
	defer st.mu.Unlock()

	delete(st.sessions, sessionID)
}

// RecordToolCall records a tool call in a session
func (st *SessionTracker) RecordToolCall(sessionID, toolName string, arguments map[string]interface{}, response string) {
	st.mu.Lock()
	defer st.mu.Unlock()

	session, exists := st.sessions[sessionID]
	if !exists {
		// Create session if it doesn't exist
		session = &SessionData{
			SessionID:    sessionID,
			ToolCalls:    make(map[string]*ToolCallInfo),
			CreatedAt:    time.Now(),
			LastActivity: time.Now(),
		}
		st.sessions[sessionID] = session
	}

	// Create a key for this specific tool call
	callKey := st.createToolCallKey(toolName, arguments)

	callInfo, exists := session.ToolCalls[callKey]
	if !exists {
		callInfo = &ToolCallInfo{
			ToolName:  toolName,
			Arguments: arguments,
			CallCount: 0,
			FirstCall: time.Now(),
		}
		session.ToolCalls[callKey] = callInfo
	}

	// Update call info
	callInfo.CallCount++
	callInfo.LastCall = time.Now()
	session.LastActivity = time.Now()

	// Store response for potential caching (keep only the most recent response)
	if response != "" {
		callInfo.Responses = []string{response}
	}
}

// IsDuplicateRequest checks if a tool call is a duplicate in the same session
func (st *SessionTracker) IsDuplicateRequest(sessionID, toolName string, arguments map[string]interface{}) (bool, *ToolCallInfo) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	session, exists := st.sessions[sessionID]
	if !exists {
		return false, nil
	}

	callKey := st.createToolCallKey(toolName, arguments)
	callInfo, exists := session.ToolCalls[callKey]

	if exists && callInfo.CallCount > 0 {
		return true, callInfo
	}

	return false, nil
}

// GetSessionStats returns statistics about a session
func (st *SessionTracker) GetSessionStats(sessionID string) map[string]interface{} {
	st.mu.RLock()
	defer st.mu.RUnlock()

	session, exists := st.sessions[sessionID]
	if !exists {
		return nil
	}

	stats := map[string]interface{}{
		"session_id":       session.SessionID,
		"created_at":       session.CreatedAt,
		"last_activity":    session.LastActivity,
		"total_tool_calls": len(session.ToolCalls),
		"tool_breakdown":   make(map[string]int),
	}

	toolBreakdown := stats["tool_breakdown"].(map[string]int)
	for _, callInfo := range session.ToolCalls {
		toolBreakdown[callInfo.ToolName] = callInfo.CallCount
	}

	return stats
}

// createToolCallKey creates a unique key for a tool call based on name and arguments
func (st *SessionTracker) createToolCallKey(toolName string, arguments map[string]interface{}) string {
	// For read_file, we want to deduplicate by file_path specifically
	if toolName == "read_file" {
		filePath := st.extractStringArgument(arguments, "file_path", "target_file", "path", "filename")
		startLine, hasStart := st.extractLineArgument(arguments, "start_line", "line_start", "start")
		endLine, hasEnd := st.extractLineArgument(arguments, "end_line", "line_end", "end")

		// Normalize missing end line when start is provided to avoid treating
		// different representations of full-file reads as distinct keys.
		if hasStart && !hasEnd {
			endLine = 0
		}
		if hasEnd && !hasStart {
			startLine = 0
		}

		if hasStart || hasEnd {
			return fmt.Sprintf("%s:%s:%d:%d", toolName, filePath, startLine, endLine)
		}
		return fmt.Sprintf("%s:%s:full", toolName, filePath)
	}

	// For other tools, create a key based on all arguments
	argsStr := ""
	for key, value := range arguments {
		argsStr += fmt.Sprintf("%s=%v,", key, value)
	}

	return fmt.Sprintf("%s:%s", toolName, argsStr)
}

func (st *SessionTracker) extractStringArgument(arguments map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := arguments[key]; ok {
			switch v := value.(type) {
			case string:
				return v
			}
		}
	}
	return ""
}

func (st *SessionTracker) extractLineArgument(arguments map[string]interface{}, keys ...string) (int, bool) {
	for _, key := range keys {
		if value, ok := arguments[key]; ok {
			switch v := value.(type) {
			case int:
				return v, true
			case int32:
				return int(v), true
			case int64:
				return int(v), true
			case float64:
				return int(v), true
			case float32:
				return int(v), true
			case string:
				if v == "" {
					continue
				}
				if parsed, err := strconv.Atoi(v); err == nil {
					return parsed, true
				}
			}
		}
	}
	return 0, false
}

// cleanupWorker runs in the background to clean up old sessions
func (st *SessionTracker) cleanupWorker() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		st.cleanupOldSessions()
	}
}

// cleanupOldSessions removes sessions that haven't been active for the specified duration
func (st *SessionTracker) cleanupOldSessions() {
	st.mu.Lock()
	defer st.mu.Unlock()

	cutoff := time.Now().Add(-st.cleanupAfter)
	toDelete := make([]string, 0)

	for sessionID, session := range st.sessions {
		if session.LastActivity.Before(cutoff) {
			toDelete = append(toDelete, sessionID)
		}
	}

	for _, sessionID := range toDelete {
		delete(st.sessions, sessionID)
	}
}

// GetRecentDuplicateRequests returns information about recent duplicate requests in a session
func (st *SessionTracker) GetRecentDuplicateRequests(sessionID string, toolName string, limit int) []*ToolCallInfo {
	st.mu.RLock()
	defer st.mu.RUnlock()

	session, exists := st.sessions[sessionID]
	if !exists {
		return nil
	}

	var duplicates []*ToolCallInfo
	for _, callInfo := range session.ToolCalls {
		if callInfo.ToolName == toolName && callInfo.CallCount > 1 {
			duplicates = append(duplicates, callInfo)
			if limit > 0 && len(duplicates) >= limit {
				break
			}
		}
	}

	return duplicates
}

// Global session tracker instance
var globalSessionTracker *SessionTracker

// GetGlobalSessionTracker returns the global session tracker
func GetGlobalSessionTracker() *SessionTracker {
	if globalSessionTracker == nil {
		globalSessionTracker = NewSessionTracker(1000, 30*time.Minute)
	}
	return globalSessionTracker
}
