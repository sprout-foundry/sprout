package subagents

import (
	"fmt"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/console"
)

// PrintSubagentStart announces a delegated subagent run with a consistent
// action glyph. task is the delegated prompt; its first line is shown,
// shortened, so the opener says what the subagent was asked to do.
func PrintSubagentStart(persona, task, provider, model string) {
	if t := TaskHeadline(task); t != "" {
		console.GlyphAction.Printf("subagent [%s] starting: %s · %s/%s", persona, t, provider, model)
		return
	}
	console.GlyphAction.Printf("subagent [%s] starting · %s/%s", persona, provider, model)
}

// TaskHeadline is the first line of a delegated prompt, cut to 60 runes.
func TaskHeadline(task string) string {
	task = strings.TrimSpace(task)
	if i := strings.IndexByte(task, '\n'); i >= 0 {
		task = strings.TrimSpace(task[:i])
	}
	if r := []rune(task); len(r) > 60 {
		task = string(r[:59]) + "…"
	}
	return task
}

// printParallelSubagentStart announces a batch of parallel subagents.
func PrintParallelSubagentStart(count int, provider, model string) {
	console.GlyphAction.Printf("%d parallel subagents starting · %s/%s", count, provider, model)
}

// PrintSubagentDone announces completion with a compact stat summary so the
// user gets closure on a delegation (files touched, tokens, cost, tools, time)
// rather than the run finishing silently. Severity glyph reflects outcome.
func PrintSubagentDone(persona string, res *SubagentResult) {
	if res == nil {
		return
	}
	console.PrintLine(FormatSubagentDone(persona, res))
}

// FormatSubagentDone renders the completion line PrintSubagentDone prints.
// label is the persona, or "persona:taskID" for a run among siblings.
func FormatSubagentDone(label string, res *SubagentResult) string {
	switch {
	case res.Cancelled:
		return console.GlyphStopped.Prefix() + fmt.Sprintf("subagent [%s] cancelled%s", label, subagentStatSuffix(res))
	case res.BudgetExceeded || res.Truncated:
		return console.GlyphWarning.Prefix() + fmt.Sprintf("subagent [%s] stopped: budget exceeded%s", label, subagentStatSuffix(res))
	case res.Error != nil:
		return console.GlyphError.Prefix() + fmt.Sprintf("subagent [%s] failed: %s", label, res.Error.Error())
	default:
		return console.GlyphSuccess.Prefix() + fmt.Sprintf("subagent [%s] done%s", label, subagentStatSuffix(res))
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
		parts = append(parts, CompactCount(res.TokensUsed)+" tok")
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
func CompactCount(n int) string {
	switch {
	case n >= 1_000_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1_000_000), ".0") + "M"
	case n >= 1_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1_000), ".0") + "k"
	default:
		return fmt.Sprintf("%d", n)
	}
}
