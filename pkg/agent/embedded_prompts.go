package agent

import (
	_ "embed"
	"fmt"
	"strings"
	"time"
)

//go:embed prompts/system_prompt.md
var systemPromptContent string

//go:embed prompts/planning_prompt.md
var planningPromptContent string

// //go:embed prompts/project_goals_prompt.md
// var projectGoalsPromptContent string

// GetEmbeddedSystemPrompt returns the embedded system prompt
func GetEmbeddedSystemPrompt() (string, error) {
	// Extract the prompt content from the markdown
	promptContent, err := extractSystemPrompt()
	if err != nil {
		return "", err
	}

	// Add current date and time for temporal context
	currentTime := time.Now()
	dateTimeString := fmt.Sprintf("\n\n## Current Date and Time\n\nCurrent date: %s\nCurrent time: %s\nCurrent timezone: %s\n\n---\n",
		currentTime.Format("2006-01-02"),
		currentTime.Format("15:04:05"),
		currentTime.Location().String())
	promptContent = promptContent + dateTimeString

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

	return promptContent, nil
}

// GetEmbeddedSystemPromptWithProvider returns the embedded system prompt with provider-specific enhancements
func GetEmbeddedSystemPromptWithProvider(provider string) (string, error) {
	promptContent, err := GetEmbeddedSystemPrompt()
	if err != nil {
		return "", err
	}

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
		return promptContent + zaiConstraints, nil
	}

	// No provider-specific enhancements for other providers
	return promptContent, nil
}

// extractSystemPrompt extracts the prompt content from the system_prompt markdown
func extractSystemPrompt() (string, error) {
	// The system_prompt.md has the prompt content in a code block
	// We need to extract from the first ``` marker to the last ``` marker
	// to include all content including examples with nested code blocks

	const promptStart = "```"

	// Find the first ``` marker
	startIdx := strings.Index(systemPromptContent, promptStart)
	if startIdx == -1 {
		return "", fmt.Errorf("critical error: system prompt start marker not found")
	}

	// Skip the opening ``` marker and any following newlines
	contentStart := startIdx + len(promptStart)
	for contentStart < len(systemPromptContent) && (systemPromptContent[contentStart] == '\n' || systemPromptContent[contentStart] == '\r') {
		contentStart++
	}

	// Find the LAST ``` marker (this handles nested code blocks)
	endIdx := strings.LastIndex(systemPromptContent, "```")
	if endIdx == -1 || endIdx <= startIdx {
		// If no closing marker, use everything after the start marker
		return strings.TrimSpace(systemPromptContent[contentStart:]), nil
	}

	// Extract everything between the first ``` and the last ```
	promptText := strings.TrimSpace(systemPromptContent[contentStart:endIdx])

	return promptText, nil
}

// GetEmbeddedPlanningPrompt returns the embedded planning prompt
func GetEmbeddedPlanningPrompt(createTodos bool) (string, error) {
	// Extract the prompt content from the markdown
	promptContent, err := extractPlanningPrompt()
	if err != nil {
		return "", err
	}

	// Add current date and time for temporal context
	currentTime := time.Now()
	dateTimeString := fmt.Sprintf("\n\n## Current Date and Time\n\nCurrent date: %s\nCurrent time: %s\nCurrent timezone: %s\n\n---\n",
		currentTime.Format("2006-01-02"),
		currentTime.Format("15:04:05"),
		currentTime.Location().String())

	// Add todo integration or not based on flag
	todoIntegration := `

# Todo Integration
`
	if createTodos {
		todoIntegration += `- When you identify clear tasks, use the add_todos tool to create them
- This creates a todo system that can be tracked during implementation
- Structure todos by phases or categories
- Include descriptions for complex todos
`
	} else {
		todoIntegration += `- Disabled (user is managing tasks separately)
`
	}

	return promptContent + dateTimeString + todoIntegration, nil
}

// extractPlanningPrompt extracts the prompt content from the planning_prompt markdown
func extractPlanningPrompt() (string, error) {
	// The planning_prompt.md has the prompt content in a code block
	// We'll extract everything between the ``` markers
	const promptStart = "You are an autonomous planning and execution assistant."

	startIdx := strings.Index(planningPromptContent, promptStart)
	if startIdx == -1 {
		return "", fmt.Errorf("critical error: planning prompt content not found")
	}

	endIdx := strings.Index(planningPromptContent[startIdx:], "```")
	if endIdx == -1 {
		// If no closing marker, use the whole content from start
		return strings.TrimSpace(planningPromptContent[startIdx:]), nil
	}

	return strings.TrimSpace(planningPromptContent[startIdx : startIdx+endIdx]), nil
}
