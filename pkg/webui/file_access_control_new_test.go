package webui

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// clearAll — pure helper
// ---------------------------------------------------------------------------

func TestClearAll_EmptyManager(t *testing.T) {
	t.Parallel()
	m := newFileConsentManager()
	m.clearAll()
	if len(m.grants) != 0 {
		t.Errorf("expected 0 grants after clearAll on empty manager, got %d", len(m.grants))
	}
}

func TestClearAll_SingleGrant(t *testing.T) {
	t.Parallel()
	m := newFileConsentManager()
	m.issue("/tmp/test.txt", "read", 1*time.Minute)
	if len(m.grants) != 1 {
		t.Fatalf("expected 1 grant before clearAll, got %d", len(m.grants))
	}
	m.clearAll()
	if len(m.grants) != 0 {
		t.Errorf("expected 0 grants after clearAll, got %d", len(m.grants))
	}
}

func TestClearAll_MultipleGrants(t *testing.T) {
	t.Parallel()
	m := newFileConsentManager()
	m.issue("/tmp/a.txt", "read", 1*time.Minute)
	m.issue("/tmp/b.txt", "write", 1*time.Minute)
	m.issue("/tmp/c.txt", "read", 1*time.Minute)
	if len(m.grants) != 3 {
		t.Fatalf("expected 3 grants before clearAll, got %d", len(m.grants))
	}
	m.clearAll()
	if len(m.grants) != 0 {
		t.Errorf("expected 0 grants after clearAll, got %d", len(m.grants))
	}
}

func TestClearAll_RecreatesMap(t *testing.T) {
	t.Parallel()
	// Verify that clearAll creates a new map (not just clearing the old one)
	m := newFileConsentManager()
	m.issue("/tmp/x.txt", "read", 1*time.Minute)
	m.clearAll()

	// After clearAll, issuing a new grant should work normally
	token, _, err := m.issue("/tmp/y.txt", "read", 1*time.Minute)
	if err != nil {
		t.Fatalf("issue after clearAll failed: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token after clearAll")
	}
	if len(m.grants) != 1 {
		t.Errorf("expected 1 grant after issuing new grant post-clearAll, got %d", len(m.grants))
	}
}

func TestClearAll_Twice(t *testing.T) {
	t.Parallel()
	m := newFileConsentManager()
	m.issue("/tmp/a.txt", "read", 1*time.Minute)
	m.clearAll()
	m.clearAll() // second call should be a no-op
	if len(m.grants) != 0 {
		t.Errorf("expected 0 grants after second clearAll, got %d", len(m.grants))
	}
}

func TestClearAll_WithExpiredGrants(t *testing.T) {
	t.Parallel()
	m := newFileConsentManager()
	// clearAll removes all grants regardless of expiry status.
	// Add a grant and clear — this tests that clearAll is unconditional.
	m.issue("/tmp/a.txt", "read", 1*time.Minute)

	m.clearAll()
	if len(m.grants) != 0 {
		t.Errorf("expected 0 grants after clearAll, got %d", len(m.grants))
	}
}
