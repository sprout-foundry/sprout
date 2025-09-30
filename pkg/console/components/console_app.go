package components

import (
    "context"
    "fmt"
    "os"
    "sync"
    "time"

	"github.com/alantheprice/ledit/pkg/console"
	"github.com/alantheprice/ledit/pkg/ui/core"
	"github.com/alantheprice/ledit/pkg/ui/core/components"
)

// ConsoleApp integrates the new component architecture with the existing console
type ConsoleApp struct {
	mu sync.Mutex

	// Core app and dependencies
	app      *core.App
	terminal console.TerminalManager
	renderer *core.TerminalRenderer

	// Active components
	activeDropdown *components.DropdownComponent

	// Callbacks
	dropdownCallback func(selected interface{}, err error)

	// Last dropdown state for selection handling
	lastDropdownState map[string]interface{}

	// State subscription
	unsubscribe func()

	// Input loop control
	inputCancel context.CancelFunc
	inputDone   chan struct{}
}

// NewConsoleApp creates a new console app
func NewConsoleApp(terminal console.TerminalManager) *ConsoleApp {
	app := core.NewApp()
	renderer := core.NewTerminalRenderer(terminal)
	app.SetRenderer(renderer)

	return &ConsoleApp{
		app:      app,
		terminal: terminal,
		renderer: renderer,
	}
}

// NewConsoleAppWithMode creates a new console app with mode-aware configuration
func NewConsoleAppWithMode(terminal console.TerminalManager, mode console.OutputMode) *ConsoleApp {
	app := core.NewApp()
	renderer := core.NewTerminalRenderer(terminal)
	app.SetRenderer(renderer)

	// Set the mode-specific configuration
	// Note: Raw mode is set during proper initialization, not here
	// The mode-aware configuration will be applied when the console app is initialized
	
	return &ConsoleApp{
		app:      app,
		terminal: terminal,
		renderer: renderer,
	}
}

// ShowDropdown displays a dropdown and returns the selected item
func (c *ConsoleApp) ShowDropdown(ctx context.Context, items []interface{}, options map[string]interface{}) (interface{}, error) {
	c.mu.Lock()

	// Create result channel
	resultChan := make(chan struct {
		selected interface{}
		err      error
	}, 1)

	// Store callback
	c.dropdownCallback = func(selected interface{}, err error) {
		resultChan <- struct {
			selected interface{}
			err      error
		}{selected, err}
	}

	// Clean up any existing dropdown
	if c.activeDropdown != nil {
		c.app.Unmount("dropdown")
		c.activeDropdown = nil
	}

	// Create fresh dropdown component
	c.activeDropdown = components.NewDropdownComponent("dropdown", c.app.GetStore(), c.renderer)
	c.app.Mount(c.activeDropdown)

	// Subscribe to state changes for dropdown results if not already subscribed
	if c.unsubscribe == nil {
		c.unsubscribe = c.app.GetStore().Subscribe(c.handleStateChange)
	}

	// Show dropdown via action
	c.app.ShowDropdown("dropdown", items, options)

	// Hide cursor during dropdown
	c.terminal.HideCursor()
	defer c.terminal.ShowCursor()

	// Save current cursor position
	fmt.Print("\033[s")

	// Keep terminal in raw mode for proper input handling
	// The dropdown needs raw mode to capture arrow keys properly
	wasRawMode := c.terminal.IsRawMode()
    if !wasRawMode {
        c.terminal.SetRawMode(true)
        // Allow terminal to settle in raw mode to avoid losing the first keystroke
        time.Sleep(30 * time.Millisecond)
        defer c.terminal.SetRawMode(false)
    }

	c.mu.Unlock()

	// Initial render
	if err := c.app.Render(ctx); err != nil {
		return nil, err
	}

	// Create context for input loop
	inputCtx, inputCancel := context.WithCancel(ctx)
	c.inputCancel = inputCancel
	c.inputDone = make(chan struct{})

	// Handle input loop
	go c.inputLoop(inputCtx)

	// Wait for result
	select {
	case result := <-resultChan:
		// Stop input loop
		if c.inputCancel != nil {
			c.inputCancel()
			// Wait for input loop to finish
			<-c.inputDone
		}

		c.mu.Lock()
		// Properly unmount the dropdown
		if c.activeDropdown != nil {
			c.app.Unmount("dropdown")
			c.activeDropdown = nil
		}
		c.inputCancel = nil
		c.inputDone = nil
		c.mu.Unlock()

		// Clear the dropdown area more thoroughly
		// Save cursor again before clearing
		fmt.Print("\033[s")

		// Clear from top of screen (where dropdown appears)
		fmt.Print("\033[H")       // Move to home
		for i := 0; i < 20; i++ { // Clear more lines to ensure all artifacts are gone
			fmt.Print("\033[2K") // Clear entire line
			if i < 19 {          // Don't move down on last iteration
				fmt.Print("\033[B") // Move down
			}
		}

		// Restore cursor position
		fmt.Print("\033[u")

		// Force a flush to ensure clearing is applied
		os.Stdout.Sync()

		return result.selected, result.err

	case <-ctx.Done():
		// Stop input loop
		if c.inputCancel != nil {
			c.inputCancel()
			<-c.inputDone
		}

		// Restore cursor position
		fmt.Print("\033[u")

		return nil, ctx.Err()
	}
}

// inputLoop handles keyboard input for the dropdown
func (c *ConsoleApp) inputLoop(ctx context.Context) {
	defer func() {
		if c.inputDone != nil {
			close(c.inputDone)
		}
	}()

	buf := make([]byte, 10)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Read input from stdin with a small timeout to check context
		// Use a non-blocking read approach
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			// Check if context is cancelled
			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}

		input := buf[:n]

		// Pass all input to the app for processing
		if err := c.app.HandleInput(input); err != nil {
			// Only log errors in debug mode
			continue
		}

		// Re-render after input
		if err := c.app.Render(ctx); err != nil {
			// Log render errors
			continue
		}
	}
}

// handleStateChange handles state changes and triggers callbacks
func (c *ConsoleApp) handleStateChange(state core.State) {
	// Check dropdown state
	ui, _ := state["ui"].(core.State)
	dropdowns, _ := ui["dropdowns"].(map[string]interface{})
	dropdown, exists := dropdowns["dropdown"]

	if exists {
		// Store current dropdown state for later
		if dropdownState, ok := dropdown.(map[string]interface{}); ok {
			c.lastDropdownState = dropdownState
		}
	} else if !exists && c.dropdownCallback != nil && c.lastDropdownState != nil {
		// Dropdown was closed, check for selection
		focus, _ := state["focus"].(core.State)
		lastAction, _ := focus["lastAction"].(string)

		// Debug logging
		// fmt.Fprintf(os.Stderr, "Dropdown closed, lastAction: %s, focus state: %+v\n", lastAction, focus)

		if lastAction == "select" {
			// Get selected item from the last known state
			// First try filteredItems, then fall back to items
			items, ok := c.lastDropdownState["filteredItems"].([]interface{})
			if !ok || len(items) == 0 {
				items, _ = c.lastDropdownState["items"].([]interface{})
			}
			selectedIndex, _ := c.lastDropdownState["selectedIndex"].(int)

			if selectedIndex >= 0 && selectedIndex < len(items) {
				c.dropdownCallback(items[selectedIndex], nil)
			} else {
				c.dropdownCallback(nil, fmt.Errorf("no selection"))
			}
		} else if lastAction == "cancel" {
			// Cancelled
			c.dropdownCallback(nil, fmt.Errorf("cancelled"))
		} else {
			// No action recorded - might be a state issue
			c.dropdownCallback(nil, fmt.Errorf("no action"))
		}

		c.dropdownCallback = nil
		c.lastDropdownState = nil

		// Clear the lastAction to prevent double processing
		c.app.Dispatch(core.Action{
			Type: "FOCUS/CLEAR_LAST_ACTION",
		})
	}
}

// GetApp returns the underlying app for advanced usage
func (c *ConsoleApp) GetApp() *core.App {
	return c.app
}

// Cleanup cleans up resources
func (c *ConsoleApp) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.activeDropdown != nil {
		c.app.Unmount("dropdown")
		c.activeDropdown = nil
	}

	if c.unsubscribe != nil {
		c.unsubscribe()
		c.unsubscribe = nil
	}
}
