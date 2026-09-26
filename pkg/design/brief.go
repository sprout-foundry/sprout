package design

// brief.go — the screen-brief core: the Brief types (ScreenBrief,
// BriefFlowEdge, BriefTokenRef, BriefFeedback, ScreenBriefInput),
// BuildScreenBrief / SerializeScreenBrief / RenderScreenBrief, and the
// screen-name / depth normalisation + JSON helpers. Flow-edge extraction
// lives in brief_flows.go; the README / token-ref / feedback lookups in
// brief_feedback.go.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Design↔code sync, screen-brief half (SP-140-5 §5g).
//
// This file is the pure, read-only core of the `design_brief` tool: given a
// screen name (a wireframe stem) and the workspace root, it reads the design/
// tree *for that screen* and returns a structured brief the implementing agent
// reads before building it:
//
//   - purpose           — the screen's purpose, from the README Screens listing;
//   - wireframe path    — design/wireframes/<stem>.svg;
//   - flows in/out      — every flow edge touching the screen, with its trigger
//     (the mermaid edge label), direction, and flow file;
//   - token paths       — the DTCG token paths the wireframe refers to (its
//     `{group.token}` comments), plus the token groups available to consume;
//   - open feedback     — pending §4d feedback annotations for the screen;
//   - status            — the README status marker (draft/review/ready).
//
// The brief is a *contract, not a generator* (§5g): it writes nothing and it
// produces no component code. It is purely advisory context returned in the
// ToolResult. Determinism is a property of the core — the same tree must yield
// the same brief byte for byte — so every slice is sorted by a canonical key
// and nothing is ordered by map iteration.
const (
	// BriefDepthSummary is the default depth (§5g): a condensed brief — counts
	// and headings rather than the full annotation/detail text.
	BriefDepthSummary = "summary"
	// BriefDepthFull is the detailed brief: annotation notes, per-edge detail,
	// and the flow file list.
	BriefDepthFull = "full"
)

// BriefFlowEdge is one flow edge touching the briefed screen (§5g "flows in/out
// with triggers"). Trigger is the mermaid edge label — the `-- "tap Submit" -->`
// form carries the trigger semantics per the design manifest contract; a
// labelled edge whose label is empty reports "".
type BriefFlowEdge struct {
	// Flow is the flow file (workspace-relative, e.g. design/flows/sign-up.mmd).
	Flow string `json:"flow"`
	// FlowName is the flow's stem (e.g. "sign-up").
	FlowName string `json:"flowName"`
	// Source / Target are the edge endpoints (node ids == wireframe stems).
	Source string `json:"source"`
	Target string `json:"target"`
	// Direction is "in" (the screen is the target), "out" (the screen is the
	// source), or "both" (a self-edge). It is the §5g "in/out" split.
	Direction string `json:"direction"`
	// Trigger is the edge label, i.e. what causes the transition (§5g
	// "triggers"). "" when the edge carries no label.
	Trigger string `json:"trigger,omitempty"`
	// OtherStem is the far endpoint of the edge (the node the screen moves to,
	// or comes from). "" for a self-edge.
	OtherStem string `json:"otherStem,omitempty"`
	// OtherLabel is the far endpoint's node label, when the flow declared one.
	OtherLabel string `json:"otherLabel,omitempty"`
}

// BriefTokenRef is one DTCG token a wireframe refers to, resolved from the
// wireframe's `{group.token}` comments (the SP-140-4 §4b convention). Known
// reports whether the reference resolves to a real DTCG leaf.
type BriefTokenRef struct {
	// Path is the dotted token path (e.g. "color.brand.primary").
	Path string `json:"path"`
	// Known is true when the reference resolves to a DTCG leaf in
	// design/tokens/.
	Known bool `json:"known"`
}

// BriefFeedback is the §4d feedback view for the screen: the feedback file
// (when one exists) plus the open (unresolved) annotation notes. Notes carries
// the annotation text at full depth and is empty at summary depth.
type BriefFeedback struct {
	// Path is the feedback file (workspace-relative) when one exists.
	Path string `json:"path,omitempty"`
	// Status is the §4d top-level status ("changes-requested", "resolved", …).
	Status string `json:"status,omitempty"`
	// Open is the number of annotations with `resolved: false`.
	Open int `json:"open"`
	// Total is the total annotation count in the file.
	Total int `json:"total"`
	// Pending reports whether the file still needs the agent's attention
	// (status changes-requested or at least one unresolved annotation, §4d).
	Pending bool `json:"pending"`
	// Resolution is the agent's closing note, when present.
	Resolution string `json:"resolution,omitempty"`
	// Notes are the open annotations' notes ("[area] note"), in file order.
	// Populated at full depth only.
	Notes []string `json:"notes,omitempty"`
}

// ScreenBrief is the structured §5g brief for one screen.
type ScreenBrief struct {
	// ScreenName is the requested wireframe stem.
	ScreenName string `json:"screenName"`
	// Depth is the depth the brief was rendered at (summary|full).
	Depth string `json:"depth"`
	// Found reports whether the screen exists in the design tree (a wireframe
	// with that stem, or a feedback file targeting it). An unknown screen
	// yields Found=false with Guidance, never an error.
	Found bool `json:"found"`

	// Purpose is the screen's purpose from the README Screens listing (§5g
	// "README purpose"). "" when the README lists no entry for the screen.
	Purpose string `json:"purpose,omitempty"`
	// Status is the README status marker (draft|review|ready). "" when the
	// README declares none.
	Status string `json:"status,omitempty"`
	// ListedInReadme reports whether the README Screens listing names the
	// screen (the §4b inventory signal: an unlisted wireframe is an orphan).
	ListedInReadme bool `json:"listedInReadme"`

	// Wireframe is the wireframe path (design/wireframes/<stem>.svg). It is
	// always set: the brief names where the wireframe *would* live, so a
	// not-found brief still points the agent at the right file.
	Wireframe string `json:"wireframe"`
	// WireframeExists reports whether that file exists.
	WireframeExists bool `json:"wireframeExists"`

	// FlowsIn / FlowsOut are the flow edges touching the screen, split by
	// direction (§5g "flows in/out with triggers"). A self-edge appears in
	// both. Both are always non-nil.
	FlowsIn  []BriefFlowEdge `json:"flowsIn"`
	FlowsOut []BriefFlowEdge `json:"flowsOut"`

	// TokenPaths are the DTCG tokens the wireframe refers to, sorted by path
	// (§5g "token paths to consume").
	TokenPaths []BriefTokenRef `json:"tokenPaths"`
	// TokenGroups are the top-level token groups available to consume, sorted
	// by group — the tree's token vocabulary, so the agent knows what exists
	// even when the wireframe refers to nothing yet.
	TokenGroups []TokenGroupCount `json:"tokenGroups"`

	// Feedback is the §4d pending-feedback view for the screen (§5g "open
	// feedback annotations"). The matching feedback file is the one whose §4d
	// target resolves to the screen's stem, or — when no target matches — the
	// one whose own file name (stem) equals the screen; the first match by
	// path order wins (ScanFeedbackDir sorts by path).
	Feedback BriefFeedback `json:"feedback"`

	// ScreenFile is the delivered design/screens/<stem>.html path, when one
	// exists (full depth only when it differs from the wireframe).
	ScreenFile string `json:"screenFile,omitempty"`

	// Guidance is a not-found helper: what to do when the screen is unknown
	// ("" when the screen exists).
	Guidance string `json:"guidance,omitempty"`

	// Notes carry run-level caveats (e.g. a tree with no README).
	Notes []string `json:"notes,omitempty"`

	// WritesNothing states the §5g contract explicitly: the brief is read-only
	// advisory context, never a generator. Always true.
	WritesNothing bool `json:"writesNothing"`
}

// SerializeScreenBrief renders the brief to deterministic, 2-space-indented
// JSON. The bytes are stable for identical trees, so a caller can hash or diff
// the output (the tests rely on this for the determinism assertion).
func SerializeScreenBrief(b *ScreenBrief) ([]byte, error) {
	return marshalIndentNoEscape(b)
}

// RenderScreenBrief builds the human/agent-readable one-block summary of the
// brief, so a model reads the shape from the text without parsing JSON. The
// summary contracts to counts at summary depth and includes the annotation
// notes at full depth.
func RenderScreenBrief(b *ScreenBrief) string {
	if b == nil {
		return "design_brief: no brief."
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "design_brief (%s): screen %q", b.Depth, b.ScreenName)
	if !b.Found {
		sb.WriteString(" — not found in the design tree.")
		if b.Guidance != "" {
			sb.WriteString(" " + b.Guidance)
		}
		return sb.String()
	}
	sb.WriteString(" — found")
	if b.Status != "" {
		fmt.Fprintf(&sb, ", status %s", b.Status)
	}
	sb.WriteString(".")
	if b.Purpose != "" {
		fmt.Fprintf(&sb, " Purpose: %s.", b.Purpose)
	}
	if b.WireframeExists {
		fmt.Fprintf(&sb, " Wireframe: %s.", b.Wireframe)
	} else {
		fmt.Fprintf(&sb, " Wireframe MISSING (expected %s).", b.Wireframe)
	}
	fmt.Fprintf(&sb, " Flows: %d in, %d out.", len(b.FlowsIn), len(b.FlowsOut))
	if triggers := briefTriggerList(b); len(triggers) > 0 {
		fmt.Fprintf(&sb, " Triggers: %s.", strings.Join(triggers, "; "))
	}
	if len(b.TokenPaths) > 0 {
		paths := make([]string, 0, len(b.TokenPaths))
		for _, t := range b.TokenPaths {
			mark := ""
			if !t.Known {
				mark = " (unknown)"
			}
			paths = append(paths, "{"+t.Path+"}"+mark)
		}
		fmt.Fprintf(&sb, " Tokens: %s.", strings.Join(paths, ", "))
	} else if len(b.TokenGroups) > 0 {
		groups := make([]string, 0, len(b.TokenGroups))
		for _, g := range b.TokenGroups {
			groups = append(groups, fmt.Sprintf("%s(%d)", g.Group, g.Tokens))
		}
		fmt.Fprintf(&sb, " Tokens: no {token} references yet; available groups: %s.", strings.Join(groups, ", "))
	}
	if b.Feedback.Open > 0 || b.Feedback.Pending {
		fmt.Fprintf(&sb, " Open feedback: %d/%d unresolved annotation(s) in %s.", b.Feedback.Open, b.Feedback.Total, b.Feedback.Path)
		if b.Depth == BriefDepthFull && len(b.Feedback.Notes) > 0 {
			sb.WriteString(" " + strings.Join(b.Feedback.Notes, " | ") + ".")
		}
	} else if b.Feedback.Path != "" {
		sb.WriteString(" No open feedback.")
	}
	if !b.ListedInReadme {
		sb.WriteString(" Not listed in the README Screens listing (orphan screen).")
	}
	sb.WriteString(" This is a contract, not a generator: read it before building the screen; no component code is produced.")
	if len(b.Notes) > 0 {
		sb.WriteString(" " + strings.Join(b.Notes, " "))
	}
	return sb.String()
}

// briefTriggerList renders each in/out edge as "src --> tgt (trigger)" for the
// summary line. Deterministic (FlowsIn/FlowsOut are already sorted).
func briefTriggerList(b *ScreenBrief) []string {
	out := make([]string, 0, len(b.FlowsIn)+len(b.FlowsOut))
	for _, group := range [][]BriefFlowEdge{b.FlowsIn, b.FlowsOut} {
		for _, e := range group {
			label := e.Trigger
			if label == "" {
				label = "no trigger label"
			}
			out = append(out, fmt.Sprintf("%s --> %s (%s)", e.Source, e.Target, label))
		}
	}
	return out
}

// ScreenBriefInput is the read-only input for BuildScreenBrief.
type ScreenBriefInput struct {
	// Root is the workspace root (the parent of design/).
	Root string
	// ScreenName is the wireframe stem to brief.
	ScreenName string
	// Depth is summary|full; "" defaults to summary.
	Depth string
}

// BuildScreenBrief reads the design/ tree under root and returns the §5g brief
// for the screen. It never writes and never errors on a missing screen or a
// missing design/ tree: an unknown screen yields Found=false with guidance, and
// a workspace with no design/ yields the same plus a note. Only a real I/O
// failure reading a file it must read is returned as an error.
func BuildScreenBrief(in ScreenBriefInput) (*ScreenBrief, error) {
	root := in.Root
	if root == "" {
		root = "."
	}
	screen := normaliseScreenName(in.ScreenName)
	depth := normaliseBriefDepth(in.Depth)

	brief := &ScreenBrief{
		ScreenName:    screen,
		Depth:         depth,
		Wireframe:     path.Join(DirName, "wireframes", screen+".svg"),
		FlowsIn:       []BriefFlowEdge{},
		FlowsOut:      []BriefFlowEdge{},
		TokenPaths:    []BriefTokenRef{},
		TokenGroups:   []TokenGroupCount{},
		WritesNothing: true,
	}

	if !FileExists(root) {
		brief.Guidance = boolGuidance(screen, false)
		brief.Notes = append(brief.Notes,
			"No design/ tree found — the brief cannot resolve a screen yet. "+
				"Scaffold design/ with the design-system skill, then re-run design_brief.")
		return brief, nil
	}

	// README purpose + status for the screen.
	purpose, status, listed := briefReadmeEntry(root, screen)
	brief.Purpose = purpose
	brief.Status = status
	brief.ListedInReadme = listed

	// Wireframe existence.
	wireframeAbs := filepath.Join(root, filepath.FromSlash(brief.Wireframe))
	if info, statErr := os.Stat(wireframeAbs); statErr == nil && !info.IsDir() {
		brief.WireframeExists = true
	}

	// Flow edges touching the screen, with triggers.
	edges, err := briefFlowEdges(root, screen)
	if err != nil {
		return nil, err
	}
	for _, e := range edges {
		switch e.Direction {
		case "in":
			brief.FlowsIn = append(brief.FlowsIn, e)
		case "out":
			brief.FlowsOut = append(brief.FlowsOut, e)
		case "both":
			brief.FlowsIn = append(brief.FlowsIn, e)
			brief.FlowsOut = append(brief.FlowsOut, e)
		}
	}

	// Token paths the wireframe refers to, plus the available token groups.
	brief.TokenPaths = briefWireframeTokenRefs(root, brief.Wireframe, WireframeExists(brief.WireframeExists))
	if groups, groupErr := scanTokenGroups(root); groupErr == nil {
		brief.TokenGroups = groups
	} else {
		brief.Notes = append(brief.Notes, "Token groups could not be read: "+groupErr.Error())
	}

	// Pending feedback for the screen.
	feedback, fbErr := briefScreenFeedback(root, screen, depth)
	if fbErr != nil {
		return nil, fbErr
	}
	brief.Feedback = feedback

	// Delivered screen file (full depth only; the long form names it).
	if depth == BriefDepthFull {
		screenRel := path.Join(DirName, "screens", screen+".html")
		if info, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(screenRel))); statErr == nil && !info.IsDir() {
			brief.ScreenFile = screenRel
		}
	}

	// Found = a wireframe with this stem, or a feedback file targeting it. A
	// screen may legitimately exist in the tree only as pending feedback (the
	// webui annotation affordance can annotate before the wireframe lands), so
	// feedback alone counts.
	brief.Found = brief.WireframeExists || brief.Feedback.Path != ""
	if !brief.Found {
		brief.Guidance = boolGuidance(screen, true)
	} else {
		if !brief.ListedInReadme {
			brief.Notes = append(brief.Notes,
				"The README Screens listing does not name this screen; it is an orphan (no inventory row / no purpose). Add it to the manifest.")
		}
	}

	return brief, nil
}

// WireframeExists is a tiny convenience so the token-reference call reads
// clearly; it mirrors the boolean field.
func WireframeExists(exists bool) bool { return exists }

// normaliseScreenName trims a screen name and strips a design-asset path or
// extension a caller might pass ("design/wireframes/login.svg" → "login"), so
// the brief accepts the stem the tool contract names even when the model passes
// a path. Any leading directory part (relative or absolute, POSIX or Windows
// separators — backslashes are normalised to '/' first) is dropped, leaving the
// final path segment; a design asset extension (.svg/.html/.mmd/.json) is then
// stripped. An empty or separator-only input yields "".
func normaliseScreenName(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.ReplaceAll(s, "\\", "/")
	s = strings.TrimSuffix(s, "/")
	if s == "" {
		return ""
	}
	// Keep only the final path segment, so both "design/wireframes/login.svg"
	// and "/tmp/login" reduce to "login".
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		s = s[i+1:]
	}
	for _, ext := range []string{".svg", ".html", ".mmd", ".json"} {
		if strings.HasSuffix(s, ext) {
			s = strings.TrimSuffix(s, ext)
			break
		}
	}
	return s
}

// normaliseBriefDepth trims and lowercases the depth argument, defaulting to
// summary (§5g: "optional depth (summary | full, default summary)").
func normaliseBriefDepth(raw string) string {
	d := strings.ToLower(strings.TrimSpace(raw))
	if d == BriefDepthFull {
		return BriefDepthFull
	}
	return BriefDepthSummary
}

// boolGuidance builds the not-found guidance for a screen name. When the tree
// exists it points at design_assets to list the real screens; when it does not,
// it points at the scaffold skill.
func boolGuidance(screen string, treeExists bool) string {
	name := screen
	if name == "" {
		name = "(empty)"
	}
	if !treeExists {
		return fmt.Sprintf("No design/ tree exists, so screen %q cannot be resolved. "+
			"Scaffold design/ (design-system skill: README, tokens, wireframes, flows, screens) and re-run.", name)
	}
	return fmt.Sprintf("No wireframe design/wireframes/%s.svg and no feedback targeting %q. "+
		"Check the screen name, or run design_assets to list the design tree's screens/flows.", name, name)
}

// marshalIndentNoEscape renders v as 2-space-indented JSON without HTML
// escaping, matching the deterministic artifact style the design tier uses.
func marshalIndentNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// unmarshalJSON decodes a JSON document. It is a tiny indirection so brief.go
// stays readable; the feedback reader uses the §4d struct.
func unmarshalJSON(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
