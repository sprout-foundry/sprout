package console

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestPromptLargePasteAction_Use(t *testing.T) {
	in := bytes.NewBufferString("u")
	got := promptLargePasteAction("some content", in)
	if got != actionUseInline {
		t.Errorf("got %v, want actionUseInline", got)
	}
}

func TestPromptLargePasteAction_UseUpper(t *testing.T) {
	in := bytes.NewBufferString("U")
	got := promptLargePasteAction("some content", in)
	if got != actionUseInline {
		t.Errorf("got %v, want actionUseInline", got)
	}
}

func TestPromptLargePasteAction_Save(t *testing.T) {
	in := bytes.NewBufferString("s")
	got := promptLargePasteAction("some content", in)
	if got != actionSaveAsFile {
		t.Errorf("got %v, want actionSaveAsFile", got)
	}
}

func TestPromptLargePasteAction_SaveUpper(t *testing.T) {
	in := bytes.NewBufferString("S")
	got := promptLargePasteAction("some content", in)
	if got != actionSaveAsFile {
		t.Errorf("got %v, want actionSaveAsFile", got)
	}
}

func TestPromptLargePasteAction_DefaultCR(t *testing.T) {
	in := bytes.NewBufferString("\r")
	got := promptLargePasteAction("some content", in)
	if got != actionSaveAsFile {
		t.Errorf("got %v, want actionSaveAsFile", got)
	}
}

func TestPromptLargePasteAction_DefaultLF(t *testing.T) {
	in := bytes.NewBufferString("\n")
	got := promptLargePasteAction("some content", in)
	if got != actionSaveAsFile {
		t.Errorf("got %v, want actionSaveAsFile", got)
	}
}

func TestPromptLargePasteAction_Cancel(t *testing.T) {
	in := bytes.NewBufferString("c")
	got := promptLargePasteAction("some content", in)
	if got != actionCancel {
		t.Errorf("got %v, want actionCancel", got)
	}
}

func TestPromptLargePasteAction_CancelUpper(t *testing.T) {
	in := bytes.NewBufferString("C")
	got := promptLargePasteAction("some content", in)
	if got != actionCancel {
		t.Errorf("got %v, want actionCancel", got)
	}
}

func TestPromptLargePasteAction_InvalidThenValid(t *testing.T) {
	// 'x' is invalid → re-prompts, then 'u' is valid → actionUseInline.
	// Do NOT use "x\nu" — '\n' is valid and returns actionSaveAsFile before reaching 'u'.
	in := bytes.NewBufferString("xu")
	got := promptLargePasteAction("some content", in)
	if got != actionUseInline {
		t.Errorf("got %v, want actionUseInline", got)
	}
}

func TestPromptLargePasteAction_EOF(t *testing.T) {
	// Empty reader triggers EOF on first Read → defaults to actionSaveAsFile.
	var in bytes.Buffer
	got := promptLargePasteAction("some content", &in)
	if got != actionSaveAsFile {
		t.Errorf("got %v, want actionSaveAsFile", got)
	}
}

func TestPromptLargePasteAction_MultipleInvalidThenValid(t *testing.T) {
	// 'a', 'b', 'c' — wait, 'c' is valid! Use 'a', 'b', 'd' then 'u'.
	// 'a' → re-prompt, 'b' → re-prompt, 'd' → re-prompt, 'u' → actionUseInline.
	in := bytes.NewBufferString("abdgu")
	got := promptLargePasteAction("some content", in)
	if got != actionUseInline {
		t.Errorf("got %v, want actionUseInline", got)
	}
}

func TestPromptLargePasteAction_InvalidThenDefaults(t *testing.T) {
	// 'x' is invalid → re-prompts, then '\n' is valid → actionSaveAsFile (default).
	in := bytes.NewBufferString("x\n")
	got := promptLargePasteAction("some content", in)
	if got != actionSaveAsFile {
		t.Errorf("got %v, want actionSaveAsFile", got)
	}
}

func TestPromptLargePasteAction_ContentUsedForStats(t *testing.T) {
	// Verify that content is used (line/byte counts) even though we can't easily capture stderr here.
	// Just confirm that different content doesn't change the behavior for the same input.
	longContent := strings.Repeat("line\n", 200)

	in1 := bytes.NewBufferString("u")
	got1 := promptLargePasteAction(longContent, in1)

	in2 := bytes.NewBufferString("u")
	got2 := promptLargePasteAction("short", in2)

	if got1 != actionUseInline || got2 != actionUseInline {
		t.Errorf("content should not affect decision for same input: got %v, %v, want actionUseInline, actionUseInline", got1, got2)
	}
}

func TestPromptLargePasteAction_ReadError(t *testing.T) {
	// A reader that returns an error on first read should default to actionSaveAsFile.
	errReader := &errorReader{err: io.EOF}
	got := promptLargePasteAction("some content", errReader)
	if got != actionSaveAsFile {
		t.Errorf("got %v, want actionSaveAsFile", got)
	}
}

// errorReader returns (0, err) on every Read call.
type errorReader struct{ err error }

func (e *errorReader) Read(p []byte) (int, error) {
	return 0, e.err
}
