package semantic

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// pythonAdapter implements semantic analysis for Python via ruff.
type pythonAdapter struct{}

// NewPythonAdapter constructs a Python semantic adapter.
func NewPythonAdapter() Adapter {
	return pythonAdapter{}
}

func (a pythonAdapter) Run(input ToolInput) (ToolResult, error) {
	switch input.Method {
	case "diagnostics":
		return runPythonDiagnostics(input)
	case "hover":
		return ToolResult{
			Capabilities: fullCaps,
			Error:        "hover requires LSP server (e.g., pylsp)",
		}, nil
	case "definition":
		return ToolResult{
			Capabilities: fullCaps,
			Error:        "definition requires LSP server (e.g., pylsp)",
		}, nil
	case "references":
		return ToolResult{
			Capabilities: fullCaps,
			Error:        "references requires LSP server (e.g., pylsp)",
		}, nil
	case "rename":
		return ToolResult{
			Capabilities: fullCaps,
			Error:        "rename requires LSP server (e.g., pylsp)",
		}, nil
	case "code_actions":
		return ToolResult{
			Capabilities: fullCaps,
			Error:        "code_actions requires LSP server (e.g., pylsp)",
		}, nil
	case "inlay_hints":
		return ToolResult{
			Capabilities: fullCaps,
			Error:        "inlay_hints requires LSP server (e.g., pylsp)",
		}, nil
	case "signature_help":
		return ToolResult{
			Capabilities: fullCaps,
			Error:        "signature_help requires LSP server (e.g., pylsp)",
		}, nil
	default:
		return ToolResult{Capabilities: fullCaps}, nil
	}
}

// ruffDiagnostic represents a single diagnostic from ruff check --output-format=json.
type ruffDiagnostic struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Location struct {
		Column int `json:"column"`
		Row    int `json:"row"`
	} `json:"location"`
	EndLocation struct {
		Column int `json:"column"`
		Row    int `json:"row"`
	} `json:"end_location"`
	Filename string `json:"filename"`
}

func runPythonDiagnostics(input ToolInput) (ToolResult, error) {
	caps := fullCaps

	if _, err := exec.LookPath("ruff"); err != nil {
		return ToolResult{Capabilities: caps}, nil
	}

	tmpDir, err := os.MkdirTemp("", "sprout-python-diag-*")
	if err != nil {
		return ToolResult{}, err
	}
	defer os.RemoveAll(tmpDir)

	baseName := filepath.Base(input.FilePath)
	if baseName == "" || baseName == "." {
		baseName = "main.py"
	}
	tmpFile := filepath.Join(tmpDir, baseName)

	if err := os.WriteFile(tmpFile, []byte(input.Content), 0600); err != nil {
		return ToolResult{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ruff", "check", "--output-format=json", tmpFile)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Dir = tmpDir

	// ruff check exits non-zero when issues found, ignore the exit code
	_ = cmd.Run()

	var ruffDiags []ruffDiagnostic
	if err := json.Unmarshal(stdout.Bytes(), &ruffDiags); err != nil {
		// If output is not valid JSON, return no diagnostics
		return ToolResult{Capabilities: caps}, nil
	}

	var diagnostics []ToolDiagnostic
	for _, rd := range ruffDiags {
		severity := ruffSeverity(rd.Code)
		// ruff uses 0-based columns, so pass them directly to LineColToOffset
		startOff := LineColToOffset(input.Content, rd.Location.Row, rd.Location.Column)
		endOff := LineColToOffset(input.Content, rd.EndLocation.Row, rd.EndLocation.Column)
		if endOff <= startOff {
			endOff = startOff + 1
		}
		diagnostics = append(diagnostics, ToolDiagnostic{
			From:     startOff,
			To:       endOff,
			Severity: severity,
			Message:  rd.Message,
			Source:   "ruff",
		})
	}

	return ToolResult{Capabilities: caps, Diagnostics: diagnostics}, nil
}

func ruffSeverity(code string) string {
	if len(code) == 0 {
		return "info"
	}
	switch code[0] {
	case 'E', 'F':
		return "error"
	case 'W':
		return "warning"
	default:
		return "info"
	}
}
