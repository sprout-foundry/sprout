package core

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// stringResult holds the result of a handler execution.
type stringResult struct {
	text string
	err  error
}

// imageResult holds the result of a HandlerWithImages execution. The images
// ride alongside the text so executeSingle can attach them to the tool-result
// Message instead of discarding them.
type imageResult struct {
	text   string
	images []ImageData
	err    error
}

// toolCallSlot holds a tool call with its original index for ordering.
type toolCallSlot struct {
	idx  int
	call ToolCall
}

// buildSchema generates a JSON Schema from ParameterConfig slice.
func buildSchema(params []ParameterConfig) ToolParameters {
	schema := ToolParameters{
		Type:       "object",
		Properties: make(map[string]ToolParameter),
		Required:   make([]string, 0),
	}
	for _, p := range params {
		schema.Properties[p.Name] = ToolParameter{
			Type:        p.Type,
			Description: p.Description,
		}
		if p.Required {
			schema.Required = append(schema.Required, p.Name)
		}
	}
	return schema
}

// parseAndValidateArgs parses JSON arguments and validates against the tool's
// parameter schema.
func parseAndValidateArgs(registry *ToolRegistry, canonicalName, rawName, rawArgs string) (map[string]interface{}, error) {
	// Attempt standard parse first.
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		// Try repair.
		repaired := repairJSON(rawArgs)
		if err := json.Unmarshal([]byte(repaired), &args); err != nil {
			return nil, fmt.Errorf("invalid JSON arguments: %w", err)
		}
	}

	// Resolve alternative parameter names.
	args = resolveAlternativeNames(registry, canonicalName, args)

	// Type coercion for string values that should be numbers/bools.
	args = coerceTypes(registry, canonicalName, args)

	// Validate required parameters.
	if err := validateRequired(registry, canonicalName, args); err != nil {
		return nil, err
	}

	return args, nil
}

// validateRequired checks that all required parameters are present in args.
func validateRequired(registry *ToolRegistry, canonicalName string, args map[string]interface{}) error {
	if registry == nil {
		return nil
	}

	registry.mu.RLock()
	entry, ok := registry.tools[canonicalName]
	registry.mu.RUnlock()

	if !ok || entry == nil {
		return nil
	}

	var missing []string
	for _, p := range entry.config.Parameters {
		if p.Required {
			// Case-insensitive lookup — some models (e.g. Gemma-4) send parameter
			// names with different casing (e.g. "Path" instead of "path").
			_, found := args[p.Name]
			if !found {
				lowerName := strings.ToLower(p.Name)
				for k := range args {
					if strings.ToLower(k) == lowerName {
						found = true
						break
					}
				}
			}
			if !found {
				missing = append(missing, p.Name)
			}
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required parameters: %s", strings.Join(missing, ", "))
	}
	return nil
}

// resolveAlternativeNames maps alternative parameter names to canonical names.
func resolveAlternativeNames(registry *ToolRegistry, canonicalName string, args map[string]interface{}) map[string]interface{} {
	if registry == nil {
		return args
	}

	registry.mu.RLock()
	entry, ok := registry.tools[canonicalName]
	registry.mu.RUnlock()

	if !ok || entry == nil {
		return args
	}

	// Build a map of alternative -> canonical for this tool.
	altMap := make(map[string]string)
	for _, p := range entry.config.Parameters {
		for _, alt := range p.Alternatives {
			altMap[strings.ToLower(alt)] = p.Name
		}
	}

	// Build a set of canonical parameter names (lowercased) for case-insensitive fallback.
	canonLower := make(map[string]string)
	for _, p := range entry.config.Parameters {
		canonLower[strings.ToLower(p.Name)] = p.Name
	}

	result := make(map[string]interface{})
	for key, val := range args {
		// 1. Check explicit alternatives first
		if resolved, ok := altMap[strings.ToLower(key)]; ok {
			result[resolved] = val
			continue
		}
		// 2. Case-insensitive canonical name match (e.g. "Path" → "path")
		if resolved, ok := canonLower[strings.ToLower(key)]; ok && resolved != key {
			result[resolved] = val
			continue
		}
		result[key] = val
	}
	return result
}

// coerceTypes performs type coercion on arguments.
func coerceTypes(registry *ToolRegistry, canonicalName string, args map[string]interface{}) map[string]interface{} {
	if registry == nil {
		return args
	}

	var entry *toolEntry
	var ok bool

	registry.mu.RLock()
	entry, ok = registry.tools[canonicalName]
	registry.mu.RUnlock()

	if !ok || entry == nil {
		return args
	}

	// Build type map for expected types.
	typeMap := make(map[string]string)
	for _, p := range entry.config.Parameters {
		typeMap[p.Name] = p.Type
	}

	result := make(map[string]interface{})
	for key, val := range args {
		if expected, ok := typeMap[key]; ok {
			result[key] = coerceValue(val, expected)
		} else {
			result[key] = val
		}
	}
	return result
}

// coerceValue attempts to convert val to the expected type.
func coerceValue(val interface{}, expectedType string) interface{} {
	switch expectedType {
	case "number", "integer":
		if str, ok := val.(string); ok {
			if n, err := strconv.ParseFloat(str, 64); err == nil {
				if expectedType == "integer" {
					return int64(n)
				}
				return n
			}
		}
		if f, ok := val.(float64); ok && expectedType == "integer" {
			return int64(f)
		}
	case "boolean":
		if str, ok := val.(string); ok {
			if b, err := strconv.ParseBool(strings.ToLower(str)); err == nil {
				return b
			}
		}
	}
	return val
}

// repairJSON attempts to fix common JSON issues.
func repairJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "null" {
		return "{}"
	}

	// Try trailing comma removal.
	fixed := strings.ReplaceAll(s, ",}", "}")
	fixed = strings.ReplaceAll(fixed, ",]", "]")
	if json.Valid([]byte(fixed)) {
		return fixed
	}

	// Try single-quote to double-quote conversion.
	fixed = strings.ReplaceAll(s, "'", "\"")
	if json.Valid([]byte(fixed)) {
		return fixed
	}

	// Try wrapping bare content in braces.
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		wrapped := "{" + trimmed + "}"
		if json.Valid([]byte(wrapped)) {
			return wrapped
		}
	}

	// Return original if no repair worked.
	return s
}

// truncateResult truncates the result if it exceeds maxSize.
func truncateResult(result string, maxSize int) string {
	if maxSize <= 0 || len(result) <= maxSize {
		return result
	}
	return result[:maxSize] + fmt.Sprintf("\n... (truncated, %d chars total)", len(result))
}

// ToolResultMessage creates a Message for a successful tool result.
// The returned Message has Status set to ToolStatusCompleted so the chat
// loop's tool_end event carries the right status without callers having
// to remember to tag it.
func ToolResultMessage(toolCallID, toolName string, content string) Message {
	return Message{
		Role:       "tool",
		Content:    content,
		ToolCallID: toolCallID,
		Status:     ToolStatusCompleted,
	}
}

// ToolResultMessageWithImages creates a Message for a successful tool
// result that also carries image data. Providers that support multimodal
// tool results (vision-capable models) embed the images into the request;
// ConversationHandler.prepareMessages strips them for non-vision models
// via stripImages, so callers don't need to gate on provider capability.
// toolName is accepted for symmetry with ToolResultMessage; the provider
// conversion layer keys off Role/ToolCallID, not the name.
func ToolResultMessageWithImages(toolCallID, toolName string, content string, images []ImageData) Message {
	msg := ToolResultMessage(toolCallID, toolName, content)
	if len(images) > 0 {
		imgCopy := make([]ImageData, len(images))
		copy(imgCopy, images)
		msg.Images = imgCopy
	}
	return msg
}

// ToolErrorMessage creates a Message for a failed tool result. Status is
// set to ToolStatusError so the chat loop's tool_end event publishes the
// "error" status and consumers (CLI, WebUI) can render error indicators.
// errMsg becomes the Message Content — keep it concise and human-readable;
// the LLM sees it as the tool's output.
func ToolErrorMessage(toolCallID, toolName string, errMsg string) Message {
	return Message{
		Role:       "tool",
		Content:    errMsg,
		ToolCallID: toolCallID,
		Status:     ToolStatusError,
	}
}

// toolMaxResultSize returns the max result size for a tool (per-tool or global).
func (r *ToolRegistry) toolMaxResultSize(name string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, exists := r.tools[name]
	if !exists {
		return 0
	}
	if entry.config.MaxResultSize > 0 {
		return entry.config.MaxResultSize
	}
	return r.maxResultSize
}
