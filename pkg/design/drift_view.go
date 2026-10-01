// drift_view.go — the design_validate / design_assets reporting surface for
// the §5c drift report, split from drift.go (which owns the analysis core:
// AnalyzeDrift + the per-direction analyzers). These functions render a
// DriftReport as validator findings, a summary line, and per-direction lookups,
// so both reporting surfaces share one vocabulary with the analyzer.
package design

import (
	"fmt"
	"strings"
)

// DriftDirectionFindings maps a drift report onto design_validate findings:
// one advisory finding per direction row that is AHEAD (§5c: both directions
// are reported, and the tools share the same rows). A synced direction emits
// no finding — the always-present two-row shape lives in the structured drift
// section, and a "nothing to do" finding on every clean run would be noise
// rather than signal. When a direction is ahead its finding names the count
// and the remedy, so the validator surface carries the run-me pointer too.
//
// Severity is advisory by construction: ahead rows are warns (an actionable
// remedy), never errors. Drift is signal, not guilt (§5c).
func DriftDirectionFindings(report *DriftReport) []Finding {
	if report == nil {
		return []Finding{}
	}
	out := make([]Finding, 0, len(report.Rows))
	for _, row := range report.Rows {
		if !row.Ahead {
			continue
		}
		out = append(out, Finding{
			File:     DirName,
			Severity: SeverityWarn,
			Message:  driftFindingMessage(row),
			Rule:     DriftRuleID(row.Direction),
		})
	}
	return out
}

// DriftRuleID is the design_validate rule id for one drift direction row. The
// ids are the direction names (design-ahead / code-ahead), prefixed with the
// shared `drift_` namespace so a consumer can tell a drift row from a validator
// rule at a glance.
func DriftRuleID(direction string) string {
	return "drift_" + strings.ReplaceAll(direction, "-", "_")
}

// driftFindingMessage renders one direction row as a finding message: the
// summary and the remedy, or the "nothing to do" statement when synced.
func driftFindingMessage(row DriftDirectionRow) string {
	if !row.Ahead {
		return fmt.Sprintf("Drift (%s): nothing to do — this direction is in sync.", row.Direction)
	}
	if row.Remedy == "" {
		return fmt.Sprintf("Drift (%s): %s", row.Direction, row.Summary)
	}
	if row.Summary == "" {
		return fmt.Sprintf("Drift (%s): %s", row.Direction, row.Remedy)
	}
	return fmt.Sprintf("Drift (%s): %s %s", row.Direction, row.Summary, row.Remedy)
}

// DriftSummaryLine renders the report as one human/agent-readable sentence for
// the design_assets summary line: both directions named with their counts, so
// the two-row shape is visible in the text as well as the JSON.
func DriftSummaryLine(report *DriftReport) string {
	if report == nil {
		return ""
	}
	var designAhead, codeAhead DriftDirectionRow
	for _, r := range report.Rows {
		switch r.Direction {
		case DriftRowDesignAhead:
			designAhead = r
		case DriftRowCodeAhead:
			codeAhead = r
		}
	}
	if report.Synced {
		return "Drift: in sync — design-ahead: none; code-ahead: none."
	}
	parts := []string{}
	if designAhead.Ahead {
		parts = append(parts, fmt.Sprintf("design-ahead %d (remedy: %s)", designAhead.Count, designAhead.NextStep))
	} else {
		parts = append(parts, "design-ahead none")
	}
	if codeAhead.Ahead {
		parts = append(parts, fmt.Sprintf("code-ahead %d (remedy: %s)", codeAhead.Count, codeAhead.NextStep))
	} else {
		parts = append(parts, "code-ahead none")
	}
	return "Drift: " + strings.Join(parts, "; ") + "."
}

// DriftDirectionRowFor returns the report's row for one direction, or a zero
// row when the direction is unknown. Both handlers use it so a consumer reads a
// direction by name rather than by slice index.
func DriftDirectionRowFor(report *DriftReport, direction string) DriftDirectionRow {
	if report == nil {
		return DriftDirectionRow{Direction: direction, Synced: true, Advisory: DriftSeverityInfo}
	}
	for _, row := range report.Rows {
		if row.Direction == direction {
			return row
		}
	}
	return DriftDirectionRow{Direction: direction, Synced: true, Advisory: DriftSeverityInfo}
}

// driftDirectionOrder exposes the fixed direction order to consumers that build
// their own view of the report (the design_assets output rows).
func DriftDirections() []string {
	return append([]string(nil), driftDirectionOrder...)
}
