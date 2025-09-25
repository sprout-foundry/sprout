package console

import (
	"context"
	"fmt"
	"os"
	"sync"
)

// consoleApp implements ConsoleApp interface
type consoleApp struct {
	mu         sync.RWMutex
	config     *Config
	terminal   TerminalManager
	controller *TerminalController
	layout     LayoutManager
	state      StateManager
	events     EventBus
	components map[string]Component
	running    bool
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewConsoleApp creates a new console application
func NewConsoleApp() ConsoleApp {
	return &consoleApp{
		components: make(map[string]Component),
	}
}

// Init initializes the console app
func (ca *consoleApp) Init(config *Config) error {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	if config == nil {
		config = DefaultConfig()
	}
	ca.config = config

	// Create context
	ca.ctx, ca.cancel = context.WithCancel(context.Background())

	// Initialize event bus first
	ca.events = NewEventBus(config.EventQueueSize)

	// Initialize terminal manager
	ca.terminal = NewTerminalManager()
	if err := ca.terminal.Init(); err != nil {
		return fmt.Errorf("failed to init terminal: %w", err)
	}

	// Initialize terminal controller with centralized handling
	ca.controller = NewTerminalController(ca.terminal, ca.events)
	if err := ca.controller.Init(); err != nil {
		return fmt.Errorf("failed to init terminal controller: %w", err)
	}

	// Set raw mode through controller
	if err := ca.controller.SetRawMode(config.RawMode); err != nil {
		return fmt.Errorf("failed to set raw mode: %w", err)
	}

	// Get terminal size for layout
	width, height, _ := ca.controller.GetSize()

	// Initialize layout manager
	ca.layout = NewLayoutManager(width, height)

	// Initialize state manager
	ca.state = NewStateManager()

	// Subscribe to resize events from controller
	ca.events.Subscribe("terminal.resized", func(event Event) error {
		if data, ok := event.Data.(map[string]interface{}); ok {
			if w, wOk := data["width"].(int); wOk {
				if h, hOk := data["height"].(int); hOk {
					ca.layout.CalculateLayout(w, h)
				}
			}
		}
		return nil
	})

	// Initialize components from config
	for _, compConfig := range config.Components {
		if !compConfig.Enabled {
			continue
		}
		// In a real implementation, you'd have a component factory here
		// For now, we'll skip component initialization
	}

	return nil
}

// Start starts the console app
func (ca *consoleApp) Start() error {
	ca.mu.Lock()
	if ca.running {
		ca.mu.Unlock()
		return fmt.Errorf("app already running")
	}
	ca.running = true
	ca.mu.Unlock()

	// Start event bus
	if err := ca.events.Start(); err != nil {
		return fmt.Errorf("failed to start event bus: %w", err)
	}

	// Start components
	components := ca.getComponentList()
	for _, comp := range components {
		if err := comp.Start(); err != nil {
			return fmt.Errorf("failed to start component %s: %w", comp.ID(), err)
		}
	}

	// Initial render
	ca.render()

	return nil
}

// Stop stops the console app
func (ca *consoleApp) Stop() error {
	ca.mu.Lock()
	if !ca.running {
		ca.mu.Unlock()
		return fmt.Errorf("app not running")
	}
	ca.running = false
	ca.mu.Unlock()

	// Stop components in reverse order
	components := ca.getComponentList()
	for i := len(components) - 1; i >= 0; i-- {
		comp := components[i]
		if err := comp.Stop(); err != nil {
			// Log error but continue stopping other components
			fmt.Printf("Error stopping component %s: %v\n", comp.ID(), err)
		}
	}

	// Stop event bus
	if err := ca.events.Stop(); err != nil {
		return fmt.Errorf("failed to stop event bus: %w", err)
	}

	// Cancel context
	if ca.cancel != nil {
		ca.cancel()
	}

	return nil
}

// Run runs the main application loop
func (ca *consoleApp) Run() error {
	if err := ca.Start(); err != nil {
		return err
	}
	defer ca.Stop()

	// Main event loop
	for {
		select {
		case <-ca.ctx.Done():
			return nil
		default:
			// Process input
			if err := ca.processInput(); err != nil {
				return err
			}

			// Check for components needing redraw
			if ca.needsRedraw() {
				ca.render()
			}
		}
	}
}

// AddComponent adds a component to the app
func (ca *consoleApp) AddComponent(component Component) error {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	if ca.running {
		return fmt.Errorf("cannot add component while running")
	}

	id := component.ID()
	if _, exists := ca.components[id]; exists {
		return fmt.Errorf("component %s already exists", id)
	}

	// Initialize component
	deps := Dependencies{
		Terminal: ca.controller, // Components use controller instead of raw terminal
		Layout:   ca.layout,
		State:    ca.state,
		Events:   ca.events,
	}

	if err := component.Init(ca.ctx, deps); err != nil {
		return fmt.Errorf("failed to init component %s: %w", id, err)
	}

	ca.components[id] = component
	return nil
}

// RemoveComponent removes a component from the app
func (ca *consoleApp) RemoveComponent(componentID string) error {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	if ca.running {
		return fmt.Errorf("cannot remove component while running")
	}

	comp, exists := ca.components[componentID]
	if !exists {
		return fmt.Errorf("component %s not found", componentID)
	}

	// Cleanup component
	if err := comp.Cleanup(); err != nil {
		return fmt.Errorf("failed to cleanup component %s: %w", componentID, err)
	}

	delete(ca.components, componentID)
	return nil
}

// GetComponent returns a component by ID
func (ca *consoleApp) GetComponent(componentID string) (Component, bool) {
	ca.mu.RLock()
	defer ca.mu.RUnlock()
	comp, exists := ca.components[componentID]
	return comp, exists
}

// ListComponents returns all component IDs
func (ca *consoleApp) ListComponents() []string {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	ids := make([]string, 0, len(ca.components))
	for id := range ca.components {
		ids = append(ids, id)
	}
	return ids
}

// Terminal returns the terminal manager
func (ca *consoleApp) Terminal() TerminalManager {
	return ca.terminal
}

// Layout returns the layout manager
func (ca *consoleApp) Layout() LayoutManager {
	return ca.layout
}

// State returns the state manager
func (ca *consoleApp) State() StateManager {
	return ca.state
}

// Events returns the event bus
func (ca *consoleApp) Events() EventBus {
	return ca.events
}

// GetConfig returns the current configuration
func (ca *consoleApp) GetConfig() *Config {
	ca.mu.RLock()
	defer ca.mu.RUnlock()
	// Return a copy to prevent external modification
	configCopy := *ca.config
	return &configCopy
}

// UpdateConfig updates the configuration
func (ca *consoleApp) UpdateConfig(config *Config) error {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	if ca.running {
		return fmt.Errorf("cannot update config while running")
	}

	ca.config = config
	return nil
}

// Cleanup cleans up all resources
func (ca *consoleApp) Cleanup() error {
	// Stop if running
	if ca.running {
		if err := ca.Stop(); err != nil {
			return err
		}
	}

	// Cleanup components
	for _, comp := range ca.components {
		if err := comp.Cleanup(); err != nil {
			// Log but continue
			fmt.Printf("Error cleaning up component %s: %v\n", comp.ID(), err)
		}
	}

	// Cleanup terminal controller first
	if ca.controller != nil {
		if err := ca.controller.Cleanup(); err != nil {
			return fmt.Errorf("failed to cleanup terminal controller: %w", err)
		}
	}

	// Cleanup terminal
	if ca.terminal != nil {
		if err := ca.terminal.Cleanup(); err != nil {
			return fmt.Errorf("failed to cleanup terminal: %w", err)
		}
	}

	return nil
}

// getComponentList returns components in a consistent order
func (ca *consoleApp) getComponentList() []Component {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	components := make([]Component, 0, len(ca.components))
	for _, comp := range ca.components {
		components = append(components, comp)
	}
	return components
}

// processInput processes input from the terminal
func (ca *consoleApp) processInput() error {
	// Read a single byte (non-blocking would be better)
	buf := make([]byte, 1)
	n, err := ca.terminal.(*terminalManager).writer.(*os.File).Read(buf)
	if err != nil || n == 0 {
		return nil // No input available
	}

	// Find component that can handle input
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	for _, comp := range ca.components {
		if comp.CanHandleInput() {
			handled, err := comp.HandleInput(buf[:n])
			if err != nil {
				return err
			}
			if handled {
				break
			}
		}
	}

	return nil
}

// needsRedraw checks if any component needs redrawing
func (ca *consoleApp) needsRedraw() bool {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	for _, comp := range ca.components {
		if comp.NeedsRedraw() {
			return true
		}
	}
	return false
}

// render renders all components
func (ca *consoleApp) render() {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	// Begin batch rendering
	ca.layout.BeginBatch()

	// Get render order from layout manager
	renderOrder := ca.layout.GetRenderOrder()

	// Render components in order
	for _, regionName := range renderOrder {
		// Find component for this region
		for _, comp := range ca.components {
			if comp.GetRegion() == regionName {
				if err := comp.Render(); err != nil {
					// Log error but continue
					fmt.Printf("Error rendering component %s: %v\n", comp.ID(), err)
				}
				// Mark as rendered
				if bc, ok := comp.(*BaseComponent); ok {
					bc.SetNeedsRedraw(false)
				}
				break
			}
		}
	}

	// End batch rendering
	ca.layout.EndBatch()

	// Flush terminal output
	ca.terminal.Flush()
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		RawMode:        true,
		MouseEnabled:   false,
		AltScreen:      false,
		MinWidth:       80,
		MinHeight:      24,
		EventQueueSize: 100,
		Debug:          false,
	}
}
