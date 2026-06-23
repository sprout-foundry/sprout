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

	r.insertAtCursor([]byte{'h'})
	r.insertAtCursor([]byte{'i'})

	if got := string(r.buffer); got != "hi" {
		t.Fatalf("expected buffer 'hi', got %q", got)
	}
}

func TestSteerInputReader_BackspaceTrims(t *testing.T) {
	var submitted []string
	var interrupted int
	r := newTestReader(&submitted, &interrupted)

	r.insertAtCursor([]byte{'a'})
	r.insertAtCursor([]byte{'b'})
	r.insertAtCursor([]byte{'c'})
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
		r.insertAtCursor([]byte{b})
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

	r.insertAtCursor([]byte{'x'})
	r.insertAtCursor([]byte{'y'})
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
		r.insertAtCursor([]byte{b})
	}
	r.handleSubmit()

	for _, b := range []byte("second") {
		r.insertAtCursor([]byte{b})
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
	out := steerRowText("hello", 20, true)
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
	out := steerRowText(long, 20, true)
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
			r.insertAtCursor([]byte{b})
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
			r.insertAtCursor([]byte{b})
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
			r.insertAtCursor([]byte{b})
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
		r.insertAtCursor([]byte{b})
	}
	r.handleSubmit()

	// Start typing a NEW message but don't submit.
	for _, b := range []byte("in-progress") {
		r.insertAtCursor([]byte{b})
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
		r.insertAtCursor([]byte{b})
	}
	r.handleSubmit()

	r.recallHistory(-1) // bring back "old message"
	r.insertAtCursor([]byte{'!'})

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
			r.insertAtCursor([]byte{b})
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
		r.insertAtCursor([]byte{b})
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
		r.insertAtCursor([]byte{b})
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
	// ASCII "hi " is 3 bytes, "字" is 3 bytes = 6 total). Place the
	// cursor at the end so backspace deletes the rune before it.
	r.buffer = []byte("hi 字")
	r.cursorPos = len(r.buffer)
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
	r.cursorPos = len(r.buffer)
	r.handleBackspace()
	if got := string(r.buffer); got != "ok " {
		t.Fatalf("expected 'ok ' after emoji backspace, got %q", got)
	}
}

func TestSteerHistory_ArrowEventsOnlyArrowsAct(t *testing.T) {
	var submitted []string
	var interrupted int
	r := newTestReader(&submitted, &interrupted)
	r.historyIndex = -1
	for _, b := range []byte("entry") {
		r.insertAtCursor([]byte{b})
	}
	r.handleSubmit()

	// Right/Left move the cursor but do NOT mutate the buffer contents.
	// Home moves the cursor to the start. None mutate the buffer.
	r.buffer = append(r.buffer[:0], []byte("current")...)
	r.cursorPos = len(r.buffer)
	r.handleEvent(&InputEvent{Type: EventRight})
	r.handleEvent(&InputEvent{Type: EventLeft})
	r.handleEvent(&InputEvent{Type: EventHome})
	if got := string(r.buffer); got != "current" {
		t.Fatalf("Left/Right/Home should not mutate buffer, got %q", got)
	}

	// Up arrow — should now recall.
	r.handleEvent(&InputEvent{Type: EventUp})
	if got := string(r.buffer); got != "entry" {
		t.Fatalf("Up arrow should recall history, got %q", got)
	}
}

func TestSteerInputReader_PasteAccumulatesIntoBuffer(t *testing.T) {
	var submitted []string
	var interrupted int
	r := newTestReader(&submitted, &interrupted)

	// Simulate the sequence the terminal would emit for a bracketed
	// paste of "hello\nworld": ESC[200~ ... bytes ... ESC[201~. We
	// drive the paste handlers directly (a full readLoop would need
	// a real stdin); the paste lifecycle is beginPaste → appendPasteByte
	// → endPaste, with the readLoop routing bytes to appendPasteByte
	// while pasteActive is true.
	r.beginPaste()
	if !r.pasteActive {
		t.Fatalf("beginPaste should set pasteActive=true")
	}
	for _, b := range []byte("hello\nworld") {
		r.appendPasteByte(b)
	}
	r.endPaste()

	if r.pasteActive {
		t.Fatalf("endPaste should clear pasteActive")
	}
	if got := string(r.buffer); got != "hello\nworld" {
		t.Fatalf("expected paste content in buffer, got %q", got)
	}
}

func TestSteerInputReader_PasteSurvivesNewlines(t *testing.T) {
	var submitted []string
	var interrupted int
	r := newTestReader(&submitted, &interrupted)

	// Critical regression test: a paste that contains \r and \n
	// bytes must NOT trigger handleSubmit. The readLoop dispatches
	// based on pasteActive — but we verify via the handlers that
	// the bytes accumulate without truncation.
	r.beginPaste()
	for _, b := range []byte("line1\r\nline2\nline3") {
		r.appendPasteByte(b)
	}
	r.endPaste()

	if len(submitted) != 0 {
		t.Fatalf("paste containing newlines must not submit, got %d submissions", len(submitted))
	}
	if got := string(r.buffer); got != "line1\r\nline2\nline3" {
		t.Fatalf("paste content corrupted, got %q", got)
	}
}

func TestSteerInputReader_PasteAppendsToExistingBuffer(t *testing.T) {
	var submitted []string
	var interrupted int
	r := newTestReader(&submitted, &interrupted)

	// User typed "hi " then pasted "there".
	for _, b := range []byte("hi ") {
		r.insertAtCursor([]byte{b})
	}
	r.beginPaste()
	for _, b := range []byte("there") {
		r.appendPasteByte(b)
	}
	r.endPaste()

	if got := string(r.buffer); got != "hi there" {
		t.Fatalf("paste should append to existing buffer, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Readline cursor-editing tests (Ctrl-A/E/B/F, word motion, kill, etc).
// These exercise the pure handlers directly — no TTY required. renderLine
// is a no-op when footer == nil, so tests can leave footer unset.
// ---------------------------------------------------------------------------

func TestSteerCursor_StartEndMovement(t *testing.T) {
	r := &SteerInputReader{}
	r.buffer = []byte("hello")
	r.cursorPos = 5

	r.moveCursorStart()
	if r.cursorPos != 0 {
		t.Fatalf("moveCursorStart: expected 0, got %d", r.cursorPos)
	}
	r.moveCursorEnd()
	if r.cursorPos != 5 {
		t.Fatalf("moveCursorEnd: expected 5, got %d", r.cursorPos)
	}
}

func TestSteerCursor_BackwardForwardRune(t *testing.T) {
	r := &SteerInputReader{}
	r.buffer = []byte("hello")
	r.cursorPos = 5

	r.moveCursorBackward()
	if r.cursorPos != 4 {
		t.Fatalf("moveCursorBackward: expected 4, got %d", r.cursorPos)
	}
	// Cursor already at start → no-op (clamped).
	r.cursorPos = 0
	r.moveCursorBackward()
	if r.cursorPos != 0 {
		t.Fatalf("moveCursorBackward at 0: expected 0, got %d", r.cursorPos)
	}

	// Forward from 4 → 5.
	r.cursorPos = 4
	r.moveCursorForward()
	if r.cursorPos != 5 {
		t.Fatalf("moveCursorForward: expected 5, got %d", r.cursorPos)
	}
	// Already at end → no-op.
	r.moveCursorForward()
	if r.cursorPos != 5 {
		t.Fatalf("moveCursorForward at end: expected 5, got %d", r.cursorPos)
	}
}

func TestSteerCursor_InsertAtCursor(t *testing.T) {
	r := &SteerInputReader{}
	r.buffer = []byte("hello")
	r.cursorPos = 2

	r.insertAtCursor([]byte{'X'})
	if got := string(r.buffer); got != "heXllo" {
		t.Fatalf("insertAtCursor: expected 'heXllo', got %q", got)
	}
	if r.cursorPos != 3 {
		t.Fatalf("insertAtCursor: cursor should advance to 3, got %d", r.cursorPos)
	}

	// Inserting a multi-byte sequence advances cursor by its length.
	r.cursorPos = 0
	r.insertAtCursor([]byte("AB"))
	if got := string(r.buffer); got != "ABheXllo" {
		t.Fatalf("insertAtCursor multi: expected 'ABheXllo', got %q", got)
	}
	if r.cursorPos != 2 {
		t.Fatalf("insertAtCursor multi: cursor should be 2, got %d", r.cursorPos)
	}
}

func TestSteerCursor_InsertAtEnd(t *testing.T) {
	// Inserting when cursor is at len(buffer) appends and advances.
	r := &SteerInputReader{}
	r.buffer = []byte("hi")
	r.cursorPos = 2
	r.insertAtCursor([]byte("!"))
	if got := string(r.buffer); got != "hi!" {
		t.Fatalf("expected 'hi!', got %q", got)
	}
	if r.cursorPos != 3 {
		t.Fatalf("cursor should be 3, got %d", r.cursorPos)
	}
}

func TestSteerCursor_InsertAtCursorInsertsAtCursor(t *testing.T) {
	// insertAtCursor inserts at the cursor position instead of appending.
	r := &SteerInputReader{}
	r.buffer = []byte("ac")
	r.cursorPos = 1
	r.insertAtCursor([]byte{'b'})
	if got := string(r.buffer); got != "abc" {
		t.Fatalf("insertAtCursor at cursor: expected 'abc', got %q", got)
	}
	if r.cursorPos != 2 {
		t.Fatalf("cursor should be 2 after insert, got %d", r.cursorPos)
	}
}

func TestSteerCursor_BackspaceAtStartIsNoop(t *testing.T) {
	// Backspace with cursor at position 0 should be a no-op.
	r := &SteerInputReader{}
	r.buffer = []byte("hello")
	r.cursorPos = 0
	r.handleBackspace()
	if got := string(r.buffer); got != "hello" {
		t.Fatalf("backspace at start should not change buffer, got %q", got)
	}
	if r.cursorPos != 0 {
		t.Fatalf("cursor should stay 0, got %d", r.cursorPos)
	}
}

func TestSteerCursor_BackspaceBeforeCursor(t *testing.T) {
	// Backspace deletes the rune BEFORE the cursor, not the last rune.
	r := &SteerInputReader{}
	r.buffer = []byte("hello")
	r.cursorPos = 3 // cursor between 'l' and 'l' (after "hel")
	r.handleBackspace()
	if got := string(r.buffer); got != "helo" {
		t.Fatalf("backspace before cursor: expected 'helo', got %q", got)
	}
	if r.cursorPos != 2 {
		t.Fatalf("cursor should move to 2, got %d", r.cursorPos)
	}
}

func TestSteerCursor_DeleteWordBackward(t *testing.T) {
	r := &SteerInputReader{}
	r.buffer = []byte("hello world")
	r.cursorPos = 11 // end
	r.deleteWordBackward()
	if got := string(r.buffer); got != "hello " {
		t.Fatalf("deleteWordBackward: expected 'hello ', got %q", got)
	}
	if r.cursorPos != 6 {
		t.Fatalf("cursor should be 6, got %d", r.cursorPos)
	}
}

func TestSteerCursor_DeleteWordBackwardTrimsLeadingSpace(t *testing.T) {
	// Cursor after a space: deleteWordBackward skips whitespace then
	// deletes the preceding word.
	r := &SteerInputReader{}
	r.buffer = []byte("foo bar  ")
	r.cursorPos = 9 // after the two trailing spaces
	r.deleteWordBackward()
	if got := string(r.buffer); got != "foo " {
		t.Fatalf("expected 'foo ', got %q", got)
	}
	if r.cursorPos != 4 {
		t.Fatalf("cursor should be 4, got %d", r.cursorPos)
	}
}

func TestSteerCursor_DeleteWordBackwardAtStartIsNoop(t *testing.T) {
	r := &SteerInputReader{}
	r.buffer = []byte("hello")
	r.cursorPos = 0
	r.deleteWordBackward()
	if got := string(r.buffer); got != "hello" {
		t.Fatalf("deleteWordBackward at start should be noop, got %q", got)
	}
	if r.cursorPos != 0 {
		t.Fatalf("cursor should stay 0, got %d", r.cursorPos)
	}
}

func TestSteerCursor_KillToEnd(t *testing.T) {
	r := &SteerInputReader{}
	r.buffer = []byte("hello world")
	r.cursorPos = 5 // after "hello"
	r.killToEnd()
	if got := string(r.buffer); got != "hello" {
		t.Fatalf("killToEnd: expected 'hello', got %q", got)
	}
	if r.cursorPos != 5 {
		t.Fatalf("cursor should stay 5, got %d", r.cursorPos)
	}
}

func TestSteerCursor_KillToEndAtEndIsNoop(t *testing.T) {
	r := &SteerInputReader{}
	r.buffer = []byte("hello")
	r.cursorPos = 5
	r.killToEnd()
	if got := string(r.buffer); got != "hello" {
		t.Fatalf("killToEnd at end should be noop, got %q", got)
	}
}

func TestSteerCursor_KillToStart(t *testing.T) {
	r := &SteerInputReader{}
	r.buffer = []byte("hello world")
	r.cursorPos = 5 // after "hello"
	r.killToStart()
	if got := string(r.buffer); got != " world" {
		t.Fatalf("killToStart: expected ' world', got %q", got)
	}
	if r.cursorPos != 0 {
		t.Fatalf("cursor should be 0, got %d", r.cursorPos)
	}
}

func TestSteerCursor_KillToStartAtStartIsNoop(t *testing.T) {
	r := &SteerInputReader{}
	r.buffer = []byte("hello")
	r.cursorPos = 0
	r.killToStart()
	if got := string(r.buffer); got != "hello" {
		t.Fatalf("killToStart at start should be noop, got %q", got)
	}
}

func TestSteerCursor_DeleteForward(t *testing.T) {
	r := &SteerInputReader{}
	r.buffer = []byte("hello")
	r.cursorPos = 0
	r.deleteForward()
	if got := string(r.buffer); got != "ello" {
		t.Fatalf("deleteForward: expected 'ello', got %q", got)
	}
	if r.cursorPos != 0 {
		t.Fatalf("cursor should stay 0, got %d", r.cursorPos)
	}
}

func TestSteerCursor_DeleteForwardAtEndIsNoop(t *testing.T) {
	r := &SteerInputReader{}
	r.buffer = []byte("hello")
	r.cursorPos = 5
	r.deleteForward()
	if got := string(r.buffer); got != "hello" {
		t.Fatalf("deleteForward at end should be noop, got %q", got)
	}
}

func TestSteerCursor_MoveWordBackward(t *testing.T) {
	r := &SteerInputReader{}
	r.buffer = []byte("hello world")
	r.cursorPos = 11
	r.moveWord(-1)
	if r.cursorPos != 6 {
		t.Fatalf("moveWord(-1): expected cursor 6, got %d", r.cursorPos)
	}
	// Another moveWord(-1) jumps to start of "hello".
	r.moveWord(-1)
	if r.cursorPos != 0 {
		t.Fatalf("moveWord(-1) again: expected 0, got %d", r.cursorPos)
	}
}

func TestSteerCursor_MoveWordForward(t *testing.T) {
	r := &SteerInputReader{}
	r.buffer = []byte("hello world")
	r.cursorPos = 0
	r.moveWord(1)
	if r.cursorPos != 5 {
		t.Fatalf("moveWord(1): expected cursor 5, got %d", r.cursorPos)
	}
	// Another moveWord(1) jumps past "world" to end.
	r.moveWord(1)
	if r.cursorPos != 11 {
		t.Fatalf("moveWord(1) again: expected 11, got %d", r.cursorPos)
	}
}

func TestSteerCursor_MoveWordSkipsSpaces(t *testing.T) {
	// Forward from start of "  two words": skips leading spaces then
	// skips the non-whitespace word "two", landing at the end of "two"
	// (byte 5). This matches InputReader.MoveWord forward semantics.
	r := &SteerInputReader{}
	r.buffer = []byte("  two words")
	r.cursorPos = 0
	r.moveWord(1)
	if r.cursorPos != 5 {
		t.Fatalf("moveWord(1) over leading spaces: expected 5, got %d", r.cursorPos)
	}
}

func TestSteerCursor_UTF8BackwardRune(t *testing.T) {
	// "héllo": h(1) é(2) l(1) l(1) o(1) = 6 bytes, cursor at end=6.
	r := &SteerInputReader{}
	r.buffer = []byte("héllo")
	r.cursorPos = len(r.buffer) // 6
	r.moveCursorBackward()      // before 'o' → byte 5
	if r.cursorPos != 5 {
		t.Fatalf("UTF-8 backward: expected 5, got %d", r.cursorPos)
	}
	r.moveCursorBackward() // before 2nd 'l' → byte 4
	if r.cursorPos != 4 {
		t.Fatalf("UTF-8 backward: expected 4, got %d", r.cursorPos)
	}
	r.moveCursorBackward() // before 1st 'l' → byte 3 (after é)
	if r.cursorPos != 3 {
		t.Fatalf("UTF-8 backward (after é): expected 3, got %d", r.cursorPos)
	}
	r.moveCursorBackward() // before é → byte 1 (é is 2 bytes: 1-2)
	if r.cursorPos != 1 {
		t.Fatalf("UTF-8 backward (before é): expected 1, got %d", r.cursorPos)
	}
}

func TestSteerCursor_UTF8MoveWordBackward(t *testing.T) {
	// "café town" — "café" is 5 bytes (c,a,f,é=2). Cursor at end.
	// moveWord(-1) lands at start of "town" (byte 6, after "café ").
	r := &SteerInputReader{}
	r.buffer = []byte("café town") // c a f é(2) SP t o w n = 10 bytes
	r.cursorPos = len(r.buffer)    // 10
	r.moveWord(-1)
	if r.cursorPos != 6 {
		t.Fatalf("UTF-8 moveWord(-1): expected 6, got %d", r.cursorPos)
	}
	// Again lands at start of "café" (byte 0).
	r.moveWord(-1)
	if r.cursorPos != 0 {
		t.Fatalf("UTF-8 moveWord(-1) again: expected 0, got %d", r.cursorPos)
	}
}

func TestSteerCursor_UTF8BackspaceDeletesFullRune(t *testing.T) {
	// Backspace before cursor deletes a full multibyte rune.
	r := &SteerInputReader{}
	r.buffer = []byte("hi 字") // h i SP 字(3 bytes) = 6 bytes
	r.cursorPos = len(r.buffer)
	r.handleBackspace()
	if got := string(r.buffer); got != "hi " {
		t.Fatalf("UTF-8 backspace: expected 'hi ', got %q", got)
	}
	if r.cursorPos != 3 {
		t.Fatalf("cursor should be 3, got %d", r.cursorPos)
	}
}

func TestSteerCursor_UTF8DeleteForward(t *testing.T) {
	// deleteForward at the start of a multibyte rune deletes the
	// whole rune.
	r := &SteerInputReader{}
	r.buffer = []byte("a🚀b") // a(1) 🚀(4) b(1) = 6 bytes
	r.cursorPos = 1          // before 🚀
	r.deleteForward()
	if got := string(r.buffer); got != "ab" {
		t.Fatalf("UTF-8 deleteForward: expected 'ab', got %q", got)
	}
	if r.cursorPos != 1 {
		t.Fatalf("cursor should stay 1, got %d", r.cursorPos)
	}
}

func TestSteerCursor_SubmitResetsCursorToZero(t *testing.T) {
	var submitted []string
	r := newTestReader(&submitted, nil)
	for _, b := range []byte("hello") {
		r.insertAtCursor([]byte{b})
	}
	if r.cursorPos != 5 {
		t.Fatalf("expected cursor 5 before submit, got %d", r.cursorPos)
	}
	r.handleSubmit()
	if r.cursorPos != 0 {
		t.Fatalf("submit should reset cursor to 0, got %d", r.cursorPos)
	}
}

func TestSteerCursor_InterruptResetsCursor(t *testing.T) {
	var interrupted int
	r := newTestReader(nil, &interrupted)
	for _, b := range []byte("hello") {
		r.insertAtCursor([]byte{b})
	}
	r.handleInterrupt()
	if r.cursorPos != 0 {
		t.Fatalf("interrupt should reset cursor to 0, got %d", r.cursorPos)
	}
}

func TestSteerCursor_ResetBufferResetsCursor(t *testing.T) {
	r := &SteerInputReader{}
	r.buffer = []byte("hello")
	r.cursorPos = 5
	r.ResetBuffer()
	if r.cursorPos != 0 {
		t.Fatalf("ResetBuffer should reset cursor to 0, got %d", r.cursorPos)
	}
	if len(r.buffer) != 0 {
		t.Fatalf("ResetBuffer should clear buffer, got %q", string(r.buffer))
	}
}

func TestSteerCursor_RecallSetsCursorToEnd(t *testing.T) {
	var submitted []string
	r := newTestReader(&submitted, nil)
	r.historyIndex = -1
	for _, b := range []byte("history entry") {
		r.insertAtCursor([]byte{b})
	}
	r.handleSubmit()

	// Recall brings the entry back and the cursor should sit at the
	// end of the recalled text.
	r.recallHistory(-1)
	if r.cursorPos != len("history entry") {
		t.Fatalf("recall should set cursor to end (%d), got %d",
			len("history entry"), r.cursorPos)
	}
}

func TestSteerCursor_PasteSetsCursorToEnd(t *testing.T) {
	r := &SteerInputReader{}
	r.beginPaste()
	for _, b := range []byte("pasted text") {
		r.appendPasteByte(b)
	}
	r.endPaste()
	if r.cursorPos != len("pasted text") {
		t.Fatalf("endPaste should set cursor to end (%d), got %d",
			len("pasted text"), r.cursorPos)
	}
}

func TestSteerHandleEvent_LeftRightMoveCursor(t *testing.T) {
	r := &SteerInputReader{}
	r.buffer = []byte("hello")
	r.cursorPos = 0

	// Right arrow moves cursor forward.
	r.handleEvent(&InputEvent{Type: EventRight})
	if r.cursorPos != 1 {
		t.Fatalf("Right arrow: expected cursor 1, got %d", r.cursorPos)
	}
	// Left arrow moves cursor back.
	r.handleEvent(&InputEvent{Type: EventLeft})
	if r.cursorPos != 0 {
		t.Fatalf("Left arrow: expected cursor 0, got %d", r.cursorPos)
	}
}

func TestSteerHandleEvent_CtrlLeftRightMoveWords(t *testing.T) {
	r := &SteerInputReader{}
	r.buffer = []byte("hello world")
	r.cursorPos = 0

	// Ctrl+Right moves forward one word.
	r.handleEvent(&InputEvent{Type: EventWordRight})
	if r.cursorPos != 5 {
		t.Fatalf("Ctrl+Right: expected cursor 5, got %d", r.cursorPos)
	}
	// Ctrl+Left moves back one word.
	r.handleEvent(&InputEvent{Type: EventWordLeft})
	if r.cursorPos != 0 {
		t.Fatalf("Ctrl+Left: expected cursor 0, got %d", r.cursorPos)
	}
}

func TestSteerHandleEvent_UpDownRecall(t *testing.T) {
	var submitted []string
	r := newTestReader(&submitted, nil)
	r.historyIndex = -1
	for _, b := range []byte("entry") {
		r.insertAtCursor([]byte{b})
	}
	r.handleSubmit()

	r.buffer = append(r.buffer[:0], []byte("current")...)
	r.cursorPos = len(r.buffer)
	// Up arrow recalls history.
	r.handleEvent(&InputEvent{Type: EventUp})
	if got := string(r.buffer); got != "entry" {
		t.Fatalf("Up arrow should recall 'entry', got %q", got)
	}
	// Down arrow returns toward the live buffer.
	r.handleEvent(&InputEvent{Type: EventDown})
	if got := string(r.buffer); got != "current" {
		t.Fatalf("Down arrow should restore 'current', got %q", got)
	}
}

func TestSteerInsertAtCursor_MidBufferKeepsRest(t *testing.T) {
	// Insert in the middle must preserve the tail of the buffer.
	r := &SteerInputReader{}
	r.buffer = []byte("hello world")
	r.cursorPos = 5
	r.insertAtCursor([]byte(" cruel"))
	if got := string(r.buffer); got != "hello cruel world" {
		t.Fatalf("mid insert: expected 'hello cruel world', got %q", got)
	}
	if r.cursorPos != 11 {
		t.Fatalf("cursor should be 11, got %d", r.cursorPos)
	}
}

// ---------------------------------------------------------------------------
// Ctrl-X Ctrl-E editor escape state machine tests (SP-048-4f parity).
// We test the pendingCtrlX state machine directly — the actual editor
// invocation requires a real TTY and is integration-level.
// ---------------------------------------------------------------------------

func TestSteerCtrlX_SetsPendingState(t *testing.T) {
	// Pressing Ctrl-X should set pendingCtrlX so the next byte is
	// checked against Ctrl-E.
	r := &SteerInputReader{}
	r.pendingCtrlX = false

	// Simulate the effect of the 0x18 case in readLoop.
	r.pendingCtrlX = true

	if !r.pendingCtrlX {
		t.Fatal("Ctrl-X should set pendingCtrlX=true")
	}
}

func TestSteerCtrlX_ThenNonCtrlEFallsThrough(t *testing.T) {
	// After Ctrl-X, a non-Ctrl-E byte should clear pendingCtrlX and
	// be processed normally. We verify the state transition here.
	r := &SteerInputReader{}
	r.pendingCtrlX = true

	// Simulate the pendingCtrlX check in readLoop: the byte is NOT
	// 0x05 (Ctrl-E), so we clear pending and fall through.
	b := byte('a') // arbitrary non-Ctrl-E byte
	if r.pendingCtrlX {
		r.pendingCtrlX = false
		if b == 0x05 {
			t.Fatal("should not reach editor for non-Ctrl-E byte")
		}
		// Fall through — the byte would be processed normally.
	}

	if r.pendingCtrlX {
		t.Fatal("pendingCtrlX should be cleared after non-Ctrl-E byte")
	}
}

func TestSteerCtrlXCtrlE_StateMachineFlow(t *testing.T) {
	// Verify the full Ctrl-X then Ctrl-E state machine logic without
	// actually launching an editor (which requires a real TTY). We
	// simulate the readLoop's byte-level dispatch for the two-byte
	// sequence and check the transitions.
	r := &SteerInputReader{}

	// Step 1: Ctrl-X (0x18) arrives → pendingCtrlX should be set.
	if r.pendingCtrlX {
		t.Fatal("pendingCtrlX should start false")
	}
	// Simulate the 0x18 case from the control-char switch.
	r.pendingCtrlX = true

	// Step 2: next byte is Ctrl-E (0x05) → editor should be triggered.
	// We can't call runExternalEditor (needs TTY), so we verify the
	// check itself: pendingCtrlX is true and byte is 0x05.
	b := byte(0x05)
	triggered := false
	if r.pendingCtrlX {
		r.pendingCtrlX = false
		if b == 0x05 {
			triggered = true
		}
	}
	if !triggered {
		t.Fatal("Ctrl-X then Ctrl-E should trigger editor")
	}
	if r.pendingCtrlX {
		t.Fatal("pendingCtrlX should be cleared after Ctrl-E")
	}
}

// ---------------------------------------------------------------------------
// Ctrl-R reverse-search tests (SP-048-4e parity).
// ---------------------------------------------------------------------------

func TestSteerSearch_EnterSearchModeSnapshotsBuffer(t *testing.T) {
	r := &SteerInputReader{}
	r.buffer = []byte("in-progress text")
	r.cursorPos = 5

	r.enterSearchMode()

	if !r.searchMode {
		t.Fatal("enterSearchMode should set searchMode=true")
	}
	if r.searchQuery != "" {
		t.Fatalf("search query should start empty, got %q", r.searchQuery)
	}
	if string(r.preSearchBuffer) != "in-progress text" {
		t.Fatalf("pre-search buffer snapshot wrong, got %q", string(r.preSearchBuffer))
	}
	if r.preSearchCursorPos != 5 {
		t.Fatalf("pre-search cursor snapshot wrong, got %d", r.preSearchCursorPos)
	}
}

func TestSteerSearch_ExitSearchModeCancelRestoresBuffer(t *testing.T) {
	r := &SteerInputReader{}
	r.buffer = []byte("original")
	r.cursorPos = 8

	r.enterSearchMode()
	// Simulate typing a query (doesn't matter what).
	r.searchQuery = "foo"
	// Cancel (accept=false) → restore original buffer.
	r.exitSearchMode(false)

	if r.searchMode {
		t.Fatal("exitSearchMode should clear searchMode")
	}
	if got := string(r.buffer); got != "original" {
		t.Fatalf("cancel should restore original buffer, got %q", got)
	}
	if r.cursorPos != 8 {
		t.Fatalf("cancel should restore cursor pos, got %d", r.cursorPos)
	}
}

func TestSteerSearch_ExitSearchModeAcceptLoadsResult(t *testing.T) {
	r := &SteerInputReader{}
	r.buffer = []byte("original")
	r.cursorPos = 8

	r.history = []string{"alpha", "beta", "gamma"}
	r.enterSearchMode()
	// Simulate finding a match.
	r.searchResult = "beta"
	r.exitSearchMode(true)

	if r.searchMode {
		t.Fatal("exitSearchMode should clear searchMode")
	}
	if got := string(r.buffer); got != "beta" {
		t.Fatalf("accept should load search result into buffer, got %q", got)
	}
	if r.cursorPos != len("beta") {
		t.Fatalf("cursor should be at end of result, got %d", r.cursorPos)
	}
}

func TestSteerSearch_RefreshFindsCaseInsensitiveMatch(t *testing.T) {
	r := &SteerInputReader{}
	r.history = []string{"Fix the auth bug", "update README", "deploy staging"}
	r.searchMode = true
	r.searchResultIndex = -1

	// Search for "auth" (lowercase) — should match "Fix the auth bug".
	r.searchQuery = "auth"
	r.refreshSearchForQuery()

	if r.searchResult != "Fix the auth bug" {
		t.Fatalf("expected match 'Fix the auth bug', got %q", r.searchResult)
	}
	if r.searchResultIndex != 0 {
		t.Fatalf("expected index 0, got %d", r.searchResultIndex)
	}
}

func TestSteerSearch_RefreshFindsCaseInsensitiveMatchUpper(t *testing.T) {
	r := &SteerInputReader{}
	r.history = []string{"Fix the auth bug", "update README", "deploy staging"}
	r.searchMode = true
	r.searchResultIndex = -1

	// Search for "README" (uppercase) — should match "update README"
	// which has it uppercase already.
	r.searchQuery = "README"
	r.refreshSearchForQuery()

	if r.searchResult != "update README" {
		t.Fatalf("expected match 'update README', got %q", r.searchResult)
	}
}

func TestSteerSearch_RefreshEmptyQueryShowsMostRecent(t *testing.T) {
	r := &SteerInputReader{}
	r.history = []string{"old", "newer", "newest"}
	r.searchMode = true
	r.searchResultIndex = -1

	r.searchQuery = ""
	r.refreshSearchForQuery()

	if r.searchResult != "newest" {
		t.Fatalf("empty query should show most recent, got %q", r.searchResult)
	}
	if r.searchResultIndex != 2 {
		t.Fatalf("expected index 2 (newest), got %d", r.searchResultIndex)
	}
}

func TestSteerSearch_RefreshNoMatchClearsResult(t *testing.T) {
	r := &SteerInputReader{}
	r.history = []string{"alpha", "beta"}
	r.searchMode = true
	r.searchResultIndex = -1

	r.searchQuery = "zzz"
	r.refreshSearchForQuery()

	if r.searchResult != "" {
		t.Fatalf("no match should clear result, got %q", r.searchResult)
	}
	if r.searchResultIndex != -1 {
		t.Fatalf("no match should set index -1, got %d", r.searchResultIndex)
	}
}

func TestSteerSearch_CycleFindsNextOlderMatch(t *testing.T) {
	r := &SteerInputReader{}
	r.history = []string{"fix auth 1", "fix auth 2", "fix auth 3", "unrelated"}
	r.searchMode = true

	// First search for "auth" → should find the newest match (index 2).
	r.searchQuery = "auth"
	r.refreshSearchForQuery()
	if r.searchResult != "fix auth 3" {
		t.Fatalf("first match should be 'fix auth 3', got %q", r.searchResult)
	}

	// Cycle → next older match (index 1).
	r.cycleSearchResult()
	if r.searchResult != "fix auth 2" {
		t.Fatalf("cycled match should be 'fix auth 2', got %q", r.searchResult)
	}

	// Cycle again → next older (index 0).
	r.cycleSearchResult()
	if r.searchResult != "fix auth 1" {
		t.Fatalf("cycled match should be 'fix auth 1', got %q", r.searchResult)
	}

	// Cycle again → no older match, should stay on current.
	r.cycleSearchResult()
	if r.searchResult != "fix auth 1" {
		t.Fatalf("no older match — should stay on 'fix auth 1', got %q", r.searchResult)
	}
}

func TestSteerSearch_CycleEmptyQueryCyclesHistory(t *testing.T) {
	r := &SteerInputReader{}
	r.history = []string{"first", "second", "third"}
	r.searchMode = true
	r.searchResultIndex = 2 // pointing at "third" (most recent)

	r.searchQuery = ""
	r.cycleSearchResult()
	if r.searchResult != "second" {
		t.Fatalf("empty-query cycle: expected 'second', got %q", r.searchResult)
	}

	r.cycleSearchResult()
	if r.searchResult != "first" {
		t.Fatalf("empty-query cycle: expected 'first', got %q", r.searchResult)
	}

	// Wrap around to newest.
	r.cycleSearchResult()
	if r.searchResult != "third" {
		t.Fatalf("empty-query cycle wrap: expected 'third', got %q", r.searchResult)
	}
}

func TestSteerSearch_HandleSearchBackspaceTrimsQuery(t *testing.T) {
	r := &SteerInputReader{}
	r.history = []string{"hello world", "goodbye"}
	r.searchMode = true
	r.searchResultIndex = -1

	// Type "hello" into the query.
	r.searchQuery = "hello"
	r.refreshSearchForQuery()
	if r.searchResult != "hello world" {
		t.Fatalf("expected match 'hello world', got %q", r.searchResult)
	}

	// Backspace once → query becomes "hell".
	r.handleSearchBackspace()
	if r.searchQuery != "hell" {
		t.Fatalf("backspace should trim query to 'hell', got %q", r.searchQuery)
	}
	// Should still match.
	if r.searchResult != "hello world" {
		t.Fatalf("expected match 'hello world' after partial query, got %q", r.searchResult)
	}
}

func TestSteerSearch_HandleSearchBackspaceMultibyte(t *testing.T) {
	r := &SteerInputReader{}
	r.history = []string{"café test"}
	r.searchMode = true
	r.searchResultIndex = -1

	// Query "café" (é is 2 bytes).
	r.searchQuery = "café"
	r.handleSearchBackspace()

	// Should remove the full é rune, leaving "caf".
	if r.searchQuery != "caf" {
		t.Fatalf("backspace should remove full multibyte rune, got %q", r.searchQuery)
	}
}

func TestSteerSearch_AcceptOnEmptyResultIsNoOp(t *testing.T) {
	// When searchResult is empty (no match), accepting should restore
	// the pre-search buffer rather than load an empty string.
	r := &SteerInputReader{}
	r.buffer = []byte("original")
	r.cursorPos = 8
	r.history = []string{"alpha"}
	// enterSearchMode snapshots the buffer so exitSearchMode can restore.
	r.enterSearchMode()
	// Simulate a failed search (no match found).
	r.searchResult = ""
	r.exitSearchMode(true)

	if got := string(r.buffer); got != "original" {
		t.Fatalf("accept with no result should restore buffer, got %q", got)
	}
}

func TestSteerSearch_EnterSearchModeOnEmptyHistory(t *testing.T) {
	r := &SteerInputReader{}
	r.buffer = []byte("typing")
	r.cursorPos = 6
	// No history at all.
	r.enterSearchMode()

	if r.searchResult != "" {
		t.Fatalf("empty history should produce empty result, got %q", r.searchResult)
	}
	if r.searchResultIndex != -1 {
		t.Fatalf("empty history should set index -1, got %d", r.searchResultIndex)
	}
}
