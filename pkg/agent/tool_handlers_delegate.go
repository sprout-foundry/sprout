package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"
)

// asyncDelegateIDCounter provides a monotonically increasing counter to
// guarantee unique delegate IDs even when multiple delegates are started
// within the same nanosecond (fixes SHOULD_FIX #4 — delegate ID collision).
var asyncDelegateIDCounter int64

// handleDelegate is the tool handler for the delegate tool.
// It creates a child delegate agent, runs the query, and returns results.
// When cfg.Async is true, the delegate runs in the background and the
// handler returns immediately with a delegate_id for later status checks.
func handleDelegate(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// 1. Parse DelegateConfig from args
	cfg, err := parseDelegateConfig(args)
	if err != nil {
		return "", fmt.Errorf("invalid delegate config: %w", err)
	}

	// 2. Validate required fields
	if cfg.Prompt == "" {
		return "", fmt.Errorf("delegate prompt is required")
	}

	// 3. Validate agent is not nil
	if a == nil {
		return "", fmt.Errorf("agent is required")
	}

	// 4. Initialize async tracker if needed
	a.initSubManagers()

	// 4. If async mode, start in background and return immediately
	if cfg.Async {
		// 4a. Create the delegate agent
		delegate, err := CreateDelegateAgent(a, cfg)
		if err != nil {
			return "", fmt.Errorf("failed to create delegate: %w", err)
		}

		delegateID := fmt.Sprintf("delegate-%d-%d", time.Now().UnixNano(), atomic.AddInt64(&asyncDelegateIDCounter, 1))

		// 4b. Start the async delegate
		if err := a.asyncDelegateTracker.Start(delegateID, cfg, a, func(ctx context.Context) (*DelegateResult, error) {
			defer delegate.interruptCancel()

			// Set up stream bridge for the async delegate
			bridge := NewDelegateStreamBridge(a, delegateID)
			bridge.Start()
			defer bridge.Stop()

			// Publish delegate started event
			bridge.PublishActivity("started", truncateSummary(cfg.Prompt, 200), a.delegateDepth+1)

			// Run the delegate's query
			result, err := runDelegateQuery(ctx, delegate, cfg.Prompt, bridge, &cfg)

			if err != nil {
				delegateResult := bridge.GetResult("", "error", err.Error())
				bridge.PublishActivity("error", err.Error(), a.delegateDepth+1)
				return delegateResult, err
			}

			delegateResult := bridge.GetResult(truncateSummary(result, 500), "success", "")
			bridge.PublishActivity("completed", truncateSummary(result, 200), a.delegateDepth+1)
			return delegateResult, nil
		}); err != nil {
			return "", fmt.Errorf("failed to start async delegate: %w", err)
		}

		// 4c. Return immediately with the delegate ID
		resultJSON, err := json.Marshal(map[string]interface{}{
			"status":  "running",
			"delegate_id": delegateID,
			"message": "Delegate is running asynchronously. Use the delegate_status tool with the delegate_id to check on progress.",
		})
		if err != nil {
			return fmt.Sprintf("Delegate started with ID: %s", delegateID), nil
		}
		return string(resultJSON), nil
	}

	// 5. Synchronous path (existing behavior - DO NOT MODIFY)
	// 5a. Create the delegate agent
	delegate, err := CreateDelegateAgent(a, cfg)
	if err != nil {
		return "", fmt.Errorf("failed to create delegate: %w", err)
	}
	defer delegate.interruptCancel()

	// 5b. Set up stream bridge
	delegateID := fmt.Sprintf("delegate-%d", time.Now().UnixNano())
	bridge := NewDelegateStreamBridge(a, delegateID)
	bridge.Start()
	defer bridge.Stop()

	// 5c. Publish delegate started event
	bridge.PublishActivity("started", truncateSummary(cfg.Prompt, 200), a.delegateDepth+1)

	// 5d. Run the delegate's query
	result, err := runDelegateQuery(ctx, delegate, cfg.Prompt, bridge, &cfg)

	// 5e. Build and return the result
	var delegateResult *DelegateResult
	if err != nil {
		delegateResult = bridge.GetResult("", "error", err.Error())
		bridge.PublishActivity("error", err.Error(), a.delegateDepth+1)
	} else {
		delegateResult = bridge.GetResult(truncateSummary(result, 500), "success", "")
		bridge.PublishActivity("completed", truncateSummary(result, 200), a.delegateDepth+1)
	}

	// 5f. Format result as JSON
	resultJSON, err := json.Marshal(delegateResult)
	if err != nil {
		return fmt.Sprintf("Delegate completed with output: %s", result), nil
	}
	return string(resultJSON), nil
}

// parseDelegateConfig parses DelegateConfig from tool call arguments
func parseDelegateConfig(args map[string]interface{}) (DelegateConfig, error) {
	cfg := DelegateConfig{}

	if v, ok := args["prompt"].(string); ok {
		cfg.Prompt = v
	}
	if v, ok := args["role"].(string); ok {
		cfg.Role = v
	}
	if v, ok := args["provider"].(string); ok {
		cfg.Provider = v
	}
	if v, ok := args["model"].(string); ok {
		cfg.Model = v
	}
	if v, ok := args["context"].(string); ok {
		cfg.Context = v
	}
	if v, ok := args["max_iterations"]; ok {
		switch val := v.(type) {
		case float64:
			cfg.MaxIterations = int(val)
		case int:
			cfg.MaxIterations = val
		}
	}
	if v, ok := args["tools"].([]interface{}); ok {
		for _, t := range v {
			if s, ok := t.(string); ok {
				cfg.Tools = append(cfg.Tools, s)
			}
		}
	}
	if v, ok := args["files"].([]interface{}); ok {
		for _, f := range v {
			if s, ok := f.(string); ok {
				cfg.Files = append(cfg.Files, s)
			}
		}
	}
	if v, ok := args["follow_up"].([]interface{}); ok {
		for _, m := range v {
			if s, ok := m.(string); ok {
				cfg.FollowUpMessages = append(cfg.FollowUpMessages, s)
			}
		}
	}
	if v, ok := args["async"].(bool); ok {
		cfg.Async = v
	}
	// Also support numeric true/false from JSON parsing (some LLM clients send 1/0)
	if v, ok := args["async"].(float64); ok && v == 1 {
		cfg.Async = true
	}

	return cfg, nil
}

const followUpInjectionDelay = 500 * time.Millisecond

// followUpCommand carries a single follow-up message from the scheduling
// goroutine to the injection goroutine so that all direct method calls on
// delegate and bridge happen in one goroutine (no shared-object access from
// the scheduler goroutine).
type followUpCommand struct {
	message string
}

// runDelegateQuery runs the delegate agent's query and collects results.
func runDelegateQuery(ctx context.Context, delegate *Agent, prompt string, bridge *DelegateStreamBridge, cfg *DelegateConfig) (string, error) {
	// Create a feedback channel for follow-up injection commands.
	feedbackChan := make(chan followUpCommand, len(cfg.FollowUpMessages))

	// Track when the scheduler goroutine exits so we don't close feedbackChan
	// prematurely and cause a "send on closed channel" panic.
	schedulerDone := make(chan struct{})

	// schedulerStop signals the scheduler to stop when ProcessQuery returns.
	// Without this, if ProcessQuery returns early (before all follow-up
	// messages are scheduled), the scheduler can deadlock: it tries to send
	// on feedbackChan which is full (injector hasn't consumed yet), the
	// injector is blocked in range waiting for more or close, and nobody
	// can proceed. Closing schedulerStop breaks this cycle.
	schedulerStop := make(chan struct{})

	if len(cfg.FollowUpMessages) > 0 {
		go func() {
			defer close(schedulerDone)
			for i, msg := range cfg.FollowUpMessages {
				select {
				case <-ctx.Done():
					return
				case <-schedulerStop:
					return
				default:
				}
				if i > 0 {
					select {
					case <-ctx.Done():
						return
					case <-schedulerStop:
						return
					case <-time.After(followUpInjectionDelay):
					}
				}
				select {
				case feedbackChan <- followUpCommand{message: msg}:
				case <-ctx.Done():
					return
				case <-schedulerStop:
					return
				}
			}
		}()
	} else {
		close(schedulerDone)
	}

	// Process follow-up commands from the channel and inject them into the
	// delegate. This keeps all direct method calls on delegate and bridge in
	// a single goroutine instead of the scheduler goroutine above.
	followUpDone := make(chan struct{})
	go func() {
		defer close(followUpDone)
		for cmd := range feedbackChan {
			if err := delegate.InjectInputContext(cmd.message); err != nil {
				delegate.PrintLineAsync(fmt.Sprintf("[warn] Failed to inject follow-up message: %v", err))
				continue
			}
			bridge.RecordFollowUpInjection(cmd.message)
		}
	}()

	// Use the delegate agent's ProcessQuery method to run the prompt.
	// ProcessQuery handles the full agent loop (tool calls, iterations, etc.)
	response, err := delegate.ProcessQuery(prompt)

	// Wait for the scheduler to finish sending all follow-up messages.
	// In the normal case, the scheduler exits on its own after scheduling
	// all messages. If a deadlock occurs (e.g., the injector stalls and
	// the channel fills), schedulerStop provides a safety valve to break
	// the cycle after a timeout. With the channel sized to
	// len(FollowUpMessages), the scheduler can always send without
	// blocking, so the timeout should never fire in practice.
	if len(cfg.FollowUpMessages) > 0 {
		// Max time for the scheduler to finish: (N-1) delays of
		// followUpInjectionDelay plus a 1s safety margin.
		maxWait := time.Duration(len(cfg.FollowUpMessages)-1) * followUpInjectionDelay + time.Second
		select {
		case <-schedulerDone:
			// Scheduler exited normally.
		case <-time.After(maxWait):
			// Safety valve: force the scheduler to stop to prevent deadlock.
			close(schedulerStop)
			<-schedulerDone
		}
	}
	// If no follow-up messages, schedulerDone is already closed above.
	close(feedbackChan)
	<-followUpDone

	if err != nil {
		return "", err
	}

	return response, nil
}

// truncateSummary truncates a string to maxLen characters
func truncateSummary(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
