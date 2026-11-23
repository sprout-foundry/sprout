package mockup

import "testing"

func TestTextLabel_NamesFactsByWhatTheySay(t *testing.T) {
	cases := []struct{ text, class, want string }{
		{"£3.40", "", "Price"},
		{"From £38", "", "Price"},
		{"10 pts", "", "Price"},
		{"7:30 pm", "", "Time"},
		{"Tuesday 6:30pm", "", "Time"},
		{"Saturday, 14 June", "", "Date"},
		{"07700 900 412", "", "Phone"},
		{"hello@rosascakes.co.uk", "", "Email"},
		{"Rosa's Cakes", "nav-brand", "Name"},
		{"Made in Bristol", "eyebrow", "Small label"},
		{"We bake one cake at a time.", "lead", "Intro"},
		{"We bake one cake at a time, every Saturday in June.", "", ""},
	}
	for _, c := range cases {
		if got := textLabel(c.text, c.class); got != c.want {
			t.Errorf("textLabel(%q, %q) = %q, want %q", c.text, c.class, got, c.want)
		}
	}
	if !interactiveTag("button", "") || !interactiveTag("a", "") || interactiveTag("a", "nav-brand") {
		t.Error("buttons and links keep their own labels; the brand is named")
	}
}
