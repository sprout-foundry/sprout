package webui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alantheprice/ledit/pkg/events"
)

// --- gatherStatsForClientIDLocked: is_processing ---

func TestGatherStatsForClientIDLocked_ActiveQuery_ReturnsIsProcessingTrue(t *testing.T) {
	server := NewReactWebServer(nil, events.NewEventBus(), 0)

	server.getOrCreateClientContext("test-client")
	server.mutex.Lock()
	server.clientContexts["test-client"].ActiveQuery = true
	stats := server.gatherStatsForClientIDLocked("test-client")
	server.mutex.Unlock()

	if v, ok := stats["is_processing"].(bool); !ok || !v {
		t.Fatalf("expected is_processing=true, got %v", stats["is_processing"])
	}
}

func TestGatherStatsForClientIDLocked_NoActiveQuery_ReturnsIsProcessingFalse(t *testing.T) {
	server := NewReactWebServer(nil, events.NewEventBus(), 0)

	server.getOrCreateClientContext("test-client")
	server.mutex.Lock()
	server.clientContexts["test-client"].ActiveQuery = false
	stats := server.gatherStatsForClientIDLocked("test-client")
	server.mutex.Unlock()

	if v, ok := stats["is_processing"].(bool); !ok || v {
		t.Fatalf("expected is_processing=false, got %v", stats["is_processing"])
	}
}

// --- gatherStatsForClientIDLocked: current_query ---

func TestGatherStatsForClientIDLocked_ActiveQueryWithText_ReturnsCurrentQuery(t *testing.T) {
	server := NewReactWebServer(nil, events.NewEventBus(), 0)

	server.getOrCreateClientContext("test-client")
	server.mutex.Lock()
	server.clientContexts["test-client"].ActiveQuery = true
	server.clientContexts["test-client"].CurrentQuery = "fix the login bug"
	stats := server.gatherStatsForClientIDLocked("test-client")
	server.mutex.Unlock()

	if v, ok := stats["current_query"].(string); !ok || v != "fix the login bug" {
		t.Fatalf("expected current_query=%q, got %v", "fix the login bug", stats["current_query"])
	}
}

func TestGatherStatsForClientIDLocked_NoActiveQuery_DoesNotReturnCurrentQuery(t *testing.T) {
	server := NewReactWebServer(nil, events.NewEventBus(), 0)

	server.getOrCreateClientContext("test-client")
	server.mutex.Lock()
	server.clientContexts["test-client"].ActiveQuery = false
	server.clientContexts["test-client"].CurrentQuery = ""
	stats := server.gatherStatsForClientIDLocked("test-client")
	server.mutex.Unlock()

	if _, exists := stats["current_query"]; exists {
		t.Fatalf("expected no current_query key in stats, but got %v", stats["current_query"])
	}
}

func TestGatherStatsForClientIDLocked_ActiveQueryButEmptyText_DoesNotReturnCurrentQuery(t *testing.T) {
	server := NewReactWebServer(nil, events.NewEventBus(), 0)

	server.getOrCreateClientContext("test-client")
	server.mutex.Lock()
	server.clientContexts["test-client"].ActiveQuery = true
	server.clientContexts["test-client"].CurrentQuery = ""
	stats := server.gatherStatsForClientIDLocked("test-client")
	server.mutex.Unlock()

	if v, ok := stats["is_processing"].(bool); !ok || !v {
		t.Fatalf("expected is_processing=true when ActiveQuery is set, got %v", stats["is_processing"])
	}
	if _, exists := stats["current_query"]; exists {
		t.Fatalf("expected no current_query key when CurrentQuery is empty, but got %v", stats["current_query"])
	}
}

// --- setClientWorkspaceRoot clears CurrentQuery ---

func TestSetClientWorkspaceRoot_ClearsCurrentQuery(t *testing.T) {
	daemonRoot := t.TempDir()
	workspaceA := filepath.Join(daemonRoot, "workspace-a")
	workspaceB := filepath.Join(daemonRoot, "workspace-b")
	if err := os.MkdirAll(workspaceA, 0o755); err != nil {
		t.Fatalf("mkdir workspace-a: %v", err)
	}
	if err := os.MkdirAll(workspaceB, 0o755); err != nil {
		t.Fatalf("mkdir workspace-b: %v", err)
	}

	server := NewReactWebServer(nil, events.NewEventBus(), 0)
	server.daemonRoot = daemonRoot
	server.workspaceRoot = daemonRoot

	clientID := "chrome-tab-1"

	// Set initial workspace
	if _, err := server.setClientWorkspaceRoot(clientID, workspaceA); err != nil {
		t.Fatalf("set workspaceA: %v", err)
	}

	// Simulate an active query with text
	server.mutex.Lock()
	server.clientContexts[clientID].ActiveQuery = true
	server.clientContexts[clientID].CurrentQuery = "refactor authentication module"
	server.mutex.Unlock()

	// Switch workspace — should clear CurrentQuery and ActiveQuery
	if _, err := server.setClientWorkspaceRoot(clientID, workspaceB); err != nil {
		t.Fatalf("set workspaceB: %v", err)
	}

	server.mutex.Lock()
	ctx := server.clientContexts[clientID]
	server.mutex.Unlock()

	if ctx.CurrentQuery != "" {
		t.Fatalf("expected CurrentQuery to be cleared after workspace switch, got %q", ctx.CurrentQuery)
	}
	if ctx.ActiveQuery {
		t.Fatal("expected ActiveQuery to be cleared after workspace switch")
	}
}

// --- decrementActiveQueries clears CurrentQuery ---

func TestDecrementActiveQueries_ClearsCurrentQuery(t *testing.T) {
	server := NewReactWebServer(nil, events.NewEventBus(), 0)

	clientID := "chrome-tab-2"
	server.getOrCreateClientContext(clientID)

	// Manually set up the state that incrementActiveQueries would produce
	server.mutex.Lock()
	server.activeQueries = 1
	server.clientContexts[clientID].ActiveQuery = true
	server.clientContexts[clientID].CurrentQuery = "write unit tests for auth module"
	server.mutex.Unlock()

	// Call decrementActiveQueries
	server.decrementActiveQueries(clientID)

	server.mutex.Lock()
	ctx := server.clientContexts[clientID]
	activeQueries := server.activeQueries
	server.mutex.Unlock()

	if activeQueries != 0 {
		t.Fatalf("expected activeQueries=0 after decrement, got %d", activeQueries)
	}
	if ctx.ActiveQuery {
		t.Fatal("expected ActiveQuery to be false after decrement")
	}
	if ctx.CurrentQuery != "" {
		t.Fatalf("expected CurrentQuery to be cleared after decrement, got %q", ctx.CurrentQuery)
	}
}

func TestDecrementActiveQueries_NonexistentClient_DoesNotPanic(t *testing.T) {
	server := NewReactWebServer(nil, events.NewEventBus(), 0)

	// Decrementing for a client that has no context should not panic.
	server.decrementActiveQueries("nonexistent-client")
}

func TestDecrementActiveQueries_AlreadyZero_DoesNotGoNegative(t *testing.T) {
	server := NewReactWebServer(nil, events.NewEventBus(), 0)

	// Start at zero, decrement should not go below zero
	server.mutex.Lock()
	server.activeQueries = 0
	server.mutex.Unlock()

	server.decrementActiveQueries("any-client")

	server.mutex.Lock()
	count := server.activeQueries
	server.mutex.Unlock()

	if count < 0 {
		t.Fatalf("expected activeQueries >= 0, got %d", count)
	}
}

// --- Cross-client isolation: is_processing is per-client ---

func TestGatherStatsForClientIDLocked_IsProcessingPerClient(t *testing.T) {
	server := NewReactWebServer(nil, events.NewEventBus(), 0)

	server.getOrCreateClientContext("window-a")
	server.getOrCreateClientContext("window-b")

	server.mutex.Lock()
	server.clientContexts["window-a"].ActiveQuery = true
	server.clientContexts["window-a"].CurrentQuery = "active query in window A"
	server.clientContexts["window-b"].ActiveQuery = false
	server.clientContexts["window-b"].CurrentQuery = ""
	statsA := server.gatherStatsForClientIDLocked("window-a")
	statsB := server.gatherStatsForClientIDLocked("window-b")
	server.mutex.Unlock()

	// Window A should show active
	if v, ok := statsA["is_processing"].(bool); !ok || !v {
		t.Errorf("window-a: expected is_processing=true, got %v", statsA["is_processing"])
	}
	if v, ok := statsA["current_query"].(string); !ok || v != "active query in window A" {
		t.Errorf("window-a: expected current_query=%q, got %v", "active query in window A", statsA["current_query"])
	}

	// Window B should show idle
	if v, ok := statsB["is_processing"].(bool); !ok || v {
		t.Errorf("window-b: expected is_processing=false, got %v", statsB["is_processing"])
	}
	if _, exists := statsB["current_query"]; exists {
		t.Errorf("window-b: expected no current_query key, got %v", statsB["current_query"])
	}
}

// --- Full flow: increment + decrement clears state correctly ---

func TestIncrementThenDecrementClearsQueryState(t *testing.T) {
	server := NewReactWebServer(nil, events.NewEventBus(), 0)

	clientID := "flow-test"
	server.getOrCreateClientContext(clientID)

	// Simulate the full query lifecycle
	server.incrementActiveQueries(clientID)

	// Set the current query text (normally done right after increment in handleAPIQuery)
	server.mutex.Lock()
	server.clientContexts[clientID].CurrentQuery = "implement dark mode"
	server.mutex.Unlock()

	// Verify state before decrement
	server.mutex.Lock()
	before := server.gatherStatsForClientIDLocked(clientID)
	server.mutex.Unlock()

	if v, ok := before["is_processing"].(bool); !ok || !v {
		t.Fatalf("before decrement: expected is_processing=true, got %v", before["is_processing"])
	}
	if v, ok := before["current_query"].(string); !ok || v != "implement dark mode" {
		t.Fatalf("before decrement: expected current_query=%q, got %v", "implement dark mode", before["current_query"])
	}

	// Query completes
	server.decrementActiveQueries(clientID)

	// Verify state after decrement
	server.mutex.Lock()
	after := server.gatherStatsForClientIDLocked(clientID)
	activeQueries := server.activeQueries
	server.mutex.Unlock()

	if activeQueries != 0 {
		t.Errorf("expected activeQueries=0 after full cycle, got %d", activeQueries)
	}
	if v, ok := after["is_processing"].(bool); !ok || v {
		t.Errorf("after decrement: expected is_processing=false, got %v", after["is_processing"])
	}
	if _, exists := after["current_query"]; exists {
		t.Errorf("after decrement: expected no current_query key, got %v", after["current_query"])
	}
}
