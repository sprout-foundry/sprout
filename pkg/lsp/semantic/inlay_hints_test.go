package semantic

import (
	"os/exec"
	"testing"
	"time"
)

// Tests for inlay hints and gopls server session paths.

func TestGoAdapterInlayHintsWithGopls(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	content := "package main\n\nfunc main() {\n\tx := 1\n\t_ = x\n}\n"
	dir, file := setupGoTestModule(t, content)

	adapter := NewGoAdapter()
	result, err := adapter.Run(ToolInput{
		Method:        "inlay_hints",
		Content:       content,
		FilePath:      file,
		WorkspaceRoot: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Capabilities should indicate inlay hints are supported
	if !result.Capabilities.InlayHints {
		t.Error("expected InlayHints capability to be true")
	}
}

func TestGoSessionAdapterDefinitionWithGopls(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	content := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"
	dir, file := setupGoTestModule(t, content)

	pool := NewGoSessionPool(5 * time.Minute)
	defer pool.Close()

	result, err := pool.Run(ToolInput{
		Method:        "definition",
		Content:       content,
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      &Position{Line: 6, Column: 6},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Capabilities.Definition != true {
		t.Error("expected Definition capability")
	}
}

func TestGoSessionAdapterInlayHintsWithGopls(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	content := "package main\n\nfunc main() {\n\tx := 1\n\t_ = x\n}\n"
	dir, file := setupGoTestModule(t, content)

	pool := NewGoSessionPool(5 * time.Minute)
	defer pool.Close()

	result, err := pool.Run(ToolInput{
		Method:        "inlay_hints",
		Content:       content,
		FilePath:      file,
		WorkspaceRoot: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// InlayHints capability should be set
	if !result.Capabilities.InlayHints {
		t.Error("expected InlayHints capability to be true")
	}
}

func TestGoAdapterRunDiagnosticsNilPosition(t *testing.T) {
	// Ensure diagnostics works with nil position (it doesn't use position, but good to verify)
	if _, err := exec.LookPath("gofmt"); err != nil {
		t.Skip("gofmt not available")
	}

	adapter := NewGoAdapter()
	result, err := adapter.Run(ToolInput{
		Method:   "diagnostics",
		Content:  "package main\n\nfunc main() {\n\tfmt.Println(\n}\n",
		FilePath: "main.go",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Diagnostics) == 0 {
		t.Error("expected diagnostics for invalid code")
	}
}

func TestRunGoDiagnosticsDirectly(t *testing.T) {
	if _, err := exec.LookPath("gofmt"); err != nil {
		t.Skip("gofmt not available")
	}

	// Test with empty workspace root
	result, err := runGoDiagnostics(ToolInput{
		Content:  "package main\n\nfunc main() {\n\tfmt.Println(\n}\n",
		FilePath: "broken.go",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Diagnostics) == 0 {
		t.Error("expected diagnostics for broken Go code")
	}
	if !result.Capabilities.Diagnostics {
		t.Error("expected Diagnostics capability")
	}
}

func TestRunGoDefinitionDirectly(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	dir, file := setupGoTestModule(t, "package main\n\nfunc main() {}\n")

	result, err := runGoDefinition(ToolInput{
		Content:       "package main\n\nfunc main() {}\n",
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      &Position{Line: 3, Column: 1},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Capabilities.Definition {
		t.Error("expected Definition capability")
	}
}

func TestRunGoHoverDirectly(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	dir, file := setupGoTestModule(t, "package main\n\nfunc main() {}\n")

	result, err := runGoHover(ToolInput{
		Content:       "package main\n\nfunc main() {}\n",
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      nil, // should default to 1:1
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With nil position, defaults to Line 1, Column 1
	if result.Error != "" {
		t.Errorf("unexpected error: %q", result.Error)
	}
	if !result.Capabilities.Hover {
		t.Error("expected Hover capability")
	}
}

func TestRunGoRenameDirectly(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	content := "package main\n\nfunc main() { println() }\n"
	dir, file := setupGoTestModule(t, content)

	result, err := runGoRename(ToolInput{
		Content:       content,
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      nil, // should default to 1:1
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Rename result must not be nil
	if result.Rename == nil {
		t.Error("expected non-nil Rename result")
	}
}

func TestRunGoReferencesDirectly(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	content := "package main\n\nfunc main() { println() }\n"
	dir, file := setupGoTestModule(t, content)

	result, err := runGoReferences(ToolInput{
		Content:       content,
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      nil, // should default to 1:1
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// References result must not be nil
	if result.References == nil {
		t.Error("expected non-nil References result")
	}
}

func TestRunGoCodeActionsDirectly(t *testing.T) {
	if _, err := exec.LookPath("goimports"); err != nil {
		t.Skip("goimports not available")
	}

	content := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"
	_, file := setupGoTestModule(t, content)

	result, err := runGoCodeActions(ToolInput{
		Content:  content,
		FilePath: file,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Capabilities.CodeActions {
		t.Error("expected CodeActions capability")
	}
}

func TestRunGoSignatureHelpDirectly(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	content := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\n}\n"
	dir, file := setupGoTestModule(t, content)

	result, err := runGoSignatureHelp(ToolInput{
		Content:       content,
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      &Position{Line: 6, Column: 14},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SignatureHelp == nil {
		t.Error("expected non-nil SignatureHelp")
	}
	if !result.Capabilities.SignatureHelp {
		t.Error("expected SignatureHelp capability")
	}
}

// Test writing to and reading from a real gopls unix socket requires the full
// gopls lifecycle which the session adapter handles. Test the session adapter's
// lifecycle methods directly.

func TestGoSessionAdapterEnsureServer(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	a := &goSessionAdapter{}

	// Before any call, Healthy should be true (no server but not closed)
	if !a.Healthy() {
		t.Error("fresh adapter should be healthy")
	}

	a.mu.Lock()
	err := a.ensureServerLocked(t.TempDir())
	a.mu.Unlock()
	if err != nil {
		t.Fatalf("ensureServerLocked failed: %v", err)
	}

	// Now server should be running
	if !a.Healthy() {
		t.Error("adapter with server should be healthy")
	}

	// Close handles cleanup (takes its own lock)
	a.Close()
}

func TestGoSessionAdapterResetServer(t *testing.T) {
	a := &goSessionAdapter{}
	// reset on clean state should not panic (lock must be held)
	a.mu.Lock()
	a.resetServerLocked()
	a.mu.Unlock()
}

// Test TypeScript session adapter's ensure worker path

func TestTypeScriptSessionAdapterResetWorker(t *testing.T) {
	a := &typeScriptSessionAdapter{}
	// reset on clean state should not panic (lock must be held)
	a.mu.Lock()
	a.resetWorkerLocked()
	a.mu.Unlock()
}

func TestParseGoplsDefinitionEmptyOutput(t *testing.T) {
	_, _, _, ok := parseGoplsDefinition("")
	if ok {
		t.Error("expected false for empty output")
	}
}
