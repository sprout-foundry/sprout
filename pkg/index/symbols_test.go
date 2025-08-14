package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildAndSearchSymbols_GoFile(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	defer os.Chdir(orig)
	_ = os.Chdir(dir)

	src := "package m\n\nfunc Hello() {}\n\ntype Person struct{}\n"
	if err := os.WriteFile("m.go", []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := BuildSymbols(dir)
	if err != nil {
		t.Fatalf("BuildSymbols: %v", err)
	}
	if idx == nil || len(idx.Files) == 0 {
		t.Fatalf("expected symbols for m.go")
	}
	// Ensure persisted file exists
	if _, err := os.Stat(filepath.Join(dir, ".ledit", "symbols.json")); err != nil {
		t.Fatalf("symbols.json missing: %v", err)
	}

	hits := SearchSymbols(idx, []string{"hello", "person"})
	if len(hits) == 0 {
		t.Fatalf("expected search hits for tokens")
	}
}
