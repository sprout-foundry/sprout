//go:build !js

package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/buildinfo"
	"github.com/sprout-foundry/sprout/pkg/updatecheck"
)

func TestIsBrewPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/opt/homebrew/Cellar/sprout/0.14.0/bin/sprout", true},
		{"/usr/local/Cellar/sprout/0.14.0/bin/sprout", true},
		{"/usr/local/bin/sprout", false},
		{"/home/alanp/go/bin/sprout", false},
	}
	for _, tc := range cases {
		// isBrewPath resolves through EvalSymlinks; non-existent paths fall
		// back to the input string, which keeps these synthetic paths intact.
		if got := isBrewPath(tc.path); got != tc.want {
			t.Errorf("isBrewPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestWriteUpdateNotice(t *testing.T) {
	var buf bytes.Buffer
	writeUpdateNotice(&buf, "v0.15.0", "sprout upgrade")
	want := "sprout v0.15.0 available — run `sprout upgrade`\n"
	if buf.String() != want {
		t.Errorf("notice = %q, want %q", buf.String(), want)
	}

	buf.Reset()
	writeUpdateNotice(&buf, "v0.15.0", "brew upgrade sprout")
	want = "sprout v0.15.0 available — run `brew upgrade sprout`\n"
	if buf.String() != want {
		t.Errorf("brew notice = %q, want %q", buf.String(), want)
	}
}

func TestMaybeRenderUpdateNotice_RespectsCacheAndInterval(t *testing.T) {
	t.Setenv("SPROUT_STATE_DIR", t.TempDir())
	setTestVersion(t, "v0.14.0")

	if err := writeTestState(updatecheck.State{LatestRelease: "v0.15.0"}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	maybeRenderUpdateNoticeTo(&buf)
	if buf.Len() == 0 {
		t.Fatal("expected a notice when cached latest is newer")
	}
	if !bytes.Contains(buf.Bytes(), []byte("v0.15.0")) {
		t.Errorf("notice should name the new version, got %q", buf.String())
	}

	buf.Reset()
	maybeRenderUpdateNoticeTo(&buf)
	if buf.Len() != 0 {
		t.Errorf("notice must not repeat within the interval, got %q", buf.String())
	}
}

func TestMaybeRenderUpdateNotice_SkipsDevBuild(t *testing.T) {
	t.Setenv("SPROUT_STATE_DIR", t.TempDir())
	setTestVersion(t, "dev")

	if err := writeTestState(updatecheck.State{LatestRelease: "v0.15.0"}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	maybeRenderUpdateNoticeTo(&buf)
	if buf.Len() != 0 {
		t.Errorf("dev builds must never notify, got %q", buf.String())
	}
}

func setTestVersion(t *testing.T, v string) {
	t.Helper()
	old := buildinfo.Version
	buildinfo.Version = v
	t.Cleanup(func() { buildinfo.Version = old })
}

// writeTestState seeds the update-check cache file in the temp state dir
// the test installed via SPROUT_STATE_DIR.
func writeTestState(s updatecheck.State) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(
		filepath.Join(os.Getenv("SPROUT_STATE_DIR"), "update-check.json"),
		b, 0o600)
}
