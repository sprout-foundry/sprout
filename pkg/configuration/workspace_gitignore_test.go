package configuration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceGitignoreKeepsMachineFilesOut(t *testing.T) {
	root := t.TempDir()
	if err := EnsureWorkspaceConfigDir(root); err != nil {
		t.Fatalf("EnsureWorkspaceConfigDir: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, ConfigDirName, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []string{"workspace.json", "config.json", "credentials/", "instances.json"} {
		if !strings.Contains(string(got), "\n"+entry+"\n") {
			t.Errorf(".gitignore is missing %q", entry)
		}
	}
}

func TestWorkspaceGitignoreRefreshesOnlySproutsOwnFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ConfigDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".gitignore")

	old := workspaceGitignoreHeader + "\n*.local.json\nchanges/\n"
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureWorkspaceConfigDir(root); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != workspaceGitignoreContent {
		t.Errorf("an outdated generated .gitignore was not refreshed:\n%s", got)
	}

	custom := "# mine\n*.local.json\n"
	if err := os.WriteFile(path, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureWorkspaceConfigDir(root); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != custom {
		t.Errorf("a user-written .gitignore was overwritten:\n%s", got)
	}
}

func TestWorkspaceConfigDirLeavesTheRepositoryClean(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if err := EnsureWorkspaceConfigDir(root); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"config.json", "workspace.json"} {
		if err := os.WriteFile(filepath.Join(root, ConfigDirName, f), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command("git", "-C", root, "status", "--porcelain", "--untracked-files=all").CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v: %s", err, out)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("sprout's own files show as changes:\n%s", out)
	}
}
