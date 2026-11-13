package kit

import (
	"errors"
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

func TestLook_NotesAreProseAndNeverReachTheStylesheet(t *testing.T) {
	l, ok := Look{
		Colors: map[string]string{"primary": "#2563eb"},
		Notes:  "A dark app with a left rail and dense tables.",
	}.Clean()
	if !ok || l.Notes == "" {
		t.Fatal("notes are kept")
	}
	doc := WithLook(`<html data-kit="clean" data-palette="project"><head></head><body></body></html>`, l)
	page := Assemble(doc)
	// The notes travel in the document's <meta> (so it reassembles the same),
	// but never into the stylesheet the kit writes.
	style := page[strings.Index(page, "<style data-mockupkit>"):]
	style = style[:strings.Index(style, "</style>")]
	if strings.Contains(style, "left rail") {
		t.Fatalf("notes never reach the page's CSS: %s", style)
	}
	// They are still readable by whoever draws the mockup.
	back, ok := LookOf(doc)
	if !ok || back.LookNotes() != "A dark app with a left rail and dense tables." {
		t.Fatalf("notes travel with the document: %+v", back)
	}
}

func TestParseLook_ToleratesFencesAndProse(t *testing.T) {
	reply := "Here is the look:\n```json\n{\"colors\":{\"primary\":\"#7c3aed\"},\"notes\":\"A board.\"}\n```\nDone."
	l, ok := ParseLook(reply)
	if !ok || l.Colors["primary"] != "#7c3aed" || l.Notes != "A board." {
		t.Fatalf("parsed: %+v %v", l, ok)
	}
	if _, ok := ParseLook("no json here"); ok {
		t.Fatal("no object is no look")
	}
}

func TestSaveAndLoadLook(t *testing.T) {
	root := t.TempDir()
	if _, err := LoadLook(root); !errors.Is(err, ErrNoLook) {
		t.Fatalf("a project with no look: %v", err)
	}
	saved, err := SaveLook(root, Look{Colors: map[string]string{"primary": "#7c3aed"}, Notes: "A board."})
	if err != nil || !saved {
		t.Fatalf("save: %v %v", saved, err)
	}
	l, err := LoadLook(root)
	if err != nil || l.Colors["primary"] != "#7c3aed" || l.LookNotes() != "A board." {
		t.Fatalf("load: %+v %v", l, err)
	}
	// A look with nothing usable is not written.
	if wrote, err := SaveLook(root, Look{Colors: map[string]string{"primary": "url(x)"}}); err != nil || wrote {
		t.Fatalf("nothing usable is not saved: %v %v", wrote, err)
	}
}
