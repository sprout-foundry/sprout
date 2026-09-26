package agent

// tool_handlers_changes_revert.go — the revert_my_changes tool: the
// handler, candidate selection / scope parsing, per-file revert execution,
// and the staleness / recoverability checks, split out of
// tool_handlers_changes.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/history"
)

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
		return revertResultDisabled("change tracking is disabled — nothing to revert"), nil
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
			return nil, agenterrors.Wrap(err, fmt.Sprintf("revert_my_changes: invalid 'since' (need RFC3339 like 2026-05-27T10:00:00Z or duration like 30m)"))
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
	if isStaleForRevertWithOriginal(abs, ch.NewCode, ch.OriginalCode) {
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

// revertResultDisabled is the disabled-tracker variant: enabled=false
// tells callers (WebUI, LLM) the request could not be served at all,
// as opposed to "served but nothing matched".
func revertResultDisabled(summary string) string {
	payload := struct {
		Enabled  bool   `json:"enabled"`
		Restored int    `json:"restored"`
		Failed   int    `json:"failed"`
		Summary  string `json:"summary"`
	}{Enabled: false, Restored: 0, Failed: 0, Summary: summary}
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
	return time.Time{}, agenterrors.NewValidation(fmt.Sprintf("'since' must be RFC3339 (e.g. 2026-05-27T10:00:00Z), duration (2d, 12h, 30m), or empty; got %q", raw), nil)
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

// isStaleForRevertWithOriginal is the original-aware variant used by
// recovery paths that have the snapshot's OriginalCode. This allows
// recovery of uncommitted work destroyed by a destructive git command
// (git checkout, git reset, git clean) that aligned the file to HEAD.
func isStaleForRevertWithOriginal(absPath, newCode, originalCode string) bool {
	return !history.IsRevertSafeWithOriginal(absPath, newCode, originalCode)
}
