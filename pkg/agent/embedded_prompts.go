package agent

import (
	_ "embed"
	"fmt"
	"os"
	"strings"

	tools "github.com/alantheprice/ledit/pkg/agent_tools"
)

//go:embed prompts/system_prompt.md
var systemPromptContent string

// //go:embed prompts/project_goals_prompt.md
// var projectGoalsPromptContent string

//go:embed prompts/project_insights_system.md
var projectInsightsPromptContent string

//go:embed prompts/code_review_prompt.md
var codeReviewPromptContent string

// getEmbeddedSystemPrompt returns the embedded system prompt
func getEmbeddedSystemPrompt() string {
	// Extract the prompt content from the markdown
	promptContent := extractSystemPrompt()

	// Add project context if available
	projectContext := getProjectContext()
	if projectContext != "" {
		promptContent = promptContent + "\n\n" + projectContext
	}

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

// getProjectContext loads project context from various possible locations
func getProjectContext() string {
	// TODO: This is not great and should use a better pattern to aggregate some of these files potentially, but also generate this if it doesn't exist
	// Check for project context files in order of priority
	contextFiles := []string{
		// ".cursor/markdown/project.md",
		// ".cursor/markdown/context.md",
		// ".claude/project.md",
		// ".claude/context.md",
		// ".project_context.md",
		// "PROJECT_CONTEXT.md",
	}

	for _, filePath := range contextFiles {
		content, err := tools.ReadFile(filePath)
		if err == nil && strings.TrimSpace(content) != "" {
			return fmt.Sprintf("PROJECT CONTEXT:\n%s", content)
		}
	}

	return ""
}
