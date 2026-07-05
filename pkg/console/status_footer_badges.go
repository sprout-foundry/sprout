package console

import (
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// color constants
// ---------------------------------------------------------------------------

// footerBaseColor is the ANSI escape used to colorize the rule row and
// the separator characters between badges. Cyan reads as "informational"
// in most terminal themes and stays legible on both dark and light
// backgrounds. The "39" code in footerResetFgKeepBase resets just the
// foreground while preserving the colored context — used between
// segments so the `·` separators stay cyan even after a colored badge.
//
// Per-segment colors (badgeColor*) replace the previous "all cyan"
// rendering: each piece of footer state carries its own semantic color
// so a glance tells you *what* is hot, not just *that* something is.
const (
	footerBaseColor       = "\033[36m"                   // cyan — rule + separators
	footerResetFgKeepBase = "\033[39m" + footerBaseColor // pop fg back to cyan

	// Per-badge palette. Bright variants (9X) read as foreground on
	// most terminal themes; the cyan/yellow/red threshold colors
	// already used for cost are preserved unchanged.
	badgeColorModel    = "\033[1;96m" // bold bright-cyan — brand identity
	badgeColorCtxOK    = "\033[32m"   // green — context <50%
	badgeColorCtxWarn  = "\033[33m"   // yellow — context 50–80%
	badgeColorCtxAlert = "\033[31m"   // red — context >80%
	badgeColorCwd      = "\033[2;36m" // dim cyan — ambient, low priority
	badgeColorSubagent = "\033[95m"   // bright magenta — persona-coded
	badgeColorQueue    = "\033[33m"   // yellow — needs attention soon
)

// ---------------------------------------------------------------------------
// badge composition
// ---------------------------------------------------------------------------

// composeLine builds the content row of the footer, padded/truncated to
// cols width. Each badge applies its own semantic color and resets back
// to the footer base (cyan) so the `·` separators stay visually
// consistent. The pattern is:
//
//	<badgeColor> <text> <footerResetFgKeepBase>
//
// Any badge can change without affecting its neighbors. Cost thresholds
// (existing behavior) are preserved.
func (f *StatusFooter) composeLine(cols int) string {
	if f.source == nil {
		return ""
	}
	model := truncTo(f.source.Model(), 30)
	used, limit := f.source.ContextTokens()
	cost := f.source.TotalCost()
	cwd := shortPath(f.source.WorkingDir())
	branch := gitBranchOf(cwd)

	// Build the cost segment. When the source exposes a per-turn cost
	// delta (CLI-UX-6), show "turn · session" split so the user gets
	// cost-pacing signal. Fall back to cumulative-only for sources that
	// don't implement turnCostSource (e.g. WebUI).
	costText := formatCost(cost)
	if tcs, ok := f.source.(turnCostSource); ok {
		if turnCost := tcs.TurnCost(); turnCost > 0.001 {
			costText = formatCost(turnCost) + " turn · " + formatCost(cost) + " session"
		}
	}

	parts := []string{
		styleSegment(badgeColorModel, model),
		styleSegment(styleCtxColor(used, limit), formatCtx(used, limit)),
		f.styleCost(cost, costText),
		styleSegment(badgeColorCwd, cwdSegment(cwd, branch)),
	}
	// SP-051-2d: append " · N sub" when subagents are active. Optional
	// interface — sources that don't implement it (e.g. WebUI) get the
	// baseline footer with no change.
	if asc, ok := f.source.(activeSubagentsSource); ok {
		if n := asc.ActiveSubagents(); n > 0 {
			parts = append(parts, styleSegment(badgeColorSubagent, fmt.Sprintf("%d sub", n)))
		}
	}
	// SP-055 Phase 3b: append "⏸ N queued" when deferred steer messages
	// are waiting for the next user turn. Tells the user at a glance
	// that they'll see queued-from-prior-turn content on their next
	// prompt.
	if qms, ok := f.source.(queuedMessagesSource); ok {
		if n := qms.QueuedMessages(); n > 0 {
			parts = append(parts, styleSegment(badgeColorQueue, fmt.Sprintf("⏸ %d queued", n)))
		}
	}
	body := " " + strings.Join(parts, " · ") + " "
	if visibleLen(body) >= cols {
		return truncWithEllipsis(body, cols)
	}
	// Pad with spaces — the top hr already provides visual framing, so
	// the content row stays light. \033[K isn't enough here because the
	// surrounding color codes need to extend through the padding too.
	return body + strings.Repeat(" ", cols-visibleLen(body))
}

// styleSegment wraps a badge body with its color prefix and a reset
// back to the footer base color so the next separator stays cyan.
// Centralized here so adding new badges is a one-liner at the callsite.
func styleSegment(color, text string) string {
	return color + text + footerResetFgKeepBase
}

// styleCtxColor picks a threshold color for the context badge based on
// how full the context window is. Thresholds: <50% green, 50–80%
// yellow, >80% red. Unknown limits (limit ≤ 0) render in the base
// footer color so we don't lie about pressure.
func styleCtxColor(used, limit int) string {
	if limit <= 0 {
		return footerBaseColor
	}
	pct := float64(used) / float64(limit)
	switch {
	case pct >= 0.80:
		return badgeColorCtxAlert
	case pct >= 0.50:
		return badgeColorCtxWarn
	default:
		return badgeColorCtxOK
	}
}

// styleCost colorizes a cost string against the threshold fields. The
// closing escape pops the foreground back to footerBaseColor (cyan)
// rather than to the terminal default, so the rest of the footer line
// stays cyan after the highlighted span. SP-048-3d.
func (f *StatusFooter) styleCost(cost float64, text string) string {
	switch {
	case cost >= f.AlertCost:
		return "\033[31m" + text + footerResetFgKeepBase // red, then back to cyan
	case cost >= f.WarnCost:
		return "\033[33m" + text + footerResetFgKeepBase // yellow, then back to cyan
	default:
		return text
	}
}
