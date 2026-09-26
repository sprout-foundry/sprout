package configuration

import (
	"os"
	"strings"
	"testing"
)

// This file is the post-migration conformance lock for the onboarding
// status line. The earlier version of this test locked the
// bracketOK / bracketWarn helpers in status_prefix.go, which existed
// only because pkg/configuration could not import pkg/console (an
// import cycle). That cycle is now broken: the onboarding call sites
// in init.go and api_keys.go emit through the console.Glyph* surface
// directly, and the bracket helpers were deleted. These tests assert
// that the Glyph surface is in use and that the bracket helpers are
// not reintroduced.
//
// They read the onboarding source off disk (the assertion that
// actually executes), matching the source-reading conformance pattern
// used elsewhere in the repo. The Glyph prefix behavior itself (colored
// glyph + reset, no-color fallback) is covered by pkg/console's
// glyphs_test.go.

var onboardingSourceFiles = []string{
	"init.go",
	"init_providers.go",
	"api_keys.go",
}

// TestOnboardingUsesGlyphSurface pins that every onboarding status line
// is emitted through the console.Glyph* surface, and that the deleted
// bracketOK / bracketWarn helpers are not referenced anywhere in the
// package. A regression to a bracket helper (or a raw "[OK]" literal)
// would mean the Glyph migration was undone.
func TestOnboardingUsesGlyphSurface(t *testing.T) {
	for _, name := range onboardingSourceFiles {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		src := string(b)
		if strings.Contains(src, "bracketOK(") || strings.Contains(src, "bracketWarn(") {
			t.Errorf("%s references the deleted bracketOK/bracketWarn helpers", name)
		}
		if strings.Contains(src, `fmt.Printf("[OK]`) || strings.Contains(src, `fmt.Printf("[WARN]`) {
			t.Errorf("%s emits a raw bracket literal; use the console.Glyph* surface", name)
		}
	}
}

// TestOnboardingEmitsStatusGlyphs pins that init.go, which owns the
// onboarding provider setup, actually reaches for the success and
// warning glyphs. If neither glyph is referenced the onboarding output
// has silently dropped its status markers.
func TestOnboardingEmitsStatusGlyphs(t *testing.T) {
	b, err := os.ReadFile("init.go")
	if err != nil {
		t.Fatalf("reading init.go: %v", err)
	}
	src := string(b)
	if !strings.Contains(src, "console.GlyphSuccess.") {
		t.Errorf("init.go does not emit a GlyphSuccess status line")
	}
	if !strings.Contains(src, "console.GlyphWarning.") {
		t.Errorf("init.go does not emit a GlyphWarning status line")
	}
}
