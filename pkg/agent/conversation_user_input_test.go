package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareUserInputForModel_SmallInputUnchanged(t *testing.T) {
	ch := &ConversationHandler{}
	t.Setenv("LEDIT_USER_INPUT_MAX_CHARS", "2000")
	input := "small input"

	got := ch.prepareUserInputForModel(input)
	if got != input {
		t.Fatalf("expected input to remain unchanged")
	}
}

func TestPrepareUserInputForModel_TruncatesAndArchivesLargeInput(t *testing.T) {
	ch := &ConversationHandler{}
	t.Setenv("LEDIT_USER_INPUT_MAX_CHARS", "100")
	archiveDir := t.TempDir()
	t.Setenv("LEDIT_USER_INPUT_ARCHIVE_DIR", archiveDir)

	input := strings.Repeat("p", 260)
	got := ch.prepareUserInputForModel(input)

	if !strings.Contains(got, "USER INPUT TRUNCATED FOR MODEL CONTEXT") {
		t.Fatalf("expected truncation marker")
	}
	if !strings.Contains(got, "Full input saved to ") {
		t.Fatalf("expected archive path marker")
	}
	if !strings.HasPrefix(got, strings.Repeat("p", 70)) {
		t.Fatalf("expected head segment to be preserved")
	}
	if !strings.HasSuffix(got, strings.Repeat("p", 30)) {
		t.Fatalf("expected tail segment to be preserved")
	}

	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		t.Fatalf("failed to read archive dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 archived file, got %d", len(entries))
	}

	path := filepath.Join(archiveDir, entries[0].Name())
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read archived file: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, input) {
		t.Fatalf("expected archived file to contain full input")
	}
}
