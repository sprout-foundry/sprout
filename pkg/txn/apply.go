package txn

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// maxSkippedEntries bounds the "skipped" array carried in an apply response.
// The contract pins per-entry skipping (never a whole-request error), so a
// pathological manifest could otherwise balloon the response with millions
// of entries — the 100 MiB request cap bounds the input, this bounds the
// echo. Entries skipped past the bound are still skipped, still make the
// result "partial", and are simply not itemized.
const maxSkippedEntries = 2000

// skipRecorder accumulates skipped entries up to maxSkippedEntries and
// remembers whether anything at all was skipped.
type skipRecorder struct {
	entries []SkippedEntry
	any     bool
	// overflow counts entries dropped from the itemized list.
	overflow int
}

func newSkipRecorder() *skipRecorder {
	return &skipRecorder{entries: []SkippedEntry{}}
}

func (s *skipRecorder) skip(path, reason string) {
	s.any = true
	if len(s.entries) >= maxSkippedEntries {
		s.overflow++
		return
	}
	s.entries = append(s.entries, SkippedEntry{Path: path, Reason: reason})
}

// ApplyDelta writes a push manifest into the workdir: parents are created
// as needed (0755), file contents land with their requested mode (0644 when
// absent), then deletes are processed.
//
// Skipping is per-entry and never fails the request: an unsafe path, a
// bad base64 blob, a cap violation or an unwritable target is recorded in
// result.Skipped with its reason and the rest of the manifest still lands.
// result.Status is "partial" iff anything was skipped.
//
// Only a catastrophic failure — an unresolvable workdir — returns an error,
// in which case nothing was applied.
func ApplyDelta(ctx context.Context, workdir string, manifest DeltaManifest) (ApplyResult, error) {
	result := newApplyResult()
	skipped := newSkipRecorder()

	dir, err := resolveWorkdir(workdir)
	if err != nil {
		return result, err
	}

	total := 0
	for i, file := range manifest.Files {
		if reason := validateRelPath(file.Path); reason != "" {
			skipped.skip(file.Path, reason)
			continue
		}
		if i >= MaxFileCount {
			skipped.skip(file.Path, SkipReasonExceedsFileCount)
			continue
		}
		content, reason := decodeDeltaContent(file.ContentBase64)
		if reason != "" {
			skipped.skip(file.Path, reason)
			continue
		}
		if len(content) > MaxFileBytes {
			skipped.skip(file.Path, SkipReasonExceedsPerFile)
			continue
		}
		if total+len(content) > MaxTotalBytes {
			skipped.skip(file.Path, SkipReasonExceedsTotal)
			continue
		}
		mode, reason := parseFileMode(file.Mode)
		if reason != "" {
			skipped.skip(file.Path, reason)
			continue
		}
		if err := applyOneFile(dir, file.Path, content, mode); err != nil {
			skipped.skip(file.Path, skipReasonForError(err))
			continue
		}
		total += len(content)
		result.Applied++
	}

	for _, rel := range manifest.Deletes {
		if reason := validateRelPath(rel); reason != "" {
			skipped.skip(rel, reason)
			continue
		}
		target, err := secureJoin(dir, rel)
		if err != nil {
			skipped.skip(rel, skipReasonForError(err))
			continue
		}
		if err := os.Remove(target); err != nil {
			if os.IsNotExist(err) {
				// The requested end state — this path absent — already
				// holds. Not an error and not a skip: nothing about the
				// tree fails to match the request.
				continue
			}
			skipped.skip(rel, SkipReasonDeleteFailed)
			continue
		}
		result.Deleted++
	}

	result.Skipped = skipped.entries
	if skipped.any {
		result.Status = StatusPartial
	}
	return result, nil
}

// applyOneFile writes one decoded entry. The chmod is unconditional: a
// plain WriteFile applies the mode only on creation, and leaving a stale
// mode behind would make push/pull never converge on an edited existing
// file.
func applyOneFile(dir, rel string, content []byte, mode os.FileMode) error {
	target, err := secureJoin(dir, rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), DefaultDirMode); err != nil {
		return fmt.Errorf("txn: create parent for %q: %w", rel, err)
	}
	if err := os.WriteFile(target, content, mode); err != nil {
		return fmt.Errorf("txn: write %q: %w", rel, err)
	}
	if err := os.Chmod(target, mode); err != nil {
		return fmt.Errorf("txn: chmod %q: %w", rel, err)
	}
	return nil
}

// decodeDeltaContent base64-decodes one entry. Size is never trusted from
// the manifest — the decoded length is what the caps are measured against.
func decodeDeltaContent(encoded string) ([]byte, string) {
	content, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, SkipReasonInvalidBase64
	}
	return content, ""
}

// parseFileMode parses the optional "mode" octal string. Empty means the
// 0644 default; anything unparsable is rejected rather than guessed, so a
// corrupt manifest cannot silently land world-writable files.
func parseFileMode(mode string) (os.FileMode, string) {
	trimmed := strings.TrimSpace(mode)
	if trimmed == "" {
		return DefaultFileMode, ""
	}
	// Tolerate a "0o"-style prefix the browser side might emit.
	trimmed = strings.TrimPrefix(strings.TrimPrefix(trimmed, "0o"), "0O")
	parsed, err := strconv.ParseUint(trimmed, 8, 32)
	if err != nil || parsed > 0o777 {
		return 0, SkipReasonInvalidMode
	}
	return os.FileMode(parsed), ""
}

// skipReasonForError maps an apply error onto the skip vocabulary. A symlink
// escape is its own reason; everything else is a write failure.
func skipReasonForError(err error) string {
	if err == errEscapesRoot {
		return SkipReasonSymlinkEscape
	}
	return SkipReasonWriteFailed
}
