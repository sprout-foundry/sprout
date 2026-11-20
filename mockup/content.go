package mockup

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	nethtml "golang.org/x/net/html"
)

// Field is one piece of text on the first look: an element's own text, or a
// form field's placeholder.
type Field struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Section string `json:"section,omitempty"`
	Text    string `json:"text"`
	// Mine marks text the person set: a later draw keeps it while its key is
	// on the page.
	Mine bool `json:"mine,omitempty"`

	pos int
}

// Content is the first look's text, in page order.
type Content struct {
	Version int     `json:"version"`
	Fields  []Field `json:"fields"`
}

// ContentVersion is the content file's format.
const ContentVersion = 1

const (
	keyAttr         = "data-c"
	placeholderAttr = "data-c-ph"
	labelAttr       = "data-label"
)

// skippedElements hold no text a person reads on the page.
var skippedElements = map[string]bool{
	"script": true, "style": true, "svg": true, "template": true, "noscript": true, "head": true,
}

var keyNumRe = regexp.MustCompile(`^c(\d+)$`)

// inlineElements are phrasing elements: inside a sentence they are part of
// that sentence's text, not texts of their own.
var inlineElements = map[string]bool{
	"strong": true, "em": true, "b": true, "i": true, "u": true, "s": true, "span": true, "small": true,
	"mark": true, "code": true, "sup": true, "sub": true, "abbr": true, "time": true, "a": true, "q": true,
}

// frame is an open element while the page is walked.
type frame struct {
	tag      string
	tagPiece int
	endPiece int
	token    nethtml.Token
	key      string
	label    string
	screen   string
	skip     bool
	// texts are the pieces holding the element's own text.
	texts []int
	// direct is the element's own text; full includes its inline children's.
	direct strings.Builder
	full   strings.Builder
	kids   []*frame
}

// Keyed gives every element with text of its own a key and returns the page
// with its keys and the page's content. Keys already on the page are kept;
// saved's fields marked as the person's replace the page's text for their
// keys. The page is otherwise left as written.
func Keyed(doc string, saved *Content) (string, Content) {
	mine := map[string]Field{}
	if saved != nil {
		for _, f := range saved.Fields {
			if f.Mine {
				mine[f.Key] = f
			}
		}
	}
	used := map[string]bool{}
	next := 1
	for _, m := range regexp.MustCompile(`data-c(?:-ph)?="(c\d+)"`).FindAllStringSubmatch(doc, -1) {
		if n, err := strconv.Atoi(m[1][1:]); err == nil && n >= next {
			next = n + 1
		}
	}
	newKey := func(existing string) string {
		if existing != "" && keyNumRe.MatchString(existing) && !used[existing] {
			used[existing] = true
			return existing
		}
		k := "c" + strconv.Itoa(next)
		next++
		used[k] = true
		return k
	}

	var pieces []string
	var stack []*frame
	var fields []Field
	heading := ""
	skipping := func() bool {
		for _, f := range stack {
			if f.skip {
				return true
			}
		}
		return false
	}
	section := func() string {
		parts := []string{}
		for i := len(stack) - 1; i >= 0; i-- {
			if stack[i].screen != "" {
				parts = append(parts, humanize(stack[i].screen))
				break
			}
		}
		if heading != "" {
			parts = append(parts, heading)
		}
		return strings.Join(parts, " · ")
	}
	var emit func(f *frame)
	emit = func(f *frame) {
		text := strings.Join(strings.Fields(f.full.String()), " ")
		key := newKey(f.key)
		if m, ok := mine[key]; ok {
			text = m.Text
			replaceText(pieces, f, html.EscapeString(text))
		}
		setAttr(&f.token, keyAttr, key)
		pieces[f.tagPiece] = renderTag(f.token, false)
		label := f.label
		if label == "" {
			label = elementLabel(f.tag, attr(f.token, "class"))
		}
		fields = append(fields, Field{Key: key, Label: label, Section: section(), Text: text, Mine: mine[key].Mine, pos: f.tagPiece})
		if f.tag == "h1" || f.tag == "h2" || f.tag == "h3" {
			heading = text
		}
	}

	// closeFrame settles an element as it closes, while it is still on the
	// stack (at index at), so its texts take its screen as their section.
	closeFrame := func(f *frame, at int) {
		if skipping() {
			return
		}
		var parent *frame
		if at > 0 {
			parent = stack[at-1]
		}
		if parent != nil && inlineElements[f.tag] && hasText(f.full.String()) {
			parent.kids = append(parent.kids, f)
			parent.full.WriteString(f.full.String())
			return
		}
		if hasText(f.direct.String()) {
			emit(f)
			return
		}
		for _, k := range f.kids {
			emit(k)
		}
	}

	z := nethtml.NewTokenizer(strings.NewReader(doc))
	for {
		tt := z.Next()
		if tt == nethtml.ErrorToken {
			break
		}
		raw := string(z.Raw())
		tok := z.Token()
		switch tt {
		case nethtml.StartTagToken, nethtml.SelfClosingTagToken:
			tag := tok.Data
			if tt == nethtml.SelfClosingTagToken || voidElements[tag] {
				if (tag == "input" || tag == "textarea") && attr(tok, "placeholder") != "" && !skipping() {
					key := newKey(attr(tok, placeholderAttr))
					text := attr(tok, "placeholder")
					if f, ok := mine[key]; ok {
						text = f.Text
					}
					setAttr(&tok, "placeholder", text)
					setAttr(&tok, placeholderAttr, key)
					fields = append(fields, Field{Key: key, Label: placeholderLabel(tok), Section: section(), Text: text, Mine: mine[key].Mine, pos: len(pieces)})
					pieces = append(pieces, renderTag(tok, tt == nethtml.SelfClosingTagToken))
					continue
				}
				pieces = append(pieces, raw)
				continue
			}
			if attr(tok, "data-screen") != "" {
				heading = ""
			}
			f := &frame{tag: tag, tagPiece: len(pieces), token: tok, key: attr(tok, keyAttr), label: attr(tok, labelAttr),
				screen: attr(tok, "data-screen"), skip: skippedElements[tag] || tag == "textarea"}
			stack = append(stack, f)
			pieces = append(pieces, raw)
		case nethtml.EndTagToken:
			pieces = append(pieces, raw)
			i := len(stack) - 1
			for i >= 0 && stack[i].tag != tok.Data {
				i--
			}
			if i < 0 {
				continue
			}
			for j := len(stack) - 1; j >= i; j-- {
				f := stack[j]
				f.endPiece = len(pieces) - 1
				closeFrame(f, j)
				stack = stack[:j]
			}
		case nethtml.TextToken:
			pieces = append(pieces, raw)
			if len(stack) > 0 && !skipping() {
				top := stack[len(stack)-1]
				if strings.TrimSpace(tok.Data) != "" {
					top.texts = append(top.texts, len(pieces)-1)
				}
				top.direct.WriteString(tok.Data)
				top.full.WriteString(tok.Data)
			}
		default:
			pieces = append(pieces, raw)
		}
	}
	return strings.Join(pieces, ""), Content{Version: ContentVersion, Fields: orderByPage(fields)}
}

// replaceText puts text in place of an element's words: its own text and its
// inline children's. Children without words (an icon) stay where they are.
func replaceText(pieces []string, f *frame, text string) {
	spots := append([]int(nil), f.texts...)
	for _, k := range f.kids {
		spots = append(spots, k.tagPiece)
	}
	sort.Ints(spots)
	for _, k := range f.kids {
		for i := k.tagPiece; i <= k.endPiece; i++ {
			pieces[i] = ""
		}
	}
	for _, i := range f.texts {
		pieces[i] = ""
	}
	if len(spots) > 0 {
		pieces[spots[0]] = text
	}
}

// orderByPage puts fields in the order their keys first appear: an element
// is recorded when it closes, so an inner element would otherwise come
// before the one holding it.
func orderByPage(fields []Field) []Field {
	sort.SliceStable(fields, func(i, j int) bool { return fields[i].pos < fields[j].pos })
	return fields
}

// ApplyContent sets fields' text on a page as the person's, and returns the
// page and its content. Keys not on the page are ignored.
func ApplyContent(doc string, current Content, changes map[string]string) (string, Content) {
	saved := Content{Version: ContentVersion}
	for _, f := range current.Fields {
		if text, ok := changes[f.Key]; ok {
			f.Text, f.Mine = text, true
		}
		saved.Fields = append(saved.Fields, f)
	}
	return Keyed(doc, &saved)
}

var contentScriptRe = regexp.MustCompile(`(?s)<script type="application/json" id="sprout-content">.*?</script>`)

// WithContent embeds the content file in the page, just before </body>, so
// a page carries its own text and keys.
func WithContent(doc string, c Content) string {
	doc = StripContent(doc)
	b, err := json.Marshal(c)
	if err != nil {
		return doc
	}
	script := `<script type="application/json" id="sprout-content">` + string(b) + `</script>`
	if i := strings.LastIndex(strings.ToLower(doc), "</body>"); i >= 0 {
		return doc[:i] + script + doc[i:]
	}
	return doc + script
}

// StripContent removes the embedded content file.
func StripContent(doc string) string {
	return contentScriptRe.ReplaceAllString(doc, "")
}

// ContentOf reads the content file a page carries.
func ContentOf(doc string) (Content, bool) {
	m := regexp.MustCompile(`(?s)<script type="application/json" id="sprout-content">(.*?)</script>`).FindStringSubmatch(doc)
	if m == nil {
		return Content{}, false
	}
	var c Content
	if err := json.Unmarshal([]byte(m[1]), &c); err != nil {
		return Content{}, false
	}
	return c, true
}

var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
	"input": true, "link": true, "meta": true, "source": true, "track": true, "wbr": true,
}

func attr(t nethtml.Token, name string) string {
	for _, a := range t.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

func setAttr(t *nethtml.Token, name, val string) {
	for i, a := range t.Attr {
		if a.Key == name {
			t.Attr[i].Val = val
			return
		}
	}
	t.Attr = append(t.Attr, nethtml.Attribute{Key: name, Val: val})
}

// renderTag writes a start tag with its attributes, values escaped.
func renderTag(t nethtml.Token, selfClosing bool) string {
	var b strings.Builder
	b.WriteString("<" + t.Data)
	for _, a := range t.Attr {
		if a.Namespace != "" {
			fmt.Fprintf(&b, " %s:%s", a.Namespace, a.Key)
		} else {
			b.WriteString(" " + a.Key)
		}
		b.WriteString(`="` + html.EscapeString(a.Val) + `"`)
	}
	if selfClosing {
		b.WriteString(" /")
	}
	b.WriteString(">")
	return b.String()
}

func elementLabel(tag, class string) string {
	switch tag {
	case "title":
		return "Page title"
	case "h1":
		return "Main heading"
	case "h2", "h3", "h4", "h5", "h6":
		return "Heading"
	case "button":
		return "Button"
	case "a":
		if strings.Contains(" "+class+" ", " btn") {
			return "Button"
		}
		return "Link"
	case "label":
		return "Field label"
	case "li":
		return "List item"
	case "th", "td":
		return "Table cell"
	case "option":
		return "Choice"
	case "blockquote", "q":
		return "Quote"
	}
	return "Text"
}

func placeholderLabel(t nethtml.Token) string {
	if l := attr(t, labelAttr); l != "" {
		return l
	}
	return "Placeholder"
}

// humanize turns a screen id ("host-view") into words ("Host view").
func humanize(id string) string {
	s := strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(id))
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// hasText reports whether s has a letter or digit: punctuation and
// separators alone are not text a person would change.
func hasText(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}
