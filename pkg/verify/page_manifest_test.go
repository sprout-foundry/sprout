package verify

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/startermanifest"
)

// ---------------------------------------------------------------------------
// Manifest / plan / baseline behavior for page checks (SP-149 §149b): a page
// check's command and routes come only from the starter manifest, the plan's
// model-proposed Check fields are inert, and a baseline run never produces a
// page check.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// (e) a route that 404s → failed via the status probe, no browser needed.
// ---------------------------------------------------------------------------

func TestPageCheckRoute404FailsViaProbe(t *testing.T) {
	nodeAvailable(t)
	root, _ := writeFixtureApp(t, false, "/missing") // the fixture 404s /missing
	writePlanFile(t, root, pageOnlyPlan(t, "a1"))

	r := newPageRunner()
	browser := &scriptedPageBrowser{fixtureDir: root}
	r.Browser = browser

	res, err := r.Run(context.Background(), root)
	require.NoError(t, err)

	page := findPageCheck(t, res)
	assert.True(t, res.Failed())
	assert.False(t, page.Passed)
	assert.Contains(t, page.Reason, "HTTP 404", "the 404 must surface via the status probe")
	assert.Empty(t, browser.opened, "a 404 route must not reach the browser")
}

// ---------------------------------------------------------------------------
// (f) a plan with page items + a manifest declaring dev/port/routes → exactly
//     one page check, Items = page item ids, Command = the manifest's dev
//     command, Routes = the manifest's routes.
// ---------------------------------------------------------------------------

// TestPageCheckPlanConstruction pins the check construction without running a
// dev server: planChecks emits exactly one page check carrying the page item
// ids and the manifest's dev command.
func TestPageCheckPlanConstruction(t *testing.T) {
	manifest := startermanifest.New("fixture", "0.0.1")
	manifest.Dev = "node server.js 43210"
	manifest.DevPort = 43210
	manifest.Routes = []string{"/login", "/home"}

	r := New()
	checks := r.planChecks(pageOnlyPlan(t, "p1", "p2"), manifest, "make build", "make test")

	require.Len(t, checks, 1, "exactly one page check for the plan's page items")
	assert.Equal(t, plancontract.KindPage, checks[0].Kind)
	assert.Equal(t, []string{"p1", "p2"}, checks[0].Items)
	assert.Equal(t, "node server.js 43210", checks[0].Command)
}

// TestPageCheckRoutesFromManifest pins that a page check's Routes and Command
// come from the manifest on a real run (the dev server starts and the routes
// are opened in manifest order).
func TestPageCheckRoutesFromManifest(t *testing.T) {
	nodeAvailable(t)
	root, port := writeFixtureApp(t, false, "/login", "/home")
	writePlanFile(t, root, pageOnlyPlan(t, "p1", "p2"))

	r := newPageRunner()
	r.Browser = &scriptedPageBrowser{fixtureDir: root}

	res, err := r.Run(context.Background(), root)
	require.NoError(t, err)

	page := findPageCheck(t, res)
	assert.True(t, res.Passed())
	require.Equal(t, []string{"/login", "/home"}, page.Routes, "Routes come only from the manifest")
	assert.Equal(t, "node server.js "+strconv.Itoa(port), page.Command, "the check's command is the manifest's dev command")
	require.Len(t, page.Screenshots, 2, "one screenshot slot per route")
	assertPortClosed(t, port)
}

// ---------------------------------------------------------------------------
// (g) baseline (no plan) never produces a page check, even when the manifest
//     declares dev/port/routes.
// ---------------------------------------------------------------------------

func TestBaselineNeverProducesPageCheck(t *testing.T) {
	root, _ := writeFixtureApp(t, false, "/login") // dev/port/routes present, no plan
	r := newPageRunner()
	r.Browser = &scriptedPageBrowser{fixtureDir: root}

	res, err := r.Run(context.Background(), root)
	require.NoError(t, err)

	assert.True(t, res.Baseline, "no plan on disk means a baseline run")
	require.Len(t, res.Checks, 2, "a baseline run is build + test only")
	for _, c := range res.Checks {
		assert.NotEqual(t, plancontract.KindPage, c.Kind, "a baseline run never produces a page check")
	}
	assert.Empty(t, openedURLs(r), "the browser must not be used in a baseline run")
}

// ---------------------------------------------------------------------------
// (h) the plan's model-proposed page Check fields have NO effect: routes come
//     only from the manifest (§149b).
// ---------------------------------------------------------------------------

func TestPageCheckModelProposedRouteHasNoEffect(t *testing.T) {
	nodeAvailable(t)
	root, port := writeFixtureApp(t, false, "/login")
	p := pageOnlyPlan(t, "a1")
	p.Acceptance[0].Check = "http://evil.example/x" // model-proposed; must be ignored
	writePlanFile(t, root, p)

	r := newPageRunner()
	r.Browser = &scriptedPageBrowser{fixtureDir: root}

	res, err := r.Run(context.Background(), root)
	require.NoError(t, err)

	page := findPageCheck(t, res)
	assert.Equal(t, []string{"/login"}, page.Routes, "routes come only from the manifest, never the plan")
	assert.Equal(t, "node server.js "+strconv.Itoa(port), page.Command)
	for _, opened := range openedURLs(r) {
		assert.NotContains(t, opened, "evil.example", "the model-proposed route must not be opened")
	}
}
