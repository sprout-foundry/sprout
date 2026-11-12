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
	for _, name := range []string{"clean", "soft", "editorial", "bold", "minimal", "studio", "craft"} {
		k, ok := kits[name]
		if !ok {
			t.Fatalf("kit %q is missing", name)
		}
		if k.About == "" || len(k.Brief) < 200 || k.Vars["--font-head"] == "" {
			t.Errorf("kit %q needs an about, a brief and its fonts", name)
		}
		if !strings.Contains(Catalog(), "- "+name+": "+k.About+" "+k.Brief) {
			t.Errorf("the catalogue must carry kit %q's brief", name)
		}
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
