// changes-directory pruning helpers for the revision compactor (split
// from compaction.go). pruneChangesDir does the single pass over changes/
// (orphan/aged/per-revision-cap drops) and dirSize is the best-effort
// byte tally both rely on. The revision-tiering pass (CompactRevisions,
// demoteToWarm, dropRevision, enforceHardCap) stays in compaction.go.
package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// pruneChangesDir does a single pass over the changes/ directory and
// drops entries that fail any of:
//
//  1. Orphan: parent revision no longer exists in `validRevisions` AND
//     the change's timestamp predates `orphanBefore` (changes within
//     the grace window are protected — see orphanGracePeriod comment
//     in CompactRevisions for the race this guards).
//  2. Aged: entry's timestamp is older than maxAge (if > 0).
//  3. Over-cap: revision has more than maxPerRev entries (if > 0). The
//     oldest entries are dropped first; newest are preserved.
//
// Returns (orphanCount, overCapCount, agedCount, bytesReclaimed).
func pruneChangesDir(validRevisions map[string]bool, maxPerRev int, maxAge time.Duration, orphanBefore time.Time) (int, int, int, int64) {
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

		// Orphan check: revision no longer in the valid set. The
		// grace-window skip protects against the TOCTOU race where a
		// concurrent Commit() writes a fresh change dir before its
		// parent revision dir lands — the snapshot we're holding
		// would otherwise misclassify the change as orphan and delete
		// it. Without this guard a background compaction goroutine
		// (started at agent construction) can race the user's first
		// Commit and silently drop their work.
		if !validRevisions[meta.RequestHash] {
			if !orphanBefore.IsZero() && meta.Timestamp.After(orphanBefore) {
				// Recent enough that we can't trust the snapshot —
				// keep it and let the next pass reach a correct
				// verdict with an up-to-date snapshot.
				buckets[meta.RequestHash] = append(buckets[meta.RequestHash], changeEntry{
					dir:       dir,
					revID:     meta.RequestHash,
					timestamp: meta.Timestamp,
					bytes:     size,
				})
				continue
			}
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
