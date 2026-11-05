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
}

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
	s := &Screen{rows: rows, cols: cols, bot: rows - 1}
	s.grid = make([][]rune, rows)
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
	copy(s.grid[s.top:s.bot], s.grid[s.top+1:s.bot+1])
	s.grid[s.bot] = blankRow(s.cols)
}

func (s *Screen) scrollDown() {
	copy(s.grid[s.top+1:s.bot+1], s.grid[s.top:s.bot])
	s.grid[s.top] = blankRow(s.cols)
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
	out := make([]string, s.rows)
	for i := range out {
		out[i] = s.lineLocked(i + 1)
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

// Resize changes the screen size the way xterm-like terminals do, without
// reflow: a shrink drops rows from the top (into scrollback) so the bottom
// rows and the cursor stay in view; a grow adds blank rows at the bottom and
// leaves the content where it was. Columns are truncated or padded, and the
// margins reset to the full screen. (Termux instead anchors growth to the
// bottom; sprout detects it separately.)
func (s *Screen) Resize(cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	grid := make([][]rune, rows)
	shift := max(s.rows-rows, 0)
	for i := range grid {
		grid[i] = blankRow(cols)
		if src := i + shift; src >= 0 && src < s.rows {
			copy(grid[i], s.grid[src])
		}
	}
	s.grid, s.r = grid, clampInt(s.r-shift, 0, rows-1)
	s.cols, s.rows = cols, rows
	s.c = min(s.c, cols-1)
	s.top, s.bot = 0, rows-1
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
