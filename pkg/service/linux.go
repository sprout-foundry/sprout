//go:build linux

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sprout-foundry/sprout/pkg/envutil"
	"github.com/sprout-foundry/sprout/pkg/utils/pidalive"
	"github.com/sprout-foundry/sprout/pkg/webui"
)

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

// pidFileManager manages the daemon via a PID file (~/.local/state/sprout/daemon.pid).
// It implements the same serviceManager interface as systemdManager.
type pidFileManager struct {
	homeDir string
}

func (m *pidFileManager) pidPath() string {
	stateDir, err := envutil.StateDir()
	if err != nil {
		return filepath.Join(m.homeDir, ".local", "state", "sprout", "daemon.pid")
	}
	return filepath.Join(stateDir, "daemon.pid")
}

func (m *pidFileManager) readPID() (int, error) {
	data, err := os.ReadFile(m.pidPath())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func (m *pidFileManager) writePID(pid int) error {
	stateDir, err := envutil.StateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0700); err != nil {
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
	fmt.Println("The daemon will persist via PID file at ~/.local/state/sprout/daemon.pid")
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
	envMap, err := LoadServiceEnvFile()
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
	stateDir, err := envutil.StateDir()
	if err != nil {
		return fmt.Errorf("failed to resolve state directory: %w", err)
	}
	logDir := filepath.Join(stateDir, "logs")
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
		return fmt.Errorf("daemon exited immediately; check ~/.local/state/sprout/logs/daemon.stderr.log")
	}

	fmt.Printf("Daemon started (PID %d)\n", cmd.Process.Pid)
	fmt.Printf("Logs: ~/.local/state/sprout/logs/daemon.stdout.log\n")
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
