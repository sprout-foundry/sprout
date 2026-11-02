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

	// The tail is anchored to the region's last row, so the header and the
	// newest lines are the ones left on screen. Written from wherever the
	// cursor happens to be, a tail taller than the region scrolls its own
	// beginning — the resumed-conversation header and the oldest lines of
	// the tail — straight into scrollback, and the user resumes looking at
	// the middle of the conversation.
	header := fmt.Sprintf("%s resumed conversation — last %d lines\n",
		console.GlyphInfo.Prefix(), len(lines))
	if skippedTools > 0 {
		header += fmt.Sprintf("  (%d tool result%s not shown — /transcript for the full session)\n",
			skippedTools, plural(skippedTools))
	}
	body := strings.Join(lines, "\n") + "\n"
	startRow := console.TranscriptStartRow(strings.Count(header+body, "\n"))

	// The whole block goes through the anchored write. Writing the body
	// line by line would re-park the cursor on the region's last row before
	// every line and throw the anchor away, scrolling the block's head off
	// screen exactly as an unanchored write does.
	console.SectionBreak()
	console.WriteTranscriptAtRow(startRow, header+body)
	console.SectionBreak()
}

// collectTailLines walks the conversation backwards, rendering each message
// into a block of lines, and returns the last `limit` lines in chronological
// order. Blocks are kept whole and in their own line order — only the
// sequence of blocks is reversed — and a blank row separates them, as it
// does between live turns. When the limit cuts into the oldest block shown,
// that block keeps its label and says it was cut.
func collectTailLines(messages []api.Message, limit int) ([]string, int) {
	if limit <= 0 || len(messages) == 0 {
		return nil, 0
	}
	cols := console.StdoutColumns()
	if cols <= 0 {
		cols = 80
	}
	colorOn := envutil.ResolveColorPreference(true)
	md := console.NewMarkdownFormatter(true, true).SetWidth(cols - len(tailIndent))

	var blocks [][]string // newest first
	total := 0
	skippedTools := 0
	considered := 0
	for i := len(messages) - 1; i >= 0 && total < limit; i-- {
		if considered >= RestoreTailMessageCount && total > 0 {
			break
		}
		considered++
		msg := messages[i]
		var block []string
		switch msg.Role {
		case "tool":
			skippedTools++
			continue
		case "user":
			text := strings.TrimSpace(agent.StripUserMessageTimestamp(msg.Content))
			if text == "" {
				continue
			}
			block = renderTailBlock(messageHeader("you", false, colorOn), text, cols)
		case "assistant":
			text := strings.TrimSpace(msg.Content)
			if text == "" {
				continue
			}
			block = renderTailBlock(messageHeader("assistant", true, colorOn), md.Format(text), cols)
		default:
			continue
		}
		if len(blocks) > 0 {
			block = append(block, "") // the separator above the newer block
		}
		blocks = append(blocks, block)
		total += len(block)
	}
	if len(blocks) == 0 {
		return nil, skippedTools
	}

	// Trim the oldest block to fit, keeping its label.
	if over := total - limit; over > 0 {
		oldest := blocks[len(blocks)-1]
		// header + "…" + the kept tail lines (the last being the separator);
		// a cut that would leave no body line drops the block instead.
		if keep := len(oldest) - over - 2; keep >= 2 {
			trimmed := []string{oldest[0], tailIndent + "…"}
			trimmed = append(trimmed, oldest[len(oldest)-keep:]...)
			blocks[len(blocks)-1] = trimmed
		} else {
			blocks = blocks[:len(blocks)-1]
		}
	}

	var lines []string
	for i := len(blocks) - 1; i >= 0; i-- {
		lines = append(lines, blocks[i]...)
	}
	if len(lines) > limit { // a single newest block taller than the limit
		lines = append([]string{lines[0], tailIndent + "…"}, lines[len(lines)-limit+2:]...)
	}
	return lines, skippedTools
}

const tailIndent = "  "

// renderTailBlock renders one message as its header line plus its body,
// indented and width-wrapped as the live transcript renders it.
func renderTailBlock(header, body string, cols int) []string {
	out := []string{header}
	wrapped := console.WrapIndented(body, tailIndent, cols)
	return append(out, strings.Split(strings.TrimRight(wrapped, "\n"), "\n")...)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
