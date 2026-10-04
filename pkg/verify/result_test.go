package verify

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
)

// TestRunExcerptIsBounded pins the output-excerpt bound (SP-149 §149c):
// a verbose output is truncated to the configured size, keeping head and
// tail.
func TestRunExcerptIsBounded(t *testing.T) {
	root := t.TempDir()
	writeManifestFile(t, root, manifestFull)

	longOutput := strings.Repeat("x", 10000) + "\nFATAL: last line"
	exec := &fakeExecutor{results: map[string]Outcome{
		"make build": {Passed: true, Output: longOutput},
	}}
	r := New()
	r.Exec = exec
	r.MaxExcerptBytes = 512

	res, err := r.Run(context.Background(), root)
	require.NoError(t, err)

	excerpt := res.Checks[0].Excerpt
	assert.LessOrEqual(t, len(excerpt), 512+128, "an excerpt stays bounded even for verbose output")
	assert.True(t, strings.HasPrefix(excerpt, "xxx"), "the head of the output is kept")
	assert.True(t, strings.HasSuffix(excerpt, "FATAL: last line"), "the tail (where failures surface) is kept")
	assert.Contains(t, excerpt, "truncated")
}

// TestRunShortOutputUntruncated pins the other side of the bound.
func TestRunShortOutputUntruncated(t *testing.T) {
	root := t.TempDir()
	writeManifestFile(t, root, manifestFull)

	exec := &fakeExecutor{results: map[string]Outcome{
		"make build": {Passed: true, Output: "ok"},
	}}
	r := New()
	r.Exec = exec

	res, err := r.Run(context.Background(), root)
	require.NoError(t, err)
	assert.Equal(t, "ok", res.Checks[0].Excerpt)
}

// TestBoundedExcerptDirect pins the excerpt helper's edge cases: output
// at exactly the bound is unchanged, and a too-small bound falls back to
// the default.
func TestBoundedExcerptDirect(t *testing.T) {
	small := strings.Repeat("a", 64)
	assert.Equal(t, small, boundedExcerpt(small, 64), "output at the bound is unchanged")

	assert.Equal(t, strings.Repeat("a", 100), boundedExcerpt(strings.Repeat("a", 100), 0),
		"a non-positive bound falls back to the default")
	assert.Equal(t, strings.Repeat("a", 100), boundedExcerpt(strings.Repeat("a", 100), 4),
		"a bound below 32 bytes falls back to the default")

	big := strings.Repeat("b", 2048) + "\nERR: tail"
	excerpt := boundedExcerpt(big, 128)
	assert.True(t, strings.HasPrefix(excerpt, "bbb"))
	assert.True(t, strings.HasSuffix(excerpt, "ERR: tail"))
	assert.Contains(t, excerpt, "truncated")
}

// TestResultAggregates pins the pass/fail aggregate semantics: a run
// fails when any executed check failed or an error was recorded; it
// passes only when nothing failed and at least one check ran; an
// all-skipped run is neither (SP-149 §149d: vacuous success is not
// success).
func TestResultAggregates(t *testing.T) {
	// A single executed, passing check is a pass.
	assert.True(t, (&Result{Checks: []Check{
		{Kind: plancontract.KindBuild, Passed: true},
	}}).Passed())

	allSkipped := &Result{
		Baseline: true,
		Checks: []Check{
			{Kind: plancontract.KindBuild, Skipped: true},
			{Kind: plancontract.KindTest, Skipped: true},
		},
	}
	assert.False(t, allSkipped.Passed(), "an all-skipped run verified nothing")
	assert.False(t, allSkipped.Failed(), "skipping is not failing")

	failed := &Result{
		Checks: []Check{
			{Kind: plancontract.KindBuild, Passed: true},
			{Kind: plancontract.KindTest, Passed: false},
		},
	}
	assert.True(t, failed.Failed())
	assert.False(t, failed.Passed())

	broken := &Result{
		Checks: []Check{{Kind: plancontract.KindBuild, Passed: true}},
		Errors: []string{"starter manifest: invalid"},
	}
	assert.True(t, broken.Failed(), "recorded errors fail the run")

	empty := &Result{Checks: []Check{}}
	assert.False(t, empty.Passed())
	assert.False(t, empty.Failed())

	assert.False(t, (func() *Result { return nil })().Passed())
	assert.False(t, (func() *Result { return nil })().Failed())
}

// TestResultSummaryPageCheck pins the deterministic rendering of a page check
// in the final-reply summary (SP-149 §149d): a page check that ran reports
// its route count, a skipped one its reason, and screenshot references stay
// off the one-line summary (they are the structured evidence).
func TestResultSummaryPageCheck(t *testing.T) {
	passed := &Result{
		PlanRevision: 2,
		Checks: []Check{
			{
				Kind:        plancontract.KindPage,
				Items:       []string{"a1"},
				Command:     "node server.js",
				Passed:      true,
				Routes:      []string{"/", "/login"},
				Screenshots: []string{"a.png", "b.png"},
			},
		},
	}
	assert.Equal(
		t,
		"plan rev 2: page: passed (node server.js) [2 routes]",
		passed.Summary(),
	)

	single := &Result{
		Checks: []Check{{Kind: plancontract.KindPage, Command: "npm run dev", Passed: true, Routes: []string{"/"}}},
	}
	assert.Equal(t, "plan rev 0: page: passed (npm run dev) [1 route]", single.Summary())

	skipped := &Result{
		Checks: []Check{{Kind: plancontract.KindPage, Skipped: true, Reason: "no dev command in the starter manifest"}},
	}
	assert.Equal(
		t,
		"plan rev 0: page: skipped — no dev command in the starter manifest",
		skipped.Summary(),
	)

	failed := &Result{
		Checks: []Check{{
			Kind: plancontract.KindPage, Command: "node server.js",
			Passed: false, Routes: []string{"/"}, Reason: "/: fixture console error",
		}},
	}
	assert.Equal(
		t,
		"plan rev 0: page: failed (node server.js) [1 route] — /: fixture console error",
		failed.Summary(),
	)
}

// TestResultSummaryInteractionAndManual pins the deterministic rendering of
// interaction and manual checks in the final-reply summary (SP-149 §149d): a
// run interaction check reports its step count, and a manual check reports the
// manual items it lists.
func TestResultSummaryInteractionAndManual(t *testing.T) {
	interaction := &Result{
		PlanRevision: 2,
		Checks: []Check{
			{
				Kind:    plancontract.KindInteraction,
				Items:   []string{"a1"},
				Command: "node server.js",
				Passed:  true,
				Steps: []plancontract.BrowseStep{
					{Action: "click", Selector: "#toggle"},
					{Action: "assert_text", Expect: "Welcome back"},
				},
			},
		},
	}
	assert.Equal(
		t,
		"plan rev 2: interaction: passed (node server.js) [2 steps]",
		interaction.Summary(),
	)

	singleStep := &Result{
		Checks: []Check{{Kind: plancontract.KindInteraction, Command: "node server.js", Passed: true, Steps: []plancontract.BrowseStep{{Action: "assert_text", Expect: "Fixture"}}}},
	}
	assert.Equal(t, "plan rev 0: interaction: passed (node server.js) [1 step]", singleStep.Summary())

	interactionFailed := &Result{
		Checks: []Check{{
			Kind: plancontract.KindInteraction, Command: "node server.js",
			Passed: false, Steps: []plancontract.BrowseStep{{Action: "click", Selector: "#toggle"}, {Action: "assert_text", Expect: "X"}},
			Reason: `a1: step[1] assert_text: missing expected text "X"`,
		}},
	}
	assert.Equal(
		t,
		"plan rev 0: interaction: failed (node server.js) [2 steps] — a1: step[1] assert_text: missing expected text \"X\"",
		interactionFailed.Summary(),
	)

	manual := &Result{
		Checks: []Check{
			{
				Kind:    plancontract.KindManual,
				Items:   []string{"m1", "m2"},
				Skipped: true,
				Reason:  "manual: verified by a human, not machine-gated (SP-149 §149a)",
			},
		},
	}
	assert.Equal(
		t,
		"plan rev 0: manual: skipped [2 manual items] — manual: verified by a human, not machine-gated (SP-149 §149a)",
		manual.Summary(),
	)
}
