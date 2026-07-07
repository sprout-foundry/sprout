package console

import (
	"bytes"
	"strings"
	"testing"
)

// TestColorBlind_GlyphErrorUsesCyanNotRed is the byte-level regression
// assertion from CLI-E-3: when color-blind mode is on, GlyphError
// must NOT emit the red ANSI sequence (\033[31m) and MUST emit cyan
// (\033[36m). This locks the palette swap so future tweaks can't
// silently regress to the green/red ambiguity that deuteranopes hit.
func TestColorBlind_GlyphErrorUsesCyanNotRed(t *testing.T) {
	prev := SetColorBlind(true)
	t.Cleanup(func() { SetColorBlind(prev) })
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")

	var buf bytes.Buffer
	GlyphError.Fprintln(&buf, "boom")
	out := buf.String()
	if strings.Contains(out, "\033[31m") {
		t.Errorf("color-blind GlyphError should NOT emit red escape: %q", out)
	}
	if !strings.Contains(out, "\033[1;36m") && !strings.Contains(out, "\033[36m") {
		t.Errorf("color-blind GlyphError should emit cyan escape: %q", out)
	}
}

// TestColorBlind_GlyphWarningUsesMagenta asserts the warning swap.
// Under deuteranopia, amber/yellow and green are the most-confused
// pair; bumping warnings to magenta puts them on the red/blue axis
// instead, which is unambiguous.
func TestColorBlind_GlyphWarningUsesMagenta(t *testing.T) {
	prev := SetColorBlind(true)
	t.Cleanup(func() { SetColorBlind(prev) })
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")

	var buf bytes.Buffer
	GlyphWarning.Fprintln(&buf, "drift")
	out := buf.String()
	if strings.Contains(out, "\033[33m") {
		t.Errorf("color-blind GlyphWarning should NOT emit amber escape: %q", out)
	}
	if !strings.Contains(out, "\033[1;35m") && !strings.Contains(out, "\033[35m") {
		t.Errorf("color-blind GlyphWarning should emit magenta escape: %q", out)
	}
}

// TestColorBlind_DefaultPaletteUnchanged asserts the swap is opt-in:
// with no env var and SetColorBlind(false), GlyphError still emits red
// and GlyphWarning still emits amber. Without this guard, a default
// flip in SetColorBlind would silently regress the canonical palette.
func TestColorBlind_DefaultPaletteUnchanged(t *testing.T) {
	// Defensive: even if a prior test left the flag on, force off.
	prev := SetColorBlind(false)
	t.Cleanup(func() { SetColorBlind(prev) })
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")

	var buf bytes.Buffer
	GlyphError.Fprintln(&buf, "x")
	if !strings.Contains(buf.String(), "\033[31m") {
		t.Errorf("default GlyphError should emit red: %q", buf.String())
	}
	buf.Reset()
	GlyphWarning.Fprintln(&buf, "y")
	if !strings.Contains(buf.String(), "\033[33m") {
		t.Errorf("default GlyphWarning should emit amber: %q", buf.String())
	}
}

// TestColorBlind_DisabledByNoColor confirms color-blind mode respects
// the existing NO_COLOR path: even when the palette swap is on, a
// non-empty NO_COLOR suppresses all ANSI escapes.
func TestColorBlind_DisabledByNoColor(t *testing.T) {
	prev := SetColorBlind(true)
	t.Cleanup(func() { SetColorBlind(prev) })
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "")

	var buf bytes.Buffer
	GlyphError.Fprintln(&buf, "x")
	if strings.Contains(buf.String(), "\033[") {
		t.Errorf("NO_COLOR should suppress all escapes even with color-blind palette: %q", buf.String())
	}
}

// TestColorBlind_ApplyFromEnv exercises the env-var wiring from CLI-E-1.
func TestColorBlind_ApplyFromEnv(t *testing.T) {
	t.Setenv("SPROUT_COLOR_BLIND", "1")
	// Reset to a known state first.
	prev := SetColorBlind(false)
	t.Cleanup(func() { SetColorBlind(prev) })
	ApplyColorBlindFromEnv()
	if !ColorBlindEnabled() {
		t.Errorf("SPROUT_COLOR_BLIND=1 should enable color-blind mode")
	}
}

// TestColorBlind_ApplyFromEnvUnset confirms the default (no env var)
// leaves color-blind mode untouched — the function only flips ON, never
// OFF. This matters because the CLI flag's explicit `false` shouldn't
// be clobbered by a stale env var.
func TestColorBlind_ApplyFromEnvUnset(t *testing.T) {
	t.Setenv("SPROUT_COLOR_BLIND", "")
	prev := SetColorBlind(false)
	t.Cleanup(func() { SetColorBlind(prev) })
	ApplyColorBlindFromEnv()
	if ColorBlindEnabled() {
		t.Errorf("empty SPROUT_COLOR_BLIND should not enable color-blind mode")
	}
}

// TestColorBlind_GlyphSuccessStillGreen guards the success/info axes
// against accidental regression: those colors stay put because they're
// on the green/blue axis that deuteranopes still distinguish.
func TestColorBlind_GlyphSuccessStillGreen(t *testing.T) {
	prev := SetColorBlind(true)
	t.Cleanup(func() { SetColorBlind(prev) })
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")

	var buf bytes.Buffer
	GlyphSuccess.Fprintln(&buf, "ok")
	if !strings.Contains(buf.String(), "\033[32m") {
		t.Errorf("color-blind GlyphSuccess should still emit green: %q", buf.String())
	}
}

// TestColorBlind_AllGlyphsProduceValidEscape asserts every defined
// glyph color() returns a valid (or empty) ANSI escape under both
// palettes. Catches a typo where the swap accidentally returns an
// invalid sequence.
func TestColorBlind_AllGlyphsProduceValidEscape(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		prev := SetColorBlind(enabled)
		t.Run("", func(t *testing.T) {
			for g := GlyphSuccess; g <= GlyphDim; g++ {
				prefix := g.Prefix()
				// Each prefix is either empty (no color), starts with
				// \033[, or starts with the bare rune + space (NO_COLOR).
				if prefix == "" {
					t.Errorf("glyph %d empty prefix", g)
				}
				if strings.HasPrefix(prefix, "\033[") && !strings.HasSuffix(prefix, " ") {
					t.Errorf("glyph %d prefix should end with space: %q", g, prefix)
				}
			}
		})
		SetColorBlind(prev)
	}
}
