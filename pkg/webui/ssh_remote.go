package webui

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/utils"
)

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

func runSSHLoggedCommand(logger *sshLaunchLogger, step, summary string, cmd *exec.Cmd) ([]byte, error) {
	logger.Logf("%s: running %s", step, summary)
	out, err := cmd.CombinedOutput()
	output := trimSSHOutput(out)
	if output != "" {
		logger.Logf("%s output:\n%s", step, output)
	}
	if err != nil {
		logger.Logf("%s error: %v", step, err)
		return out, newSSHLaunchFailure(step, "SSH workspace setup failed", output, logger)
	}
	logger.Logf("%s completed", step)
	return out, nil
}

func newSSHCommand(hostAlias, script string, extraArgs ...string) *exec.Cmd {
	baseArgs := []string{
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
	}
	baseArgs = append(baseArgs, extraArgs...)
	baseArgs = append(baseArgs, hostAlias, fmt.Sprintf("bash -lc %s", shellEscapeSSH(script)))
	return exec.Command("ssh", baseArgs...)
}

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

const (
	githubReleaseRepoOwner  = "alantheprice"
	githubReleaseRepoName   = "ledit"
	sshLaunchHealthTimeout  = 30 * time.Second
	sshRestoreHealthTimeout = 12 * time.Second
)

var errNoReleaseTagForArtifact = errors.New("no release tag available for current build")

func (ws *ReactWebServer) launchSSHWorkspace(req sshLaunchRequestDTO) (*sshLaunchResult, error) {
	hostAlias := strings.TrimSpace(req.HostAlias)
	if hostAlias == "" {
		return nil, errors.New("SSH host alias is required")
	}

	remoteWorkspacePath := strings.TrimSpace(req.RemoteWorkspacePath)
	if remoteWorkspacePath == "" {
		remoteWorkspacePath = "$HOME"
	}

	logger, loggerErr := newSSHLaunchLogger(hostAlias, remoteWorkspacePath)
	if loggerErr != nil {
		return nil, loggerErr
	}
	defer logger.Close()

	sessionKey := hostAlias + "::" + remoteWorkspacePath

	ws.sshSessionsMu.Lock()
	if existing := ws.sshSessions[sessionKey]; existing != nil {
		if err := waitForWebHealth(existing.LocalPort, 2*time.Second); err == nil {
			result := &sshLaunchResult{
				URL:       existing.URL,
				LocalPort: existing.LocalPort,
				ProxyBase: "/ssh/" + url.PathEscape(sessionKey),
			}
			ws.sshSessionsMu.Unlock()
			return result, nil
		}
		ws.stopSSHSessionLocked(sessionKey)
	}
	ws.sshSessionsMu.Unlock()

	if restored, err := ws.restorePersistedSSHSession(sessionKey); err == nil && restored != nil {
		logger.Logf("restored existing SSH session %q", sessionKey)
		return restored, nil
	}

	if err := ensureSSHProgramsAvailable(); err != nil {
		logger.Logf("ssh program availability check failed: %v", err)
		return nil, err
	}
	logger.Logf("ssh and scp detected locally")

	remoteInfo, err := inspectRemoteSSHHost(hostAlias, logger)
	if err != nil {
		return nil, err
	}
	logger.Logf("remote host detected platform=%s arch=%s", remoteInfo.Platform, remoteInfo.Arch)

	localBinary, err := prepareLocalSSHBinary(remoteInfo.Platform, remoteInfo.Arch, logger)
	if err != nil {
		return nil, err
	}
	logger.Logf("local SSH backend binary ready: %s", localBinary)

	remoteBinary, err := ensureRemoteSSHBinary(hostAlias, localBinary, remoteInfo, logger)
	if err != nil {
		return nil, err
	}
	logger.Logf("remote SSH backend installed at %s", remoteBinary)

	localPort, err := findFreeLocalPort()
	if err != nil {
		logger.Logf("failed to allocate local tunnel port: %v", err)
		return nil, err
	}
	logger.Logf("allocated local tunnel port %d", localPort)

	launcherURL := fmt.Sprintf("http://127.0.0.1:%d", ws.port)
	remotePort, remotePID, err := startRemoteSSHBackend(hostAlias, sessionKey, launcherURL, remoteWorkspacePath, remoteBinary, logger)
	if err != nil {
		return nil, err
	}
	logger.Logf("remote SSH backend started port=%d pid=%d", remotePort, remotePID)

	tunnelCmd, err := startSSHTunnel(hostAlias, localPort, remotePort, logger)
	if err != nil {
		return nil, err
	}
	logger.Logf("ssh tunnel started local_port=%d remote_port=%d", localPort, remotePort)

	if err := waitForWebHealth(localPort, sshLaunchHealthTimeout); err != nil {
		logger.Logf("health check failed: %v", err)
		details := collectSSHHealthFailureDetails(hostAlias, remotePID, err, logger)
		_ = killProcess(tunnelCmd)
		_ = stopRemoteSSHBackend(hostAlias, remotePID)
		return nil, newSSHLaunchFailure("health-check", "failed to connect to SSH workspace", details, logger)
	}
	logger.Logf("health check passed for local port %d", localPort)

	session := &sshWorkspaceSession{
		Key:                 sessionKey,
		HostAlias:           hostAlias,
		RemoteWorkspacePath: remoteWorkspacePath,
		LocalPort:           localPort,
		RemotePort:          remotePort,
		RemotePID:           remotePID,
		URL:                 fmt.Sprintf("http://127.0.0.1:%d", localPort),
		TunnelCmd:           tunnelCmd,
		StartedAt:           time.Now(),
	}

	ws.sshSessionsMu.Lock()
	ws.sshSessions[sessionKey] = session
	// Persist to disk while holding the lock to avoid race conditions with concurrent
	// session launches that could overwrite each other's registry entries.
	_ = persistSSHSession(session)
	ws.sshSessionsMu.Unlock()

	go ws.watchSSHSession(sessionKey, session, tunnelCmd)

	return &sshLaunchResult{
		URL:       session.URL,
		LocalPort: session.LocalPort,
		ProxyBase: "/ssh/" + url.PathEscape(sessionKey),
	}, nil
}

func (ws *ReactWebServer) restorePersistedSSHSession(sessionKey string) (*sshLaunchResult, error) {
	persisted, err := readPersistedSSHSession(sessionKey)
	if err != nil || persisted == nil {
		return nil, err
	}

	if err := ensureSSHProgramsAvailable(); err != nil {
		return nil, err
	}

	localPort, err := findFreeLocalPort()
	if err != nil {
		return nil, err
	}

	tunnelCmd, err := startSSHTunnel(persisted.HostAlias, localPort, persisted.RemotePort, nil)
	if err != nil {
		_ = removePersistedSSHSession(sessionKey)
		return nil, err
	}

	if err := waitForWebHealth(localPort, sshRestoreHealthTimeout); err != nil {
		_ = killProcess(tunnelCmd)
		_ = removePersistedSSHSession(sessionKey)
		return nil, err
	}

	session := &sshWorkspaceSession{
		Key:                 persisted.Key,
		HostAlias:           persisted.HostAlias,
		RemoteWorkspacePath: persisted.RemoteWorkspacePath,
		LocalPort:           localPort,
		RemotePort:          persisted.RemotePort,
		RemotePID:           persisted.RemotePID,
		URL:                 fmt.Sprintf("http://127.0.0.1:%d", localPort),
		TunnelCmd:           tunnelCmd,
		StartedAt:           persisted.StartedAt,
	}

	ws.sshSessionsMu.Lock()
	ws.sshSessions[sessionKey] = session
	ws.sshSessionsMu.Unlock()
	go ws.watchSSHSession(sessionKey, session, tunnelCmd)

	return &sshLaunchResult{
		URL:       session.URL,
		LocalPort: session.LocalPort,
		ProxyBase: "/ssh/" + url.PathEscape(sessionKey),
	}, nil
}

func (ws *ReactWebServer) listSSHSessions() ([]sshSessionEntryDTO, error) {
	registry, err := readPersistedSSHSessionRegistry()
	if err != nil {
		return nil, err
	}

	ws.sshSessionsMu.Lock()
	defer ws.sshSessionsMu.Unlock()

	sessions := make([]sshSessionEntryDTO, 0, len(registry))
	for key, persisted := range registry {
		entry := sshSessionEntryDTO{
			Key:                 key,
			HostAlias:           persisted.HostAlias,
			RemoteWorkspacePath: persisted.RemoteWorkspacePath,
			RemotePort:          persisted.RemotePort,
			RemotePID:           persisted.RemotePID,
			StartedAt:           persisted.StartedAt,
			Active:              false,
		}
		if active := ws.sshSessions[key]; active != nil {
			entry.Active = true
			entry.LocalPort = active.LocalPort
			entry.URL = active.URL
		}
		sessions = append(sessions, entry)
	}

	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].Active != sessions[j].Active {
			return sessions[i].Active && !sessions[j].Active
		}
		return sessions[i].StartedAt.After(sessions[j].StartedAt)
	})
	return sessions, nil
}

func (ws *ReactWebServer) closeSSHSession(sessionKey string) error {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return errors.New("ssh session key is required")
	}

	ws.sshSessionsMu.Lock()
	defer ws.sshSessionsMu.Unlock()
	ws.stopSSHSessionLocked(sessionKey)
	return nil
}

func ensureSSHProgramsAvailable() error {
	if _, err := exec.LookPath("ssh"); err != nil {
		return errors.New("ssh is not available on this machine")
	}
	if _, err := exec.LookPath("scp"); err != nil {
		return errors.New("scp is not available on this machine")
	}
	return nil
}

func browseSSHDirectory(hostAlias, requestedPath string) ([]sshRemoteEntry, string, string, error) {
	hostAlias = strings.TrimSpace(hostAlias)
	if hostAlias == "" {
		return nil, "", "", errors.New("SSH host alias is required")
	}
	if err := ensureSSHProgramsAvailable(); err != nil {
		return nil, "", "", err
	}

	targetPath := strings.TrimSpace(requestedPath)
	if targetPath == "" {
		targetPath = "$HOME"
	}

	pythonSnippet := strings.Join([]string{
		"import json, os, sys",
		"target = os.path.abspath(os.path.expanduser(sys.argv[1]))",
		"home = os.path.abspath(os.path.expanduser('~'))",
		"if not os.path.isdir(target):",
		"    print(f'directory not found: {target}', file=sys.stderr)",
		"    raise SystemExit(1)",
		"entries = []",
		"for name in sorted(os.listdir(target), key=str.lower):",
		"    if name.startswith('.'):",
		"        continue",
		"    path = os.path.join(target, name)",
		"    if os.path.isdir(path):",
		"        entries.append({'name': name, 'path': path, 'type': 'directory'})",
		"print(json.dumps({'path': target, 'home_path': home, 'files': entries}))",
	}, "\n")

	script := strings.Join([]string{
		"set -e",
		fmt.Sprintf("TARGET_INPUT=%s", shellEscapeSSH(targetPath)),
		`if [ "$TARGET_INPUT" = '$HOME' ]; then`,
		`  TARGET_INPUT="$HOME"`,
		"fi",
		`if command -v python3 >/dev/null 2>&1; then`,
		fmt.Sprintf("  python3 - \"$TARGET_INPUT\" <<'PY'\n%s\nPY", pythonSnippet),
		`elif command -v python >/dev/null 2>&1; then`,
		fmt.Sprintf("  python - \"$TARGET_INPUT\" <<'PY'\n%s\nPY", pythonSnippet),
		"else",
		`  echo "python3 or python is required on the remote host" >&2`,
		"  exit 1",
		"fi",
	}, "\n")

	cmd := newSSHCommand(hostAlias, script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		details := trimSSHOutput(out)
		if details == "" {
			details = err.Error()
		}
		return nil, "", "", errors.New(details)
	}

	var payload struct {
		Path     string           `json:"path"`
		HomePath string           `json:"home_path"`
		Files    []sshRemoteEntry `json:"files"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, "", "", fmt.Errorf("failed to decode ssh directory listing: %w", err)
	}

	return payload.Files, strings.TrimSpace(payload.Path), strings.TrimSpace(payload.HomePath), nil
}

func inspectRemoteSSHHost(hostAlias string, logger *sshLaunchLogger) (*remoteSSHInfo, error) {
	cmd := newSSHCommand(hostAlias, "uname -s; uname -m")
	out, err := runSSHLoggedCommand(logger, "inspect-remote", fmt.Sprintf("ssh %s uname -s; uname -m", hostAlias), cmd)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("failed to inspect remote host %s", hostAlias)
	}

	platform := normalizeRemotePlatform(lines[0])
	if platform == "" {
		return nil, fmt.Errorf("remote host %s is %s; only Linux and macOS SSH targets are supported", hostAlias, strings.TrimSpace(lines[0]))
	}
	arch := normalizeRemoteArch(lines[1])
	if arch == "" {
		return nil, fmt.Errorf("unsupported remote architecture: %s", strings.TrimSpace(lines[1]))
	}

	return &remoteSSHInfo{Platform: platform, Arch: arch}, nil
}

func normalizeRemotePlatform(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "linux":
		return "linux"
	case "darwin":
		return "darwin"
	default:
		return ""
	}
}

func normalizeRemoteArch(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "x86_64", "amd64":
		return "amd64"
	case "arm64", "aarch64":
		return "arm64"
	default:
		return ""
	}
}

func currentExecutableForSSH() (string, error) {
	executablePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to determine current ledit executable: %w", err)
	}
	executablePath, err = filepath.EvalSymlinks(executablePath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve current ledit executable: %w", err)
	}
	return executablePath, nil
}

func prepareLocalSSHBinary(remotePlatform, remoteArch string, logger *sshLaunchLogger) (string, error) {
	artifactPath, artifactErr := ensureLocalSSHBinaryArtifact(remotePlatform, remoteArch, logger)
	if artifactErr == nil && artifactPath != "" {
		return artifactPath, nil
	}

	if runtime.GOOS == remotePlatform && runtime.GOARCH == remoteArch {
		logger.Logf("release artifact unavailable for %s/%s, reusing current executable", remotePlatform, remoteArch)
		return currentExecutableForSSH()
	}

	logger.Logf("release artifact unavailable for %s/%s, falling back to local go build", remotePlatform, remoteArch)

	goBinary, err := exec.LookPath("go")
	if err != nil {
		if errors.Is(artifactErr, errNoReleaseTagForArtifact) {
			logger.Logf("no release tag for current build; attempting latest artifact as cross-arch fallback for %s/%s", remotePlatform, remoteArch)
			if latestPath, latestErr := ensureLocalSSHBinaryArtifactForTag("latest", remotePlatform, remoteArch, logger); latestErr == nil && latestPath != "" {
				return latestPath, nil
			} else if latestErr != nil {
				return "", fmt.Errorf("remote host requires %s/%s, but this machine is %s/%s and Go is not available to build a matching backend (latest artifact fallback failed: %w)", remotePlatform, remoteArch, runtime.GOOS, runtime.GOARCH, latestErr)
			}
		}
		return "", fmt.Errorf("remote host requires %s/%s, but this machine is %s/%s and Go is not available to build a matching backend", remotePlatform, remoteArch, runtime.GOOS, runtime.GOARCH)
	}

	executablePath, err := currentExecutableForSSH()
	if err != nil {
		return "", err
	}
	repoRoot := filepath.Dir(executablePath)
	if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err != nil {
		if errors.Is(artifactErr, errNoReleaseTagForArtifact) {
			logger.Logf("source tree unavailable; attempting latest artifact as cross-arch fallback for %s/%s", remotePlatform, remoteArch)
			if latestPath, latestErr := ensureLocalSSHBinaryArtifactForTag("latest", remotePlatform, remoteArch, logger); latestErr == nil && latestPath != "" {
				return latestPath, nil
			} else if latestErr != nil {
				return "", fmt.Errorf("cannot build matching SSH backend for %s/%s because the ledit source tree is not available next to %s (latest artifact fallback failed: %w)", remotePlatform, remoteArch, executablePath, latestErr)
			}
		}
		return "", fmt.Errorf("cannot build matching SSH backend for %s/%s because the ledit source tree is not available next to %s", remotePlatform, remoteArch, executablePath)
	}

	cacheDir := filepath.Join(os.TempDir(), "ledit-ssh-builds")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to prepare SSH build cache: %w", err)
	}

	outputPath := filepath.Join(cacheDir, fmt.Sprintf("ledit-%s-%s", remotePlatform, remoteArch))
	buildCmd := exec.Command(goBinary, "build", "-o", outputPath, ".")
	buildCmd.Dir = repoRoot
	buildCmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS="+remotePlatform,
		"GOARCH="+remoteArch,
	)
	out, err := buildCmd.CombinedOutput()
	if output := trimSSHOutput(out); output != "" {
		logger.Logf("build-backend output:\n%s", output)
	}
	if err != nil {
		logger.Logf("build-backend error: %v", err)
		return "", newSSHLaunchFailure(
			"build-backend",
			fmt.Sprintf("failed to build SSH backend for %s/%s", remotePlatform, remoteArch),
			trimSSHOutput(out),
			logger,
		)
	}
	logger.Logf("build-backend completed for %s/%s", remotePlatform, remoteArch)

	return outputPath, nil
}

func ensureLocalSSHBinaryArtifact(remotePlatform, remoteArch string, logger *sshLaunchLogger) (string, error) {
	tag := resolvePreferredReleaseTag()
	if strings.TrimSpace(tag) == "" {
		return "", errNoReleaseTagForArtifact
	}
	return ensureLocalSSHBinaryArtifactForTag(tag, remotePlatform, remoteArch, logger)
}

func ensureLocalSSHBinaryArtifactForTag(tag, remotePlatform, remoteArch string, logger *sshLaunchLogger) (string, error) {
	assetName := fmt.Sprintf("ledit-%s-%s.tar.gz", remotePlatform, remoteArch)
	cacheTag := strings.TrimPrefix(tag, "v")
	if strings.TrimSpace(cacheTag) == "" {
		cacheTag = "latest"
	}
	cacheDir := filepath.Join(os.TempDir(), "ledit-ssh-artifacts", cacheTag, remotePlatform+"-"+remoteArch)
	binaryPath := filepath.Join(cacheDir, fmt.Sprintf("ledit-%s-%s", remotePlatform, remoteArch))

	if info, err := os.Stat(binaryPath); err == nil && info.Mode().IsRegular() {
		logger.Logf("using cached release artifact %s", binaryPath)
		return binaryPath, nil
	}

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to prepare SSH artifact cache: %w", err)
	}

	downloadURL, err := resolveGitHubReleaseAssetURL(tag, assetName, logger)
	if err != nil {
		return "", err
	}
	logger.Logf("resolved release artifact %s for tag %s", assetName, tag)

	archivePath := filepath.Join(cacheDir, assetName)
	if err := downloadFile(downloadURL, archivePath, logger); err != nil {
		return "", err
	}

	if err := extractTarGzSingleFile(archivePath, binaryPath); err != nil {
		return "", err
	}
	if err := os.Chmod(binaryPath, 0755); err != nil {
		return "", err
	}
	return binaryPath, nil
}

func resolvePreferredReleaseTag() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		mainVersion := strings.TrimSpace(info.Main.Version)
		if tag := normalizeReleaseTagCandidate(mainVersion); tag != "" {
			return tag
		}
		for _, setting := range info.Settings {
			if (setting.Key == "vcs.tag" || setting.Key == "gitTag") && normalizeReleaseTagCandidate(setting.Value) != "" {
				return normalizeReleaseTagCandidate(setting.Value)
			}
		}

		if isDirtyOrDevVersion(mainVersion) {
			return "latest"
		}
		for _, setting := range info.Settings {
			if setting.Key == "vcs.modified" && strings.EqualFold(strings.TrimSpace(setting.Value), "true") {
				return "latest"
			}
		}
	}
	return ""
}

func normalizeReleaseTagCandidate(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "v") {
		return ""
	}
	if strings.Contains(value, "-0.") || strings.Contains(value, "+dirty") || strings.Contains(value, "(devel)") {
		return ""
	}
	return value
}

func isDirtyOrDevVersion(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	return strings.Contains(value, "-0.") ||
		strings.Contains(value, "+dirty") ||
		strings.Contains(value, "(devel)")
}

func resolveGitHubReleaseAssetURL(tag, assetName string, logger *sshLaunchLogger) (string, error) {
	if strings.TrimSpace(assetName) == "" {
		return "", errors.New("artifact name is required")
	}
	tag = strings.TrimSpace(tag)
	if tag == "" || tag == "latest" {
		url := fmt.Sprintf("https://github.com/%s/%s/releases/latest/download/%s", githubReleaseRepoOwner, githubReleaseRepoName, assetName)
		logger.Logf("resolved latest release download URL: %s", url)
		return url, nil
	}
	url := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s", githubReleaseRepoOwner, githubReleaseRepoName, tag, assetName)
	logger.Logf("resolved tagged release download URL: %s", url)
	return url, nil
}

func downloadFile(url, destPath string, logger *sshLaunchLogger) error {
	logger.Logf("downloading artifact %s to %s", url, destPath)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download SSH artifact: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("failed to download SSH artifact: %s", strings.TrimSpace(string(body)))
	}
	tmpPath := destPath + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, resp.Body); err != nil {
		file.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, destPath)
}

func extractTarGzSingleFile(archivePath, destPath string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		outFile, err := os.Create(destPath)
		if err != nil {
			return err
		}
		if _, err := io.Copy(outFile, tarReader); err != nil {
			outFile.Close()
			_ = os.Remove(destPath)
			return err
		}
		if err := outFile.Close(); err != nil {
			_ = os.Remove(destPath)
			return err
		}
		return nil
	}
	return fmt.Errorf("artifact archive %s did not contain a binary", archivePath)
}

func ensureRemoteSSHBinary(hostAlias, localBinary string, remoteInfo *remoteSSHInfo, logger *sshLaunchLogger) (string, error) {
	localFingerprint, err := fingerprintFile(localBinary)
	if err != nil {
		return "", fmt.Errorf("failed to fingerprint local executable: %w", err)
	}

	remoteDirSSH := fmt.Sprintf("$HOME/.cache/ledit-webui/backend/%s/%s-%s", localFingerprint, remoteInfo.Platform, remoteInfo.Arch)
	remoteBinarySSH := remoteDirSSH + "/ledit"
	remoteUploadSCP := fmt.Sprintf(".ledit-ssh-upload-%s.tmp", localFingerprint)

	// Fast path: if the fingerprinted backend already exists and executes,
	// skip upload/install and reuse it directly.
	checkExisting := newSSHCommand(hostAlias, fmt.Sprintf("[ -x %s ] && %s version", shellEscapeSSH(remoteBinarySSH), shellEscapeSSH(remoteBinarySSH)))
	if out, err := checkExisting.CombinedOutput(); err == nil {
		if output := trimSSHOutput(out); output != "" {
			logger.Logf("reuse-backend output:\n%s", output)
		}
		logger.Logf("reuse-backend found executable at %s", remoteBinarySSH)
		return remoteBinarySSH, nil
	} else {
		if output := trimSSHOutput(out); output != "" {
			logger.Logf("reuse-backend miss for %s:\n%s", remoteBinarySSH, output)
		}
		logger.Logf("reuse-backend check failed, proceeding with install")
	}

	mkdir := newSSHCommand(hostAlias, fmt.Sprintf("mkdir -p %s", shellEscapeSSH(remoteDirSSH)))
	if _, err := runSSHLoggedCommand(logger, "prepare-remote-dir", fmt.Sprintf("ssh %s mkdir -p %s", hostAlias, remoteDirSSH), mkdir); err != nil {
		return "", err
	}

	copyCmd := exec.Command("scp",
		"-q",
		localBinary,
		fmt.Sprintf("%s:%s", hostAlias, remoteUploadSCP),
	)
	if _, err := runSSHLoggedCommand(logger, "upload-backend", fmt.Sprintf("scp %s %s:%s", localBinary, hostAlias, remoteUploadSCP), copyCmd); err != nil {
		return "", err
	}

	install := newSSHCommand(hostAlias, fmt.Sprintf(
		"mv %s %s && chmod +x %s",
		`"$HOME/`+remoteUploadSCP+`"`,
		shellEscapeSSH(remoteBinarySSH),
		shellEscapeSSH(remoteBinarySSH),
	))
	if _, err := runSSHLoggedCommand(logger, "install-backend", fmt.Sprintf("ssh %s install backend into %s", hostAlias, remoteBinarySSH), install); err != nil {
		return "", err
	}

	// Verify the uploaded backend can execute on the remote host.
	verify := newSSHCommand(hostAlias, fmt.Sprintf("%s version", shellEscapeSSH(remoteBinarySSH)))
	if _, err := runSSHLoggedCommand(logger, "verify-backend", fmt.Sprintf("ssh %s verify backend executable %s", hostAlias, remoteBinarySSH), verify); err != nil {
		return "", newSSHLaunchFailure(
			"verify-backend",
			"uploaded SSH backend is not executable on remote host",
			err.Error(),
			logger,
		)
	}

	return remoteBinarySSH, nil
}

func fingerprintFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil))[:16], nil
}

func shellEscapeSSH(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func sshSessionRegistryPath() string {
	return filepath.Join(getLeditConfigDir(), "ssh_sessions.json")
}

func readPersistedSSHSession(sessionKey string) (*persistedSSHWorkspaceSession, error) {
	registry, err := readPersistedSSHSessionRegistry()
	if err != nil {
		return nil, err
	}
	session, ok := registry[sessionKey]
	if !ok {
		return nil, nil
	}
	copy := session
	return &copy, nil
}

func readPersistedSSHSessionRegistry() (map[string]persistedSSHWorkspaceSession, error) {
	data, err := os.ReadFile(sshSessionRegistryPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]persistedSSHWorkspaceSession{}, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return map[string]persistedSSHWorkspaceSession{}, nil
	}

	var registry map[string]persistedSSHWorkspaceSession
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, err
	}
	if registry == nil {
		registry = map[string]persistedSSHWorkspaceSession{}
	}
	return registry, nil
}

func writePersistedSSHSessionRegistry(registry map[string]persistedSSHWorkspaceSession) error {
	if err := os.MkdirAll(getLeditConfigDir(), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := sshSessionRegistryPath() + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmpPath, sshSessionRegistryPath())
}

func persistSSHSession(session *sshWorkspaceSession) error {
	if session == nil {
		return nil
	}
	registry, err := readPersistedSSHSessionRegistry()
	if err != nil {
		return err
	}
	registry[session.Key] = persistedSSHWorkspaceSession{
		Key:                 session.Key,
		HostAlias:           session.HostAlias,
		RemoteWorkspacePath: session.RemoteWorkspacePath,
		RemotePort:          session.RemotePort,
		RemotePID:           session.RemotePID,
		StartedAt:           session.StartedAt,
	}
	return writePersistedSSHSessionRegistry(registry)
}

func removePersistedSSHSession(sessionKey string) error {
	registry, err := readPersistedSSHSessionRegistry()
	if err != nil {
		return err
	}
	delete(registry, sessionKey)
	return writePersistedSSHSessionRegistry(registry)
}

func startRemoteSSHBackend(hostAlias, sessionKey, launcherURL, remoteWorkspacePath, remoteBinary string, logger *sshLaunchLogger) (int, int, error) {
	workspaceExpr := `"$HOME"`
	if remoteWorkspacePath != "$HOME" {
		workspaceExpr = shellEscapeSSH(remoteWorkspacePath)
	}

	script := strings.Join([]string{
		"set -e",
		"choose_port() {",
		"  if command -v python3 >/dev/null 2>&1; then",
		"    python3 - <<'PY'",
		"import socket",
		"s = socket.socket()",
		`s.bind(("127.0.0.1", 0))`,
		"print(s.getsockname()[1])",
		"s.close()",
		"PY",
		"    return",
		"  fi",
		"  if command -v python >/dev/null 2>&1; then",
		"    python - <<'PY'",
		"import socket",
		"s = socket.socket()",
		`s.bind(("127.0.0.1", 0))`,
		"print(s.getsockname()[1])",
		"s.close()",
		"PY",
		"    return",
		"  fi",
		`  echo "python3 or python is required on the remote host" >&2`,
		"  exit 1",
		"}",
		`mkdir -p "$HOME/.cache/ledit-webui/logs"`,
		fmt.Sprintf("cd %s", workspaceExpr),
		`REMOTE_PORT="$(choose_port)"`,
		fmt.Sprintf(`LOG_FILE="$HOME/.cache/ledit-webui/logs/%s.log"`, sanitizeRemoteLogName(hostAlias)),
		fmt.Sprintf(
			`nohup env BROWSER=none LEDIT_SSH_HOST_ALIAS=%s LEDIT_SSH_SESSION_KEY=%s LEDIT_SSH_LAUNCHER_URL=%s LEDIT_SSH_HOME="$HOME" %s --isolated-config agent --daemon --web-port "$REMOTE_PORT" >"$LOG_FILE" 2>&1 < /dev/null &`,
			shellEscapeSSH(hostAlias),
			shellEscapeSSH(sessionKey),
			shellEscapeSSH(launcherURL),
			shellEscapeSSH(remoteBinary),
		),
		"REMOTE_PID=$!",
		`printf "%s\n%s\n" "$REMOTE_PORT" "$REMOTE_PID"`,
	}, "\n")

	cmd := newSSHCommand(hostAlias, script)
	out, err := runSSHLoggedCommand(logger, "start-remote-backend", fmt.Sprintf("ssh %s start remote backend", hostAlias), cmd)
	if err != nil {
		return 0, 0, err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return 0, 0, fmt.Errorf("failed to determine remote backend port for %s", hostAlias)
	}
	remotePort, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || remotePort <= 0 {
		return 0, 0, fmt.Errorf("invalid remote web port for %s", hostAlias)
	}
	remotePID, _ := strconv.Atoi(strings.TrimSpace(lines[1]))
	return remotePort, remotePID, nil
}

func sanitizeRemoteLogName(hostAlias string) string {
	var b strings.Builder
	for _, r := range hostAlias {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '.' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "ssh"
	}
	return b.String()
}

func collectSSHHealthFailureDetails(hostAlias string, remotePID int, probeErr error, logger *sshLaunchLogger) string {
	sections := make([]string, 0, 3)
	if probeErr != nil {
		sections = append(sections, fmt.Sprintf("Local health probe failed: %v", probeErr))
	}

	remoteDetails := inspectRemoteSSHBackendFailure(hostAlias, remotePID, logger)
	if remoteDetails != "" {
		sections = append(sections, remoteDetails)
	}

	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

func inspectRemoteSSHBackendFailure(hostAlias string, remotePID int, logger *sshLaunchLogger) string {
	hostAlias = strings.TrimSpace(hostAlias)
	if hostAlias == "" {
		return ""
	}

	script := strings.Join([]string{
		"set +e",
		fmt.Sprintf("REMOTE_PID=%d", remotePID),
		fmt.Sprintf(`LOG_FILE="$HOME/.cache/ledit-webui/logs/%s.log"`, sanitizeRemoteLogName(hostAlias)),
		`if [ "$REMOTE_PID" -gt 0 ] && kill -0 "$REMOTE_PID" >/dev/null 2>&1; then`,
		`  echo "Remote backend PID: $REMOTE_PID (running)"`,
		"else",
		`  echo "Remote backend PID: $REMOTE_PID (not running)"`,
		"fi",
		`echo "Remote log: $LOG_FILE"`,
		`if [ -f "$LOG_FILE" ]; then`,
		`  echo "--- remote log tail ---"`,
		`  tail -n 80 "$LOG_FILE" 2>/dev/null || cat "$LOG_FILE" 2>/dev/null || true`,
		"else",
		`  echo "Remote log file not found"`,
		"fi",
	}, "\n")

	cmd := newSSHCommand(hostAlias, script)
	out, err := cmd.CombinedOutput()
	output := trimSSHOutput(out)
	if err != nil {
		if output != "" {
			logger.Logf("inspect-remote-failure output:\n%s", output)
		}
		logger.Logf("inspect-remote-failure error: %v", err)
		if output == "" {
			output = err.Error()
		}
		return fmt.Sprintf("Remote diagnostics failed: %s", output)
	}
	if output != "" {
		logger.Logf("inspect-remote-failure output:\n%s", output)
	}
	return output
}

func startSSHTunnel(hostAlias string, localPort, remotePort int, logger *sshLaunchLogger) (*exec.Cmd, error) {
	cmd := exec.Command("ssh",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "ExitOnForwardFailure=yes",
		"-N",
		"-L", fmt.Sprintf("%d:127.0.0.1:%d", localPort, remotePort),
		hostAlias,
	)
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		logger.Logf("start-tunnel error: %v %s", err, strings.TrimSpace(stderr.String()))
		return nil, newSSHLaunchFailure("start-tunnel", "failed to start SSH tunnel", strings.TrimSpace(stderr.String()), logger)
	}
	logger.Logf("start-tunnel launched pid=%d", cmd.Process.Pid)

	return cmd, nil
}

func waitForWebHealth(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 800 * time.Millisecond}
	var lastErr error
	const pollInterval = 250 * time.Millisecond

	// Give the remote daemon and SSH tunnel a brief warm-up window to avoid
	// failing on initial connection-reset bursts while the backend binds.
	time.Sleep(300 * time.Millisecond)

	for time.Now().Before(deadline) {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("health endpoint returned %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(pollInterval)
	}

	if lastErr == nil {
		lastErr = errors.New("timed out waiting for remote ledit backend")
	}
	return fmt.Errorf("failed to connect to SSH workspace: %w", lastErr)
}

func findFreeLocalPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("failed to determine local port")
	}
	return addr.Port, nil
}

func killProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
	return nil
}

func stopRemoteSSHBackend(hostAlias string, remotePID int) error {
	if strings.TrimSpace(hostAlias) == "" || remotePID <= 0 {
		return nil
	}

	cmd := newSSHCommand(hostAlias, fmt.Sprintf("kill %d >/dev/null 2>&1 || true", remotePID))
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	return nil
}

func (ws *ReactWebServer) stopSSHSessionLocked(key string) {
	session := ws.sshSessions[key]
	if session == nil {
		_ = removePersistedSSHSession(key)
		return
	}
	_ = killProcess(session.TunnelCmd)
	_ = stopRemoteSSHBackend(session.HostAlias, session.RemotePID)
	_ = removePersistedSSHSession(key)
	delete(ws.sshSessions, key)
}

func (ws *ReactWebServer) watchSSHSession(key string, session *sshWorkspaceSession, cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	_ = cmd.Wait()
	ws.sshSessionsMu.Lock()
	defer ws.sshSessionsMu.Unlock()
	current := ws.sshSessions[key]
	if current != nil && current == session {
		_ = stopRemoteSSHBackend(session.HostAlias, session.RemotePID)
		_ = removePersistedSSHSession(key)
		delete(ws.sshSessions, key)
	}
}

func (ws *ReactWebServer) shutdownSSHSessions() {
	ws.sshSessionsMu.Lock()
	defer ws.sshSessionsMu.Unlock()
	for key := range ws.sshSessions {
		ws.stopSSHSessionLocked(key)
	}
}
