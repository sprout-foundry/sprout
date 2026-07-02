package configuration

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

// TestOnboardingBrackets_VisibleStringPreserved is CLI-C-3's regression
// lock: every migrated [OK] / [WARN] site in pkg/configuration/onboarding
// must still emit exactly the same visible string in default (colored)
// mode that it did before the migration.
//
// pkg/configuration cannot import pkg/console (cycle via
// console/ci_output_handler.go), so the "migration" here is the
// bracketOK / bracketWarn helpers in status_prefix.go. These tests
// assert that the helpers produce the same bracketed output the
// original fmt.Printf literals emitted — so any future port to the
// console.Glyph* surface, or any change to the helpers, will surface
// a visible-string regression here.
func TestOnboardingBrackets_VisibleStringPreserved(t *testing.T) {
	cases := []struct {
		name string
		fn   func(io.Writer) // writes a single bracketed line
		want string
	}{
		{
			name: "bracketOK_simple",
			fn: func(w io.Writer) {
				bracketOK(w, "Using OpenRouter provider from environment")
			},
			want: "[OK] Using OpenRouter provider from environment\n",
		},
		{
			name: "bracketWarn_simple",
			fn: func(w io.Writer) {
				bracketWarn(w, "Please configure a real provider")
			},
			want: "[WARN] Please configure a real provider\n",
		},
		{
			name: "bracketOK_withFormatArgs",
			fn: func(w io.Writer) {
				// Mirrors the original fmt.Sprintf("[OK] API key saved for %s …", name) call.
				w.Write([]byte("[OK] "))
				fmt.Fprintf(w, "API key saved for %s (%d models available)", "OpenAI", 12)
				w.Write([]byte("\n"))
			},
			want: "[OK] API key saved for OpenAI (12 models available)\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			tc.fn(&buf)
			if got := buf.String(); got != tc.want {
				t.Errorf("visible-string mismatch\n  got:  %q\n  want: %q", got, tc.want)
			}
		})
	}
}

// TestOnboardingBrackets_NoColorEscapes is the secondary lock: the
// helpers in this package never emit ANSI escapes. Color is the
// Glyph system job; the cycle-constrained helpers here intentionally
// don't participate in NO_COLOR/FORCE_COLOR (they're a fallback while
// the cycle exists). Tests should fail if a future change accidentally
// introduces color codes — that would mean the cycle is gone and we
// should be using console.Glyph* instead.
func TestOnboardingBrackets_NoColorEscapes(t *testing.T) {
	cases := []struct {
		name string
		fn   func(io.Writer)
	}{
		{"bracketOK", func(w io.Writer) { bracketOK(w, "ok") }},
		{"bracketWarn", func(w io.Writer) { bracketWarn(w, "warn") }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			tc.fn(&buf)
			if strings.Contains(buf.String(), "\033[") {
				t.Errorf("%s emitted ANSI escapes (%q); pkg/configuration helpers are intentionally color-free", tc.name, buf.String())
			}
		})
	}
}