package core

import (
	"context"
	"sync"
)

// BaseComponent provides common component functionality
type BaseComponent struct {
	mu          sync.RWMutex
	id          string
	props       Props
	localState  State
	store       Store
	children    []Component
	needsRender bool

	// Lifecycle callbacks
	onMount   func()
	onUnmount func()
	onUpdate  func(oldProps, newProps Props)
}

// NewBaseComponent creates a new base component
func NewBaseComponent(id string, store Store) *BaseComponent {
	return &BaseComponent{
		id:         id,
		props:      make(Props),
		localState: make(State),
		store:      store,
		children:   make([]Component, 0),
	}
}

// GetID returns the component's ID
func (c *BaseComponent) GetID() string {
	return c.id
}

// SetProps updates the component's properties
func (c *BaseComponent) SetProps(props Props) {
	c.mu.Lock()
	defer c.mu.Unlock()

	oldProps := c.props
	c.props = deepCopyProps(props)

	// Check if update is needed
	if c.ShouldUpdate(oldProps, c.props, nil, nil) {
		c.needsRender = true
		if c.onUpdate != nil {
			c.onUpdate(oldProps, c.props)
		}
	}
}

// GetProps returns a copy of the component's props
func (c *BaseComponent) GetProps() Props {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return deepCopyProps(c.props)
}

// GetState returns the component's local state
func (c *BaseComponent) GetState() State {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return deepCopyState(c.localState)
}

// SetState updates local state
func (c *BaseComponent) SetState(state State) {
	c.mu.Lock()
	defer c.mu.Unlock()

	oldState := c.localState
	c.localState = deepCopyState(state)

	if !statesEqual(oldState, c.localState) {
		c.needsRender = true
	}
}

// UpdateState partially updates local state
func (c *BaseComponent) UpdateState(updates State) {
	c.mu.Lock()
	defer c.mu.Unlock()

	changed := false
	for k, v := range updates {
		if oldVal, exists := c.localState[k]; !exists || !valuesEqual(oldVal, v) {
			c.localState[k] = v
			changed = true
		}
	}

	if changed {
		c.needsRender = true
	}
}

// ShouldUpdate determines if the component needs re-rendering
func (c *BaseComponent) ShouldUpdate(oldProps, newProps Props, oldState, newState State) bool {
	// Default implementation: update if props or state changed
	if !propsEqual(oldProps, newProps) {
		return true
	}

	if oldState != nil && newState != nil && !statesEqual(oldState, newState) {
		return true
	}

	return false
}

// Render is the default render implementation (should be overridden)
func (c *BaseComponent) Render(ctx context.Context) error {
	c.mu.Lock()
	c.needsRender = false
	c.mu.Unlock()

	// Render children
	for _, child := range c.children {
		if err := child.Render(ctx); err != nil {
			return err
		}
	}

	return nil
}

// Mount mounts the component
func (c *BaseComponent) Mount() {
	if c.onMount != nil {
		c.onMount()
	}
}

// Unmount unmounts the component
func (c *BaseComponent) Unmount() {
	// Unmount children first
	for _, child := range c.children {
		if mountable, ok := child.(interface{ Unmount() }); ok {
			mountable.Unmount()
		}
	}

	if c.onUnmount != nil {
		c.onUnmount()
	}
}

// AddChild adds a child component
func (c *BaseComponent) AddChild(child Component) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.children = append(c.children, child)
}

// RemoveChild removes a child component
func (c *BaseComponent) RemoveChild(childID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i, child := range c.children {
		if child.GetID() == childID {
			// Unmount the child
			if mountable, ok := child.(interface{ Unmount() }); ok {
				mountable.Unmount()
			}

			// Remove from slice
			c.children = append(c.children[:i], c.children[i+1:]...)
			break
		}
	}
}

// Dispatch dispatches an action to the store
func (c *BaseComponent) Dispatch(action Action) {
	if c.store != nil {
		c.store.Dispatch(action)
	}
}

// Select selects state from the store
func (c *BaseComponent) Select(selector func(State) interface{}) interface{} {
	if c.store != nil {
		return c.store.Select(selector)
	}
	return nil
}

// Subscribe subscribes to store changes
func (c *BaseComponent) Subscribe(handler func(State)) func() {
	if c.store != nil {
		return c.store.Subscribe(handler)
	}
	return func() {}
}

// Helper functions

func deepCopyProps(props Props) Props {
	if props == nil {
		return nil
	}

	newProps := make(Props)
	for k, v := range props {
		newProps[k] = deepCopyValue(v)
	}
	return newProps
}

func propsEqual(p1, p2 Props) bool {
	if len(p1) != len(p2) {
		return false
	}

	for k, v1 := range p1 {
		v2, ok := p2[k]
		if !ok || !valuesEqual(v1, v2) {
			return false
		}
	}

	return true
}
