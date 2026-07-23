package tools

import (
	"errors"
	"strings"
	"testing"
)

func TestEnsureOllamaModelTag_ZC(t *testing.T) {
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
			name:  "whitespace only",
			input: "   ",
			want:  "",
		},
		{
			name:  "no tag adds latest",
			input: "llama3",
			want:  "llama3:latest",
		},
		{
			name:  "already has tag",
			input: "llama3:v1",
			want:  "llama3:v1",
		},
		{
			name:  "trims spaces before adding tag",
			input: "  glm-ocr  ",
			want:  "glm-ocr:latest",
		},
		{
			name:  "complex model name with tag",
			input: "meta-llama/Llama-3.2:2024",
			want:  "meta-llama/Llama-3.2:2024",
		},
		{
			name:  "single word no tag",
			input: "glm-ocr",
			want:  "glm-ocr:latest",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := EnsureOllamaModelTag(tt.input)
			if got != tt.want {
				t.Errorf("EnsureOllamaModelTag(%q) = %q; want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSanitizeVisionFileComponent_ZC(t *testing.T) {
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
			name:  "whitespace only",
			input: "   ",
			want:  "",
		},
		{
			name:  "simple lowercase",
			input: "hello",
			want:  "hello",
		},
		{
			name:  "lowercase with digits",
			input: "file123",
			want:  "file123",
		},
		{
			name:  "uppercase converted to lowercase",
			input: "HelloWorld",
			want:  "helloworld",
		},
		{
			name:  "spaces replaced with underscore",
			input: "hello world",
			want:  "hello_world",
		},
		{
			name:  "special chars replaced",
			input: "my-file_name.docx",
			want:  "my_file_name_docx",
		},
		{
			name:  "slashes replaced",
			input: "path/to/file",
			want:  "path_to_file",
		},
		{
			name:  "leading underscores trimmed",
			input: "  hello  ",
			want:  "hello",
		},
		{
			name:  "trailing underscores trimmed",
			input: "hello___",
			want:  "hello",
		},
		{
			name:  "trim underscore from front only",
			input: "_hello",
			want:  "hello",
		},
		{
			name:  "truncated to 64 chars",
			input: "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789",
			want:  "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz01",
		},
		{
			name:  "exactly 64 chars after processing",
			input: "0123456789abcdefghijklmnop0123456789abcdefghijklmnop01234567",
			want:  "0123456789abcdefghijklmnop0123456789abcdefghijklmnop01234567",
		},
		{
			name:  "all special chars becomes empty",
			input: "!@#$%^&*()",
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := sanitizeVisionFileComponent(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeVisionFileComponent(%q) = %q; want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestClassifyPDFProcessingErrorCode_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "nil error returns PDFProcessingFailed",
			err:  nil,
			want: ErrCodePDFProcessingFailed,
		},
		{
			name: "download pdf",
			err:  errors.New("download PDF: connection refused"),
			want: ErrCodeRemoteFetchFailed,
		},
		{
			name: "status 404",
			err:  errors.New("HTTP status 404"),
			want: ErrCodeRemoteFetchFailed,
		},
		{
			name: "status 403",
			err:  errors.New("HTTP status 403"),
			want: ErrCodeRemoteFetchFailed,
		},
		{
			name: "status 401",
			err:  errors.New("HTTP status 401"),
			want: ErrCodeRemoteFetchFailed,
		},
		{
			name: "stat pdf file",
			err:  errors.New("stat PDF file: not found"),
			want: ErrCodeLocalFileNotFound,
		},
		{
			name: "no such file or directory",
			err:  errors.New("open test.pdf: no such file or directory"),
			want: ErrCodeLocalFileNotFound,
		},
		{
			name: "ocr request",
			err:  errors.New("OCR request: server error"),
			want: ErrCodeVisionRequestFailed,
		},
		{
			name: "http 5 error",
			err:  errors.New("HTTP 500 Internal Server Error"),
			want: ErrCodeVisionRequestFailed,
		},
		{
			name: "http 4 error",
			err:  errors.New("HTTP 400 Bad Request"),
			want: ErrCodeVisionRequestFailed,
		},
		{
			name: "timeout",
			err:  errors.New("request timeout after 30s"),
			want: ErrCodeVisionRequestFailed,
		},
		{
			name: "connection reset",
			err:  errors.New("connection reset by peer"),
			want: ErrCodeVisionRequestFailed,
		},
		{
			name: "create vision client",
			err:  errors.New("create vision client: no providers"),
			want: ErrCodeVisionRequestFailed,
		},
		{
			name: "no response from ocr model",
			err:  errors.New("no response from OCR model"),
			want: ErrCodeVisionRequestFailed,
		},
		{
			name: "missing %pdf header",
			err:  errors.New("missing %PDF header"),
			want: ErrCodeInputUnsupported,
		},
		{
			name: "not a valid pdf",
			err:  errors.New("not a valid PDF document"),
			want: ErrCodeInputUnsupported,
		},
		{
			name: "unknown error falls to default",
			err:  errors.New("some random PDF error"),
			want: ErrCodePDFProcessingFailed,
		},
		{
			name: "empty error message",
			err:  errors.New(""),
			want: ErrCodePDFProcessingFailed,
		},
		{
			name: "mixed case No Such File",
			err:  errors.New("No Such File Or Directory"),
			want: ErrCodeLocalFileNotFound,
		},
		{
			name: "mixed case Missing PDF Header",
			err:  errors.New("Missing %PDF Header"),
			want: ErrCodeInputUnsupported,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := classifyPDFProcessingErrorCode(tt.err)
			if got != tt.want {
				t.Errorf("classifyPDFProcessingErrorCode(%v) = %q; want %q", tt.err, got, tt.want)
			}
		})
	}
}

func TestLimitVisionOutputText_ZC(t *testing.T) {
	// Ensure the environment variable does not interfere with the test.
	// getVisionMaxReturnedTextChars reads VISION_MAX_TEXT_CHARS via
	// configuration.GetEnvSimple, which checks both SPROUT_* and SPROUT_* prefixes.
	// NOTE: This test cannot use t.Parallel() because it modifies process-level
	// environment variables, which would be visible to other parallel tests.
	t.Setenv("SPROUT_VISION_MAX_TEXT_CHARS", "")
	t.Setenv("SPROUT_VISION_MAX_TEXT_CHARS", "")

	maxChars := getVisionMaxReturnedTextChars()
	tests := []struct {
		name            string
		text            string
		wantTruncated   bool
		wantOriginalLen int
		wantStartsWith  string
		wantEndsWith    string
	}{
		{
			name:            "empty string",
			text:            "",
			wantTruncated:   false,
			wantOriginalLen: 0,
		},
		{
			name:            "whitespace only",
			text:            "   ",
			wantTruncated:   false,
			wantOriginalLen: 0,
		},
		{
			name:            "short text unchanged",
			text:            "hello world",
			wantTruncated:   false,
			wantOriginalLen: 11,
			wantStartsWith:  "hello world",
			wantEndsWith:    "hello world",
		},
		{
			name:            "exact max length unchanged",
			text:            strings.Repeat("x", maxChars),
			wantTruncated:   false,
			wantOriginalLen: maxChars,
		},
		{
			name:            "long text truncated",
			text:            strings.Repeat("x", maxChars+1000),
			wantTruncated:   true,
			wantOriginalLen: maxChars + 1000,
			wantEndsWith:    "]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, truncated, originalLen := limitVisionOutputText(tt.text)
			if truncated != tt.wantTruncated {
				t.Errorf("limitVisionOutputText: truncated = %v; want %v", truncated, tt.wantTruncated)
			}
			if originalLen != tt.wantOriginalLen {
				t.Errorf("limitVisionOutputText: originalLen = %d; want %d", originalLen, tt.wantOriginalLen)
			}
			if tt.wantStartsWith != "" && !strings.HasPrefix(got, tt.wantStartsWith) {
				t.Errorf("limitVisionOutputText: result should start with %q", tt.wantStartsWith)
			}
			if tt.wantEndsWith != "" && !strings.HasSuffix(got, tt.wantEndsWith) {
				t.Errorf("limitVisionOutputText: result should end with %q, got %q", tt.wantEndsWith, got)
			}
		})
	}
}

func TestGetFileExtension_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "simple file",
			path: "document.pdf",
			want: ".pdf",
		},
		{
			name: "uppercase extension",
			path: "image.PNG",
			want: ".png",
		},
		{
			name: "mixed case extension",
			path: "Image.JpEg",
			want: ".jpeg",
		},
		{
			name: "no extension",
			path: "Makefile",
			want: "",
		},
		{
			name: "path with directory",
			path: "/home/user/docs/report.PDF",
			want: ".pdf",
		},
		{
			name: "dot file",
			path: ".gitignore",
			want: ".gitignore",
		},
		{
			name: "empty path",
			path: "",
			want: "",
		},
		{
			name: "multiple dots",
			path: "archive.tar.gz",
			want: ".gz",
		},
		{
			name: "windows path",
			path: "C:\\Users\\file.TXT",
			want: ".txt",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := GetFileExtension(tt.path)
			if got != tt.want {
				t.Errorf("GetFileExtension(%q) = %q; want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestGetBaseName_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "simple file",
			path: "document.pdf",
			want: "document.pdf",
		},
		{
			name: "full path",
			path: "/home/user/docs/report.pdf",
			want: "report.pdf",
		},
		{
			name: "relative path",
			path: "../sibling/file.go",
			want: "file.go",
		},
		{
			name: "trailing slash dir",
			path: "/home/user/docs/",
			want: "docs",
		},
		{
			name: "root",
			path: "/",
			want: "/",
		},
		{
			name: "empty string",
			path: "",
			want: ".",
		},
		{
			name: "current dir",
			path: ".",
			want: ".",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := GetBaseName(tt.path)
			if got != tt.want {
				t.Errorf("GetBaseName(%q) = %q; want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestCountLines_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input []byte
		want  int
	}{
		{
			name:  "nil content",
			input: nil,
			want:  0,
		},
		{
			name:  "empty content",
			input: []byte{},
			want:  0,
		},
		{
			name:  "single line no newline",
			input: []byte("hello"),
			want:  1,
		},
		{
			name:  "single line with newline",
			input: []byte("hello\n"),
			want:  2,
		},
		{
			name:  "multiple lines",
			input: []byte("line1\nline2\nline3"),
			want:  3,
		},
		{
			name:  "multiple lines ending with newline",
			input: []byte("line1\nline2\nline3\n"),
			want:  4,
		},
		{
			name:  "just newlines",
			input: []byte("\n\n\n"),
			want:  4,
		},
		{
			name:  "single newline",
			input: []byte("\n"),
			want:  2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := countLines(tt.input)
			if got != tt.want {
				t.Errorf("countLines(%q) = %d; want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestSplitLines_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input []byte
		want  []string
	}{
		{
			name:  "empty content",
			input: []byte{},
			want:  []string{},
		},
		{
			name:  "single line no newline",
			input: []byte("hello"),
			want:  []string{"hello"},
		},
		{
			name:  "single line with newline",
			input: []byte("hello\n"),
			want:  []string{"hello", ""},
		},
		{
			name:  "multiple lines no trailing newline",
			input: []byte("line1\nline2\nline3"),
			want:  []string{"line1", "line2", "line3"},
		},
		{
			name:  "multiple lines with trailing newline",
			input: []byte("line1\nline2\n"),
			want:  []string{"line1", "line2", ""},
		},
		{
			name:  "just newlines",
			input: []byte("\n\n"),
			want:  []string{"", "", ""},
		},
		{
			name:  "single newline",
			input: []byte("\n"),
			want:  []string{"", ""},
		},
		{
			name:  "nil content",
			input: nil,
			want:  []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := splitLines(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("splitLines(%q) = %v (len %d); want %v (len %d)", tt.input, got, len(got), tt.want, len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitLines(%q)[%d] = %q; want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestJoinLines_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		lines []string
		want  string
	}{
		{
			name:  "nil slice",
			lines: nil,
			want:  "",
		},
		{
			name:  "empty slice",
			lines: []string{},
			want:  "",
		},
		{
			name:  "single line",
			lines: []string{"hello"},
			want:  "hello",
		},
		{
			name:  "two lines",
			lines: []string{"line1", "line2"},
			want:  "line1\nline2",
		},
		{
			name:  "three lines",
			lines: []string{"a", "b", "c"},
			want:  "a\nb\nc",
		},
		{
			name:  "with empty lines",
			lines: []string{"a", "", "c"},
			want:  "a\n\nc",
		},
		{
			name:  "all empty",
			lines: []string{"", "", ""},
			want:  "\n\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := joinLines(tt.lines)
			if got != tt.want {
				t.Errorf("joinLines(%v) = %q; want %q", tt.lines, got, tt.want)
			}
		})
	}
}
