package git

// Budgeted diff presentation for commit message generation, adapted from
// gmitllm's PrepareDiff pipeline.
//
// DiffOptimizer replaces individual large/derived files with summaries.
// This layer wraps the whole presentation in a hard byte budget: semantic
// (source) diffs come first and get most of the budget, non-semantic files
// are presented as one-line summaries, and semantic files that no longer
// fit are truncated at a newline boundary with an explicit marker so the
// model knows exactly what it did not see.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/utils"
)

const (
	// maxCommitDiffBytes is the default total byte budget for diff content
	// sent to the model during commit message generation. 120KB of diff is
	// roughly 30–40K tokens — safely inside every supported model's window
	// and far more than a commit message needs.
	maxCommitDiffBytes = 120000

	// semanticBudgetShare is the fraction of the total budget reserved for
	// full semantic (source) diffs; the remainder absorbs summaries and
	// truncation slack.
	semanticBudgetShare = 0.8

	// minTruncateBytes is the smallest amount of a semantic diff worth
	// showing when the budget is nearly exhausted; below this the file
	// collapses to a summary line.
	minTruncateBytes = 200
)

// prepareBudgetedDiff assembles the diff presentation for a commit prompt:
//
//  1. Split the raw diff into per-file sections.
//  2. Classify each file with the shared ReviewFileClass classifier
//     (lockfile / generated / vendored / binary → summary only).
//  3. Emit semantic diffs first with context headers, enforcing the total
//     byte budget; a file that no longer fits is truncated at a newline
//     with an explicit "[... truncated ...]" marker.
//  4. Append one-line summaries for every non-semantic file, plus any
//     semantic file that could not fit at all.
//
// It also returns warnings (currently: staged binaries) for the caller to
// surface, mirroring what DiffOptimizer.OptimizeDiff used to provide.
// The result is deterministic for a given input.
func prepareBudgetedDiff(fileChanges []CommitFileChange, rawDiff string, maxBytes int) (string, []string) {
	if maxBytes <= 0 {
		maxBytes = maxCommitDiffBytes
	}
	rawDiff = strings.TrimSpace(rawDiff)
	if rawDiff == "" {
		return "", nil
	}

	type entry struct {
		status string
		path   string
		class  utils.ReviewFileClass
		diff   string
	}

	sections := splitDiffSections(rawDiff)

	// Walk files in the caller's order first (git's own ordering), then any
	// sectioned file the caller didn't list, so nothing in the diff is lost.
	paths := make([]string, 0, len(sections))
	seen := make(map[string]bool, len(fileChanges))
	for _, fc := range fileChanges {
		if _, ok := sections[fc.Path]; ok && !seen[fc.Path] {
			seen[fc.Path] = true
			paths = append(paths, fc.Path)
		}
	}
	extra := make([]string, 0, len(sections))
	for path := range sections {
		if !seen[path] {
			extra = append(extra, path)
		}
	}
	sort.Strings(extra)
	paths = append(paths, extra...)

	statusOf := func(path string) string {
		for _, fc := range fileChanges {
			if fc.Path == path {
				return fc.Status
			}
		}
		return "M"
	}

	entries := make([]entry, 0, len(paths))
	for _, p := range paths {
		entries = append(entries, entry{
			status: statusOf(p),
			path:   p,
			class:  utils.ClassifyReviewFile(p),
			diff:   sections[p],
		})
	}

	// Semantic files first (stable), so they get budget priority.
	sort.SliceStable(entries, func(i, j int) bool {
		return isSemanticClass(entries[i].class) && !isSemanticClass(entries[j].class)
	})

	var warnings []string
	semanticBudget := int(float64(maxBytes) * semanticBudgetShare)
	var full []string
	var summaries []string
	used := 0

	for _, e := range entries {
		if e.class.IsBinary {
			warnings = append(warnings,
				fmt.Sprintf("Binary file staged: %s. Check in binaries only when necessary.", e.path))
		}
		if isSemanticClass(e.class) {
			additions, deletions := countDiffStats(e.diff)
			header := diffContextHeader(e.path, e.status, e.class, additions, deletions)
			if used+len(e.diff) <= semanticBudget {
				used += len(e.diff)
				full = append(full, header+"\n"+e.diff)
				continue
			}
			// No longer fits: truncate what remains of the budget into this
			// file, then collapse it (and everything after) to summaries.
			if remaining := semanticBudget - used; remaining >= minTruncateBytes {
				truncated := truncateDiffSection(e.diff, remaining)
				used += len(truncated)
				full = append(full, header+"\n"+truncated)
			}
			summaries = append(summaries,
				fmt.Sprintf("[Truncated: %s — total %d bytes]", e.path, len(e.diff)))
			continue
		}
		summaries = append(summaries, diffFileSummary(e.class, e.status, e.path, e.diff))
	}

	var out strings.Builder
	for _, d := range full {
		if out.Len() > 0 {
			out.WriteString("\n\n")
		}
		out.WriteString(d)
	}
	if len(summaries) > 0 {
		if out.Len() > 0 {
			out.WriteString("\n\n")
		}
		for _, s := range summaries {
			out.WriteString(s)
			out.WriteString("\n")
		}
	}
	return strings.TrimRight(out.String(), "\n"), warnings
}

func isSemanticClass(class utils.ReviewFileClass) bool {
	return !class.IsLockFile && !class.IsGenerated && !class.IsVendored && !class.IsBinary
}

func diffFileSummary(class utils.ReviewFileClass, status, path, section string) string {
	var label string
	switch {
	case class.IsLockFile:
		label = "Lockfile"
	case class.IsBinary:
		label = "Binary file"
	case class.IsGenerated:
		label = "Generated"
	case class.IsVendored:
		label = "Vendored"
	default:
		label = "Truncated"
	}

	if label != "Truncated" {
		additions, deletions := countDiffStats(section)
		if additions > 0 || deletions > 0 {
			return fmt.Sprintf("[%s: %s — %d additions, %d deletions]", label, path, additions, deletions)
		}
		return fmt.Sprintf("[%s: %s — %s]", label, path, actionFromStatus(status))
	}
	return fmt.Sprintf("[Truncated: %s — total %d bytes]", path, len(section))
}

// diffContextHeader renders the one-line file context header that precedes
// a full or truncated semantic diff section.
func diffContextHeader(path, status string, class utils.ReviewFileClass, additions, deletions int) string {
	purpose := "source"
	switch {
	case class.IsLockFile:
		purpose = "dependency lock file"
	case class.IsBinary:
		purpose = "binary file"
	case class.IsGenerated:
		purpose = "auto-generated"
	case class.IsVendored:
		purpose = "vendored dependency"
	}

	actionLabel := "modified"
	switch status {
	case "A":
		actionLabel = "added"
	case "D":
		actionLabel = "deleted"
	case "R":
		actionLabel = "renamed"
	}

	return fmt.Sprintf("→ %s (%s, %s): +%d -%d", path, actionLabel, purpose, additions, deletions)
}

// countDiffStats counts + and - content lines in a diff section, skipping
// the +++/--- metadata header lines.
func countDiffStats(section string) (additions, deletions int) {
	for _, line := range strings.Split(section, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ "), strings.HasPrefix(line, "--- "):
			// diff metadata, not content
		case strings.HasPrefix(line, "+"):
			additions++
		case strings.HasPrefix(line, "-"):
			deletions++
		}
	}
	return additions, deletions
}

// truncateDiffSection keeps as much of the section as fits within maxBytes,
// preferring a newline boundary over a mid-line cut, and appends an explicit
// truncation marker so the model knows the section is partial.
func truncateDiffSection(section string, maxBytes int) string {
	if len(section) <= maxBytes {
		return section
	}

	const noticeBudget = 80 // upper bound on the marker's own length
	keep := maxBytes - noticeBudget
	if keep < minTruncateBytes {
		keep = minTruncateBytes
	}
	if keep >= len(section) {
		return section
	}

	// Prefer ending on a line boundary, but never cut below half of keep.
	breakPoint := keep
	for i := keep; i > keep/2; i-- {
		if section[i] == '\n' {
			breakPoint = i
			break
		}
	}

	truncated := section[:breakPoint]
	return truncated + fmt.Sprintf("\n\n[... truncated: showing first %d of %d bytes ...]", breakPoint, len(section))
}

// splitDiffSections parses raw `git diff` output into per-file sections.
// Keys are destination paths from the `+++ b/` header (post-rename name);
// for deletions, where `+++` is /dev/null, the `--- a/` path is used.
// Each value spans from the `diff --git` line through the last hunk of
// that file. Paths may contain spaces.
func splitDiffSections(rawDiff string) map[string]string {
	sections := make(map[string]string)
	var currentFile string
	var currentSection strings.Builder

	flush := func() {
		if currentFile != "" && currentSection.Len() > 0 {
			sections[currentFile] = strings.TrimSuffix(currentSection.String(), "\n")
		}
	}

	for _, line := range strings.Split(rawDiff, "\n") {
		if strings.HasPrefix(line, "diff --git a/") {
			flush()
			currentFile = ""
			currentSection.Reset()
			currentSection.WriteString(line)
			continue
		}
		if currentSection.Len() == 0 {
			continue // content before the first file header
		}
		if strings.HasPrefix(line, "+++ b/") {
			if newFile := strings.TrimPrefix(line, "+++ b/"); newFile != "/dev/null" {
				currentFile = newFile
			}
		} else if currentFile == "" && strings.HasPrefix(line, "--- a/") {
			currentFile = strings.TrimPrefix(line, "--- a/")
		}
		currentSection.WriteString("\n")
		currentSection.WriteString(line)
	}
	flush()

	return sections
}
