//go:build !js

package webui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runForwardScript runs the launch script's read and apply steps through a
// real shell, the way the remote host does, and returns what they export.
func runForwardScript(t *testing.T, fwd *sshForwardedProvider, report string) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not installed")
	}
	home := t.TempDir()
	script := strings.Join(append(append(append([]string{
		"set -e",
		"FORWARD_PROVIDER=1",
	}, sshForwardReadScript...), sshForwardApplyScript...), report), "\n")
	if strings.Contains(script, fwd.Env["TEST_PROVIDER_KEY"]) && fwd.Env["TEST_PROVIDER_KEY"] != "" {
		t.Fatal("the key must not be part of the script (it would show on the remote command line)")
	}
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH")}
	cmd.Stdin = strings.NewReader(fwd.stdinPayload())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script failed: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out)), home
}

func TestForwardedProviderReachesTheRemoteEnvironment(t *testing.T) {
	fwd := &sshForwardedProvider{
		Provider: "aprice",
		Model:    "qwen3.8-27b",
		Env: map[string]string{
			"SPROUT_PROVIDER":   "aprice",
			"SPROUT_MODEL":      "qwen3.8-27b",
			"TEST_PROVIDER_KEY": `k3y with 'quotes' $and "spaces"`,
		},
		Definition: []byte(`{"name":"aprice","endpoint":"https://example.test/v1","env_var":"TEST_PROVIDER_KEY"}`),
	}

	out, home := runForwardScript(t, fwd, `printf '%s|%s|%s|%s' "$SPROUT_PROVIDER" "$SPROUT_MODEL" "$TEST_PROVIDER_KEY" "$FORWARD_FP"`)

	parts := strings.Split(out, "|")
	if len(parts) != 4 {
		t.Fatalf("unexpected output %q", out)
	}
	if parts[0] != "aprice" || parts[1] != "qwen3.8-27b" {
		t.Errorf("provider/model = %q/%q", parts[0], parts[1])
	}
	if parts[2] != fwd.Env["TEST_PROVIDER_KEY"] {
		t.Errorf("key = %q, want it byte for byte", parts[2])
	}
	if parts[3] != fwd.fingerprint() {
		t.Errorf("fingerprint = %q, want %q", parts[3], fwd.fingerprint())
	}
	def, err := os.ReadFile(filepath.Join(home, ".config", "sprout", "providers", "aprice.json"))
	if err != nil {
		t.Fatalf("custom provider definition not written: %v", err)
	}
	if string(def) != string(fwd.Definition) {
		t.Errorf("definition = %s", def)
	}
}

func TestForwardedProviderWinsOverTheRemoteShell(t *testing.T) {
	fwd := &sshForwardedProvider{
		Provider: "openai",
		Env:      map[string]string{"SPROUT_PROVIDER": "openai"},
	}
	// The apply step runs after the rc files, so a provider the remote's
	// shell exported is replaced.
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not installed")
	}
	script := strings.Join(append(append(append([]string{
		"set -e",
		"FORWARD_PROVIDER=1",
	}, sshForwardReadScript...), "export SPROUT_PROVIDER=remote-own"), append(sshForwardApplyScript, `printf '%s' "$SPROUT_PROVIDER"`)...), "\n")
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH")}
	cmd.Stdin = strings.NewReader(fwd.stdinPayload())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script failed: %v\n%s", err, out)
	}
	if string(out) != "openai" {
		t.Errorf("SPROUT_PROVIDER = %q, want this machine's", out)
	}
}

func TestForwardedProviderFingerprintTracksSettings(t *testing.T) {
	a := &sshForwardedProvider{Env: map[string]string{"SPROUT_MODEL": "m1", "K": "secret"}}
	b := &sshForwardedProvider{Env: map[string]string{"SPROUT_MODEL": "m2", "K": "secret"}}
	if a.fingerprint() == b.fingerprint() {
		t.Error("a model change must change the fingerprint (the daemon restarts on it)")
	}
	if strings.Contains(a.fingerprint(), "secret") {
		t.Error("the fingerprint must not reveal the key")
	}
}

func TestForwardedProviderForRejectsUnsafeNames(t *testing.T) {
	fwd, err := forwardedProviderFor("bad name; rm -rf", "m")
	if err != nil || fwd != nil {
		t.Errorf("unsafe provider name should forward nothing, got %+v, %v", fwd, err)
	}
}
