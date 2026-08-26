//go:build !js

package txn

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------- porcelain parsing ----------

func TestParseTxnPorcelain_Table(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []treeChange
	}{
		{"empty", "", []treeChange{}},
		{
			"modified",
			" M a.go\x00",
			[]treeChange{{path: "a.go"}},
		},
		{
			"staged modified",
			"M  a.go\x00",
			[]treeChange{{path: "a.go"}},
		},
		{
			"untracked",
			"?? b.out\x00",
			[]treeChange{{path: "b.out", untracked: true}},
		},
		{
			"deleted unstaged",
			" D c.go\x00",
			[]treeChange{{path: "c.go", deleted: true}},
		},
		{
			"deleted staged",
			"D  c.go\x00",
			[]treeChange{{path: "c.go", deleted: true}},
		},
		{
			"rename consumes the original field",
			"R  new.go\x00old.go\x00",
			[]treeChange{{path: "new.go"}},
		},
		{
			"copy consumes the original field",
			"C  new.go\x00old.go\x00",
			[]treeChange{{path: "new.go"}},
		},
		{
			"quoted path stays literal under -z",
			"?? \"quoted txt\"\x00",
			[]treeChange{{path: `"quoted txt"`, untracked: true}},
		},
		{
			"tab separated XY form",
			"?? b.out\x00 M a.go\x00",
			[]treeChange{{path: "b.out", untracked: true}, {path: "a.go"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseTxnPorcelain(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("parsed %d entries, want %d: %+v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("entry %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestParseTxnPorcelain_PathWithSpacesAndNewline(t *testing.T) {
	// -z output never quotes or escapes, so a filename containing a
	// newline arrives intact as one NUL-terminated record.
	in := "?? has newline\ninside.txt\x00"
	got := parseTxnPorcelain(in)
	if len(got) != 1 || got[0].path != "has newline\ninside.txt" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseTxnPorcelain_MalformedShortRecordSkipped(t *testing.T) {
	got := parseTxnPorcelain("??\x00xy\x00 M ok.go\x00")
	if len(got) != 1 || got[0].path != "ok.go" {
		t.Fatalf("got %+v, want only ok.go", got)
	}
}

// ---------- BuildStatus ----------

func TestBuildStatus_CleanRepo(t *testing.T) {
	dir := newTxnTestRepo(t)
	status, err := BuildStatus(context.Background(), dir)
	if err != nil {
		t.Fatalf("BuildStatus: %v", err)
	}
	if !status.InGitRepo || status.Branch != "main" {
		t.Fatalf("status = %+v", status)
	}
	if len(status.DirtyFiles) != 0 || len(status.UntrackedFiles) != 0 || len(status.DeletedFiles) != 0 {
		t.Fatalf("clean repo reported changes: %+v", status)
	}
	if status.TotalChanges != 0 {
		t.Fatalf("total_changes = %d, want 0", status.TotalChanges)
	}
	if status.Timestamp == "" {
		t.Fatal("timestamp must always be set")
	}
	if _, err := time.Parse(time.RFC3339, status.Timestamp); err != nil {
		t.Fatalf("timestamp %q is not RFC3339: %v", status.Timestamp, err)
	}
}

func TestBuildStatus_SplitsDirtyUntrackedDeleted(t *testing.T) {
	dir := newTxnTestRepo(t)
	txnWriteFile(t, dir, "README.md", "modified")
	txnWriteFile(t, dir, "src/new.go", "untracked")
	// A committed second file gives us a real delete.
	txnWriteFile(t, dir, "extra.txt", "to be deleted\n")
	txnTestGit(t, dir, "add", "extra.txt")
	txnTestGit(t, dir, "commit", "-m", "add extra")
	if err := os.Remove(filepath.Join(dir, "extra.txt")); err != nil {
		t.Fatal(err)
	}

	status, err := BuildStatus(context.Background(), dir)
	if err != nil {
		t.Fatalf("BuildStatus: %v", err)
	}
	if !status.InGitRepo {
		t.Fatal("in_git_repo = false")
	}
	assertListEq(t, "dirty_files", status.DirtyFiles, []string{"README.md"})
	assertListEq(t, "untracked_files", status.UntrackedFiles, []string{"src/new.go"})
	assertListEq(t, "deleted_files", status.DeletedFiles, []string{"extra.txt"})
	if status.TotalChanges != 3 {
		t.Fatalf("total_changes = %d, want 3", status.TotalChanges)
	}
}

func TestBuildStatus_FromSubdirectoryReportsRepoRootPaths(t *testing.T) {
	dir := newTxnTestRepo(t)
	txnWriteFile(t, dir, "src/new.go", "untracked")

	sub := filepath.Join(dir, "src")
	status, err := BuildStatus(context.Background(), sub)
	if err != nil {
		t.Fatalf("BuildStatus: %v", err)
	}
	// git reports repo-ROOT-relative paths regardless of the cwd.
	assertListEq(t, "untracked_files", status.UntrackedFiles, []string{"src/new.go"})
}

func TestBuildStatus_NotARepoIsReportable(t *testing.T) {
	status, err := BuildStatus(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("not-a-repo must be reportable, got %v", err)
	}
	if status.InGitRepo {
		t.Fatal("in_git_repo = true")
	}
	if status.Branch != "" || status.TotalChanges != 0 {
		t.Fatalf("status = %+v, want zeroed fields", status)
	}
	for name, list := range map[string][]string{
		"dirty_files":     status.DirtyFiles,
		"untracked_files": status.UntrackedFiles,
		"deleted_files":   status.DeletedFiles,
	} {
		if list == nil {
			t.Fatalf("%s must be an empty array, not null", name)
		}
	}
}

func TestBuildStatus_RenameReportsNewPathOnly(t *testing.T) {
	dir := newTxnTestRepo(t)
	if err := os.Rename(filepath.Join(dir, "README.md"), filepath.Join(dir, "RENAMED.md")); err != nil {
		t.Fatal(err)
	}
	txnTestGit(t, dir, "add", "-A")

	status, err := BuildStatus(context.Background(), dir)
	if err != nil {
		t.Fatalf("BuildStatus: %v", err)
	}
	if len(status.DeletedFiles) != 0 {
		t.Fatalf("a staged rename must not surface as a delete: %+v", status.DeletedFiles)
	}
	found := false
	for _, p := range status.DirtyFiles {
		if p == "RENAMED.md" {
			found = true
		}
		if p == "README.md" {
			t.Fatalf("the consumed original path leaked into dirty_files: %+v", status.DirtyFiles)
		}
	}
	if !found {
		t.Fatalf("RENAMED.md missing from dirty_files: %+v", status.DirtyFiles)
	}
}

func TestBuildStatus_CatastrophicFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root — a 0000-mode dir would still be readable")
	}
	base := t.TempDir()
	bad := filepath.Join(base, "unreadable")
	if err := os.MkdirAll(bad, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bad, 0o755) })

	if _, err := BuildStatus(context.Background(), bad); err == nil {
		t.Fatal("expected an error for an unreadable directory")
	}
}

// ---------- BuildPull ----------

func TestBuildPull_DirtyUntrackedDeleted(t *testing.T) {
	dir := newTxnTestRepo(t)
	txnWriteFile(t, dir, "tracked.txt", "modified content")
	txnWriteFile(t, dir, "src/new.go", "package src\n")
	if err := os.Remove(filepath.Join(dir, "README.md")); err != nil {
		t.Fatal(err)
	}

	manifest, err := BuildPull(context.Background(), dir)
	if err != nil {
		t.Fatalf("BuildPull: %v", err)
	}
	if manifest.Truncated {
		t.Fatalf("truncated = true on a fully-transferable tree: %+v", manifest.Skipped)
	}
	assertListEq(t, "deletes", manifest.Deletes, []string{"README.md"})
	if len(manifest.Files) != 2 {
		t.Fatalf("files = %+v, want tracked.txt and src/new.go", manifest.Files)
	}
	byPath := map[string]DeltaFile{}
	for _, f := range manifest.Files {
		byPath[f.Path] = f
	}
	if got := decode(t, byPath["tracked.txt"].ContentBase64); got != "modified content" {
		t.Fatalf("tracked.txt content = %q", got)
	}
	if got := decode(t, byPath["src/new.go"].ContentBase64); got != "package src\n" {
		t.Fatalf("src/new.go content = %q", got)
	}
	if byPath["tracked.txt"].Mode != "0644" {
		t.Fatalf("mode = %q, want 0644", byPath["tracked.txt"].Mode)
	}
	if byPath["tracked.txt"].Size != len("modified content") {
		t.Fatalf("size = %d, want %d", byPath["tracked.txt"].Size, len("modified content"))
	}
	if manifest.Base.Client != TxnClientContainer {
		t.Fatalf("base.client = %q, want container", manifest.Base.Client)
	}
	if manifest.Base.GitSha == "" {
		t.Fatal("base.git_sha must be the HEAD sha of a committed repo")
	}
}

func TestBuildPull_DoesNotTouchWorkingTree(t *testing.T) {
	dir := newTxnTestRepo(t)
	txnWriteFile(t, dir, "untracked.txt", "u")
	before := statusOf(t, dir)

	manifest, err := BuildPull(context.Background(), dir)
	if err != nil {
		t.Fatalf("BuildPull: %v", err)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Path != "untracked.txt" {
		t.Fatalf("files = %+v", manifest.Files)
	}
	if after := statusOf(t, dir); before != after {
		t.Fatalf("BuildPull changed the tree:\nbefore %q\nafter  %q", before, after)
	}
	// The staged index must be untouched too: an untracked file stays
	// untracked, never auto-added.
	staged := txnTestGit(t, dir, "diff", "--cached", "--name-only")
	if strings.TrimSpace(staged) != "" {
		t.Fatalf("BuildPull staged files: %q", staged)
	}
}

func TestBuildPull_NotARepoIsEmpty(t *testing.T) {
	manifest, err := BuildPull(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("not-a-repo must be reportable, got %v", err)
	}
	if len(manifest.Files) != 0 || len(manifest.Deletes) != 0 || manifest.Truncated {
		t.Fatalf("manifest = %+v, want empty", manifest)
	}
	if manifest.Files == nil || manifest.Deletes == nil || manifest.Skipped == nil {
		t.Fatal("lists must be empty arrays, not null")
	}
}

func TestBuildPull_PerFileCapSkippedAndTruncated(t *testing.T) {
	defer withCap(t, func(c *capSet) { c.file = 4 })()

	dir := newTxnTestRepo(t)
	txnWriteFile(t, dir, "big.bin", "12345678")
	txnWriteFile(t, dir, "small.txt", "ok")

	manifest, err := BuildPull(context.Background(), dir)
	if err != nil {
		t.Fatalf("BuildPull: %v", err)
	}
	if !manifest.Truncated {
		t.Fatal("truncated = false, want true when an entry was skipped")
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Path != "small.txt" {
		t.Fatalf("files = %+v", manifest.Files)
	}
	if len(manifest.Skipped) != 1 || manifest.Skipped[0].Path != "big.bin" ||
		manifest.Skipped[0].Reason != SkipReasonExceedsPerFile {
		t.Fatalf("skipped = %+v", manifest.Skipped)
	}
}

func TestBuildPull_TotalCapAndCountCap(t *testing.T) {
	defer withCap(t, func(c *capSet) { c.total = 6; c.count = 3 })()

	dir := newTxnTestRepo(t)
	txnWriteFile(t, dir, "a.txt", "aaaa")
	txnWriteFile(t, dir, "b.txt", "bbbb")
	txnWriteFile(t, dir, "c.txt", "cccc")
	txnWriteFile(t, dir, "d.txt", "dddd")

	manifest, err := BuildPull(context.Background(), dir)
	if err != nil {
		t.Fatalf("BuildPull: %v", err)
	}
	if len(manifest.Files) != 1 {
		t.Fatalf("files = %+v, want only a.txt (total cap trips on b.txt)", manifest.Files)
	}
	if !manifest.Truncated {
		t.Fatal("truncated = false, want true")
	}
	reasons := map[string]string{}
	for _, s := range manifest.Skipped {
		reasons[s.Path] = s.Reason
	}
	// b.txt is the first entry to trip the total cap (4+4 > 6); every
	// later entry is refused by the still-exceeded total.
	if reasons["b.txt"] != SkipReasonExceedsTotal {
		t.Fatalf("b.txt reason = %q, want exceeds_total_cap", reasons["b.txt"])
	}
	for _, p := range []string{"c.txt", "d.txt"} {
		if reasons[p] != SkipReasonExceedsTotal {
			t.Fatalf("%s reason = %q, want exceeds_total_cap", p, reasons[p])
		}
	}
}

func TestBuildPull_FileCountCap(t *testing.T) {
	defer withCap(t, func(c *capSet) { c.count = 2 })()

	dir := newTxnTestRepo(t)
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		txnWriteFile(t, dir, name, "x")
	}

	manifest, err := BuildPull(context.Background(), dir)
	if err != nil {
		t.Fatalf("BuildPull: %v", err)
	}
	if len(manifest.Files) != 2 {
		t.Fatalf("files = %+v, want the two allowed entries", manifest.Files)
	}
	if !manifest.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if len(manifest.Skipped) != 1 || manifest.Skipped[0].Path != "c.txt" ||
		manifest.Skipped[0].Reason != SkipReasonExceedsFileCount {
		t.Fatalf("skipped = %+v, want c.txt exceeds_file_count_cap", manifest.Skipped)
	}
}

func TestBuildPull_SymlinkNotFollowed(t *testing.T) {
	dir := newTxnTestRepo(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "leak")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	manifest, err := BuildPull(context.Background(), dir)
	if err != nil {
		t.Fatalf("BuildPull: %v", err)
	}
	for _, f := range manifest.Files {
		if f.Path == "leak" {
			t.Fatal("a symlink was followed into the manifest")
		}
		if strings.Contains(decode(t, f.ContentBase64), "secret") {
			t.Fatalf("content escaped the workdir: %+v", f.Path)
		}
	}
	if !manifest.Truncated {
		t.Fatalf("the skipped symlink must set truncated: %+v", manifest.Skipped)
	}
}

func TestBuildPull_RoundTripsThroughApplyDelta(t *testing.T) {
	// The three-phase transaction, end to end: push into a clean repo, run
	// nothing, pull back — the manifest must describe exactly what landed.
	dir := newTxnTestRepo(t)
	pushed := manifestOf(
		DeltaFile{Path: "src/main.go", ContentBase64: b64("package main\n")},
		DeltaFile{Path: "notes.txt", ContentBase64: b64("hello\n")},
	)
	pushed.Deletes = []string{"README.md"}
	if _, err := ApplyDelta(context.Background(), dir, pushed); err != nil {
		t.Fatalf("ApplyDelta: %v", err)
	}

	manifest, err := BuildPull(context.Background(), dir)
	if err != nil {
		t.Fatalf("BuildPull: %v", err)
	}
	if manifest.Truncated {
		t.Fatalf("truncated = true: %+v", manifest.Skipped)
	}
	assertListEq(t, "deletes", manifest.Deletes, []string{"README.md"})
	byPath := map[string]DeltaFile{}
	for _, f := range manifest.Files {
		byPath[f.Path] = f
	}
	for _, want := range []struct{ path, content string }{
		{"src/main.go", "package main\n"},
		{"notes.txt", "hello\n"},
	} {
		got, ok := byPath[want.path]
		if !ok {
			t.Fatalf("%s missing from the pull manifest: %+v", want.path, manifest.Files)
		}
		if decode(t, got.ContentBase64) != want.content {
			t.Fatalf("%s content = %q, want %q", want.path, decode(t, got.ContentBase64), want.content)
		}
	}
}

// ---------- helpers ----------

func assertListEq(t *testing.T, name string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
	}
}

func decode(t *testing.T, encoded string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("manifest content is not valid base64: %v", err)
	}
	return string(raw)
}

func statusOf(t *testing.T, dir string) string {
	t.Helper()
	return txnTestGit(t, dir, "status", "--porcelain", "-uall")
}
