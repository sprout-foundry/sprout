//go:build windows
// +build windows

package console

import (
	"sync"
	"time"
)

// startResizeWatcher polls term.GetSize on a timer on Windows, since there is
// no SIGWINCH equivalent. When the terminal width or height changes, it calls
// onFooter first, then onSubscribers (same as the Unix SIGWINCH path).
//
// The poll interval is governed by windowsPollInterval (500ms default).
//
// Returns a stop function that halts the poll timer and waits for the
// goroutine to exit. Safe to call the stop function multiple times.
func startResizeWatcher(onSubscribers func(), onFooter func()) (stop func()) {
	stopCh := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)
		var lastW, lastH int
		ticker := time.NewTicker(windowsPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				w, h := pollTerminalSize()
				if w != lastW || h != lastH {
					lastW = w
					lastH = h
					if onFooter != nil {
						onFooter()
					}
					if onSubscribers != nil {
						onSubscribers()
					}
				}
			case <-stopCh:
				return
			}
		}
	}()

	var stopOnce sync.Once
	return func() {
		stopOnce.Do(func() {
			close(stopCh)
			<-done
		})
	}
}
