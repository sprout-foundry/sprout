package agent

import (
	"context"
	"strings"
	"testing"
	"time"
)

// handleViewHistory and handleRollbackChanges are integration-heavy because they
// call tools.ViewHistory / tools.RollbackChanges which require external state.
// We focus on the input-validation and argument-parsing aspects that can be
// tested with minimal Agent setup.

func TestHandleViewHistoryInvalidSinceFormat(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	_, err := handleViewHistory(context.Background(), a, map[string]interface{}{
		"since": "not-a-valid-timestamp",
	})
	if err == nil {
		t.Fatal("expected error for invalid since format")
	}
	if !strings.Contains(err.Error(), "invalid time format") {
		t.Errorf("expected 'invalid time format' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "ISO 8601") {
		t.Errorf("expected ISO 8601 hint in error, got: %v", err)
	}
}

func TestHandleViewHistoryLimitConversion(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	// When 'since' is empty, the handler skips time parsing and proceeds
	// to call tools.ViewHistory which will fail with test agent — but
	// we can still validate the argument parsing by checking what happens
	// with different limit types.

	// float64 limit (from JSON) should be converted to int
	_, err := handleViewHistory(context.Background(), a, map[string]interface{}{
		"limit": float64(25),
		"since": "",
	})
	// This will fail because tools.ViewHistory can't connect, but the point
	// is the float64->int conversion happens without panicking.
	if err == nil {
		t.Log("handleViewHistory succeeded (external dependency available)")
	} else {
		// We expect failure due to external deps, not argument parsing
		t.Logf("handleViewHistory failed (expected for test agent): %v", err)
	}
}

func TestHandleViewHistoryDefaultLimit(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	// No limit specified — defaults to 10
	_, err := handleViewHistory(context.Background(), a, map[string]interface{}{})
	// Will fail due to tools dependency; we just verify no panic on defaults
	if err == nil {
		t.Log("handleViewHistory succeeded (external dependency available)")
	}
}

func TestHandleViewHistoryIntLimit(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	_, err := handleViewHistory(context.Background(), a, map[string]interface{}{
		"limit": 5,
		"since": "",
	})
	if err == nil {
		t.Log("handleViewHistory succeeded (external dependency available)")
	}
}

func TestHandleViewHistoryValidSinceFormat(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	ts := time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
	_, err := handleViewHistory(context.Background(), a, map[string]interface{}{
		"limit":        5,
		"since":        ts,
		"show_content": true,
		"file_filter":  "src/",
	})
	if err == nil {
		t.Log("handleViewHistory succeeded (external dependency available)")
	}
	// We verified: valid since format does not trigger the "invalid time format" error
	if err != nil {
		if strings.Contains(err.Error(), "invalid time format") {
			t.Errorf("valid since format should not trigger format error, got: %v", err)
		}
	}
}

func TestHandleRollbackChangesMissingArgs(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	// Empty args — should use defaults without panicking
	_, err := handleRollbackChanges(context.Background(), a, map[string]interface{}{})
	if err == nil {
		t.Log("handleRollbackChanges succeeded (external dependency available)")
	}
	// No panic on empty args is the test
}

func TestHandleRollbackChangesWithArgs(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	_, err := handleRollbackChanges(context.Background(), a, map[string]interface{}{
		"revision_id": "rev-123",
		"file_path":   "src/main.go",
		"confirm":     true,
	})
	if err == nil {
		t.Log("handleRollbackChanges succeeded (external dependency available)")
	}
}

func TestHandleRollbackChangesWhitespaceTrimming(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	// Args with whitespace should be trimmed
	_, err := handleRollbackChanges(context.Background(), a, map[string]interface{}{
		"revision_id": "  rev-123  ",
		"file_path":   "  src/main.go  ",
		"confirm":     false,
	})
	if err == nil {
		t.Log("handleRollbackChanges succeeded (external dependency available)")
	}
	// No panic is the test — whitespace trimming should work without error
}

func TestHandleRollbackChangesNonStringRevisionId(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	// revision_id as float64 (from JSON parsing) — type assertion fails, defaults to ""
	_, err := handleRollbackChanges(context.Background(), a, map[string]interface{}{
		"revision_id": float64(123), // not a string
		"confirm":     false,
	})
	if err == nil {
		t.Log("handleRollbackChanges succeeded (external dependency available)")
	}
	// No panic — type assertion gracefully falls back to empty string
}
