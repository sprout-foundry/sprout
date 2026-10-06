//go:build linux

package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func indexOfPair(args []string, flag, value string) int {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return i
		}
	}
	return -1
}

func TestBwrapArgs(t *testing.T) {
	work, temp, home := t.TempDir(), t.TempDir(), t.TempDir()
	secretDir := filepath.Join(home, ".ssh")
	if err := os.Mkdir(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	secretFile := filepath.Join(home, ".netrc")
	if err := os.WriteFile(secretFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	args, err := bwrapArgs(Policy{
		WorkDir:  work,
		TempDir:  temp,
		Writable: []string{filepath.Join(home, "missing-cache")},
		DenyRead: DefaultDenyRead(home),
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"--ro-bind / /", "--dev /dev", "--proc /proc", "--unshare-pid", "--die-with-parent", "--new-session", "--unshare-net"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %s", want, joined)
		}
	}
	bindWork := indexOfPair(args, "--bind", work)
	if bindWork < 0 || indexOfPair(args, "--bind", temp) < 0 {
		t.Fatalf("WorkDir/TempDir not bound writable: %s", joined)
	}
	if slices.Contains(args, filepath.Join(home, "missing-cache")) {
		t.Errorf("a missing writable path must be skipped: %s", joined)
	}
	maskDir := indexOfPair(args, "--tmpfs", secretDir)
	if maskDir < bindWork {
		t.Errorf("deny-read dir must be masked after the writable binds: %s", joined)
	}
	if !strings.Contains(joined, "--ro-bind /dev/null "+secretFile) {
		t.Errorf("deny-read file must be masked with /dev/null: %s", joined)
	}
	if slices.Contains(args, filepath.Join(home, ".aws")) {
		t.Errorf("a deny-read path that does not exist must not be mounted: %s", joined)
	}
}

func TestBwrapArgsNetworkAllowed(t *testing.T) {
	args, err := bwrapArgs(Policy{WorkDir: t.TempDir(), AllowNetwork: true})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(args, "--unshare-net") {
		t.Fatalf("AllowNetwork must not unshare the network: %v", args)
	}
}

func TestBwrapArgsRequiresExistingWorkDir(t *testing.T) {
	if _, err := bwrapArgs(Policy{}); err == nil {
		t.Fatal("expected an error for an empty WorkDir")
	}
	if _, err := bwrapArgs(Policy{WorkDir: filepath.Join(t.TempDir(), "nope")}); err == nil {
		t.Fatal("expected an error for a missing WorkDir")
	}
}

func TestDetectLinux(t *testing.T) {
	c := Detect()
	if c.Name != "bwrap" {
		t.Fatalf("Name = %q, want bwrap", c.Name)
	}
	if !c.Available && c.Detail == "" {
		t.Fatal("unavailable capability must explain why")
	}
}
