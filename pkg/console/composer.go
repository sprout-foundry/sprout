package console

import (
	"strings"

	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// ComposerMode is what Enter does with the text in the pinned input box.
// The idle prompt and the mid-turn steer/queue input share one look; only
// the label, accent and placeholder change with the mode.
type ComposerMode int

const (
	ComposerIdle  ComposerMode = iota // between turns: Enter sends a message
	ComposerSteer                     // mid-turn: Enter steers the running turn
	ComposerQueue                     // mid-turn: Enter queues for after the turn
)

// ComposerPrefix is the label the box's first row starts with.
func ComposerPrefix(mode ComposerMode) string {
	switch mode {
	case ComposerSteer:
		return "steer › "
	case ComposerQueue:
		return "queue › "
	default:
		return "› "
	}
}

func composerPlaceholder(mode ComposerMode) string {
	switch mode {
	case ComposerSteer:
		return "Steer the current turn…"
	case ComposerQueue:
		return "Queue a message for when this turn ends…"
	default:
		return "Message sprout…  (/ commands)"
	}
}

func composerAccent(mode ComposerMode) string {
	if mode == ComposerQueue {
		return ColorBold + ColorYellow
	}
	return ColorBold + ColorBrightCyan
}

// styleComposerRow styles one rendered row of the input box: the mode's
// label in its accent, typed text in the terminal's normal color (readable,
// unlike the old all-bold-cyan row), and when the box is empty a dim
// placeholder after the caret. rendered is the row from
// steerRowTextWithCursor, caret already placed and padded to the width.
func styleComposerRow(rendered string, firstRow, empty bool, mode ComposerMode, cols int) string {
	accent := composerAccent(mode)
	prefix := ComposerPrefix(mode)
	colorOn := envutil.ResolveColorPreference(true)
	body := rendered
	head := ""
	if firstRow && strings.HasPrefix(body, prefix) {
		head = prefix
		if colorOn {
			head = accent + prefix + ColorReset
		}
		body = body[len(prefix):]
	}
	if empty && firstRow {
		caret := caretGlyph
		if colorOn {
			caret = caretOn + " " + caretOff
		}
		if strings.HasPrefix(body, caret) {
			room := cols - displayWidth(prefix) - 1
			hint := truncateToWidth(composerPlaceholder(mode), max(room, 0), "…")
			pad := strings.Repeat(" ", max(room-displayWidth(hint), 0))
			if !colorOn {
				return head + caret + hint + pad
			}
			return head + caret + ColorDim + hint + ColorReset + pad
		}
	}
	return head + body
}

// composerRule is the dim rule drawn above the input box, separating it
// from the conversation scrolling above.
func composerRule(cols int) string {
	if cols <= 0 {
		return ""
	}
	line := strings.Repeat("─", cols)
	if !envutil.ResolveColorPreference(true) {
		return line
	}
	return ColorDim + line + ColorReset
}
