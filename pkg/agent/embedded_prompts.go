package agent

import (
	"embed"
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

//go:embed prompts/system_prompt.md
var systemPromptContent string

//go:embed prompts/planning_prompt.md
var planningPromptContent string

// SP-066 Phase 2 — dedicated rollup prompt template used by the background
// rollup worker. Separate from the per-turn summarizer because its inputs
// are already-summarized data, not raw conversation messages.
//
//go:embed prompts/rollup_prompt.md
var rollupPromptContent string

// GetEmbeddedRollupPrompt returns the rollup summarizer prompt embedded
// from prompts/rollup_prompt.md. The body is used verbatim as the system
// prompt for the rollup worker's LLM call.
func GetEmbeddedRollupPrompt() string {
	return rollupPromptContent
}

//go:embed prompts/*.md prompts/subagent_prompts/*.md
var embeddedPromptFiles embed.FS

//go:embed prompts/persona_appends/orchestrator_git_policy.md
var orchestratorGitPolicyAppend string

// //go:embed prompts/project_goals_prompt.md
// var projectGoalsPromptContent string

// GetEmbeddedSystemPrompt returns the embedded system prompt
func GetEmbeddedSystemPrompt() (string, error) {
	// Extract the prompt content from the markdown
	promptContent, err := extractSystemPrompt()
	if err != nil {
		return "", agenterrors.NewPermanentError("failed to extract system prompt", err)
	}

	// Add discovered context files (AGENTS.md, Claude.md, etc.)
	// Semi-static content — placed before the volatile timestamp so it does not
	// invalidate the prompt-prefix cache for subsequent static content.
	contextFiles, err := LoadContextFiles()
	if err == nil && contextFiles != "" {
		promptContent = promptContent + contextFiles
	}

	// Add memories (user preferences and learned patterns)
	// Semi-static content — also placed before the volatile timestamp.
	memories := LoadMemoriesForPrompt()
	if memories != "" {
		promptContent = promptContent + memories
	}

	// Add current date and time for temporal context LAST. This is the only
	// volatile (per-call) content; keeping it at the end preserves cache
	// eligibility for the large static prefix (system prompt + context files
	// + memories). Providers like Anthropic cache prompt prefixes, so a
	// second-resolution timestamp anywhere but the tail would force a full
	// re-process of everything after it on every request.
	currentTime := time.Now()
	dateTimeString := fmt.Sprintf("\n\n## Current Date and Time\n\nCurrent date: %s\nCurrent time: %s\nCurrent timezone: %s\n\n---\n",
		currentTime.Format("2006-01-02"),
		currentTime.Format("15:04:05"),
		currentTime.Location().String())
	promptContent = promptContent + dateTimeString

	return promptContent, nil
}

// GetEmbeddedSystemPromptWithProvider returns the embedded system prompt
func GetEmbeddedSystemPromptWithProvider(provider string) (string, error) {
	return GetEmbeddedSystemPrompt()
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
		return "", agenterrors.NewPermanentError("critical error: system prompt start marker not found in embedded content", nil)
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
		return "", agenterrors.NewPermanentError("failed to extract planning prompt", err)
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
		todoIntegration += `- When you identify clear tasks, use the TodoWrite tool to create them
- This creates a todo system that can be tracked during implementation
- Structure todos by phases or categories
- Include descriptions for complex todos
`
	} else {
		todoIntegration += `- Disabled (user is managing tasks separately)
`
	}

	return promptContent + todoIntegration + dateTimeString, nil
}

// extractPlanningPrompt extracts the prompt content from the planning_prompt markdown
func extractPlanningPrompt() (string, error) {
	// The planning_prompt.md has the prompt content in a code block
	// We'll extract everything between the ``` markers
	const promptStart = "You are an autonomous planning and execution assistant."

	startIdx := strings.Index(planningPromptContent, promptStart)
	if startIdx == -1 {
		return "", agenterrors.NewPermanentError("critical error: planning prompt content not found in embedded content", nil)
	}

	endIdx := strings.Index(planningPromptContent[startIdx:], "```")
	if endIdx == -1 {
		// If no closing marker, use the whole content from start
		return strings.TrimSpace(planningPromptContent[startIdx:]), nil
	}

	return strings.TrimSpace(planningPromptContent[startIdx : startIdx+endIdx]), nil
}

func readEmbeddedPromptFile(filePath string) ([]byte, error) {
	trimmed := strings.TrimSpace(filePath)
	if trimmed == "" {
		return nil, agenterrors.NewInvalidInputError("embedded prompt file path is empty", nil)
	}

	normalized := filepath.ToSlash(trimmed)
	normalized = strings.TrimPrefix(normalized, "./")

	candidates := []string{}
	seen := map[string]struct{}{}
	addCandidate := func(candidate string) {
		candidate = strings.TrimSpace(strings.TrimPrefix(candidate, "./"))
		if candidate == "" {
			return
		}
		if _, exists := seen[candidate]; exists {
			return
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
	}

	addCandidate(normalized)
	if strings.HasPrefix(normalized, "pkg/agent/") {
		addCandidate(strings.TrimPrefix(normalized, "pkg/agent/"))
	}
	if idx := strings.Index(normalized, "/prompts/"); idx >= 0 {
		addCandidate(normalized[idx+1:])
	}

	for _, candidate := range candidates {
		content, err := embeddedPromptFiles.ReadFile(candidate)
		if err == nil {
			return content, nil
		}
	}

	return nil, agenterrors.NewPermanentError(fmt.Sprintf("failed to find embedded prompt: %s", filePath), nil)
}
