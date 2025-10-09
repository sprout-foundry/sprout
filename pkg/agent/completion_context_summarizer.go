package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// CompletionContextSummarizer handles aggressive summarization at completion time
// to prevent context contamination in follow-up questions
type CompletionContextSummarizer struct {
	preserveEssentialOutcomes bool
	compactToolExecutionLogs  bool
	maintainUserIntentChain bool
	debug                    bool
}

// NewCompletionContextSummarizer creates a new summarizer with default settings
func NewCompletionContextSummarizer(debug bool) *CompletionContextSummarizer {
	return &CompletionContextSummarizer{
		preserveEssentialOutcomes: true,
		compactToolExecutionLogs:  true,
		maintainUserIntentChain: true,
		debug:                   debug,
	}
}

// CreateCompletionSummary generates a compact summary of completed work
// that preserves continuity while eliminating execution baggage
func (ccs *CompletionContextSummarizer) CreateCompletionSummary(messages []api.Message) string {
	if ccs.debug {
		fmt.Printf("🔍 Creating completion summary from %d messages\n", len(messages))
	}

	// Extract key information from the conversation
	originalRequest := ccs.extractOriginalRequest(messages)
	keyAccomplishments := ccs.extractKeyAccomplishments(messages)
	filesModified := ccs.extractFilesModified(messages)

	// Build comprehensive but compact summary
	var summary strings.Builder
	summary.WriteString("## Task Completion Summary\n")
	summary.WriteString("**Original Request**: " + originalRequest + "\n\n")
	
	if keyAccomplishments != "" {
		summary.WriteString("**Key Accomplishments**:\n" + keyAccomplishments + "\n\n")
	}
	
	if filesModified != "" {
		summary.WriteString("**Files Modified**: " + filesModified + "\n\n")
	}
	
	summary.WriteString("**Status**: ✅ Task completed successfully\n")
	summary.WriteString("**Next Steps**: Future instructions should be treated as new tasks.\n")

	// Add specific completion markers
	summary.WriteString("\n[[COMPLETION_CONTEXT_SUMMARY]]")

	return summary.String()
}

// extractOriginalRequest finds the original user request
func (ccs *CompletionContextSummarizer) extractOriginalRequest(messages []api.Message) string {
	for _, msg := range messages {
		if msg.Role == "user" && !strings.Contains(msg.Content, "[[TASK_COMPLETE]]") {
			// Extract first meaningful user message (not completion signal)
			content := strings.TrimSpace(msg.Content)
			if content != "" && !strings.HasPrefix(content, "[[TASK_COMPLETE]]") {
				// Return first 200 chars of original request
				if len(content) > 200 {
					return content[:200] + "..."
				}
				return content
			}
		}
	}
	return "Work completed"
}

// extractKeyAccomplishments identifies what was actually accomplished
func (ccs *CompletionContextSummarizer) extractKeyAccomplishments(messages []api.Message) string {
	var accomplishments []string
	
	// Look for completion messages that describe what was done
	for _, msg := range messages {
		if msg.Role == "assistant" && strings.Contains(msg.Content, "[[TASK_COMPLETE]]") {
			// Extract meaningful content before completion signal
			content := strings.Split(msg.Content, "[[TASK_COMPLETE]]")[0]
			content = strings.TrimSpace(content)
			if content != "" {
				accomplishments = append(accomplishments, "• "+content)
			}
		}
	}
	
	if len(accomplishments) > 0 {
		return strings.Join(accomplishments, "\n")
	}
	return ""
}

// extractFilesModified identifies files that were touched
func (ccs *CompletionContextSummarizer) extractFilesModified(messages []api.Message) string {
	var files []string
	fileSet := make(map[string]bool)
	
	// Look for file operations in tool calls
	for _, msg := range messages {
		if msg.Role == "assistant" && msg.ToolCalls != nil {
			for _, toolCall := range msg.ToolCalls {
				if toolCall.Function.Name == "edit_file" || toolCall.Function.Name == "write_file" {
					// Extract file path from arguments
					var args map[string]interface{}
					if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err == nil {
						if filePath, ok := args["file_path"].(string); ok {
							if !fileSet[filePath] {
								fileSet[filePath] = true
								files = append(files, filePath)
							}
						}
					}
				}
			}
		}
	}
	
	if len(files) > 0 {
		if len(files) <= 3 {
			return strings.Join(files, ", ")
		}
		return fmt.Sprintf("%d files", len(files))
	}
	return ""
}

// ApplyCompletionSummarization replaces detailed execution logs with compact summaries
// to prevent context contamination in follow-up questions
func (ccs *CompletionContextSummarizer) ApplyCompletionSummarization(messages []api.Message) []api.Message {
	if len(messages) <= 3 {
		return messages // Keep short conversations intact
	}
	
	// Only apply summarization if we have completion signals
	if !ccs.ShouldApplySummarization(messages) {
		return messages
	}
	
	// Create a compact summary of the conversation
	summary := ccs.CreateCompletionSummary(messages)
	
	// Replace detailed conversation with summary
	// Keep essential context: system prompt, original request, and summary
	var summarizedMessages []api.Message
	
	// Find and keep the system prompt
	for _, msg := range messages {
		if msg.Role == "system" {
			summarizedMessages = append(summarizedMessages, msg)
			break
		}
	}
	
	// Keep the original user request if it exists
	originalRequestFound := false
	for _, msg := range messages {
		if msg.Role == "user" && !originalRequestFound {
			if !strings.Contains(msg.Content, "[[TASK_COMPLETE]]") {
				summarizedMessages = append(summarizedMessages, msg)
				originalRequestFound = true
			}
		}
	}
	
	// Add the completion summary
	summarizedMessages = append(summarizedMessages, api.Message{
		Role:    "assistant",
		Content: summary,
	})
	
	return summarizedMessages
}

// ShouldApplySummarization determines if summarization should be applied
func (ccs *CompletionContextSummarizer) ShouldApplySummarization(messages []api.Message) bool {
	// Apply summarization when we have completion signals
	for _, msg := range messages {
		if strings.Contains(msg.Content, "[[TASK_COMPLETE]]") {
			return true
		}
	}

	return false
}