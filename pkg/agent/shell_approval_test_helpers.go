//go:build !js

// Package agent — exported test helpers for the shell approval broker.
//
// These let tests in other packages (e.g. pkg/webui) interact with the
// shellApprovalBroker without needing a full agent instance. They are NOT
// for production use — only for tests.
package agent

// TestShellApprovalRegister creates a buffered response channel for the
// given request ID in the shellApprovalBroker and returns it.
//
// For testing only — called from webui tests to simulate the agent side
// registering a pending request before the handler POSTs back.
func TestShellApprovalRegister(requestID string) chan map[string]bool {
	return shellApprovalBroker.register(requestID)
}

// TestShellApprovalRespond delivers decisions to a pending request.
// Returns true if the request was found and the decisions were delivered.
//
// For testing only — called from webui tests to simulate the agent
// RespondToShellApproval without needing an agent instance.
func TestShellApprovalRespond(requestID string, decisions map[string]bool) bool {
	return shellApprovalBroker.respond(requestID, decisions)
}

// TestShellApprovalCleanup removes a pending entry from the broker.
// Used by tests to clean up after themselves so the global broker
// doesn't accumulate stale entries across tests.
func TestShellApprovalCleanup(requestID string) {
	shellApprovalBroker.cleanup(requestID)
}
