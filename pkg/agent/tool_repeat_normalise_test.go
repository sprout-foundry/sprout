// tool_repeat_normalise_test.go — the near-duplicate and growth extensions
// to the per-turn repeat guard: near-normalisation equivalences, the runaway
// clause scan and its refusal wiring in the executor, the growth shape
// (same near call whose args grow on each repeat), the cycling shape (a few
// command shapes that near-collapse to one key), the scratch-file check for
// the idle rule, and a normal debug session that never trips.

package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	core "github.com/sprout-foundry/seed/core"
	"github.com/sprout-foundry/sprout/pkg/agent/changes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeToolArgsNearEquivalences(t *testing.T) {
	cases := []struct{ a, b string }{
		{"tail -n 5 /var/log/app.log", "tail -n 55 /var/log/app.log"},
		{"head -n 10 file", "head -n 99 file"},
		{"curl --timeout 30 https://x", "curl --timeout 45 https://x"},
		{"curl --timeout=30 https://x", "curl --timeout=45 https://x"},
		{"timeout 10 curl https://x", "timeout 25 curl https://x"},
		{"sleep 30", "sleep 5"},
		{"echo run-123", "echo run-999"},
		{"echo run-123 | wc -l", "echo run-999 | wc -l"},
		{"make  build", "make build"},
	}
	for _, c := range cases {
		assert.Equal(t, normalizeToolArgsNear(c.a), normalizeToolArgsNear(c.b), c.a+" ~ "+c.b)
	}
}

func TestNearNormalizationCollidesWhatExactKeepsDistinct(t *testing.T) {
	assert.NotEqual(t,
		normalizeToolArgs("tail -n 5 /var/log/app.log"),
		normalizeToolArgs("tail -n 55 /var/log/app.log"),
		"the exact normalisation keeps the -N digit distinct")
	assert.Equal(t,
		normalizeToolArgsNear("tail -n 5 /var/log/app.log"),
		normalizeToolArgsNear("tail -n 55 /var/log/app.log"),
		"the near normalisation masks the -N digit")

	assert.NotEqual(t,
		normalizeToolArgs("echo run-123"),
		normalizeToolArgs("echo run-999"))
	assert.Equal(t,
		normalizeToolArgsNear("echo run-123"),
		normalizeToolArgsNear("echo run-999"))
}

func TestFindRunawayClauseCountsRepeatedRuns(t *testing.T) {
	clause, times := findRunawayClause(strings.Repeat("the quick brown fox ", 6))
	assert.Equal(t, "the quick brown", clause)
	assert.Equal(t, 6, times)

	// Five repetitions stay below the refusal bound (the wiring refuses on
	// times > 5, and the scan reports ("", 0) below its minimum).
	clause, times = findRunawayClause(strings.Repeat("the quick brown fox ", 5))
	assert.Equal(t, "", clause)
	assert.Equal(t, 0, times)

	clause, times = findRunawayClause("make build && make test")
	assert.Equal(t, "", clause)
	assert.Equal(t, 0, times)
}

func TestFindRunawayClauseSkipsOversizedArgs(t *testing.T) {
	args := strings.Repeat("the quick brown fox ", 300) // 6600 bytes
	assert.Greater(t, len(args), runawayMaxArgsBytes)
	clause, times := findRunawayClause(args)
	assert.Equal(t, "", clause, "args over the 4 KB bound are not scanned")
	assert.Equal(t, 0, times)
}

// TestGrowthShapeTripsAsGrowingArgs — shape 1: each call copies the previous
// one with a longer -N, so the arguments grow and no two exact keys ever
// match, but the near key is stable and its argument length grows on each
// repeat. Trips within 10 calls as a growth stall.
func TestGrowthShapeTripsAsGrowingArgs(t *testing.T) {
	a := &Agent{}
	var trippedAt int
	var summary *StallSummary
	for i := 0; i < 8; i++ {
		args := fmt.Sprintf("tail -n %s /var/log/app.log", strings.Repeat("5", i+1))
		_, stalled, s := a.recordToolCall("shell_command", args, "out")
		if stalled {
			trippedAt = i + 1
			summary = s
			break
		}
	}
	require.True(t, summary != nil, "the growth shape trips the guard")
	assert.LessOrEqual(t, trippedAt, 10, "trips within 10 calls")
	require.NotNil(t, summary)
	assert.Equal(t, "growing_args", summary.Reason)

	// Every -N variant is a distinct exact key seen once, so the exact
	// counts stay far below the identical threshold.
	tracker := a.toolRepeatTrackerForTurn()
	for i := 0; i < trippedAt; i++ {
		args := fmt.Sprintf("tail -n %s /var/log/app.log", strings.Repeat("5", i+1))
		assert.Less(t,
			tracker.keyCount(toolArgKey("shell_command", args)),
			defaultStallIdenticalCalls,
			"exact counts stay below the identical threshold")
	}
}

// TestCyclingShapesTripAsNearDuplicates — shape 2: the agent cycles through a
// few command shapes (here: the same grep with jittered --timeout values).
// Each exact shape stays below the identical threshold, but the near key
// (with the timeout masked) accumulates across the whole turn and trips.
func TestCyclingShapesTripAsNearDuplicates(t *testing.T) {
	a := &Agent{}
	a.SetStallThresholds(5, 60) // near-duplicate threshold = 2*5 = 10
	shapes := []string{
		"grep ERROR /var/log/app.log --timeout 10",
		"grep ERROR /var/log/app.log --timeout 20",
		"grep ERROR /var/log/app.log --timeout 30",
	}
	var trippedAt int
	var firstSummary *StallSummary
	for c := 0; c < 6; c++ {
		for si := 0; si < len(shapes); si++ {
			_, stalled, s := a.recordToolCall("shell_command", shapes[si], "out")
			if stalled && firstSummary == nil {
				trippedAt = c*len(shapes) + si + 1
				firstSummary = s
			}
		}
	}
	require.Equal(t, 10, trippedAt, "the cycling shape trips within 10 calls")
	require.NotNil(t, firstSummary)
	assert.Equal(t, "near_duplicates", firstSummary.Reason)
	assert.Equal(t, 10, firstSummary.Repeats, "the near count across all shapes")

	// After all six cycles each exact shape has run six times — below the
	// identical threshold (8), which is why the exact path never trips.
	tracker := a.toolRepeatTrackerForTurn()
	for _, s := range shapes {
		assert.Equal(t, 6, tracker.keyCount(toolArgKey("shell_command", s)))
	}
	assert.Less(t, 6, defaultStallIdenticalCalls)
}

// TestRepeatGuardExecutorRefusesRunawayClause — a clause repeated more than 5
// times in the call's arguments: the batch still runs (the inner executor
// saw the call) but the model-visible result is replaced with a refusal
// naming the clause, tagged as an error, and the call is NOT observed by
// the repeat guard.
func TestRepeatGuardExecutorRefusesRunawayClause(t *testing.T) {
	agent := &Agent{}
	inner := &stubExecutor{}
	e := newRepeatGuardExecutor(inner, agent)
	args := strings.Repeat("the quick brown fox ", 6)
	results := e.Execute(context.Background(), []core.ToolCall{seedCall("1", "shell_command", args)})
	require.Len(t, results, 1)
	require.Len(t, inner.calls, 1, "the inner executor ran the call (delegation happens)")
	assert.Equal(t, core.ToolStatusError, results[0].Status, "the refusal is surfaced as an error status")
	assert.Contains(t, results[0].Content, "the quick brown", "the refusal names the runaway clause")
	assert.Contains(t, results[0].Content, "degenerated")
	assert.Contains(t, results[0].Content, "stop repeating")

	tracker := agent.toolRepeatTrackerForTurn()
	assert.Equal(t, 0,
		tracker.keyCount(toolArgKey("shell_command", args)),
		"a refused call is not observed by the repeat guard")
	assert.Nil(t, agent.GetStallSummary())
}

// TestRepeatGuardExecutorLongNormalCommandUntouched — a long command whose
// argument tokens are all distinct is not a runaway clause and executes
// untouched (normal result, no error status).
func TestRepeatGuardExecutorLongNormalCommandUntouched(t *testing.T) {
	agent := &Agent{}
	inner := &stubExecutor{}
	e := newRepeatGuardExecutor(inner, agent)
	var b strings.Builder
	for i := 0; i < 40; i++ {
		if i > 0 {
			b.WriteString(" ")
		}
		fmt.Fprintf(&b, "cmd%d", i)
	}
	args := b.String() // long, but every 3-token window is unique
	results := e.Execute(context.Background(), []core.ToolCall{seedCall("1", "shell_command", args)})
	require.Len(t, inner.calls, 1)
	require.Len(t, results, 1)
	assert.Equal(t, "tool result for shell_command", results[0].Content, "a long normal command executes untouched")
	assert.NotEqual(t, core.ToolStatusError, results[0].Status)
}

// TestRepeatGuardExecutorRunawaySkippedOver4KB — arguments over the 4 KB
// scan bound are not scanned for runaway clauses (the call is not refused,
// it runs normally).
func TestRepeatGuardExecutorRunawaySkippedOver4KB(t *testing.T) {
	agent := &Agent{}
	inner := &stubExecutor{}
	e := newRepeatGuardExecutor(inner, agent)
	args := strings.Repeat("the quick brown fox ", 300) // 6600 bytes > 4096
	results := e.Execute(context.Background(), []core.ToolCall{seedCall("1", "shell_command", args)})
	require.Len(t, inner.calls, 1)
	assert.Equal(t, "tool result for shell_command", results[0].Content, "over-4 KB args are not refused")
	assert.NotEqual(t, core.ToolStatusError, results[0].Status)
}

// TestStallIdlePathScratchOnlyCountsAsIdle — a turn whose only change is a
// scratch file under the OS temp dir makes no progress: the idle rule still
// trips at the same count. A real (non-temp) file change exempts the turn.
func TestStallIdlePathScratchOnlyCountsAsIdle(t *testing.T) {
	a := &Agent{}
	a.SetStallThresholds(8, 5)
	tracker := changes.NewTrackerForTesting("rev-scratch")
	tracker.MarkTurnStart()
	require.NoError(t, tracker.TrackFileEdit(t.TempDir()+"/scratch.txt", "old", "new"))
	a.changeTracker = tracker
	require.Len(t, a.TurnChangedPaths(), 1, "the test tracker reports the scratch write")
	assert.Empty(t, progressPaths(a), "scratch files under the temp dir are filtered out")

	var stalled bool
	var summary *StallSummary
	for i := 1; i <= 5; i++ {
		_, stalled, summary = a.recordToolCall("shell_command", "same probe", "out")
		assert.False(t, stalled, "call %d: under the idle threshold", i)
	}
	_, stalled, summary = a.recordToolCall("shell_command", "same probe", "out")
	require.True(t, stalled, "scratch-only changes are no progress: the idle rule still trips")
	require.NotNil(t, summary)
	assert.Equal(t, "idle_calls", summary.Reason)

	// A real (non-temp) change is progress and exempts the turn at the same
	// call count.
	b := &Agent{}
	b.SetStallThresholds(8, 5)
	real := changes.NewTrackerForTesting("rev-real")
	real.MarkTurnStart()
	require.NoError(t, real.TrackFileEdit("/ws/app.go", "old", "new"))
	b.changeTracker = real
	assert.Len(t, progressPaths(b), 1, "a real path survives the filter")
	for i := 0; i < 6; i++ {
		_, stalled, _ = b.recordToolCall("shell_command", "same probe", "out")
		assert.False(t, stalled, "a real file change is progress: no idle trip")
	}
}

// TestDebugSessionNeverTrips — a normal debug session: a dozen varied
// commands plus one real file edit. No shape accumulates and nothing trips.
func TestDebugSessionNeverTrips(t *testing.T) {
	a := &Agent{}
	tracker := changes.NewTrackerForTesting("rev-debug")
	tracker.MarkTurnStart()
	require.NoError(t, tracker.TrackFileEdit("/ws/app.go", "old", "new"))
	a.changeTracker = tracker
	for i := 0; i < 12; i++ {
		_, stalled, summary := a.recordToolCall("shell_command", fmt.Sprintf("make target%d", i), "out")
		assert.False(t, stalled)
		assert.Empty(t, summary)
	}
	assert.Nil(t, a.GetStallSummary(), "a varied debug session never stalls")
}
