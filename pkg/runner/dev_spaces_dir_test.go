package runner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDevSpacesDirKeepsAnEarlierWorkspacesDir(t *testing.T) {
	state := t.TempDir()
	t.Setenv("SPROUT_STATE_DIR", state)
	if got, _ := DevSpacesDir(); got != filepath.Join(state, "runner", "dev-spaces") {
		t.Fatalf("fresh install = %s", got)
	}
	earlier := filepath.Join(state, "runner", "workspaces")
	if err := os.MkdirAll(earlier, 0o700); err != nil {
		t.Fatal(err)
	}
	if got, _ := DevSpacesDir(); got != earlier {
		t.Fatalf("with runner/workspaces = %s, want it kept", got)
	}
}
