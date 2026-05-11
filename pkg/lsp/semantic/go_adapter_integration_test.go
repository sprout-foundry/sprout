package semantic

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// These tests exercise the real gopls/goimports code paths when available.
// They create temp Go modules to avoid polluting the main repo.

func setupGoTestModule(t *testing.T, content string) (dir string, file string) {
	t.Helper()
	dir = t.TempDir()
	file = filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	goMod := "module test_main\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0600); err != nil {
		t.Fatal(err)
	}
	return dir, file
}

func TestGoAdapterRunDefinitionWithGopls(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	dir, file := setupGoTestModule(t, "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n")

	adapter := NewGoAdapter()
	result, err := adapter.Run(ToolInput{
		Method:        "definition",
		Content:       "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n",
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      &Position{Line: 6, Column: 6}, // inside Println
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// gopls should either find a definition or return no result; it should not error
	if result.Error != "" {
		t.Errorf("unexpected result error: %q", result.Error)
	}
}

func TestGoAdapterRunHoverWithGopls(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	dir, file := setupGoTestModule(t, "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n")

	adapter := NewGoAdapter()
	result, err := adapter.Run(ToolInput{
		Method:        "hover",
		Content:       "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n",
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      &Position{Line: 6, Column: 6},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Hover should succeed (no error) even if no hover info at this position
	if result.Error != "" {
		t.Errorf("unexpected result error: %q", result.Error)
	}
}

func TestGoAdapterRunReferencesWithGopls(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	dir, file := setupGoTestModule(t, "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n")

	adapter := NewGoAdapter()
	result, err := adapter.Run(ToolInput{
		Method:        "references",
		Content:       "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n",
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      &Position{Line: 6, Column: 6},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// References result should not be nil (empty Locations is acceptable)
	if result.References == nil {
		t.Error("expected non-nil References result")
	}
}

func TestGoAdapterRunRenameWithGopls(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	content := "package main\n\nfunc greet() string { return \"hi\" }\n\nfunc main() {\n\tprintln(greet())\n}\n"
	dir, file := setupGoTestModule(t, content)

	adapter := NewGoAdapter()
	result, err := adapter.Run(ToolInput{
		Method:        "rename",
		Content:       content,
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      &Position{Line: 3, Column: 6}, // on 'greet'
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Rename result should not be nil (empty Locations is acceptable)
	if result.Rename == nil {
		t.Error("expected non-nil Rename result")
	}
}

func TestGoAdapterRunCodeActionsWithGoimports(t *testing.T) {
	if _, err := exec.LookPath("goimports"); err != nil {
		t.Skip("goimports not available")
	}

	// Code with unused import - goimports should suggest organizing imports
	content := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"
	_, file := setupGoTestModule(t, content)

	adapter := NewGoAdapter()
	result, err := adapter.Run(ToolInput{
		Method:   "code_actions",
		Content:  content,
		FilePath: file,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Capabilities.CodeActions {
		t.Fatal("expected CodeActions capability")
	}
	if len(result.CodeActions) == 0 {
		t.Fatal("expected at least one code action for unused import")
	}
	foundOrganizeImports := false
	for _, a := range result.CodeActions {
		if a.Kind == "source.organizeImports" {
			foundOrganizeImports = true
		}
	}
	if !foundOrganizeImports {
		t.Error("expected 'source.organizeImports' action")
	}
}

func TestGoAdapterRunCodeActionsNoChanges(t *testing.T) {
	if _, err := exec.LookPath("goimports"); err != nil {
		t.Skip("goimports not available")
	}

	content := "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"
	_, file := setupGoTestModule(t, content)

	adapter := NewGoAdapter()
	result, err := adapter.Run(ToolInput{
		Method:   "code_actions",
		Content:  content,
		FilePath: file,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Capabilities.CodeActions {
		t.Error("expected CodeActions capability")
	}
	if len(result.CodeActions) != 0 {
		t.Errorf("expected no code actions for clean code, got %d", len(result.CodeActions))
	}
}

func TestGoAdapterRunSignatureHelpWithGopls(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	content := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\n}\n"
	dir, file := setupGoTestModule(t, content)

	adapter := NewGoAdapter()
	result, err := adapter.Run(ToolInput{
		Method:        "signature_help",
		Content:       content,
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      &Position{Line: 6, Column: 14}, // inside Println(
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Signature help should be populated (even if empty signatures)
	if result.SignatureHelp == nil {
		t.Error("expected non-nil SignatureHelp")
	}
}

func TestGoAdapterDiagnosticsWithSaveTrigger(t *testing.T) {
	if _, err := exec.LookPath("gofmt"); err != nil {
		t.Skip("gofmt not available")
	}

	// On save trigger with valid code, it runs go vet too
	content := "package main\n\nfunc main() {}\n"
	_, file := setupGoTestModule(t, content)

	adapter := NewGoAdapter()
	result, err := adapter.Run(ToolInput{
		Method:   "diagnostics",
		Content:  content,
		FilePath: file,
		Trigger:  "save",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Capabilities.Diagnostics {
		t.Error("expected Diagnostics capability")
	}
}

func TestGoAdapterDiagnosticsEmptyFilePath(t *testing.T) {
	if _, err := exec.LookPath("gofmt"); err != nil {
		t.Skip("gofmt not available")
	}

	adapter := NewGoAdapter()
	result, err := adapter.Run(ToolInput{
		Method:  "diagnostics",
		Content: "package main\n\nfunc main() {\n\tfmt.Println(\n}\n",
		// FilePath is empty - should default to "main.go"
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Diagnostics) == 0 {
		t.Error("expected diagnostics for invalid code with empty FilePath")
	}
}
