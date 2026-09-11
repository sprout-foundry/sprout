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
	"fmt"
	"path/filepath"

	"github.com/pmezard/go-difflib/difflib"
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
func FindPersistedOriginal(filename string) (PersistedOriginal, bool, error) {
	changes, err := fetchAllChanges()
	if err != nil {
		return PersistedOriginal{}, false, err
	}

	absTarget, err := filepath.Abs(filename)
	if err != nil {
		return PersistedOriginal{}, false, err
	}

	var oldest *ChangeLog
	for i := range changes {
		ch := &changes[i]
		chAbs, pathErr := filepath.Abs(ch.Filename)
		if pathErr != nil || chAbs != absTarget {
			continue
		}
		if !isRecoverableStoredContent(ch.OriginalCode) {
			continue
		}
		if oldest == nil || ch.Timestamp.Before(oldest.Timestamp) {
			oldest = ch
		}
	}
	if oldest == nil {
		return PersistedOriginal{}, false, nil
	}

	return PersistedOriginal{
		Filename: oldest.Filename,
		Original: oldest.OriginalCode,
		New:      oldest.NewCode,
		Status:   oldest.Status,
	}, true, nil
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
func UnifiedDiffFor(path, before, after string) string {
	if before == after {
		return "(no textual difference)"
	}
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
	return out
}
