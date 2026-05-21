// Tool registry for registering and looking up ToolHandler implementations.
//
// The registry is thread-safe via sync.RWMutex and provides deterministic
// (sorted) output for All(), Names(), and ForPersona().
package tools

import (
	"sort"
	"sync"
)

// ToolRegistry provides a thread-safe registry for tool handlers.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]ToolHandler
}

// NewToolRegistry creates an empty registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]ToolHandler),
	}
}

// Register adds a tool handler to the registry. Panics if a handler with the
// same name is already registered.
func (r *ToolRegistry) Register(handler ToolHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := handler.Name()
	if _, exists := r.tools[name]; exists {
		panic("tool already registered: " + name)
	}
	r.tools[name] = handler
}

// Lookup returns a tool handler by name, or nil if not found.
func (r *ToolRegistry) Lookup(name string) ToolHandler {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.tools[name]
}

// All returns all registered tool handlers, sorted by name.
func (r *ToolRegistry) All() []ToolHandler {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]ToolHandler, 0, len(r.tools))
	for _, h := range r.tools {
		result = append(result, h)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name() < result[j].Name()
	})
	return result
}

// ForPersona returns tools allowed for a given persona by filtering the
// registered handlers against the provided allowlist. If allowedTools is nil
// or empty, all registered tools are returned.
func (r *ToolRegistry) ForPersona(allowedTools []string) []ToolHandler {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(allowedTools) == 0 {
		result := make([]ToolHandler, 0, len(r.tools))
		for _, h := range r.tools {
			result = append(result, h)
		}
		sort.Slice(result, func(i, j int) bool {
			return result[i].Name() < result[j].Name()
		})
		return result
	}

	allowed := make(map[string]struct{}, len(allowedTools))
	for _, n := range allowedTools {
		allowed[n] = struct{}{}
	}

	result := make([]ToolHandler, 0)
	for _, h := range r.tools {
		if _, ok := allowed[h.Name()]; ok {
			result = append(result, h)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name() < result[j].Name()
	})
	return result
}

// Names returns all registered tool names, sorted alphabetically.
func (r *ToolRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
