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
	termWidth := selectTermWidth()
	s.mu.Lock()
	prevWidths := s.renderedWidths
	s.mu.Unlock()

	// Walk up over the previously-rendered rows and clear them so the
	// new frame overwrites the old without leaving residue. The clamp
	// bounds the walk to rows that physically exist after a resize —
	// walking past row 1 would wrap around and clear unrelated rows.
	clearRowsAbove(rowsOnScreen(prevWidths, termWidth))
	s.printFrameLocked(s.frameLines(termWidth))
}

// printFrameLocked writes frame lines at the cursor and records their
// widths for the next walk-back. Caller must hold outputMu.
func (s *SelectList) printFrameLocked(lines []string) {
	widths := make([]int, len(lines))
	for i, line := range lines {
		fmt.Fprintln(os.Stderr, line)
		widths[i] = displayWidth(line)
	}
	s.mu.Lock()
	s.renderedWidths = widths
	s.mu.Unlock()
}

// selectTermWidth is the width rows are laid out for (80 when unknown).
func selectTermWidth() int {
	if w, _, err := term.GetSize(int(os.Stderr.Fd())); err == nil && w > 20 {
		return w
	}
	return 80
}

// frameLines builds the frame below the title: filter row, items, hint.
func (s *SelectList) frameLines(termWidth int) []string {
	s.mu.Lock()
	filter := s.filter
	searchable := s.opts.Searchable
	dismissOnAnyKey := s.opts.DismissOnAnyKey
	pageSize := s.opts.PageSize
	cursor := s.cursor
	offset := s.offset
	footer := s.opts.Footer
	hintOnFooter := s.hintOnFooter
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
			label:  s.keyedLabel(it),
			detail: it.Detail,
			active: i == cursor,
		})
	}
	s.mu.Unlock()

	var lines []string
	emit := func(line string) { lines = append(lines, line) }
	// Title is printed once in runTTY/runFallback before the render
	// loop starts — not re-rendered here. Reprinting it on every frame
	// caused duplicate stacking when the terminal subscriber wrote
	// output between keypresses, misaligning the row-clear math.
	if searchable {
		emit(fmt.Sprintf("  filter: %s_  (%d/%d)", filter, filteredCount, totalCount))
	}

	if filteredCount == 0 {
		emit(GlyphDim.Prefix() + "(no matches)")
	}

	for _, r := range rows {
		emit(renderSelectRow(r.label, r.detail, r.active, min(termWidth, maxSelectRowWidth)))
	}

	if hintOnFooter {
		return lines
	}
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
	emit(GlyphDim.Prefix() + hint)
	return lines
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
	widths := s.renderedWidths
	s.renderedWidths = nil
	title := s.opts.Title
	s.mu.Unlock()
	LockOutput()
	defer UnlockOutput()
	// The title row is printed once in runTTY, not tracked with the rows.
	if title != "" {
		widths = append([]int{displayWidth(GlyphInfo.Prefix() + title)}, widths...)
	}
	cols := 80
	if w, _, err := term.GetSize(int(os.Stderr.Fd())); err == nil && w > 0 {
		cols = w
	}
	// Clamp to the physical height: a shrink may have left the tracked
	// row count describing rows that no longer exist, and an unclamped
	// walk-back would clear unrelated content above the prompt.
	n := clampWalkBack(rowsOnScreen(widths, cols), termRows(os.Stderr.Fd()))
	clearRowsAbove(n)

	s.mu.Lock()
	footerDriven := s.footerDriven
	s.mu.Unlock()
	if f := GetGlobalStatusFooter(); footerDriven && f != nil {
		pullDownOverClearedRows(f, n+1)
	}
}

// pullDownOverClearedRows closes the hole a dismissed picker leaves. With a
// footer the picker sits on the scroll region's last rows, so clearing it
// strands the prompt mid-screen above that many blank rows — which a later
// resize turns into a gap when the prompt moves to the bottom. Scrolling
// the region down by the cleared height brings the conversation down to
// meet the bottom row, where the prompt then goes. Caller holds outputMu.
func pullDownOverClearedRows(f *StatusFooter, n int) {
	_, rows := f.terminalSize()
	bottom := rows - f.reservedRows()
	if n < 1 || bottom <= n {
		return
	}
	fmt.Fprintf(os.Stderr, "\033[1;1H%s\033[%d;1H", strings.Repeat("\033M", n-1), bottom)
}

func (s *SelectList) isFooterDriven() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.footerDriven
}

// printExternalLocked prints a background line above the picker: the frame
// is erased, the line written where it stood, and the frame redrawn below.
// Printed after the frame instead, the line would sit inside the rows each
// redraw walks back over, one row the picker does not count — the next
// redraw then stops a row short and strands a copy of the title. Caller
// must hold outputMu.
func (s *SelectList) printExternalLocked(msg string) {
	cols := selectTermWidth()
	clearRowsAbove(rowsOnScreen(s.framedWidths(), cols))
	if !strings.HasSuffix(msg, "\n") {
		msg += "\n"
	}
	fmt.Fprint(os.Stderr, msg)
	if s.opts.Title != "" {
		fmt.Fprintln(os.Stderr, GlyphInfo.Prefix()+s.opts.Title)
	}
	s.printFrameLocked(s.frameLines(cols))
}

// clearRowsAbove erases the cursor's row and the n rows above it, leaving
// the cursor on the topmost. The walk is clamped to the screen height: a
// count describing rows that no longer exist must not clear past row 1.
func clearRowsAbove(n int) {
	walk := clampWalkBack(n, termRows(os.Stderr.Fd()))
	for i := 0; i < walk; i++ {
		fmt.Fprint(os.Stderr, "\r\033[K\033[A")
	}
	fmt.Fprint(os.Stderr, "\r\033[K")
}

// framedWidths is the title's width (when there is one) followed by the
// rendered rows' widths: everything the picker has on screen.
func (s *SelectList) framedWidths() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	widths := s.renderedWidths
	if s.opts.Title != "" {
		widths = append([]int{displayWidth(GlyphInfo.Prefix() + s.opts.Title)}, widths...)
	}
	return widths
}

// clearForResizeLocked erases the picker, title included, as the first step
// of the footer's resize. The terminal keeps the cursor on the line just
// below the picker through a resize, so the rows are found relative to it
// at the new width. Caller must hold outputMu.
func (s *SelectList) clearForResizeLocked(cols int) {
	clearRowsAbove(rowsOnScreen(s.framedWidths(), cols))
	s.mu.Lock()
	s.renderedWidths = nil
	s.mu.Unlock()
}

// renderAnchoredLocked redraws title and frame so the cursor ends on
// bottom, the scroll region's last row, as it does when the picker first
// opens. The footer's resize parks the cursor there, so a redraw relative
// to the cursor would land below the old frame instead of over it. Caller
// must hold outputMu.
func (s *SelectList) renderAnchoredLocked(bottom, cols int) {
	if cols <= 20 {
		cols = selectTermWidth()
	}
	lines := s.frameLines(cols)
	titleLine := ""
	if s.opts.Title != "" {
		titleLine = GlyphInfo.Prefix() + s.opts.Title
	}
	n := 0
	for _, l := range append([]string{titleLine}, lines...) {
		if l != "" {
			n += physicalRows(displayWidth(l), cols)
		}
	}
	fmt.Fprintf(os.Stderr, "\033[%d;1H", max(bottom-n, 1))
	if titleLine != "" {
		fmt.Fprintln(os.Stderr, titleLine)
	}
	s.printFrameLocked(lines)
}

// handleResize is the RegisterResizeSubscriber callback. It never
// draws on the SIGWINCH goroutine unless the output lock is free —
// contending for it would deadlock against the read loop's render.
// When the lock is held, the flag defers the repaint to the read
// loop's next idle tick.
func (s *SelectList) handleResize(width int) {
	s.mu.Lock()
	if s.footerDriven {
		// The footer's Resize already cleared and redrew the picker.
		s.mu.Unlock()
		return
	}
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

// maxSelectRowWidth caps how far the right-aligned detail column sits from
// its label. Narrower rows also survive a moderate window shrink without
// the terminal rewrapping them.
const maxSelectRowWidth = 100

// rowsOnScreen is how many terminal rows lines of the given display widths
// occupy at cols. Rows are padded out to the terminal's width (the detail
// column is right-aligned), so when the window narrows the terminal rewraps
// each onto several rows. A walk-back that counts only the lines it printed
// stops partway up and the redraw stacks a copy below the old frame.
func rowsOnScreen(widths []int, cols int) int {
	n := 0
	for _, w := range widths {
		n += physicalRows(w, cols)
	}
	return n
}

// keyedLabel prefixes an item's label with its shortcut key. Once any item
// has a key, keyless ones are indented to match so the labels line up.
func (s *SelectList) keyedLabel(it SelectItem) string {
	if it.Key != 0 {
		return "[" + string(it.Key) + "] " + it.Label
	}
	for _, other := range s.opts.Items {
		if other.Key != 0 {
			return "    " + it.Label
		}
	}
	return it.Label
}
