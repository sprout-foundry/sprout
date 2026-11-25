package kit

import (
	"strings"
	"testing"
)

// Every palette in the catalogue gets a family, derived from its primary
// colour's hue and chroma: the family is not hand-typed, so a palette
// added to palettes.json gets one.
func TestFamiliesAreDerivedForEveryPalette(t *testing.T) {
	known := map[string]bool{}
	for _, f := range Families {
		known[f] = true
	}
	if len(palettes) < 150 {
		t.Fatalf("%d palettes, want the catalogue to be present", len(palettes))
	}
	for _, p := range palettes {
		f := p.Family()
		if f == "" || !known[f] {
			t.Errorf("palette %s has unknown family %q", p.ID, f)
		}
	}
	// A spread of obvious cases.
	cases := map[string]string{
		"sage-linen":    "green",
		"terracotta":    "red",
		"slate-blue":    "blue",
		"espresso":      "brown/earth",
		"onyx-pearl":    "neutral",
		"cherry-blossom": "pink",
		"aurora":        "teal",
		"lavender-fields": "indigo/violet",
		"chamomile":     "yellow/gold",
		"honey-bee":     "orange",
	}
	for id, want := range cases {
		p, ok := paletteBy[id]
		if !ok {
			t.Errorf("palette %s missing from the catalogue", id)
			continue
		}
		if p.Family() != want {
			t.Errorf("palette %s family = %q, want %q", id, p.Family(), want)
		}
	}
}

// The shortlist is seeded (reproducible), spans families, respects
// avoided families, and always honours a named colour.
func TestShortlist(t *testing.T) {
	// Same seed → same shortlist.
	a := Shortlist([]string{"bakery"}, nil, nil, 42, 6)
	b := Shortlist([]string{"bakery"}, nil, nil, 42, 6)
	if len(a) == 0 {
		t.Fatal("shortlist empty")
	}
	if len(a) != len(b) {
		t.Fatalf("shortlist differs between runs: %v vs %v", a, b)
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			t.Fatalf("shortlist differs between runs: %v vs %v", a, b)
		}
	}
	// Spans families.
	fams := map[string]bool{}
	for _, p := range a {
		fams[p.Family()] = true
	}
	if len(fams) < 2 {
		t.Errorf("shortlist spans only one family: %v", a)
	}
	// Avoided families are left out.
	avoid := map[string]bool{"green": true, "blue": true, "orange": true, "red": true, "pink": true, "teal": true}
	for _, p := range Shortlist(nil, nil, avoid, 42, 6) {
		if avoid[p.Family()] {
			t.Errorf("shortlist kept an avoided family: %s", p.Family())
		}
	}
	// A named colour wins over avoidance.
	named := Shortlist(nil, []string{"blue"}, map[string]bool{"blue": true}, 42, 6)
	if len(named) == 0 {
		t.Fatal("named colour lost the shortlist")
	}
	for _, p := range named {
		if p.Family() != "blue" {
			t.Errorf("named blue got %s (%s)", p.ID, p.Family())
		}
	}
	// Several named colours keep every named family.
	both := Shortlist(nil, []string{"blue", "orange"}, nil, 42, 6)
	got := map[string]bool{}
	for _, p := range both {
		got[p.Family()] = true
	}
	if !got["blue"] || !got["orange"] {
		t.Errorf("shortlist did not span the named colours: %v", both)
	}
}

// A brief's mood words narrow the shortlist to palettes that fit.
func TestShortlistFitsTheMood(t *testing.T) {
	// "bakery"/"sweet"/"playful" are the mood words of the playful-sweet
	// palettes.
	got := Shortlist([]string{"bakery", "sweet", "playful"}, nil, nil, 42, 6)
	if len(got) == 0 {
		t.Fatal("shortlist empty")
	}
	fit := 0
	for _, p := range got {
		mood := strings.ToLower(p.Mood)
		if strings.Contains(mood, "bakery") || strings.Contains(mood, "sweet") || strings.Contains(mood, "playful") {
			fit++
		}
	}
	if fit == 0 {
		t.Errorf("shortlist fits nothing about the brief: %v", got)
	}
}
