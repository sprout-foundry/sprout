//go:build !js

package webui

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/validation"
)

func TestParseGofmtError_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		msg      string
		wantLine int
		wantCol  int
		wantOK   bool
	}{
		{
			name:     "standard input format",
			msg:      "<standard input>:42:5: expected declaration, found 'fmt'",
			wantLine: 42,
			wantCol:  5,
			wantOK:   true,
		},
		{
			name:     "stdin shorthand",
			msg:      "<stdin>:10:2: expected 'package'",
			wantLine: 10,
			wantCol:  2,
			wantOK:   true,
		},
		{
			name:     "with syntax error prefix",
			msg:      "syntax error: <standard input>:7:12: missing import path",
			wantLine: 7,
			wantCol:  12,
			wantOK:   true,
		},
		{
			name:     "empty string",
			msg:      "",
			wantLine: 0,
			wantCol:  0,
			wantOK:   false,
		},
		{
			name:     "no colons at all",
			msg:      "just some error text",
			wantLine: 0,
			wantCol:  0,
			wantOK:   false,
		},
		{
			name:     "single colon only",
			msg:      "something:else",
			wantLine: 0,
			wantCol:  0,
			wantOK:   false,
		},
		{
			name:     "non-numeric line",
			msg:      "<stdin>:abc:5: error",
			wantLine: 0,
			wantCol:  0,
			wantOK:   false,
		},
		{
			name:     "non-numeric column",
			msg:      "<stdin>:10:xyz: error",
			wantLine: 0,
			wantCol:  0,
			wantOK:   false,
		},
		{
			name:     "line 1 col 1",
			msg:      "<stdin>:1:1: error message",
			wantLine: 1,
			wantCol:  1,
			wantOK:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			line, col, ok := parseGofmtError(tt.msg)
			if ok != tt.wantOK {
				t.Errorf("parseGofmtError(%q): ok = %v; want %v", tt.msg, ok, tt.wantOK)
			}
			if line != tt.wantLine {
				t.Errorf("parseGofmtError(%q): line = %d; want %d", tt.msg, line, tt.wantLine)
			}
			if col != tt.wantCol {
				t.Errorf("parseGofmtError(%q): col = %d; want %d", tt.msg, col, tt.wantCol)
			}
		})
	}
}

func TestLineColToOffsets_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		line     int
		col      int
		content  string
		wantFrom int
	}{
		{
			name:     "line 1 col 1 on single line",
			line:     1,
			col:      1,
			content:  "hello world",
			wantFrom: 0,
		},
		{
			name:     "line 1 col 1 on multiline",
			line:     1,
			col:      1,
			content:  "hello\nworld",
			wantFrom: 0,
		},
		{
			name:     "line 2 col 1",
			line:     2,
			col:      1,
			content:  "hello\nworld",
			wantFrom: 6,
		},
		{
			name:     "line 1 col 7 (start of world)",
			line:     1,
			col:      7,
			content:  "hello world",
			wantFrom: 6,
		},
		{
			name:     "line 0 clamped to 1",
			line:     0,
			col:      1,
			content:  "hello",
			wantFrom: 0,
		},
		{
			name:     "col 0 clamped to 1",
			line:     1,
			col:      0,
			content:  "hello",
			wantFrom: 0,
		},
		{
			name:     "line beyond content",
			line:     100,
			col:      1,
			content:  "hello",
			wantFrom: 5,
		},
		{
			name:     "empty content",
			line:     1,
			col:      1,
			content:  "",
			wantFrom: 0,
		},
		{
			name:     "col beyond line length",
			line:     1,
			col:      100,
			content:  "short",
			wantFrom: 5,
		},
		{
			name:     "negative line",
			line:     -1,
			col:      1,
			content:  "hello",
			wantFrom: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			from, to := lineColToOffsets(tt.line, tt.col, tt.content)
			if from != tt.wantFrom {
				t.Errorf("lineColToOffsets(%d, %d, %q): from = %d; want %d", tt.line, tt.col, tt.content, from, tt.wantFrom)
			}
			// to must be >= from
			if to < from {
				t.Errorf("lineColToOffsets(%d, %d, %q): to (%d) < from (%d)", tt.line, tt.col, tt.content, to, from)
			}
		})
	}
}

func TestExtendToTokenEnd_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		content    string
		byteOffset int
		want       int
	}{
		{
			name:       "middle of identifier",
			content:    "package main",
			byteOffset: 2,
			want:       7, // "package" ends at 7
		},
		{
			name:       "start of identifier",
			content:    "package main",
			byteOffset: 0,
			want:       7,
		},
		{
			name:       "at space (delimiter)",
			content:    "package main",
			byteOffset: 7,
			want:       8, // space is delimiter, so extend by 1
		},
		{
			name:       "beyond content length",
			content:    "hello",
			byteOffset: 10,
			want:       10,
		},
		{
			name:       "negative offset clamped to 0",
			content:    "hello",
			byteOffset: -1,
			want:       5, // extends to end of "hello"
		},
		{
			name:       "empty content with 0 offset",
			content:    "",
			byteOffset: 0,
			want:       0,
		},
		{
			name:       "at end of content",
			content:    "hello",
			byteOffset: 5,
			want:       5,
		},
		{
			name:       "middle of word in sentence",
			content:    "var x = 42",
			byteOffset: 4, // at 'x'
			want:       5, // 'x' is followed by space
		},
		{
			name:       "at equals sign",
			content:    "var x = 42",
			byteOffset: 6, // at '='
			want:       7, // '=' is delimiter, extend by 1
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extendToTokenEnd(tt.content, tt.byteOffset)
			if got != tt.want {
				t.Errorf("extendToTokenEnd(%q, %d) = %d; want %d", tt.content, tt.byteOffset, got, tt.want)
			}
		})
	}
}

func TestIsExtDelimiter_ZC(t *testing.T) {
	t.Parallel()
	delimiters := []rune{
		' ', '\t', '\n', '\r',
		'(', ')', '{', '}', '[', ']',
		',', ';', ':', '+', '-', '*', '/',
		'=', '!', '<', '>', '&', '|', '^', '%',
		'"', '\'',
	}
	for _, ch := range delimiters {
		t.Run(string(ch), func(t *testing.T) {
			t.Parallel()
			if !isExtDelimiter(ch) {
				t.Errorf("isExtDelimiter(%c) = false; want true", ch)
			}
		})
	}

	nonDelimiters := []rune{
		'a', 'z', 'A', 'Z',
		'0', '9',
		'.', '_', '#', '@', '$',
		'~', '`', '\\', '?',
	}
	for _, ch := range nonDelimiters {
		t.Run(string(ch), func(t *testing.T) {
			t.Parallel()
			if isExtDelimiter(ch) {
				t.Errorf("isExtDelimiter(%c) = true; want false", ch)
			}
		})
	}
}

func TestValidationToFrontend_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		d            validation.Diagnostic
		content      string
		wantSeverity string
		wantMessage  string
		wantSource   string
	}{
		{
			name: "simple diagnostic",
			d: validation.Diagnostic{
				Path:     "file.go",
				Line:     10,
				Column:   5,
				Severity: "error",
				Message:  "unexpected token",
				Source:   "gofmt",
			},
			content:      "package main\n\nvar x = 10",
			wantSeverity: "error",
			wantMessage:  "unexpected token",
			wantSource:   "gofmt",
		},
		{
			name: "goimports spans entire file",
			d: validation.Diagnostic{
				Path:     "file.go",
				Line:     1,
				Column:   1,
				Severity: "warning",
				Message:  "unused import",
				Source:   "goimports",
			},
			content:      "package main\nimport \"unused\"",
			wantSeverity: "warning",
			wantMessage:  "unused import",
			wantSource:   "goimports",
		},
		{
			name: "empty diagnostic",
			d: validation.Diagnostic{
				Path:     "",
				Line:     0,
				Column:   0,
				Severity: "",
				Message:  "",
				Source:   "",
			},
			content:      "hello",
			wantSeverity: "",
			wantMessage:  "",
			wantSource:   "",
		},
		{
			name: "gofmt with parseable error message",
			d: validation.Diagnostic{
				Path:     "file.go",
				Line:     0,
				Column:   0,
				Severity: "error",
				Message:  "syntax error: <standard input>:3:2: expected declaration",
				Source:   "gofmt",
			},
			content:      "line1\nline2\nline3",
			wantSeverity: "error",
			wantMessage:  "syntax error: <standard input>:3:2: expected declaration",
			wantSource:   "gofmt",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := validationToFrontend(tt.d, tt.content)
			if got.Severity != tt.wantSeverity {
				t.Errorf("validationToFrontend: Severity = %q; want %q", got.Severity, tt.wantSeverity)
			}
			if got.Message != tt.wantMessage {
				t.Errorf("validationToFrontend: Message = %q; want %q", got.Message, tt.wantMessage)
			}
			if got.Source != tt.wantSource {
				t.Errorf("validationToFrontend: Source = %q; want %q", got.Source, tt.wantSource)
			}
			// Verify offsets are valid: From <= To <= len(content)
			if got.From > got.To {
				t.Errorf("validationToFrontend: From (%d) > To (%d)", got.From, got.To)
			}
			if got.To > len(tt.content) {
				t.Errorf("validationToFrontend: To (%d) > len(content) (%d)", got.To, len(tt.content))
			}
		})
	}
}

func TestValidationToFrontend_Offsets_ZC(t *testing.T) {
	t.Parallel()
	// Verify that offsets are actually computed correctly
	content := "line1\nline2\nline3"
	d := validation.Diagnostic{
		Line:     0,
		Column:   0,
		Severity: "error",
		Message:  "syntax error: <standard input>:3:2: expected declaration",
		Source:   "gofmt",
	}
	got := validationToFrontend(d, content)
	// Line 3, col 2: line1\n = 6 bytes, line2\n = 6 bytes, so offset = 12 + 1 = 13
	// But actually: line 3 starts at offset 12, col 2 = offset 13
	if got.From != 13 {
		t.Errorf("From = %d; want 13 (line 3, col 2 in %q)", got.From, content)
	}
}

func TestDiagnosticToOffsets_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		d        validation.Diagnostic
		content  string
		wantFrom int
	}{
		{
			name: "goimports line=1 col=1 spans entire file",
			d: validation.Diagnostic{
				Line:    1,
				Column:  1,
				Source:  "goimports",
				Message: "unused import",
			},
			content:  "package main\nimport \"fmt\"",
			wantFrom: 0,
		},
		{
			name: "gofmt with parseable error",
			d: validation.Diagnostic{
				Line:    0,
				Column:  0,
				Source:  "gofmt",
				Message: "syntax error: <standard input>:2:5: expected token",
			},
			content:  "line1\nline2\nline3",
			wantFrom: 10, // line 2 start=6, col 5 → 6+4=10
		},
		{
			name: "fallback uses diagnostic line/col",
			d: validation.Diagnostic{
				Line:    2,
				Column:  1,
				Source:  "some-other",
				Message: "some error",
			},
			content:  "line1\nline2",
			wantFrom: 6, // line 2, col 1 = offset 6
		},
		{
			name: "line=0 col=0 non-gofmt falls back to entire content",
			d: validation.Diagnostic{
				Line:    0,
				Column:  0,
				Source:  "unknown",
				Message: "error",
			},
			content:  "hello",
			wantFrom: 0,
		},
		{
			name: "goimports with non-1/1 falls through to line/col",
			d: validation.Diagnostic{
				Line:    2,
				Column:  3,
				Source:  "goimports",
				Message: "error",
			},
			content:  "line1\nline2\nline3",
			wantFrom: 8, // line 2 start=6, col 3 → 6+2=8
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			from, to := diagnosticToOffsets(tt.d, tt.content)
			if from != tt.wantFrom {
				t.Errorf("diagnosticToOffsets: from = %d; want %d", from, tt.wantFrom)
			}
			// to must be >= from and <= len(content)
			if to < from {
				t.Errorf("diagnosticToOffsets: to (%d) < from (%d)", to, from)
			}
			if to > len(tt.content) {
				t.Errorf("diagnosticToOffsets: to (%d) > len(content) (%d)", to, len(tt.content))
			}
		})
	}
}

func TestSanitizePathComponent_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "simple alphanumeric",
			input: "main",
			want:  "main",
		},
		{
			name:  "with hyphens and underscores",
			input: "my-branch_v2",
			want:  "my-branch_v2",
		},
		{
			name:  "with dots",
			input: "feature.v2.1",
			want:  "feature.v2.1",
		},
		{
			name:  "slashes replaced",
			input: "feature/new-branch",
			want:  "feature_new-branch",
		},
		{
			name:  "spaces replaced",
			input: "my branch",
			want:  "my_branch",
		},
		{
			name:  "special characters replaced",
			input: "branch@name!test",
			want:  "branch_name_test",
		},
		{
			name:  "unicode replaced",
			input: "分支",
			want:  "__",
		},
		{
			name:  "mixed safe and unsafe",
			input: "feat/add-auth_2024",
			want:  "feat_add-auth_2024",
		},
		{
			name:  "only special chars",
			input: "!@#$%",
			want:  "_____",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := sanitizePathComponent(tt.input)
			if got != tt.want {
				t.Errorf("sanitizePathComponent(%q) = %q; want %q", tt.input, got, tt.want)
			}
		})
	}
}
