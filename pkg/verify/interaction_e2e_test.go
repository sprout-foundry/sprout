//go:build browser

package verify

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
)

// interactiveFixtureHTML is a fixture app with a toggle button that reveals a
// welcome message: clicking #toggle makes #welcome (initially hidden) visible,
// so a scripted "click then assert" flow reaches a real expected outcome.
const interactiveFixtureHTML = `<!DOCTYPE html>
<html><head><title>Fixture</title></head>
<body>
<h1>Fixture</h1>
<button id="toggle">Toggle</button>
<div id="welcome" style="display:none">Welcome back</div>
<script>
  document.getElementById('toggle').addEventListener('click', function () {
    document.getElementById('welcome').style.display = 'block';
  });
</script>
</body></html>
`

// writeInteractiveFixtureApp writes an interactive fixture app (the
// interactive index.html, the dependency-free Node server, and a starter
// manifest declaring the dev command and port) into a fresh temp dir. It
// returns the root and the port the dev server will bind.
func writeInteractiveFixtureApp(t *testing.T) (string, int) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "index.html"), []byte(interactiveFixtureHTML), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "server.js"), []byte(serverJS), 0o644))
	port := freePort(t)
	manifest := fmt.Sprintf(
		`{"starter":{"id":"fixture","version":"0.0.1"},"dev":"node server.js %d","dev_port":%d,"routes":["/"]}`,
		port, port,
	)
	writeManifestFile(t, root, manifest)
	return root, port
}

// TestInteractionCheckRealBrowser_StepsPass runs the production step browser
// (webcontent.GetGlobalBrowser) against the interactive fixture dev server: a
// scripted "click then assert" flow reaches the expected outcome, the check
// passes with evidence, and the dev server is stopped afterwards.
func TestInteractionCheckRealBrowser_StepsPass(t *testing.T) {
	skipWithoutBrowser(t)
	nodeAvailable(t)
	root, port := writeInteractiveFixtureApp(t)
	steps := []plancontract.BrowseStep{
		{Action: "click", Selector: "#toggle"},
		{Action: "assert_text", Selector: "#welcome", Expect: "Welcome back"},
	}
	writePlanFile(t, root, interactionOnlyPlan(t, interactionItem(t, "a1", steps...)))

	r := New()
	r.Exec = &fakeExecutor{}
	r.StepBrowser = NewWebcontentStepBrowser()
	r.DevServerTimeout = 30 * time.Second

	res, err := r.Run(context.Background(), root)
	require.NoError(t, err)

	c := findInteractionCheck(t, res, "a1")
	assert.True(t, res.Passed(), "a passing scripted flow must pass with the real browser")
	assert.True(t, c.Passed)
	assert.NotEmpty(t, c.Excerpt, "the passing check must carry the executed-action evidence")
	assertPortClosed(t, port)
}

// TestInteractionCheckRealBrowser_FailingAssert pins the honest-failure path:
// a scripted flow whose expected outcome is wrong fails the
// check, the reason names the item and the failing step, and the dev server
// is stopped afterwards.
func TestInteractionCheckRealBrowser_FailingAssert(t *testing.T) {
	skipWithoutBrowser(t)
	nodeAvailable(t)
	root, port := writeInteractiveFixtureApp(t)
	steps := []plancontract.BrowseStep{
		{Action: "click", Selector: "#toggle"},
		{Action: "assert_text", Selector: "#welcome", Expect: "Some other text"},
	}
	writePlanFile(t, root, interactionOnlyPlan(t, interactionItem(t, "a1", steps...)))

	r := New()
	r.Exec = &fakeExecutor{}
	r.StepBrowser = NewWebcontentStepBrowser()
	r.DevServerTimeout = 30 * time.Second

	res, err := r.Run(context.Background(), root)
	require.NoError(t, err)

	c := findInteractionCheck(t, res, "a1")
	assert.True(t, res.Failed(), "a wrong expected outcome must fail the check")
	assert.False(t, c.Passed)
	assert.Contains(t, c.Reason, "a1", "the reason must name the item")
	assert.Contains(t, c.Reason, "step[1]", "the reason must name the failing step")
	assertPortClosed(t, port)
}
