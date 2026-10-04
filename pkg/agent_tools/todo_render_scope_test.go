package tools

import (
	"bytes"
	"strings"
	"testing"
)

// lineContaining returns the first line of s that contains needle, or "".
func lineContaining(s, needle string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}

// TestRenderTodosForCLI_ScopeTag pins the SP-148 §148c CLI-rendering behavior:
// a todo with a plan scope ID renders a "[scope]" tag on its line so CLI users
// can see which plan scope item it advances; a todo without a scope renders no
// scope tag on its line.
func TestRenderTodosForCLI_ScopeTag(t *testing.T) {
	todos := []TodoItem{
		{ID: "1", Content: "Implement login", Status: "in_progress", Scope: "s1"},
		{ID: "2", Content: "Build session UI", Status: "pending"},
	}

	var buf bytes.Buffer
	RenderTodosForCLI(&buf, todos)
	out := buf.String()

	// The scoped todo carries its scope tag on its own line.
	scopedLine := lineContaining(out, "Implement login")
	if !strings.Contains(scopedLine, "[s1]") {
		t.Errorf("scoped todo must render its scope tag, got line:\n  %s", scopedLine)
	}

	// The unscoped todo must NOT carry a scope tag on its line.
	unscopedLine := lineContaining(out, "Build session UI")
	if unscopedLine == "" {
		t.Fatalf("expected the unscoped todo to be rendered, got:\n%s", out)
	}
	if strings.Contains(unscopedLine, "[s1]") || strings.Contains(unscopedLine, "[s2]") {
		t.Errorf("unscoped todo must not render a scope tag, got line:\n  %s", unscopedLine)
	}

	// A scoped todo with an active form: the scope tag renders after the
	// display text (the active form is used for the in_progress line).
	todos2 := []TodoItem{
		{ID: "3", Content: "Implement login", ActiveForm: "Implementing login", Status: "in_progress", Scope: "s1"},
	}
	var buf2 bytes.Buffer
	RenderTodosForCLI(&buf2, todos2)
	if !strings.Contains(buf2.String(), "Implementing login [s1]") {
		t.Errorf("scope tag must render after the active form, got:\n%s", buf2.String())
	}
}
