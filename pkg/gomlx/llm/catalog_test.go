package llm

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSelectModelForRAM checks the catalog picks the largest model whose
// MinRAM fits, skipping entries whose directory is not installed.
func TestSelectModelForRAM(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"qwen3.5-4b-4bit", "qwen3.5-0.8b-4bit"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// The 9B is intentionally NOT created, so selection must skip it.

	cases := []struct {
		name string
		ram  uint64
		want string // expected Dir, or "" for error
	}{
		{"8gb", 8 * 1024 * 1024 * 1024, "qwen3.5-0.8b-4bit"},
		{"14gb", 14 * 1024 * 1024 * 1024, "qwen3.5-4b-4bit"},
		{"16gb", 16 * 1024 * 1024 * 1024, "qwen3.5-4b-4bit"},
		{"32gb", 32 * 1024 * 1024 * 1024, "qwen3.5-4b-4bit"}, // 9B dir absent → 4B
		{"1gb", 1 * 1024 * 1024 * 1024, "qwen3.5-0.8b-4bit"}, // 0.8B is the universal fallback (MinRAM 0)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := SelectModelForRAM(root, tc.ram)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// m.Dir is resolved to an absolute path; compare basename.
			if filepath.Base(m.Dir) != tc.want {
				t.Fatalf("got %s, want %s", filepath.Base(m.Dir), tc.want)
			}
		})
	}
}

// TestCatalogSizes ensures catalog entries are ordered largest-first so the
// selection loop's first fit is the best fit (documented invariant).
func TestCatalogSizes(t *testing.T) {
	for i := 1; i < len(ModelCatalog); i++ {
		if ModelCatalog[i-1].MinRAM < ModelCatalog[i].MinRAM {
			t.Fatalf("catalog not ordered largest-first: %s (%d) before %s (%d)",
				ModelCatalog[i-1].Name, ModelCatalog[i-1].MinRAM,
				ModelCatalog[i].Name, ModelCatalog[i].MinRAM)
		}
	}
}

// TestRecommendModelForRAM checks the pure-RAM recommendation (no disk check)
// used by the download helper: 32 GB → 9B, 16 GB → 4B, 8 GB → 2B, tiny → 0.8B.
func TestRecommendModelForRAM(t *testing.T) {
	cases := []struct {
		ram  uint64
		want string
	}{
		{32 * 1024 * 1024 * 1024, "qwen3.5-9b"},
		{16 * 1024 * 1024 * 1024, "qwen3.5-4b"},
		{14 * 1024 * 1024 * 1024, "qwen3.5-4b"},
		{8 * 1024 * 1024 * 1024, "qwen3.5-2b"},
		{4 * 1024 * 1024 * 1024, "qwen3.5-0.8b"},
		{1 * 1024 * 1024 * 1024, "qwen3.5-0.8b"},
	}
	for _, tc := range cases {
		m := RecommendModelForRAM(tc.ram)
		if m == nil || m.Name != tc.want {
			t.Fatalf("RecommendModelForRAM(%d) = %+v, want %s", tc.ram, m, tc.want)
		}
		if m.HFRepo == "" {
			t.Fatalf("catalog entry %s missing HFRepo", m.Name)
		}
	}
}
