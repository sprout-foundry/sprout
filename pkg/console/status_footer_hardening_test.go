package console

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestStatusFooter_StopDoesNotHangWhenWatcherStuck is a regression test for
// the unbounded `<-doneCh` wait in Stop(). If the SIGWINCH watcher goroutine
// is blocked (e.g. on outputMu behind a wedged PTY write), Stop() — which
// also runs from signal handlers via StopGlobalStatusFooter right before
// os.Exit — must give up after a bounded wait instead of deadlocking
// shutdown. Mirrors the bounded wait ActivityIndicator.Stop has always had.
func TestStatusFooter_StopDoesNotHangWhenWatcherStuck(t *testing.T) {
	f := NewStatusFooter(&nonTTYWriter{}, &stubSource{model: "test"})
	f.isTTY = true
	f.mu.Lock()
	f.active = true
	// Simulate a started watcher whose goroutine never closes doneCh.
	f.winchStop = make(chan struct{})
	f.winchDone = make(chan struct{})
	f.mu.Unlock()

	done := make(chan struct{})
	go func() {
		f.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop() hung waiting for a stuck SIGWINCH watcher goroutine")
	}

	f.mu.Lock()
	stillActive := f.active
	f.mu.Unlock()
	if stillActive {
		t.Fatal("Stop() returned without marking the footer inactive")
	}
}

// TestStatusFooter_ClearSteerLineUnderConcurrentRefresh is a concurrency
// smoke test for the outputMu wrapping of ClearSteerLine's multi-step ANSI
// sequence (region reset → row blanks → re-apply). Before the lock, a
// concurrent Refresh/draw could interleave mid-sequence and corrupt the
// scroll region. With the lock, the two writers serialize; we assert no
// panic/deadlock under contention.
func TestStatusFooter_ClearSteerLineUnderConcurrentRefresh(t *testing.T) {
	f := NewStatusFooter(&bytes.Buffer{}, &stubSource{model: "test"})
	f.isTTY = true
	f.mu.Lock()
	f.active = true
	f.sizeOverride = &terminalSizeOverride{cols: 80, rows: 24}
	f.steerActive = true
	f.steerLine = "⇄ steer › hello"
	f.lastSteerRows = 1
	f.mu.Unlock()

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				f.Refresh()
			}
		}
	}()

	cleared := make(chan struct{})
	go func() {
		defer close(cleared)
		f.ClearSteerLine()
	}()

	select {
	case <-cleared:
	case <-time.After(3 * time.Second):
		t.Fatal("ClearSteerLine deadlocked against concurrent Refresh")
	}
	close(stop)
	wg.Wait()
}

// TestFooterTooltipShow_NoMarginScroll is a regression test for the Alt+T
// tooltip clobbering its own rows. The first rendered line lands exactly on
// the scroll region's bottom margin (rows-2 with the footer's 2 reserved
// rows); a trailing \n there scrolls the entire region, shifting the line
// up so the next absolute-addressed write overwrites it — the header
// vanished and the conversation shifted on every Alt+T. The fix drops the
// \n: rows are absolutely addressed and don't need it.
func TestFooterTooltipShow_NoMarginScroll(t *testing.T) {
	var buf bytes.Buffer
	tt := NewFooterTooltip(&buf)
	tt.Timeout = 0 // no auto-dismiss goroutine
	tt.Source = func() []ToolInvocation {
		return []ToolInvocation{
			{Name: "read_file", Count: 3, TotalTokens: 1200},
			{Name: "shell_command", Count: 1, TotalTokens: 300},
		}
	}

	tt.Show(80, 24)

	out := buf.String()
	if !strings.Contains(out, "TOOL") {
		t.Fatal("tooltip did not render its header row")
	}
	if !strings.Contains(out, "read_file") || !strings.Contains(out, "shell_command") {
		t.Fatal("tooltip did not render per-tool rows")
	}
	// Row writes are absolutely positioned via \033[<row>;1H and must not
	// emit newlines: a \n after content at the bottom-margin row is the
	// exact regression (margin scroll shifts + overwrites).
	if strings.Count(out, "\n") != 0 {
		t.Fatalf("tooltip row writes must not emit newlines (margin scroll); got %d: %q",
			strings.Count(out, "\n"), out)
	}
	tt.Hide()
	if tt.Visible() {
		t.Fatal("Hide did not clear visibility")
	}
}
