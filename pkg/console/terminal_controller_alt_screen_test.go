package console

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeTerminalManager struct {
	width       int
	height      int
	mu          sync.Mutex
	enterCount  int
	exitCount   int
	enterCh     chan struct{}
	exitCh      chan struct{}
	resizeHooks []func(int, int)
	rawMode     bool
	rawHistory  []bool
	resetCount  int
	cursorMoves [][2]int
	scrollCalls [][2]int
}

func newFakeTerminalManager(width, height int) *fakeTerminalManager {
	return &fakeTerminalManager{
		width:   width,
		height:  height,
		enterCh: make(chan struct{}, 10),
		exitCh:  make(chan struct{}, 10),
	}
}

func (f *fakeTerminalManager) Init() error                { return nil }
func (f *fakeTerminalManager) Cleanup() error             { return nil }
func (f *fakeTerminalManager) GetSize() (int, int, error) { return f.width, f.height, nil }
func (f *fakeTerminalManager) OnResize(cb func(int, int)) { f.resizeHooks = append(f.resizeHooks, cb) }
func (f *fakeTerminalManager) SetRawMode(enabled bool) error {
	f.mu.Lock()
	f.rawMode = enabled
	f.rawHistory = append(f.rawHistory, enabled)
	f.mu.Unlock()
	return nil
}

func (f *fakeTerminalManager) IsRawMode() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rawMode
}

func (f *fakeTerminalManager) MoveCursor(x, y int) error {
	f.mu.Lock()
	f.cursorMoves = append(f.cursorMoves, [2]int{x, y})
	f.mu.Unlock()
	return nil
}
func (f *fakeTerminalManager) SaveCursor() error         { return nil }
func (f *fakeTerminalManager) RestoreCursor() error      { return nil }
func (f *fakeTerminalManager) HideCursor() error         { return nil }
func (f *fakeTerminalManager) ShowCursor() error         { return nil }
func (f *fakeTerminalManager) ClearScreen() error        { return nil }
func (f *fakeTerminalManager) ClearScrollback() error    { return nil }
func (f *fakeTerminalManager) ClearLine() error          { return nil }
func (f *fakeTerminalManager) ClearToEndOfLine() error   { return nil }
func (f *fakeTerminalManager) ClearToEndOfScreen() error { return nil }
func (f *fakeTerminalManager) EnterAltScreen() error {
	f.mu.Lock()
	f.enterCount++
	f.mu.Unlock()
	select {
	case f.enterCh <- struct{}{}:
	default:
	}
	return nil
}
func (f *fakeTerminalManager) ExitAltScreen() error {
	f.mu.Lock()
	f.exitCount++
	f.mu.Unlock()
	select {
	case f.exitCh <- struct{}{}:
	default:
	}
	return nil
}
func (f *fakeTerminalManager) EnableMouseReporting() error  { return nil }
func (f *fakeTerminalManager) DisableMouseReporting() error { return nil }
func (f *fakeTerminalManager) SetScrollRegion(top, bottom int) error {
	f.mu.Lock()
	f.scrollCalls = append(f.scrollCalls, [2]int{top, bottom})
	f.mu.Unlock()
	return nil
}

func (f *fakeTerminalManager) ResetScrollRegion() error {
	f.mu.Lock()
	f.resetCount++
	f.mu.Unlock()
	return nil
}
func (f *fakeTerminalManager) ScrollUp(int) error              { return nil }
func (f *fakeTerminalManager) ScrollDown(int) error            { return nil }
func (f *fakeTerminalManager) Write(b []byte) (int, error)     { return len(b), nil }
func (f *fakeTerminalManager) WriteText(s string) (int, error) { return len(s), nil }
func (f *fakeTerminalManager) WriteAt(int, int, []byte) error  { return nil }
func (f *fakeTerminalManager) Flush() error                    { return nil }

func (f *fakeTerminalManager) waitForEnter(t *testing.T) {
	t.Helper()
	if err := waitForSignal(f.enterCh, 200*time.Millisecond); err != nil {
		t.Fatalf("expected enter alt screen signal: %v", err)
	}
}

func (f *fakeTerminalManager) waitForExit(t *testing.T) {
	t.Helper()
	if err := waitForSignal(f.exitCh, 200*time.Millisecond); err != nil {
		t.Fatalf("expected exit alt screen signal: %v", err)
	}
}

func waitForSignal(ch <-chan struct{}, timeout time.Duration) error {
	select {
	case <-ch:
		return nil
	case <-time.After(timeout):
		return errors.New("timeout")
	}
}

func waitForDepth(t *testing.T, tc *TerminalController, want int) {
	t.Helper()
	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		tc.mu.RLock()
		depth := tc.altScreenRefCount
		tc.mu.RUnlock()
		if depth == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	tc.mu.RLock()
	depth := tc.altScreenRefCount
	tc.mu.RUnlock()
	t.Fatalf("timed out waiting for alt screen depth %d (got %d)", want, depth)
}

func TestTerminalControllerAltScreenRefCounting(t *testing.T) {
	tm := newFakeTerminalManager(80, 24)
	eventBus := NewEventBus(16)
	tc := NewTerminalController(tm, eventBus)

	if err := tc.Init(); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	t.Cleanup(func() {
		tc.Cleanup()
	})

	if err := tc.EnterAltScreen(); err != nil {
		t.Fatalf("enter alt screen: %v", err)
	}
	tm.waitForEnter(t)
	waitForDepth(t, tc, 1)

	if err := tc.EnterAltScreen(); err != nil {
		t.Fatalf("enter alt screen (nested): %v", err)
	}
	tm.waitForEnter(t)
	waitForDepth(t, tc, 2)

	if err := tc.ExitAltScreen(); err != nil {
		t.Fatalf("exit alt screen: %v", err)
	}
	tm.waitForExit(t)
	waitForDepth(t, tc, 1)

	if err := tc.ExitAltScreen(); err != nil {
		t.Fatalf("exit alt screen (final): %v", err)
	}
	tm.waitForExit(t)
	waitForDepth(t, tc, 0)

	if err := tc.ExitAltScreen(); err != nil {
		t.Fatalf("exit alt screen (underflow): %v", err)
	}
	tm.waitForExit(t)
	waitForDepth(t, tc, 0)
}

func TestTerminalControllerRawModeReferenceCounting(t *testing.T) {
	tm := newFakeTerminalManager(80, 24)
	eventBus := NewEventBus(16)
	tc := NewTerminalController(tm, eventBus)

	if err := tc.Init(); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	t.Cleanup(func() {
		tc.Cleanup()
	})

	if tm.IsRawMode() {
		t.Fatalf("expected raw mode to be disabled initially")
	}

	release, err := tc.AcquireRawMode("test")
	if err != nil {
		t.Fatalf("acquire raw mode: %v", err)
	}
	if !tm.IsRawMode() {
		t.Fatalf("expected raw mode enabled after acquire")
	}

	if err := tc.SetRawMode(false); err != nil {
		t.Fatalf("set raw mode base false: %v", err)
	}
	if !tm.IsRawMode() {
		t.Fatalf("expected raw mode to remain enabled while reference held")
	}

	release()
	if tm.IsRawMode() {
		t.Fatalf("expected raw mode disabled after release with base false")
	}

	// Ensure release is idempotent
	release()

	if err := tc.SetRawMode(true); err != nil {
		t.Fatalf("set raw mode base true: %v", err)
	}
	if !tm.IsRawMode() {
		t.Fatalf("expected raw mode enabled after base true")
	}

	release2, err := tc.AcquireRawMode("second")
	if err != nil {
		t.Fatalf("second acquire raw mode: %v", err)
	}
	if !tm.IsRawMode() {
		t.Fatalf("expected raw mode to stay enabled with base true")
	}

	release2()
	if !tm.IsRawMode() {
		t.Fatalf("expected raw mode to remain enabled after releasing when base true")
	}

	if err := tc.SetRawMode(false); err != nil {
		t.Fatalf("final set raw mode base false: %v", err)
	}
	if tm.IsRawMode() {
		t.Fatalf("expected raw mode disabled after base false and no refs")
	}

	tm.mu.Lock()
	history := append([]bool(nil), tm.rawHistory...)
	tm.mu.Unlock()
	if len(history) == 0 {
		t.Fatalf("expected raw mode transitions to be recorded")
	}
}
