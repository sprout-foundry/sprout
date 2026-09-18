// Cross-session recovery from the persisted history store.
//
// The session-buffer recovery paths (agent.ChangeTracker) can only
// restore changes still held in memory for the current session. Once a
// session ends, recovery must come from the on-disk history store,
// which persists every change's original/updated payload as base64
// files. This file exposes the smallest read-only API the agent needs
// to serve those restores.
package history

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pmezard/go-difflib/difflib"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// PersistedOriginal holds the pre-change content of one file from the
// history store.
type PersistedOriginal struct {
	Filename string
	Original string // pre-change content
	New      string // post-change content
	Status   string // history status of the newest contributing record
}

// FindPersistedOriginal returns the OLDEST recorded change for filename
// that still has recoverable original content. Later records for the
// same file contribute nothing: reverting to the oldest pre-change
// state matches the session-buffer recovery semantics
// (resolveEarliestRecoveryTarget). found=false means the store holds no
// recoverable original for the file (never tracked, or content pruned
// by retention).
//
// The scan is metadata-first: it walks change directories reading only
// metadata.json, and reads the (possibly large) base64 content files
// only for directories matching the target path, oldest first, stopping
// at the first recoverable original. fetchAllChanges here would read
// and base64-decode the ENTIRE store — every file ever changed — to
// serve one path, which made the WebUI diff fallback noticeably slow on
// workspaces with a long history.
func FindPersistedOriginal(filename string) (PersistedOriginal, bool, error) {
	if err := ensureChangesDirs(); err != nil {
		return PersistedOriginal{}, false, fmt.Errorf("get changes directory: %w", err)
	}

	absTarget, err := filepath.Abs(filename)
	if err != nil {
		return PersistedOriginal{}, false, err
	}

	entries, err := os.ReadDir(GetChangesDir())
	if err != nil {
		if os.IsNotExist(err) {
			return PersistedOriginal{}, false, nil
		}
		return PersistedOriginal{}, false, fmt.Errorf("failed to read changes directory: %w", err)
	}

	type candidate struct {
		changeDir string
		filename  string
		status    string
		timestamp time.Time
	}
	var matches []candidate
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		changeDir := filepath.Join(GetChangesDir(), entry.Name())
		metadataBytes, err := filesystem.ReadFileBytes(filepath.Join(changeDir, metadataFile))
		if err != nil {
			continue // unreadable metadata — same skip rules as fetchAllChanges
		}
		var metadata ChangeMetadata
		if jsonErr := json.Unmarshal(metadataBytes, &metadata); jsonErr != nil {
			continue
		}
		chAbs, pathErr := filepath.Abs(metadata.Filename)
		if pathErr != nil || chAbs != absTarget {
			continue // different file — content files never touched
		}
		matches = append(matches, candidate{
			changeDir: changeDir,
			filename:  metadata.Filename,
			status:    metadata.Status,
			timestamp: metadata.Timestamp,
		})
	}
	// Oldest first (matches the previous full-scan semantics).
	sort.Slice(matches, func(i, j int) bool { return matches[i].timestamp.Before(matches[j].timestamp) })

	for _, m := range matches {
		original, updated, readErr := readPersistedContentPair(m.changeDir, m.filename)
		if readErr != nil {
			continue // unreadable/pruned payload — try the next-oldest record
		}
		if !isRecoverableStoredContent(original) {
			continue
		}
		return PersistedOriginal{
			Filename: m.filename,
			Original: original,
			New:      updated,
			Status:   m.status,
		}, true, nil
	}
	return PersistedOriginal{}, false, nil
}

// readPersistedContentPair loads the base64-encoded original/updated
// content files for one change directory, using the same filename
// mangling and legacy-plain-fallback as fetchAllChanges.
func readPersistedContentPair(changeDir, filename string) (original, updated string, err error) {
	safe := strings.ReplaceAll(filename, "/", "_")
	safe = strings.ReplaceAll(safe, "\\", "_")

	originalBytes, err := filesystem.ReadFileBytes(filepath.Join(changeDir, safe+originalSuffix))
	if err != nil {
		return "", "", fmt.Errorf("read original: %w", err)
	}
	updatedBytes, err := filesystem.ReadFileBytes(filepath.Join(changeDir, safe+updatedSuffix))
	if err != nil {
		return "", "", fmt.Errorf("read updated: %w", err)
	}

	originalDecoded, decErr := base64.StdEncoding.DecodeString(string(originalBytes))
	if decErr != nil {
		originalDecoded = originalBytes // legacy plain-text payload
	}
	updatedDecoded, decErr := base64.StdEncoding.DecodeString(string(updatedBytes))
	if decErr != nil {
		updatedDecoded = updatedBytes
	}
	return string(originalDecoded), string(updatedDecoded), nil
}

// isRecoverableStoredContent reports whether a stored OriginalCode is
// real content: non-empty and not the redaction marker. Empty original
// means the store recorded a create (nothing to restore); the marker
// means the content was withheld.
func isRecoverableStoredContent(original string) bool {
	return original != "" && original != RedactedContentMarker
}

// UnifiedDiffFor renders a unified diff from stored before/after
// content. Kept next to PersistedOriginal so consumers of the
// persisted-diff fallback don't need their own diff plumbing.
//
// Input and output are bounded exactly like the agent-side
// buildUnifiedDiff (see pkg/agent maxDiffInputLines/maxDiffOutputLines):
// stored payloads are untracked in size, and an unbounded Myers diff on
// a large rewrite blocks the WebUI diff endpoint.
func UnifiedDiffFor(path, before, after string) string {
	if before == after {
		return "(no textual difference)"
	}
	before = historyTruncateLines(before, maxHistoryDiffInputLines, "before-side")
	after = historyTruncateLines(after, maxHistoryDiffInputLines, "after-side")
	d := difflib.UnifiedDiff{
		A:        difflib.SplitLines(before),
		B:        difflib.SplitLines(after),
		FromFile: path + " (before change)",
		ToFile:   path + " (after change)",
		Context:  3,
	}
	out, err := difflib.GetUnifiedDiffString(d)
	if err != nil {
		return fmt.Sprintf("(diff failed: %v)", err)
	}
	return historyTruncateDiffLines(out, maxHistoryDiffOutputLines)
}

const (
	maxHistoryDiffInputLines  = 20000
	maxHistoryDiffOutputLines = 4000
)

func historyTruncateLines(s string, max int, what string) string {
	lines := strings.SplitAfter(s, "\n")
	if len(lines) <= max {
		return s
	}
	kept := strings.Join(lines[:max], "")
	if !strings.HasSuffix(kept, "\n") {
		kept += "\n"
	}
	return kept + fmt.Sprintf("... (%s truncated at %d lines for diffing)\n", what, max)
}

func historyTruncateDiffLines(diff string, max int) string {
	lines := strings.SplitAfter(diff, "\n")
	if len(lines) <= max {
		return diff
	}
	kept := strings.Join(lines[:max], "")
	if !strings.HasSuffix(kept, "\n") {
		kept += "\n"
	}
	return kept + fmt.Sprintf("\\ No newline at end of file\n... (diff truncated at %d lines; total %d)\n", max, len(lines)-1)
}
