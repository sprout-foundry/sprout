//go:build darwin

package service

// darwin_plist_secrets.go — launchd log location and detection of plists
// written by older installs that inlined captured API keys and tokens into
// EnvironmentVariables. Only variable names are ever read out or printed.
import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/console"
)

// launchdLogPath is where launchd sends the daemon's stdout/stderr. Once the
// daemon is up it redirects its own output to the rotated logs in the state
// directory (setupDaemonLogging), so this file only collects startup output
// and runtime crash traces. launchd never rotates it.
func launchdLogPath(homeDir string) string {
	return filepath.Join(homeDir, "Library", "Logs", "sprout", "daemon.log")
}

// ensureLaunchdLogFile creates the log directory (0700) and file (0600) up
// front; launchd would otherwise create the file with its default umask.
func ensureLaunchdLogFile(logPath string) error {
	dir := filepath.Dir(logPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return fmt.Errorf("failed to restrict log directory permissions: %w", err)
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}
	if err := os.Chmod(logPath, 0600); err != nil {
		return fmt.Errorf("failed to restrict log file permissions: %w", err)
	}
	return nil
}

// inlinedSecretNames returns the names of secret-looking variables in the
// plist's EnvironmentVariables dict.
func inlinedSecretNames(plist []byte) ([]string, error) {
	names, err := plistEnvNames(plist)
	if err != nil {
		return nil, err
	}
	var secrets []string
	for _, name := range names {
		if IsSecretEnvName(name) {
			secrets = append(secrets, name)
		}
	}
	return secrets, nil
}

// inlinedSecretNamesInPlistFile is inlinedSecretNames for the plist at path;
// a missing or unparsable file reports none.
func inlinedSecretNamesInPlistFile(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	names, err := inlinedSecretNames(data)
	if err != nil {
		return nil
	}
	return names
}

// writeLaunchdPlist writes a freshly generated plist over pPath and returns
// the names of secret variables the plist it replaced had inlined, so an
// install over an older plist migrates it clean.
func writeLaunchdPlist(pPath string, cfg launchdPlistConfig) ([]string, error) {
	removed := inlinedSecretNamesInPlistFile(pPath)
	data, err := generateLaunchdPlist(cfg)
	if err != nil {
		return nil, err
	}
	if err := writePlistFile(pPath, data); err != nil {
		return nil, err
	}
	return removed, nil
}

// reportPlistMigration tells the user that an older plist's inlined secrets
// were dropped, and names any that service.env does not now provide so they
// can re-export them or store them with `sprout keys`.
func reportPlistMigration(removed []string) {
	if len(removed) == 0 {
		return
	}
	fmt.Printf("Removed %d secret variable(s) the previous launchd plist stored in plaintext: %s\n",
		len(removed), strings.Join(removed, ", "))
	envPath, _ := ServiceEnvPath()
	fmt.Printf("The daemon now reads secrets from %s (mode 0600).\n", envPath)

	current, err := LoadServiceEnvFile()
	if err != nil {
		return
	}
	var missing []string
	for _, name := range removed {
		if _, ok := current[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		console.GlyphWarning.Fprintf(os.Stdout, "Not in the installing shell's environment, so no longer passed to the daemon: %s", strings.Join(missing, ", "))
		fmt.Println("  Export them and re-run 'sprout service install', or store provider keys with 'sprout keys set <provider>'.")
	}
}

// warnIfPlistHasInlinedSecrets is the start/restart check: those commands
// reuse the plist on disk, so an old one keeps passing its plaintext secrets
// until `sprout service install` rewrites it.
func warnIfPlistHasInlinedSecrets() {
	pPath, err := plistPath()
	if err != nil {
		return
	}
	names := inlinedSecretNamesInPlistFile(pPath)
	if len(names) == 0 {
		return
	}
	console.GlyphWarning.Fprintf(os.Stdout, "%s stores %d secret variable(s) in plaintext (%s).", pPath, len(names), strings.Join(names, ", "))
	fmt.Println("  Run 'sprout service install' to rewrite it; secrets then load from service.env.")
}

// LogLocations reports where the launchd daemon writes logs, for `service
// status`.
func (m *launchdManager) LogLocations() []string {
	var locs []string
	if home, err := os.UserHomeDir(); err == nil {
		locs = append(locs, launchdLogPath(home)+" (startup and crash output)")
	}
	if dir, err := daemonLogDir(); err == nil {
		locs = append(locs, filepath.Join(dir, "daemon.stdout.log")+", daemon.stderr.log (rotated)")
	}
	return locs
}
