package runner

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/runner/sandbox"
)

// HostLauncher runs a workspace's daemon directly on this machine, with its
// toolchains. Sandboxed is native mode (the OS sandbox confines it);
// unsandboxed is bare-metal mode (it runs with the user's full access).
type HostLauncher struct {
	Sandboxed bool
	// Writable are extra paths native mode may write (toolchain caches).
	Writable []string
	// SproutBin is the binary that serves the workspace daemon.
	SproutBin string

	mu    sync.Mutex
	procs map[string]*hostProc
}

type hostProc struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	done   chan struct{}
}

// stopGrace is how long a daemon gets to exit after SIGTERM-equivalent
// cancellation before it is killed.
const stopGrace = 10 * time.Second

func (l *HostLauncher) Start(ctx context.Context, task WorkspaceTask) (*Workspace, error) {
	localDir := ""
	if task.WorkspaceDir != "" {
		// A local-directory workspace runs in place: the user's real
		// files are the workspace root, so there is nothing to clone.
		// The runner checked the allowlist (Runner.start, the only
		// caller the platform reaches); re-checking here keeps the
		// launcher safe for direct callers. The platform sends either
		// repo_url or workspace_dir, never both.
		if task.RepoURL != "" {
			return nil, fmt.Errorf("start task names both a repo (%s) and a local directory (%s)", task.RepoURL, task.WorkspaceDir)
		}
		localDir = task.WorkspaceDir
	}
	dir, err := workspaceDir(task.WorkspaceID)
	if err != nil {
		return nil, err
	}
	tmp := filepath.Join(dir, "tmp")
	for _, d := range []string{dir, tmp, filepath.Join(dir, "config"), filepath.Join(dir, "state")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, fmt.Errorf("creating workspace dirs: %w", err)
		}
	}
	repo := localDir
	if localDir == "" {
		repo = filepath.Join(dir, "repo")
		if err := cloneIfMissing(ctx, repo, task); err != nil {
			return nil, err
		}
	}

	port, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("allocating daemon port: %w", err)
	}
	// The daemon outlives the task's request context; Stop cancels it.
	procCtx, cancel := context.WithCancel(context.Background())
	cmd, err := l.command(procCtx, repo, dir, tmp, port)
	if err != nil {
		cancel()
		return nil, err
	}
	cmd.Dir = repo
	cmd.Env = l.env(task, dir, tmp)
	logFile, err := os.OpenFile(filepath.Join(dir, "daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		cancel()
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.WaitDelay = stopGrace
	if err := cmd.Start(); err != nil {
		cancel()
		_ = logFile.Close()
		return nil, fmt.Errorf("starting workspace daemon: %w", err)
	}
	p := &hostProc{cmd: cmd, cancel: cancel, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		_ = logFile.Close()
		close(p.done)
	}()
	l.track(task.WorkspaceID, p)

	if err := waitReady(ctx, port); err != nil {
		l.stopProc(task.WorkspaceID)
		return nil, fmt.Errorf("%w (log: %s)", err, filepath.Join(dir, "daemon.log"))
	}
	return &Workspace{ID: task.WorkspaceID, Port: port, Handle: strconv.Itoa(cmd.Process.Pid)}, nil
}

// command builds the daemon command. workDir is the directory commands run
// in — the clone, or the user's local directory for a path-backed workspace
// (which native mode therefore adds to the sandbox's writable set).
func (l *HostLauncher) command(ctx context.Context, workDir, dir, tmp string, port int) (*exec.Cmd, error) {
	bin := l.SproutBin
	if bin == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("locating sprout binary: %w", err)
		}
		bin = exe
	}
	args := []string{"agent", "--daemon", "--web-port", strconv.Itoa(port), "--bind", "127.0.0.1"}
	if !l.Sandboxed {
		return exec.CommandContext(ctx, bin, args...), nil //nolint:gosec // G204: the sprout binary itself, fixed daemon args
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return sandbox.CommandContext(ctx, sandbox.Policy{
		WorkDir: workDir,
		TempDir: tmp,
		// The daemon writes its config, state and daemon.log in the
		// workspace's runner-owned metadata directory, which is a
		// sibling of (not inside) the clone and may be nowhere near a
		// local-directory workspace.
		Writable:     append(append([]string{}, l.Writable...), dir),
		DenyRead:     sandbox.DefaultDenyRead(home),
		AllowNetwork: true,
	}, bin, args...)
}

// env is the daemon's environment. Bare metal inherits the user's; native
// starts from a minimal set so the user's own secrets in the runner's
// environment don't leak into workspaces.
func (l *HostLauncher) env(task WorkspaceTask, dir, tmp string) []string {
	var env []string
	if l.Sandboxed {
		for _, name := range []string{"PATH", "HOME", "USER", "LOGNAME", "SHELL", "LANG", "LC_ALL", "TERM", "DEVELOPER_DIR"} {
			if v, ok := os.LookupEnv(name); ok {
				env = append(env, name+"="+v)
			}
		}
	} else {
		for _, kv := range os.Environ() {
			if name, _, _ := strings.Cut(kv, "="); !reservedEnv(name) || name == "PATH" || name == "HOME" {
				env = append(env, kv)
			}
		}
	}
	for k, v := range workspaceEnv(task) {
		env = append(env, k+"="+v)
	}
	if l.Sandboxed {
		// The sandbox keeps ~/.npm read-only (writing a shared host cache
		// from a workspace is a poisoning route), so npm caches per
		// workspace instead.
		env = append(env, "npm_config_cache="+filepath.Join(tmp, "npm-cache"))
	}
	return append(env,
		"TMPDIR="+tmp,
		"SPROUT_CONFIG_DIR="+filepath.Join(dir, "config"),
		"SPROUT_STATE_DIR="+filepath.Join(dir, "state"),
		"SPROUT_BIND_ADDR=127.0.0.1",
	)
}

// cloneIfMissing clones the repo on first start and wires a credential
// helper that reads the token from the daemon's environment at push time, so
// the token is never written to the clone.
func cloneIfMissing(ctx context.Context, repo string, task WorkspaceTask) error {
	if _, err := os.Stat(filepath.Join(repo, ".git")); err == nil {
		return nil
	}
	if !cloneableRepoURL(task.RepoURL) {
		return fmt.Errorf("refusing to clone %q: only https repositories can be cloned on a runner", task.RepoURL)
	}
	_ = os.RemoveAll(repo)
	clone := exec.CommandContext(ctx, "git", "clone", "--", task.RepoURL, repo) //nolint:gosec // G204: https-only URL after "--"
	clone.Env = append(os.Environ(), gitAuthEnv(task.RepoURL, task.GitToken)...)
	if out, err := clone.CombinedOutput(); err != nil {
		return fmt.Errorf("cloning %s: %s", task.RepoURL, strings.TrimSpace(string(out)))
	}
	if task.GitToken == "" {
		return nil
	}
	user := "oauth2"
	if repoHost(task.RepoURL) == "github.com" {
		user = "x-access-token"
	}
	helper := `!f() { test "$1" = get && echo username=` + user + ` && echo "password=$SPROUT_GIT_TOKEN"; }; f`
	if out, err := exec.CommandContext(ctx, "git", "-C", repo, "config", "credential.helper", helper).CombinedOutput(); err != nil { //nolint:gosec // G204: fixed helper text, runner-owned clone path
		return fmt.Errorf("configuring git credentials: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func (l *HostLauncher) track(id string, p *hostProc) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.procs == nil {
		l.procs = make(map[string]*hostProc)
	}
	l.procs[id] = p
}

func (l *HostLauncher) stopProc(id string) {
	l.mu.Lock()
	p := l.procs[id]
	delete(l.procs, id)
	l.mu.Unlock()
	if p == nil {
		return
	}
	p.cancel()
	select {
	case <-p.done:
	case <-time.After(stopGrace + time.Second):
	}
}

func (l *HostLauncher) Stop(_ context.Context, ws *Workspace) error {
	if ws != nil {
		l.stopProc(ws.ID)
	}
	return nil
}

func (l *HostLauncher) Destroy(_ context.Context, workspaceID string, _ *Workspace) error {
	l.stopProc(workspaceID)
	dir, err := workspaceDir(workspaceID)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// allowFileRepos lets tests clone local fixtures. The platform only sends
// https URLs; a file: URL would copy a repository from this machine into a
// workspace the platform can pull from.
var allowFileRepos = false

func cloneableRepoURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return (u.Scheme == "https" && u.Host != "") || (allowFileRepos && u.Scheme == "file")
}
