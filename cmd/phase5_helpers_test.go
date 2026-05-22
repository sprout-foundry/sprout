//go:build !js

package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
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
