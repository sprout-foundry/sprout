//go:build browser

package verify

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
)

// skipWithoutBrowser skips the real-browser tests unless SPROUT_TEST_BROWSER=1
// is set (mirrors pkg/webcontent/browser_rod_e2e_test.go).
func skipWithoutBrowser(t *testing.T) {
	if os.Getenv("SPROUT_TEST_BROWSER") == "" {
		t.Skip("skipping: set SPROUT_TEST_BROWSER=1 to run browser tests")
	}
}

// TestPageCheckRealBrowser_ConsoleErrorFails runs the production page browser
// (webcontent.GetGlobalBrowser) against a fixture dev server: a route that
// logs a console error fails the check, a screenshot is captured and is
// non-trivial, and the dev server is stopped afterwards.
func TestPageCheckRealBrowser_ConsoleErrorFails(t *testing.T) {
	skipWithoutBrowser(t)
	nodeAvailable(t)
	root, port := writeFixtureApp(t, true, "/")
	writePlanFile(t, root, pageOnlyPlan(t, "a1"))

	r := New()
	r.Exec = &fakeExecutor{}
	r.Browser = NewWebcontentPageBrowser()
	r.DevServerTimeout = 30 * time.Second

	res, err := r.Run(context.Background(), root)
	require.NoError(t, err)

	page := findPageCheck(t, res)
	assert.True(t, res.Failed(), "a route that logs a console error must fail the check")
	assert.False(t, page.Passed)
	assert.Contains(t, page.Reason, "fixture console error")
	require.Len(t, page.Screenshots, 1)
	require.NotEmpty(t, page.Screenshots[0], "a screenshot reference must be recorded")
	info, statErr := os.Stat(page.Screenshots[0])
	require.NoError(t, statErr, "the screenshot file must exist on disk")
	assert.Greater(t, info.Size(), int64(500), "the screenshot must be non-trivial")
	assertPortClosed(t, port)
}

// TestPageCheckRealBrowser_CleanPasses runs the production page browser
// against a clean fixture route and expects a passing check with a captured
// screenshot.
func TestPageCheckRealBrowser_CleanPasses(t *testing.T) {
	skipWithoutBrowser(t)
	nodeAvailable(t)
	root, _ := writeFixtureApp(t, false, "/")
	writePlanFile(t, root, pageOnlyPlan(t, "a1"))

	r := New()
	r.Exec = &fakeExecutor{}
	r.Browser = NewWebcontentPageBrowser()
	r.DevServerTimeout = 30 * time.Second

	res, err := r.Run(context.Background(), root)
	require.NoError(t, err)

	page := findPageCheck(t, res)
	assert.True(t, res.Passed(), "a clean route must pass with the real browser")
	assert.True(t, page.Passed)
	assert.Empty(t, page.Reason)
	require.Len(t, page.Screenshots, 1)
	require.NotEmpty(t, page.Screenshots[0])
	info, statErr := os.Stat(page.Screenshots[0])
	require.NoError(t, statErr)
	assert.Greater(t, info.Size(), int64(500), "the screenshot must be non-trivial")
}

// TestPageCheckRealBrowser_OnlyPageCheck pins that a page-only plan yields a
// single page check through the production runner (sanity for the real
// browser path).
func TestPageCheckRealBrowser_OnlyPageCheck(t *testing.T) {
	skipWithoutBrowser(t)
	nodeAvailable(t)
	root, _ := writeFixtureApp(t, false, "/")
	writePlanFile(t, root, pageOnlyPlan(t, "p1", "p2"))

	r := New()
	r.Exec = &fakeExecutor{}
	r.Browser = NewWebcontentPageBrowser()
	r.DevServerTimeout = 30 * time.Second

	res, err := r.Run(context.Background(), root)
	require.NoError(t, err)

	require.Len(t, res.Checks, 1)
	assert.Equal(t, plancontract.KindPage, res.Checks[0].Kind)
	assert.Equal(t, []string{"p1", "p2"}, res.Checks[0].Items)
}
