package core

import (
	"context"
	"fmt"
)

// App represents the main application that manages components and state
type App struct {
	store         Store
	rootComponent Component
	renderer      Renderer
	components    map[string]Component
}

// NewApp creates a new application
func NewApp() *App {
	// Create the store with root reducer
	store := NewStore(RootReducer(), State{
		"ui":       State{},
		"terminal": State{"width": 80, "height": 24, "rawMode": false},
		"input":    State{"text": "", "cursorPos": 0},
		"focus":    State{},
		"command":  State{},
	})

	return &App{
		store:      store,
		components: make(map[string]Component),
	}
}

// SetRenderer sets the renderer for the app
func (a *App) SetRenderer(renderer Renderer) {
	a.renderer = renderer
}

// GetStore returns the app's store
func (a *App) GetStore() Store {
	return a.store
}

// Mount mounts a component to the app
func (a *App) Mount(component Component) {
	a.components[component.GetID()] = component

	// If this is the first component, make it root
	if a.rootComponent == nil {
		a.rootComponent = component
	}

	// Call mount lifecycle
	if mountable, ok := component.(interface{ Mount() }); ok {
		mountable.Mount()
	}
}

// Unmount unmounts a component from the app
func (a *App) Unmount(componentID string) {
	if component, exists := a.components[componentID]; exists {
		// Call unmount lifecycle
		if unmountable, ok := component.(interface{ Unmount() }); ok {
			unmountable.Unmount()
		}

		delete(a.components, componentID)

		// If this was root, clear it
		if a.rootComponent != nil && a.rootComponent.GetID() == componentID {
			a.rootComponent = nil
		}
	}
}

// Render renders the entire app
func (a *App) Render(ctx context.Context) error {
	if a.rootComponent == nil {
		return fmt.Errorf("no root component mounted")
	}

	if a.renderer == nil {
		return fmt.Errorf("no renderer set")
	}

	// Clear before rendering
	if err := a.renderer.Clear(); err != nil {
		return err
	}

	// Render root component (which renders children)
	if err := a.rootComponent.Render(ctx); err != nil {
		return err
	}

	// Flush renderer
	return a.renderer.Flush()
}

// HandleInput routes input to the appropriate component
func (a *App) HandleInput(input []byte) error {
	// Simple approach: if we have a dropdown component, it gets all input
	// This avoids complex state checking
	if dropdown, exists := a.components["dropdown"]; exists {
		if handler, ok := dropdown.(interface{ HandleInput([]byte) error }); ok {
			return handler.HandleInput(input)
		}
	}

	// Otherwise use focus system
	focusedID, _ := a.store.Select(func(s State) interface{} {
		focus, _ := s["focus"].(State)
		return focus["focusedComponent"]
	}).(string)

	if focusedID != "" {
		if component, exists := a.components[focusedID]; exists {
			if handler, ok := component.(interface{ HandleInput([]byte) error }); ok {
				return handler.HandleInput(input)
			}
		}
	}

	// Fall back to root component
	if a.rootComponent != nil {
		if handler, ok := a.rootComponent.(interface{ HandleInput([]byte) error }); ok {
			return handler.HandleInput(input)
		}
	}

	return nil
}

// Dispatch dispatches an action to the store
func (a *App) Dispatch(action Action) {
	a.store.Dispatch(action)
}

// ShowDropdown is a convenience method to show a dropdown
func (a *App) ShowDropdown(dropdownID string, items []interface{}, options map[string]interface{}) {
	// Dispatch action to show dropdown
	a.Dispatch(ShowDropdownAction(dropdownID, items, options))

	// Focus the dropdown
	a.Dispatch(FocusComponentAction(dropdownID))
}
