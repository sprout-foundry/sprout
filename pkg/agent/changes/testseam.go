package changes

import (
	"path/filepath"
)

// Test seams for cross-package change-tracking tests (SP-141 phase 2,
// spec principle #4: "imported by subpackage tests via an exported
// testutil seam"). The cluster's unexported fields are unreachable
// through the pkg/agent type alias, so agent-side test files build and
// inspect trackers through these instead of struct literals.

// TestTrackerSpec is the field set an agent-side test may seed on a
// ChangeTracker. Zero-value fields stay zero — matching the pre-move
// struct literals exactly (e.g. a site that only set enabled+changes
// leaves RevisionID/SessionID empty).
type TestTrackerSpec struct {
	RevisionID       string
	SessionID        string
	Enabled          bool
	ShellWalkEnabled bool
	Changes          []TrackedFileChange
	Agent            ChangeAgent
}

// NewTestTracker builds a ChangeTracker from a TestTrackerSpec for
// cross-package tests. The zero-value spec yields a disabled, empty
// tracker.
func NewTestTracker(spec TestTrackerSpec) *ChangeTracker {
	return &ChangeTracker{
		revisionID:       spec.RevisionID,
		sessionID:        spec.SessionID,
		enabled:          spec.Enabled,
		shellWalkEnabled: spec.ShellWalkEnabled,
		changes:          spec.Changes,
		agent:            spec.Agent,
	}
}

// SetTestTrackerAgent wires the tracker's owning agent after the fact
// (tests that build the Agent with the tracker already installed need
// the back-reference).
func SetTestTrackerAgent(ct *ChangeTracker, agent ChangeAgent) {
	if ct == nil {
		return
	}
	ct.agent = agent
}

// TestTrackerSessionID exposes the tracker's session ID for cross-package
// assertion (session-rotation tests).
func TestTrackerSessionID(ct *ChangeTracker) string {
	if ct == nil {
		return ""
	}
	return ct.sessionID
}

// ShellCachePrimed reports whether the shell-snapshot cache has been
// primed. Read-only under the cache lock so agent-side tests can assert
// lazy-vs-eager priming without touching the field.
func ShellCachePrimed(ct *ChangeTracker) bool {
	if ct == nil {
		return false
	}
	ct.shellCacheMu.Lock()
	defer ct.shellCacheMu.Unlock()
	return ct.shellCache != nil
}

// ResolveRecoveryTarget finds the most recent TrackedFileChange covering
// `abs` — either as a top-level entry or as a per-file item packed inside
// a bulk entry. For bulk items the returned pointer is to a synthesized
// TrackedFileChange carrying the bulk row's Timestamp and ToolCall. Moved
// from the recover tool handler so the in-package tests reach it; the
// handler calls it back.
func ResolveRecoveryTarget(all []TrackedFileChange, abs string) *TrackedFileChange {
	for i := len(all) - 1; i >= 0; i-- {
		ch := &all[i]
		candidatePath, err := filepath.Abs(ch.FilePath)
		if err == nil && candidatePath == abs && ch.Operation != "bulk" {
			return ch
		}
		if ch.Operation != "bulk" || len(ch.BulkItems) == 0 {
			continue
		}
		for j := len(ch.BulkItems) - 1; j >= 0; j-- {
			item := ch.BulkItems[j]
			itemPath, ierr := filepath.Abs(item.FilePath)
			if ierr != nil || itemPath != abs {
				continue
			}
			synthesized := TrackedFileChange{
				FilePath:     item.FilePath,
				OriginalCode: item.OriginalCode,
				NewCode:      item.NewCode,
				Operation:    item.Operation,
				Timestamp:    ch.Timestamp,
				ToolCall:     ch.ToolCall,
			}
			return &synthesized
		}
	}
	return nil
}

// ResolveEarliestRecoveryTarget is the scope="session_start" sibling of
// ResolveRecoveryTarget — it walks changes in append order so the FIRST
// matching entry wins (the truest pre-session state). Bulk items count
// as candidates too; the earliest individual entry — bulk-packed or
// otherwise — for the path is the answer.
func ResolveEarliestRecoveryTarget(all []TrackedFileChange, abs string) *TrackedFileChange {
	for i, ch := range all {
		candidatePath, err := filepath.Abs(ch.FilePath)
		if err == nil && candidatePath == abs && ch.Operation != "bulk" {
			return &all[i]
		}
		if ch.Operation != "bulk" || len(ch.BulkItems) == 0 {
			continue
		}
		for _, item := range ch.BulkItems {
			itemPath, ierr := filepath.Abs(item.FilePath)
			if ierr != nil || itemPath != abs {
				continue
			}
			synthesized := TrackedFileChange{
				FilePath:     item.FilePath,
				OriginalCode: item.OriginalCode,
				NewCode:      item.NewCode,
				Operation:    item.Operation,
				Timestamp:    ch.Timestamp,
				ToolCall:     ch.ToolCall,
			}
			return &synthesized
		}
	}
	return nil
}
