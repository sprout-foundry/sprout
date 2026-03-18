package agent

import (
	"testing"

	"github.com/alantheprice/ledit/pkg/trace"
)

// TestSetTraceSession verifies that the trace session is properly set on the agent
// and passed to the conversation handler.
func TestSetTraceSession(t *testing.T) {
	// When running under go test, this will use TestClientType automatically
	agent, err := NewAgentWithModel("test")
	if err != nil {
		t.Fatalf("Failed to create test agent: %v", err)
	}

	// Create a trace session
	traceSession, err := trace.NewTraceSession("/tmp/test_trace", "test_provider", "test_model")
	if err != nil {
		t.Fatalf("Failed to create trace session: %v", err)
	}
	defer traceSession.Close()

	// Set trace session on agent
	agent.SetTraceSession(traceSession)

	// Verify trace session is set on agent
	if agent.traceSession == nil {
		t.Fatal("Agent trace session is nil after SetTraceSession()")
	}

	// Create conversation handler
	handler := NewConversationHandler(agent)

	// Verify trace session is passed to conversation handler
	if handler.traceSession == nil {
		t.Fatal("ConversationHandler trace session is nil after NewConversationHandler()")
	}

	// Verify it's the same trace session
	if handler.traceSession != agent.traceSession {
		t.Fatal("ConversationHandler trace session is not the same as agent's trace session")
	}
}

// TestSetTraceSessionNil verifies that setting nil trace session works
func TestSetTraceSessionNil(t *testing.T) {
	// When running under go test, this will use TestClientType automatically
	agent, err := NewAgentWithModel("test")
	if err != nil {
		t.Fatalf("Failed to create test agent: %v", err)
	}

	// Set nil trace session
	agent.SetTraceSession(nil)

	// Verify trace session is nil on agent
	if agent.traceSession != nil {
		t.Fatal("Agent trace session should be nil after SetTraceSession(nil)")
	}

	// Create conversation handler
	handler := NewConversationHandler(agent)

	// Verify trace session is also nil on handler
	if handler.traceSession != nil {
		t.Fatal("ConversationHandler trace session should be nil when agent's trace session is nil")
	}
}
