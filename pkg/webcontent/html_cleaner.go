package webcontent

import (
	"strings"

	"golang.org/x/net/html"
)

// blockTags are HTML elements that produce a newline in text output.
var blockTags = map[string]bool{
	// Headings
	"h1":         true,
	"h2":         true,
	"h3":         true,
	"h4":         true,
	"h5":         true,
	"h6":         true,
	// Flow content
	"p":          true,
	"div":        true,
	"blockquote": true,
	"pre":        true,
	"hr":         true,
	// Semantic HTML5
	"section":    true,
	"article":    true,
	"main":       true,
	"header":     true,
	"footer":     true,
	"nav":        true,
	"aside":      true,
	"figure":     true,
	"figcaption": true,
	"details":    true,
	"summary":    true,
	// Lists
	"li":         true,
	// Table
	"table":      true,
	"thead":      true,
	"tbody":      true,
	"tfoot":      true,
	"tr":         true,
	"th":         true,
	"td":         true,
	// Other
	"br":         true,
}

// stripTags are elements whose content (and the element itself) are removed entirely.
var stripTags = map[string]bool{
	"script":   true,
	"style":    true,
	"noscript": true,
}

// HTMLToText converts an HTML document to plain text, extracting only visible
// content. Block elements are converted to newlines, list items are numbered
// or bulleted, and script/style/noscript content is stripped entirely.
func HTMLToText(htmlBody string) string {
	doc, err := html.Parse(strings.NewReader(htmlBody))
	if err != nil {
		// If parsing fails, return the original with basic cleanup.
		return normalizeWhitespace(htmlBody)
	}

	var b strings.Builder
	olCount := 0
	walk(doc, &b, &olCount, "ul")
	return normalizeWhitespace(b.String())
}

// walk traverses the HTML node tree and writes text content to b.
// olCount tracks the item counter per <ol> nesting level (shared by pointer).
// listKind tracks the type ("ul" or "ol") of the innermost enclosing list.
func walk(n *html.Node, b *strings.Builder, olCount *int, listKind string) {
	switch n.Type {
	case html.TextNode:
		b.WriteString(n.Data)
		return
	case html.DocumentNode:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, b, olCount, listKind)
		}
		return
	case html.CommentNode:
		return
	case html.ElementNode:
		// fall through to element handling below
	default:
		return
	}

	// If this is a tag to strip (script, style, noscript), skip its entire subtree.
	if stripTags[n.Data] {
		return
	}

	// Emit a newline before block-level open tags.
	if blockTags[n.Data] {
		b.WriteByte('\n')
	}

	// Update list numbering when entering a list.
	switch n.Data {
	case "ol":
		listKind = "ol"
		*olCount = 0
	case "ul":
		listKind = "ul"
	case "li":
		switch listKind {
		case "ol":
			*olCount++
			b.WriteString("\n")
			b.WriteString(itoa(*olCount))
			b.WriteString(". ")
		case "ul":
			b.WriteString("\n- ")
		}
	}

	// For images, extract alt text.
	if n.Data == "img" {
		alt := getAttr(n, "alt")
		if alt != "" {
			b.WriteString(alt)
		}
	}

	// For links, extract the href and append it after the link text.
	if n.Data == "a" {
		href := getAttr(n, "href")
		if href != "" {
			startLen := b.Len()
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c, b, olCount, listKind)
			}
			if b.Len() > startLen {
				b.WriteString(" (")
				b.WriteString(href)
				b.WriteString(")")
			}
			return
		}
	}

	// Recurse into children.
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, b, olCount, listKind)
	}
}

// getAttr returns the value of the named HTML attribute, or "" if not found.
func getAttr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

// itoa converts a small non-negative integer to its decimal string without fmt.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// normalizeWhitespace collapses runs of 3 or more newlines down to 2 newlines,
// normalizes CR+LF to LF, and strips leading/trailing whitespace from the result.
func normalizeWhitespace(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	// Collapse 3+ consecutive newlines into exactly 2.
	var b strings.Builder
	prevNewline := 0
	for _, r := range s {
		if r == '\n' {
			prevNewline++
			if prevNewline <= 2 {
				b.WriteRune('\n')
			}
		} else {
			prevNewline = 0
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}
