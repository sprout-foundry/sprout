//go:build !js

package tools

// The design_export_tokens `flows` target (SP-140-9 §9b, §9c): regenerate
// every derived flow export — design/flows/<name>.mmd — from its flow source
// .json plus the touched screens. It lives beside the handler's screens branch
// (which it mirrors) rather than in design_export_handler.go: that file is at
// the AGENTS.md line ceiling, and this branch owns its own Gate-1 prechecks,
// refusal text, and summary.
//
// The exports are written at their canonical design/flows/ paths, NOT through
// relocateArtifacts: the drift validator keys on the .mmd sitting beside its
// .json source, so an out_dir override has no meaning here and is refused.

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"

	"github.com/sprout-foundry/sprout/pkg/design"
)

// defaultExportOutDir is the export output directory the screens branch and
// this branch share (resolveExportOutDir's default).
func defaultExportOutDir() string {
	return path.Join(design.DirName, design.GeneratedSubdir)
}

// exportFlowsTarget runs the explicit-only flows target: render every derived
// export, precheck it, write it at its canonical path, and report the files.
// outDir is the caller's resolved output directory; anything but the default
// is refused because the .mmd must stay beside its .json source.
func exportFlowsTarget(ctx context.Context, env ToolEnv, root, outDir string) (ToolResult, error) {
	if outDir != defaultExportOutDir() {
		msg := "design_export_tokens: the flows target writes design/flows/<name>.mmd beside its .json source (the drift check keys on that location) — it takes no out_dir override."
		return ToolResult{Output: msg, IsError: true}, errors.New(msg)
	}

	artifacts, err := design.RenderAllFlowMDMArtifacts(root)
	if err != nil {
		if errors.Is(err, design.ErrNoFlowSources) {
			msg := "design_export_tokens: no flow sources found under design/flows/ — author design/flows/<name>.json first (SP-140-9 §9b), then regenerate the exports."
			return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_export_tokens: %w", err)
		}
		// Anything else — a malformed flow source most likely — refuses the
		// whole export: a partial regeneration would leave some exports stale
		// at their provenance hashes while looking current. The message names
		// the offending document.
		msg := "design_export_tokens refused: " + err.Error() +
			"; fix the flow source, then regenerate with targets:flows."
		return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_export_tokens: %w", err)
	}

	exported := make([]design.ExportedArtifact, 0, len(artifacts))
	for _, a := range artifacts {
		if _, decision := PrecheckFileAccess(ctx, env.FileAccessClassifier, "design_export_tokens", a.RelPath); decision == "deny" {
			msg := fmt.Sprintf("design_export_tokens blocked: %s is denied by the active file-access policy", a.RelPath)
			return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_export_tokens blocked: %s is declared denied", a.RelPath)
		}
		exported = append(exported, design.ExportedArtifact{
			Target:  design.ExportTargetFlows,
			RelPath: a.RelPath,
			Content: a.Content,
			Hash:    a.Hash,
		})
	}

	if err := design.WriteExportedArtifactsAt(root, exported); err != nil {
		msg := fmt.Sprintf("design_export_tokens failed: %v", err)
		return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_export_tokens: %w", err)
	}

	out := designExportOutput{
		Exists:      true,
		TokensPath:  filepath.ToSlash(filepath.Join(design.DirName, design.TokenSubdir)),
		OutDir:      outDir,
		TargetCount: len(exported),
		Files:       make([]designExportFile, 0, len(exported)),
	}
	for _, a := range exported {
		out.Files = append(out.Files, designExportFile{
			Target:      a.Target,
			Path:        a.RelPath,
			Bytes:       len(a.Content),
			ContentHash: a.Hash,
		})
	}
	return ToolResult{Output: renderDesignExportSummary(out), StructuredOut: out, IsError: false}, nil
}
