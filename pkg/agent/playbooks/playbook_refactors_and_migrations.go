package playbooks

import (
	"regexp"
	"strings"
)

// RefactorSmallPlaybook covers small, mechanical refactors.
type RefactorSmallPlaybook struct{}

func (p RefactorSmallPlaybook) Name() string { return "refactor_small" }
func (p RefactorSmallPlaybook) Matches(userIntent string, _ string) bool {
	lo := strings.ToLower(userIntent)
	phrases := []string{"refactor", "rename", "extract function", "extract method", "inline variable", "simplify", "dead code"}
	for _, ph := range phrases {
		if strings.Contains(lo, ph) {
			return true
		}
	}
	return false
}
func (p RefactorSmallPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Apply small, safe refactors without changing behavior."}
	candidates := make([]string, 0, len(estimatedFiles)+32)
	candidates = append(candidates, estimatedFiles...)

	// Try to target by symbol if mentioned
	if sym := parseSymbolNameFromIntent(userIntent); sym != "" {
		candidates = append(candidates, findFilesWithSymbol(sym)...)
	}
	if len(candidates) == 0 {
		// Fallback: consider larger source files first, where refactors usually help readability
		candidates = append(candidates, listGoFilesBySizeDesc(30)...) // heuristic
	}
	plan.Files = append(plan.Files, dedupeStrings(candidates)...)
	for _, f := range plan.Files {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Perform small mechanical refactor",
			Instructions:       "Apply renames, extractions, or formatting that do not alter behavior. Keep diffs clear and localized.",
			ScopeJustification: "Improves clarity with minimal risk.",
		})
	}
	return plan
}

// APIChangePropagationPlaybook updates call sites after API/signature changes.
type APIChangePropagationPlaybook struct{}

func (p APIChangePropagationPlaybook) Name() string { return "api_change_propagation" }
func (p APIChangePropagationPlaybook) Matches(userIntent string, _ string) bool {
	lo := strings.ToLower(userIntent)
	phrases := []string{"api change", "signature change", "rename function", "rename method", "breaking change", "rename type"}
	for _, ph := range phrases {
		if strings.Contains(lo, ph) {
			return true
		}
	}
	// Detect pattern "rename Old to New" or "Old -> New"
	if regexp.MustCompile(`\brename\s+[A-Za-z0-9_]+\s+to\s+[A-Za-z0-9_]+`).MatchString(lo) ||
		regexp.MustCompile(`\b[A-Za-z0-9_]+\s*->\s*[A-Za-z0-9_]+`).MatchString(lo) {
		return true
	}
	return false
}
func (p APIChangePropagationPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Propagate API/signature changes across call sites with mechanical edits."}

	oldName, newName := parseAPIRenameFromIntent(userIntent)
	candidates := make([]string, 0, len(estimatedFiles)+16)
	candidates = append(candidates, estimatedFiles...)
	if oldName != "" {
		candidates = append(candidates, findFilesWithSymbol(oldName)...)
	}
	plan.Files = append(plan.Files, dedupeStrings(candidates)...)

	for _, f := range plan.Files {
		instr := "Update imports and call sites to match the new API shape. No behavioral changes."
		if oldName != "" && newName != "" {
			instr = "Rename symbol '" + oldName + "' to '" + newName + "' in references and definitions as needed. Ensure consistency and successful build."
		}
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Adjust call sites for API change",
			Instructions:       instr,
			ScopeJustification: "Keeps code compiling after API updates.",
		})
	}
	return plan
}

// TypesMigrationPlaybook migrates to new types/interfaces with mechanical changes.
type TypesMigrationPlaybook struct{}

func (p TypesMigrationPlaybook) Name() string { return "types_migration" }
func (p TypesMigrationPlaybook) Matches(userIntent string, _ string) bool {
	lo := strings.ToLower(userIntent)
	phrases := []string{"migrate type", "replace type", "interface change", "rename struct", "rename type", "rename interface", "field rename"}
	for _, ph := range phrases {
		if strings.Contains(lo, ph) {
			return true
		}
	}
	if regexp.MustCompile(`(?i)rename\s+[A-Za-z0-9_]+\s+to\s+[A-Za-z0-9_]+`).MatchString(lo) ||
		regexp.MustCompile(`\b[A-Za-z0-9_]+\s*->\s*[A-Za-z0-9_]+`).MatchString(lo) {
		return true
	}
	return false
}
func (p TypesMigrationPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Mechanically migrate to new types or interfaces across the codebase."}
	oldType, newType := parseTypeRenameFromIntent(userIntent)
	oldField, newField := parseFieldRenameFromIntent(userIntent)

	candidates := make([]string, 0, len(estimatedFiles)+32)
	candidates = append(candidates, estimatedFiles...)
	if oldType != "" {
		candidates = append(candidates, findFilesWithSymbol(oldType)...)
	}
	if oldField != "" {
		candidates = append(candidates, findFilesWithSymbol(oldField)...)
	}
	plan.Files = append(plan.Files, dedupeStrings(candidates)...)

	for _, f := range plan.Files {
		instr := "Adjust type names/usages and imports. Keep behavior unchanged."
		if oldType != "" && newType != "" {
			instr = "Rename type '" + oldType + "' to '" + newType + "' and update references, constructors, and method receivers if applicable."
		}
		if oldField != "" && newField != "" {
			instr += " Also rename field '" + oldField + "' to '" + newField + "' in struct literals and selectors."
		}
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Update references for new types/interfaces",
			Instructions:       instr,
			ScopeJustification: "Ensures consistency after type or field migration.",
		})
	}
	return plan
}
