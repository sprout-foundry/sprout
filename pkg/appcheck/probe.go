//go:build !js

package appcheck

// probe.go is the driver around the in-page scripts: starting at /, it
// follows the app's own links and uses the app the way a visitor would. The
// first pass fills every form, makes the choices a person makes, steps
// through multi-step forms and submits them, going over the pages twice so a
// form that needs data from another page (an entry needs a client) gets it.
// The second pass loads every page fresh and records its status, visible text
// and a screenshot. Failed requests and console errors are recorded
// throughout, and the whole probe stops at its time budget (the fresh-load
// pass still runs, since the pages are what the report is about).

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// probe limits, matching the platform's check.
const (
	// maxPages bounds how many routes are visited.
	maxPages = 8
	// maxFormsPerRoute bounds how many forms are used on one page.
	maxFormsPerRoute = 4
	// maxFormSteps bounds how many Next steps a multi-step form is walked.
	maxFormSteps = 5
	// maxActionButtons bounds how many buttons outside forms are pressed.
	maxActionButtons = 4
	// navigationTimeout bounds one page load.
	navigationTimeout = 20 * time.Second
	// settleDelay is the pause after a click that changes the page, so the
	// app can react before the probe reads the result.
	settleDelay = 600 * time.Millisecond
	// handStepDelay is the pause after each hand-filled field or picked day,
	// so a chip or a calendar has time to appear.
	handStepDelay = 300 * time.Millisecond
	// submitDelay is the pause after a submit, so the request it makes is
	// recorded before the outcome is judged.
	submitDelay = 800 * time.Millisecond
	// maxPageText bounds the text kept per page in the report.
	maxPageText = 4000
	// maxConsoleErrors bounds the console errors kept in the report.
	maxConsoleErrors = 20
)

// probeRun is everything the probe observed, before findings are written.
type probeRun struct {
	pages         []Page
	forms         []FormOutcome
	failed        []string
	submitErrors  []string
	consoleErrors []string
	formsUsed     int
	probeKept     bool
	budgetReached bool
}

// probeConfig configures one probe.
type probeConfig struct {
	// Browser opens the pages.
	Browser RodBrowser
	// BaseURL is the served app's origin.
	BaseURL string
	// ShotsDir is where screenshots go ("" = none).
	ShotsDir string
	// Budget bounds the time spent using the app.
	Budget time.Duration
}

// probe uses the app in the browser and reports what it observed.
func probe(ctx context.Context, cfg probeConfig) (*probeRun, error) {
	browser, err := openProbeBrowser(ctx, cfg.Browser, cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	defer browser.close()
	return runProbe(browser, browser.base, cfg.ShotsDir, cfg.Budget)
}

// runProbe drives the probe over an already-open page. It is what the Rod
// browser and the tests both run.
func runProbe(page pageOps, baseURL, shotsDir string, budget time.Duration) (*probeRun, error) {
	if shotsDir != "" {
		if err := os.MkdirAll(shotsDir, 0o755); err != nil {
			return nil, fmt.Errorf("create screenshot directory: %w", err)
		}
	}
	p := &prober{page: page, base: baseURL, run: &probeRun{}, deadline: time.Now().Add(budget), shotsDir: shotsDir}
	routes := p.useApp()
	p.run.budgetReached = p.late()
	p.freshLoads(routes)

	p.run.failed = nonNil(dedupe(page.failed()))
	p.run.submitErrors = nonNil(dedupe(page.submitErrors()))
	p.run.consoleErrors = nonNil(limit(dedupe(page.consoleErrors()), maxConsoleErrors))
	return p.run, nil
}

// prober is one probe in progress.
type prober struct {
	page     pageOps
	base     string
	run      *probeRun
	deadline time.Time
	shotsDir string
}

// late reports whether the time budget is spent.
func (p *prober) late() bool { return time.Now().After(p.deadline) }

// remaining is the time left in the budget, never negative.
func (p *prober) remaining() time.Duration {
	if d := time.Until(p.deadline); d > 0 {
		return d
	}
	return 0
}

// word is the probe's value for the current form ("Alfa", "Bravo", ...),
// cycling so entries are distinguishable.
func (p *prober) word() string {
	return probeWords[(p.run.formsUsed-1+len(probeWords))%len(probeWords)]
}

// today is the current date, as a date field's value.
func (p *prober) today() string { return time.Now().Format("2006-01-02") }

// useApp is pass 1: use the app, and return the routes it found.
func (p *prober) useApp() []string {
	var routes []string
	for round := 0; round < 2 && !p.late(); round++ {
		queue := []string{"/"}
		seen := map[string]bool{}
		for len(queue) > 0 && len(seen) < maxPages && !p.late() {
			route := queue[0]
			queue = queue[1:]
			if seen[route] {
				continue
			}
			seen[route] = true
			if _, err := p.page.load(route, navigationTimeout); err != nil {
				// A route that does not load is reported by the fresh-load
				// pass; using it is pointless.
				continue
			}
			if round == 1 {
				// The second round presses action buttons first (one may
				// reveal a form, such as "Add client"), then uses the forms
				// again.
				p.useButtons(route)
			}
			p.useForms(route)
			for _, r := range p.discover(route) {
				if !seen[r] && !contains(queue, r) {
					queue = append(queue, r)
				}
			}
			if round == 0 {
				routes = append(routes, route)
			}
		}
	}
	return routes
}

// freshLoads is pass 2: every page loaded fresh, with its status, text and
// screenshot recorded.
func (p *prober) freshLoads(routes []string) {
	p.page.setPhase(2)
	if len(routes) == 0 {
		routes = []string{"/"}
	}
	queue := append([]string{}, routes...)
	seen := map[string]bool{}
	for len(queue) > 0 && len(p.run.pages) < maxPages {
		route := queue[0]
		queue = queue[1:]
		if seen[route] {
			continue
		}
		seen[route] = true
		res, _ := p.page.load(route, navigationTimeout)
		page := Page{Route: route, Status: res.status, Text: truncate(res.text, maxPageText)}
		if p.shotsDir != "" {
			path := filepath.Join(p.shotsDir, fmt.Sprintf("page-%d.png", len(p.run.pages)))
			if err := p.page.screenshot(path); err == nil {
				page.Screenshot = path
			}
		}
		p.run.pages = append(p.run.pages, page)
		for _, r := range p.discover(route) {
			if !seen[r] && !contains(queue, r) {
				queue = append(queue, r)
			}
		}
	}
}

// discover returns the same-origin routes linked from the route.
func (p *prober) discover(route string) []string {
	script := strings.NewReplacer("BASE_ORIGIN", quote(p.base), "BASE_ROUTE", quote(route)).Replace(discoverScript)
	var hrefs []string
	if err := p.page.eval(script, nil, &hrefs); err != nil {
		return nil
	}
	return hrefs
}

// useForms fills, chooses and submits each form on the route, recording the
// outcome, and notes whether the probe's values are shown on the page.
func (p *prober) useForms(route string) {
	// The text before anything was entered, so a value the app shows later
	// can be told from one that was already there.
	before, _ := p.page.evalText(pageTextScript, nil)
	var formCount int
	if err := p.page.eval(countFormsScript, nil, &formCount); err != nil {
		return
	}
	for i := 0; i < formCount && i < maxFormsPerRoute; i++ {
		if p.late() {
			return
		}
		p.useForm(route, i)
	}
	after, _ := p.page.evalText(pageTextScript, nil)
	if matchPattern(probeTextPattern, after) && !matchPattern(probeTextPattern, before) {
		p.run.probeKept = true
	}
}

// useForm uses the n-th form on the page: fill it, make the choices a person
// makes, walk any Next steps, submit, and judge what happened.
func (p *prober) useForm(route string, index int) {
	p.run.formsUsed++
	label, err := p.page.evalText(formScript(formLabelScript, index), nil)
	if err != nil || strings.TrimSpace(label) == "" {
		label = "form"
	}
	mark := p.page.writeMark()
	steps := 1
	p.fill(index)
	p.fillByHand(index)
	p.choose(index)
	for steps < maxFormSteps && !p.late() {
		var next int
		if err := p.page.eval(formScript(nextStepScript, index, nextStepPattern), nil, &next); err != nil || next < 0 {
			break
		}
		_ = p.page.eval(fmt.Sprintf(clickButtonScript, index, next), nil, nil)
		sleepUntil(p, settleDelay)
		steps++
		p.fill(index)
		p.fillByHand(index)
		p.choose(index)
	}

	var submit struct {
		Index int  `json:"index"`
		Next  bool `json:"next"`
	}
	_ = p.page.eval(formScript(submitIndexScript, index, nextStepPattern), nil, &submit)
	hasSubmit := submit.Index >= 0 && !submit.Next
	if hasSubmit {
		_ = p.page.eval(formScript(clickSubmitScript, index), nil, nil)
		sleepUntil(p, submitDelay)
	}

	var blocked []string
	_ = p.page.eval(formScript(blockedScript, index), nil, &blocked)
	p.run.forms = append(p.run.forms, FormOutcome{
		Route:        route,
		Form:         truncate(label, 60),
		Steps:        steps,
		SubmitButton: hasSubmit,
		Sent:         nonNil(p.page.writesSince(mark)),
		Blocked:      nonNil(blocked),
	})
}

// fieldArgs are the values the fill script uses for one form.
func (p *prober) fieldArgs(index int) []string {
	return []string{
		multiValuePattern,
		p.word(),
		p.today(),
		fmt.Sprintf("probe%d@example.com", p.run.formsUsed),
	}
}

// fill fills the form's empty fields with probe values chosen from each
// field's label. Fields that collect several values and read-only date
// fields (a calendar opens on click) are left to the hand pass.
func (p *prober) fill(index int) {
	_ = p.page.eval(formScript(fillFormScript, index, p.fieldArgs(index)...), nil, nil)
}

// fillByHand does what a person does by hand: types a tag and presses Enter,
// and picks a day in a calendar that opens from a read-only date field.
func (p *prober) fillByHand(index int) {
	var fields struct {
		Multi     int `json:"multi"`
		Calendars int `json:"calendars"`
	}
	if err := p.page.eval(formScript(handFieldsScript, index, multiValuePattern), nil, &fields); err != nil {
		return
	}
	for i := 0; i < fields.Multi; i++ {
		if p.late() {
			return
		}
		var filled bool
		script := handScriptsPrefix(multiValuePattern) + typeMultiValueBody + fmt.Sprintf(typeMultiValueScript, index, "Probe "+p.word(), "lookupHandField")
		if err := p.page.eval(script, nil, &filled); err == nil && filled {
			sleepUntil(p, handStepDelay)
		}
	}
	for i := 0; i < fields.Calendars; i++ {
		if p.late() {
			return
		}
		var opened bool
		script := handScriptsPrefix(multiValuePattern) + openCalendarBody + fmt.Sprintf(openCalendarScript, index, "lookupHandField")
		if err := p.page.eval(script, nil, &opened); err == nil && opened {
			sleepUntil(p, handStepDelay)
			var picked bool
			_ = p.page.eval(pickCalendarDayScript, nil, &picked)
			sleepUntil(p, handStepDelay)
		}
	}
}

// choose makes the choices a person makes before submitting (the first radio
// option, then the first choice button such as a time slot), and fills the
// fields again when a choice may have revealed or reset them.
func (p *prober) choose(index int) {
	var outcome struct {
		Pressed bool `json:"pressed"`
	}
	_ = p.page.eval(formScript(chooseScript, index, destructivePattern, nextStepPattern, multiValuePattern), nil, &outcome)
	if outcome.Pressed {
		sleepUntil(p, settleDelay)
		p.fill(index)
	}
}

// useButtons presses action buttons outside forms (a tick, a toggle, "mark
// done"), once each, never anything that deletes, removes or cancels.
func (p *prober) useButtons(route string) {
	var labels []string
	if err := p.page.eval(formScript(actionButtonsScript, 0, destructivePattern, actionPattern), nil, &labels); err != nil {
		return
	}
	for _, label := range labels {
		if p.late() {
			return
		}
		mark := p.page.writeMark()
		var pressed bool
		err := p.page.eval(fmt.Sprintf(pressButtonScript, quote(label)), nil, &pressed)
		if err != nil || !pressed {
			continue
		}
		sleepUntil(p, settleDelay)
		p.run.forms = append(p.run.forms, FormOutcome{
			Route:        route,
			Form:         "button: " + label,
			Steps:        1,
			SubmitButton: true,
			Sent:         nonNil(p.page.writesSince(mark)),
			Blocked:      []string{},
		})
	}
}

// sleepUntil waits for d without running past the probe's budget.
func sleepUntil(p *prober, d time.Duration) {
	if left := p.remaining(); left < d {
		d = left
	}
	if d > 0 {
		time.Sleep(d)
	}
}

// truncate shortens s to at most n bytes of text.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// dedupe removes repeated entries, keeping the first occurrence's order.
func dedupe(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// limit keeps at most n entries.
func limit(values []string, n int) []string {
	if len(values) > n {
		return values[:n]
	}
	return values
}

// nonNil returns values, or an empty list, so the report never writes null.
func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// contains reports whether values holds want.
func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// matchPattern reports whether text matches a JavaScript regular expression
// source, as the probe's patterns are written (Go and JavaScript agree on the
// subset the probe uses).
func matchPattern(source, text string) bool {
	re, err := regexp.Compile(source)
	if err != nil {
		return false
	}
	return re.MatchString(text)
}

// quote renders s as a JavaScript string literal.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		switch r {
		case '\'', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}
