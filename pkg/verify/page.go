package verify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/startermanifest"
	"github.com/sprout-foundry/sprout/pkg/webcontent"
)

// PageBrowser is the verify package's narrow browser seam for page checks
// (SP-149 §149a/§149.3): it opens one route in a headless browser and reports
// what it observed. The production implementation adapts pkg/webcontent
// (NewWebcontentPageBrowser); tests inject a fake so a run is deterministic
// and needs no Chromium.
type PageBrowser interface {
	// Open opens url in a headless browser, optionally writing a
	// screenshot to screenshotPath ("" = no screenshot), and reports the
	// observation. A nil error means the route rendered; the caller
	// inspects the observation's ConsoleErrors/PageErrors for failures.
	// errors.Is(err, webcontent.ErrBrowserUnavailable) marks a browser that
	// cannot render here (skipped, not a failure).
	Open(ctx context.Context, url string, screenshotPath string) (*PageObservation, error)
}

// PageObservation is what a PageBrowser reports for one opened route.
type PageObservation struct {
	// URL is the route that was opened.
	URL string
	// Title is the page title after rendering ("" when unknown).
	Title string
	// ConsoleErrors are the console.error-level messages captured on the
	// page (the page's own console.error calls, not the level tag).
	ConsoleErrors []string
	// PageErrors are window errors and unhandled promise rejections.
	PageErrors []string
	// ScreenshotPath is the file the screenshot was saved to, or "" when the
	// capture did not complete (the route is still checked for console
	// errors).
	ScreenshotPath string
}

// webcontentPageBrowser adapts the global webcontent browser to the verify
// PageBrowser seam: one Run call per route with console capture enabled and
// a screenshot, mapping the captured console messages to their error-level
// entries.
type webcontentPageBrowser struct{}

// NewWebcontentPageBrowser returns the production PageBrowser backed by
// webcontent.GetGlobalBrowser(). The global browser is lazy (Chromium
// launches on first use), so this adapter is cheap to construct and
// WASM-safe: it never launches a browser at construction time.
func NewWebcontentPageBrowser() PageBrowser {
	return &webcontentPageBrowser{}
}

// Open implements PageBrowser.
func (b *webcontentPageBrowser) Open(ctx context.Context, url string, screenshotPath string) (*PageObservation, error) {
	renderer := webcontent.GetGlobalBrowser()
	result, err := renderer.Run(ctx, url, webcontent.BrowseOptions{
		IncludeConsole: true,
		ScreenshotPath: screenshotPath,
	})
	if err != nil {
		return nil, err
	}
	return &PageObservation{
		URL:            url,
		Title:          result.Title,
		ConsoleErrors:  webcontent.ConsoleErrorMessages(result.ConsoleMessages),
		PageErrors:     result.PageErrors,
		ScreenshotPath: result.ScreenshotPath,
	}, nil
}

// runPageCheck executes one page check (SP-149 §149a/§149.3): start the
// manifest's dev server, open each listed route headless, and fail on a
// status-probe error, a browser-open error, or any console/page error.
//
// Skipped checks (no manifest, no dev command, no dev port, no routes, no
// browser, or an unavailable browser) are recorded with a reason and never
// gate the result (SP-149 §149d: the result says what could not be
// verified). A failed check is only ever one that started the server and
// found a concrete problem. The dev server is always stopped (the caller
// defers it), even on failure or cancellation.
//
// Routes come only from the starter manifest (SP-149 §149b): the plan's
// acceptance Check fields are never read for a route or a command.
func (r *Runner) runPageCheck(ctx context.Context, root string, c *Check, manifest *startermanifest.StarterManifest) {
	switch {
	case manifest == nil:
		c.Skipped = true
		c.Reason = "no starter manifest: a page check needs the manifest's dev command, port, and routes"
		return
	case manifestDev(manifest) == "":
		c.Skipped = true
		c.Reason = "no dev command in the starter manifest"
		return
	case manifest.DevPort == 0:
		c.Skipped = true
		c.Reason = "no dev port in the starter manifest (dev_port is 0; runtime port discovery is SP-155)"
		return
	case len(manifest.Routes) == 0:
		c.Skipped = true
		c.Reason = "no routes in the starter manifest"
		return
	case r.Browser == nil:
		c.Skipped = true
		c.Reason = "no browser configured"
		return
	}

	routes := manifest.Routes
	c.Routes = append([]string{}, routes...)
	c.Screenshots = make([]string, len(routes))

	timeout := r.DevServerTimeout
	if timeout <= 0 {
		timeout = DefaultDevServerTimeout
	}
	devServer, reason := startDevServer(ctx, root, manifestDev(manifest), manifest.DevPort, timeout)
	defer stopDevServer(devServer)
	if reason != "" {
		c.Passed = false
		c.Reason = reason
		c.Excerpt = boundedExcerpt(devServer.output(), r.MaxExcerptBytes)
		return
	}

	screenshotDir := r.screenshotDir(root)
	if err := os.MkdirAll(screenshotDir, 0o755); err != nil {
		c.Passed = false
		c.Reason = "create screenshot directory: " + err.Error()
		return
	}

	var failures []routeFailure
	for i, route := range routes {
		if ctx.Err() != nil {
			c.Skipped = true
			c.Reason = "verification run cancelled before this check"
			return
		}
		pageURL := routeURL(manifest.DevPort, route)

		status, ok := probeRouteStatus(ctx, pageURL)
		if !ok {
			failures = append(failures, routeFailure{route: route, detail: "status probe failed (no HTTP response)"})
			continue
		}
		if status >= 400 {
			failures = append(failures, routeFailure{route: route, detail: "HTTP " + strconv.Itoa(status)})
			continue
		}

		shotPath := filepath.Join(screenshotDir, routeSlug(route)+"-"+strconv.Itoa(i)+".png")
		obs, err := r.Browser.Open(ctx, pageURL, shotPath)
		if err != nil {
			if errors.Is(err, webcontent.ErrBrowserUnavailable) {
				c.Skipped = true
				c.Reason = "browser rendering not available"
				return
			}
			failures = append(failures, routeFailure{route: route, detail: "browser open failed: " + err.Error()})
			continue
		}
		c.Screenshots[i] = obs.ScreenshotPath

		if len(obs.ConsoleErrors) > 0 || len(obs.PageErrors) > 0 {
			failures = append(failures, routeFailure{route: route, consoleErrors: obs.ConsoleErrors, pageErrors: obs.PageErrors})
		}
	}

	if len(failures) > 0 {
		c.Passed = false
		c.Reason, c.Excerpt = summarizeRouteFailures(failures, r.MaxExcerptBytes)
		return
	}
	c.Passed = true
}

// routeFailure is one failing route in a page check, aggregated for the
// check's reason (first few messages) and excerpt (full detail).
type routeFailure struct {
	route         string
	detail        string
	consoleErrors []string
	pageErrors    []string
}

// summarizeRouteFailures turns the per-route failures into the check's
// Reason (first few messages per route) and Excerpt (full detail), bounded
// by maxBytes.
func summarizeRouteFailures(failures []routeFailure, maxBytes int) (reason, excerpt string) {
	var reasonParts []string
	var excerptLines []string
	for _, f := range failures {
		var short string
		if f.detail != "" {
			short = f.detail
		} else {
			short = firstFewMessages(append(append([]string{}, f.consoleErrors...), f.pageErrors...))
		}
		reasonParts = append(reasonParts, f.route+": "+short)

		var lines []string
		for _, m := range f.consoleErrors {
			lines = append(lines, f.route+" console: "+m)
		}
		for _, m := range f.pageErrors {
			lines = append(lines, f.route+" page: "+m)
		}
		if len(lines) == 0 {
			lines = append(lines, f.route+": "+f.detail)
		}
		excerptLines = append(excerptLines, strings.Join(lines, "\n"))
	}
	reason = strings.Join(reasonParts, "; ")
	excerpt = boundedExcerpt(strings.Join(excerptLines, "\n"), maxBytes)
	return reason, excerpt
}

// firstFewMessages joins at most three messages with "; " for a check's
// Reason line (the full list goes in the excerpt).
func firstFewMessages(messages []string) string {
	if len(messages) == 0 {
		return ""
	}
	if len(messages) > 3 {
		return strings.Join(messages[:3], "; ") + " (+ more, see excerpt)"
	}
	return strings.Join(messages, "; ")
}

// routeURL resolves a manifest route entry to an absolute URL. Absolute
// http(s) URLs are used as-is; anything else is joined onto the dev server's
// base URL (http://127.0.0.1:<port>).
func routeURL(port int, route string) string {
	if strings.HasPrefix(route, "http://") || strings.HasPrefix(route, "https://") {
		return route
	}
	if !strings.HasPrefix(route, "/") {
		route = "/" + route
	}
	return fmt.Sprintf("http://127.0.0.1:%d%s", port, route)
}

// routeSlug turns a route path into a screenshot file stem: non-alphanumeric
// runs collapse to "-", and an empty result (the root "/") becomes "root".
func routeSlug(route string) string {
	var b strings.Builder
	prevAlnum := false
	for _, r := range route {
		switch {
		case (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			b.WriteRune(r)
			prevAlnum = true
		default:
			if prevAlnum {
				b.WriteByte('-')
			}
			prevAlnum = false
		}
	}
	if b.Len() == 0 {
		return "root"
	}
	return b.String()
}

// probeRouteStatus does a plain GET (following redirects) and returns the
// final status code plus whether the request reached a server. ok is false
// on a transport error (e.g. connection refused), in which case status is 0.
func probeRouteStatus(ctx context.Context, url string) (status int, ok bool) {
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, false
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, false
	}
	status = resp.StatusCode
	_ = resp.Body.Close()
	return status, true
}

// manifestDev returns the manifest's dev command (trimmed), or "" when there
// is no manifest or no dev command.
func manifestDev(m *startermanifest.StarterManifest) string {
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m.Dev)
}

// screenshotDir returns the directory page-check screenshots are written to:
// Runner.ScreenshotDir when set, otherwise <root>/.sprout/verify/screenshots
// (the .sprout/ directory is gitignored project state).
func (r *Runner) screenshotDir(root string) string {
	if r.ScreenshotDir != "" {
		return r.ScreenshotDir
	}
	return filepath.Join(root, ".sprout", "verify", "screenshots")
}
