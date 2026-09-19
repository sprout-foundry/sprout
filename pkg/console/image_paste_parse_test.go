package console

import (
	"reflect"
	"testing"
)

// The exact filename class from the 2026-09-18 bug report: macOS screenshots
// contain regular spaces AND a U+202F narrow no-break space before AM/PM.
func TestParsePastedImagePlaceholders_BracketedSpaces(t *testing.T) {
	path := "/Users/alanp/Desktop/Screenshot 2026-09-18 at 2.53.42\u202fPM.png"
	query := "look at this " + PastedImagePlaceholder(path) + "and tell me what's wrong"

	got := ParsePastedImagePlaceholders(query)
	if len(got) != 1 || got[0] != path {
		t.Fatalf("expected [%q], got %q", path, got)
	}
}

func TestParsePastedImagePlaceholders_Multiple(t *testing.T) {
	a := "/tmp/sprout/a.png"
	b := "/Users/alanp/Desktop/Screenshot 2026-09-18 at 2.53.42 PM.png"
	query := PastedImagePlaceholder(a) + PastedImagePlaceholder(b) + "compare these"

	got := ParsePastedImagePlaceholders(query)
	if !reflect.DeepEqual(got, []string{a, b}) {
		t.Fatalf("expected both paths in order, got %q", got)
	}
}

// The legacy free-form marker still parses (historical sessions, WebUI
// payloads built before the bracketed format).
func TestParsePastedImagePlaceholders_LegacyMarker(t *testing.T) {
	query := "Pasted image saved to disk: /tmp/sprout/simple.png describe this"
	got := ParsePastedImagePlaceholders(query)
	if len(got) != 1 || got[0] != "/tmp/sprout/simple.png" {
		t.Fatalf("legacy marker should still parse, got %q", got)
	}
}

func TestPastedImagePlaceholder_RoundTrip(t *testing.T) {
	path := "/Users/alanp/Desktop/Screenshot 2026-09-18 at 2.53.42\u202fPM.png"
	got := ParsePastedImagePlaceholders(PastedImagePlaceholder(path))
	if len(got) != 1 || got[0] != path {
		t.Fatalf("round trip failed: %q", got)
	}
}

func TestParsePastedImagePlaceholders_NoImages(t *testing.T) {
	if got := ParsePastedImagePlaceholders("just a normal question about [brackets]"); len(got) != 0 {
		t.Fatalf("expected no paths, got %q", got)
	}
}
