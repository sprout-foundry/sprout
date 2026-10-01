package embedding

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// index_gitdiff.go — the git-diff incremental update path: UpdateFromGitDiff
// and the git/file-priority helpers that decide what to re-embed. Split out
// of index.go.

// UpdateFromGitDiff incrementally updates the index by examining files changed
// since the last index build. It uses git diff to detect modified, added,
// and deleted files. Deleted files have their records removed from the store,
// while changed/new files are re-indexed.
func (m *IndexManager) UpdateFromGitDiff(ctx context.Context, repoRoot string) (*IndexStats, error) {
	start := time.Now()
	stats := &IndexStats{}

	// Cross-process lock to prevent concurrent builds from corrupting the index.
	release, err := m.lockForBuild()
	if release != nil {
		defer release()
	}
	if err == errBuildLocked {
		debugLogf("index: git-diff update skipped — lock held by another process")
		stats.Duration = time.Since(start)
		return stats, nil
	}
	if err != nil {
		return nil, fmt.Errorf("index: acquire build lock: %w", err)
	}

	// Collect deleted files from both staged and unstaged diffs (SHOULD_FIX #8).
	var deletedFiles []string
	if files, err := runGit(repoRoot, "diff", "--name-only", "--diff-filter=D", "--cached"); err == nil {
		deletedFiles = append(deletedFiles, files...)
	}
	if files, err := runGit(repoRoot, "diff", "--name-only", "--diff-filter=D"); err == nil {
		deletedFiles = append(deletedFiles, files...)
	}

	// Filter deleted files to supported extensions only.
	toDelete := make(map[string]bool)
	for _, f := range deletedFiles {
		f = filepath.Clean(f)
		if f == "" || !isSupportedFile(f, m.opts.IndexFileLevel) {
			continue
		}
		toDelete[f] = true
	}

	// Delete records for removed files.
	for f := range toDelete {
		if err := ctx.Err(); err != nil {
			stats.Duration = time.Since(start)
			return stats, fmt.Errorf("index: cancelled")
		}
		if err := m.store.DeleteByFile(f); err != nil {
			debugLogf("index: skipping delete %s: %v", f, err)
			continue
		}
		stats.FilesProcessed++
	}

	// Collect changed files from three git sources.
	var changedFiles []string

	// 1. Staged (cached) changes
	files, err := runGit(repoRoot, "diff", "--name-only", "--cached")
	if err != nil {
		return nil, fmt.Errorf("index: git diff --cached: %w", err)
	}
	changedFiles = append(changedFiles, files...)

	// 2. Working tree (unstaged) changes
	files, err = runGit(repoRoot, "diff", "--name-only")
	if err != nil {
		return nil, fmt.Errorf("index: git diff: %w", err)
	}
	changedFiles = append(changedFiles, files...)

	// 3. Untracked (new) files
	files, err = runGit(repoRoot, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("index: git ls-files: %w", err)
	}
	changedFiles = append(changedFiles, files...)

	// Deduplicate and filter to supported extensions.
	// Skip files that are in the delete list (they've already been handled).
	fileSet := make(map[string]bool)
	for _, f := range changedFiles {
		if f == "" {
			continue
		}
		cleanPath := filepath.Clean(f)
		if !isSupportedFile(f, m.opts.IndexFileLevel) {
			continue
		}
		if toDelete[cleanPath] {
			continue // already deleted
		}
		fileSet[cleanPath] = true
	}

	if len(fileSet) == 0 && len(toDelete) == 0 {
		stats.Duration = time.Since(start)
		return stats, nil
	}

	var errs []string
	for f := range fileSet {
		if err := ctx.Err(); err != nil {
			stats.Duration = time.Since(start)
			return stats, fmt.Errorf("index: cancelled")
		}

		if err := m.UpdateFile(ctx, f); err != nil {
			// The memory floor is system-wide: every remaining file would hit
			// the same wall, so abort rather than emit per-file skip spam.
			if errors.Is(err, errMemFloor) {
				stats.Duration = time.Since(start)
				return stats, fmt.Errorf("index: update aborted: %w", err)
			}
			debugLogf("index: skipping %s: %v", f, err)
			errs = append(errs, f)
			continue
		}
		stats.FilesProcessed++
	}

	if len(errs) > 0 {
		return stats, fmt.Errorf("index: failed to update %d files: %v", len(errs), errs)
	}

	// Update manifest for changed/deleted files.
	if m.opts.ManifestPath != "" {
		m.updateManifestForDiff(fileSet, toDelete)
	}

	stats.Duration = time.Since(start)
	return stats, nil
}

// updateManifestForDiff updates the manifest entries for files that changed
// in a git diff update. It updates mtimes for changed files and removes
// entries for deleted files.
func (m *IndexManager) updateManifestForDiff(updatedFiles map[string]bool, deletedFiles map[string]bool) {
	manifest, err := LoadManifest(m.opts.ManifestPath)
	if err != nil || manifest == nil {
		manifest = &BuildManifest{
			Files:     make(map[string]int64),
			ModelHash: m.provider.ModelHash(),
		}
	}

	for f := range updatedFiles {
		mtime, e := fileModTime(f)
		if e == nil {
			manifest.Files[f] = mtime
		}
	}
	for f := range deletedFiles {
		delete(manifest.Files, f)
	}

	if err := SaveManifest(m.opts.ManifestPath, manifest); err != nil {
		debugLogf("index: update manifest after diff failed (non-fatal): %v", err)
	}
}

// runGit executes a git command in the given directory and returns the output
// split into non-empty lines.
func runGit(dir string, args ...string) ([]string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

// buildFilePriority assigns each file a priority tier using git recency.
// Tier 0 = modified within 7 days, Tier 1 = modified within 30 days,
// Tier 2 = older or not found in git history.
// Returns a map of file path → tier. If git fails, returns an empty map
// so the caller falls back to pure length-based ordering.
func buildFilePriority(repoRoot string, files []string) map[string]int {
	if repoRoot == "" {
		return nil
	}

	recent7, err := runGit(repoRoot, "log", "--name-only", "--format=", "--since=7 days ago")
	if err != nil {
		return nil
	}
	recent30, err := runGit(repoRoot, "log", "--name-only", "--format=", "--since=30 days ago")
	if err != nil {
		return nil
	}

	set7 := make(map[string]bool)
	for _, f := range recent7 {
		set7[filepath.Clean(f)] = true
	}
	set30 := make(map[string]bool)
	for _, f := range recent30 {
		set30[filepath.Clean(f)] = true
	}

	result := make(map[string]int, len(files))
	for _, f := range files {
		rel, err := filepath.Rel(repoRoot, f)
		if err != nil {
			result[f] = 2
			continue
		}
		clean := filepath.Clean(rel)
		switch {
		case set7[clean]:
			result[f] = 0
		case set30[clean]:
			result[f] = 1
		default:
			result[f] = 2
		}
	}
	return result
}

// uniqueFiles extracts the distinct file paths from a slice of CodeUnits.
func uniqueFiles(units []CodeUnit) []string {
	seen := make(map[string]bool)
	var files []string
	for _, u := range units {
		if !seen[u.File] {
			seen[u.File] = true
			files = append(files, u.File)
		}
	}
	return files
}

// isSupportedFile returns true if the file path has a supported source-code extension.
// When fileLevel is true, also includes non-code file extensions.
func isSupportedFile(path string, fileLevel bool) bool {
	ext := filepath.Ext(path)

	// Always support code extensions
	codeExts := map[string]bool{
		".go": true, ".ts": true, ".tsx": true,
		".js": true, ".jsx": true, ".mjs": true, ".py": true,
	}
	if codeExts[ext] {
		return true
	}

	// When file-level indexing is enabled, also support non-code extensions
	if fileLevel {
		if IsSupportedIndexableFile(path) {
			return true
		}
	}

	return false
}
