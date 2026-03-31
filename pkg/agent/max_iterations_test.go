package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// makeAgentWithScriptedClient creates a minimal Agent wired to the given
// scripted client.  This mirrors the setup in termination_reason_test.go.
func makeAgentWithScriptedClient(maxIter int, client *scriptedClient) *Agent {
	ctx, cancel := context.WithCancel(context.Background())
	return &Agent{
		client:             client,
		systemPrompt:       "system",
		maxIterations:      maxIter,
		inputInjectionChan: make(chan string, inputInjectionBufferSize),
		interruptCtx:       ctx,
		interruptCancel:    cancel,
		outputMutex:        &sync.Mutex{},
	}
}

// keepGoingResponse returns a ChatResponse whose finish_reason is empty,
// signalling to the conversation handler that the model wants to continue.
func keepGoingResponse() *api.ChatResponse {
	resp := &api.ChatResponse{
		Choices: []api.Choice{{
			FinishReason: "",
		}},
	}
	resp.Choices[0].Message.Role = "assistant"
	resp.Choices[0].Message.Content = "Still working..."
	return resp
}

// stopResponse returns a ChatResponse with finish_reason "stop".
func stopResponse() *api.ChatResponse {
	resp := &api.ChatResponse{
		Choices: []api.Choice{{
			FinishReason: "stop",
		}},
	}
	resp.Choices[0].Message.Role = "assistant"
	resp.Choices[0].Message.Content = "Done."
	return resp
}

// ---------------------------------------------------------------------------
// Test 1 – maxIterations = 0 means unlimited
// ---------------------------------------------------------------------------

func TestMaxIterationsZeroMeansUnlimited(t *testing.T) {
	// Build responses: 10 iterations of "keep going" followed by "stop".
	// With maxIterations=0 the loop should *never* set termination reason to
	// RunTerminationMaxIterations — only the explicit stop should end it.
	totalKeepGoing := 10
	responses := make([]*api.ChatResponse, 0, totalKeepGoing+1)
	for i := 0; i < totalKeepGoing; i++ {
		responses = append(responses, keepGoingResponse())
	}
	responses = append(responses, stopResponse())

	agent := makeAgentWithScriptedClient(0, newScriptedClient(responses...))
	_, err := agent.ProcessQuery("Keep going until done")
	if err != nil {
		t.Fatalf("ProcessQuery returned error: %v", err)
	}

	reason := agent.GetLastRunTerminationReason()
	if reason == RunTerminationMaxIterations {
		t.Fatalf("expected NO max-iterations termination; got %q — the agent should have been unlimited", reason)
	}
	if reason != RunTerminationCompleted {
		t.Fatalf("expected termination reason %q, got %q", RunTerminationCompleted, reason)
	}
	// Sanity check: the agent looped more than the old default cap would allow.
	// With the old default of 1000 this wouldn't pass if 0 meant 1.
	if agent.GetCurrentIteration() < totalKeepGoing {
		t.Fatalf("expected at least %d iterations, got %d", totalKeepGoing, agent.GetCurrentIteration())
	}
}

// ---------------------------------------------------------------------------
// Test 2 – positive maxIterations still caps the loop
// ---------------------------------------------------------------------------

func TestMaxIterationsPositiveStillCaps(t *testing.T) {
	// Every response says "keep going" (empty finish_reason).
	responses := make([]*api.ChatResponse, 100)
	for i := range responses {
		responses[i] = keepGoingResponse()
	}

	agent := makeAgentWithScriptedClient(3, newScriptedClient(responses...))
	_, err := agent.ProcessQuery("Should cap at 3")
	if err != nil {
		t.Fatalf("ProcessQuery returned error: %v", err)
	}

	if got := agent.GetLastRunTerminationReason(); got != RunTerminationMaxIterations {
		t.Fatalf("expected termination reason %q, got %q", RunTerminationMaxIterations, got)
	}
	if got := agent.GetCurrentIteration(); got != 3 {
		t.Fatalf("expected currentIteration 3, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Test 3 – SetMaxIterations(0) / SetMaxIterations(N) round-trip
// ---------------------------------------------------------------------------

func TestSetMaxIterationsZeroIsUnlimited(t *testing.T) {
	t.Run("zero means unlimited", func(t *testing.T) {
		a := makeAgentWithScriptedClient(99, newScriptedClient())
		a.SetMaxIterations(0)
		if got := a.GetMaxIterations(); got != 0 {
			t.Fatalf("GetMaxIterations() = %d, want 0 (unlimited)", got)
		}
	})

	t.Run("positive value is preserved", func(t *testing.T) {
		a := makeAgentWithScriptedClient(0, newScriptedClient())
		a.SetMaxIterations(50)
		if got := a.GetMaxIterations(); got != 50 {
			t.Fatalf("GetMaxIterations() = %d, want 50", got)
		}
	})

	t.Run("negative value is clamped to unlimited", func(t *testing.T) {
		a := makeAgentWithScriptedClient(99, newScriptedClient())
		a.SetMaxIterations(-5)
		if got := a.GetMaxIterations(); got != 0 {
			t.Fatalf("GetMaxIterations() = %d, want 0 (clamped from negative)", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Test 4 – default maxIterations is zero
// ---------------------------------------------------------------------------

func TestDefaultMaxIterationsIsZero(t *testing.T) {
	// Construct a minimal agent the way tests do; maxIterations should default
	// to the zero-value of int, which is 0 (unlimited).
	agent := makeAgentWithScriptedClient(0, newScriptedClient())

	if got := agent.GetMaxIterations(); got != 0 {
		t.Fatalf("default GetMaxIterations() = %d, want 0 (unlimited)", got)
	}

	// Also confirm currentIteration starts at 0.
	if got := agent.GetCurrentIteration(); got != 0 {
		t.Fatalf("default GetCurrentIteration() = %d, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// Test 5 – Error handler displays "(unlimited)" when maxIterations is 0
// ---------------------------------------------------------------------------

func TestErrorHandlerUnlimitedDisplay(t *testing.T) {
	t.Run("max zero shows unlimited", func(t *testing.T) {
		a := makeAgentWithScriptedClient(0, newScriptedClient())
		eh := NewErrorHandler(a)
		result, err := eh.HandleAPIFailure(fmt.Errorf("something went wrong"), []api.Message{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "(unlimited)") {
			t.Fatalf("expected error response to contain '(unlimited)' when maxIterations=0, got:\n%s", result)
		}
	})

	t.Run("max positive shows fraction", func(t *testing.T) {
		a := makeAgentWithScriptedClient(50, newScriptedClient())
		a.currentIteration = 7
		eh := NewErrorHandler(a)
		result, err := eh.HandleAPIFailure(fmt.Errorf("something went wrong"), []api.Message{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "7/50") {
			t.Fatalf("expected error response to contain '7/50' when maxIterations=50, got:\n%s", result)
		}
		if strings.Contains(result, "(unlimited)") {
			t.Fatalf("error response should NOT contain '(unlimited)' when maxIterations=50, got:\n%s", result)
		}
	})
}
