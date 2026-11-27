package mockup

import (
	"testing"
	"time"
)

func TestFixWeekdays(t *testing.T) {
	today := time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)
	cases := map[string]string{
		"Saturday, 8 April 2027":            "Thursday, 8 April 2027",
		"Sat 8 Apr 2027":                    "Thu 8 Apr 2027",
		"Saturday, 18 July":                 "Sunday, 18 July",
		"SATURDAY 18 JULY 2027":             "SUNDAY 18 JULY 2027",
		"Saturday, June 14, 2027":           "Monday, June 14, 2027",
		"Thursday, Nov 19":                  "Friday, Nov 19",
		"Friday, 27 November 2026":          "Friday, 27 November 2026",
		"Tues 1st Dec":                      "Tue 1st Dec",
		"Meet on Monday at 7pm":             "Meet on Monday at 7pm",
		"Saturday, 31 February 2027":        "Saturday, 31 February 2027",
		"<p>Sunday, the 3rd of January</p>": "<p>Sunday, the 3rd of January</p>",
	}
	for in, want := range cases {
		if got := fixWeekdays(in, today); got != want {
			t.Errorf("fixWeekdays(%q) = %q, want %q", in, got, want)
		}
	}
}
