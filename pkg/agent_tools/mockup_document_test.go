package tools

import (
	"strings"
	"testing"
)

func TestApplyMockupEdits(t *testing.T) {
	doc := "<!doctype html><html><head><style>\n    .bar {\n      background: #fff;\n    }\n</style></head><body>\n    <h1>Crumb</h1>\n    <p>Hours</p>\n</body></html>"

	// Exact text, and text whose indentation the model shifted.
	got, err := applyMockupEdits(doc, "<<<FIND\n<h1>Crumb</h1>\n===\n<h1>Fresh Crumb</h1>\n>>>\n<<<FIND\n  .bar {\n    background: #fff;\n===\n    .bar {\n      background: #14532d;\n>>>")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "<h1>Fresh Crumb</h1>") || !strings.Contains(got, "background: #14532d;") || strings.Contains(got, "#fff") {
		t.Fatalf("edited = %q", got)
	}

	// Adding after a part, and taking a part out.
	got, err = applyMockupEdits(doc, "<<<FIND\n<p>Hours</p>\n===\n<p>Hours</p>\n<p>Reviews</p>\n>>>\n<<<FIND\n<h1>Crumb</h1>\n===\n>>>")
	if err != nil || !strings.Contains(got, "<p>Reviews</p>") || strings.Contains(got, "<h1>") {
		t.Fatalf("add and remove = %q, %v", got, err)
	}

	// Nothing to apply, a find that is not there, and one that is there twice
	// (even with the indentation ignored).
	for _, reply := range []string{
		"Here is the change.",
		"<<<FIND\n<h2>Nope</h2>\n===\n<h2>x</h2>\n>>>",
		"<<<FIND\n    }\n===\n}\n>>>\n<<<FIND\n<p>\n===\n<p class=x>\n>>>",
	} {
		if _, err := applyMockupEdits(doc+"<p>", reply); err == nil {
			t.Errorf("applied %q", reply)
		}
	}
}

func TestFindIgnoringIndent(t *testing.T) {
	doc := "<section>\n\t\t<p>One</p>\n\t\t<p>Two</p>\n</section>"
	start, end, ok := findIgnoringIndent(doc, "<p>One</p>\n    <p>Two</p>")
	if !ok || !strings.Contains(doc[start:end], "<p>One</p>") || !strings.Contains(doc[start:end], "<p>Two</p>") {
		t.Fatalf("indent-shifted find = %d..%d %v", start, end, ok)
	}
	if _, _, ok := findIgnoringIndent(doc+"\n  <p>One</p>\n<p>Two</p>", "<p>One</p>\n<p>Two</p>"); ok {
		t.Error("a find that matches twice once indentation is ignored must not apply")
	}
	if _, _, ok := findIgnoringIndent(doc, "<p>Three</p>"); ok {
		t.Error("a find that is not there must not apply")
	}
}

func TestCleanMockupHTML(t *testing.T) {
	const page = "<!doctype html><html><body><section data-screen=\"home\">Hi</section></body></html>"
	for name, raw := range map[string]string{
		"bare":            page,
		"fenced":          "```html\n" + page + "\n```",
		"prose around":    "Here is your mockup:\n\n" + page + "\n\nEnjoy!",
		"html tag first":  "Sure. <html><body>x</body></html> done",
		"upper doctype":   "<!DOCTYPE html><html><BODY>x</BODY></html>",
		"no closing html": "<!doctype html><html><body>x",
	} {
		got, ok := cleanMockupHTML(raw)
		if !ok {
			t.Errorf("%s: not cleaned", name)
			continue
		}
		low := strings.ToLower(got)
		if !strings.HasPrefix(low, "<!doctype") && !strings.HasPrefix(low, "<html") {
			t.Errorf("%s: cleaned = %q", name, got)
		}
		if strings.Contains(got, "```") || strings.Contains(got, "Enjoy") {
			t.Errorf("%s: prose left in %q", name, got)
		}
	}
	for name, raw := range map[string]string{
		"no document": "I could not draw that.",
		"no body":     "<!doctype html><html><head></head></html>",
		"too large":   "<!doctype html><html><body>" + strings.Repeat("x", maxMockupBytes) + "</body></html>",
	} {
		if _, ok := cleanMockupHTML(raw); ok {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestMockupScreenIDs(t *testing.T) {
	doc := `<section data-screen="home"></section><section data-screen='menu'></section><a data-go="menu"></a><section data-screen="home"></section>`
	got := mockupScreenIDs(doc)
	if strings.Join(got, ",") != "home,menu" {
		t.Fatalf("screens = %v", got)
	}
}

func TestMockupChangeNote(t *testing.T) {
	if got := mockupChangeNote(" make it bigger ", ""); got != "make it bigger" {
		t.Errorf("note = %q", got)
	}
	if got := mockupChangeNote("make it bigger", "the button “Order a cake” on the home screen"); got != "On the button “Order a cake” on the home screen: make it bigger" {
		t.Errorf("pinned note = %q", got)
	}
}

func TestMockupChartersAreEmbedded(t *testing.T) {
	if c := mockupCharter(); !strings.Contains(c, "data-screen") || !strings.Contains(c, "data-go") || strings.HasPrefix(c, "charter-version:") {
		t.Errorf("mockup charter = %.80q", c)
	}
	if c := mockupEditCharter(); !strings.Contains(c, "<<<FIND") || strings.HasPrefix(c, "charter-version:") {
		t.Errorf("edit charter = %.80q", c)
	}
}
