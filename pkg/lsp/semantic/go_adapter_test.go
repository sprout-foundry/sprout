package semantic

import (
	"bufio"
	"bytes"
	"os/exec"
	"strings"
	"testing"
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
