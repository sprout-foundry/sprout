package console

import (
	"strings"
	"testing"
)

// --- SP-078 Phase 2: shared completion cycle -------------------------------

func TestCompletionCycle_FreshCycleAppliesFirstCandidate(t *testing.T) {
	cycle := &CompletionCycle{}
	calls := 0
	completer := func(line string, cursorPos int) []string {
		calls++
		return []string{"/model", "/mode"}
	}
	line, pos, ok := CycleCompletion(cycle, "/mo", 3, completer)
	if !ok {
		t.Fatalf("expected ok=true on first call")
	}
	if line != "/model" {
		t.Fatalf("expected first candidate /model, got %q", line)
	}
	if pos != len("/model") {
		t.Fatalf("expected cursor at end of candidate, got pos=%d", pos)
	}
	if calls != 1 {
		t.Fatalf("expected one completer call, got %d", calls)
	}
}

func TestCompletionCycle_AdvancesWhenBufferMatchesLastApplied(t *testing.T) {
	cycle := &CompletionCycle{}
	completer := func(line string, cursorPos int) []string {
		return []string{"/model", "/mode"}
	}
	// First call: applies /model, advances not yet recorded.
	CycleCompletion(cycle, "/mo", 3, completer)
	cycle.Advance("/model")
	// Second call with the same buffer: advances to /mode.
	line, _, ok := CycleCompletion(cycle, "/model", 6, completer)
	if !ok {
		t.Fatalf("expected ok=true on cycle advance")
	}
	if line != "/mode" {
		t.Fatalf("expected /mode after one cycle, got %q", line)
	}
}

func TestCompletionCycle_WrapsAround(t *testing.T) {
	cycle := &CompletionCycle{}
	completer := func(line string, cursorPos int) []string {
		return []string{"/a", "/b"}
	}
	CycleCompletion(cycle, "", 0, completer)
	cycle.Advance("/a")
	CycleCompletion(cycle, "/a", 2, completer) // → /b
	cycle.Advance("/b")
	line, _, _ := CycleCompletion(cycle, "/b", 2, completer) // → wraps to /a
	if line != "/a" {
		t.Fatalf("expected wrap to /a, got %q", line)
	}
}

func TestCompletionCycle_NoCandidatesIsNoOp(t *testing.T) {
	cycle := &CompletionCycle{}
	completer := func(line string, cursorPos int) []string {
		return nil
	}
	line, pos, ok := CycleCompletion(cycle, "/zz", 3, completer)
	if ok {
		t.Fatalf("expected ok=false on empty candidates")
	}
	if line != "/zz" || pos != 3 {
		t.Fatalf("expected unchanged buffer, got line=%q pos=%d", line, pos)
	}
}

func TestCompletionCycle_NilCompleterIsNoOp(t *testing.T) {
	cycle := &CompletionCycle{}
	line, pos, ok := CycleCompletion(cycle, "anything", 8, nil)
	if ok || line != "anything" || pos != 8 {
		t.Fatalf("nil completer must be a silent no-op, got line=%q pos=%d ok=%v", line, pos, ok)
	}
}

func TestCompletionCycle_NilCycleIsNoOp(t *testing.T) {
	completer := func(line string, cursorPos int) []string {
		return []string{"/x"}
	}
	line, pos, ok := CycleCompletion(nil, "/", 1, completer)
	if ok || line != "/" || pos != 1 {
		t.Fatalf("nil cycle must be a silent no-op, got line=%q pos=%d ok=%v", line, pos, ok)
	}
}

func TestCompletionCycle_EditResetsCycle(t *testing.T) {
	cycle := &CompletionCycle{}
	completer := func(line string, cursorPos int) []string {
		return []string{"/a", "/b"}
	}
	// Apply "/a" and record lastApplied.
	CycleCompletion(cycle, "", 0, completer)
	cycle.Advance("/a")
	// User edits buffer — caller resets cycle so next press starts fresh.
	cycle.Reset()
	// Next press against the new buffer applies /a (first candidate)
	// and advances lastApplied to /a; subsequent press against /a
	// advances to /b.
	line, _, _ := CycleCompletion(cycle, "/b", 2, completer)
	if line != "/a" {
		t.Fatalf("after Reset: expected fresh first candidate /a, got %q", line)
	}
	cycle.Advance("/a")
	line, _, _ = CycleCompletion(cycle, "/a", 2, completer)
	if line != "/b" {
		t.Fatalf("after advance: expected /b, got %q", line)
	}
}

// --- SteerInputReader completion (SP-078 Phase 2) -------------------------

func newTestReaderWithCompleter(c CompletionProvider) *SteerInputReader {
	return &SteerInputReader{
		fd:          -1,
		submitFn:    func(string) {},
		queueFn:     func(string) {},
		interruptFn: func() {},
		completer:   c,
	}
}

func TestSteerInputReader_Completion_NoCompleterIsNoOp(t *testing.T) {
	var submitted []string
	var interrupted int
	r := newTestReader(&submitted, &interrupted)
	r.insertAtCursor([]byte("/mo"))
	r.handleSteerCompletion()
	if got := string(r.buffer); got != "/mo" {
		t.Fatalf("no completer installed should leave buffer unchanged, got %q", got)
	}
}

func TestSteerInputReader_Completion_AppliesFirstCandidate(t *testing.T) {
	r := newTestReaderWithCompleter(func(line string, cursorPos int) []string {
		return []string{"/model", "/mode"}
	})
	r.insertAtCursor([]byte("/mo"))
	r.handleSteerCompletion()
	if got := string(r.buffer); got != "/model" {
		t.Fatalf("expected buffer /model, got %q", got)
	}
	if r.cursorPos != len("/model") {
		t.Fatalf("expected cursor at end /model, got %d", r.cursorPos)
	}
}

func TestSteerInputReader_Completion_CyclesOnRepeatedPress(t *testing.T) {
	r := newTestReaderWithCompleter(func(line string, cursorPos int) []string {
		return []string{"/model", "/mode"}
	})
	r.insertAtCursor([]byte("/mo"))
	r.handleSteerCompletion()
	if got := string(r.buffer); got != "/model" {
		t.Fatalf("first press: expected /model, got %q", got)
	}
	r.handleSteerCompletion()
	if got := string(r.buffer); got != "/mode" {
		t.Fatalf("second press: expected /mode, got %q", got)
	}
}

func TestSteerInputReader_Completion_EditResetsCycle(t *testing.T) {
	r := newTestReaderWithCompleter(func(line string, cursorPos int) []string {
		return []string{"/model", "/mode"}
	})
	r.insertAtCursor([]byte("/mo"))
	r.handleSteerCompletion()
	if got := string(r.buffer); got != "/model" {
		t.Fatalf("first press: expected /model, got %q", got)
	}
	// User edits: insert another character → cycle must reset.
	r.insertAtCursor([]byte("d")) // buffer is now "/modeld"
	r.handleSteerCompletion()     // fresh cycle: applies /model again
	if got := string(r.buffer); got != "/model" {
		t.Fatalf("after edit + completion: expected /model, got %q", got)
	}
}

func TestSteerInputReader_Completion_NoCandidatesIsSilent(t *testing.T) {
	r := newTestReaderWithCompleter(func(line string, cursorPos int) []string {
		return nil
	})
	r.insertAtCursor([]byte("/zz"))
	r.handleSteerCompletion()
	if got := string(r.buffer); got != "/zz" {
		t.Fatalf("no candidates must leave buffer unchanged, got %q", got)
	}
}

func TestSteerInputReader_Completion_BackspaceResetsCycle(t *testing.T) {
	r := newTestReaderWithCompleter(func(line string, cursorPos int) []string {
		return []string{"/model", "/mode"}
	})
	r.insertAtCursor([]byte("/mo"))
	r.handleSteerCompletion()
	if got := string(r.buffer); got != "/model" {
		t.Fatalf("first press: expected /model, got %q", got)
	}
	r.handleBackspace() // removes the trailing 'l' → "/mode"
	if got := string(r.buffer); got != "/mode" {
		t.Fatalf("after backspace: expected /mode, got %q", got)
	}
	r.handleSteerCompletion() // fresh cycle: applies /model
	if got := string(r.buffer); got != "/model" {
		t.Fatalf("after edit + completion: expected /model, got %q", got)
	}
}

func TestSteerInputReader_Completion_BufferEditAtBoundary(t *testing.T) {
	// A 200-char buffer followed by Ctrl-] should still cycle correctly.
	r := newTestReaderWithCompleter(func(line string, cursorPos int) []string {
		if strings.HasPrefix(line, "/") && len(line) <= 10 {
			return []string{"/alpha", "/beta"}
		}
		return nil
	})
	r.buffer = make([]byte, 200)
	for i := range r.buffer {
		r.buffer[i] = 'x'
	}
	r.cursorPos = 200
	r.handleSteerCompletion()
	// No candidates for 200 'x's — buffer unchanged.
	if r.cursorPos != 200 {
		t.Fatalf("expected cursor unchanged, got %d", r.cursorPos)
	}
	if len(r.buffer) != 200 {
		t.Fatalf("expected buffer unchanged (200 bytes), got %d", len(r.buffer))
	}
}

func TestSteerInputReader_SetCompleter_ClearsCycle(t *testing.T) {
	r := newTestReaderWithCompleter(func(line string, cursorPos int) []string {
		return []string{"/model", "/mode"}
	})
	r.insertAtCursor([]byte("/mo"))
	r.handleSteerCompletion()
	if got := string(r.buffer); got != "/model" {
		t.Fatalf("setup: expected /model, got %q", got)
	}
	// Replace completer with a different one. The cycle should reset so
	// the next press uses the new completer against the current buffer.
	r.SetCompleter(func(line string, cursorPos int) []string {
		return []string{"/foo", "/bar"}
	})
	r.handleSteerCompletion()
	// Buffer is "/model" — new completer with /m prefix matches /model
	// and /mode, neither matches /model itself, but it does return both
	// /model and /mode (lowercase /m prefix matches both). First press
	// applies the first: /model. But our completer here only knows
	// /foo and /bar, so it should return those.
	if got := string(r.buffer); got != "/foo" {
		t.Fatalf("after SetCompleter + completion: expected /foo, got %q", got)
	}
}
