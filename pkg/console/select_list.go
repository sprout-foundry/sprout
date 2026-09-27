// Select list UI component — picker state machine, input loop, and
// fallback/mouse paths (select_list.go). The frame-drawing + resize
// helpers live in select_list_render.go.
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
