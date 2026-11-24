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
	t.Setenv("SPROUT_CONFIG", repo)
	credentials.ResetStorageBackend()
	if err := (&State{LinkID: "link-1", Name: "box", Engine: "127.0.0.1:8006", Fleet: true}).Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveAPIKeyFor("box", "sfg_agent_key"); err != nil {
		t.Fatal(err)
	}
	if err := SaveEngineKeyFor("box", "engine-key"); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SPROUT_CONFIG_DIR", machine)
	t.Setenv("SPROUT_CONFIG", machine)
	credentials.ResetStorageBackend()
	moved, err := AdoptLink(repo)
	if err != nil || !moved {
		t.Fatalf("AdoptLink = %v, %v; want moved", moved, err)
	}
	links, err := LoadLinks()
	if err != nil || len(links) != 1 || links[0].LinkID != "link-1" || !links[0].Fleet || links[0].Engine != "127.0.0.1:8006" {
		t.Fatalf("machine links = %+v, %v", links, err)
	}
	if key, err := LoadAPIKeyFor("box"); err != nil || key != "sfg_agent_key" {
		t.Fatalf("machine key = %q, %v", key, err)
	}
	if got := LoadEngineKeyFor("box"); got != "engine-key" {
		t.Fatalf("machine engine key = %q", got)
	}
	if _, err := os.Stat(filepath.Join(repo, "gpu")); !os.IsNotExist(err) {
		t.Fatalf("the repo's link is still there: %v", err)
	}
	if os.Getenv("SPROUT_CONFIG_DIR") != machine {
		t.Fatal("the config dir was not put back")
	}

	// Once the machine has the link, nothing more is moved.
	if moved, err := AdoptLink(repo); err != nil || moved {
		t.Fatalf("second AdoptLink = %v, %v; want nothing moved", moved, err)
	}
}

// A legacy single-file repo link (older sprout) moves too, under its keys'
// old names.
func TestAdoptLinkMovesALegacyRepoLink(t *testing.T) {
	machine, repo := t.TempDir(), t.TempDir()
	credentials.ResetStorageBackend()
	t.Cleanup(credentials.ResetStorageBackend)

	gpuDir := filepath.Join(repo, "gpu")
	if err := os.MkdirAll(gpuDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"platform_url": "https://platform.example", "link_id": "link-2", "name": "box", "engine": "127.0.0.1:8006"}`)
	if err := os.WriteFile(filepath.Join(gpuDir, "gpu.json"), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SPROUT_CONFIG_DIR", repo)
	t.Setenv("SPROUT_CONFIG", repo)
	credentials.ResetStorageBackend()
	if _, err := saveSecret(legacyAgentKey, "sfg_legacy_key"); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SPROUT_CONFIG_DIR", machine)
	t.Setenv("SPROUT_CONFIG", machine)
	credentials.ResetStorageBackend()
	moved, err := AdoptLink(repo)
	if err != nil || !moved {
		t.Fatalf("AdoptLink = %v, %v; want moved", moved, err)
	}
	if key, err := LoadAPIKeyFor("box"); err != nil || key != "sfg_legacy_key" {
		t.Fatalf("machine key = %q, %v", key, err)
	}
}

// A repo link whose slot the machine already has stays put: it is the same
// link, and moving it would only duplicate it. AdoptLink removes only what
// it moved, so the skipped link's file survives in the repo config.
func TestAdoptLinkSkipsAnExistingSlot(t *testing.T) {
	machine, repo := t.TempDir(), t.TempDir()
	credentials.ResetStorageBackend()
	t.Cleanup(credentials.ResetStorageBackend)

	t.Setenv("SPROUT_CONFIG_DIR", machine)
	t.Setenv("SPROUT_CONFIG", machine)
	credentials.ResetStorageBackend()
	if err := (&State{LinkID: "link-machine", Name: "box", Engine: "127.0.0.1:8006"}).Save(); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SPROUT_CONFIG_DIR", repo)
	t.Setenv("SPROUT_CONFIG", repo)
	credentials.ResetStorageBackend()
	if err := (&State{LinkID: "link-repo", Name: "box", Engine: "127.0.0.1:8006"}).Save(); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SPROUT_CONFIG_DIR", machine)
	credentials.ResetStorageBackend()
	moved, err := AdoptLink(repo)
	if err != nil || moved {
		t.Fatalf("AdoptLink = %v, %v; want nothing moved", moved, err)
	}
	st, err := LoadLink("box")
	if err != nil || st.LinkID != "link-machine" {
		t.Fatalf("machine link = %+v, %v; want the machine's own", st, err)
	}
	// The repo's skipped link file stays (only moved files are removed).
	if _, err := os.Stat(filepath.Join(repo, "gpu", "links", "box.json")); err != nil {
		t.Fatalf("the skipped repo link file was removed: %v", err)
	}
}
