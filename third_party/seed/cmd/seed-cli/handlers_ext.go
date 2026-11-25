package main

import (
	"github.com/sprout-foundry/seed/core"
)

// agentOrErr returns the agent or an error response if nil.
// Uses ensureAgent() for lazy creation so that tools registered after
// agent.new but before the first access are visible to the agent.
func (s *cliState) agentOrErr() (*core.Agent, *rpcError) {
	return s.ensureAgent()
}

// ---- State management handlers (SP-016-1c) ----

func (s *cliState) stateMessages() (map[string]interface{}, *rpcError) {
	a, err := s.agentOrErr()
	if err != nil {
		return nil, err
	}
	msgs := a.State().Messages()
	out := serializeMessages(msgs)
	return map[string]interface{}{
		"messages": out,
		"len":      a.State().Len(),
	}, nil
}

func (s *cliState) stateSessionID() (map[string]interface{}, *rpcError) {
	a, err := s.agentOrErr()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"sessionId": a.State().SessionID()}, nil
}

func (s *cliState) stateSetSessionID(params map[string]interface{}) (map[string]interface{}, *rpcError) {
	a, err := s.agentOrErr()
	if err != nil {
		return nil, err
	}
	a.State().SetSessionID(getStringVal(params, "sessionId"))
	return map[string]interface{}{"ok": true}, nil
}

func (s *cliState) stateEnsureSessionID() (map[string]interface{}, *rpcError) {
	a, err := s.agentOrErr()
	if err != nil {
		return nil, err
	}
	a.State().EnsureSessionID()
	return map[string]interface{}{"ok": true, "sessionId": a.State().SessionID()}, nil
}

func (s *cliState) stateTokens() (map[string]interface{}, *rpcError) {
	a, err := s.agentOrErr()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"tokens": a.State().TotalTokens()}, nil
}

func (s *cliState) stateCost() (map[string]interface{}, *rpcError) {
	a, err := s.agentOrErr()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"cost": a.State().TotalCost()}, nil
}

func (s *cliState) stateAddMessage(params map[string]interface{}) (map[string]interface{}, *rpcError) {
	a, err := s.agentOrErr()
	if err != nil {
		return nil, err
	}
	a.State().AddMessage(core.Message{Role: getStringVal(params, "role"), Content: getStringVal(params, "content")})
	return map[string]interface{}{"ok": true}, nil
}

func (s *cliState) stateLen() (map[string]interface{}, *rpcError) {
	a, err := s.agentOrErr()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"len": a.State().Len()}, nil
}

func (s *cliState) stateClearCheckpoints() (map[string]interface{}, *rpcError) {
	a, err := s.agentOrErr()
	if err != nil {
		return nil, err
	}
	a.State().ClearCheckpoints()
	return map[string]interface{}{"ok": true}, nil
}

// ---- Configuration handlers (SP-016-1d) ----

func (s *cliState) setProvider(params map[string]interface{}) (map[string]interface{}, *rpcError) {
	a, err := s.agentOrErr()
	if err != nil {
		return nil, err
	}
	info := core.ProviderInfo{
		Model: getStringVal(params, "model"), ContextSize: getIntVal(params, "contextSize"),
		HasVision: getBoolVal(params, "hasVision"),
	}
	if info.Model == "" {
		info.Model = "swapped-model"
	}
	if info.ContextSize == 0 {
		info.ContextSize = 128000
	}
	a.SetProvider(&providerShim{info: info})
	return map[string]interface{}{"ok": true}, nil
}

func (s *cliState) setFlushCallback() (map[string]interface{}, *rpcError) {
	a, err := s.agentOrErr()
	if err != nil {
		return nil, err
	}
	bus := s.h.EventBus()
	a.SetFlushCallback(func() {
		bus.Publish("flush", map[string]interface{}{"flushed": true})
	})
	return map[string]interface{}{"ok": true}, nil
}

// ---- Steering & Injection (SP-016-1e) ----

func (s *cliState) injectInput(params map[string]interface{}) (map[string]interface{}, *rpcError) {
	a, err := s.agentOrErr()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"accepted": a.InjectInput(getStringVal(params, "input"))}, nil
}

// ---- Checkpoints, Provider, Streaming (SP-016-1f) ----

func (s *cliState) providerInfo() (map[string]interface{}, *rpcError) {
	a, err := s.agentOrErr()
	if err != nil {
		return nil, err
	}
	info := a.Provider().Info()
	return map[string]interface{}{
		"model": info.Model, "contextSize": info.ContextSize, "hasVision": info.HasVision,
	}, nil
}

func (s *cliState) estimateTokens(params map[string]interface{}) (map[string]interface{}, *rpcError) {
	a, err := s.agentOrErr()
	if err != nil {
		return nil, err
	}
	msgs := parseMessagesFromRaw(params["messages"])
	return map[string]interface{}{"count": a.Provider().EstimateTokens(&core.ChatRequest{Messages: msgs})}, nil
}

func (s *cliState) streamingBuffer() (map[string]interface{}, *rpcError) {
	a, err := s.agentOrErr()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"content": a.StreamingBuffer().String()}, nil
}

func (s *cliState) reasoningBuffer() (map[string]interface{}, *rpcError) {
	a, err := s.agentOrErr()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"content": a.ReasoningBuffer().String()}, nil
}

// ---- Output Manager stubs (SP-016-1g) ----
// OutputManager is not directly accessible from Agent's public API.
// These methods store/retrieve metadata in cliState and test indirectly.

func (s *cliState) outputSetMetadata(params map[string]interface{}) (map[string]interface{}, *rpcError) {
	s.mu.Lock()
	if s.metadata == nil {
		s.metadata = make(map[string]string)
	}
	s.metadata[getStringVal(params, "key")] = getStringVal(params, "value")
	s.mu.Unlock()
	return map[string]interface{}{"ok": true}, nil
}

func (s *cliState) outputGetMetadata(params map[string]interface{}) (map[string]interface{}, *rpcError) {
	s.mu.Lock()
	v := s.metadata[getStringVal(params, "key")]
	s.mu.Unlock()
	return map[string]interface{}{"value": v}, nil
}

func (s *cliState) outputFlush() (map[string]interface{}, *rpcError) {
	return map[string]interface{}{"ok": true}, nil
}

func (s *cliState) outputReset() (map[string]interface{}, *rpcError) {
	return map[string]interface{}{"ok": true}, nil
}

// ---- Mock additions (SP-016-1h) ----

func (s *cliState) blockUntilHandler() (map[string]interface{}, *rpcError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blockCh = s.h.Provider().BlockUntil()
	return map[string]interface{}{"blockId": "1"}, nil
}

func (s *cliState) releaseHandler(_ map[string]interface{}) (map[string]interface{}, *rpcError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blockCh != nil {
		close(s.blockCh)
		s.blockCh = nil
	}
	return map[string]interface{}{"ok": true}, nil
}
