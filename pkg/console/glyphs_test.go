package console

import (
	"bytes"
	"strings"
	"testing"
)

func TestGlyph_Rune_AllCategoriesUnique(t *testing.T) {
	seen := make(map[string]Glyph)
	for g := GlyphSuccess; g <= GlyphDim; g++ {
		r := g.Rune()
		if existing, dup := seen[r]; dup {
			t.Fatalf("glyph %d duplicates rune %q already used by %d", g, r, existing)
		}
		seen[r] = g
	}
	if len(seen) != 8 {
		t.Fatalf("expected 8 distinct glyphs, got %d", len(seen))
	}
}

func TestGlyph_Prefix_NoColor_PlainGlyphPlusSpace(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "") // ensure no FORCE_COLOR override

	got := GlyphSuccess.Prefix()
	if got != "✓ " {
		t.Errorf("with NO_COLOR, prefix should be '✓ ', got %q", got)
	}
	got2 := GlyphError.Prefix()
	if got2 != "✗ " {
		t.Errorf("with NO_COLOR, prefix should be '✗ ', got %q", got2)
	}
}

func TestGlyph_Prefix_Color_WrapsWithColorEscape(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")

	got := GlyphSuccess.Prefix()
	if !strings.HasPrefix(got, "\033[32m") {
		t.Errorf("success prefix should start with green escape; got %q", got)
	}
	if !strings.Contains(got, "✓") {
		t.Errorf("prefix should contain success glyph; got %q", got)
	}
	if !strings.HasSuffix(got, ansiReset+" ") {
		t.Errorf("prefix should end with reset + space; got %q", got)
	}
}

func TestGlyph_Fprintln_WritesGlyphAndMessage(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	GlyphWarning.Fprintln(&buf, "config drifted")
	got := buf.String()
	if !strings.HasPrefix(got, "⚠ ") {
		t.Errorf("Fprintln should start with warning glyph; got %q", got)
	}
	if !strings.Contains(got, "config drifted") {
		t.Errorf("Fprintln should include message; got %q", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("Fprintln should append newline; got %q", got)
	}
}

func TestGlyph_Fprintf_FormatsCorrectly(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	GlyphInfo.Fprintf(&buf, "model: %s · %d tokens", "gpt-4o", 1234)
	got := buf.String()
	if !strings.Contains(got, "ⓘ model: gpt-4o · 1234 tokens") {
		t.Errorf("Fprintf output unexpected; got %q", got)
	}
}

func TestGlyph_AllPrefixesNonEmpty(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for g := GlyphSuccess; g <= GlyphDim; g++ {
		if p := g.Prefix(); p == "" || p == " " {
			t.Errorf("glyph %d returned empty/blank prefix %q", g, p)
		}
	}
}
