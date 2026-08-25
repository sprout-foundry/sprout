//go:build js && wasm

// Tests for the LLM command WASM bridge functions. Run via:
//
//	GOOS=js GOARCH=wasm go test \
//	  -exec "$(go env GOROOT)/lib/wasm/go_js_wasm_exec" \
//	  ./cmd/wasm/
//
// These tests target the pure-Go logic in llm_funcs.go: the function
// registry shape and the argDiff helper. The agent-invoking functions
// (runQuestion/runCommit/runReview) require a live LLM client and are
// not unit-testable in isolation.

package main

import (
	"syscall/js"
	"testing"
)

// ── llmJSFuncs Registry ──────────────────────────────────────────

func TestLlmJSFuncs_ReturnsExpectedKeys(t *testing.T) {
	funcs := llmJSFuncs()

	expectedKeys := []string{"runQuestion", "runCommit", "runReview"}
	if len(funcs) != len(expectedKeys) {
		t.Fatalf("llmJSFuncs() has %d keys, want %d", len(funcs), len(expectedKeys))
	}

	for _, key := range expectedKeys {
		fn, ok := funcs[key]
		if !ok {
			t.Errorf("llmJSFuncs() missing key %q", key)
			continue
		}
		if fn == nil {
			t.Errorf("llmJSFuncs()[%q] is nil", key)
		}
	}
}

// ── argDiff ──────────────────────────────────────────────────────

func TestArgDiff_NilOrEmptyArgs(t *testing.T) {
	cases := []struct {
		name   string
		args   []js.Value
		idx    int
		defVal string
		want   string
	}{
		{
			name:   "nil args returns default",
			args:   nil,
			idx:    2,
			defVal: "default",
			want:   "default",
		},
		{
			name:   "empty args returns default",
			args:   []js.Value{},
			idx:    0,
			defVal: "fallback",
			want:   "fallback",
		},
		{
			name:   "idx beyond length returns default",
			args:   []js.Value{js.ValueOf("provider")},
			idx:    5,
			defVal: "mydefault",
			want:   "mydefault",
		},
		{
			name:   "idx equal to length returns default",
			args:   []js.Value{js.ValueOf("a"), js.ValueOf("b")},
			idx:    2,
			defVal: "boundary",
			want:   "boundary",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := argDiff(c.args, c.idx, c.defVal)
			if got != c.want {
				t.Errorf("argDiff(%v, %d, %q) = %q, want %q", c.args, c.idx, c.defVal, got, c.want)
			}
		})
	}
}

func TestArgDiff_StringArg(t *testing.T) {
	args := []js.Value{js.ValueOf("hello"), js.ValueOf("world")}

	cases := []struct {
		idx  int
		want string
	}{
		{0, "hello"},
		{1, "world"},
	}

	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			got := argDiff(args, c.idx, "default")
			if got != c.want {
				t.Errorf("argDiff at idx %d = %q, want %q", c.idx, got, c.want)
			}
		})
	}
}

func TestArgDiff_ObjectWithDiffProperty(t *testing.T) {
	obj := js.ValueOf(map[string]interface{}{"diff": "diff content here"})
	args := []js.Value{js.ValueOf("provider"), js.ValueOf("model"), obj}

	got := argDiff(args, 2, "default")
	if got != "diff content here" {
		t.Errorf("argDiff object.diff = %q, want %q", got, "diff content here")
	}
}

func TestArgDiff_ObjectWithContentProperty(t *testing.T) {
	obj := js.ValueOf(map[string]interface{}{"content": "content here"})
	args := []js.Value{js.ValueOf("provider"), js.ValueOf("model"), obj}

	got := argDiff(args, 2, "default")
	if got != "content here" {
		t.Errorf("argDiff object.content = %q, want %q", got, "content here")
	}
}

func TestArgDiff_ObjectWithTextProperty(t *testing.T) {
	obj := js.ValueOf(map[string]interface{}{"text": "text here"})
	args := []js.Value{js.ValueOf("provider"), js.ValueOf("model"), obj}

	got := argDiff(args, 2, "default")
	if got != "text here" {
		t.Errorf("argDiff object.text = %q, want %q", got, "text here")
	}
}

func TestArgDiff_ObjectPropertyPriority(t *testing.T) {
	// When an object has all three properties, .diff should win.
	obj := js.ValueOf(map[string]interface{}{
		"diff":    "diff value",
		"content": "content value",
		"text":    "text value",
	})
	args := []js.Value{js.ValueOf("provider"), js.ValueOf("model"), obj}

	got := argDiff(args, 2, "default")
	if got != "diff value" {
		t.Errorf("argDiff priority = %q, want %q (diff should take precedence)", got, "diff value")
	}
}

func TestArgDiff_ObjectWithMissingPropertiesReturnsDefault(t *testing.T) {
	// Object with no recognized properties falls back to default.
	obj := js.ValueOf(map[string]interface{}{
		"unknown": "unknown value",
		"other":   "other value",
	})
	args := []js.Value{js.ValueOf("provider"), obj}

	got := argDiff(args, 1, "fallback")
	if got != "fallback" {
		t.Errorf("argDiff with unknown properties = %q, want %q", got, "fallback")
	}
}

func TestArgDiff_ObjectWithNonStringPropertyReturnsDefault(t *testing.T) {
	// Object properties that are not strings (numbers, bools, etc.)
	// should be ignored and fall back to default.
	obj := js.ValueOf(map[string]interface{}{
		"diff":    42,
		"content": true,
		"text":    3.14,
	})
	args := []js.Value{js.ValueOf("provider"), obj}

	got := argDiff(args, 1, "default")
	if got != "default" {
		t.Errorf("argDiff with non-string properties = %q, want %q", got, "default")
	}
}

func TestArgDiff_NonStringNonObjectReturnsDefault(t *testing.T) {
	cases := []struct {
		name  string
		value js.Value
	}{
		{"number", js.ValueOf(42)},
		{"bool", js.ValueOf(true)},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := []js.Value{js.ValueOf("provider"), c.value}
			got := argDiff(args, 1, "default")
			if got != "default" {
				t.Errorf("argDiff with %s arg = %q, want %q", c.name, got, "default")
			}
		})
	}
}

func TestArgDiff_EmptyStringArg(t *testing.T) {
	// An empty string is treated as absent — it falls back to defaultVal.
	args := []js.Value{js.ValueOf(""), js.ValueOf("non-empty")}

	got := argDiff(args, 0, "default")
	if got != "default" {
		t.Errorf("argDiff with empty string = %q, want %q", got, "default")
	}

	got = argDiff(args, 1, "default")
	if got != "non-empty" {
		t.Errorf("argDiff with non-empty string = %q, want %q", got, "non-empty")
	}
}

func TestArgDiff_ObjectWithEmptyDiffProperty(t *testing.T) {
	// Empty string in .diff is treated as absent — it falls through to .content.
	obj := js.ValueOf(map[string]interface{}{
		"diff":    "",
		"content": "fallback content",
	})
	args := []js.Value{obj}

	got := argDiff(args, 0, "default")
	if got != "fallback content" {
		t.Errorf("argDiff with empty diff property = %q, want %q", got, "fallback content")
	}
}

func TestArgDiff_SkipsEmptyDiffAndFallsBackToContent(t *testing.T) {
	// When .diff is present but empty, argDiff now skips it and falls
	// through to .content (the same behavior as if .diff were absent).
	obj := js.ValueOf(map[string]interface{}{
		"diff":    "",
		"content": "content value",
	})
	args := []js.Value{obj}

	got := argDiff(args, 0, "default")
	if got != "content value" {
		t.Errorf("argDiff with empty diff = %q, want %q", got, "content value")
	}
}

func TestArgDiff_ZeroIndex(t *testing.T) {
	// Verify idx=0 works as the lower bound on a populated args slice.
	args := []js.Value{js.ValueOf("first")}
	got := argDiff(args, 0, "default")
	if got != "first" {
		t.Errorf("argDiff at idx 0 = %q, want %q", got, "first")
	}
}
