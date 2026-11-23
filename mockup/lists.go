package mockup

// Lists of repeated items (a menu, this week's bakes, a timetable, a row of
// cards): found while a page is keyed, so the person edits them as rows with
// a column per part, and adds, removes and reorders items.
//
// A list is an element marked data-list (its value names it), or one whose
// child elements number two or more, all of one tag and class, each holding
// text, outside navigation and forms. Its id is kept on the page in
// data-list-id, so it is the same list after a redraw that keeps it.

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	nethtml "golang.org/x/net/html"
)

// List is one list of repeated items on a page.
type List struct {
	ID string `json:"id"`
	// Name is what the items are ("This week's bakes"): the page's own
	// name for the list, else the heading before it.
	Name string `json:"name"`
	// Columns name each part of an item, in order.
	Columns []string `json:"columns"`
	Items   int      `json:"items"`
}

const (
	listAttr   = "data-list"
	listIDAttr = "data-list-id"
	// maxListColumns bounds the parts of an item read as a list's columns.
	maxListColumns = 8
)

var listIDRe = regexp.MustCompile(`data-list-id="(l\d+)"`)

// listIDs hands out list ids: ones already on the page are kept, new ones
// follow them.
type listIDs struct {
	used map[string]bool
	next int
}

func existingListIDs(doc string) *listIDs {
	ids := &listIDs{used: map[string]bool{}, next: 1}
	for _, m := range listIDRe.FindAllStringSubmatch(doc, -1) {
		if n, err := strconv.Atoi(m[1][1:]); err == nil && n >= ids.next {
			ids.next = n + 1
		}
	}
	return ids
}

func (ids *listIDs) take(existing string) string {
	if existing != "" && !ids.used[existing] {
		ids.used[existing] = true
		return existing
	}
	id := "l" + strconv.Itoa(ids.next)
	ids.next++
	ids.used[id] = true
	return id
}

// inNavigation reports whether the open elements are navigation or a form:
// repeated links and fields there are not a list to edit.
func inNavigation(stack []*frame) bool {
	for _, f := range stack {
		classes := " " + attr(f.token, "class") + " "
		if f.tag == "nav" || f.tag == "form" || f.tag == "select" ||
			strings.Contains(classes, " nav") || strings.Contains(classes, " tabbar ") || strings.Contains(classes, " sidebar ") || strings.Contains(classes, " topbar ") {
			return true
		}
	}
	return false
}

// markList reads f, just closed, as a list when it is one: it names the list,
// marks the fields inside it with their item and column, and puts the list's
// id on f's tag.
func markList(f *frame, fields []Field, heading string, nav bool, ids *listIDs) (List, bool) {
	named := strings.TrimSpace(attr(f.token, listAttr))
	if nav && named == "" {
		return List{}, false
	}
	var items []*frame
	for _, c := range f.children {
		if c.fieldsEnd > c.fieldsAt {
			items = append(items, c)
		}
	}
	if named == "" && !looksLikeList(f.children, items, fields) {
		return List{}, false
	}
	if len(items) == 0 {
		return List{}, false
	}
	// A list holding a list is read by its inner one.
	for _, it := range items {
		for _, fl := range fields[it.fieldsAt:it.fieldsEnd] {
			if fl.List != "" {
				return List{}, false
			}
		}
	}
	id := ids.take(attr(f.token, listIDAttr))
	setAttr(&f.token, listIDAttr, id)
	first := fields[items[0].fieldsAt:items[0].fieldsEnd]
	l := List{ID: id, Name: listName(named, heading), Columns: listColumns(first), Items: len(items)}
	for i, it := range items {
		for col := range fields[it.fieldsAt:it.fieldsEnd] {
			fl := &fields[it.fieldsAt+col]
			fl.List, fl.Item, fl.Col = id, i, col
		}
	}
	return l, true
}

// looksLikeList reports whether children, the ones with text being items,
// are repeated items: two or more, every child of one tag and class, each
// item with the same number of parts, and not all buttons and links.
func looksLikeList(children, items []*frame, fields []Field) bool {
	if len(items) < 2 || len(children) != len(items) {
		return false
	}
	sig := func(c *frame) string { return c.tag + "." + strings.Join(strings.Fields(attr(c.token, "class")), ".") }
	for _, c := range items {
		if !listItemTag(c) {
			return false
		}
	}
	parts := items[0].fieldsEnd - items[0].fieldsAt
	if parts > maxListColumns {
		return false
	}
	actions := 0
	for _, it := range items {
		if sig(it) != sig(items[0]) || it.fieldsEnd-it.fieldsAt != parts {
			return false
		}
		for _, fl := range fields[it.fieldsAt:it.fieldsEnd] {
			if fl.Label == "Button" || fl.Label == "Link" || fl.Label == "Choice" {
				actions++
			}
		}
	}
	return actions < len(items)*parts
}

// listItemTag reports whether c can be one item of a list: not a screen, a
// page section or a layout container.
func listItemTag(c *frame) bool {
	switch c.tag {
	case "section", "header", "footer", "main", "body", "html", "nav", "form":
		return false
	}
	if attr(c.token, "data-screen") != "" {
		return false
	}
	for _, cl := range strings.Fields(attr(c.token, "class")) {
		if cl == "section" || cl == "container" || cl == "narrow" || strings.HasPrefix(cl, "section-") {
			return false
		}
	}
	return true
}

func listName(named, heading string) string {
	if named != "" {
		if strings.ContainsAny(named, " '") {
			return named
		}
		return humanize(named)
	}
	if h := strings.TrimSpace(heading); h != "" {
		return h
	}
	return "Items"
}

// genericColumns are labels that name an element, not what it holds: a
// list's columns take plain names for them.
var genericColumns = map[string]bool{"Text": true, "Heading": true, "Main heading": true, "Small label": true, "Intro": true, "List item": true, "Table cell": true}

func listColumns(first []Field) []string {
	cols := make([]string, 0, len(first))
	generic := 0
	for _, fl := range first {
		label := fl.Label
		if genericColumns[label] {
			switch generic {
			case 0:
				label = "Name"
			case 1:
				label = "Description"
			default:
				label = fmt.Sprintf("Detail %d", generic-1)
			}
			generic++
		}
		cols = append(cols, label)
	}
	return cols
}

// ErrNoList is a list change naming a list or item the page does not have.
var ErrNoList = errors.New("no such list item")

// itemSpans are the byte spans of a list's items, in order.
func itemSpans(doc, id string) ([][2]int, error) {
	z := nethtml.NewTokenizer(strings.NewReader(doc))
	offset, depth, listDepth := 0, 0, -1
	var spans [][2]int
	itemStart := -1
	for {
		tt := z.Next()
		if tt == nethtml.ErrorToken {
			break
		}
		raw := z.Raw()
		start := offset
		offset += len(raw)
		switch tt {
		case nethtml.StartTagToken:
			tok := z.Token()
			if voidElements[tok.Data] {
				continue
			}
			depth++
			if listDepth < 0 && attr(tok, listIDAttr) == id {
				listDepth = depth
			} else if listDepth >= 0 && depth == listDepth+1 {
				itemStart = start
			}
		case nethtml.EndTagToken:
			if listDepth >= 0 && depth == listDepth+1 && itemStart >= 0 {
				spans = append(spans, [2]int{itemStart, offset})
				itemStart = -1
			}
			if depth == listDepth {
				return spans, nil
			}
			depth--
		}
	}
	if listDepth < 0 {
		return nil, ErrNoList
	}
	return spans, nil
}

var keyAttrsRe = regexp.MustCompile(` data-c(?:-ph)?="c\d+"`)

// ListAdd adds an item to the end of list id, a copy of its last item with
// fresh keys, and returns the page and its content re-keyed.
func ListAdd(doc string, current Content, id string) (string, Content, error) {
	spans, err := itemSpans(doc, id)
	if err != nil || len(spans) == 0 {
		return "", Content{}, ErrNoList
	}
	last := spans[len(spans)-1]
	item := keyAttrsRe.ReplaceAllString(doc[last[0]:last[1]], "")
	page, content := Keyed(doc[:last[1]]+item+doc[last[1]:], &current)
	return page, content, nil
}

// ListRemove removes item from list id; a list keeps at least one item.
func ListRemove(doc string, current Content, id string, item int) (string, Content, error) {
	spans, err := itemSpans(doc, id)
	if err != nil || item < 0 || item >= len(spans) || len(spans) < 2 {
		return "", Content{}, ErrNoList
	}
	s := spans[item]
	page, content := Keyed(doc[:s[0]]+doc[s[1]:], &current)
	return page, content, nil
}

// ListMove moves item from to position to in list id.
func ListMove(doc string, current Content, id string, from, to int) (string, Content, error) {
	spans, err := itemSpans(doc, id)
	if err != nil || from < 0 || from >= len(spans) || to < 0 || to >= len(spans) {
		return "", Content{}, ErrNoList
	}
	if from == to {
		page, content := Keyed(doc, &current)
		return page, content, nil
	}
	items := make([]string, len(spans))
	for i, s := range spans {
		items[i] = doc[s[0]:s[1]]
	}
	moved := items[from]
	items = append(items[:from], items[from+1:]...)
	items = append(items[:to], append([]string{moved}, items[to:]...)...)
	// The text between items (indentation) stays where it was.
	var b strings.Builder
	b.WriteString(doc[:spans[0][0]])
	for i, s := range spans {
		b.WriteString(items[i])
		if i+1 < len(spans) {
			b.WriteString(doc[s[1]:spans[i+1][0]])
		}
	}
	b.WriteString(doc[spans[len(spans)-1][1]:])
	page, content := Keyed(b.String(), &current)
	return page, content, nil
}
