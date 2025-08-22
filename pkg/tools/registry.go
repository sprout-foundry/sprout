package tools

import (
	"fmt"
	"sync"
)

// DefaultRegistry is the default implementation of Registry
type DefaultRegistry struct {
	tools map[string]Tool
	mutex sync.RWMutex
}

// NewDefaultRegistry creates a new default registry
func NewDefaultRegistry() *DefaultRegistry {
	return &DefaultRegistry{
		tools: make(map[string]Tool),
	}
}

// RegisterTool registers a new tool
func (r *DefaultRegistry) RegisterTool(tool Tool) error {
	if tool == nil {
		return fmt.Errorf("cannot register nil tool")
	}

	name := tool.Name()
	if name == "" {
		return fmt.Errorf("tool name cannot be empty")
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool %s is already registered", name)
	}

	r.tools[name] = tool
	return nil
}

// GetTool retrieves a tool by name
func (r *DefaultRegistry) GetTool(name string) (Tool, bool) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	tool, exists := r.tools[name]
	return tool, exists
}

// UnregisterTool removes a tool from the registry
func (r *DefaultRegistry) UnregisterTool(name string) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if _, exists := r.tools[name]; !exists {
		return fmt.Errorf("tool %s is not registered", name)
	}

	delete(r.tools, name)
	return nil
}

// ListTools returns all registered tools
func (r *DefaultRegistry) ListTools() []Tool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	tools := make([]Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, tool)
	}
	return tools
}

// ListToolsByCategory returns tools in a specific category
func (r *DefaultRegistry) ListToolsByCategory(category string) []Tool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	var categoryTools []Tool
	for _, tool := range r.tools {
		if tool.Category() == category {
			categoryTools = append(categoryTools, tool)
		}
	}
	return categoryTools
}

// GetToolNames returns the names of all registered tools
func (r *DefaultRegistry) GetToolNames() []string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// GetToolCount returns the number of registered tools
func (r *DefaultRegistry) GetToolCount() int {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	return len(r.tools)
}

// HasTool checks if a tool with the given name is registered
func (r *DefaultRegistry) HasTool(name string) bool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	_, exists := r.tools[name]
	return exists
}

// Clear removes all tools from the registry
func (r *DefaultRegistry) Clear() {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.tools = make(map[string]Tool)
}
