package components

import (
	"context"
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/console"
)

// mockTerminalForApp implements TerminalManager for testing
type mockTerminalForApp struct {
	width  int
	height int
}

func (m *mockTerminalForApp) Init() error                               { return nil }
func (m *mockTerminalForApp) Cleanup() error                            { return nil }
func (m *mockTerminalForApp) GetSize() (width, height int, err error)   { return m.width, m.height, nil }
func (m *mockTerminalForApp) OnResize(callback func(width, height int)) {}
func (m *mockTerminalForApp) SetRawMode(enabled bool) error             { return nil }
func (m *mockTerminalForApp) IsRawMode() bool                           { return false }
func (m *mockTerminalForApp) MoveCursor(x, y int) error                 { return nil }
func (m *mockTerminalForApp) SaveCursor() error                         { return nil }
func (m *mockTerminalForApp) RestoreCursor() error                      { return nil }
func (m *mockTerminalForApp) HideCursor() error                         { return nil }
func (m *mockTerminalForApp) ShowCursor() error                         { return nil }
func (m *mockTerminalForApp) ClearScreen() error                        { return nil }
func (m *mockTerminalForApp) ClearLine() error                          { return nil }
func (m *mockTerminalForApp) ClearToEndOfLine() error                   { return nil }
func (m *mockTerminalForApp) ClearToEndOfScreen() error                 { return nil }
func (m *mockTerminalForApp) EnterAltScreen() error                     { return nil }
func (m *mockTerminalForApp) ExitAltScreen() error                      { return nil }
func (m *mockTerminalForApp) SetScrollRegion(top, bottom int) error     { return nil }
func (m *mockTerminalForApp) ResetScrollRegion() error                  { return nil }
func (m *mockTerminalForApp) ScrollUp(lines int) error                  { return nil }
func (m *mockTerminalForApp) ScrollDown(lines int) error                { return nil }
func (m *mockTerminalForApp) Write(data []byte) (int, error)            { return len(data), nil }
func (m *mockTerminalForApp) WriteText(text string) (int, error)        { return len(text), nil }
func (m *mockTerminalForApp) WriteAt(x, y int, data []byte) error       { return nil }
func (m *mockTerminalForApp) Flush() error                              { return nil }

// mockDropdownItem implements console.DropdownItem
type mockDropdownItem struct {
	display string
	value   string
}

func (m *mockDropdownItem) Display() string    { return m.display }
func (m *mockDropdownItem) SearchText() string { return m.display }
func (m *mockDropdownItem) Value() interface{} { return m.value }

func TestConsoleApp_Creation(t *testing.T) {
	mockTerm := &mockTerminalForApp{width: 80, height: 24}
	app := NewConsoleApp(mockTerm)

	if app == nil {
		t.Fatal("NewConsoleApp returned nil")
	}

	if app.terminal != mockTerm {
		t.Error("Terminal not properly set")
	}

	if app.app == nil {
		t.Error("Core app not initialized")
	}

	if app.renderer == nil {
		t.Error("Renderer not initialized")
	}
}

func TestConsoleApp_DropdownStructure(t *testing.T) {
	mockTerm := &mockTerminalForApp{width: 80, height: 24}
	app := NewConsoleApp(mockTerm)

	// Test that we can create dropdown items
	items := []interface{}{
		&mockDropdownItem{display: "Item 1", value: "value1"},
		&mockDropdownItem{display: "Item 2", value: "value2"},
		&mockDropdownItem{display: "Item 3", value: "value3"},
	}

	opts := map[string]interface{}{
		"prompt":       "Select an item:",
		"searchPrompt": "Search: ",
		"showCounts":   true,
	}

	// Test dropdown creation (without actually showing it)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// This will timeout, but we're just testing structure
	_, err := app.ShowDropdown(ctx, items, opts)

	// We expect a timeout error or cancelled context
	if err == nil {
		t.Error("Expected error due to no input, got nil")
	}

	// Verify cleanup works
	app.Cleanup()
}

func TestConsoleUI_Integration(t *testing.T) {
	// Create a base component and set up dependencies
	base := console.NewBaseComponent("test", "agent")
	base.Deps = console.Dependencies{
		Terminal: &mockTerminalForApp{width: 80, height: 24},
	}

	// Create a mock agent console
	mockConsole := &AgentConsole{
		BaseComponent: base,
	}

	// Create console UI
	consoleUI := NewConsoleUI(mockConsole)

	if !consoleUI.isInteractive {
		t.Skip("Not in interactive mode, skipping test")
	}

	if consoleUI.consoleApp == nil {
		t.Error("ConsoleApp should be initialized in interactive mode")
	}

	// Test cleanup
	consoleUI.Cleanup()
}
