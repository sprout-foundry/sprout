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

	// Add discovered context files (AGENTS.md, Claude.md, etc.)
	contextFiles, err := LoadContextFiles()
	if err == nil && contextFiles != "" {
		promptContent = promptContent + contextFiles
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

	// Add provider-specific constraints for ZAI (GLM-4.6)
	if strings.ToLower(provider) == "zai" {
		zaiConstraints := `

## GLM-4.6 Critical Constraints

### Cognitive Load Management
- LIMIT concurrent cognitive tasks to maximum 3-5 todos
- EXECUTE tasks immediately rather than analyzing extensively  
- MINIMIZE multitasking - focus on current task completion
- PRIORIZE action over deliberation when ambiguity exists

### Response Style (GLM-4.6 Specific)
- Be extremely concise in responses
- Focus on technical execution over explanation
- Avoid verbose analysis or multi-step reasoning
- Execute tool operations decisively`
		return promptContent + zaiConstraints
	}

	// No provider-specific enhancements for other providers
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

	endIdx := strings.Index(systemPromptContent[startIdx:], "```")
	if endIdx == -1 {
		// If no closing marker, use the whole content from start
		return strings.TrimSpace(systemPromptContent[startIdx:])
	}

	return strings.TrimSpace(systemPromptContent[startIdx : startIdx+endIdx])
}