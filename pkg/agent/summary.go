package agent

import (
	"fmt"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	tools "github.com/alantheprice/ledit/pkg/agent_tools"
)

type conversationSummaryMetrics struct {
	assistantMessages int
	userMessages      int
	systemMessages    int
	toolMessages      int
	toolCalls         int
}

func computeConversationSummaryMetrics(messages []api.Message) conversationSummaryMetrics {
	metrics := conversationSummaryMetrics{}
	for _, msg := range messages {
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		switch role {
		case "assistant":
			metrics.assistantMessages++
			metrics.toolCalls += len(msg.ToolCalls)
		case "user":
			metrics.userMessages++
		case "system":
			metrics.systemMessages++
		case "tool":
			metrics.toolMessages++
		}
	}
	return metrics
}

// PrintConversationSummary displays a comprehensive conversation summary with formatting
func (a *Agent) PrintConversationSummary(forceFull bool) {

	if !forceFull {
		fmt.Println("Use /stats command for detailed conversation summary")
		return
	}

	fmt.Println("\n[chart] Conversation Summary")
	fmt.Println("══════════════════════════════")

	metrics := computeConversationSummaryMetrics(a.messages)

	// Conversation metrics
	fmt.Printf("[~] Iterations:      %d\n", a.currentIteration)
	fmt.Printf("[you] User msgs:        %d\n", metrics.userMessages)
	fmt.Printf("[bot] Assistant msgs:   %d\n", metrics.assistantMessages)
	fmt.Printf("[cfg] Tool calls:      %d\n", metrics.toolCalls)
	fmt.Printf("[tools] Tool results:    %d\n", metrics.toolMessages)
	fmt.Printf("[msg] Total messages:   %d\n", len(a.messages))
	fmt.Println()

	// Calculate processed tokens (excluding cached ones)
	processedPromptTokens := a.promptTokens - a.cachedTokens
	if processedPromptTokens < 0 {
		processedPromptTokens = 0
	}
	processedTokens := processedPromptTokens + a.completionTokens

	// Verify consistency: total - cached should approximately equal prompt-processed + completion
	expectedProcessed := a.totalTokens - a.cachedTokens
	if expectedProcessed != processedTokens {
		// Log discrepancy for debugging (only in debug mode)
		a.debugLog("Token count discrepancy: computed %d vs expected %d\n", processedTokens, expectedProcessed)
	}

	// Token usage section
	fmt.Println("[num] Token Usage")
	fmt.Println("──────────────────────────────")
	estimateLabel := ""
	if a.estimatedTokenResponses > 0 {
		estimateLabel = " (estimated)"
	}
	fmt.Printf("[pkg] Total%s:            %s\n", estimateLabel, a.formatTokenCount(a.totalTokens))
	fmt.Printf("[edit] Processed%s:        %s (%d prompt + %d completion)\n", estimateLabel,
		a.formatTokenCount(processedTokens), processedPromptTokens, a.completionTokens)

	// Context window information
	if a.maxContextTokens > 0 {
		contextUsage := float64(a.currentContextTokens) / float64(a.maxContextTokens) * 100
		fmt.Printf("[win] Context window:     %s/%s (%.1f%% used)\n",
			a.formatTokenCount(a.currentContextTokens),
			a.formatTokenCount(a.maxContextTokens),
			contextUsage)
	} else {
		fmt.Printf("[win] Context window:     %s (limit unavailable)\n", a.formatTokenCount(a.currentContextTokens))
	}

	if a.estimatedTokenResponses > 0 {
		fmt.Printf("[info] Token usage includes estimates for %d response(s) where provider usage was unavailable.\n", a.estimatedTokenResponses)
	} else if a.totalTokens == 0 && a.currentContextTokens > 0 {
		fmt.Println("[info] Token usage from provider was unavailable for this run.")
	}

	if a.cachedTokens > 0 {
		efficiency := 0.0
		if a.totalTokens > 0 {
			efficiency = float64(a.cachedTokens) / float64(a.totalTokens) * 100
		}
		fmt.Printf("[recycle] Cached reused:     %s\n", a.formatTokenCount(a.cachedTokens))
		fmt.Printf("$ Cost savings:       $%.6f\n", a.cachedCostSavings)
		fmt.Printf("[up] Efficiency:        %.1f%% tokens cached\n", efficiency)

		// Add efficiency rating
		var efficiencyRating string
		switch {
		case efficiency >= 50:
			efficiencyRating = "[cup] Excellent"
		case efficiency >= 30:
			efficiencyRating = "[OK] Good"
		case efficiency >= 15:
			efficiencyRating = "[chart] Average"
		default:
			efficiencyRating = "[down] Low"
		}
		fmt.Printf("[medal] Efficiency rating: %s\n", efficiencyRating)
	}

	fmt.Println()
	fmt.Printf("$ Total cost:        $%.6f\n", a.totalCost)

	// Add cost per iteration
	if a.currentIteration > 0 {
		costPerIteration := a.totalCost / float64(a.currentIteration)
		fmt.Printf("[list] Cost per iteration: $%.6f\n", costPerIteration)
	}

	fmt.Println("══════════════════════════════")
	fmt.Println()
}

// PrintConciseSummary displays a single line with essential token and cost information
func (a *Agent) PrintConciseSummary() {
	processedPromptTokens := a.promptTokens - a.cachedTokens
	if processedPromptTokens < 0 {
		processedPromptTokens = 0
	}
	processedTokens := processedPromptTokens + a.completionTokens

	// Verify consistency: total - cached should approximately equal prompt-processed + completion
	expectedProcessed := a.totalTokens - a.cachedTokens
	if expectedProcessed != processedTokens {
		a.debugLog("Token count discrepancy: computed %d vs expected %d\n", processedTokens, expectedProcessed)
	}

	costStr := fmt.Sprintf("$%.6f", a.totalCost)
	fmt.Printf("\n$ Session: %s total (%s processed + %s cached) | %s\n",
		a.formatTokenCount(a.totalTokens),
		a.formatTokenCount(processedTokens),
		a.formatTokenCount(a.cachedTokens),
		costStr)

	// Output machine-parseable metrics for parent agent extraction
	fmt.Printf("SUBAGENT_METRICS: total_tokens=%d prompt_tokens=%d completion_tokens=%d total_cost=%.6f cached_tokens=%d processed_prompt_tokens=%d processed_tokens=%d\n",
		a.totalTokens,
		a.promptTokens,
		a.completionTokens,
		a.totalCost,
		a.cachedTokens,
		processedPromptTokens,
		processedTokens)
}

// PrintCompactProgress prints a minimal progress indicator for non-interactive mode
// Format: [iteration:(current-context-tokens/context-limit) | total-tokens | cost]
func (a *Agent) PrintCompactProgress() {
	// Format tokens in K units for compactness
	formatTokensCompact := func(tokens int) string {
		if tokens >= 1000 {
			return fmt.Sprintf("%.1fK", float64(tokens)/1000)
		}
		return fmt.Sprintf("%d", tokens)
	}

	// Format cost compactly
	formatCostCompact := func(cost float64) string {
		if cost < 0.01 {
			return fmt.Sprintf("$%.4f", cost)
		} else if cost < 1.0 {
			return fmt.Sprintf("$%.3f", cost)
		} else {
			return fmt.Sprintf("$%.2f", cost)
		}
	}

	// Print the compact progress indicator with total tokens and cost
	fmt.Printf("[%d:(%s/%s) | %s | %s] ",
		a.currentIteration,
		formatTokensCompact(a.currentContextTokens),
		formatTokensCompact(a.maxContextTokens),
		formatTokensCompact(a.totalTokens),
		formatCostCompact(a.totalCost))
}

// calculateCachedCost calculates the cost savings from cached tokens
func (a *Agent) calculateCachedCost(cachedTokens int) float64 {
	if cachedTokens == 0 {
		return 0.0
	}

	// Calculate cost savings based on model pricing (input token rate)
	costPerToken := 0.0
	model := a.GetModel()

	// Get input token pricing based on model and provider
	provider := a.GetProvider()

	// OpenRouter-specific pricing (updated January 2025)
	if provider == "openrouter" {
		if strings.Contains(model, "deepseek-chat") || strings.Contains(model, "deepseek-r1") {
			// DeepSeek models on OpenRouter: ~$0.55 per million input tokens
			costPerToken = 0.55 / 1000000
		} else if strings.Contains(model, "gpt-4o") {
			// GPT-4o on OpenRouter: $2.50 per million input tokens
			costPerToken = 2.50 / 1000000
		} else if strings.Contains(model, "gpt-4") {
			// GPT-4 on OpenRouter: $30 per million input tokens
			costPerToken = 30.0 / 1000000
		} else if strings.Contains(model, "claude-3.5-sonnet") {
			// Claude 3.5 Sonnet: $3.00 per million input tokens
			costPerToken = 3.00 / 1000000
		} else if strings.Contains(model, "claude-3-opus") {
			// Claude 3 Opus: $15.00 per million input tokens
			costPerToken = 15.0 / 1000000
		} else if strings.Contains(model, "claude-3-sonnet") {
			// Claude 3 Sonnet: $3.00 per million input tokens
			costPerToken = 3.00 / 1000000
		} else if strings.Contains(model, "claude-3-haiku") {
			// Claude 3 Haiku: $0.25 per million input tokens
			costPerToken = 0.25 / 1000000
		} else if strings.Contains(model, "llama-3.1-405b") {
			// Llama 3.1 405B: ~$5.00 per million input tokens
			costPerToken = 5.0 / 1000000
		} else if strings.Contains(model, "llama-3.1-70b") {
			// Llama 3.1 70B: ~$0.88 per million input tokens
			costPerToken = 0.88 / 1000000
		} else if strings.Contains(model, "llama-3.1-8b") {
			// Llama 3.1 8B: ~$0.18 per million input tokens
			costPerToken = 0.18 / 1000000
		} else {
			// Default OpenRouter pricing (use DeepSeek rate as conservative estimate)
			costPerToken = 0.55 / 1000000
		}
	} else if strings.Contains(model, "gpt-oss") {
		// GPT-OSS pricing: $0.30 per million input tokens
		costPerToken = 0.30 / 1000000
	} else if strings.Contains(model, "qwen3-coder") {
		// Qwen3-Coder-480B-A35B-Instruct-Turbo pricing: $0.30 per million input tokens
		costPerToken = 0.30 / 1000000
	} else if strings.Contains(model, "llama") {
		// Llama pricing: $0.36 per million tokens
		costPerToken = 0.36 / 1000000
	} else {
		// Default pricing (conservative estimate)
		costPerToken = 1.0 / 1000000
	}

	costSavings := float64(cachedTokens) * costPerToken

	return costSavings
}

// GenerateConversationSummary creates a comprehensive summary of the conversation including todos
func (a *Agent) GenerateConversationSummary() string {
	var summary strings.Builder
	taskActions := a.GetTaskActions()

	// Add conversation metrics
	summary.WriteString("[chart] CONVERSATION SUMMARY\n")
	summary.WriteString("══════════════════════════════\n\n")

	// Add task actions summary
	if len(taskActions) > 0 {
		summary.WriteString("[*] COMPLETED ACTIONS:\n")
		summary.WriteString("──────────────────────────────\n")

		// Group actions by type
		actionCounts := make(map[string]int)
		for _, action := range taskActions {
			actionCounts[action.Type]++
		}

		for actionType, count := range actionCounts {
			summary.WriteString(fmt.Sprintf("• %s: %d actions\n", actionType, count))
		}
		summary.WriteString("\n")
	}

	// Add todo summary (using TodoRead to get current state)
	todos := tools.TodoRead()
	if len(todos) > 0 {
		completed := 0
		for _, t := range todos {
			if t.Status == "completed" {
				completed++
			}
		}
		summary.WriteString("[list] TASK PROGRESS:\n")
		summary.WriteString("──────────────────────────────\n")
		summary.WriteString(fmt.Sprintf("• Completed: %d/%d tasks\n", completed, len(todos)))
		summary.WriteString("\n")
	}

	// Add key files explored
	if a.optimizer != nil {
		stats := a.optimizer.GetOptimizationStats()
		if trackedFiles, ok := stats["file_paths"].([]string); ok && len(trackedFiles) > 0 {
			summary.WriteString("[dir/] KEY FILES EXPLORED:\n")
			summary.WriteString("──────────────────────────────\n")
			for _, file := range trackedFiles {
				summary.WriteString(fmt.Sprintf("• %s\n", file))
			}
			summary.WriteString("\n")
		}
	}

	// Add conversation metrics
	summary.WriteString("[up] CONVERSATION METRICS:\n")
	summary.WriteString("──────────────────────────────\n")
	summary.WriteString(fmt.Sprintf("• Iterations: %d\n", a.currentIteration))
	summary.WriteString(fmt.Sprintf("• Total cost: $%.6f\n", a.totalCost))
	summary.WriteString(fmt.Sprintf("• Total tokens: %s\n", a.formatTokenCount(a.totalTokens)))

	if a.cachedTokens > 0 {
		efficiency := float64(a.cachedTokens) / float64(a.totalTokens) * 100
		summary.WriteString(fmt.Sprintf("• Efficiency: %.1f%% tokens cached\n", efficiency))
	}

	summary.WriteString("══════════════════════════════\n")

	return summary.String()
}

// GenerateCompactSummary creates a compact summary for session continuity (max 5K context)
func (a *Agent) GenerateCompactSummary() string {
	var summary strings.Builder
	taskActions := a.GetTaskActions()

	// Start with a session continuity header
	summary.WriteString("[~] PREVIOUS SESSION CONTEXT\n")
	summary.WriteString("════════════════════════════\n\n")

	// Add accomplished todos
	todos := tools.TodoRead()
	if len(todos) > 0 {
		summary.WriteString("[OK] ACCOMPLISHED TASKS:\n")
		summary.WriteString("─────────────────────────────\n")
		count := 0
		for _, t := range todos {
			if t.Status == "completed" && count < 8 {
				summary.WriteString(fmt.Sprintf("• %s\n", t.Content))
				count++
			}
		}
		if count < len(todos) {
			summary.WriteString("  ... and more\n")
		}
		summary.WriteString("\n")
	}

	// Add key technical changes (limited and focused)
	if len(taskActions) > 0 {
		summary.WriteString("[tool] KEY TECHNICAL CHANGES:\n")
		summary.WriteString("─────────────────────────────\n")

		// Focus on the most important actions, limit to save space
		importantActions := []string{}
		for _, action := range taskActions {
			if action.Type == "file_modified" || action.Type == "file_created" {
				importantActions = append(importantActions,
					fmt.Sprintf("• %s: %s", action.Type, action.Description))
			}
		}

		// Limit to most recent 6 actions
		start := 0
		if len(importantActions) > 6 {
			start = len(importantActions) - 6
			summary.WriteString("  [Recent changes shown]\n")
		}

		for i := start; i < len(importantActions); i++ {
			summary.WriteString(importantActions[i] + "\n")
		}
		summary.WriteString("\n")
	}

	// Add key files touched (limited list)
	if a.optimizer != nil {
		stats := a.optimizer.GetOptimizationStats()
		if trackedFiles, ok := stats["file_paths"].([]string); ok && len(trackedFiles) > 0 {
			summary.WriteString("[doc] KEY FILES:\n")
			summary.WriteString("─────────────────────────────\n")

			// Limit to 8 files to control summary size
			count := len(trackedFiles)
			if count > 8 {
				count = 8
			}

			for i := 0; i < count; i++ {
				summary.WriteString(fmt.Sprintf("• %s\n", trackedFiles[i]))
			}

			if len(trackedFiles) > 8 {
				summary.WriteString(fmt.Sprintf("  ... and %d more files\n", len(trackedFiles)-8))
			}
			summary.WriteString("\n")
		}
	}

	// Add concise session metrics
	summary.WriteString("[chart] SESSION METRICS:\n")
	summary.WriteString("─────────────────────────────\n")
	summary.WriteString(fmt.Sprintf("• Cost: $%.4f", a.totalCost))
	if a.cachedTokens > 0 {
		efficiency := float64(a.cachedTokens) / float64(a.totalTokens) * 100
		summary.WriteString(fmt.Sprintf(" (%.0f%% cached)", efficiency))
	}
	summary.WriteString("\n")

	summary.WriteString("════════════════════════════\n")

	// Ensure summary is under 5K characters
	result := summary.String()
	if len(result) > 5000 {
		// Truncate and add indicator
		result = result[:4950] + "...\n[Summary truncated to 5K limit]\n════════════════════════════\n"
	}

	return result
}
