package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/webcontent"
)

// ---------------------------------------------------------------------------
// Fake PageBrowser driven by a fixture app
// ---------------------------------------------------------------------------

// consoleErrorRe extracts console.error("...") literals from an HTML file so
// the fake browser's console-error report comes from the fixture app itself
// (the acceptance rule: "a page check catches a route that renders with a
// console error").
var consoleErrorRe = regexp.MustCompile(`console\.error\(\s*["']([^"']*)["']`)

func extractConsoleErrors(html string) []string {
	var out []string
	for _, m := range consoleErrorRe.FindAllStringSubmatch(html, -1) {
		out = append(out, m[1])
	}
	return out
}

// scriptedPageBrowser is a deterministic PageBrowser for tests: it returns
// canned observations/errors per URL, or (when fixtureDir is set) drives the
// observation from the fixture app's index.html (extracting its console.error
// literals). It writes a tiny file to the requested screenshot path so the
// recorded screenshot references exist on disk.
type scriptedPageBrowser struct {
	fixtureDir  string
	byURL       map[string]*PageObservation
	byURLErr    map[string]error
	unavailable bool
	opened      []string
}

// Open implements PageBrowser.
func (s *scriptedPageBrowser) Open(_ context.Context, url string, screenshotPath string) (*PageObservation, error) {
	s.opened = append(s.opened, url)
	if s.unavailable {
		return nil, webcontent.ErrBrowserUnavailable
	}
	if e, ok := s.byURLErr[url]; ok {
		return nil, e
	}
	if obs, ok := s.byURL[url]; ok {
		if screenshotPath != "" {
			s.writeShot(screenshotPath)
			obs.ScreenshotPath = screenshotPath
		}
		return obs, nil
	}
	obs, err := s.observeFromFixture(url)
	if err != nil {
		return nil, err
	}
	if screenshotPath != "" {
		s.writeShot(screenshotPath)
		obs.ScreenshotPath = screenshotPath
	}
	return obs, nil
}

func (s *scriptedPageBrowser) writeShot(path string) {
	if path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte("fake-screenshot"), 0o644)
}

// observeFromFixture reports the fixture app's console errors for the route.
// The fixture is a single-page app (the server serves index.html for every
// route), so the observation is driven by index.html regardless of route.
func (s *scriptedPageBrowser) observeFromFixture(url string) (*PageObservation, error) {
	data, err := os.ReadFile(filepath.Join(s.fixtureDir, "index.html"))
	if err != nil {
		return nil, err
	}
	return &PageObservation{
		URL:           url,
		ConsoleErrors: extractConsoleErrors(string(data)),
	}, nil
}

// ---------------------------------------------------------------------------
// Fixture app (a dependency-free Node static server)
// ---------------------------------------------------------------------------

// serverJS is a dependency-free Node static server: it serves index.html with
// 200 for every route except "/missing" (404, so a failing route can be
// exercised). The port is taken from argv[2].
const serverJS = `const http = require('http');
const fs = require('fs');
const path = require('path');
const port = parseInt(process.argv[2] || '43210', 10);
const server = http.createServer((req, res) => {
  const p = req.url.split('?')[0];
  if (p === '/missing') {
    res.writeHead(404);
    res.end('not found');
    return;
  }
  fs.readFile(path.join(__dirname, 'index.html'), (err, data) => {
    if (err) { res.writeHead(500); res.end('no index'); return; }
    res.writeHead(200, { 'Content-Type': 'text/html' });
    res.end(data);
  });
});
server.listen(port, '127.0.0.1', () => {
  console.log('fixture server listening on ' + port);
});
`

const (
	brokenFixtureHTML = `<!DOCTYPE html><html><head><title>Fixture</title></head>` +
		`<body><h1>Fixture</h1><script>console.error("fixture console error")</script></body></html>`
	cleanFixtureHTML = `<!DOCTYPE html><html><head><title>Fixture</title></head>` +
		`<body><h1>Fixture</h1><script>console.log("all good")</script></body></html>`
)

// freePort returns a currently-free port on 127.0.0.1 for the fixture server
// to bind.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// nodeAvailable skips tests that need a Node dev server when node is not on
// PATH (mirrors the shAvailable pattern in executor_test.go).
func nodeAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available on this platform; skipping fixture-server tests")
	}
}

// writeFixtureApp writes a fixture app (index.html broken or clean, a
// dependency-free Node server, and a starter manifest declaring the dev
// command, port, and routes) into a fresh temp dir. It returns the root and
// the port the dev server will bind.
func writeFixtureApp(t *testing.T, broken bool, routes ...string) (string, int) {
	t.Helper()
	if len(routes) == 0 {
		routes = []string{"/"}
	}
	root := t.TempDir()
	if broken {
		require.NoError(t, os.WriteFile(filepath.Join(root, "index.html"), []byte(brokenFixtureHTML), 0o644))
	} else {
		require.NoError(t, os.WriteFile(filepath.Join(root, "index.html"), []byte(cleanFixtureHTML), 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "server.js"), []byte(serverJS), 0o644))

	port := freePort(t)
	routesJSON, err := json.Marshal(routes)
	require.NoError(t, err)
	manifest := fmt.Sprintf(
		`{"starter":{"id":"fixture","version":"0.0.1"},"dev":"node server.js %d","dev_port":%d,"routes":%s}`,
		port, port, string(routesJSON),
	)
	writeManifestFile(t, root, manifest)
	return root, port
}

// assertPortClosed asserts nothing is listening on the dev port (the dev
// server was stopped after the check).
func assertPortClosed(t *testing.T, port int) {
	t.Helper()
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
	if err == nil {
		_ = resp.Body.Close()
		t.Errorf("dev server still responding on port %d after the check", port)
	}
}

// pageOnlyPlan returns a plan whose acceptance items are all page checks.
func pageOnlyPlan(t *testing.T, itemIDs ...string) *plancontract.Plan {
	t.Helper()
	if len(itemIDs) == 0 {
		itemIDs = []string{"a1"}
	}
	p := plancontract.New("Render routes", time.Now())
	p.Scope = []plancontract.ScopeItem{{ID: "s1", Title: "Routes"}}
	p.Steps = []plancontract.Step{{Scope: "s1", Description: "Render"}}
	acceptance := make([]plancontract.Acceptance, 0, len(itemIDs))
	for _, id := range itemIDs {
		acceptance = append(acceptance, plancontract.Acceptance{ID: id, Scope: "s1", Check: "route renders", Kind: plancontract.KindPage})
	}
	p.Acceptance = acceptance
	return p
}

// findPageCheck returns the page check in a result, failing the test if there
// is none.
func findPageCheck(t *testing.T, res *Result) *Check {
	t.Helper()
	for i := range res.Checks {
		if res.Checks[i].Kind == plancontract.KindPage {
			return &res.Checks[i]
		}
	}
	require.FailNowf(t, "no page check in result", "summary: %s", res.Summary())
	return nil
}

// newPageRunner returns a Runner wired for page-check tests: the fake
// executor (no command execution) and a generous dev-server timeout.
func newPageRunner() *Runner {
	r := New()
	r.Exec = &fakeExecutor{}
	r.DevServerTimeout = 15 * time.Second
	return r
}

// openedURLs returns the URLs a scripted browser was asked to open.
func openedURLs(r *Runner) []string {
	if b, ok := r.Browser.(*scriptedPageBrowser); ok {
		return b.opened
	}
	return nil
}

// ---------------------------------------------------------------------------
// (a) broken fixture app → page check fails, names the console error,
//     records a screenshot, and the dev server is stopped afterwards.
// ---------------------------------------------------------------------------

func TestPageCheckBrokenFixtureFails(t *testing.T) {
	nodeAvailable(t)
	root, port := writeFixtureApp(t, true, "/")
	writePlanFile(t, root, pageOnlyPlan(t, "a1"))

	r := newPageRunner()
	r.Browser = &scriptedPageBrowser{fixtureDir: root}

	res, err := r.Run(context.Background(), root)
	require.NoError(t, err)

	page := findPageCheck(t, res)
	assert.True(t, res.Failed(), "a route with a console error must fail the run")
	assert.False(t, page.Passed)
	assert.False(t, page.Skipped)
	assert.Contains(t, page.Reason, "fixture console error", "the reason must name the console error")
	assert.Contains(t, page.Excerpt, "fixture console error", "the excerpt must carry the full detail")
	require.Equal(t, []string{"/"}, page.Routes)
	require.Len(t, page.Screenshots, 1)
	require.NotEmpty(t, page.Screenshots[0], "the screenshot reference must be recorded")
	info, statErr := os.Stat(page.Screenshots[0])
	require.NoError(t, statErr, "the recorded screenshot must exist on disk")
	assert.Greater(t, info.Size(), int64(0))
	assertPortClosed(t, port)
}

// ---------------------------------------------------------------------------
// (b) clean fixture app → page check passes and records a screenshot.
// ---------------------------------------------------------------------------

func TestPageCheckCleanFixturePasses(t *testing.T) {
	nodeAvailable(t)
	root, port := writeFixtureApp(t, false, "/")
	writePlanFile(t, root, pageOnlyPlan(t, "a1"))

	r := newPageRunner()
	r.Browser = &scriptedPageBrowser{fixtureDir: root}

	res, err := r.Run(context.Background(), root)
	require.NoError(t, err)

	page := findPageCheck(t, res)
	assert.True(t, res.Passed(), "a clean route must pass")
	assert.True(t, page.Passed)
	assert.Empty(t, page.Reason)
	require.Equal(t, []string{"/"}, page.Routes)
	require.Len(t, page.Screenshots, 1)
	require.NotEmpty(t, page.Screenshots[0])
	info, statErr := os.Stat(page.Screenshots[0])
	require.NoError(t, statErr)
	assert.Greater(t, info.Size(), int64(0))
	assertPortClosed(t, port)
}

// ---------------------------------------------------------------------------
// (c) dev command that exits immediately → failed check with server output in
//     the excerpt (no Node needed: a plain shell command).
// ---------------------------------------------------------------------------

func TestPageCheckDevCommandExitsEarly(t *testing.T) {
	shAvailable(t)
	root := t.TempDir()
	port := freePort(t)
	manifest := fmt.Sprintf(
		`{"starter":{"id":"fixture","version":"0.0.1"},"dev":"echo dev-server-failed; exit 1","dev_port":%d,"routes":["/"]}`,
		port,
	)
	writeManifestFile(t, root, manifest)
	writePlanFile(t, root, pageOnlyPlan(t, "a1"))

	r := newPageRunner()
	r.Browser = &scriptedPageBrowser{}
	r.DevServerTimeout = 10 * time.Second

	res, err := r.Run(context.Background(), root)
	require.NoError(t, err)

	page := findPageCheck(t, res)
	assert.True(t, res.Failed())
	assert.False(t, page.Passed)
	assert.False(t, page.Skipped)
	assert.Contains(t, page.Reason, "exited before")
	assert.Contains(t, page.Excerpt, "dev-server-failed", "the server output must be in the excerpt")
}

// ---------------------------------------------------------------------------
// (d) skip reasons: no dev command, dev_port 0, no routes, Browser==nil, and
//     an unavailable browser.
// ---------------------------------------------------------------------------

func TestPageCheckSkipReasons(t *testing.T) {
	t.Run("no dev command", func(t *testing.T) {
		root := t.TempDir()
		writeManifestFile(t, root, manifestBuildOnly) // build only: no dev
		writePlanFile(t, root, pageOnlyPlan(t, "a1"))
		r := newPageRunner()
		res, err := r.Run(context.Background(), root)
		require.NoError(t, err)
		page := findPageCheck(t, res)
		assert.True(t, page.Skipped)
		assert.Contains(t, page.Reason, "dev command")
		assert.False(t, res.Failed(), "a skipped check must not gate the run")
	})

	t.Run("no dev port", func(t *testing.T) {
		root := t.TempDir()
		manifest := `{"starter":{"id":"fixture","version":"0.0.1"},"dev":"node server.js","routes":["/"]}`
		writeManifestFile(t, root, manifest) // dev set, dev_port absent (0)
		writePlanFile(t, root, pageOnlyPlan(t, "a1"))
		r := newPageRunner()
		res, err := r.Run(context.Background(), root)
		require.NoError(t, err)
		page := findPageCheck(t, res)
		assert.True(t, page.Skipped)
		assert.Contains(t, page.Reason, "dev port")
	})

	t.Run("no routes", func(t *testing.T) {
		root := t.TempDir()
		manifest := `{"starter":{"id":"fixture","version":"0.0.1"},"dev":"node server.js","dev_port":43210}`
		writeManifestFile(t, root, manifest) // no routes
		writePlanFile(t, root, pageOnlyPlan(t, "a1"))
		r := newPageRunner()
		res, err := r.Run(context.Background(), root)
		require.NoError(t, err)
		page := findPageCheck(t, res)
		assert.True(t, page.Skipped)
		assert.Contains(t, page.Reason, "routes")
	})

	t.Run("no browser", func(t *testing.T) {
		root, _ := writeFixtureApp(t, false, "/") // dev/port/routes present
		writePlanFile(t, root, pageOnlyPlan(t, "a1"))
		r := newPageRunner()
		r.Browser = nil
		res, err := r.Run(context.Background(), root)
		require.NoError(t, err)
		page := findPageCheck(t, res)
		assert.True(t, page.Skipped)
		assert.Contains(t, page.Reason, "no browser configured")
	})

	t.Run("browser unavailable", func(t *testing.T) {
		nodeAvailable(t)
		root, _ := writeFixtureApp(t, false, "/")
		writePlanFile(t, root, pageOnlyPlan(t, "a1"))
		r := newPageRunner()
		r.Browser = &scriptedPageBrowser{unavailable: true}
		res, err := r.Run(context.Background(), root)
		require.NoError(t, err)
		page := findPageCheck(t, res)
		assert.True(t, page.Skipped, "an unavailable browser is a skip, not a failure")
		assert.Contains(t, page.Reason, "browser rendering not available")
		assert.False(t, res.Failed(), "a skipped check must not gate the run")
	})
}
