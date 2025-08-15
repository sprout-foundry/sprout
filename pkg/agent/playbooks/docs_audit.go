package playbooks

import (
	"path/filepath"
	"strings"
)

// DocsAuditPlaybook plans a documentation vs code audit
type DocsAuditPlaybook struct{}

func (p DocsAuditPlaybook) Name() string { return "docs_audit" }

func (p DocsAuditPlaybook) Matches(userIntent string, category string) bool {
	lo := strings.ToLower(userIntent)
	if category == "docs" {
		return true
	}
	return strings.Contains(lo, "documentation") || strings.Contains(lo, "docs") || strings.Contains(lo, "readme") || strings.Contains(lo, "audit")
}

func (p DocsAuditPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	// High-level: enumerate markdown/docs files, for each propose an edit operation to verify and update
	var docs []string
	for _, f := range estimatedFiles {
		lf := strings.ToLower(f)
		if strings.HasSuffix(lf, ".md") || strings.Contains(filepath.Dir(lf), "docs") {
			docs = append(docs, f)
		}
	}
	// If none estimated, fall back to simple heuristics
	if len(docs) == 0 {
		for _, f := range findDocCandidates(nil) {
			docs = append(docs, f)
		}
	}

	plan := &PlanSpec{}
	plan.Files = append(plan.Files, docs...)
	plan.Scope = "Audit documentation against actual code behavior; update/remove sections as needed."
	for _, d := range docs {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:    d,
			Description: "Verify and update documentation section(s) to match code",
			Instructions: "You must ground every change in repository files only.\n" +
				"Process: (1) Identify doc claims that may be outdated; (2) For each claim, request the minimal set of files via workspace_context/read_file to verify; (3) Produce a short claim→citation map (file:line) before editing; (4) Apply minimal, precise edits.\n" +
				"Rules: Do not use external web sources. Do not invent features. Keep diffs minimal. If evidence is insufficient, request additional files instead of guessing.",
			ScopeJustification: "Keeps documentation aligned with current code behavior.",
		})
	}
	return plan
}

func findDocCandidates(_ interface{}) []string {
	// Minimal heuristic: prefer docs/ and top-level markdown files
	var res []string
	// We don’t have a workspace walk here; rely on EstimatedFiles plus common names
	common := []string{"README.md", "README", "docs/", "docs/index.md"}
	for _, c := range common {
		res = append(res, c)
	}
	return res
}
