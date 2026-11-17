//go:build linux

package gpulink

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const systemdUnit = "sprout-gpu.service"

func servicePath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "systemd", "user", systemdUnit), nil
}

// InstallService registers `sprout gpu start` as a systemd user unit that
// starts at login and restarts if it exits.
func InstallService() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if strings.ContainsAny(exe, " \t\n\"'\\") {
		return "", fmt.Errorf("cannot write a systemd unit for a binary path with spaces or quotes: %s", exe)
	}
	path, err := servicePath()
	if err != nil {
		return "", err
	}
	unit := "[Unit]\nDescription=Sprout GPU link\nAfter=network-online.target\n\n" +
		"[Service]\nExecStart=" + exe + " gpu start\nRestart=always\nRestartSec=30\n\n" +
		"[Install]\nWantedBy=default.target\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(unit), 0o644); err != nil {
		return "", err
	}
	// restart, not just start: after a new link the running agent must pick
	// up the new link's state and key.
	for _, args := range [][]string{{"--user", "daemon-reload"}, {"--user", "enable", systemdUnit}, {"--user", "restart", systemdUnit}} {
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil { //nolint:gosec // G204: fixed systemctl subcommands for the link's own unit
			return "", fmt.Errorf("systemctl %s: %s", strings.Join(args, " "), out)
		}
	}
	// A user service stops at logout unless the user lingers; a GPU box
	// usually has nobody logged in. Enabling it needs no sudo where polkit
	// lets users linger themselves; ServiceNotice says when it did not.
	_ = exec.Command("loginctl", "enable-linger").Run()
	return path, nil
}

// UninstallService disables and removes the systemd user unit.
func UninstallService() error {
	_ = exec.Command("systemctl", "--user", "disable", "--now", systemdUnit).Run()
	path, err := servicePath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	return nil
}

// ServiceNotice is what to know about the service on this machine: on Linux,
// that it stops at logout while the user does not linger.
func ServiceNotice() string {
	out, err := exec.Command("loginctl", "show-user", strconv.Itoa(os.Getuid()), "--property=Linger", "--value").Output() //nolint:gosec // G204: fixed loginctl query for the current user
	if err == nil && strings.TrimSpace(string(out)) == "yes" {
		return ""
	}
	return "The service stops when you log out; run `sudo loginctl enable-linger $USER` to keep it serving."
}

// ServiceState is whether the service is running, and how to see its log.
func ServiceState() (state, logs string) {
	out, _ := exec.Command("systemctl", "--user", "is-active", systemdUnit).Output() //nolint:gosec // G204: fixed systemctl query for the link's own unit
	state = strings.TrimSpace(string(out))
	if state == "" {
		state = "not installed"
	}
	return state, "journalctl --user -u " + systemdUnit + " -n 50 --no-pager"
}
