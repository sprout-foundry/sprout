package console

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// SelectItem is a single entry in a SelectList.
//
//	Label  — primary text shown to the user
//	Detail — optional dim-rendered suffix, right-aligned ("anthropic · 200k")
//	Value  — payload returned when the item is chosen
type SelectItem struct {
	Label  string
	Detail string
	Value  string
}

// SelectListOptions configures a SelectList run.
type SelectListOptions struct {
	// Title is rendered above the list, glyph-prefixed (GlyphInfo).
	Title string
	// Items is the full set to choose from. Filter narrows in place.
	Items []SelectItem
	// Searchable enables type-to-filter mode. Printable characters
	// append to the filter buffer and the list reranks against the
	// filter via the shared fuzzy matcher.
	Searchable bool
	// PageSize is how many rows of items are rendered at once. 0 picks
	// a sensible default (10).
	PageSize int
	// Footer is the hint line shown beneath the list (dim). When empty,
	// SelectList renders a default hint matching the current mode.
	Footer string
}

// SelectList drives a single-column picker UI. The zero value is
// unusable — construct via NewSelectList.
type SelectList struct {
	opts SelectListOptions

	mu       sync.Mutex
	cursor   int      // index into the filtered list
	filter   string   // current filter text (Searchable=true only)
	filtered []int    // indices into opts.Items, in display order
	offset   int      // scroll offset into filtered (top-of-page)
	rendered int      // number of rows we last drew (for in-place redraw)

	fd     int
	isTTY  bool
}

// NewSelectList constructs a picker with the given options. Items
// shorter than PageSize render compactly without scroll; longer lists
// page with arrow keys.
func NewSelectList(opts SelectListOptions) *SelectList {
	if opts.PageSize <= 0 {
		opts.PageSize = 10
	}
	fd := int(os.Stdin.Fd())
	s := &SelectList{
		opts:  opts,
		fd:    fd,
		isTTY: term.IsTerminal(fd),
	}
	s.applyFilter("")
	return s
}

// Run blocks until the user picks an item or cancels. Returns the
// selected item's Value and ok=true on confirm, or ("", false) on
// cancel (Esc / Ctrl+C). On non-TTY input, falls back to numbered-list
// + numeric stdin entry so the picker remains scriptable.
func (s *SelectList) Run(ctx context.Context) (string, bool, error) {
	if s == nil {
		return "", false, errors.New("select list: nil receiver")
	}
	if len(s.opts.Items) == 0 {
		return "", false, errors.New("select list: no items")
	}
	if !s.isTTY {
		return s.runFallback()
	}
	return s.runTTY(ctx)
}

// runFallback renders a numbered list to stdout and reads a number
// from stdin. Used when stdin isn't a TTY (piped input, CI) so the
// picker remains usable in scripts.
func (s *SelectList) runFallback() (string, bool, error) {
	if s.opts.Title != "" {
		GlyphInfo.Print(s.opts.Title)
	}
	for i, item := range s.opts.Items {
		label := item.Label
		if item.Detail != "" {
			label = fmt.Sprintf("%s  %s", label, item.Detail)
		}
		fmt.Printf("  %d) %s\n", i+1, label)
	}
	fmt.Printf("  Enter choice [1-%d, blank to cancel]: ", len(s.opts.Items))

	reader := bufio.NewReader(os.Stdin)
	raw, err := reader.ReadString('\n')
	if err != nil {
		return "", false, nil
	}
	choice := strings.TrimSpace(raw)
	if choice == "" {
		return "", false, nil
	}
	n, err := strconv.Atoi(choice)
	if err != nil || n < 1 || n > len(s.opts.Items) {
		return "", false, nil
	}
	return s.opts.Items[n-1].Value, true, nil
}

// runTTY drives the interactive picker. Returns when the user presses
// Enter (confirm) or Esc/Ctrl+C (cancel).
func (s *SelectList) runTTY(ctx context.Context) (string, bool, error) {
	st, err := enterSteerMode(s.fd)
	if err != nil {
		return "", false, fmt.Errorf("select list: enter raw mode: %w", err)
	}
	defer func() {
		_ = exitSteerMode(s.fd, st)
		s.clearRendered()
	}()

	s.render()

	var buf [8]byte
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()

	for {
		if ctx != nil {
			select {
			case <-ctx.Done():
				return "", false, ctx.Err()
			default:
			}
		}

		n, err := os.Stdin.Read(buf[:])
		if n == 0 {
			if err != nil && !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, io.EOF) {
				return "", false, err
			}
			<-ticker.C
			continue
		}

		// Handle the byte(s) we just read. Most actions are a single
		// byte; ESC and arrow-key sequences read 2-3 bytes inline.
		b := buf[0]
		switch {
		case b == 0x03: // Ctrl+C
			return "", false, nil
		case b == 0x0D, b == 0x0A: // Enter
			val, ok := s.confirm()
			return val, ok, nil
		case b == 0x1B: // Esc or arrow-key prefix
			done, val, ok := s.handleEscape(n, buf[:])
			if done {
				return val, ok, nil
			}
		case b == 0x7F, b == 0x08: // Backspace / DEL
			s.filterBackspace()
			s.render()
		case b >= 0x20 && b < 0x7F: // printable ASCII
			if s.opts.Searchable {
				s.filterAppend(string(b))
				s.render()
			}
		case b >= 0xC0: // UTF-8 lead byte
			if s.opts.Searchable {
				s.consumeUTF8(b, n, buf[:])
				s.render()
			}
		}
	}
}

// handleEscape dispatches the bytes that follow ESC. Returns done=true
// (with val/ok) when the user wants to cancel; done=false when the
// sequence was just an arrow key or other navigation that mutated
// cursor/filter state.
func (s *SelectList) handleEscape(n int, buf []byte) (done bool, val string, ok bool) {
	if n == 1 {
		// Could be a plain ESC or the start of a CSI sequence. Read
		// one more byte non-blockingly via a short poll; if nothing
		// arrives, treat as cancel.
		var follow [1]byte
		deadline := time.Now().Add(20 * time.Millisecond)
		for time.Now().Before(deadline) {
			m, _ := os.Stdin.Read(follow[:])
			if m == 1 {
				if follow[0] != '[' && follow[0] != 'O' {
					// Not a CSI sequence — treat ESC as cancel.
					return true, "", false
				}
				// Read the final byte of the CSI sequence (A/B/C/D for
				// arrows). For longer sequences (Page Up/Down etc.) we
				// drain until we see a final byte in 0x40..0x7E.
				return false, "", s.consumeCSI()
			}
			time.Sleep(2 * time.Millisecond)
		}
		// No follow-up byte → plain ESC means cancel.
		return true, "", false
	}
	// We got the whole sequence in one read.
	if n >= 3 && buf[1] == '[' {
		s.dispatchCSI(buf[2])
		s.render()
		return false, "", false
	}
	return true, "", false
}

// consumeCSI reads bytes from stdin until it finds the CSI final byte
// (0x40..0x7E), then dispatches based on it. Always returns false (no
// confirm/cancel — just navigation).
func (s *SelectList) consumeCSI() bool {
	var ch [1]byte
	deadline := time.Now().Add(50 * time.Millisecond)
	for time.Now().Before(deadline) {
		n, _ := os.Stdin.Read(ch[:])
		if n == 0 {
			time.Sleep(1 * time.Millisecond)
			continue
		}
		b := ch[0]
		if b >= 0x40 && b <= 0x7E {
			s.dispatchCSI(b)
			s.render()
			return false
		}
		// Parameter byte (0x30..0x3F) or intermediate (0x20..0x2F) —
		// keep reading.
	}
	return false
}

// dispatchCSI maps a final CSI byte onto a navigation action.
func (s *SelectList) dispatchCSI(final byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch final {
	case 'A': // Up
		s.moveCursor(-1)
	case 'B': // Down
		s.moveCursor(1)
	case 'H': // Home
		s.cursor = 0
		s.adjustOffset()
	case 'F': // End
		s.cursor = len(s.filtered) - 1
		if s.cursor < 0 {
			s.cursor = 0
		}
		s.adjustOffset()
	case '5': // PgUp prefix (CSI 5~) — but we already ate the final char
	case '6': // PgDn prefix
	}
}

// moveCursor changes the cursor position with bounds clamping and
// updates the scroll offset so the cursor stays visible. Must be
// called with s.mu held.
func (s *SelectList) moveCursor(delta int) {
	if len(s.filtered) == 0 {
		s.cursor = 0
		s.offset = 0
		return
	}
	s.cursor += delta
	if s.cursor < 0 {
		s.cursor = 0
	}
	if s.cursor >= len(s.filtered) {
		s.cursor = len(s.filtered) - 1
	}
	s.adjustOffset()
}

// adjustOffset moves the page-top so cursor is visible. Must be called
// with s.mu held.
func (s *SelectList) adjustOffset() {
	if s.cursor < s.offset {
		s.offset = s.cursor
		return
	}
	if s.cursor >= s.offset+s.opts.PageSize {
		s.offset = s.cursor - s.opts.PageSize + 1
	}
	if s.offset < 0 {
		s.offset = 0
	}
}

// filterAppend adds runes to the filter and refilters.
func (s *SelectList) filterAppend(text string) {
	s.mu.Lock()
	s.filter += text
	s.mu.Unlock()
	s.applyFilter(s.filter)
}

// filterBackspace removes the last rune from the filter.
func (s *SelectList) filterBackspace() {
	s.mu.Lock()
	if s.filter == "" {
		s.mu.Unlock()
		return
	}
	_, size := utf8.DecodeLastRuneInString(s.filter)
	s.filter = s.filter[:len(s.filter)-size]
	s.mu.Unlock()
	s.applyFilter(s.filter)
}

// consumeUTF8 collects the continuation bytes that follow a UTF-8
// lead byte and appends the resulting rune to the filter. n is the
// number of bytes already in buf[]; lead is at buf[0].
func (s *SelectList) consumeUTF8(lead byte, n int, buf []byte) {
	expected := utf8Width(lead)
	collected := buf[:n]
	deadline := time.Now().Add(30 * time.Millisecond)
	for len(collected) < expected && time.Now().Before(deadline) {
		var more [4]byte
		m, _ := os.Stdin.Read(more[:expected-len(collected)])
		if m > 0 {
			collected = append(collected, more[:m]...)
		} else {
			time.Sleep(1 * time.Millisecond)
		}
	}
	if r, _ := utf8.DecodeRune(collected); r != utf8.RuneError {
		s.filterAppend(string(r))
	}
}

// utf8Width returns the expected total byte count for a UTF-8 sequence
// given its lead byte. 1 for single-byte (shouldn't happen for our
// callers), 2/3/4 for multi-byte.
func utf8Width(b byte) int {
	switch {
	case b&0xE0 == 0xC0:
		return 2
	case b&0xF0 == 0xE0:
		return 3
	case b&0xF8 == 0xF0:
		return 4
	default:
		return 1
	}
}

// applyFilter recomputes the filtered slice from opts.Items using the
// current filter. Resets cursor/offset to 0 because positions don't
// translate across filter changes.
func (s *SelectList) applyFilter(filter string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.filter = filter
	s.filtered = s.filtered[:0]
	if filter == "" {
		for i := range s.opts.Items {
			s.filtered = append(s.filtered, i)
		}
	} else {
		needle := strings.ToLower(filter)
		for i, item := range s.opts.Items {
			hay := strings.ToLower(item.Label + " " + item.Detail)
			if strings.Contains(hay, needle) {
				s.filtered = append(s.filtered, i)
			}
		}
	}
	s.cursor = 0
	s.offset = 0
}

// confirm returns the value of the currently-selected filtered item.
// Returns ok=false if the filter excludes every item.
func (s *SelectList) confirm() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.filtered) == 0 || s.cursor < 0 || s.cursor >= len(s.filtered) {
		return "", false
	}
	return s.opts.Items[s.filtered[s.cursor]].Value, true
}

// render draws the current list state. Uses cursor-up + clear-to-EOL
// to overwrite the prior frame so the list updates in place.
func (s *SelectList) render() {
	s.mu.Lock()
	prev := s.rendered
	title := s.opts.Title
	filter := s.filter
	searchable := s.opts.Searchable
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
	// new frame overwrites the old without leaving residue.
	for i := 0; i < prev; i++ {
		fmt.Fprint(os.Stderr, "\r\033[K\033[A")
	}
	fmt.Fprint(os.Stderr, "\r\033[K")

	// Compute terminal width for right-aligning Detail.
	termWidth := 80
	if w, _, err := term.GetSize(int(os.Stderr.Fd())); err == nil && w > 20 {
		termWidth = w
	}

	rendered := 0
	if title != "" {
		fmt.Fprintln(os.Stderr, GlyphInfo.Prefix()+title)
		rendered++
	}
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
	// Pad label so detail right-aligns. Account for prefix (2 cells)
	// and a 2-cell gutter between label and detail.
	const gutter = 2
	available := termWidth - 2 - gutter - len(detail)
	if available < 8 {
		// Not enough room for detail; just append it inline.
		return prefix + labelOpen + label + labelClose + "  " + dimString(detail)
	}
	labelStr := label
	if len(labelStr) > available {
		labelStr = labelStr[:available-1] + "…"
	}
	pad := available - len(labelStr)
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
	s.mu.Unlock()
	for i := 0; i < n; i++ {
		fmt.Fprint(os.Stderr, "\r\033[K\033[A")
	}
	fmt.Fprint(os.Stderr, "\r\033[K")
}
