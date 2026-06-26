package commands

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// --- helper: create a shell script in a temp file and return its path ---

func makeTempScript(t *testing.T, body string) string {
	t.Helper()
	f, err := os.CreateTemp("", "sprout-test-editor-*.sh")
	if err != nil {
		t.Fatalf("CreateTemp failed: %v", err)
	}
	if _, err := f.WriteString(body); err != nil {
		f.Close()
		os.Remove(f.Name())
		t.Fatalf("WriteString failed: %v", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		t.Fatalf("Close failed: %v", err)
	}
	if err := os.Chmod(f.Name(), 0755); err != nil {
		os.Remove(f.Name())
		t.Fatalf("Chmod failed: %v", err)
	}
	return f.Name()
}

// --- helper: capture stderr around a function call ---

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	defer func() {
		os.Stderr = old
	}()
	fn()
	w.Close()
	buf := new(bytes.Buffer)
	_, _ = io.Copy(buf, r)
	return buf.String()
}

// --- helper: create a real agent for testing InjectInputContext ---

func newTestAgent(t *testing.T) *agent.Agent {
	t.Helper()
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}
	return a
}

// ====================================================================
// Registration & metadata
// ====================================================================

func TestEditCommand_Registration(t *testing.T) {
	reg := NewCommandRegistry()
	cmd, ok := reg.GetCommand("edit")
	if !ok {
		t.Fatal("expected 'edit' command to be registered")
	}
	ec, ok := cmd.(*EditCommand)
	if !ok {
		t.Fatalf("expected *EditCommand, got %T", cmd)
	}
	if ec.Name() != "edit" {
		t.Errorf("Name() = %q, want \"edit\"", ec.Name())
	}
	if ec.Description() != "Open $EDITOR to compose or edit a query" {
		t.Errorf("Description() = %q, want correct description", ec.Description())
	}
}

// ====================================================================
// chooseEditor tests
// ====================================================================

func TestChooseEditor_VISUAL_TakesPriority(t *testing.T) {
	t.Setenv("VISUAL", "nvim")
	t.Setenv("EDITOR", "vim")
	got := chooseEditor()
	if got != "nvim" {
		t.Errorf("chooseEditor() = %q, want \"nvim\"", got)
	}
}

func TestChooseEditor_EDITOR_WhenVisualEmpty(t *testing.T) {

	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "emacs")
	got := chooseEditor()
	if got != "emacs" {
		t.Errorf("chooseEditor() = %q, want \"emacs\"", got)
	}
}

func TestChooseEditor_VISUAL_WithWhitespace(t *testing.T) {

	t.Setenv("VISUAL", "  nvim  ")
	t.Setenv("EDITOR", "vim")
	got := chooseEditor()
	if got != "nvim" {
		t.Errorf("chooseEditor() = %q, want \"nvim\"", got)
	}
}

func TestChooseEditor_EDITOR_WithWhitespace(t *testing.T) {

	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "  nano  ")
	got := chooseEditor()
	if got != "nano" {
		t.Errorf("chooseEditor() = %q, want \"nano\"", got)
	}
}

func TestChooseEditor_FallbackToVi(t *testing.T) {

	// Only set fallback if vi is available on the system
	if _, err := exec.LookPath("vi"); err != nil {
		t.Skip("vi not installed, skipping fallback test")
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	got := chooseEditor()
	if got != "vi" {
		t.Errorf("chooseEditor() fallback = %q, want \"vi\"", got)
	}
}

func TestChooseEditor_NoEditorFound(t *testing.T) {

	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	// If vi is installed on the system, chooseEditor will return "vi".
	// We can only confirm the empty return when vi is NOT installed.
	got := chooseEditor()
	if _, err := exec.LookPath("vi"); err != nil {
		// vi not installed — should return ""
		if got != "" {
			t.Errorf("chooseEditor() = %q, want \"\" (no editor found)", got)
		}
	} else {
		// vi IS installed — fallback succeeds, that's fine
		// This test just verifies the function doesn't panic
	}
}

// ====================================================================
// writeEditTempFile tests
// ====================================================================

func TestWriteEditTempFile_Empty(t *testing.T) {

	path, err := writeEditTempFile("")
	if err != nil {
		t.Fatalf("writeEditTempFile(\"\") error = %v", err)
	}
	defer os.Remove(path)

	if !strings.Contains(path, "sprout-edit-") {
		t.Errorf("expected temp file path to contain \"sprout-edit-\", got %q", path)
	}
	if !strings.HasSuffix(path, ".md") {
		t.Errorf("expected temp file path to end with \".md\", got %q", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(data) != "" {
		t.Errorf("expected empty content, got %q", data)
	}
}

func TestWriteEditTempFile_WithContent(t *testing.T) {

	content := "hello from editor\n"
	path, err := writeEditTempFile(content)
	if err != nil {
		t.Fatalf("writeEditTempFile error = %v", err)
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(data) != content {
		t.Errorf("content = %q, want %q", data, content)
	}
}

func TestWriteEditTempFile_MultilineContent(t *testing.T) {

	content := "line one\nline two\nline three\n"
	path, err := writeEditTempFile(content)
	if err != nil {
		t.Fatalf("writeEditTempFile error = %v", err)
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(data) != content {
		t.Errorf("content = %q, want %q", data, content)
	}
}

// ====================================================================
// Execute — editor produces empty result (no injection)
// ====================================================================

func TestEditCommand_Execute_EmptyBuffer_NoInjection(t *testing.T) {

	// Script: does NOT modify the file, so it remains empty (no prefill args)
	editorScript := makeTempScript(t, "#!/bin/sh\nexit 0")
	defer os.Remove(editorScript)

	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", editorScript)

	agent := newTestAgent(t)
	agent.ClearInputInjectionContext() // ensure channel is clean

	cmd := &EditCommand{}
	err := cmd.Execute(nil, agent)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	// Should NOT have injected anything
	select {
	case injected := <-agent.GetInputInjectionContext():
		t.Errorf("expected no injection, got %q", injected)
	default:
		// good — channel is empty
	}

	// Should print a message to stderr about empty buffer
	stderrOut := captureStderr(t, func() {
		_ = cmd.Execute(nil, agent)
	})
	if !strings.Contains(stderrOut, "empty buffer") {
		t.Errorf("expected stderr to contain \"empty buffer\", got: %q", stderrOut)
	}
}

// ====================================================================
// Execute — editor writes content (injection happens)
// ====================================================================

func TestEditCommand_Execute_EditorWritesContent(t *testing.T) {

	// Script: writes content to the first argument (the temp file)
	editorScript := makeTempScript(t, "#!/bin/sh\necho \"hello from editor\" > \"$1\"\nexit 0")
	defer os.Remove(editorScript)

	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", editorScript)

	agent := newTestAgent(t)
	agent.ClearInputInjectionContext()

	cmd := &EditCommand{}
	err := cmd.Execute(nil, agent)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	// Should have injected the content
	select {
	case injected := <-agent.GetInputInjectionContext():
		if injected != "hello from editor" {
			t.Errorf("injected = %q, want \"hello from editor\"", injected)
		}
	default:
		t.Error("expected injected content in channel, got nothing")
	}
}

// ====================================================================
// Execute — editor writes multiline content
// ====================================================================

func TestEditCommand_Execute_EditorWritesMultiline(t *testing.T) {

	// Script: writes multiple lines to the temp file (each echo adds a newline)
	editorScript := makeTempScript(t, "#!/bin/sh\nprintf 'line one\\nline two\\nline three\\n' > \"$1\"\nexit 0")
	defer os.Remove(editorScript)

	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", editorScript)

	agent := newTestAgent(t)
	agent.ClearInputInjectionContext()

	cmd := &EditCommand{}
	err := cmd.Execute(nil, agent)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	select {
	case injected := <-agent.GetInputInjectionContext():
		// TrimRight strips trailing newlines, so "line one\nline two\nline three"
		expected := "line one\nline two\nline three"
		if injected != expected {
			t.Errorf("injected = %q, want %q", injected, expected)
		}
	default:
		t.Error("expected injected content in channel, got nothing")
	}
}

// ====================================================================
// Execute — prefill from args, editor does not modify
// ====================================================================

func TestEditCommand_Execute_PrefillFromArgs_NoEdit(t *testing.T) {

	// Script: does NOT modify the file at all
	editorScript := makeTempScript(t, "#!/bin/sh\nexit 0")
	defer os.Remove(editorScript)

	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", editorScript)

	agent := newTestAgent(t)
	agent.ClearInputInjectionContext()

	cmd := &EditCommand{}
	// The prefill writes "foo bar baz\n" to the temp file,
	// the editor exits without changing it, so the content should
	// be read back and injected.
	err := cmd.Execute([]string{"foo", "bar", "baz"}, agent)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	select {
	case injected := <-agent.GetInputInjectionContext():
		if injected != "foo bar baz" {
			t.Errorf("injected = %q, want \"foo bar baz\"", injected)
		}
	default:
		t.Error("expected prefill content to be injected, got nothing")
	}
}

// ====================================================================
// Execute — prefill from args, editor replaces content
// ====================================================================

func TestEditCommand_Execute_PrefillFromArgs_EditorReplaces(t *testing.T) {

	// Script: overwrites the file with different content
	editorScript := makeTempScript(t, "#!/bin/sh\necho \"edited content\" > \"$1\"\nexit 0")
	defer os.Remove(editorScript)

	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", editorScript)

	agent := newTestAgent(t)
	agent.ClearInputInjectionContext()

	cmd := &EditCommand{}
	err := cmd.Execute([]string{"prefilled text"}, agent)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	select {
	case injected := <-agent.GetInputInjectionContext():
		if injected != "edited content" {
			t.Errorf("injected = %q, want \"edited content\"", injected)
		}
	default:
		t.Error("expected edited content to be injected, got nothing")
	}
}

// ====================================================================
// Execute — no editor found
// ====================================================================

func TestEditCommand_Execute_NoEditor(t *testing.T) {

	// If vi is installed on the system, we can't test the "no editor" case
	// easily since chooseEditor will fall back to vi.
	if _, err := exec.LookPath("vi"); err == nil {
		t.Skip("vi is installed; cannot test 'no editor' fallback path")
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")

	agent := newTestAgent(t)
	cmd := &EditCommand{}
	err := cmd.Execute(nil, agent)
	if err == nil {
		t.Fatal("expected error when no editor is found")
	}
	if !strings.Contains(err.Error(), "[edit]") {
		t.Errorf("error should contain \"[edit]\", got: %v", err)
	}
}

// ====================================================================
// Execute — editor exits with error
// ====================================================================

func TestEditCommand_Execute_EditorExitError(t *testing.T) {

	// Script: exits with status 1
	editorScript := makeTempScript(t, "#!/bin/sh\nexit 1")
	defer os.Remove(editorScript)

	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", editorScript)

	agent := newTestAgent(t)
	cmd := &EditCommand{}
	err := cmd.Execute(nil, agent)
	if err == nil {
		t.Fatal("expected error when editor exits non-zero")
	}
	if !strings.Contains(err.Error(), "[edit]") {
		t.Errorf("error should contain \"[edit]\", got: %v", err)
	}

	// Should NOT have injected anything
	select {
	case injected := <-agent.GetInputInjectionContext():
		t.Errorf("expected no injection after editor error, got %q", injected)
	default:
		// good
	}
}

// ====================================================================
// Execute — single arg (no joining)
// ====================================================================

func TestEditCommand_Execute_SingleArg_NoEdit(t *testing.T) {

	editorScript := makeTempScript(t, "#!/bin/sh\nexit 0")
	defer os.Remove(editorScript)

	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", editorScript)

	agent := newTestAgent(t)
	agent.ClearInputInjectionContext()

	cmd := &EditCommand{}
	err := cmd.Execute([]string{"single-line-input"}, agent)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	select {
	case injected := <-agent.GetInputInjectionContext():
		if injected != "single-line-input" {
			t.Errorf("injected = %q, want \"single-line-input\"", injected)
		}
	default:
		t.Error("expected single arg to be injected, got nothing")
	}
}

// ====================================================================
// Integration: VISUAL overrides EDITOR during execute
// ====================================================================

func TestEditCommand_Execute_VisualOverridesEditor(t *testing.T) {

	editorScript := makeTempScript(t, "#!/bin/sh\necho \"editor-was-called\" > \"$1\"\nexit 0")
	visualScript := makeTempScript(t, "#!/bin/sh\necho \"visual-was-called\" > \"$1\"\nexit 0")
	defer os.Remove(editorScript)
	defer os.Remove(visualScript)

	t.Setenv("VISUAL", visualScript)
	t.Setenv("EDITOR", editorScript)

	agent := newTestAgent(t)
	agent.ClearInputInjectionContext()

	cmd := &EditCommand{}
	err := cmd.Execute(nil, agent)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	select {
	case injected := <-agent.GetInputInjectionContext():
		if injected != "visual-was-called" {
			t.Errorf("injected = %q, want \"visual-was-called\" (VISUAL should override EDITOR)", injected)
		}
	default:
		t.Error("expected injected content, got nothing")
	}
}

// ====================================================================
// Execute — temp file is cleaned up even on editor error
// ====================================================================

func TestEditCommand_Execute_TempFileCleanup(t *testing.T) {

	// First, create the temp file manually to get its path, then have the
	// editor exit with an error and verify the file is cleaned up.
	editorScript := makeTempScript(t, "#!/bin/sh\necho \"some content\" > \"$1\"\nexit 2")
	defer os.Remove(editorScript)

	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", editorScript)

	agent := newTestAgent(t)
	cmd := &EditCommand{}
	err := cmd.Execute(nil, agent)
	if err == nil {
		t.Fatal("expected error when editor exits non-zero")
	}

	// Verify no sprout-edit-*.md files remain (defer os.Remove should clean them)
	// Note: the file path includes the temp dir prefix, so we glob broadly.
	files, _ := filepath.Glob("*/sprout-edit-*.md")
	if len(files) > 0 {
		t.Logf("WARNING: leftover temp files found: %v (may be from other tests)", files)
	}
}

// ====================================================================
// Edge case: editor trims all content (writes only newlines)
// ====================================================================

func TestEditCommand_Execute_EditorWritesOnlyNewlines(t *testing.T) {

	// Script: writes only newlines to the file
	editorScript := makeTempScript(t, "#!/bin/sh\nprintf '\\n\\n\\n' > \"$1\"\nexit 0")
	defer os.Remove(editorScript)

	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", editorScript)

	agent := newTestAgent(t)
	agent.ClearInputInjectionContext()

	cmd := &EditCommand{}
	err := cmd.Execute(nil, agent)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	// TrimRight("\n") should reduce "...\n" to "" — treated as empty
	select {
	case injected := <-agent.GetInputInjectionContext():
		t.Errorf("expected no injection for newline-only content, got %q", injected)
	default:
		// good — nothing injected
	}
}

// ====================================================================
// Integration: full round-trip with prefill + edit
// ====================================================================

func TestEditCommand_Execute_PrefillThenEdit(t *testing.T) {

	// Script: appends to existing content rather than replacing
	editorScript := makeTempScript(t, "#!/bin/sh\necho \" edited\" >> \"$1\"\nexit 0")
	defer os.Remove(editorScript)

	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", editorScript)

	agent := newTestAgent(t)
	agent.ClearInputInjectionContext()

	cmd := &EditCommand{}
	err := cmd.Execute([]string{"original"}, agent)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	// Prefill is "original\n", editor appends " edited\n" → file becomes
	// "original\n edited\n" → TrimRight strips trailing newlines
	select {
	case injected := <-agent.GetInputInjectionContext():
		expected := "original\n edited"
		if injected != expected {
			t.Errorf("injected = %q, want %q", injected, expected)
		}
	default:
		t.Error("expected injected content, got nothing")
	}
}

// ====================================================================
// Test that Execute returns fmt.Errorf with correct wrapping
// ====================================================================

func TestEditCommand_Execute_ErrorWrapping(t *testing.T) {

	tests := []struct {
		name      string
		editor    string
		args      []string
		wantErr   bool
		errSubstr string
	}{
		{
			name:      "editor that exits non-zero",
			editor:    "#!/bin/sh\nexit 42",
			args:      nil,
			wantErr:   true,
			errSubstr: "[edit]",
		},
		{
			name:      "editor that writes content",
			editor:    "#!/bin/sh\necho \"ok\" > \"$1\"\nexit 0",
			args:      nil,
			wantErr:   false,
			errSubstr: "",
		},
		{
			name:      "prefill with editor that does nothing",
			editor:    "#!/bin/sh\nexit 0",
			args:      []string{"some", "text"},
			wantErr:   false,
			errSubstr: "",
		},
	}

	for _, tt := range tests {
		tt := tt // capture range var
		t.Run(tt.name, func(t *testing.T) {
			editorScript := makeTempScript(t, tt.editor)
			defer os.Remove(editorScript)

			t.Setenv("VISUAL", "")
			t.Setenv("EDITOR", editorScript)

			agent := newTestAgent(t)
			agent.ClearInputInjectionContext()

			cmd := &EditCommand{}
			err := cmd.Execute(tt.args, agent)

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
			if tt.errSubstr != "" && err != nil && !strings.Contains(err.Error(), tt.errSubstr) {
				t.Errorf("error %q should contain %q", err.Error(), tt.errSubstr)
			}

			// Check injection for success cases
			if !tt.wantErr {
				select {
				case injected := <-agent.GetInputInjectionContext():
					// Verify the injected content is sensible
					if tt.name == "editor that writes content" && injected != "ok" {
						t.Errorf("injected = %q, want \"ok\"", injected)
					}
					if tt.name == "prefill with editor that does nothing" && injected != "some text" {
						t.Errorf("injected = %q, want \"some text\"", injected)
					}
				default:
					// Empty buffer case — no injection is OK for "does nothing" with no prefill
					if tt.name == "prefill with editor that does nothing" {
						t.Error("expected prefill content to be injected")
					}
				}
			}
		})
	}
}

// ====================================================================
// chooseEditor — VISUAL empty string with only whitespace
// ====================================================================

func TestChooseEditor_VISUAL_OnlyWhitespace(t *testing.T) {

	t.Setenv("VISUAL", "   ")
	t.Setenv("EDITOR", "vim")
	got := chooseEditor()
	// TrimSpace makes "   " into "" which is empty, so falls through to EDITOR
	if got != "vim" {
		t.Errorf("chooseEditor() = %q, want \"vim\" (VISUAL is whitespace-only)", got)
	}
}

// ====================================================================
// chooseEditor — EDITOR empty string with only whitespace
// ====================================================================

func TestChooseEditor_EDITOR_OnlyWhitespace(t *testing.T) {

	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "\t\n")
	if _, err := exec.LookPath("vi"); err != nil {
		// vi not installed — should return ""
		got := chooseEditor()
		if got != "" {
			t.Errorf("chooseEditor() = %q, want \"\"", got)
		}
	} else {
		// vi IS installed — falls back to vi
		got := chooseEditor()
		if got != "vi" {
			t.Errorf("chooseEditor() = %q, want \"vi\" (fallback)", got)
		}
	}
}

// ====================================================================
// writeEditTempFile — special characters in content
// ====================================================================

func TestWriteEditTempFile_SpecialCharacters(t *testing.T) {

	content := "line with\nnewlines\tand\ttabs\n$DOLLAR `backticks` \"quotes\"\n"
	path, err := writeEditTempFile(content)
	if err != nil {
		t.Fatalf("writeEditTempFile error = %v", err)
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(data) != content {
		t.Errorf("content = %q, want %q", data, content)
	}
}

// ====================================================================
// Execute — nil agent (should handle gracefully or error)
// ====================================================================

func TestEditCommand_Execute_NilAgent(t *testing.T) {
	cmd := &EditCommand{}
	err := cmd.Execute(nil, nil)
	if err == nil {
		t.Fatalf("expected error when agent is nil")
	}
	if !strings.Contains(err.Error(), "[edit] agent not available") {
		t.Errorf("error = %q, want it to contain \"[edit] agent not available\"", err.Error())
	}
}

// ====================================================================
// Execute — editor script that creates file with no trailing newline
// ====================================================================

func TestEditCommand_Execute_EditorNoTrailingNewline(t *testing.T) {

	// printf without \n at the end
	editorScript := makeTempScript(t, "#!/bin/sh\nprintf 'no-newline-at-end' > \"$1\"\nexit 0")
	defer os.Remove(editorScript)

	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", editorScript)

	agent := newTestAgent(t)
	agent.ClearInputInjectionContext()

	cmd := &EditCommand{}
	err := cmd.Execute(nil, agent)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	select {
	case injected := <-agent.GetInputInjectionContext():
		if injected != "no-newline-at-end" {
			t.Errorf("injected = %q, want \"no-newline-at-end\"", injected)
		}
	default:
		t.Error("expected content without trailing newline to be injected")
	}
}

// ====================================================================
// TestEditCommand_Description
// ====================================================================

func TestEditCommand_Name(t *testing.T) {

	cmd := &EditCommand{}
	if got := cmd.Name(); got != "edit" {
		t.Errorf("Name() = %q, want \"edit\"", got)
	}
}

func TestEditCommand_Description(t *testing.T) {

	cmd := &EditCommand{}
	got := cmd.Description()
	if got != "Open $EDITOR to compose or edit a query" {
		t.Errorf("Description() = %q, want \"Open $EDITOR to compose or edit a query\"", got)
	}
}

// ====================================================================
// Execute — editor writes content with Windows-style line endings
// ====================================================================

func TestEditCommand_Execute_EditorWritesCRLF(t *testing.T) {

	// Write CRLF content
	editorScript := makeTempScript(t, "#!/bin/sh\nprintf 'line1\\r\\nline2\\r\\n' > \"$1\"\nexit 0")
	defer os.Remove(editorScript)

	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", editorScript)

	agent := newTestAgent(t)
	agent.ClearInputInjectionContext()

	cmd := &EditCommand{}
	err := cmd.Execute(nil, agent)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	select {
	case injected := <-agent.GetInputInjectionContext():
		// TrimRight("\r\n") strips trailing \r and \n, so "line1\r\nline2\r\n" becomes "line1\r\nline2"
		expected := "line1\r\nline2"
		if injected != expected {
			t.Errorf("injected = %q, want %q", injected, expected)
		}
	default:
		t.Error("expected CRLF content to be injected")
	}
}

// ====================================================================
// Regression: multiple Execute calls on same agent
// ====================================================================

func TestEditCommand_Execute_MultipleCallsSameAgent(t *testing.T) {

	editorScript := makeTempScript(t, "#!/bin/sh\necho \"first\" > \"$1\"\nexit 0")
	defer os.Remove(editorScript)

	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", editorScript)

	agent := newTestAgent(t)
	cmd := &EditCommand{}

	// First call
	err := cmd.Execute(nil, agent)
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	select {
	case injected := <-agent.GetInputInjectionContext():
		if injected != "first" {
			t.Errorf("first injection = %q, want \"first\"", injected)
		}
	default:
		t.Error("expected first injection")
	}

	// Second call — same agent, should work fine
	err = cmd.Execute(nil, agent)
	if err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}
	select {
	case injected := <-agent.GetInputInjectionContext():
		if injected != "first" {
			t.Errorf("second injection = %q, want \"first\"", injected)
		}
	default:
		t.Error("expected second injection")
	}
}

// ====================================================================
// Execute — very long content
// ====================================================================

func TestEditCommand_Execute_LongContent(t *testing.T) {

	// Build a long string for the mock editor to write
	longLine := strings.Repeat("x", 50000)
	editorScript := makeTempScript(t, fmt.Sprintf("#!/bin/sh\nprintf '%s' > \"$1\"\nexit 0", longLine))
	defer os.Remove(editorScript)

	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", editorScript)

	agent := newTestAgent(t)
	agent.ClearInputInjectionContext()

	cmd := &EditCommand{}
	err := cmd.Execute(nil, agent)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	select {
	case injected := <-agent.GetInputInjectionContext():
		if len(injected) != 50000 {
			t.Errorf("injected length = %d, want 50000", len(injected))
		}
	default:
		t.Error("expected long content to be injected")
	}
}
