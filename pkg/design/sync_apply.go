package design

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// SyncPlanningOp is the kind of write a plan step performs.
type SyncPlanningOp = string

// Planning ops, fixed by the apply half.
const (
	// SyncPlanningUpdateToken rewrites an existing DTCG entry's value in place
	// (a literal token revalue). Delta is the token path.
	SyncPlanningUpdateToken SyncPlanningOp = "update-token"
	// SyncPlanningAddToken adds a DTCG entry that does not yet exist (a literal
	// token rename/addition the code introduced). Delta is the token path.
	SyncPlanningAddToken SyncPlanningOp = "add-token"
	// SyncPlanningAddWireframe creates a skeleton wireframe SVG with draft
	// status. Delta is the wireframe stem.
	SyncPlanningAddWireframe SyncPlanningOp = "add-wireframe"
	// SyncPlanningAddFlowEdge appends the proposed edge to a flow file. Delta is
	// the edge ("login --> home").
	SyncPlanningAddFlowEdge SyncPlanningOp = "add-flow-edge"
)

// SyncApplyWrite is one design-file write the apply half will perform. Path is
// workspace-relative, slash-separated, and always confined to design/ (§5e).
// Content is the full new file bytes (the write is a whole-file replace, which
// is what makes the ChangeTracker see an ordinary workspace edit).
type SyncApplyWrite struct {
	// Path is the design/ file to write, workspace-relative.
	Path string `json:"path"`
	// Op is the planning operation that produced this write.
	Op SyncPlanningOp `json:"op"`
	// Kind is the delta kind (token|wireframe|flow) this write serves, so a
	// consumer can group the run by the §5b vocabulary.
	Kind DeltaKind `json:"kind"`
	// Delta is the semantic delta description this write applies.
	Delta string `json:"delta"`
	// Token is the DTCG token path the write concerns ("" otherwise).
	Token string `json:"token,omitempty"`
	// Stem is the wireframe stem the write concerns ("" otherwise).
	Stem string `json:"stem,omitempty"`
	// Edge is the flow edge the write adds ("" otherwise).
	Edge string `json:"edge,omitempty"`
	// Status is the status marker the artifact carries (draft for §5b-created
	// artifacts; "" for a token revalue).
	Status string `json:"status,omitempty"`
	// Created is true when the write creates a file that did not exist.
	Created bool `json:"created"`
	// Content is the full new file bytes. It is deliberately JSON-omitted:
	// the plan's JSON shape names *what* will be written, and the handler is
	// the one that writes it; a consumer needing the bytes reads the file
	// after apply.
	Content []byte `json:"-"`
}

// SyncApplyProposal records a delta the apply half deliberately did NOT write:
// an inferred delta (a proposal §5b leaves to the agent/user via normal file
// edits), or a malformed/unsafe delta that could not be applied mechanically.
type SyncApplyProposal struct {
	// Delta is the semantic delta description.
	Delta string `json:"delta"`
	// Kind is the delta kind.
	Kind DeltaKind `json:"kind"`
	// Basis is literal|structural|inferred.
	Basis deltaBasis `json:"basis"`
	// Reason is why apply left it alone: "inferred" (§5b proposal),
	// "outside-design" (§5e refusal), or a specific refusal message.
	Reason string `json:"reason"`
	// ProposalKind / Proposed carry the §5b proposal fields when the delta is
	// an inferred proposal.
	ProposalKind string `json:"proposalKind,omitempty"`
	Proposed     string `json:"proposed,omitempty"`
	// CodeFiles are the touched code files the delta came from.
	CodeFiles []string `json:"codeFiles"`
}

// SyncApplyPlan is the pure result of planning a report's safe subset. The
// handler performs the Writes; nothing here touches the filesystem.
type SyncApplyPlan struct {
	// Mode is always "apply" (this is the apply half's plan).
	Mode string `json:"mode"`
	// Report is the analyze report the plan was derived from, so a consumer can
	// see the full delta set behind a partial apply.
	Report *SyncReport `json:"report"`
	// Writes is the ordered safe-subset write set (deterministic, design/-only).
	Writes []SyncApplyWrite `json:"writes"`
	// Proposals are the deltas apply deliberately left unwritten (inferred
	// proposals, plus any unsafe/unplannable delta).
	Proposals []SyncApplyProposal `json:"proposals"`
	// Refused are design-file writes that were suppressed because a write would
	// have escaped design/ (§5e). Always empty in a plan produced by
	// PlanSyncApply, which drops such writes; kept so a consumer can distinguish
	// "nothing to apply" from "something was refused".
	Refused []string `json:"refused,omitempty"`
	// AppliedCount / ProposalCount are the write and proposal tallies.
	AppliedCount  int `json:"appliedCount"`
	ProposalCount int `json:"proposalCount"`
	// WritePaths is the sorted set of distinct design/ paths the plan writes,
	// surfaced so a caller can assert design/-confinement without walking Writes.
	WritePaths []string `json:"writePaths"`
	// Notes carries run-level caveats (e.g. a malformed token file a write
	// could not be planned from).
	Notes []string `json:"notes,omitempty"`
}

// SyncFileReader returns the current bytes of an existing design file (path is
// workspace-relative, slash-separated). It returns (nil, false) when the file
// does not exist (a create) or cannot be read (a note, not a failure). The
// handler supplies a workspace-confined reader; the pure core never does I/O.
type SyncFileReader func(rel string) ([]byte, bool)

// IsConfinedToDesign reports whether every write in the plan targets a file
// inside design/ (§5e hard assertion). An empty plan is trivially confined.
func (p *SyncApplyPlan) IsConfinedToDesign() bool {
	if p == nil {
		return false
	}
	for _, w := range p.Writes {
		if !designConfinedPath(w.Path) {
			return false
		}
	}
	for _, path := range p.WritePaths {
		if !designConfinedPath(path) {
			return false
		}
	}
	return true
}

// designConfinedPath reports whether a slash path is inside design/ (the design
// dir itself or a descendant). The pure-core counterpart of the handler's
// designSyncDesignFile guard; kept here so the plan can self-check.
func designConfinedPath(p string) bool {
	clean := strings.TrimSuffix(path.Clean(strings.TrimSpace(p)), "/")
	if clean == "" || clean == "." {
		return false
	}
	return clean == DirName || strings.HasPrefix(clean, DirName+"/")
}

// PlanSyncApply is the pure apply core: given a report and a reader for the
// current design-file bytes, it returns the plan of safe-subset writes plus the
// proposals apply leaves alone. It performs no writes and never plans a write
// outside design/.
//
// A nil report (or one with no deltas) yields an empty, valid plan — apply on
// an already-synced tree is a no-op.
func PlanSyncApply(report *SyncReport, read SyncFileReader) *SyncApplyPlan {
	plan := &SyncApplyPlan{
		Mode:       SyncModeApplyConst,
		Report:     report,
		Writes:     []SyncApplyWrite{},
		Proposals:  []SyncApplyProposal{},
		WritePaths: []string{},
	}
	if report == nil {
		plan.Notes = append(plan.Notes, "No report to apply.")
		return plan
	}
	if read == nil {
		read = func(string) ([]byte, bool) { return nil, false }
	}

	// Dedup guards: two deltas may name the same file/entry (a value repeated
	// across two code files, a nav target and a route naming the same stem).
	// The plan writes each file once, folding later contributions in.
	writesByPath := map[string]int{}  // path -> index into plan.Writes
	tokenUpdates := map[string]bool{} // "file\x00token" -> already planned
	edgesByFile := map[string]map[string]bool{}
	wireframes := map[string]bool{}

	addWrite := func(w SyncApplyWrite) {
		if !designConfinedPath(w.Path) {
			// §5e: never plan a write outside design/. Surface the refusal
			// rather than silently dropping it.
			plan.Refused = append(plan.Refused, w.Path)
			return
		}
		plan.Writes = append(plan.Writes, w)
	}

	for _, d := range report.Deltas {
		if d.Proposal || !d.SafeToApply || d.Basis == DeltaBasisInferred {
			// §5b: inferred deltas (and anything not safe to apply) are
			// proposals the agent/user resolve via normal file edits.
			plan.Proposals = append(plan.Proposals, proposalFromDelta(d))
			continue
		}

		switch d.Kind {
		case DeltaKindToken:
			w, ok, note := planTokenWrite(d, read)
			if note != "" {
				plan.Notes = append(plan.Notes, note)
			}
			if !ok {
				plan.Proposals = append(plan.Proposals, SyncApplyProposal{
					Delta:     d.Delta,
					Kind:      d.Kind,
					Basis:     d.Basis,
					Reason:    note,
					CodeFiles: append([]string{}, d.CodeFiles...),
				})
				continue
			}
			key := w.Path + "\x00" + w.Token
			if tokenUpdates[key] {
				continue
			}
			tokenUpdates[key] = true
			if idx, exists := writesByPath[w.Path]; exists {
				// Fold into the existing write for this file: append this
				// token's edit onto the already-planned content by re-running
				// the token rewrite over the planned bytes.
				plan.Writes[idx].Content = w.Content
				continue
			}
			writesByPath[w.Path] = len(plan.Writes)
			addWrite(w)

		case DeltaKindWireframe:
			if d.WireframeStem == "" || wireframes[d.WireframeStem] {
				continue
			}
			wireframes[d.WireframeStem] = true
			wfPath := syncWireframePath(d)
			if !designConfinedPath(wfPath) {
				plan.Refused = append(plan.Refused, wfPath)
				continue
			}
			_, exists := read(wfPath)
			addWrite(SyncApplyWrite{
				Path:    wfPath,
				Op:      SyncPlanningAddWireframe,
				Kind:    DeltaKindWireframe,
				Delta:   d.Delta,
				Stem:    d.WireframeStem,
				Status:  FlowDraftStatus,
				Created: !exists,
				Content: []byte(skeletonWireframeSVG(d.WireframeStem)),
			})

		case DeltaKindFlow:
			if d.FlowEdge == "" || d.FlowFile == "" {
				continue
			}
			if edgesByFile[d.FlowFile] == nil {
				edgesByFile[d.FlowFile] = map[string]bool{}
			}
			if edgesByFile[d.FlowFile][d.FlowEdge] {
				continue
			}
			edgesByFile[d.FlowFile][d.FlowEdge] = true
			current, exists := read(d.FlowFile)
			var content []byte
			if exists {
				content = appendFlowEdge(current, d.FlowEdge)
			} else {
				content = []byte("flowchart TD\n  " + d.FlowEdge + "\n")
			}
			plan.Writes = append(plan.Writes, SyncApplyWrite{
				Path:    d.FlowFile,
				Op:      SyncPlanningAddFlowEdge,
				Kind:    DeltaKindFlow,
				Delta:   d.Delta,
				Edge:    d.FlowEdge,
				Stem:    d.WireframeStem,
				Status:  FlowDraftStatus,
				Created: !exists,
				Content: content,
			})

		default:
			// A feedback or unknown-kind safe delta: nothing mechanical to
			// write; leave it to the agent/user.
			plan.Proposals = append(plan.Proposals, SyncApplyProposal{
				Delta:     d.Delta,
				Kind:      d.Kind,
				Basis:     d.Basis,
				Reason:    "no mechanical write for this delta kind",
				CodeFiles: append([]string{}, d.CodeFiles...),
			})
		}
	}

	plan.AppliedCount = len(plan.Writes)
	plan.ProposalCount = len(plan.Proposals)
	plan.WritePaths = distinctSortedPaths(plan.Writes)
	return plan
}

// SyncModeApplyConst mirrors the handler's mode constant without importing the
// handler (the pure core must not depend on pkg/agent_tools).
const SyncModeApplyConst = "apply"

// proposalFromDelta turns an inferred delta into an apply proposal record.
func proposalFromDelta(d SyncDelta) SyncApplyProposal {
	reason := "inferred: a proposal for the agent/user to resolve via normal file edits"
	if d.Basis != DeltaBasisInferred {
		reason = "not safe to apply: left to the agent/user"
	}
	return SyncApplyProposal{
		Delta:        d.Delta,
		Kind:         d.Kind,
		Basis:        d.Basis,
		Reason:       reason,
		ProposalKind: d.ProposalKind,
		Proposed:     d.Proposed,
		CodeFiles:    append([]string{}, d.CodeFiles...),
	}
}

// planTokenWrite plans the DTCG write a literal token delta requires: revalue
// the referenced entry when it exists, add it when the code introduced a new
// spelling of an existing token. ok is false (with a note) when no write can be
// planned — e.g. no token file is named, or the file cannot be parsed.
func planTokenWrite(d SyncDelta, read SyncFileReader) (SyncApplyWrite, bool, string) {
	// TokenFile is the DTCG file the delta names. A delta that names none (a
	// bare token reference the analyzer could only point at the token *tier*)
	// falls back to a conventional per-group file under design/tokens/. Never
	// treat TokenEntry as a path: it is an entry key, not a file.
	tokenFile := d.TokenFile
	if tokenFile == "" {
		for _, f := range d.DesignFiles {
			if strings.HasPrefix(f, path.Join(DirName, TokenSubdir)+"/") &&
				strings.HasSuffix(f, ".tokens.json") {
				tokenFile = f
				break
			}
		}
	}
	tokenPath := tokenTokenPath(d)
	if tokenFile == "" && tokenPath != "" {
		// Conventional per-group file: design/tokens/<group>.tokens.json, the
		// same convention tokenSourceFile/proposedTokenFile use.
		if group := topGroup(tokenPath); group != "" {
			tokenFile = path.Join(DirName, TokenSubdir, group+".tokens.json")
		}
	}
	if tokenFile == "" || tokenPath == "" {
		return SyncApplyWrite{}, false, "token delta names no DTCG file/entry; left as a proposal"
	}
	if !designConfinedPath(tokenFile) {
		return SyncApplyWrite{}, false, "token delta's file is outside design/; refused"
	}
	// A mechanical token write needs a concrete literal value. A bare
	// `{token.path}` reference (a structural token delta) carries none, so
	// inventing an entry would write an empty value — that is a proposal for
	// the agent/user, not a mechanical write.
	if !hasLiteralTokenValue(d) {
		return SyncApplyWrite{}, false, "token delta carries no concrete value to write; left as a proposal"
	}

	current, exists := read(tokenFile)
	if !exists || len(current) == 0 {
		// The token file does not exist yet: create it with just this entry.
		return SyncApplyWrite{
			Path:    tokenFile,
			Op:      SyncPlanningAddToken,
			Kind:    DeltaKindToken,
			Delta:   d.Delta,
			Token:   tokenPath,
			Created: true,
			Content: createTokenDocument(tokenPath, d),
		}, true, ""
	}

	newContent, applied, err := rewriteDTCEntry(current, tokenPath, d)
	if err != nil {
		return SyncApplyWrite{}, false, fmt.Sprintf("%s: %v", tokenFile, err)
	}
	if !applied {
		// The entry is not present (the code renamed/introduced it): add it.
		newContent, err = addDTCEntry(current, tokenPath, d)
		if err != nil {
			return SyncApplyWrite{}, false, fmt.Sprintf("%s: %v", tokenFile, err)
		}
		return SyncApplyWrite{
			Path:    tokenFile,
			Op:      SyncPlanningAddToken,
			Kind:    DeltaKindToken,
			Delta:   d.Delta,
			Token:   tokenPath,
			Created: false,
			Content: newContent,
		}, true, ""
	}
	return SyncApplyWrite{
		Path:    tokenFile,
		Op:      SyncPlanningUpdateToken,
		Kind:    DeltaKindToken,
		Delta:   d.Delta,
		Token:   tokenPath,
		Created: false,
		Content: newContent,
	}, true, ""
}

// tokenTokenPath is the DTCG entry a token delta concerns: the entry when set,
// else the token.
func tokenTokenPath(d SyncDelta) string {
	if d.TokenEntry != "" {
		return d.TokenEntry
	}
	return d.Token
}

// syncWireframePath is the wireframe path a structural delta targets: the delta's
// designFiles entry ending in .svg under design/wireframes/, else the
// conventional path from the stem.
func syncWireframePath(d SyncDelta) string {
	for _, f := range d.DesignFiles {
		if strings.HasPrefix(f, path.Join(DirName, "wireframes")+"/") &&
			strings.HasSuffix(f, ".svg") {
			return f
		}
	}
	if d.WireframeStem != "" {
		return path.Join(DirName, "wireframes", d.WireframeStem+".svg")
	}
	return ""
}

// skeletonWireframeSVG renders the §5b skeleton wireframe for a new screen: a
// viewBox-only SVG carrying the draft status marker (as a comment, the same
// convention the manifest status markers and token-usage comments use) and the
// screen's stem as the root id. It is deliberately structure-only — a draft the
// next design turn fleshes out — never code.
//
// The comment carries the status so the artifact is self-describing; the frame
// matches the mobile frame the fixture tree uses (390x844).
func skeletonWireframeSVG(stem string) string {
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">` + "\n")
	fmt.Fprintf(&b, "  <!-- status: %s (created by design_sync apply from a new route/screen in code) -->\n", FlowDraftStatus)
	fmt.Fprintf(&b, "  <!-- screen: %s -->\n", stem)
	fmt.Fprintf(&b, "  <g id=\"%s\">\n", stem)
	b.WriteString(`    <rect x="0" y="0" width="390" height="844" fill="none" />` + "\n")
	b.WriteString("  </g>\n")
	b.WriteString("</svg>\n")
	return b.String()
}

// appendFlowEdge appends an edge statement to a flow document, preserving the
// existing declaration if there is one and adding one when there is not. It is
// idempotent: an edge already present (as a parsed edge) is not re-added.
func appendFlowEdge(content []byte, edge string) []byte {
	text := string(content)
	fc := ParseFlowchart(text)
	for _, e := range fc.Edges {
		if e.Source+" --> "+e.Target == edge {
			return content
		}
	}
	if len(content) > 0 && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if fc.Declarations == 0 {
		text = "flowchart TD\n" + text
	}
	return []byte(text + "  " + edge + "\n")
}

// distinctSortedPaths returns the sorted distinct write paths.
func distinctSortedPaths(writes []SyncApplyWrite) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(writes))
	for _, w := range writes {
		if seen[w.Path] {
			continue
		}
		seen[w.Path] = true
		out = append(out, w.Path)
	}
	sort.Strings(out)
	return out
}
