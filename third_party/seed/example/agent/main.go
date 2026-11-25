// agent demonstrates a minimal seed-based agent that speaks JSON-RPC over
// stdin/stdout. It is intentionally simple — a real implementation would need
// security controls, rate limiting, input validation, and more.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/seed/core"
	"github.com/sprout-foundry/seed/events"
)

// ---------------------------------------------------------------------------
// Wire types — JSON-RPC over stdin/stdout
// ---------------------------------------------------------------------------

type rpcReq struct {
	ID     *int                   `json:"id"`
	Method string                 `json:"method"`
	Params map[string]interface{} `json:"params"`
}

type rpcResp struct {
	ID     *int                   `json:"id,omitempty"`
	Result map[string]interface{} `json:"result,omitempty"`
	Error  *rpcErr                `json:"error,omitempty"`
}

type rpcEv struct {
	Event string                 `json:"event"`
	Data  map[string]interface{} `json:"data"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ---------------------------------------------------------------------------
// Global state — guarded by a mutex
// ---------------------------------------------------------------------------

type state struct {
	mu     sync.Mutex
	bus    *events.EventBus
	agent  *core.Agent
	ctx    context.Context
	cancel context.CancelFunc
}

var errNoAgent = &rpcErr{Code: -32603, Message: "agent not created; call agent.new first"}

// ---------------------------------------------------------------------------
// main — read lines from stdin, dispatch to handlers, write responses
// ---------------------------------------------------------------------------

func main() {
	s := &state{}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	defer s.cancel()

	var outMu sync.Mutex
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		var req rpcReq
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			outMu.Lock()
			writeErr(os.Stdout, nil, &rpcErr{Code: -32700, Message: "parse: " + err.Error()})
			outMu.Unlock()
			continue
		}

		if req.Method == "" {
			outMu.Lock()
			writeErr(os.Stdout, req.ID, &rpcErr{Code: -32600, Message: "missing method"})
			outMu.Unlock()
			continue
		}

		if req.Params == nil {
			req.Params = map[string]interface{}{}
		}

		// Start the event bus subscriber on the first agent.* call.
		if !strings.HasPrefix(req.Method, "agent.") {
			res, e := dispatch(s, req.Method, req.Params)
			outMu.Lock()
			if e != nil {
				writeErr(os.Stdout, req.ID, e)
			} else {
				writeResp(os.Stdout, req.ID, res)
			}
			outMu.Unlock()
			continue
		}

		if s.bus == nil {
			s.bus = events.NewEventBus()
			ch := s.bus.Subscribe("__agent__")
			go func() {
				for ev := range ch {
					b, _ := json.Marshal(rpcEv{Event: ev.Type, Data: toMap(ev.Data)})
					outMu.Lock()
					fmt.Fprintln(os.Stdout, string(b))
					outMu.Unlock()
				}
			}()
		}

		res, e := dispatch(s, req.Method, req.Params)
		outMu.Lock()
		if e != nil {
			writeErr(os.Stdout, req.ID, e)
		} else {
			writeResp(os.Stdout, req.ID, res)
		}
		outMu.Unlock()
	}
}

// ---------------------------------------------------------------------------
// Dispatch
// ---------------------------------------------------------------------------

func dispatch(s *state, method string, params map[string]interface{}) (map[string]interface{}, *rpcErr) {
	switch method {
	case "agent.new":
		return handleNew(s, params)
	case "agent.run":
		return handleRun(s, params)
	case "agent.interrupt":
		return handleInterrupt(s)
	case "agent.state":
		return handleState(s)
	case "agent.setSystemPrompt":
		return handleSetPrompt(s, params)
	default:
		return nil, &rpcErr{Code: -32601, Message: "unknown method: " + method}
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func handleNew(s *state, params map[string]interface{}) (map[string]interface{}, *rpcErr) {
	// In a real implementation, the provider config would be validated for
	// safety: check endpoint scheme (no file:// or gopher://), enforce a
	// whitelist of allowed API key environment variable names, cap context
	// sizes, etc.
	prov, err := newProvider(params)
	if err != nil {
		return nil, &rpcErr{Code: -32602, Message: err.Error()}
	}

	// In a real implementation, workingDir would be resolved and validated
	// to prevent path traversal attacks (e.g. "../../etc/passwd").
	wd := sVal(params, "workingDir")
	if wd == "" {
		wd = "."
	}

	exec := newToolExec(wd)

	opts := core.Options{
		Provider:       prov,
		Executor:       exec,
		UI:             core.NoopUI,
		EventPublisher: s.bus,
		SystemPrompt:   sVal(params, "systemPrompt"),
		MaxIterations:  iVal(params, "maxIterations"),
		MaxTokens:      iVal(params, "maxTokens"),
		// REAL IMPLEMENTATION: Set a RetryConfig with bounded retries.
		RetryConfig: core.RetryConfig{
			MaxAttempts:  3,
			InitialDelay: 2 * time.Second,
			MaxDelay:     30 * time.Second,
			Multiplier:   2.0,
		},
	}

	ag, err := core.NewAgent(opts)
	if err != nil {
		return nil, &rpcErr{Code: -32603, Message: err.Error()}
	}

	s.mu.Lock()
	s.agent = ag
	s.mu.Unlock()

	return map[string]interface{}{"ok": true}, nil
}

func handleRun(s *state, params map[string]interface{}) (map[string]interface{}, *rpcErr) {
	q, ok := params["query"].(string)
	if !ok {
		return nil, &rpcErr{Code: -32602, Message: "missing query"}
	}

	s.mu.Lock()
	ag := s.agent
	s.mu.Unlock()
	if ag == nil {
		return nil, errNoAgent
	}

	// REAL IMPLEMENTATION: Sanitize the query — reject payloads that attempt
	// prompt injection, cap length, log for auditing.
	r, err := ag.Run(s.ctx, q)
	if err != nil {
		m := err.Error()
		if strings.Contains(m, "context canceled") {
			return nil, &rpcErr{Code: -32603, Message: "cancelled"}
		}
		return nil, &rpcErr{Code: -32603, Message: m}
	}

	st := ag.State()
	return map[string]interface{}{
		"result": r,
		"tokens": st.TotalTokens(),
		"cost":   st.TotalCost(),
	}, nil
}

func handleInterrupt(s *state) (map[string]interface{}, *rpcErr) {
	s.mu.Lock()
	ag := s.agent
	s.mu.Unlock()
	if ag == nil {
		return nil, errNoAgent
	}

	ag.Interrupt()
	return map[string]interface{}{"ok": true}, nil
}

func handleState(s *state) (map[string]interface{}, *rpcErr) {
	s.mu.Lock()
	ag := s.agent
	s.mu.Unlock()
	if ag == nil {
		return nil, errNoAgent
	}

	st := ag.State()
	return map[string]interface{}{
		"messageCount": st.Len(),
		"sessionID":    st.SessionID(),
		"totalTokens":  st.TotalTokens(),
		"totalCost":    st.TotalCost(),
	}, nil
}

func handleSetPrompt(s *state, params map[string]interface{}) (map[string]interface{}, *rpcErr) {
	s.mu.Lock()
	ag := s.agent
	s.mu.Unlock()
	if ag == nil {
		return nil, errNoAgent
	}

	// REAL IMPLEMENTATION: Validate the system prompt — cap length, reject
	// dangerous patterns, log changes for audit.
	ag.SetSystemPrompt(sVal(params, "prompt"))
	return map[string]interface{}{"ok": true}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeResp(w *os.File, id *int, result map[string]interface{}) {
	b, _ := json.Marshal(rpcResp{ID: id, Result: result})
	fmt.Fprintln(w, string(b))
}

func writeErr(w *os.File, id *int, e *rpcErr) {
	b, _ := json.Marshal(rpcResp{ID: id, Error: e})
	fmt.Fprintln(w, string(b))
}

func toMap(d any) map[string]interface{} {
	if m, ok := d.(map[string]interface{}); ok {
		return m
	}
	if m, ok := d.(map[string]any); ok {
		out := make(map[string]interface{}, len(m))
		for k, v := range m {
			out[k] = v
		}
		return out
	}
	return map[string]interface{}{"value": d}
}

func sVal(m map[string]interface{}, k string) string {
	if v, ok := m[k]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func iVal(m map[string]interface{}, k string) int {
	if v, ok := m[k]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
	}
	return 0
}
