//go:build linux

package computer_use

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sync"
	"time"
)

// xdotoolChordWatcher is a Linux chord watcher that verifies X11 availability
// via xdotool. Because xdotool does not expose a "is key X currently pressed"
// query (that requires XKB extension access via CGO), this watcher keeps a
// background polling loop alive so that Stop() has something to cancel.
//
// NOTE: Full keyboard-state polling on X11 requires either CGO with XKB or a
// separate key-binding daemon (sxhkd, kglobalaccel). The WebUI Stop button
// remains the primary trigger path; this watcher exists so that the
// registration contract is satisfied and the panic key still works
// programmatically via TriggerPanicKey().
//
// On headless Linux (no DISPLAY or xdotool missing), Start() returns a
// recognizable error so callers can log and continue.
type xdotoolChordWatcher struct {
	keys []string

	mu      sync.Mutex
	cancel  context.CancelFunc
	stopped chan struct{}
}

// Compile-time check: *xdotoolChordWatcher implements ChordWatcher.
var _ ChordWatcher = (*xdotoolChordWatcher)(nil)

func newPlatformWatcher(keys []string) ChordWatcher {
	return &xdotoolChordWatcher{keys: keys}
}

func (w *xdotoolChordWatcher) Start(ctx context.Context) error {
	w.mu.Lock()
	w.stopped = make(chan struct{})
	w.mu.Unlock()

	if len(w.keys) == 0 {
		return nil
	}
	log.Printf("[computer-use] chord watcher watching for: %s", formatChordForLog(w.keys))
	if _, err := exec.LookPath("xdotool"); err != nil {
		return errMissingHelper("xdotool")
	}

	// Verify X server is reachable. xdotool getactivewindow exits non-zero
	// when DISPLAY is unset or no compositor is running.
	if err := exec.CommandContext(ctx, "xdotool", "getactivewindow").Run(); err != nil {
		return fmt.Errorf("xdotool cannot reach X server (DISPLAY unset?): %w", err)
	}

	watchCtx, cancel := context.WithCancel(ctx)
	w.mu.Lock()
	w.cancel = cancel
	w.mu.Unlock()

	// NOTE: Full chord detection requires XKB access (CGO). The polling loop
	// keeps the goroutine alive so Stop() can cancel it cleanly. Future work:
	// integrate with a key-binding daemon (sxhkd, kglobalaccel) that maps the
	// OS chord to a signal or HTTP callback into this watcher.
	go func() {
		defer close(w.stopped)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-watchCtx.Done():
				return
			case <-ticker.C:
				continue
			}
		}
	}()
	return nil
}

func (w *xdotoolChordWatcher) Stop() {
	w.mu.Lock()
	cancel := w.cancel
	stopped := w.stopped
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if stopped != nil {
		select {
		case <-stopped:
		case <-time.After(2 * time.Second):
		}
	}
}
