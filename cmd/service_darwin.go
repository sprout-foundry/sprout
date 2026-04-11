//go:build darwin

package cmd

import (
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	launchdLabel    = "com.ledit.daemon"
	launchdPlistDir = "Library/LaunchAgents"
)

// launchdManager implements serviceManager for macOS launchd.
type launchdManager struct{}

func init() {
	newServiceManager = func() serviceManager { return &launchdManager{} }
}

// ── plist XML types ──────────────────────────────────────────────────

type plistValue struct {
	XMLName xml.Name
	Key     string      `xml:",chardata"`
	Items   []string    `xml:"string,omitempty"`
	Dict    *plistDict  `xml:"dict,omitempty"`
}

type plistDict struct {
	Entries []plistValue `xml:",any"`
}

type launchdPlist struct {
	XMLName xml.Name   `xml:"plist"`
	Version string     `xml:"version,attr"`
	Dict    *plistDict `xml:"dict"`
}

// ── plist generation ────────────────────────────────────────────────

func generateLaunchdPlist(binaryPath, homeDir string) ([]byte, error) {
	stdoutPath := filepath.Join(homeDir, ".ledit/logs/daemon.stdout.log")
	stderrPath := filepath.Join(homeDir, ".ledit/logs/daemon.stderr.log")

	p := launchdPlist{
		Version: "1.0",
		Dict: &plistDict{Entries: []plistValue{
			{XMLName: xml.Name{Local: "key"}, Key: "Label"},
			{XMLName: xml.Name{Local: "string"}, Key: launchdLabel},
			{XMLName: xml.Name{Local: "key"}, Key: "ProgramArguments"},
			{XMLName: xml.Name{Local: "array"}, Items: []string{binaryPath, "agent", "-d", "--no-connection-check"}},
			{XMLName: xml.Name{Local: "key"}, Key: "WorkingDirectory"},
			{XMLName: xml.Name{Local: "string"}, Key: homeDir},
			{XMLName: xml.Name{Local: "key"}, Key: "RunAtLoad"},
			{XMLName: xml.Name{Local: "true"}},
			{XMLName: xml.Name{Local: "key"}, Key: "KeepAlive"},
			{XMLName: xml.Name{Local: "true"}},
			{XMLName: xml.Name{Local: "key"}, Key: "ThrottleInterval"},
			{XMLName: xml.Name{Local: "integer"}, Key: "30"},
			{XMLName: xml.Name{Local: "key"}, Key: "StandardOutPath"},
			{XMLName: xml.Name{Local: "string"}, Key: stdoutPath},
			{XMLName: xml.Name{Local: "key"}, Key: "StandardErrorPath"},
			{XMLName: xml.Name{Local: "string"}, Key: stderrPath},
			{XMLName: xml.Name{Local: "key"}, Key: "EnvironmentVariables"},
			{
				XMLName: xml.Name{Local: "dict"},
				Dict: &plistDict{Entries: []plistValue{
					{XMLName: xml.Name{Local: "key"}, Key: "LEDIT_SERVICE"},
					{XMLName: xml.Name{Local: "string"}, Key: "1"},
					{XMLName: xml.Name{Local: "key"}, Key: "HOME"},
					{XMLName: xml.Name{Local: "string"}, Key: homeDir},
				}},
			},
		}},
	}

	header := []byte(xml.Header)
	body, err := xml.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal plist: %w", err)
	}
	return append(header, body...), nil
}

// ── helpers ──────────────────────────────────────────────────────────

func launchdDomain() string {
	return fmt.Sprintf("gui/%d", os.Getuid())
}

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to determine home directory: %w", err)
	}
	return filepath.Join(home, launchdPlistDir, launchdLabel+".plist"), nil
}

func runLaunchctl(args ...string) (string, error) {
	out, err := exec.Command("launchctl", args...).CombinedOutput()
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return trimmed, fmt.Errorf("launchctl %s: %s", strings.Join(args, " "), trimmed)
	}
	return trimmed, nil
}

// ── serviceManager implementation ────────────────────────────────────

func (m *launchdManager) Install() error {
	binaryPath, err := getBinaryPath()
	if err != nil {
		return err
	}
	if filepath.Base(binaryPath) != "ledit" {
		return fmt.Errorf("unexpected binary name %q — service requires the ledit binary", filepath.Base(binaryPath))
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to determine home directory: %w", err)
	}

	agentsDir := filepath.Join(homeDir, launchdPlistDir)
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		return fmt.Errorf("failed to create LaunchAgents directory: %w", err)
	}
	logDir := filepath.Join(homeDir, ".ledit/logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	data, err := generateLaunchdPlist(binaryPath, homeDir)
	if err != nil {
		return err
	}

	pPath, err := plistPath()
	if err != nil {
		return fmt.Errorf("failed to determine plist path: %w", err)
	}
	tmpPath := pPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write plist: %w", err)
	}
	if err := os.Rename(tmpPath, pPath); err != nil {
		return fmt.Errorf("failed to rename plist: %w", err)
	}

	domain := launchdDomain()
	servicePath := domain + "/" + launchdLabel

	_, _ = runLaunchctl("bootout", servicePath)
	if _, err := runLaunchctl("bootstrap", domain, pPath); err != nil {
		fmt.Printf("Warning: launchctl bootstrap: %s\n", err)
	}

	fmt.Printf("Installed launchd agent: %s\n", pPath)
	return nil
}

func (m *launchdManager) Uninstall() error {
	domain := launchdDomain()
	servicePath := domain + "/" + launchdLabel

	_, _ = runLaunchctl("bootout", servicePath)

	pPath, err := plistPath()
	if err != nil {
		return fmt.Errorf("failed to determine plist path: %w", err)
	}
	if err := os.Remove(pPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove plist: %w", err)
	}

	fmt.Printf("Uninstalled launchd agent: %s\n", pPath)
	return nil
}

func (m *launchdManager) Start() error {
	servicePath := launchdDomain() + "/" + launchdLabel

	// Check if the service is currently loaded in launchd.
	if _, err := runLaunchctl("print", servicePath); err != nil {
		// Service not loaded — bootstrap it from the plist on disk.
		pPath, pathErr := plistPath()
		if pathErr != nil {
			return fmt.Errorf("service not loaded and cannot find plist: %w", pathErr)
		}
		if _, bsErr := runLaunchctl("bootstrap", launchdDomain(), pPath); bsErr != nil {
			return fmt.Errorf("failed to load service: %w", bsErr)
		}
		fmt.Println("Daemon started successfully.")
		return nil
	}

	// Service is loaded — kickstart it.
	if _, err := runLaunchctl("kickstart", "-k", servicePath); err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}
	fmt.Println("Daemon started successfully.")
	return nil
}

func (m *launchdManager) Stop() error {
	servicePath := launchdDomain() + "/" + launchdLabel
	_, err := runLaunchctl("bootout", servicePath)
	if err != nil {
		// ESRCH (exit code 3) means the service isn't loaded — already stopped.
		if isESRCH(err) {
			fmt.Println("Daemon not running.")
			return nil
		}
		return fmt.Errorf("launchctl bootout failed: %w", err)
	}
	fmt.Println("Daemon stopped successfully.")
	return nil
}

// isESRCH returns true if the error is from a process that doesn't exist
// (ESRCH = exit code 3, used by launchctl when a service isn't loaded).
func isESRCH(err error) bool {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 3 {
		return true
	}
	// String-based fallback for robustness across macOS versions.
	return strings.Contains(err.Error(), "No such process") ||
		strings.Contains(err.Error(), "Could not find service")
}

func (m *launchdManager) Status() (bool, error) {
	servicePath := launchdDomain() + "/" + launchdLabel
	out, err := runLaunchctl("print", servicePath)
	if err != nil {
		return false, nil
	}
	return strings.Contains(out, "state = running"), nil
}
