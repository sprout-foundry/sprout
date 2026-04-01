package webui

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/creack/pty"
)

// helper: return a short-lived unique session ID.
func uniqueSessionID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// helper: start a PTY running a command that continuously writes to its
// stdout so the reader goroutine in monitorSession has data in flight.
// The returned cleanup function kills the process and closes the PTY.
func startNoisyPTY(t *testing.T) (ptyFile *os.File, _ <-chan struct{}, cancel func()) {
	t.Helper()
	cmd := exec.Command("bash", "-c", "while true; do echo race-test-data; sleep 0.001; done")
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Skipf("skipping: cannot start PTY (maybe no bash available): %v", err)
	}
	ch := make(chan struct{})
	go func() {
		<-ch
		cmd.Process.Kill()
		cmd.Wait()
		f.Close()
	}()
	return f, ch, func() { close(ch) }
}

// helper: build a TerminalSession from an externally-created PTY without going
// through the full CreateSession codepath, so we can control the command.
func rawSessionFromPTY(ptyFile *os.File, id string) *TerminalSession {
	return &TerminalSession{
		ID:          id,
		Pty:         ptyFile,
		Output:      ptyFile,
		Cancel:      func() {},
		Active:      true,
		LastUsed:    time.Now(),
		OutputCh:    make(chan []byte, 10000),
		Size:        &pty.Winsize{Rows: 24, Cols: 80},
		TmuxBacked:  false,
		monitorDone: make(chan struct{}),
	}
}

// reclaimOutput drains any remaining data from OutputCh (which is closed by
// monitorSession) so that the monitor goroutine fully finishes.
func reclaimOutput(t *testing.T, sess *TerminalSession, timeout time.Duration) {
	t.Helper()
	if sess.OutputCh == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		for range sess.OutputCh {
			// drain
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Log("timed out draining OutputCh")
	}
}

// --------------------------------------------------------------------------
// Test 1: Rapid detach on a session with data in flight – stress test
// --------------------------------------------------------------------------

func TestDetachWhilePTYActive_Race(t *testing.T) {
	// Use multiple goroutines to increase the chance of exposing the race.
	oldProcs := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(oldProcs)

	t.Setenv("TERM", "xterm-256color")

	dir, err := os.MkdirTemp("", "ledit-terminal-race-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	tm := NewTerminalManager(dir)

	const iterations = 100
	panicCount := int32(0)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			id := uniqueSessionID(fmt.Sprintf("detach-race-%d", idx))

			// Use CreateSession which, on Unix without tmux, creates a raw
			// PTY session via createUnixSession.
			tm.mutex.Lock()
			tm.tmuxAvailable = false // force raw PTY path
			tm.mutex.Unlock()

			sess, err := tm.CreateSession(id)
			if err != nil {
				// If the shell doesn't exist, skip this iteration.
				if strings.Contains(err.Error(), "no suitable shell") ||
					strings.Contains(err.Error(), "no such file") {
					t.Logf("iteration %d: skipping, no shell: %v", idx, err)
					return
				}
				oops := recover()
				if oops != nil {
					mu.Lock()
					panicCount++
					mu.Unlock()
					t.Errorf("iteration %d panicked: %v", idx, oops)
				}
				return
			}

			// Give the monitor time to start reading data.
			time.Sleep(time.Millisecond)

			// Rapidly close – this triggers close(monitorDone) which races
			// with the reader goroutine trying to send on OutputCh.
			defer func() {
				if r := recover(); r != nil {
					mu.Lock()
					panicCount++
					mu.Unlock()
					t.Errorf("iteration %d panicked on close: %v", idx, r)
				}
			}()

			_ = tm.CloseSession(id)

			// Drain the output channel so monitorSession's deferred close
			// does not deadlock.
			reclaimOutput(t, sess, 2*time.Second)
		}(i)
	}

	wg.Wait()

	if panicCount > 0 {
		t.Fatalf("%d of %d iterations panicked (send on closed channel?)", panicCount, iterations)
	}
	t.Logf("all %d iterations completed without panic", iterations)
}

// --------------------------------------------------------------------------
// Test 2: Write data to PTY then detach while reader processes it
// --------------------------------------------------------------------------

func TestDetachDoesNotPanicWithBufferedOutput(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")

	dir, err := os.MkdirTemp("", "ledit-terminal-race-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	tm := NewTerminalManager(dir)

	// Force raw (non-tmux) path.
	tm.mutex.Lock()
	tm.tmuxAvailable = false
	tm.mutex.Unlock()

	id := uniqueSessionID("buffered-output")

	sess, err := tm.CreateSession(id)
	if err != nil {
		t.Skipf("skipping: cannot create session: %v", err)
	}

	// Write a bunch of data to the PTY so the reader goroutine has bytes to
	// process.  We write more than a single 1024-byte Read buffer to make
	// sure there is a steady stream of data.
	for i := 0; i < 200; i++ {
		_, err := sess.Pty.Write([]byte(fmt.Sprintf("echo line-%04d\n", i)))
		if err != nil {
			break // PTY might have been closed already
		}
	}

	// Small delay to let the reader goroutine pick up some data.
	time.Sleep(5 * time.Millisecond)

	// Now close the session — monitorDone closes, but the reader goroutine
	// may still be in the middle of a send on OutputCh.
	recovered := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				recovered = true
				t.Errorf("panic during CloseSession: %v", r)
			}
		}()
		_ = tm.CloseSession(id)
	}()

	if recovered {
		t.Fatal("CloseSession panicked — race condition NOT fixed")
	}

	// Drain the output channel so the monitor goroutine can fully exit.
	reclaimOutput(t, sess, 3*time.Second)
	t.Log("no panic observed with buffered output")
}

// --------------------------------------------------------------------------
// Test 3: CloseSession while data is actively flowing
// --------------------------------------------------------------------------

func TestCloseSessionDoesNotPanic(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")

	dir, err := os.MkdirTemp("", "ledit-terminal-race-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	const iterations = 50

	oldProcs := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(oldProcs)

	panicCount := int32(0)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			tm := NewTerminalManager(dir)
			tm.mutex.Lock()
			tm.tmuxAvailable = false
			tm.mutex.Unlock()

			id := uniqueSessionID(fmt.Sprintf("close-race-%d", idx))
			sess, err := tm.CreateSession(id)
			if err != nil {
				if strings.Contains(err.Error(), "no suitable shell") ||
					strings.Contains(err.Error(), "no such file") {
					return
				}
				t.Errorf("iteration %d: %v", idx, err)
				return
			}

			// Fire data into the PTY to keep the reader busy.
			for j := 0; j < 50; j++ {
				sess.Pty.Write([]byte(fmt.Sprintf("echo x-%d\n", j)))
			}
			time.Sleep(time.Millisecond)

			recovered := false
			func() {
				defer func() {
					if r := recover(); r != nil {
						recovered = true
						mu.Lock()
						panicCount++
						mu.Unlock()
						t.Errorf("iteration %d panicked on CloseSession: %v", idx, r)
					}
				}()
				_ = tm.CloseSession(id)
			}()
			if !recovered {
				reclaimOutput(t, sess, 2*time.Second)
			}
		}(i)
	}

	wg.Wait()

	if panicCount > 0 {
		t.Fatalf("%d of %d iterations panicked on CloseSession", panicCount, iterations)
	}
	t.Logf("all %d CloseSession iterations completed without panic", iterations)
}

// --------------------------------------------------------------------------
// Test 4: Explicit monitorSession test with controlled PTY
// --------------------------------------------------------------------------

func TestMonitorSession_ClosedBeforeReaderStops_NoPanic(t *testing.T) {
	// This test directly exercises monitorSession with a noisy PTY so the
	// reader goroutine has plenty of data to process when we close monitorDone.
	t.Setenv("TERM", "xterm-256color")

	ptyFile, ptyDone, ptyCancel := startNoisyPTY(t)
	defer ptyCancel()

	_ = ptyDone
	tm := NewTerminalManager(os.TempDir())
	id := uniqueSessionID("monitor-direct")

	sess := rawSessionFromPTY(ptyFile, id)

	// Start the monitor in a goroutine.
	var monitorExited atomic.Bool
	go func() {
		tm.monitorSession(sess)
		monitorExited.Store(true)
	}()

	// Let the reader process some data.
	time.Sleep(10 * time.Millisecond)

	// Close monitorDone — this is what Detach/CloseSession does.
	// The fix ensures monitorSession waits for the reader goroutine to stop
	// before closing OutputCh.
	close(sess.monitorDone)

	// Drain OutputCh (closed by monitorSession) to let the monitor return.
	reclaimOutput(t, sess, 3*time.Second)

	if !monitorExited.Load() {
		t.Fatalf("monitorSession did not exit within the timeout")
	}

	t.Log("monitorSession exited cleanly after monitorDone was closed")
}

// --------------------------------------------------------------------------
// Test 5: Tmux detach race (if tmux is available)
// --------------------------------------------------------------------------

func TestDetachFromTmuxSession_NoPanic(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")

	dir, err := os.MkdirTemp("", "ledit-terminal-race-tmux-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	tm := NewTerminalManager(dir)
	if !tm.IsTmuxAvailable() {
		t.Skip("tmux not available, skipping tmux detach race test")
	}

	const iterations = 20
	var wg sync.WaitGroup
	panicCount := int32(0)
	var mu sync.Mutex

	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			// Use a separate TerminalManager per goroutine to avoid racing on
			// the shared sessions map.
			iterDir, err := os.MkdirTemp(dir, fmt.Sprintf("iter-%d-*", idx))
			if err != nil {
				t.Errorf("iteration %d: failed to create temp dir: %v", idx, err)
				return
			}
			defer os.RemoveAll(iterDir)

			iterTM := NewTerminalManager(iterDir)
			if !iterTM.IsTmuxAvailable() {
				t.Errorf("iteration %d: tmux not available", idx)
				return
			}

			id := uniqueSessionID(fmt.Sprintf("tmux-detach-%d", idx))
			sess, err := iterTM.createTmuxSession(id)
			if err != nil {
				t.Errorf("iteration %d: failed to create tmux session: %v", idx, err)
				return
			}

			// Write data into the tmux session to generate output.
			for j := 0; j < 20; j++ {
				sess.Pty.Write([]byte(fmt.Sprintf("echo tmux-line-%d\n", j)))
			}
			time.Sleep(5 * time.Millisecond)

			recovered := false
			func() {
				defer func() {
					if r := recover(); r != nil {
						recovered = true
						mu.Lock()
						panicCount++
						mu.Unlock()
						t.Errorf("iteration %d panicked: %v", idx, r)
					}
				}()
				_ = iterTM.DetachFromSession(id)
			}()
			if !recovered {
				reclaimOutput(t, sess, 3*time.Second)
			}

			// Clean up: close the tmux session.
			tmuxName := iterTM.TmuxSessionName(id)
			killCmd := exec.Command("tmux", "kill-session", "-t", tmuxName)
			_ = killCmd.Run()
		}(i)
	}

	wg.Wait()

	if panicCount > 0 {
		t.Fatalf("%d of %d tmux detach iterations panicked", panicCount, iterations)
	}
	t.Logf("all %d tmux detach iterations completed without panic", iterations)
}

// --------------------------------------------------------------------------
// Test 6: ReattachSession replaces OutputCh while old monitor is still running
// --------------------------------------------------------------------------

func TestReattachOutputChannel_NoDoubleClose(t *testing.T) {
	// This test reproduces the "close of closed channel" panic that
	// occurs when ReattachSession replaces session.OutputCh while the
	// old monitorSession goroutine is still running. Without the fix,
	// both the old and new monitor defers try to close the same channel.
	t.Setenv("TERM", "xterm-256color")

	ptyFile, ptyDone, ptyCancel := startNoisyPTY(t)
	defer ptyCancel()

	_ = ptyDone
	tm := NewTerminalManager(os.TempDir())
	id := uniqueSessionID("reattach-race")

	sess := rawSessionFromPTY(ptyFile, id)

	// Start the "old" monitor goroutine (M1).
	var m1Exited atomic.Bool
	go func() {
		tm.monitorSession(sess)
		m1Exited.Store(true)
	}()

	// Let M1 read some data, and receive one message to establish
	// happens-before between M1's initial field captures (including
	// session.OutputCh and session.monitorDone) and our subsequent
	// field replacement below.
	time.Sleep(10 * time.Millisecond)
	select {
	case <-sess.OutputCh:
	default:
	}

	// Simulate what ReattachSession does:
	// 1. Signal M1 to stop by closing monitorDone.
	select {
	case <-sess.monitorDone:
	default:
		close(sess.monitorDone)
	}

	// 2. Replace channels with new ones (this is what ReattachSession does
	//    after stopping the old monitor, but the old monitor may not have
	//    fully exited yet).
	//    Save old channel before replacing (read before write — same goroutine,
	//    no race).
	oldOutputCh := sess.OutputCh
	sess.OutputCh = make(chan []byte, 10000)
	sess.monitorDone = make(chan struct{})

	// 3. Start the "new" monitor goroutine (M2) immediately.
	var m2Exited atomic.Bool
	go func() {
		tm.monitorSession(sess)
		m2Exited.Store(true)
	}()

	// Drain both output channels so both monitors can fully exit.
	// The old channel (oldOutputCh) will be closed by M1's defer.
	// The new channel (sess.OutputCh) will be closed by M2's defer.
	reclaimOutputCh := func(ch chan []byte, timeout time.Duration) {
		done := make(chan struct{})
		go func() {
			for range ch {
				// drain
			}
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(timeout):
			t.Log("timed out draining channel")
		}
	}

	reclaimOutputCh(oldOutputCh, 3*time.Second)

	// Signal M2 to stop and drain its output channel.
	select {
	case <-sess.monitorDone:
	default:
		close(sess.monitorDone)
	}
	reclaimOutputCh(sess.OutputCh, 3*time.Second)

	if !m1Exited.Load() {
		t.Fatal("M1 (old monitor) did not exit within the timeout")
	}
	if !m2Exited.Load() {
		t.Fatal("M2 (new monitor) did not exit within the timeout")
	}

	t.Log("both monitors exited cleanly — no double-close panic")
}
