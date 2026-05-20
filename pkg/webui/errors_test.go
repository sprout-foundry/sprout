package webui

import "testing"

func TestWebUIError_Error(t *testing.T) {
	err := NewWebUIError("test_code", "test message", true)
	if err.Error() != "test message" {
		t.Errorf("Error() = %q, want %q", err.Error(), "test message")
	}
	if err.Code != "test_code" {
		t.Errorf("Code = %q, want %q", err.Code, "test_code")
	}
	if !err.Retryable {
		t.Error("Retryable should be true")
	}
}

func TestWebUIErrorWithDetails(t *testing.T) {
	details := map[string]string{"key": "value"}
	err := NewWebUIErrorWithDetails("conflict", "config conflict", false, details)
	if err.Details == nil {
		t.Error("Details should not be nil")
	}
	if err.Retryable {
		t.Error("Retryable should be false")
	}
}
