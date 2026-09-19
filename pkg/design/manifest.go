package design

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Rule ids for the README manifest validator, SP-140-1 §1e/§1g. Constants
// rather than literals so findings and future tooling share one spelling.
const (
	// ruleManifestFrames fires (hard) when the frames: block is missing,
	// empty, or malformed.
	ruleManifestFrames = "manifest_frames"

	// ruleManifestLinkDangling fires (hard) when a relative link in the
	// manifest does not resolve to an existing design asset.
	ruleManifestLinkDangling = "manifest_link_dangling"

	// ruleManifestStatusMarkers is advisory: the manifest does not define
	// all of the draft/review/ready status markers.
	ruleManifestStatusMarkers = "manifest_status_markers"
)

// manifestLinkRe matches a markdown link [text](target). Submatch 2 is the
// target.
var manifestLinkRe = regexp.MustCompile(`\[([^\]]*)\]\(([^)]+)\)`)

// uriSchemePrefixRe matches a leading RFC 3986 URI scheme: one or more
// ALPHA characters followed by ':'. Scheme-relative "//host" links are
// handled separately.
var uriSchemePrefixRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)

// ValidateManifest validates the design README (design/README.md) under root,
// SP-140-1 §1e/§1g: the frames: block parses as name → WxH (a missing, empty,
// or malformed block is a hard finding), every relative link resolves to a
// real design asset (hard finding), and the draft/review/ready status
// markers are present (advisory). A missing README yields no findings (not
// an error). Findings are sorted and the result is never nil.
func ValidateManifest(root string) []Finding {
	data, err := os.ReadFile(filepath.Join(root, DirName, ManifestName))
	if err != nil {
		return []Finding{}
	}
	return validateManifestContent(root, filepath.Join(DirName, ManifestName), data)
}

// validateManifestContent runs the manifest checks over one README's content.
// root is the project root so relative links resolve against design/.
func validateManifestContent(root, relPath string, content []byte) []Finding {
	text := string(content)
	findings := []Finding{}

	// frames: block parseable + non-empty.
	frames, perr := ParseFrames(text)
	switch {
	case perr != nil:
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleManifestFrames,
			Severity: SeverityError,
			Message:  "frames: block is empty or malformed: " + perr.Error(),
		})
	case len(frames) == 0:
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleManifestFrames,
			Severity: SeverityError,
			Message:  "the manifest has no frames: block; declare a non-empty frames: block (device frames for wireframe/screen sizing)",
		})
	}

	// relative links resolve to real design assets.
	findings = append(findings, validateManifestLinks(root, relPath, text)...)

	// status markers (draft/review/ready) present.
	missing := []string{}
	lower := strings.ToLower(text)
	for _, marker := range []string{"draft", "review", "ready"} {
		if !strings.Contains(lower, marker) {
			missing = append(missing, marker)
		}
	}
	if len(missing) > 0 {
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleManifestStatusMarkers,
			Severity: SeverityInfo,
			Message:  fmt.Sprintf("manifest does not define status marker(s): %s", strings.Join(missing, ", ")),
		})
	}

	sortFindings(findings)
	return findings
}

// validateManifestLinks checks that every relative markdown link in the
// manifest (outside fenced code blocks) resolves to an existing design
// asset: SP-140-1 §1g "relative links resolve to real files". Fenced
// examples, in-page anchors, external/absolute links, and other URI-scheme
// links (e.g. mailto:) are skipped. A dangling target is a hard finding
// carrying the 1-based line number of its link.
func validateManifestLinks(root, relPath, text string) []Finding {
	findings := []Finding{}
	seen := map[string]bool{}
	inFence := false
	for i, line := range strings.Split(text, "\n") {
		lineNo := i + 1
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, m := range manifestLinkRe.FindAllStringSubmatch(line, -1) {
			target := strings.TrimSpace(m[2])
			rel := stripLinkFragment(target)
			if !isRelativeAssetLink(rel) {
				continue
			}
			if seen[rel] {
				continue
			}
			seen[rel] = true
			resolved := filepath.Join(root, DirName, filepath.Clean(rel))
			if _, err := os.Stat(resolved); err != nil {
				findings = append(findings, Finding{
					File:     relPath,
					Line:     lineNo,
					Rule:     ruleManifestLinkDangling,
					Severity: SeverityError,
					Message:  fmt.Sprintf("link target %q does not resolve to a design asset (expected %s)", rel, filepath.ToSlash(filepath.Join(DirName, filepath.Clean(rel)))),
				})
			}
		}
	}
	return findings
}

// stripLinkFragment removes a #fragment and ?query from a link target.
func stripLinkFragment(target string) string {
	t := target
	if i := strings.IndexByte(t, '#'); i >= 0 {
		t = t[:i]
	}
	if i := strings.IndexByte(t, '?'); i >= 0 {
		t = t[:i]
	}
	return strings.TrimSpace(t)
}

// isRelativeAssetLink reports whether a link target is a relative
// design-asset path worth an existence check: not empty, not an anchor,
// not an absolute path, and not carrying any URI scheme (so https://,
// mailto:, and friends are all skipped rather than resolved as odd
// relative paths).
func isRelativeAssetLink(rel string) bool {
	if rel == "" {
		return false
	}
	if uriSchemePrefixRe.MatchString(rel) {
		return false
	}
	if strings.HasPrefix(rel, "//") || strings.HasPrefix(rel, "/") {
		return false
	}
	return true
}
