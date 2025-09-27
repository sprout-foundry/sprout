// Package core provides the fundamental types and interfaces for a component-based UI system
package core

import (
	"context"
)

// State represents the immutable application state
type State map[string]interface{}

// Props represents immutable properties passed to components
type Props map[string]interface{}

// Action represents a state change request
type Action struct {
	Type    string
	Payload interface{}
}

// Reducer transforms state based on actions
type Reducer func(state State, action Action) State

// Component is the base interface for all UI components
type Component interface {
	// Render produces the component's output based on current props and state
	Render(ctx context.Context) error

	// GetID returns the component's unique identifier
	GetID() string

	// SetProps updates the component's properties and triggers re-render if needed
	SetProps(props Props)

	// ShouldUpdate determines if the component needs re-rendering
	ShouldUpdate(oldProps, newProps Props, oldState, newState State) bool
}

// StatefulComponent extends Component with local state management
type StatefulComponent interface {
	Component

	// GetState returns the component's local state
	GetState() State

	// SetState updates local state and triggers re-render
	SetState(state State)
}

// Store manages application state
type Store interface {
	// GetState returns the current state
	GetState() State

	// Dispatch sends an action to update state
	Dispatch(action Action)

	// Subscribe registers a listener for state changes
	Subscribe(listener func(state State)) func()

	// Select retrieves a specific part of the state
	Select(selector func(State) interface{}) interface{}
}

// Renderer handles the actual drawing to the terminal
type Renderer interface {
	// Clear clears the render area
	Clear() error

	// DrawText draws text at a specific position
	DrawText(x, y int, text string) error

	// DrawBox draws a box
	DrawBox(x, y, width, height int) error

	// Flush commits all pending draws
	Flush() error
}

// ComponentTree manages the component hierarchy
type ComponentTree interface {
	// Mount adds a component to the tree
	Mount(parent string, child Component) error

	// Unmount removes a component from the tree
	Unmount(id string) error

	// Update triggers updates for a component and its children
	Update(id string) error

	// GetRoot returns the root component
	GetRoot() Component
}
