package design

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// -----------------------------------------------------------------------------
// Design status — SP-140-6 §6b (TODO item 6.2)
// -----------------------------------------------------------------------------
//
// BuildDesignStatus is the read-side aggregate behind GET /api/design/status:
// one struct combining the three loop signals the webui health strip renders
// (SP-140-6 §6c) — validation tallies, the two §5c drift rows, and pending §4d
// feedback — plus the per-token reference counts the co-editing alias warning
// needs (SP-140-7 §7c).
//
// It is aggregation ONLY: every number comes from the already-exported
// scanners the agent tools use (ValidateTree, AnalyzeDrift, ScanFeedbackDir),
// so the webui reads the same truth the agent reads and no validator logic is
// forked to the client. No I/O beyond those scanners' own reads; deterministic
// output (sorted findings, sorted feedback, sorted token refs).

// StatusFindingsCap is the maximum number of findings the status payload
// carries. The tallies stay authoritative (they count everything); the list is
// capped so one broken tree cannot flood a browser. The cap keeps the
// validator's canonical order (first N in sorted order).
const StatusFindingsCap = 200

// StatusFinding is one validator finding in the status payload — the §1g
// finding shape ({file, line?, severity, message, rule}) with JSON tags for
// the HTTP surface.
type StatusFinding struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Rule     string `json:"rule"`
}

// StatusValidation is the validation section: authoritative tallies plus the
// capped finding list. Warnings count SeverityWarn AND SeverityFix — both are
// advisory-and-actionable classes, and the strip renders one "warnings" chip.
type StatusValidation struct {
	Errors   int             `json:"errors"`
	Warnings int             `json:"warnings"`
	Infos    int             `json:"infos"`
	Findings []StatusFinding `json:"findings"`
}

// StatusDriftRow is one §5c drift direction for the status payload: the fields
// the health strip renders (count + remedy, §6c) plus NextStep so the strip
// can route a click (design-ahead → Tokens tab; code-ahead → agent-panel
// prefill, SP-140-6 §6c). Shape-mirrors DriftDirectionRow without exposing the
// full evidence machinery.
type StatusDriftRow struct {
	Ahead    bool   `json:"ahead"`
	Synced   bool   `json:"synced"`
	Count    int    `json:"count"`
	Summary  string `json:"summary,omitempty"`
	Remedy   string `json:"remedy,omitempty"`
	NextStep string `json:"nextStep,omitempty"`
}

// StatusFeedbackEntry is one pending feedback target in the status payload:
// the §4d target and how many annotations are still open on it.
type StatusFeedbackEntry struct {
	Target     string `json:"target"`
	Unresolved int    `json:"unresolved"`
	Status     string `json:"status,omitempty"`
}

// StatusFeedback is the feedback section: the pending count plus the sorted
// pending targets.
type StatusFeedback struct {
	PendingCount int                   `json:"pendingCount"`
	Pending      []StatusFeedbackEntry `json:"pending"`
}

// DesignStatus is the full GET /api/design/status payload. Exists=false (no
// design/ directory) is reported with a 200 and zeroed sections — the health
// strip renders "no design tree", not an error (§6b).
type DesignStatus struct {
	Exists     bool             `json:"exists"`
	Validation StatusValidation `json:"validation"`
	// Drift carries the two §5c rows in fixed order plus the overall synced
	// flag (true when neither direction is ahead).
	Drift struct {
		DesignAhead StatusDriftRow `json:"designAhead"`
		CodeAhead   StatusDriftRow `json:"codeAhead"`
		Synced      bool           `json:"synced"`
	} `json:"drift"`
	Feedback StatusFeedback `json:"feedback"`
	// TokenRefs maps a dotted token path ({color.brand.primary}) to the number
	// of lexical alias references to it across design/tokens/*.tokens.json —
	// the SP-140-7 §7c alias-warning input ("N tokens reference this").
	// Unreferenced tokens are absent. Deterministic via JSON map marshalling.
	TokenRefs map[string]int `json:"tokenRefs,omitempty"`
	// Summary is the one-line human/agent-readable rollup for logs and titles.
	Summary string `json:"summary"`
}

// statusFindingSort orders findings deterministically before capping, using
// the validator's own canonical key order (file, line, rule, message — the
// same precedence as sortFindings in tokens.go), so the capped "first N" IS
// the validator's first N and past-cap drops agree with design_validate.
func statusFindingSort(findings []Finding) {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		if findings[i].Rule != findings[j].Rule {
			return findings[i].Rule < findings[j].Rule
		}
		return findings[i].Message < findings[j].Message
	})
}

// statusValidation converts validator findings into the status section: full
// tallies, capped sorted list. A ValidateTree walk error becomes one synthetic
// error finding so a broken walk never reads as a clean tree.
func statusValidation(findings []Finding, walkErr error) StatusValidation {
	out := StatusValidation{Findings: []StatusFinding{}}
	if walkErr != nil {
		out.Errors = 1
		out.Findings = append(out.Findings, StatusFinding{
			File:     DirName,
			Severity: string(SeverityError),
			Message:  fmt.Sprintf("validation walk failed: %v", walkErr),
			Rule:     "status_validation_failed",
		})
		return out
	}
	// Work on a copy: the caller owns the slice ValidateTree returned.
	sorted := append([]Finding(nil), findings...)
	statusFindingSort(sorted)
	for _, f := range sorted {
		switch f.Severity {
		case SeverityError:
			out.Errors++
		case SeverityWarn, SeverityFix:
			out.Warnings++
		default:
			out.Infos++
		}
		if len(out.Findings) < StatusFindingsCap {
			out.Findings = append(out.Findings, StatusFinding{
				File:     f.File,
				Line:     f.Line,
				Severity: string(f.Severity),
				Message:  f.Message,
				Rule:     f.Rule,
			})
		}
	}
	return out
}

// statusDriftRow converts one DriftDirectionRow into the payload shape.
func statusDriftRow(row DriftDirectionRow) StatusDriftRow {
	return StatusDriftRow{
		Ahead:    row.Ahead,
		Synced:   row.Synced,
		Count:    row.Count,
		Summary:  row.Summary,
		Remedy:   row.Remedy,
		NextStep: row.NextStep,
	}
}

// statusFeedback converts ScanFeedbackDir's states into the payload section:
// pending targets (IsPending, §4d) sorted by target, each with its open-
// annotation count. Invalid files are never pending, so a broken JSON file
// cannot fabricate pending work.
func statusFeedback(states []FeedbackFileState, err error) StatusFeedback {
	out := StatusFeedback{Pending: []StatusFeedbackEntry{}}
	if err != nil {
		return out
	}
	for _, state := range states {
		if !state.IsPending() {
			continue
		}
		out.PendingCount++
		out.Pending = append(out.Pending, StatusFeedbackEntry{
			Target:     state.Target,
			Unresolved: state.Pending,
			Status:     state.Status,
		})
	}
	sort.Slice(out.Pending, func(i, j int) bool {
		return out.Pending[i].Target < out.Pending[j].Target
	})
	return out
}

// tokenRefPattern matches a DTCG alias reference ({group.token}) inside token
// file content. Lexical by design: it counts references wherever they appear
// in a token document (a $value alias, or the {token.path} comment convention),
// which is the right basis for an editing warning ("N references — update
// them or check the alias graph"), not for validation.
var tokenRefPattern = regexp.MustCompile(`\{([a-zA-Z][a-zA-Z0-9_.\-]*)\}`)

// countTokenRefs walks design/tokens/*.tokens.json and counts lexical alias
// references per dotted token path (sorted file order; counts accumulate).
// Unreadable files are skipped — the validator owns read errors; the ref map
// is advisory input to a UI warning.
func countTokenRefs(root string) map[string]int {
	refs := map[string]int{}
	matches, err := filepath.Glob(filepath.Join(root, DirName, TokenSubdir, "*.tokens.json"))
	if err != nil {
		return refs
	}
	sort.Strings(matches)
	for _, match := range matches {
		data, readErr := os.ReadFile(match)
		if readErr != nil {
			continue
		}
		for _, m := range tokenRefPattern.FindAllStringSubmatch(string(data), -1) {
			refs[m[1]]++
		}
	}
	return refs
}

// BuildDesignStatus computes the §6b status for the workspace at root. touched
// is the code-ahead evidence set: an HTTP read has none (pass nil), and the
// code-ahead row then reports synced/not-assessable with its evidence — the
// honest tree-only answer (§5c). The call never fails: scanner errors become
// zeroed sections or synthetic findings, never an error return, so the
// endpoint has one shape.
func BuildDesignStatus(root string, touched []SyncFileInput) *DesignStatus {
	status := &DesignStatus{Exists: FileExists(root)}
	if !status.Exists {
		// Zeroed sections still marshal as real arrays/objects, not nulls —
		// the TS contract (designStatusApi.ts) types findings/pending as
		// non-optional arrays and consumers iterate them after an exists check.
		status.Validation = StatusValidation{Findings: []StatusFinding{}}
		status.Feedback = StatusFeedback{Pending: []StatusFeedbackEntry{}}
		status.Drift.Synced = true
		status.Summary = "No design/ tree in this workspace."
		return status
	}

	findings, walkErr := ValidateTree(root)
	status.Validation = statusValidation(findings, walkErr)

	drift := AnalyzeDrift(root, touched)
	status.Drift.DesignAhead = statusDriftRow(DriftDirectionRowFor(drift, DriftRowDesignAhead))
	status.Drift.CodeAhead = statusDriftRow(DriftDirectionRowFor(drift, DriftRowCodeAhead))
	status.Drift.Synced = drift.Synced

	fbStates, fbErr := ScanFeedbackDir(root)
	status.Feedback = statusFeedback(fbStates, fbErr)
	if fbErr != nil {
		// A feedback read failure must not read as "no pending feedback"
		// (the same never-false-clean rule as the validation walk above).
		status.Summary = fmt.Sprintf("design status incomplete: feedback scan failed: %v", fbErr)
		return status
	}

	status.TokenRefs = countTokenRefs(root)
	status.Summary = statusSummary(status)
	return status
}

// statusSummary renders the one-line rollup: validation tallies, drift
// directions with counts, pending feedback.
func statusSummary(s *DesignStatus) string {
	var b strings.Builder
	fmt.Fprintf(&b, "design: %d error(s), %d warning(s), %d info", s.Validation.Errors, s.Validation.Warnings, s.Validation.Infos)
	if s.Drift.Synced {
		b.WriteString("; drift: in sync")
	} else {
		b.WriteString("; drift:")
		if s.Drift.DesignAhead.Ahead {
			fmt.Fprintf(&b, " design-ahead %d", s.Drift.DesignAhead.Count)
		}
		if s.Drift.CodeAhead.Ahead {
			fmt.Fprintf(&b, " code-ahead %d", s.Drift.CodeAhead.Count)
		}
	}
	if s.Feedback.PendingCount > 0 {
		fmt.Fprintf(&b, "; %d pending feedback target(s)", s.Feedback.PendingCount)
	}
	return b.String()
}
