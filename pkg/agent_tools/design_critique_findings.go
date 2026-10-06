package tools

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// critiqueFinding is one structured critique finding (§4a):
// {target, area, severity, note, suggestion}.
//
// Area is one of designCritiqueAreas. Severity is advisory — a critique never
// blocks a turn — and uses the same vocabulary as the model's judgment:
// "blocker" (a user cannot complete the task), "major" (clearly wrong but
// usable), "minor" (polish), "info" (an observation, not a defect).
type critiqueFinding struct {
	Target     string `json:"target"`
	Area       string `json:"area"`
	Severity   string `json:"severity"`
	Note       string `json:"note"`
	Suggestion string `json:"suggestion,omitempty"`
	// Line is the 1-based source line for a static finding derived from the
	// design validator (item 4.2); 0/omitted for a vision finding, which has
	// no source line.
	Line int `json:"line,omitempty"`
	// Rule is the design-validator rule id a static finding came from
	// (item 4.2, e.g. "svg_data_nav_dangling"); "" for a vision finding. It
	// lets a consumer trace a degraded finding back to the static rule pack
	// that produced it.
	Rule string `json:"rule,omitempty"`
}

// critiqueSeverities are the accepted finding severities, in descending
// urgency. Kept here so the prompt, the tally, and any future validator can
// agree on one vocabulary.
var critiqueSeverities = []string{"blocker", "major", "minor", "info"}

// critiqueArtifact is a derived PNG this critique produced. Path is
// workspace-relative under design/.cache/renders/; Provenance is the textual
// banner appended to the PNG (SP-140 invariant 2).
type critiqueArtifact struct {
	Target     string `json:"target"`
	Path       string `json:"path"`
	Provenance string `json:"provenance"`
	// Cached reports that this artifact was reused from an earlier run whose
	// cache key matched, so no browser render happened for it (§4e). It is
	// deliberately unexported to JSON so the §4a artifact shape stays
	// {target, path, provenance} for consumers; the run-level CacheHits field
	// carries the same information in the structured output.
	Cached bool `json:"-"`
}

// critiqueOutput is the structured result of one design_critique run.
type critiqueOutput struct {
	// Target is the requested target, verbatim.
	Target string `json:"target"`
	// Rubric is the active rubric (always set; defaults to "all").
	Rubric string `json:"rubric"`
	// CompareTo is the requested comparison target ("" when none).
	CompareTo string `json:"compareTo,omitempty"`
	// Findings are the structured critique findings.
	Findings []critiqueFinding `json:"findings"`
	// Count is len(Findings), surfaced so a consumer need not recount.
	Count int `json:"count"`
	// BySeverity always carries every severity key so a zero is readable
	// without a presence check.
	BySeverity map[string]int `json:"bySeverity"`
	// Artifacts are the derived PNGs written under design/.cache/renders/.
	Artifacts []critiqueArtifact `json:"artifacts"`
	// Areas is the critique vocabulary the run was judged against.
	Areas []string `json:"areas"`
	// Instruction is the exact rubric prompt handed to the vision tier.
	Instruction string `json:"instruction"`
	// RenderCount is how many rasterizations this run performed, so a caller
	// (and item 4.3's cache tests) can count renders rather than infer them.
	// It counts browser renders only: a cache hit (§4e) reuses a cached PNG
	// and does not increment it, so RenderCount is the true cost signal.
	RenderCount int `json:"renderCount"`
	// CacheHits is how many targets reused a cached render instead of
	// re-rasterizing (§4e). It is surfaced so a consumer can see that a run
	// was cheap because the content was unchanged, rather than guessing.
	CacheHits int `json:"cacheHits"`
	// MaxScreens is the whole-tree cap in force for this run (§4e). It is
	// always reported (the default when the caller passed nothing) so the
	// cost ceiling is visible even on a run that did not hit it.
	MaxScreens int `json:"maxScreens"`
	// Capped is true when the whole-tree target exceeded MaxScreens and the
	// run was truncated. SkippedCount is how many targets were left
	// uncovered; both are reported so a truncated critique is never mistaken
	// for a complete one.
	Capped       bool `json:"capped"`
	SkippedCount int  `json:"skippedCount"`
	// Visual reports whether a vision-capable critique ran against real
	// pixels: a vision tier was reachable (a wired VisionProcessor, or an
	// available package-level vision capability — see
	// critiqueVisionTierAvailable) *and* at least one primary pass came back
	// with an analysis. Item 4.1 always renders and attaches; a run with no
	// vision tier reports visual=false and returns the static rubric findings
	// (item 4.2 layers the full degradation on top of this field).
	Visual bool `json:"visual"`
	// Degraded reports that the findings above are static (item 4.2): no
	// vision tier was reachable, so the run fell back to the design
	// validator's rule-pack findings mapped into the critique schema. It is
	// always true when Visual is false and findings were produced, and false
	// for a vision critique. Consumers that only care "were these judged from
	// pixels?" can read Visual; Degraded documents *why* the fallback ran.
	Degraded bool `json:"degraded"`
	// Screen is the screen slug when the target resolved to a screen ("").
	Screen string `json:"screen,omitempty"`
	// Note is the per-run notice ("" when there is nothing to say): the §4e
	// whole-tree cap notice, the item 4.2 degradation notice, and the
	// cache-skip notice can all appear here, joined so none is lost.
	Note string `json:"note,omitempty"`
}

// isCritiqueRubric reports whether r is an accepted rubric (exact, lowercase).
func isCritiqueRubric(r string) bool {
	for _, accepted := range designCritiqueRubrics {
		if r == accepted {
			return true
		}
	}
	return false
}

// rubricAreas returns the critique areas the rubric focuses on. `all` returns
// every area; the focused rubrics return the subset the designer prompt's
// vocabulary maps to them, so a focused pass is a genuine narrowing rather
// than a relabelled full pass.
func rubricAreas(rubric string) []string {
	switch rubric {
	case designRubricConsistency:
		return []string{"consistency", "spacing-rhythm"}
	case designRubricAccessibility:
		return []string{"contrast", "touch-target", "affordance"}
	case designRubricHierarchy:
		return []string{"hierarchy", "affordance"}
	default:
		return designCritiqueAreas
	}
}

// buildCritiquePrompt composes the structured rubric instruction handed to the
// vision tier. It is derived from the designer prompt's shared critique
// vocabulary, states the finding schema the tool expects back, and appends any
// caller-supplied instruction.
func buildCritiquePrompt(rubric, target, compareTo, extra string) string {
	areas := rubricAreas(rubric)

	var sb strings.Builder
	sb.WriteString("Critique this rendered design as a senior product designer. Judge ")
	if compareTo != "" {
		fmt.Fprintf(&sb, "%s against %s as a delta review — report drift between them (same job "+
			"should look the same) before judging either in isolation. ", target, compareTo)
	} else {
		fmt.Fprintf(&sb, "%s. ", target)
	}
	if rubric == designRubricAll {
		sb.WriteString("Work through the whole critique vocabulary. ")
	} else {
		fmt.Fprintf(&sb, "Focus on the %s rubric; report only findings in those areas. ", rubric)
	}

	sb.WriteString("Vocabulary: ")
	sb.WriteString(strings.Join(areas, ", "))
	sb.WriteString(".")

	sb.WriteString("\nFor each finding report: area (one of the vocabulary terms), severity " +
		"(blocker | major | minor | info), the observation with the concrete element it is " +
		"about, and a concrete fix. A specific finding is an action; vague praise is noise. " +
		"Report \"none\" for an area with nothing to say rather than inventing a finding.")

	if extra != "" {
		sb.WriteString("\nAdditional instruction: " + extra)
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// Target discovery
// ---------------------------------------------------------------------------

// trimFloat renders a viewport dimension without a trailing ".0" for integral
// values, so the material string is stable across int/float64 arg forms.
func trimFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// critiqueCapNotice is the §4e explicit notice for a truncated whole-tree
// critique: it names the cap, how many targets were skipped, and the remedy, so
// a capped run is never mistaken for a complete one.
func critiqueCapNotice(maxScreens, skipped int) string {
	return fmt.Sprintf("%s%d screens; run narrowed critiques to cover the rest",
		designCritiqueCapNoticePrefix, maxScreens) +
		fmt.Sprintf(" (%d further target(s) in this tree were not critiqued).", skipped)
}

// critiqueCacheNotice is the per-run note disclosing that renders were reused
// from the §4e cache rather than re-rasterized.
func critiqueCacheNotice(hits int) string {
	if hits == 1 {
		return "1 render was reused from the render cache (content unchanged since it was rendered)."
	}
	return fmt.Sprintf("%d renders were reused from the render cache (content unchanged since they were rendered).", hits)
}

// zeroSeverityTally returns a tally carrying every severity key at zero.
func zeroSeverityTally() map[string]int {
	tally := make(map[string]int, len(critiqueSeverities))
	for _, s := range critiqueSeverities {
		tally[s] = 0
	}
	return tally
}

// tallySeverities counts findings per severity. Unknown severities are counted
// under their own key rather than dropped, so nothing is hidden.
func tallySeverities(findings []critiqueFinding) map[string]int {
	tally := zeroSeverityTally()
	for _, f := range findings {
		sev := strings.ToLower(strings.TrimSpace(f.Severity))
		if sev == "" {
			sev = "info"
		}
		tally[sev]++
	}
	return tally
}

// buildCritiqueSummary composes the human-readable result: what was critiqued
// against which rubric, the derived artifacts, the findings, and — for a
// non-vision primary — the SP-137 fallback guidance. It never reports an error
// for a missing vision tier: the rubric plus the attached artifact is a
// complete result (§2c/§4a "must not fail").
func buildCritiqueSummary(out critiqueOutput, attached bool, analysis string, analysisErr error) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "design_critique: %s (rubric=%s)", out.Target, out.Rubric)
	if out.CompareTo != "" {
		fmt.Fprintf(&sb, " vs %s", out.CompareTo)
	}
	fmt.Fprintf(&sb, " — %d finding(s)", out.Count)
	if out.Degraded {
		sb.WriteString(" (static; visual=false)")
	}

	if len(out.Artifacts) > 0 {
		fmt.Fprintf(&sb, "\n\nRendered %d target(s); derived artifact(s):", out.RenderCount)
		if out.CacheHits > 0 {
			fmt.Fprintf(&sb, " (%d reused from cache)", out.CacheHits)
		}
		for _, a := range out.Artifacts {
			if a.Path == "" {
				continue
			}
			sb.WriteString("\n- " + a.Path + " (derived; provenance header)")
			if a.Cached {
				sb.WriteString(" [cache hit]")
			}
		}
	}
	if out.Capped {
		// §4e: the cap is stated up front in the summary, not buried in a note,
		// so a truncated whole-tree critique cannot be read as complete.
		fmt.Fprintf(&sb, "\n\nWARNING: %s", critiqueCapNotice(out.MaxScreens, out.SkippedCount))
	}
	if attached {
		sb.WriteString("\nThe rendered image is attached for visual critique.")
	}

	if out.Count > 0 {
		if out.Degraded {
			sb.WriteString("\n\nStatic findings (design-validator rule packs; visual=false):")
		} else {
			sb.WriteString("\n\nFindings:")
		}
		for _, f := range out.Findings {
			fmt.Fprintf(&sb, "\n- [%s] %s — %s", f.Severity, f.Area, f.Note)
			if f.Rule != "" {
				sb.WriteString(" (" + f.Rule + ")")
			}
			if f.Suggestion != "" {
				sb.WriteString(" → " + f.Suggestion)
			}
		}
	} else {
		sb.WriteString("\n\nNo structured findings yet.")
	}

	sb.WriteString("\n\nRubric applied:\n" + out.Instruction)

	analysis = strings.TrimSpace(analysis)
	switch {
	case analysis != "":
		sb.WriteString("\n\nCritique (vision tier):\n" + analysis)
	case analysisErr != nil:
		fmt.Fprintf(&sb, "\n\nNo critique text from the vision tier (%v); %s", analysisErr,
			degradedTail(out))
	default:
		sb.WriteString("\n\nNo vision tier was available to critique the render; " + degradedTail(out))
	}
	if out.Note != "" {
		sb.WriteString("\n\nNote: " + out.Note)
	}
	return sb.String()
}

// degradedTail is the closing guidance for a non-vision run. A degraded run
// with static findings points at the rule report; a run with neither vision
// nor static findings still points at the OCR/native recovery route over the
// rendered artifact, because §4a requires the artifact + rubric to be a
// complete result (§2c/§4a "must not fail").
func degradedTail(out critiqueOutput) string {
	if out.Degraded {
		return "the static findings above come from the design rule packs " +
			"(visual:false), so the rendered artifact and the rubric are complete " +
			"either way — run design_validate for the full rule report, read the " +
			"image directly if you can, or run analyze_image_content on the artifact path."
	}
	return "the rendered artifact and the rubric above are complete either way — " +
		"read the image directly if you can, or run analyze_image_content " +
		"(OCR/native fallback) on the artifact path."
}

// ---------------------------------------------------------------------------
// Static degradation (SP-140-4 §4a)
// ---------------------------------------------------------------------------

// appendNote joins a new notice onto an existing per-run note without losing
// either (item 4.3 adds a cap notice through the same field).
func appendNote(existing, add string) string {
	existing = strings.TrimSpace(existing)
	add = strings.TrimSpace(add)
	switch {
	case existing == "":
		return add
	case add == "":
		return existing
	default:
		return existing + " " + add
	}
}

// firstNonEmpty returns the first non-empty string, or "".
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// UnmarshalCritiqueFindings parses the vision tier's JSON response into the
// structured finding shape. The vision tier is asked for a JSON array (or an
// object carrying a `findings` array); anything else yields no findings rather
// than an error, because the analysis text remains valuable on its own.
//
// Item 4.1 exposes this for the vision-scripted test path; the wiring of the
// call's JSON request lives with the vision prompt, not here.
func UnmarshalCritiqueFindings(raw string) []critiqueFinding {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	// Tolerate a fenced code block.
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```")
		raw = strings.TrimPrefix(strings.TrimPrefix(raw, "json"), "\n")
		if idx := strings.LastIndex(raw, "```"); idx >= 0 {
			raw = raw[:idx]
		}
		raw = strings.TrimSpace(raw)
	}

	var direct []critiqueFinding
	if err := json.Unmarshal([]byte(raw), &direct); err == nil && direct != nil {
		return normalizeFindings(direct)
	}
	var wrapper struct {
		Findings []critiqueFinding `json:"findings"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err == nil && wrapper.Findings != nil {
		return normalizeFindings(wrapper.Findings)
	}
	return nil
}

// normalizeFindings trims the parsed rows and drops the ones with no
// observation (an empty note is not a finding).
func normalizeFindings(in []critiqueFinding) []critiqueFinding {
	out := make([]critiqueFinding, 0, len(in))
	for _, f := range in {
		f.Target = strings.TrimSpace(f.Target)
		f.Area = strings.ToLower(strings.TrimSpace(f.Area))
		f.Severity = strings.ToLower(strings.TrimSpace(f.Severity))
		f.Note = strings.TrimSpace(f.Note)
		f.Suggestion = strings.TrimSpace(f.Suggestion)
		f.Rule = strings.TrimSpace(f.Rule)
		if f.Note == "" {
			continue
		}
		if f.Severity == "" {
			f.Severity = "info"
		}
		out = append(out, f)
	}
	return out
}
