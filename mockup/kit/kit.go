// Package kit is the design kit planner mockups are built from: style
// kits (one shared component stylesheet in a few looks, each with a design
// brief the model composes to and a few rules of its own), contrast-checked
// colour palettes, an icon set (Lucide, ISC licence) and a small runtime for
// screens, tabs and forms. The model names a kit and a palette on the
// document's <html> element and references icons by name; the platform adds
// the kit itself, so the model writes markup instead of styling and drawing,
// and every mockup looks designed rather than generated. Strip gives back the
// model's own document (the source), which is what a change is made to;
// Assemble adds the kit to it for showing and saving.
package kit

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

//go:embed icons.json
var iconsFile []byte

//go:embed palettes.json
var palettesFile []byte

//go:embed kits/base.css
var baseCSS string

//go:embed kits/kits.json
var kitsFile []byte

//go:embed runtime.js
var runtimeJS string

// Palette is one colour scheme.
type Palette struct {
	ID     string            `json:"id"`
	Mood   string            `json:"mood"`
	Colors map[string]string `json:"colors"`
}

// Kit is one look of the component stylesheet: what it suits, how a page in
// it is composed (the brief), its variables and its own rules.
type Kit struct {
	About string            `json:"about"`
	Brief string            `json:"brief"`
	Vars  map[string]string `json:"vars"`
	CSS   string            `json:"css"`
}

var (
	icons      map[string]string
	iconSource string
	palettes   []Palette
	paletteBy  map[string]Palette
	kits       map[string]Kit
	kitNames   []string
)

func init() {
	var ic struct {
		Source string            `json:"source"`
		Icons  map[string]string `json:"icons"`
	}
	mustJSON(iconsFile, &ic)
	icons, iconSource = ic.Icons, ic.Source
	mustJSON(palettesFile, &palettes)
	paletteBy = make(map[string]Palette, len(palettes))
	for _, p := range palettes {
		paletteBy[p.ID] = p
	}
	mustJSON(kitsFile, &kits)
	for k := range kits {
		kitNames = append(kitNames, k)
	}
	sort.Strings(kitNames)
}

func mustJSON(b []byte, v any) {
	if err := json.Unmarshal(b, v); err != nil {
		panic(fmt.Sprintf("mockupkit: bad embedded data: %v", err))
	}
}

// DefaultKit and DefaultPalette are used when a document names none (or an
// unknown one).
const (
	DefaultKit     = "clean"
	DefaultPalette = "slate-blue"
)

const (
	blockStart = "<!--mockupkit:start-->"
	blockEnd   = "<!--mockupkit:end-->"
)

var (
	htmlTagRe  = regexp.MustCompile(`(?is)<html\b[^>]*>`)
	attrRe     = regexp.MustCompile(`(?i)\bdata-(kit|palette)\s*=\s*["']([^"']*)["']`)
	bodyOpenRe = regexp.MustCompile(`(?is)<body\b[^>]*>`)
	iconRefRe  = regexp.MustCompile(`#i-([a-z0-9-]+)`)
	blockRe    = regexp.MustCompile(`(?s)` + regexp.QuoteMeta(blockStart) + `.*?` + regexp.QuoteMeta(blockEnd))
)

// Choice is the kit and palette a document asks for.
type Choice struct {
	Kit, Palette string
	// Look is the project's own look, for a document that names the
	// project palette and carries one (look.go).
	Look *Look
	// Uses is whether the document uses the kit at all (it names a kit or a
	// palette, or references an icon). A document that does not is shown as
	// written.
	Uses bool
}

// ChoiceOf reads the kit and palette named on the document's <html> element.
func ChoiceOf(doc string) Choice {
	c := Choice{Kit: DefaultKit, Palette: DefaultPalette}
	if tag := htmlTagRe.FindString(doc); tag != "" {
		for _, m := range attrRe.FindAllStringSubmatch(tag, -1) {
			v := strings.TrimSpace(strings.ToLower(m[2]))
			switch strings.ToLower(m[1]) {
			case "kit":
				c.Uses = true
				if _, ok := kits[v]; ok {
					c.Kit = v
				}
			case "palette":
				c.Uses = true
				if v == LookPalette {
					if l, ok := LookOf(doc); ok {
						c.Palette, c.Look = LookPalette, &l
					}
				} else if _, ok := paletteBy[v]; ok {
					c.Palette = v
				} else if base := strings.TrimSuffix(strings.TrimSuffix(v, "-dark"), "-soft"); paletteBy[base].ID != "" {
					// A variant that did not pass the contrast checks falls
					// back to its theme's light scheme.
					c.Palette = base
				}
			}
		}
	}
	if !c.Uses && iconRefRe.MatchString(doc) {
		c.Uses = true
	}
	return c
}

// Block is what the platform adds to a document: the palette and kit as CSS,
// an icon sprite (the named icons, or all of them when iconNames is nil) and
// the runtime, marked so it can be stripped again.
func Block(c Choice, iconNames []string) string {
	var b strings.Builder
	b.WriteString(blockStart)
	b.WriteString(`<style data-mockupkit>:root{`)
	pal, ok := paletteBy[c.Palette]
	if !ok {
		pal = paletteBy[DefaultPalette]
	}
	vars := map[string]string{}
	for k, v := range pal.Colors {
		vars["--"+strings.ReplaceAll(k, "_", "-")] = v
	}
	kit := kits[c.Kit]
	for k, v := range kit.Vars {
		vars[k] = v
	}
	// The project's look goes over both: its colours, type and corners.
	if c.Look != nil {
		for k, v := range lookVars(*c.Look) {
			vars[k] = v
		}
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "%s:%s;", k, vars[k])
	}
	b.WriteString("}\n")
	b.WriteString(baseCSS)
	if strings.HasSuffix(c.Palette, "-dark") || (c.Look != nil && c.Look.Dark) {
		// The page is already dark: a dark band is a slightly lifted surface
		// rather than the text colour, which on a dark palette is light.
		b.WriteString("\n" + darkPaletteCSS)
	}
	if kit.CSS != "" {
		b.WriteString("\n")
		b.WriteString(kit.CSS)
	}
	b.WriteString("</style>")
	if iconNames == nil {
		for n := range icons {
			iconNames = append(iconNames, n)
		}
	}
	sort.Strings(iconNames)
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" style="display:none" aria-hidden="true"><!-- Icons: %s -->`, iconSource)
	for _, n := range iconNames {
		if body, ok := icons[n]; ok {
			fmt.Fprintf(&b, `<symbol id="i-%s" viewBox="0 0 24 24">%s</symbol>`, n, body)
		}
	}
	b.WriteString("</svg><script data-mockupkit>")
	b.WriteString(runtimeJS)
	b.WriteString("</script>")
	b.WriteString(blockEnd)
	return b.String()
}

// darkPaletteCSS keeps section-dark dark on a dark palette.
const darkPaletteCSS = `.section-dark{background:var(--soft);color:var(--text)}.section-dark h1,.section-dark h2,.section-dark h3,.section-dark .display{color:var(--text)}.section-dark .muted,.section-dark .lead{color:var(--muted)}`

// Strip removes what Assemble added, giving back the model's document.
func Strip(doc string) string {
	return blockRe.ReplaceAllString(doc, "")
}

// IconsUsed lists the known icons a document references.
func IconsUsed(doc string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range iconRefRe.FindAllStringSubmatch(doc, -1) {
		if _, ok := icons[m[1]]; ok && !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// Assemble is the document as it is shown and committed: the model's
// document with the kit added just inside <body>. A document that does not
// use the kit is returned as written.
func Assemble(doc string) string {
	src := Strip(doc)
	c := ChoiceOf(src)
	if !c.Uses {
		return src
	}
	used := IconsUsed(src)
	if used == nil {
		used = []string{}
	}
	block := Block(c, used)
	if loc := bodyOpenRe.FindStringIndex(src); loc != nil {
		return src[:loc[1]] + block + src[loc[1]:]
	}
	return block + src
}

// Streamer adds the kit to a document while it is being written, as soon as
// its <body> tag has passed, so the person sees the designed page from the
// first screen on. Every icon is included (which ones the page uses is not
// known yet); the stored document is assembled again when it is complete.
type Streamer struct {
	// Look is the project's look the draw is made in, when there is one:
	// the document does not carry it until it is finished.
	Look *Look

	seen     strings.Builder
	injected bool
}

// Feed takes the next piece of the document and returns what to show.
func (s *Streamer) Feed(chunk string) string {
	if s.injected {
		return chunk
	}
	before := s.seen.Len()
	s.seen.WriteString(chunk)
	text := s.seen.String()
	loc := bodyOpenRe.FindStringIndex(text)
	if loc == nil {
		return chunk
	}
	s.injected = true
	c := ChoiceOf(text[:loc[0]])
	if s.Look != nil && strings.Contains(strings.ToLower(htmlTagRe.FindString(text)), `"`+LookPalette+`"`) {
		if l, ok := s.Look.Clean(); ok {
			c.Palette, c.Look = LookPalette, &l
		}
	}
	if !c.Uses {
		return chunk
	}
	at := loc[1] - before
	if at < 0 {
		at = 0
	}
	return chunk[:at] + Block(c, nil) + chunk[at:]
}

// Catalog is the kit's reference for the model: the kits, the palettes by
// mood, and the icon names.
func Catalog() string {
	var b strings.Builder
	b.WriteString("Kits (data-kit), each with what it suits and how to compose a page in it:\n")
	for _, k := range kitNames {
		fmt.Fprintf(&b, "- %s: %s %s\n", k, kits[k].About, kits[k].Brief)
	}
	b.WriteString("\nPalettes (data-palette), with their mood and colours; \"-soft\" ones have a tinted background, \"-dark\" ones are dark. The primary colour carries the buttons and highlights, so pick a palette whose primary suits the feel (a vivid one for an energetic product, a grey one only for a quiet look):\n")
	for _, p := range palettes {
		if strings.HasSuffix(p.ID, "-soft") || strings.HasSuffix(p.ID, "-dark") {
			continue
		}
		fmt.Fprintf(&b, "- %s (also -soft, -dark): %s; primary %s, accent %s\n", p.ID, p.Mood, p.Colors["primary"], p.Colors["accent"])
	}
	names := make([]string, 0, len(icons))
	for n := range icons {
		names = append(names, n)
	}
	sort.Strings(names)
	b.WriteString("\nIcons (<svg class=\"i\"><use href=\"#i-NAME\"/></svg>): ")
	b.WriteString(strings.Join(names, ", "))
	b.WriteString("\n")
	return b.String()
}

// HasPalette reports whether id names a palette.
func HasPalette(id string) bool { _, ok := paletteBy[id]; return ok }
