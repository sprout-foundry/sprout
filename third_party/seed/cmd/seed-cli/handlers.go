package main

import (
	"context"

	"github.com/sprout-foundry/seed/core"
)

// ---- Agent methods not in state.go ----

// runStream calls agent.RunStream with the given query.
func (s *cliState) runStream(params map[string]interface{}) (map[string]interface{}, *rpcError) {
	query, ok := params["query"].(string)
	if !ok {
		return nil, &rpcError{Code: -32602, Message: "missing required param: query"}
	}

	agent, rpcErr := s.ensureAgent()
	if rpcErr != nil {
		return nil, rpcErr
	}

	// If the agent was pre-interrupted before creation, return error immediately.
	s.mu.Lock()
	isInterrupted := s.interrupted
	s.mu.Unlock()
	if isInterrupted {
		return nil, &rpcError{Code: -32603, Message: "agent interrupted"}
	}

	// Context can be injected via _runCtx param (by the CLI for async dispatch)
	// or fall back to the default CLI context.
	ctx, ok := params["_runCtx"].(context.Context)
	if !ok || ctx == nil {
		s.mu.Lock()
		ctx = s.ctx
		s.mu.Unlock()
	}

	result, err := agent.RunStream(ctx, query)
	if err != nil {
		msg := err.Error()
		if msg == "context canceled" || msg == "context deadline exceeded" {
			return nil, &rpcError{Code: -32603, Message: "query cancelled"}
		}
		return nil, &rpcError{Code: -32603, Message: msg}
	}
	return map[string]interface{}{"result": result}, nil
}

// exportState serializes the current agent state to JSON.
func (s *cliState) exportState() (map[string]interface{}, *rpcError) {
	a, err := s.agentOrErr()
	if err != nil {
		return nil, err
	}

	data, err2 := a.ExportState()
	if err2 != nil {
		return nil, &rpcError{Code: -32603, Message: err2.Error()}
	}
	return map[string]interface{}{"state": string(data)}, nil
}

// importState deserializes state from JSON into the agent.
func (s *cliState) importState(params map[string]interface{}) (map[string]interface{}, *rpcError) {
	raw, ok := params["state"].(string)
	if !ok {
		return nil, &rpcError{Code: -32602, Message: "missing required param: state"}
	}

	a, err := s.agentOrErr()
	if err != nil {
		return nil, err
	}

	if err := a.ImportState([]byte(raw)); err != nil {
		return nil, &rpcError{Code: -32603, Message: err.Error()}
	}
	return map[string]interface{}{"ok": true}, nil
}

// setSystemPrompt updates the system prompt for future queries.
func (s *cliState) setSystemPrompt(params map[string]interface{}) (map[string]interface{}, *rpcError) {
	prompt, ok := params["prompt"].(string)
	if !ok {
		return nil, &rpcError{Code: -32602, Message: "missing required param: prompt"}
	}

	a, err := s.agentOrErr()
	if err != nil {
		return nil, err
	}

	a.SetSystemPrompt(prompt)
	return map[string]interface{}{"ok": true}, nil
}

// steer queues a transient steering message.
func (s *cliState) steer(params map[string]interface{}) (map[string]interface{}, *rpcError) {
	role, _ := params["role"].(string)
	content, _ := params["content"].(string)

	if role == "" {
		role = "system"
	}

	a, err := s.agentOrErr()
	if err != nil {
		return nil, err
	}

	a.Steer(core.Message{Role: role, Content: content})
	return map[string]interface{}{"ok": true}, nil
}

// steerSystem is a convenience for injecting a system-level steering message.
func (s *cliState) steerSystem(params map[string]interface{}) (map[string]interface{}, *rpcError) {
	content, ok := params["content"].(string)
	if !ok {
		return nil, &rpcError{Code: -32602, Message: "missing required param: content"}
	}

	a, err := s.agentOrErr()
	if err != nil {
		return nil, err
	}

	a.SteerSystem(content)
	return map[string]interface{}{"ok": true}, nil
}

// pause pauses the agent for user clarification.
func (s *cliState) pause() (map[string]interface{}, *rpcError) {
	a, err := s.agentOrErr()
	if err != nil {
		return nil, err
	}

	a.Pause()
	return map[string]interface{}{"ok": true}, nil
}

// resume resumes a paused agent.
func (s *cliState) resume() (map[string]interface{}, *rpcError) {
	a, err := s.agentOrErr()
	if err != nil {
		return nil, err
	}

	a.Resume()
	return map[string]interface{}{"ok": true}, nil
}

// isPaused returns whether the agent is paused.
func (s *cliState) isPaused() (map[string]interface{}, *rpcError) {
	a, err := s.agentOrErr()
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{"paused": a.IsPaused()}, nil
}

// checkpoints returns a copy of all recorded turn checkpoints.
func (s *cliState) checkpoints() (map[string]interface{}, *rpcError) {
	a, err := s.agentOrErr()
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{"checkpoints": a.Checkpoints()}, nil
}

// agentState returns the message count and session ID from the agent's state.
func (s *cliState) agentState() (map[string]interface{}, *rpcError) {
	a, err := s.agentOrErr()
	if err != nil {
		return nil, err
	}

	st := a.State()
	return map[string]interface{}{
		"messageCount": st.Len(),
		"sessionID":    st.SessionID(),
		"totalTokens":  st.TotalTokens(),
		"totalCost":    st.TotalCost(),
	}, nil
}

// ---- Mock provider methods not in state.go ----

// addMalformedResponse queues a response with tool calls embedded in content.
func (s *cliState) addMalformedResponse(params map[string]interface{}) (map[string]interface{}, *rpcError) {
	content, ok := params["content"].(string)
	if !ok {
		return nil, &rpcError{Code: -32602, Message: "missing required param: content"}
	}

	s.h.Provider().AddMalformedResponse(content)
	return map[string]interface{}{"ok": true}, nil
}

// addTextResponseWithFinish queues a text response with a custom finish reason.
func (s *cliState) addTextResponseWithFinish(params map[string]interface{}) (map[string]interface{}, *rpcError) {
	content, ok := params["content"].(string)
	if !ok {
		return nil, &rpcError{Code: -32602, Message: "missing required param: content"}
	}

	finishReason, _ := params["finishReason"].(string)
	if finishReason == "" {
		finishReason = "stop"
	}

	s.h.Provider().AddTextResponseWithFinish(content, finishReason)
	return map[string]interface{}{"ok": true}, nil
}

// withInfo sets the provider info.
func (s *cliState) withInfo(params map[string]interface{}) (map[string]interface{}, *rpcError) {
	info := core.ProviderInfo{
		Model:       getStringVal(params, "model"),
		ContextSize: getIntVal(params, "contextSize"),
		HasVision:   getBoolVal(params, "hasVision"),
	}
	if info.Model == "" {
		info.Model = "mock-model"
	}
	if info.ContextSize == 0 {
		info.ContextSize = 128000
	}

	s.h.Provider().WithInfo(info)
	return map[string]interface{}{"ok": true}, nil
}

// withTokenEstimate sets the token estimate.
// Accepts "count" or "estimate" as param name.
func (s *cliState) withTokenEstimate(params map[string]interface{}) (map[string]interface{}, *rpcError) {
	var count float64
	var ok bool
	if count, ok = params["count"].(float64); !ok {
		if count, ok = params["estimate"].(float64); !ok {
			return nil, &rpcError{Code: -32602, Message: "missing required param: count or estimate"}
		}
	}

	s.h.Provider().WithTokenEstimate(int(count))
	return map[string]interface{}{"ok": true}, nil
}

// withStreaming enables streaming mode on the mock provider.
func (s *cliState) withStreaming() (map[string]interface{}, *rpcError) {
	s.h.Provider().WithStreaming()
	return map[string]interface{}{"ok": true}, nil
}

// addStreamChunks configures explicit chunk sequences for streaming.
// Accepts two formats:
//   - ["chunk1", "chunk2"] (simple strings)
//   - [{"content": "Hel"}, {"reasoning": "think"}, {"content": "lo"}] (objects with content/reasoning)
func (s *cliState) addStreamChunks(params map[string]interface{}) (map[string]interface{}, *rpcError) {
	raw, ok := params["chunks"].([]interface{})
	if !ok {
		return nil, &rpcError{Code: -32602, Message: "missing required param: chunks"}
	}

	var chunks []string
	var finalContent string
	for _, c := range raw {
		switch v := c.(type) {
		case string:
			chunks = append(chunks, v)
			finalContent += v
		case map[string]interface{}:
			// Object format: {content: "...", reasoning: "..."}
			if content, ok := v["content"].(string); ok {
				chunks = append(chunks, content)
				finalContent += content
			} else if reasoning, ok := v["reasoning"].(string); ok {
				chunks = append(chunks, reasoning)
				// reasoning doesn't add to final content for the response
			}
		}
	}

	// Enable streaming mode and add chunks
	p := s.h.Provider()
	p.WithStreaming()

	// Add a placeholder response so ChatStream doesn't fail with
	// "no more responses configured". The handler will replace
	// the content with the actual streamed content via OnDone.
	p.AddTextResponse(finalContent)

	p.AddStreamChunks(chunks...)
	return map[string]interface{}{"ok": true}, nil
}

// blockOnCallN sets the Nth call to block until unblock is called.
func (s *cliState) blockOnCallN(params map[string]interface{}) (map[string]interface{}, *rpcError) {
	n, ok := params["call"].(float64)
	if !ok {
		return nil, &rpcError{Code: -32602, Message: "missing required param: call"}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	ch := s.h.Provider().BlockOnCallN(int(n))
	s.blockCh = ch
	return map[string]interface{}{"ok": true}, nil
}

// unblock closes the block channel to unblock a pending provider call.
func (s *cliState) unblock() (map[string]interface{}, *rpcError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blockCh != nil {
		close(s.blockCh)
		s.blockCh = nil
	}
	return map[string]interface{}{"ok": true}, nil
}

// executorCallCount returns the number of executor Execute() calls.
func (s *cliState) executorCallCount() (map[string]interface{}, *rpcError) {
	return map[string]interface{}{"count": s.h.Executor().CallCount()}, nil
}

// executorLastCalls returns the tool calls from the last Execute() call.
func (s *cliState) executorLastCalls() (map[string]interface{}, *rpcError) {
	calls := s.h.Executor().LastCalls()
	if len(calls) == 0 {
		return map[string]interface{}{"calls": []map[string]interface{}{}}, nil
	}

	out := make([]map[string]interface{}, len(calls))
	for i, c := range calls {
		out[i] = map[string]interface{}{
			"id":   c.ID,
			"type": c.Type,
			"name": c.Function.Name,
			"args": c.Function.Arguments,
		}
	}
	return map[string]interface{}{"calls": out}, nil
}
