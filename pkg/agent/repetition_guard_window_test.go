// repetition_guard_window_test.go — the cycling-phrases signal: a reply that
// cycles a few short phrases without ever repeating one line six times in a
// row is still a degenerate loop; mostly-distinct short lines are not.
package agent

import (
	"fmt"
	"strings"
	"testing"
)

// cyclingLoop is a degenerate stream recorded from an automation run: the
// model cycles "Let me run." variants instead of calling the tool. No single
// line repeats more than twice in a row.
var cyclingLoop = []string{
	"Let me check.", "Let me run.", "Let me do it.", "Let me run.",
	"Let me execute.", "Let me run.", "Running.", "Let me execute.",
	"Let me run git status.", "Let me do it.", "Let me run.", "Executing.",
	"Let me run.", "Let me do it.", "Let me run.", "Let me execute.",
	"Let me do it.", "Let me run.", "Now.", "Let me run.", "(Executing.)",
}

func TestRepetitionGuardCyclingPhrasesFlag(t *testing.T) {
	rec := &sinkRecorder{}
	g := enabledGuard(rec)

	for _, l := range cyclingLoop {
		g.Write(l + "\n")
	}
	g.Finish()

	if !g.Looping() {
		t.Fatalf("a reply cycling a few short phrases must flag")
	}
	if g.Dropped() == "" {
		t.Errorf("the detected loop must be recorded as dropped text")
	}
}

func TestRepetitionGuardCyclingStopsDeliveringAfterDetection(t *testing.T) {
	rec := &sinkRecorder{}
	g := enabledGuard(rec)

	for _, l := range cyclingLoop {
		g.Write(l + "\n")
	}
	before := rec.joined()
	g.Write("Let me run.\n")
	g.Finish()

	if got := rec.joined(); got != before {
		t.Errorf("nothing may be delivered after detection; got extra %q", strings.TrimPrefix(got, before))
	}
}

func TestRepetitionGuardDistinctShortLinesNotFlagged(t *testing.T) {
	rec := &sinkRecorder{}
	g := enabledGuard(rec)

	for i := 1; i <= 24; i++ {
		g.Write(fmt.Sprintf("- [x] step %d done\n", i))
	}
	g.Finish()

	if g.Looping() {
		t.Fatalf("a checklist of distinct short lines must not flag")
	}
}

func TestRepetitionGuardLongLineClearsWindow(t *testing.T) {
	rec := &sinkRecorder{}
	g := enabledGuard(rec)

	prose := strings.Repeat("This sentence is long enough to read as prose rather than a token. ", 3)
	for i := 0; i < 3; i++ {
		for _, l := range cyclingLoop[:10] {
			g.Write(l + "\n")
		}
		g.Write(prose + "\n")
	}
	g.Finish()

	if g.Looping() {
		t.Fatalf("short phrases separated by prose never fill the window and must not flag")
	}
}

func TestRepetitionGuardCyclingInsideFenceNotFlagged(t *testing.T) {
	rec := &sinkRecorder{}
	g := enabledGuard(rec)

	g.Write("```text\n")
	for i := 0; i < 3; i++ {
		for _, l := range cyclingLoop {
			g.Write(l + "\n")
		}
	}
	g.Write("```\n")
	g.Finish()

	if g.Looping() {
		t.Fatalf("lines inside a fenced block must never flag")
	}
}

func TestRepetitionGuardCyclingWithToolCallKept(t *testing.T) {
	rec := &sinkRecorder{}
	g := enabledGuard(rec)

	for _, l := range cyclingLoop {
		g.Write(l + "\n")
	}
	g.NoteToolCall()
	g.Finish()

	if g.Looping() {
		t.Fatalf("a reply that emits a tool call is never a degenerate loop")
	}
}

func TestRepetitionGuardResetClearsWindow(t *testing.T) {
	rec := &sinkRecorder{}
	g := enabledGuard(rec)

	for _, l := range cyclingLoop[:12] {
		g.Write(l + "\n")
	}
	g.Reset()
	for _, l := range cyclingLoop[:12] {
		g.Write(l + "\n")
	}
	g.Finish()

	if g.Looping() {
		t.Fatalf("Reset must clear the window: two 12-line halves never fill it")
	}
}
