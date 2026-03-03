package agent

import (
	"context"
	"strings"
	"testing"
)

func TestParseStructuredJSONContent_SyntaxErrorIncludesLocationAndSnippet(t *testing.T) {
	_, err := parseStructuredJSONContent("{\n  \"name\": \"ok\"\n  \"price\": 10\n}", "write_file")
	if err == nil {
		t.Fatal("expected parse error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "line=") || !strings.Contains(msg, "col=") {
		t.Fatalf("expected line/col details in error, got: %s", msg)
	}
	if !strings.Contains(msg, "snippet=") {
		t.Fatalf("expected snippet in error, got: %s", msg)
	}
	if !strings.Contains(msg, "next_step=fix JSON syntax and retry write_file") {
		t.Fatalf("expected next_step hint in error, got: %s", msg)
	}
}

func TestParseStructuredJSONContent_RejectsScalarTopLevel(t *testing.T) {
	_, err := parseStructuredJSONContent("\"hello\"", "write_file")
	if err == nil {
		t.Fatal("expected top-level scalar to be rejected")
	}
	if !strings.Contains(err.Error(), "top-level JSON must be an object or array") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseStructuredJSONContent_EditFileHintUsesEditFile(t *testing.T) {
	_, err := parseStructuredJSONContent("{\n  \"name\": \"ok\"\n  \"price\": 10\n}", "edit_file")
	if err == nil {
		t.Fatal("expected parse error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "retry edit_file") {
		t.Fatalf("expected edit_file retry hint, got: %s", msg)
	}
	if strings.Contains(msg, "write_structured_file") {
		t.Fatalf("did not expect structured tool guidance in proxy path error: %s", msg)
	}
}

func TestHandleWriteFile_InvalidJSON_ReturnsForwardingDiagnostics(t *testing.T) {
	_, err := handleWriteFile(context.Background(), nil, map[string]interface{}{
		"path":    "./menu.json",
		"content": "{\n  \"name\": \"ok\"\n  \"price\": 10\n}",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "write_file JSON forwarding failed") {
		t.Fatalf("expected forwarding failure prefix, got: %s", msg)
	}
	if !strings.Contains(msg, "line=") || !strings.Contains(msg, "col=") {
		t.Fatalf("expected line/col details, got: %s", msg)
	}
}
