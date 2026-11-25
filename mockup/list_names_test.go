package mockup

import (
	"strings"
	"testing"
)

func listsOf(t *testing.T, page string) []List {
	t.Helper()
	_, c := Keyed(page, nil)
	return c.Lists
}

// Prices labelled per item ("Sourdough price", "Rye price") share one
// column, named by the words the labels share.
func TestKeyed_ColumnFromTheLabelsItemsShare(t *testing.T) {
	lists := listsOf(t, `<section data-screen="home"><h2>Breads</h2><div class="grid">
<div class="card"><h3>Sourdough</h3><span data-label="Sourdough price">£4.50</span><p>Slow and sour.</p></div>
<div class="card"><h3>Rye</h3><span data-label="Rye price">£5.00</span><p>Dark and nutty.</p></div>
<div class="card"><h3>Country white</h3><span data-label="Country white price">£4.00</span><p>Plain and right.</p></div>
</div></section>`)
	if len(lists) != 1 || strings.Join(lists[0].Columns, ",") != "Name,Description,Price" {
		t.Fatalf("lists = %+v, want one list with Name, Description, Price", lists)
	}
}

// A long label is not a column name: "Lessons given each week" reads as
// the part it is.
func TestKeyed_NoLongLabelAsAColumn(t *testing.T) {
	lists := listsOf(t, `<section data-screen="home"><h2>Lessons</h2><ul class="lessons">
<li><span data-label="Lessons given each week">Beginners</span><span>£30</span></li>
<li><span data-label="Lessons given each week">Grade 5</span><span>£35</span></li>
<li><span data-label="Lessons given each week">Adults</span><span>£40</span></li>
</ul></section>`)
	if len(lists) != 1 || strings.Join(lists[0].Columns, ",") != "Name,Price" {
		t.Fatalf("lists = %+v, want columns Name, Price", lists)
	}
}

// A list is named by what its items are, not by a headline sentence.
func TestKeyed_ListNamedNotByAHeadlineSentence(t *testing.T) {
	lists := listsOf(t, `<section data-screen="home"><h2>Two lengths, one simple price.</h2><div class="grid">
<div class="card"><h3>Half hour</h3><span class="price">£20</span></div>
<div class="card"><h3>Full hour</h3><span class="price">£35</span></div>
</div><h2>Free times</h2><ul class="slots"><li>Mon</li><li>Wed</li><li>Fri</li></ul></section>`)
	if len(lists) != 2 || lists[0].Name == "Two lengths, one simple price." || lists[1].Name != "Free times" {
		t.Fatalf("lists = %+v: a sentence names no list; a short heading does", lists)
	}
}

// A timetable row keeps each part as its own column: day, date and time.
func TestKeyed_TimetableKeepsEveryPart(t *testing.T) {
	lists := listsOf(t, `<section data-screen="home"><h2>Free times</h2><ul class="slots">
<li><span class="day">Mon</span><span class="date">1 Jan</span><span class="time">9:00</span></li>
<li><span class="day">Tue</span><span class="date">2 Jan</span><span class="time">10:00</span></li>
<li><span class="day">Wed</span><span class="date">3 Jan</span><span class="time">16:30</span></li>
</ul></section>`)
	if len(lists) != 1 || strings.Join(lists[0].Columns, ",") != "Day,Date,Time" {
		t.Fatalf("lists = %+v, want columns Day, Date, Time", lists)
	}
}

// A hero card beside smaller cards of one class is a layout, not a list.
func TestKeyed_HeroBesideCardsIsNotAList(t *testing.T) {
	lists := listsOf(t, `<section data-screen="home"><div class="tiles">
<div class="tile"><p class="eyebrow">Leeds</p><h1>Piano lessons</h1><p class="lead">For every age.</p><a class="btn">Book</a></div>
<div class="tile"><h3>Beginners</h3><p>Start here.</p><p>£30</p><p>Weekly</p></div>
<div class="tile"><h3>Grade 5</h3><p>Exam prep.</p><p>£35</p><p>Weekly</p></div>
</div></section>`)
	for _, l := range lists {
		if l.Items == 3 {
			t.Fatalf("lists = %+v: the hero is read as one item of a list", lists)
		}
	}
}

// A card's running number ("01") is a number, not a time.
func TestTextLabel_BareNumberIsANumber(t *testing.T) {
	if got := textLabel("01", ""); got != "Number" {
		t.Fatalf("textLabel(01) = %q, want Number", got)
	}
	if got := textLabel("9:30am", ""); got != "Time" {
		t.Fatalf("textLabel(9:30am) = %q, want Time", got)
	}
}

// A row of cards each naming a different fact is the page's facts, not a
// list, even where the drawing named it one; cards sharing labels still are.
func TestKeyed_FactCardsAreNotAList(t *testing.T) {
	_, c := Keyed(`<section data-screen="home"><div class="grid" data-list="Party facts">
<div class="card"><span data-label="Party date">Saturday 14 March</span><span>2pm – 4pm</span></div>
<div class="card"><span data-label="Party place">12 Example Lane</span><span>Our house</span></div>
<div class="card"><span data-label="Reply by">Wed 11 March</span><span>Don't be late!</span></div>
</div></section>`, nil)
	if len(c.Lists) != 0 {
		t.Fatalf("lists = %+v: distinct fact cards are not a list", c.Lists)
	}
	for _, f := range c.Fields {
		if f.Label == "Party date" && f.List != "" {
			t.Fatalf("the party date is in a list: %+v", f)
		}
	}
	lists := listsOf(t, `<section data-screen="home"><h2>Breads</h2><div class="grid">
<div class="card"><h3>Sourdough</h3><span data-label="Sourdough price">£4.50</span></div>
<div class="card"><h3>Rye</h3><span data-label="Rye price">£5.00</span></div>
</div></section>`)
	if len(lists) != 1 {
		t.Fatalf("lists = %+v: cards of one kind stay a list", lists)
	}
}
