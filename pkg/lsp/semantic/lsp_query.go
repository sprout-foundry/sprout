package semantic

import (
	"context"
	"os/exec"
)

// fullCaps is the default capability set for adapters that support diagnostics
// via external tools and have LSP server support for other features.
var fullCaps = Capabilities{
	Diagnostics:   true,
	Definition:    true,
	Hover:         true,
	Rename:        true,
	References:    true,
	CodeActions:   true,
	InlayHints:    true,
	SignatureHelp: true,
}

// LineColToOffset converts a 1-based line and a 0-based column to a byte offset
// in the given content string. This is language-agnostic and shared across adapters.
func LineColToOffset(content string, line, col int) int {
	if line < 1 {
		return 0
	}
	offset := 0
	currentLine := 1
	for i := 0; i < len(content); i++ {
		if currentLine == line {
			if col < 0 {
				col = 0
			}
			result := offset + col
			if result > len(content) {
				result = len(content)
			}
			return result
		}
		if content[i] == '\n' {
			currentLine++
			offset = i + 1
		}
	}
	if currentLine == line {
		if col < 0 {
			col = 0
		}
		result := offset + col
		if result > len(content) {
			result = len(content)
		}
		return result
	}
	return len(content)
}

// LSPQueryHelper provides common functionality for LSP-based semantic adapters.
// It handles the boilerplate of connecting to the LSP proxy, sending requests,
// and parsing responses.
type LSPQueryHelper struct {
	languageID string
	binaryName string
}

// NewLSPQueryHelper creates a new helper for the given language and binary.
func NewLSPQueryHelper(languageID, binaryName string) *LSPQueryHelper {
	return &LSPQueryHelper{languageID: languageID, binaryName: binaryName}
}

// BinaryAvailable checks whether the configured binary is on PATH.
func (h *LSPQueryHelper) BinaryAvailable() bool {
	_, err := exec.LookPath(h.binaryName)
	return err == nil
}

// Capabilities returns the full set of features this helper can support.
func (h *LSPQueryHelper) Capabilities() Capabilities {
	return fullCaps
}

// RunDiagnosticsViaLSP is a placeholder for future LSP-based diagnostics.
func (h *LSPQueryHelper) RunDiagnosticsViaLSP(_ context.Context, _ ToolInput) ([]ToolDiagnostic, error) {
	return nil, nil
}

// RunHoverViaLSP sends a textDocument/hover request (placeholder).
func (h *LSPQueryHelper) RunHoverViaLSP(_ context.Context, _ ToolInput) (*ToolHover, error) {
	return nil, nil
}

// RunDefinitionViaLSP sends a textDocument/definition request (placeholder).
func (h *LSPQueryHelper) RunDefinitionViaLSP(_ context.Context, _ ToolInput) (*ToolDefinition, error) {
	return nil, nil
}

// RunReferencesViaLSP sends a textDocument/references request (placeholder).
func (h *LSPQueryHelper) RunReferencesViaLSP(_ context.Context, _ ToolInput) ([]ToolReferenceLocation, error) {
	return nil, nil
}
