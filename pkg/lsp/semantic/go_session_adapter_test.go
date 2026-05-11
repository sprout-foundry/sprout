package semantic

import (
	"os/exec"
	"testing"
	"time"
)

func TestGoSessionAdapterHealthy(t *testing.T) {
	a := &goSessionAdapter{}
	if !a.Healthy() {
		t.Error("fresh adapter should be healthy")
	}
}

func TestGoSessionAdapterClose(t *testing.T) {
	a := &goSessionAdapter{}
	if err := a.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if a.Healthy() {
		t.Error("closed adapter should not be healthy")
	}
}

func TestGoSessionAdapterRunWhenClosed(t *testing.T) {
	a := &goSessionAdapter{}
	a.Close()
	_, err := a.Run(ToolInput{Method: "diagnostics"})
	if err == nil {
		t.Error("expected error when running on closed adapter")
	}
}

func TestGoSessionAdapterRunUnknownMethod(t *testing.T) {
	a := &goSessionAdapter{}
	defer a.Close()

	result, err := a.Run(ToolInput{Method: "unknown"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Capabilities.Diagnostics {
		t.Error("expected empty capabilities for unknown method")
	}
}

func TestGoSessionAdapterRunDiagnostics(t *testing.T) {
	_, err := exec.LookPath("gofmt")
	if err != nil {
		t.Skip("gofmt not available")
	}

	a := &goSessionAdapter{}
	defer a.Close()

	result, err := a.Run(ToolInput{
		Method:   "diagnostics",
		Content:  "package main\n\nfunc main() {\n\tfmt.Println(\n}\n",
		FilePath: "main.go",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Capabilities.Diagnostics {
		t.Error("expected diagnostics capability")
	}
	if len(result.Diagnostics) == 0 {
		t.Error("expected at least one diagnostic for invalid code")
	}
}

func TestGoSessionAdapterRunDefinitionNoGopls(t *testing.T) {
	_, err := exec.LookPath("gopls")
	if err == nil {
		t.Skip("gopls is available; skipping not-available path test")
	}

	a := &goSessionAdapter{}
	defer a.Close()

	result, err := a.Run(ToolInput{
		Method:        "definition",
		Content:       "package main\n",
		FilePath:      "main.go",
		WorkspaceRoot: "/tmp",
		Position:      &Position{Line: 1, Column: 1},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "gopls_not_available" {
		t.Errorf("expected gopls_not_available, got %q", result.Error)
	}
}

func TestGoSessionAdapterRunHoverNoGopls(t *testing.T) {
	_, err := exec.LookPath("gopls")
	if err == nil {
		t.Skip("gopls is available; skipping not-available path test")
	}

	a := &goSessionAdapter{}
	defer a.Close()

	result, err := a.Run(ToolInput{
		Method:        "hover",
		Content:       "package main\n",
		FilePath:      "main.go",
		WorkspaceRoot: "/tmp",
		Position:      &Position{Line: 1, Column: 1},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "gopls_not_available" {
		t.Errorf("expected gopls_not_available, got %q", result.Error)
	}
}

func TestGoSessionAdapterRunInlayHintsNoGopls(t *testing.T) {
	_, err := exec.LookPath("gopls")
	if err == nil {
		t.Skip("gopls is available; skipping not-available path test")
	}

	a := &goSessionAdapter{}
	defer a.Close()

	result, err := a.Run(ToolInput{
		Method:        "inlay_hints",
		Content:       "package main\n",
		FilePath:      "main.go",
		WorkspaceRoot: "/tmp",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "gopls_not_available" {
		t.Errorf("expected gopls_not_available, got %q", result.Error)
	}
}

func TestNewGoSessionPool(t *testing.T) {
	pool := NewGoSessionPool(5 * time.Minute)
	if pool == nil {
		t.Fatal("expected non-nil pool")
	}
	pool.Close()
}
