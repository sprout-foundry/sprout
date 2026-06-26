//go:build !js

package webui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// settings_api_helpers.go — validateReasoningEffort
// ---------------------------------------------------------------------------

func TestValidateReasoningEffort_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		err   bool
	}{
		{"", false}, // empty is valid
		{"low", false},
		{"medium", false},
		{"high", false},
		{"invalid", true},
		{"LOW", true}, // case-sensitive
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := validateReasoningEffort(tt.input)
			if (got != nil) != tt.err {
				t.Errorf("validateReasoningEffort(%q) error = %v, wantErr %v", tt.input, got, tt.err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// settings_api_helpers.go — validateSelfReviewGateMode
// ---------------------------------------------------------------------------

func TestValidateSelfReviewGateMode_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		err   bool
	}{
		{"off", false},
		{"code", false},
		{"always", false},
		{"invalid", true},
		{"", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := validateSelfReviewGateMode(tt.input)
			if (got != nil) != tt.err {
				t.Errorf("validateSelfReviewGateMode(%q) error=%v, wantErr %v", tt.input, got, tt.err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// settings_api_helpers.go — validateHistoryScope
// ---------------------------------------------------------------------------

func TestValidateHistoryScope_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		err   bool
	}{
		{"project", false},
		{"global", false},
		{"local", true},
		{"", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := validateHistoryScope(tt.input)
			if (got != nil) != tt.err {
				t.Errorf("validateHistoryScope(%q) error=%v, wantErr %v", tt.input, got, tt.err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// settings_api_helpers.go — validateAPITimeout
// ---------------------------------------------------------------------------

func TestValidateAPITimeout_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input int
		err   bool
	}{
		{30, false},
		{1, false},
		{0, true},
		{-1, true},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.input), func(t *testing.T) {
			t.Parallel()
			got := validateAPITimeout(tt.input)
			if (got != nil) != tt.err {
				t.Errorf("validateAPITimeout(%d) error=%v, wantErr %v", tt.input, got, tt.err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// settings_api_helpers.go — extractPathSegment
// ---------------------------------------------------------------------------

func TestExtractPathSegment_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path   string
		prefix string
		want   string
	}{
		{"/api/settings/mcp/servers/myserver", "/api/settings/mcp/servers/", "myserver"},
		{"/api/settings/mcp/servers/myserver/", "/api/settings/mcp/servers/", "myserver"},
		{"/api/other", "/api/settings/mcp/servers/", ""},
		{"nomatch", "/prefix", ""},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			got := extractPathSegment(tt.path, tt.prefix)
			if got != tt.want {
				t.Errorf("extractPathSegment(%q, %q) = %q, want %q", tt.path, tt.prefix, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// settings_api_helpers.go — asInt
// ---------------------------------------------------------------------------

func TestAsInt_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input interface{}
		want  int
		ok    bool
	}{
		{float64(42), 42, true},
		{int(42), 42, true},
		{int64(42), 42, true},
		{"42", 0, false},
		{nil, 0, false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%T", tt.input), func(t *testing.T) {
			t.Parallel()
			got, ok := asInt(tt.input)
			if ok != tt.ok || got != tt.want {
				t.Errorf("asInt(%v) = (%d, %v), want (%d, %v)", tt.input, got, ok, tt.want, tt.ok)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// settings_api_helpers.go — writeJSON / writeJSONError / writeJSONErr
// ---------------------------------------------------------------------------

func TestWriteJSON_ZC(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusOK, map[string]string{"key": "value"})
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"key"`) || !strings.Contains(body, `"value"`) {
		t.Errorf("response body should contain key/value: %s", body)
	}
}

func TestWriteJSONError_ZC(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	writeJSONError(w, http.StatusBadRequest, "test error")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "test error") {
		t.Errorf("response body should contain error: %s", body)
	}
}

func TestWriteJSONErr_ZC(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	writeJSONErr(w, http.StatusInternalServerError, "INTERNAL", "something broke")
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "INTERNAL") || !strings.Contains(body, "something broke") {
		t.Errorf("response body should contain code/message: %s", body)
	}
}

// ---------------------------------------------------------------------------
// search_api.go — getContextLines
// ---------------------------------------------------------------------------

func TestGetContextLines_ZC(t *testing.T) {
	t.Parallel()
	t.Run("zero_context", func(t *testing.T) {
		got := getContextLines([]string{"a", "b", "c"}, 3, 0, true)
		if got != nil {
			t.Errorf("expected nil for zero context, got %v", got)
		}
	})
	t.Run("before_lines", func(t *testing.T) {
		buffer := []string{"line0", "line1", "line2", "line3", "line4"}
		got := getContextLines(buffer, 5, 2, true)
		if len(got) != 2 {
			t.Fatalf("expected 2 context lines, got %d", len(got))
		}
		if got[0] != "line2" || got[1] != "line3" {
			t.Errorf("expected [line2, line3], got %v", got)
		}
	})
	t.Run("more_context_than_buffer", func(t *testing.T) {
		t.Parallel()
		buffer := []string{"line0", "line1"}
		got := getContextLines(buffer, 2, 5, true)
		if len(got) != 1 || got[0] != "line0" {
			t.Errorf("expected [line0], got %v", got)
		}
	})
}

// ---------------------------------------------------------------------------
// search_api.go — compileSearchPattern
// ---------------------------------------------------------------------------

func TestCompileSearchPattern_ZC(t *testing.T) {
	t.Parallel()
	t.Run("plain_text", func(t *testing.T) {
		re, err := compileSearchPattern("hello", false, false, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !re.MatchString("hello world") {
			t.Error("should match plain text")
		}
	})
	t.Run("case_insensitive", func(t *testing.T) {
		re, err := compileSearchPattern("Hello", false, false, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !re.MatchString("hello world") {
			t.Error("should match case-insensitive")
		}
	})
	t.Run("case_sensitive", func(t *testing.T) {
		re, err := compileSearchPattern("Hello", true, false, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if re.MatchString("hello world") {
			t.Error("should NOT match case-sensitive")
		}
	})
	t.Run("whole_word", func(t *testing.T) {
		re, err := compileSearchPattern("test", false, true, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !re.MatchString("this is a test here") {
			t.Error("should match whole word")
		}
	})
	t.Run("regex_mode", func(t *testing.T) {
		re, err := compileSearchPattern("foo.*bar", false, false, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !re.MatchString("fooxyzbar") {
			t.Error("should match regex pattern")
		}
	})
	t.Run("regex_escapes_special", func(t *testing.T) {
		re, err := compileSearchPattern("func()", false, false, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !re.MatchString("call func() now") {
			t.Error("should match escaped special chars")
		}
	})
	t.Run("invalid_regex", func(t *testing.T) {
		_, err := compileSearchPattern("(:", false, false, true)
		if err == nil {
			t.Error("should return error for invalid regex")
		}
	})
}

// ---------------------------------------------------------------------------
// search_api.go — parsePatterns
// ---------------------------------------------------------------------------

func TestParsePatterns_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"*.go", 1},
		{"*.go, *.js", 2},
		{"*.go, , *.js", 2},
		{"  *.go  ,  *.js  ", 2},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := parsePatterns(tt.input)
			if len(got) != tt.want {
				t.Errorf("parsePatterns(%q) = %d patterns, want %d", tt.input, len(got), tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// search_api.go — matchesAnyPattern
// ---------------------------------------------------------------------------

func TestMatchesAnyPattern_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path     string
		patterns []string
		want     bool
	}{
		{"main.go", []string{"*.go"}, true},
		{"main.go", []string{"*.js"}, false},
		{"src/main.go", []string{"*.go"}, true},
		{"src/main.go", []string{"main.go"}, true},
		{"src/test.js", []string{"*.go", "*.js"}, true},
		{"readme.md", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			got := matchesAnyPattern(tt.path, tt.patterns)
			if got != tt.want {
				t.Errorf("matchesAnyPattern(%q, %v) = %v, want %v", tt.path, tt.patterns, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// api_misc.go — tryParseMultipartFile
// ---------------------------------------------------------------------------

func TestTryParseMultipartFile_ZC(t *testing.T) {
	t.Parallel()
	t.Run("not_multipart", func(t *testing.T) {
		_, ok := tryParseMultipartFile([]byte("hello"), "application/json")
		if ok {
			t.Error("should return false for non-multipart content type")
		}
	})
	t.Run("invalid_multipart", func(t *testing.T) {
		_, ok := tryParseMultipartFile([]byte("garbage"), "multipart/form-data; boundary=----test")
		// Parsing garbage multipart should return false
		if ok {
			t.Error("should return false for invalid multipart data")
		}
	})
}
