package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// UsageCommand implements the /usage slash command — a visual dashboard
// with Unicode bar charts for context window, cache efficiency, and cost.
type UsageCommand struct{}

// Name returns the command name
func (u *UsageCommand) Name() string {
	return "usage"
}

// Description returns the command description
func (u *UsageCommand) Description() string {
	return "Show visual usage dashboard with bar charts"
}

// Usage returns the detailed help text shown by `/help usage`.
func (u *UsageCommand) Usage() string {
	return strings.Join([]string{
		"/usage   Show a visual usage dashboard with Unicode bar charts.",
		"",
		"Displays context window fill, cache efficiency, token breakdown,",
		"cost (total and per-turn), and cache savings.",
		"Alias: /stats",
		"",
		"Flags:",
		"  --json   Output the same data as a JSON object",
	}, "\n")
}

// Execute renders the usage dashboard
func (u *UsageCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		fmt.Println(console.GlyphInfo.Prefix() + "No usage data yet — start a conversation to see metrics.")
		return nil
	}
	if chatAgent.GetTotalTokens() == 0 {
		fmt.Println(console.GlyphInfo.Prefix() + "No usage data yet — start a conversation to see metrics.")
		return nil
	}

	// Gather metrics
	totalTokens := chatAgent.GetTotalTokens()
	promptTokens := chatAgent.GetPromptTokens()
	completionTokens := chatAgent.GetCompletionTokens()
	cachedTokens := chatAgent.GetCachedTokens()
	cacheWriteTokens := chatAgent.GetCacheWriteTokens()
	currentContext := chatAgent.GetCurrentContextTokens()
	maxContext := chatAgent.GetMaxContextTokens()
	totalCost := chatAgent.GetTotalCost()
	iterations := chatAgent.GetCurrentIteration()
	cachedSavings := chatAgent.GetCachedCostSavings()
	estimatedResponses := chatAgent.GetEstimatedTokenResponses()

	// Computed values (matching summary.go)
	processedPromptTokens := promptTokens - cachedTokens
	if processedPromptTokens < 0 {
		processedPromptTokens = 0
	}
	processedTokens := processedPromptTokens + completionTokens

	// Model info for header — model name is already unique, don't
	// concatenate provider onto it.
	modelLabel := chatAgent.GetModel()

	// Header line
	fmt.Println()
	fmt.Printf("Session Usage                    %s · %d turns\n", modelLabel, iterations)
	fmt.Println("──────────────────────────────────────────────────────────────────")

	// Context bar
	if maxContext > 0 {
		contextBar := renderBar(currentContext, maxContext, 16)
		contextPct := float64(currentContext) / float64(maxContext) * 100
		fmt.Printf(" Context    %s  %s / %s   (%.1f%%)\n",
			contextBar,
			formatTokens(currentContext),
			formatTokens(maxContext),
			contextPct)
	} else {
		fmt.Printf(" Context    %s  %s (limit unavailable)\n",
			renderBar(0, 1, 16),
			formatTokens(currentContext))
	}

	// Cached bar (cached / prompt)
	if promptTokens > 0 {
		cacheBar := renderBar(cachedTokens, promptTokens, 16)
		cachePct := float64(cachedTokens) / float64(promptTokens) * 100
		fmt.Printf(" Cached     %s  %s / %s  (%.1f%% reused)\n",
			cacheBar,
			formatTokens(cachedTokens),
			formatTokens(promptTokens),
			cachePct)
	} else {
		fmt.Printf(" Cached     %s  %s / %s  (0.0%% reused)\n",
			renderBar(0, 1, 16),
			formatTokens(cachedTokens),
			formatTokens(promptTokens))
	}

	// Token breakdown
	fmt.Println()
	fmt.Printf(" Tokens     Prompt: %s    Completion: %s    Cache write: %s\n",
		formatTokens(promptTokens),
		formatTokens(completionTokens),
		formatTokens(cacheWriteTokens))

	// Cost line
	costStr := fmt.Sprintf("$%.6f", totalCost)
	if iterations > 0 {
		costPerTurn := totalCost / float64(iterations)
		fmt.Printf(" Cost       %-16s ($%.3f/turn)\n", costStr, costPerTurn)
	} else {
		fmt.Printf(" Cost       %s\n", costStr)
	}

	// Estimated token note
	if estimatedResponses > 0 {
		fmt.Printf("            Token usage includes estimates for %d response(s).\n", estimatedResponses)
	}

	// Cache savings and efficiency
	if cachedTokens > 0 {
		efficiency := 0.0
		if totalTokens > 0 {
			efficiency = float64(cachedTokens) / float64(totalTokens) * 100
		}

		savingsStr := fmt.Sprintf("$%.6f", cachedSavings)
		ratingGlyph, ratingText := getEfficiencyRating(efficiency)

		fmt.Println()
		fmt.Printf(" Cache savings  %-16s Efficiency: %s (%.1f%% cached)\n",
			savingsStr,
			ratingGlyph.Prefix()+ratingText,
			efficiency)
	}

	fmt.Println("──────────────────────────────────────────────────────────────────")
	fmt.Println()

	// Processed tokens summary line
	console.GlyphInfo.Fprintf(os.Stdout, "Processed: %s (%d prompt + %d completion)\n",
		formatTokens(processedTokens), processedPromptTokens, completionTokens)

	return nil
}

// usageJSONPayload is the JSON representation produced by /usage --json.
type usageJSONPayload struct {
	Model              string  `json:"model"`
	Turns              int     `json:"turns"`
	TotalTokens        int     `json:"total_tokens"`
	PromptTokens       int     `json:"prompt_tokens"`
	CompletionTokens   int     `json:"completion_tokens"`
	CachedTokens       int     `json:"cached_tokens"`
	CacheWriteTokens   int     `json:"cache_write_tokens"`
	ProcessedPrompt    int     `json:"processed_prompt_tokens"`
	ProcessedTotal     int     `json:"processed_total_tokens"`
	CurrentContext     int     `json:"current_context_tokens"`
	MaxContext         int     `json:"max_context_tokens"`
	ContextPct         float64 `json:"context_pct"`
	CachePct           float64 `json:"cache_pct"`
	TotalCost          float64 `json:"total_cost"`
	CostPerTurn        float64 `json:"cost_per_turn"`
	CacheSavings       float64 `json:"cache_savings"`
	CacheEfficiencyPct float64 `json:"cache_efficiency_pct"`
	EstimatedResponses int     `json:"estimated_responses"`
}

// ExecuteWithJSONOutput emits the usage dashboard data as JSON.
func (u *UsageCommand) ExecuteWithJSONOutput(args []string, chatAgent *agent.Agent, ctx *CommandContext) error {
	if chatAgent == nil || chatAgent.GetTotalTokens() == 0 {
		return WriteJSONToOutput(usageJSONPayload{})
	}

	totalTokens := chatAgent.GetTotalTokens()
	promptTokens := chatAgent.GetPromptTokens()
	completionTokens := chatAgent.GetCompletionTokens()
	cachedTokens := chatAgent.GetCachedTokens()
	cacheWriteTokens := chatAgent.GetCacheWriteTokens()
	currentContext := chatAgent.GetCurrentContextTokens()
	maxContext := chatAgent.GetMaxContextTokens()
	totalCost := chatAgent.GetTotalCost()
	iterations := chatAgent.GetCurrentIteration()
	cachedSavings := chatAgent.GetCachedCostSavings()
	estimatedResponses := chatAgent.GetEstimatedTokenResponses()

	processedPromptTokens := promptTokens - cachedTokens
	if processedPromptTokens < 0 {
		processedPromptTokens = 0
	}
	processedTokens := processedPromptTokens + completionTokens

	contextPct := 0.0
	if maxContext > 0 {
		contextPct = float64(currentContext) / float64(maxContext) * 100
	}

	cachePct := 0.0
	if promptTokens > 0 {
		cachePct = float64(cachedTokens) / float64(promptTokens) * 100
	}

	costPerTurn := 0.0
	if iterations > 0 {
		costPerTurn = totalCost / float64(iterations)
	}

	cacheEfficiencyPct := 0.0
	if totalTokens > 0 {
		cacheEfficiencyPct = float64(cachedTokens) / float64(totalTokens) * 100
	}

	return WriteJSONToOutput(usageJSONPayload{
		Model:              chatAgent.GetModel(),
		Turns:              iterations,
		TotalTokens:        totalTokens,
		PromptTokens:       promptTokens,
		CompletionTokens:   completionTokens,
		CachedTokens:       cachedTokens,
		CacheWriteTokens:   cacheWriteTokens,
		ProcessedPrompt:    processedPromptTokens,
		ProcessedTotal:     processedTokens,
		CurrentContext:     currentContext,
		MaxContext:         maxContext,
		ContextPct:         contextPct,
		CachePct:           cachePct,
		TotalCost:          totalCost,
		CostPerTurn:        costPerTurn,
		CacheSavings:       cachedSavings,
		CacheEfficiencyPct: cacheEfficiencyPct,
		EstimatedResponses: estimatedResponses,
	})
}

// renderBar returns a bar string of the given width showing the filled/total ratio.
// Uses █ for filled and ░ for empty segments.
// Edge cases: total=0 returns all empty; filled>=total returns all full.
func renderBar(filled, total, width int) string {
	if width <= 0 {
		return ""
	}
	if total <= 0 {
		return strings.Repeat("░", width)
	}
	if filled >= total {
		return strings.Repeat("█", width)
	}

	filledSegs := float64(filled) / float64(total) * float64(width)
	filledSegsInt := int(filledSegs)
	// Round if close to next segment
	if filledSegs-float64(filledSegsInt) >= 0.5 {
		filledSegsInt++
	}
	if filledSegsInt > width {
		filledSegsInt = width
	}
	if filledSegsInt < 0 {
		filledSegsInt = 0
	}

	return strings.Repeat("█", filledSegsInt) + strings.Repeat("░", width-filledSegsInt)
}

// formatTokens formats token counts for display:
// <1000: raw number, 1000-999999: X.Xk, 1M+: X.XM
func formatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1000000)
}

// getEfficiencyRating returns the appropriate glyph and text for cache efficiency.
// Thresholds match summary.go: ≥50% Excellent, ≥30% Good, ≥15% Average, <15% Low.
func getEfficiencyRating(efficiency float64) (console.Glyph, string) {
	switch {
	case efficiency >= 50:
		return console.GlyphSuccess, "Excellent"
	case efficiency >= 30:
		return console.GlyphSuccess, "Good"
	case efficiency >= 15:
		return console.GlyphWarning, "Average"
	default:
		return console.GlyphWarning, "Low"
	}
}
