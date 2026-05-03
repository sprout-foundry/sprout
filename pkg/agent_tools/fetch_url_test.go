package tools

import (
	"strings"
	"testing"
)

func TestFetchURL_EmptyURL(t *testing.T) {
	_, err := FetchURL("", nil)
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
	if !strings.Contains(err.Error(), "URL cannot be empty") {
		t.Errorf("unexpected error message: %v", err)
	}
}
