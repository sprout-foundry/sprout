package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// PathTier classifies a filesystem path for approval purposes.
// See ClassifyPathAccess for the resolution rules. The tiers are
// ordered from least to most restrictive.
type PathTier int

const (
	// PathTierUnknown is the zero value and shouldn't be returned;
	// it exists so a missing tier shows up loudly in tests rather
	// than silently behaving as "allow".
	PathTierUnknown PathTier = iota

	// PathTierWorkspace — the path is inside the agent's workspace
	// root (or the sprout config dir). No approval required.
	PathTierWorkspace

	// PathTierExternal — outside the workspace but not in a system
	// or off-CWD home directory. Eligible for the "Allow this folder
	// for the rest of the session" approval choice. Once a parent
	// folder is in the agent's session allowlist, future accesses
	// under it auto-approve.
	PathTierExternal

	// PathTierSensitive — system directories (/etc, /usr, ...) OR
	// home-directory paths when the agent's CWD is outside the user's
	// home. These ALWAYS prompt and CANNOT be added to the session
	// allowlist. The "Allow folder this session" choice is hidden
	// from the dialog for this tier.
	PathTierSensitive
)

// String returns a stable lowercase identifier used in dialog
// extras (the WebUI uses it to pick a button set) and tests.
func (t PathTier) String() string {
	switch t {
	case PathTierWorkspace:
		return "workspace"
	case PathTierExternal:
		return "external"
	case PathTierSensitive:
		return "sensitive"
	default:
		return "unknown"
	}
}

// ClassifyPathAccess decides which approval tier a path falls into.
// All three input paths should be absolute (or empty for unset fields).
// Behavior:
//
//  1. Inside workspaceRoot → PathTierWorkspace.
//  2. Under a known system directory (e.g. /etc, /usr, /var on Unix;
//     C:\Windows, C:\Program Files on Windows) → PathTierSensitive.
//  3. Under the user's home dir AND the agent's CWD is NOT under
//     home → PathTierSensitive (working in /tmp, accessing ~ is
//     unusual and shouldn't get session-wide allowlisting).
//  4. Anything else outside the workspace → PathTierExternal.
//
// homeDir and cwd are passed explicitly so tests can drive each
// branch deterministically. Production callers use os.UserHomeDir
// and the agent's effective CWD.
//
// Symlinks: this classifier compares cleaned path strings; it does
// NOT call filepath.EvalSymlinks. The filesystem layer
// (pkg/filesystem) resolves symlinks before returning
// ErrOutsideWorkingDirectory, so the path we receive here is
// already the symlink-resolved target — the comparison is correct
// for cases where the filesystem layer hands us a real path.
// For non-filesystem callers (e.g. the WebUI file API consulting
// IsFolderSessionAllowed), the path is whatever the caller resolved.
// The classifier is therefore advisory: if you've crafted a clever
// symlink to dodge tier classification, the filesystem layer still
// enforces its own checks at write time.
func ClassifyPathAccess(path, workspaceRoot, homeDir, cwd string) PathTier {
	if path == "" {
		return PathTierUnknown
	}
	abs := normalizePath(path)
	if workspaceRoot != "" && isUnderPrefix(abs, normalizePath(workspaceRoot)) {
		return PathTierWorkspace
	}
	if isSystemPath(abs) {
		return PathTierSensitive
	}
	if homeDir != "" {
		homeAbs := normalizePath(homeDir)
		pathInHome := isUnderPrefix(abs, homeAbs)
		cwdInHome := cwd != "" && isUnderPrefix(normalizePath(cwd), homeAbs)
		if pathInHome && !cwdInHome {
			return PathTierSensitive
		}
	}
	return PathTierExternal
}

// systemPathPrefixes lists directory prefixes that the OS itself
// owns. Reads or writes here ALWAYS prompt — the user is touching
// platform infrastructure and shouldn't be able to silently
// allowlist /etc for an entire session.
//
// On Linux/macOS we include the standard FHS dirs plus the
// Mac-specific /System and /Library. On Windows we cover the
// usual installation roots.
func systemPathPrefixes() []string {
	if runtime.GOOS == "windows" {
		return []string{
			`C:\Windows`,
			`C:\Program Files`,
			`C:\Program Files (x86)`,
			`C:\ProgramData`,
		}
	}
	return []string{
		"/etc",
		"/usr",
		"/var",
		"/bin",
		"/sbin",
		"/boot",
		"/proc",
		"/sys",
		"/dev",
		"/lib",
		"/lib64",
		"/opt",
		"/root",
		"/System",
		"/Library",
		"/private/etc",
		"/private/var",
		"/Applications",
	}
}

func isSystemPath(absPath string) bool {
	if absPath == "" {
		return false
	}
	for _, prefix := range systemPathPrefixes() {
		if isUnderPrefix(absPath, prefix) {
			return true
		}
	}
	return false
}

// normalizePath cleans the path so prefix comparisons aren't fooled
// by trailing slashes or "./" segments. Symlinks aren't resolved
// here — that's the filesystem layer's job. On Windows the path
// becomes case-insensitive for comparison.
func normalizePath(p string) string {
	if p == "" {
		return p
	}
	clean := filepath.Clean(p)
	// Resolve symlinks so that macOS /var → /private/var doesn't cause
	// mismatches between allowlist entries and canonical paths.
	if evaled, err := filepath.EvalSymlinks(clean); err == nil {
		clean = evaled
	}
	if runtime.GOOS == "windows" {
		return strings.ToLower(clean)
	}
	return clean
}

// isUnderPrefix reports whether `path` equals `prefix` or sits in
// a subdirectory of it. Both args must already be normalized.
// The check is path-component aware: "/foobar" is NOT under "/foo".
func isUnderPrefix(path, prefix string) bool {
	if path == "" || prefix == "" {
		return false
	}
	if path == prefix {
		return true
	}
	sep := string(filepath.Separator)
	if !strings.HasSuffix(prefix, sep) {
		prefix += sep
	}
	return strings.HasPrefix(path, prefix)
}

// detectHomeDir returns the user's home directory or empty if it
// can't be resolved (very rare). Wrapped so tests can stub it.
var detectHomeDir = func() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}
