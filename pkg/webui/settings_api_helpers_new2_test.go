//go:build !js

package webui

import (
	"testing"
)

// Test validateReasoningEffort function
func TestValidateReasoningEffort2(t *testing.T) {
	tests := []struct {
		name    string
		v       string
		wantErr bool
	}{
		{
			name:    "valid empty string",
			v:       "",
			wantErr: false,
		},
		{
			name:    "valid low",
			v:       "low",
			wantErr: false,
		},
		{
			name:    "valid medium",
			v:       "medium",
			wantErr: false,
		},
		{
			name:    "valid high",
			v:       "high",
			wantErr: false,
		},
		{
			name:    "invalid value - extra high",
			v:       "extra",
			wantErr: true,
		},
		{
			name:    "invalid value - nonsense",
			v:       "invalid",
			wantErr: true,
		},
		{
			name:    "invalid value - mixed case",
			v:       "Low",
			wantErr: true,
		},
		{
			name:    "invalid value - with spaces",
			v:       " low ",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateReasoningEffort(tt.v)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateReasoningEffort(%q) error = %v, wantErr %v", tt.v, err, tt.wantErr)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("validateReasoningEffort(%q) unexpected error: %v", tt.v, err)
			}
		})
	}
}

// Test validateHistoryScope function
func TestValidateHistoryScope2(t *testing.T) {
	tests := []struct {
		name    string
		v       string
		wantErr bool
	}{
		{
			name:    "valid project",
			v:       "project",
			wantErr: false,
		},
		{
			name:    "valid global",
			v:       "global",
			wantErr: false,
		},
		{
			name:    "invalid value - all",
			v:       "all",
			wantErr: true,
		},
		{
			name:    "invalid value - workspace",
			v:       "workspace",
			wantErr: true,
		},
		{
			name:    "invalid value - nonsense",
			v:       "invalid",
			wantErr: true,
		},
		{
			name:    "invalid value - mixed case",
			v:       "Project",
			wantErr: true,
		},
		{
			name:    "invalid value - with spaces",
			v:       " project ",
			wantErr: true,
		},
		{
			name:    "empty string invalid",
			v:       "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateHistoryScope(tt.v)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateHistoryScope(%q) error = %v, wantErr %v", tt.v, err, tt.wantErr)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("validateHistoryScope(%q) unexpected error: %v", tt.v, err)
			}
		})
	}
}

// Test validateAPITimeout function
func TestValidateAPITimeout2(t *testing.T) {
	tests := []struct {
		name    string
		t       int
		wantErr bool
	}{
		{
			name:    "valid timeout 1",
			t:       1,
			wantErr: false,
		},
		{
			name:    "valid timeout 30",
			t:       30,
			wantErr: false,
		},
		{
			name:    "valid timeout 60",
			t:       60,
			wantErr: false,
		},
		{
			name:    "valid timeout 120",
			t:       120,
			wantErr: false,
		},
		{
			name:    "invalid timeout 0",
			t:       0,
			wantErr: true,
		},
		{
			name:    "invalid timeout negative",
			t:       -1,
			wantErr: true,
		},
		{
			name:    "invalid timeout large negative",
			t:       -100,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAPITimeout(tt.t)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateAPITimeout(%d) error = %v, wantErr %v", tt.t, err, tt.wantErr)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("validateAPITimeout(%d) unexpected error: %v", tt.t, err)
			}
		})
	}
}

// Test extractPathSegment function
func TestExtractPathSegment2(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		prefix   string
		expected string
	}{
		{
			name:     "simple extraction",
			path:     "/api/settings/mcp/servers/myserver",
			prefix:   "/api/settings/mcp/servers/",
			expected: "myserver",
		},
		{
			name:     "extraction with trailing slash in path",
			path:     "/api/settings/mcp/servers/myserver/",
			prefix:   "/api/settings/mcp/servers/",
			expected: "myserver",
		},
		{
			name:     "no prefix match",
			path:     "/api/settings/other/path",
			prefix:   "/api/settings/mcp/servers/",
			expected: "",
		},
		{
			name:     "empty path",
			path:     "",
			prefix:   "/api/settings/mcp/servers/",
			expected: "",
		},
		{
			name:     "empty prefix",
			path:     "/api/settings/mcp/servers/myserver",
			prefix:   "",
			expected: "/api/settings/mcp/servers/myserver",
		},
		{
			name:     "segment with special characters",
			path:     "/api/settings/mcp/servers/my-server-123",
			prefix:   "/api/settings/mcp/servers/",
			expected: "my-server-123",
		},
		{
			name:     "segment with slashes (should trim)",
			path:     "/api/settings/mcp/servers/my/server",
			prefix:   "/api/settings/mcp/servers/",
			expected: "my/server",
		},
		{
			name:     "exact match prefix",
			path:     "/api/settings/mcp/servers/",
			prefix:   "/api/settings/mcp/servers/",
			expected: "",
		},
		{
			name:     "multiple trailing slashes",
			path:     "/api/settings/mcp/servers/myserver///",
			prefix:   "/api/settings/mcp/servers/",
			expected: "myserver",
		},
		{
			name:     "segment with encoded characters",
			path:     "/api/settings/mcp/servers/my%20server",
			prefix:   "/api/settings/mcp/servers/",
			expected: "my%20server",
		},
		{
			name:     "partial prefix match should return empty",
			path:     "/api/settings/mcp",
			prefix:   "/api/settings/mcp/servers/",
			expected: "",
		},
		{
			name:     "long prefix",
			path:     "/a/b/c/d/e/f/item",
			prefix:   "/a/b/c/d/e/f/",
			expected: "item",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPathSegment(tt.path, tt.prefix)
			if got != tt.expected {
				t.Errorf("extractPathSegment(%q, %q) = %q, want %q", tt.path, tt.prefix, got, tt.expected)
			}
		})
	}
}

// Test asInt function
func TestAsInt2(t *testing.T) {
	tests := []struct {
		name   string
		v      interface{}
		want   int
		wantOK bool
	}{
		{
			name:   "float64 integer value",
			v:      42.0,
			want:   42,
			wantOK: true,
		},
		{
			name:   "float64 with decimal (truncated)",
			v:      42.7,
			want:   42,
			wantOK: true,
		},
		{
			name:   "int value",
			v:      42,
			want:   42,
			wantOK: true,
		},
		{
			name:   "int64 value",
			v:      int64(42),
			want:   42,
			wantOK: true,
		},
		{
			name:   "negative int",
			v:      -10,
			want:   -10,
			wantOK: true,
		},
		{
			name:   "negative float64",
			v:      -10.5,
			want:   -10,
			wantOK: true,
		},
		{
			name:   "large int64",
			v:      int64(9223372036854775807),
			want:   9223372036854775807, // On 64-bit systems, int can hold this value
			wantOK: true,
		},
		{
			name:   "string should fail",
			v:      "42",
			want:   0,
			wantOK: false,
		},
		{
			name:   "nil should fail",
			v:      nil,
			want:   0,
			wantOK: false,
		},
		{
			name:   "bool should fail",
			v:      true,
			want:   0,
			wantOK: false,
		},
		{
			name:   "float64 zero",
			v:      0.0,
			want:   0,
			wantOK: true,
		},
		{
			name:   "int zero",
			v:      0,
			want:   0,
			wantOK: true,
		},
		{
			name:   "float64 very small",
			v:      0.1,
			want:   0,
			wantOK: true,
		},
		{
			name:   "uint value",
			v:      uint(42),
			want:   0,
			wantOK: false,
		},
		{
			name:   "map should fail",
			v:      map[string]interface{}{},
			want:   0,
			wantOK: false,
		},
		{
			name:   "slice should fail",
			v:      []interface{}{},
			want:   0,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotOK := asInt(tt.v)
			if got != tt.want || gotOK != tt.wantOK {
				t.Errorf("asInt(%v) = (%v, %v), want (%v, %v)", tt.v, got, gotOK, tt.want, tt.wantOK)
			}
		})
	}
}

// Test edge cases for validation functions
func TestValidationEdgeCases2(t *testing.T) {
	t.Run("validateReasoningEffort with whitespace", func(t *testing.T) {
		err := validateReasoningEffort("  low  ")
		if err == nil {
			t.Error("validateReasoningEffort should reject whitespace-padded values")
		}
	})

	t.Run("validateHistoryScope with whitespace", func(t *testing.T) {
		err := validateHistoryScope("  project  ")
		if err == nil {
			t.Error("validateHistoryScope should reject whitespace-padded values")
		}
	})
}

// Test extractPathSegment edge cases
func TestExtractPathSegmentEdgeCases2(t *testing.T) {
	t.Run("empty both", func(t *testing.T) {
		got := extractPathSegment("", "")
		if got != "" {
			t.Errorf("extractPathSegment(\"\", \"\") = %q, want \"\"", got)
		}
	})

	t.Run("prefix longer than path", func(t *testing.T) {
		got := extractPathSegment("/short", "/verylong/")
		if got != "" {
			t.Errorf("expected empty string when prefix is longer than path, got %q", got)
		}
	})

	t.Run("case sensitivity", func(t *testing.T) {
		got := extractPathSegment("/API/Settings/mcp/servers/myserver", "/api/settings/mcp/servers/")
		if got != "" {
			t.Errorf("extractPathSegment should be case-sensitive, got %q", got)
		}
	})
}

// Test asInt with various JSON-like values
func TestAsIntJSONValues2(t *testing.T) {
	t.Run("float64 from JSON decoder", func(t *testing.T) {
		// JSON numbers are decoded as float64
		v := float64(123)
		got, ok := asInt(v)
		if !ok || got != 123 {
			t.Errorf("asInt(float64(123)) = (%v, %v), want (123, true)", got, ok)
		}
	})

	t.Run("float64 negative from JSON decoder", func(t *testing.T) {
		v := float64(-456)
		got, ok := asInt(v)
		if !ok || got != -456 {
			t.Errorf("asInt(float64(-456)) = (%v, %v), want (-456, true)", got, ok)
		}
	})

	t.Run("float64 large value truncation", func(t *testing.T) {
		v := float64(123456789012.99)
		got, ok := asInt(v)
		if !ok || got != 123456789012 {
			t.Errorf("asInt(float64(123456789012.99)) = (%v, %v), want (123456789012, true)", got, ok)
		}
	})

	t.Run("int64 max value", func(t *testing.T) {
		v := int64(2147483647) // Max int32
		got, ok := asInt(v)
		if !ok || got != 2147483647 {
			t.Errorf("asInt(int64(2147483647)) = (%v, %v), want (2147483647, true)", got, ok)
		}
	})

	t.Run("int64 negative value", func(t *testing.T) {
		v := int64(-2147483648) // Min int32
		got, ok := asInt(v)
		if !ok || got != -2147483648 {
			t.Errorf("asInt(int64(-2147483648)) = (%v, %v), want (-2147483648, true)", got, ok)
		}
	})
}
