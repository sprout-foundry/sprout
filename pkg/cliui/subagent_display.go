//go:build !js

package cliui

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SubagentProgressSnapshot is the most-recent live snapshot of a running
// subagent's token / context usage, refreshed by the runner's monitorProgress
// ticker (~every 2s). The CLI subscriber appends a compact "· 12.3k/128k ctx"
// suffix to subsequent tool-start lines fired by the same persona so users
// see the budget burn in real time, instead of only learning the final
// numbers in the "completed" line after the subagent has already exited.
type SubagentProgressSnapshot struct {
	TokensUsed  int
	CtxUsed     int
	CtxMax      int
	Iteration   int
	LastUpdated time.Time
}

// FormatSubagentCtxSuffix renders the trailing "· 12.3k/128k ctx" hint
// appended to depth>0 tool-start lines. Returns "" when no useful
// numbers are available so the line stays clean during the first tick
// before any tokens have accumulated.
func FormatSubagentCtxSuffix(snap SubagentProgressSnapshot) string {
	if snap.CtxMax > 0 && snap.CtxUsed > 0 {
		return fmt.Sprintf(" · %s/%s ctx", FormatTokensShort(snap.CtxUsed), FormatTokensShort(snap.CtxMax))
	}
	if snap.TokensUsed > 0 {
		return fmt.Sprintf(" · %s tok", FormatTokensShort(snap.TokensUsed))
	}
	return ""
}

// FormatTokensShort formats a token count compactly: "1234" → "1.2k",
// "1234567" → "1.2M". Used inside tool/spawn lines where horizontal
// space is at a premium — the full comma-separated form lives in the
// "↳ done" line at the end of the subagent run.
func FormatTokensShort(n int) string {
	switch {
	case n < 1000:
		return strconv.Itoa(n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000.0)
	default:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000.0)
	}
}

// ReadEventInt extracts an int from an event payload, tolerating the
// numeric types the event bus may marshal through (int / int64 /
// float64 round-trip via JSON).
func ReadEventInt(data map[string]interface{}, key string) int {
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

// ReadEventInt64 extracts an int64 from an event payload.
func ReadEventInt64(data map[string]interface{}, key string) int64 {
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

// FormatThousands renders an integer with comma separators (e.g.
// 1234567 → "1,234,567"). Negative numbers keep the sign.
func FormatThousands(n int) string {
	if n < 0 {
		return "-" + FormatThousands(-n)
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

// ReadEventDepth reads the subagent_depth from an event payload. Returns 0
// for missing or malformed values — matches today's "primary agent" rendering
// when older events that pre-date SP-051 metadata land in the bus.
func ReadEventDepth(data map[string]interface{}) int {
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

// ReadEventPersona reads the active_persona from an event payload, trimmed.
// Returns "" when absent — which suppresses the persona badge.
func ReadEventPersona(data map[string]interface{}) string {
	if data == nil {
		return ""
	}
	if s, ok := data["active_persona"].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}
