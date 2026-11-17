package gpulink

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/credentials"
)

// A link saved to a repo's .sprout config moves to the machine's config,
// keys and all, and the repo's copy goes.
func TestAdoptLinkMovesARepoLink(t *testing.T) {
	machine, repo := t.TempDir(), t.TempDir()
	credentials.ResetStorageBackend()
	t.Cleanup(credentials.ResetStorageBackend)

	t.Setenv("SPROUT_CONFIG_DIR", repo)
	if err := (&State{LinkID: "link-1", Name: "box", Engine: "127.0.0.1:8006", Fleet: true}).Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveAPIKey("sfg_agent_key"); err != nil {
		t.Fatal(err)
	}
	if err := SaveEngineKey("engine-key"); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SPROUT_CONFIG_DIR", machine)
	credentials.ResetStorageBackend()
	moved, err := AdoptLink(repo)
	if err != nil || !moved {
		t.Fatalf("AdoptLink = %v, %v; want moved", moved, err)
	}
	st, err := LoadState()
	if err != nil || st.LinkID != "link-1" || !st.Fleet || st.Engine != "127.0.0.1:8006" {
		t.Fatalf("machine state = %+v, %v", st, err)
	}
	if key, err := LoadAPIKey(); err != nil || key != "sfg_agent_key" {
		t.Fatalf("machine key = %q, %v", key, err)
	}
	if got := LoadEngineKey(); got != "engine-key" {
		t.Fatalf("machine engine key = %q", got)
	}
	if _, err := os.Stat(filepath.Join(repo, "gpu", "gpu.json")); !os.IsNotExist(err) {
		t.Fatalf("the repo's link is still there: %v", err)
	}
	if os.Getenv("SPROUT_CONFIG_DIR") != machine {
		t.Fatal("the config dir was not put back")
	}

	// Once the machine has a link, nothing more is moved.
	if moved, err := AdoptLink(repo); err != nil || moved {
		t.Fatalf("second AdoptLink = %v, %v; want nothing moved", moved, err)
	}
}
