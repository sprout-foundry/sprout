package localmodel

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/buildinfo"
)

// The MLX runtime (libmlxc, libmlx, mlx.metallib) that local models need on
// Apple Silicon. Homebrew's mlx-c works when installed; otherwise sprout
// downloads the copy packaged with each release
// (scripts/package-mlx-runtime.sh) into RuntimeDir, so no other tools are
// needed.

// RuntimeAssetName is the release asset holding the packaged runtime.
const RuntimeAssetName = "sprout-mlx-runtime-darwin-arm64.tar.gz"

// runtimeArchiveRoot is the directory the archive's files sit under.
const runtimeArchiveRoot = "mlx-runtime"

// runtimeReleaseBaseURL is the GitHub releases root; tests override it.
var runtimeReleaseBaseURL = "https://github.com/sprout-foundry/sprout/releases"

// maxRuntimeFileSize bounds each extracted file; the real libraries are
// well under it.
const maxRuntimeFileSize = 1 << 30

// runtimeHTTPClient downloads the runtime; tests override it.
var runtimeHTTPClient = http.DefaultClient

var releaseTagPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// RuntimeDir is where sprout keeps its own copy of the runtime, next to
// the local models.
func RuntimeDir() string {
	return filepath.Join(filepath.Dir(DefaultModelsDir), runtimeArchiveRoot)
}

// RuntimeLibPath is the runtime's libmlxc.dylib, the file MLX_C_LIB names.
func RuntimeLibPath() string { return filepath.Join(RuntimeDir(), "libmlxc.dylib") }

// RuntimeInstalled reports whether sprout's own runtime copy is present.
func RuntimeInstalled() bool {
	for _, name := range []string{"libmlxc.dylib", "libmlx.dylib", "mlx.metallib"} {
		if st, err := os.Stat(filepath.Join(RuntimeDir(), name)); err != nil || !st.Mode().IsRegular() {
			return false
		}
	}
	return true
}

// runtimeReleaseURL is the download URL of a release file: this build's own
// release, or the latest one for source builds.
func runtimeReleaseURL(name string) string {
	if releaseTagPattern.MatchString(buildinfo.Version) {
		return runtimeReleaseBaseURL + "/download/" + buildinfo.Version + "/" + name
	}
	return runtimeReleaseBaseURL + "/latest/download/" + name
}

// InstallRuntime downloads the packaged runtime, checks it against the
// release's SHA256SUMS and installs it into RuntimeDir, replacing any older
// copy. progressFn receives downloaded and total bytes (total 0 if unknown).
func InstallRuntime(ctx context.Context, progressFn ProgressCallback) error {
	want, err := runtimeChecksum(ctx)
	if err != nil {
		return err
	}
	parent := filepath.Dir(RuntimeDir())
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", parent, err)
	}
	archive, err := os.CreateTemp(parent, ".mlx-runtime-*.tar.gz")
	if err != nil {
		return fmt.Errorf("create download file: %w", err)
	}
	defer func() {
		_ = archive.Close()
		_ = os.Remove(archive.Name())
	}()

	got, err := downloadRuntimeArchive(ctx, archive, progressFn)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("MLX runtime download is corrupt (checksum %s, expected %s)", got, want)
	}

	staging, err := os.MkdirTemp(parent, ".mlx-runtime-")
	if err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("read runtime archive: %w", err)
	}
	if err := extractRuntime(archive, staging); err != nil {
		return err
	}
	if err := os.RemoveAll(RuntimeDir()); err != nil {
		return fmt.Errorf("remove old runtime: %w", err)
	}
	if err := os.Rename(staging, RuntimeDir()); err != nil {
		return fmt.Errorf("install runtime: %w", err)
	}
	if !RuntimeInstalled() {
		return errors.New("MLX runtime archive is missing files")
	}
	return nil
}

// runtimeChecksum reads the runtime's SHA-256 from the release's SHA256SUMS.
func runtimeChecksum(ctx context.Context) (string, error) {
	resp, err := runtimeGet(ctx, runtimeReleaseURL("SHA256SUMS"))
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == RuntimeAssetName {
			return strings.ToLower(fields[0]), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read SHA256SUMS: %w", err)
	}
	return "", fmt.Errorf("this release has no MLX runtime (%s is not in SHA256SUMS)", RuntimeAssetName)
}

func downloadRuntimeArchive(ctx context.Context, dst io.Writer, progressFn ProgressCallback) (string, error) {
	resp, err := runtimeGet(ctx, runtimeReleaseURL(RuntimeAssetName))
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	total := resp.ContentLength
	if total < 0 {
		total = 0
	}
	hash := sha256.New()
	var done int64
	buf := make([]byte, 256*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := dst.Write(buf[:n]); err != nil {
				return "", fmt.Errorf("write runtime download: %w", err)
			}
			hash.Write(buf[:n])
			done += int64(n)
			if progressFn != nil {
				progressFn(done, total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", fmt.Errorf("download MLX runtime: %w", readErr)
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func runtimeGet(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := runtimeHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", path.Base(url), err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("download %s: HTTP %d", path.Base(url), resp.StatusCode)
	}
	return resp, nil
}

// extractRuntime unpacks the archive's mlx-runtime/ files into dest. Only
// regular files and directories inside that folder are written.
func extractRuntime(r io.Reader, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("read runtime archive: %w", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read runtime archive: %w", err)
		}
		name := path.Clean(hdr.Name)
		rel, ok := strings.CutPrefix(name, runtimeArchiveRoot+"/")
		if !ok || rel == "" || strings.HasPrefix(rel, "../") || path.IsAbs(rel) {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if hdr.Size > maxRuntimeFileSize {
				return fmt.Errorf("runtime archive entry %s is too large", rel)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644) //nolint:gosec // G302: shared libraries must be readable to load
			if err != nil {
				return err
			}
			if _, err := io.CopyN(f, tr, hdr.Size); err != nil {
				_ = f.Close()
				return fmt.Errorf("extract %s: %w", rel, err)
			}
			if err := f.Close(); err != nil {
				return err
			}
		}
	}
}
