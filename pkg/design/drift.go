package design

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// -----------------------------------------------------------------------------
// Drift direction — SP-140-5 §5c
// -----------------------------------------------------------------------------
//
// §5c reinterprets "stale" as *signal*, not defect. There are two directions
// the design/ tree and the implementation can drift apart, and they mean
// opposite things:
//
//   - Design-ahead (design/ changed, generated/code behind): the expected
//     healthy state of an active project. Generated outputs are regenerable
//     (design_export_tokens) and code catching up is normal work-in-progress.
//     Surfaced as actionable ("regenerate theme; 2 screens ahead of build").
//
//   - Code-ahead (implementation changed, semantic layer behind): the state
//     design_sync exists to fix. Surfaced with a run-me pointer
//     ("run design_sync to import 3 semantic deltas").
//
// The two are ALWAYS reported as two distinct rows with two distinct remedies,
// even when one side has nothing to say: a stable shape a model can read
// without inferring which state it is in. The drift guard survives from the
// previous draft, reinterpreted: it reports direction and remedy, not guilt.
//
// Both handlers that report drift (design_assets and design_validate) go
// through this one analyzer, so the vocabulary and the counts agree by
// construction.

// Drift direction ids, fixed by §5c. These are the stable row identifiers both
// design_assets and design_validate emit.
const (
	// DriftDirectionDesignAhead is design/ ahead of generated/code.
	DriftDirectionDesignAhead = "design-ahead"
	// DriftDirectionCodeAhead is implementation ahead of the semantic layer.
	DriftDirectionCodeAhead = "code-ahead"
	// DriftDirectionSynced is the balanced row: neither side is ahead. Both
	// direction rows still report, with Synced=true and an empty remedy, so a
	// consumer reads a stable two-row shape and "no drift" is explicit.
	DriftDirectionSynced = "synced"
)

// Drift severities. Drift is signal, never an error (§5c): both rows are
// advisory. info = "nothing to do", warn = "there is a remedy to run".
const (
	// DriftSeverityInfo marks a row with no remedy (in sync, or ahead with
	// nothing countable to point at).
	DriftSeverityInfo = "info"
	// DriftSeverityWarn marks a row that carries an actionable remedy.
	DriftSeverityWarn = "warn"
)

// Drift rows (stable row order). Every DriftReport carries exactly these two,
// in this order.
const (
	// DriftRowDesignAhead is the design-ahead row.
	DriftRowDesignAhead = "design-ahead"
	// DriftRowCodeAhead is the code-ahead row.
	DriftRowCodeAhead = "code-ahead"
)

// Drift remedy next-steps: the run-me pointers §5c fixes. They are stable
// strings the handler rows and the tests assert, so the two surfaces cannot
// drift apart in wording either.
const (
	// DriftDesignAheadNextStep is the design-ahead remedy pointer: regenerate
	// the generated theme from the token sources (§5a). The count of screens
	// ahead of the build is reported in the row's Count/Summary.
	DriftDesignAheadNextStep = "Regenerate the exported theme with design_export_tokens; the generated/ outputs are regenerable and code catching up is normal work-in-progress."
	// DriftCodeAheadNextStep is the code-ahead remedy pointer: import the
	// semantic deltas the implementation introduced (§5b).
	DriftCodeAheadNextStep = "Run design_sync to import the implementation's semantic deltas into design/, then design_export_tokens to regenerate the theme."
)

// DriftDirectionRow is one direction row of the drift report (§5c). Both
// directions always produce a row; a row with nothing to report reads
// Synced=true with an empty Count and Remedy, so the two-row shape is stable.
type DriftDirectionRow struct {
	// Direction is design-ahead | code-ahead (the stable row id).
	Direction string `json:"direction"`
	// Ahead is true when this direction actually holds (there is drift here).
	Ahead bool `json:"ahead"`
	// Synced is true when this direction has nothing ahead — the complement of
	// Ahead, explicit so a model reads "no drift" without a negation.
	Synced bool `json:"synced"`
	// Count is the concrete magnitude of the drift: screens ahead of the build
	// (design-ahead) or semantic deltas to import (code-ahead). 0 when Synced
	// or when nothing countable was found.
	Count int `json:"count"`
	// Summary is the one-line description of what is ahead ("2 screens ahead
	// of the exported theme"). Empty when Synced.
	Summary string `json:"summary,omitempty"`
	// Remedy is the run-me pointer for this direction (§5c): regenerate the
	// theme (design-ahead) or run design_sync (code-ahead). Empty when Synced.
	Remedy string `json:"remedy,omitempty"`
	// NextStep is the tool-level pointer the remedy names (the design_export_tokens
	// or design_sync call), stable so a consumer can route on it. Empty when Synced.
	NextStep string `json:"nextStep,omitempty"`
	// Advisory is the severity class: info (nothing to do) or warn (a remedy to
	// run). Drift is signal, never an error (§5c), so this is never "error".
	Advisory string `json:"advisory"`
}

// DriftReport is the §5c drift-direction view: the two direction rows (always
// both, always in DriftRowDesignAhead, DriftRowCodeAhead order) plus the
// mapping of each row onto a design_validate finding, so both reporting
// surfaces share one vocabulary. It is deterministic: the rows are fixed in
// order and every count is derived from a sorted walk.
type DriftReport struct {
	// Rows are the two direction rows, design-ahead then code-ahead.
	Rows []DriftDirectionRow `json:"rows"`
	// DesignAheadCount / CodeAheadCount mirror the rows for a reader that does
	// not want to index Rows.
	DesignAheadCount int `json:"designAheadCount"`
	CodeAheadCount   int `json:"codeAheadCount"`
	// Synced is true when neither direction is ahead.
	Synced bool `json:"synced"`
	// Evidence carries how each count was computed (the generated provenance
	// hash basis, the touched-file basis, or the offline fallback), so a model
	// can judge the confidence of a count. Deterministic order.
	Evidence []string `json:"evidence,omitempty"`
}

// Rows are the fixed two directions, in report order.
var driftDirectionOrder = []string{DriftRowDesignAhead, DriftRowCodeAhead}

// AnalyzeDrift computes the §5c drift report for one workspace.
//
// It is deliberately cheap and deterministic — it is called by both
// design_assets and design_validate, tools a turn runs freely — and it never
// fails a caller: a workspace with no design/ tree yields a synced report
// rather than an error (the design_assets handler's {exists:false} path is the
// scaffold signal; drift is only meaningful once a tree exists).
//
// The two signals:
//
//   - design-ahead — the design/ tree versus the exported provenance. When
//     design/generated/ carries the §5a source-hash provenance header, the
//     current token-input hash is recomputed and compared: a difference means
//     the tokens moved after the last export, so the generated theme is behind.
//     Screens/wireframes give the count ("N screens ahead of the build").
//     Without a generated/ tree the whole tree counts as ahead — the design
//     side exists and nothing has been exported from it yet.
//
//   - code-ahead — the implementation versus the semantic layer. When the
//     caller knows the touched code files (design_validate can ask the
//     ChangeTracker; a standalone design_assets run passes none), the §5b
//     analysis is reused verbatim: the delta count is exactly what design_sync
//     would import, restricted to the literal/structural subset design_sync can
//     actually adopt. Without a touched set, whether the implementation is
//     ahead is simply not knowable from the tree alone, so the row reports
//     synced and Evidence says so — the remedy direction is unchanged, the
//     count is just not claimable. This is core-owned deliberately: the pkg/agent_tools
//     layer supplies the touched files, the semantics live here.
func AnalyzeDrift(root string, touched []SyncFileInput) *DriftReport {
	report := &DriftReport{Rows: []DriftDirectionRow{}}

	designAhead, evidence := analyzeDesignAhead(root)
	report.Evidence = append(report.Evidence, evidence...)
	report.DesignAheadCount = designAhead.Count

	codeAhead := analyzeCodeAhead(root, touched)
	if codeAhead.Evidence != "" {
		report.Evidence = append(report.Evidence, codeAhead.Evidence)
	}
	report.CodeAheadCount = codeAhead.Row.Count

	report.Rows = append(report.Rows, designAhead, codeAhead.Row)
	report.Synced = !designAhead.Ahead && !codeAhead.Row.Ahead
	return report
}

// driftSignal is the internal shape of one computed direction: the row plus an
// optional evidence line.
type driftSignal struct {
	Row      DriftDirectionRow
	Evidence string
}

// analyzeDesignAhead computes the design-ahead row: is design/ ahead of its
// generated outputs, and by how many screens.
//
// §5c's design-ahead is a *divergence* signal, so it requires something to
// diverge from: a design/generated/ export. The row is ahead exactly when the
// export exists and its §5a provenance source-hash differs from the current
// token-input hash — i.e. design/tokens moved after the last export, so the
// generated theme is behind (the healthy state of an active project, whose
// remedy is simply to regenerate). The magnitude reported is the number of
// screens/flows in the tree (the "2 screens ahead of build" count).
//
// A tree with no design/generated/ has not adopted export yet, so there is
// nothing to be behind: that is a separate concern from drift, and reporting
// it as drift would fire on every fresh tree. The row reports synced, with the
// evidence line saying why the direction is not assessable.
func analyzeDesignAhead(root string) (DriftDirectionRow, []string) {
	row := DriftDirectionRow{Direction: DriftRowDesignAhead}
	var evidence []string

	hasGenerated, tokenStale, genEvidence := generatedProvenanceState(root)
	if genEvidence != "" {
		evidence = append(evidence, genEvidence)
	}

	if !hasGenerated {
		row.Synced = true
		row.Advisory = DriftSeverityInfo
		evidence = append(evidence,
			"No design/generated/ export found, so design-ahead is not assessable (nothing to be behind); run design_export_tokens to establish the generated baseline.")
		return row, evidence
	}

	if !tokenStale {
		row.Synced = true
		row.Advisory = DriftSeverityInfo
		evidence = append(evidence,
			"design/generated/ provenance hash matches the current design/tokens/*.tokens.json input hash; the generated theme is current.")
		return row, evidence
	}

	// Ahead: the tokens moved after the last export. The magnitude is the
	// design surfaces that exist in the tree (screens/flows ahead of the build).
	ahead := designSurfacesAhead(root)
	row.Ahead = true
	row.Advisory = DriftSeverityWarn
	row.Count = ahead
	if ahead > 0 {
		row.Summary = fmt.Sprintf("design/tokens changed since the last export; %d screen(s)/flow(s) ahead of the build.", ahead)
	} else {
		row.Summary = "design/tokens changed since the last export; the generated theme is behind."
	}
	evidence = append(evidence,
		"design/generated/ provenance hash differs from the current design/tokens/*.tokens.json input hash.")
	row.Remedy = DriftDesignAheadNextStep
	row.NextStep = "design_export_tokens"
	return row, evidence
}

// driftCodeAhead is code-ahead's computed row plus its evidence.
type driftCodeAhead struct {
	Row      DriftDirectionRow
	Evidence string
}

// analyzeCodeAhead computes the code-ahead row: has the implementation moved
// ahead of the semantic layer?
func analyzeCodeAhead(root string, touched []SyncFileInput) driftCodeAhead {
	row := DriftDirectionRow{Direction: DriftRowCodeAhead}

	if len(touched) == 0 {
		// No touched set: the tree alone cannot say whether the implementation
		// is ahead. Report synced and say why.
		row.Synced = true
		row.Advisory = DriftSeverityInfo
		return driftCodeAhead{
			Row: row,
			Evidence: "No touched code files supplied, so code-ahead is not assessable from the tree alone; " +
				"pass the turn's changed files (design_validate reads them itself) to measure it.",
		}
	}

	// Reuse the §5b analysis verbatim: the code-ahead count is exactly what
	// design_sync would import. Only the literal/structural subset is
	// countable as "to import" (the safe-to-apply set); inferred deltas are
	// proposals and are reported separately in Evidence so the row does not
	// overclaim.
	report, err := AnalyzeTouchedFiles(SyncInput{Root: root, Touched: touched})
	if err != nil || report == nil {
		row.Synced = true
		row.Advisory = DriftSeverityInfo
		return driftCodeAhead{
			Row:      row,
			Evidence: "Semantic-delta analysis could not run, so code-ahead is reported as synced.",
		}
	}

	importable := safeDeltaCount(report)
	inferred := report.ByBasis[string(DeltaBasisInferred)]
	if importable == 0 {
		row.Synced = true
		row.Advisory = DriftSeverityInfo
		ev := fmt.Sprintf("Analysed %d touched code file(s): no importable semantic deltas.", len(touched))
		if inferred > 0 {
			ev += fmt.Sprintf(" %d inferred proposal(s) need review (they are proposals, not deltas to import).", inferred)
		}
		return driftCodeAhead{Row: row, Evidence: ev}
	}

	row.Ahead = true
	row.Advisory = DriftSeverityWarn
	row.Count = importable
	if inferred > 0 {
		row.Summary = fmt.Sprintf("implementation is ahead: %d semantic delta(s) to import (%d inferred proposal(s) to review).",
			importable, inferred)
	} else {
		row.Summary = fmt.Sprintf("implementation is ahead: %d semantic delta(s) to import.", importable)
	}
	row.Remedy = DriftCodeAheadNextStep
	row.NextStep = "design_sync"
	return driftCodeAhead{
		Row: row,
		Evidence: fmt.Sprintf("Analysed %d touched code file(s) with the design_sync analysis: %d importable semantic delta(s).",
			len(touched), importable),
	}
}

// generatedProvenanceState inspects design/generated/ and reports whether an
// export exists and whether it is behind the current token sources, using the
// §5a provenance header (SP-140 invariant 2). It is best-effort: a
// missing/unreadable generated/ directory reports hasGenerated=false, and a
// directory whose artifacts carry no provenance header reports stale=false (the
// header convention is the signal; its absence is not itself drift).
func generatedProvenanceState(root string) (hasGenerated, stale bool, evidence string) {
	genDir := filepath.Join(root, DirName, GeneratedSubdir)
	info, err := os.Stat(genDir)
	if err != nil || !info.IsDir() {
		return false, false, ""
	}
	hasGenerated = true

	// The export this analysis trusts is any generated artifact's header; the
	// canonical one is the CSS target (rendered first, always present in a
	// §5a export).
	artifact := filepath.Join(genDir, ExportFilenames[ExportTargets[0]])
	data, readErr := os.ReadFile(artifact)
	if readErr != nil {
		return true, false, ""
	}
	recorded := provenanceSourceHash(string(data))
	if recorded == "" {
		return true, false, "design/generated/ has no parsable provenance header; treating the export as current."
	}

	// Recompute the current token-input hash from design/tokens/*.tokens.json
	// (the same recomputation a consumer at any checkout can do offline, §5f).
	current, hashErr := currentTokenInputHash(root)
	if hashErr != nil {
		return true, false, ""
	}
	return true, recorded != current, ""
}

// currentTokenInputHash recomputes the §5a provenance hash over the current
// design/tokens/*.tokens.json bytes, in source-name order, so it matches
// TokenExportInputHash exactly. A missing tokens directory yields "" and no
// error (there is nothing to be stale against).
func currentTokenInputHash(root string) (string, error) {
	sources, err := tokenExportSources(root)
	if err != nil {
		return "", err
	}
	if len(sources) == 0 {
		return "", nil
	}
	return exportTokenInputHash(sources), nil
}

// tokenExportSources reads design/tokens/*.tokens.json into the §5a hash input
// shape (path, base name, exact bytes), sorted by base name. It is the
// read-side twin of ResolveExportTokens' provenance input, kept separate so
// drift analysis does not have to fully project (and alias-check) the tokens
// just to compare hashes.
func tokenExportSources(root string) ([]TokenExportSource, error) {
	matches, err := filepath.Glob(filepath.Join(root, DirName, TokenSubdir, "*.tokens.json"))
	if err != nil {
		return nil, err
	}
	sources := make([]TokenExportSource, 0, len(matches))
	for _, match := range matches {
		data, readErr := os.ReadFile(match)
		if readErr != nil {
			return nil, readErr
		}
		rel, relErr := filepath.Rel(root, match)
		if relErr != nil {
			rel = match
		}
		sources = append(sources, TokenExportSource{
			Path:    filepath.ToSlash(rel),
			Name:    path.Base(filepath.ToSlash(rel)),
			Content: data,
		})
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Name < sources[j].Name })
	return sources, nil
}

// provenanceSourceHash extracts the `source-hash:` value from a generated
// artifact's §5a provenance banner. It returns "" when the artifact carries no
// header (an older or hand-written file). Two banner shapes are legal — the
// one provenanceHeader renders (a "source-hash: fnv1a64:<hex>" line inside a
// comment) and the JSON target's field form ("source-hash": "<hex>"), which
// exists because JSON has no comment syntax; both carry the same value and
// this extractor is the one place that must agree on them.
func provenanceSourceHash(artifact string) string {
	for _, line := range strings.Split(artifact, "\n") {
		trimmed := strings.TrimSpace(line)
		trimmed = strings.TrimLeft(trimmed, "/*#- \t")
		if !strings.HasPrefix(trimmed, "source-hash:") {
			if !strings.HasPrefix(trimmed, `"source-hash":`) {
				continue
			}
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, `"source-hash":`))
			value = strings.Trim(value, `",`)
			if value != "" {
				return value
			}
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "source-hash:"))
		value = strings.TrimRight(value, "*/ \t")
		return strings.TrimSpace(value)
	}
	return ""
}

// designSurfacesAhead counts the design surfaces that are ahead of the build:
// the wireframes (one per screen) plus the flows. That is the "2 screens ahead
// of build" magnitude §5c names. The count is derived from a sorted directory
// walk, so it is deterministic.
func designSurfacesAhead(root string) int {
	wireframes := len(assetStems(root, "wireframes", ".svg"))
	flows := len(assetStems(root, FlowSubdir, ".mmd"))
	return wireframes + flows
}

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
