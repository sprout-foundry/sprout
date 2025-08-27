package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ToolCallingCapability defines what tool calling features a provider supports
type ToolCallingCapability int

const (
	// No native tool calling support - must use text-based fallbacks
	ToolCallingNone ToolCallingCapability = iota
	// Basic OpenAI-compatible function calling
	ToolCallingOpenAI
	// Advanced tool calling with custom schemas
	ToolCallingAdvanced
)

// ProviderToolSupport maps providers to their tool calling capabilities
var ProviderToolSupport = map[string]ToolCallingCapability{
	"openai":    ToolCallingOpenAI,
	"groq":      ToolCallingOpenAI,
	"gemini":    ToolCallingAdvanced,
	"deepseek":  ToolCallingOpenAI, // DeepSeek API supports OpenAI-compatible function calling
	"deepinfra": ToolCallingOpenAI, // DeepInfra supports OpenAI-compatible function calling
	"ollama":    ToolCallingNone,   // Most local models don't support tool calls
	"cerebras":  ToolCallingNone,   // Cerebras doesn't support tool calls
	"lambda":    ToolCallingNone,   // Lambda Labs doesn't support tool calls
}

// ModelSpecificToolSupport allows overriding provider defaults for specific models
var ModelSpecificToolSupport = map[string]ToolCallingCapability{
	// Example: Override provider default for specific models that differ
	// "someProvider:special-model-without-tools": ToolCallingNone,
	// "ollama:llama3.1:latest": ToolCallingOpenAI, // If specific Ollama model supports tools
	// Add model-specific overrides as needed
}

// ToolCallingStrategy determines how to handle tool calling for a specific provider
type ToolCallingStrategy struct {
	Provider   string
	Capability ToolCallingCapability
	UseNative  bool
}

// GetToolCallingStrategy determines the best tool calling approach for a provider
func GetToolCallingStrategy(modelName string) *ToolCallingStrategy {
	parts := strings.SplitN(modelName, ":", 2)
	provider := parts[0]

	// Check for model-specific overrides first
	capability, exists := ModelSpecificToolSupport[modelName]
	if !exists {
		// Fall back to provider default
		capability, exists = ProviderToolSupport[provider]
		if !exists {
			// Unknown provider, assume no native support
			capability = ToolCallingNone
		}
	}

	return &ToolCallingStrategy{
		Provider:   provider,
		Capability: capability,
		UseNative:  capability != ToolCallingNone,
	}
}

// OverrideProviderToolSupport allows runtime configuration of tool calling capabilities
// This can be used to test or configure providers dynamically
func OverrideProviderToolSupport(provider string, capability ToolCallingCapability) {
	ProviderToolSupport[provider] = capability
}

// OverrideModelToolSupport allows runtime configuration of model-specific tool calling capabilities
func OverrideModelToolSupport(modelName string, capability ToolCallingCapability) {
	ModelSpecificToolSupport[modelName] = capability
}

// PrepareToolsForProvider formats tools according to the provider's expected format
func (s *ToolCallingStrategy) PrepareToolsForProvider(tools []Tool) (interface{}, error) {
	switch s.Capability {
	case ToolCallingOpenAI:
		return s.formatOpenAITools(tools), nil
	case ToolCallingAdvanced:
		return s.formatAdvancedTools(tools), nil
	case ToolCallingNone:
		return nil, nil // No tools in request, use text-based fallback
	default:
		return nil, fmt.Errorf("unsupported tool calling capability: %d", s.Capability)
	}
}

// formatOpenAITools converts tools to OpenAI-compatible format
func (s *ToolCallingStrategy) formatOpenAITools(tools []Tool) []map[string]any {
	var formatted []map[string]any
	for _, tool := range tools {
		formatted = append(formatted, map[string]any{
			"type":     tool.Type,
			"function": tool.Function,
		})
	}
	return formatted
}

// formatAdvancedTools converts tools to advanced format (e.g., Gemini)
func (s *ToolCallingStrategy) formatAdvancedTools(tools []Tool) []map[string]any {
	// For now, use OpenAI format as baseline
	// Can be extended for provider-specific schemas
	return s.formatOpenAITools(tools)
}

// GetSystemPrompt returns the appropriate system prompt based on tool calling strategy
func (s *ToolCallingStrategy) GetSystemPrompt() string {
	if s.UseNative {
		return GetSystemMessageForNativeToolCalling()
	} else {
		return GetSystemMessageForTextToolCalling()
	}
}

// ParseToolCallsForProvider parses tool calls according to the provider's format
func (s *ToolCallingStrategy) ParseToolCallsForProvider(response string, nativeToolCalls interface{}) ([]ToolCall, error) {
	if s.UseNative && nativeToolCalls != nil {
		return s.parseNativeToolCalls(nativeToolCalls)
	}

	// Fall back to text parsing
	return ParseToolCalls(response)
}

// parseNativeToolCalls handles native tool call responses from providers
func (s *ToolCallingStrategy) parseNativeToolCalls(nativeToolCalls interface{}) ([]ToolCall, error) {
	switch s.Capability {
	case ToolCallingOpenAI:
		return s.parseOpenAIToolCalls(nativeToolCalls)
	case ToolCallingAdvanced:
		return s.parseAdvancedToolCalls(nativeToolCalls)
	default:
		return nil, fmt.Errorf("cannot parse native tool calls for capability: %d", s.Capability)
	}
}

// parseOpenAIToolCalls parses OpenAI-style native tool calls
func (s *ToolCallingStrategy) parseOpenAIToolCalls(nativeToolCalls interface{}) ([]ToolCall, error) {
	// Convert to our standard ToolCall format
	toolCallsBytes, err := json.Marshal(nativeToolCalls)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal native tool calls: %w", err)
	}

	var toolCalls []ToolCall
	if err := json.Unmarshal(toolCallsBytes, &toolCalls); err != nil {
		return nil, fmt.Errorf("failed to parse OpenAI tool calls: %w", err)
	}

	return normalizeToolCallArgs(toolCalls), nil
}

// parseAdvancedToolCalls parses advanced tool call formats (e.g., Gemini)
func (s *ToolCallingStrategy) parseAdvancedToolCalls(nativeToolCalls interface{}) ([]ToolCall, error) {
	// For now, try OpenAI format as baseline
	return s.parseOpenAIToolCalls(nativeToolCalls)
}

// GetSystemMessageForNativeToolCalling returns a system message for native tool calling
func GetSystemMessageForNativeToolCalling() string {
	return `You are an expert software developer with access to tools. Use the available tools to gather information and complete tasks.

Available tools will be provided in the tools array. When you need to use a tool, the system will handle the tool calling format automatically.

Key principles:
- Always read files before editing them
- Use run_shell_command (find, grep, ls) to discover relevant files
- Validate changes after making edits
- Provide clear explanations of your actions`
}

// GetSystemMessageForTextToolCalling returns a system message for text-based tool calling
func GetSystemMessageForTextToolCalling() string {
	return FormatToolsForPrompt()
}
