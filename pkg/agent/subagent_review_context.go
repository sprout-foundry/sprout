package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/personas"
)

// Bounds for the change context pre-loaded into a reviewer subagent's task.
// The diff budget keeps a typical multi-file review inline while leaving the
// reviewer's context window for the files it chooses to open; anything larger
// degrades to a stat summary and the reviewer fetches hunks per file.
const (
	reviewContextMaxDiffBytes     = 96 * 1024
	reviewContextMaxStatBytes     = 16 * 1024
	reviewContextMaxConventions   = 16 * 1024
	reviewContextMaxUntracked     = 50
	reviewContextGitCommandBudget = 10 * time.Second
)

// reviewConventionFiles are checked in order at the workspace root; the first
// one found is inlined so the reviewer does not spend a turn reading it.
var reviewConventionFiles = []string{"AGENTS.md", "CLAUDE.md"}

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
// (staged + unstaged vs HEAD, plus untracked file names) and the repo's
// conventions file, formatted as a prompt section. A reviewer otherwise spends
// its first turns running git diff and reading AGENTS.md; each turn is a full
// LLM round trip. Returns "" when the directory is not a git work tree or has
// no uncommitted changes and no conventions file.
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

	var b strings.Builder

	if conventions, name := readReviewConventions(workspaceRoot); conventions != "" {
		fmt.Fprintf(&b, "# Repo Conventions (%s, pre-loaded — do not re-read)\n\n%s\n\n---\n\n", name, conventions)
	}

	if change := buildChangeSection(ctx, workspaceRoot, scope); change != "" {
		b.WriteString(change)
	}

	return b.String()
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

	switch {
	case diffErr != nil || strings.TrimSpace(diff) == "":
		// Only untracked files changed; nothing to inline.
	case len(diff) > reviewContextMaxDiffBytes:
		fmt.Fprintf(&b, "## Diff\n\nThe full diff is %d KB, too large to inline. Fetch hunks per file with `git diff %s -- <path>`, prioritizing the files with the largest or riskiest changes in the stat above.\n\n", len(diff)/1024, base[0])
	default:
		fmt.Fprintf(&b, "## Diff\n\n```diff\n%s\n```\n\n", strings.TrimRight(diff, "\n"))
	}

	if len(untracked) > 0 {
		b.WriteString("## Untracked files (new, not in the diff — read them if they are part of the change)\n\n")
		shown := untracked
		if len(shown) > reviewContextMaxUntracked {
			shown = shown[:reviewContextMaxUntracked]
		}
		for _, f := range shown {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		if omitted := len(untracked) - len(shown); omitted > 0 {
			fmt.Fprintf(&b, "- ... (%d more)\n", omitted)
		}
		b.WriteString("\n")
	}

	b.WriteString("---\n\n")
	return b.String()
}

func readReviewConventions(workspaceRoot string) (string, string) {
	for _, name := range reviewConventionFiles {
		content, err := os.ReadFile(filepath.Join(workspaceRoot, name))
		if err != nil {
			continue
		}
		trimmed := strings.TrimSpace(string(content))
		if trimmed == "" {
			continue
		}
		return truncateReviewSection(trimmed, reviewContextMaxConventions), name
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
