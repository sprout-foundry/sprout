package console

import (
	"os"
	"strings"
	"testing"
)

func TestChooseExternalEditor_VisualWins(t *testing.T) {
	t.Setenv("VISUAL", "my-special-editor")
	t.Setenv("EDITOR", "vi")
	if got := chooseExternalEditor(); got != "my-special-editor" {
		t.Errorf("VISUAL should win, got %q", got)
	}
}

func TestChooseExternalEditor_FallsBackToEditor(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "vim")
	if got := chooseExternalEditor(); got != "vim" {
		t.Errorf("EDITOR should be used when VISUAL is unset, got %q", got)
	}
}

func TestChooseExternalEditor_TrimsWhitespace(t *testing.T) {
	t.Setenv("VISUAL", "  nano  ")
	t.Setenv("EDITOR", "")
	if got := chooseExternalEditor(); got != "nano" {
		t.Errorf("whitespace should be trimmed, got %q", got)
	}
}

func TestChooseExternalEditor_FallbackProbe(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	// At least one of nano / vim / vi is virtually always installed on
	// any system that runs a Go test suite, so chooseExternalEditor
	// should find something.
	got := chooseExternalEditor()
	if got == "" {
		t.Skip("none of nano/vim/vi installed — environment-specific, skipping")
	}
	if got != "nano" && got != "vim" && got != "vi" {
		t.Errorf("expected one of nano/vim/vi, got %q", got)
	}
}

func TestWriteBufferToTempFile_RoundTrip(t *testing.T) {
	content := "hello\nworld\n# heading\n"
	path, err := writeBufferToTempFile(content)
	if err != nil {
		t.Fatalf("writeBufferToTempFile failed: %v", err)
	}
	defer os.Remove(path)

	if !strings.HasSuffix(path, ".md") {
		t.Errorf("expected .md suffix on temp file, got %q", path)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read-back failed: %v", err)
	}
	if string(got) != content {
		t.Errorf("round-trip mismatch: got %q, want %q", string(got), content)
	}
}

func TestWriteBufferToTempFile_EmptyBuffer(t *testing.T) {
	path, err := writeBufferToTempFile("")
	if err != nil {
		t.Fatalf("empty buffer should be permitted, got error: %v", err)
	}
	defer os.Remove(path)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read-back failed: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty file, got %d bytes", len(got))
	}
}
