package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLimitVisionOutputText_NoTruncation(t *testing.T) {
	text := "short text"
	out, truncated, original := limitVisionOutputText(text)
	if truncated {
		t.Fatalf("expected non-truncated output")
	}
	if original != len(text) {
		t.Fatalf("expected original len %d, got %d", len(text), original)
	}
	if out != text {
		t.Fatalf("expected identical output, got %q", out)
	}
}

func TestLimitVisionOutputText_TruncatesLargePayload(t *testing.T) {
	text := strings.Repeat("A", visionMaxReturnedTextChars+500)
	out, truncated, original := limitVisionOutputText(text)
	if !truncated {
		t.Fatalf("expected truncation")
	}
	if original != len(text) {
		t.Fatalf("expected original len %d, got %d", len(text), original)
	}
	if len(out) > visionMaxReturnedTextChars+120 {
		t.Fatalf("expected bounded output size, got len=%d", len(out))
	}
	if !strings.Contains(out, "[TRUNCATED: returned first") {
		t.Fatalf("expected truncation marker, got %q", out)
	}
}

func TestPersistVisionFullText_WritesReadableRelativePath(t *testing.T) {
	workDir := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWD) })

	t.Setenv("LEDIT_RESOURCE_DIRECTORY", "captures")
	outPath, err := persistVisionFullText("https://example.com/menu.pdf", "full text body")
	if err != nil {
		t.Fatalf("persistVisionFullText failed: %v", err)
	}
	if !strings.HasPrefix(outPath, "./captures/") {
		t.Fatalf("expected relative path under captures, got %s", outPath)
	}
	fullPath := filepath.Join(workDir, strings.TrimPrefix(outPath, "./"))
	data, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("expected output file to exist: %v", err)
	}
	if string(data) != "full text body" {
		t.Fatalf("unexpected output text: %q", string(data))
	}
}
