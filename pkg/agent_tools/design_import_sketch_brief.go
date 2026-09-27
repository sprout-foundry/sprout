//go:build !js

package tools

// design_import_sketch_brief.go — the vision-tier brief composition for the
// design_import_sketch tool, split out of design_import_sketch_handler.go.
// These helpers turn the sketchImportOutput brief into what the vision tier
// analyzes and what the model does next: sketchVisionMode picks the analysis
// mode, sketchNextSteps is the ordered post-extraction workflow,
// sketchAnalysisPrompt composes the extraction instruction, and
// buildSketchImportSummary composes the human-readable result.
import (
	"fmt"
	"strings"
)

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
