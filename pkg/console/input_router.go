package console

import (
	"sync"
)

// InputEvent represents a keyboard input event
type InputEvent struct {
	Type InputEventType
	Data interface{}
}

// InputEventType represents the type of input event
type InputEventType int

const (
	KeystrokeEvent InputEventType = iota
	InterruptEvent
	ResizeEvent
)

// KeystrokeData contains keystroke information
type KeystrokeData struct {
	Bytes []byte
	Raw   []byte
}

// InputHandler interface that components can implement to receive input events
type InputHandler interface {
	HandleInput(event InputEvent) bool // Returns true if event was handled
	GetHandlerID() string
}

// InputRouter manages routing input events to the currently active handler
type InputRouter struct {
	mutex          sync.RWMutex
	activeHandler  InputHandler
	defaultHandler InputHandler
	eventChannel   chan InputEvent
	subscribers    map[string]InputHandler
}

// NewInputRouter creates a new input router
func NewInputRouter() *InputRouter {
	return &InputRouter{
		eventChannel: make(chan InputEvent, 100),
		subscribers:  make(map[string]InputHandler),
	}
}

// SetDefaultHandler sets the default handler (usually the main console)
func (ir *InputRouter) SetDefaultHandler(handler InputHandler) {
	ir.mutex.Lock()
	defer ir.mutex.Unlock()

	ir.defaultHandler = handler
	if ir.activeHandler == nil {
		ir.activeHandler = handler
	}
}

// SetActiveHandler switches the active input handler
func (ir *InputRouter) SetActiveHandler(handlerID string) bool {
	ir.mutex.Lock()
	defer ir.mutex.Unlock()

	if handler, exists := ir.subscribers[handlerID]; exists {
		ir.activeHandler = handler
		return true
	}
	return false
}

// RestoreDefaultHandler switches back to the default handler
func (ir *InputRouter) RestoreDefaultHandler() {
	ir.mutex.Lock()
	defer ir.mutex.Unlock()

	ir.activeHandler = ir.defaultHandler
}

// RegisterHandler registers a new input handler
func (ir *InputRouter) RegisterHandler(handler InputHandler) {
	ir.mutex.Lock()
	defer ir.mutex.Unlock()

	ir.subscribers[handler.GetHandlerID()] = handler
}

// UnregisterHandler removes an input handler
func (ir *InputRouter) UnregisterHandler(handlerID string) {
	ir.mutex.Lock()
	defer ir.mutex.Unlock()

	delete(ir.subscribers, handlerID)

	// If this was the active handler, revert to default
	if ir.activeHandler != nil && ir.activeHandler.GetHandlerID() == handlerID {
		ir.activeHandler = ir.defaultHandler
	}
}

// PublishEvent sends an input event to the active handler
func (ir *InputRouter) PublishEvent(event InputEvent) bool {
	ir.mutex.RLock()
	activeHandler := ir.activeHandler
	ir.mutex.RUnlock()

	if activeHandler != nil {
		return activeHandler.HandleInput(event)
	}

	return false
}

// Start begins processing input events
func (ir *InputRouter) Start() {
	go ir.eventLoop()
}

// eventLoop processes input events from the channel
func (ir *InputRouter) eventLoop() {
	for event := range ir.eventChannel {
		ir.PublishEvent(event)
	}
}

// SendKeystroke sends a keystroke event to the active handler
func (ir *InputRouter) SendKeystroke(data []byte) bool {
	event := InputEvent{
		Type: KeystrokeEvent,
		Data: KeystrokeData{
			Bytes: data,
			Raw:   data,
		},
	}
	return ir.PublishEvent(event)
}

// SendInterrupt sends an interrupt event to the active handler
func (ir *InputRouter) SendInterrupt() bool {
	event := InputEvent{
		Type: InterruptEvent,
		Data: nil,
	}
	return ir.PublishEvent(event)
}
