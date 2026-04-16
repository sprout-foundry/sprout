package webui

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/utils"
)

// ---------------------------------------------------------------------------
// Launch error
// ---------------------------------------------------------------------------

type sshLaunchError struct {
	Step    string
	Message string
	Details string
	LogPath string
}

func (e *sshLaunchError) Error() string {
	if e == nil {
		return ""
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = "failed to open SSH workspace"
	}
	if step := strings.TrimSpace(e.Step); step != "" {
		return fmt.Sprintf("%s (%s)", message, step)
	}
	return message
}

// ---------------------------------------------------------------------------
// Launch logger
// ---------------------------------------------------------------------------

type sshLaunchLogger struct {
	path   string
	logger *utils.Logger
	prefix string
}

func newSSHLaunchLogger(hostAlias, remoteWorkspacePath string) (*sshLaunchLogger, error) {
	logger := &sshLaunchLogger{
		path:   workspaceLogPath(),
		logger: utils.GetLogger(true),
		prefix: fmt.Sprintf("[ssh-launch %s %s]", hostAlias, remoteWorkspacePath),
	}
	logger.Logf("launch started")
	return logger, nil
}

func (l *sshLaunchLogger) Close() error {
	return nil
}

func (l *sshLaunchLogger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

func (l *sshLaunchLogger) Logf(format string, args ...interface{}) {
	if l == nil || l.logger == nil {
		return
	}
	l.logger.Logf("%s %s", l.prefix, fmt.Sprintf(format, args...))
}

// ---------------------------------------------------------------------------
// Shared types
// ---------------------------------------------------------------------------

type sshWorkspaceSession struct {
	Key                 string
	HostAlias           string
	RemoteWorkspacePath string
	LocalPort           int
	RemotePort          int
	RemotePID           int
	URL                 string
	TunnelCmd           *exec.Cmd
	StartedAt           time.Time
	// ReusedDaemon is true when this session connected to an existing daemon
	// that was not started by this launch.  When set, closing the session
	// kills only the local tunnel — never the remote daemon process.
	ReusedDaemon bool
}

type persistedSSHWorkspaceSession struct {
	Key                 string    `json:"key"`
	HostAlias           string    `json:"host_alias"`
	RemoteWorkspacePath string    `json:"remote_workspace_path"`
	RemotePort          int       `json:"remote_port"`
	RemotePID           int       `json:"remote_pid"`
	StartedAt           time.Time `json:"started_at"`
}

type sshLaunchResult struct {
	URL       string
	LocalPort int
	// ProxyBase is the path prefix served by the local server that proxies
	// all traffic to this SSH session's tunnel (e.g. /ssh/ai-worker%3A%3A%24HOME).
	// Using this URL keeps the browser on the same origin, preserving PWA
	// functionality and avoiding cross-origin storage isolation.
	ProxyBase string
}

type remoteSSHInfo struct {
	Platform string
	Arch     string
}

type sshRemoteEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
}

type sshLaunchStatus struct {
	Key        string
	Step       string
	Status     string
	InProgress bool
	LastError  string
	// Details and LogPath carry structured diagnostic info from sshLaunchError
	// when the launch fails, giving async pollers the same context as the
	// former synchronous response.
	Details   string
	LogPath   string
	UpdatedAt time.Time
	// ProxyBase, ProxyURL, and LocalPort are populated when InProgress is false
	// and LastError is empty — i.e. the launch completed successfully.
	ProxyBase string
	ProxyURL  string
	LocalPort int
}

// ---------------------------------------------------------------------------
// Constants & sentinels
// ---------------------------------------------------------------------------

const (
	githubReleaseRepoOwner = "alantheprice"
	githubReleaseRepoName  = "ledit"

	sshLaunchHealthTimeout  = 30 * time.Second
	sshRestoreHealthTimeout = 12 * time.Second

	// DaemonPort is the unified fixed port used by all ledit daemons
	// (both local and SSH-launched remote).  All daemons on a given host
	// share this port — the launcher detects an existing daemon and
	// reuses it rather than starting a duplicate.
	DaemonPort = 54000
)

var errNoReleaseTagForArtifact = errors.New("no release tag available for current build")

// ---------------------------------------------------------------------------
// Shared utility functions
// ---------------------------------------------------------------------------

func workspaceLogPath() string {
	home := os.Getenv("HOME")
	if strings.TrimSpace(home) == "" {
		return ".ledit/workspace.log"
	}
	return filepath.Join(home, ".ledit", "workspace.log")
}

func newSSHLaunchFailure(step, message, details string, logger *sshLaunchLogger) error {
	return &sshLaunchError{
		Step:    strings.TrimSpace(step),
		Message: strings.TrimSpace(message),
		Details: strings.TrimSpace(details),
		LogPath: strings.TrimSpace(func() string {
			if logger == nil {
				return ""
			}
			return logger.Path()
		}()),
	}
}

func trimSSHOutput(raw []byte) string {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return ""
	}
	const maxLen = 4000
	if len(text) > maxLen {
		return text[:maxLen] + "\n...[truncated]"
	}
	return text
}

func shellEscapeSSH(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func killProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
	return nil
}

func findFreeLocalPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("allocate free local port: %w", err)
	}
	defer listener.Close()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("failed to determine local port")
	}
	return addr.Port, nil
}

func ensureSSHProgramsAvailable() error {
	if _, err := exec.LookPath("ssh"); err != nil {
		return fmt.Errorf("ssh is not available on this machine")
	}
	if _, err := exec.LookPath("scp"); err != nil {
		return fmt.Errorf("scp is not available on this machine")
	}
	return nil
}

func localSSHCacheRoot() string {
	if tempBase := strings.TrimSpace(os.TempDir()); tempBase != "" {
		candidate := filepath.Join(tempBase, "ledit-ssh-cache")
		if err := os.MkdirAll(candidate, 0755); err == nil {
			return candidate
		}
	}

	home := strings.TrimSpace(os.Getenv("HOME"))
	if home != "" {
		for _, base := range []string{
			filepath.Join(home, ".cache"),
			filepath.Join(home, ".ledit", "cache"),
		} {
			candidate := filepath.Join(base, "ledit-ssh-cache")
			if err := os.MkdirAll(candidate, 0755); err == nil {
				return candidate
			}
		}
	}

	return filepath.Join(".", "ledit-ssh-cache")
}
