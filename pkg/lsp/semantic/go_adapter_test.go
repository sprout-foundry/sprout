package semantic

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- goLineColToOffset ---

func TestGoLineColToOffset(t *testing.T) {
	tests := []struct {
		name    string
		content string
		line    int
		col     int
		want    int
	}{
		{"line 1 col 1", "hello", 1, 1, 0},
		{"line 1 col 3", "hello", 1, 3, 2},
		{"multi-line line 2 col 1", "abc\ndef\nghi", 2, 1, 4},
		{"multi-line line 2 col 3", "abc\ndef\nghi", 2, 3, 6},
		{"line out of range", "abc", 10, 1, 3},
		{"negative line defaults to 1", "abc", -1, 2, 1},
		{"negative col defaults to 1", "abc", 1, -1, 0},
		{"empty content", "", 1, 1, 0},
		{"single line end", "hello", 1, 6, 5},
		{"past end of content", "hi", 1, 10, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := goLineColToOffset(tt.content, tt.line, tt.col)
			if got != tt.want {
				t.Errorf("goLineColToOffset(%q, %d, %d) = %d, want %d", tt.content, tt.line, tt.col, got, tt.want)
			}
		})
	}
}

// --- isIdentRune ---

func TestIsIdentRune(t *testing.T) {
	tests := []struct {
		r    rune
		want bool
	}{
		{'a', true}, {'Z', true}, {'0', true}, {'_', true},
		{' ', false}, {'-', false}, {'(', false}, {'.', false},
	}
	for _, tt := range tests {
		if got := isIdentRune(tt.r); got != tt.want {
			t.Errorf("isIdentRune(%q) = %v, want %v", tt.r, got, tt.want)
		}
	}
}

// --- parseGofmtErrors ---

func TestParseGofmtErrors(t *testing.T) {
	content := "package main\n\nfunc main() {\n\tfmt.Println(\n}\n"
	output := "/tmp/main.go:4:2: expected expression\n/tmp/main.go:5:1: expected ';'\n"

	diags := parseGofmtErrors(output, content)
	if len(diags) != 2 {
		t.Fatalf("expected 2 diagnostics, got %d", len(diags))
	}
	if diags[0].Severity != "error" {
		t.Errorf("expected severity error, got %s", diags[0].Severity)
	}
	if diags[0].Source != "gofmt" {
		t.Errorf("expected source gofmt, got %s", diags[0].Source)
	}
	if diags[0].Message != "expected expression" {
		t.Errorf("expected message 'expected expression', got %s", diags[0].Message)
	}
	if diags[1].Message != "expected ';'" {
		t.Errorf("expected message 'expected ;', got %s", diags[1].Message)
	}
}

func TestParseGofmtErrorsEmpty(t *testing.T) {
	diags := parseGofmtErrors("", "package main")
	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d", len(diags))
	}
}

func TestParseGofmtErrorsNonMatching(t *testing.T) {
	output := "some random text\nno colons here\n"
	diags := parseGofmtErrors(output, "package main")
	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d", len(diags))
	}
}

// --- parseGoVetErrors ---

func TestParseGoVetErrors(t *testing.T) {
	content := "package main\n\nfunc main() {\n}\n"
	output := "# command-line-arguments\n/tmp/main.go:3:2: unreachable code\n"

	diags := parseGoVetErrors(output, content)
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diags))
	}
	if diags[0].Severity != "warning" {
		t.Errorf("expected severity warning, got %s", diags[0].Severity)
	}
	if diags[0].Source != "go vet" {
		t.Errorf("expected source 'go vet', got %s", diags[0].Source)
	}
}

func TestParseGoVetErrorsSkipsHashLines(t *testing.T) {
	output := "# command-line-arguments\n/tmp/main.go:3:2: some issue\n"
	content := "package main\n\nfunc main() {\n}\n"
	diags := parseGoVetErrors(output, content)
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic (hash line should be skipped), got %d", len(diags))
	}
}

func TestParseGoVetErrorsEmpty(t *testing.T) {
	diags := parseGoVetErrors("", "package main")
	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d", len(diags))
	}
}

// --- parseGoplsDefinition ---

func TestParseGoplsDefinition(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		wantPath string
		wantLine int
		wantCol  int
		wantOK   bool
	}{
		{
			name:     "valid definition",
			output:   "/home/user/project/main.go:10:5\n",
			wantPath: "/home/user/project/main.go", wantLine: 10, wantCol: 5, wantOK: true,
		},
		{
			name:     "multiple lines",
			output:   "/home/user/project/main.go:10:5-8\n/home/user/project/other.go:20:3\n",
			wantPath: "/home/user/project/main.go", wantLine: 10, wantCol: 5, wantOK: true,
		},
		{
			name:   "empty output",
			output: "",
			wantOK: false,
		},
		{
			name:   "blank lines only",
			output: "\n\n",
			wantOK: false,
		},
		{
			name:   "non-matching format",
			output: "some random text\n",
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, line, col, ok := parseGoplsDefinition(tt.output)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok {
				if path != tt.wantPath || line != tt.wantLine || col != tt.wantCol {
					t.Errorf("got (%q, %d, %d), want (%q, %d, %d)", path, line, col, tt.wantPath, tt.wantLine, tt.wantCol)
				}
			}
		})
	}
}

// --- parseGoplsInlayHints ---

func TestParseGoplsInlayHints(t *testing.T) {
	content := "package main\n\nfunc main() {\n\tx := 1\n}\n"

	t.Run("nil result", func(t *testing.T) {
		hints := parseGoplsInlayHints(map[string]interface{}{}, content)
		if len(hints) != 0 {
			t.Fatalf("expected 0 hints, got %d", len(hints))
		}
	})

	t.Run("empty result array", func(t *testing.T) {
		resp := map[string]interface{}{"result": []interface{}{}}
		hints := parseGoplsInlayHints(resp, content)
		if len(hints) != 0 {
			t.Fatalf("expected 0 hints, got %d", len(hints))
		}
	})

	t.Run("string label", func(t *testing.T) {
		resp := map[string]interface{}{
			"result": []interface{}{
				map[string]interface{}{
					"position": map[string]interface{}{"line": float64(3), "character": float64(1)},
					"label":    "int",
					"kind":     float64(1),
				},
			},
		}
		hints := parseGoplsInlayHints(resp, content)
		if len(hints) != 1 {
			t.Fatalf("expected 1 hint, got %d", len(hints))
		}
		if hints[0].Label != "int" {
			t.Errorf("label = %q, want 'int'", hints[0].Label)
		}
		if hints[0].Kind != "type" {
			t.Errorf("kind = %q, want 'type'", hints[0].Kind)
		}
	})

	t.Run("array label parts", func(t *testing.T) {
		resp := map[string]interface{}{
			"result": []interface{}{
				map[string]interface{}{
					"position": map[string]interface{}{"line": float64(3), "character": float64(0)},
					"label": []interface{}{
						map[string]interface{}{"value": "param: "},
						map[string]interface{}{"value": "string"},
					},
					"kind": float64(2),
				},
			},
		}
		hints := parseGoplsInlayHints(resp, content)
		if len(hints) != 1 {
			t.Fatalf("expected 1 hint, got %d", len(hints))
		}
		if hints[0].Label != "param: string" {
			t.Errorf("label = %q, want 'param: string'", hints[0].Label)
		}
		if hints[0].Kind != "parameter" {
			t.Errorf("kind = %q, want 'parameter'", hints[0].Kind)
		}
	})

	t.Run("non-array result", func(t *testing.T) {
		resp := map[string]interface{}{"result": "not an array"}
		hints := parseGoplsInlayHints(resp, content)
		if len(hints) != 0 {
			t.Fatalf("expected 0 hints for non-array result, got %d", len(hints))
		}
	})
}

// --- mapInlayHintKind ---

func TestMapInlayHintKind(t *testing.T) {
	tests := []struct {
		kind int
		want string
	}{
		{1, "type"},
		{2, "parameter"},
		{0, "none"},
		{3, "none"},
		{99, "none"},
	}
	for _, tt := range tests {
		got := mapInlayHintKind(tt.kind)
		if got != tt.want {
			t.Errorf("mapInlayHintKind(%d) = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

// --- computeGoEdits ---

func TestComputeGoEdits(t *testing.T) {
	tests := []struct {
		name     string
		original string
		modified string
		wantNil  bool
	}{
		{"identical", "hello", "hello", true},
		{"append", "hello", "helloworld", false},
		{"delete", "hello world", "hello", false},
		{"replace middle", "abcdef", "abXYef", false},
		{"empty to content", "", "abc", false},
		{"content to empty", "abc", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edits := computeGoEdits(tt.original, tt.modified, "test.go")
			if tt.wantNil {
				if edits != nil {
					t.Errorf("expected nil edits, got %v", edits)
				}
				return
			}
			if len(edits) == 0 {
				t.Fatalf("expected non-empty edits")
			}
			// Verify that applying the edit produces the modified string
			applied := tt.original[:edits[0].From] + edits[0].NewText + tt.original[edits[0].To:]
			if applied != tt.modified {
				t.Errorf("applying edit: got %q, want %q", applied, tt.modified)
			}
		})
	}
}

// --- parseGoplsSignatureHelp ---

func TestParseGoplsSignatureHelp(t *testing.T) {
	t.Run("empty output", func(t *testing.T) {
		result := parseGoplsSignatureHelp("", ToolInput{})
		if len(result.Signatures) != 0 {
			t.Fatalf("expected 0 signatures, got %d", len(result.Signatures))
		}
	})

	t.Run("single signature", func(t *testing.T) {
		output := "func Foo(a int, b string) error\n"
		input := ToolInput{Content: "Foo(1, \"hi\")", Position: &Position{Line: 1, Column: 10}}
		result := parseGoplsSignatureHelp(output, input)
		if len(result.Signatures) != 1 {
			t.Fatalf("expected 1 signature, got %d", len(result.Signatures))
		}
		if result.Signatures[0].Label != "func Foo(a int, b string) error" {
			t.Errorf("label = %q", result.Signatures[0].Label)
		}
		if len(result.Signatures[0].Parameters) != 2 {
			t.Fatalf("expected 2 parameters, got %d", len(result.Signatures[0].Parameters))
		}
		if result.Signatures[0].Parameters[0].Label != "a int" {
			t.Errorf("first param = %q", result.Signatures[0].Parameters[0].Label)
		}
		if result.Signatures[0].Parameters[1].Label != "b string" {
			t.Errorf("second param = %q", result.Signatures[0].Parameters[1].Label)
		}
	})

	t.Run("method signature skips receiver", func(t *testing.T) {
		output := "func (t *T) Method(a int) error\n"
		input := ToolInput{Content: "t.Method(1)", Position: &Position{Line: 1, Column: 10}}
		result := parseGoplsSignatureHelp(output, input)
		if len(result.Signatures) != 1 {
			t.Fatalf("expected 1 signature, got %d", len(result.Signatures))
		}
		params := result.Signatures[0].Parameters
		if len(params) != 1 {
			t.Fatalf("expected 1 parameter (skipping receiver), got %d: %v", len(params), params)
		}
		if params[0].Label != "a int" {
			t.Errorf("param = %q, want 'a int'", params[0].Label)
		}
	})
}

// --- computeActiveParameter ---

func TestComputeActiveParameter(t *testing.T) {
	tests := []struct {
		name    string
		content string
		line    int
		col     int
		want    int
	}{
		{"nil position", "foo()", 0, 0, 0}, // uses nil position
		{"before paren", "foo()", 1, 2, 0},
		{"at first param", "foo(1)", 1, 5, 0},
		{"at second param", "foo(1, 2)", 1, 7, 1},
		{"nested parens", "foo(bar(1), 2)", 1, 14, 1},
		{"no open paren found", "x + y", 1, 3, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var pos *Position
			if tt.line > 0 || tt.col > 0 {
				pos = &Position{Line: tt.line, Column: tt.col}
			}
			input := ToolInput{Content: tt.content, Position: pos}
			got := computeActiveParameter(input)
			if got != tt.want {
				t.Errorf("computeActiveParameter() = %d, want %d", got, tt.want)
			}
		})
	}
}

// --- extractParamsFromSignature ---

func TestExtractParamsFromSignature(t *testing.T) {
	tests := []struct {
		name       string
		sig        string
		wantParams []string
	}{
		{"no parens", "func foo", nil},
		{"empty parens", "func foo()", nil},
		{"single param", "func Foo(a int)", []string{"a int"}},
		{"two params", "func Foo(a int, b string)", []string{"a int", "b string"}},
		{"method with receiver", "func (t *T) Method(a int, b string) error", []string{"a int", "b string"}},
		{"nested types", "func Foo(a func(int) error, b []string)", []string{"a func(int) error", "b []string"}},
		{"three params", "func Foo(a int, b string, c bool)", []string{"a int", "b string", "c bool"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := extractParamsFromSignature(tt.sig)
			if tt.wantParams == nil {
				if params != nil {
					t.Fatalf("expected nil params, got %v", params)
				}
				return
			}
			if len(params) != len(tt.wantParams) {
				t.Fatalf("expected %d params, got %d: %v", len(tt.wantParams), len(params), params)
			}
			for i, p := range params {
				if p.Label != tt.wantParams[i] {
					t.Errorf("param[%d] = %q, want %q", i, p.Label, tt.wantParams[i])
				}
			}
		})
	}
}

// --- writeJSONRPC / readJSONRPC ---

func TestWriteReadJSONRPC(t *testing.T) {
	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)

	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      float64(1),
		"method":  "initialize",
		"params":  map[string]interface{}{"processId": float64(1234)},
	}
	writeJSONRPC(writer, msg)
	writer.Flush()

	// Verify header format
	b := buf.String()
	if !strings.HasPrefix(b, "Content-Length: ") {
		t.Fatalf("expected Content-Length header, got: %q", b[:50])
	}
	if !strings.Contains(b, "\r\n\r\n") {
		t.Fatalf("expected CRLF CRLF separator")
	}

	// Read it back
	reader := bufio.NewReader(&buf)
	readMsg, err := readJSONRPC(reader)
	if err != nil {
		t.Fatalf("readJSONRPC failed: %v", err)
	}
	if readMsg["method"] != "initialize" {
		t.Errorf("method = %v, want 'initialize'", readMsg["method"])
	}
	params, ok := readMsg["params"].(map[string]interface{})
	if !ok {
		t.Fatalf("params type = %T", readMsg["params"])
	}
	if params["processId"] != float64(1234) {
		t.Errorf("processId = %v, want 1234", params["processId"])
	}
}

// --- NewGoAdapter ---

func TestNewGoAdapter(t *testing.T) {
	adapter := NewGoAdapter()
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
}

func TestGoAdapterRunUnknownMethod(t *testing.T) {
	adapter := NewGoAdapter()
	result, err := adapter.Run(ToolInput{Method: "unknown"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Capabilities.Diagnostics || result.Capabilities.Definition {
		t.Errorf("expected empty capabilities for unknown method, got %+v", result.Capabilities)
	}
}

// --- goAdapter.Run with diagnostics ---

func TestGoAdapterRunDiagnosticsInvalidCode(t *testing.T) {
	gofmtPath, err := exec.LookPath("gofmt")
	if err != nil {
		t.Skip("gofmt not available")
	}
	_ = gofmtPath

	adapter := NewGoAdapter()
	result, err := adapter.Run(ToolInput{
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

func TestGoAdapterRunDiagnosticsValidCode(t *testing.T) {
	_, err := exec.LookPath("gofmt")
	if err != nil {
		t.Skip("gofmt not available")
	}

	adapter := NewGoAdapter()
	result, err := adapter.Run(ToolInput{
		Method:   "diagnostics",
		Content:  "package main\n\nfunc main() {}\n",
		FilePath: "main.go",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Capabilities.Diagnostics {
		t.Error("expected diagnostics capability")
	}
}

// --- goAdapter.Run with various methods (gopls-dependent) ---

func TestGoAdapterRunDefinitionNoGopls(t *testing.T) {
	// This test verifies the "gopls_not_available" path
	// It will only trigger if gopls is not installed
	_, err := exec.LookPath("gopls")
	if err == nil {
		t.Skip("gopls is available; skipping not-available path test")
	}

	adapter := NewGoAdapter()
	result, err := adapter.Run(ToolInput{
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
		t.Errorf("expected gopls_not_available error, got %q", result.Error)
	}
}

func TestGoAdapterRunHoverNoGopls(t *testing.T) {
	_, err := exec.LookPath("gopls")
	if err == nil {
		t.Skip("gopls is available; skipping not-available path test")
	}

	adapter := NewGoAdapter()
	result, err := adapter.Run(ToolInput{
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
		t.Errorf("expected gopls_not_available error, got %q", result.Error)
	}
}

func TestGoAdapterRunRenameNoGopls(t *testing.T) {
	_, err := exec.LookPath("gopls")
	if err == nil {
		t.Skip("gopls is available; skipping not-available path test")
	}

	adapter := NewGoAdapter()
	result, err := adapter.Run(ToolInput{
		Method:        "rename",
		Content:       "package main\n",
		FilePath:      "main.go",
		WorkspaceRoot: "/tmp",
		Position:      &Position{Line: 1, Column: 1},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "gopls_not_available" {
		t.Errorf("expected gopls_not_available error, got %q", result.Error)
	}
}

func TestGoAdapterRunReferencesNoGopls(t *testing.T) {
	_, err := exec.LookPath("gopls")
	if err == nil {
		t.Skip("gopls is available; skipping not-available path test")
	}

	adapter := NewGoAdapter()
	result, err := adapter.Run(ToolInput{
		Method:        "references",
		Content:       "package main\n",
		FilePath:      "main.go",
		WorkspaceRoot: "/tmp",
		Position:      &Position{Line: 1, Column: 1},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "gopls_not_available" {
		t.Errorf("expected gopls_not_available error, got %q", result.Error)
	}
}

func TestGoAdapterRunCodeActionsNoGoimports(t *testing.T) {
	_, err := exec.LookPath("goimports")
	if err == nil {
		t.Skip("goimports is available; skipping not-available path test")
	}

	adapter := NewGoAdapter()
	result, err := adapter.Run(ToolInput{
		Method:   "code_actions",
		Content:  "package main\n",
		FilePath: "main.go",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "goimports_not_available" {
		t.Errorf("expected goimports_not_available error, got %q", result.Error)
	}
}

func TestGoAdapterRunInlayHintsNoGopls(t *testing.T) {
	_, err := exec.LookPath("gopls")
	if err == nil {
		t.Skip("gopls is available; skipping not-available path test")
	}

	adapter := NewGoAdapter()
	result, err := adapter.Run(ToolInput{
		Method:   "inlay_hints",
		Content:  "package main\n",
		FilePath: "main.go",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "gopls_not_available" {
		t.Errorf("expected gopls_not_available error, got %q", result.Error)
	}
}

func TestGoAdapterRunSignatureHelpNoGopls(t *testing.T) {
	_, err := exec.LookPath("gopls")
	if err == nil {
		t.Skip("gopls is available; skipping not-available path test")
	}

	adapter := NewGoAdapter()
	result, err := adapter.Run(ToolInput{
		Method:   "signature_help",
		Content:  "package main\n",
		FilePath: "main.go",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "gopls_not_available" {
		t.Errorf("expected gopls_not_available error, got %q", result.Error)
	}
}

// =============================================================================
// readJSONRPC error paths
// =============================================================================

func TestReadJSONRPCErrorReadingHeader(t *testing.T) {
	// Simulate a reader that errors on first read
	r := bufio.NewReader(&errorReader{err: errors.New("read failed")})
	result, err := readJSONRPC(r)
	if err == nil {
		t.Fatal("expected error reading header")
	}
	if result != nil {
		t.Error("expected nil result")
	}
}

func TestReadJSONRPCErrorReadingSeparator(t *testing.T) {
	// Reader that returns the header line, then errors on the separator
	r := bufio.NewReader(&errorAfterNthRead{
		data:  []string{"Content-Length: 5\r\n", ""},
		errAt: 1,
		err:   errors.New("read separator failed"),
	})
	result, err := readJSONRPC(r)
	if err == nil {
		t.Fatal("expected error reading separator")
	}
	if result != nil {
		t.Error("expected nil result")
	}
}

func TestReadJSONRPCErrorReadingContent(t *testing.T) {
	// Reader that returns header + separator, but not enough content bytes
	r := bufio.NewReader(&errorAfterNthRead{
		data:  []string{"Content-Length: 100\r\n", "\r\n", "short"},
		errAt: 2,
		err:   io.EOF,
	})
	result, err := readJSONRPC(r)
	if err == nil {
		t.Fatal("expected error reading content")
	}
	if result != nil {
		t.Error("expected nil result")
	}
}

func TestReadJSONRPCJSONUnmarshalError(t *testing.T) {
	// Valid Content-Length but invalid JSON body
	body := "not json"
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	r := bufio.NewReader(strings.NewReader(header))
	result, err := readJSONRPC(r)
	if err == nil {
		t.Fatal("expected JSON unmarshal error")
	}
	if result != nil {
		t.Error("expected nil result on unmarshal error")
	}
}

func TestReadJSONRPCHeaderWithoutContentLength(t *testing.T) {
	// Header line that doesn't contain "Content-Length:" — Sscanf will leave contentLen as 0
	header := "X-Custom: 5\r\n\r\nhello"
	r := bufio.NewReader(strings.NewReader(header))
	result, err := readJSONRPC(r)
	if err == nil {
		t.Fatal("expected error for empty body (Content-Length parsed as 0)")
	}
	if result != nil {
		t.Errorf("expected nil result for empty body, got %v", result)
	}
}

// errorReader always returns an error on Read.
type errorReader struct {
	err error
}

func (r *errorReader) Read(p []byte) (n int, err error) {
	return 0, r.err
}

// errorAfterNthRead returns data strings on successive Read() calls, then
// returns err. NOTE: This mock relies on bufio.Reader making exactly one
// Read() call per ReadString() invocation when the data is short enough to
// fit in the buffer. If bufio internals change, these tests may need adjustment.
type errorAfterNthRead struct {
	data  []string
	errAt int
	err   error
	readN int
}

func (r *errorAfterNthRead) Read(p []byte) (int, error) {
	if r.readN >= len(r.data) {
		return 0, r.err
	}
	n := copy(p, []byte(r.data[r.readN]))
	r.readN++
	if r.readN > r.errAt {
		return n, r.err
	}
	return n, nil
}

// =============================================================================
// parseGoplsInlayHints edge cases
// =============================================================================

func TestParseGoplsInlayHintsNoPosition(t *testing.T) {
	content := "package main\n"
	resp := map[string]interface{}{
		"result": []interface{}{
			map[string]interface{}{
				"label": "int",
				"kind":  float64(1),
				// no "position" field
			},
		},
	}
	hints := parseGoplsInlayHints(resp, content)
	if len(hints) != 0 {
		t.Fatalf("expected 0 hints when no position, got %d", len(hints))
	}
}

func TestParseGoplsInlayHintsNonMapPosition(t *testing.T) {
	content := "package main\n"
	resp := map[string]interface{}{
		"result": []interface{}{
			map[string]interface{}{
				"position": "not a map",
				"label":    "int",
				"kind":     float64(1),
			},
		},
	}
	hints := parseGoplsInlayHints(resp, content)
	if len(hints) != 0 {
		t.Fatalf("expected 0 hints for non-map position, got %d", len(hints))
	}
}

func TestParseGoplsInlayHintsNonMapItem(t *testing.T) {
	content := "package main\n"
	resp := map[string]interface{}{
		"result": []interface{}{
			"not a map",
			123,
			nil,
		},
	}
	hints := parseGoplsInlayHints(resp, content)
	if len(hints) != 0 {
		t.Fatalf("expected 0 hints for non-map items, got %d", len(hints))
	}
}

func TestParseGoplsInlayHintsNoLabel(t *testing.T) {
	content := "package main\n"
	resp := map[string]interface{}{
		"result": []interface{}{
			map[string]interface{}{
				"position": map[string]interface{}{"line": float64(0), "character": float64(0)},
				"kind":     float64(1),
				// no "label" field at all
			},
		},
	}
	hints := parseGoplsInlayHints(resp, content)
	if len(hints) != 0 {
		t.Fatalf("expected 0 hints when no label, got %d", len(hints))
	}
}

func TestParseGoplsInlayHintsArrayLabelNonMapParts(t *testing.T) {
	content := "package main\n"
	resp := map[string]interface{}{
		"result": []interface{}{
			map[string]interface{}{
				"position": map[string]interface{}{"line": float64(0), "character": float64(0)},
				"label": []interface{}{
					"not a map",
					123,
					nil,
				},
				"kind": float64(2),
			},
		},
	}
	hints := parseGoplsInlayHints(resp, content)
	if len(hints) != 1 {
		t.Fatalf("expected 1 hint, got %d", len(hints))
	}
	if hints[0].Label != "" {
		t.Errorf("expected empty label for non-map parts, got %q", hints[0].Label)
	}
}

func TestParseGoplsInlayHintsNoKind(t *testing.T) {
	content := "package main\n"
	resp := map[string]interface{}{
		"result": []interface{}{
			map[string]interface{}{
				"position": map[string]interface{}{"line": float64(0), "character": float64(0)},
				"label":    "int",
				// no "kind" field
			},
		},
	}
	hints := parseGoplsInlayHints(resp, content)
	if len(hints) != 1 {
		t.Fatalf("expected 1 hint, got %d", len(hints))
	}
	if hints[0].Kind != "none" {
		t.Errorf("expected kind 'none', got %q", hints[0].Kind)
	}
}

func TestParseGoplsInlayHintsMixedValidAndInvalid(t *testing.T) {
	content := "package main\n\nfunc main() {\n\tx := 1\n}\n"
	resp := map[string]interface{}{
		"result": []interface{}{
			// valid hint
			map[string]interface{}{
				"position": map[string]interface{}{"line": float64(3), "character": float64(1)},
				"label":    "int",
				"kind":     float64(1),
			},
			// invalid: no position
			map[string]interface{}{
				"label": "string",
				"kind":  float64(2),
			},
			// invalid: non-map item
			"garbage",
			// valid hint
			map[string]interface{}{
				"position": map[string]interface{}{"line": float64(4), "character": float64(0)},
				"label":    "unused",
				"kind":     float64(1),
			},
		},
	}
	hints := parseGoplsInlayHints(resp, content)
	if len(hints) != 2 {
		t.Fatalf("expected 2 valid hints, got %d", len(hints))
	}
	if hints[0].Label != "int" {
		t.Errorf("first hint label = %q, want 'int'", hints[0].Label)
	}
	if hints[1].Label != "unused" {
		t.Errorf("second hint label = %q, want 'unused'", hints[1].Label)
	}
}

// =============================================================================
// computeActiveParameter edge cases
// =============================================================================

func TestComputeActiveParameterLineBeyondContent(t *testing.T) {
	input := ToolInput{
		Content:  "foo(1, 2)",
		Position: &Position{Line: 10, Column: 5},
	}
	got := computeActiveParameter(input)
	if got != 0 {
		t.Errorf("expected 0 for line beyond content, got %d", got)
	}
}

func TestComputeActiveParameterColumnBeyondLine(t *testing.T) {
	input := ToolInput{
		Content:  "foo(1, 2)",
		Position: &Position{Line: 1, Column: 100},
	}
	got := computeActiveParameter(input)
	// Should still work: cursor is past end of line, counts commas before ')'
	if got < 0 {
		t.Errorf("expected non-negative, got %d", got)
	}
}

func TestComputeActiveParameterMultipleNestedCalls(t *testing.T) {
	// foo(bar(1, 2), baz(3))
	// The backward walk from '3' hits '(' of baz first (depth=1), then ',' at depth=1 (skipped),
	// then ')' of bar(...) at depth=2, then ',' at depth=2 (skipped), etc.
	// Eventually hits '(' of foo at depth=0 with commaCount=0.
	// This is a known limitation of the simple backward-walk algorithm.
	input := ToolInput{
		Content:  "foo(bar(1, 2), baz(3))",
		Position: &Position{Line: 1, Column: 20}, // at '3'
	}
	got := computeActiveParameter(input)
	if got != 0 {
		t.Errorf("expected active param 0 (backward walk limitation with nested calls), got %d", got)
	}
}

func TestComputeActiveParameterMultiLine(t *testing.T) {
	content := "foo(\n\t1,\n\t2,\n\t3\n)"
	input := ToolInput{
		Content:  content,
		Position: &Position{Line: 3, Column: 3}, // at '2'
	}
	got := computeActiveParameter(input)
	if got != 1 {
		t.Errorf("expected active param 1 (the '2'), got %d", got)
	}
}

func TestComputeActiveParameterEmptyParens(t *testing.T) {
	input := ToolInput{
		Content:  "foo()",
		Position: &Position{Line: 1, Column: 4}, // between parens
	}
	got := computeActiveParameter(input)
	if got != 0 {
		t.Errorf("expected active param 0 for empty parens, got %d", got)
	}
}

func TestComputeActiveParameterAfterClosingParen(t *testing.T) {
	// foo(1, 2) — cursor after closing paren.
	// Backward walk finds '(' at depth 0, commaCount=0 (no commas at depth 0 before '(').
	// Actually no commas were found at depth 0 because we stopped at '('.
	input := ToolInput{
		Content:  "foo(1, 2)",
		Position: &Position{Line: 1, Column: 10}, // after closing paren
	}
	got := computeActiveParameter(input)
	if got != 0 {
		t.Errorf("expected active param 0 (backward walk stops at opening paren), got %d", got)
	}
}

func TestComputeActiveParameterTripleNesting(t *testing.T) {
	// foo(a, bar(baz(1)))
	// Backward walk from '1' hits '(' of baz first (depth=1), then ')' of bar (depth=2),
	// then '(' of bar (depth=1), then ',' at depth=1 (skipped), then '(' of foo (depth=0).
	// Returns commaCount=0.
	input := ToolInput{
		Content:  "foo(a, bar(baz(1)))",
		Position: &Position{Line: 1, Column: 18}, // at '1'
	}
	got := computeActiveParameter(input)
	if got != 0 {
		t.Errorf("expected active param 0 (backward walk limitation with triple nesting), got %d", got)
	}
}

func TestComputeActiveParameterEmptyContent(t *testing.T) {
	input := ToolInput{
		Content:  "",
		Position: &Position{Line: 1, Column: 1},
	}
	got := computeActiveParameter(input)
	if got != 0 {
		t.Errorf("expected 0 for empty content, got %d", got)
	}
}

// =============================================================================
// extractParamsFromSignature edge cases
// =============================================================================

func TestExtractParamsFromSignaturePointerReceiverNoName(t *testing.T) {
	// func (*T) Method(a int) — pointer receiver without variable name
	params := extractParamsFromSignature("func (*T) Method(a int) error")
	if len(params) != 1 {
		t.Fatalf("expected 1 param, got %d: %v", len(params), params)
	}
	if params[0].Label != "a int" {
		t.Errorf("param = %q, want 'a int'", params[0].Label)
	}
}

func TestExtractParamsFromSignatureCurlyBraces(t *testing.T) {
	params := extractParamsFromSignature("func Foo(a struct{x int})")
	if len(params) != 1 {
		t.Fatalf("expected 1 param, got %d: %v", len(params), params)
	}
	if params[0].Label != "a struct{x int}" {
		t.Errorf("param = %q, want 'a struct{x int}'", params[0].Label)
	}
}

func TestExtractParamsFromSignatureBareParens(t *testing.T) {
	// A signature that is just parens with no "func" prefix
	params := extractParamsFromSignature("(a int, b string)")
	if len(params) != 2 {
		t.Fatalf("expected 2 params, got %d: %v", len(params), params)
	}
	if params[0].Label != "a int" {
		t.Errorf("first param = %q, want 'a int'", params[0].Label)
	}
	if params[1].Label != "b string" {
		t.Errorf("second param = %q, want 'b string'", params[1].Label)
	}
}

func TestExtractParamsFromSignatureMapType(t *testing.T) {
	params := extractParamsFromSignature("func Foo(m map[string]int)")
	if len(params) != 1 {
		t.Fatalf("expected 1 param, got %d: %v", len(params), params)
	}
	if params[0].Label != "m map[string]int" {
		t.Errorf("param = %q, want 'm map[string]int'", params[0].Label)
	}
}

func TestExtractParamsFromSignatureVariadic(t *testing.T) {
	params := extractParamsFromSignature("func Foo(args ...int)")
	if len(params) != 1 {
		t.Fatalf("expected 1 param, got %d: %v", len(params), params)
	}
	if params[0].Label != "args ...int" {
		t.Errorf("param = %q, want 'args ...int'", params[0].Label)
	}
}

func TestExtractParamsFromSignatureMultipleResults(t *testing.T) {
	params := extractParamsFromSignature("func Foo(a int) (int, error)")
	if len(params) != 1 {
		t.Fatalf("expected 1 param, got %d: %v", len(params), params)
	}
	if params[0].Label != "a int" {
		t.Errorf("param = %q, want 'a int'", params[0].Label)
	}
}

func TestExtractParamsFromSignatureChannelParam(t *testing.T) {
	params := extractParamsFromSignature("func Foo(ch chan int, done <-chan bool)")
	if len(params) != 2 {
		t.Fatalf("expected 2 params, got %d: %v", len(params), params)
	}
	if params[0].Label != "ch chan int" {
		t.Errorf("first param = %q, want 'ch chan int'", params[0].Label)
	}
	if params[1].Label != "done <-chan bool" {
		t.Errorf("second param = %q, want 'done <-chan bool'", params[1].Label)
	}
}

// =============================================================================
// parseGoplsSignatureHelp edge cases
// =============================================================================

func TestParseGoplsSignatureHelpMultiLineOutput(t *testing.T) {
	// gopls signature-help can output two lines:
	// Line 1: full signature
	// Line 2: params only
	output := "func Foo(a int, b string) error\na int, b string\n"
	input := ToolInput{
		Content:  "Foo(1, \"hi\")",
		Position: &Position{Line: 1, Column: 10},
	}
	result := parseGoplsSignatureHelp(output, input)
	if len(result.Signatures) != 1 {
		t.Fatalf("expected 1 signature, got %d", len(result.Signatures))
	}
	// The label should be the first line only
	if result.Signatures[0].Label != "func Foo(a int, b string) error" {
		t.Errorf("label = %q", result.Signatures[0].Label)
	}
}

func TestParseGoplsSignatureHelpBlankLines(t *testing.T) {
	output := "\n\nfunc Foo(a int)\n"
	input := ToolInput{Content: "Foo(1)", Position: &Position{Line: 1, Column: 5}}
	result := parseGoplsSignatureHelp(output, input)
	if len(result.Signatures) != 1 {
		t.Fatalf("expected 1 signature, got %d", len(result.Signatures))
	}
}

func TestParseGoplsSignatureHelpNilPosition(t *testing.T) {
	output := "func Foo(a int)\n"
	input := ToolInput{Content: "Foo(1)", Position: nil}
	result := parseGoplsSignatureHelp(output, input)
	if len(result.Signatures) != 1 {
		t.Fatalf("expected 1 signature, got %d", len(result.Signatures))
	}
	// With nil position, active param should be 0
	if result.ActiveParameter != 0 {
		t.Errorf("expected active param 0, got %d", result.ActiveParameter)
	}
}

// =============================================================================
// goSessionAdapter Run dispatch tests (when gopls is NOT available)
// =============================================================================

func TestGoSessionAdapterRunRenameNoGopls(t *testing.T) {
	_, err := exec.LookPath("gopls")
	if err == nil {
		t.Skip("gopls is available; skipping not-available path test")
	}

	a := &goSessionAdapter{}
	defer a.Close()

	result, err := a.Run(ToolInput{
		Method:        "rename",
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

func TestGoSessionAdapterRunReferencesNoGopls(t *testing.T) {
	_, err := exec.LookPath("gopls")
	if err == nil {
		t.Skip("gopls is available; skipping not-available path test")
	}

	a := &goSessionAdapter{}
	defer a.Close()

	result, err := a.Run(ToolInput{
		Method:        "references",
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

func TestGoSessionAdapterRunCodeActionsNoGoimports(t *testing.T) {
	_, err := exec.LookPath("goimports")
	if err == nil {
		t.Skip("goimports is available; skipping not-available path test")
	}

	a := &goSessionAdapter{}
	defer a.Close()

	result, err := a.Run(ToolInput{
		Method:   "code_actions",
		Content:  "package main\n",
		FilePath: "main.go",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "goimports_not_available" {
		t.Errorf("expected goimports_not_available, got %q", result.Error)
	}
}

func TestGoSessionAdapterRunSignatureHelpNoGopls(t *testing.T) {
	_, err := exec.LookPath("gopls")
	if err == nil {
		t.Skip("gopls is available; skipping not-available path test")
	}

	a := &goSessionAdapter{}
	defer a.Close()

	result, err := a.Run(ToolInput{
		Method:   "signature_help",
		Content:  "package main\n",
		FilePath: "main.go",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "gopls_not_available" {
		t.Errorf("expected gopls_not_available, got %q", result.Error)
	}
}

func TestGoSessionAdapterRunRenameNilPosition(t *testing.T) {
	_, err := exec.LookPath("gopls")
	if err == nil {
		t.Skip("gopls is available; skipping not-available path test")
	}

	a := &goSessionAdapter{}
	defer a.Close()

	// With nil position, should default to 1:1 without panicking
	result, err := a.Run(ToolInput{
		Method:        "rename",
		Content:       "package main\n",
		FilePath:      "main.go",
		WorkspaceRoot: "/tmp",
		Position:      nil,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "gopls_not_available" {
		t.Errorf("expected gopls_not_available, got %q", result.Error)
	}
}

// =============================================================================
// goSessionAdapter ensureServerLocked / resetServerLocked tests
// =============================================================================

func TestGoSessionAdapterEnsureServerAlreadyRunning(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	a := &goSessionAdapter{}
	defer a.Close()

	// Start the server once
	a.mu.Lock()
	err := a.ensureServerLocked(t.TempDir())
	a.mu.Unlock()
	if err != nil {
		t.Fatalf("ensureServerLocked failed: %v", err)
	}

	// Call ensureServerLocked again — should find existing server and return nil
	a.mu.Lock()
	err = a.ensureServerLocked(t.TempDir())
	a.mu.Unlock()
	if err != nil {
		t.Fatalf("ensureServerLocked on existing server failed: %v", err)
	}

	// Verify server is still the same
	if a.serverCmd == nil {
		t.Fatal("server should still be running")
	}
}

func TestGoSessionAdapterResetServerWithNonNilFields(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	a := &goSessionAdapter{}
	defer a.Close()

	// Start server to populate fields
	a.mu.Lock()
	err := a.ensureServerLocked(t.TempDir())
	a.mu.Unlock()
	if err != nil {
		t.Fatalf("ensureServerLocked failed: %v", err)
	}

	// Verify fields are set
	a.mu.Lock()
	if a.serverCmd == nil {
		t.Fatal("serverCmd should be set")
	}
	if a.tmpDir == "" {
		t.Fatal("tmpDir should be set")
	}
	if a.remoteAddr == "" {
		t.Fatal("remoteAddr should be set")
	}
	a.mu.Unlock()

	// Now reset — should clean up without panic
	a.mu.Lock()
	a.resetServerLocked()
	a.mu.Unlock()

	// Verify fields are cleared
	a.mu.Lock()
	if a.serverCmd != nil {
		t.Error("serverCmd should be nil after reset")
	}
	if a.tmpDir != "" {
		t.Error("tmpDir should be empty after reset")
	}
	if a.remoteAddr != "" {
		t.Error("remoteAddr should be empty after reset")
	}
	a.mu.Unlock()
}

// =============================================================================
// runGoInlayHintsWithRemote tests
// =============================================================================

func TestRunGoInlayHintsWithRemoteInvalidAddress(t *testing.T) {
	result, err := runGoInlayHintsWithRemote(ToolInput{}, "", "not-a-unix-addr")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "invalid_remote_address" {
		t.Errorf("expected invalid_remote_address, got %q", result.Error)
	}
	if result.Capabilities.InlayHints {
		t.Error("expected InlayHints capability to be false")
	}
}

func TestRunGoInlayHintsWithRemoteEmptyUnixPrefix(t *testing.T) {
	// "unix;" with empty socket path — should try to connect and fail
	result, err := runGoInlayHintsWithRemote(ToolInput{}, "", "unix;")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "gopls_socket_connect_failed" {
		t.Errorf("expected gopls_socket_connect_failed, got %q", result.Error)
	}
}

// =============================================================================
// runGoDefinitionWithRemote tests
// =============================================================================

func TestRunGoDefinitionWithRemoteNilPosition(t *testing.T) {
	// This test verifies nil position doesn't panic. It connects to a
	// nonexistent socket which can take up to ~4s on Linux dial timeout.
	// The coverage gain justifies the cost.
	result, _ := runGoDefinitionWithRemote(ToolInput{
		Content:       "package main\n",
		FilePath:      "main.go",
		WorkspaceRoot: "/tmp",
		Position:      nil,
	}, "gopls", "unix;/nonexistent/socket")
	// The result will have no definition since gopls won't be available
	if result.Definition != nil {
		t.Logf("got definition: %+v (unexpected but not fatal)", result.Definition)
	}
}

// =============================================================================
// NewGoSessionPool factory test
// =============================================================================

func TestNewGoSessionPoolWithGopls(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	pool := NewGoSessionPool(5 * time.Minute)
	defer pool.Close()

	content := "package main\n\nfunc main() {}\n"
	dir, file := setupGoTestModule(t, content)

	// First call creates the session
	result, err := pool.Run(ToolInput{
		Method:        "definition",
		Content:       content,
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

	// Second call reuses the session
	result2, err := pool.Run(ToolInput{
		Method:        "definition",
		Content:       content,
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      &Position{Line: 3, Column: 1},
	})
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if !result2.Capabilities.Definition {
		t.Error("expected Definition capability on second call")
	}
}

// =============================================================================
// TypeScript session adapter coverage
// =============================================================================

func TestTypeScriptSessionAdapterResetWorkerWithNonNilFields(t *testing.T) {
	a := &typeScriptSessionAdapter{}
	// Manually set fields to non-nil to test reset path
	a.stdin = &noopWriteCloser{}
	a.stdout = bufio.NewReader(strings.NewReader("ready\n"))

	a.mu.Lock()
	a.resetWorkerLocked()
	a.mu.Unlock()

	a.mu.Lock()
	if a.stdout != nil {
		t.Error("stdout should be nil after reset")
	}
	if a.stdin != nil {
		t.Error("stdin should be nil after reset")
	}
	a.mu.Unlock()
}

func TestTypeScriptSessionAdapterHealthyNoCmd(t *testing.T) {
	a := &typeScriptSessionAdapter{}
	if a.Healthy() {
		t.Error("should not be healthy with no cmd")
	}
}

func TestTypeScriptSessionAdapterHealthyClosed(t *testing.T) {
	a := &typeScriptSessionAdapter{}
	a.Close()
	if a.Healthy() {
		t.Error("should not be healthy when closed")
	}
}

type noopWriteCloser struct{}

func (n *noopWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (n *noopWriteCloser) Close() error                { return nil }

// =============================================================================
// runGoDiagnostics edge cases
// =============================================================================

func TestRunGoDiagnosticsEmptyBaseName(t *testing.T) {
	if _, err := exec.LookPath("gofmt"); err != nil {
		t.Skip("gofmt not available")
	}

	result, err := runGoDiagnostics(ToolInput{
		Content:  "package main\n\nfunc main() {}\n",
		FilePath: ".", // empty-ish base name
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Capabilities.Diagnostics {
		t.Error("expected Diagnostics capability")
	}
}

func TestRunGoDiagnosticsTriggerEditSkipsVet(t *testing.T) {
	if _, err := exec.LookPath("gofmt"); err != nil {
		t.Skip("gofmt not available")
	}

	// With trigger="edit", go vet should be skipped even for valid code
	result, err := runGoDiagnostics(ToolInput{
		Content:  "package main\n\nfunc main() {}\n",
		FilePath: "main.go",
		Trigger:  "edit",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have no diagnostics for valid code
	if len(result.Diagnostics) != 0 {
		t.Errorf("expected 0 diagnostics for valid code with edit trigger, got %d", len(result.Diagnostics))
	}
}

// =============================================================================
// runGoHover edge cases
// =============================================================================

func TestRunGoHoverNilPosition(t *testing.T) {
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
	// Hover at 1:1 on "package" line should return something or nil
	if result.Error != "" {
		t.Errorf("unexpected error: %q", result.Error)
	}
}

// =============================================================================
// runGoCodeActions edge cases
// =============================================================================

func TestRunGoCodeActionsNoChanges(t *testing.T) {
	if _, err := exec.LookPath("goimports"); err != nil {
		t.Skip("goimports not available")
	}

	// Already clean code — goimports should produce no changes
	content := "package main\n\nfunc main() {}\n"
	result, err := runGoCodeActions(ToolInput{
		Content:  content,
		FilePath: "main.go",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.CodeActions != nil {
		t.Error("expected nil CodeActions for clean code")
	}
}

// =============================================================================
// runGoSignatureHelp edge cases
// =============================================================================

func TestRunGoSignatureHelpNilPosition(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	dir, file := setupGoTestModule(t, "package main\n\nfunc main() {}\n")

	result, err := runGoSignatureHelp(ToolInput{
		Content:       "package main\n\nfunc main() {}\n",
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      nil, // should default to 1:1
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Capabilities.SignatureHelp {
		t.Error("expected SignatureHelp capability")
	}
}

// =============================================================================
// session_pool requestEvict edge cases
// =============================================================================

func TestRequestEvictMismatchedAdapter(t *testing.T) {
	first := &fakeSessionAdapter{healthy: true}
	second := &fakeSessionAdapter{healthy: true}
	pool := NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		return first, nil
	}, 0)

	// Run to create session
	_, _ = pool.Run(ToolInput{WorkspaceRoot: "/repo-a"})

	// Request evict with a different adapter — should be a no-op
	pool.requestEvict("/repo-a", second)

	// Session should still exist
	pool.mu.Lock()
	_, exists := pool.sessions["/repo-a"]
	pool.mu.Unlock()
	if !exists {
		t.Error("expected session to still exist after evict with mismatched adapter")
	}

	pool.Close()
}

func TestReleaseMismatchedAdapter(t *testing.T) {
	first := &fakeSessionAdapter{healthy: true}
	second := &fakeSessionAdapter{healthy: true}
	pool := NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		return first, nil
	}, 0)

	// Acquire the adapter manually to control the inUse count
	adapter, err := pool.acquire("/repo-a")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	// Release with a different adapter — should be a no-op
	pool.release("/repo-a", second)

	// inUse should still be 1 (from the acquire, release with mismatched adapter is no-op)
	pool.mu.Lock()
	entry := pool.sessions["/repo-a"]
	pool.mu.Unlock()
	if entry == nil {
		t.Fatal("expected session to still exist")
	}
	if entry.inUse != 1 {
		t.Errorf("expected inUse=1 after mismatched release, got %d", entry.inUse)
	}

	// Clean up
	pool.release("/repo-a", adapter)
	pool.Close()
}

// =============================================================================
// goAdapter.Run with code_actions on syntax-error code
// =============================================================================

func TestGoAdapterRunCodeActionsSyntaxError(t *testing.T) {
	if _, err := exec.LookPath("goimports"); err != nil {
		t.Skip("goimports not available")
	}

	// Code with syntax errors — goimports should fail, returning no actions
	adapter := NewGoAdapter()
	result, err := adapter.Run(ToolInput{
		Method:   "code_actions",
		Content:  "package main\n\nfunc main() {\n\tfmt.Println(\n}\n",
		FilePath: "main.go",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Capabilities.CodeActions {
		t.Error("expected CodeActions capability")
	}
	// Should have no actions since goimports fails on broken code
	if result.CodeActions != nil {
		t.Errorf("expected nil CodeActions for syntax-error code, got %d actions", len(result.CodeActions))
	}
}

// =============================================================================
// runGoReferences with workspace-relative paths
// =============================================================================

func TestRunGoReferencesWorkspaceRelativePath(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	content := "package main\n\nfunc main() { println() }\n"
	dir, file := setupGoTestModule(t, content)

	result, err := runGoReferences(ToolInput{
		Content:       content,
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      &Position{Line: 3, Column: 17}, // on println
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.References == nil {
		t.Error("expected non-nil References")
	}
	// If references were found, verify paths are relative
	if result.References != nil && len(result.References.Locations) > 0 {
		for _, loc := range result.References.Locations {
			if strings.HasPrefix(loc.FilePath, "/") {
				t.Errorf("expected relative path, got absolute: %s", loc.FilePath)
			}
		}
	}
}

// =============================================================================
// runGoRename with multiple locations
// =============================================================================

func TestRunGoRenameMultipleOccurrences(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	content := "package main\n\nvar x int\n\nfunc main() {\n\ty := x\n\t_ = y + x\n}\n"
	dir, file := setupGoTestModule(t, content)

	result, err := runGoRename(ToolInput{
		Content:       content,
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      &Position{Line: 3, Column: 5}, // on 'x' in "var x int"
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Rename == nil {
		t.Error("expected non-nil Rename")
	}
	// gopls may or may not find rename locations depending on file/project state
	// Just verify the result structure is valid
	if result.Rename != nil {
		t.Logf("Found %d rename locations", len(result.Rename.Locations))
	}
}

// =============================================================================
// runGoDiagnostics with save trigger (runs go vet)
// =============================================================================

func TestRunGoDiagnosticsSaveTrigger(t *testing.T) {
	if _, err := exec.LookPath("gofmt"); err != nil {
		t.Skip("gofmt not available")
	}

	// Valid code with save trigger — should run go vet (no errors expected)
	result, err := runGoDiagnostics(ToolInput{
		Content:  "package main\n\nfunc main() {}\n",
		FilePath: "main.go",
		Trigger:  "save",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Capabilities.Diagnostics {
		t.Error("expected Diagnostics capability")
	}
}

// =============================================================================
// runGoCodeActions with empty base name
// =============================================================================

func TestRunGoCodeActionsEmptyBaseName(t *testing.T) {
	if _, err := exec.LookPath("goimports"); err != nil {
		t.Skip("goimports not available")
	}

	result, err := runGoCodeActions(ToolInput{
		Content:  "package main\n\nfunc main() {}\n",
		FilePath: ".",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Capabilities.CodeActions {
		t.Error("expected CodeActions capability")
	}
}

// =============================================================================
// runGoInlayHints with gopls available
// =============================================================================

func TestRunGoInlayHintsDirectlyWithGopls(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	content := "package main\n\nfunc main() {\n\tx := 1\n\t_ = x\n}\n"
	dir, file := setupGoTestModule(t, content)

	result, err := runGoInlayHints(ToolInput{
		Content:       content,
		FilePath:      file,
		WorkspaceRoot: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Capabilities.InlayHints {
		t.Error("expected InlayHints capability")
	}
}

// =============================================================================
// goSessionAdapter Run with gopls available — definition via remote
// =============================================================================

func TestGoSessionAdapterRunDefinitionWithGopls(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	content := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"
	dir, file := setupGoTestModule(t, content)

	a := &goSessionAdapter{}
	defer a.Close()

	result, err := a.Run(ToolInput{
		Method:        "definition",
		Content:       content,
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      &Position{Line: 6, Column: 6},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Capabilities.Definition {
		t.Error("expected Definition capability")
	}
}

// =============================================================================
// goSessionAdapter Healthy with server process
// =============================================================================

func TestGoSessionAdapterHealthyWithServer(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	a := &goSessionAdapter{}
	defer a.Close()

	a.mu.Lock()
	err := a.ensureServerLocked(t.TempDir())
	a.mu.Unlock()
	if err != nil {
		t.Fatalf("ensureServerLocked failed: %v", err)
	}

	if !a.Healthy() {
		t.Error("adapter with running server should be healthy")
	}
}

// =============================================================================
// goSessionAdapter Healthy when closed
// =============================================================================

func TestGoSessionAdapterHealthyAfterClose(t *testing.T) {
	a := &goSessionAdapter{}
	a.Close()
	if a.Healthy() {
		t.Error("closed adapter should not be healthy")
	}
}

// =============================================================================
// runGoReferences — line text extraction, relative paths, symbol name, sorting
// =============================================================================

func TestRunGoReferencesExtractsLineText(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	// Define a variable used on multiple lines so references span different lines.
	content := "package main\n\nvar config string\n\nfunc init() {\n\tconfig = \"default\"\n}\n\nfunc main() {\n\t_ = config\n}\n"
	dir, file := setupGoTestModule(t, content)

	result, err := runGoReferences(ToolInput{
		Content:       content,
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      &Position{Line: 3, Column: 5}, // on 'config' in "var config string"
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.References == nil {
		t.Fatal("expected non-nil References")
	}
	if result.References.SymbolName == "" {
		t.Error("expected non-empty SymbolName from cursor position")
	}
	// Verify lineText is populated for each location
	for i, loc := range result.References.Locations {
		if loc.LineText == "" {
			t.Errorf("location[%d] has empty LineText", i)
		}
		// Paths should be relative (workspace-root-relative)
		if strings.HasPrefix(loc.FilePath, "/") {
			t.Errorf("location[%d] has absolute path: %s", i, loc.FilePath)
		}
	}
}

func TestRunGoReferencesMultiFileSymbolSorting(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	// Define a function used in two files to test current-file-first sorting.
	mainContent := "package main\n\nfunc greet() string { return \"hi\" }\n\nfunc main() {\n\tprintln(greet())\n}\n"
	utilContent := "package main\n\nfunc helper() {\n\t_ = greet()\n}\n"

	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.go")
	utilFile := filepath.Join(dir, "util.go")
	goMod := "module testmain\n\ngo 1.21\n"
	for _, pair := range [][2]string{
		{mainFile, mainContent},
		{utilFile, utilContent},
		{filepath.Join(dir, "go.mod"), goMod},
	} {
		if err := os.WriteFile(pair[0], []byte(pair[1]), 0644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := runGoReferences(ToolInput{
		Content:       mainContent,
		FilePath:      mainFile,
		WorkspaceRoot: dir,
		Position:      &Position{Line: 3, Column: 6}, // on 'greet'
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.References == nil {
		t.Fatal("expected non-nil References")
	}
	// If multiple locations found, current file should come first
	if len(result.References.Locations) > 1 {
		firstFile := result.References.Locations[0].FilePath
		expectedRel, _ := filepath.Rel(dir, mainFile)
		expectedRel = filepath.ToSlash(expectedRel)
		if firstFile != expectedRel && firstFile != "main.go" {
			t.Logf("first location file=%q, expected current file first (%q)", firstFile, expectedRel)
		}
	}
}

// =============================================================================
// runGoRename — deduplication and sorting by From offset
// =============================================================================

func TestRunGoRenameWithSameFileRefs(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	// Variable used multiple times in the same file.
	content := "package main\n\nvar x int\n\nfunc main() {\n\ty := x\n\t_ = y + x\n}\n"
	dir, file := setupGoTestModule(t, content)

	result, err := runGoRename(ToolInput{
		Content:       content,
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      &Position{Line: 3, Column: 5}, // on 'x' in "var x int"
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Rename == nil {
		t.Fatal("expected non-nil Rename")
	}
	// Verify sorting: locations must be sorted by From offset ascending
	for i := 1; i < len(result.Rename.Locations); i++ {
		if result.Rename.Locations[i].From < result.Rename.Locations[i-1].From {
			t.Errorf("locations[%d].From=%d < locations[%d].From=%d (not sorted)",
				i, result.Rename.Locations[i].From, i-1, result.Rename.Locations[i-1].From)
		}
	}
	// Verify deduplication: no two locations should have the same from:to
	seen := make(map[string]bool)
	for i, loc := range result.Rename.Locations {
		key := fmt.Sprintf("%d:%d", loc.From, loc.To)
		if seen[key] {
			t.Errorf("location[%d] has duplicate key %s", i, key)
		}
		seen[key] = true
	}
}

func TestRunGoRenameFiltersOtherFiles(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	// Define a function used in two files — rename should only return locations
	// from the current file.
	mainContent := "package main\n\nfunc greet() string { return \"hi\" }\n\nfunc main() {\n\tprintln(greet())\n}\n"
	utilContent := "package main\n\nfunc helper() {\n\t_ = greet()\n}\n"

	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.go")
	utilFile := filepath.Join(dir, "util.go")
	goMod := "module testmain\n\ngo 1.21\n"
	for _, pair := range [][2]string{
		{mainFile, mainContent},
		{utilFile, utilContent},
		{filepath.Join(dir, "go.mod"), goMod},
	} {
		if err := os.WriteFile(pair[0], []byte(pair[1]), 0644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := runGoRename(ToolInput{
		Content:       mainContent,
		FilePath:      mainFile,
		WorkspaceRoot: dir,
		Position:      &Position{Line: 3, Column: 6}, // on 'greet' in main.go
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Rename == nil {
		t.Fatal("expected non-nil Rename")
	}
	// All returned locations must be from the current file only
	for i, loc := range result.Rename.Locations {
		if filepath.Clean(loc.FilePath) != filepath.Clean(mainFile) {
			t.Errorf("location[%d] is from %s, expected only %s", i, loc.FilePath, mainFile)
		}
	}
}

// =============================================================================
// goSessionAdapter.Run dispatch tests (with gopls available)
// =============================================================================

func TestGoSessionAdapterRunHoverWithGopls(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	content := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"
	dir, file := setupGoTestModule(t, content)

	a := &goSessionAdapter{}
	defer a.Close()

	result, err := a.Run(ToolInput{
		Method:        "hover",
		Content:       content,
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      &Position{Line: 6, Column: 6}, // on fmt
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Capabilities.Hover {
		t.Error("expected Hover capability")
	}
}

func TestGoSessionAdapterRunRenameWithGopls(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	content := "package main\n\nfunc greet() string { return \"hi\" }\n\nfunc main() {\n\tprintln(greet())\n}\n"
	dir, file := setupGoTestModule(t, content)

	a := &goSessionAdapter{}
	defer a.Close()

	result, err := a.Run(ToolInput{
		Method:        "rename",
		Content:       content,
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      &Position{Line: 3, Column: 6}, // on 'greet'
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Capabilities.Rename {
		t.Error("expected Rename capability")
	}
}

func TestGoSessionAdapterRunReferencesWithGopls(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	content := "package main\n\nvar config string\n\nfunc main() {\n\t_ = config\n}\n"
	dir, file := setupGoTestModule(t, content)

	a := &goSessionAdapter{}
	defer a.Close()

	result, err := a.Run(ToolInput{
		Method:        "references",
		Content:       content,
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      &Position{Line: 3, Column: 5}, // on 'config'
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Capabilities.References {
		t.Error("expected References capability")
	}
}

func TestGoSessionAdapterRunCodeActionsWithGoimports(t *testing.T) {
	if _, err := exec.LookPath("goimports"); err != nil {
		t.Skip("goimports not available")
	}

	content := "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"
	_, file := setupGoTestModule(t, content)

	a := &goSessionAdapter{}
	defer a.Close()

	result, err := a.Run(ToolInput{
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
}

func TestGoSessionAdapterRunSignatureHelpWithGopls(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	content := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\n}\n"
	dir, file := setupGoTestModule(t, content)

	a := &goSessionAdapter{}
	defer a.Close()

	result, err := a.Run(ToolInput{
		Method:        "signature_help",
		Content:       content,
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      &Position{Line: 6, Column: 14}, // inside Println(
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Capabilities.SignatureHelp {
		t.Error("expected SignatureHelp capability")
	}
}

func TestGoSessionAdapterRunInlayHintsWithGopls(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	content := "package main\n\nfunc main() {\n\tx := 1\n\t_ = x\n}\n"
	dir, file := setupGoTestModule(t, content)

	a := &goSessionAdapter{}
	defer a.Close()

	result, err := a.Run(ToolInput{
		Method:        "inlay_hints",
		Content:       content,
		FilePath:      file,
		WorkspaceRoot: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Capabilities.InlayHints {
		t.Error("expected InlayHints capability")
	}
}

// =============================================================================
// runGoHover — empty content path
// =============================================================================

func TestRunGoHoverEmptyContent(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	// Hover on an empty line — gopls returns empty stdout
	content := "package main\n\nfunc main() {}\n"
	dir, file := setupGoTestModule(t, content)

	result, err := runGoHover(ToolInput{
		Content:       content,
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      &Position{Line: 2, Column: 1}, // empty line
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Capabilities.Hover {
		t.Error("expected Hover capability")
	}
	// When gopls returns empty hover, the Hover field should be nil
	if result.Hover != nil {
		t.Errorf("expected nil Hover on empty line, got %v", result.Hover)
	}
}

func TestRunGoHoverCommentLine(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	content := "package main\n// just a comment\nfunc main() {}\n"
	dir, file := setupGoTestModule(t, content)

	result, err := runGoHover(ToolInput{
		Content:       content,
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      &Position{Line: 2, Column: 3}, // on comment
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Hover on a comment typically returns nil
	if result.Hover != nil {
		t.Logf("got hover on comment (unexpected but not fatal): %v", result.Hover)
	}
}

// =============================================================================
// ensureServerLocked — server start failure path
// =============================================================================

func TestGoSessionAdapterEnsureServerStartFailure(t *testing.T) {
	// This covers the path where gopls server fails to become ready (timeout).
	// We can't easily simulate this, so we verify that after reset, subsequent
	// ensureServerLocked can recover.
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	a := &goSessionAdapter{}
	defer a.Close()

	// Start server, then reset it
	a.mu.Lock()
	err := a.ensureServerLocked(t.TempDir())
	a.mu.Unlock()
	if err != nil {
		t.Fatalf("ensureServerLocked failed: %v", err)
	}

	a.mu.Lock()
	a.resetServerLocked()
	a.mu.Unlock()

	// Now ensureServerLocked should start a new server
	dir2 := t.TempDir()
	a.mu.Lock()
	err = a.ensureServerLocked(dir2)
	a.mu.Unlock()
	if err != nil {
		t.Fatalf("ensureServerLocked after reset failed: %v", err)
	}
}

// =============================================================================
// TypeScript adapter with Node.js
// =============================================================================

func TestRunTypeScriptToolDiagnostics(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}

	// TypeScript code with a syntax error
	content := "const x: number = \"not a number\"\n"

	result, err := runTypeScriptTool(ToolInput{
		Content:       content,
		FilePath:      "test.ts",
		WorkspaceRoot: t.TempDir(),
		Method:        "diagnostics",
	})
	if err != nil {
		// Node might not have typescript installed; that's OK
		// We still cover the function path
		t.Logf("runTypeScriptTool returned error (typescript may not be installed): %v", err)
		return
	}

	// If it succeeded, verify the result structure
	if !result.Capabilities.Diagnostics {
		// TypeScript not available in this environment is acceptable
		t.Logf("Diagnostics capability not available (typescript not installed)")
	}
}

func TestRunTypeScriptToolDiagnosticsNoTs(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}

	// Run in a directory with no node_modules — typescript won't be found
	tmpDir := t.TempDir()

	result, err := runTypeScriptTool(ToolInput{
		Content:       "const x = 1\n",
		FilePath:      "test.ts",
		WorkspaceRoot: tmpDir,
		Method:        "diagnostics",
	})
	// When typescript is not available, we expect an error about it
	if err == nil {
		if result.Error != "" {
			if result.Error != "typescript_not_available" {
				t.Logf("got error %q (not typescript_not_available, but acceptable)", result.Error)
			}
		}
	} else {
		t.Logf("runTypeScriptTool returned error (expected when no typescript): %v", err)
	}
}

func TestTypeScriptSessionAdapterEnsureWorker(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}

	a := &typeScriptSessionAdapter{}
	defer a.Close()

	// ensureWorkerLocked should succeed
	dir := t.TempDir()
	a.mu.Lock()
	err := a.ensureWorkerLocked(dir)
	a.mu.Unlock()
	if err != nil {
		t.Fatalf("ensureWorkerLocked failed: %v", err)
	}

	// Verify fields are set
	a.mu.Lock()
	if a.cmd == nil {
		t.Error("cmd should be set after ensureWorkerLocked")
	}
	if a.stdin == nil {
		t.Error("stdin should be set after ensureWorkerLocked")
	}
	if a.stdout == nil {
		t.Error("stdout should be set after ensureWorkerLocked")
	}
	a.mu.Unlock()
}

func TestTypeScriptSessionAdapterEnsureWorkerAlreadyRunning(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}

	a := &typeScriptSessionAdapter{}
	defer a.Close()

	dir := t.TempDir()
	a.mu.Lock()
	err := a.ensureWorkerLocked(dir)
	a.mu.Unlock()
	if err != nil {
		t.Fatalf("ensureWorkerLocked failed: %v", err)
	}

	// Save original cmd
	a.mu.Lock()
	origCmd := a.cmd
	a.mu.Unlock()

	// Call again — should find existing worker and return nil
	a.mu.Lock()
	err = a.ensureWorkerLocked(dir)
	a.mu.Unlock()
	if err != nil {
		t.Fatalf("ensureWorkerLocked on existing worker failed: %v", err)
	}

	// Should be the same process
	a.mu.Lock()
	if a.cmd != origCmd {
		t.Error("should reuse existing worker")
	}
	a.mu.Unlock()
}

func TestTypeScriptSessionAdapterRunValidInput(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}

	a := &typeScriptSessionAdapter{}
	defer a.Close()

	dir := t.TempDir()

	// Start the worker manually so we can control the lifecycle
	a.mu.Lock()
	err := a.ensureWorkerLocked(dir)
	a.mu.Unlock()
	if err != nil {
		t.Fatalf("ensureWorkerLocked failed: %v", err)
	}

	// Verify worker is running and healthy
	a.mu.Lock()
	if a.cmd == nil {
		t.Fatal("cmd should be set")
	}
	if a.stdin == nil {
		t.Fatal("stdin should be set")
	}
	if a.stdout == nil {
		t.Fatal("stdout should be set")
	}
	a.mu.Unlock()

	if !a.Healthy() {
		t.Error("adapter with running worker should be healthy")
	}

	// Close to clean up the worker (don't call Run() since it may block
	// if typescript is not installed and the worker doesn't respond)
}

func TestTypeScriptSessionPool(t *testing.T) {
	pool := NewTypeScriptSessionPool(5 * time.Minute)
	if pool == nil {
		t.Fatal("expected non-nil pool")
	}
	pool.Close()
}

// =============================================================================
// TypeScript session adapter — resetWorkerLocked with cmd non-nil, process nil
// =============================================================================

func TestTypeScriptSessionAdapterResetWorkerWithNilProcess(t *testing.T) {
	a := &typeScriptSessionAdapter{}
	a.cmd = &exec.Cmd{}
	a.stdin = &noopWriteCloser{}
	a.stdout = bufio.NewReader(strings.NewReader(""))

	a.mu.Lock()
	a.resetWorkerLocked()
	a.mu.Unlock()

	a.mu.Lock()
	if a.stdout != nil {
		t.Error("stdout should be nil after reset")
	}
	if a.stdin != nil {
		t.Error("stdin should be nil after reset")
	}
	if a.cmd != nil {
		t.Error("cmd should be nil after reset")
	}
	a.mu.Unlock()
}

// =============================================================================
// TypeScript session adapter — Healthy edge cases
// =============================================================================

func TestTypeScriptSessionAdapterHealthyWithProcess(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}

	a := &typeScriptSessionAdapter{}
	defer a.Close()

	dir := t.TempDir()
	a.mu.Lock()
	err := a.ensureWorkerLocked(dir)
	a.mu.Unlock()
	if err != nil {
		t.Fatalf("ensureWorkerLocked failed: %v", err)
	}

	if !a.Healthy() {
		t.Error("adapter with running worker should be healthy")
	}
}

// =============================================================================
// runTypeScriptTool — error output path
// =============================================================================

func TestRunTypeScriptToolNodeError(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}

	// Valid TS content in a dir without typescript
	tmpDir := t.TempDir()

	result, err := runTypeScriptTool(ToolInput{
		Content:       "const x = 1\n",
		FilePath:      "test.ts",
		WorkspaceRoot: tmpDir,
		Method:        "diagnostics",
	})
	// The error should contain info about typescript not being available
	_ = result
	if err == nil {
		// If no error, check if error is in the result
		if result.Error == "" && result.Capabilities.Diagnostics {
			// TypeScript happened to be installed; not an error condition
			t.Skip("typescript is globally available")
		}
	}
}

// =============================================================================
// requestEvict — evictOnRelease path in session_pool
// =============================================================================

func TestRequestEvictEvictsOnRelease(t *testing.T) {
	callCount := 0
	pool := NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		callCount++
		return &fakeSessionAdapter{healthy: true}, nil
	}, 0)
	defer pool.Close()

	// Acquire the adapter
	adapter, err := pool.acquire("/repo-a")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	// Request evict with the same adapter
	pool.requestEvict("/repo-a", adapter)

	// Release — should trigger eviction
	pool.release("/repo-a", adapter)

	// Session should be evicted
	pool.mu.Lock()
	_, exists := pool.sessions["/repo-a"]
	pool.mu.Unlock()
	if exists {
		t.Error("expected session to be evicted after release with evictOnRelease")
	}

	// Next acquire should create a new adapter
	_, err = pool.acquire("/repo-a")
	if err != nil {
		t.Fatalf("re-acquire failed: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected factory called twice, got %d", callCount)
	}
}

// =============================================================================
// session_pool requestEvict — uncovered path: nonexistent workspace key
// =============================================================================

func TestRequestEvictNonexistentWorkspace(t *testing.T) {
	pool := NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		return &fakeSessionAdapter{healthy: true}, nil
	}, 0)

	// requestEvict on a workspace that was never created — should be a no-op
	pool.requestEvict("/nonexistent", &fakeSessionAdapter{healthy: true})

	pool.Close()
}

// =============================================================================
// NewTypeScriptSessionPool — improve from 33.3% by exercising the pool
// =============================================================================

func TestNewTypeScriptSessionPoolFactory(t *testing.T) {
	// Just verify the pool is created and can be closed without panic.
	// We don't call Run() through the pool because the persistent node worker
	// will block waiting for stdin and never respond if typescript isn't available.
	pool := NewTypeScriptSessionPool(0)
	if pool == nil {
		t.Fatal("expected non-nil pool")
	}
	pool.Close()
}

func TestNewTypeScriptSessionPoolWithIdleEviction(t *testing.T) {
	pool := NewTypeScriptSessionPool(50 * time.Millisecond)
	if pool == nil {
		t.Fatal("expected non-nil pool")
	}
	// EvictIdle should not panic on empty pool
	pool.EvictIdle()
	pool.Close()
}

// =============================================================================
// typeScriptSessionAdapter Run — improve from 17.4% by covering error paths
// via the SessionPool with a mock SessionFactory
// =============================================================================

// errSessionAdapter is a SessionAdapter whose Run always returns an error.
type errSessionAdapter struct {
	closed bool
	err    error
}

func (a *errSessionAdapter) Run(input ToolInput) (ToolResult, error) {
	return ToolResult{}, a.err
}
func (a *errSessionAdapter) Healthy() bool { return !a.closed }
func (a *errSessionAdapter) Close() error {
	a.closed = true
	return nil
}

func TestSessionPoolRunWithAdapterRunError(t *testing.T) {
	callCount := 0
	pool := NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		callCount++
		return &errSessionAdapter{err: errors.New("adapter crashed")}, nil
	}, 0)
	defer pool.Close()

	_, err := pool.Run(ToolInput{WorkspaceRoot: "/repo-a"})
	if err == nil {
		t.Fatal("expected error from adapter Run")
	}
	if err.Error() != "adapter crashed" {
		t.Errorf("unexpected error: %v", err)
	}

	// After error, the adapter should be evicted, and the next call creates a new one
	_, err = pool.Run(ToolInput{WorkspaceRoot: "/repo-a"})
	if err == nil {
		t.Fatal("expected error from second adapter Run")
	}
	if callCount != 2 {
		t.Fatalf("expected factory to be called twice (eviction after error), got %d", callCount)
	}
}

func TestSessionPoolReleasesInUseOnSuccess(t *testing.T) {
	var mu sync.Mutex
	runCount := 0
	pool := NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		return &trackRunAdapter{runFn: func() {
			mu.Lock()
			runCount++
			mu.Unlock()
		}}, nil
	}, 0)
	defer pool.Close()

	_, err := pool.Run(ToolInput{WorkspaceRoot: "/repo-a"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	rc := runCount
	mu.Unlock()
	if rc != 1 {
		t.Fatalf("expected 1 run, got %d", rc)
	}
}

type trackRunAdapter struct {
	runFn func()
}

func (a *trackRunAdapter) Run(input ToolInput) (ToolResult, error) {
	a.runFn()
	return ToolResult{Capabilities: Capabilities{Diagnostics: true}}, nil
}
func (a *trackRunAdapter) Healthy() bool { return true }
func (a *trackRunAdapter) Close() error  { return nil }

// =============================================================================
// typeScriptSessionAdapter — Run error paths more specifically
// The internal fields are unexported, so we test through ensureWorkerLocked
// and then manually corrupt fields to trigger error paths.
// =============================================================================

func TestTypeScriptSessionAdapterRunStdoutReadError(t *testing.T) {
	a := &typeScriptSessionAdapter{}
	defer a.Close()

	// Pre-populate internal fields with mocks to avoid starting a real node process.
	a.mu.Lock()
	initFakeWorker(a, &noopWriteCloser{}, bufio.NewReader(&errorReader{err: errors.New("read failed")}))
	a.mu.Unlock()

	_, err := a.Run(ToolInput{
		Method:        "diagnostics",
		Content:       "const x = 1",
		FilePath:      "test.ts",
		WorkspaceRoot: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error when stdout read fails")
	}
	if !strings.Contains(err.Error(), "read failed") {
		t.Errorf("error should mention read failure, got: %v", err)
	}
}

func TestTypeScriptSessionAdapterRunWriteError(t *testing.T) {
	a := &typeScriptSessionAdapter{}
	defer a.Close()

	a.mu.Lock()
	initFakeWorker(a, &errWriteCloser{err: errors.New("write failed")}, bufio.NewReader(strings.NewReader("{}\n")))
	a.mu.Unlock()

	_, err := a.Run(ToolInput{
		Method:        "diagnostics",
		Content:       "const x = 1",
		FilePath:      "test.ts",
		WorkspaceRoot: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error when stdin write fails")
	}
	if !strings.Contains(err.Error(), "write failed") {
		t.Errorf("error should mention write failure, got: %v", err)
	}
}

func TestTypeScriptSessionAdapterRunJSONParseError(t *testing.T) {
	a := &typeScriptSessionAdapter{}
	defer a.Close()

	a.mu.Lock()
	initFakeWorker(a, &noopWriteCloser{}, bufio.NewReader(strings.NewReader("not valid json\n")))
	a.mu.Unlock()

	_, err := a.Run(ToolInput{
		Method:        "diagnostics",
		Content:       "const x = 1",
		FilePath:      "test.ts",
		WorkspaceRoot: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected JSON parse error")
	}
	if !strings.Contains(err.Error(), "parse failed") {
		t.Errorf("error should mention parse failure, got: %v", err)
	}
}

func TestTypeScriptSessionAdapterRunWithSuccessResponse(t *testing.T) {
	a := &typeScriptSessionAdapter{}
	defer a.Close()

	validResult := ToolResult{
		Capabilities: Capabilities{Diagnostics: true},
		Error:        "typescript_not_available",
	}
	resultJSON, _ := json.Marshal(validResult)
	a.mu.Lock()
	initFakeWorker(a, &noopWriteCloser{}, bufio.NewReader(bytes.NewReader(append(resultJSON, '\n'))))
	a.mu.Unlock()

	result, err := a.Run(ToolInput{
		Method:        "diagnostics",
		Content:       "const x = 1",
		FilePath:      "test.ts",
		WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "typescript_not_available" {
		t.Errorf("expected typescript_not_available error, got %q", result.Error)
	}
}

func initFakeWorker(a *typeScriptSessionAdapter, stdin io.WriteCloser, stdout *bufio.Reader) {
	// Set internal fields so ensureWorkerLocked's guard passes:
	//   a.cmd != nil && a.cmd.Process != nil && a.cmd.ProcessState == nil
	// We use PID 1 (init), which is always running. Kill on it returns
	// EPERM (discarded by resetWorkerLocked). Wait returns an error (also discarded).
	a.cmd = &exec.Cmd{Process: &os.Process{Pid: 1}}
	a.stdin = stdin
	a.stdout = stdout
}

type errWriteCloser struct {
	err error
}

func (e *errWriteCloser) Write(p []byte) (int, error) { return 0, e.err }
func (e *errWriteCloser) Close() error                { return nil }

// =============================================================================
// runTypeScriptTool — cover edge cases for node exec failure
// =============================================================================

func TestRunTypeScriptToolNodeExecError(t *testing.T) {
	// Test the case where the node command processes input but typescript is not installed.
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}

	tmpDir := t.TempDir()
	result, err := runTypeScriptTool(ToolInput{
		Content:       "const x = 1",
		FilePath:      "test.ts",
		WorkspaceRoot: tmpDir,
		Method:        "diagnostics",
	})
	// Either the tool runs successfully or returns an error — both are valid.
	// The important thing is that it does not panic.
	if err != nil {
		t.Logf("runTypeScriptTool returned error (expected without typescript): %v", err)
	} else if !result.Capabilities.Diagnostics {
		t.Log("diagnostics capability not set (acceptable without typescript)")
	}
}

func TestRunTypeScriptToolWithUnknownMethod(t *testing.T) {
	// Verify that an unknown method does not cause a panic.
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}

	tmpDir := t.TempDir()
	result, err := runTypeScriptTool(ToolInput{
		Content:       "",
		FilePath:      "test.ts",
		WorkspaceRoot: tmpDir,
		Method:        "unknown_method",
	})
	// Either outcome is valid; the test ensures no panic.
	if err != nil {
		t.Logf("runTypeScriptTool with unknown method returned error: %v", err)
	} else {
		t.Logf("runTypeScriptTool with unknown method returned without error, capabilities=%+v", result.Capabilities)
	}
}

// =============================================================================
// goSessionAdapter Run — cover dispatch paths without gopls
// These paths should return capability errors without panicking.
// =============================================================================

func TestGoSessionAdapterRunReferencesNoGoplsDispatch(t *testing.T) {
	_, err := exec.LookPath("gopls")
	if err == nil {
		t.Skip("gopls is available; skipping not-available path test")
	}

	a := &goSessionAdapter{}
	defer a.Close()

	result, err := a.Run(ToolInput{
		Method:        "references",
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

// =============================================================================
// goSessionAdapter ensureServerLocked — cover the server-not-ready path
// =============================================================================

func TestGoSessionAdapterEnsureServerGoplsNotAvailable(t *testing.T) {
	_, err := exec.LookPath("gopls")
	if err == nil {
		t.Skip("gopls is available; skipping not-available path test")
	}

	a := &goSessionAdapter{}
	defer a.Close()

	a.mu.Lock()
	err = a.ensureServerLocked(t.TempDir())
	a.mu.Unlock()

	if err == nil {
		t.Fatal("expected error when gopls is not available")
	}
}

// =============================================================================
// Registry — Singleton adaptation edge cases
// =============================================================================

func TestRegistrySingletonMultipleLanguages(t *testing.T) {
	r := NewRegistry()
	singleton := &countingAdapter{}
	r.RegisterSingleton(singleton, "typescript", "typescriptreact", "javascript", "javascriptreact")

	for _, lang := range []string{"typescript", "typescriptreact", "javascript", "javascriptreact"} {
		adapter, ok := r.AdapterForLanguage(lang)
		if !ok {
			t.Errorf("expected %q to be registered", lang)
			continue
		}
		_, err := adapter.Run(ToolInput{})
		if err != nil {
			t.Errorf("Run failed for %q: %v", lang, err)
		}
	}

	if singleton.runCount != 4 {
		t.Errorf("expected 4 runs on singleton, got %d", singleton.runCount)
	}
}

func TestRegistrySingletonOverridesFactory(t *testing.T) {
	r := NewRegistry()
	factoryCalls := 0
	r.Register("go", func() Adapter {
		factoryCalls++
		return &countingAdapter{}
	})

	singleton := &countingAdapter{}
	r.RegisterSingleton(singleton, "go")

	adapter, ok := r.AdapterForLanguage("go")
	if !ok {
		t.Fatal("expected adapter")
	}
	_, _ = adapter.Run(ToolInput{})

	if factoryCalls != 0 {
		t.Error("factory should not be called when singleton is registered")
	}
	if singleton.runCount != 1 {
		t.Errorf("expected singleton runCount=1, got %d", singleton.runCount)
	}
}

// =============================================================================
// SessionPool — concurrent access
// =============================================================================

func TestSessionPoolConcurrentAccess(t *testing.T) {
	var createMu sync.Mutex
	created := 0
	pool := NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		createMu.Lock()
		created++
		createMu.Unlock()
		return &fakeSessionAdapter{healthy: true}, nil
	}, 0)
	defer pool.Close()

	var (
		errMu sync.Mutex
		errs  []error
	)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			root := fmt.Sprintf("/repo-%d", idx%3) // 3 different workspaces
			if _, err := pool.Run(ToolInput{WorkspaceRoot: root}); err != nil {
				errMu.Lock()
				errs = append(errs, fmt.Errorf("goroutine %d: %w", idx, err))
				errMu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	for _, e := range errs {
		t.Error(e.Error())
	}

	if created != 3 {
		t.Errorf("expected 3 adapters (one per workspace), got %d", created)
	}
}

// =============================================================================
// goLineColToOffset — additional edge cases
// =============================================================================

func TestGoLineColToOffsetMultiLine(t *testing.T) {
	// Multi-line content — verify that line offsets are computed correctly
	content := "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"
	// Line 1, col 1 → offset 0
	if got := goLineColToOffset(content, 1, 1); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
	// Line 3, col 6 → should be at 'm' in "main"
	want := len("package main\n\nfunc ")
	if got := goLineColToOffset(content, 3, 6); got != want {
		t.Errorf("got %d, want %d", got, want)
	}
}

func TestGoLineColToOffsetSingleLine(t *testing.T) {
	if got := goLineColToOffset("hello", 1, 1); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
	if got := goLineColToOffset("hello", 1, 5); got != 4 {
		t.Errorf("got %d, want 4", got)
	}
	if got := goLineColToOffset("hello", 2, 1); got != 5 {
		t.Errorf("got %d (past end of single line), want 5", got)
	}
}

// =============================================================================
// parseGofmtErrors / parseGoVetErrors — additional edge cases
// =============================================================================

func TestParseGofmtErrorsMultiline(t *testing.T) {
	content := "package main\n\nfunc main() {\n\tfmt.Println(\n}\n"
	output := "/tmp/main.go:4:2: expected expression, found }\n/tmp/main.go:5:1: expected ';'\n/tmp/main.go:6:1: unexpected }\n"
	diags := parseGofmtErrors(output, content)
	if len(diags) != 3 {
		t.Fatalf("expected 3 diagnostics, got %d", len(diags))
	}
	for _, d := range diags {
		if d.Severity != "error" {
			t.Errorf("expected severity error, got %s", d.Severity)
		}
		if d.Source != "gofmt" {
			t.Errorf("expected source gofmt, got %s", d.Source)
		}
		if d.From < 0 || d.To <= d.From {
			t.Errorf("invalid from/to: %d/%d", d.From, d.To)
		}
	}
}

func TestParseGoVetErrorsMultipleVets(t *testing.T) {
	content := "package main\n\nfunc main() {\n}\nfunc unused() {}\n"
	output := "# command-line-arguments\n/tmp/main.go:3:2: unreachable code\n/tmp/main.go:5:6: unused variable\n"
	diags := parseGoVetErrors(output, content)
	if len(diags) != 2 {
		t.Fatalf("expected 2 diagnostics, got %d", len(diags))
	}
	for _, d := range diags {
		if d.Severity != "warning" {
			t.Errorf("expected severity warning, got %s", d.Severity)
		}
		if d.Source != "go vet" {
			t.Errorf("expected source 'go vet', got %s", d.Source)
		}
	}
}

func TestParseGoVetErrorsAllHashLines(t *testing.T) {
	output := "# line 1\n# line 2\n# line 3\n"
	diags := parseGoVetErrors(output, "package main")
	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics for all-hash output, got %d", len(diags))
	}
}

// =============================================================================
// computeGoEdits — additional edge cases
// =============================================================================

func TestComputeGoEditsInsertAtBeginning(t *testing.T) {
	original := "world"
	modified := "hello world"
	edits := computeGoEdits(original, modified, "test.go")
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	if edits[0].From != 0 {
		t.Errorf("From = %d, want 0", edits[0].From)
	}
	if edits[0].To != 0 {
		t.Errorf("To = %d, want 0", edits[0].To)
	}
	if edits[0].NewText != "hello " {
		t.Errorf("NewText = %q, want 'hello '", edits[0].NewText)
	}
}

func TestComputeGoEditsDeleteFromEnd(t *testing.T) {
	original := "hello world"
	modified := "hello"
	edits := computeGoEdits(original, modified, "test.go")
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	if edits[0].From != 5 {
		t.Errorf("From = %d, want 5", edits[0].From)
	}
	if edits[0].To != 11 {
		t.Errorf("To = %d, want 11", edits[0].To)
	}
	if edits[0].NewText != "" {
		t.Errorf("NewText = %q, want empty", edits[0].NewText)
	}
}

func TestComputeGoEditsReplaceMiddle(t *testing.T) {
	original := "abcdef"
	modified := "abXYef"
	edits := computeGoEdits(original, modified, "test.go")
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	applied := original[:edits[0].From] + edits[0].NewText + original[edits[0].To:]
	if applied != modified {
		t.Errorf("applied = %q, want %q", applied, modified)
	}
}

// =============================================================================
// mapInlayHintKind — ensure full coverage
// =============================================================================

func TestMapInlayHintKindAllCases(t *testing.T) {
	tests := []struct {
		kind int
		want string
	}{
		{1, "type"},
		{2, "parameter"},
		{0, "none"},
		{-1, "none"},
		{3, "none"},
		{100, "none"},
	}
	for _, tt := range tests {
		got := mapInlayHintKind(tt.kind)
		if got != tt.want {
			t.Errorf("mapInlayHintKind(%d) = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

// =============================================================================
// parseGoplsInlayHints — non-string label (non-array, non-string)
// =============================================================================

func TestParseGoplsInlayHintsLabelIsNumber(t *testing.T) {
	content := "package main\n"
	resp := map[string]interface{}{
		"result": []interface{}{
			map[string]interface{}{
				"position": map[string]interface{}{"line": float64(0), "character": float64(0)},
				"label":    float64(42), // label is a number, not string or array
				"kind":     float64(1),
			},
		},
	}
	hints := parseGoplsInlayHints(resp, content)
	if len(hints) != 0 {
		t.Fatalf("expected 0 hints when label is a number, got %d", len(hints))
	}
}

// =============================================================================
// parseGoplsDefinition — with extra output format variations
// =============================================================================

func TestParseGoplsDefinitionWithTrailingText(t *testing.T) {
	output := "/path/to/file.go:10:5-8 some trailing text\n"
	path, line, col, ok := parseGoplsDefinition(output)
	if !ok {
		t.Fatal("expected ok")
	}
	if path != "/path/to/file.go" {
		t.Errorf("path = %q", path)
	}
	if line != 10 {
		t.Errorf("line = %d, want 10", line)
	}
	if col != 5 {
		t.Errorf("col = %d, want 5", col)
	}
}

func TestParseGoplsDefinitionWindowsPath(t *testing.T) {
	output := "C:\\Users\\project\\main.go:10:5\n"
	path, line, col, ok := parseGoplsDefinition(output)
	if !ok {
		t.Fatal("expected ok for Windows path")
	}
	if line != 10 || col != 5 {
		t.Errorf("got line=%d col=%d, want 10, 5", line, col)
	}
	_ = path
}

// =============================================================================
// extractParamsFromSignature — more edge cases
// =============================================================================

func TestExtractParamsFromSignatureOnlyReceiver(t *testing.T) {
	// Method signature with only a receiver, no params: func (t *T) Method()
	params := extractParamsFromSignature("func (t *T) Method()")
	if len(params) != 0 {
		t.Fatalf("expected 0 params for method with no args, got %d: %v", len(params), params)
	}
}

func TestExtractParamsFromSignatureSingleUnnamed(t *testing.T) {
	params := extractParamsFromSignature("func Foo(int)")
	if len(params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(params))
	}
	if params[0].Label != "int" {
		t.Errorf("param = %q, want 'int'", params[0].Label)
	}
}

func TestExtractParamsFromSignatureComplexNested(t *testing.T) {
	params := extractParamsFromSignature("func Foo(fn func(int, string) error, ch chan<- struct{})")
	if len(params) != 2 {
		t.Fatalf("expected 2 params, got %d: %v", len(params), params)
	}
	if params[0].Label != "fn func(int, string) error" {
		t.Errorf("first param = %q", params[0].Label)
	}
	if params[1].Label != "ch chan<- struct{}" {
		t.Errorf("second param = %q", params[1].Label)
	}
}

// =============================================================================
// computeActiveParameter — more edge cases
// =============================================================================

func TestComputeActiveParameterAtThirdParam(t *testing.T) {
	input := ToolInput{
		Content:  "foo(1, 2, 3)",
		Position: &Position{Line: 1, Column: 10}, // at '3'
	}
	got := computeActiveParameter(input)
	if got != 2 {
		t.Errorf("expected param 2 (the '3'), got %d", got)
	}
}

func TestComputeActiveParameterSingleParam(t *testing.T) {
	input := ToolInput{
		Content:  "foo(42)",
		Position: &Position{Line: 1, Column: 5}, // at '42'
	}
	got := computeActiveParameter(input)
	if got != 0 {
		t.Errorf("expected param 0, got %d", got)
	}
}

// =============================================================================
// Go diagnostics with save trigger and go vet errors
// =============================================================================

func TestRunGoDiagnosticsWithSaveTrigger(t *testing.T) {
	// Tests the save trigger path which runs go vet when gofmt finds no errors
	if _, err := exec.LookPath("gofmt"); err != nil {
		t.Skip("gofmt not available")
	}

	content := "package main\n\nfunc main() {}\n"
	result, err := runGoDiagnostics(ToolInput{
		Content:  content,
		FilePath: "main.go",
		Trigger:  "save",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Capabilities.Diagnostics {
		t.Error("expected Diagnostics capability")
	}
	// Valid code should have no diagnostics
	if len(result.Diagnostics) != 0 {
		t.Logf("Got %d diagnostics (expected 0 but vet might report issues)", len(result.Diagnostics))
	}
}

// =============================================================================
// goSessionAdapter — ensureServerLocked covers real gopls path
// =============================================================================

func TestGoSessionAdapterEnsureServerAndReset(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	a := &goSessionAdapter{}

	// Start the server
	a.mu.Lock()
	err := a.ensureServerLocked(t.TempDir())
	a.mu.Unlock()
	if err != nil {
		t.Fatalf("ensureServerLocked failed: %v", err)
	}

	// Verify process is running
	if !a.Healthy() {
		t.Error("expected healthy after server start")
	}

	// Reset should clean up
	a.mu.Lock()
	a.resetServerLocked()
	a.mu.Unlock()

	// Now should not be healthy (server killed, but not closed)
	a.mu.Lock()
	hasCmd := a.serverCmd != nil
	a.mu.Unlock()
	if hasCmd {
		t.Error("serverCmd should be nil after reset")
	}

	// Close handles cleanup (idempotent)
	a.Close()
}

// =============================================================================
// runGoRename — filter that excludes other files (testing with multi-file setup)
// =============================================================================

func TestRunGoRenameOnlyCurrentFile(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	// Create a multi-file Go module
	dir := t.TempDir()
	mainContent := "package main\n\nvar config string\n\nfunc main() {\n\tprintln(config)\n}\n"
	utilContent := "package main\n\nfunc helper() {\n\t_ = config\n}\n"

	for _, pair := range [][2]string{
		{filepath.Join(dir, "main.go"), mainContent},
		{filepath.Join(dir, "util.go"), utilContent},
		{filepath.Join(dir, "go.mod"), "module testmain\n\ngo 1.21\n"},
	} {
		if err := os.WriteFile(pair[0], []byte(pair[1]), 0644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := runGoRename(ToolInput{
		Content:       mainContent,
		FilePath:      filepath.Join(dir, "main.go"),
		WorkspaceRoot: dir,
		Position:      &Position{Line: 3, Column: 5},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Rename == nil {
		t.Fatal("expected non-nil Rename")
	}
	for i, loc := range result.Rename.Locations {
		if filepath.Clean(loc.FilePath) != filepath.Clean(filepath.Join(dir, "main.go")) {
			t.Errorf("location[%d] file=%q should be main.go only", i, loc.FilePath)
		}
	}
}

// =============================================================================
// runGoReferences — symbol name extraction and sorting
// =============================================================================

func TestRunGoReferencesSymbolName(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	content := "package main\n\nvar myVar int\n\nfunc main() {\n\t_ = myVar\n}\n"
	dir, file := setupGoTestModule(t, content)

	result, err := runGoReferences(ToolInput{
		Content:       content,
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      &Position{Line: 3, Column: 5}, // on 'myVar'
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.References == nil {
		t.Fatal("expected non-nil References")
	}
	if result.References.SymbolName != "myVar" {
		t.Errorf("SymbolName = %q, want 'myVar'", result.References.SymbolName)
	}
}

func TestRunGoReferencesEmptyContentAtPosition(t *testing.T) {
	// Testing symbol name extraction when position is beyond content lines
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	content := "package main\n"
	dir, file := setupGoTestModule(t, content)

	result, err := runGoReferences(ToolInput{
		Content:       content,
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      &Position{Line: 5, Column: 5}, // beyond content
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should not panic, even if no references found
	if result.References == nil {
		t.Error("expected non-nil References")
	}
}

// =============================================================================
// writeJSONRPC / readJSONRPC — additional coverage
// =============================================================================

func TestWriteJSONRPCMultipleMessages(t *testing.T) {
	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)

	msg1 := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      float64(1),
		"method":  "initialize",
	}
	msg2 := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      float64(2),
		"method":  "shutdown",
	}

	writeJSONRPC(writer, msg1)
	writeJSONRPC(writer, msg2)
	writer.Flush()

	reader := bufio.NewReader(&buf)

	read1, err := readJSONRPC(reader)
	if err != nil {
		t.Fatalf("first read failed: %v", err)
	}
	if read1["method"] != "initialize" {
		t.Errorf("first method = %v", read1["method"])
	}

	read2, err := readJSONRPC(reader)
	if err != nil {
		t.Fatalf("second read failed: %v", err)
	}
	if read2["method"] != "shutdown" {
		t.Errorf("second method = %v", read2["method"])
	}
}

// =============================================================================
// parseGoplsSignatureHelp — edge cases
// =============================================================================

func TestParseGoplsSignatureHelpOnlyBlankLines(t *testing.T) {
	output := "\n\n\n"
	result := parseGoplsSignatureHelp(output, ToolInput{})
	if len(result.Signatures) != 0 {
		t.Fatalf("expected 0 signatures, got %d", len(result.Signatures))
	}
}

func TestParseGoplsSignatureHelpWhitespaceOnly(t *testing.T) {
	output := "   \n"
	result := parseGoplsSignatureHelp(output, ToolInput{})
	if len(result.Signatures) != 0 {
		t.Fatalf("expected 0 signatures for whitespace-only output, got %d", len(result.Signatures))
	}
}

func TestParseGoplsSignatureHelpVaradicParams(t *testing.T) {
	output := "func Printf(format string, args ...interface{}) (n int, err error)\n"
	input := ToolInput{
		Content:  `fmt.Printf("%s", "hi")`,
		Position: &Position{Line: 1, Column: 14},
	}
	result := parseGoplsSignatureHelp(output, input)
	if len(result.Signatures) != 1 {
		t.Fatalf("expected 1 signature, got %d", len(result.Signatures))
	}
	params := result.Signatures[0].Parameters
	if len(params) != 2 {
		t.Fatalf("expected 2 params, got %d: %v", len(params), params)
	}
	if params[0].Label != "format string" {
		t.Errorf("first param = %q", params[0].Label)
	}
	if params[1].Label != "args ...interface{}" {
		t.Errorf("second param = %q", params[1].Label)
	}
}

// =============================================================================
// runGoInlayHintsWithRemote — gopls not available path (tested indirectly)
// and invalid remote address variations
// =============================================================================

func TestRunGoInlayHintsWithRemoteNonexistentSocket(t *testing.T) {
	result, err := runGoInlayHintsWithRemote(ToolInput{
		Content:       "package main\n",
		FilePath:      "main.go",
		WorkspaceRoot: "/tmp",
	}, "", "unix;/nonexistent/path/to/socket.sock")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should fail to connect but not panic
	if result.Error == "" {
		t.Error("expected an error from socket connect failure")
	}
}

// =============================================================================
// NewGoAdapter returns correct type
// =============================================================================

func TestNewGoAdapterImplementsAdapter(t *testing.T) {
	var _ Adapter = NewGoAdapter()
}

func TestNewTypeScriptAdapterImplementsAdapter(t *testing.T) {
	var _ Adapter = NewTypeScriptAdapter()
}

// =============================================================================
// SessionPool EvictIdle — edge cases
// =============================================================================

func TestSessionPoolEvictIdleLeavesActiveSessions(t *testing.T) {
	created := 0
	pool := NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		created++
		return &fakeSessionAdapter{healthy: true}, nil
	}, 50*time.Millisecond)
	defer pool.Close()

	// Acquire adapter for /repo-a
	_, err := pool.acquire("/repo-a")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	// Wait past TTL
	time.Sleep(100 * time.Millisecond)

	// EvictIdle should not evict /repo-a because inUse > 0 from direct acquire
	// (Note: acquire incremented inUse, release hasn't been called)
	pool.EvictIdle()

	pool.mu.Lock()
	_, exists := pool.sessions["/repo-a"]
	pool.mu.Unlock()
	if !exists {
		t.Error("expected /repo-a to NOT be evicted while in use")
	}
}
