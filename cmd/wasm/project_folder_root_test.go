//go:build js && wasm

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/wasmshell"
)

func withTempProjectFolder(t *testing.T) string {
	t.Helper()
	if wasmshell.ShellEnv == nil {
		wasmshell.SetShellEnv(wasmshell.NewEnv())
	}
	prevRoot := projectFolder
	prevCwd, _ := os.Getwd()
	dir := filepath.Join(t.TempDir(), "project")
	if err := setProjectFolder(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		projectFolder = prevRoot
		_ = os.Chdir(prevCwd)
	})
	return dir
}

func TestProjectFolderPathResolvesHostPathsAgainstTheProjectFolder(t *testing.T) {
	root := withTempProjectFolder(t)
	home := wasmshell.ShellEnv.Get("HOME")

	cases := map[string]string{
		"src/a.go":   filepath.Join(root, "src/a.go"),
		"./README":   filepath.Join(root, "README"),
		"/etc/hosts": "/etc/hosts",
		"~":          home,
		"~/.config":  filepath.Join(home, ".config"),
	}
	for in, want := range cases {
		if got := projectFolderPath(in); got != want {
			t.Errorf("projectFolderPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProjectFolderPathIgnoresTheTerminalsCwd(t *testing.T) {
	root := withTempProjectFolder(t)
	sub := filepath.Join(root, "api")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(sub); err != nil {
		t.Fatal(err)
	}
	if got := projectFolderPath("go.mod"); got != filepath.Join(root, "go.mod") {
		t.Errorf("after cd api, projectFolderPath(go.mod) = %q, want the project folder's go.mod", got)
	}
}

func TestFromProjectFolderRunsAtTheRootAndRestoresTheCwd(t *testing.T) {
	root := withTempProjectFolder(t)
	sub := filepath.Join(root, "api")
	_ = os.MkdirAll(sub, 0o755)
	_ = os.Chdir(sub)

	ran := fromProjectFolder(func() string {
		cwd, _ := os.Getwd()
		return cwd
	})

	if !samePath(ran, root) {
		t.Errorf("ran in %q, want the project folder %q", ran, root)
	}
	if cwd, _ := os.Getwd(); !samePath(cwd, sub) {
		t.Errorf("cwd after = %q, want it restored to %q", cwd, sub)
	}
}

// samePath compares paths through symlinks (a temp dir can sit behind one).
func samePath(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}
