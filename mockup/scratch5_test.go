package mockup

import "testing"

func TestScratch5(t *testing.T) {
	page := `<!doctype html><html><body><section data-screen="home">
<h2>Lessons</h2>
<ul class="lessons">
  <li><span class="name" data-label="Lessons given each week">4</span><span class="desc">Beginners</span></li>
  <li><span class="name" data-label="Lessons given each week">3</span><span class="desc">Advanced</span></li>
  <li><span class="name" data-label="Lessons given each week">2</span><span class="desc">Intermediate</span></li>
</ul>
</section></body></html>`
	_, c := Keyed(page, nil)
	for _, l := range c.Lists {
		t.Logf("name=%q columns=%q", l.Name, l.Columns)
	}
	for _, f := range c.Fields {
		if f.List != "" {
			t.Logf("text=%q label=%q col=%d", f.Text, f.Label, f.Col)
		}
	}
}
