package health

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile writes content at root/rel, creating parent dirs.
func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// repeatedLines builds a source file of exactly n lines.
func repeatedLines(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString("// filler\n")
	}
	return b.String()
}

func TestScanSourcesCountsLinesAndSkipsDirs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "small.go", "package x\n\nfunc A() int { return 1 }\n")
	writeFile(t, root, "big.go", repeatedLines(50))
	// node_modules and hidden dirs must be skipped.
	writeFile(t, root, "node_modules/dep.go", repeatedLines(500))
	writeFile(t, root, ".hidden/secret.go", repeatedLines(500))
	// A non-source file is ignored.
	writeFile(t, root, "README.md", repeatedLines(500))

	res, err := ScanSources(root, 0)
	if err != nil {
		t.Fatalf("ScanSources: %v", err)
	}

	paths := map[string]int{}
	for _, f := range res.Files {
		paths[f.File] = f.Lines
	}
	if _, ok := paths["small.go"]; !ok {
		t.Fatalf("small.go missing from scan: %v", paths)
	}
	if got := paths["big.go"]; got != 50 {
		t.Fatalf("big.go lines = %d, want 50", got)
	}
	for _, skipped := range []string{"node_modules/dep.go", ".hidden/secret.go", "README.md"} {
		if _, ok := paths[skipped]; ok {
			t.Errorf("%s should have been skipped", skipped)
		}
	}
}

func TestScanSourcesComputesFunctionComplexity(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "hot.go", `package x

func Simple(x int) int {
	return x
}

func Hot(xs []int) int {
	n := 0
	for _, x := range xs {
		if x > 0 {
			if x%2 == 0 {
				n++
			}
		}
	}
	return n
}
`)
	res, err := ScanSources(root, 0)
	if err != nil {
		t.Fatalf("ScanSources: %v", err)
	}

	byName := map[string]FunctionComplexity{}
	for _, fn := range res.Functions {
		byName[fn.Name] = fn
	}
	simple, ok := byName["Simple"]
	if !ok {
		t.Fatalf("Simple not found; functions: %+v", res.Functions)
	}
	if simple.Complexity != 1 {
		t.Errorf("Simple complexity = %d, want 1", simple.Complexity)
	}
	hot, ok := byName["Hot"]
	if !ok {
		t.Fatalf("Hot not found; functions: %+v", res.Functions)
	}
	// for + if + if = 3 branches → 4
	if hot.Complexity != 4 {
		t.Errorf("Hot complexity = %d, want 4", hot.Complexity)
	}
}

func TestScanSourcesBoundsFileCount(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 10; i++ {
		writeFile(t, root, filepath.Join("pkg", "f"+string(rune('a'+i))+".go"), "package x\n")
	}
	res, err := ScanSources(root, 3)
	if err != nil {
		t.Fatalf("ScanSources: %v", err)
	}
	if len(res.Files) > 3 {
		t.Fatalf("scan returned %d files, want at most 3", len(res.Files))
	}
}

func TestScanSourcesRequiresRoot(t *testing.T) {
	if _, err := ScanSources("", 0); err == nil {
		t.Fatal("expected an error for an empty root")
	}
}
