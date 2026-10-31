package cliui

// restore_render.go — show the tail of a resumed session (SP-171g).
//
// Resuming a session loaded its state but rendered nothing: the user
// arrived at an empty screen holding a conversation they could not see, and
// had to ask the model to recap. The transcript is the context. This renders
// the last N conversation lines into the scroll region on restore, so the
// session starts where it left off.

import (
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// RestoreTailLines is how many rendered lines of a resumed session are
// reprinted. Chosen to fill a typical terminal's scrollback without
// burying the prompt; the rest stays reachable through /transcript.
const RestoreTailLines = 200

// RestoreTailMessageCount caps how many messages are considered before
// wrapping to lines, so a session of tiny messages still fills the tail.
const RestoreTailMessageCount = 60

// RenderSessionTail reprints the end of a resumed conversation. It walks
// backwards accumulating rendered lines until it has RestoreTailLines, then
// prints them oldest-first so the newest content sits just above the prompt.
//
// Tool messages are skipped: their content is machine payload, not
// conversation, and reprinting it would bury the human turns the user
// actually wants. A one-line count of the skipped tool calls keeps the
// record honest about what is not shown.
func RenderSessionTail(messages []api.Message) {
	lines, skippedTools := collectTailLines(messages, RestoreTailLines)
	if len(lines) == 0 {
		return
	}

	console.SectionBreak()
	console.WriteTranscript(fmt.Sprintf("%s resumed conversation — last %d lines\n",
		console.GlyphInfo.Prefix(), len(lines)))
	if skippedTools > 0 {
		console.WriteTranscript(fmt.Sprintf("  (%d tool result%s not shown — /transcript for the full session)\n",
			skippedTools, plural(skippedTools)))
	}
	console.SectionBreak()
	for _, line := range lines {
		console.WriteTranscript(line + "\n")
	}
	console.SectionBreak()
}

// collectTailLines walks the conversation backwards, rendering each message
// into lines, and returns the last `limit` lines in chronological order.
func collectTailLines(messages []api.Message, limit int) ([]string, int) {
	if limit <= 0 || len(messages) == 0 {
		return nil, 0
	}
	cols := console.StdoutColumns()
	if cols <= 0 {
		cols = 80
	}
	colorOn := envutil.ResolveColorPreference(true)

	// Render newest-first, then reverse at the end.
	var collected []string
	skippedTools := 0
	considered := 0
	for i := len(messages) - 1; i >= 0 && len(collected) < limit; i-- {
		if considered >= RestoreTailMessageCount && len(collected) > 0 {
			break
		}
		considered++
		msg := messages[i]
		switch msg.Role {
		case "tool":
			skippedTools++
			continue
		case "user":
			text := strings.TrimSpace(agent.StripUserMessageTimestamp(msg.Content))
			if text == "" {
				continue
			}
			collected = append(collected, renderTailBlock("you", text, cols, colorOn)...)
		case "assistant":
			text := strings.TrimSpace(msg.Content)
			if text == "" {
				continue
			}
			collected = append(collected, renderTailBlock("assistant", text, cols, colorOn)...)
		}
	}

	if len(collected) > limit {
		collected = collected[:limit]
	}
	// Reverse into chronological order.
	for i, j := 0, len(collected)-1; i < j; i, j = i+1, j-1 {
		collected[i], collected[j] = collected[j], collected[i]
	}
	return collected, skippedTools
}

// renderTailBlock renders one message as a header line plus indented,
// width-wrapped body lines.
func renderTailBlock(label, text string, cols int, colorOn bool) []string {
	var out []string
	if colorOn {
		out = append(out, fmt.Sprintf("\033[2m▌ %s\033[0m", label))
	} else {
		out = append(out, "▌ "+label)
	}
	body := console.WrapIndented(text, "  ", cols)
	out = append(out, strings.Split(strings.TrimRight(body, "\n"), "\n")...)
	return out
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
