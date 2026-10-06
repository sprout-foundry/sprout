//go:build darwin

package runner

import (
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

const launchdLabel = "com.sprout.runner"

func servicePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist"), nil
}

// InstallService registers `sprout runner start` as a launchd agent that runs
// at login and restarts if it exits.
func InstallService() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	path, err := servicePath()
	if err != nil {
		return "", err
	}
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	logPath := filepath.Join(dir, "runner.log")
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>` + launchdLabel + `</string>
	<key>ProgramArguments</key>
	<array><string>` + html.EscapeString(exe) + `</string><string>runner</string><string>start</string></array>
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
	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		return "", err
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	_ = exec.Command("launchctl", "bootout", domain+"/"+launchdLabel).Run()                            //nolint:gosec // G204: launchctl with the runner's own plist
	if out, err := exec.Command("launchctl", "bootstrap", domain, path).CombinedOutput(); err != nil { //nolint:gosec // G204: launchctl with the runner's own plist
		return "", fmt.Errorf("launchctl bootstrap: %s", out)
	}
	return path, nil
}

// UninstallService stops and removes the launchd agent.
func UninstallService() error {
	path, err := servicePath()
	if err != nil {
		return err
	}
	_ = exec.Command("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/"+launchdLabel).Run() //nolint:gosec // G204: launchctl with the runner's own plist
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
