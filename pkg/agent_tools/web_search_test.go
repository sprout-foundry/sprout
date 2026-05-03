package tools

import (
	"strings"
	"testing"
)

func TestWebSearch_EmptyQuery(t *testing.T) {
	_, err := WebSearch("", nil)
	if err == nil {
		t.Fatal("expected error for empty query")
	}
	if !strings.Contains(err.Error(), "search query cannot be empty") {
		t.Errorf("unexpected error message: %v", err)
	}
}
