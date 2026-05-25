package semantic

import (
	"context"
	"os/exec"
	"testing"
)

// --- LSPQueryHelper Constructor ---

func TestNewLSPQueryHelper_SetsFields(t *testing.T) {
	h := NewLSPQueryHelper("python", "ruff")
	if h.languageID != "python" {
		t.Errorf("expected languageID = 'python', got %q", h.languageID)
	}
	if h.binaryName != "ruff" {
		t.Errorf("expected binaryName = 'ruff', got %q", h.binaryName)
	}
}

// --- BinaryAvailable ---

func TestBinaryAvailable_ForExistingTool(t *testing.T) {
	// "go" should be available on any system building Go code.
	h := NewLSPQueryHelper("go", "go")
	if !h.BinaryAvailable() {
		t.Error("expected 'go' binary to be available on PATH")
	}
}

func TestBinaryAvailable_ForNonExistentTool(t *testing.T) {
	h := NewLSPQueryHelper("unknown", "definitely_not_a_real_binary_xyz")
	if h.BinaryAvailable() {
		t.Error("expected non-existent binary to not be available")
	}
}

// Cross-check: the helper's BinaryAvailable should agree with exec.LookPath
func TestBinaryAvailable_AgreesWithLookPath(t *testing.T) {
	h := NewLSPQueryHelper("go", "go")
	_, lookPathErr := exec.LookPath("go")
	if h.BinaryAvailable() != (lookPathErr == nil) {
		t.Errorf("BinaryAvailable() = %v, but exec.LookPath says %v", h.BinaryAvailable(), lookPathErr == nil)
	}
}

// --- Capabilities ---

func TestCapabilities_ReturnsAllTrue(t *testing.T) {
	h := NewLSPQueryHelper("python", "ruff")
	caps := h.Capabilities()

	if !caps.Diagnostics {
		t.Error("expected Diagnostics to be true")
	}
	if !caps.Definition {
		t.Error("expected Definition to be true")
	}
	if !caps.Hover {
		t.Error("expected Hover to be true")
	}
	if !caps.Rename {
		t.Error("expected Rename to be true")
	}
	if !caps.References {
		t.Error("expected References to be true")
	}
	if !caps.CodeActions {
		t.Error("expected CodeActions to be true")
	}
	if !caps.InlayHints {
		t.Error("expected InlayHints to be true")
	}
	if !caps.SignatureHelp {
		t.Error("expected SignatureHelp to be true")
	}
}

// --- Placeholder LSP Methods ---

func TestRunDiagnosticsViaLSP_ReturnsNil(t *testing.T) {
	h := NewLSPQueryHelper("python", "ruff")
	diags, err := h.RunDiagnosticsViaLSP(context.Background(), ToolInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diags != nil {
		t.Errorf("expected nil diagnostics, got %v", diags)
	}
}

func TestRunHoverViaLSP_ReturnsNil(t *testing.T) {
	h := NewLSPQueryHelper("python", "ruff")
	hover, err := h.RunHoverViaLSP(context.Background(), ToolInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hover != nil {
		t.Errorf("expected nil hover, got %v", hover)
	}
}

func TestRunDefinitionViaLSP_ReturnsNil(t *testing.T) {
	h := NewLSPQueryHelper("python", "ruff")
	def, err := h.RunDefinitionViaLSP(context.Background(), ToolInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if def != nil {
		t.Errorf("expected nil definition, got %v", def)
	}
}

func TestRunReferencesViaLSP_ReturnsNil(t *testing.T) {
	h := NewLSPQueryHelper("python", "ruff")
	refs, err := h.RunReferencesViaLSP(context.Background(), ToolInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if refs != nil {
		t.Errorf("expected nil references, got %v", refs)
	}
}
