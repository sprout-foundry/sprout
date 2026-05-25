package semantic

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// cppAdapter implements semantic analysis for C/C++ via clang-tidy.
type cppAdapter struct{}

// NewCppAdapter constructs a C/C++ semantic adapter.
func NewCppAdapter() Adapter {
	return cppAdapter{}
}

func (a cppAdapter) Run(input ToolInput) (ToolResult, error) {
	switch input.Method {
	case "diagnostics":
		return runCppDiagnostics(input)
	case "hover":
		return ToolResult{
			Capabilities: fullCaps,
			Error:        "hover requires clangd",
		}, nil
	case "definition":
		return ToolResult{
			Capabilities: fullCaps,
			Error:        "definition requires clangd",
		}, nil
	case "references":
		return ToolResult{
			Capabilities: fullCaps,
			Error:        "references requires clangd",
		}, nil
	case "rename":
		return ToolResult{
			Capabilities: fullCaps,
			Error:        "rename requires clangd",
		}, nil
	case "code_actions":
		return ToolResult{
			Capabilities: fullCaps,
			Error:        "code_actions requires clangd",
		}, nil
	case "inlay_hints":
		return ToolResult{
			Capabilities: fullCaps,
			Error:        "inlay_hints requires clangd",
		}, nil
	case "signature_help":
		return ToolResult{
			Capabilities: fullCaps,
			Error:        "signature_help requires clangd",
		}, nil
	default:
		return ToolResult{Capabilities: fullCaps}, nil
	}
}

// clangTidyPattern matches clang-tidy diagnostic output lines like:
// /path/to/file.cpp:10:5: warning: unused variable 'x' [misc-unused-parameters]
// /path/to/file.cpp:10:5: error: use of undeclared identifier 'y' [clang-diagnostic-error]
var clangTidyPattern = regexp.MustCompile(`^.+?:(\d+):(\d+):\s*(warning|error|note):\s*(.+?)(?:\s*\[([^\]]+)\])?$`)

func runCppDiagnostics(input ToolInput) (ToolResult, error) {
	caps := fullCaps

	if _, err := exec.LookPath("clang-tidy"); err != nil {
		return ToolResult{Capabilities: caps}, nil
	}

	tmpDir, err := os.MkdirTemp("", "sprout-cpp-diag-*")
	if err != nil {
		return ToolResult{}, err
	}
	defer os.RemoveAll(tmpDir)

	baseName := filepath.Base(input.FilePath)
	if baseName == "" || baseName == "." {
		baseName = "main.cpp"
	}
	tmpFile := filepath.Join(tmpDir, baseName)

	if err := os.WriteFile(tmpFile, []byte(input.Content), 0600); err != nil {
		return ToolResult{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "clang-tidy", tmpFile, "--export-fixes=-")
	cmd.Dir = tmpDir
	var stderr bytes.Buffer
	cmd.Stdout = nil // discard stdout (YAML fixes output)
	cmd.Stderr = &stderr
	_ = cmd.Run()

	var diagnostics []ToolDiagnostic
	scanner := bufio.NewScanner(strings.NewReader(stderr.String()))
	for scanner.Scan() {
		line := scanner.Text()
		matches := clangTidyPattern.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		lineNum := 0
		colNum := 0
		// Parse line number
		for _, ch := range matches[1] {
			lineNum = lineNum*10 + int(ch-'0')
		}
		// Parse column number
		for _, ch := range matches[2] {
			colNum = colNum*10 + int(ch-'0')
		}

		level := matches[3]
		message := matches[4]
		// checkName := matches[5] // available if needed

		severity := "info"
		switch level {
		case "error":
			severity = "error"
		case "warning":
			severity = "warning"
		}

		startOff := LineColToOffset(input.Content, lineNum, colNum-1)
		endOff := startOff + 1
		if endOff > len(input.Content) {
			endOff = len(input.Content)
		}

		diagnostics = append(diagnostics, ToolDiagnostic{
			From:     startOff,
			To:       endOff,
			Severity: severity,
			Message:  message,
			Source:   "clang-tidy",
		})
	}

	return ToolResult{Capabilities: caps, Diagnostics: diagnostics}, nil
}
