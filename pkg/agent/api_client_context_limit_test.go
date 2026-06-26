package agent

import (
	"fmt"
	"regexp"
	"testing"
)

// isContextLimitError checks if an error is related to context window limits.
func isContextLimitError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := err.Error()
	// Check for various context limit error patterns
	limitPatterns := []string{
		"context window exceeds limit",
		"context window over limit",
		"context_limit exceeded",
		"context exceeds",
		"max context tokens exceeded",
		"token limit exceeded",
		"exceeds the available context size",
		"exceed_context_size_error",
		"maximum context length",
	}
	for _, pattern := range limitPatterns {
		if containsIgnoreCase(errMsg, pattern) {
			return true
		}
	}
	return false
}

// extractContextLimitTokenPair extracts prompt tokens and context limit from an error message.
type contextLimitPair struct {
	prompt int
	limit  int
}

func extractContextLimitTokenPair(err error) contextLimitPair {
	pair := contextLimitPair{}
	if err == nil {
		return pair
	}
	errMsg := err.Error()

	// Pattern: request (NNN tokens) exceeds the available context size (NNN tokens)
	promptRe := regexp.MustCompile(`request \((\d+) tokens\) exceeds`)
	match := promptRe.FindStringSubmatch(errMsg)
	if len(match) > 1 {
		fmt.Sscanf(match[1], "%d", &pair.prompt)
	}

	// Pattern: context size (NNN tokens), try increasing it
	limitRe := regexp.MustCompile(`context size \((\d+) tokens\)`)
	match = limitRe.FindStringSubmatch(errMsg)
	if len(match) > 1 {
		fmt.Sscanf(match[1], "%d", &pair.limit)
	}

	return pair
}

func containsIgnoreCase(s, substr string) bool {
	return regexp.MustCompile("(?i)" + regexp.QuoteMeta(substr)).MatchString(s)
}

func TestIsContextLimitError(t *testing.T) {
	tests := []struct {
		name     string
		errMsg   string
		expected bool
	}{
		{
			name:     "context window exceeds",
			errMsg:   "context window exceeds limit (2013)",
			expected: true,
		},
		{
			name:     "context window over",
			errMsg:   "context window over limit",
			expected: true,
		},
		{
			name:     "context limit in error",
			errMsg:   "context_limit exceeded",
			expected: true,
		},
		{
			name:     "context exceeds",
			errMsg:   "context exceeds max",
			expected: true,
		},
		{
			name:     "max context",
			errMsg:   "max context tokens exceeded",
			expected: true,
		},
		{
			name:     "token limit exceeded",
			errMsg:   "token limit exceeded",
			expected: true,
		},
		{
			name:     "available context size exceeded",
			errMsg:   "HTTP 400: request (131306 tokens) exceeds the available context size (131072 tokens)",
			expected: true,
		},
		{
			name:     "provider exceed context error code",
			errMsg:   "{\"error\":{\"type\":\"exceed_context_size_error\"}}",
			expected: true,
		},
		{
			name:     "maximum context length",
			errMsg:   "This model's maximum context length is 131072 tokens",
			expected: true,
		},
		{
			name:     "generic error",
			errMsg:   "something went wrong",
			expected: false,
		},
		{
			name:     "rate limit error",
			errMsg:   "rate limit exceeded",
			expected: false,
		},
		{
			name:     "nil error",
			errMsg:   "",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.errMsg != "" {
				err = &testError{message: tc.errMsg}
			}
			result := isContextLimitError(err)
			if result != tc.expected {
				t.Errorf("isContextLimitError(%q) = %v, want %v", tc.errMsg, result, tc.expected)
			}
		})
	}
}

type testError struct {
	message string
}

func (e *testError) Error() string {
	return e.message
}

func TestExtractContextLimitTokenPair(t *testing.T) {
	err := &testError{message: "HTTP 400: {\"error\":{\"message\":\"request (131306 tokens) exceeds the available context size (131072 tokens), try increasing it\",\"type\":\"exceed_context_size_error\",\"n_prompt_tokens\":131306,\"n_ctx\":131072}}"}
	pair := extractContextLimitTokenPair(err)
	if pair.prompt != 131306 {
		t.Fatalf("prompt tokens = %d, want 131306", pair.prompt)
	}
	if pair.limit != 131072 {
		t.Fatalf("context limit = %d, want 131072", pair.limit)
	}
}
