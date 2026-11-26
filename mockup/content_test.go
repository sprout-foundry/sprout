package mockup

import (
	"strings"
	"testing"
)

const invite = `<!doctype html><html><head><title>Maya</title><style>p{color:red}</style></head><body>
<section data-screen="invitation"><h1>Maya turns thirty</h1>
<p data-label="Event date">Saturday, June 14 &middot; 7pm</p>
<p>Come hungry, <strong>stay late</strong>, bring someone.</p>
<a class="btn btn-primary" data-go="rsvp">RSVP by June 7</a>
<svg><text>ignored</text></svg><script>var x = "<p>no</p>";</script></section>
<section data-screen="rsvp"><h2>Will you be there?</h2>
<form><label for="n">Your name</label><input id="n" placeholder="Priya Raman"><textarea>typed</textarea><button>Send</button></form>
<p>   </p><p>·</p></section></body></html>`

func fieldByText(c Content, text string) (Field, bool) {
	for _, f := range c.Fields {
		if f.Text == text {
			return f, true
		}
	}
	return Field{}, false
}

func TestKeyed_EveryPieceOfTextHasAKeyLabelAndSection(t *testing.T) {
	doc, c := Keyed(invite, nil)
	want := []struct{ text, label, section string }{
		{"Maya turns thirty", "Main heading", "Invitation · Maya turns thirty"},
		{"Saturday, June 14 · 7pm", "Event date", "Invitation · Maya turns thirty"},
		{"Come hungry, stay late, bring someone.", "Text", "Invitation · Maya turns thirty"},
		{"RSVP by June 7", "Button", "Invitation · Maya turns thirty"},
		{"Will you be there?", "Heading", "Rsvp · Will you be there?"},
		{"Your name", "Field label", "Rsvp · Will you be there?"},
		{"Priya Raman", "Placeholder", "Rsvp · Will you be there?"},
		{"Send", "Button", "Rsvp · Will you be there?"},
	}
	if len(c.Fields) != len(want) {
		t.Fatalf("fields = %+v", c.Fields)
	}
	for i, w := range want {
		f := c.Fields[i]
		if f.Text != w.text || f.Label != w.label || f.Section != w.section || f.Key == "" {
			t.Errorf("field %d = %+v, want %+v", i, f, w)
		}
		if !strings.Contains(doc, `data-c="`+f.Key+`"`) && !strings.Contains(doc, `data-c-ph="`+f.Key+`"`) {
			t.Errorf("%q has no key on the page", f.Text)
		}
	}
	for _, skipped := range []string{"ignored", "no", "typed", "Maya", "stay late", "·"} {
		if _, ok := fieldByText(c, skipped); ok {
			t.Errorf("%q should not be a field", skipped)
		}
	}
	if !strings.Contains(doc, `var x = "<p>no</p>";`) {
		t.Error("scripts are left exactly as written")
	}
}

func TestKeyed_KeysAreKeptAndNewTextGetsNewOnes(t *testing.T) {
	doc, first := Keyed(invite, nil)
	again, second := Keyed(doc, nil)
	if again != doc {
		t.Error("keying a keyed page changes nothing")
	}
	if len(first.Fields) != len(second.Fields) || first.Fields[0].Key != second.Fields[0].Key {
		t.Errorf("keys moved: %+v vs %+v", first.Fields, second.Fields)
	}
	added := strings.Replace(doc, "</section></body>", "<p>A new line</p></section></body>", 1)
	_, third := Keyed(added, nil)
	f, ok := fieldByText(third, "A new line")
	if !ok {
		t.Fatal("new text gets a field")
	}
	for _, old := range first.Fields {
		if old.Key == f.Key {
			t.Errorf("new text reused key %s", f.Key)
		}
	}
}

func TestApplyContent_ThePersonsTextStaysThroughARedraw(t *testing.T) {
	doc, c := Keyed(invite, nil)
	button, _ := fieldByText(c, "RSVP by June 7")
	name, _ := fieldByText(c, "Priya Raman")
	doc, c = ApplyContent(doc, c, map[string]string{button.Key: "Count me in <now>", name.Key: "Your full name"})
	if !strings.Contains(doc, "Count me in &lt;now&gt;") || !strings.Contains(doc, `placeholder="Your full name"`) {
		t.Fatalf("the page shows the person's text:\n%s", doc)
	}
	if f, _ := fieldByText(c, "Count me in <now>"); !f.Mine {
		t.Error("the field is marked as theirs")
	}
	// A redraw writes the invented text again; the person's text wins.
	redrawn := strings.Replace(doc, "Count me in &lt;now&gt;", "RSVP by June 1", 1)
	out, after := Keyed(redrawn, &c)
	if !strings.Contains(out, "Count me in &lt;now&gt;") {
		t.Error("the person's text survives a redraw")
	}
	if f, _ := fieldByText(after, "Count me in <now>"); !f.Mine || f.Key != button.Key {
		t.Errorf("field after redraw = %+v", f)
	}
}

func TestWithContent_EmbedsAndReadsBack(t *testing.T) {
	doc, c := Keyed(invite, nil)
	page := WithContent(doc, c)
	got, ok := ContentOf(page)
	if !ok || len(got.Fields) != len(c.Fields) {
		t.Fatalf("content read back = %+v", got)
	}
	if strings.Count(WithContent(page, c), `id="sprout-content"`) != 1 {
		t.Error("embedding twice keeps one content file")
	}
	if StripContent(page) != doc {
		t.Error("stripping gives back the page")
	}
}

func TestApplyContent_ReplacesWordsAndKeepsIcons(t *testing.T) {
	page := `<body><p>Come hungry, <strong>stay late</strong>, bring someone.</p><button><svg><use href="#i-send"></use></svg> Send RSVP</button></body>`
	doc, c := Keyed(page, nil)
	sentence, _ := fieldByText(c, "Come hungry, stay late, bring someone.")
	button, _ := fieldByText(c, "Send RSVP")
	out, after := ApplyContent(doc, c, map[string]string{sentence.Key: "Dinner at eight.", button.Key: "Count me in"})
	if strings.Contains(out, "stay late") || !strings.Contains(out, ">Dinner at eight.</p>") {
		t.Errorf("the sentence is replaced whole:\n%s", out)
	}
	if !strings.Contains(out, `<use href="#i-send"></use></svg>Count me in</button>`) {
		t.Errorf("the icon stays and the words change:\n%s", out)
	}
	if len(after.Fields) != 2 {
		t.Errorf("fields = %+v", after.Fields)
	}
}

func TestKeyed_ALineBreakSeparatesWords(t *testing.T) {
	_, c := Keyed(`<html><body><h1>Bring a book,<br>leave with another.</h1></body></html>`, nil)
	if len(c.Fields) != 1 || c.Fields[0].Text != "Bring a book, leave with another." {
		t.Fatalf("fields = %+v, want the heading's words separated at the line break", c.Fields)
	}
}

// On a page with no screens (a built app), a heading is not filed under the
// heading before it: each heading starts its own section, with its texts.
func TestKeyed_HeadingDoesNotJoinThePreviousSection(t *testing.T) {
	_, c := Keyed(`<main><h2>Seeded wholemeal</h2><p>Soft enough for sandwiches.</p><h2>Country white</h2><p>A big open crumb.</p></main>`, nil)
	sections := map[string]string{}
	for _, f := range c.Fields {
		sections[f.Text] = f.Section
	}
	if got := sections["Country white"]; got != "Country white" {
		t.Fatalf("the Country white heading has section %q, want its own", got)
	}
	if got := sections["A big open crumb."]; got != "Country white" {
		t.Fatalf("text after a heading has section %q, want %q", got, "Country white")
	}
	if got := sections["Soft enough for sandwiches."]; got != "Seeded wholemeal" {
		t.Fatalf("text after a heading has section %q, want %q", got, "Seeded wholemeal")
	}
}

// A redraw that numbers the page afresh can put a saved key on another kind
// of element; the saved text stays off it rather than turning a field's label
// into its placeholder.
func TestKeyed_SavedTextStaysOnItsKindOfElement(t *testing.T) {
	form := `<section data-screen="book"><form>
<div class="field"><label for="n">Your name</label><input id="n" placeholder="Your name"></div>
<div class="field"><label for="e">Email</label><input id="e" type="email" placeholder="you@example.com"></div>
</form></section>`
	_, first := Keyed(form, nil)
	for i := range first.Fields {
		first.Fields[i].Mine = true
	}
	shifted := `<section data-screen="book"><p>Fill in your details.</p>` + form[len(`<section data-screen="book">`):]
	_, c := Keyed(shifted, &first)
	got := map[string]string{}
	for _, f := range c.Fields {
		got[f.Label+"|"+f.Text] = f.Key
	}
	for _, want := range []string{"Field label|Your name", "Placeholder|Your name", "Field label|Email", "Placeholder|you@example.com"} {
		if _, ok := got[want]; !ok {
			t.Fatalf("missing %q in %+v", want, c.Fields)
		}
	}
}

// A placeholder the drawing named is still a placeholder: its saved text does
// not move onto a field label when a redraw shifts the keys.
func TestKeyed_NamedPlaceholderStaysAPlaceholder(t *testing.T) {
	form := `<section data-screen="book"><form>
<div class="field"><label for="e">Email</label><input id="e" data-label="Your email" placeholder="you@example.com"></div>
</form></section>`
	_, first := Keyed(form, nil)
	for i := range first.Fields {
		first.Fields[i].Mine = true
	}
	_, c := Keyed(`<section data-screen="book"><p>Fill in your details.</p>`+form[len(`<section data-screen="book">`):], &first)
	for _, f := range c.Fields {
		if f.Kind == "Field label" && f.Text != "Email" {
			t.Fatalf("the label reads %q: %+v", f.Text, c.Fields)
		}
	}
}

// Text saved before kinds were kept is guarded the same way: a named
// placeholder's text does not move onto a field label when a redraw shifts
// the keys, and it is not discarded — it stays in the content.
func TestKeyed_OldContentNamedPlaceholderIsKeptNotMoved(t *testing.T) {
	form := `<section data-screen="book"><form>
<div class="field"><label for="e">Email</label><input id="e" data-label="Your email" placeholder="you@example.com"></div>
</form></section>`
	_, first := Keyed(form, nil)
	for i := range first.Fields {
		first.Fields[i].Kind = ""
		first.Fields[i].Mine = true
	}
	_, c := Keyed(`<section data-screen="book"><p>Fill in your details.</p>`+form[len(`<section data-screen="book">`):], &first)
	for _, f := range c.Fields {
		if f.Kind == "Field label" && f.Text != "Email" {
			t.Fatalf("the label reads %q: %+v", f.Text, c.Fields)
		}
	}
	for _, f := range c.Fields {
		if f.Text == "you@example.com" && f.Mine {
			return
		}
	}
	t.Fatalf("the person's text is not kept in the content: %+v", c.Fields)
}

// Text the person sets goes onto the element its key is on now, even for a
// field saved before kinds were kept.
func TestApplyContent_OldFieldTextIsSetOnItsElement(t *testing.T) {
	page := `<section data-screen="book"><form>
<div class="field"><label for="e">Email</label><input id="e" data-label="Your email" placeholder="you@example.com"></div>
</form></section>`
	_, c := Keyed(page, nil)
	for i := range c.Fields {
		c.Fields[i].Kind = ""
	}
	ph, _ := fieldByText(c, "you@example.com")
	out, after := ApplyContent(page, c, map[string]string{ph.Key: "email@work.com"})
	if !strings.Contains(out, `placeholder="email@work.com"`) {
		t.Fatalf("the person's edit is not on the page:\n%s", out)
	}
	for _, f := range after.Fields {
		if f.Key == ph.Key && (f.Text != "email@work.com" || !f.Mine) {
			t.Fatalf("the field = %+v", after.Fields)
		}
	}
}

// The person's text a guard kept (its key is on another kind of element) is
// not discarded: an element that still holds their words takes the field, so
// the person's words stay protected on that element through the next draw.
func TestKeyed_KeptTextReHomesToTheElementThatHoldsIt(t *testing.T) {
	form := `<section data-screen="book"><form>
<div class="field"><label for="e">Email</label><input id="e" data-label="Your email" placeholder="you@example.com"></div>
</form></section>`
	_, first := Keyed(form, nil)
	for i := range first.Fields {
		if first.Fields[i].Kind == "Placeholder" {
			first.Fields[i].Mine = true
		}
	}
	// A redraw adds a paragraph and renumbers the page: the placeholder's
	// key is now on a paragraph, not the placeholder.
	shifted := `<section data-screen="book"><p>Fill in your details.</p>` + form[len(`<section data-screen="book">`):]
	_, c := Keyed(shifted, &first)
	// The person's text re-homed to the placeholder (a same-kind element that
	// still holds it), not the paragraph its key landed on.
	ph, ok := fieldByText(c, "you@example.com")
	if !ok || ph.Kind != "Placeholder" || !ph.Mine {
		t.Fatalf("the person's text is not re-homed onto its placeholder: %+v", c.Fields)
	}
	para, _ := fieldByText(c, "Fill in your details.")
	// A stable draw keeps the keys and the model rewrites the placeholder's
	// words. The person's text, protected on that key and kind, wins.
	stable := `<section data-screen="book"><p data-c="` + para.Key + `">Fill in your details.</p><form>
<div class="field"><label for="e">Email</label><input id="e" data-c-ph="` + ph.Key + `" data-label="Your email" placeholder="email address"></div>
</form></section>`
	_, after := Keyed(stable, &c)
	if f, _ := fieldByText(after, "you@example.com"); f.Kind != "Placeholder" || !f.Mine {
		t.Errorf("the person's text is not kept on its placeholder through a stable redraw: %+v", after.Fields)
	}
}

// A text no element holds (the model rewrote it) is kept in the content
// under the key it was saved with, not discarded, and the guard keeps it off
// the element that key now sits on.
func TestKeyed_KeptTextNoElementHoldsIsKeptNotDiscarded(t *testing.T) {
	form := `<section data-screen="book"><form>
<div class="field"><label for="e">Email</label><input id="e" data-label="Your email" placeholder="you@example.com"></div>
</form></section>`
	_, first := Keyed(form, nil)
	for i := range first.Fields {
		if first.Fields[i].Kind == "Placeholder" {
			first.Fields[i].Mine = true
		}
	}
	// A redraw renumbers and the model rewrote the placeholder's words, so
	// no element holds the person's text.
	shifted := `<section data-screen="book"><p>Fill in your details.</p><form>
<div class="field"><label for="e">Email</label><input id="e" data-label="Your email" placeholder="type it here"></div>
</form></section>`
	_, c := Keyed(shifted, &first)
	// It is not discarded.
	kept := false
	for _, f := range c.Fields {
		if f.Text == "you@example.com" && f.Mine {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("the person's text is not kept in the content: %+v", c.Fields)
	}
	// And the guard keeps it off the label its key landed on.
	for _, f := range c.Fields {
		if f.Kind == "Field label" && f.Text != "Email" {
			t.Fatalf("the label reads %q: %+v", f.Text, c.Fields)
		}
	}
}
