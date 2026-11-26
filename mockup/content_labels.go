package mockup

import (
	"regexp"
	"strings"

	nethtml "golang.org/x/net/html"
)

var (
	priceRe   = regexp.MustCompile(`(?i)^(from\s+)?[£$€]\s?\d[\d,]*(\.\d\d)?(\s?(each|pp|a head|per \w+|/\s?\w+))?$|^\d[\d,]*(\.\d\d)?\s?(€|pts?|points)$`)
	timeRe    = regexp.MustCompile(`(?i)^(\w+day,?\s+)?\d{1,2}([:.]\d{2})?\s?(am|pm)?(\s?[–-]\s?\d{1,2}([:.]\d{2})?\s?(am|pm)?)?$`)
	dateRe    = regexp.MustCompile(`(?i)^(mon|tues|wednes|thurs|fri|satur|sun)day\b|\b\d{1,2}(st|nd|rd|th)?\s+(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\b|\b(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\s+\d{1,2}(st|nd|rd|th)?\b`)
	phoneRe   = regexp.MustCompile(`^\+?[\d ()-]{9,}$`)
	numberRe  = regexp.MustCompile(`^\d{1,3}\.?$`)
	weekdayRe = regexp.MustCompile(`(?i)^(mon|tue|tues|wed|wednes|thu|thur|thurs|fri|sat|satur|sun)(day)?\.?$`)
	emailRe   = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[a-z]{2,}$`)
)

// textLabel names a piece of text by what it says or the kit part it is, for
// one the drawing gave no label: a price, a time, a date, contact details,
// the business's name. "" when it is none of those.
func textLabel(text, class string) string {
	classes := " " + class + " "
	has := func(c string) bool { return strings.Contains(classes, " "+c+" ") }
	t := strings.TrimSpace(text)
	switch {
	case has("nav-brand") || has("logo"):
		return "Name"
	case has("price") || priceRe.MatchString(t):
		return "Price"
	case emailRe.MatchString(t):
		return "Email"
	case phoneRe.MatchString(t) && strings.Count(t, " ") < 5:
		return "Phone"
	case numberRe.MatchString(t):
		return "Number"
	case has("time") || timeRe.MatchString(t):
		return "Time"
	case has("day") || weekdayRe.MatchString(t):
		return "Day"
	case has("date") || (len(t) < 40 && dateRe.MatchString(t)):
		return "Date"
	case has("eyebrow") || has("kicker"):
		return "Small label"
	case has("lead"):
		return "Intro"
	case has("stat"):
		return "Figure"
	case has("stat-label"):
		return "Figure label"
	case has("badge") || has("chip"):
		return "Tag"
	}
	return ""
}

// interactiveTag is text the person acts on or a form names: it keeps its
// element's label (a button saying a date is still a button).
func interactiveTag(tag, class string) bool {
	if strings.Contains(" "+class+" ", " nav-brand ") {
		return false
	}
	switch tag {
	case "a", "button", "option", "label", "title":
		return true
	}
	return strings.Contains(" "+class+" ", " btn")
}

func elementLabel(tag, class string) string {
	switch tag {
	case "title":
		return "Page title"
	case "h1":
		return "Main heading"
	case "h2", "h3", "h4", "h5", "h6":
		return "Heading"
	case "button":
		return "Button"
	case "a":
		if strings.Contains(" "+class+" ", " btn") {
			return "Button"
		}
		return "Link"
	case "label":
		return "Field label"
	case "li":
		return "List item"
	case "th", "td":
		return "Table cell"
	case "option":
		return "Choice"
	case "blockquote", "q":
		return "Quote"
	}
	return "Text"
}

// elementLabels are the labels elementLabel and placeholderLabel give by the
// kind of element alone.
var elementLabels = map[string]bool{
	"Page title": true, "Main heading": true, "Heading": true, "Button": true, "Link": true,
	"Field label": true, "List item": true, "Table cell": true, "Choice": true, "Quote": true,
	"Text": true, "Placeholder": true,
}

// sameKind reports whether saved text belongs on an element of kind now.
// A key is a position on the page, so a redraw can put it on another kind
// of element (a field's label where its placeholder was). Text saved with
// its kind goes back only onto that kind; text saved before kinds were kept
// goes back only when its label names the kind — a name the drawing gave a
// text (data-label) names no kind, so its text is kept, not moved onto
// another kind of element. An element that already holds the person's words
// is the one the text was saved from — unless it is another kind of element
// that happens to hold them; that key then keeps looking for the words.
func sameKind(saved Field, now, nowText string) bool {
	if nowText == saved.Text {
		return saved.Kind == "" || saved.Kind == now
	}
	if saved.Kind != "" {
		return saved.Kind == now
	}
	return elementLabels[saved.Label] && saved.Label == now
}

func placeholderLabel(t nethtml.Token) string {
	if l := attr(t, labelAttr); l != "" {
		return l
	}
	return "Placeholder"
}

// humanize turns a screen id ("host-view") into words ("Host view").
