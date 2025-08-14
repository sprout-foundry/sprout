package playbooks

import (
	"path/filepath"
	"regexp"
	"strings"
)

// SimpleReplacePlaybook handles prompts like: change 'old' to 'new'.
type SimpleReplacePlaybook struct{}

func (p SimpleReplacePlaybook) Name() string { return "simple_replace" }
func (p SimpleReplacePlaybook) Matches(userIntent string, _ string) bool {
	lo := strings.ToLower(userIntent)
	return strings.Contains(lo, "change '") || strings.Contains(lo, "replace '") || strings.Contains(lo, "s/")
}
func (p SimpleReplacePlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Apply deterministic small textual replacements across target files."}
	plan.Files = append(plan.Files, estimatedFiles...)
	for _, f := range estimatedFiles {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Apply requested small textual replacement",
			Instructions:       "Perform the exact replacement requested, ensuring only intended occurrences are changed. Keep diffs minimal.",
			ScopeJustification: "Small deterministic edit.",
		})
	}
	return plan
}

// FileRenamePlaybook handles file moves/renames with import/call-site updates.
type FileRenamePlaybook struct{}

func (p FileRenamePlaybook) Name() string { return "file_rename" }
func (p FileRenamePlaybook) Matches(userIntent string, _ string) bool {
	lo := strings.ToLower(userIntent)
	if strings.Contains(lo, "rename file") || strings.Contains(lo, "move file") || strings.Contains(lo, "relocate") || strings.Contains(lo, "mv ") {
		return true
	}
	// Generic rename with path-like tokens
	if regexp.MustCompile(`(?i)rename\s+[A-Za-z0-9_./\-]+\s+(to|->)\s+[A-Za-z0-9_./\-]+`).MatchString(lo) {
		return true
	}
	return false
}
func (p FileRenamePlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Perform file rename/move and adjust minimal references accordingly."}
	from, to := parseFileRenameFromIntent(userIntent)
	candidates := make([]string, 0, len(estimatedFiles)+16)
	candidates = append(candidates, estimatedFiles...)
	if from != "" {
		candidates = append(candidates, from)
		// Find files that mention the old file path or base name
		base := filepath.Base(from)
		mentionRefs := findFilesMentioningString(base, 50)
		candidates = append(candidates, mentionRefs...)
	}
	plan.Files = append(plan.Files, dedupeStrings(candidates)...)
	for _, f := range plan.Files {
		instr := "Move/rename file as requested and update imports/references. Keep changes minimal and compiling."
		if f == from && to != "" {
			instr = "Rename/move this file to '" + to + "' and update any build tags or package references as needed."
		}
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Rename or move file and update references",
			Instructions:       instr,
			ScopeJustification: "Maintains integrity after file relocation.",
		})
	}
	return plan
}

// MultiFileEditPlaybook coordinates multiple related edits.
type MultiFileEditPlaybook struct{}

func (p MultiFileEditPlaybook) Name() string { return "multi_file_edit" }
func (p MultiFileEditPlaybook) Matches(userIntent string, _ string) bool {
	lo := strings.ToLower(userIntent)
	if strings.Contains(lo, "multiple files") || strings.Contains(lo, "several files") || strings.Contains(lo, "across files") {
		return true
	}
	// If the intent includes more than one file path, treat as multi-file
	fps := extractFilePathsFromIntent(userIntent, 3)
	return len(fps) >= 2
}
func (p MultiFileEditPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Apply coordinated edits across related files while keeping each change minimal."}
	candidates := make([]string, 0, len(estimatedFiles)+32)
	candidates = append(candidates, estimatedFiles...)
	// Add files explicitly mentioned in the intent
	explicit := extractFilePathsFromIntent(userIntent, 20)
	candidates = append(candidates, explicit...)
	// If still narrow, add symbol-targeted files
	if len(candidates) < 2 {
		if sym := parseSymbolNameFromIntent(userIntent); sym != "" {
			candidates = append(candidates, findFilesWithSymbol(sym)...)
		}
	}
	plan.Files = append(plan.Files, dedupeStrings(candidates)...)
	for _, f := range plan.Files {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Apply coordinated change",
			Instructions:       "Implement the requested change in this file, ensuring consistency with other touched files.",
			ScopeJustification: "Keeps cross-file changes aligned.",
		})
	}
	return plan
}
