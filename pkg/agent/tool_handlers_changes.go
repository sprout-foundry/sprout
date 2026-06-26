// Agent-facing tools backed by the ChangeTracker's session buffer.
//
// After the SP-061-2 consolidation this file ships only two tools — the
// rest were folded into options on these:
//
//   - list_changes
//     Manifest of the session's changes, with three optional knobs:
//     include_diff: bool        per-file unified diff (was show_my_change)
//     group_by: "block"|""      activity-block summary (was summarize_my_session)
//     include_persisted: bool   merge hot+warm history (was my_recent_changes)
//     Plus the existing filters: since, tool, path_pattern.
//
//   - revert_my_changes
//     Bulk undo by scope ("all" or "since"). The previous file= scope was
//     removed because recover_file(scope="session_start") does the same
//     thing with clearer semantics.
//
// Recovery of an individual file (or bulk entry, or session-start state)
// lives in tool_handlers_recover.go.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
				original, latestNew, _, _, found := collectFileChangeSpan(tracker.GetChanges(), abs)
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

func buildUnifiedDiff(path, before, after string) string {
	if before == after {
		return "(no textual difference)"
	}
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
	return out
}

// ---------------------------------------------------------------------------
// revert_my_changes (slimmed to scope=all and since=)
// ---------------------------------------------------------------------------

// handleRevertMyChanges restores files to their session-start state
// for a SCOPE — every change, or every change after a timestamp. The
// previous file= scope was removed because recover_file(scope=
// "session_start") does the same thing with clearer semantics.
//
// Scopes:
//
//   - scope="all"            Restore every file the tracker recorded.
//   - since="<RFC3339|dur>"  Revert changes recorded at or after the
//     given timestamp (e.g. "2026-05-27T10:00:00Z"
//     or "30m"). When set, scope defaults to "all".
//
// Returns a JSON envelope listing per-file outcomes so the model can
// report exactly what happened back to the user.
func handleRevertMyChanges(_ context.Context, a *Agent, args map[string]interface{}) (string, error) {
	scope := strings.TrimSpace(asString(args["scope"]))
	sinceStr := strings.TrimSpace(asString(args["since"]))

	if scope == "" && sinceStr == "" {
		scope = "all"
	}

	tracker := a.GetChangeTracker()
	if tracker == nil || !tracker.IsEnabled() {
		return revertResult(0, 0, "change tracking is disabled — nothing to revert", nil), nil
	}

	candidates, err := selectRevertCandidates(tracker.GetChanges(), scope, sinceStr)
	if err != nil {
		return "", err
	}

	type entry struct {
		Path    string `json:"path"`
		Action  string `json:"action"`
		Message string `json:"message,omitempty"`
		OK      bool   `json:"ok"`
	}
	results := make([]entry, 0, len(candidates))
	var restored, failed int
	for _, ch := range candidates {
		action, ok, msg := a.revertOne(ch)
		results = append(results, entry{Path: ch.FilePath, Action: action, OK: ok, Message: msg})
		if ok {
			restored++
		} else {
			failed++
		}
	}
	summary := fmt.Sprintf("%d restored, %d failed (scope=%s)", restored, failed, describeScope(scope, sinceStr))
	return revertResult(restored, failed, summary, results), nil
}

// selectRevertCandidates collects ONE TrackedFileChange per path to
// revert — the OLDEST entry's original content. Reverting to the
// oldest pre-session state is the right behavior: even if the agent
// edited a file three times this session, the user wants "back to
// before the agent touched it", not "back to the previous edit".
func selectRevertCandidates(changes []TrackedFileChange, scope, since string) ([]TrackedFileChange, error) {
	var cutoff time.Time
	if since != "" {
		t, err := parseRecentSince(since)
		if err != nil {
			return nil, fmt.Errorf("revert_my_changes: invalid 'since' (need RFC3339 like 2026-05-27T10:00:00Z or duration like 30m): %w", err)
		}
		cutoff = t
	}

	filtered := make([]TrackedFileChange, 0, len(changes))
	for _, ch := range changes {
		if !cutoff.IsZero() && ch.Timestamp.Before(cutoff) {
			continue
		}
		filtered = append(filtered, ch)
	}

	if scope == "all" || scope == "" {
		// no further narrowing
	}

	// Collapse to earliest entry per path (preserving the first
	// OriginalCode encountered for each file). The slice is in
	// append-order so the first occurrence wins.
	//
	// Also track the latest NewCode per path so the staleness guard
	// can compare disk content against the current intended state
	// (not just the earliest edit's NewCode).
	seen := make(map[string]bool, len(filtered))
	latestNewCode := make(map[string]string, len(filtered))
	for _, ch := range filtered {
		latestNewCode[ch.FilePath] = ch.NewCode
	}
	earliest := make([]TrackedFileChange, 0, len(filtered))
	for _, ch := range filtered {
		key := ch.FilePath
		if seen[key] {
			continue
		}
		seen[key] = true
		ch.NewCode = latestNewCode[key]
		earliest = append(earliest, ch)
	}
	return earliest, nil
}

// revertOne writes the change's OriginalCode back to disk (or removes
// the file if the change is a create-with-no-original). Returns
// (action, ok, message). A method on *Agent so it can enforce the
// workspace boundary check (C1) via a.IsPathOutsideWorkspace and reach
// the change tracker via a.GetChangeTracker.
func (a *Agent) revertOne(ch TrackedFileChange) (string, bool, string) {
	abs, err := filepath.Abs(ch.FilePath)
	if err != nil {
		return "", false, fmt.Sprintf("resolve path: %v", err)
	}

	// C1: Refuse to write to or delete anything outside the workspace
	// root. Out-of-workspace entries are reported as skipped (not
	// failures) so a bulk revert still succeeds for the in-workspace
	// majority without aborting on a single stray path.
	if a.IsPathOutsideWorkspace(abs) {
		return "", false, "path is outside the workspace — skipped"
	}

	// Staleness guard: if the file on disk no longer matches what the
	// agent wrote (NewCode), it was modified intentionally after the
	// snapshot — by a git commit, another session, or manual edit.
	// Reverting would silently clobber that newer work.
	if isStaleForRevert(abs, ch.NewCode) {
		history.AuditRevertSkip("revertOne", abs, "stale or committed")
		return "", false, "file modified since snapshot (stale — skipped)"
	}

	tracker := a.GetChangeTracker()

	if ch.Operation == "create" {
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			return "delete", false, fmt.Sprintf("remove created file: %v", err)
		}
		if tracker != nil {
			tracker.SyncShellCacheForPath(abs)
		}
		return "delete", true, "removed file created during session"
	}

	if !isRecoverableOriginal(ch.OriginalCode) {
		return "", false, "original content was not captured (binary, oversized, or outside workspace)"
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", false, fmt.Sprintf("create parent dir: %v", err)
	}
	// Belt-and-suspenders: isRecoverableOriginal already rejects the redacted
	// marker above, but this final guard sits directly at the write so a
	// future code path that bypasses that check can never persist the literal
	// marker string to a user's file.
	if ch.OriginalCode == RedactedContentMarker {
		return "", false, "refusing to write redacted marker to disk"
	}
	history.AuditRevertWrite("revertOne", abs, "OriginalCode")
	if err := os.WriteFile(abs, []byte(ch.OriginalCode), 0o644); err != nil {
		return "", false, fmt.Sprintf("write: %v", err)
	}
	if tracker != nil {
		tracker.SyncShellCacheForPath(abs)
	}
	return "restore", true, "wrote original content back to disk"
}

func describeScope(scope, since string) string {
	if since != "" {
		return fmt.Sprintf("since=%s", since)
	}
	if scope == "" {
		return "all"
	}
	return scope
}

func revertResult(restored, failed int, summary string, entries interface{}) string {
	payload := struct {
		Restored int         `json:"restored"`
		Failed   int         `json:"failed"`
		Summary  string      `json:"summary"`
		Entries  interface{} `json:"entries,omitempty"`
	}{Restored: restored, Failed: failed, Summary: summary, Entries: entries}
	b, _ := json.MarshalIndent(payload, "", "  ")
	return string(b)
}

// ---------------------------------------------------------------------------
// shared helpers
// ---------------------------------------------------------------------------

// parseRecentSince accepts three "since" forms the model is likely to
// produce:
//
//   - RFC3339 timestamp:        "2026-05-27T10:00:00Z"
//   - duration with d/h/m/s:    "2d", "12h", "30m", "300s"
//   - empty string:             no cutoff (returns everything)
//
// Returns the absolute cutoff time. Invalid input → error.
func parseRecentSince(raw string) (time.Time, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	// Duration with d-suffix support (Go's time.ParseDuration doesn't
	// understand "d" natively). Normalize Nd → N*24h.
	if strings.HasSuffix(s, "d") {
		var days int
		if _, err := fmt.Sscanf(s, "%dd", &days); err == nil && days > 0 {
			return time.Now().Add(-time.Duration(days) * 24 * time.Hour), nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil {
		return time.Now().Add(-d), nil
	}
	return time.Time{}, fmt.Errorf("'since' must be RFC3339 (e.g. 2026-05-27T10:00:00Z), duration (2d, 12h, 30m), or empty; got %q", raw)
}

// deriveOpFromChangeLog infers the create/edit/delete code for a
// persisted change. The ChangeLog itself doesn't carry an op field,
// so we use the presence of OriginalCode + NewCode as the signal.
func deriveOpFromChangeLog(ch history.ChangeLog) string {
	hasOrig := ch.OriginalCode != ""
	hasNew := ch.NewCode != ""
	switch {
	case !hasOrig && hasNew:
		return "create"
	case hasOrig && !hasNew:
		return "delete"
	default:
		return "edit"
	}
}

// asString returns the string value at args[key], or empty string if
// the key is absent or holds a non-string. A tiny shim that keeps the
// option-parsing call sites readable.
func asString(v interface{}) string {
	s, _ := v.(string)
	return s
}

// isRecoverableOriginal reports whether a TrackedFileChange.OriginalCode
// value represents real recoverable content. The tracker uses three
// "non-content" sentinels: empty string (for created files — no
// original existed), the redacted marker (external workspace), and the
// "[CONTENT NOT CAPTURED: …]" prefix (shell snapshot filtered out).
func isRecoverableOriginal(original string) bool {
	if original == "" {
		return false
	}
	if original == RedactedContentMarker {
		return false
	}
	if len(original) >= len("[CONTENT NOT CAPTURED:") &&
		original[:len("[CONTENT NOT CAPTURED:")] == "[CONTENT NOT CAPTURED:" {
		return false
	}
	return true
}

// isStaleForRevert reports whether reverting the file must be skipped
// because the revert would clobber intentional work. It returns true
// (stale — skip) when:
//   - the file on disk differs from the agent's recorded NewCode
//     (modified after the snapshot by a git commit, manual edit, or
//     another session), OR
//   - the disk content matches NewCode but that content is now
//     committed to git HEAD (the work is version-controlled and
//     reverting to OriginalCode would silently undo it).
//
// It returns false (safe to proceed) when:
//   - newCode is empty or the redacted marker (no baseline to compare)
//   - the file doesn't exist on disk (create/delete is safe)
//   - the disk content matches newCode and is not committed to git
//
// isStaleForRevert is the negation of history.IsRevertSafe so the agent
// package and the history package share a single canonical, git-aware
// staleness decision. See history.IsRevertSafe for the full rationale.
func isStaleForRevert(absPath, newCode string) bool {
	return !history.IsRevertSafe(absPath, newCode)
}
