package agent

import (
	"crypto/md5"
	"fmt"
	"time"
)

// generateSessionID returns a unique session identifier based on the current time.
func generateSessionID() string {
	return fmt.Sprintf("agent-%d", time.Now().UnixNano())
}

// generateRevisionID hashes sessionID + instructions + a timestamp to
// produce a stable per-session identifier for history storage.
func generateRevisionID(sessionID, instructions string) string {
	hash := md5.Sum([]byte(sessionID + "-" + instructions + "-" + fmt.Sprint(time.Now().UnixNano())))
	return fmt.Sprintf("agent-%x", hash)[:16]
}

// determineWriteOperation classifies a write as "create", "write", or "overwrite".
func determineWriteOperation(originalContent, newContent string) string {
	if originalContent == "" {
		return "create"
	}
	if originalContent != newContent {
		return "write"
	}
	return "overwrite"
}

// getAgentModel returns the model identifier from the agent's runtime config.
func (ct *ChangeTracker) getAgentModel() string {
	if ct.agent != nil {
		return ct.agent.GetModel()
	}
	return "unknown"
}

// limitString truncates a string to the specified length with ellipsis.
func limitString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
