package ui

import (
	"fmt"
	"strings"
)

// MessagePriority defines the importance level of a message for UI display
type MessagePriority int

const (
	// PriorityHigh - Always show: File edits, code reviews, direct answers, errors
	PriorityHigh MessagePriority = iota
	// PriorityMedium - Show conditionally: Important progress, command results
	PriorityMedium
	// PriorityLow - Hide from UI, keep in full logs: Internal processes, file loading
	PriorityLow
	// PriorityVerbose - Only in debug mode: Detailed internal operations
	PriorityVerbose
)

// MessageFilter determines what messages should be shown in the UI vs full logs
type MessageFilter struct {
	UIEnabled   bool
	ShowMedium  bool // Whether to show medium priority messages in UI
	ShowLow     bool // Whether to show low priority messages in UI
	VerboseMode bool // Whether to show verbose messages
}

// DefaultUIFilter returns the default filtering for UI mode
func DefaultUIFilter() MessageFilter {
	return MessageFilter{
		UIEnabled:   true,
		ShowMedium:  true,  // Show important progress
		ShowLow:     false, // Hide verbose internal details
		VerboseMode: false,
	}
}

// ConsoleFilter returns filtering for console mode (show more detail)
func ConsoleFilter() MessageFilter {
	return MessageFilter{
		UIEnabled:   false,
		ShowMedium:  true,
		ShowLow:     true, // Show more detail in console
		VerboseMode: false,
	}
}

// ShouldShow determines if a message should be displayed based on priority and filter settings
func (f MessageFilter) ShouldShow(priority MessagePriority) bool {
	switch priority {
	case PriorityHigh:
		return true // Always show high priority
	case PriorityMedium:
		return f.ShowMedium
	case PriorityLow:
		return f.ShowLow
	case PriorityVerbose:
		return f.VerboseMode
	default:
		return true // Default to showing unknown priorities
	}
}

// ClassifyMessage automatically determines the priority of a message based on content
func ClassifyMessage(message string) MessagePriority {
	msg := strings.ToLower(strings.TrimSpace(message))

	// High Priority: Results, edits, reviews, errors, answers
	highPriorityPatterns := []string{
		"✅ changes applied",
		"❌ error",
		"⚠️ warning",
		"🔧 edited",
		"📝 created",
		"🗑️ deleted",
		"📋 code review",
		"✅ review approved",
		"❌ review rejected",
		"💡 answer:",
		"🤖 response:",
		"✅ completed successfully",
		"❌ failed:",
		"🚀 result:",
		"📊 summary:",
		"diff applied",
		"file updated",
	}

	// Medium Priority: Progress, commands, important status
	mediumPriorityPatterns := []string{
		"🎯 executing:",
		"🚀 starting",
		"⚙️ agent analyzing",
		"📊 tracked",
		"💰 budget status",
		"🔍 validating",
		"▶️ running",
		"✅ validation passed",
		"🧪 running validation",
	}

	// Low Priority: File loading, context selection, internal processes
	lowPriorityPatterns := []string{
		"loading file",
		"selected files",
		"workspace context",
		"building context",
		"📄 loaded",
		"context includes",
		"analyzing workspace",
		"selected for context",
		"file content:",
		"processing file",
		"reading file",
		"context built",
		"workspace analysis",
		"file selection",
	}

	// Verbose Priority: Debug details, token usage details, internal operations
	verbosePriorityPatterns := []string{
		"debug:",
		"token usage",
		"model response",
		"api call",
		"estimated tokens",
		"prompt tokens",
		"completion tokens",
		"retry attempt",
		"fallback to",
		"internal:",
		"trace:",
	}

	// Check patterns in priority order (most specific first)
	for _, pattern := range highPriorityPatterns {
		if strings.Contains(msg, pattern) {
			return PriorityHigh
		}
	}

	for _, pattern := range mediumPriorityPatterns {
		if strings.Contains(msg, pattern) {
			return PriorityMedium
		}
	}

	for _, pattern := range lowPriorityPatterns {
		if strings.Contains(msg, pattern) {
			return PriorityLow
		}
	}

	for _, pattern := range verbosePriorityPatterns {
		if strings.Contains(msg, pattern) {
			return PriorityVerbose
		}
	}

	// Default to medium priority for unclassified messages
	return PriorityMedium
}

// Global filter state
var currentFilter = DefaultUIFilter()

// SetMessageFilter updates the global message filter
func SetMessageFilter(filter MessageFilter) {
	currentFilter = filter
}

// GetMessageFilter returns the current global message filter
func GetMessageFilter() MessageFilter {
	return currentFilter
}

// LogWithPriority logs a message with explicit priority
func LogWithPriority(message string, priority MessagePriority) {
	if currentFilter.ShouldShow(priority) {
		Log(message)
	}
	// TODO: Always log to full log file regardless of UI filtering
}

// LogfWithPriority logs a formatted message with explicit priority
func LogfWithPriority(format string, priority MessagePriority, args ...any) {
	if currentFilter.ShouldShow(priority) {
		Logf(format, args...)
	}
	// TODO: Always log to full log file regardless of UI filtering
}

// SmartLog automatically classifies and logs a message
func SmartLog(message string) {
	priority := ClassifyMessage(message)
	LogWithPriority(message, priority)
}

// SmartLogf automatically classifies and logs a formatted message
func SmartLogf(format string, args ...any) {
	message := strings.TrimSpace(fmt.Sprintf(format, args...))
	SmartLog(message)
}

// ShowProgressWithDetails shows a brief main UI message and detailed logs
// This is perfect for operations like workspace loading where we want:
// - Main UI: "🔄 Preparing workspace..."
// - Logs: "Building workspace context (files, syntactic overviews)..."
func ShowProgressWithDetails(mainMessage, detailMessage string) {
	// Always show brief progress in main UI
	Out().Print(mainMessage + "\n")

	// Show details in logs (filtered by priority)
	SmartLog(detailMessage)
}

// ShowProgressf is a formatted version of ShowProgressWithDetails
func ShowProgressf(mainFormat string, detailFormat string, args ...any) {
	mainMsg := fmt.Sprintf(mainFormat, args...)
	detailMsg := fmt.Sprintf(detailFormat, args...)
	ShowProgressWithDetails(mainMsg, detailMsg)
}
