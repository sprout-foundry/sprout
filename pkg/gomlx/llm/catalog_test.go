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
	for _, d := range []string{"gemma-4-e2b-it-4bit"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// lfm2.5-2.6b-mlx/4bit is NOT created, so it must be skipped.

	cases := []struct {
		name string
		ram  uint64
		want string // expected Dir basename, or "" for error
	}{
		{"8gb_no_lfm", 8 * 1024 * 1024 * 1024, "gemma-4-e2b-it-4bit"},
		{"4gb", 4 * 1024 * 1024 * 1024, "gemma-4-e2b-it-4bit"},
		{"1gb", 1 * 1024 * 1024 * 1024, "gemma-4-e2b-it-4bit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := SelectModelForRAM(root, tc.ram)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if filepath.Base(m.Dir) != tc.want {
				t.Fatalf("got %s, want %s", filepath.Base(m.Dir), tc.want)
			}
		})
	}
}

// TestCatalogSizes ensures catalog entries are ordered largest-first so the
// selection loop's first fit is the best fit.
func TestCatalogSizes(t *testing.T) {
	for i := 1; i < len(ModelCatalog); i++ {
		if ModelCatalog[i-1].MinRAM < ModelCatalog[i].MinRAM {
			t.Fatalf("catalog not ordered largest-first: %s (%d) before %s (%d)",
				ModelCatalog[i-1].Name, ModelCatalog[i-1].MinRAM,
				ModelCatalog[i].Name, ModelCatalog[i].MinRAM)
		}
	}
}

// TestRecommendModelForRAM checks the pure-RAM recommendation.
func TestRecommendModelForRAM(t *testing.T) {
	cases := []struct {
		ram  uint64
		want string
	}{
		{32 * 1024 * 1024 * 1024, "lfm2.5-2.6b"},
		{8 * 1024 * 1024 * 1024, "lfm2.5-2.6b"},
		{4 * 1024 * 1024 * 1024, "gemma4-e2b"},
		{1 * 1024 * 1024 * 1024, "gemma4-e2b"},
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
