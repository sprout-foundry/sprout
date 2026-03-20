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

func TestPrepareUserInputForModel_UseAutomationLimitByDefault(t *testing.T) {
	ch := &ConversationHandler{}

	// Input within automation limit
	input := strings.Repeat("x", 20000)
	got := ch.prepareUserInputForModel(input)

	// Should NOT truncate since input is below automation limit
	if strings.Contains(got, "USER INPUT TRUNCATED FOR MODEL CONTEXT") {
		t.Fatalf("unexpected truncation for input within automation limit")
	}
}

func TestPrepareUserInputForModel_UseInteractiveLimitWhenInteractive(t *testing.T) {
	agent := &Agent{}
	ch := &ConversationHandler{agent: agent}
	t.Setenv("LEDIT_INTERACTIVE", "1")

	// Input larger than interactive limit (100000)
	input := strings.Repeat("y", 110000)
	got := ch.prepareUserInputForModel(input)

	// SHOULD truncate since input exceeds interactive limit
	if !strings.Contains(got, "USER INPUT TRUNCATED FOR MODEL CONTEXT") {
		t.Fatalf("expected truncation for input exceeding interactive limit")
	}
	// The returned content should mention LEDIT_INTERACTIVE_INPUT_MAX_CHARS
	if !strings.Contains(got, "LEDIT_INTERACTIVE_INPUT_MAX_CHARS") {
		t.Fatalf("expected truncation notice to mention LEDIT_INTERACTIVE_INPUT_MAX_CHARS")
	}
}

func TestPrepareUserInputForModel_UseAutomationLimitWhenAutomation(t *testing.T) {
	agent := &Agent{}
	ch := &ConversationHandler{agent: agent}
	t.Setenv("LEDIT_FROM_AGENT", "1") // Forces automation mode

	// Input within automation limit (50000)
	input := strings.Repeat("z", 20000)
	got := ch.prepareUserInputForModel(input)

	// Should NOT truncate since input is within automation limit
	if strings.Contains(got, "USER INPUT TRUNCATED FOR MODEL CONTEXT") {
		t.Fatalf("unexpected truncation for input within automation limit")
	}
}

