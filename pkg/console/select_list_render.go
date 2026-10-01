// Select list UI component — render, resize, and geometry helpers
// (split from select_list.go). These are the frame-drawing + in-place
// repaint path: render/renderLocked, renderSelectRow, dimString,
// clearRendered, handleResize, repaintIfResized, clampWalkBack, termRows.
// The picker state machine, input loop, and mouse/fallback paths stay in
// select_list.go.
package console

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// render draws the current list state. Uses cursor-up + clear-to-EOL
// to overwrite the prior frame so the list updates in place.
func (s *SelectList) render() {
	// Serialize against PrintExternal and other console chrome so
	// background messages can't interleave with the row-clear/write
	// sequence and leave duplicate rows on screen.
	LockOutput()
	defer UnlockOutput()
	s.renderLocked()
}

// renderLocked is the lock-free body of render. Caller MUST hold
// outputMu — mirrors the draw/drawLocked split in status_footer.go so
// the resize repaint path can reuse the body without re-entering the
// non-reentrant mutex.
func (s *SelectList) renderLocked() {
	s.mu.Lock()
	prev := s.rendered
	filter := s.filter
	searchable := s.opts.Searchable
	dismissOnAnyKey := s.opts.DismissOnAnyKey
	pageSize := s.opts.PageSize
	cursor := s.cursor
	offset := s.offset
	footer := s.opts.Footer
	filteredCount := len(s.filtered)
	totalCount := len(s.opts.Items)

	// Resolve the visible window of items, capturing label+detail
	// strings while we hold the lock so render proceeds without
	// touching s after Unlock().
	type row struct {
		label  string
		detail string
		active bool
	}
	end := offset + pageSize
	if end > filteredCount {
		end = filteredCount
	}
	rows := make([]row, 0, end-offset)
	for i := offset; i < end; i++ {
		idx := s.filtered[i]
		it := s.opts.Items[idx]
		rows = append(rows, row{
			label:  it.Label,
			detail: it.Detail,
			active: i == cursor,
		})
	}
	s.mu.Unlock()

	// Walk up over the previously-rendered rows and clear them so the
	// new frame overwrites the old without leaving residue. The clamp
	// bounds the walk to rows that physically exist after a resize —
	// walking past row 1 would wrap around and clear unrelated rows.
	walk := clampWalkBack(prev, termRows(os.Stderr.Fd()))
	for i := 0; i < walk; i++ {
		fmt.Fprint(os.Stderr, "\r\033[K\033[A")
	}
	fmt.Fprint(os.Stderr, "\r\033[K")

	// Compute terminal width for right-aligning Detail.
	termWidth := 80
	if w, _, err := term.GetSize(int(os.Stderr.Fd())); err == nil && w > 20 {
		termWidth = w
	}

	rendered := 0
	// Title is printed once in runTTY/runFallback before the render
	// loop starts — not re-rendered here. Reprinting it on every frame
	// caused duplicate stacking when the terminal subscriber wrote
	// output between keypresses, misaligning the row-clear math.
	if searchable {
		fmt.Fprintf(os.Stderr, "  filter: %s_  (%d/%d)\n", filter, filteredCount, totalCount)
		rendered++
	}

	if filteredCount == 0 {
		GlyphDim.Fprintln(os.Stderr, "(no matches)")
		rendered++
	}

	for _, r := range rows {
		line := renderSelectRow(r.label, r.detail, r.active, termWidth)
		fmt.Fprintln(os.Stderr, line)
		rendered++
	}

	// Footer hint
	hint := footer
	if hint == "" {
		if searchable {
			hint = "↑↓ select · enter confirm · type to filter · esc cancel"
		} else if dismissOnAnyKey {
			hint = "↑↓ select · enter confirm · any other key dismiss"
		} else {
			hint = "↑↓ select · enter confirm · esc cancel"
		}
	}
	GlyphDim.Fprintln(os.Stderr, hint)
	rendered++

	s.mu.Lock()
	s.rendered = rendered
	s.mu.Unlock()
}

// renderSelectRow formats a single row with optional right-aligned
// Detail.  The active row gets a heavier visual treatment than before
// (filled-arrow prefix + bold label) so selection is obvious at a glance:
//
//	  Inactive label                                detail
//	❯ Active label (bold)                          detail
//
// The prefix occupies 2 visible cells in both states so the label column
// stays aligned.  In NO_COLOR mode the bold escape is dropped but the
// filled arrow still differentiates the active row.
func renderSelectRow(label, detail string, active bool, termWidth int) string {
	useColor := envutil.ResolveColorPreference(true)

	var prefix, labelOpen, labelClose string
	if active {
		if useColor {
			// Bold bright-cyan filled arrow + bold label.  Matches the
			// GlyphAction color so picker selection looks consistent
			// with action-in-flight indicators elsewhere in the CLI.
			prefix = "\033[1;96m❯\033[0m "
			labelOpen = "\033[1m"
			labelClose = "\033[0m"
		} else {
			prefix = "❯ "
		}
	} else {
		prefix = "  "
	}

	if detail == "" {
		return prefix + labelOpen + label + labelClose
	}
	// Pad label so detail right-aligns. Account for prefix (2 cells) and a
	// 2-cell gutter between label and detail. Measure in display columns so
	// wide/CJK labels and details align correctly and aren't split mid-rune.
	const gutter = 2
	available := termWidth - 2 - gutter - displayWidth(detail)
	if available < 8 {
		// Not enough room for detail; just append it inline.
		return prefix + labelOpen + label + labelClose + "  " + dimString(detail)
	}
	labelStr := label
	if displayWidth(labelStr) > available {
		labelStr = truncateToWidth(label, available, "…")
	}
	pad := available - displayWidth(labelStr)
	if pad < 0 {
		pad = 0
	}
	return prefix + labelOpen + labelStr + labelClose + strings.Repeat(" ", pad+gutter) + dimString(detail)
}

// dimString wraps text in the GlyphDim color escape (or returns it
// unchanged when color is disabled).
func dimString(s string) string {
	if s == "" {
		return s
	}
	prefix := GlyphDim.color()
	if prefix == "" {
		return s
	}
	return prefix + s + ansiReset
}

// clearRendered erases the rendered frame on exit so the picker
// doesn't leave detritus in scrollback.
func (s *SelectList) clearRendered() {
	s.mu.Lock()
	n := s.rendered
	s.rendered = 0
	hasTitle := s.opts.Title != ""
	s.mu.Unlock()
	LockOutput()
	defer UnlockOutput()
	// +1 for the title row (printed once in runTTY, not tracked in rendered)
	if hasTitle {
		n++
	}
	// Clamp to the physical height: a shrink may have left the tracked
	// row count describing rows that no longer exist, and an unclamped
	// walk-back would clear unrelated content above the prompt.
	walk := clampWalkBack(n, termRows(os.Stderr.Fd()))
	for i := 0; i < walk; i++ {
		fmt.Fprint(os.Stderr, "\r\033[K\033[A")
	}
	fmt.Fprint(os.Stderr, "\r\033[K")
}

// handleResize is the RegisterResizeSubscriber callback. It never
// draws on the SIGWINCH goroutine unless the output lock is free —
// contending for it would deadlock against the read loop's render.
// When the lock is held, the flag defers the repaint to the read
// loop's next idle tick.
func (s *SelectList) handleResize(width int) {
	s.mu.Lock()
	s.resized = true
	s.mu.Unlock()
	if !TryLockOutput() {
		return
	}
	defer UnlockOutput()
	s.mu.Lock()
	s.resized = false
	s.mu.Unlock()
	s.renderLocked()
}

// repaintIfResized consumes the resized flag and repaints the frame.
// Called from the read loop's idle tick.
func (s *SelectList) repaintIfResized() {
	s.mu.Lock()
	flagged := s.resized
	s.resized = false
	s.mu.Unlock()
	if !flagged {
		return
	}
	LockOutput()
	defer UnlockOutput()
	s.renderLocked()
}

// clampWalkBack bounds a cursor-relative walk-back so it never ascends
// past row 1. rows is the current terminal height; non-positive or
// unknown heights clamp to 0 — callers treat that as "clear nothing,
// redraw from the current row".
func clampWalkBack(prev, rows int) int {
	if prev <= 0 || rows <= 1 {
		return 0
	}
	if prev > rows-1 {
		return rows - 1
	}
	return prev
}

// termRows returns the current terminal height for fd, 0 when unknown.
func termRows(fd uintptr) int {
	_, r, err := term.GetSize(int(fd))
	if err != nil || r <= 0 {
		return 0
	}
	return r
}
