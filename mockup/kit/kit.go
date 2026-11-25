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
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
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

//go:embed kits/compositions.json
var compositionsFile []byte

//go:embed fonts/fonts.json fonts/*.woff2 fonts/LICENSE
var fontsFS embed.FS

//go:embed runtime.js
var runtimeJS string

// Palette is one colour scheme.
type Palette struct {
	ID     string            `json:"id"`
	Mood   string            `json:"mood"`
	Colors map[string]string `json:"colors"`
}

// Kit is one look of the component stylesheet: what it suits, how its pages
// are built (the brief), the first-screen compositions it suits, the heading
// fonts that suit it, its variables and its own rules.
type Kit struct {
	About        string            `json:"about"`
	Brief        string            `json:"brief"`
	Compositions []string          `json:"compositions"`
	Fonts        []string          `json:"fonts"`
	Vars         map[string]string `json:"vars"`
	CSS          string            `json:"css"`
}

// Composition is a way to build a page's first screen.
type Composition struct {
	ID    string `json:"id"`
	About string `json:"about"`
}

// Font is a heading face a page may name (data-font), embedded when used.
type Font struct {
	ID       string `json:"id"`
	Family   string `json:"family"`
	About    string `json:"about"`
	Fallback string `json:"fallback"`
	File     string `json:"file"`
}

var (
	icons      map[string]string
	iconSource string
	palettes   []Palette
	paletteBy  map[string]Palette
	kits       map[string]Kit
	kitNames   []string
	comps      []Composition
	compBy     map[string]Composition
	fonts      []Font
	fontBy     map[string]Font
	fontFiles  map[string][]byte
)

func init() {
	var ic struct {
		Source string            `json:"source"`
		Icons  map[string]string `json:"icons"`
	}
	mustJSON(iconsFile, &ic)
	icons, iconSource = ic.Icons, ic.Source
	mustJSON(palettesFile, &palettes)
	mustJSON(kitsFile, &kits)
	mustJSON(compositionsFile, &comps)
	raw, err := fontsFS.ReadFile("fonts/fonts.json")
	if err != nil {
		panic(fmt.Sprintf("mockupkit: %v", err))
	}
	mustJSON(raw, &fonts)
	fontFiles = map[string][]byte{}
	for _, f := range fonts {
		b, err := fontsFS.ReadFile("fonts/" + f.File)
		if err != nil {
			panic(fmt.Sprintf("mockupkit: font %s: %v", f.ID, err))
		}
		fontFiles[f.ID] = b
	}
	reindex()
}

// reindex rebuilds the lookups after the kits, compositions or fonts change.
func reindex() {
	kitNames = kitNames[:0]
	for k := range kits {
		kitNames = append(kitNames, k)
	}
	sort.Strings(kitNames)
	compBy = make(map[string]Composition, len(comps))
	for _, c := range comps {
		compBy[c.ID] = c
	}
	fontBy = make(map[string]Font, len(fonts))
	for _, f := range fonts {
		fontBy[f.ID] = f
	}
	paletteBy = make(map[string]Palette, len(palettes))
	for _, p := range palettes {
		paletteBy[p.ID] = p
	}
}

// Extension adds kits, compositions, fonts and palettes to the built-in ones,
// or replaces those with the same name, so a host can tune its mockups'
// variety without a new release of this package.
type Extension struct {
	Kits         map[string]Kit
	Compositions []Composition
	Palettes     []Palette
	// Fonts are the faces to add; FontFiles holds each one's woff2 by ID.
	Fonts     []Font
	FontFiles map[string][]byte
}

// Extend applies an extension. Call it once at start-up, before any mockup
// is drawn or assembled; it is not safe alongside them.
func Extend(e Extension) error {
	for _, f := range e.Fonts {
		if len(e.FontFiles[f.ID]) == 0 && len(fontFiles[f.ID]) == 0 {
			return fmt.Errorf("mockupkit: font %q has no file", f.ID)
		}
	}
	known := map[string]bool{}
	for _, c := range comps {
		known[c.ID] = true
	}
	for _, c := range e.Compositions {
		known[c.ID] = true
	}
	for name, k := range e.Kits {
		for _, c := range k.Compositions {
			if !known[c] {
				return fmt.Errorf("mockupkit: kit %q names unknown composition %q", name, c)
			}
		}
	}
	for name, k := range e.Kits {
		kits[name] = k
	}
	comps = mergeBy(comps, e.Compositions, func(c Composition) string { return c.ID })
	palettes = mergeBy(palettes, e.Palettes, func(p Palette) string { return p.ID })
	fonts = mergeBy(fonts, e.Fonts, func(f Font) string { return f.ID })
	for id, b := range e.FontFiles {
		fontFiles[id] = b
	}
	reindex()
	return nil
}

func mergeBy[T any](have, add []T, id func(T) string) []T {
	at := map[string]int{}
	for i, x := range have {
		at[id(x)] = i
	}
	for _, x := range add {
		if i, ok := at[id(x)]; ok {
			have[i] = x
		} else {
			at[id(x)] = len(have)
			have = append(have, x)
		}
	}
	return have
}

// FontLicense is the licence and notices of the built-in heading fonts (SIL
// Open Font License 1.1), to ship alongside a face a build uses.
func FontLicense() string {
	b, _ := fontsFS.ReadFile("fonts/LICENSE")
	return string(b)
}

// CompositionIDs lists the first-screen compositions.
func CompositionIDs() []string {
	ids := make([]string, len(comps))
	for i, c := range comps {
		ids[i] = c.ID
	}
	return ids
}

// FontFile is a heading face's woff2 and description, for a build that
// ships the face its mockup used.
func FontFile(id string) (Font, []byte, bool) {
	f, ok := fontBy[id]
	if !ok {
		return Font{}, nil, false
	}
	return f, fontFiles[id], true
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
	htmlTagRe    = regexp.MustCompile(`(?is)<html\b[^>]*>`)
	choiceAttrRe = regexp.MustCompile(`(?i)\sdata-(?:kit|palette|composition|font)\s*=\s*(?:"[^"]*"|'[^']*')`)
	attrRe       = regexp.MustCompile(`(?i)\bdata-(kit|palette|composition|font)\s*=\s*["']([^"']*)["']`)
	bodyOpenRe   = regexp.MustCompile(`(?is)<body\b[^>]*>`)
	iconRefRe    = regexp.MustCompile(`#i-([a-z0-9-]+)`)
	blockRe      = regexp.MustCompile(`(?s)` + regexp.QuoteMeta(blockStart) + `.*?` + regexp.QuoteMeta(blockEnd))
)

// Choice is the kit, palette, first-screen composition and heading font a
// document asks for.
type Choice struct {
	Kit, Palette string
	// Composition and Font are empty when the document names none (or an
	// unknown one).
	Composition, Font string
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
			case "composition":
				if _, ok := compBy[v]; ok {
					c.Composition = v
				}
			case "font":
				if _, ok := fontBy[v]; ok {
					c.Font = v
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
	// A product's own look keeps its own type: no bundled face over it.
	font, hasFont := fontBy[c.Font]
	hasFont = hasFont && c.Look == nil
	if hasFont {
		vars["--font-head"] = fmt.Sprintf("%q,%s", font.Family, font.Fallback)
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
	if hasFont {
		fmt.Fprintf(&b, "@font-face{font-family:%q;src:url(data:font/woff2;base64,%s) format(\"woff2\");font-display:swap}\n",
			font.Family, base64.StdEncoding.EncodeToString(fontFiles[font.ID]))
	}
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

// WithChoice stamps c's kit, palette, composition and font on the document's
// <html> tag: attributes the choice leaves empty are removed, so the document
// asks for exactly the choice given. The tag's other attributes are left alone.
func WithChoice(doc string, c Choice) string {
	loc := htmlTagRe.FindStringIndex(doc)
	if loc == nil {
		return doc
	}
	tag := doc[loc[0]:loc[1]]
	closing := tag[len(tag)-1:]
	if strings.HasSuffix(tag, "/>") {
		closing = "/>"
		tag = strings.TrimSuffix(tag, "/>")
	} else {
		tag = strings.TrimSuffix(tag, ">")
		closing = ">"
	}
	tag = choiceAttrRe.ReplaceAllString(tag, "")
	var b strings.Builder
	b.WriteString(tag)
	add := func(name, value string) {
		if value == "" {
			return
		}
		b.WriteString(` `)
		b.WriteString("data-" + name + `="`)
		b.WriteString(html.EscapeString(value))
		b.WriteString(`"`)
	}
	add("kit", c.Kit)
	add("palette", c.Palette)
	add("composition", c.Composition)
	add("font", c.Font)
	b.WriteString(closing)
	return doc[:loc[0]] + b.String() + doc[loc[1]:]
}

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
	return catalog(BasePalettes())
}

// CatalogFor is the kit's reference with only the given palettes listed: the
// shortlist a draw offers the model, so it names a palette from that list
// and not from the full catalogue.
func CatalogFor(shortlist []Palette) string {
	return catalog(shortlist)
}

// catalog lists the kits, compositions and fonts in full, and the palettes
// given (or all base palettes when nil).
func catalog(shortlist []Palette) string {
	palettes := shortlist
	if len(palettes) == 0 {
		palettes = BasePalettes()
	}
	var b strings.Builder
	b.WriteString("Kits (data-kit), each with what it suits, the first-screen compositions it suits, the heading fonts that suit it, and how its pages are built:\n")
	for _, k := range kitNames {
		kt := kits[k]
		fmt.Fprintf(&b, "- %s: %s Compositions: %s. Fonts: %s. %s\n", k, kt.About, strings.Join(kt.Compositions, ", "), strings.Join(kt.Fonts, ", "), kt.Brief)
	}
	b.WriteString("\nFirst-screen compositions (data-composition):\n")
	for _, c := range comps {
		fmt.Fprintf(&b, "- %s: %s\n", c.ID, c.About)
	}
	b.WriteString("\nHeading fonts (data-font), embedded for you:\n")
	for _, f := range fonts {
		fmt.Fprintf(&b, "- %s: %s\n", f.ID, f.About)
	}
	b.WriteString("\nModifiers on <html>, each optional: data-density=\"airy|compact\", data-corners=\"sharp|round\", data-contrast=\"high\", data-scale=\"large|small\" (the headline size).\n")
	b.WriteString("\nPalettes (data-palette), with their mood and colours; \"-soft\" ones have a tinted background, \"-dark\" ones are dark. The primary colour carries the buttons and highlights, so pick a palette whose primary suits the feel (a vivid one for an energetic product, a grey one only for a quiet look):\n")
	for _, p := range palettes {
		fmt.Fprintf(&b, "- %s (also -soft, -dark): %s; family %s; primary %s, accent %s\n", p.ID, p.Mood, p.Family(), p.Colors["primary"], p.Colors["accent"])
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
