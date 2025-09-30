package console

import (
	"context"
	"fmt"
)

// BaseComponent provides common functionality for components
type BaseComponent struct {
	id            string
	componentType string
	region        string
	needsRedraw   bool
	ctx           context.Context

	// Protected fields - accessible by embedding components
	Deps Dependencies
}

// NewBaseComponent creates a new base component
func NewBaseComponent(id, componentType string) *BaseComponent {
	return &BaseComponent{
		id:            id,
		componentType: componentType,
		needsRedraw:   true,
	}
}

// Init initializes the component with dependencies
func (bc *BaseComponent) Init(ctx context.Context, deps Dependencies) error {
	bc.ctx = ctx
	bc.Deps = deps
	return nil
}

// Start starts the component
func (bc *BaseComponent) Start() error {
	return nil // Override in subcomponents if needed
}

// Stop stops the component
func (bc *BaseComponent) Stop() error {
	return nil // Override in subcomponents if needed
}

// Cleanup cleans up component resources
func (bc *BaseComponent) Cleanup() error {
	return nil // Override in subcomponents if needed
}

// ID returns the component ID
func (bc *BaseComponent) ID() string {
	return bc.id
}

// Type returns the component type
func (bc *BaseComponent) Type() string {
	return bc.componentType
}

// Render renders the component
func (bc *BaseComponent) Render() error {
	return fmt.Errorf("render not implemented for %s", bc.componentType)
}

// NeedsRedraw returns if component needs redrawing
func (bc *BaseComponent) NeedsRedraw() bool {
	return bc.needsRedraw
}

// SetNeedsRedraw marks component for redraw
func (bc *BaseComponent) SetNeedsRedraw(needs bool) {
	bc.needsRedraw = needs
}

// HandleInput handles input
func (bc *BaseComponent) HandleInput(input []byte) (bool, error) {
	return false, nil // Override in subcomponents if needed
}

// CanHandleInput returns if component can handle input
func (bc *BaseComponent) CanHandleInput() bool {
	return false // Override in subcomponents if needed
}

// GetRegion returns the component's region
func (bc *BaseComponent) GetRegion() string {
	return bc.region
}

// SetRegion sets the component's region
func (bc *BaseComponent) SetRegion(region string) {
	bc.region = region
}

// Terminal returns the terminal manager
func (bc *BaseComponent) Terminal() TerminalManager {
	return bc.Deps.Terminal
}

// Controller returns the terminal controller
func (bc *BaseComponent) Controller() *TerminalController {
	return bc.Deps.Controller
}

// Layout returns the layout manager
func (bc *BaseComponent) Layout() LayoutManager {
	return bc.Deps.Layout
}

// State returns the state manager
func (bc *BaseComponent) State() StateManager {
	return bc.Deps.State
}

// Events returns the event bus
func (bc *BaseComponent) Events() EventBus {
	return bc.Deps.Events
}

// PublishEvent publishes an event from this component
func (bc *BaseComponent) PublishEvent(eventType string, data interface{}) {
	event := Event{
		Type:   eventType,
		Source: bc.id,
		Data:   data,
	}
	bc.Deps.Events.PublishAsync(event)
}
