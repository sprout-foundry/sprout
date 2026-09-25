package design

// The screens.json derived index (SP-143 §4, §143.5): the machine-readable
// screen graph — per stem, the device/frame, declared states, and data-nav
// edges with their triggers — generated from the screens' data-attributes,
// provenance-hashed over the inputs. Drift (hand-edit or staleness) is a
// validator error, the same convention as the .mmd/token exports: the index
// is generated, never hand-edited; fix the screens and regenerate.
//
// This file is the pure half: parsing and rendering with no workspace I/O in
// the render path, so it is unit-testable without a tree. The
// filesystem-facing entry points are DeriveScreensIndex (below),
// ValidateScreensIndex / validateScreenIndexGraph in screens.go (wired into
// ValidateTree), and the design_export_tokens `screens` target.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ScreensIndexFilename is the screens.json artifact name under
// design/generated/ (SP-143 §4).
const ScreensIndexFilename = "screens.json"

// ExportTargetScreens is the design_export_tokens target that writes the
// derived screens.json. It is deliberately not part of ExportTargets (the
// `all` set): the token targets render from the token sources, while the
// screens index derives from design/screens/*.html, so a plain re-theme run
// (`all`) never rewrites the screen graph — it is an explicit regeneration.
const ExportTargetScreens = "screens"

// ErrNoScreensForIndex is returned when the screens index is requested but
// design/screens/ holds no screens — there is nothing to derive, and an
// empty index would be misleading. A distinct sentinel so the handler
// reports a usage error, mirroring ErrNoTokensForExport.
var ErrNoScreensForIndex = errors.New("no design screens found to index")

// screenIndexProvenance / screenIndexSource are the fixed banner facts of the
// derived index, worded like the token export's.
const (
	screenIndexProvenance = "Generated from design/screens/*.html data-attributes (SP-143 §4). Do not edit by hand; regenerate with design_export_tokens targets:screens."
	screenIndexSourceFmt  = "design/screens/*.html. Recompute the hash from those bytes (concat in stem order, %s) to verify."
)

// ScreenIndexDoc is the whole design/screens/ graph derived from the screens'
// data-attributes. Screens are sorted by stem; Nav is sorted by (trigger, to)
// and deduplicated. Everything is derived, so the JSON is byte-identical for
// identical screen bytes — the determinism the drift check depends on.
type ScreenIndexDoc struct {
	Provenance    string             `json:"provenance"`
	SourceHash    string             `json:"source-hash"`
	Source        string             `json:"source"`
	FormatVersion int                `json:"format-version"`
	Screens       []ScreenIndexEntry `json:"screens"`
}

// ScreenIndexEntry is one screen's derived metadata.
type ScreenIndexEntry struct {
	// Stem is the screen's file stem (the slug nav targets and the README
	// Screens listing use).
	Stem string `json:"stem"`
	// Device is the screen's data-device attribute, or "" when absent (the
	// desktop default; chrome.css draws no chrome for it).
	Device string `json:"device"`
	// Frame is the px width a container on this screen is sized to when it
	// matches a README-declared frame (advisory per the existing frame rule),
	// else 0. Derived from the same width scan the frame validator runs.
	Frame int `json:"frame"`
	// States are the screen's declared state names (html[data-states]),
	// in declaration order, deduplicated.
	States []string `json:"states"`
	// Nav is the screen's data-nav edges, sorted by (trigger, to).
	Nav []ScreenIndexNav `json:"nav"`
}

// ScreenIndexNav is one data-nav edge: the target stem and the authored
// trigger label (`to:<stem>;trigger:<label>`).
type ScreenIndexNav struct {
	To string `json:"to"`
	// Trigger is the authored trigger label; "" when the attribute carries
	// only to:.
	Trigger string `json:"trigger,omitempty"`
}

// screenIndexFormatVersion is stamped in the doc so a format change can bump
// it and readers can tell.
const screenIndexFormatVersion = 1

// screenAnyAttrRe captures any HTML attribute as (name, value). Submatches:
// 1 = attribute name, 2/3/4 = double-quoted / single-quoted / unquoted value.
var screenAnyAttrRe = regexp.MustCompile(`(?i)\b([a-zA-Z_:][-a-zA-Z0-9_:.]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)

// ParseScreenHTML derives the screens-index view of one screen document: the
// html element's data-device / data-states, the body's data-nav edges, and
// the declared frame a container width matches. It reads data-attributes
// only — the point of the index is that other tooling never parses HTML at
// read time, so the one place that does is here.
//
// frames is the README-declared device frame list (may be nil). A container
// width matching a declared frame width is reported as Frame (the first
// match in frame order); no match or no frames yields 0.
func ParseScreenHTML(content []byte, frames []Frame) ScreenIndexEntry {
	entry := ScreenIndexEntry{States: []string{}, Nav: []ScreenIndexNav{}}

	htmlAttrs := htmlElementAttrs(content)
	entry.Device = htmlAttrs["data-device"]
	seenState := map[string]bool{}
	for _, name := range strings.Split(htmlAttrs["data-states"], ",") {
		name = strings.TrimSpace(name)
		if name == "" || seenState[name] {
			continue
		}
		seenState[name] = true
		entry.States = append(entry.States, name)
	}

	seenEdge := map[ScreenIndexNav]bool{}
	for _, nav := range extractDataNavs(content) {
		edge := ScreenIndexNav{To: nav.value, Trigger: nav.trigger}
		if seenEdge[edge] {
			continue
		}
		seenEdge[edge] = true
		entry.Nav = append(entry.Nav, edge)
	}
	sort.SliceStable(entry.Nav, func(i, j int) bool {
		if entry.Nav[i].Trigger != entry.Nav[j].Trigger {
			return entry.Nav[i].Trigger < entry.Nav[j].Trigger
		}
		return entry.Nav[i].To < entry.Nav[j].To
	})

	if width := screenWidths(string(content)); len(width) > 0 {
		for _, f := range frames {
			for _, w := range width {
				if w == f.Width {
					entry.Frame = f.Width
					break
				}
			}
			if entry.Frame != 0 {
				break
			}
		}
	}

	return entry
}

// htmlElementAttrs extracts the attributes of the document's <html …> open
// tag, lowercase-keyed. A document without one yields an empty map (a
// fragment); structural problems are the screen validator's business, not
// the index's.
func htmlElementAttrs(content []byte) map[string]string {
	m := map[string]string{}
	text := string(content)
	idx := strings.Index(text, "<html")
	if idx < 0 {
		return m
	}
	end := strings.Index(text[idx:], ">")
	if end < 0 {
		return m
	}
	for _, pair := range screenAnyAttrRe.FindAllStringSubmatch(text[idx:idx+end+1], -1) {
		value := pair[2]
		if value == "" {
			value = pair[3]
		}
		if value == "" {
			value = pair[4]
		}
		m[strings.ToLower(pair[1])] = value
	}
	return m
}

// screenDataNav is one authored attribute occurrence: the captured value, an
// optional parsed trigger label (data-nav), and the 1-based source line.
type screenDataNav struct {
	value   string
	trigger string
	line    int
}

// dataNavSpecRe matches a `to:<stem>` segment and captures the stem.
var dataNavSpecRe = regexp.MustCompile(`(?i)(?:^|;)\s*to:([a-zA-Z0-9_-]+)`)

// dataNavTriggerRe matches a `trigger:<label>` segment and captures the label.
var dataNavTriggerRe = regexp.MustCompile(`(?i)(?:^|;)\s*trigger:([^;]*)`)

// screenAttrOccurrences returns every attribute value in the document whose
// name equals want (case-insensitively, per HTML), in document order, with
// 1-based lines.
func screenAttrOccurrences(text, want string) []screenDataNav {
	var out []screenDataNav
	for _, m := range screenAnyAttrRe.FindAllStringSubmatchIndex(text, -1) {
		if !strings.EqualFold(text[m[2]:m[3]], want) {
			continue
		}
		vs, ve := m[4], m[5]
		if vs < 0 {
			vs, ve = m[6], m[7]
		}
		if vs < 0 {
			vs, ve = m[8], m[9]
		}
		if vs < 0 {
			continue
		}
		out = append(out, screenDataNav{
			value: text[vs:ve],
			line:  strings.Count(text[:vs], "\n") + 1,
		})
	}
	return out
}

// extractDataNavs parses every data-nav value per the SP-140-9 §9a contract
// (to:<stem>[;trigger:<label>]). An unparsable value (no to: segment) is
// skipped — the index derives only from well-formed edges; the target
// validator reports the rest.
func extractDataNavs(content []byte) []screenDataNav {
	var out []screenDataNav
	text := string(content)
	for _, occ := range screenAttrOccurrences(text, "data-nav") {
		nav := screenDataNav{line: occ.line}
		if mm := dataNavSpecRe.FindStringSubmatch(occ.value); mm != nil {
			nav.value = strings.ToLower(mm[1])
		}
		if nav.value == "" {
			continue
		}
		if mm := dataNavTriggerRe.FindStringSubmatch(occ.value); mm != nil {
			nav.trigger = strings.TrimSpace(mm[1])
		}
		out = append(out, nav)
	}
	return out
}

// collectUsedStates returns every data-state section marker in document order
// (name + line). data-states (the declaration) is a different attribute name
// and never matches.
func collectUsedStates(content []byte) []screenDataNav {
	var out []screenDataNav
	for _, occ := range screenAttrOccurrences(string(content), "data-state") {
		if strings.TrimSpace(occ.value) == "" {
			continue
		}
		out = append(out, occ)
	}
	return out
}

// RenderScreensIndex renders the derived screens.json bytes. Deterministic:
// the doc's slices are sorted by construction, encoding/json emits struct
// fields in declaration order, and there is no map anywhere in the shape, so
// identical screen bytes render identical bytes on every run and platform.
func RenderScreensIndex(doc *ScreenIndexDoc) ([]byte, error) {
	if doc.SourceHash == "" {
		return nil, fmt.Errorf("screens index doc carries no source-hash")
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("rendering screens index: %w", err)
	}
	return append(out, '\n'), nil
}

// screensIndexInputHash computes the §5f provenance hash over the screen
// bytes in stem order — the screens.json twin of exportTokenInputHash. Each
// document is folded in as "len(bytes)\n bytes" so file partitions cannot
// concatenate to the same byte stream; the fold is byte-identical to the
// token convention, so the same offline recompute recipe applies (concat
// design/screens/*.html sorted by stem, fnv1a64 that).
func screensIndexInputHash(sources []TokenExportSource) string {
	ordered := append([]TokenExportSource(nil), sources...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })
	return TokenExportInputHash(ordered)
}

// ScreensIndexSources reads every design/screens/*.html under root into the
// §5f hash-input shape (workspace-relative slash path, stem, exact bytes),
// sorted by stem. A missing screens directory yields an empty slice and no
// error.
func ScreensIndexSources(root string) ([]TokenExportSource, error) {
	pattern := filepath.Join(root, DirName, "screens", "*.html")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("globbing %s: %w", pattern, err)
	}
	sort.Strings(matches)
	sources := make([]TokenExportSource, 0, len(matches))
	for _, match := range matches {
		data, err := os.ReadFile(match)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", match, err)
		}
		rel, err := filepath.Rel(root, match)
		if err != nil {
			rel = path.Base(match)
		}
		stem := path.Base(filepath.ToSlash(rel))
		sources = append(sources, TokenExportSource{
			Path:    filepath.ToSlash(rel),
			Name:    strings.TrimSuffix(stem, ".html"),
			Content: data,
		})
	}
	return sources, nil
}

// DeriveScreensIndex reads design/screens/*.html under root and derives the
// screens.json document (screen graph + provenance over the input bytes).
// An empty screens tier is not an error: it yields an empty-document index;
// the export tool refuses to write an empty one and ValidateScreensIndex
// treats an absent index as clean for an empty tier, so trees without
// screens (or without the runtime kit at all) validate clean.
func DeriveScreensIndex(root string) (*ScreenIndexDoc, error) {
	sources, err := ScreensIndexSources(root)
	if err != nil {
		return nil, err
	}
	doc := &ScreenIndexDoc{
		Provenance:    screenIndexProvenance,
		SourceHash:    screensIndexInputHash(sources),
		Source:        fmt.Sprintf(screenIndexSourceFmt, TokenExportSourceHashLabel),
		FormatVersion: screenIndexFormatVersion,
		Screens:       []ScreenIndexEntry{},
	}
	frames := manifestFrames(root)
	for _, src := range sources {
		entry := ParseScreenHTML(src.Content, frames)
		entry.Stem = src.Name
		doc.Screens = append(doc.Screens, entry)
	}
	return doc, nil
}

// ReadScreensIndex parses the design/generated/screens.json bytes into a
// ScreenIndexDoc. A file that does not parse yields an error with the JSON
// fault included, so the drift finding can name the real problem (a
// hand-edit that broke the JSON, not just a stale index).
func ReadScreensIndex(data []byte) (*ScreenIndexDoc, error) {
	var doc ScreenIndexDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", ScreensIndexFilename, err)
	}
	return &doc, nil
}

// RenderScreensIndexArtifact derives, renders, and hashes the screens.json
// artifact for the export tool's `screens` target: the deterministic bytes
// plus the content hash the handler reports per file. The RelPath is the
// canonical design/generated/ location; the handler's relocation logic owns
// any out_dir override.
func RenderScreensIndexArtifact(root string) (ExportedArtifact, error) {
	doc, err := DeriveScreensIndex(root)
	if err != nil {
		return ExportedArtifact{}, err
	}
	if len(doc.Screens) == 0 {
		return ExportedArtifact{}, ErrNoScreensForIndex
	}
	content, err := RenderScreensIndex(doc)
	if err != nil {
		return ExportedArtifact{}, err
	}
	return ExportedArtifact{
		Target:  ExportTargetScreens,
		RelPath: filepath.ToSlash(filepath.Join(DirName, GeneratedSubdir, ScreensIndexFilename)),
		Content: content,
		Hash:    tokenExportArtifactHash(content),
	}, nil
}
