package ui

import (
	"fmt"
	"strings"
	"time"
)

// SessionItem represents a conversation session for dropdown selection
type SessionItem struct {
	SessionID   string
	Timestamp   time.Time
}

// Display returns the string to show in the dropdown
func (s *SessionItem) Display() string {
	// Format: session_id (time ago)
	timeAgo := time.Since(s.Timestamp)
	
	// Format time ago in a human-readable way
	var timeStr string
	if timeAgo < time.Minute {
		timeStr = "just now"
	} else if timeAgo < time.Hour {
		minutes := int(timeAgo.Minutes())
		timeStr = fmt.Sprintf("%dm ago", minutes)
	} else if timeAgo < 24*time.Hour {
		hours := int(timeAgo.Hours())
		timeStr = fmt.Sprintf("%dh ago", hours)
	} else {
		days := int(timeAgo.Hours() / 24)
		timeStr = fmt.Sprintf("%dd ago", days)
	}
	
	// Build the display string
	return fmt.Sprintf("%s (%s)", s.SessionID, timeStr)
}

// SearchText returns the text used for searching
func (s *SessionItem) SearchText() string {
	return strings.ToLower(s.SessionID)
}

// Value returns the actual value when selected
func (s *SessionItem) Value() interface{} {
	return s.SessionID
}

// DisplayCompact returns a compact display for the session
func (s *SessionItem) DisplayCompact(maxWidth int) string {
	baseDisplay := s.Display()
	if len(baseDisplay) <= maxWidth {
		return baseDisplay
	}
	
	// Truncate session ID if needed
	sessionPart := s.SessionID
	if len(sessionPart) > maxWidth/2 {
		sessionPart = sessionPart[:maxWidth/2-3] + "..."
	}
	
	// Build compact display
	timeAgo := time.Since(s.Timestamp)
	var timeStr string
	if timeAgo < time.Minute {
		timeStr = "now"
	} else if timeAgo < time.Hour {
		minutes := int(timeAgo.Minutes())
		timeStr = fmt.Sprintf("%dm", minutes)
	} else if timeAgo < 24*time.Hour {
		hours := int(timeAgo.Hours())
		timeStr = fmt.Sprintf("%dh", hours)
	} else {
		days := int(timeAgo.Hours() / 24)
		timeStr = fmt.Sprintf("%dd", days)
	}
	
	compact := fmt.Sprintf("%s (%s)", sessionPart, timeStr)
	
	if len(compact) > maxWidth {
		compact = compact[:maxWidth-3] + "..."
	}
	
	return compact
}