package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/codereview"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

// Upper bounds for the change context pre-loaded into a reviewer subagent's
// task, reached on large-window models. The diff bound keeps a typical
// multi-file review inline; anything larger degrades to a stat summary and the
// reviewer fetches hunks per file. The excerpt bound covers line-numbered code
// around each hunk plus new-file content: with it inline a reviewer can judge
// most hunks without reading files, and since every turn resends the whole
// context, a bigger first prompt buys far fewer turns.
const (
	reviewContextMaxDiffBytes     = 96 * 1024
	reviewContextMaxStatBytes     = 16 * 1024
	reviewContextMaxExcerptBytes  = 96 * 1024
	repoConventionsMaxBytes       = 16 * 1024
	reviewContextMaxUntracked     = 50
	reviewContextGitCommandBudget = 10 * time.Second
	// reviewContextWindowShare is the share of the reviewer model's context
	// window the change context may take, leaving the rest for the system
	// prompt, tool schemas, and the review's own turns.
	reviewContextWindowShare = 0.35
	approxBytesPerToken      = 4
)

// changeContextBudget sizes a reviewer's pre-loaded change context.
type changeContextBudget struct {
	total    int // diff + stat + excerpts
	diff     int
	stat     int
	excerpts int
}

var defaultChangeContextBudget = changeContextBudget{
	total:    reviewContextMaxDiffBytes + reviewContextMaxStatBytes + reviewContextMaxExcerptBytes,
	diff:     reviewContextMaxDiffBytes,
	stat:     reviewContextMaxStatBytes,
	excerpts: reviewContextMaxExcerptBytes,
}

// changeContextBudgetFor scales the change context to a reviewer whose
// context window is windowTokens, so a small local model isn't overflowed by
// its starting prompt. An unknown window (<= 0) gets the default bounds.
func changeContextBudgetFor(windowTokens int) changeContextBudget {
	if windowTokens <= 0 {
		return defaultChangeContextBudget
	}
	total := int(float64(windowTokens)*reviewContextWindowShare) * approxBytesPerToken
	if total >= defaultChangeContextBudget.total {
		return defaultChangeContextBudget
	}
	return changeContextBudget{
		total:    total,
		diff:     total * 45 / 100,
		stat:     total * 8 / 100,
		excerpts: total * 47 / 100,
	}
}

// linesPerReviewer is how many changed lines one reviewer slice should hold
// so its diff fits the budget (~80 bytes per diff line with context).
func (b changeContextBudget) linesPerReviewer() int {
	return max(150, min(1200, b.diff/80))
}

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

// reviewTarget selects which change a reviewer is pre-loaded with.
type reviewTarget struct {
	kind reviewTargetKind
	// rangeSpec is a git revision range for reviewTargetRange, e.g.
	// "main...HEAD", "abc123^!", or a single revision (diffed against the
	// working tree).
	rangeSpec string
}

type reviewTargetKind int

const (
	reviewTargetWorkingTree reviewTargetKind = iota // staged + unstaged vs HEAD, plus untracked files
	reviewTargetStaged                              // the index only (what the next commit would contain)
	reviewTargetRange                               // a commit or branch range
)

var (
	workingTreeTarget = reviewTarget{kind: reviewTargetWorkingTree}
	stagedTarget      = reviewTarget{kind: reviewTargetStaged}
)

// rangeSpecPattern admits git revision syntax (names, SHAs, ~ ^ @{}, ..,
// ...) and nothing that git could read as an option or a pathspec.
var rangeSpecPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_./~^@{}+!-]*$`)

func validateRangeSpec(spec string) error {
	if !rangeSpecPattern.MatchString(spec) {
		return fmt.Errorf("invalid revision range %q: use a branch, tag, or SHA range (e.g. main...HEAD, or abc123^! for one commit)", spec)
	}
	return nil
}

// diffBase returns the git diff arguments selecting the target's change.
func (t reviewTarget) diffBase() []string {
	switch t.kind {
	case reviewTargetStaged:
		return []string{"--cached"}
	case reviewTargetRange:
		return []string{t.rangeSpec}
	default:
		return []string{"HEAD"}
	}
}

// postImageRev is the revision whose content a hunk excerpt should show:
// ":" for the index, a commit for a range ending at one, or "" for the
// working tree.
func (t reviewTarget) postImageRev() string {
	switch t.kind {
	case reviewTargetStaged:
		return ":"
	case reviewTargetRange:
		spec := t.rangeSpec
		if base, ok := strings.CutSuffix(spec, "^!"); ok {
			return base
		}
		for _, sep := range []string{"...", ".."} {
			if i := strings.Index(spec, sep); i >= 0 {
				if end := spec[i+len(sep):]; end != "" {
					return end
				}
				return "HEAD"
			}
		}
		return "" // a single revision is diffed against the working tree
	default:
		return ""
	}
}

func (t reviewTarget) description() string {
	switch t.kind {
	case reviewTargetStaged:
		return "Staged change (`git diff --cached`)"
	case reviewTargetRange:
		return fmt.Sprintf("Change in range `%s`", t.rangeSpec)
	default:
		return "Working-tree change vs HEAD (staged + unstaged)"
	}
}

// buildReviewerChangeContext snapshots the uncommitted change in workspaceRoot
// as a prompt section. A reviewer otherwise spends its first turns running
// git diff and reading files; each turn is a full LLM round trip. Returns ""
// when the directory is not a git work tree or has no uncommitted changes.
func buildReviewerChangeContext(ctx context.Context, workspaceRoot string, budget changeContextBudget) string {
	return buildChangeContext(ctx, workspaceRoot, workingTreeTarget, nil, budget)
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

// reviewRepoRoot returns the git top level containing dir, or "" when dir is
// not in a work tree. All review git commands run there so diff paths,
// pathspecs, and untracked listings share one base.
func reviewRepoRoot(ctx context.Context, dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	top, err := runReviewGit(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(top)
}

// listUntracked returns untracked, non-ignored files (repo-root relative),
// limited to paths when paths is non-empty.
func listUntracked(ctx context.Context, repoRoot string, paths []string) []string {
	raw, _ := runReviewGit(ctx, repoRoot, append([]string{"ls-files", "--others", "--exclude-standard", "--"}, paths...)...)
	var untracked []string
	for _, line := range strings.Split(raw, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			untracked = append(untracked, line)
		}
	}
	return untracked
}

// buildChangeContext formats target's change in the repo containing
// workspaceRoot — limited to paths when non-empty — with the diff, code
// around each hunk, and new-file content, within the excerpt budget.
func buildChangeContext(ctx context.Context, workspaceRoot string, target reviewTarget, paths []string, budget changeContextBudget) string {
	repoRoot := reviewRepoRoot(ctx, workspaceRoot)
	if repoRoot == "" {
		return ""
	}
	if target.kind == reviewTargetWorkingTree {
		if _, err := runReviewGit(ctx, repoRoot, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
			return ""
		}
	}

	pathspec := append([]string{"--"}, paths...)
	stat, _ := runReviewGit(ctx, repoRoot, append(append([]string{"diff", "--stat"}, target.diffBase()...), pathspec...)...)
	diff, diffErr := runReviewGit(ctx, repoRoot, append(append([]string{"diff", "--no-ext-diff"}, target.diffBase()...), pathspec...)...)
	var untracked []string
	if target.kind == reviewTargetWorkingTree {
		untracked = listUntracked(ctx, repoRoot, paths)
	}

	if strings.TrimSpace(diff) == "" && len(untracked) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("# Change Under Review (pre-loaded)\n\n")
	b.WriteString(target.description() + ", captured when this review was spawned. ")
	b.WriteString("Review this directly — do not re-run `git diff` for it. ")
	if len(paths) > 0 {
		b.WriteString("This review covers only the files below; other files in the change are reviewed separately.\n\n")
	} else {
		b.WriteString("If your task names a different range (a commit, a branch, specific paths), review that instead and ignore the parts of this section outside it.\n\n")
	}

	used := 0
	if s := strings.TrimSpace(stat); s != "" {
		s = truncateReviewSection(s, budget.stat)
		used += len(s)
		fmt.Fprintf(&b, "## Diff stat\n\n```\n%s\n```\n\n", s)
	}

	diffInlined := false
	switch {
	case diffErr != nil || strings.TrimSpace(diff) == "":
		// Only untracked files changed; nothing to inline.
	case len(diff) > budget.diff:
		fmt.Fprintf(&b, "## Diff\n\nThe full diff is %d KB, too large to inline. Fetch hunks per file with `git diff %s -- <path>`, prioritizing the files with the largest or riskiest changes in the stat above.\n\n", len(diff)/1024, target.diffBase()[0])
	default:
		fmt.Fprintf(&b, "## Diff\n\n```diff\n%s\n```\n\n", strings.TrimRight(diff, "\n"))
		diffInlined = true
		used += len(diff)
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

	// Excerpts get what the diff and stat left of the total, up to their cap.
	excerptBudget := max(0, min(budget.excerpts, budget.total-used))
	included := writeExcerpts(&b, "## New untracked files (part of the change; not in the diff)", newCode, &excerptBudget, false)

	if diffInlined {
		var hunks []codereview.FileExcerpt
		switch rev := target.postImageRev(); rev {
		case "":
			hunks = codereview.HunkExcerpts(repoRoot, diff)
			writeExcerpts(&b, "## Code around each change (current working-tree content, line-numbered)", hunks, &excerptBudget, true)
		default:
			// Excerpt the content the diff ends at (the index for a staged
			// review, a commit for a range), never unrelated working-tree edits.
			hunks = codereview.HunkExcerptsFrom(diff, func(rel string) ([]string, bool) {
				ref := rev + ":" + rel
				if rev == ":" {
					ref = ":" + rel
				}
				content, err := runReviewGit(ctx, repoRoot, "show", ref)
				if err != nil {
					return nil, false
				}
				return codereview.SplitContextLines(content)
			})
			label := "staged content"
			if target.kind == reviewTargetRange {
				label = "content at " + rev
			}
			writeExcerpts(&b, "## Code around each change ("+label+", line-numbered)", hunks, &excerptBudget, true)
		}
	}

	for path := range writeExcerpts(&b, "## New untracked test files", newTests, &excerptBudget, false) {
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
