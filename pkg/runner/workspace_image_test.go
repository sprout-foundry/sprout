package runner

// These tests run the public workspace image's entrypoint
// (docker/workspace/entrypoint.sh) and git credential helper on the host
// with sh and git: no Docker, no network. A stub `sprout` on PATH records how
// the daemon would have been started, SPROUT_WORKSPACE_ROOT points the
// entrypoint at a temp directory instead of /workspace, and a git insteadOf
// rewrite turns the https REPO_URL into a local fixture repository.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const imgRepoURL = "https://git.example.test/team/app.git"

const imgStubSprout = `#!/bin/sh
{ pwd; echo "$*"; } >"$STUB_SPROUT_OUT"
`

type imgRun struct {
	out      string
	err      error
	home     string
	launched string
}

func imgDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the workspace image entrypoint is POSIX sh")
	}
	for _, tool := range []string{"sh", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found", tool)
		}
	}
	dir, err := filepath.Abs(filepath.Join("..", "..", "docker", "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// imgSourceRepo creates a local repository with a couple of commits' worth of
// files for the entrypoint to clone.
func imgSourceRepo(t *testing.T) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "src")
	imgMust(t, os.MkdirAll(filepath.Join(src, "src"), 0o755))
	imgMust(t, os.WriteFile(filepath.Join(src, "README.md"), []byte("hello\n"), 0o644))
	imgMust(t, os.WriteFile(filepath.Join(src, "src", "main.go"), []byte("package main\n"), 0o644))
	imgMust(t, os.WriteFile(filepath.Join(src, ".gitignore"), []byte("bin/\n"), 0o644))
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "."},
		{"-c", "user.name=t", "-c", "user.email=t@example.test", "commit", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = src
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir())
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return src
}

func imgMust(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func runEntrypoint(t *testing.T, root string, env map[string]string) imgRun {
	t.Helper()
	dir := imgDir(t)
	bin := t.TempDir()
	imgMust(t, os.WriteFile(filepath.Join(bin, "sprout"), []byte(imgStubSprout), 0o755))
	home := t.TempDir()
	launched := filepath.Join(t.TempDir(), "launched")
	cmd := exec.Command("sh", filepath.Join(dir, "entrypoint.sh"))
	cmd.Env = []string{
		"PATH=" + bin + string(os.PathListSeparator) + dir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + home,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"SPROUT_WORKSPACE_ROOT=" + root,
		"STUB_SPROUT_OUT=" + launched,
	}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	data, _ := os.ReadFile(launched)
	return imgRun{out: string(out), err: err, home: home, launched: string(data)}
}

// cloneEnv makes git fetch imgRepoURL from the local source repository.
func cloneEnv(src string) map[string]string {
	return map[string]string{
		"SPROUT_MODE":        "daemon",
		"REPO_URL":           imgRepoURL,
		"GIT_CONFIG_COUNT":   "1",
		"GIT_CONFIG_KEY_0":   "url.file://" + filepath.ToSlash(src) + ".insteadOf",
		"GIT_CONFIG_VALUE_0": imgRepoURL,
	}
}

// snapshot lists every path under root with its file content.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	imgMust(t, filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if info.IsDir() {
			files[rel+"/"] = ""
			return nil
		}
		data, err := os.ReadFile(p)
		files[rel] = string(data)
		return err
	}))
	return files
}

func assertDaemonLaunched(t *testing.T, r imgRun, root string) {
	t.Helper()
	if r.err != nil {
		t.Fatalf("entrypoint failed: %v\n%s", r.err, r.out)
	}
	lines := strings.Split(strings.TrimSpace(r.launched), "\n")
	if len(lines) != 2 {
		t.Fatalf("daemon not launched; stub recorded %q\n%s", r.launched, r.out)
	}
	wantDir, _ := filepath.EvalSymlinks(root)
	if gotDir, _ := filepath.EvalSymlinks(lines[0]); gotDir != wantDir {
		t.Errorf("daemon ran in %q, want %q", lines[0], root)
	}
	if want := "agent --daemon --web-port 56000 --bind 0.0.0.0"; lines[1] != want {
		t.Errorf("daemon args = %q, want %q", lines[1], want)
	}
}

func assertNoCloneLeftovers(t *testing.T, root string) {
	t.Helper()
	for _, name := range []string{".sprout-clone-pending", ".sprout-clone-staging", ".sprout-clone-pending.tmp"} {
		if _, err := os.Lstat(filepath.Join(root, name)); err == nil {
			t.Errorf("%s left behind", name)
		}
	}
}

func TestWorkspaceImageServesALocalDirectoryInPlace(t *testing.T) {
	root := t.TempDir()
	imgMust(t, os.WriteFile(filepath.Join(root, "notes.txt"), []byte("mine\n"), 0o644))
	imgMust(t, os.MkdirAll(filepath.Join(root, "sub"), 0o755))
	imgMust(t, os.WriteFile(filepath.Join(root, "sub", "a.txt"), []byte("a\n"), 0o644))
	before := snapshot(t, root)

	env := cloneEnv(imgSourceRepo(t))
	env["WORKSPACE_DIR"] = "/workspace"
	r := runEntrypoint(t, root, env)
	assertDaemonLaunched(t, r, root)
	after := snapshot(t, root)
	if len(after) != len(before) {
		t.Fatalf("local directory changed: before %v, after %v", before, after)
	}
	for k, v := range before {
		if after[k] != v {
			t.Errorf("%s changed: %q -> %q", k, v, after[k])
		}
	}
}

func TestWorkspaceImageRefusesToCloneOverUnknownContent(t *testing.T) {
	root := t.TempDir()
	imgMust(t, os.WriteFile(filepath.Join(root, "keep.txt"), []byte("keep\n"), 0o644))
	r := runEntrypoint(t, root, cloneEnv(imgSourceRepo(t)))
	if r.err == nil {
		t.Fatalf("entrypoint must refuse a non-empty workspace\n%s", r.out)
	}
	if !strings.Contains(r.out, "refusing to clone") || !strings.Contains(r.out, "keep.txt") {
		t.Errorf("error should name the refusal and the entry:\n%s", r.out)
	}
	if r.launched != "" {
		t.Error("daemon must not start after a refused clone")
	}
	if data, err := os.ReadFile(filepath.Join(root, "keep.txt")); err != nil || string(data) != "keep\n" {
		t.Errorf("keep.txt must survive: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		t.Error("nothing may be cloned into a refused workspace")
	}
}

func TestWorkspaceImageRefusesNonHTTPSRepos(t *testing.T) {
	root := t.TempDir()
	env := cloneEnv(imgSourceRepo(t))
	env["REPO_URL"] = "file:///etc"
	r := runEntrypoint(t, root, env)
	if r.err == nil || !strings.Contains(r.out, "only https") {
		t.Fatalf("a non-https REPO_URL must be refused: %v\n%s", r.err, r.out)
	}
}

func TestWorkspaceImageClonesBesideLostAndFound(t *testing.T) {
	root := t.TempDir()
	imgMust(t, os.MkdirAll(filepath.Join(root, "lost+found"), 0o700))
	imgMust(t, os.WriteFile(filepath.Join(root, "lost+found", "#123"), []byte("fsck\n"), 0o600))

	r := runEntrypoint(t, root, cloneEnv(imgSourceRepo(t)))
	assertDaemonLaunched(t, r, root)
	for _, p := range []string{".git/HEAD", "README.md", "src/main.go", ".gitignore", "lost+found/#123"} {
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			t.Errorf("%s missing after clone: %v", p, err)
		}
	}
	assertNoCloneLeftovers(t, root)
	status := exec.Command("git", "-C", root, "status", "--porcelain")
	status.Env = append(os.Environ(), "HOME="+r.home, "GIT_CONFIG_NOSYSTEM=1")
	if out, err := status.CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "?? lost+found/" {
		t.Errorf("checkout should be clean apart from lost+found: %q, %v", out, err)
	}

	// A restart finds the repository and leaves it alone.
	imgMust(t, os.WriteFile(filepath.Join(root, "work.txt"), []byte("wip\n"), 0o644))
	r = runEntrypoint(t, root, cloneEnv(imgSourceRepo(t)))
	assertDaemonLaunched(t, r, root)
	if _, err := os.Stat(filepath.Join(root, "work.txt")); err != nil {
		t.Error("a restart must not touch an existing checkout")
	}
}

func TestWorkspaceImageRecoversItsOwnInterruptedClone(t *testing.T) {
	root := t.TempDir()
	imgMust(t, os.MkdirAll(filepath.Join(root, "lost+found"), 0o700))
	// An earlier boot recorded its manifest, moved README.md and src in and
	// stopped before .git.
	imgMust(t, os.WriteFile(filepath.Join(root, ".sprout-clone-pending"), []byte(".git\n.gitignore\nREADME.md\nsrc\n"), 0o644))
	imgMust(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("partial"), 0o644))
	imgMust(t, os.MkdirAll(filepath.Join(root, "src"), 0o755))
	imgMust(t, os.MkdirAll(filepath.Join(root, ".sprout-clone-staging", ".git"), 0o755))
	imgMust(t, os.WriteFile(filepath.Join(root, ".sprout-clone-staging", ".gitignore"), []byte("x"), 0o644))

	r := runEntrypoint(t, root, cloneEnv(imgSourceRepo(t)))
	assertDaemonLaunched(t, r, root)
	if data, _ := os.ReadFile(filepath.Join(root, "README.md")); string(data) != "hello\n" {
		t.Errorf("README.md = %q, want the cloned content", data)
	}
	if _, err := os.Stat(filepath.Join(root, ".git", "HEAD")); err != nil {
		t.Errorf("clone did not complete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "lost+found")); err != nil {
		t.Error("lost+found must survive recovery")
	}
	assertNoCloneLeftovers(t, root)
}

func TestWorkspaceImageRecoveryNeverDeletesUnlistedFiles(t *testing.T) {
	root := t.TempDir()
	imgMust(t, os.WriteFile(filepath.Join(root, ".sprout-clone-pending"), []byte("README.md\n"), 0o644))
	imgMust(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("partial"), 0o644))
	imgMust(t, os.WriteFile(filepath.Join(root, "keep.txt"), []byte("keep\n"), 0o644))

	r := runEntrypoint(t, root, cloneEnv(imgSourceRepo(t)))
	if r.err == nil || !strings.Contains(r.out, "keep.txt") {
		t.Fatalf("an unlisted file must stop recovery: %v\n%s", r.err, r.out)
	}
	for _, name := range []string{"keep.txt", "README.md", ".sprout-clone-pending"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Errorf("%s must be left untouched when recovery is refused", name)
		}
	}
}

func TestWorkspaceImageGitCredentialsAreHostScoped(t *testing.T) {
	dir := imgDir(t)
	helper := filepath.Join(dir, "git-credential-sprout")
	ask := func(input string, env ...string) string {
		t.Helper()
		cmd := exec.Command("sh", helper, "get")
		cmd.Env = append([]string{"PATH=" + os.Getenv("PATH")}, env...)
		cmd.Stdin = strings.NewReader(input)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("helper failed: %v\n%s", err, out)
		}
		return string(out)
	}
	tok := "SPROUT_GIT_TOKEN=tok-123"
	cases := []struct {
		name, input string
		env         []string
		want        string
	}{
		{"github default", "protocol=https\nhost=github.com\n\n", []string{tok}, "username=x-access-token\npassword=tok-123\n"},
		{"other host", "protocol=https\nhost=evil.example\n\n", []string{tok}, ""},
		{"ssh", "protocol=ssh\nhost=github.com\n\n", []string{tok}, ""},
		{"plain http", "protocol=http\nhost=github.com\n\n", []string{tok}, ""},
		{"configured host", "protocol=https\nhost=gitlab.example\n\n", []string{tok, "SPROUT_GIT_HOST=gitlab.example"}, "username=oauth2\npassword=tok-123\n"},
		{"default host when another is configured", "protocol=https\nhost=github.com\n\n", []string{tok, "SPROUT_GIT_HOST=gitlab.example"}, ""},
		{"no token", "protocol=https\nhost=github.com\n\n", nil, ""},
	}
	for _, c := range cases {
		if got := ask(c.input, c.env...); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}

	// The entrypoint wires git to the helper in the (tmpfs) home directory.
	root := t.TempDir()
	r := runEntrypoint(t, root, map[string]string{"WORKSPACE_DIR": "/workspace"})
	assertDaemonLaunched(t, r, root)
	fill := exec.Command("git", "credential", "fill")
	fill.Env = []string{
		"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + r.home, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", tok,
	}
	fill.Stdin = strings.NewReader("protocol=https\nhost=github.com\n\n")
	out, err := fill.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "password=tok-123") {
		t.Errorf("git credential fill through the configured helper: %v\n%s", err, out)
	}
}
