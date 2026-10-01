package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/codereview"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

// Bounds for the change context pre-loaded into a reviewer subagent's task.
// The diff budget keeps a typical multi-file review inline while leaving the
// reviewer's context window for the files it chooses to open; anything larger
// degrades to a stat summary and the reviewer fetches hunks per file.
const (
	reviewContextMaxDiffBytes     = 96 * 1024
	reviewContextMaxStatBytes     = 16 * 1024
	repoConventionsMaxBytes       = 16 * 1024
	reviewContextMaxUntracked     = 50
	reviewContextGitCommandBudget = 10 * time.Second
	// reviewContextMaxExcerptBytes bounds the line-numbered code around each
	// hunk plus new-file content. With it inline a reviewer can judge most
	// hunks without reading files, and every turn resends the whole context,
	// so the budget trades a bigger first prompt for far fewer turns.
	reviewContextMaxExcerptBytes = 96 * 1024
)

// repoConventionFiles are checked in order at the workspace root; the first
// one found is inlined into every subagent's system prompt so it does not
// spend a turn reading it (or skip it and miss the repo's rules).
var repoConventionFiles = []string{"AGENTS.md", "CLAUDE.md"}

// isReviewerPersona reports whether persona resolves (by ID or alias) to the
// canonical reviewer persona.
func isReviewerPersona(a *Agent, persona string) bool {
	return canonicalPersonaID(a, persona) == personas.IDReviewer
}

// canonicalPersonaID resolves a persona ID or alias to its canonical catalog
// ID. Unknown personas are returned normalized but otherwise unchanged.
func canonicalPersonaID(a *Agent, persona string) string {
	normalized := normalizeAgentPersonaID(persona)
	if normalized == "" || a == nil {
		return normalized
	}
	if cfg := a.GetConfig(); cfg != nil {
		if st := cfg.GetSubagentType(normalized); st != nil && st.ID != "" {
			return normalizeAgentPersonaID(st.ID)
		}
	}
	return normalized
}

// buildReviewerChangeContext snapshots the uncommitted change in workspaceRoot
// (staged + unstaged vs HEAD, plus untracked file names) as a prompt section.
// A reviewer otherwise spends its first turns running git diff; each turn is a
// full LLM round trip. Returns "" when the directory is not a git work tree or
// has no uncommitted changes.
func buildReviewerChangeContext(ctx context.Context, workspaceRoot string) string {
	return buildReviewerChangeContextForScope(ctx, workspaceRoot, reviewScopeWorkingTree)
}

// reviewScope selects which uncommitted change a reviewer is pre-loaded with.
type reviewScope int

const (
	reviewScopeWorkingTree reviewScope = iota // staged + unstaged vs HEAD, plus untracked names
	reviewScopeStaged                         // the index only (what the next commit would contain)
)

func buildReviewerChangeContextForScope(ctx context.Context, workspaceRoot string, scope reviewScope) string {
	if strings.TrimSpace(workspaceRoot) == "" {
		return ""
	}
	return buildChangeSection(ctx, workspaceRoot, scope)
}

// subagentConventionsSection formats the workspace's conventions file for a
// subagent system prompt, or returns "" when there is none.
func subagentConventionsSection(workspaceRoot string) string {
	if strings.TrimSpace(workspaceRoot) == "" {
		return ""
	}
	conventions, name := readRepoConventions(workspaceRoot)
	if conventions == "" {
		return ""
	}
	return fmt.Sprintf("\n\n## Repo Conventions (from %s — already loaded, do not re-read)\n\n%s\n", name, conventions)
}

func buildChangeSection(ctx context.Context, workspaceRoot string, scope reviewScope) string {
	base := []string{"HEAD"}
	description := "Working-tree change vs HEAD (staged + unstaged)"
	if scope == reviewScopeStaged {
		base = []string{"--cached"}
		description = "Staged change (`git diff --cached`)"
		if _, err := runReviewGit(ctx, workspaceRoot, "rev-parse", "--is-inside-work-tree"); err != nil {
			return ""
		}
	} else if _, err := runReviewGit(ctx, workspaceRoot, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
		return ""
	}

	stat, _ := runReviewGit(ctx, workspaceRoot, append([]string{"diff", "--stat"}, base...)...)
	diff, diffErr := runReviewGit(ctx, workspaceRoot, append([]string{"diff", "--no-ext-diff"}, base...)...)
	untrackedRaw := ""
	if scope == reviewScopeWorkingTree {
		untrackedRaw, _ = runReviewGit(ctx, workspaceRoot, "ls-files", "--others", "--exclude-standard")
	}

	var untracked []string
	for _, line := range strings.Split(untrackedRaw, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			untracked = append(untracked, line)
		}
	}

	if strings.TrimSpace(diff) == "" && len(untracked) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("# Change Under Review (pre-loaded)\n\n")
	b.WriteString(description + ", captured when this review was spawned. ")
	b.WriteString("Review this directly — do not re-run `git diff` for it. ")
	b.WriteString("If your task names a different range (a commit, a branch, specific paths), review that instead and ignore the parts of this section outside it.\n\n")

	if s := strings.TrimSpace(stat); s != "" {
		fmt.Fprintf(&b, "## Diff stat\n\n```\n%s\n```\n\n", truncateReviewSection(s, reviewContextMaxStatBytes))
	}

	diffInlined := false
	switch {
	case diffErr != nil || strings.TrimSpace(diff) == "":
		// Only untracked files changed; nothing to inline.
	case len(diff) > reviewContextMaxDiffBytes:
		fmt.Fprintf(&b, "## Diff\n\nThe full diff is %d KB, too large to inline. Fetch hunks per file with `git diff %s -- <path>`, prioritizing the files with the largest or riskiest changes in the stat above.\n\n", len(diff)/1024, base[0])
	default:
		fmt.Fprintf(&b, "## Diff\n\n```diff\n%s\n```\n\n", strings.TrimRight(diff, "\n"))
		diffInlined = true
	}

	repoRoot := workspaceRoot
	if top, err := runReviewGit(ctx, workspaceRoot, "rev-parse", "--show-toplevel"); err == nil && strings.TrimSpace(top) != "" {
		repoRoot = strings.TrimSpace(top)
	}

	// New files go first: they aren't in the diff, so without an excerpt the
	// reviewer has to read them in full. Tests come last — they matter less
	// for judging the change than the code they exercise.
	var newCode, newTests []codereview.FileExcerpt
	for _, rel := range untracked {
		if e, ok := codereview.NewFileExcerpt(repoRoot, rel); ok {
			if isTestPath(rel) {
				newTests = append(newTests, e)
			} else {
				newCode = append(newCode, e)
			}
		}
	}

	budget := reviewContextMaxExcerptBytes
	included := writeExcerpts(&b, "## New untracked files (part of the change; not in the diff)", newCode, &budget, false)

	if diffInlined {
		var hunks []codereview.FileExcerpt
		heading := "## Code around each change (current working-tree content, line-numbered)"
		if scope == reviewScopeStaged {
			// Excerpt the staged (index) content, not the working tree, so the
			// reviewer never sees unstaged edits that won't be committed.
			heading = "## Code around each change (staged content, line-numbered)"
			hunks = codereview.HunkExcerptsFrom(diff, func(rel string) ([]string, bool) {
				content, err := runReviewGit(ctx, repoRoot, "show", ":"+rel)
				if err != nil {
					return nil, false
				}
				return codereview.SplitContextLines(content)
			})
		} else {
			hunks = codereview.HunkExcerpts(repoRoot, diff)
		}
		writeExcerpts(&b, heading, hunks, &budget, true)
	}

	for path := range writeExcerpts(&b, "## New untracked test files", newTests, &budget, false) {
		included[path] = true
	}

	var rest []string
	for _, rel := range untracked {
		if !included[rel] {
			rest = append(rest, rel)
		}
	}
	if len(rest) > 0 {
		b.WriteString("## Other untracked files (not pre-loaded — read with view_range if they are part of the change)\n\n")
		shown := rest[:min(len(rest), reviewContextMaxUntracked)]
		for _, f := range shown {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		if omitted := len(rest) - len(shown); omitted > 0 {
			fmt.Fprintf(&b, "- ... (%d more)\n", omitted)
		}
		b.WriteString("\n")
	}

	b.WriteString("---\n\n")
	return b.String()
}

// writeExcerpts writes as many excerpts as fit in *budget under heading and
// returns the paths written. Excerpts that don't fit are named so the reviewer
// knows to open them.
func writeExcerpts(b *strings.Builder, heading string, excerpts []codereview.FileExcerpt, budget *int, listSkipped bool) map[string]bool {
	written := map[string]bool{}
	if len(excerpts) == 0 {
		return written
	}
	var skipped []string
	var body strings.Builder
	for _, e := range excerpts {
		if len(e.Body) > *budget {
			skipped = append(skipped, e.Path)
			continue
		}
		*budget -= len(e.Body)
		body.WriteString(e.Body + "\n\n")
		written[e.Path] = true
	}
	if body.Len() == 0 && (!listSkipped || len(skipped) == 0) {
		return written
	}
	b.WriteString(heading + "\n\n")
	b.WriteString(body.String())
	if listSkipped && len(skipped) > 0 {
		fmt.Fprintf(b, "Not pre-loaded (context budget reached), open with view_range if needed: %s\n\n", strings.Join(skipped, ", "))
	}
	return written
}

func isTestPath(rel string) bool {
	base := filepath.Base(rel)
	return strings.HasSuffix(base, "_test.go") || strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") ||
		strings.HasPrefix(base, "test_") || strings.Contains(filepath.ToSlash(rel), "/__tests__/")
}

func readRepoConventions(workspaceRoot string) (string, string) {
	for _, name := range repoConventionFiles {
		content, err := os.ReadFile(filepath.Join(workspaceRoot, name))
		if err != nil {
			continue
		}
		trimmed := strings.TrimSpace(string(content))
		if trimmed == "" {
			continue
		}
		return truncateReviewSection(trimmed, repoConventionsMaxBytes), name
	}
	return "", ""
}

func runReviewGit(ctx context.Context, dir string, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cmdCtx, cancel := context.WithTimeout(ctx, reviewContextGitCommandBudget)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "git", append([]string{"-C", dir, "--no-pager", "-c", "color.ui=false"}, args...)...) //nolint:gosec // G204: fixed git subcommands; dir is the resolved workspace root
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	return string(out), err
}

func truncateReviewSection(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes] + "\n... [truncated]"
}
