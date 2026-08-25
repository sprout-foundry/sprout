package localmodel

import (
	"testing"
	"time"
)

func TestTouchActivity(t *testing.T) {
	ResetIdleForTest()

	TouchActivity()
	last := LastActivity()
	if last.IsZero() {
		t.Error("LastActivity should not be zero after TouchActivity")
	}

	time.Sleep(10 * time.Millisecond)
	TouchActivity()
	last2 := LastActivity()
	if !last2.After(last) {
		t.Error("LastActivity should advance after second TouchActivity")
	}
}

func TestResetIdleForTest(t *testing.T) {
	TouchActivity()
	if LastActivity().IsZero() {
		t.Error("expected non-zero activity after Touch")
	}
	ResetIdleForTest()
	if !LastActivity().IsZero() {
		t.Error("expected zero activity after ResetIdleForTest")
	}
}

func TestPlatformSupported(t *testing.T) {
	// Should not panic, should return a bool.
	result := PlatformSupported()
	_ = result
}

func TestIsRunning_NotRunning(t *testing.T) {
	// No server running on a fresh test — should return false.
	// (If a server happens to be running, this test is skipped.)
	if IsRunning() {
		t.Skip("a local LLM server is running — skipping not-running test")
	}
}

func TestEnsureServerForProvider_NonLocalProvider(t *testing.T) {
	ctx := t.Context()
	err := EnsureServerForProvider(ctx, "openrouter")
	if err != nil {
		t.Errorf("EnsureServerForProvider should be no-op for non-local providers, got: %v", err)
	}
}

func TestSproutLocalProviderID(t *testing.T) {
	if sproutLocalProviderID != "sprout-local" {
		t.Errorf("expected 'sprout-local', got %q", sproutLocalProviderID)
	}
}

func TestMarkServerActivityForTest(t *testing.T) {
	specific := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	markServerActivityForTest(specific)
	if got := LastActivity(); !got.Equal(specific) {
		t.Errorf("expected %v, got %v", specific, got)
	}
	ResetIdleForTest()
}
