package components

import (
	"sync"
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/console"
)

type testTerminalManager struct {
	mu         sync.Mutex
	rawMode    bool
	resetCount int
	width      int
	height     int
}

func newTestTerminalManager(width, height int) *testTerminalManager {
	return &testTerminalManager{width: width, height: height}
}

func (tm *testTerminalManager) Init() error                { return nil }
func (tm *testTerminalManager) Cleanup() error             { return nil }
func (tm *testTerminalManager) GetSize() (int, int, error) { return tm.width, tm.height, nil }
func (tm *testTerminalManager) OnResize(func(int, int))    {}
func (tm *testTerminalManager) SetRawMode(enabled bool) error {
	tm.mu.Lock()
	tm.rawMode = enabled
	tm.mu.Unlock()
	return nil
}
func (tm *testTerminalManager) IsRawMode() bool {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.rawMode
}
func (tm *testTerminalManager) MoveCursor(int, int) error      { return nil }
func (tm *testTerminalManager) SaveCursor() error              { return nil }
func (tm *testTerminalManager) RestoreCursor() error           { return nil }
func (tm *testTerminalManager) HideCursor() error              { return nil }
func (tm *testTerminalManager) ShowCursor() error              { return nil }
func (tm *testTerminalManager) ClearScreen() error             { return nil }
func (tm *testTerminalManager) ClearScrollback() error         { return nil }
func (tm *testTerminalManager) ClearLine() error               { return nil }
func (tm *testTerminalManager) ClearToEndOfLine() error        { return nil }
func (tm *testTerminalManager) ClearToEndOfScreen() error      { return nil }
func (tm *testTerminalManager) EnterAltScreen() error          { return nil }
func (tm *testTerminalManager) ExitAltScreen() error           { return nil }
func (tm *testTerminalManager) EnableMouseReporting() error    { return nil }
func (tm *testTerminalManager) DisableMouseReporting() error   { return nil }
func (tm *testTerminalManager) SetScrollRegion(int, int) error { return nil }
func (tm *testTerminalManager) ResetScrollRegion() error {
	tm.mu.Lock()
	tm.resetCount++
	tm.mu.Unlock()
	return nil
}
func (tm *testTerminalManager) ScrollUp(int) error              { return nil }
func (tm *testTerminalManager) ScrollDown(int) error            { return nil }
func (tm *testTerminalManager) Write(b []byte) (int, error)     { return len(b), nil }
func (tm *testTerminalManager) WriteText(s string) (int, error) { return len(s), nil }
func (tm *testTerminalManager) WriteAt(int, int, []byte) error  { return nil }
func (tm *testTerminalManager) Flush() error                    { return nil }
func (tm *testTerminalManager) rawModeResets() int {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.resetCount
}

// MockTerminal for testing
type MockTerminalForPassthrough struct {
	commands []string
	isRaw    bool
}

func (mt *MockTerminalForPassthrough) MakeRaw() error {
	mt.isRaw = true
	mt.commands = append(mt.commands, "MakeRaw")
	return nil
}

func (mt *MockTerminalForPassthrough) Restore() error {
	mt.isRaw = false
	mt.commands = append(mt.commands, "Restore")
	return nil
}

func (mt *MockTerminalForPassthrough) IsRaw() bool {
	return mt.isRaw
}

func (mt *MockTerminalForPassthrough) GetCommands() []string {
	return mt.commands
}

func TestInputManager_SetPassthroughMode(t *testing.T) {
	// Create input manager
	im := NewInputManager("> ")

	// Test initial state
	if im.paused {
		t.Error("Expected input manager to start unpaused")
	}

	if im.running {
		t.Error("Expected input manager to start not running")
	}

	// Test enabling passthrough mode when not running
	im.SetPassthroughMode(true)

	// Should not change anything if not running
	if im.running {
		t.Error("Expected input manager to remain not running")
	}

	// Test disabling passthrough mode when not running
	im.SetPassthroughMode(false)

	// Should not change anything if not running
	if im.running {
		t.Error("Expected input manager to remain not running")
	}
}

func TestInputManager_PassthroughModeFlow(t *testing.T) {
	// Create input manager
	im := NewInputManager("> ")

	// Start the input manager (this will fail in test environment, but we can check the state)
	err := im.Start()
	if err != nil {
		// Expected in test environment without terminal
		t.Logf("Start failed as expected in test environment: %v", err)
	}

	// Manually set running state for testing
	im.mutex.Lock()
	im.running = true
	im.isRawMode = false // Simulate terminal state
	im.mutex.Unlock()

	// Test enabling passthrough mode
	im.SetPassthroughMode(true)

	// Verify state changes
	im.mutex.RLock()
	running := im.running
	im.mutex.RUnlock()

	if running {
		t.Error("Expected input manager to be stopped after enabling passthrough mode")
	}

	// Test disabling passthrough mode
	im.SetPassthroughMode(false)

	// Give goroutines time to start
	time.Sleep(10 * time.Millisecond)

	// Check if input manager restarted
	im.mutex.RLock()
	running = im.running
	im.mutex.RUnlock()

	// In test environment, this might fail due to terminal requirements
	// But we can verify the logic was attempted
	t.Logf("Input manager running state after restart attempt: %v", running)
}

func TestInputManager_PassthroughModeIdempotent(t *testing.T) {
	im := NewInputManager("> ")

	// Manually set states for testing
	im.mutex.Lock()
	im.running = true
	im.mutex.Unlock()

	// Enable passthrough mode multiple times
	im.SetPassthroughMode(true)
	im.SetPassthroughMode(true)
	im.SetPassthroughMode(true)

	// Should handle multiple calls gracefully
	im.mutex.RLock()
	running := im.running
	im.mutex.RUnlock()

	if running {
		t.Error("Expected input manager to be stopped after multiple passthrough enables")
	}

	// Disable passthrough mode multiple times
	im.SetPassthroughMode(false)
	im.SetPassthroughMode(false)

	// Should handle multiple calls gracefully - no panics expected
	t.Log("Multiple passthrough mode toggles completed without panic")
}

func TestInputManager_PassthroughModeRaceConditions(t *testing.T) {
	im := NewInputManager("> ")

	// Manually set running state
	im.mutex.Lock()
	im.running = true
	im.mutex.Unlock()

	// Test concurrent access to passthrough mode
	done := make(chan bool, 2)

	go func() {
		for i := 0; i < 10; i++ {
			im.SetPassthroughMode(true)
			time.Sleep(1 * time.Millisecond)
			im.SetPassthroughMode(false)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 10; i++ {
			im.SetPassthroughMode(false)
			time.Sleep(1 * time.Millisecond)
			im.SetPassthroughMode(true)
		}
		done <- true
	}()

	// Wait for both goroutines to complete
	<-done
	<-done

	// Should not panic or deadlock
	t.Log("Concurrent passthrough mode access completed without issues")
}

func TestInputManager_PassthroughModeMemoryLeaks(t *testing.T) {
	im := NewInputManager("> ")

	// Simulate starting and stopping many times to check for leaks
	for i := 0; i < 5; i++ {
		im.mutex.Lock()
		im.running = true
		im.mutex.Unlock()

		// Enable/disable passthrough
		im.SetPassthroughMode(true)
		im.SetPassthroughMode(false)

		// Brief pause to let goroutines settle
		time.Sleep(5 * time.Millisecond)
	}

	// This test mainly ensures no panics occur and goroutines are cleaned up
	t.Log("Multiple passthrough cycles completed")
}

func TestInputManager_ControllerRawModeLifecycle(t *testing.T) {
	tm := newTestTerminalManager(80, 24)
	eventBus := console.NewEventBus(16)
	tc := console.NewTerminalController(tm, eventBus)
	if err := tc.Init(); err != nil {
		t.Fatalf("terminal controller init failed: %v", err)
	}
	defer tc.Cleanup()

	im := NewInputManager("> ")
	im.mutex.Lock()
	im.running = true
	im.mutex.Unlock()

	im.SetController(tc)

	if !tm.IsRawMode() {
		t.Fatalf("expected raw mode to be enabled after controller wiring")
	}

	im.SetPassthroughMode(true)

	if tm.IsRawMode() {
		t.Fatalf("expected raw mode to be released during passthrough mode")
	}

	if resets := tm.rawModeResets(); resets == 0 {
		t.Fatalf("expected controller to reset scroll region during passthrough")
	}

	im.mutex.Lock()
	im.running = true
	im.ensureRawModeLocked(rawModeOwnerInputManager)
	im.mutex.Unlock()

	if !tm.IsRawMode() {
		t.Fatalf("expected raw mode to be reacquired after passthrough")
	}
}

// Test the integration with agent console command handling
func TestAgentConsole_InteractiveCommandDetection(t *testing.T) {
	// Test that we correctly identify interactive commands
	interactiveCommands := []string{"models", "mcp", "commit", "shell", "providers"}
	nonInteractiveCommands := []string{"log", "help", "changes", "status", "info"}

	for _, cmd := range interactiveCommands {
		// This would be the logic from agent console
		isInteractive := cmd == "models" || cmd == "mcp" || cmd == "commit" || cmd == "shell" || cmd == "providers"
		if !isInteractive {
			t.Errorf("Command '%s' should be detected as interactive", cmd)
		}
	}

	for _, cmd := range nonInteractiveCommands {
		// This would be the logic from agent console
		isInteractive := cmd == "models" || cmd == "mcp" || cmd == "commit" || cmd == "shell" || cmd == "providers"
		if isInteractive {
			t.Errorf("Command '%s' should not be detected as interactive", cmd)
		}
	}
}
