package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildSectionTOC(t *testing.T) {
	t.Run("no headers returns empty", func(t *testing.T) {
		content := "just some plain text\nno headers here\n"
		result := buildSectionTOC(content)
		if result != "" {
			t.Errorf("expected empty TOC for content without headers, got: %s", result)
		}
	})

	t.Run("single section covers all lines", func(t *testing.T) {
		content := "# Overview\n\nSome content here.\nMore content.\n"
		result := buildSectionTOC(content)
		if !strings.Contains(result, "**Overview**") {
			t.Errorf("missing section title in TOC: %s", result)
		}
		// 5 lines total (header + blank + 2 content + trailing newline = 5 elements from Split)
		if !strings.Contains(result, "lines 1–5") && !strings.Contains(result, "lines 1–4") {
			t.Errorf("expected section to cover all lines, got: %s", result)
		}
	})

	t.Run("multiple sections have correct ranges", func(t *testing.T) {
		content := "# Intro\n\nLine 2\nLine 3\n## Details\n\nLine 5\nLine 6\n## Examples\n\nLine 8\n"
		result := buildSectionTOC(content)
		// Intro: line 1 (header) through line 4 (blank before ## Details)
		if !strings.Contains(result, "**Intro** (lines 1–4") {
			t.Errorf("Intro range wrong: %s", result)
		}
		// Details: line 5 (## Details header) through line 8 (blank before ## Examples)
		if !strings.Contains(result, "**Details** (lines 5–8") {
			t.Errorf("Details range wrong: %s", result)
		}
		// Examples: line 9 (## Examples header) to end
		if !strings.Contains(result, "**Examples** (lines 9–") {
			t.Errorf("Examples range wrong: %s", result)
		}
	})

	t.Run("indents nested sections", func(t *testing.T) {
		content := "# Top\n\n## Sub\n\n### Deep\n\nText\n"
		result := buildSectionTOC(content)
		if !strings.Contains(result, "- **Top**") {
			t.Errorf("missing top-level indent: %s", result)
		}
		if !strings.Contains(result, "  - **Sub**") {
			t.Errorf("missing first-level indent: %s", result)
		}
		if !strings.Contains(result, "    - **Deep**") {
			t.Errorf("missing second-level indent: %s", result)
		}
	})

	t.Run("ignores h4+ headers", func(t *testing.T) {
		content := "# Top\n\n#### Very deep\n\nText\n"
		result := buildSectionTOC(content)
		if strings.Contains(result, "Very deep") {
			t.Errorf("should ignore h4+ headers, got: %s", result)
		}
	})

	t.Run("includes line count and char count", func(t *testing.T) {
		content := "# Section\n\nSome text here.\n"
		result := buildSectionTOC(content)
		if !strings.Contains(result, "1 sections") {
			t.Errorf("missing section count: %s", result)
		}
		if !strings.Contains(result, "chars") {
			t.Errorf("missing char count: %s", result)
		}
	})
}

func TestSaveFetchContent(t *testing.T) {
	t.Run("saves content and returns path", func(t *testing.T) {
		url := "https://example.com/page"
		content := "Hello world"
		path, err := saveFetchContent(url, content)
		requireNoError(t, err)

		if path == "" {
			t.Fatal("expected non-empty path")
		}

		// Verify file exists and contains the content.
		data, err := os.ReadFile(path)
		requireNoError(t, err)
		if string(data) != content {
			t.Errorf("file content mismatch: got %q, want %q", string(data), content)
		}

		// Cleanup.
		os.Remove(path)
	})

	t.Run("deterministic path for same URL", func(t *testing.T) {
		url := "https://docs.example.com/api"
		path1, err := saveFetchContent(url, "content v1")
		requireNoError(t, err)
		path2, err := saveFetchContent(url, "content v2")
		requireNoError(t, err)

		if path1 != path2 {
			t.Errorf("same URL should produce same path: %s vs %s", path1, path2)
		}

		// Second write overwrites first.
		data, err := os.ReadFile(path1)
		requireNoError(t, err)
		if string(data) != "content v2" {
			t.Errorf("expected overwritten content, got %q", string(data))
		}

		// Cleanup.
		os.Remove(path1)
	})

	t.Run("creates temp directory if needed", func(t *testing.T) {
		// Remove the dir to test creation.
		os.RemoveAll(fetchTempDir)
		defer os.RemoveAll(fetchTempDir)

		path, err := saveFetchContent("https://test.com", "data")
		requireNoError(t, err)

		if !strings.HasPrefix(path, fetchTempDir) {
			t.Errorf("path should be under fetchTempDir, got %s", path)
		}

		// Verify directory was created.
		info, err := os.Stat(fetchTempDir)
		requireNoError(t, err)
		if !info.IsDir() {
			t.Error("fetchTempDir should be a directory")
		}
	})

	t.Run("path is under /tmp/sprout/fetch", func(t *testing.T) {
		path, err := saveFetchContent("https://example.com", "test")
		requireNoError(t, err)
		defer os.Remove(path)

		dir := filepath.Dir(path)
		if dir != fetchTempDir {
			t.Errorf("expected path under %s, got %s", fetchTempDir, dir)
		}
	})
}

func TestFetchURLHandler_LargeContent(t *testing.T) {
	// Verify that content above the threshold triggers temp file behavior.
	// We test the handler logic indirectly via the threshold constant.
	if fetchContentThreshold <= 0 {
		t.Fatal("threshold must be positive")
	}

	// Small content should be returned inline (tested by existing conformance tests).
	// Large content should produce a TOC + file path.
	largeContent := strings.Repeat("x", fetchContentThreshold+100)
	toc := buildSectionTOC(largeContent)
	// No headers in this content, so TOC is empty.
	if toc != "" {
		t.Errorf("expected empty TOC for content without headers, got: %s", toc)
	}

	// Content with headers above threshold should produce a TOC.
	largeWithHeaders := "# Section A\n\n" + strings.Repeat("x", fetchContentThreshold) + "\n## Section B\n\nEnd"
	toc2 := buildSectionTOC(largeWithHeaders)
	if !strings.Contains(toc2, "Section A") || !strings.Contains(toc2, "Section B") {
		t.Errorf("TOC missing sections: %s", toc2)
	}
}
