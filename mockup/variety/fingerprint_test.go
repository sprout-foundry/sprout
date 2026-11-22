package variety

import "testing"

const archPage = `<!doctype html><html data-kit="soft" data-palette="terracotta-soft"><body>
<nav class="nav"><div class="container"><a class="nav-brand">Clay &amp; Ember</a></div></nav>
<section class="section"><div class="container split"><div class="stack"><p class="eyebrow">Bolton Street</p>
<h1 class="display">Get your hands in the clay.</h1><a class="btn btn-primary">Book a seat</a></div>
<div class="overlap"><div class="media media-mesh media-arch"><svg class="i i-lg"></svg></div>
<div class="float-card"><strong>4.9</strong> from 128 potters</div></div></div></section>
<section class="section section-soft"><div class="container grid-3"><div class="card">A</div></div></section>
</body></html>`

const timetablePage = `<!doctype html><html data-kit="editorial" data-palette="ink" data-composition="content"><body>
<nav class="nav"><div class="container"><a class="nav-brand">Kiln Club</a></div></nav>
<header class="section"><div class="container"><h1 class="display">Thursday nights at the wheel.</h1>
<table class="table"><tr><td>Thu 7pm</td><td>Beginners</td></tr></table></div></header>
</body></html>`

func TestFingerprintReadsTheFirstScreen(t *testing.T) {
	f := Of(archPage)
	if f.Kit != "soft" || f.Palette != "terracotta" {
		t.Fatalf("kit/palette = %s/%s", f.Kit, f.Palette)
	}
	want := map[string]bool{"media-arch": true, "float-card": true, "display": true, "split": true}
	got := map[string]bool{}
	for _, p := range f.Parts {
		got[p] = true
	}
	for p := range want {
		if !got[p] {
			t.Errorf("first screen parts %v lack %s", f.Parts, p)
		}
	}
	if got["grid-3"] {
		t.Errorf("the next section leaked into the first screen: %v", f.Parts)
	}
	if g := Of(timetablePage); g.Composition != "content" {
		t.Errorf("composition = %q", g.Composition)
	}
}

func TestSimilarity(t *testing.T) {
	a, b := Of(archPage), Of(timetablePage)
	if s := Similarity(a, a); s < 0.99 {
		t.Errorf("a page with itself = %.2f, want 1", s)
	}
	if s := Similarity(a, b); s > 0.4 {
		t.Errorf("an arch hero and a timetable hero = %.2f, want low", s)
	}
}

func TestInventedProof(t *testing.T) {
	if got := InventedProof(archPage); len(got) == 0 {
		t.Errorf("missed the invented rating in the arch page")
	}
	if got := InventedProof(timetablePage); len(got) != 0 {
		t.Errorf("flagged a timetable: %v", got)
	}
	for _, s := range []string{"48 happy dogs", "★★★★★", "4.8 stars", "from 62 walks", "1,200 reviews"} {
		if len(InventedProof("<p>"+s+"</p>")) == 0 {
			t.Errorf("missed %q", s)
		}
	}
	for _, s := range []string{"£18 / 45 minutes", "Open 9.30 to 5", "Classes from £25", "Version 2.1 is out"} {
		if got := InventedProof("<p>" + s + "</p>"); len(got) != 0 {
			t.Errorf("flagged %q: %v", s, got)
		}
	}
}
