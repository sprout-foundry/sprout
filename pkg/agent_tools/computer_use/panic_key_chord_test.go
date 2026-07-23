//go:build !windows && !js

package computer_use

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

// -----------------------------------------------------------------------
// parseChord tests
// -----------------------------------------------------------------------

func TestParseChord_BasicChord(t *testing.T) {
	got := parseChord("ctrl+shift+escape")
	want := []string{"ctrl", "shift", "escape"}
	if len(got) != len(want) {
		t.Fatalf("parseChord() = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("got[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestParseChord_Uppercase(t *testing.T) {
	got := parseChord("CTRL+SHIFT+ESCAPE")
	want := []string{"ctrl", "shift", "escape"}
	if len(got) != len(want) {
		t.Fatalf("parseChord() = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("got[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestParseChord_Disabled(t *testing.T) {
	if got := parseChord("disabled"); got != nil {
		t.Errorf("parseChord(\"disabled\") = %v, want nil", got)
	}
	if got := parseChord(""); got != nil {
		t.Errorf("parseChord(\"\") = %v, want nil", got)
	}
}

func TestParseChord_WithSpaces(t *testing.T) {
	got := parseChord("ctrl + shift + escape")
	want := []string{"ctrl", "shift", "escape"}
	if len(got) != len(want) {
		t.Fatalf("parseChord() = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("got[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestParseChord_MultiPlatform(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"cmd+space", []string{"cmd", "space"}},
		{"super+x", []string{"super", "x"}},
		{"alt+tab", []string{"alt", "tab"}},
		{"ctrl+shift", []string{"ctrl", "shift"}},
		{" meta + alt + f ", []string{"meta", "alt", "f"}},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseChord(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("parseChord(%q) = %v, want %v", tt.input, got, tt.want)
			}
			for i, w := range tt.want {
				if got[i] != w {
					t.Errorf("got[%d] = %q, want %q", i, got[i], w)
				}
			}
		})
	}
}

// -----------------------------------------------------------------------
// IsChordDisabled tests
// -----------------------------------------------------------------------

func TestIsChordDisabled(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"disabled", true},
		{"DISABLED", true},
		{" Disabled ", true},
		{"ctrl+shift+escape", false},
		{"", false},
		{"ctrl+break", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := IsChordDisabled(tt.input)
			if got != tt.want {
				t.Errorf("IsChordDisabled(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// -----------------------------------------------------------------------
// ChordWatcher registry tests
// -----------------------------------------------------------------------

// stoppableWatcher is a test double for ChordWatcher that tracks Start/Stop calls.
type stoppableWatcher struct {
	keys      []string
	started   bool
	stopped   bool
	startMu   sync.Mutex
	stopMu    sync.Mutex
	startCh   chan struct{}
	stoppedCh chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
}

func newStoppableWatcher(keys []string) *stoppableWatcher {
	return &stoppableWatcher{
		keys:      keys,
		startCh:   make(chan struct{}),
		stoppedCh: make(chan struct{}),
	}
}

func (w *stoppableWatcher) Start(ctx context.Context) error {
	w.startMu.Lock()
	w.started = true
	w.startMu.Unlock()
	w.startOnce.Do(func() { close(w.startCh) })
	return nil
}

func (w *stoppableWatcher) Stop() {
	w.stopMu.Lock()
	w.stopped = true
	w.stopMu.Unlock()
	w.stopOnce.Do(func() { close(w.stoppedCh) })
}

func TestChordWatcherRegistry_SetAndGet(t *testing.T) {
	// Save and restore the active watcher.
	prev := SetActiveChordWatcher(nil)
	defer SetActiveChordWatcher(prev)

	w := newStoppableWatcher([]string{"ctrl", "shift"})
	SetActiveChordWatcher(w)

	got := ActiveChordWatcher()
	if got != w {
		t.Errorf("ActiveChordWatcher() = %v, want %v", got, w)
	}
}

func TestChordWatcherRegistry_ReplaceStopsPrevious(t *testing.T) {
	// Save and restore the active watcher.
	prev := SetActiveChordWatcher(nil)
	defer SetActiveChordWatcher(prev)

	first := newStoppableWatcher([]string{"ctrl", "shift"})
	SetActiveChordWatcher(first)

	second := newStoppableWatcher([]string{"alt", "tab"})
	replaced := SetActiveChordWatcher(second)

	if replaced != first {
		t.Errorf("SetActiveChordWatcher returned %v, want first watcher", replaced)
	}
	if !first.stopped {
		t.Error("first watcher should have been stopped after replacement")
	}
	if ActiveChordWatcher() != second {
		t.Errorf("ActiveChordWatcher() = %v, want second", ActiveChordWatcher())
	}
}

func TestChordWatcherRegistry_NilSafe(t *testing.T) {
	// Save and restore the active watcher.
	prev := SetActiveChordWatcher(nil)
	defer SetActiveChordWatcher(prev)

	// Clear the registry.
	SetActiveChordWatcher(nil)

	// First set should return nil (no previous watcher to stop).
	w := newStoppableWatcher([]string{"ctrl"})
	replaced := SetActiveChordWatcher(w)
	if replaced != nil {
		t.Errorf("first SetActiveChordWatcher returned %v, want nil", replaced)
	}

	// ActiveChordWatcher should return the new watcher.
	if ActiveChordWatcher() != w {
		t.Errorf("ActiveChordWatcher() = %v, want %v", ActiveChordWatcher(), w)
	}
}

// -----------------------------------------------------------------------
// TriggerPanicKeyFromChord tests
// -----------------------------------------------------------------------

func TestTriggerPanicKeyFromChord_UsesOsChordReason(t *testing.T) {
	restore := saveHooks()
	t.Cleanup(restore)

	dir := t.TempDir()
	mock := &MockBackend{}

	// Build chain: auditing(panicable(mock))
	panicable := NewPanicableBackend(mock)
	ab, err := NewAuditingBackend(panicable, dir, "chord-reason-test")
	if err != nil {
		t.Fatalf("NewAuditingBackend: %v", err)
	}

	// Set the global backend so RecordSafetyEvent routes to the auditing layer.
	prevBackend := GetBackend()
	SetBackend(ab)
	t.Cleanup(func() { SetBackend(prevBackend) })

	// Trigger via the chord wrapper.
	if err := TriggerPanicKeyFromChord(); err != nil {
		t.Fatalf("TriggerPanicKeyFromChord() = %v", err)
	}

	// Verify the backend is halted.
	if !panicable.IsHalted() {
		t.Fatal("backend should be halted after TriggerPanicKeyFromChord")
	}

	// Verify the halt reason is "os_chord".
	if got := panicable.HaltReason(); got != "os_chord" {
		t.Errorf("HaltReason() = %q, want %q", got, "os_chord")
	}

	// Close the audit writer so the file is flushed.
	if err := ab.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Read the JSONL log and look for the panic_key_triggered event with reason "os_chord".
	path := filepath.Join(dir, "chord-reason-test.jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()

	found := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec AuditRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("bad jsonl line: %v", err)
		}
		if rec.Action == "panic_key_triggered" {
			found = true
			if rec.Args["reason"] != "os_chord" {
				t.Errorf("panic_key_triggered reason = %v, want 'os_chord'", rec.Args["reason"])
			}
		}
	}
	if !found {
		t.Error("panic_key_triggered event not found in audit log")
	}
}

func TestTriggerPanicKeyFromChord_NoBackend(t *testing.T) {
	restore := saveHooks()
	t.Cleanup(restore)

	// Clear the global backend.
	prevGlobal := globalPanicableBackend
	globalPanicableBackend = nil
	t.Cleanup(func() { globalPanicableBackend = prevGlobal })

	// Should be a no-op.
	err := TriggerPanicKeyFromChord()
	if err != nil {
		t.Errorf("TriggerPanicKeyFromChord() = %v, want nil (no-op)", err)
	}
}

// -----------------------------------------------------------------------
// formatChordForLog tests
// -----------------------------------------------------------------------

func TestFormatChordForLog(t *testing.T) {
	tests := []struct {
		keys []string
		want string
	}{
		{[]string{"ctrl", "shift", "escape"}, "ctrl+shift+escape"},
		{[]string{}, "<none>"},
		{nil, "<none>"},
		{[]string{"alt"}, "alt"},
	}
	for _, tt := range tests {
		got := formatChordForLog(tt.keys)
		if got != tt.want {
			t.Errorf("formatChordForLog(%v) = %q, want %q", tt.keys, got, tt.want)
		}
	}
}

// -----------------------------------------------------------------------
// errMissingHelper tests
// -----------------------------------------------------------------------

func TestErrMissingHelper(t *testing.T) {
	err := errMissingHelper("xdotool")
	if err == nil {
		t.Fatal("errMissingHelper() returned nil")
	}
	got := err.Error()
	if got != "chord watcher requires xdotool to be installed and on $PATH; install it or set computer_use.panic_key_chord = \"disabled\"" {
		t.Errorf("errMissingHelper() = %q, want friendly error message", got)
	}
}

// -----------------------------------------------------------------------
// GOOSName tests
// -----------------------------------------------------------------------

func TestGOOSName(t *testing.T) {
	got := GOOSName()
	if got != runtime.GOOS {
		t.Errorf("GOOSName() = %q, want %q", got, runtime.GOOS)
	}
}

// -----------------------------------------------------------------------
// Platform-specific watcher tests
// -----------------------------------------------------------------------

func TestNoOpWatcher_StartStopNoPanic(t *testing.T) {
	// On non-darwin/non-linux platforms, the noop watcher is used.
	// On darwin/linux, the platform watcher may fail to start (no xdotool/osascript)
	// but should not panic.
	w := NewChordWatcher("ctrl+shift+escape")

	// Start should not panic.
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Start() panicked: %v", r)
			}
		}()
		err := w.Start(context.Background())
		// Error is acceptable (no xdotool/osascript installed in test env).
		if err != nil {
			t.Logf("Start() returned expected error: %v", err)
		}
	}()

	// Stop should not panic.
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Stop() panicked: %v", r)
			}
		}()
		w.Stop()
	}()
}

func TestDisabledChordWatcher_NoOp(t *testing.T) {
	// A disabled chord should produce a watcher whose Start/Stop are no-ops.
	w := NewChordWatcher("disabled")

	err := w.Start(context.Background())
	if err != nil {
		t.Errorf("Start() on disabled chord = %v, want nil", err)
	}
	w.Stop() // should not panic
}

func TestEmptyChordWatcher_NoOp(t *testing.T) {
	// An empty chord string should produce a watcher whose Start/Stop are no-ops.
	w := NewChordWatcher("")

	err := w.Start(context.Background())
	if err != nil {
		t.Errorf("Start() on empty chord = %v, want nil", err)
	}
	w.Stop() // should not panic
}

// -----------------------------------------------------------------------
// NewChordWatcher integration test
// -----------------------------------------------------------------------

func TestNewChordWatcher_ReturnsNonNil(t *testing.T) {
	w := NewChordWatcher("ctrl+shift+escape")
	if w == nil {
		t.Fatal("NewChordWatcher() returned nil")
	}
}

func TestNewChordWatcher_DisabledReturnsNonNil(t *testing.T) {
	w := NewChordWatcher("disabled")
	if w == nil {
		t.Fatal("NewChordWatcher(\"disabled\") returned nil — should be a no-op watcher")
	}
}

// -----------------------------------------------------------------------
// ChordWatcher interface compliance (compile-time checks)
// -----------------------------------------------------------------------

// These are compile-time checks that ensure the platform-specific watcher
// types satisfy the ChordWatcher interface. The actual type depends on the
// build platform, so we test the result of newPlatformWatcher.
func TestChordWatcher_InterfaceConformance(t *testing.T) {
	// newPlatformWatcher is the platform-specific constructor.
	// Verify it returns a ChordWatcher (compile-time check).
	var _ ChordWatcher = newPlatformWatcher([]string{"ctrl", "shift"})
}

// -----------------------------------------------------------------------
// Concurrent registry safety
// -----------------------------------------------------------------------

func TestChordWatcherRegistry_ConcurrentAccess(t *testing.T) {
	// Save and restore the active watcher.
	prev := SetActiveChordWatcher(nil)
	defer SetActiveChordWatcher(prev)

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			w := newStoppableWatcher([]string{"ctrl"})
			SetActiveChordWatcher(w)
			// Read should not panic.
			_ = ActiveChordWatcher()
		}(i)
	}
	wg.Wait()

	// Final state should have exactly one watcher.
	if ActiveChordWatcher() == nil {
		t.Error("ActiveChordWatcher() is nil after concurrent sets")
	}
}

// -----------------------------------------------------------------------
// StoppableWatcher self-test (ensure the test double works)
// -----------------------------------------------------------------------

func TestStoppableWatcher_Basic(t *testing.T) {
	w := newStoppableWatcher([]string{"ctrl"})

	// Start should succeed and signal startCh.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	select {
	case <-w.startCh:
		// expected
	default:
		t.Error("startCh was not closed after Start()")
	}

	// Stop should signal stoppedCh.
	w.Stop()
	select {
	case <-w.stoppedCh:
		// expected
	default:
		t.Error("stoppedCh was not closed after Stop()")
	}
}
