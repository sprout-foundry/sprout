package console

import (
	"strings"
	"sync"
	"testing"
)

// SteerInputReader is hard to unit-test end-to-end because Start()
// requires a real TTY (term.MakeRaw fails on a pipe). These tests
// exercise the key-handling pure logic by constructing the reader
// with isTTY=false and calling the handlers directly. That covers
// the buffer/submit/clear semantics without touching the terminal.

func newTestReader(submitted *[]string, interrupted *int) *SteerInputReader {
	var mu sync.Mutex
	return &SteerInputReader{
		fd: -1, // not a TTY
		submitFn: func(s string) {
			mu.Lock()
			defer mu.Unlock()
			*submitted = append(*submitted, s)
		},
		interruptFn: func() {
			mu.Lock()
			defer mu.Unlock()
			*interrupted++
		},
	}
}

// newTestReaderWithQueue is like newTestReader but also wires a queue
// callback so SP-055 Phase 3b (Tab toggle → queue submit) can be
// tested in isolation from the agent.
func newTestReaderWithQueue(submitted, queued *[]string) *SteerInputReader {
	var mu sync.Mutex
	return &SteerInputReader{
		fd: -1,
		submitFn: func(s string) {
			mu.Lock()
			defer mu.Unlock()
			*submitted = append(*submitted, s)
		},
		queueFn: func(s string) {
			mu.Lock()
			defer mu.Unlock()
			*queued = append(*queued, s)
		},
		interruptFn: func() {},
	}
}

func TestSteerInputReader_PrintableAccumulates(t *testing.T) {
	var submitted []string
	var interrupted int
	r := newTestReader(&submitted, &interrupted)

	r.handlePrintable('h')
	r.handlePrintable('i')

	if got := string(r.buffer); got != "hi" {
		t.Fatalf("expected buffer 'hi', got %q", got)
	}
}

func TestSteerInputReader_BackspaceTrims(t *testing.T) {
	var submitted []string
	var interrupted int
	r := newTestReader(&submitted, &interrupted)

	r.handlePrintable('a')
	r.handlePrintable('b')
	r.handlePrintable('c')
	r.handleBackspace()

	if got := string(r.buffer); got != "ab" {
		t.Fatalf("expected 'ab', got %q", got)
	}
}

func TestSteerInputReader_BackspaceOnEmptyIsNoop(t *testing.T) {
	var submitted []string
	var interrupted int
	r := newTestReader(&submitted, &interrupted)

	r.handleBackspace()
	r.handleBackspace()

	if len(r.buffer) != 0 {
		t.Fatalf("expected empty buffer, got %q", string(r.buffer))
	}
}

func TestSteerInputReader_SubmitFiresCallbackAndClearsBuffer(t *testing.T) {
	var submitted []string
	var interrupted int
	r := newTestReader(&submitted, &interrupted)

	for _, b := range []byte("focus on perf") {
		r.handlePrintable(b)
	}
	r.handleSubmit()

	if len(submitted) != 1 {
		t.Fatalf("expected 1 submission, got %d", len(submitted))
	}
	if submitted[0] != "focus on perf" {
		t.Fatalf("expected 'focus on perf', got %q", submitted[0])
	}
	if len(r.buffer) != 0 {
		t.Fatalf("buffer should clear after submit, got %q", string(r.buffer))
	}
}

func TestSteerInputReader_EmptySubmitIsNoop(t *testing.T) {
	var submitted []string
	var interrupted int
	r := newTestReader(&submitted, &interrupted)

	r.handleSubmit()
	r.handleSubmit()

	if len(submitted) != 0 {
		t.Fatalf("empty submit should not fire callback, got %d calls", len(submitted))
	}
}

func TestSteerInputReader_InterruptFiresAndClears(t *testing.T) {
	var submitted []string
	var interrupted int
	r := newTestReader(&submitted, &interrupted)

	r.handlePrintable('x')
	r.handlePrintable('y')
	r.handleInterrupt()

	if interrupted != 1 {
		t.Fatalf("expected 1 interrupt, got %d", interrupted)
	}
	if len(r.buffer) != 0 {
		t.Fatalf("interrupt should clear buffer, got %q", string(r.buffer))
	}
	if len(submitted) != 0 {
		t.Fatalf("interrupt should not submit, got %d submissions", len(submitted))
	}
}

func TestSteerInputReader_NonTTYIsNoop(t *testing.T) {
	// Calling Start() on a non-TTY reader should not panic or block.
	// We verify by constructing one with isTTY=false (the default for
	// fd=-1) and calling Start/Stop.
	var submitted []string
	var interrupted int
	r := newTestReader(&submitted, &interrupted)

	r.Start()
	r.Stop()
	// No assertion beyond "didn't hang or panic".
}

func TestSteerInputReader_BufferIsolatedAcrossSubmissions(t *testing.T) {
	var submitted []string
	var interrupted int
	r := newTestReader(&submitted, &interrupted)

	for _, b := range []byte("first") {
		r.handlePrintable(b)
	}
	r.handleSubmit()

	for _, b := range []byte("second") {
		r.handlePrintable(b)
	}
	r.handleSubmit()

	if len(submitted) != 2 {
		t.Fatalf("expected 2 submissions, got %d", len(submitted))
	}
	if submitted[0] != "first" || submitted[1] != "second" {
		t.Fatalf("submissions out of order: %v", submitted)
	}
}

func TestSteerLineWithCursor_FitsInWidth(t *testing.T) {
	// The pinned line renders text + caret padded to terminal width.
	// Verify the cursor caret is present and the line is exactly cols
	// wide (accounting for visible chars, not bytes).
	out := steerLineWithCursor("hello", 20)
	if !strings.Contains(out, "▏") {
		t.Fatalf("expected cursor caret in output, got %q", out)
	}
	if visibleLen(out) != 20 {
		t.Fatalf("expected visible length 20, got %d (%q)", visibleLen(out), out)
	}
}

func TestSteerLineWithCursor_TruncatesLongInput(t *testing.T) {
	// Input longer than the terminal width should ellipsize so the
	// caret stays visible (otherwise the user can't tell where their
	// keystrokes land).
	long := strings.Repeat("a", 100)
	out := steerLineWithCursor(long, 20)
	if !strings.Contains(out, "…") {
		t.Fatalf("expected ellipsis for overflow, got %q", out)
	}
	if !strings.Contains(out, "▏") {
		t.Fatalf("caret should still appear, got %q", out)
	}
	if visibleLen(out) != 20 {
		t.Fatalf("expected visible length 20, got %d", visibleLen(out))
	}
}

func TestSteerPromptPrefix_NonEmpty(t *testing.T) {
	// Sanity check on the exported prefix constant so a future rename
	// flags it via test failure.
	if SteerPromptPrefix == "" {
		t.Fatal("SteerPromptPrefix should not be empty")
	}
}

// History tests (SP-055 Phase 3)

func TestSteerHistory_SubmissionsAccumulate(t *testing.T) {
	var submitted []string
	var interrupted int
	r := newTestReader(&submitted, &interrupted)
	r.historyIndex = -1

	for _, msg := range []string{"first", "second", "third"} {
		for _, b := range []byte(msg) {
			r.handlePrintable(b)
		}
		r.handleSubmit()
	}

	if got := len(r.history); got != 3 {
		t.Fatalf("expected 3 history entries, got %d", got)
	}
	if r.history[0] != "first" || r.history[2] != "third" {
		t.Fatalf("history not in submit order: %v", r.history)
	}
}

func TestSteerHistory_ConsecutiveDupsCollapsed(t *testing.T) {
	var submitted []string
	var interrupted int
	r := newTestReader(&submitted, &interrupted)
	r.historyIndex = -1

	for i := 0; i < 3; i++ {
		for _, b := range []byte("same") {
			r.handlePrintable(b)
		}
		r.handleSubmit()
	}

	if got := len(r.history); got != 1 {
		t.Fatalf("consecutive dups should collapse, got %d entries: %v", got, r.history)
	}
}

func TestSteerHistory_UpArrowRecallsMostRecent(t *testing.T) {
	var submitted []string
	var interrupted int
	r := newTestReader(&submitted, &interrupted)
	r.historyIndex = -1

	for _, msg := range []string{"alpha", "beta"} {
		for _, b := range []byte(msg) {
			r.handlePrintable(b)
		}
		r.handleSubmit()
	}

	// Up arrow: should bring back "beta" (most recent).
	r.recallHistory(-1)
	if got := string(r.buffer); got != "beta" {
		t.Fatalf("expected 'beta' after Up, got %q", got)
	}
	// Another Up: should walk to "alpha".
	r.recallHistory(-1)
	if got := string(r.buffer); got != "alpha" {
		t.Fatalf("expected 'alpha' after second Up, got %q", got)
	}
}

func TestSteerHistory_DownArrowReturnsToPendingBuffer(t *testing.T) {
	var submitted []string
	var interrupted int
	r := newTestReader(&submitted, &interrupted)
	r.historyIndex = -1

	// Submit one entry.
	for _, b := range []byte("hello") {
		r.handlePrintable(b)
	}
	r.handleSubmit()

	// Start typing a NEW message but don't submit.
	for _, b := range []byte("in-progress") {
		r.handlePrintable(b)
	}

	// Up arrow → recall "hello" (snapshots "in-progress" as pending).
	r.recallHistory(-1)
	if got := string(r.buffer); got != "hello" {
		t.Fatalf("expected 'hello' after Up, got %q", got)
	}
	// Down arrow → return to "in-progress".
	r.recallHistory(+1)
	if got := string(r.buffer); got != "in-progress" {
		t.Fatalf("expected pending buffer restored, got %q", got)
	}
}

func TestSteerHistory_TypingExitsRecall(t *testing.T) {
	var submitted []string
	var interrupted int
	r := newTestReader(&submitted, &interrupted)
	r.historyIndex = -1

	for _, b := range []byte("old message") {
		r.handlePrintable(b)
	}
	r.handleSubmit()

	r.recallHistory(-1) // bring back "old message"
	r.handlePrintable('!')

	if got := string(r.buffer); got != "old message!" {
		t.Fatalf("expected edited recall, got %q", got)
	}
	if r.historyIndex != -1 {
		t.Fatalf("typing should exit history nav, got index=%d", r.historyIndex)
	}
}

func TestSteerHistory_CapBounded(t *testing.T) {
	var submitted []string
	var interrupted int
	r := newTestReader(&submitted, &interrupted)
	r.historyIndex = -1

	// Submit SteerHistoryCap+5 unique messages.
	for i := 0; i < SteerHistoryCap+5; i++ {
		msg := []byte{'a' + byte(i%26), byte('0' + (i/26)%10)}
		for _, b := range msg {
			r.handlePrintable(b)
		}
		r.handleSubmit()
	}

	if got := len(r.history); got != SteerHistoryCap {
		t.Fatalf("history should cap at %d, got %d", SteerHistoryCap, got)
	}
}

func TestSteerHistory_EmptyHistoryNoOpOnArrow(t *testing.T) {
	var submitted []string
	var interrupted int
	r := newTestReader(&submitted, &interrupted)
	r.historyIndex = -1

	r.recallHistory(-1) // Up on empty history
	if len(r.buffer) != 0 {
		t.Fatalf("Up on empty history should leave buffer empty, got %q", string(r.buffer))
	}
}

// Done-queue mode (SP-055 Phase 3b)

func TestSteerSubmitMode_DefaultIsNow(t *testing.T) {
	var submitted, queued []string
	r := newTestReaderWithQueue(&submitted, &queued)
	if r.SubmitMode() != SteerSubmitModeNow {
		t.Fatalf("expected default SubmitMode = Now, got %v", r.SubmitMode())
	}
}

func TestSteerSubmitMode_TabTogglesWhenQueueFnWired(t *testing.T) {
	var submitted, queued []string
	r := newTestReaderWithQueue(&submitted, &queued)

	r.toggleSubmitMode()
	if r.SubmitMode() != SteerSubmitModeQueue {
		t.Fatalf("first toggle should be Queue, got %v", r.SubmitMode())
	}
	r.toggleSubmitMode()
	if r.SubmitMode() != SteerSubmitModeNow {
		t.Fatalf("second toggle should be Now, got %v", r.SubmitMode())
	}
}

func TestSteerSubmitMode_TabNoopWithoutQueueFn(t *testing.T) {
	// Reader built WITHOUT a queueFn (e.g. tests that didn't opt in).
	// Tab should be inert.
	var submitted []string
	var interrupted int
	r := newTestReader(&submitted, &interrupted)
	r.toggleSubmitMode()
	if r.SubmitMode() != SteerSubmitModeNow {
		t.Fatalf("toggle without queueFn must stay Now, got %v", r.SubmitMode())
	}
}

func TestSteerSubmitMode_EnterRoutesToActiveCallback(t *testing.T) {
	var submitted, queued []string
	r := newTestReaderWithQueue(&submitted, &queued)

	// First submit in Now mode → goes to submitFn.
	for _, b := range []byte("inline steer") {
		r.handlePrintable(b)
	}
	r.handleSubmit()
	if len(submitted) != 1 || submitted[0] != "inline steer" {
		t.Fatalf("expected submitFn fired with 'inline steer', got submitted=%v", submitted)
	}
	if len(queued) != 0 {
		t.Fatalf("queueFn should NOT have fired, got queued=%v", queued)
	}

	// Toggle then submit → goes to queueFn.
	r.toggleSubmitMode()
	for _, b := range []byte("save for later") {
		r.handlePrintable(b)
	}
	r.handleSubmit()
	if len(queued) != 1 || queued[0] != "save for later" {
		t.Fatalf("expected queueFn fired with 'save for later', got queued=%v", queued)
	}
	// submitFn should NOT have fired again.
	if len(submitted) != 1 {
		t.Fatalf("submitFn fired a second time; got %v", submitted)
	}
}

func TestSteerSubmitMode_PromptPrefixesAreDistinct(t *testing.T) {
	if SteerPromptPrefix == QueuePromptPrefix {
		t.Fatal("steer and queue prefixes must differ visually")
	}
}

// UTF-8 input (SP-055 Phase 3c)

func TestSteerBackspace_RemovesFullMultibyteRune(t *testing.T) {
	var submitted []string
	var interrupted int
	r := newTestReader(&submitted, &interrupted)

	// Manually load a buffer with "hi 字" (4-byte UTF-8 string —
	// ASCII "hi " is 3 bytes, "字" is 3 bytes = 6 total).
	r.buffer = []byte("hi 字")
	r.handleBackspace()
	got := string(r.buffer)
	if got != "hi " {
		t.Fatalf("backspace should remove the whole rune '字', got %q (%d bytes)", got, len(r.buffer))
	}

	// Another backspace removes the trailing space.
	r.handleBackspace()
	if got := string(r.buffer); got != "hi" {
		t.Fatalf("expected 'hi', got %q", got)
	}
}

func TestSteerBackspace_RemovesFourByteEmoji(t *testing.T) {
	var submitted []string
	var interrupted int
	r := newTestReader(&submitted, &interrupted)
	// Rocket emoji is 4 bytes in UTF-8.
	r.buffer = []byte("ok 🚀")
	r.handleBackspace()
	if got := string(r.buffer); got != "ok " {
		t.Fatalf("expected 'ok ' after emoji backspace, got %q", got)
	}
}

func TestSteerHistory_DispatchCSIFinal_OnlyArrowsAct(t *testing.T) {
	var submitted []string
	var interrupted int
	r := newTestReader(&submitted, &interrupted)
	r.historyIndex = -1
	for _, b := range []byte("entry") {
		r.handlePrintable(b)
	}
	r.handleSubmit()

	// Right arrow ('C') / Left arrow ('D') / Home etc. — should NOT
	// touch the buffer.
	r.buffer = append(r.buffer[:0], []byte("current")...)
	r.dispatchCSIFinal('C')
	r.dispatchCSIFinal('D')
	r.dispatchCSIFinal('H')
	if got := string(r.buffer); got != "current" {
		t.Fatalf("non-arrow CSI keys should be inert, got %q", got)
	}

	// Up arrow ('A') — should now recall.
	r.dispatchCSIFinal('A')
	if got := string(r.buffer); got != "entry" {
		t.Fatalf("Up arrow should recall history, got %q", got)
	}
}
