// Select list UI component — types and rendering
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
	// DismissOnAnyKey makes any printable key (that isn't navigation or
	// Enter) dismiss the picker with ("", false, nil). Useful for
	// "press any key to continue"-style dismissal so the user doesn't
	// have to reach for Esc or Enter. Ignored when Searchable is true.
	DismissOnAnyKey bool
}

// SelectList drives a single-column picker UI. The zero value is
// unusable — construct via NewSelectList.
type SelectList struct {
	opts SelectListOptions

	mu       sync.Mutex
	cursor   int    // index into the filtered list
	filter   string // current filter text (Searchable=true only)
	filtered []int  // indices into opts.Items, in display order
	offset   int    // scroll offset into filtered (top-of-page)
	rendered int    // number of rows we last drew (for in-place redraw)

	fd    int
	isTTY bool

	// testOut, when non-nil, overrides the destination for mouse-tracking
	// escape sequences so tests can capture the emitted bytes. It is a
	// test seam only; production code leaves it nil and sequences are
	// written to os.Stderr.
	testOut io.Writer

	// dismissKey holds the printable text of the key that dismissed
	// the picker under DismissOnAnyKey. Empty when the picker exited
	// via Enter/Esc/Ctrl+C or when DismissOnAnyKey is off. Callers
	// that want to forward the dismissed keystroke (e.g. pre-filling
	// the REPL input buffer) should read it via DismissKey().
	dismissKey string

	// fallbackReader is the reader for the non-TTY numbered-list
	// fallback. os.Stdin by default; tests inject a pipe so they can
	// drive the fallback path hermetically.
	fallbackReader io.Reader

	// resized is set by the resize subscriber (handleResize) so the
	// read loop repaints the frame after a SIGWINCH. The footer's own
	// resize handler teleports the cursor, which stales this picker's
	// cursor-relative walk-back math until the next repaint.
	resized bool

	// lastEnterProcessed tracks whether we've already processed an Enter
	// key to avoid re-processing multi-byte sequences like \r\n.
	lastEnterProcessed bool
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
		opts:           opts,
		fd:             fd,
		isTTY:          term.IsTerminal(fd),
		fallbackReader: os.Stdin,
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
		return s.runFallback(ctx)
	}
	return s.runTTY(ctx)
}

// runFallback renders a numbered list to stdout and reads a number
// from stdin. Used when stdin isn't a TTY (piped input, CI) so the
// picker remains usable in scripts.
func (s *SelectList) runFallback(ctx context.Context) (string, bool, error) {
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

	reader := bufio.NewReader(s.fallbackReader)
	// Race the blocking read against ctx so a cancelled context
	// (timeout / user interrupt) doesn't hang the fallback forever on
	// an idle piped stdin. A single goroutine owns the reader for the
	// whole fallback; close(done) unblocks it after the read resolves.
	type readRes struct {
		raw string
		err error
	}
	ch := make(chan readRes, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		raw, err := reader.ReadString('\n')
		select {
		case ch <- readRes{raw, err}:
		case <-done:
			return
		}
	}()
	var raw string
	select {
	case <-ctx.Done():
		// Cancelled: treat like the TTY path's Esc/cancel — an empty
		// value with ok=false and no error.
		return "", false, nil
	case res := <-ch:
		if res.err != nil {
			return "", false, nil
		}
		raw = res.raw
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
	// Enable SGR mouse tracking for wheel scroll support (SP-106 T3).
	s.enableMouseTracking()
	defer func() {
		s.disableMouseTracking()
		_ = exitSteerMode(s.fd, st)
		s.clearRendered()
	}()

	// Repaint on terminal resize. Registered AFTER the teardown defer
	// so LIFO runs unsub() first — no resize callback may fire after
	// clearRendered has wiped the frame. The callback never draws on
	// the SIGWINCH goroutine unless TryLockOutput succeeds; otherwise
	// it just flags and the read loop's idle tick repaints.
	unsub := RegisterResizeSubscriber(s.handleResize)
	defer unsub()

	// Print the title once before entering the render loop. The title
	// stays pinned above the list and is excluded from the render()
	// row-clear math so subscriber output between keypresses doesn't
	// misalign the walk-back count and stack duplicate titles.
	// Hold the output lock across the title + first render so a
	// background PrintExternal can't insert a line between them.
	LockOutput()
	if s.opts.Title != "" {
		fmt.Fprintln(os.Stderr, GlyphInfo.Prefix()+s.opts.Title)
	}
	UnlockOutput()

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
			s.repaintIfResized()
			continue
		}

		// Handle the byte(s) we just read. Most actions are a single
		// byte; ESC and arrow-key sequences read 2-3 bytes inline.
		b := buf[0]
		done, val, ok := s.processKey(b, n, buf[:])
		if done {
			return val, ok, nil
		}
	}
}

// mouseOut returns the writer used for mouse-tracking escape sequences.
// It prefers the test seam (s.testOut) so tests can capture the bytes;
// otherwise it falls back to os.Stderr, which is what the interactive
// TTY path has always used.
func (s *SelectList) mouseOut() io.Writer {
	if s.testOut != nil {
		return s.testOut
	}
	return os.Stderr
}

// enableMouseTracking writes the SGR + VT200 mouse-tracking enable
// sequences. It is a no-op when stdin isn't a TTY, matching the
// requirement that non-interactive runs never emit mouse escapes.
func (s *SelectList) enableMouseTracking() {
	if !s.isTTY {
		return
	}
	fmt.Fprint(s.mouseOut(), MouseTrackingSGR)
	fmt.Fprint(s.mouseOut(), MouseTrackingVT200)
}

// disableMouseTracking writes the disable sequence (which tears down
// SGR, VT200, and X10 modes) to turn mouse tracking off on exit. Like
// enableMouseTracking it is a no-op in non-TTY contexts.
func (s *SelectList) disableMouseTracking() {
	if !s.isTTY {
		return
	}
	fmt.Fprint(s.mouseOut(), MouseTrackingDisable)
}

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
