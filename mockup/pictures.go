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
	// Image is the id of the person's picture in the slot; "" while the
	// stand-in shows.
	Image string `json:"image,omitempty"`
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
				pic.Image = p.Image
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
	saved := current
	saved.Pictures = append([]Picture(nil), current.Pictures...)
	for i, p := range saved.Pictures {
		if id, ok := set[p.Key]; ok {
			saved.Pictures[i].Image = id
		}
	}
	return Keyed(doc, &saved)
}
