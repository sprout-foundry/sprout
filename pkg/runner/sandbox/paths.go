//go:build darwin || linux

package sandbox

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// canonicalPath returns p absolute with every symlink in its existing prefix
// resolved, so a policy names the same path the kernel checks. A missing
// tail is kept verbatim: deny rules must cover paths created later.
func canonicalPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("empty path")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", p, err)
	}
	existing, rest := abs, ""
	for {
		resolved, err := filepath.EvalSymlinks(existing)
		if err == nil {
			return filepath.Join(resolved, rest), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("resolving %q: %w", p, err)
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return abs, nil
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = parent
	}
}

// pathVariants returns the canonical path plus the cleaned absolute path
// when they differ, so a deny rule still holds if a symlink is swapped.
func pathVariants(p string) ([]string, error) {
	c, err := canonicalPath(p)
	if err != nil {
		return nil, err
	}
	abs, _ := filepath.Abs(p)
	if abs != "" && abs != c {
		return []string{c, abs}, nil
	}
	return []string{c}, nil
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func firstLine(out []byte, err error) string {
	s := strings.TrimSpace(string(out))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return err.Error()
	}
	return s
}
