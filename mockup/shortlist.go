// The palette shortlist for a draw: the small set of base palettes the
// model is offered instead of the kit's full catalogue, sampled across
// families so two ideas do not come out alike by palette family.
package mockup

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/sprout-foundry/sprout/mockup/kit"
)

// ShortlistSize is how many base palettes a draw offers the model.
const ShortlistSize = 6

// namedColours maps a person's word to the palette family it names. A
// named colour always wins over avoidance and mood matching.
var namedColours = map[string]string{
	"red": "red", "crimson": "red", "scarlet": "red", "coral": "red",
	"orange": "orange", "amber": "orange", "tangerine": "orange",
	"yellow": "yellow/gold", "gold": "yellow/gold", "golden": "yellow/gold", "mustard": "yellow/gold",
	"green": "green", "lime": "green", "olive": "green",
	"teal": "teal", "turquoise": "teal", "aqua": "teal", "seafoam": "teal",
	"blue": "blue", "navy": "blue", "cobalt": "blue", "sky": "blue", "azure": "blue",
	"indigo": "indigo/violet", "violet": "indigo/violet", "purple": "indigo/violet", "plum": "indigo/violet", "lavender": "indigo/violet",
	"pink": "pink", "magenta": "pink", "rose": "pink", "blush": "pink", "fuchsia": "pink",
	"brown": "brown/earth", "earth": "brown/earth", "beige": "brown/earth", "tan": "brown/earth", "sand": "brown/earth", "cream": "brown/earth",
	"grey": "neutral", "gray": "neutral", "black": "neutral", "white": "neutral", "neutral": "neutral", "slate": "neutral", "stone": "neutral",
}

// stopWords are not mood words.
var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "of": true, "to": true, "for": true,
	"with": true, "on": true, "in": true, "at": true, "by": true, "is": true, "it": true, "this": true,
	"that": true, "my": true, "your": true, "our": true, "their": true, "you": true, "i": true,
	"we": true, "they": true, "them": true, "about": true, "are": true, "as": true, "be": true,
}

var wordRe = regexp.MustCompile(`[a-z][a-z'-]+`)

// briefWords collects the words of the plan and the change the person
// asked for: the brief's mood words, and the colours they named ("blue and
// orange").
func briefWords(plan []byte, change string) (tokens []string, named []string) {
	var texts []string
	if len(plan) > 0 {
		var v any
		if err := json.Unmarshal(plan, &v); err == nil {
			collectStrings(v, &texts)
		} else {
			texts = append(texts, string(plan))
		}
	}
	if strings.TrimSpace(change) != "" {
		texts = append(texts, change)
	}
	seenToken := map[string]bool{}
	seenNamed := map[string]bool{}
	for _, text := range texts {
		for _, m := range wordRe.FindAllString(strings.ToLower(text), -1) {
			m = strings.Trim(m, "'-")
			if len(m) < 3 || stopWords[m] {
				continue
			}
			if f, ok := namedColours[m]; ok && !seenNamed[f] {
				seenNamed[f] = true
				named = append(named, f)
			}
			if !seenToken[m] {
				seenToken[m] = true
				tokens = append(tokens, m)
			}
		}
	}
	return tokens, named
}

func collectStrings(v any, out *[]string) {
	switch x := v.(type) {
	case string:
		if strings.TrimSpace(x) != "" {
			*out = append(*out, x)
		}
	case map[string]any:
		for _, val := range x {
			collectStrings(val, out)
		}
	case []any:
		for _, val := range x {
			collectStrings(val, out)
		}
	}
}

// familyOfChoice returns the colour family of a choice's palette, clipping
// its -soft/-dark modifier; "" when it names no palette.
func familyOfChoice(c kit.Choice) string {
	if c.Palette == "" || c.Palette == kit.LookPalette {
		return ""
	}
	base := strings.TrimSuffix(strings.TrimSuffix(c.Palette, "-dark"), "-soft")
	if p, ok := kit.PaletteByID(base); ok {
		return p.Family()
	}
	return ""
}

// avoidedFamilies collects the colour families the person's recent first
// looks used — for a new idea — and, for a Fresh redraw, the current look's
// family too. An explicit named colour wins and makes palette avoidance not
// apply at all.
func avoidedFamilies(avoid []kit.Choice, fresh bool, current string, named []string) map[string]bool {
	if len(named) > 0 {
		return nil
	}
	fams := map[string]bool{}
	for _, c := range avoid {
		if f := familyOfChoice(c); f != "" {
			fams[f] = true
		}
	}
	if fresh && current != "" {
		if f := familyOfChoice(kit.ChoiceOf(current)); f != "" {
			fams[f] = true
		}
	}
	return fams
}
