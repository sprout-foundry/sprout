package design

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Feedback-subdirectory name under design/. Named so the validator, the
// design_assets inventory, and the parser all agree on one literal.
const FeedbackSubdir = "feedback"

// Rule ids for the feedback JSON validator, SP-140-1 §1g + SP-140-4 §4d.
//
// The canonical feedback document (SP-140-4 §4d) is the single schema written
// by the webui feedback affordance (SP-140-3 §3e, webui/src/design/
// feedbackWrite.ts) and read here:
//
//	{"target": "<workspace-rel path>", "status": "changes-requested"|"resolved"|...,
//	 "resolution": "", "annotations": [{"id", "at": {"x","y"}, "area",
//	 "note", "resolved": bool, "created": RFC3339}]}
const (
	// ruleFeedbackJSON fires (hard) when a feedback file does not parse as a
	// valid feedback JSON object.
	ruleFeedbackJSON = "feedback_json"

	// ruleFeedbackTargetRef fires (hard) when target is empty.
	ruleFeedbackTargetRef = "feedback_target_ref"

	// ruleFeedbackTargetDangling fires (advisory) when the target does not
	// resolve to a known design asset stem (wireframe, screen, or flow).
	ruleFeedbackTargetDangling = "feedback_target_dangling"

	// ruleFeedbackAnnotationID fires (advisory) when an annotation id is
	// empty or duplicated within its file.
	ruleFeedbackAnnotationID = "feedback_annotation_id"

	// ruleFeedbackAnnotationNote fires (advisory) when an annotation note is
	// empty.
	ruleFeedbackAnnotationNote = "feedback_annotation_note"
)

// FeedbackStatusChangesRequested is the §4d status a human-written annotation
// carries while the agent still has to address it. The feedback affordance
// writes it (webui/src/design/feedbackWrite.ts PENDING_FEEDBACK_STATUS) and the
// design-system skill's loop starts any target carrying it with a read of its
// feedback file.
const FeedbackStatusChangesRequested = "changes-requested"

// feedbackFile is the machine-parseable shape of a design/feedback/*.json file
// per SP-140-4 §4d: the annotated target, the human-set status, the agent's
// resolution note, and the annotations themselves.
type feedbackFile struct {
	Target      string               `json:"target"`
	Status      string               `json:"status"`
	Resolution  string               `json:"resolution"`
	Annotations []feedbackAnnotation `json:"annotations"`
}

// feedbackAnnotation is one human annotation inside a feedback file: a
// normalized 0–1 point, the critique-area it belongs to, the note, its
// resolution flag, and its creation timestamp.
type feedbackAnnotation struct {
	ID       string        `json:"id"`
	At       feedbackPoint `json:"at"`
	Area     string        `json:"area"`
	Note     string        `json:"note"`
	Resolved bool          `json:"resolved"`
	Created  string        `json:"created"`
}

// feedbackPoint is the normalized 0–1 annotation location (§4d).
type feedbackPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// FeedbackFileState is the parsed, reporting-oriented view of one
// design/feedback/*.json file: the §4d fields plus the pending/resolved
// annotation counts the design_assets pending-feedback section needs.
//
// Parse is lenient by design — a malformed document yields Valid=false and the
// zero counts rather than an error, so a reporting run never fails on one bad
// file (the validator, not the reader, is where a bad file becomes a finding).
type FeedbackFileState struct {
	// Path is the workspace-relative slash path of the feedback file
	// (e.g. "design/feedback/login.json").
	Path string `json:"path"`
	// Target is the §4d target (e.g. "design/wireframes/login.svg"); empty
	// when the file is invalid or omits it.
	Target string `json:"target,omitempty"`
	// Status is the §4d top-level status ("changes-requested", "resolved",
	// …); empty when absent.
	Status string `json:"status,omitempty"`
	// Resolution is the agent's closing note; empty while the loop is open.
	Resolution string `json:"resolution,omitempty"`
	// Annotations is the total annotation count.
	Annotations int `json:"annotations"`
	// Resolved is the count of annotations with `resolved: true`.
	Resolved int `json:"resolved"`
	// Pending is the count of annotations with `resolved: false` (i.e.
	// Annotations - Resolved); it is the count the agent must address.
	Pending int `json:"pending"`
	// Valid reports whether the file parsed as the §4d document shape.
	Valid bool `json:"valid"`
}

// IsPending reports whether this feedback file still needs the agent's
// attention: its status is "changes-requested" OR it carries at least one
// annotation with `resolved: false` (§4d — the two conditions the design-system
// skill's loop starts on). A file that is not valid is never pending.
func (f FeedbackFileState) IsPending() bool {
	if !f.Valid {
		return false
	}
	return f.Status == FeedbackStatusChangesRequested || f.Pending > 0
}

// parseFeedbackFile parses one design/feedback JSON document into the §4d
// state. relPath is used only for the Path field. A document that is not valid
// JSON, or whose top level is not an object, yields Valid=false; the counts
// remain zero. Parsing is intentionally permissive about individual annotation
// fields (the validator reports those), so the reporting path cannot be broken
// by one odd annotation.
func parseFeedbackFile(relPath string, content []byte) FeedbackFileState {
	state := FeedbackFileState{Path: relPath}
	if len(strings.TrimSpace(string(content))) == 0 {
		return state
	}
	var doc feedbackFile
	if err := json.Unmarshal(content, &doc); err != nil {
		return state
	}
	// json.Unmarshal into a struct accepts a top-level array/string (it errors
	// on those, so the branch above covers it), but a top-level JSON literal
	// like `null` unmarshals cleanly into a nil map and the zero struct. Guard
	// the "was this actually an object" question explicitly so a bare
	// `null`/`42` stays invalid rather than reading as an empty-but-valid file.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(content, &probe); err != nil || probe == nil {
		return state
	}

	state.Valid = true
	state.Target = strings.TrimSpace(doc.Target)
	state.Status = strings.TrimSpace(doc.Status)
	state.Resolution = doc.Resolution
	state.Annotations = len(doc.Annotations)
	for _, a := range doc.Annotations {
		if a.Resolved {
			state.Resolved++
		}
	}
	state.Pending = state.Annotations - state.Resolved
	return state
}

// ScanFeedbackDir reads every design/feedback/*.json file under root and
// returns the parsed §4d state for each, sorted by path. A missing feedback
// directory yields an empty, non-nil slice; a real I/O failure reading the
// directory is returned as an error. Individual unreadable files are skipped
// rather than failing the walk: reporting must not break on one bad file.
func ScanFeedbackDir(root string) ([]FeedbackFileState, error) {
	dir := filepath.Join(root, DirName, FeedbackSubdir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []FeedbackFileState{}, nil
		}
		return nil, err
	}
	states := []FeedbackFileState{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		abs := filepath.Join(dir, e.Name())
		content, readErr := os.ReadFile(abs)
		if readErr != nil {
			continue
		}
		rel := DirName + "/" + FeedbackSubdir + "/" + e.Name()
		states = append(states, parseFeedbackFile(rel, content))
	}
	sort.Slice(states, func(i, j int) bool { return states[i].Path < states[j].Path })
	return states, nil
}

// PendingFeedbackCount returns the number of pending feedback files: those
// whose status is "changes-requested" or that carry at least one unresolved
// annotation (§4d). It is the single predicate the design_assets summary uses
// and the skill loop keys on.
func PendingFeedbackCount(states []FeedbackFileState) int {
	n := 0
	for _, s := range states {
		if s.IsPending() {
			n++
		}
	}
	return n
}

// ValidateFeedbackDir validates all design/feedback/*.json files under root,
// SP-140-4 §4d: the file parses as a feedback JSON object (hard `feedback_json`
// when it does not), target is non-empty (hard `feedback_target_ref`), each
// annotation id is non-empty and unique (`feedback_annotation_id`, advisory),
// each annotation note is non-empty (`feedback_annotation_note`, advisory), and
// the target resolves to a known wireframe/screen/flow stem
// (`feedback_target_dangling`, advisory). A missing feedback directory yields
// no findings. Findings are sorted by file, line, rule, message.
func ValidateFeedbackDir(root string) ([]Finding, error) {
	dir := filepath.Join(root, DirName, FeedbackSubdir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Finding{}, nil
		}
		return nil, err
	}
	stems := feedbackTargetStems(root)

	findings := []Finding{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(dir, e.Name()))
		if readErr != nil {
			return nil, readErr
		}
		findings = append(findings, validateFeedbackFile(root, DirName+"/"+FeedbackSubdir+"/"+e.Name(), data, stems)...)
	}
	sortFindings(findings)
	return findings, nil
}

// feedbackTargetStems returns the known design asset stems (wireframes,
// components, screens, flows) a §4d target may resolve against, lowercased and
// de-duplicated. Targets are workspace-relative asset paths
// ("design/wireframes/login.svg"), so resolution is by file stem.
func feedbackTargetStems(root string) []string {
	seen := map[string]bool{}
	var stems []string
	for _, spec := range []struct{ subdir, ext string }{
		{"wireframes", ".svg"},
		{"components", ".svg"},
		{"screens", ".html"},
		{"flows", ".mmd"},
	} {
		for _, stem := range fileStems(root, spec.subdir, spec.ext) {
			key := strings.ToLower(stem)
			if !seen[key] {
				seen[key] = true
				stems = append(stems, key)
			}
		}
	}
	return stems
}

// validateFeedbackFile validates one feedback JSON file's content against the
// §4d schema. stems are the known lowercased design asset stems the target is
// cross-referenced against.
func validateFeedbackFile(root, relPath string, content []byte, stems []string) []Finding {
	findings := []Finding{}

	if !json.Valid(content) {
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleFeedbackJSON,
			Severity: SeverityError,
			Message:  "feedback file is not valid JSON; the schema is {\"target\": ..., \"status\": ..., \"resolution\": ..., \"annotations\": [...]}",
		})
		return finalizeFeedbackFindings(findings)
	}

	// A top-level object is required (a bare `null`/`[]` parses as valid JSON
	// but is not a document; `null` in particular unmarshals into a nil map
	// without error, so the nil check is load-bearing).
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(content, &probe); err != nil || probe == nil {
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleFeedbackJSON,
			Severity: SeverityError,
			Message:  "feedback file is not a JSON object of shape {target, status, resolution, annotations}",
		})
		return finalizeFeedbackFindings(findings)
	}

	var fb feedbackFile
	if err := json.Unmarshal(content, &fb); err != nil {
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleFeedbackJSON,
			Severity: SeverityError,
			Message:  "feedback file is not a valid object of shape {target, status, resolution, annotations}: " + err.Error(),
		})
		return finalizeFeedbackFindings(findings)
	}

	// target is a non-empty string.
	target := strings.TrimSpace(fb.Target)
	if target == "" {
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleFeedbackTargetRef,
			Severity: SeverityError,
			Message:  "target must be a non-empty string naming the annotated asset (e.g. design/wireframes/login.svg)",
		})
	}

	// annotations[].id non-empty and unique; note non-empty (advisory).
	seenIDs := map[string]bool{}
	for i, a := range fb.Annotations {
		id := strings.TrimSpace(a.ID)
		if id == "" {
			findings = append(findings, Finding{
				File:     relPath,
				Rule:     ruleFeedbackAnnotationID,
				Severity: SeverityInfo,
				Message:  fmt.Sprintf("annotations[%d].id is empty", i),
			})
		} else if seenIDs[id] {
			findings = append(findings, Finding{
				File:     relPath,
				Rule:     ruleFeedbackAnnotationID,
				Severity: SeverityInfo,
				Message:  fmt.Sprintf("annotations[%d].id %q is duplicated", i, id),
			})
		} else {
			seenIDs[id] = true
		}
		if strings.TrimSpace(a.Note) == "" {
			findings = append(findings, Finding{
				File:     relPath,
				Rule:     ruleFeedbackAnnotationNote,
				Severity: SeverityInfo,
				Message:  fmt.Sprintf("annotations[%d].note is empty", i),
			})
		}
	}

	// cross-reference: the target resolves to a known design asset stem.
	if target != "" && len(stems) > 0 {
		if !sliceContains(stems, feedbackTargetStem(target)) {
			findings = append(findings, Finding{
				File:     relPath,
				Rule:     ruleFeedbackTargetDangling,
				Severity: SeverityInfo,
				Message:  fmt.Sprintf("target %q does not match any wireframes/, screens/, or flows/ file stem", target),
			})
		}
	}

	return finalizeFeedbackFindings(findings)
}

// feedbackTargetStem returns the lowercase file stem of a §4d target path: the
// last path segment with its extension removed ("design/wireframes/login.svg" →
// "login"). It mirrors the webui write path's stem keying (webui/src/design/
// feedbackWrite.ts feedbackStem). Lowercase so callers can compare against
// feedbackTargetStems directly, matching the case-insensitive stem vocabulary.
func feedbackTargetStem(target string) string {
	clean := strings.ReplaceAll(strings.TrimSpace(target), "\\", "/")
	clean = strings.TrimSuffix(clean, "/")
	base := path.Base(clean)
	if dot := strings.LastIndex(base, "."); dot > 0 {
		base = base[:dot]
	}
	return strings.ToLower(base)
}

// finalizeFeedbackFindings normalizes a feedback finding slice: never nil and
// sorted deterministically (file, line, rule, message). File and Severity are
// stamped at creation, so this only guards and sorts.
func finalizeFeedbackFindings(findings []Finding) []Finding {
	if findings == nil {
		findings = []Finding{}
	}
	sortFindings(findings)
	return findings
}

// sliceContains reports whether s contains v.
func sliceContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// fileStems returns the file stems (name without extension) of the files in
// root/design/subdir ending in ext, for cross-reference resolution.
func fileStems(root, subdir, ext string) []string {
	dir := filepath.Join(root, DirName, subdir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var stems []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ext) {
			continue
		}
		stems = append(stems, strings.TrimSuffix(e.Name(), ext))
	}
	return stems
}
