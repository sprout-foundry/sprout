//go:build linux

package service

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/utils/pidalive"
	"github.com/sprout-foundry/sprout/pkg/webui"
)

// isPortInUse checks if a TCP port is already in use by attempting to bind to it.
func isPortInUse(port int) bool {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return true
	}
	listener.Close()
	return false
}

// findPIDOnPort returns the PID of a process listening on the given port.
// Tries 'fuser' first (works on most Linux); falls back to /proc/net/tcp;
// then pgrep for the known sprout daemon process name.
func findPIDOnPort(port int) int {
	if pid := findPIDViaFuser(port); pid > 0 {
		return pid
	}
	if pid := findPIDViaProcNet(port); pid > 0 {
		return pid
	}
	return findPIDViaPgrep()
}

// findPIDViaFuser runs 'fuser <port>/tcp' to find the PID holding a port.
func findPIDViaFuser(port int) int {
	cmd := exec.Command("fuser", fmt.Sprintf("%d/tcp", port))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(out))
	for _, f := range fields {
		// fuser outputs PIDs with a '+' for listening sockets; strip it
		pidStr := strings.TrimRight(f, "+")
		if pid, err := strconv.Atoi(pidStr); err == nil && pid > 0 {
			return pid
		}
	}
	return 0
}

// findPIDViaPgrep runs 'pgrep -f "sprout agent"' as a last resort for
// environments where fuser and /proc/net/tcp are both inaccessible (e.g.
// Termux sandbox). Returns the first matching PID or 0.
func findPIDViaPgrep() int {
	cmd := exec.Command("pgrep", "-f", "sprout agent")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0
	}
	for _, line := range strings.Fields(string(out)) {
		if pid, err := strconv.Atoi(line); err == nil && pid > 0 {
			return pid
		}
	}
	return 0
}

// findPIDViaProcNet scans /proc/net/tcp for a listening socket on the port,
// then finds the owning PID via /proc/[pid]/fd inode matching.
func findPIDViaProcNet(port int) int {
	data, err := os.ReadFile("/proc/net/tcp")
	if err != nil {
		return 0
	}
	hexPort := fmt.Sprintf("%04X", port)
	target := fmt.Sprintf("0100007F:%s", hexPort)
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, " sl ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		if fields[1] == target && strings.TrimSpace(fields[3]) == "0A" {
			inode := strings.TrimSpace(fields[9])
			return findPIDByInode(inode)
		}
	}
	return 0
}

// findPIDByInode scans /proc/[pid]/fd for a socket with the given inode.
// Only matches processes owned by the current user.
func findPIDByInode(inode string) int {
	myUID := os.Getuid()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		// Skip processes not owned by us
		status, _ := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
		if !strings.Contains(string(status), fmt.Sprintf("Uid:\t%d", myUID)) {
			continue
		}
		fds, _ := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
		for _, fd := range fds {
			link, _ := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", pid, fd.Name()))
			if strings.Contains(link, "socket:"+inode) {
				return pid
			}
		}
	}
	return 0
}

func init() {
	if systemdAvailable() {
		newServiceManager = func() serviceManager { return &systemdManager{} }
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			homeDir = ""
		}
		newServiceManager = func() serviceManager { return &pidFileManager{homeDir: homeDir} }
	}
}

// systemdAvailable checks if systemctl --user can communicate with a running systemd instance.
func systemdAvailable() bool {
	cmd := exec.Command("systemctl", "--user", "is-system-running")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	s := strings.TrimSpace(string(out))
	return s == "running" || s == "degraded"
}

type systemdManager struct{}

// systemdExecArg quotes a path for use in a systemd ExecStart line.
// Only the executable in ExecStart= supports quoting; other directives
// like WorkingDirectory= and EnvironmentFile= take literal paths.
func systemdExecArg(s string) string {
	if strings.ContainsAny(s, " \t\"\\") {
		return strconv.Quote(s)
	}
	return s
}

// generateSystemdUnit produces a systemd user unit file for the sprout daemon.
func generateSystemdUnit(binaryPath, homeDir string) ([]byte, error) {
	if binaryPath == "" {
		return nil, fmt.Errorf("binary path must not be empty")
	}
	if homeDir == "" {
		return nil, fmt.Errorf("home directory must not be empty")
	}

	// Build absolute path to service.env file for EnvironmentFile directive
	envFile := ServiceEnvPath(homeDir)

	// ExecStart executable can be quoted for paths with spaces.
	// WorkingDirectory, Environment HOME, and EnvironmentFile take literal
	// unquoted paths — systemd treats quotes as part of the value.
	unit := fmt.Sprintf(`[Unit]
Description=sprout daemon - AI coding assistant web UI
After=default.target

[Service]
Type=simple
ExecStart=%s agent -d --no-connection-check
WorkingDirectory=%s
Restart=on-failure
RestartSec=5
TimeoutStopSec=15
KillMode=mixed
KillSignal=SIGTERM
Environment=SPROUT_SERVICE=1
Environment=HOME=%s
Environment=SPROUT_DAEMON_ROOT=%s
EnvironmentFile=-%s
StandardOutput=null
StandardError=null

[Install]
WantedBy=default.target
`, systemdExecArg(binaryPath), homeDir, homeDir, homeDir, envFile)

	return []byte(unit), nil
}

func (m *systemdManager) Install() error {
	binaryPath, err := getBinaryPath()
	if err != nil {
		return fmt.Errorf("failed to get binary path: %w", err)
	}
	if filepath.Base(binaryPath) != "sprout" {
		return fmt.Errorf("unexpected binary name %q — service unit requires the sprout binary", filepath.Base(binaryPath))
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	// Capture API keys from the current environment and write to service.env
	// This is done before writing the unit file so the EnvironmentFile can reference it
	if err := GenerateServiceEnvFile(homeDir); err != nil {
		fmt.Printf("Warning: failed to generate service.env: %v\n", err)
		fmt.Println("The service will be installed but may not have access to API keys.")
	}

	unitDir := filepath.Join(homeDir, ".config", "systemd", "user")
	if err := os.MkdirAll(unitDir, 0755); err != nil {
		return fmt.Errorf("failed to create systemd user directory: %w", err)
	}

	content, err := generateSystemdUnit(binaryPath, homeDir)
	if err != nil {
		return fmt.Errorf("failed to generate unit file: %w", err)
	}

	unitFile := filepath.Join(unitDir, "sprout.service")
	tmpFile := unitFile + ".tmp"
	if err := os.WriteFile(tmpFile, content, 0644); err != nil {
		return fmt.Errorf("failed to write unit file: %w", err)
	}
	if err := os.Rename(tmpFile, unitFile); err != nil {
		return fmt.Errorf("failed to rename unit file: %w", err)
	}

	fmt.Printf("Installed systemd user unit to %s\n", unitFile)

	// daemon-reload may fail in containers/VMs without a running systemd user instance
	// The unit file is still written and can be started manually or on next login
	if _, err := runSystemctl("daemon-reload"); err != nil {
		fmt.Printf("Warning: daemon-reload failed (systemd user instance may not be running): %v\n", err)
		fmt.Printf("\nTo start the daemon without systemd, run:\n  %s\n", fallbackCommand(homeDir))
	}

	if _, err := runSystemctl("enable", "sprout.service"); err != nil {
		fmt.Printf("Warning: failed to enable service: %v\n", err)
	} else {
		fmt.Println("Service enabled.")
	}
	return nil
}

func (m *systemdManager) Uninstall() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	// Check for active sessions before uninstalling
	count, err := checkActiveSessions()
	if err != nil {
		fmt.Printf("Warning: failed to check active sessions: %v\n", err)
	}
	if count > 0 {
		fmt.Printf("Warning: %d active agent session(s) detected. Uninstalling will stop the daemon and terminate these sessions.\n", count)
		if !ForceConfirm {
			fmt.Printf("Continue? %s ", console.FormatYesNoPromptStdout(false))
			reader := bufio.NewReader(os.Stdin)
			resp, _ := reader.ReadString('\n')
			resp = strings.TrimSpace(strings.ToLower(resp))
			if resp != "y" {
				fmt.Println("Aborted.")
				return nil
			}
		} else {
			fmt.Println("Skipping confirmation (--yes flag set).")
		}
	}

	// Stop and disable — ignore errors since the service may not be running.
	runSystemctl("stop", "sprout.service")
	runSystemctl("disable", "sprout.service")

	unitFile := filepath.Join(homeDir, ".config", "systemd", "user", "sprout.service")
	if err := os.Remove(unitFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove unit file: %w", err)
	}

	// Remove the service.env file if it exists
	envFile := ServiceEnvPath(homeDir)
	if err := os.Remove(envFile); err != nil && !os.IsNotExist(err) {
		// Don't fail the whole uninstall if we can't remove service.env
		fmt.Printf("Warning: failed to remove %s: %v\n", envFile, err)
	}

	if _, err := runSystemctl("daemon-reload"); err != nil {
		return fmt.Errorf("daemon-reload failed: %w", err)
	}

	fmt.Printf("Uninstalled systemd user unit from %s\n", unitFile)
	return nil
}

func (m *systemdManager) Start() error {
	// Try systemctl first; if it fails (e.g., systemd user instance not running in containers),
	// fall back to running the daemon directly in the background
	if _, err := runSystemctl("start", "sprout.service"); err != nil {
		// Check if it's a systemd availability issue (not a service config issue)
		if strings.Contains(err.Error(), "Failed to start") {
			return fmt.Errorf("failed to start service: %w", err)
		}
		// For other errors (like unit not found), try to start the daemon directly
		fmt.Printf("systemctl start failed (systemd may not be running): %v\n", err)
		fmt.Printf("To start the daemon manually in the background:\n  %s\n", fallbackCommand(""))
		return nil
	}
	fmt.Println("Service started.")
	return nil
}

func (m *systemdManager) Stop() error {
	if _, err := runSystemctl("stop", "sprout.service"); err != nil {
		return fmt.Errorf("failed to stop service: %w", err)
	}
	fmt.Println("Service stopped.")
	return nil
}

func (m *systemdManager) Status() (bool, error) {
	output, err := runSystemctl("show", "--property=SubState", "sprout.service")
	if err != nil {
		return false, nil // service not known — treat as stopped
	}
	// Output format: "SubState=running\n"
	return strings.HasPrefix(strings.TrimSpace(output), "SubState=running"), nil
}

// fallbackCommand returns the manual start command for non-systemd environments.
func fallbackCommand(homeDir string) string {
	if homeDir == "" {
		var err error
		homeDir, err = os.UserHomeDir()
		if err != nil {
			homeDir = "$HOME"
		}
	}
	return "nohup sprout agent -d &\nView logs with: tail -f ~/.sprout/logs/daemon.stdout.log"
}

// runSystemctl executes a systemctl command at user scope and returns its stdout.
func runSystemctl(args ...string) (string, error) {
	userArgs := append([]string{"--user"}, args...)
	cmd := exec.Command("systemctl", userArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("systemctl %s: %s", strings.Join(userArgs, " "), strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// -----------------------------------------------------------------------
// PID-file based service manager (non-systemd environments)
// -----------------------------------------------------------------------

// pidFileManager manages the daemon via a PID file (~/.sprout/daemon.pid).
// It implements the same serviceManager interface as systemdManager.
type pidFileManager struct {
	homeDir string
}

func (m *pidFileManager) pidPath() string {
	return filepath.Join(m.homeDir, ".sprout", "daemon.pid")
}

func (m *pidFileManager) readPID() (int, error) {
	data, err := os.ReadFile(m.pidPath())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func (m *pidFileManager) writePID(pid int) error {
	sproutDir := filepath.Join(m.homeDir, ".sprout")
	if err := os.MkdirAll(sproutDir, 0755); err != nil {
		return err
	}
	tmpFile := m.pidPath() + ".tmp"
	if err := os.WriteFile(tmpFile, []byte(strconv.Itoa(pid)), 0644); err != nil {
		os.Remove(tmpFile)
		return err
	}
	return os.Rename(tmpFile, m.pidPath())
}

func (m *pidFileManager) Install() error {
	fmt.Println("No systemd detected — no service installation needed.")
	fmt.Println("Use 'sprout service start' to run the daemon directly.")
	fmt.Println("The daemon will persist via PID file at ~/.sprout/daemon.pid")
	return nil
}

func (m *pidFileManager) Uninstall() error {
	pidPath := m.pidPath()
	if err := os.Remove(pidPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove PID file: %w", err)
	}
	fmt.Println("Daemon PID file cleaned up.")
	return nil
}

func (m *pidFileManager) Start() error {
	binaryPath, err := getBinaryPath()
	if err != nil {
		return fmt.Errorf("failed to get binary path: %w", err)
	}

	// Check if already running via PID file
	if pid, err := m.readPID(); err == nil && pidalive.IsAlive(pid) {
		fmt.Printf("Daemon already running (PID %d)\n", pid)
		return nil
	}

	// Check if port is already in use (e.g. pre-existing nohup daemon or
	// another sprout instance). This prevents spawning a second daemon that
	// will immediately fail on port binding.
	if isPortInUse(webui.DaemonPort) {
		fmt.Printf("Port %d is already in use — a daemon may already be running\n", webui.DaemonPort)
		return nil
	}

	// Clean up stale PID file if process is dead
	if _, err := m.readPID(); err == nil {
		os.Remove(m.pidPath())
	}

	// Load service.env into env vars
	envMap, err := LoadServiceEnvFile(m.homeDir)
	if err != nil {
		fmt.Printf("Warning: failed to load service.env: %v\n", err)
		envMap = make(map[string]string)
	}

	// Build environment for child process
	childEnv := os.Environ()
	// Add/override env vars from service.env
	envMap["SPROUT_SERVICE"] = "1"
	for k, v := range envMap {
		// Remove any existing var with same prefix
		childEnv = removeEnvPrefix(childEnv, k+"=")
		childEnv = append(childEnv, k+"="+v)
	}

	// Redirect stdout/stderr to log files
	logDir := filepath.Join(m.homeDir, ".sprout", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}
	stdoutFile, err := os.OpenFile(filepath.Join(logDir, "daemon.stdout.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open stdout log: %w", err)
	}
	stderrFile, err := os.OpenFile(filepath.Join(logDir, "daemon.stderr.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		stdoutFile.Close()
		return fmt.Errorf("failed to open stderr log: %w", err)
	}

	cmd := exec.Command(binaryPath, "agent", "-d", "--no-connection-check")
	cmd.Env = childEnv
	cmd.Dir = m.homeDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // New session (daemonize)
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile

	if err := cmd.Start(); err != nil {
		stdoutFile.Close()
		stderrFile.Close()
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Write PID file
	if err := m.writePID(cmd.Process.Pid); err != nil {
		// Failed to write PID — kill the process to avoid orphan
		cmd.Process.Kill()
		cmd.Process.Wait() // reap the zombie
		stdoutFile.Close()
		stderrFile.Close()
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	// Close file descriptors in parent (child inherits them)
	stdoutFile.Close()
	stderrFile.Close()

	// Brief wait to check if it exited immediately
	time.Sleep(200 * time.Millisecond)
	if !pidalive.IsAlive(cmd.Process.Pid) {
		os.Remove(m.pidPath())
		return fmt.Errorf("daemon exited immediately; check ~/.sprout/logs/daemon.stderr.log")
	}

	fmt.Printf("Daemon started (PID %d)\n", cmd.Process.Pid)
	fmt.Printf("Logs: ~/.sprout/logs/daemon.stdout.log\n")
	return nil
}

func (m *pidFileManager) Stop() error {
	pid, err := m.readPID()
	if err == nil && pidalive.IsAlive(pid) {
		// We have a known PID — stop it
		if err := m.stopProcess(pid); err != nil {
			return err
		}
		os.Remove(m.pidPath())
		fmt.Println("Daemon stopped.")
		return nil
	}

	// No known PID or process is dead. Clean up stale PID file.
	if err == nil {
		os.Remove(m.pidPath())
	}

	// Check if something is still holding the port (e.g. pre-existing nohup daemon)
	if isPortInUse(webui.DaemonPort) {
		portPID := findPIDOnPort(webui.DaemonPort)
		if portPID > 0 {
			fmt.Printf("No PID file, but port %d is in use (PID %d). Stopping...\n", webui.DaemonPort, portPID)
			if err := m.stopProcess(portPID); err != nil {
				return err
			}
			fmt.Println("Daemon stopped.")
			return nil
		}
	}

	fmt.Println("Daemon is not running (no PID file).")
	return nil
}

// stopProcess sends SIGTERM, waits up to 15s, then SIGKILL.
func (m *pidFileManager) stopProcess(pid int) error {
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM to daemon (PID %d): %w", pid, err)
	}
	for i := 0; i < 150; i++ {
		time.Sleep(100 * time.Millisecond)
		if !pidalive.IsAlive(pid) {
			break
		}
	}
	if pidalive.IsAlive(pid) {
		syscall.Kill(pid, syscall.SIGKILL)
		time.Sleep(200 * time.Millisecond)
	}
	return nil
}

func (m *pidFileManager) Status() (bool, error) {
	pid, err := m.readPID()
	if err == nil && pidalive.IsAlive(pid) {
		return true, nil
	}
	// No PID file or stale PID — check if port is in use (e.g. pre-existing daemon)
	if isPortInUse(webui.DaemonPort) {
		return true, nil
	}
	return false, nil
}

// removeEnvPrefix removes all entries from env that start with the given prefix.
func removeEnvPrefix(env []string, prefix string) []string {
	result := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			result = append(result, e)
		}
	}
	return result
}
