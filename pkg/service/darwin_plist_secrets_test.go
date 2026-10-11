//go:build darwin

package service

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const syntheticSecretValue = "synthetic-test-value-not-a-key"

// isolateServiceState points service.env at a temp state dir and returns its
// path. Nothing here reads or writes the real user's files.
func isolateServiceState(t *testing.T) string {
	t.Helper()
	stateDir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SPROUT_STATE_DIR", stateDir)
	return stateDir
}

func testPlistConfig(home string) launchdPlistConfig {
	return launchdPlistConfig{
		BinaryPath: "/usr/local/bin/sprout",
		HomeDir:    home,
		LogPath:    launchdLogPath(home),
		Path:       "/usr/bin:/bin",
	}
}

// legacyPlist reproduces the shape older installs wrote: captured secrets
// inlined into EnvironmentVariables next to the fixed settings.
func legacyPlist(home string) []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.sprout.daemon</string>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>EnvironmentVariables</key>
	<dict>
		<key>SPROUT_SERVICE</key>
		<string>1</string>
		<key>HOME</key>
		<string>` + home + `</string>
		<key>EXAMPLE_API_KEY</key>
		<string>` + syntheticSecretValue + `</string>
		<key>EXAMPLE_TOKEN</key>
		<string>` + syntheticSecretValue + `</string>
		<key>SPROUT_PROVIDER</key>
		<string>example</string>
	</dict>
</dict>
</plist>`)
}

func TestGenerateLaunchdPlist_NoSecretsEvenWhenServiceEnvHasThem(t *testing.T) {
	stateDir := isolateServiceState(t)
	envFile := "EXAMPLE_API_KEY=" + syntheticSecretValue + "\nEXAMPLE_TOKEN=" + syntheticSecretValue + "\n"
	if err := os.WriteFile(filepath.Join(stateDir, "service.env"), []byte(envFile), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OTHER_API_KEY", syntheticSecretValue)

	home := t.TempDir()
	data, err := generateLaunchdPlist(testPlistConfig(home))
	if err != nil {
		t.Fatalf("generateLaunchdPlist: %v", err)
	}
	if bytes.Contains(data, []byte(syntheticSecretValue)) {
		t.Fatal("plist contains a secret value")
	}
	names, err := plistEnvNames(data)
	if err != nil {
		t.Fatalf("plistEnvNames: %v", err)
	}
	want := []string{"SPROUT_SERVICE", "HOME", "SPROUT_DAEMON_ROOT", "PATH"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("plist env names = %v, want %v", names, want)
	}
	if secrets, _ := inlinedSecretNames(data); len(secrets) != 0 {
		t.Errorf("plist has inlined secrets: %v", secrets)
	}
}

func TestGenerateLaunchdPlist_LogPaths(t *testing.T) {
	home := t.TempDir()
	data, err := generateLaunchdPlist(testPlistConfig(home))
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(home, "Library", "Logs", "sprout", "daemon.log")
	for _, key := range []string{"StandardOutPath", "StandardErrorPath"} {
		want := "<key>" + key + "</key>\n\t<string>" + logPath + "</string>"
		if !strings.Contains(string(data), want) {
			t.Errorf("plist missing %s = %s\n%s", key, logPath, data)
		}
	}
}

func TestGenerateLaunchdPlist_DaemonRoot(t *testing.T) {
	home := t.TempDir()
	cfg := testPlistConfig(home)

	data, err := generateLaunchdPlist(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "<key>SPROUT_DAEMON_ROOT</key>\n\t\t<string>"+home+"</string>") {
		t.Errorf("default SPROUT_DAEMON_ROOT should be home\n%s", data)
	}

	cfg.DaemonRoot = filepath.Join(home, "projects")
	data, err = generateLaunchdPlist(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "<key>SPROUT_DAEMON_ROOT</key>\n\t\t<string>"+cfg.DaemonRoot+"</string>") {
		t.Errorf("SPROUT_DAEMON_ROOT should be the --root value\n%s", data)
	}
	if !strings.Contains(string(data), "<key>HOME</key>\n\t\t<string>"+home+"</string>") {
		t.Errorf("--root must not change HOME\n%s", data)
	}
}

func TestEnsureLaunchdLogFilePermissions(t *testing.T) {
	logPath := launchdLogPath(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ensureLaunchdLogFile(logPath); err != nil {
		t.Fatalf("ensureLaunchdLogFile: %v", err)
	}
	dirInfo, err := os.Stat(filepath.Dir(logPath))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Errorf("log dir mode = %04o, want 0700", got)
	}
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("log file mode = %04o, want 0600", got)
	}
	if content, _ := os.ReadFile(logPath); string(content) != "old\n" {
		t.Errorf("existing log content not preserved: %q", content)
	}
}

func TestWriteLaunchdPlist_MigratesLegacyInlinedSecrets(t *testing.T) {
	isolateServiceState(t)
	home := t.TempDir()
	pPath := filepath.Join(t.TempDir(), "com.sprout.daemon.plist")
	if err := os.WriteFile(pPath, legacyPlist(home), 0o644); err != nil {
		t.Fatal(err)
	}

	removed, err := writeLaunchdPlist(pPath, testPlistConfig(home))
	if err != nil {
		t.Fatalf("writeLaunchdPlist: %v", err)
	}
	if want := []string{"EXAMPLE_API_KEY", "EXAMPLE_TOKEN"}; !reflect.DeepEqual(removed, want) {
		t.Errorf("removed = %v, want %v", removed, want)
	}

	data, err := os.ReadFile(pPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(syntheticSecretValue)) {
		t.Fatal("rewritten plist still contains a secret value")
	}
	if names := inlinedSecretNamesInPlistFile(pPath); len(names) != 0 {
		t.Errorf("rewritten plist still has secrets: %v", names)
	}
	info, err := os.Stat(pPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != plistFileMode {
		t.Errorf("plist mode = %04o, want %04o", got, plistFileMode)
	}

	again, err := writeLaunchdPlist(pPath, testPlistConfig(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Errorf("clean plist reported removed secrets: %v", again)
	}
}

func TestDiagnoseInlinedSecrets(t *testing.T) {
	home := t.TempDir()

	var buf bytes.Buffer
	diagnoseInlinedSecrets(&buf, legacyPlist(home))
	out := buf.String()
	if strings.Contains(out, syntheticSecretValue) {
		t.Fatal("diagnose printed a secret value")
	}
	for _, want := range []string{"EXAMPLE_API_KEY", "EXAMPLE_TOKEN", "sprout service install"} {
		if !strings.Contains(out, want) {
			t.Errorf("diagnose output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "SPROUT_PROVIDER") {
		t.Errorf("non-secret SPROUT_PROVIDER flagged as a secret:\n%s", out)
	}

	clean, err := generateLaunchdPlist(testPlistConfig(home))
	if err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	diagnoseInlinedSecrets(&buf, clean)
	if !strings.Contains(buf.String(), "No secrets stored in the plist") {
		t.Errorf("clean plist not reported clean:\n%s", buf.String())
	}
}

func TestLaunchdLogLocationsNameBothLogs(t *testing.T) {
	isolateServiceState(t)
	locs := (&launchdManager{}).LogLocations()
	joined := strings.Join(locs, "\n")
	if !strings.Contains(joined, filepath.Join("Library", "Logs", "sprout", "daemon.log")) {
		t.Errorf("log locations missing launchd log: %v", locs)
	}
	if !strings.Contains(joined, "daemon.stdout.log") {
		t.Errorf("log locations missing rotated daemon log: %v", locs)
	}
}
