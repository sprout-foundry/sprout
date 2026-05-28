// Agent-facing tools backed by the ChangeTracker's session buffer.
// Five tools share this file because they all operate on the same
// in-memory []TrackedFileChange and emit JSON envelopes the LLM can
// reason about directly:
//
//   - list_changes       Manifest of every change this session.
//   - show_my_change     Inline diff for one file (uses go-difflib).
//   - revert_my_changes  Bulk undo with scopes (all / file / since).
//   - summarize_my_session  Grouped digest by contiguous activity block.
//   - my_recent_changes  Unified timeline across in-memory + persistent.
//
// All five are read/self-audit-or-revert tools — none of them call
// out to the LLM, all are deterministic, and all return structured
// JSON so the model has a stable shape to reason about.
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

// ---------------------------------------------------------------------------
// list_changes (with filtering)
// ---------------------------------------------------------------------------

func handleListChanges(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	tracker := a.GetChangeTracker()
	if tracker == nil || !tracker.IsEnabled() {
		return `{"revision_id":"","enabled":false,"count":0,"files":[]}`, nil
	}

	changes := tracker.GetChanges()
	changes = applyChangeFilters(changes, args)

	type fileEntry struct {
		Path        string    `json:"path"`
		Op          string    `json:"op"`
		Tool        string    `json:"tool"`
		Timestamp   time.Time `json:"timestamp"`
		Recoverable bool      `json:"recoverable"`
	}
	files := make([]fileEntry, 0, len(changes))
	for _, ch := range changes {
		files = append(files, fileEntry{
			Path:        ch.FilePath,
			Op:          ch.Operation,
			Tool:        ch.ToolCall,
			Timestamp:   ch.Timestamp,
			Recoverable: isRecoverableOriginal(ch.OriginalCode),
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

// applyChangeFilters narrows a TrackedFileChange slice by optional
// since=<ISO8601>, tool=<name>, path_pattern=<glob> args. Returns a
// copy of the matched subset so callers don't mutate the tracker's
// internal slice.
func applyChangeFilters(changes []TrackedFileChange, args map[string]interface{}) []TrackedFileChange {
	var sinceCutoff time.Time
	if s, ok := args["since"].(string); ok && strings.TrimSpace(s) != "" {
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(s)); err == nil {
			sinceCutoff = t
		}
	}
	toolFilter, _ := args["tool"].(string)
	pattern, _ := args["path_pattern"].(string)

	if sinceCutoff.IsZero() && toolFilter == "" && pattern == "" {
		return changes
	}
	out := make([]TrackedFileChange, 0, len(changes))
	for _, ch := range changes {
		if !sinceCutoff.IsZero() && ch.Timestamp.Before(sinceCutoff) {
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
// show_my_change
// ---------------------------------------------------------------------------

func handleShowMyChange(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	rawPath, ok := args["path"].(string)
	if !ok || strings.TrimSpace(rawPath) == "" {
		return "", fmt.Errorf("show_my_change: 'path' parameter is required")
	}
	abs, err := filepath.Abs(rawPath)
	if err != nil {
		return "", fmt.Errorf("show_my_change: resolve %q: %w", rawPath, err)
	}

	tracker := a.GetChangeTracker()
	if tracker == nil || !tracker.IsEnabled() {
		return showChangeResult(false, abs, "", "change tracking is disabled", "", ""), nil
	}

	changes := tracker.GetChanges()
	earliestOriginal, latestNew, op, tool, found := collectFileChangeSpan(changes, abs)
	if !found {
		return showChangeResult(false, abs, "", "no tracked changes recorded for this path in this session", "", ""), nil
	}

	diff := buildUnifiedDiff(abs, earliestOriginal, latestNew)
	return showChangeResult(true, abs, op, tool, diff, summarizeDiffStats(earliestOriginal, latestNew)), nil
}

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

func summarizeDiffStats(before, after string) string {
	beforeLines := len(strings.Split(before, "\n"))
	afterLines := len(strings.Split(after, "\n"))
	return fmt.Sprintf("%d lines before → %d lines after", beforeLines, afterLines)
}

func showChangeResult(ok bool, path, op, tool, diff, stats string) string {
	payload := struct {
		Found bool   `json:"found"`
		Path  string `json:"path"`
		Op    string `json:"op,omitempty"`
		Tool  string `json:"tool,omitempty"`
		Stats string `json:"stats,omitempty"`
		Diff  string `json:"diff,omitempty"`
	}{Found: ok, Path: path, Op: op, Tool: tool, Diff: diff, Stats: stats}
	b, _ := json.MarshalIndent(payload, "", "  ")
	return string(b)
}

// ---------------------------------------------------------------------------
// revert_my_changes
// ---------------------------------------------------------------------------

// handleRevertMyChanges restores files to their session-start state
// based on the requested scope. Scopes are mutually exclusive:
//
//   - scope="all"            Restore every file the tracker recorded.
//   - file="<path>"          Restore one file (latest matching entry).
//   - since="<RFC3339>"      Revert changes recorded at or after the
//                            given timestamp.
//
// Returns a JSON envelope listing per-file outcomes so the model can
// report exactly what happened back to the user.
func handleRevertMyChanges(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	scope := ""
	if s, ok := args["scope"].(string); ok {
		scope = strings.TrimSpace(s)
	}
	filePath, _ := args["file"].(string)
	sinceStr, _ := args["since"].(string)

	// Default scope = "all" when no narrowing arg is provided.
	if scope == "" && filePath == "" && sinceStr == "" {
		scope = "all"
	}

	tracker := a.GetChangeTracker()
	if tracker == nil || !tracker.IsEnabled() {
		return revertResult(0, 0, "change tracking is disabled — nothing to revert", nil), nil
	}

	candidates, err := selectRevertCandidates(tracker.GetChanges(), scope, filePath, sinceStr)
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
		action, ok, msg := revertOne(ch, tracker)
		results = append(results, entry{Path: ch.FilePath, Action: action, OK: ok, Message: msg})
		if ok {
			restored++
		} else {
			failed++
		}
	}
	summary := fmt.Sprintf("%d restored, %d failed (scope=%s)", restored, failed, describeScope(scope, filePath, sinceStr))
	return revertResult(restored, failed, summary, results), nil
}

// selectRevertCandidates collects ONE TrackedFileChange per path to
// revert — the OLDEST entry's original content. Reverting to the
// oldest pre-session state is the right behavior: even if the agent
// edited a file three times this session, the user wants "back to
// before the agent touched it", not "back to the previous edit".
func selectRevertCandidates(changes []TrackedFileChange, scope, file, since string) ([]TrackedFileChange, error) {
	var cutoff time.Time
	if since != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(since))
		if err != nil {
			return nil, fmt.Errorf("revert_my_changes: invalid 'since' (need RFC3339 like 2026-05-27T10:00:00Z): %w", err)
		}
		cutoff = t
	}

	var fileAbs string
	if file != "" {
		var err error
		fileAbs, err = filepath.Abs(file)
		if err != nil {
			return nil, fmt.Errorf("revert_my_changes: resolve file path: %w", err)
		}
	}

	// First pass: filter by scope/file/since. Then collapse to one
	// per-path entry, keeping the earliest (so we revert to the
	// truest pre-session state).
	filtered := make([]TrackedFileChange, 0, len(changes))
	for _, ch := range changes {
		if !cutoff.IsZero() && ch.Timestamp.Before(cutoff) {
			continue
		}
		if fileAbs != "" {
			chAbs, _ := filepath.Abs(ch.FilePath)
			if chAbs != fileAbs {
				continue
			}
		}
		filtered = append(filtered, ch)
	}

	if scope == "all" || scope == "" {
		// no further narrowing
	}

	// Collapse to earliest entry per path (preserving the first
	// OriginalCode encountered for each file). The slice is in
	// append-order so the first occurrence wins.
	seen := make(map[string]bool, len(filtered))
	earliest := make([]TrackedFileChange, 0, len(filtered))
	for _, ch := range filtered {
		key := ch.FilePath
		if seen[key] {
			continue
		}
		seen[key] = true
		earliest = append(earliest, ch)
	}
	return earliest, nil
}

// revertOne writes the change's OriginalCode back to disk (or removes
// the file if the change is a create-with-no-original). Returns
// (action, ok, message).
func revertOne(ch TrackedFileChange, tracker *ChangeTracker) (string, bool, string) {
	abs, err := filepath.Abs(ch.FilePath)
	if err != nil {
		return "", false, fmt.Sprintf("resolve path: %v", err)
	}

	if ch.Operation == "create" {
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			return "delete", false, fmt.Sprintf("remove created file: %v", err)
		}
		tracker.SyncShellCacheForPath(abs)
		return "delete", true, "removed file created during session"
	}

	if !isRecoverableOriginal(ch.OriginalCode) {
		return "", false, "original content was not captured (binary, oversized, or outside workspace)"
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", false, fmt.Sprintf("create parent dir: %v", err)
	}
	if err := os.WriteFile(abs, []byte(ch.OriginalCode), 0o644); err != nil {
		return "", false, fmt.Sprintf("write: %v", err)
	}
	tracker.SyncShellCacheForPath(abs)
	return "restore", true, "wrote original content back to disk"
}

func describeScope(scope, file, since string) string {
	switch {
	case file != "":
		return fmt.Sprintf("file=%s", file)
	case since != "":
		return fmt.Sprintf("since=%s", since)
	default:
		return scope
	}
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
// summarize_my_session
// ---------------------------------------------------------------------------

// activityGapThreshold is the time gap between consecutive changes
// that splits one "activity block" from the next. 30 seconds is long
// enough that work within a single agent turn clusters together but
// short enough that distinct turns separate cleanly.
const activityGapThreshold = 30 * time.Second

func handleSummarizeMySession(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	tracker := a.GetChangeTracker()
	if tracker == nil || !tracker.IsEnabled() {
		return `{"enabled":false,"blocks":[],"totals":{"changes":0,"files":0}}`, nil
	}
	changes := tracker.GetChanges()
	if len(changes) == 0 {
		return `{"enabled":true,"blocks":[],"totals":{"changes":0,"files":0}}`, nil
	}

	// Sort by timestamp so block detection is deterministic. The
	// tracker is append-order, which already approximates this, but
	// not guaranteed (e.g., direct hooks vs shell diff may arrive
	// out of order in rare cases).
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
		StartedAt time.Time           `json:"started_at"`
		EndedAt   time.Time           `json:"ended_at"`
		Tools     map[string]int      `json:"tools"`
		Files     []fileLite          `json:"files"`
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
		Enabled bool    `json:"enabled"`
		Blocks  []block `json:"blocks"`
		Totals  struct {
			Changes int `json:"changes"`
			Files   int `json:"files"`
		} `json:"totals"`
	}{Enabled: true, Blocks: blocks}
	out.Totals.Changes = len(changes)
	out.Totals.Files = len(allFiles)

	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ---------------------------------------------------------------------------
// my_recent_changes (Phase 1.5: bridge in-memory + persistent)
// ---------------------------------------------------------------------------

func handleMyRecentChanges(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	sinceArg, _ := args["since"].(string)
	cutoff, err := parseRecentSince(sinceArg)
	if err != nil {
		return "", err
	}

	tracker := a.GetChangeTracker()

	type item struct {
		Path       string    `json:"path"`
		Op         string    `json:"op"`
		Tool       string    `json:"tool"`
		Source     string    `json:"source"` // "session" | "persisted"
		RevisionID string    `json:"revision_id,omitempty"`
		Timestamp  time.Time `json:"timestamp"`
		Tier       string    `json:"tier,omitempty"`
	}
	var items []item

	// In-memory session buffer.
	if tracker != nil && tracker.IsEnabled() {
		for _, ch := range tracker.GetChanges() {
			if !cutoff.IsZero() && ch.Timestamp.Before(cutoff) {
				continue
			}
			items = append(items, item{
				Path:      ch.FilePath,
				Op:        ch.Operation,
				Tool:      ch.ToolCall,
				Source:    "session",
				Timestamp: ch.Timestamp,
			})
		}
	}

	// Persistent history (hot + warm tier; cold is dropped, so it
	// can't appear here anyway). Best-effort — failures yield an
	// empty list and we still return the session-buffer items.
	if persisted, err := history.GetAllChanges(); err == nil {
		for _, ch := range persisted {
			if !cutoff.IsZero() && ch.Timestamp.Before(cutoff) {
				continue
			}
			items = append(items, item{
				Path:       ch.Filename,
				Op:         deriveOpFromChangeLog(ch),
				Tool:       "(persisted)",
				Source:     "persisted",
				RevisionID: ch.RequestHash,
				Timestamp:  ch.Timestamp,
				Tier:       ch.Tier,
			})
		}
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Timestamp.After(items[j].Timestamp)
	})

	out := struct {
		Since string `json:"since,omitempty"`
		Count int    `json:"count"`
		Items []item `json:"items"`
	}{Count: len(items), Items: items}
	if !cutoff.IsZero() {
		out.Since = cutoff.Format(time.RFC3339)
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

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
	return time.Time{}, fmt.Errorf("my_recent_changes: 'since' must be RFC3339 (e.g. 2026-05-27T10:00:00Z), duration (2d, 12h, 30m), or empty; got %q", raw)
}

// deriveOpFromChangeLog infers the create/edit/delete code for a
// persisted change. The ChangeLog itself doesn't carry an op field,
// so we use the presence of OriginalCode + NewCode as the signal —
// same logic the cold-tier classifier used (but we don't go through
// that path anymore now that cold tier is gone; this is for the
// hot/warm persisted entries my_recent_changes surfaces).
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

// ---------------------------------------------------------------------------
// shared helpers (already used by list_changes / recover_file)
// ---------------------------------------------------------------------------

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
