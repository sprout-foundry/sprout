package text

import (
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/stretchr/testify/assert"
)

func TestGetSummary(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		path        string
		cfg         *configuration.Config
		wantSummary string
		wantExports string
		wantRefs    string
		wantErr     bool
	}{
		{
			name:        "empty content with nil config",
			content:     "",
			path:        "test.go",
			cfg:         nil,
			wantSummary: "File summary",
			wantExports: "File exports",
			wantRefs:    "File references",
			wantErr:     false,
		},
		{
			name:        "simple content with nil config",
			content:     "package main\n\nfunc main() {}\n",
			path:        "main.go",
			cfg:         nil,
			wantSummary: "File summary",
			wantExports: "File exports",
			wantRefs:    "File references",
			wantErr:     false,
		},
		{
			name:        "content with config",
			content:     "package test\n\nfunc TestFunction() int { return 42 }\n",
			path:        "test/test.go",
			cfg:         configuration.NewConfig(),
			wantSummary: "File summary",
			wantExports: "File exports",
			wantRefs:    "File references",
			wantErr:     false,
		},
		{
			name:        "large content",
			content:     strings.Repeat("// comment line\n", 100),
			path:        "large.go",
			cfg:         nil,
			wantSummary: "File summary",
			wantExports: "File exports",
			wantRefs:    "File references",
			wantErr:     false,
		},
		{
			name:        "path with special characters",
			content:     "test content",
			path:        "/path/with spaces/and-dashes/file.go",
			cfg:         nil,
			wantSummary: "File summary",
			wantExports: "File exports",
			wantRefs:    "File references",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary, exports, refs, err := GetSummary(tt.content, tt.path, tt.cfg)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.wantSummary, summary, "summary mismatch")
			assert.Equal(t, tt.wantExports, exports, "exports mismatch")
			assert.Equal(t, tt.wantRefs, refs, "references mismatch")
		})
	}
}

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: []string{},
		},
		{
			name:     "single word",
			input:    "hello",
			expected: []string{"hello"},
		},
		{
			name:     "multiple words with single spaces",
			input:    "hello world test",
			expected: []string{"hello", "world", "test"},
		},
		{
			name:     "multiple words with multiple spaces",
			input:    "hello    world   test",
			expected: []string{"hello", "world", "test"},
		},
		{
			name:     "words with tabs",
			input:    "hello\tworld\ttest",
			expected: []string{"hello", "world", "test"},
		},
		{
			name:     "words with newlines",
			input:    "hello\nworld\ntest",
			expected: []string{"hello", "world", "test"},
		},
		{
			name:     "mixed whitespace",
			input:    "hello  \tworld\n\ntest  ",
			expected: []string{"hello", "world", "test"},
		},
		{
			name:     "leading and trailing whitespace",
			input:    "   hello world   ",
			expected: []string{"hello", "world"},
		},
		{
			name:     "only whitespace",
			input:    "   \t\n   ",
			expected: []string{},
		},
		{
			name:     "code-like content",
			input:    "func main() { return 42 }",
			expected: []string{"func", "main()", "{", "return", "42", "}"},
		},
		{
			name:     "punctuation",
			input:    "hello, world! test?",
			expected: []string{"hello,", "world!", "test?"},
		},
		{
			name:     "numbers and symbols",
			input:    "123 456 abc-def ghi_jkl",
			expected: []string{"123", "456", "abc-def", "ghi_jkl"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractKeywords(tt.input)
			assert.Equal(t, tt.expected, result, "keywords mismatch")
		})
	}
}

// BenchmarkExtractKeywords benchmarks the ExtractKeywords function
func BenchmarkExtractKeywords(b *testing.B) {
	benchmarks := []struct {
		name  string
		input string
	}{
		{
			name:  "short string",
			input: "hello world test",
		},
		{
			name:  "medium string",
			input: strings.Repeat("word ", 50),
		},
		{
			name:  "long string",
			input: strings.Repeat("word ", 500),
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ExtractKeywords(bm.input)
			}
		})
	}
}

// BenchmarkGetSummary benchmarks the GetSummary function
func BenchmarkGetSummary(b *testing.B) {
	benchmarks := []struct {
		name    string
		content string
		path    string
	}{
		{
			name:    "small file",
			content: "package main\n\nfunc main() {}\n",
			path:    "main.go",
		},
		{
			name:    "medium file",
			content: strings.Repeat("// comment\nfunc f() {}\n", 20),
			path:    "medium.go",
		},
		{
			name:    "large file",
			content: strings.Repeat("// comment\nfunc f() {}\n", 200),
			path:    "large.go",
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				GetSummary(bm.content, bm.path, nil)
			}
		})
	}
}
