package agent

// tool_handlers_changes.go — the list_changes tool: the handler, the
// persisted-only variant, and the file-list / block-summary / diff / filter
// builders over the ChangeTracker session buffer. The revert_my_changes tool
// lives in tool_handlers_changes_revert.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pmezard/go-difflib/difflib"

	"github.com/sprout-foundry/sprout/pkg/history"
)

// activityGapThreshold is the time gap between consecutive changes
// that splits one "activity block" from the next. 30 seconds is long
// enough that work within a single agent turn clusters together but
// short enough that distinct turns separate cleanly.
const activityGapThreshold = 30 * time.Second

// ---------------------------------------------------------------------------
// list_changes
// ---------------------------------------------------------------------------

func handleListChanges(_ context.Context, a *Agent, args map[string]interface{}) (string, error) {
	tracker := a.GetChangeTracker()

	// Read knobs. group_by switches the response shape entirely; the
	// rest are additive on the per-file shape.
	groupBy, _ := args["group_by"].(string)
	includeDiff, _ := args["include_diff"].(bool)
	// include_persisted defaults to true so the manifest reflects the
	// full session (in-memory + already-committed-to-history entries)
	// rather than just the current turn's uncommitted buffer. A caller
	// who wants the live buffer only can pass include_persisted=false.
	// The persisted merge is SESSION-SCOPED (matches the tracker's
	// revisionID), so it never surfaces other sessions' noise.
	includePersisted := true
	if v, ok := args["include_persisted"].(bool); ok {
		includePersisted = v
	}

	// include_cross_session, when true, merges persisted changes from
	// ALL sessions instead of only the current one. Used by the
	// timeline ("Recent history") tab so cross-session change history
	// is visible even when a live agent is running. The default path
	// (session tab) uses session-scoped merge to keep noise out.
	includeCrossSession := false
	if v, ok := args["include_cross_session"].(bool); ok {
		includeCrossSession = v
	}

	if tracker == nil || !tracker.IsEnabled() {
		if groupBy == "block" {
			return `{"enabled":false,"blocks":[],"totals":{"changes":0,"files":0}}`, nil
		}
		if includePersisted {
			return handleListChangesPersistedOnly(args)
		}
		return `{"revision_id":"","enabled":false,"count":0,"files":[]}`, nil
	}

	changes := applyChangeFilters(tracker.GetChanges(), args)

	if groupBy == "block" {
		return buildBlockSummary(tracker.GetRevisionID(), changes)
	}

	return buildFileList(tracker, changes, includeDiff, includePersisted, includeCrossSession, args)
}

// handleListChangesPersistedOnly is the include_persisted path when no
// in-memory tracker is enabled (or empty). It still returns the
// list_changes envelope so the caller's parser doesn't have to branch.
func handleListChangesPersistedOnly(args map[string]interface{}) (string, error) {
	type fileEntry struct {
		Path        string    `json:"path"`
		Op          string    `json:"op"`
		Tool        string    `json:"tool"`
		Timestamp   time.Time `json:"timestamp"`
		Source      string    `json:"source,omitempty"`
		RevisionID  string    `json:"revision_id,omitempty"`
		Tier        string    `json:"tier,omitempty"`
		Recoverable bool      `json:"recoverable"`
	}
	cutoff, _ := parseRecentSince(asString(args["since"]))
	files := make([]fileEntry, 0)
	// H2: Metadata-only scan avoids the O(history) base64 decode.
	// This fallback path (no in-memory tracker) only needs the
	// manifest fields, never the full content.
	if persisted, err := history.GetAllChangesMetadata(); err == nil {
		for _, ch := range persisted {
			if !cutoff.IsZero() && ch.Timestamp.Before(cutoff) {
				continue
			}
			files = append(files, fileEntry{
				Path:        ch.Filename,
				Op:          deriveOpFromChangeLog(ch),
				Tool:        "(persisted)",
				Timestamp:   ch.Timestamp,
				Source:      "persisted",
				RevisionID:  ch.RequestHash,
				Tier:        ch.Tier,
				Recoverable: ch.OriginalCode != "",
			})
		}
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Timestamp.After(files[j].Timestamp)
	})
	out := struct {
		RevisionID string      `json:"revision_id"`
		Enabled    bool        `json:"enabled"`
		Count      int         `json:"count"`
		Files      []fileEntry `json:"files"`
	}{Count: len(files), Files: files}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// buildFileList renders the per-file shape for list_changes. Used both
// for session-only and session+persisted output.
func buildFileList(tracker *ChangeTracker, changes []TrackedFileChange, includeDiff, includePersisted, includeCrossSession bool, args map[string]interface{}) (string, error) {
	type bulkItemEntry struct {
		Path string `json:"path"`
		Op   string `json:"op"`
	}
	type fileEntry struct {
		Path        string          `json:"path"`
		Op          string          `json:"op"`
		Tool        string          `json:"tool"`
		Timestamp   time.Time       `json:"timestamp"`
		Source      string          `json:"source,omitempty"`
		RevisionID  string          `json:"revision_id,omitempty"`
		Tier        string          `json:"tier,omitempty"`
		Recoverable bool            `json:"recoverable"`
		BulkCount   int             `json:"bulk_count,omitempty"`
		BulkItems   []bulkItemEntry `json:"bulk_items,omitempty"`
		Diff        string          `json:"diff,omitempty"`
	}

	// Hoist the snapshot out of the loop: GetChanges() copies the full
	// tracked-change buffer (including file contents), and calling it
	// per file made include_diff O(N²) in buffer size.
	var allChanges []TrackedFileChange
	if includeDiff && len(changes) > 0 {
		allChanges = tracker.GetChanges()
	}
	files := make([]fileEntry, 0, len(changes))
	for _, ch := range changes {
		entry := fileEntry{
			Path:        ch.FilePath,
			Op:          ch.Operation,
			Tool:        ch.ToolCall,
			Timestamp:   ch.Timestamp,
			Source:      ch.Source,
			Recoverable: isRecoverableOriginal(ch.OriginalCode),
			BulkCount:   ch.BulkCount,
		}
		if ch.Operation == "bulk" {
			entry.Recoverable = len(ch.BulkItems) > 0
			if len(ch.BulkItems) > 0 {
				items := make([]bulkItemEntry, len(ch.BulkItems))
				for i, it := range ch.BulkItems {
					items[i] = bulkItemEntry{Path: it.FilePath, Op: it.Operation}
				}
				entry.BulkItems = items
			}
		}
		if includeDiff && ch.Operation != "bulk" {
			// Reuse collectFileChangeSpan so the diff reflects the
			// CUMULATIVE state across multiple edits to the same file,
			// not just the immediate change. Matches the prior
			// show_my_change behaviour.
			if abs, err := filepath.Abs(ch.FilePath); err == nil {
				original, latestNew, _, _, found := collectFileChangeSpan(allChanges, abs)
				if found {
					entry.Diff = buildUnifiedDiff(abs, original, latestNew)
				}
			}
		}
		files = append(files, entry)
	}

	if includePersisted {
		cutoff, _ := parseRecentSince(asString(args["since"]))
		// Session-scope the persisted merge: only entries recorded
		// under THIS session's revisionID. This keeps other sessions'
		// work out of the manifest (the historical reason
		// include_persisted was opt-in) while still surfacing this
		// session's already-committed entries — which is what makes
		// the default manifest correct across turn boundaries, where
		// a file edited last turn was committed to history and may
		// not yet be re-touched this turn.
		sessionRevID := tracker.GetRevisionID()
		// Build a dedup set from the in-memory entries so a file that
		// is BOTH committed (persisted) and re-edited this turn isn't
		// listed twice. The in-memory entry wins because it carries
		// the latest, possibly uncommitted, state.
		seenInMemory := make(map[string]bool, len(files))
		for _, f := range files {
			seenInMemory[f.Path] = true
		}
		// H2: Use the metadata-only scan. The persisted entries here
		// never need full content (diffs are computed only from
		// in-memory entries via collectFileChangeSpan). The metadata
		// scan avoids reading + base64-decoding every .original/
		// .updated file on disk — the dominant cost of list_changes
		// on a large history.
		if persisted, err := history.GetAllChangesMetadata(); err == nil {
			for _, ch := range persisted {
				// Session filter: skip other sessions' revisions
				// unless include_cross_session is true (timeline path).
				if !includeCrossSession && sessionRevID != "" && ch.RequestHash != sessionRevID {
					continue
				}
				// Dedup: skip if the in-memory buffer already shows
				// this path (it has newer or equal info). Applies to
				// both same-session and cross-session merges.
				if seenInMemory[ch.Filename] {
					continue
				}
				if !cutoff.IsZero() && ch.Timestamp.Before(cutoff) {
					continue
				}
				files = append(files, fileEntry{
					Path:        ch.Filename,
					Op:          deriveOpFromChangeLog(ch),
					Tool:        "(persisted)",
					Timestamp:   ch.Timestamp,
					Source:      "persisted",
					RevisionID:  ch.RequestHash,
					Tier:        ch.Tier,
					Recoverable: ch.OriginalCode != "",
				})
			}
		}
		sort.Slice(files, func(i, j int) bool {
			return files[i].Timestamp.After(files[j].Timestamp)
		})
	}

	out := struct {
		RevisionID string      `json:"revision_id"`
		Enabled    bool        `json:"enabled"`
		Count      int         `json:"count"`
		Files      []fileEntry `json:"files"`
	}{
		RevisionID: tracker.GetRevisionID(),
		Enabled:    true,
		Count:      len(files),
		Files:      files,
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// buildBlockSummary renders the activity-block shape for
// list_changes(group_by="block"). Activity-blocks are the contiguous
// runs of work separated by activityGapThreshold of quiet — a useful
// "what did this session look like at a glance" summary.
func buildBlockSummary(revisionID string, changes []TrackedFileChange) (string, error) {
	if len(changes) == 0 {
		return `{"enabled":true,"revision_id":"` + revisionID + `","blocks":[],"totals":{"changes":0,"files":0}}`, nil
	}

	// Sort by timestamp so block boundaries are deterministic. The
	// tracker is append-order, which already approximates this, but
	// direct hooks vs shell diff can arrive out of order in rare cases.
	sorted := make([]TrackedFileChange, len(changes))
	copy(sorted, changes)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.Before(sorted[j].Timestamp)
	})

	type fileLite struct {
		Path string `json:"path"`
		Op   string `json:"op"`
	}
	type block struct {
		StartedAt time.Time      `json:"started_at"`
		EndedAt   time.Time      `json:"ended_at"`
		Tools     map[string]int `json:"tools"`
		Files     []fileLite     `json:"files"`
	}
	var blocks []block
	current := block{StartedAt: sorted[0].Timestamp, Tools: map[string]int{}}
	prev := sorted[0].Timestamp
	seenInBlock := make(map[string]bool)
	for _, ch := range sorted {
		if ch.Timestamp.Sub(prev) > activityGapThreshold && len(current.Files) > 0 {
			current.EndedAt = prev
			blocks = append(blocks, current)
			current = block{StartedAt: ch.Timestamp, Tools: map[string]int{}}
			seenInBlock = make(map[string]bool)
		}
		key := ch.FilePath + "|" + ch.Operation
		if !seenInBlock[key] {
			current.Files = append(current.Files, fileLite{Path: ch.FilePath, Op: ch.Operation})
			seenInBlock[key] = true
		}
		current.Tools[ch.ToolCall]++
		prev = ch.Timestamp
	}
	current.EndedAt = prev
	blocks = append(blocks, current)

	allFiles := make(map[string]bool, len(changes))
	for _, ch := range changes {
		allFiles[ch.FilePath] = true
	}

	out := struct {
		Enabled    bool    `json:"enabled"`
		RevisionID string  `json:"revision_id"`
		Blocks     []block `json:"blocks"`
		Totals     struct {
			Changes int `json:"changes"`
			Files   int `json:"files"`
		} `json:"totals"`
	}{Enabled: true, RevisionID: revisionID, Blocks: blocks}
	out.Totals.Changes = len(changes)
	out.Totals.Files = len(allFiles)

	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// applyChangeFilters narrows a TrackedFileChange slice by optional
// since=<ISO8601 OR duration>, tool=<name>, path_pattern=<glob> args.
// Returns a copy of the matched subset so callers don't mutate the
// tracker's internal slice.
func applyChangeFilters(changes []TrackedFileChange, args map[string]interface{}) []TrackedFileChange {
	cutoff, _ := parseRecentSince(asString(args["since"]))
	toolFilter, _ := args["tool"].(string)
	pattern, _ := args["path_pattern"].(string)

	if cutoff.IsZero() && toolFilter == "" && pattern == "" {
		return changes
	}
	out := make([]TrackedFileChange, 0, len(changes))
	for _, ch := range changes {
		if !cutoff.IsZero() && ch.Timestamp.Before(cutoff) {
			continue
		}
		if toolFilter != "" && ch.ToolCall != toolFilter {
			continue
		}
		if pattern != "" {
			match, _ := filepath.Match(pattern, ch.FilePath)
			if !match {
				continue
			}
		}
		out = append(out, ch)
	}
	return out
}

// ---------------------------------------------------------------------------
// list_changes helpers reused by recover_file (scope="session_start")
// ---------------------------------------------------------------------------

// collectFileChangeSpan walks the tracker's changes in append order
// and returns (earliestOriginal, latestNew, op, tool, found) for the
// given absolute path. "earliestOriginal" is the OriginalCode of the
// FIRST change to this path (i.e., the state before the agent touched
// it). "latestNew" is the NewCode of the LAST change (i.e., the
// current intended on-disk state).
//
// op reflects the AGGREGATE outcome across all this-session edits:
//   - if first op is create and file still ends up present → "create"
//   - if any op is delete → "delete"
//   - else → "edit"
func collectFileChangeSpan(changes []TrackedFileChange, abs string) (string, string, string, string, bool) {
	var firstOriginal, lastNew, firstOp, lastTool string
	var sawDelete bool
	var firstSeen, lastSeen bool
	for _, ch := range changes {
		chAbs, err := filepath.Abs(ch.FilePath)
		if err != nil || chAbs != abs {
			continue
		}
		if !firstSeen {
			firstOriginal = ch.OriginalCode
			firstOp = ch.Operation
			firstSeen = true
		}
		lastNew = ch.NewCode
		lastTool = ch.ToolCall
		lastSeen = true
		if ch.Operation == "delete" {
			sawDelete = true
		}
	}
	if !firstSeen {
		return "", "", "", "", false
	}
	op := "edit"
	switch {
	case sawDelete:
		op = "delete"
	case firstOp == "create" && lastSeen:
		op = "create"
	}
	return firstOriginal, lastNew, op, lastTool, true
}

// Bounds for unified-diff rendering. The tracker stores full file
// contents, and go-difflib's Myers diff is O((N+M)·D) — without caps a
// rewritten minified/lock/generated file costs seconds-to-minutes of CPU
// on the caller's goroutine, which blocks the WebUI diff endpoint and
// starves the daemon (the "diff view lockup"). Both bounds were chosen
// so the worst case finishes in milliseconds.
const (
	// maxDiffInputLines caps each side before Myers runs. Larger files
	// are head-truncated first: a diff of the first N lines is still
	// useful, and the truncation notice tells the reader what happened.
	maxDiffInputLines = 20000
	// maxDiffOutputLines caps the rendered diff (difflib can EXPLODE
	// output: a whole-file rewrite of an N-line file yields ~2N+ hunk
	// lines even when inputs are small).
	maxDiffOutputLines = 4000
)

// headTruncateLines caps s at max lines, appending a notice line when
// truncated. Matches difflib.SplitLines' semantics (every line keeps
// its trailing \n).
func headTruncateLines(s string, max int, what string) string {
	lines := strings.SplitAfter(s, "\n")
	if len(lines) <= max {
		return s
	}
	// SplitAfter yields a trailing "" element when s ends in \n; the
	// first max elements are then exactly max real lines.
	kept := strings.Join(lines[:max], "")
	if !strings.HasSuffix(kept, "\n") {
		kept += "\n"
	}
	return kept + fmt.Sprintf("... (%s truncated at %d lines for diffing)\n", what, max)
}

func buildUnifiedDiff(path, before, after string) string {
	if before == after {
		return "(no textual difference)"
	}
	before = headTruncateLines(before, maxDiffInputLines, "before-side")
	after = headTruncateLines(after, maxDiffInputLines, "after-side")

	d := difflib.UnifiedDiff{
		A:        difflib.SplitLines(before),
		B:        difflib.SplitLines(after),
		FromFile: path + " (before session)",
		ToFile:   path + " (after session)",
		Context:  3,
	}
	out, err := difflib.GetUnifiedDiffString(d)
	if err != nil {
		return fmt.Sprintf("(diff failed: %v)", err)
	}
	return truncateDiffLines(out, maxDiffOutputLines)
}

// truncateDiffLines caps a rendered unified diff at max lines. The cut
// is inserted before the closing hunk so the result stays a parseable
// diff; a trailing notice explains the omission.
func truncateDiffLines(diff string, max int) string {
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

// ---------------------------------------------------------------------------
// revert_my_changes (slimmed to scope=all and since=)
// ---------------------------------------------------------------------------
