package webcontent

import (
	"strings"

	"golang.org/x/net/html"
)

// blockTags are HTML elements that produce a newline in text output.
var blockTags = map[string]bool{
	// Headings
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	// Flow content
	"p": true, "div": true, "blockquote": true, "pre": true, "hr": true,
	// Semantic HTML5
	"section": true, "article": true, "main": true, "header": true, "footer": true,
	"nav": true, "aside": true, "figure": true, "figcaption": true,
	"details": true, "summary": true,
	// Lists
	"li": true,
	// Table
	"table": true, "thead": true, "tbody": true, "tfoot": true,
	"tr": true, "th": true, "td": true,
	// Other
	"br": true,
}

// stripTags are elements whose content (and the element itself) are removed entirely.
var stripTags = map[string]bool{
	"script": true, "style": true, "noscript": true,
	// Form controls produce no useful text when scraped.
	"input":    true,
	"select":   true,
	"textarea": true,
	"button":   true,
	"legend":   true,
	"fieldset": true,
	// SVG elements produce path data noise, not readable text.
	"svg": true,
	// Template tags contain document fragments not rendered until cloned.
	"template": true,
}

// genericAltText is alt text on <img> tags that adds no information.
// Common CMS placeholders like "Image" or empty strings.
var genericAltText = map[string]bool{
	"":      true,
	"Image": true, "image": true,
	"Photo": true, "photo": true,
}

// ---------------------------------------------------------------------------
// Head metadata extraction
// ---------------------------------------------------------------------------

// headMetadata holds useful information extracted from an HTML <head>.
type headMetadata struct {
	title, description, ogTitle, ogDesc, canonical string
}

// extractHeadMetadata walks the children of a <head> element and populates
// useful metadata: <title>, <meta name="description">, <meta property="og:title">,
// <meta property="og:description">, and <link rel="canonical">.
func extractHeadMetadata(head *html.Node) headMetadata {
	var m headMetadata
	for c := head.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		switch c.Data {
		case "title":
			m.title = collectText(c)
		case "meta":
			name := getAttr(c, "name")
			prop := getAttr(c, "property")
			val := getAttr(c, "content")
			switch {
			case name == "description" && m.description == "":
				m.description = val
			case prop == "og:title" && m.ogTitle == "":
				m.ogTitle = val
			case prop == "og:description" && m.ogDesc == "":
				m.ogDesc = val
			}
		case "link":
			if strings.EqualFold(getAttr(c, "rel"), "canonical") && getAttr(c, "href") != "" {
				m.canonical = getAttr(c, "href")
			}
		}
	}
	return m
}

// collectText returns all text node descendants concatenated together.
func collectText(n *html.Node) string {
	var b strings.Builder
	var walkText func(*html.Node)
	walkText = func(cur *html.Node) {
		if cur.Type == html.TextNode {
			b.WriteString(cur.Data)
			return
		}
		for c := cur.FirstChild; c != nil; c = c.NextSibling {
			walkText(c)
		}
	}
	walkText(n)
	return strings.TrimSpace(b.String())
}

// formatMetadata returns a human-readable block for the extracted metadata,
// or an empty string if nothing useful was found.
func formatMetadata(m headMetadata) string {
	var b strings.Builder
	wrote := false

	if m.title != "" {
		b.WriteString("Title: ")
		b.WriteString(m.title)
		wrote = true
	} else if m.ogTitle != "" {
		b.WriteString("Title: ")
		b.WriteString(m.ogTitle)
		wrote = true
	}

	if m.description != "" {
		if wrote {
			b.WriteString("\n")
		}
		b.WriteString("Description: ")
		b.WriteString(m.description)
		wrote = true
	} else if m.ogDesc != "" {
		if wrote {
			b.WriteString("\n")
		}
		b.WriteString("Description: ")
		b.WriteString(m.ogDesc)
		wrote = true
	}

	if m.canonical != "" {
		if wrote {
			b.WriteString("\n")
		}
		b.WriteString("URL: ")
		b.WriteString(m.canonical)
		wrote = true
	}

	if !wrote {
		return ""
	}
	return b.String()
}

// findHead returns the first <head> element in the document, or nil.
func findHead(doc *html.Node) *html.Node {
	var f func(*html.Node) *html.Node
	f = func(n *html.Node) *html.Node {
		if n.Type == html.ElementNode && n.Data == "head" {
			return n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if found := f(c); found != nil {
				return found
			}
		}
		return nil
	}
	return f(doc)
}

// ---------------------------------------------------------------------------
// HTML → text conversion
// ---------------------------------------------------------------------------

// HTMLToText converts an HTML document to plain text, extracting only visible
// content. Block elements become newlines, list items are numbered or bulleted,
// and script/style/noscript content is stripped entirely.
// Useful <head> metadata (title, description, canonical URL) is extracted and
// prepended to the output. The result is post-processed to collapse
// whitespace, remove useless lines, and deduplicate repeated navigation links.
func HTMLToText(htmlBody string) string {
	doc, err := html.Parse(strings.NewReader(htmlBody))
	if err != nil {
		return normalizeWhitespace(htmlBody)
	}

	// Extract metadata from <head> before converting the body to text.
	var meta headMetadata
	if head := findHead(doc); head != nil {
		meta = extractHeadMetadata(head)
	}

	var b strings.Builder
	olCount := 0
	walk(doc, &b, &olCount, "ul")
	body := postProcess(normalizeWhitespace(b.String()))

	metaBlock := formatMetadata(meta)
	if metaBlock == "" {
		return body
	}
	return metaBlock + "\n\n" + body
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

	// If this is a tag to strip (script, style, noscript, form controls),
	// or the <head> (whose useful content was already extracted by
	// extractHeadMetadata), skip its entire subtree.
	if stripTags[n.Data] || n.Data == "head" {
		return
	}

	// Skip elements marked aria-hidden="true" — these are modals, off-canvas
	// menus, and other overlay content not visible until user interaction.
	if getAttr(n, "aria-hidden") == "true" {
		return
	}

	// Skip <label> elements only when they reference a form control via the
	// "for" attribute — the associated input is already stripped, making
	// the label meaningless stub text. Labels without "for" may carry
	// useful context on sites with non-semantic HTML.
	if n.Data == "label" && getAttr(n, "for") != "" {
		return
	}

	// Skip elements with the HTML hidden attribute.
	if hasHiddenAttr(n) {
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

	// For images, extract alt text unless it's a generic placeholder.
	if n.Data == "img" {
		if alt := getAttr(n, "alt"); alt != "" && !genericAltText[alt] {
			b.WriteString(alt)
		}
	}

	// For links, extract the href and append it after the link text.
	if n.Data == "a" {
		href := getAttr(n, "href")
		if href != "" {
			// Write link children into a temporary builder so we can check
			// whether the link contributed non-whitespace text before
			// committing it (and the URL suffix) to the output.
			var linkBuf strings.Builder
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c, &linkBuf, olCount, listKind)
			}
			if strings.TrimSpace(linkBuf.String()) != "" {
				b.WriteString(linkBuf.String())
				b.WriteString(" (")
				b.WriteString(href)
				b.WriteString(")")
			}
			// If the link contained only whitespace, drop it entirely.
			return
		}
	}

	// Recurse into children.
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, b, olCount, listKind)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// getAttr returns the value of the named HTML attribute, or "" if not found.
func getAttr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

// hasHiddenAttr returns true if the element has an HTML "hidden" attribute.
func hasHiddenAttr(n *html.Node) bool {
	for _, a := range n.Attr {
		if a.Key == "hidden" {
			return true
		}
	}
	return false
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

// normalizeWhitespace collapses runs of 3+ newlines down to 2,
// normalizes CR+LF to LF, and strips leading/trailing whitespace.
func normalizeWhitespace(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
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

// ---------------------------------------------------------------------------
// Post-processing pipeline
// ---------------------------------------------------------------------------

// postProcess cleans up converted text for maximum readability:
//   - Collapses whitespace-only lines to blank (preserves leading whitespace on content lines, e.g. <pre>)
//   - Joins orphan bullet prefixes ("- " with no text) with the next content line
//   - Removes useless lines (CAPTCHA refs, bare URLs from empty links, etc.)
//   - Deduplicates repeated navigation links
func postProcess(text string) string {
	lines := strings.Split(text, "\n")

	// Phase 1: Collapse whitespace-only lines to blank.
	// Content lines (including <pre> with intentional leading spaces) are untouched.
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			lines[i] = ""
		}
	}

	// Phase 2: Join orphan bullet prefixes with the next content line.
	//   "- " alone followed by "About (url)" on the next line → "- About (url)"
	for i := 0; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t == "-" || t == "- " {
			merged := false
			for j := i + 1; j < len(lines); j++ {
				nt := strings.TrimSpace(lines[j])
				if nt != "" {
					lines[i] = "- " + nt
					lines[j] = ""
					merged = true
					break
				}
			}
			if !merged {
				lines[i] = ""
			}
		}
	}

	// Phase 3: Remove useless lines (trailing whitespace is cleaned up but
	// leading indentation is preserved for <pre> blocks).
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			filtered = append(filtered, "")
			continue
		}
		if isUselessLine(strings.TrimSpace(line)) {
			continue
		}
		filtered = append(filtered, strings.TrimRight(line, " \t"))
	}

	// Phase 4: Collapse consecutive blank lines to a single blank line.
	collapsed := make([]string, 0, len(filtered))
	prevBlank := false
	for _, line := range filtered {
		if line == "" {
			if !prevBlank {
				collapsed = append(collapsed, "")
			}
			prevBlank = true
		} else {
			collapsed = append(collapsed, line)
			prevBlank = false
		}
	}

	// Phase 5: Deduplicate repeated bullet-point navigation links.
	collapsed = deduplicateBulletLinks(collapsed)

	return strings.TrimSpace(strings.Join(collapsed, "\n"))
}

// isUselessLine returns true for lines that add no readable value.
func isUselessLine(s string) bool {
	// CAPTCHA fields in scraped forms.
	if s == "CAPTCHA" {
		return true
	}

	// A bare "(url)" with no link text — produced by <a> elements that
	// contained only whitespace children (already mostly suppressed in walk,
	// but catches edge cases after line-level trimming).
	if len(s) > 2 && s[0] == '(' && s[len(s)-1] == ')' {
		inner := strings.TrimSpace(s[1 : len(s)-1])
		if inner != "" && (strings.HasPrefix(inner, "http") || strings.HasPrefix(inner, "/") || strings.HasPrefix(inner, "tel:") || strings.HasPrefix(inner, "mailto:")) {
			return true
		}
	}

	// Standalone "Image" with no context — likely from wrapper elements
	// around <img> tags that have no alt text.
	if s == "Image" {
		return true
	}

	return false
}

// deduplicateBulletLinks removes later occurrences of "- text (url)" lines
// that have already been seen. This effectively removes repeated navigation
// blocks (header nav, footer nav, mobile drawer) that are common on websites.
// Numbered list items (1. 2. 3.) are intentionally left alone since they
// are more likely to be body content.
func deduplicateBulletLinks(lines []string) []string {
	seen := make(map[string]bool, 64)
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, "- ") {
			// Normalize internal whitespace for comparison so that
			// "-  About   (url)" and "- About (url)" are treated as the same.
			key := strings.Join(strings.Fields(line), " ")
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		result = append(result, line)
	}
	return result
}
