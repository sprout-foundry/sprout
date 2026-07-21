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

// SP-125: lite prompt for Low-Context Mode (8K–64K context windows).
// Selected by ContextProfile.SystemPromptPath at agent creation. Roughly
// ~1K tokens vs the full prompt's ~6.6K — strips delegation/review/persona
// sections that reference tools unavailable in LCM.
//
//go:embed prompts/system_prompt.lite.md
var systemPromptLiteContent string

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
	// Semi-static content — also placed before the volatile tail.
	memories := LoadMemoriesForPrompt()
	if memories != "" {
		promptContent = promptContent + memories
	}

	// Add the current working directory LAST among semi-static content.
	// Volatile per-call content (cwd) is grouped at the tail to preserve
	// prompt-prefix cache eligibility for the large static prefix. Providers
	// like Anthropic cache prompt prefixes; placing cwd at the start would
	// force a full re-process of every subsequent request.
	//
	// Note: date/time is NOT injected here. It lives in the user message
	// (see injectUserMessageTimestamp in seed_query.go) because second-
	// resolution timestamps invalidate the cached prefix on every request.
	// The system prompt remains byte-identical across calls — that is the
	// cache-eligibility invariant this section preserves.
	cwdString := buildCurrentWorkingDirectorySection("")

	promptContent = promptContent + cwdString

	return promptContent, nil
}

// GetEmbeddedSystemPromptWithProvider returns the embedded system prompt
func GetEmbeddedSystemPromptWithProvider(provider string) (string, error) {
	return GetEmbeddedSystemPrompt()
}

// GetEmbeddedSystemPromptForProfile (SP-125) selects the full or lite system
// prompt based on the resolved ContextProfile, then runs the standard
// augmentation (context files, memories, timestamp). In LCM the lite prompt
// (~1K tokens) replaces the full prompt (~6.6K). AGENTS.md is still injected
// by the context-files step regardless of profile — conventions are mandatory.
func GetEmbeddedSystemPromptForProfile(profile configuration.ContextProfile, provider string, contextWindow int, workspaceRoot string) (string, error) {
	promptContent, err := extractSystemPromptForProfile(profile)
	if err != nil {
		return "", agenterrors.NewPermanentError("failed to extract system prompt", err)
	}

	contextFiles, err := LoadContextFiles()
	if err == nil && contextFiles != "" {
		// SP-125: AGENTS.md is always injected (project conventions are
		// mandatory in every mode). In LCM only, warn once if the file is
		// large so the user understands the context cost and can shrink it.
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

	// Add the current working directory LAST among semi-static content.
	// Grouped at the tail to preserve prompt-prefix cache eligibility for
	// the static prefix.
	//
	// Note: date/time is NOT injected here. It lives in the user message
	// (see injectUserMessageTimestamp in seed_query.go) because second-
	// resolution timestamps invalidate the cached prefix on every request.
	// The system prompt remains byte-identical across calls — that is the
	// cache-eligibility invariant this section preserves.
	cwdString := buildCurrentWorkingDirectorySection(workspaceRoot)

	promptContent = promptContent + cwdString

	return promptContent, nil
}

// buildCurrentWorkingDirectorySection formats the "Current Working Directory"
// block injected at the tail of every system prompt. When workspaceRoot is
// non-empty it is used directly; otherwise falls back to os.Getwd() then ".".
// This ordering is intentional: workspaceRoot is the authoritative value in
// daemon mode, while os.Getwd() is correct in CLI/test mode (where
// workspaceRoot is empty).
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

// extractSystemPromptForProfile selects the full or lite prompt content based
// on the profile's SystemPromptPath. Falls back to the full prompt if the lite
// marker isn't present or the lite content can't be extracted.
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
	// Extract the prompt content from the markdown
	promptContent, err := extractPlanningPrompt()
	if err != nil {
		return "", agenterrors.NewPermanentError("failed to extract planning prompt", err)
	}

	// NOTE: date/time used to be appended here. It was removed because the
	// planning prompt is sent as the system prompt for one-shot planning
	// calls, and the time-dependent block would invalidate the prefix cache
	// on every invocation. The current timestamp now arrives as a
	// `<current-time>` tag in the user message (see
	// injectUserMessageTimestamp in seed_query.go) when this prompt is used
	// in agent-style flows; for the WASM/CLI `sprout plan` one-shot path,
	// callers can prepend a `<current-time>` tag themselves if needed.

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
