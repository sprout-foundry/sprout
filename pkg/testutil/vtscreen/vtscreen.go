// Package vtscreen is a minimal VT100 emulator for tests that need to assert
// what a terminal user would see rather than the escape bytes a writer
// emitted. It models what sprout's CLI emits — CUP, DECSTBM margins,
// DECSC/DECRC, LF/CR, reverse index, EL/ED, CUU/CUD/CUF/CUB/CHA — and accepts
// and ignores SGR and private modes. LF is treated as CR LF, as a TTY with
// OPOST|ONLCR (cooked mode, and sprout's steer cbreak mode) delivers it.
package vtscreen

import (
	"strconv"
	"strings"
	"sync"
)

// Screen is the emulated terminal. Safe for one writer goroutine; reads
// from another goroutine must hold the lock (Snapshot, Line and the other
// accessors do).
type Screen struct {
	mu           sync.Mutex
	rows, cols   int
	grid         [][]rune
	r, c         int // cursor, 0-based
	top, bot     int // scroll margins, 0-based inclusive
	savedR       int
	savedC       int
	pendingBytes []byte
	// wrappedPastEdge records that a write ran past the last column and
	// auto-wrapped: a row the writer meant to fit did not.
	wrappedPastEdge bool
	rawNewlines     bool
	// wrapped marks the rows whose content continued onto the row below via
	// auto-wrap, so a reflowing Resize can rejoin them into logical lines.
	wrapped []bool
	// scrollback holds the rows that left the top of the screen. It is only
	// a test fixture's memory of what a real terminal's scrollback would
	// hold: nothing on screen can reach it, and rows scrolled out INSIDE a
	// DECSTBM region are lost rather than stored here, which is what xterm
	// does when the region excludes row 1.
	scrollback []string
	resizeMode ResizeMode
}

// ResizeMode picks how a Resize repositions the content already on screen.
// Terminal emulators disagree here, and sprout's footer has to cope with the
// disagreement, so a test has to be able to ask for each family.
type ResizeMode int

const (
	// ResizeNonReflow is xterm and its descendants (VTE, Windows Terminal):
	// a grow adds blank rows at the bottom and leaves the content where it
	// was; a shrink drops rows off the top into scrollback. A width change
	// truncates or pads rows without rewrapping them.
	ResizeNonReflow ResizeMode = iota
	// ResizeBottomAnchored is Termux (TerminalBuffer.resize): the bottom row
	// stays glued to the bottom edge, so a grow shifts content DOWN and pulls
	// rows back in from scrollback, and a shrink pushes them up into it.
	ResizeBottomAnchored
	// ResizeReflow is iTerm2, Terminal.app and kitty: a width change rewraps
	// the soft-wrapped rows at the new width, and a height change keeps the
	// bottom of the content in view by pushing rows off the top.
	ResizeReflow
)

// TranslateNewlines sets whether LF also returns the carriage (the default,
// standing in for the kernel's ONLCR when a test writes straight to the
// screen). Turn it off when feeding bytes read from a pty master: the kernel
// already translated them, and in raw mode a bare LF must stay a bare LF.
func (s *Screen) TranslateNewlines(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rawNewlines = !on
}

// New returns a blank cols×rows screen with the cursor at the top left.
func New(cols, rows int) *Screen {
	s := &Screen{rows: rows, cols: cols, bot: rows - 1, resizeMode: ResizeNonReflow}
	s.grid = make([][]rune, rows)
	s.wrapped = make([]bool, rows)
	for i := range s.grid {
		s.grid[i] = blankRow(cols)
	}
	return s
}

func blankRow(cols int) []rune {
	row := make([]rune, cols)
	for i := range row {
		row[i] = ' '
	}
	return row
}

// WriteString lets the screen stand in for the footer's io.Writer when the
// footer is built over a *strings.Builder-shaped writer.
func (s *Screen) WriteString(p string) (int, error) { return s.Write([]byte(p)) }

// Write feeds terminal output to the screen.
func (s *Screen) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data := append(s.pendingBytes, p...)
	s.pendingBytes = nil
	str := string(data)
	for i := 0; i < len(str); {
		ch := str[i]
		switch {
		case ch == 0x1b:
			n, ok := s.escape(str[i:])
			if !ok {
				s.pendingBytes = []byte(str[i:])
				return len(p), nil
			}
			i += n
			continue
		case ch == '\r':
			s.c = 0
		case ch == '\b':
			s.c = max(0, min(s.c, s.cols)-1)
		case ch == '\t':
			s.c = min(s.cols-1, (s.c/8+1)*8)
		case ch == '\n':
			// A writer under test writes through a TTY with OPOST|ONLCR on
			// (cooked, or steer mode's cbreak), so a bare LF arrives as CR
			// LF — unless the bytes come from a real pty master, where the
			// kernel has already translated them (see TranslateNewlines).
			if !s.rawNewlines {
				s.c = 0
			}
			s.lineFeed()
		case ch < 0x20:
		default:
			r, size := decodeRune(str[i:])
			s.put(r)
			i += size
			continue
		}
		i++
	}
	return len(p), nil
}

func decodeRune(s string) (rune, int) {
	for i, r := range s {
		if i == 0 {
			return r, len(string(r))
		}
	}
	return ' ', 1
}

func (s *Screen) put(r rune) {
	if s.c >= s.cols {
		s.wrappedPastEdge = true
		// The row's content continues onto the row below: a reflowing
		// Resize has to join them back into one logical line.
		s.wrapped[s.r] = true
		s.c = 0
		s.lineFeed()
	}
	s.grid[s.r][s.c] = r
	s.c++
}

// lineFeed scrolls the margins when the cursor sits on the bottom margin;
// below the margins it moves down until the last screen row and then does
// nothing — the "every line overwrites the last row" failure.
func (s *Screen) lineFeed() {
	switch {
	case s.r == s.bot:
		s.scrollUp()
	case s.r < s.rows-1:
		s.r++
	}
}

func (s *Screen) scrollUp() {
	s.lineFeedScrollback()
	copy(s.grid[s.top:s.bot], s.grid[s.top+1:s.bot+1])
	copy(s.wrapped[s.top:s.bot], s.wrapped[s.top+1:s.bot+1])
	s.grid[s.bot] = blankRow(s.cols)
	s.wrapped[s.bot] = false
}

// lineFeedScrollback records the row about to be discarded from the top of
// the region, into scrollback when the region starts at row 1. A region that
// excludes row 1 discards the line outright — the terminal has nowhere to put
// it — so content scrolled inside such a region is genuinely lost, which is
// one of the things a test may need to prove.
func (s *Screen) lineFeedScrollback() {
	if s.top == 0 {
		s.scrollback = append(s.scrollback, strings.TrimRight(string(s.grid[s.top]), " "))
	}
}

func (s *Screen) scrollDown() {
	copy(s.grid[s.top+1:s.bot+1], s.grid[s.top:s.bot])
	copy(s.wrapped[s.top+1:s.bot+1], s.wrapped[s.top:s.bot])
	s.grid[s.top] = blankRow(s.cols)
	s.wrapped[s.top] = false
}

// escape consumes one escape sequence and reports its length; ok is false
// when the sequence is incomplete.
func (s *Screen) escape(str string) (int, bool) {
	if len(str) < 2 {
		return 0, false
	}
	switch str[1] {
	case '7':
		s.savedR, s.savedC = s.r, s.c
		return 2, true
	case '8':
		s.r, s.c = s.savedR, s.savedC
		return 2, true
	case 'M':
		if s.r == s.top {
			s.scrollDown()
		} else if s.r > 0 {
			s.r--
		}
		return 2, true
	case '[':
	default:
		return 2, true
	}
	end := 2
	for end < len(str) && (str[end] < 0x40 || str[end] > 0x7e) {
		end++
	}
	if end >= len(str) {
		return 0, false
	}
	s.csi(str[2:end], str[end])
	return end + 1, true
}

func (s *Screen) csi(params string, final byte) {
	if strings.HasPrefix(params, "?") {
		return
	}
	var args []int
	for _, p := range strings.Split(params, ";") {
		n, _ := strconv.Atoi(p)
		args = append(args, n)
	}
	arg := func(i, def int) int {
		if i < len(args) && args[i] > 0 {
			return args[i]
		}
		return def
	}
	switch final {
	case 'H', 'f':
		s.r = clampInt(arg(0, 1)-1, 0, s.rows-1)
		s.c = clampInt(arg(1, 1)-1, 0, s.cols-1)
	case 'r':
		s.top = clampInt(arg(0, 1)-1, 0, s.rows-1)
		s.bot = clampInt(arg(1, s.rows)-1, 0, s.rows-1)
		s.r, s.c = 0, 0
	case 'A':
		floor := 0
		if s.r >= s.top {
			floor = s.top
		}
		s.r = max(floor, s.r-arg(0, 1))
	case 'B':
		ceil := s.rows - 1
		if s.r <= s.bot {
			ceil = s.bot
		}
		s.r = min(ceil, s.r+arg(0, 1))
	case 'C':
		s.c = min(s.cols-1, s.c+arg(0, 1))
	case 'D':
		s.c = max(0, s.c-arg(0, 1))
	case 'G':
		s.c = clampInt(arg(0, 1)-1, 0, s.cols-1)
	case 'd':
		s.r = clampInt(arg(0, 1)-1, 0, s.rows-1)
	case 'S':
		for i := 0; i < arg(0, 1); i++ {
			s.scrollUp()
		}
	case 'T':
		for i := 0; i < arg(0, 1); i++ {
			s.scrollDown()
		}
	case 'X':
		for i := s.c; i < min(s.cols, s.c+arg(0, 1)); i++ {
			s.grid[s.r][i] = ' '
		}
	case 'K':
		switch arg(0, 0) {
		case 0:
			for i := s.c; i < s.cols; i++ {
				s.grid[s.r][i] = ' '
			}
		case 2:
			s.grid[s.r] = blankRow(s.cols)
		}
	case 'J':
		switch arg(0, 0) {
		case 0:
			for i := s.c; i < s.cols; i++ {
				s.grid[s.r][i] = ' '
			}
			for r := s.r + 1; r < s.rows; r++ {
				s.grid[r] = blankRow(s.cols)
			}
		case 2:
			for r := range s.grid {
				s.grid[r] = blankRow(s.cols)
			}
		}
	}
}

func clampInt(v, lo, hi int) int {
	return max(lo, min(hi, v))
}

// Line returns row n (1-based) with trailing blanks trimmed.
func (s *Screen) Line(n int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lineLocked(n)
}

func (s *Screen) lineLocked(n int) string {
	if n < 1 || n > s.rows {
		return ""
	}
	return strings.TrimRight(string(s.grid[n-1]), " ")
}

// RawLine returns row n (1-based) untrimmed, padded to the width.
func (s *Screen) RawLine(n int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n < 1 || n > s.rows {
		return ""
	}
	return string(s.grid[n-1])
}

// Lines returns every row, trimmed.
func (s *Screen) Lines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.linesLocked()
}

func (s *Screen) linesLocked() []string {
	out := make([]string, s.rows)
	for i := range out {
		out[i] = strings.TrimRight(string(s.grid[i]), " ")
	}
	return out
}

// Text is the whole screen, trimmed rows joined by newlines.
func (s *Screen) Text() string { return strings.Join(s.Lines(), "\n") }

// RegionLines returns the trimmed rows inside the scroll margins.
func (s *Screen) RegionLines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for r := s.top; r <= s.bot; r++ {
		out = append(out, s.lineLocked(r+1))
	}
	return out
}

// Margins returns the scroll region, 1-based inclusive.
func (s *Screen) Margins() (top, bottom int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.top + 1, s.bot + 1
}

// CursorInRegion reports whether a line feed at the cursor would scroll the
// region rather than stall on a pinned row.
func (s *Screen) CursorInRegion() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.r >= s.top && s.r <= s.bot
}

// Cursor returns the cursor position, 1-based — what the terminal answers to
// a cursor position request (DSR 6).
func (s *Screen) Cursor() (row, col int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.r + 1, min(s.c, s.cols-1) + 1
}

// Size returns the screen's columns and rows.
func (s *Screen) Size() (cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cols, s.rows
}

// WrappedPastEdge reports whether any write ran past the last column and
// auto-wrapped: a row the writer meant to fit did not.
func (s *Screen) WrappedPastEdge() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.wrappedPastEdge
}

// SetResizeMode picks the resize semantics the screen models. See ResizeMode.
func (s *Screen) SetResizeMode(m ResizeMode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resizeMode = m
}

// ResizeMode returns the modelled resize semantics.
func (s *Screen) ResizeMode() ResizeMode {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resizeMode
}

// Scrollback returns the trimmed rows that left the top of the screen, oldest
// first: what a real terminal's scrollback would hold. It is a fixture's
// memory, not part of what a program under test can address — nothing written
// to the screen can reach it, which is exactly why losing a row into it is a
// bug worth asserting on.
func (s *Screen) Scrollback() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.scrollback))
	copy(out, s.scrollback)
	return out
}

// AllText is scrollback plus the visible rows, oldest first: the whole
// transcript as the user could read it by scrolling up. Asserting a row
// appears here (rather than in Text) separates "scrolled out of view" from
// "erased".
func (s *Screen) AllText() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := append(append([]string{}, s.scrollback...), s.linesLocked()...)
	return strings.Join(all, "\n")
}

// Resize changes the screen size, modelling the emulator family selected by
// SetResizeMode. Every mode resets the scroll margins to the full screen, as
// a resize does in reality.
func (s *Screen) Resize(cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.resizeMode {
	case ResizeBottomAnchored:
		s.resizeBottomAnchored(cols, rows)
	case ResizeReflow:
		s.resizeReflow(cols, rows)
	default:
		s.resizeNonReflow(cols, rows)
	}
}

// resizeNonReflow models xterm and its descendants: rows keep their absolute
// screen positions on a grow (new blank rows appear at the bottom) and are
// dropped off the top on a shrink; columns truncate or pad without rewrapping.
func (s *Screen) resizeNonReflow(cols, rows int) {
	shift := max(s.rows-rows, 0)
	if shift > 0 {
		s.dropTopToScrollback(shift)
	}
	text := make([]string, rows)
	cont := make([]bool, rows)
	for i := range text {
		if src := i + shift; src < s.rows {
			text[i] = strings.TrimRight(string(s.grid[src]), " ")
			cont[i] = s.wrapped[src]
		}
	}
	s.install(rows, cols, text, cont)
	s.r = clampInt(s.r-shift, 0, rows-1)
}

// resizeBottomAnchored models Termux's TerminalBuffer.resize: the content
// stays glued to the bottom edge, so a grow shifts it DOWN and pulls the
// matching rows back in from scrollback, and a shrink pushes them up into it.
func (s *Screen) resizeBottomAnchored(cols, rows int) {
	text := make([]string, rows)
	cont := make([]bool, rows)
	shift := max(s.rows-rows, 0)
	grow := max(rows-s.rows, 0)
	if shift > 0 {
		s.dropTopToScrollback(shift)
		for i := range text {
			if src := i + shift; src < s.rows {
				text[i] = strings.TrimRight(string(s.grid[src]), " ")
				cont[i] = s.wrapped[src]
			}
		}
	} else {
		// Rows the earlier scroll-up pushed out of view come back at the
		// top. Scrollback may hold fewer than `grow` of them early in a
		// session; the gap then stays blank, as it does on the device.
		head := s.takeScrollback(grow)
		copy(text, head)
		for i := 0; i < s.rows && i+grow < rows; i++ {
			text[i+grow] = strings.TrimRight(string(s.grid[i]), " ")
			cont[i+grow] = s.wrapped[i]
		}
	}
	s.install(rows, cols, text, cont)
	s.r = clampInt(s.r+grow-shift, 0, rows-1)
}

// resizeReflow models iTerm2, Terminal.app and kitty: soft-wrapped rows are
// rejoined into logical lines and rewrapped at the new width, and the viewport
// keeps the BOTTOM of the content in view.
//
// Bottom anchoring is a fixture simplification: a cursor parked mid-screen by
// a pinned footer makes "keep the cursor visible" ambiguous, and in every case
// asserted here the newest content sits at the bottom edge.
func (s *Screen) resizeReflow(cols, rows int) {
	logical, cursorLine := s.logicalLines()
	phys, cont, start := rewrap(logical, cols)
	// The cursor's physical row: its logical line's first row, plus however
	// many columns into that line it sits.
	cursorRow := 0
	if cursorLine >= 0 && cursorLine < len(start) {
		cursorRow = start[cursorLine] + s.c/cols
	}
	// Rows below the buffer's content are the unused part of the old screen,
	// not content, and leaving them in would push real content up when the
	// viewport is bottom-aligned. Only BLANK rows may go: a pinned footer
	// parks its cursor mid-screen, and cutting at the cursor there would drop
	// every conversation row beneath it.
	end := len(phys)
	for end > cursorRow+1 && phys[end-1] == "" && !cont[end-1] {
		end--
	}
	phys, cont = phys[:end], cont[:end]
	// The visible screen is the tail of the buffer: a grow reveals hidden
	// lines, an overflow of the rewrapped text hides the oldest ones.
	if head := rows - len(phys); head > 0 {
		text := make([]string, 0, rows)
		text = append(text, s.takeScrollback(head)...)
		for len(text) < head {
			text = append(text, "")
		}
		s.install(rows, cols, append(text, phys...),
			append(make([]bool, head), cont...))
		s.r = clampInt(cursorRow+head, 0, rows-1)
	} else {
		off := len(phys) - rows
		s.scrollback = append(s.scrollback, phys[:off]...)
		s.install(rows, cols, phys[off:], cont[off:])
		s.r = clampInt(cursorRow-off, 0, rows-1)
	}
}

// logicalLines joins soft-wrapped rows back into logical lines and reports the
// logical line the cursor sits on (-1 when it is past the end).
func (s *Screen) logicalLines() ([]string, int) {
	var logical []string
	cursorLine := -1
	for i := 0; i < s.rows; i++ {
		text := strings.TrimRight(string(s.grid[i]), " ")
		if i > 0 && s.wrapped[i-1] && len(logical) > 0 {
			logical[len(logical)-1] += text
		} else {
			logical = append(logical, text)
		}
		if i == s.r {
			cursorLine = len(logical) - 1
		}
	}
	return logical, cursorLine
}

// rewrap re-wraps logical lines to width cols. It returns the physical rows,
// each row's "continues onto the next row" flag, and the index of the first
// physical row of every logical line. Long lines split at the column edge on
// rune boundaries, as a terminal's autowrap does.
func rewrap(logical []string, cols int) (text []string, cont []bool, start []int) {
	if cols < 1 {
		cols = 1
	}
	for _, line := range logical {
		runes := []rune(line)
		start = append(start, len(text))
		for {
			n := min(cols, len(runes))
			text = append(text, string(runes[:n]))
			runes = runes[n:]
			if len(runes) == 0 {
				cont = append(cont, false)
				break
			}
			cont = append(cont, true)
		}
	}
	return text, cont, start
}

// install replaces the grid with the given trimmed rows and continue-flags,
// truncating or padding to cols and padding blank to rows.
func (s *Screen) install(rows, cols int, text []string, cont []bool) {
	grid := make([][]rune, rows)
	wrapped := make([]bool, rows)
	for i := range grid {
		grid[i] = blankRow(cols)
		if i < len(text) {
			copy(grid[i], []rune(text[i]))
		}
		if i < len(cont) {
			wrapped[i] = cont[i]
		}
	}
	s.grid, s.wrapped, s.rows, s.cols = grid, wrapped, rows, cols
	s.c = min(s.c, cols-1)
	s.top, s.bot = 0, rows-1
}

// dropTopToScrollback moves the top n visible rows into scrollback.
func (s *Screen) dropTopToScrollback(n int) {
	for i := 0; i < n && i < s.rows; i++ {
		s.scrollback = append(s.scrollback, strings.TrimRight(string(s.grid[i]), " "))
	}
}

// takeScrollback removes up to n rows from the tail of scrollback, oldest
// first, as a terminal pulling hidden lines back into view.
func (s *Screen) takeScrollback(n int) []string {
	if n <= 0 || len(s.scrollback) == 0 {
		return nil
	}
	if n > len(s.scrollback) {
		n = len(s.scrollback)
	}
	out := make([]string, n)
	copy(out, s.scrollback[len(s.scrollback)-n:])
	s.scrollback = s.scrollback[:len(s.scrollback)-n]
	return out
}

// Dump renders the screen for failure messages: ">" marks the cursor row,
// "|" the scroll margins.
func (s *Screen) Dump() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b strings.Builder
	for i := range s.grid {
		mark := " "
		if i == s.r {
			mark = ">"
		}
		if i == s.top || i == s.bot {
			mark += "|"
		} else {
			mark += " "
		}
		b.WriteString(mark + string(s.grid[i]) + "\n")
	}
	return b.String()
}
