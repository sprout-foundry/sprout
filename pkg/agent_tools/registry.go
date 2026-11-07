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

// HostProvided is implemented by a ToolHandler a host registered at run time
// (the headless agent API's registerHostTool) rather than one of Sprout's
// built-in handlers. It lets the roster tell a host's tools from the
// built-ins without the agent importing the host's code.
type HostProvided interface {
	// HostProvidedTool marks the handler as host-registered.
	HostProvidedTool() bool
}

// RegisteredHostToolNames returns the names of the registered host-provided
// tools, sorted. Resolving this at roster-build time is what lets a persona
// whose host_tools contains "*" be offered a tool the host registers after
// the agent was created (SP-175).
func RegisteredHostToolNames() []string {
	var names []string
	for name, handler := range GetNewToolRegistry().All() {
		if hp, ok := handler.(HostProvided); ok && hp.HostProvidedTool() {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// IsHostProvidedTool reports whether the named registered tool came from the
// host rather than Sprout's built-ins.
func IsHostProvidedTool(name string) bool {
	handler, ok := GetNewToolRegistry().Lookup(name)
	if !ok {
		return false
	}
	hp, ok := handler.(HostProvided)
	return ok && hp.HostProvidedTool()
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

// ForPersona returns the subset of registered tools whose names appear in
// allowlist. An empty or nil allowlist returns every tool (matching the
// behavior of unrestricted personas). Tool names present in allowlist but not
// in the registry are silently skipped — callers shouldn't have to defend
// against stale allowlists.
//
// The returned map is a copy; callers may mutate it without affecting the
// registry's state.
func (r *ToolRegistry) ForPersona(allowlist []string) map[string]ToolHandler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(allowlist) == 0 {
		out := make(map[string]ToolHandler, len(r.tools))
		for k, v := range r.tools {
			out[k] = v
		}
		return out
	}
	out := make(map[string]ToolHandler, len(allowlist))
	for _, name := range allowlist {
		if h, ok := r.tools[name]; ok {
			out[name] = h
		}
	}
	return out
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
