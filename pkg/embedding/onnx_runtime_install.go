//go:build !wasm && cgo

package embedding

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// onnxRuntimeVersion is the ONNX Runtime release sprout downloads as the
// production-grade fallback when no library is pre-staged. The version
// MUST match the ORT_API_VERSION declared by the yalue/onnxruntime_go
// header (`onnxruntime_c_api.h`). v1.30.x of that binding sets
// `#define ORT_API_VERSION 25` (Microsoft's 1.25.x line), so the staged
// dylib must come from a 1.25.x release. Loading a 1.20.x dylib against
// this binding fails with "The requested API version [25] is not
// available, only API versions [1, 20] are supported" — verified on
// macOS arm64, 2026-06-04.
//
// When bumping yalue/onnxruntime_go, re-check the value of
// ORT_API_VERSION in the binding's header and bump this constant to
// match the corresponding ORT release.
const onnxRuntimeVersion = "1.25.1"

// onnxRuntimeReleaseConfig points at one platform-specific archive published
// on the upstream microsoft/onnxruntime GitHub releases page, plus the
// internal archive path where the shared library lives.
type onnxRuntimeReleaseConfig struct {
	// URL of the official release archive.
	URL string
	// SHA256 hex of the archive. Empty = unpinned (downloader will accept
	// any payload). Pin per-platform once the team is comfortable with a
	// reproducible upstream revision.
	SHA256 string
	// Tail of the path inside the archive that identifies the shared
	// library file we want. The full entry name varies by platform/version
	// (e.g. `onnxruntime-linux-aarch64-1.20.1/lib/libonnxruntime.so.1.20.1`)
	// so we match by suffix to stay robust across version bumps.
	InnerLibSuffix string
	// Archive format. "tgz" = tar.gz; "zip" = .zip.
	Format string
}

// onnxRuntimeReleaseFor returns the release config for the current
// GOOS/GOARCH, or false if no published archive matches this platform.
func onnxRuntimeReleaseFor(goos, goarch string) (onnxRuntimeReleaseConfig, bool) {
	base := "https://github.com/microsoft/onnxruntime/releases/download/v" + onnxRuntimeVersion

	switch goos {
	case "linux":
		switch goarch {
		case "amd64":
			return onnxRuntimeReleaseConfig{
				URL:            base + "/onnxruntime-linux-x64-" + onnxRuntimeVersion + ".tgz",
				InnerLibSuffix: "/lib/libonnxruntime.so." + onnxRuntimeVersion,
				Format:         "tgz",
			}, true
		case "arm64":
			return onnxRuntimeReleaseConfig{
				URL:            base + "/onnxruntime-linux-aarch64-" + onnxRuntimeVersion + ".tgz",
				InnerLibSuffix: "/lib/libonnxruntime.so." + onnxRuntimeVersion,
				Format:         "tgz",
			}, true
		}
	case "darwin":
		switch goarch {
		case "amd64":
			return onnxRuntimeReleaseConfig{
				URL:            base + "/onnxruntime-osx-x86_64-" + onnxRuntimeVersion + ".tgz",
				InnerLibSuffix: "/lib/libonnxruntime." + onnxRuntimeVersion + ".dylib",
				Format:         "tgz",
			}, true
		case "arm64":
			return onnxRuntimeReleaseConfig{
				URL:            base + "/onnxruntime-osx-arm64-" + onnxRuntimeVersion + ".tgz",
				InnerLibSuffix: "/lib/libonnxruntime." + onnxRuntimeVersion + ".dylib",
				Format:         "tgz",
			}, true
		}
	case "windows":
		if goarch == "amd64" {
			return onnxRuntimeReleaseConfig{
				URL:            base + "/onnxruntime-win-x64-" + onnxRuntimeVersion + ".zip",
				InnerLibSuffix: "/lib/onnxruntime.dll",
				Format:         "zip",
			}, true
		}
	}
	// Note: Android has no entry here because Microsoft does not publish a
	// standalone Android artifact on the GitHub releases page. Android ONNX
	// Runtime builds ship via Maven Central as
	// `com.microsoft.onnxruntime:onnxruntime-android:<ver>` (AAR format).
	// Auto-download from Maven is a separate implementation — for now,
	// Android users must stage the library manually and point sprout at it
	// via SPROUT_ONNX_RUNTIME_LIB. See docs/ONNX_RUNTIME.md ("Termux /
	// Android").
	return onnxRuntimeReleaseConfig{}, false
}

// downloadAndStageONNXRuntime pulls the platform-appropriate ONNX Runtime
// release from upstream, extracts the shared library, and saves it to
// runtimeDir/<libName>. Returns the absolute path of the staged library
// on success.
//
// Network failures are routine on first use; callers should treat any error
// from this function as "fall through to the next resolver step" rather
// than fatal.
func downloadAndStageONNXRuntime(ctx context.Context, runtimeDir, libName string) (string, error) {
	cfg, ok := onnxRuntimeReleaseFor(runtime.GOOS, runtime.GOARCH)
	if !ok {
		return "", fmt.Errorf("onnx: no published ONNX Runtime release for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		return "", fmt.Errorf("onnx: create runtime dir: %w", err)
	}

	// Download the archive to a tempfile in runtimeDir. Using runtimeDir
	// keeps the temp on the same filesystem so the final rename is atomic.
	archive, err := os.CreateTemp(runtimeDir, ".onnxruntime-download-*.bin")
	if err != nil {
		return "", fmt.Errorf("onnx: create temp: %w", err)
	}
	archivePath := archive.Name()
	defer os.Remove(archivePath)

	if err := streamDownloadAndHash(ctx, cfg.URL, archive, cfg.SHA256); err != nil {
		archive.Close()
		return "", fmt.Errorf("onnx: download %s: %w", cfg.URL, err)
	}
	if err := archive.Close(); err != nil {
		return "", err
	}

	destPath := filepath.Join(runtimeDir, libName)
	if cfg.Format == "zip" {
		if err := extractZipBySuffix(archivePath, cfg.InnerLibSuffix, destPath); err != nil {
			return "", err
		}
	} else {
		if err := extractTgzBySuffix(archivePath, cfg.InnerLibSuffix, destPath); err != nil {
			return "", err
		}
	}
	return destPath, nil
}

// streamDownloadAndHash downloads url to dst while computing SHA-256 of the
// streamed bytes; on completion it validates against expectedHash (if set).
// On hash mismatch, dst is left as-is and the caller is expected to discard it.
func streamDownloadAndHash(ctx context.Context, url string, dst io.Writer, expectedHash string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d %s", resp.StatusCode, resp.Status)
	}
	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(dst, hasher), resp.Body); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	if expectedHash != "" {
		actual := fmt.Sprintf("%x", hasher.Sum(nil))
		if actual != expectedHash {
			return fmt.Errorf("hash mismatch: expected %s, got %s", expectedHash, actual)
		}
	}
	return nil
}

// extractTgzBySuffix scans a tar.gz archive for an entry whose name ends
// with `suffix` and writes its bytes to `destPath`. Symlinks are skipped —
// the upstream archive includes both a real file and a versioned symlink
// pointing at it; we want the real file.
func extractTgzBySuffix(archivePath, suffix, destPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("archive does not contain an entry matching %q", suffix)
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if !strings.HasSuffix(hdr.Name, suffix) {
			continue
		}
		return writeFileAtomic(destPath, tr, 0o755)
	}
}

// extractZipBySuffix does the same for .zip archives.
func extractZipBySuffix(archivePath, suffix, destPath string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("zip open: %w", err)
	}
	defer r.Close()
	for _, f := range r.File {
		// Zip entries use forward slashes per spec; suffix comparison works
		// across hosts so long as we keep the suffix in that form.
		if !strings.HasSuffix(f.Name, suffix) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("zip read %s: %w", f.Name, err)
		}
		defer rc.Close()
		return writeFileAtomic(destPath, rc, 0o755)
	}
	return fmt.Errorf("archive does not contain an entry matching %q", suffix)
}

// writeFileAtomic copies src to a temp in the destination directory and
// renames into place, so a crash mid-write doesn't leave a half-written
// shared library that the runtime will then try to load.
func writeFileAtomic(destPath string, src io.Reader, mode os.FileMode) error {
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".onnxruntime-extract-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, destPath); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
