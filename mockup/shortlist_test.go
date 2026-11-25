package mockup

import (
	"context"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/mockup/kit"
)

// The avoided families are the recent looks' families; the current look's
// family too for a Fresh redraw; a named colour wins over all of them.
func TestAvoidedFamilies(t *testing.T) {
	avoid := []kit.Choice{
		{Kit: "craft", Palette: "terracotta", Composition: "art", Font: "fraunces"},
		{Kit: "craft", Palette: "sage-linen-dark", Composition: "art"},
	}
	fams := avoidedFamilies(avoid, false, "", nil)
	// terracotta → red, sage-linen → green (a -dark variant keeps its family).
	if !fams["red"] || !fams["green"] {
		t.Errorf("families = %v, want red and green", fams)
	}
	// A Fresh redraw avoids the current look's family as well.
	current := kit.Assemble(`<!doctype html><html data-kit="soft" data-palette="ocean-mist"><body><section data-screen="home"><h1>X</h1></section></body></html>`)
	fams = avoidedFamilies(avoid, true, current, nil)
	if !fams["teal"] {
		t.Errorf("fresh avoided = %v, want teal (current family)", fams)
	}
	// A named colour wins: nothing is avoided.
	if fams = avoidedFamilies(avoid, true, current, []string{"blue"}); len(fams) != 0 {
		t.Errorf("named colour should win: %v", fams)
	}
}

// A fresh look must use a family not in Avoid and different from the
// current look's family: the steering names both.
func TestSteerRequestFreshUsesADifferentFamily(t *testing.T) {
	fresh := kit.Assemble(`<!doctype html><html data-kit="soft" data-palette="sage-linen"><body><section data-screen="home"><h1>X</h1></section></body></html>`)
	s := steerRequest([]kit.Choice{{Kit: "craft", Palette: "terracotta"}}, true, fresh, nil)
	if !strings.Contains(s, "a palette family other than") {
		t.Fatalf("steer = %q", s)
	}
	// The current family (green) and the avoided family (red) are named.
	if !strings.Contains(s, "green") || !strings.Contains(s, "red") {
		t.Errorf("steer = %q, want green and red", s)
	}
	// Not fresh: the steering says "unless the idea plainly needs one".
	s = steerRequest([]kit.Choice{{Kit: "craft", Palette: "terracotta"}}, false, "", nil)
	if !strings.Contains(s, "unless the idea plainly needs") {
		t.Errorf("steer = %q", s)
	}
	// A named colour wins: no family is steered away from.
	s = steerRequest([]kit.Choice{{Kit: "craft", Palette: "terracotta"}}, false, "", []string{"blue"})
	if strings.Contains(s, "palette family") {
		t.Errorf("named colour: steer = %q", s)
	}
}

// The shortlist in the charter: the model is offered a handful of base
// palettes, the charter lists only those, and a named colour always wins.
func TestDrawShortlistInTheCharter(t *testing.T) {
	page := `<!doctype html><html data-kit="soft" data-palette="blueprint"><body><section data-screen="home"><h1>X</h1></section></body></html>`
	stream, _ := replies(t, page)
	var systems []string
	systemReply := func(_ context.Context, system, user string, onChunk func(string)) (string, error) {
		systems = append(systems, system)
		return stream(t.Context(), system, user, onChunk)
	}
	if _, err := Draw(t.Context(), systemReply, Request{
		Plan: []byte(`{"goal":"a bakery site for Rosa"}`),
		Seed: 42,
	}, nil); err != nil {
		t.Fatal(err)
	}
	if len(systems) == 0 {
		t.Fatal("no request")
	}
	charter := systems[0]
	// The charter's palette shortlist names only shortlisted palettes and
	// says to pick from it.
	if !strings.Contains(charter, "Palette shortlist") {
		t.Errorf("charter lacks the shortlist: %.200s", charter)
	}
	if !strings.Contains(charter, "palette") {
		t.Errorf("charter lacks the palette list: %.200s", charter)
	}
	// The charter's palette list is the shortlist, not the whole kit.
	idx := strings.Index(charter, "## Palette shortlist")
	if idx == -1 {
		t.Fatal("charter lacks the shortlist block")
	}
	// The shortlist block runs to the next "##" section or to the end.
	block := charter[idx:]
	end := strings.Index(block, "\n## ")
	if end != -1 {
		block = block[:end]
	}
	for _, line := range strings.Split(block, "\n") {
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		id := strings.TrimSpace(strings.Split(line[2:], " ")[0])
		id = strings.TrimSuffix(id, ":")
		found := false
		for _, p := range kit.Shortlist([]string{"bakery", "site", "rosa"}, nil, nil, 42, ShortlistSize) {
			if p.ID == id {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("charter lists palette %s which is not in the shortlist", id)
		}
	}
}

func TestNamedColourWinsTheShortlist(t *testing.T) {
	// The plan names "blue and orange": the shortlist spans both families
	// and no others.
	tokens, named := briefWords([]byte(`{"goal":"a blue and orange bakery site"}`), "")
	if len(named) != 2 {
		t.Fatalf("named = %v, want 2 families", named)
	}
	// tokens/named order follows text order; the set is what matters.
	got := map[string]bool{}
	for _, f := range named {
		got[f] = true
	}
	if !got["blue"] || !got["orange"] {
		t.Errorf("named = %v, want blue and orange", named)
	}
	shortlist := kit.Shortlist(tokens, named, nil, 42, ShortlistSize)
	if len(shortlist) == 0 {
		t.Fatal("shortlist empty")
	}
	for _, p := range shortlist {
		if !got[p.Family()] {
			t.Errorf("shortlist palette %s is family %s, not a named one", p.ID, p.Family())
		}
	}
	// Avoidance does not apply when the person named a colour.
	shortlist = kit.Shortlist(tokens, named, map[string]bool{"blue": true}, 42, ShortlistSize)
	if len(shortlist) == 0 {
		t.Fatal("named colour lost the shortlist")
	}
}

func TestBriefWordsTokens(t *testing.T) {
	tokens, named := briefWords([]byte(`{"version":1,"goal":"a walking club site for the neighbourhood","steps":[{"description":"list the walks"}]}`), "")
	joined := strings.Join(tokens, " ")
	if strings.Contains(joined, "the") {
		t.Errorf("stop word leaked: %v", tokens)
	}
	if !strings.Contains(joined, "walking") {
		t.Errorf("mood word missing: %v", tokens)
	}
	if len(named) != 0 {
		t.Errorf("named = %v, want none", named)
	}
}

// A Fresh redraw's shortlist contains no palette from the current look's
// family — the family is the thing the redraw must leave behind.
func TestFreshNeverReturnsTheCurrentFamily(t *testing.T) {
	current := kit.Assemble(`<!doctype html><html data-kit="soft" data-palette="forest-trail"><body><section data-screen="home"><h1>X</h1></section></body></html>`)
	currentFamily := ""
	for _, p := range kit.BasePalettes() {
		if p.ID == "forest-trail" {
			currentFamily = p.Family()
		}
	}
	if currentFamily == "" {
		t.Fatal("forest-trail not found")
	}
	avoid := avoidedFamilies([]kit.Choice{{Kit: "craft", Palette: "terracotta"}}, true, current, nil)
	if !avoid[currentFamily] {
		t.Errorf("fresh avoided should include the current family %s: %v", currentFamily, avoid)
	}
	shortlist := kit.Shortlist([]string{"walking"}, nil, avoid, 7, ShortlistSize)
	for _, p := range shortlist {
		if p.Family() == currentFamily {
			t.Errorf("shortlist palette %s is the current family %s", p.ID, currentFamily)
		}
	}
}
