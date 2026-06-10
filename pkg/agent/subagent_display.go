package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/console"
)

// printSubagentStart announces a delegated subagent run with a consistent
// action glyph instead of the old literal "[~] Spawning subagent" prefix.
func printSubagentStart(persona, provider, model string) {
	console.GlyphAction.Printf("subagent [%s] starting · %s/%s", persona, provider, model)
}

// printParallelSubagentStart announces a batch of parallel subagents.
func printParallelSubagentStart(count int, provider, model string) {
	console.GlyphAction.Printf("%d parallel subagents starting · %s/%s", count, provider, model)
}

// printSubagentDone announces completion with a compact stat summary so the
// user gets closure on a delegation (files touched, tokens, cost, tools, time)
// rather than the run finishing silently. Severity glyph reflects outcome.
func printSubagentDone(persona string, res *SubagentResult) {
	if res == nil {
		return
	}
	switch {
	case res.Cancelled:
		console.GlyphStopped.Printf("subagent [%s] cancelled%s", persona, subagentStatSuffix(res))
	case res.BudgetExceeded || res.Truncated:
		console.GlyphWarning.Printf("subagent [%s] stopped: budget exceeded%s", persona, subagentStatSuffix(res))
	case res.Error != nil:
		console.GlyphError.Printf("subagent [%s] failed: %s", persona, res.Error.Error())
	default:
		console.GlyphSuccess.Printf("subagent [%s] done%s", persona, subagentStatSuffix(res))
	}
}

// subagentStatSuffix renders " · 3 files · 12.5k tok · $0.02 · 4 tools · 8.1s",
// omitting any zero-valued component, or "" when nothing is available.
func subagentStatSuffix(res *SubagentResult) string {
	var parts []string
	if n := len(res.FileChanges); n > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", n, plural(n, "file", "files")))
	}
	if res.TokensUsed > 0 {
		parts = append(parts, compactCount(res.TokensUsed)+" tok")
	}
	if res.Cost > 0 {
		parts = append(parts, fmt.Sprintf("$%.2f", res.Cost))
	}
	if res.ToolCalls > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", res.ToolCalls, plural(res.ToolCalls, "tool", "tools")))
	}
	if res.Elapsed > 0 {
		parts = append(parts, res.Elapsed.Round(100*time.Millisecond).String())
	}
	if len(parts) == 0 {
		return ""
	}
	return " · " + strings.Join(parts, " · ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// compactCount renders 12500 → "12.5k", 1_500_000 → "1.5M".
func compactCount(n int) string {
	switch {
	case n >= 1_000_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1_000_000), ".0") + "M"
	case n >= 1_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1_000), ".0") + "k"
	default:
		return fmt.Sprintf("%d", n)
	}
}
