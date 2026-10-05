package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
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
// It is a shared tool (SP-158). Rasterization goes through the same browser
// seam as design_render. The browser build has no separate vision tier, so
// there the rendered image and rubric ride the tool result and the primary
// model writes the critique (design_critique_vision_js.go).
//
// Scope note (SP-140-4 items 4.1–4.3): this is the tool core — render, attach,
// structured findings, the derived-artifact cache path with its provenance
// header (4.1), the non-vision degradation (4.2), and the content-hash render
// cache + whole-tree cost cap (4.3). The consistency rule packs (items
// 4.4/4.5) and feedback consumption (item 4.7) are out of scope here.
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
	// The §6d findings sidecars need the per-artifact target record (the
	// artifacts slice alone cannot answer "which target is this PNG?").
	artifactTargets := make([]critiqueTarget, 0, 4)
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
		artifactTargets = append(artifactTargets, t)
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
	// §6d: persist the findings beside the artifacts so the last critique of
	// each screen is reviewable in the DesignView detail pane (SP-140-6 §6g).
	// Best-effort — the sidecars are derived output; their failure never
	// fails a critique whose pixels and tool output already succeeded.
	writeCritiqueFindingsSidecars(ctx, env, &out, artifactTargets)
	attachment.Output = buildCritiqueSummary(out, len(attachment.Images) > 0, visionAnalysis, visionErr)
	attachment.StructuredOut = out
	return attachment, nil
}

// critiqueError is the shared failure shape: the same text in Output and as
// the returned typed error, which is the handler contract.
func (h *designCritiqueHandler) critiqueError(msg string) (ToolResult, error) {
	return ToolResult{Output: msg, IsError: true}, agenterrors.NewTool("design_critique", msg, nil)
}

// ---------------------------------------------------------------------------
// Rubric + prompt
// ---------------------------------------------------------------------------

func (h *designCritiqueHandler) Aliases() []string { return nil }

// designCritiqueTimeout bounds one Execute run. The §4e whole-tree cap allows
// up to 20 screens per run, each costing a browser render plus a vision pass
// (cache hits skip the render but not the vision call), so the registry's
// 5-minute default kills a mid-sized tree critique before it finishes. Thirty
// minutes covers 20 screens at roughly a minute apiece with headroom for the
// browser's first launch (possible Chrome download) and slower local vision
// models. Single-target runs finish far earlier; the timeout is a worst-case
// bound, not a target.
const designCritiqueTimeout = 30 * time.Minute

func (h *designCritiqueHandler) Timeout() time.Duration { return designCritiqueTimeout }
func (h *designCritiqueHandler) MaxResultSize() int     { return 0 }
func (h *designCritiqueHandler) SafeForParallel() bool  { return false }
func (h *designCritiqueHandler) Interactive() bool      { return false }
