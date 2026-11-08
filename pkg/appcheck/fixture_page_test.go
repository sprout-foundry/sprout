//go:build !js

package appcheck

// fixture_page_test.go is a pageOps stand-in for a small fixture app: it
// answers the probe's scripts from a declarative description of the site, the
// way a browser would for the shapes the tests exercise (a page that errors,
// a page that is blank, a form that sends nothing, a request that fails). The
// real Rod page is exercised by the browser-tagged tests and by `sprout check
// app` itself.

import (
	"fmt"
	"strings"
	"time"
)

// fixtureSite describes a small app.
type fixtureSite struct {
	// pages maps a route to its page.
	pages map[string]*fixturePage
	// formsUsed counts the forms the probe used on each route, so the second
	// round differs from the first (as it does on a real app).
	formsUsed map[string]int
}

// fixturePage is one page of the fixture app.
type fixturePage struct {
	// status is the response status (0 = 200).
	status int
	// text is the visible text.
	text string
	// forms are the forms; submitting form i calls submit(i).
	forms []fixtureForm
	// links are same-origin routes the page links to.
	links []string
	// consoleErrors are produced when the page loads.
	consoleErrors []string
}

// fixtureForm is one form.
type fixtureForm struct {
	label  string
	submit func() fixtureSubmit
}

// fixtureSubmit is what submitting a form does.
type fixtureSubmit struct {
	// blocked is the browser's refusal ("" = the app takes the values).
	blocked string
	// sent is the write request ("status method path"; "" = nothing sent).
	sent string
}

// fixturePageOps is the pageOps the probe drives in unit tests.
type fixturePageOps struct {
	site        *fixtureSite
	page        *fixturePage
	route       string
	phase       int
	writes      []string
	failedLines []string
	subErr      []string
	conErr      []string
	// submitted records, per route and form, whether it submitted.
	submitted map[string]bool
	shots     int
}

func newFixturePageOps(site *fixtureSite) *fixturePageOps {
	return &fixturePageOps{site: site, submitted: map[string]bool{}}
}

// load implements pageOps.
func (p *fixturePageOps) load(route string, _ time.Duration) (pageResult, error) {
	page, ok := p.site.pages[route]
	if !ok {
		p.recordRequest("GET", route, 404)
		return pageResult{status: 404}, nil
	}
	p.page, p.route = page, route

	status := page.status
	if status == 0 {
		status = 200
	}
	p.conErr = append(p.conErr, page.consoleErrors...)
	p.recordRequest("GET", route, status)
	return pageResult{status: status, text: page.text}, nil
}

// recordRequest records a request the way the browser's network events do:
// writes and failures go to the pass's lists.
func (p *fixturePageOps) recordRequest(method, path string, status int) {
	line := fmt.Sprintf("%d %s %s", status, method, path)
	if method != "GET" {
		p.writes = append(p.writes, line)
	}
	if status < 400 {
		return
	}
	if p.phase == 2 {
		p.failedLines = append(p.failedLines, line)
	} else if status == 404 || status >= 500 {
		p.subErr = append(p.subErr, line)
	}
}

// eval implements pageOps: it answers the scripts the driver uses, by
// matching the exact script where the driver uses one and the script's shape
// where it is built per form.
func (p *fixturePageOps) eval(script string, args any, out any) error {

	switch {
	case script == countFormsScript:
		*out.(*int) = len(p.page.forms)
	case script == pickCalendarDayScript:
		*out.(*bool) = false
	case strings.Contains(script, "new URL(href, base + "):
		*out.(*[]string) = p.page.links
	case strings.Contains(script, "return { index: -1, next: false };"):
		*out.(*struct {
			Index int  `json:"index"`
			Next  bool `json:"next"`
		}) = struct {
			Index int  `json:"index"`
			Next  bool `json:"next"`
		}{Index: 0, Next: false}
	case strings.Contains(script, "return -1;"):
		*out.(*int) = -1
	case strings.Contains(script, "const own = "):
		if i := formIndexArgs(script); i < len(p.page.forms) {
			*out.(*string) = p.page.forms[i].label
		}
	case strings.Contains(script, "e.validationMessage"):
		index := formIndexArgs(script)
		blocked := []string{}
		if s := p.submitForm(index); s.blocked != "" {
			blocked = append(blocked, s.blocked)
		}
		*out.(*[]string) = blocked
	case strings.Contains(script, "pressed: false"):
		*out.(*struct {
			Pressed bool `json:"pressed"`
		}) = struct {
			Pressed bool `json:"pressed"`
		}{Pressed: false}
	case strings.Contains(script, "out.push(text.slice(0, 40))"):
		*out.(*[]string) = []string{}
	case strings.Contains(script, "return { multi: 0, calendars: 0 }"):
		*out.(*struct {
			Multi     int `json:"multi"`
			Calendars int `json:"calendars"`
		}) = struct {
			Multi     int `json:"multi"`
			Calendars int `json:"calendars"`
		}{}
	case strings.Contains(script, "lookupTypeMultiValue"), strings.Contains(script, "lookupOpenCalendar"):
		*out.(*bool) = false
	}
	return nil
}

// submitForm submits form index once per form per round (a second submit of
// the same form in the same round does nothing, as a disabled button would).
func (p *fixturePageOps) submitForm(index int) fixtureSubmit {
	if index >= len(p.page.forms) {
		return fixtureSubmit{}
	}
	form := p.page.forms[index]
	result := fixtureSubmit{}
	if form.submit != nil {
		result = form.submit()
	}
	key := fmt.Sprintf("%s/%d/%d", p.route, index, len(p.writes))
	if p.submitted[key] {
		return result
	}
	p.submitted[key] = true
	if result.sent != "" {
		parts := strings.SplitN(result.sent, " ", 3)
		status := 0
		fmt.Sscanf(parts[0], "%d", &status)
		p.recordRequest(parts[1], parts[2], status)
	}
	return result
}

// formIndexArgs reads the form index from a rendered script ("...('form')[2]").
func formIndexArgs(script string) int {
	i := strings.Index(script, "document.querySelectorAll('form')[")
	if i < 0 {
		return 0
	}
	rest := script[i+len("document.querySelectorAll('form')["):]
	end := strings.IndexByte(rest, ']')
	if end < 0 {
		return 0
	}
	n := 0
	fmt.Sscanf(rest[:end], "%d", &n)
	return n
}

// evalText implements pageOps.
func (p *fixturePageOps) evalText(script string, args any) (string, error) {
	if script == pageTextScript {
		return p.page.text, nil
	}
	var text string
	if err := p.eval(script, args, &text); err != nil {
		return "", err
	}
	return text, nil
}

// screenshot implements pageOps.
func (p *fixturePageOps) screenshot(path string) error {
	p.shots++
	return writeEmptyFile(path)
}

// setPhase implements pageOps.
func (p *fixturePageOps) setPhase(phase int) { p.phase = phase }

// failed implements pageOps.
func (p *fixturePageOps) failed() []string { return p.failedLines }

// submitErrors implements pageOps.
func (p *fixturePageOps) submitErrors() []string { return p.subErr }

// consoleErrors implements pageOps.
func (p *fixturePageOps) consoleErrors() []string { return p.conErr }

// writeMark implements pageOps.
func (p *fixturePageOps) writeMark() int { return len(p.writes) }

// writesSince implements pageOps.
func (p *fixturePageOps) writesSince(mark int) []string { return p.writes[mark:] }

// close implements pageOps.
func (p *fixturePageOps) close() {}
