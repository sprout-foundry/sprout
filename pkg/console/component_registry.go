package console

import (
	"fmt"
	"sync"
)

// ComponentFactory creates component instances
type ComponentFactory func() Component

// ComponentRegistry manages component factories
type ComponentRegistry struct {
	mu        sync.RWMutex
	factories map[string]ComponentFactory
}

// NewComponentRegistry creates a new component registry
func NewComponentRegistry() *ComponentRegistry {
	return &ComponentRegistry{
		factories: make(map[string]ComponentFactory),
	}
}

// Register registers a component factory
func (r *ComponentRegistry) Register(componentType string, factory ComponentFactory) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.factories[componentType]; exists {
		return fmt.Errorf("component type %s already registered", componentType)
	}

	r.factories[componentType] = factory
	return nil
}

// Create creates a new component instance
func (r *ComponentRegistry) Create(componentType string) (Component, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	factory, exists := r.factories[componentType]
	if !exists {
		return nil, fmt.Errorf("unknown component type: %s", componentType)
	}

	return factory(), nil
}

// ListTypes returns all registered component types
func (r *ComponentRegistry) ListTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.factories))
	for t := range r.factories {
		types = append(types, t)
	}
	return types
}

// Global registry instance
var globalRegistry = NewComponentRegistry()

// RegisterComponent registers a component factory globally
func RegisterComponent(componentType string, factory ComponentFactory) error {
	return globalRegistry.Register(componentType, factory)
}

// CreateComponent creates a component from the global registry
func CreateComponent(componentType string) (Component, error) {
	return globalRegistry.Create(componentType)
}
