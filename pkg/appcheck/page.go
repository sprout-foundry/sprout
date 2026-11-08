//go:build !js

package appcheck

// page.go is the seam between the probe's driver and the browser: the driver
// (probe.go) uses only pageOps, which the Rod implementation in browser.go
// satisfies. A test supplies its own pageOps, so the driver's rounds, forms,
// budget and findings are exercised without a browser; the Rod implementation
// is exercised by the browser-tagged tests and by `sprout check app` itself.

import "time"

// pageOps is what the probe needs from a browser page.
type pageOps interface {
	// load navigates to a route and returns its status (the document
	// response; -1 when nothing answered) and its visible text. A page that
	// never goes idle is not an error: its content is still returned.
	load(route string, timeout time.Duration) (pageResult, error)
	// eval runs a page script and decodes its result into out (nil out
	// discards it).
	eval(script string, args any, out any) error
	// evalText runs a page script and returns its result as text.
	evalText(script string, args any) (string, error)
	// screenshot writes a full-page screenshot.
	screenshot(path string) error
	// setPhase switches the pass failing requests are classified for (1 =
	// using the app, 2 = fresh page loads).
	setPhase(phase int)
	// failed, submitErrors and consoleErrors are the recordings so far.
	failed() []string
	submitErrors() []string
	consoleErrors() []string
	// writeMark and writesSince delimit the write requests one form's submit
	// made.
	writeMark() int
	writesSince(mark int) []string
	// close releases the page.
	close()
}
