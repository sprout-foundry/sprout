package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// handleDelegate is the tool handler for the delegate tool.
// It creates a child delegate agent, runs the query, and returns results.
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

	// 3. Create the delegate agent
	delegate, err := CreateDelegateAgent(a, cfg)
	if err != nil {
		return "", fmt.Errorf("failed to create delegate: %w", err)
	}
	defer delegate.interruptCancel()

	// 4. Set up stream bridge
	delegateID := fmt.Sprintf("delegate-%d", time.Now().UnixNano())
	bridge := NewDelegateStreamBridge(a, delegateID)
	bridge.Start()
	defer bridge.Stop()

	// 5. Publish delegate started event
	bridge.PublishActivity("started", truncateSummary(cfg.Prompt, 200), a.delegateDepth+1)

	// 6. Run the delegate's query
	result, err := runDelegateQuery(ctx, delegate, cfg.Prompt, bridge, &cfg)

	// 7. Build and return the result
	var delegateResult *DelegateResult
	if err != nil {
		delegateResult = bridge.GetResult("", "error", err.Error())
		bridge.PublishActivity("error", err.Error(), a.delegateDepth+1)
	} else {
		delegateResult = bridge.GetResult(truncateSummary(result, 500), "success", "")
		bridge.PublishActivity("completed", truncateSummary(result, 200), a.delegateDepth+1)
	}

	// 8. Format result as JSON
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

	return cfg, nil
}

const followUpInjectionDelay = 500 * time.Millisecond

// runDelegateQuery runs the delegate agent's query and collects results
func runDelegateQuery(ctx context.Context, delegate *Agent, prompt string, bridge *DelegateStreamBridge, cfg *DelegateConfig) (string, error) {
	// Start a goroutine to inject follow-up messages while the delegate is processing.
	// This must be started before ProcessQuery so messages are injected during execution.
	if len(cfg.FollowUpMessages) > 0 {
		go func() {
			for i, msg := range cfg.FollowUpMessages {
				select {
				case <-ctx.Done():
					return
				default:
				}
				if i > 0 {
					select {
					case <-ctx.Done():
						return
					case <-time.After(followUpInjectionDelay):
					}
				}
				if err := delegate.InjectInputContext(msg); err != nil {
					// Log the error and continue — don't fail the whole delegate.
					delegate.PrintLineAsync(fmt.Sprintf("[warn] Failed to inject follow-up message: %v", err))
					continue
				}
				bridge.RecordFollowUpInjection(msg)
			}
		}()
	}

	// Use the delegate agent's ProcessQuery method to run the prompt.
	// ProcessQuery handles the full agent loop (tool calls, iterations, etc.)
	response, err := delegate.ProcessQuery(prompt)
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
