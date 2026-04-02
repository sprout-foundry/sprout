package agent

import (
	"context"
	"sync"
	"testing"
)

func TestProcessQuerySetsMaxIterationsTerminationReason(t *testing.T) {
	// Create a ScriptedResponse for the test
	scriptedResp := NewScriptedResponseBuilder().
		Content("Working through the next step.").
		FinishReason("").
		Build()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agent := &Agent{
		client:             NewScriptedClient(scriptedResp),
		systemPrompt:       "system",
		maxIterations:      1,
		inputInjectionChan: make(chan string, inputInjectionBufferSize),
		interruptCtx:       ctx,
		interruptCancel:    cancel,
		outputMutex:        &sync.Mutex{},
	}

	_, err := agent.ProcessQuery("Investigate the issue")
	if err != nil {
		t.Fatalf("ProcessQuery returned error: %v", err)
	}

	if got := agent.GetLastRunTerminationReason(); got != RunTerminationMaxIterations {
		t.Fatalf("expected termination reason %q, got %q", RunTerminationMaxIterations, got)
	}
}
