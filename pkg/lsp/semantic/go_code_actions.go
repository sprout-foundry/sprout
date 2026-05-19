package semantic

import (
	"os"
	"os/exec"
	"path/filepath"
)

// runGoCodeActions provides code actions for the current file using goimports.
func runGoCodeActions(input ToolInput) (ToolResult, error) {
	caps := Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true}

	// Check if goimports is available
	goimportsPath, err := exec.LookPath("goimports")
	if err != nil {
		return ToolResult{
			Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: false},
			Error:        "goimports_not_available",
		}, nil
	}

	// Write content to a temp file for goimports to process
	tmpDir, err := os.MkdirTemp("", "sprout-go-codeaction-*")
	if err != nil {
		return ToolResult{}, err
	}
	defer os.RemoveAll(tmpDir)

	baseName := filepath.Base(input.FilePath)
	if baseName == "" || baseName == "." {
		baseName = "main.go"
	}
	tmpFile := filepath.Join(tmpDir, baseName)

	if err := os.WriteFile(tmpFile, []byte(input.Content), 0600); err != nil {
		return ToolResult{}, err
	}

	// Run goimports to get formatted output with organized imports
	formatted, err := exec.Command(goimportsPath, tmpFile).Output()
	if err != nil {
		// goimports can fail on syntax errors; return no actions
		return ToolResult{Capabilities: caps, CodeActions: nil}, nil
	}

	formattedStr := string(formatted)
	if formattedStr == input.Content {
		return ToolResult{Capabilities: caps, CodeActions: nil}, nil
	}

	// Compute minimal edits between original and formatted
	edits := computeGoEdits(input.Content, formattedStr, input.FilePath)
	if len(edits) == 0 {
		return ToolResult{Capabilities: caps, CodeActions: nil}, nil
	}

	actions := []ToolCodeAction{
		{
			Title: "Organize Imports",
			Kind:  "source.organizeImports",
			Edits: edits,
		},
	}

	return ToolResult{Capabilities: caps, CodeActions: actions}, nil
}

// computeGoEdits produces a list of edits by comparing original and new text.
func computeGoEdits(original, modified, filePath string) []ToolCodeActionEdit {
	// Find common prefix
	prefixLen := 0
	for prefixLen < len(original) && prefixLen < len(modified) && original[prefixLen] == modified[prefixLen] {
		prefixLen++
	}

	// Find common suffix
	origSuffix := len(original)
	modSuffix := len(modified)
	for origSuffix > prefixLen && modSuffix > prefixLen && original[origSuffix-1] == modified[modSuffix-1] {
		origSuffix--
		modSuffix--
	}

	if prefixLen == origSuffix && prefixLen == modSuffix {
		return nil // no changes
	}

	return []ToolCodeActionEdit{
		{
			FilePath: filePath,
			From:     prefixLen,
			To:       origSuffix,
			NewText:  modified[prefixLen:modSuffix],
		},
	}
}
