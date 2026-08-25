//go:build !darwin && !linux

package computer_use

import (
	"context"
	"log"
	"sync/atomic"
)

var noopLogged atomic.Bool

// noopChordWatcher is the stub for unsupported platforms (Windows, *BSD,
// WASM, etc.). It logs once at startup and provides inert Start/Stop.
type noopChordWatcher struct {
	keys []string
}

// Compile-time check: *noopChordWatcher implements ChordWatcher.
var _ ChordWatcher = (*noopChordWatcher)(nil)

func newPlatformWatcher(keys []string) ChordWatcher {
	return &noopChordWatcher{keys: keys}
}

func (w *noopChordWatcher) Start(ctx context.Context) error {
	if !noopLogged.Swap(true) {
		log.Printf("[computer-use] panic key chord watcher is not supported on this platform (%s); use the WebUI Stop button or programmatic TriggerPanicKey() instead", GOOSName())
	}
	return nil
}

func (w *noopChordWatcher) Stop() {}
