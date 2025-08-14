package playbooks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDedupeStrings(t *testing.T) {
	in := []string{"a", "b", "a", "c", "b", ""}
	got := dedupeStrings(in)
	want := []string{"a", "b", "c"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("dedupeStrings got %v want %v", got, want)
	}
}

func TestStartsWithGoComment(t *testing.T) {
	dir := t.TempDir()
	with := filepath.Join(dir, "with.go")
	without := filepath.Join(dir, "without.go")
	if err := os.WriteFile(with, []byte("// header\npackage x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(without, []byte("package x\n// later\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if !startsWithGoComment(with) {
		t.Fatalf("expected startsWithGoComment true for %s", with)
	}
	if startsWithGoComment(without) {
		t.Fatalf("expected startsWithGoComment false for %s", without)
	}
}

func TestFindBuildErrorCandidateFiles_SymbolDiscovery(t *testing.T) {
	// Work inside a temp directory to avoid scanning the whole repo
	orig, _ := os.Getwd()
	dir := t.TempDir()
	defer os.Chdir(orig)
	_ = os.Chdir(dir)

	// Create a Go file that defines FooSym
	if err := os.WriteFile("a.go", []byte("package a\n\ntype FooSym int\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Provide intent containing undefined: FooSym to trigger symbol scan
	res := findBuildErrorCandidateFiles("undefined: FooSym in file a.go")
	// Expect that a.go is in the results
	found := false
	for _, r := range res {
		if r == "a.go" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a.go in results, got %v", res)
	}
}

func TestFindFailingTestCandidateFiles_ByTestName(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	defer os.Chdir(orig)
	_ = os.Chdir(dir)

	// Create a source file and its test file with a known test name
	if err := os.WriteFile("mod.go", []byte("package mod\n\nfunc Add(a,b int) int { return a+b }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testContent := "package mod\n\nimport \"testing\"\n\nfunc TestAbc(t *testing.T){ if Add(1,2)!=3 { t.Fatal(\"bad\") } }\n"
	if err := os.WriteFile("mod_test.go", []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	res := findFailingTestCandidateFiles("--- FAIL: TestAbc (0.00s)\n\tmod_test.go:12: assertion failed")
	// Expect both test and src file to be referenced
	hasTest := false
	hasSrc := false
	for _, r := range res {
		if r == "mod_test.go" {
			hasTest = true
		}
		if r == "mod.go" {
			hasSrc = true
		}
	}
	if !hasTest || !hasSrc {
		t.Fatalf("expected mod_test.go and mod.go in results, got %v", res)
	}
}
