//go:build !js

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

// literalSearchOpts mirrors the search_files arguments.
type literalSearchOpts struct {
	Directory     string
	Pattern       string
	FileGlob      string
	CaseSensitive bool
	MaxResults    int
	MaxBytes      int
	// MaxFiles caps DISTINCT matching files rather than total match lines.
	//
	// Capping on lines terminates the walk wherever the quota happens to fill,
	// which is a position in the directory tree, not a relevance boundary: a
	// search for "backoff" with a 30-line cap exhausted it in cmd/ and
	// packages/ and never reached pkg/mcp/, so the implementation was
	// unreachable by its own name. When set, MaxResults becomes a per-file line
	// cap instead of a global one.
	MaxFiles int
}

// runLiteralSearch walks directory and returns structured regex matches.
//
// Extracted from searchFilesHandler.Execute so the fused `search` tool and
// `search_files` share one implementation. Two independent walkers would drift
// on the things that are easy to get subtly different — skip-dir rules, binary
// detection, the early-exit caps — and disagree about what the repository
// contains, which is the same failure this codebase already had between the
// repo map and the embedding index.
func runLiteralSearch(ctx context.Context, opts literalSearchOpts) ([]literalHit, error) {
	if opts.MaxResults <= 0 {
		opts.MaxResults = 50
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = 102400
	}

	compiled, err := compileSearchPattern(opts.Pattern, opts.CaseSensitive)
	if err != nil {
		return nil, fmt.Errorf("invalid search pattern %q: %w", opts.Pattern, err)
	}

	matcher := newGlobMatcher(opts.FileGlob)
	var hits []literalHit
	totalBytes := 0
	filesMatched := 0

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
		if totalBytes >= opts.MaxBytes {
			return filepath.SkipAll
		}
		if opts.MaxFiles > 0 {
			if filesMatched >= opts.MaxFiles {
				return filepath.SkipAll
			}
		} else if len(hits) >= opts.MaxResults {
			return filepath.SkipAll
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

		fileHits, used, err := searchFileStructured(path, compiled)
		if err != nil {
			return nil
		}
		if len(fileHits) == 0 {
			return nil
		}
		if opts.MaxFiles > 0 {
			filesMatched++
			// Keep the file's first few matches so one verbose file cannot
			// dominate the byte budget and cut the walk short.
			if len(fileHits) > opts.MaxResults {
				fileHits = fileHits[:opts.MaxResults]
			}
		}
		totalBytes += used
		hits = append(hits, fileHits...)
		return nil
	})

	if walkErr != nil && len(hits) == 0 {
		return nil, walkErr
	}
	return hits, nil
}

// searchFileStructured is searchFile's structured counterpart.
func searchFileStructured(path string, pattern *regexp.Regexp) ([]literalHit, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	var hits []literalHit
	total := 0
	for i, line := range strings.Split(string(data), "\n") {
		if pattern.MatchString(line) {
			h := literalHit{Path: path, Line: i + 1, Text: line}
			hits = append(hits, h)
			total += len(h.String())
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
