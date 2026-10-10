package runner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalDirValidatesAndExpands(t *testing.T) {
	base := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	if _, err := LocalDir(""); err == nil {
		t.Error("an empty directory must be rejected")
	}
	if _, err := LocalDir("relative/path"); err == nil {
		t.Error("a relative path must be rejected")
	}
	if _, err := LocalDir(filepath.Join(base, "missing")); err == nil {
		t.Error("a missing directory must be rejected")
	}
	file := filepath.Join(base, "afile")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LocalDir(file); err == nil {
		t.Error("a file must be rejected")
	}

	got, err := LocalDir(base)
	if err != nil {
		t.Fatalf("LocalDir(%q): %v", base, err)
	}
	resolvedBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(resolvedBase) {
		t.Errorf("LocalDir(%q) = %q, want the resolved path %q", base, got, resolvedBase)
	}

	nested := filepath.Join(home, "src", "proj")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	got, err = LocalDir("~/src/proj")
	if err != nil {
		t.Fatalf("LocalDir(~/src/proj): %v", err)
	}
	resolvedNested, err := filepath.EvalSymlinks(nested)
	if err != nil {
		t.Fatal(err)
	}
	if got != resolvedNested {
		t.Errorf("tilde expansion: got %q, want %q", got, resolvedNested)
	}

	if _, err := LocalDir("/tmp:no/such"); err == nil {
		t.Error("a path containing ':' would break the docker volume syntax and must be rejected")
	}

	// The stored entry is symlink-resolved: a link and its target produce
	// the same allowlist entry.
	link := filepath.Join(base, "link")
	if err := os.Symlink(base, link); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
	throughLink, err := LocalDir(link)
	if err != nil {
		t.Fatalf("LocalDir(%q): %v", link, err)
	}
	if throughLink != resolvedBase {
		t.Errorf("a symlinked directory must resolve to its target: link %q, direct %q", throughLink, resolvedBase)
	}
}

func TestResolveLocalTaskDirResolvesSymlinks(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
	got, err := resolveLocalTaskDir(link)
	if err != nil {
		t.Fatalf("resolveLocalTaskDir(%q): %v", link, err)
	}
	if got != filepath.Clean(want) {
		t.Errorf("resolved %q to %q, want %q", link, got, want)
	}
	if _, err := resolveLocalTaskDir("relative"); err == nil {
		t.Error("a relative task dir must be rejected")
	}
	if _, err := resolveLocalTaskDir(filepath.Join(base, "missing")); err == nil {
		t.Error("a missing task dir must be rejected")
	}
}

func TestAllowedLocalDirIsExactMatch(t *testing.T) {
	allow := []string{"/Users/me/src/proj", "/Users/alanp/work"}
	if !allowedLocalDir("/Users/me/src/proj", allow) {
		t.Error("an allowlisted path must be allowed")
	}
	for _, dir := range []string{
		"/Users/me/src",           // a parent
		"/Users/me/src/proj2",     // a sibling sharing a prefix
		"/Users/me/src/proj/edge", // a child
		"/etc",
		"",
	} {
		if allowedLocalDir(dir, allow) {
			t.Errorf("%q must not be allowed by a prefixing entry", dir)
		}
	}
}

func TestWorkspaceEnvCarriesTheLocalDirectoryInsteadOfTheRepo(t *testing.T) {
	env := workspaceEnv(WorkspaceTask{
		WorkspaceID:  "ws-local",
		WorkspaceDir: "/Users/me/src/proj",
		TxnSecret:    "s3cret",
	})
	if env["REPO_URL"] != "" {
		t.Errorf("a local workspace has no repo; REPO_URL = %q", env["REPO_URL"])
	}
	if env["WORKSPACE_DIR"] != "/Users/me/src/proj" {
		t.Errorf("WORKSPACE_DIR = %q, want the served directory", env["WORKSPACE_DIR"])
	}
	if env["WORKSPACE_TOKEN"] != "s3cret" {
		t.Errorf("a local workspace still authenticates; WORKSPACE_TOKEN = %q", env["WORKSPACE_TOKEN"])
	}
	if env["SPROUT_GIT_HOST"] != "" {
		t.Errorf("a local workspace has no git host; SPROUT_GIT_HOST = %q", env["SPROUT_GIT_HOST"])
	}

	// Execution follows workspace_dir regardless of a stale repo_url, and
	// the env agrees (dir wins; the platform sends at most one).
	env = workspaceEnv(WorkspaceTask{
		WorkspaceID:  "ws-local",
		WorkspaceDir: "/Users/me/src/proj",
		RepoURL:      "https://github.com/o/r",
	})
	if env["WORKSPACE_DIR"] == "" || env["REPO_URL"] != "" {
		t.Errorf("a dir-backed workspace must carry WORKSPACE_DIR, not REPO_URL; got %+v", env)
	}
}

func TestLocalDirRefusesBroadDirectories(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home", "me")
	state := filepath.Join(root, "state")
	config := filepath.Join(root, "config")
	project := filepath.Join(home, "src", "proj")
	for _, d := range []string{project, state, config} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SPROUT_STATE_DIR", state)
	t.Setenv("SPROUT_CONFIG_DIR", config)

	for name, dir := range map[string]string{
		"filesystem root":       string(filepath.Separator),
		"home":                  home,
		"home via tilde":        "~",
		"ancestor of home":      filepath.Dir(home),
		"state dir":             state,
		"inside the state dir":  mkdir(t, filepath.Join(state, "runner", "workspaces")),
		"inside the config dir": mkdir(t, filepath.Join(config, "providers")),
		"ancestor of both":      root,
	} {
		if got, err := LocalDir(dir); err == nil {
			t.Errorf("%s (%q) must be refused; got %q", name, dir, got)
		}
	}
	if _, err := LocalDir(project); err != nil {
		t.Errorf("a project directory inside home must be allowed: %v", err)
	}
}

func mkdir(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}
