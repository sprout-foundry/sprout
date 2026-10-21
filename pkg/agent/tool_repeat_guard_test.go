// tool_repeat_guard_test.go — the per-turn, every-tool repeat guard and the
// stall stop: identical-call counting and the model-visible nudge, the
// byte-identical note, threshold-based stall detection (identical and idle
// paths), turn-boundary resets, and the summary bookkeeping the workflow
// continuation loop reads.

package agent

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent/changes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeToolArgs(t *testing.T) {
	assert.Equal(t, "make build", normalizeToolArgs("make  \t\n build "))
	assert.Equal(t, "", normalizeToolArgs("   "))
	assert.Equal(t, "git status", normalizeToolArgs("git status"), "single spaces stay single")
}

func TestToolArgKeyStripsChannelSuffix(t *testing.T) {
	assert.Equal(t, toolArgKey("shell_command", "ls"), toolArgKey("shell_command<|channel|>7", "ls"))
	assert.NotEqual(t, toolArgKey("shell_command", "ls"), toolArgKey("read_file", "ls"))
}

func TestRepeatNoteAtThirdCall(t *testing.T) {
	a := &Agent{}
	for i := 1; i <= 3; i++ {
		note, stalled, summary := a.recordToolCall("shell_command", `{"command":"make build"}`, "ok")
		assert.Empty(t, summary)
		assert.False(t, stalled)
		if i < 3 {
			assert.Empty(t, note, "no note before the 3rd identical call")
		} else {
			require.NotEmpty(t, note)
			assert.Contains(t, note, "shell_command")
			assert.Contains(t, note, "3 times")
			assert.Contains(t, note, "will not change the outcome")
		}
	}
}

func TestRepeatNoteCountsUpward(t *testing.T) {
	a := &Agent{}
	var note string
	for i := 1; i <= 5; i++ {
		note, _, _ = a.recordToolCall("shell_command", "ls", "out")
	}
	assert.Contains(t, note, "5 times")
}

func TestRepeatNoteByteIdenticalFlag(t *testing.T) {
	a := &Agent{}
	_, _, _ = a.recordToolCall("shell_command", "ls", "same output")
	_, _, _ = a.recordToolCall("shell_command", "ls", "same output")
	note, _, _ := a.recordToolCall("shell_command", "ls", "same output")
	assert.Contains(t, note, "byte-identical")

	other := &Agent{}
	_, _, _ = other.recordToolCall("shell_command", "ls", "first")
	_, _, _ = other.recordToolCall("shell_command", "ls", "second")
	note, _, _ = other.recordToolCall("shell_command", "ls", "third")
	assert.NotContains(t, note, "byte-identical")
}

func TestVariedCallsNeverNoted(t *testing.T) {
	a := &Agent{}
	for i := 0; i < 10; i++ {
		note, stalled, _ := a.recordToolCall("shell_command", string(rune('a'+i)), "out")
		assert.Empty(t, note)
		assert.False(t, stalled)
	}
}

func TestWhitespaceOnlyArgDifferenceSharesKey(t *testing.T) {
	a := &Agent{}
	a.SetStallThresholds(3, 60)
	n1, _, _ := a.recordToolCall("shell_command", "make   build", "out")
	n2, _, _ := a.recordToolCall("shell_command", "make build", "out")
	_, _, n3 := a.recordToolCall("shell_command", "make\n\tbuild", "out")
	assert.Empty(t, n1)
	assert.Empty(t, n2)
	require.NotNil(t, n3, "whitespace-only variants share one key, so the 3rd call stalls")
	assert.Contains(t, n3.Args, "make build", "the summary carries the normalised args")
}

func TestStallAtIdenticalThreshold(t *testing.T) {
	a := &Agent{}
	a.SetStallThresholds(4, 60)
	var stalled bool
	var summary *StallSummary
	for i := 0; i < 4; i++ {
		_, stalled, summary = a.recordToolCall("shell_command", "probe", "out")
	}
	require.True(t, stalled)
	require.NotNil(t, summary)
	assert.Equal(t, "shell_command", summary.Tool)
	assert.Equal(t, "probe", summary.Args)
	assert.Equal(t, 4, summary.Repeats)
	assert.Equal(t, "identical_calls", summary.Reason)
	assert.Equal(t, "out", summary.Output)

	// The executor wrapper stores the summary when it observes the stall
	// (markTurnStalled); the continuation loop reads it off the agent.
	a.markTurnStalled(summary)
	require.NotNil(t, a.GetStallSummary())
	assert.Equal(t, "shell_command", a.GetStallSummary().Tool)

	// Below the threshold nothing trips.
	b := &Agent{}
	b.SetStallThresholds(8, 60)
	for i := 0; i < 7; i++ {
		_, stalled, _ = b.recordToolCall("shell_command", "probe", "out")
	}
	assert.False(t, stalled)
}

func TestStallIdleCallsNeedsNoFileChange(t *testing.T) {
	// idleCalls counts repeats of already-seen keys (totalCalls -
	// distinctCalls); the idle path also demands the turn changed no files.
	// Six identical calls = 5 redundant calls at the 6th, which crosses an
	// idle threshold of 5 (the identical threshold of 8 stays untripped).
	a := &Agent{}
	a.SetStallThresholds(8, 5)
	var stalled bool
	var summary *StallSummary
	for i := 1; i <= 5; i++ {
		_, stalled, summary = a.recordToolCall("shell_command", "same probe", "out")
		assert.False(t, stalled, "call %d: %d redundant, under the idle threshold", i, i-1)
	}
	_, stalled, summary = a.recordToolCall("shell_command", "same probe", "out")
	require.True(t, stalled, "6th call: 5 redundant calls crosses the idle threshold of 5")
	require.NotNil(t, summary)
	assert.Equal(t, "idle_calls", summary.Reason)

	// A new distinct call keeps a healthy turn under the threshold: its
	// redundant count still grows, but the identical path stays far away.
	b := &Agent{}
	b.SetStallThresholds(8, 60)
	for i := 0; i < 20; i++ {
		_, stalled, _ = b.recordToolCall("shell_command", fmt.Sprintf("probe %d", i), "out")
		assert.False(t, stalled, "varied distinct calls never accumulate redundancy")
	}
}

func TestStallIdlePathSkipsTurnsWithFileChanges(t *testing.T) {
	// A turn whose change tracker has recorded edits is making progress and
	// never trips the idle path.
	a := &Agent{}
	a.SetStallThresholds(8, 2)
	tracker := changes.NewTrackerForTesting("rev-stall")
	tracker.MarkTurnStart()
	require.NoError(t, tracker.TrackFileEdit("/ws/app.go", "old", "new"))
	a.changeTracker = tracker
	require.Len(t, a.TurnChangedPaths(), 1, "the test tracker reports the edit")

	for i := 0; i < 6; i++ {
		_, stalled, _ := a.recordToolCall("shell_command", "same probe", "out")
		assert.False(t, stalled, "a turn with file changes is not idle")
	}
}

func TestResetAtTurnBoundary(t *testing.T) {
	a := &Agent{}
	_, _, _ = a.recordToolCall("shell_command", "ls", "out")
	_, _, _ = a.recordToolCall("shell_command", "ls", "out")
	a.ResetToolRepeatGuardForNewTurn()
	note, stalled, _ := a.recordToolCall("shell_command", "ls", "out")
	assert.Empty(t, note, "a repeat in a NEW turn is a first call")
	assert.False(t, stalled)
	assert.Equal(t, 0, a.toolRepeatTrackerForTurn().idleCalls())
}

func TestStallSummarySurvivesGuardReset(t *testing.T) {
	// The continuation loop reads the summary AFTER the turn ends; the next
	// turn's guard reset must not clear it (ClearStallSummary does, at the
	// turn open, before the run).
	a := &Agent{}
	a.SetStallThresholds(2, 60)
	_, _, _ = a.recordToolCall("shell_command", "p", "out")
	_, stalled, summary := a.recordToolCall("shell_command", "p", "out")
	require.True(t, stalled)
	require.NotNil(t, summary)
	a.markTurnStalled(summary)
	a.ResetToolRepeatGuardForNewTurn()
	assert.NotNil(t, a.GetStallSummary(), "guard reset keeps the summary")
	a.ClearStallSummary()
	assert.Nil(t, a.GetStallSummary())
}

func TestSetStallThresholdsDefaults(t *testing.T) {
	a := &Agent{}
	assert.Equal(t, defaultStallIdenticalCalls, a.stallIdenticalThreshold())
	assert.Equal(t, defaultStallIdleCalls, a.stallIdleThreshold())
	a.SetStallThresholds(3, 10)
	assert.Equal(t, 3, a.stallIdenticalThreshold())
	assert.Equal(t, 10, a.stallIdleThreshold())
	a.SetStallThresholds(0, 0)
	assert.Equal(t, defaultStallIdenticalCalls, a.stallIdenticalThreshold(), "non-positive falls back to defaults")
	assert.Equal(t, defaultStallIdleCalls, a.stallIdleThreshold())
}

func TestNilAgentSafety(t *testing.T) {
	var a *Agent
	note, stalled, summary := a.recordToolCall("shell_command", "ls", "out")
	assert.Empty(t, note)
	assert.False(t, stalled)
	assert.Nil(t, summary)
	assert.Nil(t, a.GetStallSummary())
	assert.NotPanics(t, func() {
		a.SetStallThresholds(1, 1)
		a.ResetToolRepeatGuardForNewTurn()
		a.ClearStallSummary()
	})
}

func TestErrTurnStalledIsTyped(t *testing.T) {
	wrapped := wrapStallErrForTest()
	require.True(t, errors.Is(wrapped, ErrTurnStalled))
	assert.True(t, strings.HasPrefix(ErrTurnStalled.Error(), "turn stalled"))
}

func TestTrackerConcurrentObserve(t *testing.T) {
	tr := newToolRepeatTracker()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = tr.observe("k", "k", "result")
			_ = tr.idleCalls()
			_ = tr.keyCount("k")
		}()
	}
	wg.Wait()
	assert.Equal(t, 16, tr.keyCount("k"))
}

// wrapStallErrForTest mirrors the handleQueryResult wrap site so the typed
// error chain is pinned here too.
func wrapStallErrForTest() error {
	inner := errors.New("context canceled")
	return errors.Join(ErrTurnStalled, inner)
}
