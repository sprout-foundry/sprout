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

// GetEmbeddedSystemPrompt returns the embedded system prompt
func GetEmbeddedSystemPrompt() string {
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

// GetEmbeddedSystemPromptWithProvider returns the embedded system prompt with provider-specific enhancements
func GetEmbeddedSystemPromptWithProvider(provider string) string {
	promptContent := GetEmbeddedSystemPrompt()

	// Add provider-specific enhancements
	switch provider {
	case "zai":
		// GLM-4.6 specific constraints for excessive verbosity
		promptContent = promptContent + `

## GLM-4.6 Critical Constraints

### Tool Usage Limits
- NEVER create more than 3-5 todos for any task
- NEVER repeat the same todo operation (adding/updating same task multiple times)
- NEVER use tools excessively - analyze first, then act decisively
- NEVER read more than 5 files before making a decision

### Response Discipline  
- Complete analysis in 1-2 tool calls, not dozens
- Make decisive choices without over-analysis
- If you find yourself making many tool calls, STOP and simplify your approach
- Prefer simple, direct solutions over complex analysis

### Anti-Verbose Rules
- DO NOT create extensive todo lists for simple tasks
- DO NOT over-explain your reasoning process
- DO NOT read files that aren't directly relevant to the core task
- DO NOT make repetitive tool calls`
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
