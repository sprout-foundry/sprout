package providers

import (
	"fmt"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// MessageConversionOptions controls how agent messages are transformed into
// OpenAI-compatible payloads.
type MessageConversionOptions struct {
	// Convert tool-role messages to user messages with a labeled prefix. Some
	// providers (DeepInfra) reject the "tool" role entirely.
	ConvertToolRoleToUser bool
	// Include tool_call_id when present. Required for OpenRouter when sending
	// tool execution results back to the model.
	IncludeToolCallID bool
}

// BuildOpenAIChatMessages converts agent messages into OpenAI/OpenRouter style
// chat message payloads, including multimodal content where necessary.
func BuildOpenAIChatMessages(messages []api.Message, opts MessageConversionOptions) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(messages))

	for _, msg := range messages {
		role := msg.Role
		content := msg.Content

		if opts.ConvertToolRoleToUser && role == "tool" {
			role = "user"
			if strings.TrimSpace(content) == "" {
				content = "Tool Response received with no textual output."
			}
			content = fmt.Sprintf("Tool Response:\n%s", content)
		}

		messageMap := map[string]interface{}{
			"role": role,
		}

		if len(msg.Images) > 0 {
			contentArray := []map[string]interface{}{}
			if strings.TrimSpace(content) != "" {
				contentArray = append(contentArray, map[string]interface{}{
					"type": "text",
					"text": content,
				})
			}

			for _, img := range msg.Images {
				imagePayload := map[string]interface{}{}
				switch {
				case img.URL != "":
					imagePayload["image_url"] = map[string]interface{}{"url": img.URL}
				case img.Base64 != "":
					mimeType := img.Type
					if mimeType == "" {
						mimeType = "image/jpeg"
					}
					dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, img.Base64)
					imagePayload["image_url"] = map[string]interface{}{"url": dataURL}
				default:
					continue
				}
				imagePayload["type"] = "image_url"
				contentArray = append(contentArray, imagePayload)
			}

			messageMap["content"] = contentArray
		} else {
			messageMap["content"] = content
		}

		if opts.IncludeToolCallID && msg.ToolCallId != "" {
			messageMap["tool_call_id"] = msg.ToolCallId
		}

		// Include tool_calls for assistant messages (critical for tool conversation flow)
		if len(msg.ToolCalls) > 0 {
			toolCalls := make([]map[string]interface{}, len(msg.ToolCalls))
			for i, tc := range msg.ToolCalls {
				toolCall := map[string]interface{}{
					"id":   tc.ID,
					"type": tc.Type,
					"function": map[string]interface{}{
						"name":      tc.Function.Name,
						"arguments": tc.Function.Arguments,
					},
				}
				toolCalls[i] = toolCall
			}
			messageMap["tool_calls"] = toolCalls
		}

		result = append(result, messageMap)
	}

	return result
}

// BuildOpenAIStreamingMessages converts messages for streaming endpoints. The
// content is identical to the chat payload, but represented as []interface{} to
// match the JSON marshalling performed by providers.
func BuildOpenAIStreamingMessages(messages []api.Message, opts MessageConversionOptions) []interface{} {
	chatMessages := BuildOpenAIChatMessages(messages, opts)
	result := make([]interface{}, len(chatMessages))
	for i, msg := range chatMessages {
		result[i] = msg
	}
	return result
}

// BuildOpenAIToolsPayload normalises internal tool definitions to the OpenAI
// function-calling schema used by OpenRouter, DeepInfra, and other compatible
// providers.
func BuildOpenAIToolsPayload(tools []api.Tool) []map[string]interface{} {
	if len(tools) == 0 {
		return nil
	}

	openAITools := make([]map[string]interface{}, len(tools))
	for i, tool := range tools {
		openAITools[i] = map[string]interface{}{
			"type": tool.Type,
			"function": map[string]interface{}{
				"name":        tool.Function.Name,
				"description": tool.Function.Description,
				"parameters":  tool.Function.Parameters,
			},
		}
	}
	return openAITools
}

// EstimateInputTokens provides a quick upper bound for prompt tokens based on
// message lengths and attached tool metadata.
func EstimateInputTokens(messages []api.Message, tools []api.Tool) int {
	inputTokens := 0
	for _, msg := range messages {
		inputTokens += len(msg.Content) / 4
	}
	inputTokens += len(tools) * 200
	inputTokens += 500 // buffer for system instructions / formatting
	return inputTokens
}

// CalculateMaxTokens returns an appropriate max_tokens value given the context
// window and prompt size. The caller passes the effective context limit, making
// it easy to reuse across providers with custom limit lookups.
func CalculateMaxTokens(contextLimit int, messages []api.Message, tools []api.Tool) int {
	if contextLimit == 0 {
		contextLimit = 32000
	}

	inputTokens := 0
	for _, msg := range messages {
		inputTokens += len(msg.Content) / 4
	}
	inputTokens += len(tools) * 200

	maxOutput := contextLimit - inputTokens - 1000
	if maxOutput > 16000 {
		maxOutput = 16000
	} else if maxOutput < 1000 {
		maxOutput = 1000
	}
	return maxOutput
}
