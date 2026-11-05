package injection

import "strings"

// TrustClass is how content at a path is treated with respect to the
// prompt-injection defense (SP-169). The zero value is TrustUnknown on
// purpose: an unclassified path is *not* silently first-party, because
// mislabelling our own code as untrusted is a worse error than the one the
// defense fixes, and treating unknown code as ours is the ingress the defense
// exists to close.
type TrustClass int

const (
	// TrustUnknown means no decision has been recorded and no structural
	// marker applies. Callers ask the user (once, per machine and repo) and
	// persist the answer.
	TrustUnknown TrustClass = iota
	// TrustFirstParty means the user classified the path as their own code.
	TrustFirstParty
	// TrustThirdParty means the path is content we did not write: a known
	// dependency tree, or a path the user classified as third-party.
	TrustThirdParty
)

// String renders the class for CLI output and diagnostics.
func (c TrustClass) String() string {
	switch c {
	case TrustFirstParty:
		return "first-party"
	case TrustThirdParty:
		return "third-party"
	default:
		return "unknown"
	}
}

// KnownThirdPartyMarkers are directory names that identify a dependency tree
// without asking. Content under any of these is never ours: the author of the
// dependency controls comments, string literals and fixture data, all of which
// reach the model when a file is read.
//
// These are unambiguous by construction, so classification is deterministic
// and needs no persisted decision.
var KnownThirdPartyMarkers = []string{
	"node_modules",
	"vendor",
	".venv",
	"venv",
	"site-packages",
	".m2",
	".cargo",
	".git",
}

// markerSet is the lookup form of KnownThirdPartyMarkers.
var markerSet = func() map[string]bool {
	m := make(map[string]bool, len(KnownThirdPartyMarkers))
	for _, name := range KnownThirdPartyMarkers {
		m[name] = true
	}
	return m
}()

// NormalizeRelPath canonicalizes a root-relative, slash-separated path so two
// spellings of the same location compare equal. A leading "./" is trimmed and
// the empty string becomes "." (the workspace root itself). Paths are not
// cleaned further: the input is already relative to the workspace root, and
// resolving ".." here would be a guess about a filesystem this function
// deliberately does not touch.
func NormalizeRelPath(relPath string) string {
	p := strings.TrimSpace(relPath)
	p = strings.ReplaceAll(p, "\\", "/")
	for strings.HasPrefix(p, "./") {
		p = strings.TrimPrefix(p, "./")
	}
	p = strings.Trim(p, "/")
	if p == "" {
		return "."
	}
	return p
}

// hasKnownMarker reports whether any segment of the normalized path is a
// known dependency-tree marker.
func hasKnownMarker(normalized string) bool {
	if normalized == "." {
		return false
	}
	for _, seg := range strings.Split(normalized, "/") {
		if markerSet[seg] {
			return true
		}
	}
	return false
}

// ClassifyPath decides how content at relPath (slash-separated, relative to
// the workspace root) is treated. decisions maps a root-relative path to its
// classification; the longest matching prefix wins.
//
// Rules, in order:
//
//  1. If any path segment matches a known marker → TrustThirdParty.
//  2. Longest-prefix match in decisions → TrustThirdParty or TrustFirstParty.
//  3. Otherwise TrustUnknown.
//
// Rule 1 outranks rule 2 deliberately: a decision recorded on a parent
// directory (say "." or "deps") cannot make a vendored tree first-party. The
// marker is structural and cannot be argued with by a persisted answer.
//
// Prefix matching is segment-wise, so a decision on "vendor/acme" does not
// match "vendor/acmecorp" — a sibling directory that merely shares a byte
// prefix is a different tree with a different author.
func ClassifyPath(relPath string, decisions map[string]bool) TrustClass {
	normalized := NormalizeRelPath(relPath)

	if hasKnownMarker(normalized) {
		return TrustThirdParty
	}

	best := ""
	bestClass := TrustUnknown
	for rawPath, thirdParty := range decisions {
		key := NormalizeRelPath(rawPath)
		if !isPrefixPath(normalized, key) {
			continue
		}
		// Longest match wins; ties are impossible because keys that
		// normalize to the same string are the same match.
		if len(key) > len(best) || best == "" {
			best = key
			bestClass = TrustThirdParty
			if !thirdParty {
				bestClass = TrustFirstParty
			}
		}
	}
	return bestClass
}

// isPrefixPath reports whether prefix (normalized) is path (normalized) itself
// or a directory containing it, comparing whole segments only.
func isPrefixPath(path, prefix string) bool {
	if prefix == "." {
		return true
	}
	if path == prefix {
		return true
	}
	return strings.HasPrefix(path, prefix+"/")
}
