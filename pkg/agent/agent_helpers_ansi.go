package agent

import (
	"regexp"
)

// ---------------------------------------------------------------------------
// Standalone helper functions (extracted from deleted ConversationHandler)
// ---------------------------------------------------------------------------

// ansiEscapeRegex matches standard ANSI escape sequences.
var ansiEscapeRegex = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// ansiCSIRegex matches common CSI (Control Sequence Introducer) sequences.
var ansiCSIRegex = regexp.MustCompile(`\x1b\[[0-9;]*[mGKHJABCD]`)

// ansiCharsetRegex matches character set designation sequences.
var ansiCharsetRegex = regexp.MustCompile(`\x1b\([0-9;]*[AB]`)

// sanitizeContent removes ANSI escape sequences, think tags, and other problematic characters from content.
func sanitizeContent(content string) string {
	// Remove think tags (some models output <think...</think_>)
	thinkRegex := regexp.MustCompile(`<think.*?</think_>`)
	cleaned := thinkRegex.ReplaceAllString(content, "")

	// Remove ANSI escape sequences
	cleaned = ansiCSIRegex.ReplaceAllString(cleaned, "")
	cleaned = ansiCharsetRegex.ReplaceAllString(cleaned, "")
	cleaned = ansiEscapeRegex.ReplaceAllString(cleaned, "")

	return cleaned
}

// sanitizeStreamingContent sanitizes streaming output by removing ANSI escape sequences.
func sanitizeStreamingContent(content string) string {
	cleaned := ansiCSIRegex.ReplaceAllString(content, "")
	cleaned = ansiCharsetRegex.ReplaceAllString(cleaned, "")
	cleaned = ansiEscapeRegex.ReplaceAllString(cleaned, "")
	return cleaned
}
