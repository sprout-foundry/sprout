package console

import (
	"testing"
)

func TestSym_DefaultsToUnicode(t *testing.T) {
	t.Setenv("SPROUT_ASCII", "")
	t.Setenv("LC_ALL", "en_US.UTF-8")
	prev := SetASCIISymbols(false)
	t.Cleanup(func() { SetASCIISymbols(prev) })

	if got := Sym().Success; got != "✓" {
		t.Errorf("Unicode Success = %q, want %q", got, "✓")
	}
}

func TestSym_ASCIISymbolsASCII(t *testing.T) {
	if got := asciiSymbols.Success; got != "+" {
		t.Errorf("ASCII Success = %q, want %q", got, "+")
	}
	if got := asciiSymbols.Spinner; len(got) == 0 {
		t.Error("ASCII spinner frames should be non-empty")
	}
}

func TestSetASCIISymbols_SwitchesGlyphSet(t *testing.T) {
	prev := SetASCIISymbols(true)
	t.Cleanup(func() { SetASCIISymbols(prev) })

	if !ASCIISymbols() {
		t.Fatal("ASCIISymbols() should be true after SetASCIISymbols(true)")
	}
	if got := Sym().Error; got != "x" {
		t.Errorf("ASCII Error = %q, want %q", got, "x")
	}
	if got := GlyphError.Rune(); got != "x" {
		t.Errorf("GlyphError.Rune() under ASCII = %q, want %q", got, "x")
	}

	SetASCIISymbols(false)
	if ASCIISymbols() {
		t.Fatal("ASCIISymbols() should be false after SetASCIISymbols(false)")
	}
	if got := GlyphError.Rune(); got != "✗" {
		t.Errorf("GlyphError.Rune() under Unicode = %q, want %q", got, "✗")
	}
}

func TestSym_HonorsSPROUTASCIIEnv(t *testing.T) {
	// Sym() resolves once per process via CompareAndSwap; force a fresh
	// resolution by clearing the cached pointer for this assertion.
	activeSymbols.Store(nil)
	t.Setenv("SPROUT_ASCII", "1")
	t.Cleanup(func() {
		t.Setenv("SPROUT_ASCII", "")
		activeSymbols.Store(nil)
	})

	if !ASCIISymbols() {
		t.Error("SPROUT_ASCII=1 should select the ASCII glyph set")
	}
}

func TestLocaleIsUTF8_PrecedenceAndDefaults(t *testing.T) {
	tests := []struct {
		name   string
		lcAll  string
		lcType string
		lang   string
		want   bool
	}{
		{"unset locale treated as UTF-8", "", "", "", true},
		{"LANG utf-8", "", "", "en_US.UTF-8", true},
		{"LC_ALL non-utf8 wins", "C", "", "en_US.UTF-8", false},
		{"LC_CTYPE non-utf8 over LANG", "", "POSIX", "en_US.UTF-8", false},
		{"utf8 without dash", "", "", "de_DE.utf8", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("LC_ALL", tt.lcAll)
			t.Setenv("LC_CTYPE", tt.lcType)
			t.Setenv("LANG", tt.lang)
			if got := localeIsUTF8(); got != tt.want {
				t.Errorf("localeIsUTF8() = %v, want %v", got, tt.want)
			}
		})
	}
}
