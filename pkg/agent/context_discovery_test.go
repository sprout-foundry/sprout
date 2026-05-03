package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProcessContextContent_BasicContent(t *testing.T) {
	input := "Line one\nLine two\nLine three"
	result := processContextContent(input)

	if result != input {
		t.Errorf("basic content should pass through unchanged; got %q", result)
	}
}

func TestProcessContextContent_TableOfContentsSkipped(t *testing.T) {
	cases := []struct {
		desc string
		line string
	}{
		{"table of contents", "Table of Contents"},
		{"contents", "Contents"},
		{"navigation", "Navigation"},
		{"menu", "Menu"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			input := "Before\n" + tc.line + "\nAfter"
			result := processContextContent(input)

			if strings.Contains(result, tc.line) {
				t.Errorf("line %q should be skipped but was found in result: %q", tc.line, result)
			}

			// The next line after a skipped line is also removed
			// So "After" (the line after the TOC line) is gone too
			// The result should be just "Before\n" (trailing newline from join) or "Before"
		})
	}
}

func TestProcessContextContent_IndexHeadersSkipped(t *testing.T) {
	cases := []struct {
		desc string
		line string
	}{
		{"index header", "# Index"},
		{"toc header", "## TOC"},
		{"toc header lowercase", "## toc"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			input := "Before\n" + tc.line + "\nAfter"
			result := processContextContent(input)

			if strings.Contains(result, tc.line) {
				t.Errorf("header %q should be skipped but was found in result: %q", tc.line, result)
			}
		})
	}
}

func TestProcessContextContent_CodeBlocksPreserved(t *testing.T) {
	input := "```go\nfunc main() {}\n```"
	result := processContextContent(input)

	if !strings.Contains(result, "```go") {
		t.Error("code block marker should be preserved")
	}
	if !strings.Contains(result, "func main()") {
		t.Error("code content should be preserved")
	}
}

func TestProcessContextContent_ExcessiveEmptyLinesRemoved(t *testing.T) {
	// Triple newlines → double
	input := "Line1\n\n\nLine2"
	result := processContextContent(input)

	// Should not contain triple or more newlines
	if strings.Contains(result, "\n\n\n") {
		t.Errorf("triple empty lines should be reduced; got %q", result)
	}
}

func TestProcessContextContent_LeadingEmptyLineStripped(t *testing.T) {
	input := "\nLine one\nLine two"
	result := processContextContent(input)

	if strings.HasPrefix(result, "\n") {
		t.Errorf("leading empty line should be stripped; got %q", result)
	}
	if !strings.HasPrefix(result, "Line one") {
		t.Errorf("first line should be 'Line one'; got %q", result)
	}
}

func TestDiscoverContextFiles_NoFilesFound(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	info, err := DiscoverContextFiles()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info != nil {
		t.Errorf("expected nil when no context files exist; got %+v", info)
	}
}

func TestDiscoverContextFiles_FoundFileInCWD(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	content := "# Agent Configuration\nTest content"
	if err := os.WriteFile(filepath.Join(tmpDir, "AGENTS.md"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to create AGENTS.md: %v", err)
	}

	info, err := DiscoverContextFiles()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected AGENTS.md to be found")
	}

	if info.Path != filepath.Join(tmpDir, "AGENTS.md") {
		t.Errorf("expected path %q; got %q", filepath.Join(tmpDir, "AGENTS.md"), info.Path)
	}
	if info.Description != "Agent configuration and context" {
		t.Errorf("expected description 'Agent configuration and context'; got %q", info.Description)
	}
	if info.Priority != 1 {
		t.Errorf("expected priority 1; got %d", info.Priority)
	}
}

func TestDiscoverContextFiles_PriorityOrder(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	// Create both AGENTS.md and README.md
	if err := os.WriteFile(filepath.Join(tmpDir, "AGENTS.md"), []byte("Agents content"), 0644); err != nil {
		t.Fatalf("failed to create AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("README content"), 0644); err != nil {
		t.Fatalf("failed to create README.md: %v", err)
	}

	info, err := DiscoverContextFiles()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected a context file to be found")
	}
	if info.Path != filepath.Join(tmpDir, "AGENTS.md") {
		t.Errorf("AGENTS.md should have priority over README.md; got %q", info.Path)
	}
}

func TestDiscoverContextFiles_ReadContent(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	expectedContent := "# My Project\n\nThis is some project context."
	if err := os.WriteFile(filepath.Join(tmpDir, "AGENTS.md"), []byte(expectedContent), 0644); err != nil {
		t.Fatalf("failed to create AGENTS.md: %v", err)
	}

	info, err := DiscoverContextFiles()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected AGENTS.md to be found")
	}
	if info.Content != expectedContent {
		t.Errorf("content mismatch: expected %q; got %q", expectedContent, info.Content)
	}
}

func TestLoadContextFiles_NoContext(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	result, err := LoadContextFiles()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty string when no context files exist; got %q", result)
	}
}

func TestLoadContextFiles_WithContext(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	content := "# Agent Rules\n\nDo things properly."
	if err := os.WriteFile(filepath.Join(tmpDir, "AGENTS.md"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to create AGENTS.md: %v", err)
	}

	result, err := LoadContextFiles()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result when context file exists")
	}

	// Check that the formatted output contains expected sections
	if !strings.Contains(result, "Agent configuration and context") {
		t.Errorf("expected description in output; got %q", result)
	}
	if !strings.Contains(result, "Loaded from:") {
		t.Errorf("expected 'Loaded from:' in output; got %q", result)
	}
	if !strings.Contains(result, "Agent Rules") {
		t.Errorf("expected processed content in output; got %q", result)
	}
}
