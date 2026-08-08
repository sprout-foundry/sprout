package agent

import (
	"embed"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// agentsMdLargeTokenThreshold is the token count above which AGENTS.md (and
// sibling context files) triggers a size warning in Low-Context Mode. The file
// is still injected regardless — this is advisory only.
const agentsMdLargeTokenThreshold = 4000

//go:embed prompts/system_prompt.md
var systemPromptContent string

// Lite prompt for Low-Context Mode (8K–64K context windows). ~1K tokens vs the full prompt's ~6.6K.
//
//go:embed prompts/system_prompt.lite.md
var systemPromptLiteContent string

//go:embed prompts/planning_prompt.md
var planningPromptContent string

// Rollup prompt template for the background rollup worker. Separate from the per-turn summarizer.
//
//go:embed prompts/rollup_prompt.md
var rollupPromptContent string

// GetEmbeddedRollupPrompt returns the rollup summarizer prompt.
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
	promptContent, err := extractSystemPrompt()
	if err != nil {
		return "", agenterrors.NewPermanentError("failed to extract system prompt", err)
	}

	// Context files (AGENTS.md, etc.) - placed before volatile content to preserve prompt-prefix cache.
	contextFiles, err := LoadContextFiles()
	if err == nil && contextFiles != "" {
		promptContent = promptContent + contextFiles
	}

	// Memories - also placed before the volatile tail.
	memories := LoadMemoriesForPrompt()
	if memories != "" {
		promptContent = promptContent + memories
	}

	// Cwd at the tail to preserve prompt-prefix cache eligibility. Timestamp lives in the user message.
	cwdString := buildCurrentWorkingDirectorySection("")

	promptContent = promptContent + cwdString

	return promptContent, nil
}

// GetEmbeddedSystemPromptWithProvider returns the embedded system prompt
func GetEmbeddedSystemPromptWithProvider(provider string) (string, error) {
	return GetEmbeddedSystemPrompt()
}

// GetEmbeddedSystemPromptForProfile selects the full or lite system prompt based on the ContextProfile.
func GetEmbeddedSystemPromptForProfile(profile configuration.ContextProfile, provider string, contextWindow int, workspaceRoot string) (string, error) {
	promptContent, err := extractSystemPromptForProfile(profile)
	if err != nil {
		return "", agenterrors.NewPermanentError("failed to extract system prompt", err)
	}

	contextFiles, err := LoadContextFiles()
	if err == nil && contextFiles != "" {
		// AGENTS.md is always injected. In LCM, warn if the file is large.
		if profile.Mode == configuration.ContextModeLowContext {
			tokens := EstimateTokens(contextFiles)
			if tokens > agentsMdLargeTokenThreshold {
				windowLabel := "the context window"
				windowK := contextWindow / 1000
				pct := 0
				if contextWindow > 0 {
					pct = tokens * 100 / contextWindow
					windowLabel = fmt.Sprintf("a %dK window", windowK)
				}
				fmt.Fprintf(os.Stderr,
					"⚠ AGENTS.md is large (~%d tokens, ~%d%% of %s).\n"+
						"  It will still be injected — project conventions are mandatory.\n"+
						"  To shrink it: move reference material to linked docs, split into\n"+
						"  per-package AGENTS.md files, or trim historical notes.\n",
					tokens, pct, windowLabel)
			}
		}
		promptContent = promptContent + contextFiles
	}

	memories := LoadMemoriesForPrompt()
	if memories != "" {
		promptContent = promptContent + memories
	}

	// Cwd at the tail to preserve prompt-prefix cache. Timestamp lives in the user message.
	cwdString := buildCurrentWorkingDirectorySection(workspaceRoot)

	promptContent = promptContent + cwdString

	return promptContent, nil
}

// buildCurrentWorkingDirectorySection formats the "Current Working Directory" block. Uses workspaceRoot if set, otherwise os.Getwd().
func buildCurrentWorkingDirectorySection(workspaceRoot string) string {
	cwd := workspaceRoot
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil || cwd == "" {
			cwd = "."
		}
	}
	return fmt.Sprintf("\n\n## Current Working Directory\n\n`%s`\n\n---\n", cwd)
}

// extractSystemPromptForProfile selects the full or lite prompt based on SystemPromptPath. Falls back to full prompt.
func extractSystemPromptForProfile(profile configuration.ContextProfile) (string, error) {
	if strings.HasSuffix(profile.SystemPromptPath, "lite.md") {
		if content, err := extractFromContent(systemPromptLiteContent); err == nil {
			return content, nil
		}
	}
	return extractSystemPrompt()
}

// extractSystemPrompt extracts the prompt content from the system_prompt markdown
func extractSystemPrompt() (string, error) {
	return extractFromContent(systemPromptContent)
}

// extractFromContent extracts prompt text from a markdown source that wraps
// the prompt body in triple-backtick fences. Finds the first ``` and the last
// ``` (handles nested code blocks inside the prompt). Shared by the full and
// lite prompt extractors.
func extractFromContent(source string) (string, error) {
	const promptStart = "```"

	startIdx := strings.Index(source, promptStart)
	if startIdx == -1 {
		return "", agenterrors.NewPermanentError("system prompt start marker not found in embedded content", nil)
	}

	contentStart := startIdx + len(promptStart)
	for contentStart < len(source) && (source[contentStart] == '\n' || source[contentStart] == '\r') {
		contentStart++
	}

	endIdx := strings.LastIndex(source, "```")
	if endIdx == -1 || endIdx <= startIdx {
		return strings.TrimSpace(source[contentStart:]), nil
	}

	return strings.TrimSpace(source[contentStart:endIdx]), nil
}

// GetEmbeddedPlanningPrompt returns the embedded planning prompt
func GetEmbeddedPlanningPrompt(createTodos bool) (string, error) {
	promptContent, err := extractPlanningPrompt()
	if err != nil {
		return "", agenterrors.NewPermanentError("failed to extract planning prompt", err)
	}

	// Timestamp removed from here to preserve prefix cache; it arrives in the user message instead.

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

	return promptContent + todoIntegration, nil
}

// extractPlanningPrompt extracts the prompt content from the planning_prompt markdown
func extractPlanningPrompt() (string, error) {
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
