package api

import (
	"fmt"
	"strings"
)

// HarmonyFormatter handles conversion from OpenAI format to harmony format
type HarmonyFormatter struct {
	reasoningLevel string // low, medium, high
}

// HarmonyOptions configures the harmony formatting
type HarmonyOptions struct {
	ReasoningLevel string // "low", "medium", "high" - defaults to "high"
	EnableAnalysis bool   // Whether to enable analysis channel guidance
}

// FormatMessagesForCompletion converts OpenAI-style messages to harmony format
func (h *HarmonyFormatter) FormatMessagesForCompletion(messages []Message, tools []Tool, opts *HarmonyOptions) string {
	if opts == nil {
		opts = &HarmonyOptions{
			ReasoningLevel: "high",
			EnableAnalysis: true,
		}
	}

	// Ensure default reasoning level is set
	if opts.ReasoningLevel == "" {
		opts.ReasoningLevel = "high"
	}

	// Validate messages
	if err := h.validateMessages(messages); err != nil {
		// Log error but continue with available messages
		fmt.Printf("Warning: Message validation failed: %v\n", err)
	}

	var result strings.Builder

	// Process messages with proper channel support
	for i, msg := range messages {
		switch msg.Role {
		case "system":
			// Add reasoning level to system message
			systemContent := msg.Content
			if opts.ReasoningLevel != "" {
				systemContent += fmt.Sprintf("\n\nReasoning: %s", opts.ReasoningLevel)
			}
			result.WriteString(fmt.Sprintf("<|start|>system<|message|>%s<|end|>\n\n", systemContent))
		case "user":
			result.WriteString(fmt.Sprintf("<|start|>user<|message|>%s<|end|>", msg.Content))
			// Add newlines only if not the last message
			if i < len(messages)-1 {
				result.WriteString("\n\n")
			}
		case "assistant":
			// Assistant messages should specify channel
			result.WriteString(fmt.Sprintf("<|start|>assistant<|channel|>final<|message|>%s<|end|>\n\n", msg.Content))
		case "developer":
			result.WriteString(fmt.Sprintf("<|start|>developer<|message|>%s<|end|>\n\n", msg.Content))
		}
	}

	// Add tools to developer message if provided
	if len(tools) > 0 {
		result.WriteString("<|start|>developer<|message|># Available Tools\n\n")
		result.WriteString("## functions\n\n")
		result.WriteString("namespace functions {\n\n")

		for _, tool := range tools {
			if tool.Type == "function" {
				result.WriteString(fmt.Sprintf("// %s\ntype %s = (%s) => any;\n\n",
					tool.Function.Description,
					tool.Function.Name,
					h.formatToolParameters(tool.Function.Parameters)))
			}
		}

		result.WriteString("} // namespace functions\n\n")

		// Add tool calling instructions
		result.WriteString("## Tool Calling Instructions\n\n")
		result.WriteString("Call tools in the commentary channel using this format:\n")
		result.WriteString("`<|start|>assistant<|channel|>commentary to=functions.TOOL_NAME <|constrain|>json<|message|>{\"param\": \"value\"}<|call|>`\n\n")
		result.WriteString("After tool execution, provide your analysis in the analysis channel if needed, then give the final response in the final channel.<|end|>\n\n")
	}

	// Start assistant response - let model choose its own approach
	result.WriteString("<|start|>assistant<|channel|>final<|message|>")

	return result.String()
}

// formatToolParameters converts JSON schema to TypeScript-like parameters
func (h *HarmonyFormatter) formatToolParameters(params interface{}) string {
	if params == nil {
		return "_: any"
	}

	// Parse the JSON schema and convert to TypeScript-like syntax
	if paramsMap, ok := params.(map[string]interface{}); ok {
		if props, exists := paramsMap["properties"]; exists {
			if propsMap, ok := props.(map[string]interface{}); ok {
				var paramParts []string
				// Get required fields for better typing
				requiredFields := make(map[string]bool)
				if req, exists := paramsMap["required"]; exists {
					if reqSlice, ok := req.([]interface{}); ok {
						for _, field := range reqSlice {
							if fieldStr, ok := field.(string); ok {
								requiredFields[fieldStr] = true
							}
						}
					}
				}

				for paramName, paramDef := range propsMap {
					if defMap, ok := paramDef.(map[string]interface{}); ok {
						paramType := "string" // default
						if typeVal, exists := defMap["type"]; exists {
							if typeStr, ok := typeVal.(string); ok {
								paramType = typeStr
							}
						}
						// Add optional marker if not required
						optionalMarker := ""
						if !requiredFields[paramName] {
							optionalMarker = "?"
						}
						paramParts = append(paramParts, fmt.Sprintf("%s%s: %s", paramName, optionalMarker, paramType))
					}
				}
				if len(paramParts) > 0 {
					return fmt.Sprintf("{%s}", strings.Join(paramParts, ", "))
				}
			}
		}
	}

	return "_: any"
}

// validateMessages performs basic validation on messages
func (h *HarmonyFormatter) validateMessages(messages []Message) error {
	if len(messages) == 0 {
		return fmt.Errorf("no messages provided")
	}

	validRoles := map[string]bool{
		"system": true, "user": true, "assistant": true, "developer": true, "tool": true,
	}

	for i, msg := range messages {
		if !validRoles[msg.Role] {
			return fmt.Errorf("invalid role '%s' at message %d", msg.Role, i)
		}
		if strings.TrimSpace(msg.Content) == "" {
			return fmt.Errorf("empty content at message %d", i)
		}
	}

	return nil
}

// AddReturnToken adds the completion token to a harmony response
func (h *HarmonyFormatter) AddReturnToken(response string) string {
	if !strings.HasSuffix(response, "<|return|>") {
		return response + "<|return|>"
	}
	return response
}

// ConvertReturnToEnd converts <|return|> tokens to <|end|> for conversation history
func (h *HarmonyFormatter) ConvertReturnToEnd(conversation string) string {
	return strings.ReplaceAll(conversation, "<|return|>", "<|end|>")
}

// StripReturnToken removes <|return|> tokens from model responses
func (h *HarmonyFormatter) StripReturnToken(response string) string {
	return strings.TrimSuffix(strings.TrimSpace(response), "<|return|>")
}

// NewHarmonyFormatter creates a new harmony formatter
func NewHarmonyFormatter() *HarmonyFormatter {
	return &HarmonyFormatter{
		reasoningLevel: "high",
	}
}

// NewHarmonyFormatterWithReasoning creates a harmony formatter with specific reasoning level
func NewHarmonyFormatterWithReasoning(reasoning string) *HarmonyFormatter {
	return &HarmonyFormatter{
		reasoningLevel: reasoning,
	}
}
