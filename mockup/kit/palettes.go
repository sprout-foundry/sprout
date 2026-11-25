// The kit's colour families: every palette is grouped by the hue and
// chroma of its primary colour, so the draw can steer away from whole
// families rather than from a list of palette ids. The family is derived
// from the colour at load time, never hand-typed, so palettes added to the
// catalogue get one automatically.
package kit

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Families are the colour families the kit knows, in a stable order.
var Families = []string{
	"red", "orange", "yellow/gold", "green", "teal", "blue", "indigo/violet", "pink", "brown/earth", "neutral",
}

// FamilyOf returns the colour family of a colour: a hex string (#rgb,
// #rrggbb, #rrggbbaa) or an rgb()/hsl() string. An unparseable colour is
// neutral.
func FamilyOf(c string) string {
	r, g, b, ok := parseColour(c)
	if !ok {
		return "neutral"
	}
	// HSL.
	rf, gf, bf := r/255, g/255, b/255
	max := rf
	if gf > max {
		max = gf
	}
	if bf > max {
		max = bf
	}
	min := rf
	if gf < min {
		min = gf
	}
	if bf < min {
		min = bf
	}
	l := (max + min) / 2
	if max == min {
		// Greys: no hue to call a family.
		return "neutral"
	}
	d := max - min
	var h, s float64
	if l >= 0.5 {
		s = d / (2 - max - min)
	} else {
		s = d / (max + min)
	}
	switch {
	case max == rf:
		h = 60 * math.Mod((gf-bf)/d+6, 6)
		if gf < bf {
			h -= 360
		}
	case max == gf:
		h = 60 * ((bf - rf) / d + 2)
	default:
		h = 60 * ((rf - gf) / d + 4)
	}
	h = math.Mod(h+360, 360)
	if s < 0.12 {
		return "neutral"
	}
	if s < 0.33 && h >= 15 && h <= 65 {
		// Low-chroma warm colours read as earth, not as orange or gold.
		return "brown/earth"
	}
	switch {
	case h < 20 || h >= 345:
		return "red"
	case h < 45:
		return "orange"
	case h < 75:
		return "yellow/gold"
	case h < 160:
		return "green"
	case h < 195:
		return "teal"
	case h < 250:
		return "blue"
	case h < 295:
		return "indigo/violet"
	default:
		return "pink"
	}
}

var (
	hexRe = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
	rgbRe = regexp.MustCompile(`^rgba?\(\s*([0-9.]+)\s*,\s*([0-9.]+)\s*,\s*([0-9.]+)`)
	hslRe = regexp.MustCompile(`^hsla?\(\s*([0-9.]+)\s*,\s*([0-9.]+)%\s*,\s*([0-9.]+)%`)
)

func parseColour(c string) (float64, float64, float64, bool) {
	c = strings.TrimSpace(c)
	if m := hexRe.FindStringSubmatch(c); m != nil {
		h := m[1]
		switch len(h) {
		case 3, 4: // expand each nibble
			b := make([]byte, 0, len(h)*2)
			for i := 0; i < len(h); i++ {
				b = append(b, h[i], h[i])
			}
			h = string(b)
		}
		if len(h) == 8 {
			h = h[:6] // the alpha does not affect the family
		}
		if len(h) != 6 {
			return 0, 0, 0, false
		}
		v, err := strconv.ParseInt(h, 16, 32)
		if err != nil {
			return 0, 0, 0, false
		}
		return float64((v >> 16) & 0xff), float64((v >> 8) & 0xff), float64(v & 0xff), true
	}
	if m := rgbRe.FindStringSubmatch(c); m != nil {
		parts := make([]float64, 3)
		for i := 1; i <= 3; i++ {
			f, err := strconv.ParseFloat(strings.TrimSpace(m[i]), 64)
			if err != nil {
				return 0, 0, 0, false
			}
			parts[i-1] = f
		}
		return parts[0], parts[1], parts[2], true
	}
	if m := hslRe.FindStringSubmatch(c); m != nil {
		h, err1 := strconv.ParseFloat(m[1], 64)
		s, err2 := strconv.ParseFloat(m[2], 64)
		l, err3 := strconv.ParseFloat(m[3], 64)
		if err1 != nil || err2 != nil || err3 != nil {
			return 0, 0, 0, false
		}
		r, g, b := hslToRgb(h, s/100, l/100)
		return r, g, b, true
	}
	// A bare hex without '#' also parses.
	if m := hexRe.FindStringSubmatch("#" + c); m != nil {
		return parseColour("#" + c)
	}
	return 0, 0, 0, false
}

func hslToRgb(h, s, l float64) (float64, float64, float64) {
	h = math.Mod(h+360, 360)
	if s == 0 {
		v := l * 255
		return v, v, v
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	return hueToRgb(p, q, h/360+1/3) * 255,
		hueToRgb(p, q, h/360) * 255,
		hueToRgb(p, q, h/360-1/3) * 255
}

func hueToRgb(p, q, t float64) float64 {
	if t < 0 {
		t += 1
	}
	if t > 1 {
		t -= 1
	}
	if t < 1/6 {
		return p + (q-p)*6*t
	}
	if t < 1/2 {
		return q
	}
	if t < 2/3 {
		return p + (q-p)*(2/3-t)*6
	}
	return p
}

// Family is the colour family of the palette's primary colour.
func (p Palette) Family() string {
	if p.Colors == nil {
		return "neutral"
	}
	return FamilyOf(p.Colors["primary"])
}

// BasePalettes are the palettes without a -soft or -dark suffix. The
// modifiers are the model's choice on top of one of these; the shortlist
// offers the base ones.
func BasePalettes() []Palette {
	var out []Palette
	for _, p := range palettes {
		if strings.HasSuffix(p.ID, "-soft") || strings.HasSuffix(p.ID, "-dark") {
			continue
		}
		out = append(out, p)
	}
	return out
}

// Palettes lists every palette.
func Palettes() []Palette {
	out := make([]Palette, len(palettes))
	copy(out, palettes)
	return out
}

// PaletteByID returns the named palette.
func PaletteByID(id string) (Palette, bool) {
	p, ok := paletteBy[id]
	return p, ok
}

// ShortlistLine describes one palette for the model.
func (p Palette) ShortlistLine() string {
	return fmt.Sprintf("- %s (%s): family %s, primary %s", p.ID, p.Mood, p.Family(), p.Colors["primary"])
}
