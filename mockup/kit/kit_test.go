package kit

import (
	"strings"
	"testing"
)

const doc = `<!doctype html><html lang="en" data-kit="soft" data-palette="terracotta"><head><title>x</title></head>
<body><section data-screen="home"><svg class="i"><use href="#i-cake"/></svg> <svg class="i"><use href="#i-nope"/></svg></section>
<section data-screen="order"></section></body></html>`

func TestCatalogAndData(t *testing.T) {
	if len(palettes) < 150 {
		t.Fatalf("%d palettes, want at least 150", len(palettes))
	}
	if len(icons) < 200 || len(kits) < 5 {
		t.Fatalf("icons %d kits %d", len(icons), len(kits))
	}
	cat := Catalog()
	for _, want := range []string{"editorial", "sage-linen", "shopping-cart"} {
		if !strings.Contains(cat, want) {
			t.Errorf("catalog lacks %q", want)
		}
	}
}

func TestChoiceOf(t *testing.T) {
	c := ChoiceOf(doc)
	if !c.Uses || c.Kit != "soft" || c.Palette != "terracotta" {
		t.Fatalf("choice = %+v", c)
	}
	if c := ChoiceOf(`<html data-kit="nope" data-palette="sage-linen-soft-dark"><body></body></html>`); c.Kit != DefaultKit || c.Palette != "sage-linen" {
		t.Fatalf("fallbacks = %+v", c)
	}
	if c := ChoiceOf(`<html><body><p>plain</p></body></html>`); c.Uses {
		t.Fatal("a plain document was taken as using the kit")
	}
}

// Assemble adds the kit once, inside <body>, with only the icons used; Strip
// gives back the model's document; a plain document is left alone.
func TestAssembleAndStrip(t *testing.T) {
	out := Assemble(doc)
	if strings.Count(out, blockStart) != 1 || !strings.Contains(out, `<symbol id="i-cake"`) || strings.Contains(out, `id="i-coffee"`) {
		t.Fatalf("assembled document is wrong")
	}
	if !strings.Contains(out, "--primary:") || !strings.Contains(out, "--radius-btn:999px") {
		t.Fatal("palette or kit variables missing")
	}
	if i, j := strings.Index(out, "<body>"), strings.Index(out, blockStart); j != i+len("<body>") {
		t.Fatalf("block not just inside body (%d, %d)", i, j)
	}
	if Assemble(out) != out {
		t.Fatal("assembling twice changed the document")
	}
	if Strip(out) != doc {
		t.Fatal("strip did not give back the source")
	}
	plain := `<html><body><p>plain</p></body></html>`
	if Assemble(plain) != plain {
		t.Fatal("a plain document was changed")
	}
}

// The streamer adds the kit right after <body>, even when the tag is split
// across pieces, and only once.
func TestStreamer(t *testing.T) {
	var s Streamer
	var shown strings.Builder
	for _, piece := range []string{`<!doctype html><html data-kit="bold" data-palette="flamingo"><head></head><bo`, `dy class="x">`, `<section data-screen="a">hi</section>`, `</body></html>`} {
		shown.WriteString(s.Feed(piece))
	}
	got := shown.String()
	if strings.Count(got, blockStart) != 1 || !strings.Contains(got, `<body class="x">`+blockStart) {
		t.Fatalf("streamed document = %.200s", got)
	}
	if Strip(got) != `<!doctype html><html data-kit="bold" data-palette="flamingo"><head></head><body class="x"><section data-screen="a">hi</section></body></html>` {
		t.Fatal("the streamed document is not the source plus the kit")
	}
}

func TestKitsHaveBriefs(t *testing.T) {
	if len(kitNames) < 14 {
		t.Fatalf("kits = %v, want at least 14", kitNames)
	}
	used := map[string]bool{}
	for _, name := range kitNames {
		k := kits[name]
		if k.About == "" || len(k.Brief) < 120 || k.Vars["--font-head"] == "" {
			t.Errorf("kit %q needs an about, a brief and its fonts", name)
		}
		if len(k.Compositions) < 3 {
			t.Errorf("kit %q offers %d compositions, want 3 or more", name, len(k.Compositions))
		}
		for _, c := range k.Compositions {
			if _, ok := compBy[c]; !ok {
				t.Errorf("kit %q names unknown composition %q", name, c)
			}
			used[c] = true
		}
		for _, f := range k.Fonts {
			if _, ok := fontBy[f]; !ok {
				t.Errorf("kit %q names unknown font %q", name, f)
			}
		}
		if !strings.Contains(Catalog(), "- "+name+": "+k.About) || !strings.Contains(Catalog(), k.Brief) {
			t.Errorf("the catalogue must carry kit %q's brief", name)
		}
		if strings.Contains(k.Brief, "rating") {
			t.Errorf("kit %q's brief invites a rating", name)
		}
	}
	for _, c := range comps {
		if !used[c.ID] {
			t.Errorf("no kit offers composition %q", c.ID)
		}
		if !strings.Contains(Catalog(), "- "+c.ID+": ") {
			t.Errorf("the catalogue must list composition %q", c.ID)
		}
	}
}

// A page names its composition and heading font; only the named face is
// embedded, and it becomes the heading font.
func TestCompositionAndFont(t *testing.T) {
	doc := `<html data-kit="craft" data-palette="terracotta" data-composition="content" data-font="fraunces"><body></body></html>`
	c := ChoiceOf(doc)
	if c.Composition != "content" || c.Font != "fraunces" {
		t.Fatalf("choice = %+v", c)
	}
	block := Block(c, []string{})
	if strings.Count(block, "@font-face") != 1 || !strings.Contains(block, `font-family:"Fraunces"`) || !strings.Contains(block, `--font-head:"Fraunces",`) {
		t.Errorf("the named font is not embedded as the heading font")
	}
	if plain := Block(ChoiceOf(`<html data-kit="craft" data-font="no-such-font"><body></body></html>`), []string{}); strings.Contains(plain, "@font-face") {
		t.Errorf("an unknown font embedded a face")
	}
	if !strings.Contains(FontLicense(), "SIL Open Font License") || !strings.Contains(FontLicense(), "Fraunces") {
		t.Errorf("the font licence is missing")
	}
	look := Look{Colors: map[string]string{"primary": "#123456"}, FontBody: "Inter, sans-serif"}
	if withLook := Block(Choice{Kit: "craft", Palette: LookPalette, Font: "fraunces", Look: &look, Uses: true}, []string{}); strings.Contains(withLook, "@font-face") {
		t.Errorf("a product's own look got a bundled font over its type")
	}
	if f, b, ok := FontFile("fraunces"); !ok || f.Family != "Fraunces" || len(b) < 1000 {
		t.Errorf("FontFile(fraunces) = %+v, %d bytes, %v", f, len(b), ok)
	}
}

// A host adds a composition and a kit that uses it; a kit naming an unknown
// composition is refused.
func TestExtend(t *testing.T) {
	if err := Extend(Extension{Kits: map[string]Kit{"x": {Compositions: []string{"nope"}}}}); err == nil {
		t.Fatal("a kit with an unknown composition was accepted")
	}
	err := Extend(Extension{
		Compositions: []Composition{{ID: "gallery", About: "A wall of the work itself."}},
		Kits: map[string]Kit{"gallerist": {About: "For galleries.", Brief: "Quiet.", Compositions: []string{"gallery", "type"},
			Vars: map[string]string{"--font-head": "Georgia,serif"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { delete(kits, "gallerist"); comps = comps[:len(comps)-1]; reindex() })
	if c := ChoiceOf(`<html data-kit="gallerist" data-composition="gallery"><body></body></html>`); c.Kit != "gallerist" || c.Composition != "gallery" {
		t.Errorf("choice = %+v", c)
	}
	if !strings.Contains(Catalog(), "- gallery: A wall of the work itself.") {
		t.Errorf("the catalogue does not list the added composition")
	}
}

func TestBlockCarriesTheKitsOwnRules(t *testing.T) {
	craft := Block(Choice{Kit: "craft", Palette: DefaultPalette, Uses: true}, []string{})
	if !strings.Contains(craft, kits["craft"].CSS) || !strings.Contains(craft, ".menu-item") {
		t.Errorf("the block must carry the shared components and the kit's own rules")
	}
	if clean := Block(Choice{Kit: "clean", Palette: DefaultPalette, Uses: true}, []string{}); strings.Contains(clean, kits["craft"].CSS) {
		t.Errorf("one kit's rules must not leak into another's block")
	}
}

func TestDarkPaletteKeepsDarkBandsDark(t *testing.T) {
	dark := Block(Choice{Kit: "bold", Palette: "charcoal-ember-dark", Uses: true}, []string{})
	light := Block(Choice{Kit: "bold", Palette: "charcoal-ember", Uses: true}, []string{})
	if !strings.Contains(dark, darkPaletteCSS) || strings.Contains(light, darkPaletteCSS) {
		t.Errorf("only a dark palette gets the dark-band override")
	}
}

func TestWithChoice(t *testing.T) {
	c := Choice{Kit: "clean", Palette: "sage-linen", Composition: "band", Font: "fraunces"}
	out := WithChoice(`<!doctype html><html data-kit="soft" data-palette="terracotta" data-composition="hero" data-font="lato" data-lang="en"><body></body></html>`, c)
	if got := ChoiceOf(out); got.Kit != "clean" || got.Palette != "sage-linen" || got.Composition != "band" || got.Font != "fraunces" {
		t.Fatalf("choice = %+v", got)
	}
	if !strings.Contains(out, `data-lang="en"`) {
		t.Error("the tag's other attributes were dropped")
	}
	// Empty fields are removed, leaving the document with the default.
	out = WithChoice(`<!doctype html><html data-kit="soft" data-palette="sage-linen"><body></body></html>`, Choice{Palette: "sage-linen"})
	if got := ChoiceOf(out); got.Kit != DefaultKit || got.Palette != "sage-linen" || got.Composition != "" || got.Font != "" {
		t.Fatalf("choice = %+v", got)
	}
	// A document without an <html> tag is left alone.
	plain := `<body>plain</body>`
	if out := WithChoice(plain, c); out != plain {
		t.Errorf("no html tag: document changed: %s", out)
	}
}

// A picker lists every kit and heading font, each one a choice ChoiceOf reads.
func TestKitsAndFontsList(t *testing.T) {
	ks, fs := Kits(), Fonts()
	if len(ks) == 0 || len(fs) == 0 {
		t.Fatalf("kits %d, fonts %d", len(ks), len(fs))
	}
	p := BasePalettes()[0].ID
	for _, k := range ks {
		if c := ChoiceOf(`<html data-kit="` + k.ID + `" data-palette="` + p + `"></html>`); c.Kit != k.ID {
			t.Errorf("kit %q reads back as %q", k.ID, c.Kit)
		}
	}
	for _, f := range fs {
		if f.File != "" {
			t.Errorf("font %q lists its file", f.ID)
		}
		if c := ChoiceOf(`<html data-kit="` + ks[0].ID + `" data-palette="` + p + `" data-font="` + f.ID + `"></html>`); c.Font != f.ID {
			t.Errorf("font %q reads back as %q", f.ID, c.Font)
		}
	}
}
