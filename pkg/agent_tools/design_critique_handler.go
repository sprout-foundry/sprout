//go:build !js

package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	stdsort "sort"
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/design"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// designCritiqueHandler implements ToolHandler for the design_critique tool
// (SP-140-4 §4a): it closes the visual loop by rendering a design target
// through the SP-140-2 render machinery, attaching the PNG via the SP-137
// tool-result image path, and asking the vision tier to critique it against
// the designer prompt's critique vocabulary (hierarchy, affordance,
// consistency, spacing rhythm, contrast, touch targets).
//
// The tool is the composition of three pieces that already exist, not a new
// pipeline:
//
//   - target discovery + classification → design.Target (pkg/design),
//   - rasterization → renderInputToString / buildBrowserRenderAttachment
//     (the same shared helper design_render uses — SP-140-2 §2c),
//   - image attach + analysis → AnalyzeImage (the SP-137 tool-result path).
//
// It depends on the host browser tier (rasterization) and the vision tier
// (critique), so it is a //go:build !js file with a WASM stub in
// design_critique_handler_js.go (mirroring design_render_handler.go). The
// registration is build-tagged and lives in neither the shared AllTools list
// nor the WASM roster (SP-140 invariant 7).
//
// Scope note (SP-140-4 items 4.1–4.3): this is the tool core — render, attach,
// structured findings, the derived-artifact cache path with its provenance
// header (4.1), the non-vision degradation (4.2), and the content-hash render
// cache + whole-tree cost cap (4.3). The consistency rule packs (items
// 4.4/4.5) and feedback consumption (item 4.7) are separate TODO items.
type designCritiqueHandler struct{}

func (h *designCritiqueHandler) Name() string { return "design_critique" }

// Critique rubrics: the designer prompt's vocabulary, grouped so the model can
// request a focused pass instead of always paying for the full set. RubricAll
// is the default (§4a).
const (
	designRubricConsistency   = "consistency"
	designRubricAccessibility = "accessibility"
	designRubricHierarchy     = "hierarchy"
	designRubricAll           = "all"
)

// designCritiqueRubrics is the accepted rubric set, in contract order.
var designCritiqueRubrics = []string{
	designRubricAll,
	designRubricConsistency,
	designRubricAccessibility,
	designRubricHierarchy,
}

// designCritiqueAreas lists the critique areas the rubric prompt is built
// from. They are the designer prompt's shared vocabulary — the same words the
// model is told to report findings in — so an area string in a finding is
// always one of these.
var designCritiqueAreas = []string{
	"hierarchy",
	"affordance",
	"consistency",
	"spacing-rhythm",
	"contrast",
	"touch-target",
}

// designArtifactDirName is the derived-artifact cache root, relative to the
// design/ tree (SP-140 invariant 2 + SP-140-1 §1h: .gitignore carries
// `design/.cache/`, so everything here is regenerable and never a source).
const designArtifactDirName = ".cache"

// designRenderArtifactDirName is the subdirectory rendered PNGs land in
// (§4a: "cache under design/.cache/renders/").
const designRenderArtifactDirName = "renders"

// designCritiqueStemSeparator joins a target stem to the comparison stem in a
// delta-review artifact name (login~home.png). `~` cannot appear in a design
// slug (design.SlugPattern), so the composed stem stays unambiguous.
const designCritiqueStemSeparator = "~"

// provenanceHeaderPrefix opens the textual provenance banner written into
// every derived PNG. SP-140 invariant 2 requires derived artifacts to be
// recognizable as derived "so tooling can regenerate them"; a PNG carries no
// metadata field for this tool to populate (no PNG encoder is available in
// this dependency-free path), so the artifact is documented both in-band —
// these bytes, appended after IEND, are ignored by every PNG decoder — and
// out-of-band, in the tool result's provenance fields. See
// buildCritiqueProvenance.
const provenanceHeaderPrefix = "sprout:derived-artifact"

// provenanceHeaderTerminator closes the provenance banner so a reader can
// extract it without guessing a length.
const provenanceHeaderTerminator = "sprout:end-provenance"

// designCritiqueDefaultMaxScreens is the §4e whole-tree cost cap: one
// design_critique run over the whole design/ tree may rasterize (and, with a
// vision tier, critique) at most this many screens, so a single turn cannot
// fire one vision call per file in a large tree. It is a *tool argument*
// (max_screens) with this default, never a CLI flag — the cap has to travel
// with the call that spends the budget.
//
// The spec's "cap at 20 screens per run" is this number; the cap applies to
// the whole-tree target only, because a critique the caller narrowed to one
// screen is already the narrow critique the notice tells them to run.
const designCritiqueDefaultMaxScreens = 20

// designCritiqueCapNoticePrefix opens the explicit notice appended to a capped
// run's Note (and therefore its human-readable summary). It names the cap, the
// number of targets left uncovered, and the remedy, so a truncated critique is
// never silently mistaken for a complete one (§4e "with explicit notice").
const designCritiqueCapNoticePrefix = "critique capped at "

// Definition describes the tool. The §4e whole-tree cap is a *tool argument*
// (max_screens) rather than a CLI flag, because the cap has to travel with the
// call that spends the vision budget; the default is
// designCritiqueDefaultMaxScreens.
func (h *designCritiqueHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "design_critique",
		Description: "Critique a rendered design target with the vision tier: render a screen " +
			"(`design/wireframes/login.svg`, `design/screens/login.html`), a flow " +
			"(`design/flows/sign-up.mmd`), or the whole design/ tree, attach the rendered PNG, and " +
			"return structured findings {target, area, severity, note, suggestion} judged against " +
			"the design critique vocabulary (hierarchy, affordance, consistency, spacing rhythm, " +
			"contrast, touch targets). Use it after writing or revising a design, after " +
			"design_validate is clean of errors, to see what the render actually looks like. " +
			"Pass rubric to focus the pass (consistency, accessibility, hierarchy, or all — the " +
			"default) and compare_to to review a second screen for delta/drift instead of " +
			"absolutes. The render is cached as a derived artifact under design/.cache/renders/ " +
			"keyed by the source content hash, so re-critiquing an unchanged screen skips the " +
			"re-render and reuses the cached PNG (the PNG is never a source of truth). A " +
			"whole-tree critique is capped at 20 screens per run (raise or lower it with " +
			"max_screens); when the tree exceeds the cap the result says so explicitly and " +
			"tells you to run narrowed critiques for the rest, so one turn cannot fire a vision " +
			"call per file. A critique never " +
			"blocks the turn: the findings are advisory, and a non-vision primary still receives " +
			"the rendered artifact and the rubric. When no vision tier is reachable the critique " +
			"degrades to design_validate-style static findings marked `visual: false` (hierarchy, " +
			"consistency, contrast, etc. from the design rule packs) rather than returning nothing.",
		Parameters: []ParameterDef{
			{
				Name:        "target",
				Type:        "string",
				Required:    true,
				Description: "What to critique: a workspace-relative source (`design/wireframes/login.svg`, `design/screens/login.html`, `design/flows/sign-up.mmd`), a bare screen/flow slug (`login`), a screen name (`design/screens/login.html`), or `design` / the whole tree to critique every renderable target in the design/ tree.",
			},
			{
				Name:        "rubric",
				Type:        "string",
				Required:    false,
				Description: "Optional critique focus: `consistency`, `accessibility`, `hierarchy`, or `all` (default). Unknown values are rejected so a typo cannot silently narrow the pass.",
			},
			{
				Name:        "compare_to",
				Type:        "string",
				Required:    false,
				Description: "Optional second target (same forms as `target`) to review as a delta against `target` — same spacing, labels, and component shapes across screens — instead of judging it in isolation.",
			},
			{
				Name:        "analysis_prompt",
				Type:        "string",
				Required:    false,
				Description: "Optional extra instruction appended to the rubric prompt (e.g. `focus on the empty state`).",
			},
			{
				Name:        "viewport_width",
				Type:        "integer",
				Required:    false,
				Description: "Browser width in px for each render (default 1280).",
			},
			{
				Name:        "viewport_height",
				Type:        "integer",
				Required:    false,
				Description: "Browser height in px for each render (default 720).",
			},
			{
				Name:        "max_screens",
				Type:        "integer",
				Required:    false,
				Description: "Cap on screens rendered/critiqued in one run, applied to a whole-tree (design/) target (default 20). The cap is enforced with an explicit notice naming how many targets were skipped; run narrowed critiques (a subtree, a slug, or a file) to cover the rest. Ignored for a single named target, which is already narrow.",
			},
		},
		Required: []string{"target"},
	}
}

func (h *designCritiqueHandler) Validate(args map[string]any) error {
	if _, err := extractString(args, "target"); err != nil {
		return err
	}
	for _, key := range []string{"rubric", "compare_to", "analysis_prompt"} {
		if v, exists := lookupKey(args, key); exists && v != nil {
			if _, ok := v.(string); !ok {
				return fmt.Errorf("parameter '%s' must be a string, got %T", key, v)
			}
		}
	}
	// max_screens is optional; when present it must be an integer in range, so
	// a "20" string or a negative number cannot silently become the default.
	if _, err := critiqueMaxScreensArg(args); err != nil {
		return err
	}
	return nil
}

// critiqueTarget is one renderable unit of the critique: where it came from
// (Source, workspace-relative, "" for a synthesized target), what it is
// (Kind/Screen/Stage), and which file the browser rasterizes (RenderSource).
type critiqueTarget struct {
	// Label is the workspace-relative path the finding's `target` field
	// carries. It is Source when the caller named a file, and the source the
	// target was derived from when they named a slug or the tree.
	Label string
	// Source is the workspace-relative path the target was derived from ("").
	Source string
	// RenderSource is the local file handed to the browser.
	RenderSource string
	// Kind is the classifyDesignSource verdict (browser / mermaid).
	Kind designRenderKind
	// Screen is the screen slug when the target is a screen ("" otherwise).
	Screen string
	// Stage names which critique step this target belongs to:
	// "target", "compare", or "tree".
	Stage string
	// CompareLabel is the baseline target label a comparison target is
	// reviewed against ("" unless Stage == "compare"). It sets the artifact
	// name so a target and its comparison never collide.
	CompareLabel string
}

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

func (h *designCritiqueHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	target, err := extractString(args, "target")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return h.critiqueError("target must not be empty")
	}

	rubric := strings.ToLower(strings.TrimSpace(stringArg(args, "rubric")))
	if rubric == "" {
		rubric = designRubricAll
	}
	if !isCritiqueRubric(rubric) {
		return h.critiqueError(fmt.Sprintf("unsupported rubric %q — expected one of: %s",
			rubric, strings.Join(designCritiqueRubrics, ", ")))
	}

	compareTo := strings.TrimSpace(stringArg(args, "compare_to"))
	analysisPrompt := strings.TrimSpace(stringArg(args, "analysis_prompt"))

	// §4e cost cap: the whole-tree ceiling, as a tool argument with a default.
	maxScreens, err := critiqueMaxScreensArg(args)
	if err != nil {
		return h.critiqueError(err.Error())
	}

	// Gate-1 precheck (SP-140 invariant 7): every workspace path the tool
	// touches is checked before it is read, mirroring design_render. The
	// requested target is checked here — before discovery stats it — and tree
	// expansion prechecks each discovered child (see precheckCritiqueTarget),
	// so no file is ever opened without a verdict.
	if err := precheckCritiquePath(ctx, env, target); err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}
	if compareTo != "" {
		if err := precheckCritiquePath(ctx, env, compareTo); err != nil {
			return ToolResult{Output: err.Error(), IsError: true}, err
		}
	}

	targets, err := discoverCritiqueTargets(ctx, env, target)
	if err != nil {
		msg := err.Error()
		return ToolResult{Output: msg, IsError: true}, err
	}

	// §4e whole-tree cap: a target that resolved to the whole design/ tree is
	// truncated to maxScreens so one run cannot fire one vision call per file
	// (the spec's "cap at 20 screens per run"). A single named target, a slug,
	// and a subtree are already narrow — the caller did the narrowing — so
	// they are never capped. The truncated targets are reported explicitly so
	// a capped critique is never mistaken for a complete one.
	capped := false
	skippedCount := 0
	if targetIsWholeTree(cleanCritiqueTarget(target)) && len(targets) > maxScreens {
		capped = true
		skippedCount = len(targets) - maxScreens
		targets = targets[:maxScreens]
	}

	if compareTo != "" {
		cmpTargets, cmpErr := discoverCritiqueTargets(ctx, env, compareTo)
		if cmpErr != nil {
			msg := cmpErr.Error()
			return ToolResult{Output: msg, IsError: true}, cmpErr
		}
		// The comparison targets carry the target label that the delta-review
		// prompt names them by, so the vision tier and the artifact name agree
		// on which render is the baseline. Only the first comparison target is
		// used: compare_to is "a second screen", not a set.
		if len(cmpTargets) > 0 {
			cmp := cmpTargets[0]
			cmp.Stage = "compare"
			cmp.CompareLabel = targets[0].Label
			targets = append(targets, cmp)
		}
	}

	viewOpts := RenderInputOptions{
		Mode:                     RenderModeBrowser,
		ViewportWidth:            viewportDim(args, "viewport_width", defaultViewportWidth),
		ViewportHeight:           viewportDim(args, "viewport_height", defaultViewportHeight),
		ReportBrowserUnavailable: false,
	}

	out := critiqueOutput{
		Target:       target,
		Rubric:       rubric,
		CompareTo:    compareTo,
		Findings:     []critiqueFinding{},
		BySeverity:   zeroSeverityTally(),
		Artifacts:    []critiqueArtifact{},
		Areas:        designCritiqueAreas,
		Visual:       false,
		MaxScreens:   maxScreens,
		Capped:       capped,
		SkippedCount: skippedCount,
		Instruction:  buildCritiquePrompt(rubric, target, compareTo, analysisPrompt),
	}
	if capped {
		out.Note = appendNote(out.Note, critiqueCapNotice(maxScreens, skippedCount))
	}

	var renderedAny bool
	var critiqued bool
	var attachment ToolResult
	var visionAnalysis string
	var visionErr error
	for _, t := range targets {
		// Gate-1 precheck per discovered child. A deny fails the tool (the
		// same contract as the requested-target precheck); a "prompt" verdict
		// with no prompter falls through to the raw filesystem error.
		if err := precheckCritiqueTarget(ctx, env, t); err != nil {
			return ToolResult{Output: err.Error(), IsError: true}, err
		}

		rendered, att, artifact, analysis, analyzeErr, renderErr := renderCritiqueTarget(ctx, env, t, viewOpts, out.Instruction, t.Stage != "compare")
		if renderErr != nil {
			// A render failure is a real failure (browser tier missing, or
			// the browser refused the source). Report it; do not pretend the
			// target was critiqued.
			msg := fmt.Sprintf("design_critique: cannot render %s: %v", t.RenderSource, renderErr)
			return ToolResult{Output: msg, IsError: true}, agenterrors.NewTool("design_critique", msg, renderErr)
		}
		if !rendered {
			continue
		}
		renderedAny = true
		// A cache hit reused a stored artifact and did not rasterize, so it
		// must not be counted as a render (§4e: repeat critiques of unchanged
		// content skip the re-render). The artifact still rides the result.
		if artifact.Cached {
			out.CacheHits++
		} else {
			out.RenderCount++
		}
		out.Artifacts = append(out.Artifacts, artifact)
		if out.Screen == "" && t.Screen != "" {
			out.Screen = t.Screen
		}

		// The very first attachment rides the tool result (SP-137).
		if len(attachment.Images) == 0 {
			attachment = att
		}
		if t.Stage == "compare" {
			// The comparison render is a delta input, not a second critique:
			// it is attached to the primary target's pass by the delta-review
			// prompt instead of being critiqued on its own.
			continue
		}
		if analyzeErr != nil {
			visionErr = analyzeErr
			continue
		}
		// A pass that produced no analysis text was not a critique, whatever
		// the tier reported: nothing was judged, so Visual stays false. This
		// is the same signal the summary uses to print its "no critique text"
		// paragraph, and it keeps the field tied to evidence rather than to
		// the absence of a Go error.
		analysis = strings.TrimSpace(analysis)
		if analysis == "" {
			continue
		}
		visionAnalysis = analysis
		critiqued = true
		// Findings carry the target they belong to: the vision tier reports
		// the observation, the tool stamps the label so a whole-tree critique
		// stays attributable to a file.
		for _, f := range UnmarshalCritiqueFindings(analysis) {
			if f.Target == "" {
				f.Target = t.Label
			}
			out.Findings = append(out.Findings, f)
		}
	}

	if !renderedAny {
		msg := fmt.Sprintf("design_critique: %s has nothing renderable — expected a .svg, .html/.htm, or .mmd target (or a slug/tree naming one)", target)
		return ToolResult{Output: msg, IsError: true}, agenterrors.NewTool("design_critique", msg, nil)
	}

	// §4e: disclose the cache reuse so a cheap run is legible as "nothing
	// changed" rather than as a run whose renders mysteriously did not happen.
	if out.CacheHits > 0 {
		out.Note = appendNote(out.Note, critiqueCacheNotice(out.CacheHits))
	}

	// Visual is decided once, after every pass: true only when a vision tier
	// was actually available and one of the primary passes came back with an
	// analysis. A run that never reached the tier (no wired processor and no
	// package-level vision capability — the hermetic unit-test environment, or
	// a non-vision primary) reports visual=false no matter what AnalyzeImage
	// returned, so the field is deterministic on every platform. This is the
	// §4a "explicit visual: false marker" that item 4.2 layers its full
	// degradation (static findings, never-fail) on top of.
	out.Visual = critiqued && critiqueVisionTierAvailable(env)

	// Item 4.2: a non-vision primary degrades to static findings rather than
	// returning an empty critique. When no vision critique ran (Visual is
	// false — no tier reachable, the tier errored, or it produced no
	// analysis), fall back to the design validator's rule-pack findings for
	// the critiqued targets, mapped into the critique schema and marked
	// `visual:false`/`degraded:true`. This never fails the turn: the render
	// path already succeeded, and the static pass is advisory (its only
	// failure mode — an unreadable file — is swallowed into a notice).
	//
	// SP-137 tier order: the static pass runs *only* when no vision critique
	// happened, so a reachable vision tier is always preferred.
	if !out.Visual {
		out.Findings = staticCritiqueFindings(env, targets, rubric)
		out.Degraded = len(out.Findings) > 0
		if out.Degraded {
			// Surfaced through Note so the human-readable summary carries the
			// degradation, and appended so a per-run notice (item 4.3's cap)
			// is preserved.
			out.Note = appendNote(out.Note, staticCritiqueNotice())
		} else if out.Note == "" {
			// A degraded run with a clean target still says that the fallback
			// ran and found nothing, so "no findings" is not mistaken for "no
			// critique was attempted".
			out.Note = staticCritiqueCleanNotice()
		}
	}

	out.Count = len(out.Findings)
	out.BySeverity = tallySeverities(out.Findings)
	attachment.Output = buildCritiqueSummary(out, len(attachment.Images) > 0, visionAnalysis, visionErr)
	attachment.StructuredOut = out
	return attachment, nil
}

// critiqueVisionTierAvailable reports whether a vision pass can actually reach
// a tier for this environment: either the caller wired a VisionProcessor (the
// agent path always does — see pkg/agent's ToolEnv construction), or the
// package-level SP-137 vision capability resolves (registry-driven vision
// client, or the platform's native OCR shim).
//
// It is the availability half of the critiqueOutput.Visual decision, and it is
// deterministic and hermetic: with no wired processor it only consults the
// capability probe — never the network — so a unit-test env with no provider
// config, no custom vision providers and no native OCR reports false.
//
// Package-level state (provider configs, native-OCR shim discovery) is read at
// call time rather than cached, so a process that gains or loses a tier
// between runs is reported correctly.
func critiqueVisionTierAvailable(env ToolEnv) bool {
	// A wired processor is honoured as a truthy signal — the same shape the
	// other consumers of this seam in pkg/agent use (`workflow != nil`), and
	// `critiqued` still gates on real analysis text, so a processor that
	// cannot talk to a model never yields visual=true.
	if env.VisionProcessor != nil {
		return true
	}
	// With no wired processor the pass goes through the package-level
	// AnalyzeImage, which reports a missing capability as a structured
	// response rather than an error — so probe the capability directly
	// instead of inferring it from the response. The probe never touches the
	// network: native OCR presence, then provider-config vision models.
	return HasVisionCapability()
}

// runCritiqueVisionPass sends the rendered pixels at pngPath and the rubric
// instruction to the vision tier through the SP-137 entry point and returns the
// analysis text. A missing or failing vision tier is not an error: the caller
// degrades to the static rubric result (item 4.2 layers the full degradation on
// top of this).
//
// When the environment carries a wired VisionProcessor (the agent path always
// does — see pkg/agent's ToolEnv construction), the pass goes through it
// directly: it is the same tier AnalyzeImage resolves, but it keeps the call
// hermetic and injectable.
//
// Otherwise the package-level AnalyzeImage is used, which resolves the
// registry-driven vision client (SP-137). That path is guarded by the
// capability probe first: AnalyzeImage would otherwise be free to *construct* a
// vision client and issue a live request, so a run with no vision tier would
// depend on the host's provider configuration and network rather than degrading
// immediately. The probe is local (native-OCR shim, provider configs) and
// touches no network.
//
// A pass that yields no analysis text is reported as an error, not as an empty
// success, so the caller's `visual` decision is always backed by evidence.
func runCritiqueVisionPass(ctx context.Context, env ToolEnv, instruction, pngPath string) (string, error) {
	if strings.TrimSpace(pngPath) == "" {
		return "", errors.New("no rendered image to critique")
	}

	if env.VisionProcessor != nil {
		analysis, err := env.VisionProcessor.AnalyzeImage(ctx, pngPath, instruction)
		if err != nil {
			return "", err
		}
		text := strings.TrimSpace(analysis.Description)
		if text == "" {
			return "", errors.New("vision tier returned no critique")
		}
		return text, nil
	}

	if !HasVisionCapability() {
		return "", errors.New("no vision capability available: no wired vision processor and no configured vision provider")
	}

	out, err := AnalyzeImage(ctx, pngPath, instruction, visionModeFrontend)
	if err != nil {
		return "", err
	}
	// AnalyzeImage reports a missing capability as a structured JSON response
	// with an error code rather than a Go error, so unwrap that response. A
	// response that is not a successful analysis is the same "not a critique"
	// signal as an empty one.
	var resp ImageAnalysisResponse
	if jerr := json.Unmarshal([]byte(out), &resp); jerr == nil {
		if !resp.Success {
			msg := resp.ErrorMessage
			if msg == "" {
				msg = resp.ErrorCode
			}
			if msg == "" {
				msg = "vision tier returned no critique"
			}
			return "", errors.New(msg)
		}
		if text := strings.TrimSpace(resp.ExtractedText); text != "" {
			return text, nil
		}
	}
	text := strings.TrimSpace(out)
	if text == "" {
		return "", errors.New("vision tier returned no critique")
	}
	return text, nil
}

// critiqueError is the shared failure shape: the same text in Output and as
// the returned typed error, which is the handler contract.
func (h *designCritiqueHandler) critiqueError(msg string) (ToolResult, error) {
	return ToolResult{Output: msg, IsError: true}, agenterrors.NewTool("design_critique", msg, nil)
}

// ---------------------------------------------------------------------------
// Rubric + prompt
// ---------------------------------------------------------------------------

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

// discoverCritiqueTargets resolves the target argument to the concrete set of
// renderable units. Accepted forms:
//
//   - a workspace-relative source path (design/wireframes/login.svg, …),
//   - a bare slug (login → the wireframe, and its screen when one exists),
//   - a design/ subtree (design/screens → every screen in it),
//   - the design root or "design/" ("whole tree" per §4a).
//
// A target that resolves to nothing is a usage error (tool failure), never an
// empty success — a critique that silently critiqued nothing is worse than no
// critique.
//
// Every directory this walk reads is prechecked first (SP-140 invariant 7),
// because a tree/subtree target enumerates paths the caller never named.
func discoverCritiqueTargets(ctx context.Context, env ToolEnv, requested string) ([]critiqueTarget, error) {
	root := strings.TrimSpace(env.WorkspaceRoot)
	if root == "" {
		root = "."
	}

	clean := strings.TrimSuffix(filepath.ToSlash(path.Clean(strings.TrimSpace(requested))), "/")

	// Bare design root / whole tree.
	if clean == design.DirName || clean == "." || clean == "" {
		if err := precheckCritiquePath(ctx, env, design.DirName); err != nil {
			return nil, err
		}
		return discoverTreeTargets(root)
	}

	// A design/ subtree that is a canonical subdirectory.
	if sub, ok := designSubdirOf(clean); ok {
		if err := precheckCritiquePath(ctx, env, clean); err != nil {
			return nil, err
		}
		return discoverSubtreeTargets(root, sub)
	}

	// An explicit renderable source path. An absolute path is used as-is: it
	// may be inside the workspace (the common case) or outside it, in which
	// case Gate 1 decides — discovery must not silently rewrite it.
	if isRenderableCritiqueSource(clean) {
		renderSource := filepath.FromSlash(clean)
		if !filepath.IsAbs(renderSource) {
			renderSource = filepath.Join(root, renderSource)
		}
		if _, statErr := os.Stat(renderSource); statErr == nil {
			return []critiqueTarget{{
				Label:        clean,
				Source:       clean,
				RenderSource: renderSource,
				Kind:         classifyDesignSource(clean),
				Screen:       screenSlugFor(clean),
				Stage:        "target",
			}}, nil
		}
		// The path names an extension we render but the file is absent: fall
		// through to slug resolution so `design/wireframes/login.svg` and
		// `login` behave the same way when only one of them exists.
	}

	// A bare slug.
	if isSlug(clean) {
		if t, ok := targetForSlug(root, clean); ok {
			return []critiqueTarget{t}, nil
		}
	}

	return nil, critiqueUsageError(requested)
}

// designSubdirOf reports whether p is exactly a canonical design/ subdirectory
// tracked by the design contract (design/wireframes, design/screens, …).
func designSubdirOf(p string) (string, bool) {
	rest, ok := strings.CutPrefix(p, design.DirName+"/")
	if !ok {
		return "", false
	}
	for _, sub := range design.Subdirs {
		if rest == sub {
			return sub, true
		}
	}
	return "", false
}

// isRenderableCritiqueSource reports whether p carries an extension the render
// helper can rasterize (SVG/HTML directly, .mmd through the mermaid page).
func isRenderableCritiqueSource(p string) bool {
	return classifyDesignSource(p) != renderKindUnknown
}

// screenSlugFor returns the screen slug a design/ source path belongs to, or
// "" when the path is not a screen asset. It prefers the screens/ subdirectory
// (a hi-fi screen), then the wireframes/ one (a wireframe for that screen).
func screenSlugFor(p string) string {
	base := GetBaseName(p)
	if i := strings.LastIndex(base, "."); i > 0 {
		base = base[:i]
	}
	stem := strings.ToLower(base)
	if !isSlug(stem) {
		return ""
	}
	slash := filepath.ToSlash(p)
	switch {
	case strings.HasPrefix(slash, design.DirName+"/screens/"):
		return stem
	case strings.HasPrefix(slash, design.DirName+"/wireframes/"):
		return stem
	default:
		return ""
	}
}

// targetForSlug resolves a bare slug to a critique target, preferring the
// hi-fi screen (design/screens/<slug>.html) over the wireframe
// (design/wireframes/<slug>.svg). Only an existing file is returned.
func targetForSlug(root, slug string) (critiqueTarget, bool) {
	candidates := []struct {
		rel  string
		kind designRenderKind
	}{
		{path.Join(design.DirName, "screens", slug+".html"), renderKindBrowser},
		{path.Join(design.DirName, "screens", slug+".htm"), renderKindBrowser},
		{path.Join(design.DirName, "wireframes", slug+".svg"), renderKindBrowser},
		{path.Join(design.DirName, "flows", slug+".mmd"), renderKindMermaid},
	}
	for _, c := range candidates {
		if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(c.rel))); statErr == nil {
			return critiqueTarget{
				Label:        c.rel,
				RenderSource: filepath.Join(root, filepath.FromSlash(c.rel)),
				Kind:         c.kind,
				Screen:       screenSlugFor(c.rel),
				Stage:        "target",
			}, true
		}
	}
	return critiqueTarget{}, false
}

// discoverSubtreeTargets enumerates every renderable target under a canonical
// design/ subdirectory, in a deterministic order.
//
// Only the wireframes, screens, and flows subdirectories hold renderable
// sources; tokens/brand/icons/feedback either are not renderable as whole
// screens or are documented as other assets, so they return a usage error
// rather than an empty critique.
func discoverSubtreeTargets(root, sub string) ([]critiqueTarget, error) {
	dir := filepath.Join(root, design.DirName, sub)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, critiqueUsageError(path.Join(design.DirName, sub))
	}

	var targets []critiqueTarget
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		rel := path.Join(design.DirName, sub, e.Name())
		if !isRenderableCritiqueSource(rel) {
			continue
		}
		targets = append(targets, critiqueTarget{
			Label:        rel,
			Source:       rel,
			RenderSource: filepath.Join(root, filepath.FromSlash(rel)),
			Kind:         classifyDesignSource(rel),
			Screen:       screenSlugFor(rel),
			Stage:        "tree",
		})
	}
	if len(targets) == 0 {
		return nil, critiqueUsageError(path.Join(design.DirName, sub))
	}
	return targets, nil
}

// discoverTreeTargets enumerates every renderable target in the design/ tree
// across the renderable subdirectories, in contract order and — within a
// directory — lexicographically, so a whole-tree critique is reproducible.
//
// §4a's "whole tree" is the union of the screen-bearing subtrees. A tree with
// no design/ directory at all, or with nothing renderable, is a usage error
// carrying scaffold guidance (the same posture as design_assets' {exists:
// false}).
func discoverTreeTargets(root string) ([]critiqueTarget, error) {
	if !design.FileExists(root) {
		return nil, critiqueUsageError(design.DirName)
	}

	var targets []critiqueTarget
	// Wireframes first, then screens, then flows: the order a design is built
	// in, so a truncated run (item 4.3's cap) reports structure before polish.
	for _, sub := range []string{"wireframes", "screens", "flows"} {
		dir := filepath.Join(root, design.DirName, sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // absent subdirectory is normal
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if !e.IsDir() {
				names = append(names, e.Name())
			}
		}
		sortStrings(names)
		for _, name := range names {
			rel := path.Join(design.DirName, sub, name)
			if !isRenderableCritiqueSource(rel) {
				continue
			}
			targets = append(targets, critiqueTarget{
				Label:        rel,
				Source:       rel,
				RenderSource: filepath.Join(root, filepath.FromSlash(rel)),
				Kind:         classifyDesignSource(rel),
				Screen:       screenSlugFor(rel),
				Stage:        "tree",
			})
		}
	}
	if len(targets) == 0 {
		return nil, critiqueUsageError(design.DirName)
	}
	return targets, nil
}

// critiqueUsageError builds the shared "nothing to critique here" failure.
func critiqueUsageError(requested string) error {
	msg := fmt.Sprintf("design_critique: target %q resolved to no renderable design target — "+
		"pass a wireframe/screen/flow path, a screen slug, or design/ for the whole tree "+
		"(renderable sources are %s)",
		requested, strings.Join(renderableCritiqueExtensions(), ", "))
	return agenterrors.NewTool("design_critique", msg, nil)
}

// renderableCritiqueExtensions lists the renderable source extensions for the
// usage-error message, sorted for stability.
func renderableCritiqueExtensions() []string {
	return []string{".html/.htm", ".mmd", ".svg"}
}

// sortStrings is sort.Strings kept local so the deterministic ordering rule is
// explicit at each call site (the package sorts in several places).
func sortStrings(s []string) { stdsort.Strings(s) }

// ---------------------------------------------------------------------------
// Gate-1 precheck
// ---------------------------------------------------------------------------

// precheckCritiquePath runs Gate 1 for a caller-supplied path — the requested
// target (or compare_to) before discovery touches it. It shares the deny/prompt
// contract with precheckCritiqueTarget.
func precheckCritiquePath(ctx context.Context, env ToolEnv, p string) error {
	if strings.TrimSpace(p) == "" {
		return nil
	}
	resolvedPath, decision := PrecheckFileAccess(ctx, env.FileAccessClassifier, "design_critique", p)
	if decision == "deny" {
		return fmt.Errorf("design_critique blocked: %s is not accessible from this session", p)
	}
	if decision == "prompt" && env.FileAccessPrompter != nil {
		if _, approved := promptForOffWorkspacePath(ctx, env, "design_critique", p, resolvedPath, "read"); !approved {
			return fmt.Errorf("design_critique blocked: off-workspace access to %s was not approved", p)
		}
	}
	return nil
}

// precheckCritiqueTarget runs Gate 1 for one discovered target. The requested
// target is already prechecked by the caller; this covers the children tree
// expansion discovers, so no file is read before it is classified.
func precheckCritiqueTarget(ctx context.Context, env ToolEnv, t critiqueTarget) error {
	// Path relative to the workspace root, which is the form the classifier
	// and the VFS resolver agree on.
	rel := t.Source
	if rel == "" {
		rel = t.Label
	}
	if rel == "" {
		return nil
	}
	return precheckCritiquePath(ctx, env, rel)
}

// ---------------------------------------------------------------------------
// Render + artifact
// ---------------------------------------------------------------------------

// renderCritiqueTarget rasterizes one target through the shared render helper,
// and — inside the one window where the browser's temp PNG provably exists —
// captures the SP-137 attachment, the raw bytes for the provenance-headed
// artifact, and (when critique is true) the vision tier's analysis of those
// pixels. It returns (rendered, attachment, artifact, analysis, analyzeErr,
// renderErr).
//
// §4e render cache: before rasterizing, it computes the target's cache key
// (source content hash + render material) and, when an artifact from an
// earlier run matches, reuses it — the browser is not asked to render again,
// and the cached provenance is preserved (writeCritiqueArtifact replaces it
// with a fresh one, which carries the same sourceHash and render material).
//
// analyzeErr is a degraded-result signal, not a tool failure: a missing or
// failing vision tier still leaves a rendered artifact and a rubric, which item
// 4.2 turns into the visual:false static result.
func renderCritiqueTarget(
	ctx context.Context,
	env ToolEnv,
	t critiqueTarget,
	viewOpts RenderInputOptions,
	instruction string,
	critique bool,
) (bool, ToolResult, critiqueArtifact, string, error, error) {
	renderSource := t.RenderSource

	// §4e cache lookup: read the source bytes once (for either the mermaid
	// page or the plain browser render) and derive the content hash from them,
	// so a repeat critique of unchanged content can skip the re-render.
	sourceBytes, hashErr := readCritiqueSourceBytes(ctx, t)
	if hashErr != nil {
		return false, ToolResult{}, critiqueArtifact{}, "", nil, hashErr
	}
	sourceHash := critiqueContentHash(sourceBytes)
	material := critiqueRenderMaterial(viewOpts, instruction)
	key := critiqueCacheKey(sourceHash, material)

	var cleanup func()
	if t.Kind == renderKindMermaid {
		htmlPath, mermaidCleanup, buildErr := writeMermaidHTMLFile(string(sourceBytes), "")
		if buildErr != nil {
			return false, ToolResult{}, critiqueArtifact{}, "", nil,
				fmt.Errorf("cannot prepare flow render page: %w", buildErr)
		}
		renderSource = htmlPath
		cleanup = mermaidCleanup
	}
	if cleanup != nil {
		defer cleanup()
	}

	artifactPath := critiqueArtifactPath(t)

	// A matching cache entry means the pixels for this exact content + render
	// material are already on disk: attach them, report the artifact, and skip
	// the browser entirely. The critique still runs against the cached PNG, so
	// the vision tier judges the same bytes it would have rendered.
	if hit, ok := loadCachedCritiqueArtifact(ctx, artifactPath, key); ok {
		hit.artifact.Target = t.Label
		analysis, analyzeErr := "", error(nil)
		if critique {
			// The cached PNG is the render; run the vision pass against it so a
			// cache hit produces the same findings a fresh render would.
			pngAbs, resolveErr := filesystem.SafeResolvePathWithBypass(ctx, artifactPath)
			if resolveErr == nil {
				analysis, analyzeErr = runCritiqueVisionPass(ctx, env, instruction, pngAbs)
			} else {
				analyzeErr = resolveErr
			}
		}
		return true, hit.attachment, hit.artifact, analysis, analyzeErr, nil
	}

	var attachment ToolResult
	var pngBytes []byte
	var analysis string
	var analyzeErr error
	_, renderedBool, renderErr := renderInputToString(ctx, env, "design_critique", renderSource, viewOpts,
		func(renderCtx context.Context, pngPath string) (string, error) {
			// The browser's temp PNG exists only here: capture the SP-137
			// attachment, the raw bytes for the provenance-headed artifact,
			// and the vision analysis in this one window.
			if att, ok := buildRenderAttachment(renderCtx, pngPath); ok {
				attachment = att
			}
			safePath, resolveErr := filesystem.SafeResolvePathWithBypass(renderCtx, pngPath)
			if resolveErr != nil {
				return "", resolveErr
			}
			data, readErr := os.ReadFile(safePath)
			if readErr != nil {
				return "", readErr
			}
			pngBytes = data
			if !critique {
				return "", nil
			}
			// A vision failure must not fail the render: return the analysis
			// text (possibly empty) and surface the error through analyzeErr.
			text, aErr := runCritiqueVisionPass(renderCtx, env, instruction, pngPath)
			analysis, analyzeErr = text, aErr
			return text, nil
		})
	if renderErr != nil {
		return false, ToolResult{}, critiqueArtifact{}, "", nil, renderErr
	}
	if !renderedBool {
		return false, ToolResult{}, critiqueArtifact{}, "", nil, nil
	}

	artifact, writeErr := writeCritiqueArtifact(ctx, t, pngBytes, instruction, key, sourceHash, material)
	if writeErr != nil {
		// The artifact is derived output: failing to cache it must not fail
		// the critique. The caller still gets the render count and the
		// attached pixels.
		return true, attachment, critiqueArtifact{Target: t.Label}, analysis, analyzeErr, nil
	}
	return true, attachment, artifact, analysis, analyzeErr, nil
}

// writeCritiqueArtifact persists a rendered PNG under
// design/.cache/renders/ and appends the SP-140 invariant 2 provenance header.
// It returns the artifact descriptor (workspace-relative path + provenance
// text). A failure is returned for the caller to downgrade to a notice.
//
// sourceHash (the §4e content hash of the render source bytes) and material
// (the canonical render material) are recorded in the provenance so a consumer
// can verify the artifact; cacheKey (their composed key) is written to the
// sidecar so a later run can prove a cache hit rather than assume it.
func writeCritiqueArtifact(ctx context.Context, t critiqueTarget, png []byte, instruction, cacheKey, sourceHash, material string) (critiqueArtifact, error) {
	if len(png) == 0 {
		return critiqueArtifact{}, errors.New("no rendered bytes to persist")
	}

	artifactPath := critiqueArtifactPath(t)

	abs, err := filesystem.SafeResolvePathForWriteWithBypass(ctx, artifactPath)
	if err != nil {
		return critiqueArtifact{}, err
	}
	if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
		return critiqueArtifact{}, mkErr
	}

	provenance := buildCritiqueProvenance(t, instruction, sourceHash, material)
	if writeErr := os.WriteFile(abs, append(append([]byte{}, png...), []byte(provenanceBanner(provenance))...), 0o644); writeErr != nil {
		return critiqueArtifact{}, writeErr
	}
	// The sidecar is what makes the next run's cache lookup possible (§4e).
	// Best-effort: a sidecar write failure still leaves a valid PNG, so the
	// artifact is returned and the next run simply re-renders.
	_ = writeCritiqueCache(ctx, artifactPath, cacheKey, sourceHash, material)
	return critiqueArtifact{Target: t.Label, Path: artifactPath, Provenance: provenance}, nil
}

// critiqueArtifactPath is the derived-artifact path for a target:
// design/.cache/renders/<stem>.png, or design/.cache/renders/<base>~<cmp>.png
// for a comparison artifact, where <base> is the compared-against target's
// stem and <cmp> is this target's. `~` cannot appear in a design slug, so the
// composed name is unambiguous and a target never collides with its
// comparison.
func critiqueArtifactPath(t critiqueTarget) string {
	dir := path.Join(design.DirName, designArtifactDirName, designRenderArtifactDirName)
	stem := critiqueStem(t)
	if t.Stage != "compare" || t.CompareLabel == "" {
		return path.Join(dir, stem+".png")
	}
	base := critiqueStemForLabel(t.CompareLabel)
	return path.Join(dir, base+designCritiqueStemSeparator+stem+".png")
}

// critiqueStem is the slug-safe stem used for a target's artifact name,
// derived from its label (falling back to its source).
func critiqueStem(t critiqueTarget) string {
	stem := critiqueStemForLabel(t.Label)
	if stem == "target" {
		stem = critiqueStemForLabel(t.Source)
	}
	return stem
}

// critiqueStemForLabel derives a slug-safe stem from a workspace-relative
// label: "design/wireframes/login.svg" → "login". It returns "target" when the
// label yields nothing usable, so an artifact name is always produced.
func critiqueStemForLabel(label string) string {
	base := GetBaseName(label)
	if i := strings.LastIndex(base, "."); i > 0 {
		base = base[:i]
	}
	if base == "" {
		return "target"
	}
	// slugifyImageStem drops characters it cannot map, including a leading
	// digit or hyphen; the sentinel keeps them and is trimmed back off.
	stem := strings.TrimPrefix(slugifyImageStem("x"+base, ""), "x")
	if stem == "" {
		return "target"
	}
	return stem
}

// provenanceBanner renders the textual provenance block appended to a derived
// PNG. It is plain ASCII with a fixed opener and terminator so a tool that
// reads the raw bytes can extract it without parsing the image.
func provenanceBanner(provenance string) string {
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(provenanceHeaderPrefix)
	sb.WriteString("\n")
	sb.WriteString(provenance)
	sb.WriteString("\n")
	sb.WriteString(provenanceHeaderTerminator)
	sb.WriteString("\n")
	return sb.String()
}

// buildCritiqueProvenance composes the provenance text for one artifact
// (SP-140 invariant 2: a derived artifact must be recognizable as derived and
// carry enough to regenerate it).
//
// sourceHash is the artifact's §4e content hash (the SHA-256 of the render
// source bytes) and material is the canonical render material (viewport +
// rubric) folded into the cache key. Both are recorded so a consumer can
// verify the artifact's provenance and so a later run can prove a cache hit
// rather than assume it.
func buildCritiqueProvenance(t critiqueTarget, instruction, sourceHash, material string) string {
	var sb strings.Builder
	sb.WriteString("tool: design_critique\n")
	fmt.Fprintf(&sb, "source: %s\n", t.Label)
	fmt.Fprintf(&sb, "kind: %s\n", renderKindName(t.Kind))
	if sourceHash != "" {
		// The source content hash, per §4e.
		fmt.Fprintf(&sb, "sourceHash: %s\n", sourceHash)
	}
	if material != "" {
		fmt.Fprintf(&sb, "renderMaterial: %s\n", material)
	}
	sb.WriteString("generated: " + time.Now().UTC().Format(time.RFC3339) + "\n")
	sb.WriteString("note: derived render cache — never edit; regenerate with design_render/design_critique\n")
	if instruction != "" {
		// The instruction is the render-time input most likely to explain a
		// surprising artifact, so it is recorded (single line, for a stable
		// banner shape).
		sb.WriteString("instruction: " + strings.Join(strings.Fields(instruction), " ") + "\n")
	}
	return sb.String()
}

// renderKindName names a render kind for the provenance banner.
func renderKindName(k designRenderKind) string {
	switch k {
	case renderKindBrowser:
		return "browser"
	case renderKindMermaid:
		return "mermaid"
	default:
		return "unknown"
	}
}

// ---------------------------------------------------------------------------
// Render cache + cost cap (SP-140-4 §4e, TODO item 4.3)
// ---------------------------------------------------------------------------

// critiqueCacheFilename is the sidecar a cached render writes next to its PNG:
// design/.cache/renders/<stem>.cache.json. The PNG stays a plain
// provenance-headed image (SP-140 invariant 2); the sidecar holds the machine
// -readable cache key, so a cache lookup never has to parse the provenance
// banner out of image bytes.
const critiqueCacheFilenameSuffix = ".cache.json"

// critiqueCacheEntry is the sidecar record for one cached render. It is
// written atomically alongside the PNG and read on the next run.
type critiqueCacheEntry struct {
	// SourceHash is the SHA-256 (hex) of the render source bytes — the §4e
	// content hash. For a mermaid flow it is the hash of the .mmd content; for
	// an SVG/HTML screen it is the hash of the source file content.
	SourceHash string `json:"sourceHash"`
	// RenderMaterial is the canonical render material (viewport + rubric)
	// folded into the cache key, so a re-render with a different viewport or
	// rubric under the same path does not return a stale critique.
	RenderMaterial string `json:"renderMaterial"`
	// Artifact is the workspace-relative PNG path this entry describes.
	Artifact string `json:"artifact"`
	// Generated is when the cached render was produced (RFC3339 UTC).
	Generated string `json:"generated"`
}

// critiqueContentHash is the §4e content hash of a render source: the SHA-256
// of the source bytes, hex-encoded. It is the same hash the webui sidecar uses
// in spirit — a content-addressed digest of the render input — so an unchanged
// screen produces an unchanged key regardless of file mtime, and the hash can
// be recorded in the provenance header for a consumer to verify.
func critiqueContentHash(source []byte) string {
	sum := sha256.Sum256(source)
	return hex.EncodeToString(sum[:])
}

// critiqueRenderMaterial canonically describes everything besides the source
// content that can change the rendered pixels or the critique: the viewport
// and the rubric/instruction. Two runs whose source content is identical but
// whose viewport or rubric differ are *not* interchangeable — a re-render at a
// new viewport (or under a different rubric) must not return a stale critique —
// so the material is folded into the cache key and any change is a cache miss.
func critiqueRenderMaterial(viewOpts RenderInputOptions, instruction string) string {
	return fmt.Sprintf("viewport=%sx%s;rubric=%s",
		trimFloat(viewOpts.ViewportWidth), trimFloat(viewOpts.ViewportHeight),
		strings.Join(strings.Fields(instruction), " "))
}

// critiqueCacheKey composes the artifact's cache key from the source content
// hash and the render material, so a consumer (and the sidecar) has one string
// that identifies "these pixels from this source at this viewport under this
// rubric".
func critiqueCacheKey(sourceHash, material string) string {
	sum := sha256.Sum256([]byte(sourceHash + "\x00" + material))
	return hex.EncodeToString(sum[:])
}

// critiqueCachePath is the sidecar path for a target's artifact, alongside the
// PNG under design/.cache/renders/.
func critiqueCachePath(artifactPath string) string {
	return artifactPath + critiqueCacheFilenameSuffix
}

// readCritiqueSourceBytes reads the bytes the §4e content hash is derived from:
// the render source itself. For a mermaid flow that is the .mmd file content
// (the HTML page is generated from it and is not the source of truth); for an
// SVG/HTML screen it is the source file content. Reading goes through the
// workspace-safe resolver, so it is subject to the same Gate-1 checks as any
// other read.
func readCritiqueSourceBytes(ctx context.Context, t critiqueTarget) ([]byte, error) {
	rel := firstNonEmpty(t.Source, t.Label)
	if rel == "" {
		return nil, errors.New("no render source to hash")
	}
	data, err := readDesignSource(ctx, rel)
	if err != nil {
		return nil, fmt.Errorf("cannot read render source %s: %w", rel, err)
	}
	return data, nil
}

// loadCachedCritiqueArtifact returns a reusable cached render for artifactPath
// when one exists whose sidecar matches cacheKey, so the caller can skip the
// browser render entirely (§4e). The PNG is re-attached through the SP-137 path
// and the vision critique is run against the cached pixels, so a cache hit
// produces the same result as a fresh render would — the only thing skipped is
// the rasterization.
//
// A missing/corrupt/uncertain cache is a miss, never an error: the caller then
// renders normally. That keeps a stale or hand-edited cache from breaking a
// critique.
func loadCachedCritiqueArtifact(ctx context.Context, artifactPath, cacheKey string) (cachedCritiqueArtifact, bool) {
	sidecarPath := critiqueCachePath(artifactPath)
	absSidecar, err := filesystem.SafeResolvePathWithBypass(ctx, sidecarPath)
	if err != nil {
		return cachedCritiqueArtifact{}, false
	}
	raw, err := os.ReadFile(absSidecar)
	if err != nil {
		return cachedCritiqueArtifact{}, false
	}
	var entry critiqueCacheEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return cachedCritiqueArtifact{}, false
	}
	if entry.SourceHash == "" || entry.RenderMaterial == "" {
		return cachedCritiqueArtifact{}, false
	}
	// A sidecar naming a different artifact is not this artifact's cache entry
	// (e.g. a stale copy left behind by a rename): treat it as a miss.
	if entry.Artifact != "" && entry.Artifact != artifactPath {
		return cachedCritiqueArtifact{}, false
	}
	if critiqueCacheKey(entry.SourceHash, entry.RenderMaterial) != cacheKey {
		return cachedCritiqueArtifact{}, false
	}

	absPNG, err := filesystem.SafeResolvePathWithBypass(ctx, artifactPath)
	if err != nil {
		return cachedCritiqueArtifact{}, false
	}
	pngWithProvenance, readErr := os.ReadFile(absPNG)
	if readErr != nil {
		return cachedCritiqueArtifact{}, false
	}
	attachment, ok := buildRenderAttachment(ctx, absPNG)
	if !ok {
		return cachedCritiqueArtifact{}, false
	}
	return cachedCritiqueArtifact{
		attachment: attachment,
		artifact: critiqueArtifact{
			Target:     "",
			Path:       artifactPath,
			Provenance: extractProvenanceBanner(string(pngWithProvenance)),
			Cached:     true,
		},
	}, true
}

// extractProvenanceBanner pulls the textual provenance block back out of a
// cached artifact's bytes, so a cache hit reports the same provenance a fresh
// render would (the banner is appended after IEND and ignored by PNG decoders).
// A missing/malformed banner yields "" rather than an error: the artifact is
// still usable.
func extractProvenanceBanner(raw string) string {
	idx := strings.Index(raw, provenanceHeaderPrefix+"\n")
	if idx < 0 {
		return ""
	}
	body := raw[idx+len(provenanceHeaderPrefix)+1:]
	end := strings.Index(body, provenanceHeaderTerminator)
	if end < 0 {
		return ""
	}
	return strings.TrimSuffix(body[:end], "\n")
}

// cachedCritiqueArtifact is the reusable part of a cache hit: the SP-137
// attachment re-derived from the cached PNG plus the artifact descriptor.
type cachedCritiqueArtifact struct {
	attachment ToolResult
	artifact   critiqueArtifact
}

// writeCritiqueCache records the sidecar that makes a future run's cache lookup
// possible. It is best-effort: a failure to write the sidecar (or hand back a
// usable PNG) is reported so the caller can skip the cache but must never fail
// the critique, which already has its rendered pixels.
func writeCritiqueCache(ctx context.Context, artifactPath, cacheKey, sourceHash, material string) error {
	abs, err := filesystem.SafeResolvePathForWriteWithBypass(ctx, critiqueCachePath(artifactPath))
	if err != nil {
		return err
	}
	if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
		return mkErr
	}
	entry := critiqueCacheEntry{
		SourceHash:     sourceHash,
		RenderMaterial: material,
		Artifact:       artifactPath,
		Generated:      time.Now().UTC().Format(time.RFC3339),
	}
	data, jsonErr := json.MarshalIndent(entry, "", "  ")
	if jsonErr != nil {
		return jsonErr
	}
	return os.WriteFile(abs, append(data, '\n'), 0o644)
}

// trimFloat renders a viewport dimension without a trailing ".0" for integral
// values, so the material string is stable across int/float64 arg forms.
func trimFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// targetIsWholeTree reports whether the requested target names the whole
// design/ tree (the §4e cap's scope). Bare `design`, `design/`, `.`, and the
// empty string all resolve to the tree (see discoverCritiqueTargets).
func targetIsWholeTree(clean string) bool {
	return clean == design.DirName || clean == "." || clean == ""
}

// cleanCritiqueTarget normalizes a requested target the same way
// discoverCritiqueTargets does, so the cap's scope test agrees with discovery.
func cleanCritiqueTarget(requested string) string {
	return strings.TrimSuffix(filepath.ToSlash(path.Clean(strings.TrimSpace(requested))), "/")
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

// critiqueMaxScreensArg extracts the optional max_screens tool argument with
// the §4e default, rejecting a non-integer or non-positive value so a typo
// cannot quietly lower or disable the cost cap.
func critiqueMaxScreensArg(args map[string]any) (int, error) {
	v, exists := lookupKey(args, "max_screens")
	if !exists || v == nil {
		return designCritiqueDefaultMaxScreens, nil
	}
	var n int
	switch t := v.(type) {
	case int:
		n = t
	case int64:
		n = int(t)
	case float64:
		if t != float64(int(t)) {
			return 0, fmt.Errorf("parameter 'max_screens' must be a whole number, got %v", t)
		}
		n = int(t)
	default:
		return 0, fmt.Errorf("parameter 'max_screens' must be an integer, got %T", v)
	}
	if n < 1 {
		return 0, fmt.Errorf("parameter 'max_screens' must be at least 1, got %d", n)
	}
	return n, nil
}

// ---------------------------------------------------------------------------
// Findings + output
// ---------------------------------------------------------------------------

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
// Static degradation (SP-140-4 §4a, TODO item 4.2)
// ---------------------------------------------------------------------------

// staticCritiqueFindings is the non-vision degradation path: it runs the
// existing design validator (the same rule packs design_validate surfaces) over
// the critiqued targets and maps each design.Finding into the critique schema
// {target, area, severity, note, suggestion} with the validator rule id kept
// on the finding (critiqueFinding.Rule) so a consumer can trace it back.
//
// It is deliberately built on the validator that already exists — §4a says
// "degrade to design_validate-style static findings" — so a non-vision primary
// still gets a useful, structured critique (dangling data-nav, missing
// wireframes, literal-vs-token colors, screen inventory, naming) instead of an
// empty result. Items 4.4/4.5 add *new* rule packs to that validator; this
// function picks them up for free, which is why it calls the validator rather
// than re-implementing checks here.
//
// Root is taken from env.WorkspaceRoot exactly as design_validate does. A
// validator I/O error yields no static findings (never an error): the critique
// must not fail the turn, and a target that cannot be statically validated is
// simply reported with no static findings. Findings are filtered to the active
// rubric so a focused pass stays focused, and sorted for determinism.
func staticCritiqueFindings(env ToolEnv, targets []critiqueTarget, rubric string) []critiqueFinding {
	root := strings.TrimSpace(env.WorkspaceRoot)
	if root == "" {
		root = "."
	}

	// A whole-tree critique (any "tree" stage) validates the tree once instead
	// of validating each discovered file in turn: the tree validator is the
	// same code design_validate runs with no path, and it resolves
	// cross-file references (data-nav targets, flow node stems, README links)
	// that a single-file validation cannot see.
	treeWide := false
	for _, t := range targets {
		if t.Stage == "tree" {
			treeWide = true
			break
		}
	}

	var designFindings []design.Finding
	if treeWide {
		found, err := design.ValidateTree(root)
		if err != nil {
			// I/O failure inside the validator: degrade to no static findings
			// rather than failing a critique whose render already succeeded.
			return nil
		}
		designFindings = found
	} else {
		for _, t := range targets {
			// The comparison target is a delta input, not a separate critique;
			// its structural problems are not this run's findings.
			if t.Stage == "compare" {
				continue
			}
			rel := t.Label
			if rel == "" {
				rel = t.Source
			}
			if rel == "" {
				continue
			}
			found, err := design.ValidateFile(root, rel)
			if err != nil {
				// A flow (.mmd) is validated as a whole-tree asset by
				// ValidateFile, so it is covered only in the treeWide branch.
				// Any other per-file error is swallowed for the same
				// never-fail reason: advisory findings, not a turn failure.
				continue
			}
			designFindings = append(designFindings, found...)
		}
	}

	// A flow target validates as a tree asset only; when the caller named a
	// single flow, run the tree validator and keep just the flow's findings so
	// a named flow still gets its static checks.
	if !treeWide {
		for _, t := range targets {
			if t.Stage == "compare" || t.Kind != renderKindMermaid {
				continue
			}
			found, err := design.ValidateTree(root)
			if err != nil {
				continue
			}
			if rel := firstNonEmpty(t.Label, t.Source); rel != "" {
				for _, f := range found {
					if f.File == rel {
						designFindings = append(designFindings, f)
					}
				}
			}
			break
		}
	}

	areas := rubricAreas(rubric)
	out := make([]critiqueFinding, 0, len(designFindings))
	seen := make(map[string]bool, len(designFindings))
	for _, f := range designFindings {
		cf := critiqueFindingFromDesign(f)
		if !critiqueAreaInRubric(cf.Area, areas) {
			continue
		}
		// ValidateFile and the flow fallback can both surface a flow finding;
		// dedupe on the tuple a consumer sees.
		key := fmt.Sprintf("%s\x00%d\x00%s\x00%s", cf.Target, cf.Line, cf.Rule, cf.Note)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, cf)
	}
	stdsort.SliceStable(out, func(i, j int) bool {
		if out[i].Target != out[j].Target {
			return out[i].Target < out[j].Target
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		if out[i].Rule != out[j].Rule {
			return out[i].Rule < out[j].Rule
		}
		return out[i].Note < out[j].Note
	})
	return out
}

// critiqueFindingFromDesign maps one validator finding
// {file, line?, severity, message, rule} into the critique schema
// {target, area, severity, note, suggestion}. The validator rule id rides on
// the finding so tracing back to the static rule pack is possible.
func critiqueFindingFromDesign(f design.Finding) critiqueFinding {
	note := strings.TrimSpace(f.Message)
	if line := f.Line; line > 0 {
		note = fmt.Sprintf("line %d: %s", line, note)
	}
	return critiqueFinding{
		Target:     f.File,
		Area:       critiqueAreaForRule(f.Rule),
		Severity:   critiqueSeverityForDesign(f.Severity),
		Note:       note,
		Suggestion: critiqueStaticSuggestion(f.Rule),
		Line:       f.Line,
		Rule:       f.Rule,
	}
}

// critiqueSeverityForDesign maps the validator's severity vocabulary onto the
// critique's. The validator's `error` is a hard violation, which is a critique
// `blocker`; `warn` maps to `major`, `fix` (a machine-applicable advisory) to
// `minor`, and `info` stays `info`. An unknown severity degrades to `info`
// rather than being dropped.
func critiqueSeverityForDesign(s design.Severity) string {
	switch s {
	case design.SeverityError:
		return "blocker"
	case design.SeverityWarn:
		return "major"
	case design.SeverityFix:
		return "minor"
	case design.SeverityInfo:
		return "info"
	default:
		return "info"
	}
}

// critiqueAreaForRule maps a statically-known validator rule id into the
// critique vocabulary so a static finding carries a meaningful area. Unknown
// rules (including rule packs items 4.4/4.5 add later) fall back to
// "consistency", which is the honest description of "a design convention was
// violated".
func critiqueAreaForRule(rule string) string {
	switch rule {
	// Hierarchy: structure the user must be able to read top-down —
	// wireframe/frame/shape and the flow structure that frames it.
	case "svg_viewbox", "svg_frame_match", "screen_device_frame",
		"manifest_frames", "svg_wellformed", "flowchart_declaration",
		"flowchart_syntax", "flowchart_node_stem":
		return "hierarchy"

	// Affordance: something a user must be able to act on — an external
	// resource that will not load, an icon that is not drawable, a missing
	// wireframe the flow depends on.
	case "screen_external_ref", "icon_wellformed", "icon_self_containment",
		"icon_sprite_symbol", "svg_self_containment":
		return "affordance"

	// Contrast: the token/colour checks — literal colours and raw hex are
	// the static proxy for "this will not meet the palette / may not
	// contrast".
	case "brand_raw_hex", "brand_no_token_refs", "svg_data_uri_size":
		return "contrast"

	// Everything else is consistency: token membership/aliases, naming
	// (slugs), dangling references (data-nav, manifest links, feedback
	// targets), stable ids, text usage, feedback schema, the git contract.
	default:
		return "consistency"
	}
}

// critiqueAreaInRubric reports whether area is in the rubric's focused set.
func critiqueAreaInRubric(area string, areas []string) bool {
	for _, a := range areas {
		if a == area {
			return true
		}
	}
	return false
}

// critiqueStaticSuggestion is the rule-derived fix guidance attached to a
// static finding. §4a's findings carry a concrete fix; for a static finding the
// concrete fix is the validator's own remedy, which is what makes the
// degradation genuinely useful to a non-vision primary. Unknown rules get no
// suggestion rather than invented guidance.
func critiqueStaticSuggestion(rule string) string {
	switch rule {
	case "svg_data_nav_dangling":
		return "Point data-nav at an existing wireframe stem, or add the missing wireframe."
	case "svg_slug_name", "screen_slug_name", "icon_slug_name":
		return "Rename the file to the design slug form (lowercase, hyphen-separated)."
	case "svg_stable_ids":
		return "Give elements stable, semantic ids so flow targets can reference them."
	case "svg_self_containment":
		return "Inline the referenced asset; wireframes and icons must be self-contained."
	case "screen_external_ref":
		return "Inline the resource so the screen renders without network access."
	case "manifest_link_dangling":
		return "Fix the link to an existing design asset, or add the file it names."
	case "manifest_frames":
		return "Add a frames: block naming each device frame as name: WxH."
	case "flowchart_node_stem":
		return "Rename the flow node to match an existing wireframe stem."
	case "consistency_flow_edge_wireframe":
		return "Add the missing wireframe for the flow node, or mark the node terminal (no outgoing edge)."
	case "flowchart_declaration", "flowchart_syntax":
		return "Fix the mermaid declaration/syntax so the flow parses."
	case "token_alias_dangling":
		return "Point the alias at an existing token path, or add the aliased token."
	case "token_alias_cycle":
		return "Break the alias cycle so the token resolves to a literal value."
	case "token_type_membership":
		return "Give the token a declared $type so it satisfies the token contract."
	case "brand_raw_hex", "brand_no_token_refs":
		return "Reference the design token (e.g. a {color.*} token) instead of a literal value."
	case "screen_device_frame":
		return "Match the container width to a frame declared in the design README."
	default:
		return ""
	}
}

// staticCritiqueNotice is the per-run note that accompanies a degraded
// (visual:false) run with static findings.
func staticCritiqueNotice() string {
	return "No vision tier was reachable, so this critique degraded to static " +
		"design-validator findings (visual:false). Areas come from the design " +
		"rule packs, not from pixels: run design_validate for the full rule " +
		"report, and use a vision-capable model — or analyze_image_content — " +
		"to judge the rendered artifact directly."
}

// staticCritiqueCleanNotice is the note for a degraded run whose static pass
// found nothing, so an empty findings list is not mistaken for "no critique
// was attempted".
func staticCritiqueCleanNotice() string {
	return "No vision tier was reachable, so this critique degraded to static " +
		"design-validator findings (visual:false); the static pass found no " +
		"violations. Use a vision-capable model — or analyze_image_content — to " +
		"judge the rendered artifact directly."
}

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

func (h *designCritiqueHandler) Aliases() []string      { return nil }
func (h *designCritiqueHandler) Timeout() time.Duration { return 0 }
func (h *designCritiqueHandler) MaxResultSize() int     { return 0 }
func (h *designCritiqueHandler) SafeForParallel() bool  { return false }
func (h *designCritiqueHandler) Interactive() bool      { return false }

// registerDesignCritiqueTools registers the design_critique tool, which needs
// the host browser tier (rasterization) and the vision tier (critique).
// Excluded from WASM builds via design_critique_handler_js.go, which returns
// nil — mirroring registerDesignRenderTools (SP-140 invariant 7, SP-140-4 §4a).
func registerDesignCritiqueTools() []ToolHandler {
	return []ToolHandler{
		&designCritiqueHandler{},
	}
}
