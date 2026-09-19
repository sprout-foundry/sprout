//go:build !js

package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/design"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// designImportSketchHandler implements ToolHandler for the design_import_sketch
// tool (SP-140-2 §2c): a whiteboard/paper/photo/reference image becomes a
// starting point in the design/ tree.
//
// This is a thin gatekeeper, not an image pipeline. It validates the request,
// runs the Gate-1 file-access precheck, classifies the target, attaches the
// image through the SP-137 images-preserving tool-result path, and returns an
// extraction brief carrying the target's format conventions. The extraction
// itself is done by the vision tier and the model, which writes the
// convention-compliant SVG/token/mermaid files with the normal file tools.
//
// It depends on the vision tier for the extraction text, so it is a
// //go:build !js file with a WASM stub in
// design_import_sketch_handler_js.go (mirroring design_render_handler.go).
// The registration is build-tagged and lives in neither the shared AllTools
// list nor the WASM roster (SP-140 invariant 7, SP-140-2 §2c).
type designImportSketchHandler struct{}

func (h *designImportSketchHandler) Name() string { return "design_import_sketch" }

// Sketch import targets: the canonical design/ subdirectory names an import can
// seed. These are exactly the three targets §2c names, and all three are
// design.Subdirs members.
const (
	sketchTargetWireframes = "wireframes"
	sketchTargetTokens     = "tokens"
	sketchTargetFlows      = "flows"
)

// sketchTargets is the accepted target set, in contract order.
var sketchTargets = []string{sketchTargetWireframes, sketchTargetTokens, sketchTargetFlows}

func (h *designImportSketchHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "design_import_sketch",
		Description: "Import a whiteboard/paper/photo/reference image of a UI as a starting point in " +
			"the design/ tree. Pass the image, the target (wireframes | tokens | flows) and an " +
			"optional screen_name; the image is attached and analyzed (vision tier), and this tool " +
			"returns the target's format conventions plus an extraction brief. Extraction is yours: " +
			"write the convention-compliant SVG/token/mermaid files yourself with write_file, using " +
			"design_assets first if a design/ tree already exists, then run design_validate and fix " +
			"the error-severity findings before declaring the import done. This tool never writes " +
			"into the design/ tree and never fails on a non-vision primary — it returns the image and " +
			"the brief either way.",
		Parameters: []ParameterDef{
			{
				Name:        "image_path",
				Type:        "string",
				Required:    true,
				Description: "Workspace-relative path to the sketch image (e.g. `design/feedback/login-sketch.png`), a photo of a whiteboard/paper drawing, or an exported frame. PNG/JPEG/WebP/etc — the image is attached for the vision tier.",
			},
			{
				Name:        "target",
				Type:        "string",
				Required:    true,
				Description: "Design artifact class to extract: `wireframes` (SVG wireframe), `tokens` (W3C DTCG *.tokens.json), or `flows` (mermaid .mmd). Other targets are rejected — import into one of these three.",
			},
			{
				Name:        "screen_name",
				Type:        "string",
				Required:    false,
				Description: "Optional kebab-case slug for the imported artifact (e.g. `login`, `sign-up`), used to name the target file (`design/wireframes/login.svg`). Must match the design slug rule ^[a-z0-9]+(-[a-z0-9]+)*$. Omit to let the extracted screen title suggest one.",
			},
			{
				Name:        "analysis_prompt",
				Type:        "string",
				Required:    false,
				Description: "Optional extra extraction instruction passed through to the vision tier (e.g. `focus on the form fields and their labels`).",
			},
		},
		Required: []string{"image_path", "target"},
	}
}

func (h *designImportSketchHandler) Validate(args map[string]any) error {
	if _, err := extractString(args, "image_path"); err != nil {
		return err
	}
	if _, err := extractString(args, "target"); err != nil {
		return err
	}
	if v, exists := lookupKey(args, "screen_name"); exists && v != nil {
		if _, ok := v.(string); !ok {
			return fmt.Errorf("parameter 'screen_name' must be a string, got %T", v)
		}
	}
	if v, exists := lookupKey(args, "analysis_prompt"); exists && v != nil {
		if _, ok := v.(string); !ok {
			return fmt.Errorf("parameter 'analysis_prompt' must be a string, got %T", v)
		}
	}
	return nil
}

// sketchImportOutput is the JSON-friendly structured result of one
// design_import_sketch run. It is the extraction brief: where the source came
// from, what to produce, where to put it, and the mandatory validate step.
// Nothing is written into the design/ tree by this tool, so there is no
// "created" field.
type sketchImportOutput struct {
	// ImagePath is the workspace-relative sketch image, exactly as supplied.
	ImagePath string `json:"imagePath"`
	// Target is the canonical design/ subdirectory: wireframes|tokens|flows.
	Target string `json:"target"`
	// TargetDir is the design/ subdirectory the artifacts belong in.
	TargetDir string `json:"targetDir"`
	// ScreenName is the requested slug ("" when omitted).
	ScreenName string `json:"screenName,omitempty"`
	// SuggestedName is the slug the agent should use; it equals ScreenName
	// when one was supplied, otherwise a slug derived from the image
	// basename (falling back to a target-specific default).
	SuggestedName string `json:"suggestedName"`
	// Artifact is the design/ path the import should produce.
	Artifact string `json:"artifact"`
	// Conventions is the target's format rules, verbatim from the tool's
	// conventions table, injected into the brief so the model does not have
	// to rediscover them.
	Conventions []string `json:"conventions"`
	// NextSteps is the ordered workflow after extraction. It always ends
	// with the design_validate reminder.
	NextSteps []string `json:"nextSteps"`
}

// sketchConventions holds the per-target format rules injected into the
// extraction brief. Keeping them here (rather than only in the design-system
// skill) is the §2c requirement that target conventions travel with the tool
// call, so an import cannot silently produce a non-conforming artifact.
var sketchConventions = map[string][]string{
	sketchTargetWireframes: {
		"One SVG per screen: design/wireframes/<slug>.svg with a viewBox and no width/height attributes (viewBox-only SVG is the contract).",
		"Draw structure, not polish: layout rectangles, labels, and placeholder copy — no brand color, no imagery.",
		"Name the file with the screen slug and give every interactive element a stable id (e.g. id=\"submit\") so flows can reference it.",
		"Use data-nav=\"<screen-slug>\" on elements that navigate to another screen; the slug must match an existing wireframe file stem.",
		"Starter document: <svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 390 844\"> … </svg>",
	},
	sketchTargetTokens: {
		"One W3C DTCG file per group: design/tokens/<group>.tokens.json (e.g. color.tokens.json, spacing.tokens.json).",
		"Every token leaf is {\"$value\": …, \"$type\": …}; $type must be a valid DTCG type (color, dimension, fontWeight, number, string, …).",
		"Reference other tokens with {group.subgroup.leaf} aliases rather than repeating literals — aliases must resolve and must not form a cycle.",
		"Colors read off a sketch are candidates, not truth: name them semantically (color.brand.primary, not color.blue-500).",
	},
	sketchTargetFlows: {
		"One mermaid flowchart per flow: design/flows/<slug>.mmd, starting with `flowchart TD` (or LR for a wide flow).",
		"Node ids are the screen slugs; every node must have a matching design/wireframes/<slug>.svg wireframe — a node with no wireframe is a validator finding.",
		"Keep labels short (the screen name); put branching on labelled edges: a -->|success| b.",
	},
}

// targetDirFor maps a sketch target to its canonical design/ subdirectory path
// ("wireframes" → "design/wireframes"). Unknown targets yield "".
func targetDirFor(target string) string {
	dir, ok := design.SubdirByName(target)
	if !ok {
		return ""
	}
	return dir
}

// isSketchTargetName reports whether name is one of the three accepted import
// targets (exact, lowercase).
func isSketchTargetName(name string) bool {
	for _, t := range sketchTargets {
		if t == name {
			return true
		}
	}
	return false
}

// isSlug reports whether s matches the design slug rule (design.SlugPattern),
// reimplemented here so the check needs no regexp compilation on the hot path.
func isSlug(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			continue
		case r == '-':
			// No leading, trailing, or doubled hyphens.
			if i == 0 || i == len(s)-1 || s[i-1] == '-' {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// slugifyImageStem derives a candidate slug from an image path's basename
// ("Login Screen 2.png" → "login-screen-2"), falling back to fallback when the
// basename carries no usable characters.
func slugifyImageStem(imagePath, fallback string) string {
	base := GetBaseName(imagePath)
	if i := strings.LastIndex(base, "."); i > 0 {
		base = base[:i]
	}
	base = strings.ToLower(base)

	var b strings.Builder
	lastHyphen := true // suppresses a leading hyphen
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return fallback
	}
	return slug
}

// sketchArtifactPath returns the design/ file the import should produce for a
// target + slug, or "" when the target is unknown.
func sketchArtifactPath(target, slug string) string {
	dir := targetDirFor(target)
	if dir == "" {
		return ""
	}
	switch target {
	case sketchTargetWireframes:
		return dir + "/" + slug + ".svg"
	case sketchTargetTokens:
		return dir + "/" + slug + ".tokens.json"
	case sketchTargetFlows:
		return dir + "/" + slug + ".mmd"
	default:
		return ""
	}
}

// defaultSketchSlug is the slug used when neither screen_name nor the image
// basename yields one.
func defaultSketchSlug(target string) string {
	switch target {
	case sketchTargetTokens:
		return "imported"
	case sketchTargetFlows:
		return "imported-flow"
	default:
		return "imported-screen"
	}
}

func (h *designImportSketchHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	imagePath, err := extractString(args, "image_path")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}
	imagePath = strings.TrimSpace(imagePath)

	target, err := extractString(args, "target")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}
	target = strings.ToLower(strings.TrimSpace(target))

	if imagePath == "" {
		msg := "design_import_sketch: image_path is required"
		return ToolResult{Output: msg, IsError: true}, agenterrors.NewTool("design_import_sketch", msg, nil)
	}
	if !isSketchTargetName(target) {
		msg := fmt.Sprintf("design_import_sketch: unsupported target %q — expected one of: %s",
			target, strings.Join(sketchTargets, ", "))
		return ToolResult{Output: msg, IsError: true}, agenterrors.NewTool("design_import_sketch", msg, nil)
	}

	// The sketch is always a workspace-local image: a URL is not an import
	// source (fetch it first, then import). This keeps the Gate-1 path check
	// meaningful for every call.
	if isHTTPURL(imagePath) {
		msg := fmt.Sprintf("design_import_sketch: image_path must be a workspace-local image, got URL %q — fetch it into the workspace first", imagePath)
		return ToolResult{Output: msg, IsError: true}, agenterrors.NewTool("design_import_sketch", msg, nil)
	}

	// Gate-1 precheck (SP-140 invariant 7): the image is read (attached) by
	// this tool, so it is checked before anything else touches it, mirroring
	// analyze_ui_screenshot / design_render.
	resolvedPath, decision := PrecheckFileAccess(ctx, env.FileAccessClassifier, "design_import_sketch", imagePath)
	if decision == "deny" {
		msg := fmt.Sprintf("design_import_sketch blocked: %s is not accessible from this session", imagePath)
		return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_import_sketch blocked: %s is not accessible", imagePath)
	}
	if decision == "prompt" && env.FileAccessPrompter != nil {
		if ctx2, approved := promptForOffWorkspacePath(ctx, env, "design_import_sketch", imagePath, resolvedPath, "read"); approved {
			ctx = ctx2
		} else {
			msg := fmt.Sprintf("design_import_sketch blocked: off-workspace access to %s was not approved", imagePath)
			return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_import_sketch blocked: off-workspace access to %s was not approved", imagePath)
		}
	}

	// The source must look like an image. The extension is the cheap gate;
	// buildImageAttachment independently verifies the magic bytes, so a
	// mislabelled file degrades to a brief without an attachment rather than
	// serving garbage as base64.
	if !isImageExtension(imagePath) {
		msg := fmt.Sprintf("design_import_sketch: %s is not a recognized image file — expected one of %s",
			imagePath, strings.Join(imageExtensionList(), ", "))
		return ToolResult{Output: msg, IsError: true}, agenterrors.NewTool("design_import_sketch", msg, nil)
	}

	screenName := strings.TrimSpace(stringArg(args, "screen_name"))
	if screenName != "" && !isSlug(screenName) {
		msg := fmt.Sprintf("design_import_sketch: screen_name %q is not a valid slug — use %s", screenName, design.SlugPattern)
		return ToolResult{Output: msg, IsError: true}, agenterrors.NewTool("design_import_sketch", msg, nil)
	}

	slug := screenName
	if slug == "" {
		slug = slugifyImageStem(imagePath, defaultSketchSlug(target))
	}

	out := sketchImportOutput{
		ImagePath:     imagePath,
		Target:        target,
		TargetDir:     targetDirFor(target),
		ScreenName:    screenName,
		SuggestedName: slug,
		Artifact:      sketchArtifactPath(target, slug),
		Conventions:   sketchConventions[target],
		NextSteps:     sketchNextSteps(target, slug),
	}

	// Attach the image through the SP-137 path so a vision-capable primary
	// sees the pixels, then ask the vision tier for the extraction text. Both
	// steps are best-effort: a non-vision primary still receives the brief,
	// which is the §2c "must not fail" requirement.
	attachment := buildImageAttachment(ctx, imagePath)
	analysisPrompt := sketchAnalysisPrompt(target, out, stringArg(args, "analysis_prompt"))
	analysis, analysisErr := AnalyzeImage(ctx, imagePath, analysisPrompt, sketchVisionMode(target))

	attachment.Output = buildSketchImportSummary(out, len(attachment.Images) > 0, analysis, analysisErr)
	attachment.StructuredOut = out
	return attachment, nil
}

// sketchVisionMode maps an import target to the analysis mode handed to the
// vision tier. All three modes resolve to the same analysis pipeline today;
// naming them keeps the intent explicit and lets the tier specialize later
// without changing the tool.
func sketchVisionMode(target string) string {
	switch target {
	case sketchTargetTokens:
		return "design-tokens"
	case sketchTargetFlows:
		return "design-flow"
	default:
		return "design-wireframe"
	}
}

// sketchNextSteps is the ordered workflow the agent follows after the brief.
// The last steps are always the design_validate reminder required by §2c.
func sketchNextSteps(target, slug string) []string {
	steps := []string{
		"Check for an existing design/ tree with design_assets and extend it rather than overwriting anything it reports.",
	}
	switch target {
	case sketchTargetWireframes:
		steps = append(steps,
			fmt.Sprintf("Write the wireframe to design/wireframes/%s.svg with write_file, viewBox-only, structure not polish.", slug),
			"If the sketch shows a multi-screen journey, write one SVG per screen and wire them with data-nav slugs.",
			"Add or update the design/README.md manifest entry for the new screen (name, status marker, one-line summary).",
		)
	case sketchTargetTokens:
		steps = append(steps,
			fmt.Sprintf("Write the extracted tokens to design/tokens/%s.tokens.json with write_file, one file per group, DTCG $value/$type on every leaf.", slug),
			"Merge new token groups into the existing design/tokens/*.tokens.json rather than dropping groups you did not re-extract.",
		)
	case sketchTargetFlows:
		steps = append(steps,
			fmt.Sprintf("Write the flow to design/flows/%s.mmd with write_file, `flowchart TD` (or LR), node ids matching screen slugs.", slug),
			"Add or update the design/README.md manifest entry for the flow (name, status marker, one-line summary).",
		)
	}
	steps = append(steps,
		"Run design_validate and fix every error-severity finding before declaring the import done.",
		"Design work is not done until design_validate reports zero error findings.",
	)
	return steps
}

// sketchAnalysisPrompt composes the extraction instruction handed to the
// vision tier. It names the target's expected structure so the returned text
// is already in the shape the agent needs to write, then appends any
// caller-supplied extra instruction.
func sketchAnalysisPrompt(target string, out sketchImportOutput, extra string) string {
	var sb strings.Builder
	switch target {
	case sketchTargetTokens:
		sb.WriteString("Extract the design tokens implied by this sketch: color roles, " +
			"typography sizes/weights, spacing steps, and radii. Report each as a named " +
			"token (semantic name, value, DTCG type) grouped by category, plus any " +
			"tokens you inferred rather than read. Do not invent a palette the sketch does not suggest.")
	case sketchTargetFlows:
		sb.WriteString("Extract the user flow shown in this sketch: every screen/step as a node, " +
			"every transition as a labelled edge, in order, with branch conditions named. " +
			"Report each node as a kebab-case slug suitable for a wireframe file stem.")
	default:
		sb.WriteString("Extract the UI structure shown in this sketch as an ordered, region-by-region " +
			"description: screen name, layout regions, each visible control with its label and " +
			"state, and where a tap would navigate next. Describe structure and hierarchy, not " +
			"visual polish, and state what is ambiguous or unreadable rather than guessing.")
	}
	fmt.Fprintf(&sb, " The extraction is destined for %s.", out.Artifact)
	if out.ScreenName == "" {
		sb.WriteString(" There is no screen name yet: propose a slug from the sketch's title.")
	}
	if extra = strings.TrimSpace(extra); extra != "" {
		sb.WriteString(" Additional instruction: " + extra)
	}
	return sb.String()
}

// buildSketchImportSummary composes the human-readable result: where the image
// came from, what to produce, the conventions, the next steps, and the vision
// tier's extraction text when available. It never reports an error for a
// missing vision tier — the brief plus the attachment is a complete result.
func buildSketchImportSummary(out sketchImportOutput, attached bool, analysis string, analysisErr error) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "design_import_sketch: %s → %s (target=%s).\n", out.ImagePath, out.Artifact, out.Target)
	if attached {
		sb.WriteString("The sketch image is attached for visual extraction.\n")
	}

	sb.WriteString("\nConventions for " + out.Target + ":")
	for _, c := range out.Conventions {
		sb.WriteString("\n- " + c)
	}

	sb.WriteString("\n\nNext steps:")
	for i, s := range out.NextSteps {
		fmt.Fprintf(&sb, "\n%d. %s", i+1, s)
	}

	analysis = strings.TrimSpace(analysis)
	switch {
	case analysis != "":
		sb.WriteString("\n\nExtracted structure (vision tier):\n" + analysis)
	case analysisErr != nil:
		fmt.Fprintf(&sb, "\n\nNo extraction text from the vision tier (%v); "+
			"read the attached image directly, or re-run with analyze_image_content (OCR/native fallback).", analysisErr)
	default:
		sb.WriteString("\n\nNo vision tier was available to extract structure; " +
			"the image is attached for a vision-capable primary, otherwise read it with " +
			"analyze_image_content (OCR/native fallback). The brief above is complete either way.")
	}
	return sb.String()
}

// imageExtensionList returns the recognized image extensions, sorted for a
// stable error message.
func imageExtensionList() []string {
	exts := make([]string, 0, len(imageExtensions))
	for ext := range imageExtensions {
		exts = append(exts, ext)
	}
	sort.Strings(exts)
	return exts
}

func (h *designImportSketchHandler) Aliases() []string      { return nil }
func (h *designImportSketchHandler) Timeout() time.Duration { return 0 }
func (h *designImportSketchHandler) MaxResultSize() int     { return 0 }
func (h *designImportSketchHandler) SafeForParallel() bool  { return false }
func (h *designImportSketchHandler) Interactive() bool      { return false }

// registerDesignImportSketchTools registers the design_import_sketch tool,
// which needs the vision tier to turn the attached image into extraction text.
// Excluded from WASM builds via design_import_sketch_handler_js.go, which
// returns nil — mirroring registerDesignRenderTools (SP-140 invariant 7,
// SP-140-2 §2c).
func registerDesignImportSketchTools() []ToolHandler {
	return []ToolHandler{
		&designImportSketchHandler{},
	}
}
