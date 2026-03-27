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
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"
)

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
}

type remoteSSHInfo struct {
	Platform string
	Arch     string
}

const (
	githubReleaseRepoOwner = "alantheprice"
	githubReleaseRepoName  = "ledit"
)

func (ws *ReactWebServer) launchSSHWorkspace(req sshLaunchRequestDTO) (*sshLaunchResult, error) {
	hostAlias := strings.TrimSpace(req.HostAlias)
	if hostAlias == "" {
		return nil, errors.New("SSH host alias is required")
	}

	remoteWorkspacePath := strings.TrimSpace(req.RemoteWorkspacePath)
	if remoteWorkspacePath == "" {
		remoteWorkspacePath = "$HOME"
	}

	sessionKey := hostAlias + "::" + remoteWorkspacePath

	ws.sshSessionsMu.Lock()
	if existing := ws.sshSessions[sessionKey]; existing != nil {
		if err := waitForWebHealth(existing.LocalPort, 2*time.Second); err == nil {
			result := &sshLaunchResult{URL: existing.URL, LocalPort: existing.LocalPort}
			ws.sshSessionsMu.Unlock()
			return result, nil
		}
		ws.stopSSHSessionLocked(sessionKey)
	}
	ws.sshSessionsMu.Unlock()

	if restored, err := ws.restorePersistedSSHSession(sessionKey); err == nil && restored != nil {
		return restored, nil
	}

	if err := ensureSSHProgramsAvailable(); err != nil {
		return nil, err
	}

	remoteInfo, err := inspectRemoteSSHHost(hostAlias)
	if err != nil {
		return nil, err
	}

	localBinary, err := prepareLocalSSHBinary(remoteInfo.Platform, remoteInfo.Arch)
	if err != nil {
		return nil, err
	}

	remoteBinary, err := ensureRemoteSSHBinary(hostAlias, localBinary, remoteInfo)
	if err != nil {
		return nil, err
	}

	localPort, err := findFreeLocalPort()
	if err != nil {
		return nil, err
	}

	remotePort, remotePID, err := startRemoteSSHBackend(hostAlias, remoteWorkspacePath, remoteBinary)
	if err != nil {
		return nil, err
	}

	tunnelCmd, err := startSSHTunnel(hostAlias, localPort, remotePort)
	if err != nil {
		return nil, err
	}

	if err := waitForWebHealth(localPort, 10*time.Second); err != nil {
		_ = killProcess(tunnelCmd)
		return nil, err
	}

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

	tunnelCmd, err := startSSHTunnel(persisted.HostAlias, localPort, persisted.RemotePort)
	if err != nil {
		_ = removePersistedSSHSession(sessionKey)
		return nil, err
	}

	if err := waitForWebHealth(localPort, 6*time.Second); err != nil {
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

func inspectRemoteSSHHost(hostAlias string) (*remoteSSHInfo, error) {
	cmd := exec.Command("ssh",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		hostAlias,
		"bash", "-lc",
		"uname -s; uname -m",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
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

func prepareLocalSSHBinary(remotePlatform, remoteArch string) (string, error) {
	if runtime.GOOS == remotePlatform && runtime.GOARCH == remoteArch {
		return currentExecutableForSSH()
	}

	if artifactPath, err := ensureLocalSSHBinaryArtifact(remotePlatform, remoteArch); err == nil && artifactPath != "" {
		return artifactPath, nil
	}

	goBinary, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("remote host requires %s/%s, but this machine is %s/%s and Go is not available to build a matching backend", remotePlatform, remoteArch, runtime.GOOS, runtime.GOARCH)
	}

	executablePath, err := currentExecutableForSSH()
	if err != nil {
		return "", err
	}
	repoRoot := filepath.Dir(executablePath)
	if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err != nil {
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
	if out, err := buildCmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("failed to build SSH backend for %s/%s: %s", remotePlatform, remoteArch, msg)
	}

	return outputPath, nil
}

func ensureLocalSSHBinaryArtifact(remotePlatform, remoteArch string) (string, error) {
	tag := resolvePreferredReleaseTag()
	assetName := fmt.Sprintf("ledit-%s-%s.tar.gz", remotePlatform, remoteArch)
	cacheDir := filepath.Join(os.TempDir(), "ledit-ssh-artifacts", strings.TrimPrefix(tag, "v"), remotePlatform+"-"+remoteArch)
	binaryPath := filepath.Join(cacheDir, fmt.Sprintf("ledit-%s-%s", remotePlatform, remoteArch))

	if info, err := os.Stat(binaryPath); err == nil && info.Mode().IsRegular() {
		return binaryPath, nil
	}

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to prepare SSH artifact cache: %w", err)
	}

	downloadURL, err := resolveGitHubReleaseAssetURL(tag, assetName)
	if err != nil {
		return "", err
	}

	archivePath := filepath.Join(cacheDir, assetName)
	if err := downloadFile(downloadURL, archivePath); err != nil {
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
		if tag := normalizeReleaseTagCandidate(info.Main.Version); tag != "" {
			return tag
		}
		for _, setting := range info.Settings {
			if (setting.Key == "vcs.tag" || setting.Key == "gitTag") && normalizeReleaseTagCandidate(setting.Value) != "" {
				return normalizeReleaseTagCandidate(setting.Value)
			}
		}
	}
	return "latest"
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

func resolveGitHubReleaseAssetURL(tag, assetName string) (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/%s", githubReleaseRepoOwner, githubReleaseRepoName, releaseSelector(tag))
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to query GitHub releases: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("failed to resolve GitHub release asset %s for %s: %s", assetName, tag, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	for _, asset := range payload.Assets {
		if asset.Name == assetName && strings.TrimSpace(asset.URL) != "" {
			return asset.URL, nil
		}
	}
	return "", fmt.Errorf("release %s does not contain asset %s", tag, assetName)
}

func releaseSelector(tag string) string {
	if tag == "" || tag == "latest" {
		return "latest"
	}
	return "tags/" + tag
}

func downloadFile(url, destPath string) error {
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

func ensureRemoteSSHBinary(hostAlias, localBinary string, remoteInfo *remoteSSHInfo) (string, error) {
	localFingerprint, err := fingerprintFile(localBinary)
	if err != nil {
		return "", fmt.Errorf("failed to fingerprint local executable: %w", err)
	}

	remoteDirSSH := fmt.Sprintf("$HOME/.cache/ledit-webui/backend/%s/%s-%s", localFingerprint, remoteInfo.Platform, remoteInfo.Arch)
	remoteDirSCP := fmt.Sprintf("~/.cache/ledit-webui/backend/%s/%s-%s", localFingerprint, remoteInfo.Platform, remoteInfo.Arch)
	remoteBinarySSH := remoteDirSSH + "/ledit"
	remoteBinarySCP := remoteDirSCP + "/ledit"

	mkdir := exec.Command("ssh",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		hostAlias,
		"bash", "-lc",
		fmt.Sprintf("mkdir -p %s", shellEscapeSSH(remoteDirSSH)),
	)
	if out, err := mkdir.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}

	copyCmd := exec.Command("scp",
		"-q",
		localBinary,
		fmt.Sprintf("%s:%s.tmp", hostAlias, remoteBinarySCP),
	)
	if out, err := copyCmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}

	install := exec.Command("ssh",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		hostAlias,
		"bash", "-lc",
		fmt.Sprintf(
			"mv %s %s && chmod +x %s",
			shellEscapeSSH(remoteBinarySSH+".tmp"),
			shellEscapeSSH(remoteBinarySSH),
			shellEscapeSSH(remoteBinarySSH),
		),
	)
	if out, err := install.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
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

func startRemoteSSHBackend(hostAlias, remoteWorkspacePath, remoteBinary string) (int, int, error) {
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
		fmt.Sprintf(`nohup env BROWSER=none %s --isolated-config agent --daemon --web-port "$REMOTE_PORT" >"$LOG_FILE" 2>&1 < /dev/null &`, shellEscapeSSH(remoteBinary)),
		"REMOTE_PID=$!",
		`printf "%s\n%s\n" "$REMOTE_PORT" "$REMOTE_PID"`,
	}, "; ")

	cmd := exec.Command("ssh",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		hostAlias,
		"bash", "-lc",
		script,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return 0, 0, errors.New(msg)
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

func startSSHTunnel(hostAlias string, localPort, remotePort int) (*exec.Cmd, error) {
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
		return nil, fmt.Errorf("failed to start SSH tunnel: %w", err)
	}

	return cmd, nil
}

func waitForWebHealth(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 800 * time.Millisecond}
	var lastErr error

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
		time.Sleep(250 * time.Millisecond)
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

	cmd := exec.Command("ssh",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		hostAlias,
		"bash", "-lc",
		fmt.Sprintf("kill %d >/dev/null 2>&1 || true", remotePID),
	)
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
