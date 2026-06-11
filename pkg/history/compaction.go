// Quantity-based tiered compaction for the persistent revision store.
//
// The revisions / changes directories grow unbounded over a project's
// lifetime — every Commit() writes a new revision dir + diff payloads.
// On a heavy project this is hundreds of MB per month and untenable
// over a year. The ChangeTracker is a short-horizon stop-gap (undo a
// bad sed -i, recover a hasty rm), not a long-term audit log — git
// is for that — so the compaction policy is correspondingly simple:
//
//   - Hot  (most recent HotCount revisions):  kept verbatim
//   - Warm (next WarmCount):                  conversation.json dropped
//   - Dropped (everything older):             deleted (or archived if
//                                             ArchiveFrozen is enabled)
//
// Position is by revision-directory mtime (newest first). The view
// tools (view_history) bump mtime when they access a revision so an
// old revision the user comes back to floats to the top automatically
// — the next compaction pass sees it as hot and keeps it (or warm,
// depending on its new position).
package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// CompactionStats reports what a single CompactRevisions pass did.
// Useful for logs / metrics; not consumed by anything load-bearing.
type CompactionStats struct {
	TotalRevisions         int
	HotKept                int
	WarmDemoted            int // revisions moved hot→warm or already warm
	Dropped                int // revisions moved out of warm → deleted/archived
	ChangesPayloadsDeleted int
	BytesReclaimed         int64
	HardCapTrimmed         int
	OrphanChangesDropped   int
	OverCapChangesDropped  int
	AgedChangesDropped     int
}

// RetentionPolicy is the subset of RevisionRetentionConfig the
// compactor needs. Kept separate to avoid a cycle with pkg/configuration.
type RetentionPolicy struct {
	HotCount      int
	WarmCount     int
	MaxDirBytes   int64
	ArchiveFrozen bool

	// MaxChangesPerRevision caps the per-revision change-record count
	// in the changes/ directory. A single runaway session can produce
	// tens of thousands of records (e.g. when the agent `cd`s into
	// $HOME and a shell walk misclassifies pre-existing files as
	// creates). Without this cap, count-based bloat persists even
	// when total bytes are under MaxDirBytes. Zero disables.
	MaxChangesPerRevision int

	// MaxChangesAge drops change records older than this regardless of
	// their parent revision's tier. Belt-and-suspenders against
	// changes/ growing unbounded inside the hot window. Zero disables.
	MaxChangesAge time.Duration
}

// fileConversationJSON is the only revision-dir file the compactor
// touches directly — its presence/absence is the hot/warm marker.
const fileConversationJSON = "conversation.json"

// compactionMu serializes compaction so two agents in the same process
// don't both try to migrate the same revisions dir at once. Cheap
// because compaction runs at startup only.
var compactionMu sync.Mutex

// CompactRevisions runs one compaction pass over the configured
// revisions directory according to the given policy. Safe to call
// concurrently from multiple agents (mutex-serialized). Idempotent:
// repeated calls are no-ops once revisions are in their target tier.
//
// Returns stats for logging; errors are non-fatal at the call site
// (caller should log and continue — a failed compaction just means
// disk usage stays where it was, nothing breaks).
func CompactRevisions(policy RetentionPolicy) (CompactionStats, error) {
	compactionMu.Lock()
	defer compactionMu.Unlock()

	stats := CompactionStats{}
	revDir := GetRevisionsDir()
	if revDir == "" {
		return stats, nil
	}
	if _, err := os.Stat(revDir); os.IsNotExist(err) {
		return stats, nil
	}

	revs, err := listRevisions(revDir)
	if err != nil {
		return stats, fmt.Errorf("list revisions: %w", err)
	}
	stats.TotalRevisions = len(revs)

	// Sort newest first by mtime — touched revisions float up.
	sort.Slice(revs, func(i, j int) bool {
		return revs[i].ModTime.After(revs[j].ModTime)
	})

	hotEnd := policy.HotCount
	warmEnd := hotEnd + policy.WarmCount

	for i, rev := range revs {
		switch {
		case i < hotEnd:
			stats.HotKept++
			// Hot: nothing to do (already verbatim).
		case i < warmEnd:
			demoted, err := demoteToWarm(rev)
			if err != nil {
				continue
			}
			if demoted {
				stats.WarmDemoted++
			}
		default:
			freed, payloadCount, err := dropRevision(rev, policy.ArchiveFrozen)
			if err != nil {
				continue
			}
			stats.Dropped++
			stats.BytesReclaimed += freed
			stats.ChangesPayloadsDeleted += payloadCount
		}
	}

	// Hard-cap fallback: if total dir size still exceeds the policy
	// cap, trim oldest warm entries until under cap. Hot tier is
	// never trimmed — losing the user's most recent active work would
	// be worse than disk pressure.
	if policy.MaxDirBytes > 0 {
		trimmed, err := enforceHardCap(revs, hotEnd, policy.MaxDirBytes, policy.ArchiveFrozen)
		if err == nil {
			stats.HardCapTrimmed = trimmed
		}
	}

	// Defensive changes/ cleanup: orphan + age + per-revision-cap.
	// Runs after the revision-tier passes so the valid-revisions set
	// reflects post-compaction state.
	if policy.MaxChangesPerRevision > 0 || policy.MaxChangesAge > 0 {
		valid := make(map[string]bool, len(revs))
		for _, r := range revs {
			valid[r.ID] = true
		}
		// Re-list after drops so newly orphaned revs aren't kept as valid.
		if remaining, err := listRevisions(revDir); err == nil {
			valid = make(map[string]bool, len(remaining))
			for _, r := range remaining {
				valid[r.ID] = true
			}
		}
		orphan, overcap, aged, bytes := pruneChangesDir(valid, policy.MaxChangesPerRevision, policy.MaxChangesAge)
		stats.OrphanChangesDropped = orphan
		stats.OverCapChangesDropped = overcap
		stats.AgedChangesDropped = aged
		stats.BytesReclaimed += bytes
	}

	return stats, nil
}

// pruneChangesDir does a single pass over the changes/ directory and
// drops entries that fail any of:
//
//  1. Orphan: parent revision no longer exists in `validRevisions`.
//  2. Aged: entry's timestamp is older than maxAge (if > 0).
//  3. Over-cap: revision has more than maxPerRev entries (if > 0). The
//     oldest entries are dropped first; newest are preserved.
//
// Returns (orphanCount, overCapCount, agedCount, bytesReclaimed).
func pruneChangesDir(validRevisions map[string]bool, maxPerRev int, maxAge time.Duration) (int, int, int, int64) {
	changesDir := GetChangesDir()
	if changesDir == "" {
		return 0, 0, 0, 0
	}
	entries, err := os.ReadDir(changesDir)
	if err != nil {
		return 0, 0, 0, 0
	}

	type changeEntry struct {
		dir       string // absolute path of the change-record directory
		revID     string
		timestamp time.Time
		bytes     int64
	}

	// Bucket by revision so the per-revision cap can drop oldest first.
	buckets := make(map[string][]changeEntry, len(entries))
	var orphan, overcap, aged int
	var bytes int64
	cutoff := time.Time{}
	if maxAge > 0 {
		cutoff = time.Now().Add(-maxAge)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(changesDir, e.Name())
		metaPath := filepath.Join(dir, metadataFile)
		metaBytes, mErr := os.ReadFile(metaPath)
		if mErr != nil {
			continue
		}
		var meta ChangeMetadata
		if jErr := json.Unmarshal(metaBytes, &meta); jErr != nil {
			continue
		}

		size := dirSize(dir)

		// Orphan check: revision no longer in the valid set.
		if !validRevisions[meta.RequestHash] {
			if err := os.RemoveAll(dir); err == nil {
				orphan++
				bytes += size
			}
			continue
		}

		// Age check: timestamp older than cutoff.
		if !cutoff.IsZero() && meta.Timestamp.Before(cutoff) {
			if err := os.RemoveAll(dir); err == nil {
				aged++
				bytes += size
			}
			continue
		}

		buckets[meta.RequestHash] = append(buckets[meta.RequestHash], changeEntry{
			dir:       dir,
			revID:     meta.RequestHash,
			timestamp: meta.Timestamp,
			bytes:     size,
		})
	}

	if maxPerRev > 0 {
		for _, group := range buckets {
			if len(group) <= maxPerRev {
				continue
			}
			sort.Slice(group, func(i, j int) bool {
				return group[i].timestamp.After(group[j].timestamp)
			})
			for _, e := range group[maxPerRev:] {
				if err := os.RemoveAll(e.dir); err == nil {
					overcap++
					bytes += e.bytes
				}
			}
		}
	}

	return orphan, overcap, aged, bytes
}

// dirSize sums the bytes of all regular files under dir. Returns 0 on
// any walk error — sizing is best-effort and only used for stats.
func dirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, infoErr := d.Info(); infoErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// revisionEntry is the in-memory record the compactor sorts and acts on.
type revisionEntry struct {
	ID      string // basename of the dir = revision ID
	Path    string // absolute path
	ModTime time.Time
}

func listRevisions(revDir string) ([]revisionEntry, error) {
	entries, err := os.ReadDir(revDir)
	if err != nil {
		return nil, err
	}
	var revs []revisionEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Skip internal sidecars (e.g., _frozen/ archive holding area).
		if len(e.Name()) > 0 && e.Name()[0] == '_' {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		revs = append(revs, revisionEntry{
			ID:      e.Name(),
			Path:    filepath.Join(revDir, e.Name()),
			ModTime: info.ModTime(),
		})
	}
	return revs, nil
}

// demoteToWarm drops conversation.json from a revision dir if present.
// Returns (true, nil) when a transition happened, (false, nil) when
// already at warm.
func demoteToWarm(rev revisionEntry) (bool, error) {
	convPath := filepath.Join(rev.Path, fileConversationJSON)
	if _, err := os.Stat(convPath); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	// Preserve mtime — we don't want demoting to bump the dir
	// timestamp and accidentally promote it back to hot next time.
	dirInfo, dirErr := os.Stat(rev.Path)
	if err := os.Remove(convPath); err != nil {
		return false, err
	}
	if dirErr == nil {
		_ = os.Chtimes(rev.Path, dirInfo.ModTime(), dirInfo.ModTime())
	}
	return true, nil
}

// dropRevision removes a revision dir and all its change payloads
// entirely. Returns (bytesReclaimed, payloadsRemoved, err).
//
// When archive=true, the revision dir is moved into a sibling
// _frozen/ holding area instead of being deleted outright. The
// associated change records (which the user could reconstruct from
// the moved dir alone) are still purged. Best-effort; if the move
// fails we fall back to outright delete so we don't leak the disk
// space we promised to free.
func dropRevision(rev revisionEntry, archive bool) (int64, int, error) {
	var bytesReclaimed int64
	_ = filepath.WalkDir(rev.Path, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, infoErr := d.Info(); infoErr == nil {
			bytesReclaimed += info.Size()
		}
		return nil
	})

	if archive {
		frozenDir := filepath.Join(filepath.Dir(rev.Path), "_frozen")
		if err := os.MkdirAll(frozenDir, 0o755); err == nil {
			if err := os.Rename(rev.Path, filepath.Join(frozenDir, rev.ID)); err == nil {
				_, payloadsRemoved, payloadBytes := purgeChangesForRevision(rev.ID)
				bytesReclaimed += payloadBytes
				return bytesReclaimed, payloadsRemoved, nil
			}
		}
		// Fall through to outright delete on any archive failure.
	}

	_, payloadsRemoved, payloadBytes := purgeChangesForRevision(rev.ID)
	bytesReclaimed += payloadBytes
	if err := os.RemoveAll(rev.Path); err != nil {
		return bytesReclaimed, payloadsRemoved, err
	}
	return bytesReclaimed, payloadsRemoved, nil
}

// purgeChangesForRevision removes the entire changes directory entry
// for every change tied to the given revision_id. Returns
// (filesAffected, payloadsRemoved, bytesReclaimed). Used by
// dropRevision to clean up the bulk content tied to the revision
// being removed.
func purgeChangesForRevision(revisionID string) ([]string, int, int64) {
	var files []string
	var payloadsRemoved int
	var bytesReclaimed int64

	changesDir := GetChangesDir()
	entries, err := os.ReadDir(changesDir)
	if err != nil {
		return files, payloadsRemoved, bytesReclaimed
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		metaPath := filepath.Join(changesDir, entry.Name(), metadataFile)
		metaBytes, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var meta ChangeMetadata
		if jsonErr := json.Unmarshal(metaBytes, &meta); jsonErr != nil {
			continue
		}
		if meta.RequestHash != revisionID {
			continue
		}

		files = append(files, meta.Filename)
		changeDir := filepath.Join(changesDir, entry.Name())

		// Tally size before removal for stats.
		_ = filepath.WalkDir(changeDir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if info, infoErr := d.Info(); infoErr == nil {
				bytesReclaimed += info.Size()
				payloadsRemoved++
			}
			return nil
		})
		_ = os.RemoveAll(changeDir)
	}
	return files, payloadsRemoved, bytesReclaimed
}

// enforceHardCap walks the revisions list and trims oldest entries
// until total size is under cap. Touches only the warm tier
// (positions >= hotEnd); the hot tier is sacred. Returns the number
// of entries trimmed.
func enforceHardCap(revs []revisionEntry, hotEnd int, cap int64, archiveFrozen bool) (int, error) {
	totalBytes := computeRevisionsTotalBytes(revs)
	if totalBytes <= cap {
		return 0, nil
	}

	trimmed := 0
	for i := len(revs) - 1; i >= hotEnd && totalBytes > cap; i-- {
		rev := revs[i]
		freed, _, err := dropRevision(rev, archiveFrozen)
		if err != nil {
			continue
		}
		totalBytes -= freed
		trimmed++
	}
	return trimmed, nil
}

// computeRevisionsTotalBytes adds up bytes across all revision dirs
// AND the entire changes directory (we can't trivially partition by
// revision without re-scanning all metadata; the changes dir total
// is a fine proxy for "how much payload exists overall").
func computeRevisionsTotalBytes(revs []revisionEntry) int64 {
	var total int64
	for _, rev := range revs {
		_ = filepath.WalkDir(rev.Path, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if info, infoErr := d.Info(); infoErr == nil {
				total += info.Size()
			}
			return nil
		})
	}
	changesDir := GetChangesDir()
	_ = filepath.WalkDir(changesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, infoErr := d.Info(); infoErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// TouchRevision bumps the revision directory's mtime to now. Called
// when view_history accesses a revision so the next compaction pass
// considers it "recently used" and keeps it (or re-promotes it from
// warm back toward hot) regardless of its position in raw creation
// order. No-op if the revision dir doesn't exist (already dropped).
func TouchRevision(revisionID string) error {
	if revisionID == "" {
		return nil
	}
	revPath := filepath.Join(GetRevisionsDir(), revisionID)
	now := time.Now()
	return os.Chtimes(revPath, now, now)
}
