//go:build !js

package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWordColumn(t *testing.T) {
	for _, c := range []struct {
		text, word string
		want       int
	}{
		{"x := Run(y)", "Run", 6},
		{"RunAll(); Run()", "Run", 11},
		{"_Run Run", "Run", 6},
		{"nothing here", "Run", 0},
	} {
		if got := wordColumn(c.text, c.word); got != c.want {
			t.Errorf("wordColumn(%q, %q) = %d, want %d", c.text, c.word, got, c.want)
		}
	}
}

func TestLSPNavRefusesWithoutAServer(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("Run\n"), 0o644)
	res, _ := (&findDefinitionHandler{}).Execute(context.Background(), ToolEnv{WorkspaceRoot: dir}, map[string]any{"path": "notes.txt", "line": 1.0, "symbol": "Run"})
	if !res.IsError || !strings.Contains(res.Output, "no language server") {
		t.Fatalf("got %+v", res)
	}
	res, _ = (&findDefinitionHandler{}).Execute(context.Background(), ToolEnv{WorkspaceRoot: dir}, map[string]any{"path": "missing.go", "line": 1.0, "symbol": "Run"})
	if !res.IsError {
		t.Fatalf("a missing file is an error: %+v", res)
	}
}

// Against a real gopls: a call in one file finds its definition in another,
// and the function's references include the call.
func TestLSPNavWithGopls(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not on PATH")
	}
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/nav\n\ngo 1.21\n")
	write("lib.go", "package nav\n\n// Greet says hello.\nfunc Greet(name string) string { return \"hi \" + name }\n")
	write("use.go", "package nav\n\nfunc use() string {\n\treturn Greet(\"x\")\n}\n")
	env := ToolEnv{WorkspaceRoot: dir}

	res, err := (&findDefinitionHandler{}).Execute(context.Background(), env, map[string]any{"path": "use.go", "line": 4.0, "symbol": "Greet"})
	if err != nil || res.IsError {
		t.Fatalf("definition: %+v %v", res, err)
	}
	if !strings.Contains(res.Output, "lib.go:4") {
		t.Fatalf("definition = %q, want lib.go:4", res.Output)
	}

	res, err = (&findReferencesHandler{}).Execute(context.Background(), env, map[string]any{"path": "lib.go", "line": 4.0, "symbol": "Greet"})
	if err != nil || res.IsError {
		t.Fatalf("references: %+v %v", res, err)
	}
	if !strings.Contains(res.Output, "use.go:4") {
		t.Fatalf("references = %q, want use.go:4", res.Output)
	}
}
