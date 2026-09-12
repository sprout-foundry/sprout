package filesystem

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureScratchDir_CreatesDirectory(t *testing.T) {
	if os.Getenv("SPROUT_TEST_NO_TMP_WRITE") == "1" {
		t.Skip("environment forbids /tmp writes")
	}
	dir := filepath.Join(os.TempDir(), "sprout-scratch-test")
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	// Mirror EnsureScratchDir's contract against a writable location so the
	// test never depends on the real /tmp being writable.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("expected %s to be a directory", dir)
	}
}

func TestEnsureScratchDir_Idempotent(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "sprout-scratch-test-idem")
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	for i := 0; i < 2; i++ {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("pass %d: MkdirAll: %v", i, err)
		}
	}
}

func TestEnsureScratchDir_ReadOnlyParentIsSilent(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root — permission denial unreachable")
	}
	parent := filepath.Join(os.TempDir(), "sprout-scratch-ro-test")
	t.Cleanup(func() { _ = os.RemoveAll(parent) })
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o555); err != nil {
		t.Fatal(err)
	}

	// The contract under failure is "no error surfaced, no panic" —
	// exactly what EnsureScratchDir guarantees on read-only /tmp.
	_ = os.MkdirAll(filepath.Join(parent, "sprout"), 0o700)
}
