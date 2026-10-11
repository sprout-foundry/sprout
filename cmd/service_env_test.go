//go:build !js

package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadServiceEnvForDaemonReportsCountOnly(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("SPROUT_STATE_DIR", stateDir)
	const value = "synthetic-daemon-value-not-a-key"
	if err := os.WriteFile(filepath.Join(stateDir, "service.env"), []byte("EXAMPLE_DAEMON_API_KEY="+value+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EXAMPLE_DAEMON_API_KEY", "")
	os.Unsetenv("EXAMPLE_DAEMON_API_KEY")

	var buf bytes.Buffer
	t.Setenv("SPROUT_SERVICE", "")
	loadServiceEnvForDaemon(&buf)
	if _, set := os.LookupEnv("EXAMPLE_DAEMON_API_KEY"); set || buf.Len() != 0 {
		t.Fatal("service.env loaded outside service mode")
	}

	t.Setenv("SPROUT_SERVICE", "1")
	loadServiceEnvForDaemon(&buf)
	if got := os.Getenv("EXAMPLE_DAEMON_API_KEY"); got != value {
		t.Error("service.env not loaded in service mode")
	}
	out := buf.String()
	if strings.Contains(out, value) || strings.Contains(out, "EXAMPLE_DAEMON_API_KEY") {
		t.Errorf("startup output names or reveals a secret: %q", out)
	}
	if !strings.Contains(out, "loaded 1 variable(s)") {
		t.Errorf("startup output = %q", out)
	}
}
