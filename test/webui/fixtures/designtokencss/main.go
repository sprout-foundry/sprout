// Command designtokencss regenerates design/generated/tokens.css for a
// workspace root — exactly the design_export_tokens css target
// (design.ResolveExportTokens + RenderArtifacts + WriteExportedArtifactsAt).
//
// SP-143 143.7's e2e spec uses it for the token-edit restyle scenario: the
// webui exposes no export endpoint (read-only /api/design/status plus the
// /api/file read/write proxy) and design_export_tokens is an agent-tool
// surface, so the spec drives the same pkg/design pipeline through this
// helper instead of re-implementing the renderer in TypeScript.
//
// Usage: go run ./test/webui/fixtures/designtokencss <workspaceRoot>
package main

import (
	"fmt"
	"os"

	"github.com/sprout-foundry/sprout/pkg/design"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: designtokencss <workspaceRoot>")
		os.Exit(2)
	}
	root := os.Args[1]

	tokens, err := design.ResolveExportTokens(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "designtokencss: %v\n", err)
		os.Exit(1)
	}
	targets, err := design.ResolveExportTargets(design.ExportTargetCSS)
	if err != nil {
		fmt.Fprintf(os.Stderr, "designtokencss: %v\n", err)
		os.Exit(1)
	}
	artifacts, err := design.RenderArtifacts(tokens, targets)
	if err != nil {
		fmt.Fprintf(os.Stderr, "designtokencss: %v\n", err)
		os.Exit(1)
	}
	if err := design.WriteExportedArtifactsAt(root, artifacts); err != nil {
		fmt.Fprintf(os.Stderr, "designtokencss: %v\n", err)
		os.Exit(1)
	}
	for _, a := range artifacts {
		fmt.Printf("%s %s %s\n", a.Target, a.RelPath, a.Hash)
	}
}
