//go:build !js

package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// subagentProgressSnapshot is the most-recent live snapshot of a running
// subagent's token / context usage, refreshed by the runner's monitorProgress
// ticker (~every 2s). The CLI subscriber appends a compact "· 12.3k/128k ctx"
// suffix to subsequent tool-start lines fired by the same persona so users
// see the budget burn in real time, instead of only learning the final
// numbers in the "completed" line after the subagent has already exited.
type subagentProgressSnapshot struct {
	tokensUsed  int
	ctxUsed     int
	ctxMax      int
	iteration   int
	lastUpdated time.Time
}

// formatSubagentCtxSuffix renders the trailing "· 12.3k/128k ctx" hint
// appended to depth>0 tool-start lines. Returns "" when no useful
// numbers are available so the line stays clean during the first tick
// before any tokens have accumulated.
func formatSubagentCtxSuffix(snap subagentProgressSnapshot) string {
	if snap.ctxMax > 0 && snap.ctxUsed > 0 {
		return fmt.Sprintf(" · %s/%s ctx", formatTokensShort(snap.ctxUsed), formatTokensShort(snap.ctxMax))
	}
	if snap.tokensUsed > 0 {
		return fmt.Sprintf(" · %s tok", formatTokensShort(snap.tokensUsed))
	}
	return ""
}

// formatTokensShort formats a token count compactly: "1234" → "1.2k",
// "1234567" → "1.2M". Used inside tool/spawn lines where horizontal
// space is at a premium — the full comma-separated form lives in the
// "↳ done" line at the end of the subagent run.
func formatTokensShort(n int) string {
	switch {
	case n < 1000:
		return strconv.Itoa(n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000.0)
	default:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000.0)
	}
}

// formatSpawnLine renders the one-shot "↳ persona spawned (provider · model · 128k ctx)"
// line emitted the first time the CLI sees a new (depth, persona) pair in a
// turn. Indent matches the corresponding tool-line depth so it visually
// nests under the parent that spawned it. The `maxCtx` argument carries
// the subagent's model context budget (from monitorProgress's initial
// emit); 0 means "unknown" and the ctx suffix is dropped — the line
// degrades to the original "(provider · model)" form.
func formatSpawnLine(chatAgent *agent.Agent, depth int, persona string, maxCtx int) string {
	indent := console.PersonaIndent(depth)
	badge := console.PersonaBadge(depth, persona)
	suffix := ""
	if chatAgent != nil {
		if provider, model, err := chatAgent.GetPersonaProviderModel(persona); err == nil && (provider != "" || model != "") {
			if maxCtx > 0 {
				suffix = fmt.Sprintf(" (%s · %s · %s ctx)", provider, model, formatTokensShort(maxCtx))
			} else {
				suffix = fmt.Sprintf(" (%s · %s)", provider, model)
			}
		}
	}
	return fmt.Sprintf("%s  ↳ %sspawned%s", indent, badge, suffix)
}

// formatSubagentDoneLine renders the per-subagent completion summary —
// the closing bracket for the spawn line. Format:
//
//	  ↳ [persona] done · 12,345 tok · $0.0234 · 4.2s
//	  ↳ [persona] cancelled (budget_exceeded) · 8,901 tok · $0.0102 · 2.1s
//
// Indents at depth 1 to nest visually under the parent's run_subagent
// row. Numeric fields are omitted when zero so a no-cost / no-token
// cancellation stays terse rather than printing "0 tok · $0.0000".
func formatSubagentDoneLine(persona, status, reason string, tokens int, cost, elapsedSec float64) string {
	indent := console.PersonaIndent(1)
	badge := console.PersonaBadge(1, persona)
	icon := console.GlyphSuccess.Prefix()
	verb := "done"
	if status == "cancelled" {
		icon = console.GlyphPaused.Prefix()
		verb = "cancelled"
		if reason != "" {
			verb = fmt.Sprintf("cancelled (%s)", reason)
		}
	}
	parts := []string{}
	if tokens > 0 {
		parts = append(parts, fmt.Sprintf("%s tok", formatThousands(tokens)))
	}
	if cost > 0 {
		parts = append(parts, fmt.Sprintf("$%.4f", cost))
	}
	if elapsedSec > 0 {
		parts = append(parts, fmt.Sprintf("%.1fs", elapsedSec))
	}
	suffix := ""
	if len(parts) > 0 {
		suffix = " · " + strings.Join(parts, " · ")
	}
	return fmt.Sprintf("%s  ↳ %s %s%s%s", indent, icon, badge, verb, suffix)
}

// readEventInt extracts an int from an event payload, tolerating the
// numeric types the event bus may marshal through (int / int64 /
// float64 round-trip via JSON).
func readEventInt(data map[string]interface{}, key string) int {
	if data == nil {
		return 0
	}
	switch v := data[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

func readEventInt64(data map[string]interface{}, key string) int64 {
	if data == nil {
		return 0
	}
	switch v := data[key].(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	}
	return 0
}

// formatThousands renders an integer with comma separators (e.g.
// 1234567 → "1,234,567"). Negative numbers keep the sign.
func formatThousands(n int) string {
	if n < 0 {
		return "-" + formatThousands(-n)
	}
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	// Insert commas from the right.
	rem := len(s) % 3
	var b strings.Builder
	if rem > 0 {
		b.WriteString(s[:rem])
		if len(s) > rem {
			b.WriteByte(',')
		}
	}
	for i := rem; i < len(s); i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < len(s) {
			b.WriteByte(',')
		}
	}
	return b.String()
}

// readEventDepth reads the subagent_depth from an event payload. Returns 0
// for missing or malformed values — matches today's "primary agent" rendering
// when older events that pre-date SP-051 metadata land in the bus.
func readEventDepth(data map[string]interface{}) int {
	if data == nil {
		return 0
	}
	switch v := data["subagent_depth"].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

// readEventPersona reads the active_persona from an event payload, trimmed.
// Returns "" when absent — which suppresses the persona badge.
func readEventPersona(data map[string]interface{}) string {
	if data == nil {
		return ""
	}
	if s, ok := data["active_persona"].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}
