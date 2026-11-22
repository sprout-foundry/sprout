//go:build darwin

package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/runner/sandbox"
)

// A runner whose config root moved still hides its credential files from
// native dev spaces: the deny list follows the root this process uses.
func TestHostCredentialDirsFollowAMovedConfigRoot(t *testing.T) {
	if c := sandbox.Detect(); !c.Available {
		t.Skipf("seatbelt unavailable: %s", c.Detail)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SPROUT_CONFIG_DIR", filepath.Join(root, "custom-config"))
	dirs := hostCredentialDirs()
	want := filepath.Join(root, "custom-config", "credentials")
	if len(dirs) != 1 || dirs[0] != want {
		t.Fatalf("hostCredentialDirs = %v, want [%s]", dirs, want)
	}
	if err := os.MkdirAll(want, 0o700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(want, "api_keys.json")
	if err := os.WriteFile(secret, []byte("synthetic-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	work, _ := filepath.EvalSymlinks(t.TempDir())
	tmp, _ := filepath.EvalSymlinks(t.TempDir())
	cmd, err := sandbox.CommandContext(context.Background(), sandbox.Policy{WorkDir: work, TempDir: tmp, DenyRead: hostCredentialDirs()}, "/bin/cat", secret)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("a native workspace read the runner's credential file: %q", out)
	}
}
