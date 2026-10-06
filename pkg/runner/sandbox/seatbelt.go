//go:build darwin

package sandbox

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// seatbeltDeviceWrites are the device nodes ordinary processes open for
// writing: output sinks, terminals, and dtrace's helper, which every
// dyld-loaded binary touches at launch.
const seatbeltDeviceWrites = `  (literal "/dev/null")
  (literal "/dev/zero")
  (literal "/dev/dtracehelper")
  (literal "/dev/ptmx")
  (regex #"^/dev/tty")
  (regex #"^/dev/fd/")
`

// seatbeltSharedTemp is world-writable already, and many build scripts write
// to /tmp by literal path rather than through TMPDIR.
const seatbeltSharedTemp = "/private/tmp"

// seatbeltProfile renders p as an SBPL profile. Reads are allowed host-wide
// except p.DenyRead; writes only to the workspace, its temp dir, the
// allowlisted paths and the per-user system dirs toolchains cannot work
// without. Seatbelt evaluates the last matching rule, so deny-read rules
// come last and win over any overlapping allow.
func seatbeltProfile(p Policy, sys darwinUserDirs) (string, error) {
	if p.WorkDir == "" {
		return "", errors.New("policy has no WorkDir")
	}
	writable := []string{p.WorkDir}
	if p.TempDir != "" {
		writable = append(writable, p.TempDir)
	}
	writable = append(writable, p.Writable...)
	writable = append(writable, seatbeltSharedTemp)
	if sys.Cache != "" {
		writable = append(writable, sys.Cache)
	}
	if sys.Temp != "" {
		writable = append(writable, filepath.Join(sys.Temp, "TemporaryItems"))
	}

	var b strings.Builder
	b.WriteString("(version 1)\n(allow default)\n(deny file-write*)\n(allow file-write*\n")
	b.WriteString(seatbeltDeviceWrites)
	seen := map[string]bool{}
	for _, w := range writable {
		c, err := canonicalPath(w)
		if err != nil {
			return "", fmt.Errorf("writable path: %w", err)
		}
		if err := writeSubpath(&b, c, seen); err != nil {
			return "", err
		}
	}
	if sys.Temp != "" {
		if err := writeTempPrefixes(&b, sys.Temp); err != nil {
			return "", err
		}
	}
	b.WriteString(")\n")

	if !p.AllowNetwork {
		b.WriteString("(deny network-outbound)\n")
		b.WriteString("(allow network-outbound\n  (remote ip \"localhost:*\")\n  (remote unix-socket))\n")
	}

	if len(p.DenyRead) > 0 {
		b.WriteString("(deny file-read* file-write*\n")
		seen = map[string]bool{}
		for _, d := range p.DenyRead {
			variants, err := pathVariants(d)
			if err != nil {
				return "", fmt.Errorf("deny-read path: %w", err)
			}
			for _, v := range variants {
				if err := writeSubpath(&b, v, seen); err != nil {
					return "", err
				}
			}
		}
		b.WriteString(")\n")
	}
	return b.String(), nil
}

// seatbeltUserTempPrefixes are entries toolchains create directly in the
// per-user temp dir whatever TMPDIR says: xcrun caches SDK lookups there,
// SwiftPM (inside xcodebuild) stages manifest builds there, and xcodebuild
// saves its error result bundle there. Allowing the prefixes rather than the
// dir keeps other apps' temp files in it read-only.
var seatbeltUserTempPrefixes = []string{"xcrun_db", "TemporaryDirectory.", "ResultBundle_"}

func writeTempPrefixes(b *strings.Builder, userTemp string) error {
	dir, err := canonicalPath(userTemp)
	if err != nil {
		return fmt.Errorf("user temp dir: %w", err)
	}
	for _, prefix := range seatbeltUserTempPrefixes {
		re, err := sbplRegex("^" + regexp.QuoteMeta(filepath.Join(dir, prefix)))
		if err != nil {
			return err
		}
		fmt.Fprintf(b, "  (regex %s)\n", re)
	}
	return nil
}

func writeSubpath(b *strings.Builder, path string, seen map[string]bool) error {
	if seen[path] {
		return nil
	}
	seen[path] = true
	lit, err := sbplString(path)
	if err != nil {
		return err
	}
	fmt.Fprintf(b, "  (subpath %s)\n", lit)
	return nil
}

// sbplRegex renders pattern as an SBPL #"..." regex literal, which has no
// escape for the closing quote.
func sbplRegex(pattern string) (string, error) {
	for _, r := range pattern {
		if r == '"' || r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("pattern %q cannot be expressed as an SBPL regex", pattern)
		}
	}
	return `#"` + pattern + `"`, nil
}

// sbplString quotes s as an SBPL string literal. Control characters have no
// place in a path a policy should name, so they are rejected rather than
// escaped.
func sbplString(s string) (string, error) {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			return "", fmt.Errorf("path %q contains a control character", s)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String(), nil
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
