//go:build !js

package cmd

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

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
