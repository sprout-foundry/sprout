package mockup

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	weekdayNames = `Monday|Tuesday|Wednesday|Thursday|Friday|Saturday|Sunday|Mon|Tue|Tues|Wed|Thu|Thur|Thurs|Fri|Sat|Sun`
	monthNames   = `January|February|March|April|May|June|July|August|September|October|November|December|Jan|Feb|Mar|Apr|Jun|Jul|Aug|Sep|Sept|Oct|Nov|Dec`
)

// A weekday with a date after it: "Saturday, 8 April 2027", "Sat 8th Apr",
// or "Saturday, June 14, 2027".
var (
	weekdayDayMonthRe = regexp.MustCompile(`(?i)\b(` + weekdayNames + `)\b(,?\s+(?:the\s+)?)(\d{1,2})(?:st|nd|rd|th)?(\s+(?:of\s+)?)(` + monthNames + `)\b(,?\s+(\d{4}))?`)
	weekdayMonthDayRe = regexp.MustCompile(`(?i)\b(` + weekdayNames + `)\b(,?\s+)(` + monthNames + `)(\.?\s+)(\d{1,2})(?:st|nd|rd|th)?\b(,?\s+(\d{4}))?`)
)

// fixWeekdays corrects the weekday written before a date, which a model often
// gets wrong. A date without a year is taken as its next one from today.
func fixWeekdays(doc string, today time.Time) string {
	doc = weekdayDayMonthRe.ReplaceAllStringFunc(doc, func(s string) string {
		m := weekdayDayMonthRe.FindStringSubmatch(s)
		return withWeekday(s, m[1], m[5], m[3], m[7], today)
	})
	return weekdayMonthDayRe.ReplaceAllStringFunc(doc, func(s string) string {
		m := weekdayMonthDayRe.FindStringSubmatch(s)
		return withWeekday(s, m[1], m[3], m[5], m[7], today)
	})
}

// withWeekday is s with its weekday made the one of day month year, written
// the way the weekday was (full or short, and its case).
func withWeekday(s, weekday, month, day, year string, today time.Time) string {
	mon := monthNumber(month)
	d, err := strconv.Atoi(day)
	if mon == 0 || err != nil || d < 1 || d > 31 {
		return s
	}
	y := today.Year()
	if year != "" {
		y, _ = strconv.Atoi(year)
	}
	date := time.Date(y, mon, d, 12, 0, 0, 0, time.UTC)
	if date.Day() != d {
		return s
	}
	if year == "" && date.Before(time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)) {
		date = date.AddDate(1, 0, 0)
	}
	right := date.Weekday().String()
	if len(weekday) < 6 {
		right = right[:3]
	}
	switch {
	case weekday == strings.ToUpper(weekday):
		right = strings.ToUpper(right)
	case weekday == strings.ToLower(weekday):
		right = strings.ToLower(right)
	}
	return right + s[len(weekday):]
}

func monthNumber(name string) time.Month {
	name = strings.ToLower(name)
	for m := time.January; m <= time.December; m++ {
		if strings.HasPrefix(strings.ToLower(m.String()), name[:3]) {
			return m
		}
	}
	return 0
}
