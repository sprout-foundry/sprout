//go:build !js

package cliui

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/envutil"
	"github.com/sprout-foundry/sprout/pkg/notify"
	"golang.org/x/term"
)

// NotifyTurnCompletion emits a terminal bell and/or OS notification when a turn
// completes after exceeding the configured minimum duration. Suppressed in
// non-interactive sessions, when --skip-prompt is set, or for fast turns.
// SP-070-2.
func NotifyTurnCompletion(chatAgent *agent.Agent, turnStart time.Time, skipPrompt bool) {
	// Skip if non-interactive
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return
	}
	// Skip if --skip-prompt
	if skipPrompt {
		return
	}
	// Get config
	mgr := chatAgent.GetConfigManager()
	if mgr == nil {
		return
	}
	cfg := mgr.GetConfig()
	if cfg == nil || cfg.Notifications == nil {
		return
	}
	notif := cfg.Notifications.Resolve()
	// Check minimum duration
	if time.Since(turnStart).Seconds() < notif.MinSeconds {
		return
	}
	// Emit bell to stderr
	fmt.Fprint(os.Stderr, "\a")
	// OS notification if configured
	if notif.OSNotify {
		_ = notify.New().Notify("Sprout", "Task complete")
	}
}

// PrintAssistantHeader writes the dim "▌ assistant · <model>" header that
// marks the start of an assistant turn. Honors NO_COLOR via the existing
// color preference resolver. The brand cyan `▌` aligns visually with the
// glyph vocabulary in pkg/console; the model name sits in dim grey so the
// eye is drawn to the bar, not the metadata.
func PrintAssistantHeader(model string) {
	model = ShortModelName(model)
	colorOn := envutil.ResolveColorPreference(true)
	if !colorOn {
		fmt.Printf("▌ assistant · %s\n", model)
		return
	}
	fmt.Printf("\033[1;96m▌\033[0m \033[2massistant · %s\033[0m\n", model)
}

// ShouldShowTurnStats returns true when stderr is connected to a TTY.
// The turn-summary line is written to os.Stderr, so we must check stderr
// (not stdout) to determine whether it will render cleanly. This matters
// in piping scenarios like `sprout agent "query" > output.txt` where
// stdout is piped but stderr is still the terminal. SP-048-5a.
func ShouldShowTurnStats() bool {
	return term.IsTerminal(int(os.Stderr.Fd()))
}

// FormatTurnStatsLine builds the dim single-line turn-summary string.
// When color is disabled (NO_COLOR), ANSI dim codes are stripped.
// SP-048-5a.
//
// ttft (time to first token) is rendered as a separate segment when
// non-zero. Threshold coloring (yellow >2s, red >5s) makes slow
// provider connections visible at a glance — they're the most common
// cause of "is sprout stuck?" perception even when the actual model
// run is fast once it starts streaming.
func FormatTurnStatsLine(promptDelta, completionDelta int, costDelta float64, elapsed, ttft time.Duration) string {
	colorOn := envutil.ResolveColorPreference(true)
	var dim, reset string
	if colorOn {
		dim, reset = "\033[2m", "\033[0m"
	}

	ttftSeg := ""
	if ttft > 0 {
		ttftStr := CompactDuration(ttft)
		styled := ttftStr
		if colorOn {
			switch {
			case ttft > 5*time.Second:
				// Pop out of dim into red for the duration of this segment,
				// then drop back into dim so the rest of the line stays muted.
				styled = reset + "\033[31m" + ttftStr + reset + dim
			case ttft > 2*time.Second:
				styled = reset + "\033[33m" + ttftStr + reset + dim
			}
		}
		ttftSeg = fmt.Sprintf(" · ttft %s", styled)
	}

	costSeg := CompactCost(costDelta)
	if costDelta <= 0 {
		costSeg = "" // omit "$0.0000" for models without pricing
	}

	return fmt.Sprintf("%s⎯ this turn: %s in / %s out%s%s · %s%s ⎯%s\n",
		dim,
		CompactTokens(promptDelta),
		CompactTokens(completionDelta),
		CostPrefix(costSeg),
		costSeg,
		CompactDuration(elapsed),
		ttftSeg,
		reset,
	)
}

// CostPrefix returns " · " when cost is non-empty, "" when empty, so
// the turn-summary line omits the cost segment cleanly for models
// without pricing.
func CostPrefix(cost string) string {
	if cost == "" {
		return ""
	}
	return " · "
}

// TurnFirstTokenAt is set (atomically) to the Unix nano time of the
// first non-empty stream chunk in the current turn. Read by
// PrintPerTurnSummary to compute time-to-first-token, then reset to 0
// at the start of each turn. Package-level so the streaming callback
// in SetupAgentEvents (no agent-state to hang it on) can flip it.
var TurnFirstTokenAt int64

// NoteFirstStreamChunk is invoked once per turn from the streaming
// callback. CompareAndSwap ensures only the very first non-empty chunk
// updates the timestamp — later chunks are no-ops.
func NoteFirstStreamChunk() {
	atomic.CompareAndSwapInt64(&TurnFirstTokenAt, 0, time.Now().UnixNano())
}

// ResetTurnFirstToken clears the ttft tracker. Called by the REPL just
// before submitting a turn so each turn's measurement is independent.
func ResetTurnFirstToken() {
	atomic.StoreInt64(&TurnFirstTokenAt, 0)
}

// PrintPerTurnSummary emits a dim single-line summary of what just happened
// in the LLM round-trip: input/output tokens consumed, $ spent, elapsed
// wall time, plus ttft when available. Silent when no tokens were used
// (e.g. the turn was a slash command or zsh fast path). Only shown when
// stderr is a TTY (respects NO_COLOR for ANSI codes). SP-048-5a.
func PrintPerTurnSummary(chatAgent *agent.Agent, start time.Time, promptBefore, completionBefore int) {
	if !ShouldShowTurnStats() {
		return
	}
	promptDelta := chatAgent.GetPromptTokens() - promptBefore
	completionDelta := chatAgent.GetCompletionTokens() - completionBefore
	if promptDelta <= 0 && completionDelta <= 0 {
		return
	}
	elapsed := time.Since(start)

	var ttft time.Duration
	if firstAt := atomic.LoadInt64(&TurnFirstTokenAt); firstAt > 0 {
		ttft = time.Duration(firstAt - start.UnixNano())
		if ttft < 0 {
			ttft = 0
		}
	}

	fmt.Fprint(os.Stderr, FormatTurnStatsLine(promptDelta, completionDelta, 0, elapsed, ttft))
}

// CompactTokens formats token counts compactly.
func CompactTokens(n int) string {
	if n < 0 {
		n = 0
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// CompactCost formats a cost value compactly.
func CompactCost(c float64) string {
	switch {
	case c < 0:
		return "$0.00"
	case c < 0.01:
		return fmt.Sprintf("$%.4f", c)
	case c < 1.0:
		return fmt.Sprintf("$%.3f", c)
	default:
		return fmt.Sprintf("$%.2f", c)
	}
}

// CompactDuration formats a time.Duration compactly.
func CompactDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	mins := int(d / time.Minute)
	secs := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%dm%ds", mins, secs)
}

// ShortModelName strips the lab/org prefix from a model ID for display.
// "deepseek-ai/DeepSeek-V4-Flash" → "DeepSeek-V4-Flash"
// "meta-llama/Llama-3.3-70B-Instruct" → "Llama-3.3-70B-Instruct"
// "glm-4.6" → "glm-4.6" (no slash, returned as-is)
func ShortModelName(model string) string {
	if i := strings.LastIndex(model, "/"); i >= 0 {
		return model[i+1:]
	}
	return model
}

// BuildPromptPrefix returns the interactive REPL prompt for the given
// model. SP-048-5d. Format: "<model> ▸ " when a model name is available,
// "sprout> " as the legacy fallback when it isn't.
func BuildPromptPrefix(model string) string {
	model = strings.TrimSpace(ShortModelName(model))
	if model == "" {
		return "sprout> "
	}
	return model + " ▸ "
}
