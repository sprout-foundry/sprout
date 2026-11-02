package console

import (
	"strconv"
	"strings"
)

// vtScreen is a minimal VT100 emulator for footer tests. It models only
// what the footer and transcript writers emit — CUP, DECSTBM margins,
// DECSC/DECRC, LF/CR, reverse index, EL/ED, CUU/CUD — so a test can assert
// what the user would actually see instead of the escape bytes that got
// them there. SGR and private modes are accepted and ignored.
type vtScreen struct {
	rows, cols   int
	grid         [][]rune
	r, c         int // cursor, 0-based
	top, bot     int // scroll margins, 0-based inclusive
	savedR       int
	savedC       int
	pendingBytes []byte
}

func newVTScreen(cols, rows int) *vtScreen {
	s := &vtScreen{rows: rows, cols: cols, bot: rows - 1}
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
func (s *vtScreen) WriteString(p string) (int, error) { return s.Write([]byte(p)) }

func (s *vtScreen) Write(p []byte) (int, error) {
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
		case ch == '\n':
			// The footer writes through a TTY with OPOST|ONLCR on (cooked,
			// or steer mode's cbreak), so a bare LF arrives as CR LF.
			s.c = 0
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

func (s *vtScreen) put(r rune) {
	if s.c >= s.cols {
		s.c = 0
		s.lineFeed()
	}
	s.grid[s.r][s.c] = r
	s.c++
}

// lineFeed scrolls the margins when the cursor sits on the bottom margin;
// below the margins it moves down until the last screen row and then does
// nothing — the "every line overwrites the last row" failure.
func (s *vtScreen) lineFeed() {
	switch {
	case s.r == s.bot:
		s.scrollUp()
	case s.r < s.rows-1:
		s.r++
	}
}

func (s *vtScreen) scrollUp() {
	copy(s.grid[s.top:s.bot], s.grid[s.top+1:s.bot+1])
	s.grid[s.bot] = blankRow(s.cols)
}

func (s *vtScreen) scrollDown() {
	copy(s.grid[s.top+1:s.bot+1], s.grid[s.top:s.bot])
	s.grid[s.top] = blankRow(s.cols)
}

// escape consumes one escape sequence and reports its length; ok is false
// when the sequence is incomplete.
func (s *vtScreen) escape(str string) (int, bool) {
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

func (s *vtScreen) csi(params string, final byte) {
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

// line returns row n (1-based) with trailing blanks trimmed.
func (s *vtScreen) line(n int) string {
	return strings.TrimRight(string(s.grid[n-1]), " ")
}

// regionLines returns the trimmed rows inside the scroll margins.
func (s *vtScreen) regionLines() []string {
	var out []string
	for r := s.top; r <= s.bot; r++ {
		out = append(out, s.line(r+1))
	}
	return out
}

// cursorInRegion reports whether a line feed at the cursor would scroll the
// transcript rather than stall on a pinned row.
func (s *vtScreen) cursorInRegion() bool { return s.r >= s.top && s.r <= s.bot }

func (s *vtScreen) dump() string {
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
