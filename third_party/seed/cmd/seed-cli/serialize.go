package main

import (
	"github.com/sprout-foundry/seed/core"
)

// mapToMap converts a ChatRequest to a serializable map.
func mapToMap(req *core.ChatRequest) map[string]interface{} {
	result := map[string]interface{}{
		"model":     req.Model,
		"messages":  msgSlice(req.Messages),
		"maxTokens": req.MaxTokens,
		"stream":    req.Stream,
	}
	if len(req.Tools) > 0 {
		result["tools"] = toolSlice(req.Tools)
	}
	if req.ToolChoice != "" {
		result["toolChoice"] = req.ToolChoice
	}
	return result
}

func msgSlice(msgs []core.Message) []map[string]interface{} {
	out := make([]map[string]interface{}, len(msgs))
	for i, m := range msgs {
		out[i] = map[string]interface{}{
			"role":    m.Role,
			"content": m.Content,
		}
		if m.ReasoningContent != "" {
			out[i]["reasoningContent"] = m.ReasoningContent
		}
		if m.ToolCallID != "" {
			out[i]["toolCallId"] = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			out[i]["toolCalls"] = tcSlice(m.ToolCalls)
		}
	}
	return out
}

func tcSlice(calls []core.ToolCall) []map[string]interface{} {
	out := make([]map[string]interface{}, len(calls))
	for i, c := range calls {
		out[i] = map[string]interface{}{
			"id":   c.ID,
			"type": c.Type,
			"function": map[string]interface{}{
				"name":      c.Function.Name,
				"arguments": c.Function.Arguments,
			},
		}
	}
	return out
}

func toolSlice(tools []core.Tool) []map[string]interface{} {
	out := make([]map[string]interface{}, len(tools))
	for i, t := range tools {
		out[i] = map[string]interface{}{
			"type": t.Type,
			"function": map[string]interface{}{
				"name":        t.Function.Name,
				"description": t.Function.Description,
				"parameters":  t.Function.Parameters,
			},
		}
	}
	return out
}
