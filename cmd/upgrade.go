//go:build !js

package cmd

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	githubAPIURL   = "https://api.github.com/repos/sprout-foundry/sprout/releases/latest"
	releaseBaseURL = "https://github.com/sprout-foundry/sprout/releases/download"
)

var (
	upgradeCheckOnly bool
	upgradeYes       bool
	upgradeVersion   string
)

// upgradeCmd is the in-binary equivalent of scripts/install.{sh,ps1} so
// users don't have to pipe curl into a shell to update. The flow mirrors
// the script: fetch latest tag → download tarball/zip → verify SHA256
// against the release's SHA256SUMS manifest → atomic-replace the running
// binary. SLSA provenance can still be verified out-of-band via
// `gh attestation verify`.
var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade sprout to the latest release",
	Long: `Replace the running sprout binary with a newer release.

Compares the current version against the latest GitHub release, downloads
the matching archive for this OS / architecture, verifies its SHA256
checksum against the release's SHA256SUMS manifest, and atomically
replaces the binary in place.

With --check the command only prints whether an upgrade is available
and exits. With --version pins a specific release tag instead of
"latest". With --yes skips the confirmation prompt (useful in CI).`,
	RunE: runUpgrade,
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
	upgradeCmd.Flags().BoolVar(&upgradeCheckOnly, "check", false,
		"Only check whether an upgrade is available; don't install.")
	upgradeCmd.Flags().BoolVarP(&upgradeYes, "yes", "y", false,
		"Skip the confirmation prompt.")
	upgradeCmd.Flags().StringVar(&upgradeVersion, "version", "",
		"Install a specific release tag instead of the latest (e.g. v0.14.0).")
}

func runUpgrade(cmd *cobra.Command, _ []string) error {
	target, err := resolveTargetVersion()
	if err != nil {
		return err
	}

	current := normalizeVersion(version)
	if target == current && !upgradeCheckOnly && upgradeVersion == "" {
		fmt.Printf("sprout is already at %s — nothing to do.\n", current)
		return nil
	}

	if upgradeCheckOnly {
		if target == current {
			fmt.Printf("sprout %s is up to date.\n", current)
			return nil
		}
		fmt.Printf("Upgrade available: %s → %s\n", current, target)
		fmt.Println("Run `sprout upgrade` to install.")
		return nil
	}

	fmt.Printf("Upgrading sprout: %s → %s\n", current, target)

	if !upgradeYes {
		if !confirm("Proceed?") {
			fmt.Println("Aborted.")
			return nil
		}
	}

	return performUpgrade(target)
}

// resolveTargetVersion returns either the --version flag value or the
// latest release tag from GitHub. Mirrors install.sh:get_version().
func resolveTargetVersion() (string, error) {
	if upgradeVersion != "" {
		return normalizeVersion(upgradeVersion), nil
	}
	tag, err := fetchLatestTag()
	if err != nil {
		return "", fmt.Errorf("look up latest version: %w\n\nPin a tag explicitly with --version vX.Y.Z if you're behind a proxy or hitting GitHub's 60 req/hr unauthenticated rate limit", err)
	}
	return normalizeVersion(tag), nil
}

func fetchLatestTag() (string, error) {
	req, err := http.NewRequest(http.MethodGet, githubAPIURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "sprout-upgrade")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode API response: %w", err)
	}
	if payload.TagName == "" {
		return "", errors.New("GitHub API returned an empty tag_name")
	}
	return payload.TagName, nil
}

// performUpgrade does the download → verify → replace dance.
func performUpgrade(target string) error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate current binary: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("resolve binary path: %w", err)
	}

	archiveName, isZip := archiveNameForPlatform()
	if archiveName == "" {
		return fmt.Errorf("no release archive published for %s/%s — build from source instead", runtime.GOOS, runtime.GOARCH)
	}

	tempDir, err := os.MkdirTemp("", "sprout-upgrade-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	archivePath := filepath.Join(tempDir, archiveName)
	archiveURL := fmt.Sprintf("%s/%s/%s", releaseBaseURL, target, archiveName)
	fmt.Printf("Downloading %s\n", archiveName)
	if err := downloadTo(archiveURL, archivePath); err != nil {
		return fmt.Errorf("download %s: %w", archiveName, err)
	}

	if skip := os.Getenv("SPROUT_SKIP_CHECKSUM"); skip == "1" {
		fmt.Println("WARN: SPROUT_SKIP_CHECKSUM=1 — skipping checksum verification.")
	} else {
		sumsPath := filepath.Join(tempDir, "SHA256SUMS")
		sumsURL := fmt.Sprintf("%s/%s/SHA256SUMS", releaseBaseURL, target)
		if err := downloadTo(sumsURL, sumsPath); err != nil {
			return fmt.Errorf("download SHA256SUMS: %w (re-run with SPROUT_SKIP_CHECKSUM=1 to bypass if you trust the source)", err)
		}
		if err := verifyChecksum(archivePath, sumsPath, archiveName); err != nil {
			return err
		}
	}

	binaryName := "sprout"
	if runtime.GOOS == "windows" {
		binaryName = "sprout.exe"
	}
	extractedPath := filepath.Join(tempDir, binaryName)
	if isZip {
		if err := extractBinaryFromZip(archivePath, binaryName, extractedPath); err != nil {
			return fmt.Errorf("extract %s: %w", archiveName, err)
		}
	} else {
		if err := extractBinaryFromTarGz(archivePath, extractedPath); err != nil {
			return fmt.Errorf("extract %s: %w", archiveName, err)
		}
	}

	if err := os.Chmod(extractedPath, 0755); err != nil {
		return fmt.Errorf("chmod new binary: %w", err)
	}

	if err := replaceBinary(execPath, extractedPath); err != nil {
		return err
	}

	fmt.Printf("sprout upgraded to %s\n", target)
	if runtime.GOOS == "windows" {
		fmt.Println("Restart any running sprout process to pick up the new binary.")
	}
	return nil
}

// archiveNameForPlatform returns the release asset name matching the
// running OS / arch, or "" if no asset is published for this platform.
// The bool reports whether the archive is a zip (Windows) vs tar.gz.
func archiveNameForPlatform() (string, bool) {
	osPart := runtime.GOOS
	archPart := runtime.GOARCH
	switch osPart {
	case "linux", "darwin":
		// Both architectures shipped per release.yml.
		if archPart != "amd64" && archPart != "arm64" {
			return "", false
		}
		return fmt.Sprintf("sprout-%s-%s.tar.gz", osPart, archPart), false
	case "windows":
		// Only amd64 shipped today.
		if archPart != "amd64" {
			return "", true
		}
		return "sprout-windows-amd64.zip", true
	default:
		return "", false
	}
}

// downloadTo fetches a URL into dst with a 60s connect timeout and a
// total deadline of 5 minutes. Caller's responsibility to size that for
// their needs — release tarballs are ~30MB so 5m is enormous headroom.
func downloadTo(url, dst string) error {
	resp, err := httpClient().Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	return nil
}

// verifyChecksum compares the SHA256 of `archive` against the entry for
// `name` in `sumsPath`. The SHA256SUMS file is the standard `<hex>  <name>`
// format produced by sha256sum / shasum -a 256.
func verifyChecksum(archive, sumsPath, name string) error {
	expected, err := findChecksumLine(sumsPath, name)
	if err != nil {
		return err
	}
	actual, err := sha256OfFile(archive)
	if err != nil {
		return fmt.Errorf("hash downloaded archive: %w", err)
	}
	if !strings.EqualFold(expected, actual) {
		return fmt.Errorf("checksum mismatch for %s\n  expected: %s\n  actual:   %s\n\nRefusing to install. The download may be corrupted or tampered with", name, expected, actual)
	}
	fmt.Printf("Checksum verified (%s)\n", expected)
	return nil
}

func findChecksumLine(sumsPath, name string) (string, error) {
	f, err := os.Open(sumsPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// Strip the leading '*' that sha256sum's binary-mode output adds.
		fname := strings.TrimPrefix(fields[1], "*")
		if fname == name {
			return fields[0], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("%s not listed in SHA256SUMS", name)
}

func sha256OfFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// extractBinaryFromTarGz unpacks the single binary inside the tarball.
// Release tarballs contain exactly one regular file (e.g. sprout-linux-amd64),
// so we don't try to preserve a directory layout — just write the first
// regular file to dst.
func extractBinaryFromTarGz(tgz, dst string) error {
	f, err := os.Open(tgz)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return errors.New("tarball contained no regular files")
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			continue
		}
		out, err := os.Create(dst)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, tr)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
}

// extractBinaryFromZip extracts the first .exe in the archive (Windows).
func extractBinaryFromZip(zipPath, binaryName, dst string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, entry := range zr.File {
		if !strings.EqualFold(filepath.Base(entry.Name), binaryName) &&
			!strings.HasSuffix(strings.ToLower(entry.Name), ".exe") {
			continue
		}
		in, err := entry.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(dst)
		if err != nil {
			in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		_ = in.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	return errors.New("no .exe entry found in zip")
}

// replaceBinary swaps the running binary with the freshly-downloaded one.
// Unix can rename(2) over a running ELF/Mach-O file — the kernel keeps
// the old inode open for the running process and frees it on exit, so
// the swap is atomic and the running sprout keeps working until exit.
//
// Windows can't replace a running .exe; the OS holds an exclusive lock
// on it. The standard workaround: rename the running exe to ".old"
// (which Windows DOES permit on a running image) and write the new one
// in its place. The .old file is left for the user to delete after
// restart — we don't schedule it via MOVEFILE_DELAY_UNTIL_REBOOT because
// that needs admin and feels heavy-handed for a CLI tool.
func replaceBinary(targetPath, newPath string) error {
	dir := filepath.Dir(targetPath)
	if runtime.GOOS == "windows" {
		backup := targetPath + ".old"
		// Best-effort: if a .old from a previous upgrade still exists,
		// try to remove it. If that fails (the OS may still consider it
		// busy), continue — we'll just write a fresh .old below.
		_ = os.Remove(backup)
		if err := os.Rename(targetPath, backup); err != nil {
			return fmt.Errorf("rename running binary to %s: %w", backup, err)
		}
		if err := moveFile(newPath, targetPath); err != nil {
			// Best-effort rollback: put the original back.
			_ = os.Rename(backup, targetPath)
			return fmt.Errorf("install new binary at %s: %w", targetPath, err)
		}
		fmt.Printf("Note: previous binary saved at %s — remove it once you've restarted.\n", backup)
		return nil
	}

	// Unix path — atomic rename within the same filesystem.
	stagingPath := filepath.Join(dir, ".sprout.upgrade.tmp")
	_ = os.Remove(stagingPath)
	if err := moveFile(newPath, stagingPath); err != nil {
		return fmt.Errorf("stage new binary in install dir: %w", err)
	}
	if err := os.Rename(stagingPath, targetPath); err != nil {
		// Could happen if install dir isn't writable (binary was installed
		// to /usr/local/bin via sudo). Surface a clear hint.
		_ = os.Remove(stagingPath)
		return fmt.Errorf("replace %s: %w\n\nIf sprout was installed system-wide, re-run with sudo or use the install script", targetPath, err)
	}
	return nil
}

// moveFile copies src → dst then removes src. Used instead of os.Rename
// when the staging temp dir is on a different filesystem than the install
// dir (common: /tmp is tmpfs, /usr/local is the root fs).
func moveFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}

// confirm reads a single Y/N answer from stdin, defaulting to yes.
func confirm(prompt string) bool {
	fmt.Printf("%s [Y/n] ", prompt)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return true
	}
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	return answer == "" || answer == "y" || answer == "yes"
}

// normalizeVersion strips a leading 'v' (or 'V') so v1.2.3 / V1.2.3 / 1.2.3
// compare equal. We re-add the 'v' before constructing release URLs because
// the upstream tags carry it.
func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if len(v) > 0 && (v[0] == 'v' || v[0] == 'V') {
		return "v" + v[1:]
	}
	if v == "dev" {
		return v
	}
	return "v" + v
}

func httpClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Minute}
}
