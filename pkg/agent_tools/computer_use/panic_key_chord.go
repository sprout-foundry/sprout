package computer_use

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
)

// ChordWatcher watches the OS for a keyboard chord and triggers the panic
// key when detected. Implementations live in platform-specific files:
//
//   - panic_key_chord_darwin.go (macOS, osascript-based)
//   - panic_key_chord_linux.go  (Linux, xdotool-based)
//   - panic_key_chord_other.go  (stub for all other platforms)
type ChordWatcher interface {
	// Start begins watching for the chord. The watcher should call
	// TriggerPanicKey("os_chord") when the chord is detected. Returns
	// an error if the watcher cannot start (e.g. xdotool not installed).
	// Start is best-effort: callers should log the error and continue.
	Start(ctx context.Context) error
	// Stop halts the watcher and releases any resources. Safe to call
	// even if Start was never called or failed.
	Stop()
}

// NewChordWatcher returns a platform-specific ChordWatcher for the given
// chord string (e.g. "ctrl+shift+escape"). The chord is parsed into a list
// of required keys (modifiers + main key). When chord is "disabled", the
// returned watcher is a no-op whose Start/Stop never do anything.
//
// The parser is permissive: keys are lowercased and trimmed. Unknown keys
// are kept as-is so the platform-specific matcher can decide. Modifiers
// ("ctrl", "shift", "alt", "meta"/"cmd"/"super") are separated from the
// main key.
func NewChordWatcher(chord string) ChordWatcher {
	keys := parseChord(chord)
	return newPlatformWatcher(keys)
}

// parseChord splits a chord string like "ctrl+shift+escape" into a slice of
// key tokens: ["ctrl", "shift", "escape"]. Case-insensitive. Returns nil
// when chord is empty or "disabled".
func parseChord(chord string) []string {
	chord = strings.TrimSpace(strings.ToLower(chord))
	if chord == "" || chord == "disabled" {
		return nil
	}
	parts := strings.Split(chord, "+")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// IsChordDisabled reports whether the chord string is the explicit-off
// sentinel "disabled". Used by the registration code to skip starting
// the watcher entirely.
func IsChordDisabled(chord string) bool {
	return strings.TrimSpace(strings.ToLower(chord)) == "disabled"
}

// chordWatcherMu protects a package-level "currently active" watcher so
// re-registration doesn't leak goroutines.
var (
	watcherMu    sync.Mutex
	chordWatcher ChordWatcher
)

// SetActiveChordWatcher swaps the active watcher, stopping any previous
// one. Returns the previously-active watcher (for tests).
func SetActiveChordWatcher(w ChordWatcher) ChordWatcher {
	watcherMu.Lock()
	prev := chordWatcher
	chordWatcher = w
	watcherMu.Unlock()
	// Stop is called outside the lock on purpose: Stop() may block (waiting for a
	// goroutine to exit) and we don't want to hold the package mutex during it.
	// `prev` is captured under the lock above so we stop the correct watcher
	// even if another caller swaps in a third watcher between Unlock() and
	// Stop().
	if prev != nil {
		prev.Stop()
	}
	return prev
}

// ActiveChordWatcher returns the currently registered watcher (nil if none).
func ActiveChordWatcher() ChordWatcher {
	watcherMu.Lock()
	defer watcherMu.Unlock()
	return chordWatcher
}

// TriggerPanicKeyFromChord is called by a ChordWatcher when the OS chord is
// detected. It is a thin wrapper around TriggerPanicKey with a fixed reason
// of "os_chord" so audit logs can distinguish OS-chord triggers from
// programmatic ones.
func TriggerPanicKeyFromChord() error {
	return TriggerPanicKey("os_chord")
}

// formatChordForLog returns a stable human-readable representation of the
// parsed chord keys for log lines.
func formatChordForLog(keys []string) string {
	if len(keys) == 0 {
		return "<none>"
	}
	return strings.Join(keys, "+")
}

// errMissingHelper returns a friendly error string for callers that don't
// install xdotool/osascript.
func errMissingHelper(tool string) error {
	return fmt.Errorf("chord watcher requires %s to be installed and on $PATH; install it or set computer_use.panic_key_chord = \"disabled\"", tool)
}

// GOOSName returns runtime.GOOS, exposed via this package to keep
// platform-specific files independent of import duplication.
func GOOSName() string {
	return runtime.GOOS
}
