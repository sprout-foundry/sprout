//go:build !js

package webcontent

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The eval step must run ordinary JavaScript. rod wraps whatever it is given
// as `function() { return (<js>).apply(this, arguments) }`, so a statement, an
// IIFE, or a plain expression all failed with a TypeError naming `.apply` —
// an error about rod's wrapping that read like a bug in the caller's script.
// scriptAsStatement rewrites the script so all three shapes work; these tests
// pin that contract against a real page.

// skipWithoutEvalBrowser skips when no headless browser is reachable, unless
// SPROUT_REQUIRE_BROWSER=1 is set — the same contract the design render tests
// use, so CI asserts the path ran instead of hiding behind a green skip.
func skipWithoutEvalBrowser(t *testing.T, err error) {
	t.Helper()
	if os.Getenv("SPROUT_REQUIRE_BROWSER") != "" {
		t.Fatalf("no usable headless browser, but SPROUT_REQUIRE_BROWSER is set: %v", err)
	}
	t.Skipf("no usable headless browser in this environment: %v", err)
}

func newEvalTestPage(t *testing.T) *rod.Page {
	t.Helper()
	l := launcher.New().Headless(true).NoSandbox(true)
	for _, bin := range systemBrowserPaths() {
		if _, statErr := os.Stat(bin); statErr == nil {
			l = l.Bin(bin)
			break
		}
	}
	u, err := l.Launch()
	if err != nil {
		skipWithoutEvalBrowser(t, err)
	}
	browser := rod.New().ControlURL(u)
	if err := browser.Connect(); err != nil {
		_ = browser.Close()
		skipWithoutEvalBrowser(t, err)
	}
	t.Cleanup(func() { _ = browser.Close() })

	page, err := browser.Page(proto.TargetCreateTarget{})
	if err != nil {
		skipWithoutEvalBrowser(t, err)
	}
	t.Cleanup(func() { _ = page.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	page = page.Context(ctx)
	require.NoError(t, page.Navigate("data:text/html,<html><body><div id=\"x\">initial</div></body></html>"))
	require.NoError(t, page.WaitLoad())
	return page
}

func TestScriptAsStatement_WrapsUserScript(t *testing.T) {
	// The wrapper must be a function expression (rod calls it) and must
	// evaluate the script as a statement via indirect eval.
	got := scriptAsStatement(`document.title`)
	assert.True(t, strings.HasPrefix(got, "() =>"), "wrapper must be a function expression: %s", got)
	assert.Contains(t, got, `(0, eval)("document.title")`)
}

func TestBrowseStepEval_RunsStatementsExpressionsAndArrowFunctions(t *testing.T) {
	page := newEvalTestPage(t)

	cases := []struct {
		name   string
		script string
		want   string
		// wantValue asserts the script's return value; false for scripts
		// whose effect is the point (a function value serialises as {}).
		wantValue bool
	}{
		{
			name:      "statement",
			script:    `document.getElementById('x').textContent = 'from-statement'`,
			want:      "from-statement",
			wantValue: true,
		},
		{
			name:      "expression",
			script:    `document.getElementById('x').textContent`,
			want:      "from-statement",
			wantValue: true,
		},
		{
			name: "arrow function",
			// A script that is itself a function is a VALUE: the step
			// returns it rather than calling it, so it has no side effect.
			// What matters is that it does not throw rod's `.apply` TypeError.
			script: `() => document.getElementById('x').textContent`,
			want:   "from-statement",
		},
		{
			name:      "iife",
			script:    `(() => { document.getElementById('x').textContent = 'from-iife'; return 'from-iife' })()`,
			want:      "from-iife",
			wantValue: true,
		},
		{
			name:      "multi-line statement block",
			script:    "const el = document.getElementById('x');\nel.textContent = 'multi';\nel.textContent",
			want:      "multi",
			wantValue: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var recorded []string
			result := &BrowseResult{}
			err := browseStepEval(page, BrowseStep{Action: "eval", Script: tc.script},
				defaultWaitTimeout, func(s string) { recorded = append(recorded, s) }, result)
			require.NoError(t, err, "script %q must run", tc.script)

			require.Len(t, result.EvalResults, 1)
			assert.Empty(t, result.EvalResults[0].Error)
			// The script's effect must have happened either way.
			text, textErr := page.Eval(scriptAsStatement(`document.getElementById('x').textContent`))
			require.NoError(t, textErr)
			assert.Equal(t, tc.want, text.Value.Str(), "script %q must take effect", tc.script)
			if tc.wantValue {
				assert.Contains(t, result.EvalResults[0].Value, tc.want,
					"script %q should return %q", tc.script, tc.want)
			}
			assert.Equal(t, []string{"eval"}, recorded)
		})
	}
}

func TestBrowseStepEval_ReturnsArrowFunctionValue(t *testing.T) {
	page := newEvalTestPage(t)

	// A script that is itself a function is a value: the step returns it
	// rather than calling it, so `() => 1 + 1` yields the source text.
	var recorded []string
	result := &BrowseResult{}
	err := browseStepEval(page, BrowseStep{Action: "eval", Script: `() => document.getElementById('x').textContent`},
		defaultWaitTimeout, func(s string) { recorded = append(recorded, s) }, result)
	require.NoError(t, err)
	require.Len(t, result.EvalResults, 1)
	assert.Empty(t, result.EvalResults[0].Error)
	// rod serialises a function value as an empty object; the point is that
	// the call succeeds and does not throw rod's `.apply` TypeError.
	assert.NotContains(t, result.EvalResults[0].Value, "apply")
}

func TestBrowseStepEval_ReportsScriptErrorWithoutApplyNoise(t *testing.T) {
	page := newEvalTestPage(t)

	result := &BrowseResult{}
	err := browseStepEval(page, BrowseStep{Action: "eval", Script: `throw new Error('boom')`},
		defaultWaitTimeout, func(string) {}, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
	assert.NotContains(t, err.Error(), ".apply",
		"the error must name the script's failure, not rod's wrapping")
}

func TestBrowseStepWaitForFunction_AcceptsStatement(t *testing.T) {
	page := newEvalTestPage(t)

	err := browseStepWaitForFunction(page,
		BrowseStep{Action: "wait_for_function", Script: `document.getElementById('x') !== null`},
		5*time.Second, func(string) {}, &BrowseResult{})
	require.NoError(t, err)
}
