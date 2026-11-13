package kit

import (
	"strings"
	"testing"
)

func TestLookClean_KeepsOnlyPlainValues(t *testing.T) {
	l, ok := Look{
		Colors: map[string]string{
			"primary": "#2563eb",
			"bg":      "rgb(10, 12, 20)",
			"text":    "red;}</style><script>alert(1)</script>",
			"accent":  "url(javascript:alert(1))",
			"hover":   "#fff", // not a slot
		},
		FontBody: "Inter, system-ui, sans-serif",
		FontHead: "x;}</style>",
		Radius:   "8px",
	}.Clean()
	if !ok {
		t.Fatal("a look with good values is kept")
	}
	if l.Colors["primary"] != "#2563eb" || l.Colors["bg"] != "rgb(10, 12, 20)" {
		t.Fatalf("plain colours kept: %v", l.Colors)
	}
	for _, k := range []string{"text", "accent", "hover"} {
		if _, bad := l.Colors[k]; bad {
			t.Fatalf("%s kept: %v", k, l.Colors)
		}
	}
	if l.FontBody != "Inter, system-ui, sans-serif" || l.FontHead != "" {
		t.Fatalf("fonts: %q %q", l.FontBody, l.FontHead)
	}
	if _, ok := (Look{Radius: "calc(1px)", Colors: map[string]string{"bg": "blue"}}).Clean(); ok {
		t.Fatal("nothing plain is nothing")
	}
}

func TestLook_TravelsWithTheDocumentAndStylesIt(t *testing.T) {
	doc := `<!doctype html><html lang="en" data-kit="clean" data-palette="project"><head><title>x</title></head><body><section data-screen="board">Board</section></body></html>`
	doc = WithLook(doc, Look{Colors: map[string]string{"primary": "#7c3aed", "bg": "#0b0d12"}, FontBody: "Inter, sans-serif", Radius: "6px", Dark: true})
	if _, ok := LookOf(doc); !ok {
		t.Fatal("the look is read back from the document")
	}
	again := WithLook(doc, Look{Colors: map[string]string{"primary": "#111111"}})
	if strings.Count(again, `name="mockupkit-look"`) != 1 {
		t.Fatal("stamping again replaces the look")
	}

	page := Assemble(doc)
	for _, want := range []string{"--primary:#7c3aed;", "--bg:#0b0d12;", "--font-body:Inter, sans-serif;", "--radius:6px;", "--radius-btn:4.5px;", ".section-dark{"} {
		if !strings.Contains(page, want) {
			t.Fatalf("assembled page lacks %q", want)
		}
	}
	// Assembled again (a reload), it is the same page.
	if Assemble(page) != page {
		t.Fatal("assembly is stable")
	}
}

func TestLook_ProjectPaletteWithoutALookFallsBack(t *testing.T) {
	doc := `<html data-palette="project"><body></body></html>`
	if c := ChoiceOf(doc); c.Palette != DefaultPalette || c.Look != nil {
		t.Fatalf("choice: %+v", c)
	}
}

func TestStreamer_DrawsInTheLookAsItStreams(t *testing.T) {
	s := Streamer{Look: &Look{Colors: map[string]string{"primary": "#7c3aed"}}}
	out := s.Feed(`<html data-kit="clean" data-palette="project"><head></head><body>`)
	if !strings.Contains(out, "--primary:#7c3aed;") {
		t.Fatal("the look is applied from the first screen")
	}
}
