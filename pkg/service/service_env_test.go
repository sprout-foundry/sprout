//go:build !js

package service

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const syntheticEnvValue = "synthetic-env-value-not-a-key"

// captureStdio runs fn with os.Stdout and os.Stderr redirected and returns
// what was written to either.
func captureStdio(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	func() {
		defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
		fn()
	}()
	_ = w.Close()
	return <-done
}

func TestGenerateServiceEnvFileIsOwnerOnly(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	t.Setenv("SPROUT_STATE_DIR", stateDir)
	t.Setenv("EXAMPLE_SVC_API_KEY", syntheticEnvValue)

	out := captureStdio(t, func() {
		if err := GenerateServiceEnvFile(); err != nil {
			t.Fatalf("GenerateServiceEnvFile: %v", err)
		}
	})
	if strings.Contains(out, syntheticEnvValue) {
		t.Fatal("GenerateServiceEnvFile printed a secret value")
	}

	envPath, err := ServiceEnvPath()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("service.env mode = %04o, want 0600", got)
	}
	vars, err := LoadServiceEnvFile()
	if err != nil {
		t.Fatal(err)
	}
	if vars["EXAMPLE_SVC_API_KEY"] != syntheticEnvValue {
		t.Error("service.env did not capture EXAMPLE_SVC_API_KEY")
	}
}

func TestApplyServiceEnvFileKeepsExistingAndPrintsNothing(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("SPROUT_STATE_DIR", stateDir)
	content := "# comment\nEXAMPLE_NEW_API_KEY=" + syntheticEnvValue + "\nEXAMPLE_SET_TOKEN=from-file\n"
	if err := os.WriteFile(filepath.Join(stateDir, "service.env"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EXAMPLE_SET_TOKEN", "from-env")
	// Register EXAMPLE_NEW_API_KEY for restore, then make it absent.
	t.Setenv("EXAMPLE_NEW_API_KEY", "")
	os.Unsetenv("EXAMPLE_NEW_API_KEY")

	var applied []string
	out := captureStdio(t, func() {
		var err error
		applied, err = ApplyServiceEnvFile()
		if err != nil {
			t.Errorf("ApplyServiceEnvFile: %v", err)
		}
	})
	if out != "" {
		t.Errorf("ApplyServiceEnvFile wrote output: %q", out)
	}
	if want := []string{"EXAMPLE_NEW_API_KEY"}; !reflect.DeepEqual(applied, want) {
		t.Errorf("applied = %v, want %v", applied, want)
	}
	if got := os.Getenv("EXAMPLE_NEW_API_KEY"); got != syntheticEnvValue {
		t.Error("EXAMPLE_NEW_API_KEY not loaded from service.env")
	}
	if got := os.Getenv("EXAMPLE_SET_TOKEN"); got != "from-env" {
		t.Errorf("existing EXAMPLE_SET_TOKEN overridden: %q", got)
	}
}

func TestApplyServiceEnvFileMissingFileIsNoop(t *testing.T) {
	t.Setenv("SPROUT_STATE_DIR", t.TempDir())
	applied, err := ApplyServiceEnvFile()
	if err != nil || len(applied) != 0 {
		t.Errorf("missing service.env: applied=%v err=%v", applied, err)
	}
}

func TestIsSecretEnvName(t *testing.T) {
	for name, want := range map[string]bool{
		"OPENAI_API_KEY":     true,
		"GITHUB_TOKEN":       true,
		"aws_secret":         true,
		"SPROUT_PROVIDER":    false,
		"PATH":               false,
		"SPROUT_DAEMON_ROOT": false,
	} {
		if got := IsSecretEnvName(name); got != want {
			t.Errorf("IsSecretEnvName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestPlistEnvNames(t *testing.T) {
	plist := []byte(`<?xml version="1.0"?>
<plist version="1.0"><dict>
<key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
<key>EnvironmentVariables</key><dict>
<key>HOME</key><string>/h</string>
<key>A_TOKEN</key><string>x</string>
</dict>
<key>Label</key><string>l</string>
</dict></plist>`)
	names, err := plistEnvNames(plist)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"HOME", "A_TOKEN"}; !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}

	_, err = plistEnvNames([]byte(`<plist><dict><key>EnvironmentVariables</key><dict><key>X_TOKEN</key><string>` + syntheticEnvValue + `&bogus;</string></dict></dict></plist>`))
	if err == nil {
		t.Fatal("expected a parse error")
	}
	if strings.Contains(err.Error(), "synthetic") || strings.Contains(err.Error(), "bogus") {
		t.Errorf("parse error quotes plist content: %v", err)
	}
}

func TestResolveDaemonRoot(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "projects")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(home, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if got, err := resolveDaemonRoot("", home); err != nil || got != home {
		t.Errorf("empty --root = %q, %v; want home", got, err)
	}
	if got, err := resolveDaemonRoot(root, home); err != nil || got != root {
		t.Errorf("--root %s = %q, %v", root, got, err)
	}
	if got, err := resolveDaemonRoot("~/projects", home); err != nil || got != root {
		t.Errorf("--root ~/projects = %q, %v; want %s", got, err, root)
	}
	if _, err := resolveDaemonRoot(filepath.Join(home, "missing"), home); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("missing --root: err = %v", err)
	}
	if _, err := resolveDaemonRoot(file, home); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("file --root: err = %v", err)
	}
}

func TestServiceInstallHasRootFlag(t *testing.T) {
	if serviceInstallCmd.Flags().Lookup("root") == nil {
		t.Fatal("service install has no --root flag")
	}
}
