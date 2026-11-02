package tools

// The mockup document: one self-contained HTML file, design/mockup.html, with
// a <section data-screen="id"> per screen. A change is made by edits rather
// than by writing the document again (seconds rather than a minute): the
// model replies with find-and-replace edits on the current document, which
// are applied here. Each edit must find its text exactly once; the model
// often shifts indentation, so a find that does not match as written is
// matched again line by line with the indentation ignored. Edits that cannot
// be applied, or that leave no document, fall back to writing it whole.

import (
	_ "embed"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

//go:embed mockup_prompts/mockup-charter.md
var mockupCharterFile string

//go:embed mockup_prompts/mockup-edit.md
var mockupEditFile string

// maxMockupBytes bounds a mockup document.
const maxMockupBytes = 400 << 10

var (
	mockupEditRe   = regexp.MustCompile(`(?s)<<<FIND\n(.*?)\n===\n(.*?)\n?>>>`)
	mockupScreenRe = regexp.MustCompile(`data-screen\s*=\s*["']([^"']+)["']`)
)

// mockupCharter and mockupEditCharter are the prompts without their version
// line.
func mockupCharter() string     { return stripCharterVersion(mockupCharterFile) }
func mockupEditCharter() string { return stripCharterVersion(mockupEditFile) }

func stripCharterVersion(text string) string {
	if i := strings.Index(text, "\n"); i >= 0 && strings.HasPrefix(text, "charter-version:") {
		text = text[i+1:]
	}
	return strings.TrimSpace(text)
}

// applyMockupEdits applies the model's edits to doc in order.
func applyMockupEdits(doc, reply string) (string, error) {
	edits := mockupEditRe.FindAllStringSubmatch(reply, -1)
	if len(edits) == 0 {
		return "", errors.New("no edits")
	}
	for _, e := range edits {
		find, replace := e[1], e[2]
		if find == "" {
			return "", errors.New("an edit has nothing to find")
		}
		if strings.Count(doc, find) == 1 {
			doc = strings.Replace(doc, find, replace, 1)
			continue
		}
		start, end, ok := findIgnoringIndent(doc, find)
		if !ok {
			return "", fmt.Errorf("edit does not match once: %.60q", find)
		}
		doc = doc[:start] + replace + doc[end:]
	}
	return doc, nil
}

// findIgnoringIndent finds find in doc line by line, ignoring the spaces and
// tabs around each line, and reports where when it occurs exactly once.
func findIgnoringIndent(doc, find string) (int, int, bool) {
	lines := strings.Split(strings.Trim(find, "\n"), "\n")
	for i, l := range lines {
		lines[i] = regexp.QuoteMeta(strings.TrimSpace(l))
	}
	re, err := regexp.Compile(`[ \t]*` + strings.Join(lines, `[ \t]*\r?\n[ \t]*`) + `[ \t]*`)
	if err != nil {
		return 0, 0, false
	}
	found := re.FindAllStringIndex(doc, 2)
	if len(found) != 1 {
		return 0, 0, false
	}
	return found[0][0], found[0][1], true
}

// cleanMockupHTML trims what a model may put around the document (code
// fences, a sentence before or after) and reports whether a document is
// left: it must start at <!doctype or <html, carry a <body, and fit the size
// bound.
func cleanMockupHTML(raw string) (string, bool) {
	text := strings.TrimSpace(raw)
	lower := strings.ToLower(text)
	start := strings.Index(lower, "<!doctype")
	if start < 0 {
		start = strings.Index(lower, "<html")
	}
	if start < 0 {
		return "", false
	}
	text = text[start:]
	lower = lower[start:]
	if end := strings.LastIndex(lower, "</html>"); end >= 0 {
		text = text[:end+len("</html>")]
	}
	if len(text) > maxMockupBytes || !strings.Contains(strings.ToLower(text), "<body") {
		return "", false
	}
	return text, true
}

// mockupScreenIDs lists the document's data-screen ids in order, once each.
func mockupScreenIDs(doc string) []string {
	seen := map[string]bool{}
	ids := []string{}
	for _, m := range mockupScreenRe.FindAllStringSubmatch(doc, -1) {
		id := strings.TrimSpace(m[1])
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

// mockupChangeNote is the change as the model reads it: pinned to a part
// when one is named ("On the button “Order a cake” on the home screen: make
// it bigger").
func mockupChangeNote(change, part string) string {
	change = strings.TrimSpace(change)
	part = strings.TrimSpace(part)
	if part == "" {
		return change
	}
	return "On " + part + ": " + change
}

// mockupWholeRequest is the request for a whole document: the plan (when
// there is one) and, for a change, the current mockup and the change.
func mockupWholeRequest(planJSON []byte, current, note string) string {
	var b strings.Builder
	if len(planJSON) > 0 {
		b.WriteString("The agreed plan:\n")
		b.Write(planJSON)
		b.WriteString("\n")
	}
	switch {
	case current != "" && note != "":
		b.WriteString("\nThe current mockup:\n")
		b.WriteString(current)
		b.WriteString("\n\nThe change they asked for: ")
		b.WriteString(note)
		b.WriteString("\n\nWrite the whole updated document.")
	case note != "":
		b.WriteString("\nThey also asked: ")
		b.WriteString(note)
		b.WriteString("\n\nWrite the mockup.")
	default:
		b.WriteString("\nWrite the mockup.")
	}
	return b.String()
}

// mockupEditRequest is the request for edits to the current mockup.
func mockupEditRequest(current, note string) string {
	return "The current mockup:\n" + current + "\n\nThe change they asked for: " + note
}
