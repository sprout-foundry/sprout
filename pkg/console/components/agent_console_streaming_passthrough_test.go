package components

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/console"
)

type streamingPassthroughTerminal struct {
	mu           sync.Mutex
	width        int
	height       int
	rawMode      bool
	enterCount   int
	exitCount    int
	resetCount   int
	scrollTop    int
	scrollBottom int
}

func newStreamingPassthroughTerminal(width, height int) *streamingPassthroughTerminal {
	return &streamingPassthroughTerminal{width: width, height: height}
}

func (tm *streamingPassthroughTerminal) Init() error                     { return nil }
func (tm *streamingPassthroughTerminal) Cleanup() error                  { return nil }
func (tm *streamingPassthroughTerminal) GetSize() (int, int, error)      { return tm.width, tm.height, nil }
func (tm *streamingPassthroughTerminal) OnResize(func(int, int))         {}
func (tm *streamingPassthroughTerminal) SaveCursor() error               { return nil }
func (tm *streamingPassthroughTerminal) RestoreCursor() error            { return nil }
func (tm *streamingPassthroughTerminal) HideCursor() error               { return nil }
func (tm *streamingPassthroughTerminal) ShowCursor() error               { return nil }
func (tm *streamingPassthroughTerminal) ClearScreen() error              { return nil }
func (tm *streamingPassthroughTerminal) ClearScrollback() error          { return nil }
func (tm *streamingPassthroughTerminal) ClearLine() error                { return nil }
func (tm *streamingPassthroughTerminal) ClearToEndOfLine() error         { return nil }
func (tm *streamingPassthroughTerminal) ClearToEndOfScreen() error       { return nil }
func (tm *streamingPassthroughTerminal) ScrollUp(int) error              { return nil }
func (tm *streamingPassthroughTerminal) ScrollDown(int) error            { return nil }
func (tm *streamingPassthroughTerminal) Write(b []byte) (int, error)     { return len(b), nil }
func (tm *streamingPassthroughTerminal) WriteText(s string) (int, error) { return len(s), nil }
func (tm *streamingPassthroughTerminal) WriteAt(int, int, []byte) error  { return nil }
func (tm *streamingPassthroughTerminal) Flush() error                    { return nil }
func (tm *streamingPassthroughTerminal) EnableMouseReporting() error     { return nil }
func (tm *streamingPassthroughTerminal) DisableMouseReporting() error    { return nil }

func (tm *streamingPassthroughTerminal) SetRawMode(enabled bool) error {
	tm.mu.Lock()
	tm.rawMode = enabled
	tm.mu.Unlock()
	return nil
}

func (tm *streamingPassthroughTerminal) IsRawMode() bool {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.rawMode
}

func (tm *streamingPassthroughTerminal) MoveCursor(x, y int) error {
	tm.mu.Lock()
	// store last cursor position implicitly via scroll region bounds if needed
	tm.mu.Unlock()
	return nil
}

func (tm *streamingPassthroughTerminal) SetScrollRegion(top, bottom int) error {
	tm.mu.Lock()
	tm.scrollTop = top
	tm.scrollBottom = bottom
	tm.mu.Unlock()
	return nil
}

func (tm *streamingPassthroughTerminal) ResetScrollRegion() error {
	tm.mu.Lock()
	tm.resetCount++
	tm.mu.Unlock()
	return nil
}

func (tm *streamingPassthroughTerminal) EnterAltScreen() error {
	tm.mu.Lock()
	tm.enterCount++
	tm.mu.Unlock()
	return nil
}

func (tm *streamingPassthroughTerminal) ExitAltScreen() error {
	tm.mu.Lock()
	tm.exitCount++
	tm.mu.Unlock()
	return nil
}

func waitForCondition(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAgentConsole_StreamingPassthroughRestoresTerminalState(t *testing.T) {
	tm := newStreamingPassthroughTerminal(80, 24)
	eventBus := console.NewEventBus(32)
	if err := eventBus.Start(); err != nil {
		t.Fatalf("event bus start failed: %v", err)
	}
	t.Cleanup(func() {
		_ = eventBus.Stop()
	})

	controller := console.NewTerminalController(tm, eventBus)
	if err := controller.Init(); err != nil {
		t.Fatalf("controller init failed: %v", err)
	}
	t.Cleanup(func() {
		controller.Cleanup()
	})

	config := &AgentConsoleConfig{HistoryFile: "", Prompt: "> "}
	ac := NewAgentConsole(nil, config)
	deps := console.Dependencies{
		Terminal:   tm,
		Controller: controller,
		Layout:     ac.autoLayoutManager,
		Events:     eventBus,
		State:      console.NewStateManager(),
	}

	if err := ac.Init(context.Background(), deps); err != nil {
		t.Fatalf("agent console init failed: %v", err)
	}

	ac.inputManager.mutex.Lock()
	ac.inputManager.running = true
	ac.inputManager.ensureRawModeLocked(rawModeOwnerInputManager)
	ac.inputManager.mutex.Unlock()
	defer ac.inputManager.Stop()

	if !waitForCondition(func() bool { return controller.IsRawMode() }, 100*time.Millisecond) {
		t.Fatalf("expected controller to enter raw mode after input manager setup")
	}

	if err := controller.EnterAltScreen(); err != nil {
		t.Fatalf("enter alt screen failed: %v", err)
	}
	if !waitForCondition(func() bool { return controller.IsAltScreen() }, 200*time.Millisecond) {
		t.Fatalf("expected controller to report alt screen active")
	}

	ac.isStreaming = true
	ac.streamingFormatter.Write("Streaming line one\n")
	ac.streamingFormatter.Write("Streaming line two\n")
	ac.streamingFormatter.ForceFlush()

	if err := controller.WithPrimaryScreen(func() error {
		ac.inputManager.SetPassthroughMode(true)
		// simulate brief interactive activity
		time.Sleep(10 * time.Millisecond)
		ac.inputManager.SetPassthroughMode(false)
		return nil
	}); err != nil {
		t.Fatalf("with primary screen failed: %v", err)
	}

	if !waitForCondition(func() bool { return controller.IsAltScreen() }, 200*time.Millisecond) {
		t.Fatalf("expected controller to restore alt screen state after passthrough")
	}
	if !waitForCondition(func() bool { return controller.IsRawMode() }, 200*time.Millisecond) {
		t.Fatalf("expected controller to reacquire raw mode after passthrough")
	}

	tm.mu.Lock()
	enterCount := tm.enterCount
	exitCount := tm.exitCount
	resetCount := tm.resetCount
	tm.mu.Unlock()

	if exitCount == 0 {
		t.Fatalf("expected passthrough to trigger alt screen exit")
	}
	if enterCount < 2 {
		t.Fatalf("expected alt screen to be re-entered, got %d entries", enterCount)
	}
	if resetCount == 0 {
		t.Fatalf("expected scroll region reset during passthrough")
	}
}
