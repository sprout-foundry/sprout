package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/buildinfo"
)

func setVersion(t *testing.T, v string) {
	t.Helper()
	old := buildinfo.Version
	buildinfo.Version = v
	t.Cleanup(func() { buildinfo.Version = old })
}

func TestDefaultImageFollowsTheRunnerRelease(t *testing.T) {
	for version, want := range map[string]string{
		"v0.26.1":        WorkspaceImageRepo + ":v0.26.1",
		"v1.10.0":        WorkspaceImageRepo + ":v1.10.0",
		"dev":            WorkspaceImageRepo + ":latest",
		"dev-abc1234":    WorkspaceImageRepo + ":latest",
		"v0.27.0-beta.1": WorkspaceImageRepo + ":latest",
		"":               WorkspaceImageRepo + ":latest",
	} {
		setVersion(t, version)
		if got := DefaultImage(); got != want {
			t.Errorf("version %q: DefaultImage() = %q, want %q", version, got, want)
		}
	}
}

func writeState(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SPROUT_CONFIG_DIR", dir)
	path := filepath.Join(dir, "runner", "runner.json")
	if body != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestLoadStateDefaultsToTheReleaseImage(t *testing.T) {
	setVersion(t, "v0.26.1")
	writeState(t, "")
	st, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if st.Image != WorkspaceImageRepo+":v0.26.1" {
		t.Errorf("fresh runner image = %q", st.Image)
	}
}

func TestLoadStateMigratesOnlyTheOldDefaultImage(t *testing.T) {
	setVersion(t, "v0.26.1")
	path := writeState(t, `{"runner_id":"r1","mode":"container","image":"ghcr.io/sprout-foundry/sprout:latest"}`)
	st, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if st.Image != WorkspaceImageRepo+":v0.26.1" {
		t.Fatalf("old default not migrated: %q", st.Image)
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), `"image"`) {
		t.Errorf("the default image must not be saved:\n%s", data)
	}

	// An upgraded runner moves to its own release's image.
	setVersion(t, "v0.27.0")
	if st, err = LoadState(); err != nil || st.Image != WorkspaceImageRepo+":v0.27.0" {
		t.Errorf("after upgrade image = %q, err = %v", st.Image, err)
	}

	for _, chosen := range []string{
		"ghcr.io/sprout-foundry/sprout:v0.25.0",
		"registry.example/team/sprout:1",
		WorkspaceImageRepo + ":v0.26.1",
	} {
		writeState(t, `{"runner_id":"r1","mode":"container","image":"`+chosen+`"}`)
		st, err := LoadState()
		if err != nil {
			t.Fatal(err)
		}
		if st.Image != chosen {
			t.Errorf("a chosen image must win: got %q, want %q", st.Image, chosen)
		}
		if err := st.Save(); err != nil {
			t.Fatal(err)
		}
		if st, _ = LoadState(); st.Image != chosen {
			t.Errorf("a chosen image must survive a save: got %q, want %q", st.Image, chosen)
		}
	}
}
