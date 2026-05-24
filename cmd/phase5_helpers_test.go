//go:build !js

package cmd

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// SP-048-5d
func TestBuildPromptPrefix(t *testing.T) {
	cases := []struct {
		model string
		want  string
	}{
		{"", "sprout> "},
		{"  ", "sprout> "},
		{"claude-opus-4-7", "claude-opus-4-7 ▸ "},
		{"  trim-me  ", "trim-me ▸ "},
	}
	for _, c := range cases {
		if got := buildPromptPrefix(c.model); got != c.want {
			t.Errorf("buildPromptPrefix(%q) = %q, want %q", c.model, got, c.want)
		}
	}
}

// SP-048-5c
func TestCompactTokens(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{-5, "0"},
		{0, "0"},
		{42, "42"},
		{999, "999"},
		{1000, "1.0k"},
		{1500, "1.5k"},
		{12345, "12.3k"},
	}
	for _, c := range cases {
		if got := compactTokens(c.in); got != c.want {
			t.Errorf("compactTokens(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

// SP-048-5c
func TestCompactTokens_Boundaries(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		// Exactly 10_000 → "10.0k"
		{10000, "10.0k"},
		// 99_999 → "100.0k" (rounded)
		{99999, "100.0k"},
		// 1_000_000 → "1000.0k"
		{1_000_000, "1000.0k"},
		// Precision: 1001 → "1.0k" (truncates to 1 decimal)
		{1001, "1.0k"},
		// Precision: 1009 → "1.0k"
		{1009, "1.0k"},
		// Precision: 1010 → "1.0k"
		{1010, "1.0k"},
		// Precision: 1050 → "1.1k" (rounds up)
		{1050, "1.1k"},
	}
	for _, c := range cases {
		if got := compactTokens(c.in); got != c.want {
			t.Errorf("compactTokens(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
// SP-048-5c
func TestCompactCost(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{-0.5, "$0.00"},
		{0.0, "$0.0000"},
		{0.0023, "$0.0023"},
		{0.05, "$0.050"},
		{0.999, "$0.999"},
		{1.0, "$1.00"},
		{12.34, "$12.34"},
	}
	for _, c := range cases {
		if got := compactCost(c.in); got != c.want {
			t.Errorf("compactCost(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// SP-048-5c
func TestCompactDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{200 * time.Millisecond, "200ms"},
		{999 * time.Millisecond, "999ms"},
		{time.Second, "1.0s"},
		{2500 * time.Millisecond, "2.5s"},
		{59*time.Second + 999*time.Millisecond, "60.0s"},
		{75 * time.Second, "1m15s"},
		{125 * time.Second, "2m5s"},
	}
	for _, c := range cases {
		if got := compactDuration(c.in); got != c.want {
			t.Errorf("compactDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// SP-048-5a
func TestHumanizeAge(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{2 * time.Minute, "2m ago"},
		{59 * time.Minute, "59m ago"},
		{2 * time.Hour, "2h ago"},
		{23 * time.Hour, "23h ago"},
		{25 * time.Hour, "1d ago"},
		{5 * 24 * time.Hour, "5d ago"},
	}
	for _, c := range cases {
		if got := humanizeAge(c.in); got != c.want {
			t.Errorf("humanizeAge(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// SP-048-5a
func TestFormatTurnStatsLine_Color(t *testing.T) {
	// Save and restore NO_COLOR so other tests aren't affected.
	old := os.Getenv("NO_COLOR")
	defer os.Setenv("NO_COLOR", old)

	// With color enabled (default): should contain ANSI dim codes.
	os.Unsetenv("NO_COLOR")
	os.Unsetenv("FORCE_COLOR")
	out := formatTurnStatsLine(1200, 4800, 0.04, 6*time.Second+100*time.Millisecond)
	if !strings.Contains(out, "\033[2m") {
		t.Errorf("with color, expected ANSI dim code in output: %q", out)
	}
	if !strings.Contains(out, "\033[0m") {
		t.Errorf("with color, expected ANSI reset code in output: %q", out)
	}
	if !strings.Contains(out, "1.2k in / 4.8k out") {
		t.Errorf("expected token summary in output: %q", out)
	}
	if !strings.Contains(out, "$0.040") {
		t.Errorf("expected cost in output: %q", out)
	}
	if !strings.Contains(out, "6.1s") {
		t.Errorf("expected duration in output: %q", out)
	}

	// With color disabled: no ANSI codes.
	os.Setenv("NO_COLOR", "1")
	out = formatTurnStatsLine(1200, 4800, 0.04, 6*time.Second+100*time.Millisecond)
	if strings.Contains(out, "\033[2m") || strings.Contains(out, "\033[0m") {
		t.Errorf("without color, no ANSI codes expected: %q", out)
	}
	if !strings.Contains(out, "1.2k in / 4.8k out") {
		t.Errorf("expected token summary in output: %q", out)
	}
}

// SP-048-5a
func TestFormatTurnStatsLine_Durations(t *testing.T) {
	old := os.Getenv("NO_COLOR")
	defer os.Setenv("NO_COLOR", old)
	os.Setenv("NO_COLOR", "1") // strip ANSI for easier assertions

	// Sub-second
	out := formatTurnStatsLine(100, 200, 0.0023, 450*time.Millisecond)
	if !strings.Contains(out, "450ms") {
		t.Errorf("expected '450ms' in %q", out)
	}

	// Seconds
	out = formatTurnStatsLine(50, 60, 0.001, 3*time.Second)
	if !strings.Contains(out, "3.0s") {
		t.Errorf("expected '3.0s' in %q", out)
	}

	// Minutes
	out = formatTurnStatsLine(500, 600, 0.12, 1*time.Minute+30*time.Second)
	if !strings.Contains(out, "1m30s") {
		t.Errorf("expected '1m30s' in %q", out)
	}
}

// SP-048-5a
func TestFormatTurnStatsLine_EdgeCases(t *testing.T) {
	old := os.Getenv("NO_COLOR")
	defer os.Setenv("NO_COLOR", old)
	os.Setenv("NO_COLOR", "1")

	// Zero deltas still produce output (the caller is responsible for
	// filtering out zero-token turns).
	out := formatTurnStatsLine(0, 0, 0, 0)
	if !strings.Contains(out, "0 in / 0 out") {
		t.Errorf("expected zero stats in %q", out)
	}

	// Negative cost (shouldn't happen, but formatTurnStatsLine delegates
	// to compactCost which clamps to $0.00).
	out = formatTurnStatsLine(100, 200, -0.5, 2*time.Second)
	if !strings.Contains(out, "$0.00") {
		t.Errorf("expected clamped cost in %q", out)
	}
}

// SP-048-5a
func TestShouldShowTurnStats(t *testing.T) {
	// In a test harness, stderr is not a TTY (no terminal attached), so
	// shouldShowTurnStats() must return false. This is correct: the
	// function checks stderr (not stdout) because printPerTurnSummary
	// writes to os.Stderr.
	if shouldShowTurnStats() {
		t.Error("shouldShowTurnStats() should return false in a non-TTY test environment")
	}
}

// SP-048-5a
func TestTruncateLabel(t *testing.T) {
	cases := []struct {
		s    string
		max  int
		want string
	}{
		{"", 10, ""},
		{"short", 10, "short"},
		{"exactlyten", 10, "exactlyten"},
		{"over the limit by a lot", 10, "over the …"},
		{"x", 1, "x"},
		{"longer", 1, "l"},
	}
	for _, c := range cases {
		if got := truncateLabel(c.s, c.max); got != c.want {
			t.Errorf("truncateLabel(%q, %d) = %q, want %q", c.s, c.max, got, c.want)
		}
	}
}

// SP-048-5b
func TestFirstRunState_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, ".sprout", "state.json")

	// Missing file → ReadFile error → nil state, error returned.
	if _, err := loadFirstRunState(statePath); err == nil {
		t.Error("loadFirstRunState should return an error when file doesn't exist")
	}

	// Save and reload.
	in := &sproutState{
		SeenFirstRunHint: []string{"/home/u/proj-a", "/home/u/proj-b"},
	}
	if err := saveFirstRunState(statePath, in); err != nil {
		t.Fatalf("saveFirstRunState: %v", err)
	}
	out, err := loadFirstRunState(statePath)
	if err != nil {
		t.Fatalf("loadFirstRunState: %v", err)
	}
	if len(out.SeenFirstRunHint) != 2 ||
		out.SeenFirstRunHint[0] != "/home/u/proj-a" ||
		out.SeenFirstRunHint[1] != "/home/u/proj-b" {
		t.Errorf("round-trip mismatch: %+v", out.SeenFirstRunHint)
	}

	// File should be valid JSON.
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read raw: %v", err)
	}
	var verify map[string]any
	if err := json.Unmarshal(raw, &verify); err != nil {
		t.Errorf("file is not valid JSON: %v", err)
	}
}

// SP-048-5c
func TestCompactCost_Boundaries(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		// Exactly at the 0.01 threshold → switches from 4-decimal to 3-decimal
		{0.0099, "$0.0099"},
		{0.01, "$0.010"},
		{0.011, "$0.011"},
		// 0.9999 < 1.0 so uses $%.3f, which rounds to $1.000 (format rounding, not threshold)
		{0.9999, "$1.000"},
		// Exactly at the 1.0 threshold → switches from 3-decimal to 2-decimal
		{1.0, "$1.00"},
		{1.001, "$1.00"},
		// Large costs
		{100.50, "$100.50"},
		{999.99, "$999.99"},
		{1234.56, "$1234.56"},
	}
	for _, c := range cases {
		if got := compactCost(c.in); got != c.want {
			t.Errorf("compactCost(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// SP-048-5c
func TestCompactDuration_Boundaries(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		// Zero duration
		{0, "0ms"},
		// Negative duration: Milliseconds() returns negative value, not clamped
		{-1 * time.Second, "-1000ms"},
		// Exact 1000ms boundary: should render as "1.0s" not "1000ms"
		{1000 * time.Millisecond, "1.0s"},
		// 59.9s still seconds
		{59900 * time.Millisecond, "59.9s"},
		// Exactly 1 minute
		{60 * time.Second, "1m0s"},
		// Just over 1 minute
		{61 * time.Second, "1m1s"},
		// Large duration: hours with remainder
		{3*time.Hour + 30*time.Minute + 45*time.Second, "210m45s"},
	}
	for _, c := range cases {
		if got := compactDuration(c.in); got != c.want {
			t.Errorf("compactDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// SP-048-5a
func TestFormatTurnStatsLine_ExactFormat(t *testing.T) {
	old := os.Getenv("NO_COLOR")
	defer os.Setenv("NO_COLOR", old)
	os.Setenv("NO_COLOR", "1")

	out := formatTurnStatsLine(100, 200, 0.05, 3*time.Second)

	// Verify the line format: "⎯ this turn: X in / Y out · $Z · Ts ⎯\n"
	// Strip trailing newline for assertions
	out = strings.TrimSuffix(out, "\n")

	if !strings.HasPrefix(out, "⎯ this turn: ") {
		t.Errorf("expected line to start with '⎯ this turn: ', got: %q", out)
	}
	if !strings.HasSuffix(out, " ⎯") {
		t.Errorf("expected line to end with ' ⎯', got: %q", out)
	}
	if !strings.Contains(out, "·") {
		t.Errorf("expected '·' separators in output: %q", out)
	}
	// Verify the structure between delimiters
	inner := strings.TrimPrefix(out, "⎯ this turn: ")
	inner = strings.TrimSuffix(inner, " ⎯")
	// Should have exactly 2 "·" separators creating 3 segments: tokens · cost · duration
	parts := strings.Split(inner, " · ")
	if len(parts) != 3 {
		t.Errorf("expected 3 segments separated by ' · ', got %d in: %q", len(parts), inner)
	}

	// Verify specific segment contents
	if !strings.Contains(parts[0], "100 in / 200 out") {
		t.Errorf("first segment should contain token counts: %q", parts[0])
	}
	if parts[1] != "$0.050" {
		t.Errorf("second segment should be cost, got: %q", parts[1])
	}
	if parts[2] != "3.0s" {
		t.Errorf("third segment should be duration, got: %q", parts[2])
	}
}

// SP-048-5a
func TestFormatTurnStatsLine_LargeValues(t *testing.T) {
	old := os.Getenv("NO_COLOR")
	defer os.Setenv("NO_COLOR", old)
	os.Setenv("NO_COLOR", "1")

	cases := []struct {
		name       string
		prompt     int
		completion int
		cost       float64
		elapsed    time.Duration
		wants      []string // substrings that must appear
	}{
		{
			name:       "millions of tokens",
			prompt:     1_500_000,
			completion: 800_000,
			cost:       50.25,
			elapsed:    120 * time.Second,
			wants:      []string{"1500.0k in", "800.0k out", "$50.25", "2m0s"},
		},
		{
			name:       "zero cost",
			prompt:     100,
			completion: 200,
			cost:       0,
			elapsed:    1 * time.Second,
			wants:      []string{"100 in", "200 out", "$0.0000", "1.0s"},
		},
		{
			name:       "sub-millisecond (0ms)",
			prompt:     50,
			completion: 100,
			cost:       0.005,
			elapsed:    500 * time.Millisecond,
			wants:      []string{"50 in", "100 out", "$0.0050", "500ms"},
		},
		{
			name:       "cost >= $1.00",
			prompt:     5000,
			completion: 3000,
			cost:       2.5,
			elapsed:    15 * time.Second,
			wants:      []string{"5.0k in", "3.0k out", "$2.50", "15.0s"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := formatTurnStatsLine(c.prompt, c.completion, c.cost, c.elapsed)
			for _, want := range c.wants {
				if !strings.Contains(out, want) {
					t.Errorf("expected %q in output: %q", want, out)
				}
			}
		})
	}
}

// SP-048-5a
func TestFormatTurnStatsLine_ForceColor(t *testing.T) {
	oldNOColor := os.Getenv("NO_COLOR")
	oldForceColor := os.Getenv("FORCE_COLOR")
	defer func() {
		os.Setenv("NO_COLOR", oldNOColor)
		os.Setenv("FORCE_COLOR", oldForceColor)
	}()

	// NO_COLOR always wins over FORCE_COLOR (no-color.org precedence).
	// When both are set, output should NOT contain ANSI codes.
	os.Setenv("NO_COLOR", "1")
	os.Setenv("FORCE_COLOR", "1")
	out := formatTurnStatsLine(100, 200, 0.05, 3*time.Second)
	if strings.Contains(out, "\033[2m") {
		t.Errorf("NO_COLOR should win over FORCE_COLOR, expected no ANSI codes in: %q", out)
	}

	// FORCE_COLOR alone (no NO_COLOR) should enable ANSI codes.
	os.Setenv("NO_COLOR", "")
	os.Setenv("FORCE_COLOR", "1")
	out = formatTurnStatsLine(100, 200, 0.05, 3*time.Second)
	if !strings.Contains(out, "\033[2m") {
		t.Errorf("FORCE_COLOR alone should enable ANSI codes, expected dim code in: %q", out)
	}
}

// SP-048-5a
func TestFormatTurnStatsLine_CostThresholds(t *testing.T) {
	old := os.Getenv("NO_COLOR")
	defer os.Setenv("NO_COLOR", old)
	os.Setenv("NO_COLOR", "1")

	cases := []struct {
		name string
		cost float64
		want string
	}{
		{"exactly $0.01", 0.01, "$0.010"},
		{"exactly $0.99", 0.99, "$0.990"},
		{"exactly $1.00", 1.0, "$1.00"},
		{"exactly $10.00", 10.0, "$10.00"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := formatTurnStatsLine(100, 200, c.cost, 1*time.Second)
			if !strings.Contains(out, c.want) {
				t.Errorf("expected %q in output: %q", c.want, out)
			}
		})
	}
}

// SP-048-5a
func TestPrintPerTurnSummary_SuppressedInTestEnv(t *testing.T) {
	// In a test environment, stderr is not a TTY, so shouldShowTurnStats()
	// returns false and printPerTurnSummary produces no output. We verify
	// this by capturing stderr. We pass nil for the agent because the early
	// return in shouldShowTurnStats() means the agent is never dereferenced.
	old := os.Stderr
	defer func() { os.Stderr = old }()

	r, w, _ := os.Pipe()
	os.Stderr = w

	printPerTurnSummary(nil, time.Now().Add(-time.Second), 0, 0, 0)

	w.Close()
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read from pipe: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("printPerTurnSummary should produce no output in non-TTY env, got: %q", got)
	}
}
