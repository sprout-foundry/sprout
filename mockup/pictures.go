package mockup

// Picture slots (SP-BUILDER-46 in the platform): the pictures a page wants,
// each marked data-img with a data-label saying what it should show. The
// drawing never holds a picture: the person's picture in a slot is named by
// its id (data-img-id), and whoever shows the page puts the picture in.

import (
	"regexp"
	"strings"

	nethtml "golang.org/x/net/html"
)

// Picture is one picture slot and the person's picture in it.
type Picture struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Section string `json:"section,omitempty"`
	// Image is the id of the picture in the slot; "" while the stand-in
	// shows.
	Image string `json:"image,omitempty"`
	// Picked says Sprout chose Image, until the person sets their own.
	Picked bool `json:"picked,omitempty"`
}

const (
	pictureAttr   = "data-img"
	pictureIDAttr = "data-img-id"
	// LogoSlot is the nav logo's slot, on every page that has one.
	LogoSlot = "logo"
)

var pictureKeyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// pictureSlot reads tok as a picture slot when it is one: marked data-img,
// or the nav's logo. It sets the person's picture from saved on the tag, and
// answers the slot.
func pictureSlot(tok *nethtml.Token, section string, saved *Content) (Picture, bool) {
	key := strings.ToLower(strings.TrimSpace(attr(*tok, pictureAttr)))
	if key == "" && strings.Contains(" "+attr(*tok, "class")+" ", " logo ") {
		key = LogoSlot
		setAttr(tok, pictureAttr, key)
	}
	if key == "" || !pictureKeyRe.MatchString(key) {
		return Picture{}, false
	}
	label := strings.TrimSpace(attr(*tok, labelAttr))
	if label == "" {
		label = humanize(key)
		if key == LogoSlot {
			label = "Logo"
		}
	}
	pic := Picture{Key: key, Label: label, Section: section}
	if saved != nil {
		for _, p := range saved.Pictures {
			if p.Key == key {
				pic.Image, pic.Picked = p.Image, p.Picked && p.Image != ""
			}
		}
	}
	if pic.Image != "" {
		setAttr(tok, pictureIDAttr, pic.Image)
	} else {
		removeAttr(tok, pictureIDAttr)
	}
	return pic, true
}

func removeAttr(t *nethtml.Token, name string) {
	out := t.Attr[:0]
	for _, a := range t.Attr {
		if a.Key != name {
			out = append(out, a)
		}
	}
	t.Attr = out
}

// ApplyPictures puts the person's pictures in slots (slot key to picture
// id; "" clears a slot) and returns the page and its content.
func ApplyPictures(doc string, current Content, set map[string]string) (string, Content) {
	return applyPictures(doc, current, set, false)
}

// ApplyPicks puts pictures Sprout chose in slots, marked as its picks until
// the person sets their own.
func ApplyPicks(doc string, current Content, set map[string]string) (string, Content) {
	return applyPictures(doc, current, set, true)
}

func applyPictures(doc string, current Content, set map[string]string, picked bool) (string, Content) {
	saved := current
	saved.Pictures = append([]Picture(nil), current.Pictures...)
	for i, p := range saved.Pictures {
		if id, ok := set[p.Key]; ok {
			saved.Pictures[i].Image, saved.Pictures[i].Picked = id, picked && id != ""
		}
	}
	return Keyed(doc, &saved)
}

var pictureSlotRe = regexp.MustCompile(`<[a-zA-Z][^>]*\sdata-img\s*=\s*["']?([a-z0-9][a-z0-9-]*)["']?[^>]*>`)
var pictureLabelRe = regexp.MustCompile(`\sdata-label\s*=\s*"([^"]*)"`)

// pictureWords are the words of a picture's label that say what it shows.
func pictureWords(label string) map[string]bool {
	words := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(label), func(r rune) bool { return !(r >= 'a' && r <= 'z') }) {
		switch w {
		case "photo", "picture", "image", "the", "and", "our", "your", "with", "for", "this", "that", "from":
			continue
		}
		if len(w) > 2 {
			words[strings.TrimSuffix(w, "s")] = true
		}
	}
	return words
}

// keepPictureKeys gives a picture spot a redraw renamed its old key back:
// each saved spot holding the person's picture whose key the page no longer
// has takes the new spot whose label and key share the most words with its
// own, or
// the only new spot when there is just one. Spots the page kept are left be.
func keepPictureKeys(doc string, saved *Content) string {
	if saved == nil {
		return doc
	}
	type slot struct{ key, label string }
	var fresh []slot
	onPage := map[string]bool{}
	for _, m := range pictureSlotRe.FindAllStringSubmatch(doc, -1) {
		label := ""
		if l := pictureLabelRe.FindStringSubmatch(m[0]); l != nil {
			label = l[1]
		}
		onPage[m[1]] = true
		fresh = append(fresh, slot{m[1], label})
	}
	wasSaved := map[string]bool{}
	for _, p := range saved.Pictures {
		wasSaved[p.Key] = true
	}
	var candidates []slot
	for _, s := range fresh {
		if !wasSaved[s.key] {
			candidates = append(candidates, s)
		}
	}
	taken := map[string]bool{}
	renames := map[string]string{}
	for _, p := range saved.Pictures {
		if p.Image == "" || onPage[p.Key] || p.Key == LogoSlot {
			continue
		}
		want := pictureWords(p.Label + " " + p.Key)
		best, bestScore := "", 0
		for _, c := range candidates {
			if taken[c.key] {
				continue
			}
			score := 0
			for w := range pictureWords(c.label + " " + c.key) {
				if want[w] {
					score++
				}
			}
			if score > bestScore {
				best, bestScore = c.key, score
			}
		}
		if best == "" {
			var left []string
			for _, c := range candidates {
				if !taken[c.key] {
					left = append(left, c.key)
				}
			}
			if len(left) == 1 {
				best = left[0]
			}
		}
		if best != "" {
			taken[best] = true
			renames[best] = p.Key
		}
	}
	if len(renames) == 0 {
		return doc
	}
	return pictureSlotRe.ReplaceAllStringFunc(doc, func(tag string) string {
		m := pictureSlotRe.FindStringSubmatch(tag)
		old, ok := renames[m[1]]
		if !ok {
			return tag
		}
		return regexp.MustCompile(`(\sdata-img\s*=\s*["']?)`+regexp.QuoteMeta(m[1])).ReplaceAllString(tag, "${1}"+old)
	})
}
