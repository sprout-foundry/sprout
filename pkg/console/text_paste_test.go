package console

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShouldSmartSavePaste(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty", "", false},
		{"short", "hello world", false},
		{"few-lines", strings.Repeat("line\n", 10), false},
		{"exactly-threshold-lines", strings.Repeat("x\n", SmartPasteLineThreshold-1), false},
		{"over-threshold-lines", strings.Repeat("x\n", SmartPasteLineThreshold+5), true},
		{"under-byte-threshold", strings.Repeat("a", SmartPasteByteThreshold-1), false},
		{"over-byte-threshold", strings.Repeat("a", SmartPasteByteThreshold+1), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ShouldSmartSavePaste(c.content); got != c.want {
				t.Errorf("ShouldSmartSavePaste(len=%d, lines≈%d) = %v, want %v",
					len(c.content), strings.Count(c.content, "\n"), got, c.want)
			}
		})
	}
}

func TestSavePastedText_WritesFileAndReturnsPath(t *testing.T) {
	tmpDir := t.TempDir()
	content := "first line\nsecond line\nthird line\n"

	path, err := SavePastedText(content, tmpDir)
	if err != nil {
		t.Fatalf("SavePastedText failed: %v", err)
	}

	// Returned path should be a relative reference suitable for `@path`.
	if !strings.HasPrefix(path, "./"+PastedTextDirName+"/") {
		t.Errorf("expected path under ./%s/, got %q", PastedTextDirName, path)
	}

	// Verify file exists and has correct content.
	absPath := filepath.Join(tmpDir, strings.TrimPrefix(path, "./"))
	got, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("file should exist at %s, got error: %v", absPath, err)
	}
	if string(got) != content {
		t.Errorf("file content mismatch:\n  got:  %q\n  want: %q", string(got), content)
	}
}

func TestSavePastedText_GeneratesUniqueFilenames(t *testing.T) {
	tmpDir := t.TempDir()
	paths := make(map[string]bool)
	for i := 0; i < 5; i++ {
		path, err := SavePastedText("content", tmpDir)
		if err != nil {
			t.Fatalf("save %d failed: %v", i, err)
		}
		if paths[path] {
			t.Errorf("duplicate path generated: %q", path)
		}
		paths[path] = true
	}
}

func TestSavePastedText_HandlesEmptyContent(t *testing.T) {
	tmpDir := t.TempDir()
	path, err := SavePastedText("", tmpDir)
	if err != nil {
		t.Fatalf("empty content should be permitted: %v", err)
	}
	absPath := filepath.Join(tmpDir, strings.TrimPrefix(path, "./"))
	info, err := os.Stat(absPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("expected empty file, got %d bytes", info.Size())
	}
}
