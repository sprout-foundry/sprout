package mockup

import (
	"strings"
	"testing"
)

const menuPage = `<!doctype html><html><body><section data-screen="home">
<nav class="nav"><a href="#a">Bakes</a><a href="#b">Visit</a></nav>
<div class="stack"><p class="eyebrow">Hebden Bridge</p><h1>Sourdough on Saturdays</h1><p class="lead">Baked Friday night.</p></div>
<h2>This week</h2>
<div class="stack" data-list="This week's bakes">
  <div class="menu-item"><span>Country white</span><span class="leader"></span><span class="price">£5.50</span></div>
  <div class="menu-item"><span>Seeded rye</span><span class="leader"></span><span class="price">£6.00</span></div>
</div>
<ul class="area-list"><li>Hebden Bridge</li><li>Mytholmroyd</li><li>Todmorden</li></ul>
<div class="row"><a class="btn btn-primary">Reserve</a><a class="btn btn-secondary">Hours</a></div>
</section></body></html>`

func TestKeyed_FindsListsAndTheirColumns(t *testing.T) {
	_, c := Keyed(menuPage, nil)
	if len(c.Lists) != 2 {
		t.Fatalf("lists = %+v, want the bakes and the areas", c.Lists)
	}
	bakes, areas := c.Lists[0], c.Lists[1]
	if bakes.Name != "This week's bakes" || bakes.Items != 2 || strings.Join(bakes.Columns, ",") != "Name,Price" {
		t.Errorf("bakes = %+v", bakes)
	}
	if areas.Name != "This week" || areas.Items != 3 || strings.Join(areas.Columns, ",") != "List item" && strings.Join(areas.Columns, ",") != "Name" {
		t.Errorf("areas = %+v", areas)
	}
	var rye, price Field
	for _, f := range c.Fields {
		switch f.Text {
		case "Seeded rye":
			rye = f
		case "£6.00":
			price = f
		case "Bakes", "Reserve", "Sourdough on Saturdays":
			if f.List != "" {
				t.Errorf("%q is in a list: navigation, buttons and the hero are not lists", f.Text)
			}
		}
	}
	if rye.List != bakes.ID || rye.Item != 1 || rye.Col != 0 || price.Item != 1 || price.Col != 1 {
		t.Errorf("rye = %+v, price = %+v", rye, price)
	}
}

func TestKeyed_KeepsListIDsThroughARekey(t *testing.T) {
	page, c := Keyed(menuPage, nil)
	again, c2 := Keyed(page, &c)
	if again != page || c2.Lists[0].ID != c.Lists[0].ID {
		t.Fatalf("the list's id changed on a rekey")
	}
}

func TestListAddRemoveMove(t *testing.T) {
	page, c := Keyed(menuPage, nil)
	id := c.Lists[0].ID
	// The person's own text stays through every change.
	page, c = ApplyContent(page, c, map[string]string{keyOf(t, c, "Country white"): "Country white (large)"})

	added, ca, err := ListAdd(page, c, id)
	if err != nil || ca.Lists[0].Items != 3 {
		t.Fatalf("add: %v, %+v", err, ca.Lists)
	}
	if strings.Count(added, "Seeded rye") != 2 || !strings.Contains(added, "Country white (large)") {
		t.Errorf("the new item copies the last; the person's words stay")
	}
	keys := map[string]bool{}
	for _, f := range ca.Fields {
		if keys[f.Key] {
			t.Fatalf("key %s used twice", f.Key)
		}
		keys[f.Key] = true
	}

	moved, cm, err := ListMove(added, ca, id, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	first := ""
	for _, f := range cm.Fields {
		if f.List == id && f.Item == 0 && f.Col == 0 {
			first = f.Text
		}
	}
	if first != "Seeded rye" {
		t.Errorf("after the move the first item is %q", first)
	}
	if !strings.Contains(moved, "Country white (large)") {
		t.Error("a move lost the person's words")
	}

	removed, cr, err := ListRemove(moved, cm, id, 0)
	if err != nil || cr.Lists[0].Items != 2 || strings.Count(removed, "Seeded rye") != 1 {
		t.Fatalf("remove: %v items=%d", err, cr.Lists[0].Items)
	}
	if _, _, err := ListRemove(removed, cr, "l99", 0); err == nil {
		t.Error("a list the page does not have is refused")
	}
}

func keyOf(t *testing.T, c Content, text string) string {
	t.Helper()
	for _, f := range c.Fields {
		if f.Text == text {
			return f.Key
		}
	}
	t.Fatalf("no field %q", text)
	return ""
}

// A list takes the heading above it, not one inside its items; a table's
// header row and a pair of single lines are not lists; and a second list
// under the same heading is named after its items.
func TestKeyed_ListNoise(t *testing.T) {
	page := `<!doctype html><html><body><section data-screen="home">
<h2>Pick your favourite</h2>
<div class="grid">
  <div class="card"><h3>Lemon</h3><p>Sharp and light.</p></div>
  <div class="card"><h3>Chocolate</h3><p>Dark and rich.</p></div>
  <div class="card"><h3>Orange</h3><p>Gluten free.</p></div>
</div>
<h2>Three steps</h2>
<ol><li>Choose a cake.</li><li>Pick a day.</li><li>Collect it.</li></ol>
<div class="grid"><div class="card"><h3>Give 48 hours</h3><p>Order ahead.</p></div><div class="card"><h3>Front door</h3><p>Collect at home.</p></div></div>
<table><thead><tr><th>Ordered</th><th>Cake</th><th>Customer</th></tr></thead></table>
<div class="row"><p>Hosted by neighbours</p><p>14 Willow Street</p></div>
</section></body></html>`
	_, c := Keyed(page, nil)
	var names []string
	for _, l := range c.Lists {
		names = append(names, l.Name)
	}
	want := "Pick your favourite|Three steps|Give 48 hours, Front door"
	if strings.Join(names, "|") != want {
		t.Fatalf("lists = %q, want %q", names, want)
	}
}
