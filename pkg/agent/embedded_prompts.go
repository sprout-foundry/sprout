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

	// Add provider-specific enhancements
	switch provider {
	case "zai":
		// GLM-4.6 specific constraints based on research patterns for optimal coding performance
		promptContent = promptContent + `

## GLM-4.6 Critical Constraints (Optimized for Coding Tasks)

### Cognitive Load Management
- LIMIT concurrent cognitive tasks to maximum 3-5 todos
- AVOID decision paralysis - make reasonable assumptions and proceed
- PATTERN MATCH over exhaustive analysis when similar solutions exist
- THINK step-by-step but ACT decisively

### Tool Usage Optimization
- BATCH related operations (read multiple files together)
- CHAIN tool calls logically (analyze → plan → implement → verify)
- MINIMIZE context switching between different types of operations
- USE tools with purpose, not exploratory wandering

### Code-Specific Reasoning
- FOCUS on the specific programming language and framework patterns
- LEVERAGE existing codebase conventions and idioms
- PREFER refactoring over rewriting when possible
- CONSIDER testability and maintainability in all changes

### Response Efficiency Patterns
- STRUCTURE responses: action → result → next step (if needed)
- MINIMIZE meta-commentary about your thinking process
- PROVIDE concrete evidence of success (test output, build results)
- RECOGNIZE completion criteria explicitly

### Error Recovery Protocol
- SYSTEMATIC error analysis: symptom → root cause → solution
- LEVERAGE error messages as diagnostic tools
- ISOLATE variables when debugging (test one change at a time)
- DOCUMENT fixes implicitly through working code

### Performance Optimization
- AVOID unnecessary file I/O operations
- CACHE relevant context during extended sessions
- USE appropriate search strategies (grep vs file reading)
- BALANCE thoroughness with efficiency

### COMPLETION CRITERIA
- EVIDENCE-based completion: working code + passing tests
- NO open loops or unresolved dependencies
- READY for next phase or commit`
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

	endIdx := strings.Index(systemPromptContent[startIdx:], "```")
	if endIdx == -1 {
		// If no closing marker, use the whole content from start
		return strings.TrimSpace(systemPromptContent[startIdx:])
	}

	return strings.TrimSpace(systemPromptContent[startIdx : startIdx+endIdx])
}
