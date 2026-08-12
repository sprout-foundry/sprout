package console

import (
	"os"
	"sync"
	"time"

	"golang.org/x/term"
)

// resizeSubscribers holds callbacks notified on terminal resize. Each callback
// receives the new terminal width in columns. Subscribers are notified on the
// goroutine that detected the resize (SIGWINCH handler on Unix, poll timer on
// Windows), so callbacks must not block.
var (
	resizeSubMu  sync.RWMutex
	resizeSubs   []resizeSub
	resizeSubSeq int64
)

// resizeSub is a single subscription entry.
type resizeSub struct {
	id int64
	fn func(width int)
}

// RegisterResizeSubscriber registers a callback invoked whenever the terminal
// is resized. The callback receives the new terminal width in columns.
// Returns a deregistration function — call it to unsubscribe.
func RegisterResizeSubscriber(fn func(width int)) func() {
	resizeSubMu.Lock()
	resizeSubSeq++
	id := resizeSubSeq
	resizeSubs = append(resizeSubs, resizeSub{id: id, fn: fn})
	resizeSubMu.Unlock()
	return func() {
		resizeSubMu.Lock()
		defer resizeSubMu.Unlock()
		for i, s := range resizeSubs {
			if s.id == id {
				resizeSubs = append(resizeSubs[:i], resizeSubs[i+1:]...)
				break
			}
		}
	}
}

// notifyResizeSubscribers queries the live terminal width and invokes all
// registered resize callbacks. Called from the SIGWINCH handler (Unix) or
// the poll timer (Windows). Safe to call from any goroutine.
func notifyResizeSubscribers() {
	w := liveTerminalWidth()
	resizeSubMu.RLock()
	subs := make([]resizeSub, len(resizeSubs))
	copy(subs, resizeSubs)
	resizeSubMu.RUnlock()
	for _, s := range subs {
		s.fn(w)
	}
}

// liveTerminalWidth reads the current terminal column count from stdout's fd,
// falling back to 80 when the size can't be determined (not a TTY, error).
func liveTerminalWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return 80
	}
	return w
}

// pollTerminalSize returns the current terminal width and height. Used by the
// Windows polling resize watcher to detect changes.
func pollTerminalSize() (width, height int) {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return 80, 24
	}
	return w, h
}

// startResizePoller starts the platform-specific resize detector. On Unix
// it is a no-op (SIGWINCH is already handled by the footer's watchResize
// goroutine, which calls notifyResizeSubscribers after its own Resize).
// On Windows it starts a polling timer since there is no SIGWINCH, and
// calls both onResize (the footer's Resize) and notifyResizeSubscribers.
//
// Returns a stop function that halts detection and waits for the goroutine
// to exit.
func startResizePoller(onResize func()) (stop func()) {
	return startResizeWatcher(notifyResizeSubscribers, onResize)
}

// windowsPollInterval is the interval at which Windows polls for terminal size
// changes, since SIGWINCH is unavailable. Exported as a var so tests can
// shorten it.
var windowsPollInterval = 500 * time.Millisecond
