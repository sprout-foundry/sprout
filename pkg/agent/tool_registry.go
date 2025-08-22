package agent

import (
	"fmt"
	"sync"
)

// DefaultToolRegistry is the default implementation of ToolRegistry
type DefaultToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewToolRegistry creates a new tool registry
func NewToolRegistry() ToolRegistry {
	return &DefaultToolRegistry{
		tools: make(map[string]Tool),
	}
}

// Register registers a new tool
func (r *DefaultToolRegistry) Register(tool Tool) error {
	if tool == nil {
		return fmt.Errorf("cannot register nil tool")
	}

	name := tool.Name()
	if name == "" {
		return fmt.Errorf("cannot register tool with empty name")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool %s is already registered", name)
	}

	r.tools[name] = tool
	return nil
}

// Get retrieves a tool by name
func (r *DefaultToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tool, exists := r.tools[name]
	return tool, exists
}

// List returns all registered tools
func (r *DefaultToolRegistry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tools := make([]Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, tool)
	}
	return tools
}

// ListByCategory returns tools filtered by category
func (r *DefaultToolRegistry) ListByCategory(category string) []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var tools []Tool
	for _, tool := range r.tools {
		if tool.Category() == category {
			tools = append(tools, tool)
		}
	}
	return tools
}

// IsRegistered checks if a tool is registered
func (r *DefaultToolRegistry) IsRegistered(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.tools[name]
	return exists
}

// Unregister removes a tool from the registry
func (r *DefaultToolRegistry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[name]; !exists {
		return fmt.Errorf("tool %s is not registered", name)
	}

	delete(r.tools, name)
	return nil
}

// GetToolNames returns all registered tool names
func (r *DefaultToolRegistry) GetToolNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// GetToolCategories returns all unique categories
func (r *DefaultToolRegistry) GetToolCategories() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	categorySet := make(map[string]bool)
	for _, tool := range r.tools {
		categorySet[tool.Category()] = true
	}

	categories := make([]string, 0, len(categorySet))
	for category := range categorySet {
		categories = append(categories, category)
	}
	return categories
}

// GetToolsByCategories returns a map of category to tools
func (r *DefaultToolRegistry) GetToolsByCategories() map[string][]Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string][]Tool)
	for _, tool := range r.tools {
		category := tool.Category()
		result[category] = append(result[category], tool)
	}
	return result
}
