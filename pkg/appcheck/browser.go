//go:build !js

package appcheck

// browser.go is the Rod side of the probe: one page driven across the whole
// run, with console messages, page errors and network responses recorded
// from the browser's own events (the same recordings browse_url reads from
// its page instrumentation).

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"
	"github.com/sprout-foundry/sprout/pkg/webcontent"
	"github.com/ysmood/gson"
)

// stableWait is how long the network and DOM must stay quiet before a page
// counts as settled.
const stableWait = 500 * time.Millisecond

// probeBrowser is the browser during one probe.
type probeBrowser struct {
	session RodSession
	page    *rod.Page
	base    string
	origin  string
	rec     *recorder
}

// openProbeBrowser opens an incognito page and starts recording its events.
// The viewport is the platform check's (1280x900).
func openProbeBrowser(ctx context.Context, browser RodBrowser, baseURL string) (*probeBrowser, error) {
	session, err := browser.OpenSession(ctx)
	if err != nil {
		return nil, err
	}
	page := session.Page()
	origin := strings.TrimSuffix(baseURL, "/")
	b := &probeBrowser{session: session, page: page, base: origin, origin: origin, rec: newRecorder(origin)}
	if err := page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{Width: 1280, Height: 900}); err != nil {
		session.Close()
		return nil, fmt.Errorf("set viewport: %w", err)
	}
	if err := (proto.NetworkEnable{}).Call(page); err != nil {
		session.Close()
		return nil, fmt.Errorf("enable network recording: %w", err)
	}
	b.listen()
	// Dialogs (alert, confirm, prompt) are dismissed: a visitor would never
	// get past one the probe cannot answer.
	go page.EachEvent(func(e *proto.PageJavascriptDialogOpening) {
		_ = proto.PageHandleJavaScriptDialog{Accept: false}.Call(page)
	})()
	return b, nil
}

// listen records network responses and console errors for the page's
// lifetime in the background; the wait function returns when the page closes.
func (b *probeBrowser) listen() {
	go b.page.EachEvent(
		func(e *proto.NetworkRequestWillBeSent) {
			if e.Request == nil {
				return
			}
			b.rec.request(e.RequestID, e.Request.Method, e.Request.URL)
		},
		func(e *proto.NetworkResponseReceived) {
			if e.Response == nil {
				return
			}
			b.rec.response(e.RequestID, e.Type, e.Response.URL, e.Response.Status)
		},
		func(e *proto.RuntimeConsoleAPICalled) {
			if e.Type == proto.RuntimeConsoleAPICalledTypeError {
				b.rec.console(consoleText(e))
			}
		},
		func(e *proto.RuntimeExceptionThrown) {
			b.rec.console(exceptionText(e.ExceptionDetails))
		},
	)()
}

// close releases the page and its context.
func (b *probeBrowser) close() { b.session.Close() }

// exceptionText is an uncaught exception as the page's error message, like
// the Node probe's String(error): Chrome's own Text is only "Uncaught", and
// the message is the thrown value's description.
func exceptionText(d *proto.RuntimeExceptionDetails) string {
	if d == nil {
		return ""
	}
	if d.Exception != nil && strings.TrimSpace(d.Exception.Description) != "" {
		// The description carries the stack after the first line.
		line, _, _ := strings.Cut(d.Exception.Description, "\n")
		return line
	}
	return d.Text
}

// consoleText renders a console call's arguments as one line.
func consoleText(e *proto.RuntimeConsoleAPICalled) string {
	parts := make([]string, 0, len(e.Args))
	for _, arg := range e.Args {
		if arg == nil {
			continue
		}
		if s, ok := arg.Value.Val().(string); ok {
			parts = append(parts, s)
		} else {
			parts = append(parts, arg.Value.JSON("", ""))
		}
	}
	return strings.Join(parts, " ")
}

// pageResult is what one navigation produced.
type pageResult struct {
	status int
	text   string
}

// navigate loads a route and waits for it to settle, bounded by timeout. It is
// also pageOps' load.
// The status is the document response the browser recorded (a navigation
// reports none of its own); a page that never goes idle still has its
// content, so a quiescence timeout is not a failure.
func (b *probeBrowser) navigate(route string, timeout time.Duration) (pageResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	p := b.page.Context(ctx)
	target := b.base + route
	mark := b.rec.mark()
	if err := p.Navigate(target); err != nil {
		return pageResult{status: b.rec.statusOf(target, mark)}, fmt.Errorf("load %s: %w", route, err)
	}
	if err := p.WaitLoad(); err != nil {
		return pageResult{status: b.rec.statusOf(target, mark)}, fmt.Errorf("wait for %s: %w", route, err)
	}
	_ = p.WaitStable(stableWait)
	text, terr := b.evalString(pageTextScript, nil)
	return pageResult{status: b.rec.statusOf(target, mark), text: text}, terr
}

// evalRaw runs a page script with no arguments and, when out is non-nil,
// decodes the result into it. A page that navigated away mid-script makes the
// call fail; that is reported, not fatal to the probe.
func (b *probeBrowser) evalRaw(script string, _ any, out any) error {
	value, err := b.evalValue(script)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := value.Unmarshal(out); err != nil {
		return fmt.Errorf("decode script result: %w", err)
	}
	return nil
}

// evalValue evaluates a script in the page and returns its result. Rod panics
// when the page detaches (a navigation during the script); the probe survives
// that by treating the evaluation as failed.
func (b *probeBrowser) evalValue(script string) (value gson.JSON, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("evaluate script: %v", r)
		}
	}()
	res, evalErr := b.page.Eval(webcontent.EvalStatement(script))
	if evalErr != nil {
		return gson.JSON{}, fmt.Errorf("evaluate script: %w", evalErr)
	}
	return res.Value, nil
}

// evalBool runs a script and reads its result as a boolean.
func (b *probeBrowser) evalBool(script string, args any) bool {
	var ok bool
	if err := b.evalRaw(script, args, &ok); err != nil {
		return false
	}
	return ok
}

// evalString runs a script and reads its result as a string.
func (b *probeBrowser) evalString(script string, args any) (string, error) {
	var s string
	if err := b.evalRaw(script, args, &s); err != nil {
		return "", err
	}
	return s, nil
}

// pressEnter presses Enter on the focused element.
func (b *probeBrowser) pressEnter() error {
	return b.page.Keyboard.Press(input.Enter)
}

// screenshot writes a full-page screenshot to path.
func (b *probeBrowser) screenshot(path string) error {
	data, err := b.page.Context(context.Background()).Screenshot(true, &proto.PageCaptureScreenshot{
		Format: proto.PageCaptureScreenshotFormatPng,
	})
	if err != nil {
		return fmt.Errorf("screenshot: %w", err)
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create screenshot directory: %w", err)
		}
	}
	return os.WriteFile(path, data, 0o644)
}

// recorder records the page's requests, responses and console errors, and
// classifies a failing request by the pass it happened in.
type recorder struct {
	mu            sync.Mutex
	origin        string
	phase         int
	pending       map[proto.NetworkRequestID]recordedRequest
	responses     []recordedResponse
	writes        []string
	failed        []string
	submitErrors  []string
	consoleErrors []string
}

type recordedRequest struct {
	method string
	url    string
}

type recordedResponse struct {
	url    string
	status int
}

// maxRecordedResponses bounds the response log: only the latest page load's
// document response is read back.
const maxRecordedResponses = 500

func newRecorder(origin string) *recorder {
	return &recorder{origin: origin, pending: map[proto.NetworkRequestID]recordedRequest{}}
}

// setPhase switches the pass the recorder classifies failures for.
func (r *recorder) setPhase(phase int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.phase = phase
}

// sameOrigin reports whether rawURL belongs to the served app.
func (r *recorder) sameOrigin(rawURL string) bool {
	return strings.HasPrefix(rawURL, r.origin+"/") || rawURL == r.origin
}

// request remembers a request's method so the response can be reported with
// it.
func (r *recorder) request(id proto.NetworkRequestID, method, rawURL string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pending[id] = recordedRequest{method: method, url: rawURL}
}

// response records one response: the write it made, its page-load status, and
// whether it failed for the person.
func (r *recorder) response(id proto.NetworkRequestID, resourceType proto.NetworkResourceType, rawURL string, status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	req := r.pending[id]
	delete(r.pending, id)
	if !r.sameOrigin(rawURL) {
		return
	}
	if resourceType == proto.NetworkResourceTypeDocument {
		r.recordDocumentLocked(rawURL, status)
	}
	method := req.method
	if method == "" {
		method = "GET"
	}
	line := fmt.Sprintf("%d %s %s", status, method, pathOf(rawURL))
	if method != "GET" {
		r.writes = append(r.writes, line)
	}
	// A missing favicon is the browser's own request, not something a person
	// using the app runs into.
	if status < 400 || pathOf(rawURL) == "/favicon.ico" {
		return
	}
	switch {
	case r.phase == 2:
		// Every failed request of a fresh page load is what a visitor runs
		// into (including the 401 of a redirect-to-login, and a missing
		// image).
		r.failed = append(r.failed, line)
	case status == 404 || status >= 500:
		// Pass 1 records only server errors and missing routes: a 400 there
		// is the app rejecting a probe value, which is fine.
		r.submitErrors = append(r.submitErrors, line)
	}
}

// console records one console error or page error.
func (r *recorder) console(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.consoleErrors = append(r.consoleErrors, truncate(text, 200))
}

// mark returns how many document responses have been recorded.
func (r *recorder) mark() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.responses)
}

// statusOf returns the status of the document response for target recorded
// after mark, or -1 when the page did not load at all (no response).
func (r *recorder) statusOf(target string, mark int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.responses) - 1; i >= mark; i-- {
		if r.responses[i].url == target {
			return r.responses[i].status
		}
	}
	return -1
}

func (r *recorder) recordDocumentLocked(url string, status int) {
	if len(r.responses) > maxRecordedResponses {
		r.responses = r.responses[len(r.responses)/2:]
	}
	r.responses = append(r.responses, recordedResponse{url: url, status: status})
}

// writeMark is the number of write requests recorded so far.
func (r *recorder) writeMark() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.writes)
}

// writesSince returns the write requests recorded after mark.
func (r *recorder) writesSince(mark int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if mark > len(r.writes) {
		return nil
	}
	return append([]string{}, r.writes[mark:]...)
}

// recorded is a copy of everything the recorder holds.
type recorded struct {
	failed        []string
	submitErrors  []string
	consoleErrors []string
}

// snapshot copies the recorder's findings (concurrent event delivery may
// still be appending, so the copy is taken under the lock).
func (r *recorder) snapshot() recorded {
	r.mu.Lock()
	defer r.mu.Unlock()
	return recorded{
		failed:        append([]string{}, r.failed...),
		submitErrors:  append([]string{}, r.submitErrors...),
		consoleErrors: append([]string{}, r.consoleErrors...),
	}
}

// pathOf returns the path (with query) of a URL, as requests read in the
// report.
func pathOf(rawURL string) string {
	i := strings.Index(rawURL, "://")
	if i < 0 {
		return rawURL
	}
	rest := rawURL[i+3:]
	if j := strings.Index(rest, "/"); j >= 0 {
		return rest[j:]
	}
	return "/"
}

// The rest of pageOps (page.go) — the probe's seam — as thin adapters.

// eval implements pageOps.
func (b *probeBrowser) eval(script string, args any, out any) error {
	return b.evalRaw(script, args, out)
}

// evalText implements pageOps.
func (b *probeBrowser) evalText(script string, args any) (string, error) {
	return b.evalString(script, args)
}

// setPhase implements pageOps.
func (b *probeBrowser) setPhase(phase int) { b.rec.setPhase(phase) }

// failed implements pageOps.
func (b *probeBrowser) failed() []string {
	b.rec.mu.Lock()
	defer b.rec.mu.Unlock()
	return append([]string{}, b.rec.failed...)
}

// submitErrors implements pageOps.
func (b *probeBrowser) submitErrors() []string {
	b.rec.mu.Lock()
	defer b.rec.mu.Unlock()
	return append([]string{}, b.rec.submitErrors...)
}

// consoleErrors implements pageOps.
func (b *probeBrowser) consoleErrors() []string {
	b.rec.mu.Lock()
	defer b.rec.mu.Unlock()
	return append([]string{}, b.rec.consoleErrors...)
}

// writeMark implements pageOps.
func (b *probeBrowser) writeMark() int { return b.rec.writeMark() }

// writesSince implements pageOps.
func (b *probeBrowser) writesSince(mark int) []string { return b.rec.writesSince(mark) }

// load implements pageOps: a navigation, as the probe asks for it.
func (b *probeBrowser) load(route string, timeout time.Duration) (pageResult, error) {
	return b.navigate(route, timeout)
}
