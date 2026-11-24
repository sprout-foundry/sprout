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

// systemdUnitTemplate is the template unit; each link runs as an instance
// sprout-gpu@<slot>.service, so several links share one machine.
const systemdUnitTemplate = "sprout-gpu@.service"

// legacySystemdUnit is the single unit older sprouts installed; its
// replacement by instances happens on the next install.
const legacySystemdUnit = "sprout-gpu.service"

func systemdUserDir() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "systemd", "user"), nil
}

func instanceUnit(slot string) string { return "sprout-gpu@" + slot + ".service" }

// writeTemplate writes the template unit both instances and legacy-unit
// replacement run through.
func writeTemplate() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if strings.ContainsAny(exe, " \t\n\"'\\") {
		return "", fmt.Errorf("cannot write a systemd unit for a binary path with spaces or quotes: %s", exe)
	}
	dir, err := systemdUserDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, systemdUnitTemplate)
	unit := "[Unit]\nDescription=Sprout GPU link %i\nAfter=network-online.target\n\n" +
		"[Service]\nExecStart=" + exe + " gpu start --name %i\nRestart=always\nRestartSec=30\n\n" +
		"[Install]\nWantedBy=default.target\n"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(unit), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// InstallService registers `sprout gpu start --name <name>` as a systemd
// user unit instance that starts at login and restarts if it exits, and
// replaces the single unit an older sprout installed. The instance is the
// link's slot: systemd instance names, like the state files, key on it.
func InstallService(name string) (string, error) {
	slot := Slot(name)
	if slot == "" {
		return "", fmt.Errorf("link name %q has no usable service instance name; pick another with --name", name)
	}
	path, err := writeTemplate()
	if err != nil {
		return "", err
	}
	// restart, not just start: after a new link the running agent must pick
	// up the new link's state and key.
	for _, args := range [][]string{
		{"--user", "daemon-reload"},
		{"--user", "enable", instanceUnit(slot)},
		{"--user", "restart", instanceUnit(slot)},
	} {
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil { //nolint:gosec // G204: fixed systemctl subcommands for the link's own unit
			return "", fmt.Errorf("systemctl %s: %s", strings.Join(args, " "), out)
		}
	}
	// A user service stops at logout unless the user lingers; a GPU box
	// usually has nobody logged in. Enabling it needs no sudo where polkit
	// lets users linger themselves; ServiceNotice says when it did not.
	_ = exec.Command("loginctl", "enable-linger").Run()
	uninstallLegacyService()
	return path, nil
}

// UninstallService disables and removes one link's service instance. An
// empty name removes every instance and the template.
func UninstallService(name string) error {
	if name != "" {
		_ = exec.Command("systemctl", "--user", "disable", "--now", instanceUnit(name)).Run() //nolint:gosec // G204: fixed systemctl subcommand for the link's own unit
		uninstallLegacyService()
		return nil
	}
	dir, err := systemdUserDir()
	if err != nil {
		return err
	}
	links, err := LoadLinks()
	if err == nil {
		for _, st := range links {
			_ = exec.Command("systemctl", "--user", "disable", "--now", instanceUnit(st.Slot())).Run() //nolint:gosec // G204: fixed systemctl subcommand for the link's own unit
		}
	}
	_ = exec.Command("systemctl", "--user", "disable", "--now", legacySystemdUnit).Run()
	if err := os.Remove(filepath.Join(dir, systemdUnitTemplate)); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	return nil
}

// uninstallLegacyService stops and removes the single unit an older sprout
// installed; instances have replaced it.
func uninstallLegacyService() {
	_ = exec.Command("systemctl", "--user", "disable", "--now", legacySystemdUnit).Run() //nolint:gosec // G204: fixed systemctl subcommand for the link's own unit
	dir, err := systemdUserDir()
	if err != nil {
		return
	}
	_ = os.Remove(filepath.Join(dir, legacySystemdUnit))
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
}

// LegacyServiceInstalled reports whether the single unit an older sprout
// installed is still on disk.
func LegacyServiceInstalled() bool {
	dir, err := systemdUserDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, legacySystemdUnit))
	return err == nil
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

// ServiceState is whether a link's service is running, and how to see its log.
func ServiceState(name string) (state, logs string) {
	unit := instanceUnit(name)
	out, _ := exec.Command("systemctl", "--user", "is-active", unit).Output() //nolint:gosec // G204: fixed systemctl query for the link's own unit
	state = strings.TrimSpace(string(out))
	if state == "" {
		state = "not installed"
	}
	return state, "journalctl --user -u " + unit + " -n 50 --no-pager"
}
