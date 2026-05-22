package console

import (
	"bytes"
	"strings"
	"testing"
)

// stubSource is a minimal ContentSource for tests.
type stubSource struct {
	model    string
	used     int
	limit    int
	cost     float64
	workdir  string
}

func (s *stubSource) Model() string                       { return s.model }
func (s *stubSource) ContextTokens() (used, limit int)    { return s.used, s.limit }
func (s *stubSource) TotalCost() float64                  { return s.cost }
func (s *stubSource) WorkingDir() string                  { return s.workdir }

func TestStatusFooter_NoOpOnNonTTY(t *testing.T) {
	w := &nonTTYWriter{}
	f := NewStatusFooter(w, &stubSource{model: "test"})
	// Start, Refresh, Resize, Stop should all be no-ops on non-TTY: no
	// scroll region set, nothing written.
	f.Start()
	f.Refresh()
	f.Resize()
	f.Stop()
	if w.Len() != 0 {
		t.Errorf("non-TTY writer should receive zero bytes; got %d (%q)", w.Len(), w.String())
	}
}

func TestStatusFooter_NilSafe(t *testing.T) {
	var f *StatusFooter
	f.Start()
	f.Refresh()
	f.Resize()
	f.Stop() // no panic = pass
}

func TestFormatTokens(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{50, "50"},
		{999, "999"},
		{1000, "1.0k"},
		{1500, "1.5k"},
		{12345, "12.3k"},
		{100000, "100.0k"},
	}
	for _, c := range cases {
		if got := formatTokens(c.in); got != c.want {
			t.Errorf("formatTokens(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatCost(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0.0, "$0.0000"},
		{0.001, "$0.0010"},
		{0.0094, "$0.0094"},
		{0.05, "$0.050"},
		{0.999, "$0.999"},
		{1.0, "$1.00"},
		{12.34, "$12.34"},
	}
	for _, c := range cases {
		if got := formatCost(c.in); got != c.want {
			t.Errorf("formatCost(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatCtx(t *testing.T) {
	cases := []struct {
		used, limit int
		want        string
	}{
		{0, 0, "0 ctx"},
		{50, 0, "50 ctx"},
		{500, 200000, "500/200.0k ctx"},
		{14200, 200000, "14.2k/200.0k ctx"},
	}
	for _, c := range cases {
		got := formatCtx(c.used, c.limit)
		if got != c.want {
			t.Errorf("formatCtx(%d,%d) = %q, want %q", c.used, c.limit, got, c.want)
		}
	}
}

func TestShortPath_ReplacesHomeWithTilde(t *testing.T) {
	// shortPath uses os.UserHomeDir. We can't reliably mock that here,
	// but for inputs that don't start with home, identity is expected.
	if got := shortPath(""); got != "" {
		t.Errorf("empty input should return empty")
	}
	if got := shortPath("/etc/hosts"); got != "/etc/hosts" {
		t.Errorf("non-home path should pass through, got %q", got)
	}
}

func TestVisibleLen_StripsANSI(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abc", 3},
		{"\033[31mred\033[0m", 3},
		{"\033[33m$1.50\033[0m", 5},
		{"mix\033[31med\033[0m", 5},
	}
	for _, c := range cases {
		if got := visibleLen(c.in); got != c.want {
			t.Errorf("visibleLen(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestTruncTo(t *testing.T) {
	cases := []struct {
		s    string
		n    int
		want string
	}{
		{"", 5, ""},
		{"abc", 5, "abc"},
		{"hello", 5, "hello"},
		{"helloworld", 5, "hell…"},
		{"x", 1, "x"},
		{"hi", 0, ""},
	}
	for _, c := range cases {
		if got := truncTo(c.s, c.n); got != c.want {
			t.Errorf("truncTo(%q, %d) = %q, want %q", c.s, c.n, got, c.want)
		}
	}
}

func TestStatusFooter_StyleCost_ColorThresholds(t *testing.T) {
	f := &StatusFooter{WarnCost: 1.0, AlertCost: 5.0}
	if got := f.styleCost(0.5, "$0.50"); strings.Contains(got, "\033[") {
		t.Errorf("cost below warn should have no color, got %q", got)
	}
	if got := f.styleCost(2.0, "$2.00"); !strings.Contains(got, "\033[33m") {
		t.Errorf("cost above warn should be yellow, got %q", got)
	}
	if got := f.styleCost(10.0, "$10.00"); !strings.Contains(got, "\033[31m") {
		t.Errorf("cost above alert should be red, got %q", got)
	}
}

func TestStatusFooter_ComposeLine_NonTTY_StillProducesString(t *testing.T) {
	// composeLine is pure logic — it doesn't write to the terminal, it
	// just builds a string. Test it directly even on non-TTY.
	f := NewStatusFooter(&nonTTYWriter{}, &stubSource{
		model:   "claude-opus-4-7",
		used:    14200,
		limit:   200000,
		cost:    0.42,
		workdir: "/tmp/work",
	})
	line := f.composeLine(120)
	if !strings.Contains(line, "claude-opus-4-7") {
		t.Errorf("composeLine should include model name; got %q", line)
	}
	if !strings.Contains(line, "14.2k/200.0k ctx") {
		t.Errorf("composeLine should include context; got %q", line)
	}
	if !strings.Contains(line, "$0.42") {
		t.Errorf("composeLine should include cost; got %q", line)
	}
	if !strings.Contains(line, "/tmp/work") {
		t.Errorf("composeLine should include cwd; got %q", line)
	}
}

func TestStatusFooter_ComposeLine_TruncatesAtNarrowWidth(t *testing.T) {
	f := NewStatusFooter(&nonTTYWriter{}, &stubSource{
		model:   "a-very-long-model-name-that-exceeds-thirty",
		workdir: "/tmp/foo",
	})
	line := f.composeLine(40)
	if visibleLen(line) > 40 {
		t.Errorf("composeLine should fit within terminal width; visible=%d (line=%q)", visibleLen(line), line)
	}
}

func TestStatusFooter_ComposeLine_PadsWithSpaces(t *testing.T) {
	var buf bytes.Buffer
	f := NewStatusFooter(&buf, &stubSource{
		model:   "m",
		workdir: "/x",
	})
	line := f.composeLine(80)
	// Trailing padding is spaces — the top hr (drawn separately by
	// draw()) provides the visual framing, so the content row stays
	// uncluttered.
	if !strings.HasSuffix(line, " ") {
		t.Errorf("composeLine should pad with spaces, got %q", line)
	}
	if visibleLen(line) != 80 {
		t.Errorf("composeLine should be exactly 80 visible chars, got %d", visibleLen(line))
	}
}

// SP-048-3d: cost styling should keep the footer's base color (cyan) on
// either side of the highlighted span, so the line reads as a coherent
// chrome rather than three separately-colored pieces.
func TestStatusFooter_StyleCost_RestoresBaseColorAfterAlert(t *testing.T) {
	f := &StatusFooter{WarnCost: 1.0, AlertCost: 5.0}
	got := f.styleCost(10.0, "$10.00")
	if !strings.HasPrefix(got, "\033[31m") {
		t.Errorf("alert cost should start with red ANSI, got %q", got)
	}
	if !strings.HasSuffix(got, footerResetFgKeepBase) {
		t.Errorf("alert cost should end by popping fg back to base color, got %q", got)
	}
}
