package design

// brief_feedback.go — the README / token-ref / feedback lookups of the
// screen brief: briefReadmeEntry, briefWireframeTokenRefs, and the
// feedback-file helpers (briefScreenFeedback, briefFeedbackMatches,
// briefOpenAnnotationNotes), split out of brief.go.

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// briefReadmeEntry extracts the screen's purpose, status marker, and whether
// the README Screens listing names it. A missing README yields ("", "", false)
// plus no error — the brief reports the orphan rather than failing.
func briefReadmeEntry(root, screen string) (purpose, status string, listed bool) {
	data, err := os.ReadFile(filepath.Join(root, DirName, ManifestName))
	if err != nil {
		return "", "", false
	}
	text := string(data)
	statuses, summaries := parseManifestListings(text)
	if s, ok := statuses[screen]; ok {
		status = s
	}
	if s, ok := summaries[screen]; ok {
		purpose = s
	}
	for _, ref := range readmeScreenRefs(text) {
		if ref.section == "Screens" && ref.name == screen {
			listed = true
			break
		}
	}
	return purpose, status, listed
}

// briefWireframeTokenRefs reads the wireframe and returns the `{group.token}`
// references its comments carry, sorted by path and de-duplicated. A missing or
// unreadable wireframe yields an empty, non-nil slice (the brief reports the
// missing wireframe separately).
func briefWireframeTokenRefs(root, wireframeRel string, exists bool) []BriefTokenRef {
	refs := []BriefTokenRef{}
	if !exists {
		return refs
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(wireframeRel)))
	if err != nil {
		return refs
	}

	// Known token paths: reuse the export projection so a reference resolves
	// against the same set design_export_tokens consumes.
	known := map[string]bool{}
	if tokens, tErr := ResolveExportTokens(root); tErr == nil && tokens != nil {
		for _, leaf := range tokens.Leaves {
			known[leaf.Name] = true
		}
	}

	seen := map[string]bool{}
	for _, m := range tokenRefRe.FindAllSubmatch(data, -1) {
		p := string(m[1])
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		refs = append(refs, BriefTokenRef{Path: p, Known: known[p]})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Path < refs[j].Path })
	return refs
}

// briefScreenFeedback returns the §4d pending-feedback view for the screen: the
// feedback file whose target resolves to the screen stem (by file name or by
// target path). Notes are populated at full depth only.
func briefScreenFeedback(root, screen, depth string) (BriefFeedback, error) {
	states, err := ScanFeedbackDir(root)
	if err != nil {
		return BriefFeedback{}, err
	}
	for _, s := range states {
		if !briefFeedbackMatches(s, screen) {
			continue
		}
		fb := BriefFeedback{
			Path:       s.Path,
			Status:     s.Status,
			Open:       s.Pending,
			Total:      s.Annotations,
			Pending:    s.IsPending(),
			Resolution: s.Resolution,
		}
		if depth == BriefDepthFull {
			fb.Notes = briefOpenAnnotationNotes(root, s)
		}
		return fb, nil
	}
	return BriefFeedback{}, nil
}

// briefFeedbackMatches reports whether a feedback file concerns the screen:
// its target stem equals the screen, or its file name (stem) does.
func briefFeedbackMatches(s FeedbackFileState, screen string) bool {
	if s.Target != "" && feedbackTargetStem(s.Target) == strings.ToLower(screen) {
		return true
	}
	return assetName(path.Base(s.Path)) == screen
}

// briefOpenAnnotationNotes reads a feedback file and returns the open
// (unresolved) annotations as "[area] note" strings, in file order. A read or
// parse failure yields an empty, non-nil slice.
func briefOpenAnnotationNotes(root string, s FeedbackFileState) []string {
	notes := []string{}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(s.Path)))
	if err != nil {
		return notes
	}
	var doc feedbackFile
	if jsonErr := unmarshalJSON(data, &doc); jsonErr != nil {
		return notes
	}
	for _, a := range doc.Annotations {
		if a.Resolved {
			continue
		}
		note := strings.TrimSpace(a.Note)
		area := strings.TrimSpace(a.Area)
		switch {
		case area != "" && note != "":
			notes = append(notes, fmt.Sprintf("[%s] %s", area, note))
		case note != "":
			notes = append(notes, note)
		case area != "":
			notes = append(notes, "["+area+"]")
		}
	}
	return notes
}
