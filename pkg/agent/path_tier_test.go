package agent

import (
	"path/filepath"
	"runtime"
	"testing"
)

// TestClassifyPathAccess walks every branch of the tier classifier.
// Unix-only fixtures — Windows paths use different separators and
// are covered by TestClassifyPathAccess_Windows below.
func TestClassifyPathAccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix paths only; see TestClassifyPathAccess_Windows")
	}
	workspace := "/home/alan/proj"
	home := "/home/alan"

	tests := []struct {
		name string
		path string
		cwd  string
		want PathTier
	}{
		{
			name: "inside workspace",
			path: "/home/alan/proj/src/main.go",
			cwd:  workspace,
			want: PathTierWorkspace,
		},
		{
			name: "workspace root itself",
			path: workspace,
			cwd:  workspace,
			want: PathTierWorkspace,
		},
		{
			name: "system /etc always sensitive (CWD in workspace)",
			path: "/etc/passwd",
			cwd:  workspace,
			want: PathTierSensitive,
		},
		{
			name: "system /usr always sensitive (CWD in home)",
			path: "/usr/local/bin/foo",
			cwd:  home + "/somewhere-else",
			want: PathTierSensitive,
		},
		{
			name: "Mac /Library is sensitive",
			path: "/Library/Application Support/foo",
			cwd:  workspace,
			want: PathTierSensitive,
		},
		{
			name: "home path AND cwd outside home → sensitive",
			path: "/home/alan/.ssh/id_rsa",
			cwd:  "/tmp/sandbox",
			want: PathTierSensitive,
		},
		{
			name: "home path AND cwd in home → external (allowlistable)",
			path: "/home/alan/other-project/main.go",
			cwd:  workspace, // workspace is under /home/alan
			want: PathTierExternal,
		},
		{
			name: "random external path → external",
			path: "/tmp/scratch/notes.md",
			cwd:  workspace,
			want: PathTierExternal,
		},
		{
			name: "/etc-suffix not under /etc — external",
			// guarding against the prefix-bug where "/etc-mirror"
			// would match "/etc". The component-aware check should
			// classify this as external, not sensitive.
			path: "/etc-mirror/file",
			cwd:  workspace,
			want: PathTierExternal,
		},
		{
			name: "empty path returns unknown",
			path: "",
			cwd:  workspace,
			want: PathTierUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyPathAccess(tc.path, workspace, home, tc.cwd)
			if got != tc.want {
				t.Errorf("ClassifyPathAccess(%q, ws=%q, home=%q, cwd=%q) = %v, want %v",
					tc.path, workspace, home, tc.cwd, got, tc.want)
			}
		})
	}
}

// TestClassifyPathAccess_PrefixComponentSafety guards against the
// classic prefix-bug — "/foobar" must NOT count as being under "/foo".
// This is a separate test because regressions here are easy to miss
// and dangerous (a file like /etc-backups could leak past the
// sensitive tier check).
func TestClassifyPathAccess_PrefixComponentSafety(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix paths only")
	}
	// "/etcfoo" is NOT under "/etc"; the classifier must use
	// component-aware matching, not raw string HasPrefix.
	got := ClassifyPathAccess("/etcfoo/data", "/work", "/home/u", "/work")
	if got == PathTierSensitive {
		t.Errorf("/etcfoo/data was classified as Sensitive; the prefix check is leaking ('/etc' shouldn't match '/etcfoo')")
	}
}

// TestIsUnderPrefix locks down the component-aware matching helper.
func TestIsUnderPrefix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-only fixtures")
	}
	tests := []struct {
		path, prefix string
		want         bool
	}{
		{"/a/b/c", "/a/b", true},
		{"/a/b", "/a/b", true},
		{"/a/b/c", "/a", true},
		{"/a/b", "/a/b/c", false},
		{"/ab", "/a", false}, // component boundary
		{"/a", "/", true},    // everything is under root
		{"/", "/", true},     // root itself
		{"", "/a", false},
		{"/a", "", false},
	}
	for _, tc := range tests {
		got := isUnderPrefix(tc.path, tc.prefix)
		if got != tc.want {
			t.Errorf("isUnderPrefix(%q, %q) = %v, want %v", tc.path, tc.prefix, got, tc.want)
		}
	}
}

// TestSessionAllowedFolders exercises the per-folder allowlist API
// on the security manager: add, prefix-match lookup, snapshot, dedup.
func TestSessionAllowedFolders(t *testing.T) {
	sm := NewAgentSecurityManager()

	if sm.IsFolderSessionAllowed("/tmp/foo/bar.txt") {
		t.Fatal("fresh manager should not allow any folder")
	}
	if sm.IsSecurityBypassApproved() {
		t.Fatal("fresh manager: IsSecurityBypassApproved should be false")
	}

	sm.AddSessionAllowedFolder("/tmp/foo")

	if !sm.IsFolderSessionAllowed("/tmp/foo/bar.txt") {
		t.Error("path under allowed folder should be allowed")
	}
	if !sm.IsFolderSessionAllowed("/tmp/foo") {
		t.Error("the exact folder itself should be allowed")
	}
	if !sm.IsFolderSessionAllowed("/tmp/foo/sub/dir/x.txt") {
		t.Error("deep path under allowed folder should be allowed")
	}
	if sm.IsFolderSessionAllowed("/tmp/other/y.txt") {
		t.Error("path outside any allowed folder should not be allowed")
	}
	if sm.IsFolderSessionAllowed("/tmp/foobar/x.txt") {
		t.Error("component-boundary safety: /tmp/foobar should not match /tmp/foo")
	}
	if !sm.IsSecurityBypassApproved() {
		t.Error("with a folder allowlisted, the coarse signal should be true")
	}

	// Dedup: re-adding the same folder doesn't grow the list.
	sm.AddSessionAllowedFolder("/tmp/foo")
	if got := len(sm.SnapshotSessionAllowedFolders()); got != 1 {
		t.Errorf("expected 1 entry after dedup, got %d", got)
	}

	// Snapshot is independent — mutating it doesn't change the manager.
	snap := sm.SnapshotSessionAllowedFolders()
	snap[0] = "/mutated"
	if !sm.IsFolderSessionAllowed("/tmp/foo/x.txt") {
		t.Error("mutating the snapshot should not affect the manager")
	}
}

// TestSessionAllowedFolders_EmptyAndAbsentInputs guards the helpers
// against nil/empty edge cases.
func TestSessionAllowedFolders_EmptyAndAbsentInputs(t *testing.T) {
	sm := NewAgentSecurityManager()
	if sm.IsFolderSessionAllowed("") {
		t.Error("empty path must not be approved")
	}
	sm.AddSessionAllowedFolder("")
	if got := len(sm.SnapshotSessionAllowedFolders()); got != 0 {
		t.Errorf("adding empty folder should be a no-op, got %d entries", got)
	}
}

// TestClassifyPathAccess_NormalizationStripsTrailingSlash confirms
// the classifier doesn't get confused by trailing separators on the
// inputs.
func TestClassifyPathAccess_NormalizationStripsTrailingSlash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix only")
	}
	got := ClassifyPathAccess("/home/alan/proj/", "/home/alan/proj/", "/home/alan", "/home/alan/proj/")
	if got != PathTierWorkspace {
		t.Errorf("trailing slashes broke workspace detection: got %v", got)
	}
}

// TestClassifyPathAccess_Windows covers the C:\ prefix handling.
// Runs only on Windows; on other platforms the prefix list is
// Unix and we'd false-positive.
func TestClassifyPathAccess_Windows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows fixtures")
	}
	workspace := `C:\projects\foo`
	home := `C:\Users\alan`

	// System path on Windows.
	if got := ClassifyPathAccess(`C:\Windows\System32\drivers\etc\hosts`, workspace, home, workspace); got != PathTierSensitive {
		t.Errorf("C:\\Windows path should be sensitive, got %v", got)
	}
	// Workspace.
	if got := ClassifyPathAccess(filepath.Join(workspace, "main.go"), workspace, home, workspace); got != PathTierWorkspace {
		t.Errorf("path inside workspace should be Workspace, got %v", got)
	}
}
