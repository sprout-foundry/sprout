package localmodel

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/buildinfo"
)

func runtimeArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func serveRuntimeRelease(t *testing.T, archive []byte, sum string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/SHA256SUMS"):
			_, _ = w.Write([]byte("abc  sprout-darwin-arm64\n" + sum + "  " + RuntimeAssetName + "\n"))
		case strings.HasSuffix(r.URL.Path, "/"+RuntimeAssetName):
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	oldBase, oldDir := runtimeReleaseBaseURL, DefaultModelsDir
	runtimeReleaseBaseURL = srv.URL
	DefaultModelsDir = filepath.Join(t.TempDir(), "models")
	t.Cleanup(func() { runtimeReleaseBaseURL, DefaultModelsDir = oldBase, oldDir })
}

func validRuntimeFiles() map[string]string {
	return map[string]string{
		"mlx-runtime/libmlxc.dylib":        "c",
		"mlx-runtime/libmlx.dylib":         "core",
		"mlx-runtime/mlx.metallib":         "metal",
		"mlx-runtime/licenses/mlx-LICENSE": "mit",
		"mlx-runtime/../escape":            "nope",
		"elsewhere/libmlxc.dylib":          "nope",
	}
}

func sha(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestInstallRuntimeVerifiesAndInstalls(t *testing.T) {
	archive := runtimeArchive(t, validRuntimeFiles())
	serveRuntimeRelease(t, archive, sha(archive))

	if RuntimeInstalled() {
		t.Fatal("runtime reported installed before install")
	}
	var last int64
	if err := InstallRuntime(context.Background(), func(done, _ int64) { last = done }); err != nil {
		t.Fatalf("InstallRuntime: %v", err)
	}
	if !RuntimeInstalled() {
		t.Fatal("runtime not installed")
	}
	if last != int64(len(archive)) {
		t.Fatalf("progress ended at %d, want %d", last, len(archive))
	}
	got, err := os.ReadFile(filepath.Join(RuntimeDir(), "licenses", "mlx-LICENSE"))
	if err != nil || string(got) != "mit" {
		t.Fatalf("license file = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(RuntimeDir()), "escape")); !os.IsNotExist(err) {
		t.Fatal("archive entry escaped the runtime directory")
	}
	entries, _ := os.ReadDir(filepath.Dir(RuntimeDir()))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".mlx-runtime-") {
			t.Fatalf("leftover temp file %s", e.Name())
		}
	}
}

func TestInstallRuntimeRejectsChecksumMismatch(t *testing.T) {
	archive := runtimeArchive(t, validRuntimeFiles())
	serveRuntimeRelease(t, archive, strings.Repeat("0", 64))

	err := InstallRuntime(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("err = %v, want checksum failure", err)
	}
	if RuntimeInstalled() {
		t.Fatal("corrupt runtime was installed")
	}
}

func TestInstallRuntimeRejectsIncompleteArchive(t *testing.T) {
	archive := runtimeArchive(t, map[string]string{"mlx-runtime/libmlxc.dylib": "c"})
	serveRuntimeRelease(t, archive, sha(archive))

	if err := InstallRuntime(context.Background(), nil); err == nil {
		t.Fatal("expected an error for an archive missing libraries")
	}
}

func TestInstallRuntimeWithoutReleaseAsset(t *testing.T) {
	serveRuntimeRelease(t, nil, "")
	runtimeReleaseBaseURL += "/missing"

	if err := InstallRuntime(context.Background(), nil); err == nil {
		t.Fatal("expected an error when the release has no runtime")
	}
}

func TestRuntimeReleaseURLUsesBuildVersion(t *testing.T) {
	cases := map[string]string{
		"v1.2.3":        "/download/v1.2.3/x",
		"dev":           "/latest/download/x",
		"v1.2.3-4-gabc": "/latest/download/x",
	}
	for version, want := range cases {
		t.Run(version, func(t *testing.T) {
			setBuildVersion(t, version)
			if got := runtimeReleaseURL("x"); !strings.HasSuffix(got, want) {
				t.Fatalf("runtimeReleaseURL = %s, want suffix %s", got, want)
			}
		})
	}
}

func setBuildVersion(t *testing.T, v string) {
	t.Helper()
	old := buildinfo.Version
	buildinfo.Version = v
	t.Cleanup(func() { buildinfo.Version = old })
}
