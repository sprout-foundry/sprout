package design

import (
	"fmt"
	"sort"
	"strings"
)

// basisOrder ranks the bases for the canonical delta ordering: literal first
// (the safe mechanical subset), then structural, then inferred proposals.
func basisOrder(b deltaBasis) int {
	switch b {
	case DeltaBasisLiteral:
		return 0
	case DeltaBasisStructural:
		return 1
	case DeltaBasisInferred:
		return 2
	default:
		return 3
	}
}

// CompareDeltas is the canonical delta ordering: by basis, then kind, then
// code file, then the delta text. It is the determinism guarantee for the
// report — two runs over the same inputs emit the same Deltas slice.
func CompareDeltas(a, b SyncDelta) bool {
	if basisOrder(a.Basis) != basisOrder(b.Basis) {
		return basisOrder(a.Basis) < basisOrder(b.Basis)
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	if firstString(a.CodeFiles) != firstString(b.CodeFiles) {
		return firstString(a.CodeFiles) < firstString(b.CodeFiles)
	}
	if a.Delta != b.Delta {
		return a.Delta < b.Delta
	}
	return a.Token < b.Token
}

// firstString returns the first element of a slice, "" when empty.
func firstString(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

// sortAndDedupDeltas orders the deltas canonically and coalesces exact
// duplicates (same basis, kind, description, token, and code files) so a value
// repeated across two files is reported once per file but not twice per match.
func sortAndDedupDeltas(deltas []SyncDelta) []SyncDelta {
	seen := map[string]bool{}
	out := make([]SyncDelta, 0, len(deltas))
	for _, d := range deltas {
		if d.DesignFiles == nil {
			d.DesignFiles = []string{}
		}
		if d.CodeFiles == nil {
			d.CodeFiles = []string{}
		}
		sort.Strings(d.DesignFiles)
		sort.Strings(d.CodeFiles)
		key := strings.Join([]string{
			string(d.Basis), d.Kind, d.Delta, d.Token, d.Proposed,
			strings.Join(d.CodeFiles, ","), strings.Join(d.DesignFiles, ","),
		}, "\x00")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool { return CompareDeltas(out[i], out[j]) })
	return out
}

// -----------------------------------------------------------------------------
// Report summary
// -----------------------------------------------------------------------------

// RenderSyncSummary is the human/agent-readable one-liner for a report: the
// touched-file count and the delta split by basis, plus a pointer to the safe
// subset apply can write. Aimed at the model reading the ToolResult text.
func RenderSyncSummary(r *SyncReport) string {
	if r == nil {
		return "design_sync: no report."
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "design_sync (analyze): %d touched file(s), %d semantic delta(s)",
		r.TouchedCount, r.DeltaCount)
	if r.DeltaCount > 0 {
		parts := []string{}
		for _, b := range []deltaBasis{DeltaBasisLiteral, DeltaBasisStructural, DeltaBasisInferred} {
			if n := r.ByBasis[string(b)]; n > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", n, b))
			}
		}
		fmt.Fprintf(&sb, " (%s)", strings.Join(parts, ", "))
	}
	sb.WriteString(".")
	if safe := safeDeltaCount(r); safe > 0 {
		fmt.Fprintf(&sb, " %d safe to apply (run mode=apply to write them into design/).", safe)
	}
	if r.ByBasis[string(DeltaBasisInferred)] > 0 {
		fmt.Fprintf(&sb, " %d inferred proposal(s) need review.", r.ByBasis[string(DeltaBasisInferred)])
	}
	if len(r.Skipped) > 0 {
		fmt.Fprintf(&sb, " Skipped %d unreadable path(s).", len(r.Skipped))
	}
	return sb.String()
}

// safeDeltaCount counts the deltas apply may write mechanically.
func safeDeltaCount(r *SyncReport) int {
	if r == nil {
		return 0
	}
	n := 0
	for _, d := range r.Deltas {
		if d.SafeToApply {
			n++
		}
	}
	return n
}

// -----------------------------------------------------------------------------
// Apply half — SP-140-5 §5b apply mode
// -----------------------------------------------------------------------------
//
// The apply half is the pure planning core for `design_sync` mode=apply. It
// takes a report (as produced by AnalyzeTouchedFiles) plus a reader for the
// current bytes of an existing design file, and returns a *SyncApplyPlan*: the
// ordered design-file writes the safe subset requires.
//
// The §5e rule is baked into the type system here, not just the prose:
//
//   - design_sync --apply writes design files; it NEVER rewrites the
//     implementation. The plan carries only design/-confined writes — the
//     handler is the one that performs I/O, and it refuses any plan whose
//     write set escapes design/ (see plan.IsConfinedToDesign).
//   - The semantic layer is *invited to adopt*, never auto-enforced onto code:
//     there is no "edit the implementation to match design/" operation in
//     this file at all, and none is reachable from the plan.
//
// The safe subset §5b names is exactly the deltas with SafeToApply set:
//
//   - literal token renames/revalues → rewrite the referenced DTCG entry
//     (revalue) or add the missing renamed entry, in the delta's TokenFile.
//   - structural new route/screen → create a skeleton wireframe SVG with
//     `draft` status, and append the proposed flow edge to the flow file.
//   - wireframe attribute/sidecar updates → the skeleton wireframe carries the
//     `draft` status marker and the proposed stem.
//
// Inferred deltas are *proposals*: the plan records them in Proposals and
// writes nothing for them (§5b "inferred ones are proposals"; AC "apply does
// not auto-create tokens without the literal/structural confidence bar").
//
// Everything is deterministic: the plan's writes and proposals are ordered by
// design path, and each write's bytes are a pure function of the delta and the
// file's current bytes.
