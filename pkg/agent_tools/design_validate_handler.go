package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/design"
)

// designValidateHandler implements ToolHandler for the design_validate tool
// (SP-140-1 §1g). It validates the design/ tree (tokens, wireframes, icons,
// flows, screens, README manifest, brand, feedback) and returns structured
// findings {file, line?, severity, message, rule}. Pure Go with no browser or
// vision dependencies, so it compiles on WASM and native builds alike and
// lives in the shared AllTools list rather than a build-tagged registration.
type designValidateHandler struct{}

func (h *designValidateHandler) Name() string {
	return "design_validate"
}

func (h *designValidateHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "design_validate",
		Description: "Validate the design/ workspace tree against the sprout design " +
			"conventions (SP-140-1): DTCG tokens, SVG wireframes, icon SVGs, mermaid flows, " +
			"self-contained screen HTML, the README manifest, brand.md, feedback JSON, and the " +
			"git contract (.gitattributes SVG diff rule, .gitignore design/.cache/ policy, " +
			"oversized embedded data URIs). " +
			"Returns structured findings {file, line, severity, message, rule}. Findings are " +
			"advisory — they never block the turn — so run this after creating or editing " +
			"design assets and fix the error-severity findings before declaring a design done. " +
			"Severity `fix` findings are machine-applicable: append the exact line they name " +
			"(never replace existing rules).",
		Parameters: []ParameterDef{
			{
				Name:        "path",
				Type:        "string",
				Required:    false,
				Description: "Optional path to a single design asset (e.g. `design/wireframes/login.svg`) or a repository-level git-contract file (`.gitattributes`, `.gitignore`), relative to the workspace root. Omit to validate the whole `design/` tree.",
			},
		},
		Required: nil,
	}
}

func (h *designValidateHandler) Validate(args map[string]any) error {
	if p, exists := lookupKey(args, "path"); exists && p != nil {
		if _, ok := p.(string); !ok {
			return fmt.Errorf("parameter 'path' must be a string, got %T", p)
		}
	}
	return nil
}

func (h *designValidateHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	root := strings.TrimSpace(env.WorkspaceRoot)
	if root == "" {
		root = "."
	}

	var (
		findings []design.Finding
		err      error
	)

	if rawPath, exists := lookupKey(args, "path"); exists && rawPath != nil {
		p, ok := rawPath.(string)
		if !ok {
			p = fmt.Sprintf("%v", rawPath)
		}
		p = strings.TrimSpace(p)
		if p == "" {
			// An empty path means "whole tree", matching the no-args call.
			findings, err = design.ValidateTree(root)
		} else {
			// Gate-1 precheck on the requested asset before touching it.
			preRes, decision := PrecheckFileAccess(ctx, env.FileAccessClassifier, "design_validate", p)
			if decision == "deny" {
				msg := fmt.Sprintf("design_validate blocked: %s is denied by the active file-access policy", p)
				return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_validate blocked: %s is declared denied", p)
			}
			if decision == "allow" && preRes != "" {
				// preRes is the symlink-resolved canonical path; convert it
				// back to a workspace-relative slash path when it sits under
				// the workspace root, which is the form ValidateFile expects.
				if rel, relErr := filepath.Rel(root, preRes); relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					p = filepath.ToSlash(rel)
				}
			}
			findings, err = design.ValidateFile(root, p)
		}
	} else {
		findings, err = design.ValidateTree(root)
	}

	if err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("design_validate failed: %v", err),
			IsError: true,
		}, fmt.Errorf("design_validate: %w", err)
	}

	if findings == nil {
		findings = []design.Finding{}
	}

	structured := buildFindingsOutput(findings)
	return ToolResult{
		Output:        renderFindingsSummary(root, structured),
		StructuredOut: structured,
		IsError:       false,
	}, nil
}

// findingOut is the JSON-friendly shape of one validator finding
// ({file, line?, severity, message, rule}, SP-140-1 §1g). Line is omitted
// when the validator could not derive one (0).
type findingOut struct {
	File     string `json:"file"`
	Line     int    `json:"line,omitempty"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Rule     string `json:"rule"`
}

// findingsOutput is the structured result of one design_validate run: the
// per-finding rows, the total count, and per-severity tallies.
type findingsOutput struct {
	Findings   []findingOut   `json:"findings"`
	Count      int            `json:"count"`
	BySeverity map[string]int `json:"bySeverity"`
}

// buildFindingsOutput converts validator findings into the structured output
// shape. bySeverity always carries every severity key so consumers can read a
// zero without a presence check.
func buildFindingsOutput(findings []design.Finding) findingsOutput {
	out := findingsOutput{
		Findings:   make([]findingOut, 0, len(findings)),
		BySeverity: map[string]int{"error": 0, "warn": 0, "info": 0, "fix": 0},
	}
	for _, f := range findings {
		out.Findings = append(out.Findings, findingOut{
			File:     f.File,
			Line:     f.Line,
			Severity: f.Severity.String(),
			Message:  f.Message,
			Rule:     f.Rule,
		})
		out.Count++
		out.BySeverity[f.Severity.String()]++
	}
	return out
}

// renderFindingsSummary builds the human-readable one-liner. Findings are
// advisory: a run full of error-severity findings still reports success —
// only tool/I-O failure (handled above) sets IsError.
func renderFindingsSummary(root string, out findingsOutput) string {
	if out.Count == 0 {
		// Distinguish "clean tree" from "no design/ at all" so the agent
		// knows to scaffold first.
		if !design.FileExists(root) {
			return "design_validate: No design/ directory found — nothing to validate. " +
				"Use the design-system skill to scaffold one."
		}
		return "design_validate: 0 findings — the design/ tree satisfies the conventions."
	}
	var parts []string
	for _, sev := range []string{"error", "warn", "info", "fix"} {
		if n := out.BySeverity[sev]; n > 0 {
			if sev == "fix" {
				parts = append(parts, fmt.Sprintf("%d fix(es) to apply", n))
				continue
			}
			parts = append(parts, fmt.Sprintf("%d %s(s)", n, sev))
		}
	}
	return fmt.Sprintf("design_validate: %d finding(s) — %s", out.Count, strings.Join(parts, ", "))
}

func (h *designValidateHandler) Aliases() []string      { return nil }
func (h *designValidateHandler) Timeout() time.Duration { return 60 * time.Second }
func (h *designValidateHandler) MaxResultSize() int     { return 0 }
func (h *designValidateHandler) SafeForParallel() bool  { return true }
func (h *designValidateHandler) Interactive() bool      { return false }
