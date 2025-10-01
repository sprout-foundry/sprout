package agent

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
)

//go:embed prompts/system_prompt.md
var systemPromptContent string

// //go:embed prompts/project_goals_prompt.md
// var projectGoalsPromptContent string

// getEmbeddedSystemPrompt returns the embedded system prompt
func getEmbeddedSystemPrompt() string {
	// Extract the prompt content from the markdown
	promptContent := extractSystemPrompt()

	// Add project validation context
	validationContext, err := generateProjectValidationContext()
	if err == nil && validationContext != "" {
		promptContent = promptContent + "\n\n" + validationContext
	}

	// Add MCP server summary if available
	// TODO: Implement mcpServerSummary when needed
	mcpServerSummary := ""
	if mcpServerSummary != "" {
		promptContent = promptContent + "\n" + mcpServerSummary
	}

	return promptContent
}

// extractSystemPrompt extracts the prompt content from the system_prompt markdown
func extractSystemPrompt() string {
	// The system_prompt.md has the prompt content in a code block
	// We'll extract everything between the ``` markers
	const promptStart = "# Ledit"

	startIdx := strings.Index(systemPromptContent, promptStart)
	if startIdx == -1 {
		// If not found, throw an error and exit this is a critical failure
		fmt.Fprintln(os.Stderr, "Critical error: system prompt content not found")
		os.Exit(1)
	}

	// Find the end of the code block (closing ```)
	endIdx := strings.Index(systemPromptContent[startIdx:], "```")
	if endIdx == -1 {
		// If no closing marker, use the whole content from start
		return strings.TrimSpace(systemPromptContent[startIdx:])
	}

	return strings.TrimSpace(systemPromptContent[startIdx : startIdx+endIdx])
}

// Find the end of the code block (closing ```)
