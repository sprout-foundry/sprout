package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// literalHit is one regex match, kept structured so callers can rank and merge
// rather than only print. search_files formats these directly; the fused
// `search` tool ranks them against semantic hits.
type literalHit struct {
	Path string
	Line int
	Text string
}

func (h literalHit) String() string {
	return fmt.Sprintf("%s:%d:%s", h.Path, h.Line, h.Text)
}

// literalResult carries the matches plus what was left out, so callers can say
// "showing 20 of 340 files" instead of presenting a truncated list as if it
// were the whole answer.
type literalResult struct {
	Hits         []literalHit
	FilesMatched int
	FilesShown   int
	Truncated    bool
}

// literalSearchOpts mirrors the search_files arguments.
type literalSearchOpts struct {
	Directory     string
	Pattern       string
	FileGlob      string
	CaseSensitive bool

	// MaxFiles caps how many distinct matching files are RETURNED. The walk
	// itself always completes — see the note in runLiteralSearch.
	MaxFiles int
	// MaxPerFile caps match lines kept per file, so one verbose file cannot
	// crowd out every other result. 0 means unlimited.
	MaxPerFile int
	// MaxBytes bounds retained match text as a memory valve, not a scan limit.
	MaxBytes int
}

// runLiteralSearch walks directory and returns structured regex matches.
//
// The walk ALWAYS completes. It previously stopped via filepath.SkipAll once a
// match quota filled, which truncates at a position in the directory tree
// rather than at a relevance boundary: searching this repository for "atomic"
// exhausted a 50-file quota inside pkg/agent/ and never reached
// pkg/workflow/checkpoint.go, so WriteFileAtomic was unfindable by its own
// name. Any cap applied during the walk has that property; only a cap applied
// after it is position-independent.
//
// Completing the walk costs about what the old capped walk cost. Measured on
// this repository (3,335 files): ~630ms either way, because the time goes to
// reading files, not to matching them — the 50-file cap was only faster when it
// stopped early, which is precisely when it returned the wrong answer.
func runLiteralSearch(ctx context.Context, opts literalSearchOpts) (literalResult, error) {
	if opts.MaxFiles <= 0 {
		opts.MaxFiles = 50
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = 512 * 1024
	}

	compiled, err := compileSearchPattern(opts.Pattern, opts.CaseSensitive)
	if err != nil {
		return literalResult{}, fmt.Errorf("invalid search pattern %q: %w", opts.Pattern, err)
	}

	matcher := newGlobMatcher(opts.FileGlob)
	var res literalResult
	totalBytes := 0

	walkErr := walkDirCompat(opts.Directory, func(path string, info os.DirEntry, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if shouldSkipDir(path) {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !isTextFile(path) {
			return nil
		}
		if matcher != nil && !matcher.Match(path) {
			return nil
		}

		fileHits, used, err := searchFileStructured(path, compiled, opts.MaxPerFile)
		if err != nil || len(fileHits) == 0 {
			return nil
		}

		res.FilesMatched++

		// Retention, not scanning, is what the caps bound. The walk continues
		// either way so FilesMatched stays a true total.
		if res.FilesShown >= opts.MaxFiles || totalBytes >= opts.MaxBytes {
			res.Truncated = true
			return nil
		}
		res.FilesShown++
		totalBytes += used
		res.Hits = append(res.Hits, fileHits...)
		return nil
	})

	if walkErr != nil && len(res.Hits) == 0 {
		return literalResult{}, walkErr
	}
	return res, nil
}

// searchFileStructured returns up to maxPerFile matching lines from path.
//
// A whole-buffer pattern.Match pre-check was tried here to skip line-splitting
// for non-matching files; it measured neutral (the cost is file I/O, not
// matching) and was removed rather than kept as unearned complexity.
func searchFileStructured(path string, pattern *regexp.Regexp, maxPerFile int) ([]literalHit, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	var hits []literalHit
	total := 0
	for i, line := range strings.Split(string(data), "\n") {
		if !pattern.MatchString(line) {
			continue
		}
		h := literalHit{Path: path, Line: i + 1, Text: line}
		hits = append(hits, h)
		total += len(h.String())
		if maxPerFile > 0 && len(hits) >= maxPerFile {
			break
		}
	}
	return hits, total, nil
}

// resolveSearchDirectory applies the same workspace-root and traversal rules
// search_files uses. "." must resolve against the workspace root, not the
// process CWD: the daemon's CWD is the home directory, and walking it triggers
// macOS permission prompts and takes minutes.
func resolveSearchDirectory(directory, workspaceRoot string) (string, error) {
	if directory == "" {
		directory = "."
	}
	if directory == "." && workspaceRoot != "" {
		directory = workspaceRoot
	}
	if directory != "." && strings.Contains(directory, "..") {
		return "", fmt.Errorf("invalid search directory: %q", directory)
	}
	return directory, nil
}
