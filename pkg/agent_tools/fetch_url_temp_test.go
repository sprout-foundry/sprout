package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestBuildSectionTOC(t *testing.T) {
	t.Run("no headers returns fallback summary", func(t *testing.T) {
		content := "just some plain text\nno headers here\n"
		result := buildSectionTOC(content)
		if strings.Contains(result, "sections") {
			t.Errorf("should not mention sections for headerless content: %s", result)
		}
		if !strings.Contains(result, "no section headers") {
			t.Errorf("expected fallback message, got: %s", result)
		}
		if !strings.Contains(result, "chars") {
			t.Errorf("expected char count in fallback, got: %s", result)
		}
		if !strings.Contains(result, "lines") {
			t.Errorf("expected line count in fallback, got: %s", result)
		}
	})

	t.Run("single section covers all lines", func(t *testing.T) {
		content := "# Overview\n\nSome content here.\nMore content.\n"
		result := buildSectionTOC(content)
		if !strings.Contains(result, "**Overview**") {
			t.Errorf("missing section title in TOC: %s", result)
		}
		if !strings.Contains(result, "lines 1–5") && !strings.Contains(result, "lines 1–4") {
			t.Errorf("expected section to cover all lines, got: %s", result)
		}
	})

	t.Run("multiple sections have correct ranges", func(t *testing.T) {
		content := "# Intro\n\nLine 2\nLine 3\n## Details\n\nLine 5\nLine 6\n## Examples\n\nLine 8\n"
		result := buildSectionTOC(content)
		if !strings.Contains(result, "**Intro** (lines 1–4") {
			t.Errorf("Intro range wrong: %s", result)
		}
		if !strings.Contains(result, "**Details** (lines 5–8") {
			t.Errorf("Details range wrong: %s", result)
		}
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
		if err != nil {
			t.Fatalf("saveFetchContent failed: %v", err)
		}

		if path == "" {
			t.Fatal("expected non-empty path")
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		if string(data) != content {
			t.Errorf("file content mismatch: got %q, want %q", string(data), content)
		}

		os.Remove(path)
	})

	t.Run("deterministic path for same URL", func(t *testing.T) {
		url := "https://docs.example.com/api"
		path1, err := saveFetchContent(url, "content v1")
		if err != nil {
			t.Fatalf("saveFetchContent v1 failed: %v", err)
		}
		path2, err := saveFetchContent(url, "content v2")
		if err != nil {
			t.Fatalf("saveFetchContent v2 failed: %v", err)
		}

		if path1 != path2 {
			t.Errorf("same URL should produce same path: %s vs %s", path1, path2)
		}

		data, err := os.ReadFile(path1)
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		if string(data) != "content v2" {
			t.Errorf("expected overwritten content, got %q", string(data))
		}

		os.Remove(path1)
	})

	t.Run("creates temp directory if needed", func(t *testing.T) {
		os.RemoveAll(fetchTempDir())
		defer os.RemoveAll(fetchTempDir())

		path, err := saveFetchContent("https://test.com", "data")
		if err != nil {
			t.Fatalf("saveFetchContent failed: %v", err)
		}

		if !strings.HasPrefix(path, fetchTempDir()) {
			t.Errorf("path should be under fetchTempDir, got %s", path)
		}

		info, err := os.Stat(fetchTempDir())
		if err != nil {
			t.Fatalf("Stat failed: %v", err)
		}
		if !info.IsDir() {
			t.Error("fetchTempDir should be a directory")
		}
	})

	t.Run("path is under the fetch temp dir", func(t *testing.T) {
		path, err := saveFetchContent("https://example.com", "test")
		if err != nil {
			t.Fatalf("saveFetchContent failed: %v", err)
		}
		defer os.Remove(path)

		dir := filepath.Dir(path)
		if dir != fetchTempDir() {
			t.Errorf("expected path under %s, got %s", fetchTempDir(), dir)
		}
	})

	t.Run("file permissions are 0600", func(t *testing.T) {
		path, err := saveFetchContent("https://perm-test.com", "secret")
		if err != nil {
			t.Fatalf("saveFetchContent failed: %v", err)
		}
		defer os.Remove(path)

		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat failed: %v", err)
		}
		mode := info.Mode().Perm()
		if mode != 0600 {
			t.Errorf("expected file permissions 0600, got %o", mode)
		}
	})

	t.Run("concurrent writes to same URL", func(t *testing.T) {
		url := "https://example.com/race"
		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				content := fmt.Sprintf("content-%d", n)
				path, err := saveFetchContent(url, content)
				if err != nil {
					t.Errorf("saveFetchContent failed: %v", err)
					return
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Errorf("ReadFile failed: %v", err)
					return
				}
				found := false
				for k := 0; k < 10; k++ {
					if string(data) == fmt.Sprintf("content-%d", k) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("file contains garbled content: %q", string(data))
				}
			}(i)
		}
		wg.Wait()
	})
}

func TestEvictOldFiles(t *testing.T) {
	dir := fetchTempDir()
	os.RemoveAll(dir)
	defer os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	for i := 0; i < maxFetchFiles+5; i++ {
		path := filepath.Join(dir, fmt.Sprintf("fetch_%08d.txt", i))
		if err := os.WriteFile(path, []byte(fmt.Sprintf("content-%d", i)), 0600); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}

	_, err := saveFetchContent("https://eviction-test.com", "new content")
	if err != nil {
		t.Fatalf("saveFetchContent failed: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() && !strings.HasSuffix(e.Name(), ".tmp") {
			count++
		}
	}
	if count > maxFetchFiles+1 {
		t.Errorf("expected at most %d files after eviction, got %d", maxFetchFiles+1, count)
	}
}

func TestFetchURLHandler_LargeContent(t *testing.T) {
	if fetchContentThreshold <= 0 {
		t.Fatal("threshold must be positive")
	}

	largeContent := strings.Repeat("x", fetchContentThreshold+100)
	toc := buildSectionTOC(largeContent)
	if !strings.Contains(toc, "no section headers") {
		t.Errorf("expected fallback summary for headerless content, got: %s", toc)
	}

	largeWithHeaders := "# Section A\n\n" + strings.Repeat("x", fetchContentThreshold) + "\n## Section B\n\nEnd"
	toc2 := buildSectionTOC(largeWithHeaders)
	if !strings.Contains(toc2, "Section A") || !strings.Contains(toc2, "Section B") {
		t.Errorf("TOC missing sections: %s", toc2)
	}
}
