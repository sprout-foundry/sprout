package main

import (
	"context"
	"strings"
	"sync"

	"github.com/sprout-foundry/seed/core"
	"github.com/sprout-foundry/seed/internal/test"
)

// cliState holds all shared state for the CLI session.
type cliState struct {
	mu        sync.Mutex
	h         *test.Harness
	agent     *core.Agent
	agentOpts *core.Options // stored options for lazy agent creation
	ctx       context.Context
	metadata  map[string]string

	// blockCh is closed by mock.release to unblock a pending provider call.
	blockCh     chan struct{}
	interrupted bool // pending interrupt flag for lazy creation
}

// newCliState creates a fresh CLI state with mocks and context.
func newCliState() *cliState {
	return &cliState{
		h:   test.NewHarness(),
		ctx: context.Background(),
	}
}

// newAgent stores options for lazy agent creation. The agent is created
// lazily in ensureAgent() so that tools registered after agent.new but
// before the first run() are visible to the fallback parser.
func (s *cliState) newAgent(params map[string]interface{}) (map[string]interface{}, *rpcError) {
	opts := core.Options{
		Provider:       s.h.Provider(),
		Executor:       s.h.Executor(),
		UI:             core.NoopUI,
		EventPublisher: s.h.EventBus(),
	}

	// Wire OnCheckpoint to publish on_checkpoint events via the event bus.
	bus := s.h.EventBus()
	opts.OnCheckpoint = func(cp core.TurnCheckpoint) {
		bus.Publish("on_checkpoint", map[string]interface{}{
			"checkpoint": cp,
		})
	}

	if v, ok := params["systemPrompt"]; ok {
		opts.SystemPrompt = v.(string)
	}
	if v, ok := params["maxIterations"]; ok {
		switch v2 := v.(type) {
		case float64:
			opts.MaxIterations = int(v2)
		case int:
			opts.MaxIterations = v2
		}
	}
	if v, ok := params["maxTokens"]; ok {
		switch v2 := v.(type) {
		case float64:
			opts.MaxTokens = int(v2)
		case int:
			opts.MaxTokens = v2
		}
	}
	if _, ok := params["debug"]; ok {
		// Accept the parameter but ignore it. Debug output (fmt.Printf from
		// the response validator) would corrupt the JSON-RPC protocol on stdout.
		// The CLI is headless — debug output is not applicable.
	}
	if v, ok := params["compactionTriggerFraction"]; ok {
		switch v2 := v.(type) {
		case float64:
			opts.CompactionTriggerFraction = v2
		}
	}
	if v, ok := params["disableFallbackParser"]; ok {
		opts.DisableFallbackParser = v.(bool)
	}
	if v, ok := params["disableValidator"]; ok {
		opts.DisableValidator = v.(bool)
	}
	if v, ok := params["disableNormalizer"]; ok {
		opts.DisableNormalizer = v.(bool)
	}

	// SP-016-1k: Additional Options fields
	if v, ok := params["initialMessages"]; ok {
		if arr, ok := v.([]interface{}); ok {
			opts.InitialMessages = parseMessages(arr)
		}
	}
	if v, ok := params["initialCheckpoints"]; ok {
		if arr, ok := v.([]interface{}); ok {
			opts.InitialCheckpoints = parseCheckpoints(arr)
		}
	}
	if v, ok := params["retryConfig"]; ok {
		if m, ok := v.(map[string]interface{}); ok {
			opts.RetryConfig = parseRetryConfig(m)
		}
	}
	if v, ok := params["optimizer"]; ok {
		if m, ok := v.(map[string]interface{}); ok {
			opts.Optimizer = parseOptimizer(m)
		}
	}

	// Store options for lazy creation — agent is created in ensureAgent().
	s.mu.Lock()
	s.agentOpts = &opts
	s.agent = nil
	s.mu.Unlock()

	return map[string]interface{}{"ok": true}, nil
}

// ensureAgent creates the agent lazily from stored options, or returns the
// existing agent. This ensures tools registered after agent.new but before
// the first run() are visible to the fallback parser and other components.
func (s *cliState) ensureAgent() (*core.Agent, *rpcError) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.agent != nil {
		return s.agent, nil
	}
	if s.agentOpts == nil {
		return nil, &rpcError{Code: -32603, Message: "agent not created; call agent.new first"}
	}

	agent, err := core.NewAgent(*s.agentOpts)
	if err != nil {
		return nil, &rpcError{Code: -32603, Message: err.Error()}
	}
	s.agent = agent

	// Apply pending interrupt if one was queued before agent creation.
	if s.interrupted {
		agent.Interrupt()
	}

	return agent, nil
}

// run calls agent.Run with the given query.
func (s *cliState) run(params map[string]interface{}) (map[string]interface{}, *rpcError) {
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

	// Use the CLI context for this run.
	s.mu.Lock()
	ctx := s.ctx
	s.mu.Unlock()

	result, err := agent.Run(ctx, query)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "context canceled") {
			return nil, &rpcError{Code: -32603, Message: "query cancelled"}
		}
		return nil, &rpcError{Code: -32603, Message: msg}
	}
	return map[string]interface{}{"result": result}, nil
}

// interrupt calls agent.Interrupt() or sets a pending interrupt flag if the
// agent hasn't been created yet (lazy creation).
func (s *cliState) interrupt() (map[string]interface{}, *rpcError) {
	s.mu.Lock()
	s.interrupted = true
	agent := s.agent
	s.mu.Unlock()

	if agent != nil {
		agent.Interrupt()
	}
	return map[string]interface{}{"ok": true}, nil
}

// resetInterrupt clears the interrupt state on the agent and pending flag.
func (s *cliState) resetInterrupt() (map[string]interface{}, *rpcError) {
	s.mu.Lock()
	s.interrupted = false
	agent := s.agent
	s.mu.Unlock()

	if agent != nil {
		ctx := agent.ResetInterrupt()
		return map[string]interface{}{"ok": true, "context": ctx != nil}, nil
	}
	// Agent not yet created — just clear the flag.
	return map[string]interface{}{"ok": true, "context": true}, nil
}
