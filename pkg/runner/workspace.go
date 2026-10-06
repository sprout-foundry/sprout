package runner

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/credentials"
)

// daemonPort is the port the workspace image's daemon listens on.
const daemonPort = 56000

// readyTimeout bounds how long a fresh workspace has to answer /health.
const readyTimeout = 3 * time.Minute

// Workspace is a running workspace's daemon, reachable on loopback.
type Workspace struct {
	ID   string
	Port int
	// Handle identifies it to its launcher (a container name, or a pid).
	Handle string
}

// Launcher starts and stops workspaces in one execution mode.
type Launcher interface {
	Start(ctx context.Context, task WorkspaceTask) (*Workspace, error)
	Stop(ctx context.Context, ws *Workspace) error
	Destroy(ctx context.Context, workspaceID string, ws *Workspace) error
}

var (
	envNameRe     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	workspaceIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
)

// reservedEnv is set by the runner and never by user env (GAP-2 filtering
// mirrors the platform's deny-list).
func reservedEnv(name string) bool {
	if strings.HasPrefix(name, "SPROUT_") || strings.HasPrefix(name, "WORKSPACE_") {
		return true
	}
	switch name {
	case "REPO_URL", "PLATFORM_API_URL", "PATH", "HOME", "TMPDIR", "CODING_ENV_JWT_SECRET":
		return true
	}
	return false
}

// workspaceEnv is the environment a workspace daemon gets: its identity and
// per-workspace secret, the platform URL, git and LLM credentials, then the
// user's own variables (never overriding the runner's).
func workspaceEnv(task WorkspaceTask) map[string]string {
	env := map[string]string{
		"SPROUT_MODE":  "daemon",
		"REPO_URL":     task.RepoURL,
		"WORKSPACE_ID": task.WorkspaceID,
	}
	if task.TxnSecret != "" {
		env["WORKSPACE_TOKEN"] = task.TxnSecret
		env["SPROUT_AUTH_TOKEN"] = task.TxnSecret
		env["SPROUT_WORKSPACE_TXN_SECRET"] = task.TxnSecret
	}
	if task.PlatformAPIURL != "" {
		env["PLATFORM_API_URL"] = task.PlatformAPIURL
	}
	if task.GitToken != "" {
		env["SPROUT_GIT_TOKEN"] = task.GitToken
		if host := repoHost(task.RepoURL); host != "" {
			env["SPROUT_GIT_HOST"] = host
		}
	}
	if task.LLMKey != "" {
		name := credentials.ProviderEnvVar(task.LLMProvider)
		if name == "" {
			name = "DEEPINFRA_API_KEY"
		}
		env[name] = task.LLMKey
	}
	for k, v := range task.UserEnv {
		if !envNameRe.MatchString(k) || reservedEnv(k) {
			continue
		}
		if _, taken := env[k]; !taken {
			env[k] = v
		}
	}
	return env
}

func repoHost(repoURL string) string {
	u, err := url.Parse(repoURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// gitAuthEnv makes git authenticate to the repo's host with token without the
// token appearing in any process's arguments (git reads GIT_CONFIG_* from its
// environment).
func gitAuthEnv(repoURL, token string) []string {
	if token == "" {
		return nil
	}
	user := "oauth2"
	if repoHost(repoURL) == "github.com" {
		user = "x-access-token"
	}
	basic := base64.StdEncoding.EncodeToString([]byte(user + ":" + token))
	return []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.extraHeader",
		"GIT_CONFIG_VALUE_0=Authorization: Basic " + basic,
		"GIT_TERMINAL_PROMPT=0",
	}
}

// workspaceDir is the per-workspace directory under the runner's state.
func workspaceDir(workspaceID string) (string, error) {
	if !workspaceIDRe.MatchString(workspaceID) {
		return "", fmt.Errorf("invalid workspace id %q", workspaceID)
	}
	base, err := WorkspacesDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, workspaceID), nil
}

// freePort asks the OS for an unused loopback port.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// waitReady polls the daemon's /health until it answers or ctx/timeout ends.
func waitReady(ctx context.Context, port int) error {
	ctx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()
	client := &http.Client{Timeout: 3 * time.Second}
	health := "http://127.0.0.1:" + strconv.Itoa(port) + "/health"
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, health, nil)
		if resp, err := client.Do(req); err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("workspace daemon on port %d did not become ready: %w", port, ctx.Err())
		case <-time.After(time.Second):
		}
	}
}

// writeEnvFile writes env as a KEY=VALUE file readable only by the owner, so
// secrets reach `docker run` without appearing in process arguments.
func writeEnvFile(dir string, env map[string]string) (string, error) {
	f, err := os.CreateTemp(dir, ".env-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	if err := f.Chmod(0o600); err != nil {
		return "", err
	}
	for k, v := range env {
		if strings.ContainsAny(v, "\r\n") {
			return "", fmt.Errorf("env var %s contains a newline", k)
		}
		if _, err := fmt.Fprintf(f, "%s=%s\n", k, v); err != nil {
			return "", err
		}
	}
	return f.Name(), nil
}
