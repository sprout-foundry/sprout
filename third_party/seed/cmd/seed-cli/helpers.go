package main

import (
	"context"
	"time"

	"github.com/sprout-foundry/seed/core"
)

// parseMessages converts raw JSON message arrays to []core.Message.
func parseMessages(raw []interface{}) []core.Message {
	msgs := make([]core.Message, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]interface{}); ok {
			msg := core.Message{Role: getStringVal(m, "role"), Content: getStringVal(m, "content")}
			if id, ok := m["tool_call_id"].(string); ok {
				msg.ToolCallID = id
			}
			if tcs, ok := m["tool_calls"].([]interface{}); ok {
				msg.ToolCalls = parseToolCalls(tcs)
			}
			msgs = append(msgs, msg)
		}
	}
	return msgs
}

func parseCheckpoints(raw []interface{}) []core.TurnCheckpoint {
	cps := make([]core.TurnCheckpoint, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]interface{}); ok {
			cps = append(cps, core.TurnCheckpoint{
				StartIndex:        getIntVal(m, "start_index"),
				EndIndex:          getIntVal(m, "end_index"),
				UserMessage:       getStringVal(m, "user_message"),
				Summary:           getStringVal(m, "summary"),
				ActionableSummary: getStringVal(m, "actionable_summary"),
			})
		}
	}
	return cps
}

func parseRetryConfig(m map[string]interface{}) core.RetryConfig {
	rc := core.RetryConfig{}
	rc.MaxAttempts = getIntVal(m, "maxAttempts")
	if v, ok := m["initialDelay"]; ok {
		if f, ok := v.(float64); ok {
			rc.InitialDelay = time.Duration(f) * time.Millisecond
		}
	}
	if v, ok := m["maxDelay"]; ok {
		if f, ok := v.(float64); ok {
			rc.MaxDelay = time.Duration(f) * time.Millisecond
		}
	}
	if v, ok := m["multiplier"]; ok {
		if f, ok := v.(float64); ok {
			rc.Multiplier = f
		}
	}
	if v, ok := m["jitter"]; ok {
		if f, ok := v.(float64); ok {
			rc.Jitter = f
		}
	}
	return rc
}

func parseOptimizer(m map[string]interface{}) *core.ConversationOptimizer {
	cats, ok := m["toolCategories"].(map[string]interface{})
	if !ok || len(cats) == 0 {
		return nil
	}
	catMap := make(map[string]string)
	for name, cat := range cats {
		if s, ok := cat.(string); ok {
			catMap[name] = s
		}
	}
	fn := func(name string) core.ToolCategory {
		if cat, ok := catMap[name]; ok {
			switch cat {
			case "file_read":
				return core.ToolCategoryFileRead
			case "shell_command":
				return core.ToolCategoryShellCommand
			}
		}
		return core.ToolCategoryUnknown
	}
	return core.NewConversationOptimizer(core.ConversationOptimizerOptions{Enabled: true, KnownToolFn: fn})
}

func parseToolCalls(raw []interface{}) []core.ToolCall {
	calls := make([]core.ToolCall, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]interface{}); ok {
			call := core.ToolCall{ID: getStringVal(m, "id"), Type: getStringVal(m, "type")}
			if fn, ok := m["function"].(map[string]interface{}); ok {
				call.Function = core.ToolCallFunction{
					Name: getStringVal(fn, "name"), Arguments: getStringVal(fn, "arguments"),
				}
			}
			calls = append(calls, call)
		}
	}
	return calls
}

func parseMessagesFromRaw(raw interface{}) []core.Message {
	if arr, ok := raw.([]interface{}); ok {
		return parseMessages(arr)
	}
	return nil
}

func serializeMessages(msgs []core.Message) []map[string]interface{} {
	out := make([]map[string]interface{}, len(msgs))
	for i, m := range msgs {
		entry := map[string]interface{}{"role": m.Role, "content": m.Content}
		if m.ToolCallID != "" {
			entry["toolCallId"] = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			entry["toolCalls"] = serializeToolCalls(m.ToolCalls)
		}
		if m.Status != "" {
			entry["status"] = m.Status
		}
		out[i] = entry
	}
	return out
}

func serializeToolCalls(calls []core.ToolCall) []map[string]interface{} {
	out := make([]map[string]interface{}, len(calls))
	for i, c := range calls {
		out[i] = map[string]interface{}{
			"id":   c.ID,
			"type": c.Type,
			"function": map[string]interface{}{
				"name": c.Function.Name, "arguments": c.Function.Arguments,
			},
		}
	}
	return out
}

// providerShim is a minimal Provider for SetProvider testing.
type providerShim struct{ info core.ProviderInfo }

func (p *providerShim) Chat(_ context.Context, _ *core.ChatRequest) (*core.ChatResponse, error) {
	return nil, nil
}
func (p *providerShim) ChatStream(_ context.Context, _ *core.ChatRequest, _ core.StreamHandler) error {
	return nil
}
func (p *providerShim) Info() core.ProviderInfo                { return p.info }
func (p *providerShim) EstimateTokens(_ *core.ChatRequest) int { return 100 }

// getStringVal gets a string from a map.
func getStringVal(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getIntVal gets an int from a map (handles float64 from JSON).
func getIntVal(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
	}
	return 0
}

// getBoolVal gets a bool from a map.
func getBoolVal(m map[string]interface{}, key string) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}
