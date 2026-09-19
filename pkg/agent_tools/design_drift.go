package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/design"
)

// designDriftReport computes the SP-140-5 §5c drift-direction report for a
// workspace and resolves the two direction rows for both reporting surfaces
// (design_assets and design_validate).
//
// §5c reports two states separately, with distinct remedies:
//
//   - design-ahead (design/ changed, generated/code behind) — the expected
//     healthy state of an active project; remedy: regenerate the theme
//     (design_export_tokens), and the count of screens ahead of the build.
//   - code-ahead (implementation changed, semantic layer behind) — the state
//     design_sync exists to fix; remedy: run design_sync to import the
//     deltas.
//
// The pure semantics live in pkg/design (AnalyzeDrift); this helper is the
// ToolEnv-facing half: it supplies the touched-code-file context the code-ahead
// signal needs (through the sanctioned env.ResolveToolFuncs().ListChanges seam — the
// same seam design_sync uses; design_validate and design_assets are plain Go tools, so
// this file is untagged and WASM-safe).
//
// Drift is signal, never a failure: a workspace with no design/ tree, an
// unavailable change tracker, or an unreadable file all degrade to a synced
// report rather than an error.
func designDriftReport(ctx context.Context, env ToolEnv, root string) *design.DriftReport {
	if !design.FileExists(root) {
		// No tree: drift is not meaningful yet. Report both rows synced so the
		// shape stays stable (the design_assets handler's exists:false path is
		// the scaffold signal).
		return &design.DriftReport{Rows: []design.DriftDirectionRow{
			{Direction: design.DriftRowDesignAhead, Synced: true, Advisory: design.DriftSeverityInfo},
			{Direction: design.DriftRowCodeAhead, Synced: true, Advisory: design.DriftSeverityInfo},
		}, Synced: true}
	}
	return design.AnalyzeDrift(root, designDriftTouched(ctx, env, root))
}

// designDriftTouched resolves the touched UI code files for the code-ahead
// signal. It reads the turn's ChangeTracker set through the sanctioned
// ListChanges seam (string output, parsed). The result is limited to code
// files — design/ paths are the semantic layer, not "the implementation" — so a
// design-only edit session cannot masquerade as code-ahead.
//
// An unavailable seam (nil function, an error, a non-JSON payload) yields no
// paths: code-ahead is then reported as not assessable, which is the honest
// answer, not a failure.
func designDriftTouched(ctx context.Context, env ToolEnv, root string) []design.SyncFileInput {
	fn := env.ResolveToolFuncs().ListChanges
	if fn == nil {
		return nil
	}
	raw, err := fn(ctx, map[string]any{})
	if err != nil {
		return nil
	}
	paths := parseListChangesPaths(raw)
	if len(paths) == 0 {
		return nil
	}

	inputs := make([]design.SyncFileInput, 0, len(paths))
	for _, p := range paths {
		if isDesignLayerPath(p) {
			continue
		}
		abs := p
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, filepath.FromSlash(p))
		}
		rel, relErr := filepath.Rel(root, abs)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		info, statErr := os.Stat(abs)
		if statErr != nil || info.IsDir() {
			continue
		}
		data, readErr := os.ReadFile(abs)
		if readErr != nil {
			continue
		}
		inputs = append(inputs, design.SyncFileInput{Path: filepath.ToSlash(rel), Content: data})
	}
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].Path < inputs[j].Path })
	return inputs
}

// isDesignLayerPath reports whether a workspace-relative path names the design/
// semantic layer (or one of its git-contract files), which is never "the
// implementation" for the code-ahead signal.
func isDesignLayerPath(p string) bool {
	clean := strings.TrimSuffix(filepath.ToSlash(strings.TrimSpace(p)), "/")
	return clean == design.DirName || strings.HasPrefix(clean, design.DirName+"/")
}

// designDriftRowOut is the section-5c drift row in both tools' structured
// output. It mirrors design.DriftDirectionRow but is a separate type so the
// tool layer owns its JSON contract (and so a row is always present, even when
// the underlying signal could not be computed).
type designDriftRowOut struct {
	// Direction is design-ahead | code-ahead.
	Direction string `json:"direction"`
	// Ahead is true when this direction actually holds.
	Ahead bool `json:"ahead"`
	// Synced is the explicit "nothing to do here" flag (the complement of Ahead).
	Synced bool `json:"synced"`
	// Count is the magnitude: screens ahead of the build (design-ahead), or
	// semantic deltas to import (code-ahead).
	Count int `json:"count"`
	// Summary / Remedy / NextStep are the §5c one-liner, the remedy, and the
	// tool it names. Empty when Synced.
	Summary  string `json:"summary,omitempty"`
	Remedy   string `json:"remedy,omitempty"`
	NextStep string `json:"nextStep,omitempty"`
	// Advisory is the severity class: info (nothing to do) or warn (remedy to
	// run). Never error — drift is signal (§5c).
	Advisory string `json:"advisory"`
}

// designDriftOut is the §5c drift section of a tool result: the two direction
// rows (always both, in design-ahead → code-ahead order), the synced flag, and
// the evidence lines explaining how each count was derived.
type designDriftOut struct {
	Rows             []designDriftRowOut `json:"directions"`
	DesignAheadCount int                 `json:"designAheadCount"`
	CodeAheadCount   int                 `json:"codeAheadCount"`
	Synced           bool                `json:"synced"`
	Evidence         []string            `json:"evidence,omitempty"`
}

// buildDesignDriftOut converts the pure drift report into the tool-layer
// structured section. Both direction rows are always emitted, in canonical
// order, so design_assets and design_validate report the same vocabulary. A
// nil report (e.g. a workspace with no design/ tree) yields both rows synced,
// keeping the two-row shape stable for every consumer.
func buildDesignDriftOut(report *design.DriftReport) designDriftOut {
	out := designDriftOut{Rows: []designDriftRowOut{}}
	for _, direction := range design.DriftDirections() {
		row := design.DriftDirectionRowFor(report, direction)
		out.Rows = append(out.Rows, designDriftRowOut{
			Direction: row.Direction,
			Ahead:     row.Ahead,
			Synced:    !row.Ahead,
			Count:     row.Count,
			Summary:   row.Summary,
			Remedy:    row.Remedy,
			NextStep:  row.NextStep,
			Advisory:  row.Advisory,
		})
	}
	if report == nil {
		out.Synced = true
		return out
	}
	out.DesignAheadCount = report.DesignAheadCount
	out.CodeAheadCount = report.CodeAheadCount
	out.Synced = report.Synced
	out.Evidence = report.Evidence
	return out
}

// designDriftRowsByName indexes the section rows by direction for callers that
// read a single direction (the design_validate finding emitter).
func designDriftRowsByName(rows []designDriftRowOut) map[string]designDriftRowOut {
	byName := make(map[string]designDriftRowOut, len(rows))
	for _, r := range rows {
		byName[r.Direction] = r
	}
	return byName
}

// marshalDesignDrift is a small helper so tests can render the drift section as
// JSON without re-implementing the shape.
func marshalDesignDrift(out designDriftOut) string {
	data, err := json.Marshal(out)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// listChangesEnvelope is the subset of the list_changes JSON output the design
// tools read: the per-file rows' `path` field, plus a bulk row's nested
// `bulk_items[].path` list (a shell command that touched many files is one
// `bulk` row carrying its members). The full envelope is wider (diffs,
// timestamps); only the paths matter here, so the parse is deliberately
// tolerant of the rest.
//
// It lives in this untagged file because both the native-only design_sync tool
// and the WASM-roster design_validate tool read the ChangeTracker through this
// seam.
type listChangesEnvelope struct {
	Files []struct {
		Path      string `json:"path"`
		Op        string `json:"op"`
		BulkItems []struct {
			Path string `json:"path"`
		} `json:"bulk_items"`
	} `json:"files"`
}

// parseListChangesPaths extracts the changed paths from a list_changes JSON
// string (§5b: the sanctioned seam is "string output, parsed"). It is
// deliberately tolerant: a non-JSON or unexpected payload yields no paths
// rather than an error, because analyze mode degrades to "nothing to analyse"
// better than it fails a turn. Paths are deduped and sorted.
func parseListChangesPaths(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var env listChangesEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		// Tolerant by design: a malformed payload yields no paths (an empty
		// touched set), because analyze mode degrades to "nothing to analyse"
		// rather than failing a dev turn.
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(env.Files))
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, f := range env.Files {
		// A bulk entry (a shell command that touched many files) lists its
		// members in nested bulk_items; those are the real paths, and the
		// row's own `path` is empty for a bulk row.
		for _, b := range f.BulkItems {
			add(b.Path)
		}
		if len(f.BulkItems) > 0 {
			continue
		}
		add(f.Path)
	}
	sort.Strings(out)
	return out
}
