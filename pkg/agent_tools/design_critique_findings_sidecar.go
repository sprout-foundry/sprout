//go:build !js

package tools

// ---------------------------------------------------------------------------
// design_critique findings sidecar (SP-140-6 §6d)
//
// §4a's critique findings are tool output and evaporate with the turn; only
// the PNG and the §4e cache-key sidecar persist under design/.cache/renders/.
// This module adds a third derived artifact: <artifact>.findings.json, the
// structured findings of the last critique of that target, so the DesignView
// detail pane (SP-140-6 §6g) can show "last critique" without re-running a
// vision call.
//
// Invariant-2 discipline: the sidecar is DERIVED OUTPUT — regenerable by
// re-running the tool, carrying its own provenance (sourceHash + generated,
// the same convention as the §4e cache sidecar), living under design/.cache/
// (already gitignore-guided by SP-140-1 §1h), and invisible to design_validate
// (findings for findings would be noise; the validator's asset classifier
// ignores .cache/ contents). Writes are best-effort exactly like the §4e
// sidecar: a failure never fails the critique, whose pixels and tool output
// already succeeded.
// ---------------------------------------------------------------------------

import (
	"context"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/design"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// critiqueFindingsSidecarSuffix closes every sidecar filename.
const critiqueFindingsSidecarSuffix = ".findings.json" // CritiqueFindingsSidecarPath is the exported sidecar path for one critique
// target: design/.cache/renders/findings/<label-slug>.findings.json. The
// webui's detail pane (SP-140-6 §6g) computes the same path for an asset, so
// the naming rule lives in exactly one place.
func CritiqueFindingsSidecarPath(targetLabel string) string {
	slug := strings.ReplaceAll(targetLabel, "/", "-")
	return path.Join(design.DirName, designArtifactDirName, designRenderArtifactDirName,
		"findings", slug+critiqueFindingsSidecarSuffix)
}

// critiqueFindingsSidecarPath is the handler-internal form taking a target.
func critiqueFindingsSidecarPath(t critiqueTarget) string {
	label := t.Label
	if label == "" {
		label = t.Source
	}
	return CritiqueFindingsSidecarPath(label)
}

// critiqueFindingsSidecarDoc is the persisted shape of one target's last
// critique. Severity vocabulary is the tool's own (blocker/major/minor/info);
// a non-vision run records visual=false with its static findings (§4a).
type critiqueFindingsSidecarDoc struct {
	// Target is the workspace-relative path the findings are about (the
	// critique target's label).
	Target string `json:"target"`
	// Rubric is the rubric the run judged against.
	Rubric string `json:"rubric"`
	// SourceHash is the §4e content hash of the render source at sidecar
	// time, so a consumer can tell the findings from a changed screen.
	SourceHash string `json:"sourceHash,omitempty"`
	// Generated is when this critique ran (RFC3339 UTC).
	Generated string `json:"generated"`
	// Visual is the §4a marker: a vision pass produced the findings.
	Visual bool `json:"visual"`
	// Findings are the critique findings for this target (its own only — a
	// whole-tree run writes one sidecar per critiqued target).
	Findings []critiqueFinding `json:"findings"`
}

// writeCritiqueFindingsSidecars persists one sidecar per non-compare artifact
// of the run. artifactTargets is the per-artifact target record the Execute
// loop kept in artifact order. Best-effort: any failure skips that sidecar
// silently — the critique's own result is unaffected.
func writeCritiqueFindingsSidecars(ctx context.Context, env ToolEnv, out *critiqueOutput, artifactTargets []critiqueTarget) {
	if out == nil || len(out.Artifacts) == 0 {
		return
	}
	n := len(out.Artifacts)
	if len(artifactTargets) < n {
		n = len(artifactTargets)
	}
	for i := 0; i < n; i++ {
		t := artifactTargets[i]
		// A comparison render is a delta input, not a critique of its own —
		// it gets no sidecar (the same rule its render follows).
		if t.Stage == "compare" {
			continue
		}
		if err := writeCritiqueFindingsSidecar(ctx, env, out, t); err != nil {
			continue
		}
	}
}

// writeCritiqueFindingsSidecar persists one target's findings beside the
// renders, keyed by target label.
func writeCritiqueFindingsSidecar(ctx context.Context, env ToolEnv, out *critiqueOutput, t critiqueTarget) error {
	findings := critiqueFindingsForTarget(out.Findings, t)
	sourceHash := critiqueSidecarSourceHash(ctx, t)

	doc := critiqueFindingsSidecarDoc{
		Target:     t.Label,
		Rubric:     out.Rubric,
		SourceHash: sourceHash,
		Generated:  time.Now().UTC().Format(time.RFC3339),
		Visual:     out.Visual,
		Findings:   findings,
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}

	abs, err := filesystem.SafeResolvePathForWriteWithBypass(ctx, critiqueFindingsSidecarPath(t))
	if err != nil {
		return err
	}
	if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
		return mkErr
	}
	return os.WriteFile(abs, append(data, '\n'), 0o644)
}

// critiqueFindingsForTarget filters the run's findings to one target's own.
// Both finding paths stamp Target with the workspace-relative label (the
// vision tier's per-observation target and the static rule pack's file), so a
// label match is the whole rule.
func critiqueFindingsForTarget(findings []critiqueFinding, t critiqueTarget) []critiqueFinding {
	own := []critiqueFinding{}
	for _, f := range findings {
		if f.Target == t.Label {
			own = append(own, f)
		}
	}
	return own
}

// critiqueSidecarSourceHash recomputes the §4e content hash of the target's
// render source at sidecar time (best-effort: "" when unreadable — the
// provenance stays honest by omitting the field rather than guessing).
func critiqueSidecarSourceHash(ctx context.Context, t critiqueTarget) string {
	source, err := readCritiqueSourceBytes(ctx, t)
	if err != nil {
		return ""
	}
	return critiqueContentHash(source)
}
