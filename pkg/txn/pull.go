package txn

import (
	"context"
	"fmt"
	"os"
)

// BuildStatus reports shape #3: the working-tree state a transaction is
// about to run against. It is strictly read-only — one `git status` and one
// branch probe, no fetch, no write.
//
// "Not a git repository" is a reportable state (InGitRepo=false, empty
// lists, timestamp set), not an error; only a catastrophic failure
// (unreadable directory, broken git, corrupt index) returns a non-nil error.
// A porcelain failure inside a real repo is catastrophic too: a status the
// caller cannot trust must not be laundered into "clean".
func BuildStatus(ctx context.Context, workdir string) (Status, error) {
	status := newStatus()

	dir, err := resolveWorkdir(workdir)
	if err != nil {
		return status, err
	}

	inRepo, err := detectTxnGitRepo(ctx, dir)
	if err != nil {
		return status, err
	}
	if !inRepo {
		return status, nil
	}
	status.InGitRepo = true
	status.Branch = txnBranch(ctx, dir)

	changes, err := collectTreeChanges(ctx, dir)
	if err != nil {
		return status, err
	}
	for _, change := range changes {
		switch {
		case change.untracked:
			status.UntrackedFiles = append(status.UntrackedFiles, change.path)
		case change.deleted:
			status.DeletedFiles = append(status.DeletedFiles, change.path)
		default:
			status.DirtyFiles = append(status.DirtyFiles, change.path)
		}
	}
	status.TotalChanges = len(status.DirtyFiles) + len(status.UntrackedFiles) + len(status.DeletedFiles)
	return status, nil
}

// BuildPull computes shape #1 from the working tree: every dirty tracked
// file (contents base64-encoded) plus every untracked file, with deleted
// tracked files listed in "deletes". It NEVER touches the working tree —
// no add, no stash, no reset — so a pull is safe to run at any point in a
// transaction, including after a failed run.
//
// Caps are honored by omission: an over-cap entry lands in "skipped" with
// its reason and sets "truncated", so the platform knows the manifest does
// not fully describe the tree and can fall back to a narrower sync. Paths
// that cannot be read (permissions, a symlink, a directory) are skipped the
// same way rather than failing the manifest.
func BuildPull(ctx context.Context, workdir string) (DeltaManifest, error) {
	manifest := newManifest()

	dir, err := resolveWorkdir(workdir)
	if err != nil {
		return manifest, err
	}

	inRepo, err := detectTxnGitRepo(ctx, dir)
	if err != nil {
		return manifest, err
	}
	if !inRepo {
		// Reportable state: an empty manifest for a non-repo directory.
		return manifest, nil
	}

	manifest.Base = DeltaBase{
		GitSha: txnHeadSha(ctx, dir),
		Client: TxnClientContainer,
	}

	changes, err := collectTreeChanges(ctx, dir)
	if err != nil {
		return manifest, err
	}

	// git reports paths relative to the repo ROOT even when invoked from a
	// subdirectory, so entries join onto the toplevel — not onto dir.
	root := txnRepoRoot(ctx, dir)
	if root == "" {
		return manifest, fmt.Errorf("txn: %s: failed to resolve the repository root", dir)
	}

	skipped := newSkipRecorder()
	count, total := 0, 0
	for _, change := range changes {
		if change.deleted {
			manifest.Deletes = append(manifest.Deletes, change.path)
			continue
		}
		entry, reason := readPullEntry(root, change.path)
		if reason == "" && count >= MaxFileCount {
			reason = SkipReasonExceedsFileCount
		}
		if reason == "" && total+entry.Size > MaxTotalBytes {
			reason = SkipReasonExceedsTotal
		}
		if reason != "" {
			skipped.skip(change.path, reason)
			continue
		}
		manifest.Files = append(manifest.Files, entry)
		count++
		total += entry.Size
	}

	manifest.Skipped = skipped.entries
	manifest.Truncated = skipped.any
	return manifest, nil
}

// readPullEntry reads one working-tree file into a manifest entry. Symlinks
// are skipped rather than followed — reading through one could exfiltrate a
// file outside the workdir into the manifest.
func readPullEntry(root, rel string) (DeltaFile, string) {
	if reason := validateRelPath(rel); reason != "" {
		return DeltaFile{}, reason
	}
	target, err := secureJoin(root, rel)
	if err != nil {
		if err == errEscapesRoot {
			return DeltaFile{}, SkipReasonSymlinkEscape
		}
		return DeltaFile{}, SkipReasonReadFailed
	}
	info, err := os.Lstat(target)
	if err != nil {
		return DeltaFile{}, SkipReasonReadFailed
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return DeltaFile{}, SkipReasonSymlink
	}
	if !info.Mode().IsRegular() {
		return DeltaFile{}, SkipReasonNotAFile
	}
	if info.Size() > int64(MaxFileBytes) {
		return DeltaFile{Path: rel, Size: int(info.Size()), Mode: formatFileMode(info.Mode())}, SkipReasonExceedsPerFile
	}
	content, err := os.ReadFile(target)
	if err != nil {
		return DeltaFile{}, SkipReasonReadFailed
	}
	return DeltaFile{
		Path:          rel,
		ContentBase64: encodeBase64(content),
		Size:          len(content),
		Mode:          formatFileMode(info.Mode()),
	}, ""
}
