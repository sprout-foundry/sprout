package console

import (
	"sync"
)

// UIHandler manages UI events and coordinates component updates
type UIHandler struct {
	mu            sync.RWMutex
	components    map[string]ResizableComponent
	terminal      TerminalManager
	isInitialized bool
}

// ResizableComponent is an interface for components that can handle resize events
type ResizableComponent interface {
	OnResize(width, height int)
}

// NewUIHandler creates a new UI handler
func NewUIHandler(terminal TerminalManager) *UIHandler {
	return &UIHandler{
		components: make(map[string]ResizableComponent),
		terminal:   terminal,
	}
}

// RegisterComponent registers a component for resize notifications
func (h *UIHandler) RegisterComponent(id string, component ResizableComponent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.components[id] = component
}

// UnregisterComponent removes a component from resize notifications
func (h *UIHandler) UnregisterComponent(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.components, id)
}

// Initialize sets up the UI handler and starts listening for events
func (h *UIHandler) Initialize() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.isInitialized {
		return nil
	}

	// Register for terminal resize events
	if h.terminal != nil {
		h.terminal.OnResize(h.handleResize)
	}

	h.isInitialized = true
	return nil
}

// handleResize is called when the terminal is resized
func (h *UIHandler) handleResize(width, height int) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Notify all registered components
	for _, component := range h.components {
		component.OnResize(width, height)
	}
}

// ForceRedraw forces all components to redraw
func (h *UIHandler) ForceRedraw() {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Get current terminal size
	if h.terminal != nil {
		if width, height, err := h.terminal.GetSize(); err == nil {
			// Notify all components with current size
			for _, component := range h.components {
				component.OnResize(width, height)
			}
		}
	}
}
