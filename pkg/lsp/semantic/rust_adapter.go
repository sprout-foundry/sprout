package semantic

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// rustAdapter implements semantic analysis for Rust via cargo check.
type rustAdapter struct{}

// NewRustAdapter constructs a Rust semantic adapter.
func NewRustAdapter() Adapter {
	return rustAdapter{}
}

func (a rustAdapter) Run(input ToolInput) (ToolResult, error) {
	switch input.Method {
	case "diagnostics":
		return runRustDiagnostics(input)
	case "hover":
		return ToolResult{
			Capabilities: fullCaps,
			Error:        "hover requires rust-analyzer",
		}, nil
	case "definition":
		return ToolResult{
			Capabilities: fullCaps,
			Error:        "definition requires rust-analyzer",
		}, nil
	case "references":
		return ToolResult{
			Capabilities: fullCaps,
			Error:        "references requires rust-analyzer",
		}, nil
	case "rename":
		return ToolResult{
			Capabilities: fullCaps,
			Error:        "rename requires rust-analyzer",
		}, nil
	case "code_actions":
		return ToolResult{
			Capabilities: fullCaps,
			Error:        "code_actions requires rust-analyzer",
		}, nil
	case "inlay_hints":
		return ToolResult{
			Capabilities: fullCaps,
			Error:        "inlay_hints requires rust-analyzer",
		}, nil
	case "signature_help":
		return ToolResult{
			Capabilities: fullCaps,
			Error:        "signature_help requires rust-analyzer",
		}, nil
	default:
		return ToolResult{Capabilities: fullCaps}, nil
	}
}

// cargoMessage represents a line-delimited JSON message from cargo check --message-format=json.
type cargoMessage struct {
	Reason  string `json:"reason"`
	Message struct {
		Code    *struct{ Code string `json:"code"` } `json:"code"`
		Level   string `json:"level"`
		Message string `json:"message"`
		Spans   []struct {
			FileName    string `json:"file_name"`
			LineStart   int    `json:"line_start"`
			LineEnd     int    `json:"line_end"`
			ColumnStart int    `json:"column_start"`
			ColumnEnd   int    `json:"column_end"`
			IsPrimary   bool   `json:"is_primary"`
		} `json:"spans"`
	} `json:"message"`
}

func runRustDiagnostics(input ToolInput) (ToolResult, error) {
	caps := fullCaps

	if _, err := exec.LookPath("cargo"); err != nil {
		return ToolResult{Capabilities: caps}, nil
	}

	tmpDir, err := os.MkdirTemp("", "sprout-rust-diag-*")
	if err != nil {
		return ToolResult{}, err
	}
	defer os.RemoveAll(tmpDir)

	// Create a minimal Cargo project structure.
	cargoToml := "[package]\nname = \"sprout_temp\"\nversion = \"0.1.0\"\nedition = \"2021\"\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "Cargo.toml"), []byte(cargoToml), 0600); err != nil {
		return ToolResult{}, err
	}
	srcDir := filepath.Join(tmpDir, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		return ToolResult{}, err
	}

	baseName := filepath.Base(input.FilePath)
	if baseName == "" || baseName == "." || !strings.HasSuffix(baseName, ".rs") {
		baseName = "main.rs"
	}
	tmpFile := filepath.Join(srcDir, baseName)

	if err := os.WriteFile(tmpFile, []byte(input.Content), 0600); err != nil {
		return ToolResult{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "cargo", "check", "--message-format=json")
	cmd.Dir = tmpDir
	var stdout strings.Builder
	cmd.Stdout = &stdout
	_ = cmd.Run()

	var diagnostics []ToolDiagnostic
	scanner := bufio.NewScanner(strings.NewReader(stdout.String()))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		var msg cargoMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if msg.Reason != "compiler-message" {
			continue
		}
		severity := "info"
		switch msg.Message.Level {
		case "error":
			severity = "error"
		case "warning":
			severity = "warning"
		}
		code := ""
		if msg.Message.Code != nil {
			code = msg.Message.Code.Code
		}

		if len(msg.Message.Spans) == 0 {
			// No span info — emit a file-level diagnostic.
			diagnostics = append(diagnostics, ToolDiagnostic{
				From:     0,
				To:       0,
				Severity: severity,
				Message:  msg.Message.Message,
				Source:   "cargo",
			})
			continue
		}

		for _, span := range msg.Message.Spans {
			if !span.IsPrimary {
				continue
			}
			startOff := LineColToOffset(input.Content, span.LineStart, span.ColumnStart-1)
			endOff := LineColToOffset(input.Content, span.LineEnd, span.ColumnEnd-1)
			if endOff <= startOff {
				endOff = startOff + 1
			}
			msgText := msg.Message.Message
			if code != "" {
				msgText = code + ": " + msgText
			}
			diagnostics = append(diagnostics, ToolDiagnostic{
				From:     startOff,
				To:       endOff,
				Severity: severity,
				Message:  msgText,
				Source:   "cargo",
			})
		}
	}

	return ToolResult{Capabilities: caps, Diagnostics: diagnostics}, nil
}
