package zsh

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLooksLikeCommandLine(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte("build:\n\tgo build\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	cases := map[string]bool{
		"go":                        false,
		"go on":                     false,
		"go test ./...":             true,
		"make sure the tests pass":  false,
		"make build":                true,
		"find the bug in auth":      false,
		"find . -name '*.go'":       true,
		"top priority is the login": false,
		"ls src":                    true,
		"ls -la":                    true,
		"cat main.go":               true,
		"echo $HOME":                true,
		"type safety in the parser": false,
		"git status":                true,
		"which go?":                 false,
		"grep TODO | wc -l":         true,
		"npm install":               true,
		"npm is broken again":       false,
	}
	for input, want := range cases {
		if got := LooksLikeCommandLine(input); got != want {
			t.Errorf("LooksLikeCommandLine(%q) = %v, want %v", input, got, want)
		}
	}
}
