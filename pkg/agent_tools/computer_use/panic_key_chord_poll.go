package computer_use

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

var armedChord atomic.Value

// ArmedPanicChord names the panic-key chord being watched right now ("" when
// it is disabled or the watcher could not start).
func ArmedPanicChord() string {
	chord, _ := armedChord.Load().(string)
	return chord
}

// keyReader is the platform half of the chord watcher: it names the key
// codes that satisfy a chord token and reads which keys are held.
type keyReader interface {
	// resolve maps one chord token ("ctrl", "escape", "k") to the key codes
	// that satisfy it — both sides for a modifier.
	resolve(token string) ([]int, bool)
	// held reads the keyboard once and reports, per key code, whether it is
	// down.
	held() (func(code int) bool, error)
	close()
}

const chordPollInterval = 50 * time.Millisecond

// pollingChordWatcher halts computer use when every key of the chord is held
// at once. It fires once per press: the chord has to be released before it
// can fire again.
type pollingChordWatcher struct {
	keys []string
	open func() (keyReader, error)
	// fire defaults to TriggerPanicKey.
	fire func() error

	mu      sync.Mutex
	cancel  context.CancelFunc
	stopped chan struct{}
}

func (w *pollingChordWatcher) Start(_ context.Context) error {
	if len(w.keys) == 0 {
		return nil
	}
	reader, err := w.open()
	if err != nil {
		return err
	}
	groups := make([][]int, 0, len(w.keys))
	for _, key := range w.keys {
		codes, ok := reader.resolve(key)
		if !ok {
			reader.close()
			return fmt.Errorf("panic-key chord: unknown key %q", key)
		}
		groups = append(groups, codes)
	}

	// The watch outlives the caller's start-up context (it runs for the rest
	// of the process); Stop ends it.
	loopCtx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	w.mu.Lock()
	w.cancel = cancel
	w.stopped = stopped
	w.mu.Unlock()

	go func() {
		defer close(stopped)
		defer reader.close()
		ticker := time.NewTicker(chordPollInterval)
		defer ticker.Stop()
		wasHeld := false
		for {
			select {
			case <-loopCtx.Done():
				return
			case <-ticker.C:
			}
			isDown, err := reader.held()
			if err != nil {
				continue
			}
			nowHeld := chordHeld(isDown, groups)
			if nowHeld && !wasHeld {
				log.Printf("[computer-use] panic-key chord %s pressed — halting computer use", formatChordForLog(w.keys))
				if err := w.trigger(); err != nil {
					log.Printf("[computer-use] panic key: %v", err)
				}
			}
			wasHeld = nowHeld
		}
	}()
	armedChord.Store(formatChordForLog(w.keys))
	log.Printf("[computer-use] panic-key chord %s armed", formatChordForLog(w.keys))
	return nil
}

func (w *pollingChordWatcher) trigger() error {
	if w.fire != nil {
		return w.fire()
	}
	return TriggerPanicKey("os_chord")
}

func (w *pollingChordWatcher) Stop() {
	w.mu.Lock()
	cancel, stopped := w.cancel, w.stopped
	w.cancel, w.stopped = nil, nil
	w.mu.Unlock()
	if cancel == nil {
		return
	}
	armedChord.Store("")
	cancel()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
	}
}

// chordHeld reports whether every chord key has at least one of its codes
// down.
func chordHeld(isDown func(int) bool, groups [][]int) bool {
	for _, codes := range groups {
		any := false
		for _, code := range codes {
			if isDown(code) {
				any = true
				break
			}
		}
		if !any {
			return false
		}
	}
	return true
}

// canonicalChordKey folds the spellings a chord may use for one key into the
// name the platform key tables use.
func canonicalChordKey(token string) string {
	switch token {
	case "control":
		return "ctrl"
	case "option", "opt":
		return "alt"
	case "command", "meta", "super", "win":
		return "cmd"
	case "esc":
		return "escape"
	case "return":
		return "enter"
	}
	return token
}
