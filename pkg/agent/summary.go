package agent

import (
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent_tools"
)

// PrintConversationSummary displays a comprehensive conversation summary with formatting
func (a *Agent) PrintConversationSummary(forceFull bool) {

	if !forceFull {
		fmt.Println("Use /info command for detailed conversation summary")
		return
	}

	fmt.Println("\n📊 Conversation Summary")
	fmt.Println("══════════════════════════════")

	assistantMsgCount := 0
	userMsgCount := 0
	toolCallCount := 0

	for _, msg := range a.messages {
		switch msg.Role {
		case "assistant":
			assistantMsgCount++
			if strings.Contains(msg.Content, "tool_calls") {
				toolCallCount++
			}
		case "user":
			if msg.Content != a.messages[1].Content { // Skip original user query
				userMsgCount++
			}
		}
	}

	// Conversation metrics
	fmt.Printf("🔄 Iterations:      %d\n", a.currentIteration)
	fmt.Printf("🤖 Assistant msgs:   %d\n", assistantMsgCount)
	fmt.Printf("⚡ Tool executions:  %d\n", userMsgCount) // Tool results come back as user messages
	fmt.Printf("📨 Total messages:   %d\n", len(a.messages))
	fmt.Println()

	// Calculate actual processed tokens (excluding cached ones)
	actualProcessedTokens := a.totalTokens - a.cachedTokens

	// Token usage section with better formatting
	fmt.Println("🔢 Token Usage")
	fmt.Println("──────────────────────────────")
	fmt.Printf("📦 Total processed:    %s\n", a.formatTokenCount(a.totalTokens))
	fmt.Printf("📝 Actual processed:   %s (%d prompt + %d completion)\n",
		a.formatTokenCount(actualProcessedTokens), a.promptTokens, a.completionTokens)

	// Context window information
	contextUsage := float64(a.currentContextTokens) / float64(a.maxContextTokens) * 100
	fmt.Printf("🪟 Context window:     %s/%s (%.1f%% used)\n",
		a.formatTokenCount(a.currentContextTokens),
		a.formatTokenCount(a.maxContextTokens),
		contextUsage)

	if a.cachedTokens > 0 {
		efficiency := float64(a.cachedTokens) / float64(a.totalTokens) * 100
		fmt.Printf("♻️  Cached reused:     %s\n", a.formatTokenCount(a.cachedTokens))
		fmt.Printf("💰 Cost savings:       $%.6f\n", a.cachedCostSavings)
		fmt.Printf("📈 Efficiency:        %.1f%% tokens cached\n", efficiency)

		// Add efficiency rating
		var efficiencyRating string
		switch {
		case efficiency >= 50:
			efficiencyRating = "🏆 Excellent"
		case efficiency >= 30:
			efficiencyRating = "✅ Good"
		case efficiency >= 15:
			efficiencyRating = "📊 Average"
		default:
			efficiencyRating = "📉 Low"
		}
		fmt.Printf("🏅 Efficiency rating: %s\n", efficiencyRating)
	}

	fmt.Println()
	fmt.Printf("💵 Total cost:        $%.6f\n", a.totalCost)

	// Add cost per iteration
	if a.currentIteration > 0 {
		costPerIteration := a.totalCost / float64(a.currentIteration)
		fmt.Printf("📋 Cost per iteration: $%.6f\n", costPerIteration)
	}

	// Show optimization stats if enabled
	if a.optimizer != nil && a.optimizer.IsEnabled() {
		stats := a.optimizer.GetOptimizationStats()
		fmt.Println()
		fmt.Println("🔄 Conversation Optimization")
		fmt.Println("──────────────────────────────")
		fmt.Printf("📁 Files tracked:     %d\n", stats["tracked_files"])
		fmt.Printf("⚡ Commands tracked:  %d\n", stats["tracked_commands"])

		if trackedFiles, ok := stats["file_paths"].([]string); ok && len(trackedFiles) > 0 {
			if len(trackedFiles) <= 3 {
				fmt.Printf("📂 Tracked files:     %s\n", strings.Join(trackedFiles, ", "))
			} else {
				fmt.Printf("📂 Tracked files:     %s, +%d more\n",
					strings.Join(trackedFiles[:2], ", "), len(trackedFiles)-2)
			}
		}

		if trackedCommands, ok := stats["shell_commands"].([]string); ok && len(trackedCommands) > 0 {
			if len(trackedCommands) <= 3 {
				fmt.Printf("🔧 Tracked commands:  %s\n", strings.Join(trackedCommands, ", "))
			} else {
				fmt.Printf("🔧 Tracked commands:  %s, +%d more\n",
					strings.Join(trackedCommands[:2], ", "), len(trackedCommands)-2)
			}
		}
	}

	fmt.Println("══════════════════════════════")
	fmt.Println()
}

// PrintConciseSummary displays a single line with essential token and cost information
func (a *Agent) PrintConciseSummary() {
	actualProcessed := a.totalTokens - a.cachedTokens
	costStr := fmt.Sprintf("$%.6f", a.totalCost)
	fmt.Printf("💰 Session: %s total (%s processed + %s cached) | %s\n",
		a.formatTokenCount(a.totalTokens),
		a.formatTokenCount(actualProcessed),
		a.formatTokenCount(a.cachedTokens),
		costStr)
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

	// Add conversation metrics
	summary.WriteString("📊 CONVERSATION SUMMARY\n")
	summary.WriteString("══════════════════════════════\n\n")

	// Add task actions summary
	if len(a.taskActions) > 0 {
		summary.WriteString("🎯 COMPLETED ACTIONS:\n")
		summary.WriteString("──────────────────────────────\n")

		// Group actions by type
		actionCounts := make(map[string]int)
		for _, action := range a.taskActions {
			actionCounts[action.Type]++
		}

		for actionType, count := range actionCounts {
			summary.WriteString(fmt.Sprintf("• %s: %d actions\n", actionType, count))
		}
		summary.WriteString("\n")
	}

	// Add todo summary
	todoSummary := tools.GetTaskSummary()
	if todoSummary != "No tasks tracked in this session." {
		summary.WriteString("📋 TASK PROGRESS:\n")
		summary.WriteString("──────────────────────────────\n")
		summary.WriteString(todoSummary)
		summary.WriteString("\n")
	}

	// Add key files explored
	if a.optimizer != nil {
		stats := a.optimizer.GetOptimizationStats()
		if trackedFiles, ok := stats["file_paths"].([]string); ok && len(trackedFiles) > 0 {
			summary.WriteString("📂 KEY FILES EXPLORED:\n")
			summary.WriteString("──────────────────────────────\n")
			for _, file := range trackedFiles {
				summary.WriteString(fmt.Sprintf("• %s\n", file))
			}
			summary.WriteString("\n")
		}
	}

	// Add conversation metrics
	summary.WriteString("📈 CONVERSATION METRICS:\n")
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

	// Start with a session continuity header
	summary.WriteString("🔄 PREVIOUS SESSION CONTEXT\n")
	summary.WriteString("════════════════════════════\n\n")

	// Add accomplished todos with context
	todoSummary := tools.GetTaskSummary()
	if todoSummary != "No tasks tracked in this session." {
		summary.WriteString("✅ ACCOMPLISHED TASKS:\n")
		summary.WriteString("─────────────────────────────\n")

		// Get completed tasks with more detail
		completedTasks := tools.GetCompletedTasks()
		if len(completedTasks) > 0 {
			for i, task := range completedTasks {
				if i >= 8 { // Limit to 8 tasks to control size
					summary.WriteString("  ... and more\n")
					break
				}
				summary.WriteString(fmt.Sprintf("• %s\n", task))
			}
		} else {
			// Fallback to basic summary if detailed tasks not available
			lines := strings.Split(todoSummary, "\n")
			for _, line := range lines {
				if strings.Contains(line, "completed") || strings.Contains(line, "✅") {
					summary.WriteString(fmt.Sprintf("• %s\n", strings.TrimSpace(line)))
				}
			}
		}
		summary.WriteString("\n")
	}

	// Add key technical changes (limited and focused)
	if len(a.taskActions) > 0 {
		summary.WriteString("🔧 KEY TECHNICAL CHANGES:\n")
		summary.WriteString("─────────────────────────────\n")

		// Focus on the most important actions, limit to save space
		importantActions := []string{}
		for _, action := range a.taskActions {
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
			summary.WriteString("📄 KEY FILES:\n")
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
	summary.WriteString("📊 SESSION METRICS:\n")
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
