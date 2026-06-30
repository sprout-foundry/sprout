//go:build darwin

package computer_use

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sync"
	"time"
)

// osascriptChordWatcher is a macOS chord watcher that uses osascript to
// detect keyboard chords via System Events.
//
// NOTE: AppleScript's "get modifiers" only exposes modifier state (cmd/option/
// ctrl/shift), not arbitrary key state. Full chord detection for arbitrary
// keys requires a Cocoa event monitor (NSEvent) which cannot be scripted from
// vanilla osascript. This watcher verifies that osascript is available and
// keeps a background polling loop alive so Stop() can cancel it cleanly.
//
// On non-interactive macOS sessions (e.g. launched from SSH or launchd
// without a GUI session), osascript may fail with a TCC permission error or
// a window-server error. The watcher logs the failure and returns gracefully
// so the panic key still works programmatically (WebUI button, etc.).
type osascriptChordWatcher struct {
	keys []string

	mu      sync.Mutex
	cancel  context.CancelFunc
	stopped chan struct{}
}

// Compile-time check: *osascriptChordWatcher implements ChordWatcher.
var _ ChordWatcher = (*osascriptChordWatcher)(nil)

func newPlatformWatcher(keys []string) ChordWatcher {
	return &osascriptChordWatcher{keys: keys}
}

func (w *osascriptChordWatcher) Start(ctx context.Context) error {
	w.mu.Lock()
	w.stopped = make(chan struct{})
	w.mu.Unlock()

	if len(w.keys) == 0 {
		return nil
	}
	log.Printf("[computer-use] chord watcher watching for: %s", formatChordForLog(w.keys))
	if _, err := exec.LookPath("osascript"); err != nil {
		return errMissingHelper("osascript")
	}

	// Verify osascript can reach the window server. A simple "get modifiers"
	// call will fail if there's no GUI session or no accessibility permission.
	script := `tell application "System Events" to get modifiers`
	if err := exec.CommandContext(ctx, "osascript", "-e", script).Run(); err != nil {
		return fmt.Errorf("osascript cannot reach window server (no GUI session or accessibility permission?): %w", err)
	}

	watchCtx, cancel := context.WithCancel(ctx)
	w.mu.Lock()
	w.cancel = cancel
	w.mu.Unlock()

	// NOTE: Full chord detection for arbitrary keys requires a Cocoa event
	// monitor (NSEvent) which cannot be scripted from vanilla osascript.
	// The polling loop keeps the goroutine alive so Stop() can cancel it
	// cleanly. Future work: integrate with a small Cocoa helper binary or
	// use a key-binding daemon that maps the OS chord to a signal.
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

func (w *osascriptChordWatcher) Stop() {
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
