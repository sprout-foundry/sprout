package main

import (
	"encoding/json"
	"errors"

	"github.com/sprout-foundry/seed/core"
)

// addTextResponse queues a text response on the mock provider.
func (s *cliState) addTextResponse(params map[string]interface{}) (map[string]interface{}, *rpcError) {
	content, ok := params["content"].(string)
	if !ok {
		return nil, &rpcError{Code: -32602, Message: "missing required param: content"}
	}

	s.h.Provider().AddTextResponse(content)
	return map[string]interface{}{"ok": true}, nil
}

// addToolCallResponse queues a tool call response on the mock provider.
// Accepts two formats:
//   - "toolCalls": [{id, type, function: {name, arguments}}] (nested format)
//   - "calls": [{id, name, arguments: {...}}] (flat format, arguments is an object)
func (s *cliState) addToolCallResponse(params map[string]interface{}) (map[string]interface{}, *rpcError) {
	content, _ := params["content"].(string)

	// Try nested format first (toolCalls)
	var raw []interface{}
	if v, ok := params["toolCalls"]; ok {
		if arr, ok := v.([]interface{}); ok {
			raw = arr
		}
	} else if v, ok := params["calls"]; ok {
		// Flat format: {id, name, arguments: {...}}
		if arr, ok := v.([]interface{}); ok {
			raw = arr
		}
	}

	var toolCalls []core.ToolCall
	for _, item := range raw {
		obj, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		// Detect flat format: has "name" at top level
		if name, ok := obj["name"].(string); ok {
			// Flat format: {id, name, arguments: {...}}
			call := core.ToolCall{
				ID:   getStringVal(obj, "id"),
				Type: getStringVal(obj, "type"),
			}
			if call.Type == "" {
				call.Type = "function"
			}
			call.Function.Name = name
			// Arguments is an object, serialize it to JSON string
			if args, ok := obj["arguments"].(map[string]interface{}); ok {
				data, err := json.Marshal(args)
				if err == nil {
					call.Function.Arguments = string(data)
				}
			}
			toolCalls = append(toolCalls, call)
			continue
		}

		// Nested format: {id, type, function: {name, arguments}}
		call := core.ToolCall{
			ID:   getStringVal(obj, "id"),
			Type: getStringVal(obj, "type"),
		}
		if fn, ok := obj["function"].(map[string]interface{}); ok {
			call.Function = core.ToolCallFunction{
				Name:      getStringVal(fn, "name"),
				Arguments: getStringVal(fn, "arguments"),
			}
		}
		toolCalls = append(toolCalls, call)
	}

	s.h.Provider().AddToolCallResponse(content, toolCalls...)
	return map[string]interface{}{"ok": true}, nil
}

// addError queues an error on the mock provider.
// Accepts an optional "errorType" param to create typed errors:
//   - "transient" → TransientError
//   - "rate_limit" → RateLimitError
//   - "auth" → AuthError
//   - "context_overflow" → ContextOverflowError
//   - "client" → ClientError
//   - "content_filtered" → ContentFilteredError
//   - "blank" → BlankResponseError
//
// If no errorType is specified, creates a plain error.
func (s *cliState) addError(params map[string]interface{}) (map[string]interface{}, *rpcError) {
	msg, ok := params["message"].(string)
	if !ok {
		return nil, &rpcError{Code: -32602, Message: "missing required param: message"}
	}

	errorType, _ := params["errorType"].(string)

	var err error
	switch errorType {
	case "transient":
		err = &core.TransientError{
			Op:       "chat",
			Provider: "mock-model",
			Wrapped:  errors.New(msg),
		}
	case "rate_limit":
		err = &core.RateLimitError{
			Provider: "mock-model",
			Wrapped:  errors.New(msg),
		}
	case "auth":
		err = &core.AuthError{
			Provider: "mock-model",
			Wrapped:  errors.New(msg),
		}
	case "context_overflow":
		err = &core.ContextOverflowError{
			Wrapped: errors.New(msg),
		}
	case "client":
		err = &core.ClientError{
			Provider: "mock-model",
			Wrapped:  errors.New(msg),
		}
	case "content_filtered":
		err = &core.ContentFilteredError{
			Provider: "mock-model",
		}
	case "blank":
		err = &core.BlankResponseError{
			Provider: "mock-model",
			Count:    2,
		}
	default:
		err = errors.New(msg)
	}

	s.h.Provider().AddError(err)
	return map[string]interface{}{"ok": true}, nil
}

// addTool adds a tool to the mock executor.
func (s *cliState) addTool(params map[string]interface{}) (map[string]interface{}, *rpcError) {
	name, ok := params["name"].(string)
	if !ok {
		return nil, &rpcError{Code: -32602, Message: "missing required param: name"}
	}

	tool := core.Tool{
		Type: "function",
		Function: core.ToolFunction{
			Name:        name,
			Description: getStringVal(params, "description"),
		},
	}

	if p, ok := params["parameters"].(map[string]interface{}); ok {
		tool.Function.Parameters = p
	}

	s.h.Executor().AddTool(tool)
	return map[string]interface{}{"ok": true}, nil
}

// addToolResult queues a tool result on the mock executor.
func (s *cliState) addToolResult(params map[string]interface{}) (map[string]interface{}, *rpcError) {
	callID, ok := params["toolCallId"].(string)
	if !ok {
		return nil, &rpcError{Code: -32602, Message: "missing required param: toolCallId"}
	}

	content, _ := params["content"].(string)
	s.h.Executor().AddToolResult(callID, content)
	return map[string]interface{}{"ok": true}, nil
}

// reset clears all mock state, agent, and pending state.
func (s *cliState) reset() (map[string]interface{}, *rpcError) {
	s.h.Reset()
	s.mu.Lock()
	s.agent = nil
	s.agentOpts = nil
	s.interrupted = false
	s.mu.Unlock()
	return map[string]interface{}{"ok": true}, nil
}

// callCount returns the provider call count.
func (s *cliState) callCount() (map[string]interface{}, *rpcError) {
	return map[string]interface{}{"count": s.h.Provider().CallCount()}, nil
}

// lastRequest returns the last provider request.
func (s *cliState) lastRequest() (map[string]interface{}, *rpcError) {
	req := s.h.Provider().LastRequest()
	if req == nil {
		return map[string]interface{}{"request": nil}, nil
	}
	return mapToMap(req), nil
}
