package agent

import (
	"context"
	"testing"
	"time"
)

// TestShellApprovalBroker_RegisterRespond verifies the core register →
// respond → receive lifecycle.
func TestShellApprovalBroker_RegisterRespond(t *testing.T) {
	broker := &shellApprovalBrokerType{
		pending: make(map[string]*shellApprovalEntry),
	}
	ch := broker.register("test-1")

	go func() {
		time.Sleep(10 * time.Millisecond)
		broker.respond("test-1", map[string]bool{"part-0": true, "part-1": false})
	}()

	select {
	case decisions := <-ch:
		if decisions["part-0"] != true {
			t.Errorf("part-0: want true, got false")
		}
		if decisions["part-1"] != false {
			t.Errorf("part-1: want false, got true")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for decisions")
	}
}

// TestShellApprovalBroker_UnknownIDRespond returns false for an
// unregistered request ID.
func TestShellApprovalBroker_UnknownIDRespond(t *testing.T) {
	broker := &shellApprovalBrokerType{
		pending: make(map[string]*shellApprovalEntry),
	}
	if broker.respond("nonexistent", map[string]bool{}) {
		t.Error("respond to unknown ID should return false")
	}
}

// TestShellApprovalBroker_DoubleRespond returns false on the second
// attempt (the channel is buffered 1; the first fill succeeds).
func TestShellApprovalBroker_DoubleRespond(t *testing.T) {
	broker := &shellApprovalBrokerType{
		pending: make(map[string]*shellApprovalEntry),
	}
	broker.register("test-double")

	first := broker.respond("test-double", map[string]bool{"part-0": true})
	if !first {
		t.Error("first respond should succeed")
	}

	second := broker.respond("test-double", map[string]bool{"part-0": false})
	if second {
		t.Error("second respond should fail (channel full)")
	}
}

// TestShellApprovalBroker_Cleanup removes the pending entry.
func TestShellApprovalBroker_Cleanup(t *testing.T) {
	broker := &shellApprovalBrokerType{
		pending: make(map[string]*shellApprovalEntry),
	}
	broker.register("test-cleanup")
	broker.cleanup("test-cleanup")

	// After cleanup, respond should fail (ID not found).
	if broker.respond("test-cleanup", map[string]bool{}) {
		t.Error("respond after cleanup should return false")
	}
}

// TestRequestShellApprovalViaWebUI_TimeoutDeniesAll verifies that when
// the timeout fires without a response, all parts are denied (false).
// This is the safe fallback — never approve without explicit user consent.
func TestRequestShellApprovalViaWebUI_TimeoutDeniesAll(t *testing.T) {
	// Use a short timeout so the test runs fast.
	original := shellApprovalTimeout
	shellApprovalTimeout = 50 * time.Millisecond
	defer func() { shellApprovalTimeout = original }()

	a := &Agent{}
	proposal := ShellProposal{
		Command: "echo hello && rm -rf /tmp/x",
		Parts: []ShellPart{
			{ID: "part-0", Text: "echo hello", Kind: CommandKindUnknown},
			{ID: "part-1", Text: "rm -rf /tmp/x", Kind: CommandKindRm},
		},
	}

	// RequestShellApproval calls requestShellApprovalViaWebUI only when
	// hasWebUI is true. Test the method directly to avoid mocking the
	// WebUI detection.
	ctx := context.Background()
	decisions, err := a.requestShellApprovalViaWebUI(ctx, proposal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, part := range proposal.Parts {
		if decisions[part.ID] {
			t.Errorf("part %s on timeout: want false (deny), got true", part.ID)
		}
	}
}

// TestRequestShellApprovalViaWebUI_ResponseDelivered verifies the happy
// path: a goroutine responds before the timeout, and the decisions are
// returned faithfully.
func TestRequestShellApprovalViaWebUI_ResponseDelivered(t *testing.T) {
	original := shellApprovalTimeout
	shellApprovalTimeout = 2 * time.Second
	defer func() { shellApprovalTimeout = original }()

	a := &Agent{}
	proposal := ShellProposal{
		Command: "ls && rm -rf /tmp/x",
		Parts: []ShellPart{
			{ID: "part-0", Text: "ls", Kind: CommandKindUnknown},
			{ID: "part-1", Text: "rm -rf /tmp/x", Kind: CommandKindRm},
		},
	}

	// We can't easily predict the generated request ID, so register a
	// responder that polls the broker. The simplest approach: launch the
	// approval in a goroutine and respond via a background watcher.
	done := make(chan map[string]bool, 1)
	go func() {
		ctx := context.Background()
		decisions, _ := a.requestShellApprovalViaWebUI(ctx, proposal)
		done <- decisions
	}()

	// Poll for the pending entry, then respond.
	// The broker is package-level; wait until the request appears.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		shellApprovalBroker.mu.Lock()
		var foundID string
		for id := range shellApprovalBroker.pending {
			foundID = id
			break
		}
		shellApprovalBroker.mu.Unlock()
		if foundID != "" {
			shellApprovalBroker.respond(foundID, map[string]bool{"part-0": true, "part-1": false})
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	select {
	case decisions := <-done:
		if !decisions["part-0"] {
			t.Error("part-0: want true (approved), got false")
		}
		if decisions["part-1"] {
			t.Error("part-1: want false (denied), got true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for approval result")
	}
}

// TestGenerateShellApprovalRequestID produces unique, incrementing IDs.
func TestGenerateShellApprovalRequestID(t *testing.T) {
	id1 := generateShellApprovalRequestID()
	id2 := generateShellApprovalRequestID()
	if id1 == id2 {
		t.Errorf("expected unique IDs, got %s twice", id1)
	}
	if id1 == "" || id2 == "" {
		t.Error("IDs should not be empty")
	}
}
