package console

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// TestSelectList_ClearRenderedNoDeadlockWithFooter is a regression test for
// a re-entrant outputMu self-deadlock in the picker's teardown.
// clearRendered already holds outputMu when it repaints the footer, and it
// used to call f.Refresh() → draw() → LockOutput(). Go's sync.Mutex is
// non-reentrant, so the picker's teardown goroutine wedged forever: the
// picker never returned, the terminal was left in steer mode, and the REPL
// prompt was never drawn. Every keystroke then echoed from the tty's own
// line discipline and Enter only moved to a new line — the CLI looked
// completely dead.
//
// The test drives clearRendered directly (the picker's defer calls it) and
// fails on a timeout instead of hanging the suite.
func TestSelectList_ClearRenderedNoDeadlockWithFooter(t *testing.T) {
	var buf bytes.Buffer
	f := NewStatusFooter(&buf, &stubSource{model: "test"})
	f.isTTY = true
	f.active = true
	f.sizeOverride = &terminalSizeOverride{cols: 80, rows: 24}
	RegisterGlobalStatusFooter(f)
	t.Cleanup(func() { RegisterGlobalStatusFooter(nil) })

	s := NewSelectList(SelectListOptions{
		Title:  "Recent sessions in this workspace",
		Items:  []SelectItem{{Label: "one", Value: "1"}},
		Footer: "↑↓ select · enter resume",
	})
	s.footerDriven = true
	s.renderedWidths = []int{40}

	done := make(chan struct{})
	go func() {
		s.clearRendered()
		close(done)
	}()

	select {
	case <-done:
		// success — no deadlock
	case <-time.After(2 * time.Second):
		t.Fatal("clearRendered deadlocked with a footer-driven picker (re-entrant outputMu)")
	}
}

// TestSelectList_ClearRenderedRepaintsFooter pins the behaviour the
// deadlock fix had to preserve: with a footer-driven picker the footer
// still repaints itself on teardown, so the hint row it lent the picker
// does not survive as stale chrome.
func TestSelectList_ClearRenderedRepaintsFooter(t *testing.T) {
	var buf bytes.Buffer
	f := NewStatusFooter(&buf, &stubSource{model: "test"})
	f.isTTY = true
	f.active = true
	f.sizeOverride = &terminalSizeOverride{cols: 80, rows: 24}
	RegisterGlobalStatusFooter(f)
	t.Cleanup(func() { RegisterGlobalStatusFooter(nil) })

	s := NewSelectList(SelectListOptions{
		Title:  "Recent sessions",
		Items:  []SelectItem{{Label: "one", Value: "1"}},
		Footer: "↑↓ select · enter resume",
	})
	s.footerDriven = true
	s.renderedWidths = []int{40}

	s.clearRendered()

	out := buf.String()
	// 24 rows, 2 reserved (rule + content) → the region ends on row 22.
	if !strings.Contains(out, "\033[1;22r") {
		t.Fatalf("footer did not re-assert its scroll region on picker teardown; output=%q", out)
	}
	if !strings.Contains(out, "test") {
		t.Fatalf("footer did not repaint its content row on picker teardown; output=%q", out)
	}
}
