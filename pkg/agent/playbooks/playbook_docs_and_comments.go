package playbooks

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// HeaderSummaryPlaybook inserts or updates concise header summaries in source files.
type HeaderSummaryPlaybook struct{}

func (p HeaderSummaryPlaybook) Name() string { return "header_summary" }
func (p HeaderSummaryPlaybook) Matches(userIntent string, category string) bool {
	lo := strings.ToLower(userIntent)
	if category == "docs" {
		return strings.Contains(lo, "header") || strings.Contains(lo, "summary") || strings.Contains(lo, "comment")
	}
	return strings.Contains(lo, "header summary") || strings.Contains(lo, "file header") || strings.Contains(lo, "top comment")
}
func (p HeaderSummaryPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Insert or refresh concise header summaries at the top of relevant files."}
	candidates := make([]string, 0, len(estimatedFiles))
	candidates = append(candidates, estimatedFiles...)
	if len(candidates) == 0 {
		candidates = append(candidates, findFilesNeedingHeaderComments()...)
	}
	plan.Files = append(plan.Files, candidates...)
	for _, f := range candidates {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Add or update a short header summary comment",
			Instructions:       "If the file lacks a header comment, insert one summarizing the file's purpose in 1-2 sentences. If present, refresh for accuracy. Keep it concise.",
			ScopeJustification: "Improves readability with minimal change.",
		})
	}
	return plan
}

// CodeCommentSyncPlaybook focuses on comment/documentation within code.
type CodeCommentSyncPlaybook struct{}

func (p CodeCommentSyncPlaybook) Name() string { return "code_comment_sync" }
func (p CodeCommentSyncPlaybook) Matches(userIntent string, _ string) bool {
	lo := strings.ToLower(userIntent)
	phrases := []string{"comment only", "update comments", "comment clarity", "docstring", "docs in code", "outdated comments", "clarify comments"}
	for _, ph := range phrases {
		if strings.Contains(lo, ph) {
			return true
		}
	}
	return false
}
func (p CodeCommentSyncPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Align code comments with actual behavior; no logic changes."}
	candidates := make([]string, 0, len(estimatedFiles)+50)
	candidates = append(candidates, estimatedFiles...)
	if len(candidates) == 0 {
		candidates = append(candidates, findCommentHeavyFiles(40)...)
	}
	plan.Files = append(plan.Files, dedupeStrings(candidates)...)
	for _, f := range plan.Files {
		plan.Ops = append(plan.Ops, PlanOp{
			FilePath:           f,
			Description:        "Update comments for accuracy",
			Instructions:       "Review code and ensure comments accurately reflect behavior and constraints. Do not change logic. Prefer concise, high-signal comments.",
			ScopeJustification: "Improves clarity with zero behavior change.",
		})
	}
	return plan
}

// findFilesNeedingHeaderComments scans for Go source files that do not start with a comment block.
func findFilesNeedingHeaderComments() []string {
	var results []string
	_ = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "assets" || name == "debug" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if startsWithGoComment(path) {
			return nil
		}
		results = append(results, filepath.ToSlash(path))
		return nil
	})
	return dedupeStrings(results)
}

func findCommentHeavyFiles(limit int) []string {
	type entry struct {
		p     string
		ratio float64
	}
	var arr []entry
	_ = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "assets" || name == "debug" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		lines := strings.Split(string(b), "\n")
		if len(lines) == 0 {
			return nil
		}
		comment := 0
		code := 0
		for _, ln := range lines {
			t := strings.TrimSpace(ln)
			if t == "" {
				continue
			}
			if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "* ") {
				comment++
			} else {
				code++
			}
		}
		total := comment + code
		if total == 0 {
			return nil
		}
		ratio := float64(comment) / float64(total)
		if ratio >= 0.20 {
			arr = append(arr, entry{p: filepath.ToSlash(path), ratio: ratio})
		}
		return nil
	})
	sort.Slice(arr, func(i, j int) bool { return arr[i].ratio > arr[j].ratio })
	if len(arr) > limit {
		arr = arr[:limit]
	}
	res := make([]string, 0, len(arr))
	for _, e := range arr {
		res = append(res, e.p)
	}
	return res
}
