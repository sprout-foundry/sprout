package agent

import (
	"strings"
	"testing"
)

func TestSanitizeToolFailureMessage(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		wantContains    string
		wantNotContains string
		wantExact       string
	}{
		{
			name:      "empty string returns unknown tool error",
			input:     "",
			wantExact: "unknown tool error",
		},
		{
			name:      "whitespace only returns unknown tool error",
			input:     "   \t\n  ",
			wantExact: "unknown tool error",
		},
		{
			name:      "normal message unchanged",
			input:     "file not found",
			wantExact: "file not found",
		},
		{
			name:            "data URL with base64 is redacted",
			input:           "Error with data:image/png;base64,ABCDEF123456",
			wantContains:    "[REDACTED]",
			wantNotContains: "ABCDEF123456",
		},
		{
			name:         "data URL preserves MIME type",
			input:        "Failed: data:application/pdf;base64,XYZ789",
			wantContains: "data:application/pdf;base64,[REDACTED]",
		},
		{
			name:         "long base64 runs are redacted",
			input:        "Error: " + strings.Repeat("A", 512),
			wantContains: "[BASE64_REDACTED]",
		},
		{
			name:         "message over maxToolFailureMessageChars is truncated",
			input:        strings.Repeat("!", maxToolFailureMessageChars+100),
			wantContains: "... (truncated)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeToolFailureMessage(tt.input)

			if tt.wantExact != "" && result != tt.wantExact {
				t.Errorf("sanitizeToolFailureMessage(%q) = %q, want %q", tt.input, result, tt.wantExact)
			}

			if tt.wantContains != "" && !strings.Contains(result, tt.wantContains) {
				t.Errorf("sanitizeToolFailureMessage(%q) = %q, want to contain %q", tt.input, result, tt.wantContains)
			}

			if tt.wantNotContains != "" && strings.Contains(result, tt.wantNotContains) {
				t.Errorf("sanitizeToolFailureMessage(%q) = %q, want NOT to contain %q", tt.input, result, tt.wantNotContains)
			}
		})
	}
}

func TestParseToolArgumentsWithRepair(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantError    bool
		wantRepaired bool
		wantKey      string      // key to check in result
		wantValue    interface{} // expected value for that key
	}{
		{
			name:         "valid JSON not repaired",
			input:        `{"key": "value"}`,
			wantError:    false,
			wantRepaired: false,
			wantKey:      "key",
			wantValue:    "value",
		},
		{
			name:      "empty string returns error",
			input:     "",
			wantError: true,
		},
		{
			name:      "whitespace only returns error",
			input:     "   ",
			wantError: true,
		},
		{
			name:      "plain non-JSON returns error",
			input:     "not json at all",
			wantError: true,
		},
		{
			name:         "markdown code fence repaired",
			input:        "```json\n{\"path\": \"test.go\"}\n```",
			wantError:    false,
			wantRepaired: true,
			wantKey:      "path",
			wantValue:    "test.go",
		},
		{
			name:         "trailing comma repaired",
			input:        `{"a":1,}`,
			wantError:    false,
			wantRepaired: true,
			wantKey:      "a",
			wantValue:    float64(1),
		},
		{
			name:         "missing closing brace repaired",
			input:        `{"a":1`,
			wantError:    false,
			wantRepaired: true,
			wantKey:      "a",
			wantValue:    float64(1),
		},
		{
			name:         "JSON with surrounding text extracted",
			input:        "Here's my JSON: {\"key\": \"val\"} end",
			wantError:    false,
			wantRepaired: true,
			wantKey:      "key",
			wantValue:    "val",
		},
		{
			name:         "nested trailing commas",
			input:        `{"a":{"b":1,},}`,
			wantError:    false,
			wantRepaired: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, repaired, err := parseToolArgumentsWithRepair(tt.input)

			if (err != nil) != tt.wantError {
				t.Errorf("parseToolArgumentsWithRepair(%q) error = %v, want error = %v", tt.input, err, tt.wantError)
				return
			}

			if tt.wantError {
				return
			}

			if repaired != tt.wantRepaired {
				t.Errorf("parseToolArgumentsWithRepair(%q) repaired = %v, want %v", tt.input, repaired, tt.wantRepaired)
			}

			if tt.wantKey != "" {
				val, ok := args[tt.wantKey]
				if !ok {
					t.Errorf("parseToolArgumentsWithRepair(%q) missing key %q", tt.input, tt.wantKey)
					return
				}
				if val != tt.wantValue {
					t.Errorf("parseToolArgumentsWithRepair(%q) args[%q] = %v, want %v", tt.input, tt.wantKey, val, tt.wantValue)
				}
			}
		})
	}
}

func TestStripMarkdownCodeFence(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no fence returns trimmed input",
			input: "  hello world  ",
			want:  "hello world",
		},
		{
			name:  "json fence stripped",
			input: "```json\n{\"a\":1}\n```",
			want:  `{"a":1}`,
		},
		{
			name:  "plain fence stripped",
			input: "```\n{\"a\":1}\n```",
			want:  `{"a":1}`,
		},
		{
			name:  "fence without closing returns content",
			input: "```json\n{\"a\":1}",
			want:  `{"a":1}`,
		},
		{
			name:  "single line with fence returned as-is",
			input: "```",
			want:  "```",
		},
		{
			name:  "no backticks",
			input: "hello",
			want:  "hello",
		},
		{
			name:  "multi-line content inside fence",
			input: "```json\n{\n  \"a\": 1\n}\n```",
			want:  "{\n  \"a\": 1\n}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripMarkdownCodeFence(tt.input)
			if result != tt.want {
				t.Errorf("stripMarkdownCodeFence(%q) = %q, want %q", tt.input, result, tt.want)
			}
		})
	}
}

func TestExtractOuterJSONObject(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "extract from surrounded text",
			input: `prefix{"a":1}suffix`,
			want:  `{"a":1}`,
		},
		{
			name:  "no braces returns empty",
			input: "no braces here",
			want:  "",
		},
		{
			name:  "only opening brace returns empty",
			input: "{no closing",
			want:  "",
		},
		{
			name:  "only closing brace returns empty",
			input: "no opening}",
			want:  "",
		},
		{
			name:  "pure JSON returned as-is",
			input: `{"a":1}`,
			want:  `{"a":1}`,
		},
		{
			name:  "multiple objects returns outer span",
			input: `{"a":1}{"b":2}`,
			want:  `{"a":1}{"b":2}`,
		},
		{
			name:  "closing before opening returns empty",
			input: "}before{",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractOuterJSONObject(tt.input)
			if result != tt.want {
				t.Errorf("extractOuterJSONObject(%q) = %q, want %q", tt.input, result, tt.want)
			}
		})
	}
}

func TestExtractFirstBalancedJSONObject(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "nested braces correctly tracked",
			input: `prefix{"a":{"b":1}}suffix`,
			want:  `{"a":{"b":1}}`,
		},
		{
			name:  "string containing braces ignored",
			input: `{"key": "}not end"}`,
			want:  `{"key": "}not end"}`,
		},
		{
			name:  "escaped quotes in string",
			input: `{"key": "escaped \" quote"}extra`,
			want:  `{"key": "escaped \" quote"}`,
		},
		{
			name:  "no braces returns empty",
			input: "no braces",
			want:  "",
		},
		{
			name:  "unbalanced returns empty",
			input: `{"a":{"b":1}`,
			want:  "",
		},
		{
			name:  "first object extracted from multiple",
			input: `{"a":1} {"b":2}`,
			want:  `{"a":1}`,
		},
		{
			name:  "pure JSON returned as-is",
			input: `{"a":1}`,
			want:  `{"a":1}`,
		},
		{
			name:  "text before JSON",
			input: `some text {"a": 1} more text`,
			want:  `{"a": 1}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractFirstBalancedJSONObject(tt.input)
			if result != tt.want {
				t.Errorf("extractFirstBalancedJSONObject(%q) = %q, want %q", tt.input, result, tt.want)
			}
		})
	}
}

func TestRemoveJSONTrailingCommas(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "trailing comma in object",
			input: `{"a":1,}`,
			want:  `{"a":1}`,
		},
		{
			name:  "trailing comma in array",
			input: `[1,2,]`,
			want:  `[1,2]`,
		},
		{
			name:  "normal JSON unchanged",
			input: `{"a":1}`,
			want:  `{"a":1}`,
		},
		{
			name:  "multiple trailing commas",
			input: `{"a":{"b":1,},}`,
			want:  `{"a":{"b":1}}`,
		},
		{
			name:  "trailing comma with whitespace",
			input: `{"a":1,  }`,
			want:  `{"a":1  }`,
		},
		{
			name:  "no trailing commas",
			input: `{"a":1,"b":2}`,
			want:  `{"a":1,"b":2}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeJSONTrailingCommas(tt.input)
			if result != tt.want {
				t.Errorf("removeJSONTrailingCommas(%q) = %q, want %q", tt.input, result, tt.want)
			}
		})
	}
}

func TestCloseJSONDelimiters(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "missing closing brace",
			input: `{"a":1`,
			want:  `{"a":1}`,
		},
		{
			name:  "missing closing bracket",
			input: `[1,2`,
			want:  `[1,2]`,
		},
		{
			name:  "nested missing delimiters",
			input: `{"a":[1,2`,
			want:  `{"a":[1,2]}`,
		},
		{
			name:  "already balanced unchanged",
			input: `{"a":1}`,
			want:  `{"a":1}`,
		},
		{
			name:  "improper nesting returned unchanged",
			input: `]{`,
			want:  `]{`,
		},
		{
			name:  "missing two delimiters",
			input: `{"a":{"b":1`,
			want:  `{"a":{"b":1}}`,
		},
		{
			name:  "string containing brackets unchanged",
			input: `[{"key": "}]"}]`,
			want:  `[{"key": "}]"}]`,
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := closeJSONDelimiters(tt.input)
			if result != tt.want {
				t.Errorf("closeJSONDelimiters(%q) = %q, want %q", tt.input, result, tt.want)
			}
		})
	}
}
