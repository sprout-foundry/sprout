// repetition_guard_test.go — unit tests for the streaming degenerate
// repetition guard: a scripted looping stream is cut, legitimate repetition
// (a list, a code block, a progress log) is not flagged, and a tool call
// suppresses flagging.
package agent

import (
	"strings"
	"testing"
)

// enabledGuard builds an enabled guard with the default thresholds (a run of
// six identical short lines) and a recording sink.
func enabledGuard(rec *sinkRecorder) *RepetitionGuard {
	return NewRepetitionGuard(RepetitionGuardConfig{Enabled: true}, rec.sink)
}

// shortGuard builds an enabled guard with a custom run threshold.
func shortGuard(min int, rec *sinkRecorder) *RepetitionGuard {
	return NewRepetitionGuard(RepetitionGuardConfig{Enabled: true, MinRepetitions: min}, rec.sink)
}

// TestRepetitionGuardLoopsOnRepeatedLine pins the core detection: the same
// short line repeated past the threshold flags the guard, the held duplicates
// never reach the sink, and every later chunk is captured but not delivered.
func TestRepetitionGuardLoopsOnRepeatedLine(t *testing.T) {
	rec := &sinkRecorder{}
	g := enabledGuard(rec)

	// Five occurrences are below the default threshold of six: the first
	// streams through, the rest are held (not yet delivered).
	for i := 0; i < 5; i++ {
		g.Write("Let me run.\n")
	}
	if g.Looping() {
		t.Fatalf("below the threshold the guard must not flag")
	}
	if got, want := rec.joined(), "Let me run.\n"; got != want {
		t.Fatalf("only the first occurrence may reach the sink; sink got %q", got)
	}

	// The sixth occurrence crosses the threshold: the guard flags, and the
	// five held duplicates + this occurrence are dropped.
	g.Write("Let me run.\n")
	if !g.Looping() {
		t.Fatalf("a run of six identical lines must flag the guard")
	}
	if got, want := rec.joined(), "Let me run.\n"; got != want {
		t.Errorf("the degenerate text must not reach the sink; sink got %q, want %q", got, want)
	}
	wantDropped := strings.Repeat("Let me run.\n", 5)
	if got := g.Dropped(); got != wantDropped {
		t.Errorf("Dropped() = %q, want the five held+flagging occurrences %q", got, wantDropped)
	}

	// Later chunks are captured, never delivered.
	g.Write("Let me run.\n")
	g.Finish()
	if got, want := rec.joined(), "Let me run.\n"; got != want {
		t.Errorf("after the loop no further content may reach the sink; got %q", got)
	}
	if !strings.Contains(g.Dropped(), "Let me run.") {
		t.Errorf("Dropped() must capture the post-loop chunk; got %q", g.Dropped())
	}
}

// TestRepetitionGuardBreakFlushesHeldDuplicates pins that a run which breaks
// before the threshold is legitimate: the held duplicates are flushed to the
// sink in order, so the reply streams through unmodified.
func TestRepetitionGuardBreakFlushesHeldDuplicates(t *testing.T) {
	rec := &sinkRecorder{}
	g := enabledGuard(rec)

	g.Write("Let me run.\n")
	g.Write("Let me run.\n")
	g.Write("Let me run.\n") // three occurrences, still below the threshold
	g.Write("All done.\n")   // the run breaks

	if g.Looping() {
		t.Fatalf("a broken run below the threshold must not flag")
	}
	want := "Let me run.\nLet me run.\nLet me run.\nAll done.\n"
	if got := rec.joined(); got != want {
		t.Errorf("a broken run must be delivered in order; got %q, want %q", got, want)
	}
	if got := g.Dropped(); got != "" {
		t.Errorf("Dropped() = %q, want \"\" (nothing was dropped)", got)
	}
}

// TestRepetitionGuardSimilarListNotFlagged pins the false-positive guard for a
// list of similar-but-different items: they never form a consecutive identical
// run.
func TestRepetitionGuardSimilarListNotFlagged(t *testing.T) {
	rec := &sinkRecorder{}
	g := enabledGuard(rec)

	items := []string{
		"- Install the dependencies\n",
		"- Run the test suite\n",
		"- Build the binary\n",
		"- Update the changelog\n",
		"- Tag the release\n",
		"- Push the tag\n",
		"- Publish the package\n",
	}
	for _, it := range items {
		g.Write(it)
	}
	g.Finish()

	if g.Looping() {
		t.Fatalf("a list of different items must not flag")
	}
	if got, want := rec.joined(), strings.Join(items, ""); got != want {
		t.Errorf("the list must stream through unmodified; got %q, want %q", got, want)
	}
}

// TestRepetitionGuardCodeBlockRepeatedLinesNotFlagged pins the fence rule: a
// fenced code block with the same line many times is legal output and must not
// flag.
func TestRepetitionGuardCodeBlockRepeatedLinesNotFlagged(t *testing.T) {
	rec := &sinkRecorder{}
	g := enabledGuard(rec)

	g.Write("```go\n")
	for i := 0; i < 12; i++ {
		g.Write("x := 1\n")
	}
	g.Write("```\n")

	if g.Looping() {
		t.Fatalf("repeated lines inside a fenced code block must not flag")
	}
	want := "```go\n" + strings.Repeat("x := 1\n", 12) + "```\n"
	if got := rec.joined(); got != want {
		t.Errorf("the code block must stream through unmodified; got %q, want %q", got, want)
	}
}

// TestRepetitionGuardProgressLogNotFlagged pins that a progress log whose lines
// differ (by counter, path, or status) never accumulates a run.
func TestRepetitionGuardProgressLogNotFlagged(t *testing.T) {
	rec := &sinkRecorder{}
	g := enabledGuard(rec)

	for i := 1; i <= 10; i++ {
		g.Write("Building package " + strings.Repeat("a", i) + "\n")
	}
	g.Finish()

	if g.Looping() {
		t.Fatalf("a progress log must not flag")
	}
	if got := rec.joined(); !strings.Contains(got, "Building package a") {
		t.Errorf("the progress log must stream through; got %q", got)
	}
}

// TestRepetitionGuardToolCallSuppressesFlagging pins that a reply which acts is
// never a degenerate loop: a noted tool call suppresses the signal even after
// the text run crossed the threshold.
func TestRepetitionGuardToolCallSuppressesFlagging(t *testing.T) {
	rec := &sinkRecorder{}
	g := enabledGuard(rec)

	g.NoteToolCall()
	for i := 0; i < 8; i++ {
		g.Write("Let me run.\n")
	}

	if g.Looping() {
		t.Fatalf("a tool-call reply must not be flagged as a degenerate loop")
	}
}

// TestRepetitionGuardToolCallAfterSomeRepetitionKept pins the acceptance case:
// a stream with some repetition (below the threshold) followed by a tool call
// is kept in full.
func TestRepetitionGuardToolCallAfterSomeRepetitionKept(t *testing.T) {
	rec := &sinkRecorder{}
	g := enabledGuard(rec)

	g.Write("Let me check the file.\n")
	g.Write("Let me check the file.\n")
	g.Write("Let me check the file.\n") // three, below the threshold
	g.NoteToolCall()                    // the response acts
	g.Finish()

	if g.Looping() {
		t.Fatalf("repetition below the threshold plus a tool call must be kept")
	}
	want := strings.Repeat("Let me check the file.\n", 3)
	if got := rec.joined(); got != want {
		t.Errorf("the repeated preamble must be kept; got %q, want %q", got, want)
	}
}

// TestRepetitionGuardLongLinesNotFlagged pins the length rule: a long line
// repeated many times is prose, not a stuck token, and must not flag.
func TestRepetitionGuardLongLinesNotFlagged(t *testing.T) {
	rec := &sinkRecorder{}
	g := enabledGuard(rec)

	long := strings.Repeat("word ", 40) + "\n" // well past MaxLineChars
	for i := 0; i < 10; i++ {
		g.Write(long)
	}
	g.Finish()

	if g.Looping() {
		t.Fatalf("repeated long lines must not flag")
	}
	if got, want := rec.joined(), strings.Repeat(long, 10); got != want {
		t.Errorf("long repeated lines must stream through; got len %d, want len %d", len(got), len(want))
	}
}

// TestRepetitionGuardDifferingOnlyByWhitespaceFlags pins that near-verbatim
// identity is what counts: lines that differ only in surrounding whitespace
// form a run.
func TestRepetitionGuardDifferingOnlyByWhitespaceFlags(t *testing.T) {
	rec := &sinkRecorder{}
	g := shortGuard(4, rec)

	g.Write("Let me run.\n")
	g.Write("  Let me run.\n")
	g.Write("\tLet me run.\n")
	g.Write("Let me run.  \n")

	if !g.Looping() {
		t.Fatalf("lines differing only in whitespace must form a run")
	}
	if got, want := rec.joined(), "Let me run.\n"; got != want {
		t.Errorf("only the first occurrence may reach the sink; got %q, want %q", got, want)
	}
}

// TestRepetitionGuardDisabledIsPassthrough pins the opt-out: a disabled guard
// delivers every chunk eagerly and never flags.
func TestRepetitionGuardDisabledIsPassthrough(t *testing.T) {
	rec := &sinkRecorder{}
	g := NewRepetitionGuard(RepetitionGuardConfig{Enabled: false}, rec.sink)

	for i := 0; i < 10; i++ {
		g.Write("Let me run.\n")
	}
	g.Finish()

	if g.Looping() {
		t.Fatalf("a disabled guard must never flag")
	}
	if got, want := rec.joined(), strings.Repeat("Let me run.\n", 10); got != want {
		t.Errorf("a disabled guard must be a passthrough; got %q, want %q", got, want)
	}
}

// TestRepetitionGuardResetDiscardsState pins that Reset (the retry-once
// attempt) drops a looping/held attempt so it never leaks into the next.
func TestRepetitionGuardResetDiscardsState(t *testing.T) {
	rec := &sinkRecorder{}
	g := enabledGuard(rec)

	for i := 0; i < 6; i++ {
		g.Write("Let me run.\n")
	}
	if !g.Looping() {
		t.Fatalf("expected the run to flag")
	}
	g.Reset()

	if g.Looping() {
		t.Fatalf("after Reset the guard must not be looping")
	}
	if got := g.Dropped(); got != "" {
		t.Errorf("after Reset Dropped() = %q, want \"\"", got)
	}
	// A fresh, non-degenerate stream streams through normally.
	g.Write("Done.\n")
	g.Finish()
	if got := rec.joined(); !strings.Contains(got, "Done.\n") {
		t.Errorf("a fresh stream after Reset must stream through; got %q", got)
	}
}

// TestRepetitionGuardPartialLineStreamsPerChunk pins that a chunk with no
// newline streams through eagerly (live streaming is unchanged) — only a
// repeated complete line is withheld.
func TestRepetitionGuardPartialLineStreamsPerChunk(t *testing.T) {
	rec := &sinkRecorder{}
	g := enabledGuard(rec)

	g.Write("The build ")
	g.Write("succeeded ")
	g.Write("after applying the patch.")

	if got, want := rec.joined(), "The build succeeded after applying the patch."; got != want {
		t.Errorf("a single line split across chunks must stream through per chunk; got %q, want %q", got, want)
	}
	if len(rec.calls) != 3 {
		t.Errorf("each chunk must be delivered separately (live streaming); got %d calls", len(rec.calls))
	}
}

// TestRepetitionGuardBlankLinesNeutral pins that blank lines between repeated
// short lines do not reset the run (they are padding, not content).
func TestRepetitionGuardBlankLinesNeutral(t *testing.T) {
	rec := &sinkRecorder{}
	g := shortGuard(3, rec)

	g.Write("Let me run.\n")
	g.Write("\n")
	g.Write("Let me run.\n")
	g.Write("\n")
	g.Write("Let me run.\n")

	if !g.Looping() {
		t.Fatalf("blank lines between identical short lines must not reset the run")
	}
}

// TestRepetitionGuardLongNoNewlineLineStreamsAndStaysBounded pins that a very
// long single line (no newline) is streamed through eagerly and the guard's
// internal buffer stays bounded — it never accumulates the whole line.
func TestRepetitionGuardLongNoNewlineLineStreamsAndStaysBounded(t *testing.T) {
	rec := &sinkRecorder{}
	g := NewRepetitionGuard(RepetitionGuardConfig{Enabled: true, MaxLineChars: 50}, rec.sink)

	// Stream a long line in small chunks with no newline: every byte must be
	// delivered and the internal buffer must not grow past the cap (+ one
	// chunk).
	chunk := strings.Repeat("x", 10)
	for i := 0; i < 50; i++ {
		g.Write(chunk)
	}
	if got, want := len(rec.joined()), 500; got != want {
		t.Errorf("delivered %d bytes, want all %d (a long line must stream through)", got, want)
	}
	if got := g.buf.Len(); got > 60 {
		t.Errorf("guard buffer = %d bytes, want bounded (~MaxLineChars + one chunk)", got)
	}
	g.Finish()
	if g.Looping() {
		t.Errorf("a long line must not flag the guard")
	}
}
