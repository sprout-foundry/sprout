package ui

import (
	"fmt"
	"strings"
	"time"
)

// SessionItem represents a conversation session for dropdown selection
type SessionItem struct {
	SessionID string
	Timestamp time.Time
	Preview   string // First 50 characters of the first user message
}

// Display returns the string to show in the dropdown
func (s *SessionItem) Display() string {
	// Format: time ago - preview (no session ID for cleaner display)
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

	// Build the display string with preview (no session ID)
	if s.Preview != "" {
		return fmt.Sprintf("%s - %s", timeStr, s.Preview)
	}
	return timeStr
}

// SearchText returns the text used for searching
func (s *SessionItem) SearchText() string {
	// Only search by preview content since session ID is hidden from user
	if s.Preview != "" {
		return strings.ToLower(s.Preview)
	}
	return ""
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

	// Build compact display (no session ID)
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

	// Calculate space for preview
	maxPreviewLen := maxWidth - len(timeStr) - 3 // 3 for " - "
	if maxPreviewLen <= 0 {
		return timeStr
	}

	// Truncate preview if needed
	previewPart := ""
	if s.Preview != "" {
		previewPart = s.Preview
		if len(previewPart) > maxPreviewLen {
			previewPart = previewPart[:maxPreviewLen-3] + "..."
		}
	}

	var compact string
	if previewPart != "" {
		compact = fmt.Sprintf("%s - %s", timeStr, previewPart)
	} else {
		compact = timeStr
	}

	if len(compact) > maxWidth {
		compact = compact[:maxWidth-3] + "..."
	}

	return compact
}
