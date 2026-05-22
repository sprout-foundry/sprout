package console

import (
	"errors"
	"strings"
	"testing"
)

func TestFormatErrorBlock_NilError(t *testing.T) {
	if got := FormatErrorBlock("[FAIL] Error", nil); got != "" {
		t.Errorf("nil error should produce empty string, got %q", got)
	}
}

// Single-line errors must render byte-identical to the previous
// `fmt.Fprintf(os.Stderr, "[FAIL] Error: %v\n", err)` output so existing
// log scrapers, screenshots, and tests don't see a regression.
func TestFormatErrorBlock_SingleLine_MatchesLegacyFormat(t *testing.T) {
	got := FormatErrorBlock("[FAIL] Error", errors.New("exit status 1"))
	want := "[FAIL] Error: exit status 1\n"
	if got != want {
		t.Errorf("single-line format = %q, want %q", got, want)
	}
}

func TestFormatErrorBlock_TrimsTrailingNewline(t *testing.T) {
	// Errors built from command stderr often carry a trailing \n; the
	// block formatter should not produce double newlines.
	got := FormatErrorBlock("[FAIL] Error", errors.New("exit status 1\n"))
	want := "[FAIL] Error: exit status 1\n"
	if got != want {
		t.Errorf("trailing-newline single-line = %q, want %q", got, want)
	}
}

func TestFormatErrorBlock_MultiLine_IndentsAndPreservesContent(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	err := errors.New("compilation failed\nfoo.go:10: missing semicolon\nfoo.go:11: undefined: bar")
	got := FormatErrorBlock("[FAIL] Error", err)
	// Header + colon + newline + each line indented by two spaces.
	want := "[FAIL] Error:\n  compilation failed\n  foo.go:10: missing semicolon\n  foo.go:11: undefined: bar\n"
	if got != want {
		t.Errorf("multi-line format = %q, want %q", got, want)
	}
}

func TestFormatErrorBlock_MultiLine_NoColor_NoANSI(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	got := FormatErrorBlock("[FAIL] Error", errors.New("line1\nline2"))
	if strings.Contains(got, "\033[") {
		t.Errorf("NO_COLOR=1 should suppress ANSI escapes, got %q", got)
	}
}

func TestFormatErrorBlock_MultiLine_WithColor_HasRedAndReset(t *testing.T) {
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("NO_COLOR", "") // FORCE_COLOR loses to NO_COLOR per no-color.org; clear it
	got := FormatErrorBlock("[FAIL] Error", errors.New("line1\nline2"))
	if !strings.Contains(got, errorBlockRedFG) {
		t.Errorf("colored block should contain red ANSI, got %q", got)
	}
	if !strings.Contains(got, "\033[0m") {
		t.Errorf("colored block should contain ANSI reset, got %q", got)
	}
}

// Single-line errors should not gain a colored block even with colors
// enabled — the legacy format is plain text and we don't want to drift.
func TestFormatErrorBlock_SingleLine_WithColor_StillPlain(t *testing.T) {
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("NO_COLOR", "")
	got := FormatErrorBlock("[FAIL] Error", errors.New("simple"))
	if strings.Contains(got, "\033[") {
		t.Errorf("single-line should never gain ANSI, got %q", got)
	}
}
