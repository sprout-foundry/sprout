//go:build !js

package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// newTestLoopAgent builds an Agent wired to a ScriptedClient, isolated in a
// temp config directory so tests never touch the real config.
func newTestLoopAgent(t *testing.T, client *agent.ScriptedClient) *agent.Agent {
	t.Helper()

	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	ag, err := agent.NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient: %v", err)
	}
	t.Cleanup(ag.Shutdown)
	return ag
}

// writeTempTodoFile creates a TODO.md with the given items, returning its path.
func writeTempTodoFile(t *testing.T, dir string, items []string) string {
	t.Helper()
	var sb strings.Builder
	sb.WriteString("## Test Items\n")
	for _, item := range items {
		sb.WriteString("- [ ] " + item + "\n")
	}
	path := filepath.Join(dir, "TODO.md")
	if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
		t.Fatalf("write TODO.md: %v", err)
	}
	return path
}

// writeGatePromptFile writes the gate system prompt file and returns its path.
func writeGatePromptFile(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "gate_prompt.md")
	if err := os.WriteFile(path, []byte("Parse the TODO section into a delegation prompt."), 0644); err != nil {
		t.Fatalf("write gate_prompt.md: %v", err)
	}
	return path
}

// gateResponse builds a ScriptedResponse mimicking a successful gate call.
func gateResponse(title, prompt string, skip bool) *agent.ScriptedResponse {
	skipStr := "false"
	if skip {
		skipStr = "true"
	}
	return &agent.ScriptedResponse{
		Content: fmt.Sprintf(`{"title": %q, "prompt": %q, "skip": %s}`, title, prompt, skipStr),
	}
}

// triageResponse builds a ScriptedResponse mimicking a triage gate call.
func triageResponse(action, reason string) *agent.ScriptedResponse {
	return &agent.ScriptedResponse{
		Content: fmt.Sprintf(`{"action": %q, "reason": %q}`, action, reason),
	}
}

// =============================================================================
// TestRunAgentWorkflowLoop — integration tests for the TODO loop
// =============================================================================

func TestRunAgentWorkflowLoop(t *testing.T) {
	tests := []struct {
		name string
		// items in the TODO file.
		items []string
		// Scripted responses for gate / triage LLM calls, consumed in order.
		responses []*agent.ScriptedResponse
		// processResults defines the return values for each processQueryFn call.
		// Index matches call order. nil means success.
		processResults []error
		// buildCmd is the shell command for build verification.
		buildCmd string
		// expected outcomes
		wantComplete  bool
		wantError     bool
		wantChecked   int // number of items marked [x]
		wantProcessed int // itemsProcessed counter
		wantFailed    int // itemsFailed counter
		wantSkipped   int // itemsSkipped counter
		// contextTimeout, if > 0, sets a context deadline to prevent infinite
		// loops (e.g. when an incomplete item keeps being retried without
		// being marked done).
		contextTimeout string // Go duration string like "500ms"
	}{
		{
			name:     "full_success_path",
			items:    []string{"Implement feature X"},
			responses: []*agent.ScriptedResponse{
				gateResponse("Implement X", "Do the implementation", false),
			},
			processResults: []error{nil},
			buildCmd:       "true",
			wantComplete:   true,
			wantError:      false,
			wantChecked:    1,
			wantProcessed:  1,
		},
		{
			name:     "max_iterations_incomplete",
			items:    []string{"Refactor module Y"},
			responses: []*agent.ScriptedResponse{
				gateResponse("Refactor Y", "Refactor the module", false),
			},
			processResults: []error{fmt.Errorf("max iterations reached (50)")},
			buildCmd:       "true",
			wantComplete:   false,
			wantError:      true,
			wantChecked:    0,
			wantProcessed:  0,
			wantFailed:     1,
			// The item stays unchecked, causing the loop to re-find it forever.
			// We use a short timeout to halt.
			contextTimeout: "500ms",
		},
		{
			name:     "triage_skip",
			items:    []string{"Fix critical bug Z"},
			responses: []*agent.ScriptedResponse{
				gateResponse("Fix bug Z", "Fix the bug", false),
				triageResponse("skip", "blocking issue requires manual intervention"),
			},
			processResults: []error{nil},
			buildCmd:       "false",
			// After triage skip, the item is NOT marked [x] (correct behaviour —
			// the triage said it wasn't even attempted). The loop re-finds it
			// and runs out of scripted responses. The context timeout triggers
			// a "loop cancelled" error, which is the expected outcome.
			contextTimeout: "500ms",
			wantComplete:   false,
			wantError:      true,
			wantChecked:    0,
			wantProcessed:  0,
			wantFailed:     0,
			wantSkipped:    1,
		},
		{
			name:     "build_failure_then_retry_then_success",
			items:    []string{"Add new API endpoint W"},
			responses: []*agent.ScriptedResponse{
				gateResponse("Add API W", "Add the endpoint", false),
				triageResponse("retry", "transient build error, likely fixable"),
			},
			processResults: []error{nil, nil}, // first call + retry call
			// Counter-based: fails on first invocation (count=0), passes on
			// second (count=1). The retry processQueryFn writes the counter.
			buildCmd:      `count=$(cat /tmp/test_sprout_retry_count 2>/dev/null || echo 0); echo $((count+1)) > /tmp/test_sprout_retry_count; [ $count -gt 0 ]`,
			wantComplete:  true,
			wantError:     false,
			wantChecked:   1,
			wantProcessed: 1,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// Clean up stateful files used by some test scenarios.
			os.Remove("/tmp/test_sprout_retry_count")

			dir := t.TempDir()
			todoPath := writeTempTodoFile(t, dir, tt.items)
			gatePromptPath := writeGatePromptFile(t, dir)

			client := agent.NewScriptedClient(tt.responses...)
			client.SetModel("test:test")

			chatAgent := newTestLoopAgent(t, client)
			eventBus := events.NewEventBus()

			// Override processQueryFn for this test.
			oldProcessFn := processQueryFn
			callCount := 0
			processQueryFn = func(ctx context.Context, _ *agent.Agent, _ *events.EventBus, query string) error {
				if callCount >= len(tt.processResults) {
					t.Fatalf("processQueryFn called %d times but only %d results configured", callCount+1, len(tt.processResults))
				}
				err := tt.processResults[callCount]
				callCount++

				// For the retry scenario: write the build counter so the
				// retry build check passes.
				if tt.name == "build_failure_then_retry_then_success" && callCount > 1 {
					// Second (retry) call: prime the counter so `[ $count -gt 0 ]` passes.
					os.WriteFile("/tmp/test_sprout_retry_count", []byte("1\n"), 0644)
				}

				_ = query
				return err
			}
			defer func() { processQueryFn = oldProcessFn }()

			cfg := &AgentWorkflowConfig{
				Loop: &AgentWorkflowLoopConfig{
					TodoFile:       todoPath,
					GatePromptFile: gatePromptPath,
					MaxRetries:     1,
					MaxIterations:  50,
					BuildCommand:   tt.buildCmd,
				},
			}

			state := &workflowExecutionState{
				Version:       1,
				NextStepIndex: 0,
			}

			// Build context with optional timeout.
			ctx := context.Background()
			if tt.contextTimeout != "" {
				d, err := time.ParseDuration(tt.contextTimeout)
				if err != nil {
					t.Fatalf("invalid contextTimeout %q: %v", tt.contextTimeout, err)
				}
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, d)
				defer cancel()
			}

			yielded, err := runAgentWorkflowLoop(ctx, chatAgent, eventBus, cfg, state)

			if (err != nil) != tt.wantError {
				t.Errorf("runAgentWorkflowLoop error = %v, want error = %v", err, tt.wantError)
			}
			if state.Complete != tt.wantComplete {
				t.Errorf("state.Complete = %v, want %v", state.Complete, tt.wantComplete)
			}
			if yielded {
				t.Errorf("yielded = true, want false")
			}

			// Count [x] items in the TODO file.
			gotChecked := countChecked(t, todoPath)
			if gotChecked != tt.wantChecked {
				t.Errorf("checked [x] items = %d, want %d", gotChecked, tt.wantChecked)
			}
		})
	}
}

// countChecked returns the number of "[x]" or "[X]" markers in a file.
func countChecked(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	s := string(data)
	n := 0
	for i := 0; i+4 < len(s); i++ {
		if s[i] == '-' && s[i+1] == ' ' && s[i+2] == '[' && (s[i+3] == 'x' || s[i+3] == 'X') && s[i+4] == ']' {
			n++
		}
	}
	return n
}
