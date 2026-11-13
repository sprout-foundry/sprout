package kit

// A project's own look: the colours, type and corners of an existing product,
// so a mockup of a change to it looks like it rather than like one of the
// kit's palettes. The host reads the look from the project; the document
// carries it in a <meta> the engine writes (never the model), so the page is
// assembled the same way on every reload and edit. Every value is checked
// before it reaches the page's styles: a look is read from someone's
// repository by a model, and nothing else in it is trusted.

import (
	"encoding/json"
	"html"
	"regexp"
	"strings"
)

// LookPalette is the palette a document names to use the project's look.
const LookPalette = "project"

// Look is a product's look, in the kit's terms.
type Look struct {
	// Colors are the kit's palette slots: bg, surface, soft, border, text,
	// muted, primary, on_primary, accent, on_accent. A missing one keeps the
	// default palette's.
	Colors map[string]string `json:"colors"`
	// FontBody and FontHead are font stacks ("Inter, system-ui, sans-serif").
	// They are named, not loaded: a font the viewer lacks falls back.
	FontBody string `json:"font_body,omitempty"`
	FontHead string `json:"font_head,omitempty"`
	// Radius is the corner radius of cards ("8px"); buttons and small parts
	// are drawn in proportion.
	Radius string `json:"radius,omitempty"`
	// Dark is a dark interface.
	Dark bool `json:"dark,omitempty"`
}

var (
	lookColorRe  = regexp.MustCompile(`^(#[0-9a-fA-F]{3,8}|(rgb|rgba|hsl|hsla)\([0-9.,%\s/]+\))$`)
	lookFontRe   = regexp.MustCompile(`^[A-Za-z0-9 ,'"\-_.]+$`)
	lookRadiusRe = regexp.MustCompile(`^(\d{1,3}(\.\d+)?)(px|rem|em)$`)
	lookMetaRe   = regexp.MustCompile(`(?is)<meta\s+name="mockupkit-look"\s+content="([^"]*)"\s*/?>\s*`)
	headOpenRe   = regexp.MustCompile(`(?is)<head\b[^>]*>`)
	lookSlots    = map[string]bool{"bg": true, "surface": true, "soft": true, "border": true, "text": true, "muted": true, "primary": true, "on_primary": true, "accent": true, "on_accent": true}
)

// Clean is the look with every value that is not plainly a colour, a font
// stack or a length left out. It reports whether anything is left.
func (l Look) Clean() (Look, bool) {
	out := Look{Colors: map[string]string{}, Dark: l.Dark}
	for k, v := range l.Colors {
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
		if lookSlots[k] && lookColorRe.MatchString(v) && len(v) <= 40 {
			out.Colors[k] = v
		}
	}
	for _, f := range []struct {
		in  string
		out *string
	}{{l.FontBody, &out.FontBody}, {l.FontHead, &out.FontHead}} {
		if v := strings.TrimSpace(f.in); v != "" && len(v) <= 160 && lookFontRe.MatchString(v) {
			*f.out = v
		}
	}
	if v := strings.TrimSpace(l.Radius); lookRadiusRe.MatchString(v) {
		out.Radius = v
	}
	return out, len(out.Colors) > 0 || out.FontBody != "" || out.FontHead != "" || out.Radius != ""
}

// WithLook stamps the look into a document's <head> (replacing one already
// there), so its assembly uses it.
func WithLook(doc string, l Look) string {
	clean, ok := l.Clean()
	doc = lookMetaRe.ReplaceAllString(doc, "")
	if !ok {
		return doc
	}
	raw, err := json.Marshal(clean)
	if err != nil {
		return doc
	}
	meta := `<meta name="mockupkit-look" content="` + html.EscapeString(string(raw)) + `">`
	if loc := headOpenRe.FindStringIndex(doc); loc != nil {
		return doc[:loc[1]] + meta + doc[loc[1]:]
	}
	if loc := htmlTagRe.FindStringIndex(doc); loc != nil {
		return doc[:loc[1]] + meta + doc[loc[1]:]
	}
	return meta + doc
}

// LookOf reads the look a document carries.
func LookOf(doc string) (Look, bool) {
	m := lookMetaRe.FindStringSubmatch(doc)
	if m == nil {
		return Look{}, false
	}
	var l Look
	if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), &l); err != nil {
		return Look{}, false
	}
	return l.Clean()
}

// lookVars are the look's CSS custom properties, over the default palette
// and the kit's own.
func lookVars(l Look) map[string]string {
	vars := map[string]string{}
	for k, v := range l.Colors {
		vars["--"+strings.ReplaceAll(k, "_", "-")] = v
	}
	if l.FontBody != "" {
		vars["--font-body"] = l.FontBody
	}
	if l.FontHead != "" {
		vars["--font-head"] = l.FontHead
	} else if l.FontBody != "" {
		vars["--font-head"] = l.FontBody
	}
	if m := lookRadiusRe.FindStringSubmatch(l.Radius); m != nil {
		vars["--radius"] = l.Radius
		vars["--radius-sm"] = scaledLength(m[1], m[3], 0.7)
		vars["--radius-btn"] = scaledLength(m[1], m[3], 0.75)
	}
	return vars
}

func scaledLength(num, unit string, f float64) string {
	var n float64
	if err := json.Unmarshal([]byte(num), &n); err != nil {
		return num + unit
	}
	v := n * f
	b, _ := json.Marshal(float64(int(v*100+0.5)) / 100)
	return string(b) + unit
}
