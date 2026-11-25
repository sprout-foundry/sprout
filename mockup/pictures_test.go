package mockup

import (
	"strings"
	"testing"
)

const picturePage = `<!doctype html><html><body>
<nav class="nav"><div class="container"><a class="nav-brand"><span class="logo">R</span> Rosa's Cakes</a></div></nav>
<section data-screen="home"><h1>Cakes made to order</h1>
<div class="media media-wide" data-img="cakes" data-label="Photo of the cakes"><svg class="i"><use href="#i-cake"/></svg></div>
</section></body></html>`

func TestPictures_SlotsAndThePersonsPictures(t *testing.T) {
	page, c := Keyed(picturePage, nil)
	if len(c.Pictures) != 2 || c.Pictures[0].Key != LogoSlot || c.Pictures[0].Label != "Logo" || c.Pictures[1].Label != "Photo of the cakes" {
		t.Fatalf("pictures = %+v", c.Pictures)
	}
	if !strings.Contains(page, `class="logo" data-img="logo"`) {
		t.Error("the nav's logo is the slot logo")
	}

	page, c = ApplyPictures(page, c, map[string]string{"cakes": "img-42"})
	if !strings.Contains(page, `data-img-id="img-42"`) || c.Pictures[1].Image != "img-42" {
		t.Fatalf("the picture is not in its slot: %+v", c.Pictures)
	}
	// Saving text, and a redraw that keeps the slot, keep the picture.
	page, c = ApplyContent(page, c, map[string]string{keyOf(t, c, "Cakes made to order"): "Rosa's cakes"})
	if c.Pictures[1].Image != "img-42" || !strings.Contains(page, `data-img-id="img-42"`) {
		t.Fatal("a text save dropped the picture")
	}
	redrawn, rc := Keyed(picturePage, &c)
	if rc.Pictures[1].Image != "img-42" || !strings.Contains(redrawn, `data-img-id="img-42"`) {
		t.Fatal("a redraw that keeps the slot dropped its picture")
	}
	// Clearing the slot brings the stand-in back.
	page, c = ApplyPictures(page, c, map[string]string{"cakes": ""})
	if strings.Contains(page, "data-img-id") || c.Pictures[1].Image != "" {
		t.Fatal("the slot was not cleared")
	}
}

// A redraw that renames a spot holding the person's picture gives it its old
// key back: by its label, or as the only new spot.
func TestPictures_RedrawKeepsRenamedSpots(t *testing.T) {
	saved := Content{Pictures: []Picture{
		{Key: LogoSlot, Label: "Logo"},
		{Key: "loaves", Label: "Photo of this week's loaves", Image: "img-1"},
		{Key: "team", Label: "The bakers at work"},
	}}
	redraw := `<!doctype html><html><body><section data-screen="home">
<div class="media" data-img="counter" data-label="Photo of the loaves on the counter"></div>
<div class="media" data-img="ovens" data-label="The ovens at dawn"></div>
</section></body></html>`
	page, c := Keyed(redraw, &saved)
	if !strings.Contains(page, `data-img="loaves"`) || strings.Contains(page, `data-img="counter"`) {
		t.Fatalf("the renamed spot did not get its key back: %s", page)
	}
	var got Picture
	for _, p := range c.Pictures {
		if p.Key == "loaves" {
			got = p
		}
	}
	if got.Image != "img-1" || !strings.Contains(page, `data-img-id="img-1"`) {
		t.Fatalf("the picture did not stay: %+v", c.Pictures)
	}
	if !strings.Contains(page, `data-img="ovens"`) {
		t.Error("an unrelated new spot was renamed")
	}

	only := `<section data-screen="home"><div class="media" data-img="hero" data-label="A warm welcome"></div></section>`
	if page, _ := Keyed(only, &saved); !strings.Contains(page, `data-img="loaves"`) {
		t.Errorf("the only new spot should take the picture: %s", page)
	}

	kept := `<section data-screen="home"><div class="media" data-img="loaves" data-label="Loaves"></div><div class="media" data-img="extra" data-label="Loaves too"></div></section>`
	if page, _ := Keyed(kept, &saved); !strings.Contains(page, `data-img="extra"`) {
		t.Errorf("a spot the redraw kept should leave the others be: %s", page)
	}
}
