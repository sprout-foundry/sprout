package tools

import (
	"fmt"
	"slices"
	"sync"
)

// ---------------------------------------------------------------------------
// Global singleton for new-style tool registry (SP-038-2a dual-dispatch)
// ---------------------------------------------------------------------------

var (
	defaultNewRegistry *ToolRegistry
	newRegistryOnce    sync.Once
)

// GetNewToolRegistry returns the global new-style tool registry singleton.
func GetNewToolRegistry() *ToolRegistry {
	newRegistryOnce.Do(func() {
		defaultNewRegistry = NewToolRegistry()
		for _, h := range AllTools() {
			if err := defaultNewRegistry.Register(h); err != nil {
				// Log but don't panic - this is initialization code
				fmt.Printf("WARNING: failed to register tool %q: %v\n", h.Name(), err)
			}
		}
	})
	return defaultNewRegistry
}

// ToolRegistry provides thread-safe registration and lookup of ToolHandlers.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]ToolHandler
}

// NewToolRegistry creates an empty ToolRegistry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]ToolHandler),
	}
}

// Register adds a tool handler. Returns error if name is already registered.
func (r *ToolRegistry) Register(handler ToolHandler) error {
	name := handler.Name()
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool %q is already registered", name)
	}
	r.tools[name] = handler
	return nil
}

// Lookup finds a tool by name. Returns (handler, true) if found, (nil, false) otherwise.
func (r *ToolRegistry) Lookup(name string) (ToolHandler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.tools[name]
	return h, ok
}

// All returns a copy of all registered tools.
func (r *ToolRegistry) All() map[string]ToolHandler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]ToolHandler, len(r.tools))
	for k, v := range r.tools {
		out[k] = v
	}
	return out
}

// ForPersona returns tools available for a given persona. Initially returns all tools.
// TODO(SP-038): Implement per-persona tool filtering
func (r *ToolRegistry) ForPersona(persona string) map[string]ToolHandler {
	return r.All()
}

// Unregister removes a tool handler by name. Returns true if the tool was found and removed.
func (r *ToolRegistry) Unregister(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[name]; !exists {
		return false
	}
	delete(r.tools, name)
	return true
}

// Names returns a sorted list of all registered tool names.
func (r *ToolRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
