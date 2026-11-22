// Package variety measures how alike first looks are, so a change to the
// kit or the charter can be checked for whether it makes pages more
// varied or less: a structural fingerprint of a page's first screen, a
// similarity between two of them, and the invented social proof a page
// should never carry.
package variety

import (
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/html"

	"github.com/sprout-foundry/sprout/mockup/kit"
)

// Fingerprint is what a first look's first screen is made of.
type Fingerprint struct {
	Kit, Palette string
	// Composition is the first screen's composition when the page names one
	// (data-composition on <html>).
	Composition string
	// Font is the page's named heading face (data-font), when it names one.
	Font string
	// Parts are the kit classes the first screen is built from, sorted.
	Parts []string
}

var (
	htmlTagRe = regexp.MustCompile(`(?is)<html\b[^>]*>`)
	htmlAttr  = func(name string) *regexp.Regexp {
		return regexp.MustCompile(`(?i)\bdata-` + name + `\s*=\s*["']([^"']*)["']`)
	}
	compositionRe = htmlAttr("composition")
	fontRe        = htmlAttr("font")
)

// Of fingerprints a document.
func Of(doc string) Fingerprint {
	c := kit.ChoiceOf(doc)
	f := Fingerprint{Kit: c.Kit, Palette: strings.TrimSuffix(strings.TrimSuffix(c.Palette, "-soft"), "-dark")}
	if tag := htmlTagRe.FindString(doc); tag != "" {
		f.Composition = firstGroup(compositionRe, tag)
		f.Font = firstGroup(fontRe, tag)
	}
	f.Parts = firstScreenParts(kit.Strip(doc))
	return f
}

func firstGroup(re *regexp.Regexp, s string) string {
	if m := re.FindStringSubmatch(s); m != nil {
		return strings.ToLower(strings.TrimSpace(m[1]))
	}
	return ""
}

// firstScreenParts collects the classes of the page's first screen: the
// first <section> (or <header>) after the navigation that holds a heading.
func firstScreenParts(doc string) []string {
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return nil
	}
	hero := find(root, func(n *html.Node) bool {
		return n.Type == html.ElementNode && (n.Data == "section" || n.Data == "header") &&
			!hasClass(n, "nav") && find(n, isHeading) != nil
	})
	if hero == nil {
		return nil
	}
	seen := map[string]bool{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for _, c := range classes(n) {
				seen[c] = true
			}
			if n.Data == "section" && n != hero && n.Parent != nil && n.Parent.Data != "section" {
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(hero)
	parts := make([]string, 0, len(seen))
	for p := range seen {
		parts = append(parts, p)
	}
	sort.Strings(parts)
	return parts
}

func isHeading(n *html.Node) bool {
	return n.Type == html.ElementNode && (n.Data == "h1" || hasClass(n, "display"))
}

func find(n *html.Node, match func(*html.Node) bool) *html.Node {
	if match(n) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if f := find(c, match); f != nil {
			return f
		}
	}
	return nil
}

func classes(n *html.Node) []string {
	for _, a := range n.Attr {
		if a.Key == "class" {
			return strings.Fields(strings.ToLower(a.Val))
		}
	}
	return nil
}

func hasClass(n *html.Node, c string) bool {
	for _, x := range classes(n) {
		if x == c {
			return true
		}
	}
	return false
}

// Similarity is how alike two first screens are, from 0 (nothing shared) to
// 1 (the same): mostly the parts they are built from, then the kit and the
// composition.
func Similarity(a, b Fingerprint) float64 {
	score := 0.6 * jaccard(a.Parts, b.Parts)
	if a.Kit == b.Kit {
		score += 0.2
	}
	switch {
	case a.Composition != "" && a.Composition == b.Composition:
		score += 0.2
	case a.Composition == "" && b.Composition == "":
		score += 0.2 * jaccard(a.Parts, b.Parts)
	}
	return score
}

func jaccard(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1
	}
	in := map[string]bool{}
	for _, x := range a {
		in[x] = true
	}
	both, union := 0, len(in)
	for _, x := range b {
		if in[x] {
			both++
		} else {
			union++
		}
	}
	return float64(both) / float64(union)
}

var proofRe = regexp.MustCompile(`(?i)(\b[1-5][.,]\d\s*(★|·|stars?|/\s*5|rating|from\b)|★{3,}|\b\d[\d,]*\s*\+?\s*(happy|satisfied|five-star|5-star)\b|\bfrom\s+\d[\d,]*\s+(reviews|ratings|customers|clients|guests|walks|potters|students|families)\b|\b\d[\d,]*\s+(reviews|ratings)\b)`)

// InventedProof lists the ratings, review counts and customer numbers a page
// states: a first look has no reviews to quote, so each is invented.
func InventedProof(doc string) []string {
	text := textOf(kit.Strip(doc))
	var out []string
	for _, m := range proofRe.FindAllString(text, -1) {
		out = append(out, strings.TrimSpace(m))
	}
	return out
}

func textOf(doc string) string {
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return doc
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
			return
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteString(" ")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return b.String()
}
