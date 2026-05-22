package agent

import (
	"context"
	"sync"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// ParameterConfig defines parameter validation rules for a tool
type ParameterConfig struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"` // "string", "integer", "number", "boolean"
	Required     bool     `json:"required"`
	Alternatives []string `json:"alternatives"` // Alternative parameter names for backward compatibility
	Description  string   `json:"description"`
}

// ToolConfig holds configuration for a tool
type ToolConfig struct {
	Name          string                `json:"name"`
	Description   string                `json:"description"`
	Parameters    []ParameterConfig     `json:"parameters"`
	Handler       ToolHandler           `json:"-"` // Function reference, not serialized
	HandlerImages ToolHandlerWithImages `json:"-"` // Optional image-returning handler (takes precedence over Handler when set)
}

// ToolHandler represents a function that can handle a tool execution
type ToolHandler func(ctx context.Context, a *Agent, args map[string]interface{}) (string, error)

// ToolHandlerWithImages is like ToolHandler but can also return image data
// for multimodal (vision-capable) models. The []api.ImageData slice should be
// nil when no images are produced; the string is always the text result.
type ToolHandlerWithImages func(ctx context.Context, a *Agent, args map[string]interface{}) ([]api.ImageData, string, error)

// ToolRegistry manages tool configurations in a data-driven way
type ToolRegistry struct {
	tools map[string]ToolConfig
}

var defaultToolRegistry *ToolRegistry
var registryOnce sync.Once

// GetToolRegistry returns the default tool registry, initializing it lazily if needed (thread-safe)
func GetToolRegistry() *ToolRegistry {
	registryOnce.Do(func() {
		defaultToolRegistry = newDefaultToolRegistry()
	})
	return defaultToolRegistry
}

// InitializeToolRegistry pre-creates the tool registry to avoid first-use overhead
// This should be called during agent initialization for better performance
func InitializeToolRegistry() {
	registryOnce.Do(func() {
		defaultToolRegistry = newDefaultToolRegistry()
	})
}

// RegisterTool adds a tool to the registry
func (r *ToolRegistry) RegisterTool(config ToolConfig) {
	r.tools[config.Name] = config
}

// GetAvailableTools returns a list of all registered tool names
func (r *ToolRegistry) GetAvailableTools() []string {
	tools := make([]string, 0, len(r.tools))
	for toolName := range r.tools {
		tools = append(tools, toolName)
	}
	return tools
}

// GetToolConfig returns the ToolConfig for the given tool name.
// Returns the config and true if found, or zero-value and false if not.
func (r *ToolRegistry) GetToolConfig(name string) (ToolConfig, bool) {
	config, ok := r.tools[name]
	return config, ok
}

// GetAllToolConfigs returns a copy of all registered tool configs keyed by name.
func (r *ToolRegistry) GetAllToolConfigs() map[string]ToolConfig {
	result := make(map[string]ToolConfig, len(r.tools))
	for name, config := range r.tools {
		result[name] = config
	}
	return result
}
