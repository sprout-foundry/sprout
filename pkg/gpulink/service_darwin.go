//go:build darwin

package gpulink

import (
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// launchdLabelPrefix begins each link's launchd label; the slot follows
// (com.sprout.gpu.<slot>), so several links share one machine. The label
// without a slot is the single agent older sprouts installed.
const launchdLabelPrefix = "com.sprout.gpu."

func launchdLabel(slot string) string { return launchdLabelPrefix + slot }

func legacyLaunchdLabel() string { return "com.sprout.gpu" }

func servicePath(slot string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel(slot)+".plist"), nil
}

// InstallService registers `sprout gpu start --name <name>` as a launchd
// agent that runs at login and restarts if it exits.
func InstallService(name string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	slot := Slot(name)
	if slot == "" {
		return "", fmt.Errorf("link name %q has no usable service label; pick another with --name", name)
	}
	path, err := servicePath(slot)
	if err != nil {
		return "", err
	}
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	logPath := filepath.Join(dir, "logs", slot+".log")
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>` + html.EscapeString(launchdLabel(slot)) + `</string>
	<key>ProgramArguments</key>
	<array><string>` + html.EscapeString(exe) + `</string><string>gpu</string><string>start</string><string>--name</string><string>` + html.EscapeString(slot) + `</string></array>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>ThrottleInterval</key><integer>30</integer>
	<key>StandardOutPath</key><string>` + html.EscapeString(logPath) + `</string>
	<key>StandardErrorPath</key><string>` + html.EscapeString(logPath) + `</string>
</dict>
</plist>
`
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		return "", err
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	_ = exec.Command("launchctl", "bootout", domain+"/"+launchdLabel(slot)).Run()                      //nolint:gosec // G204: launchctl with the link's own plist
	if out, err := exec.Command("launchctl", "bootstrap", domain, path).CombinedOutput(); err != nil { //nolint:gosec // G204: launchctl with the link's own plist
		return "", fmt.Errorf("launchctl bootstrap: %s", out)
	}
	uninstallLegacyService()
	return path, nil
}

// UninstallService stops and removes one link's launchd agent. An empty slot
// removes every link's agent and the legacy one.
func UninstallService(slot string) error {
	if slot != "" {
		path, err := servicePath(slot)
		if err != nil {
			return err
		}
		_ = exec.Command("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/"+launchdLabel(slot)).Run() //nolint:gosec // G204: launchctl with the link's own plist
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	dir, err := systemdAgentsDir()
	if err == nil {
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, e := range entries {
				if name := strings.TrimSuffix(e.Name(), ".plist"); strings.HasPrefix(name, launchdLabelPrefix) {
					_ = UninstallService(Slot(strings.TrimPrefix(name, launchdLabelPrefix)))
				}
			}
		}
	}
	uninstallLegacyService()
	return nil
}

// uninstallLegacyService stops and removes the single launchd agent an older
// sprout installed; per-link agents have replaced it.
func uninstallLegacyService() {
	_ = exec.Command("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/"+legacyLaunchdLabel()).Run() //nolint:gosec // G204: launchctl with the link's own plist
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	_ = os.Remove(filepath.Join(home, "Library", "LaunchAgents", legacyLaunchdLabel()+".plist"))
}

// LegacyServiceInstalled reports whether the single launchd agent an older
// sprout installed is still on disk.
func LegacyServiceInstalled() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(home, "Library", "LaunchAgents", legacyLaunchdLabel()+".plist"))
	return err == nil
}

func systemdAgentsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents"), nil
}

// ServiceNotice is what to know about the service on this machine.
func ServiceNotice() string {
	return "The agent serves while you are logged in and the Mac is awake; keep it from sleeping while you want it to serve (System Settings → Energy, or `caffeinate -s`)."
}

// ServiceState is whether a link's service is running, and how to see its log.
func ServiceState(slot string) (state, logs string) {
	out, err := exec.Command("launchctl", "print", "gui/"+strconv.Itoa(os.Getuid())+"/"+launchdLabel(slot)).Output() //nolint:gosec // G204: launchctl query for the link's own agent
	state = "not installed"
	if err == nil {
		state = "loaded"
		for _, line := range strings.Split(string(out), "\n") {
			if l := strings.TrimSpace(line); strings.HasPrefix(l, "state = ") {
				state = strings.TrimPrefix(l, "state = ")
				break
			}
		}
	}
	logs = "the log file in the GPU links' config directory"
	if dir, err := Dir(); err == nil {
		logs = "tail -n 50 " + filepath.Join(dir, "logs", slot+".log")
	}
	return state, logs
}
