//go:build !js

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	stdsort "sort"
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
// Scope note (SP-140-4 item 4.1): this is the tool core — render, attach,
// structured findings, and the derived-artifact cache path with its
// provenance header. Non-vision degradation (item 4.2), reasoning about
// render counts/caps (item 4.3), the consistency rule packs (items 4.4/4.5),
// and feedback consumption (item 4.7) are separate TODO items; the seams they
// need (visualCritiqueEnabled, designCriticoArtifactPath, the finding model)
// are in place here but their behavior is deliberately not implemented.
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

// designCritiqueMaxTargetsBanner is deliberately NOT the §4e cap. §4e's
// whole-tree cap (20 screens, item 4.3) is a separate TODO item; this tool
// critiques exactly what the caller named so item 4.3 can add the cap without
// rewriting the discovery path.
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
			"with a provenance header; that PNG is never a source of truth. A critique never " +
			"blocks the turn: the findings are advisory, and a non-vision primary still receives " +
			"the rendered artifact and the rubric.",
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
	RenderCount int `json:"renderCount"`
	// Visual reports whether a vision-capable critique ran against real
	// pixels: a vision tier was reachable (a wired VisionProcessor, or an
	// available package-level vision capability — see
	// critiqueVisionTierAvailable) *and* at least one primary pass came back
	// with an analysis. Item 4.1 always renders and attaches; a run with no
	// vision tier reports visual=false and returns the static rubric findings
	// (item 4.2 layers the full degradation on top of this field).
	Visual bool `json:"visual"`
	// Screen is the screen slug when the target resolved to a screen ("").
	Screen string `json:"screen,omitempty"`
	// Note is the per-run notice ("" when there is nothing to say). Used for
	// the §4e whole-tree cap notice by item 4.3 and for render-skip notices.
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
		Target:      target,
		Rubric:      rubric,
		CompareTo:   compareTo,
		Findings:    []critiqueFinding{},
		BySeverity:  zeroSeverityTally(),
		Artifacts:   []critiqueArtifact{},
		Areas:       designCritiqueAreas,
		Visual:      false,
		Instruction: buildCritiquePrompt(rubric, target, compareTo, analysisPrompt),
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
		out.RenderCount++
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

	// Visual is decided once, after every pass: true only when a vision tier
	// was actually available and one of the primary passes came back with an
	// analysis. A run that never reached the tier (no wired processor and no
	// package-level vision capability — the hermetic unit-test environment, or
	// a non-vision primary) reports visual=false no matter what AnalyzeImage
	// returned, so the field is deterministic on every platform. This is the
	// §4a "explicit visual: false marker" that item 4.2 layers its full
	// degradation (static findings, never-fail) on top of.
	out.Visual = critiqued && critiqueVisionTierAvailable(env)

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
	var cleanup func()
	if t.Kind == renderKindMermaid {
		data, readErr := readDesignSource(ctx, t.Source)
		if readErr != nil {
			return false, ToolResult{}, critiqueArtifact{}, "", nil,
				fmt.Errorf("cannot read flow source %s: %w", t.Source, readErr)
		}
		htmlPath, mermaidCleanup, buildErr := writeMermaidHTMLFile(string(data), "")
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

	artifact, writeErr := writeCritiqueArtifact(ctx, t, pngBytes, instruction)
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
func writeCritiqueArtifact(ctx context.Context, t critiqueTarget, png []byte, instruction string) (critiqueArtifact, error) {
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

	provenance := buildCritiqueProvenance(t, instruction)
	if writeErr := os.WriteFile(abs, append(append([]byte{}, png...), []byte(provenanceBanner(provenance))...), 0o644); writeErr != nil {
		return critiqueArtifact{}, writeErr
	}
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
func buildCritiqueProvenance(t critiqueTarget, instruction string) string {
	var sb strings.Builder
	sb.WriteString("tool: design_critique\n")
	fmt.Fprintf(&sb, "source: %s\n", t.Label)
	fmt.Fprintf(&sb, "kind: %s\n", renderKindName(t.Kind))
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

	if len(out.Artifacts) > 0 {
		fmt.Fprintf(&sb, "\n\nRendered %d target(s); derived artifact(s):", out.RenderCount)
		for _, a := range out.Artifacts {
			if a.Path == "" {
				continue
			}
			sb.WriteString("\n- " + a.Path + " (derived; provenance header)")
		}
	}
	if attached {
		sb.WriteString("\nThe rendered image is attached for visual critique.")
	}

	if out.Count > 0 {
		sb.WriteString("\n\nFindings:")
		for _, f := range out.Findings {
			fmt.Fprintf(&sb, "\n- [%s] %s — %s", f.Severity, f.Area, f.Note)
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
		fmt.Fprintf(&sb, "\n\nNo critique text from the vision tier (%v); the rendered artifact "+
			"and the rubric above are complete either way — read the image directly if you can, "+
			"or run analyze_image_content (OCR/native fallback) on the artifact path.", analysisErr)
	default:
		sb.WriteString("\n\nNo vision tier was available to critique the render; the rendered " +
			"artifact and the rubric above are complete either way — read the image directly " +
			"if you can, or run analyze_image_content (OCR/native fallback) on the artifact path.")
	}
	if out.Note != "" {
		sb.WriteString("\n\nNote: " + out.Note)
	}
	return sb.String()
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
