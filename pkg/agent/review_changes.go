package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/factory"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

// Splitting policy for review_changes. A change that fits one reviewer's
// budget (changeContextBudget.linesPerReviewer, scaled to the reviewer
// model's context window) is one reviewer; above it, files are grouped
// (keeping directories together) into roughly equal slices, each judged by
// its own reviewer in parallel.
const (
	reviewMaxReviewers    = 4
	untrackedLineCountCap = 5000
)

// ReviewChangesOptions selects what review_changes reviews.
type ReviewChangesOptions struct {
	// Scope is "working_tree" (default), "staged", or "range".
	Scope string
	// Range is the git revision range for Scope "range".
	Range string
	// Focus is optional guidance passed to every reviewer.
	Focus string
	// Dir overrides the directory whose repository is reviewed; empty uses
	// the agent's workspace root.
	Dir string

	// taskPrefix prefixes reviewer task IDs; a background review uses its
	// task ID so check_subagent can find its reviewers.
	taskPrefix string
	// quiet suppresses terminal streaming (background reviews).
	quiet bool
}

func (o ReviewChangesOptions) target() (reviewTarget, error) {
	switch strings.TrimSpace(o.Scope) {
	case "", "working_tree":
		return workingTreeTarget, nil
	case "staged":
		return stagedTarget, nil
	case "range":
		spec := strings.TrimSpace(o.Range)
		if err := validateRangeSpec(spec); err != nil {
			return reviewTarget{}, agenterrors.NewValidation(err.Error(), nil)
		}
		return reviewTarget{kind: reviewTargetRange, rangeSpec: spec}, nil
	default:
		return reviewTarget{}, agenterrors.NewValidation(fmt.Sprintf("unknown review scope %q (use working_tree, staged, or range)", o.Scope), nil)
	}
}

// ReviewFinding is one issue reported by a reviewer.
type ReviewFinding struct {
	Severity string `json:"severity"` // MUST_FIX | VERIFY | NOTE
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Issue    string `json:"issue"`
	Evidence string `json:"evidence,omitempty"`
	Fix      string `json:"fix,omitempty"`
}

// ReviewerRun summarizes one reviewer subagent within a review.
type ReviewerRun struct {
	ID         string        `json:"id"`
	Files      []string      `json:"files,omitempty"`
	Elapsed    time.Duration `json:"elapsed"`
	Iterations int           `json:"iterations"`
	TokensUsed int           `json:"tokens_used"`
	Error      string        `json:"error,omitempty"`
	// Unstructured holds the reviewer's report when it ended without a
	// parseable findings block, so nothing it found is lost.
	Unstructured string `json:"unstructured,omitempty"`
}

// ReviewChangesResult is the merged outcome of a review.
type ReviewChangesResult struct {
	// Verdict is "APPROVE", "CHANGES_REQUIRED", or "INCONCLUSIVE" (a
	// reviewer failed or returned no structured verdict, and no MUST_FIX
	// was found elsewhere).
	Verdict   string          `json:"verdict"`
	Findings  []ReviewFinding `json:"findings"`
	Reviewers []ReviewerRun   `json:"reviewers"`
	Elapsed   time.Duration   `json:"elapsed"`
}

// ReviewChanges reviews a change with one or more reviewer subagents. It owns
// the parts that shouldn't depend on the calling model: building each
// reviewer's pre-loaded context, deciding whether to split the change across
// parallel reviewers, and merging their findings into one result.
func (a *Agent) ReviewChanges(ctx context.Context, opts ReviewChangesOptions) (*ReviewChangesResult, error) {
	if !a.CanSpawnSubagents() {
		return nil, agenterrors.NewValidation("review_changes spawns reviewer subagents and is not available at this subagent depth", nil)
	}
	target, err := opts.target()
	if err != nil {
		return nil, err
	}
	dir := opts.Dir
	if dir == "" {
		dir = a.currentWorkspaceRoot()
	}
	workspaceRoot, err := filepath.Abs(dir)
	if err != nil {
		return nil, agenterrors.NewConfig("failed to resolve absolute workspace path", err)
	}
	repoRoot := reviewRepoRoot(ctx, workspaceRoot)
	if repoRoot == "" {
		return nil, agenterrors.NewValidation("review_changes requires a git repository", nil)
	}

	provider, model, role, systemPrompt, err := resolveSubagentProviderModel(a, personas.IDReviewer, true, workspaceRoot)
	if err != nil {
		return nil, err
	}
	budget := changeContextBudgetFor(a.subagentContextWindow(provider, model))

	groups := planReviewGroups(changedLineCounts(ctx, repoRoot, target), budget.linesPerReviewer())
	if len(groups) == 0 {
		return nil, agenterrors.NewValidation("no changes to review for scope "+target.description(), nil)
	}

	prefix := opts.taskPrefix
	if prefix == "" {
		prefix = fmt.Sprintf("review-%d", time.Now().UnixNano())
	}
	tasks := make([]SubagentTask, 0, len(groups))
	for i, files := range groups {
		var paths []string
		if len(groups) > 1 {
			paths = files
		}
		changeContext := buildChangeContext(ctx, repoRoot, target, paths, budget)
		if changeContext == "" {
			continue
		}
		tasks = append(tasks, SubagentTask{
			ID:           fmt.Sprintf("%s-%d", prefix, i+1),
			Prompt:       changeContext + reviewTaskText(opts.Focus, i, len(groups), files),
			Persona:      personas.IDReviewer,
			Provider:     provider,
			Model:        model,
			SystemPrompt: systemPrompt,
			Role:         role,
		})
	}
	if len(tasks) == 0 {
		return nil, agenterrors.NewValidation("no changes to review for scope "+target.description(), nil)
	}

	start := time.Now()
	if !opts.quiet {
		printSubagentStart(personas.IDReviewer, displayOrDefault(provider), displayOrDefault(model))
	}
	results := a.runReviewTasks(ctx, tasks, opts.quiet)

	review := mergeReviewResults(tasks, groups, results)
	review.Elapsed = time.Since(start)
	return review, nil
}

func (a *Agent) runReviewTasks(ctx context.Context, tasks []SubagentTask, quiet bool) []*SubagentResult {
	runner := a.GetSubagentRunner()
	var results []*SubagentResult
	if len(tasks) == 1 {
		t := tasks[0]
		res := runner.runTask(ctx, t.ID, t.Prompt, SubagentOptions{
			Persona: t.Persona, Provider: t.Provider, Model: t.Model, SystemPrompt: t.SystemPrompt, Quiet: quiet, Role: t.Role,
		}, nil, 0)
		results = []*SubagentResult{res}
	} else {
		opts := SubagentOptions{MaxConcurrentSubagents: 1, Quiet: quiet}
		if cfg := a.GetConfig(); cfg != nil && cfg.GetSubagentParallelEnabled() {
			opts.MaxConcurrentSubagents = min(max(cfg.GetSubagentMaxParallel(), 1), 16)
		}
		results = runner.RunParallel(ctx, tasks, opts)
	}
	for _, r := range results {
		if r == nil {
			continue
		}
		if !quiet {
			printSubagentDone(personas.IDReviewer, r)
		}
		if r.TokensUsed > 0 || r.Cost > 0 {
			// Roll the reviewer's usage up under its own role with its real
			// prompt/completion token split (SP-150 §150c, item 150.5).
			a.RollupSubagentUsage(r)
		}
	}
	return results
}

func reviewTaskText(focus string, index, total int, files []string) string {
	var b strings.Builder
	b.WriteString("# Your Task\n\nReview the change above for real problems.")
	if total > 1 {
		fmt.Fprintf(&b, " This change was split across %d reviewers; you are reviewer %d and own only these files: %s.", total, index+1, strings.Join(files, ", "))
	}
	if focus = strings.TrimSpace(focus); focus != "" {
		b.WriteString("\n\nFocus: " + focus)
	}
	b.WriteString("\n\nEnd with the JSON findings block described in your report format.")
	return b.String()
}

// changedLineCounts returns changed (added + deleted) lines per file for
// target, counting untracked files' lines for a working-tree review.
func changedLineCounts(ctx context.Context, repoRoot string, target reviewTarget) map[string]int {
	counts := map[string]int{}
	raw, _ := runReviewGit(ctx, repoRoot, append([]string{"diff", "--numstat"}, target.diffBase()...)...)
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 || fields[2] == "" {
			continue
		}
		added, _ := strconv.Atoi(fields[0]) // "-" for binary files counts as 0
		deleted, _ := strconv.Atoi(fields[1])
		counts[numstatPath(fields[2])] += max(added+deleted, 1)
	}
	if target.kind == reviewTargetWorkingTree {
		for _, rel := range listUntracked(ctx, repoRoot, nil) {
			counts[rel] = countFileLines(filepath.Join(repoRoot, rel))
		}
	}
	return counts
}

// numstatPath resolves a numstat rename entry ("old => new" or
// "dir/{old => new}/f") to the new path.
func numstatPath(p string) string {
	if !strings.Contains(p, " => ") {
		return p
	}
	if open := strings.Index(p, "{"); open >= 0 {
		if end := strings.Index(p[open:], "}"); end >= 0 {
			inner := p[open+1 : open+end]
			if _, after, ok := strings.Cut(inner, " => "); ok {
				return filepath.ToSlash(filepath.Clean(p[:open] + after + p[open+end+1:]))
			}
		}
	}
	_, after, _ := strings.Cut(p, " => ")
	return after
}

func countFileLines(path string) int {
	lines, ok := readLinesCapped(path, untrackedLineCountCap)
	if !ok {
		return 1
	}
	return max(lines, 1)
}

// planReviewGroups splits files into reviewer slices. Files are ordered by
// path so a slice keeps neighbouring files (same package/directory) together.
func planReviewGroups(counts map[string]int, linesPerReviewer int) [][]string {
	if len(counts) == 0 {
		return nil
	}
	files := make([]string, 0, len(counts))
	total := 0
	for f, n := range counts {
		files = append(files, f)
		total += n
	}
	sort.Strings(files)
	if total <= linesPerReviewer*5/4 || len(files) == 1 {
		return [][]string{files}
	}

	n := min(min((total+linesPerReviewer-1)/linesPerReviewer, reviewMaxReviewers), len(files))
	groups := make([][]string, 0, n)
	var current []string
	acc := 0
	for i, f := range files {
		current = append(current, f)
		acc += counts[f]
		remainingFiles := len(files) - i - 1
		remainingGroups := n - len(groups) - 1
		if remainingGroups > 0 && remainingFiles >= remainingGroups && acc >= total*(len(groups)+1)/n {
			groups = append(groups, current)
			current = nil
		}
	}
	if len(current) > 0 {
		groups = append(groups, current)
	}
	return groups
}

func mergeReviewResults(tasks []SubagentTask, groups [][]string, results []*SubagentResult) *ReviewChangesResult {
	review := &ReviewChangesResult{}
	inconclusive := false
	for i, r := range results {
		run := ReviewerRun{ID: tasks[i].ID}
		if len(groups) > 1 && i < len(groups) {
			run.Files = groups[i]
		}
		if r == nil {
			run.Error = "reviewer did not run"
			inconclusive = true
			review.Reviewers = append(review.Reviewers, run)
			continue
		}
		run.Elapsed, run.Iterations, run.TokensUsed = r.Elapsed, r.Iterations, r.TokensUsed
		if r.Error != nil {
			run.Error = r.Error.Error()
		}
		parsed, ok := parseReviewerReport(r.Output)
		if ok {
			review.Findings = append(review.Findings, parsed.Findings...)
		} else {
			inconclusive = true
			if out := strings.TrimSpace(r.Output); out != "" {
				run.Unstructured = out
			}
		}
		if r.Error != nil && !ok {
			inconclusive = true
		}
		review.Reviewers = append(review.Reviewers, run)
	}

	review.Findings = dedupeFindings(review.Findings)
	switch {
	case countSeverity(review.Findings, "MUST_FIX") > 0:
		review.Verdict = "CHANGES_REQUIRED"
	case inconclusive:
		review.Verdict = "INCONCLUSIVE"
	default:
		review.Verdict = "APPROVE"
	}
	return review
}

var severityRank = map[string]int{"MUST_FIX": 0, "VERIFY": 1, "NOTE": 2}

func normalizeSeverity(s string) string {
	s = strings.ToUpper(strings.TrimSpace(strings.ReplaceAll(s, " ", "_")))
	if _, ok := severityRank[s]; ok {
		return s
	}
	switch s {
	case "MUSTFIX", "BLOCKER", "CRITICAL", "HIGH":
		return "MUST_FIX"
	case "SHOULD_FIX", "MEDIUM", "QUESTION":
		return "VERIFY"
	default:
		return "NOTE"
	}
}

func dedupeFindings(findings []ReviewFinding) []ReviewFinding {
	byKey := map[string]int{}
	var out []ReviewFinding
	for _, f := range findings {
		f.Severity = normalizeSeverity(f.Severity)
		issue := strings.ToLower(strings.Join(strings.Fields(f.Issue), " "))
		if len(issue) > 60 {
			issue = issue[:60]
		}
		key := fmt.Sprintf("%s:%d:%s", f.File, f.Line, issue)
		if i, ok := byKey[key]; ok {
			if severityRank[f.Severity] < severityRank[out[i].Severity] {
				out[i].Severity = f.Severity
			}
			continue
		}
		byKey[key] = len(out)
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if severityRank[out[i].Severity] != severityRank[out[j].Severity] {
			return severityRank[out[i].Severity] < severityRank[out[j].Severity]
		}
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

func countSeverity(findings []ReviewFinding, severity string) int {
	n := 0
	for _, f := range findings {
		if f.Severity == severity {
			n++
		}
	}
	return n
}

// Markdown renders the result compactly for the calling model and for display.
func (r *ReviewChangesResult) Markdown() string {
	var b strings.Builder
	var tokens int
	for _, run := range r.Reviewers {
		tokens += run.TokensUsed
	}
	fmt.Fprintf(&b, "Review verdict: %s — %d MUST_FIX, %d VERIFY, %d NOTE (%d reviewer(s), %s, %d tokens)\n",
		r.Verdict, countSeverity(r.Findings, "MUST_FIX"), countSeverity(r.Findings, "VERIFY"), countSeverity(r.Findings, "NOTE"),
		len(r.Reviewers), r.Elapsed.Round(time.Second), tokens)

	for _, severity := range []string{"MUST_FIX", "VERIFY", "NOTE"} {
		first := true
		for _, f := range r.Findings {
			if f.Severity != severity {
				continue
			}
			if first {
				fmt.Fprintf(&b, "\n## %s\n", severity)
				first = false
			}
			loc := f.File
			if f.Line > 0 {
				loc = fmt.Sprintf("%s:%d", f.File, f.Line)
			}
			if loc != "" {
				loc += " — "
			}
			fmt.Fprintf(&b, "- %s%s\n", loc, f.Issue)
			if f.Evidence != "" {
				fmt.Fprintf(&b, "  - evidence: %s\n", f.Evidence)
			}
			if f.Fix != "" {
				fmt.Fprintf(&b, "  - fix: %s\n", f.Fix)
			}
		}
	}
	if len(r.Findings) == 0 && r.Verdict == "APPROVE" {
		b.WriteString("\nNo issues found.\n")
	}

	for _, run := range r.Reviewers {
		if run.Error == "" && run.Unstructured == "" {
			continue
		}
		fmt.Fprintf(&b, "\n## Reviewer %s", run.ID)
		if len(run.Files) > 0 {
			fmt.Fprintf(&b, " (%s)", strings.Join(run.Files, ", "))
		}
		b.WriteString("\n")
		if run.Error != "" {
			fmt.Fprintf(&b, "Error: %s\n", run.Error)
		}
		if run.Unstructured != "" {
			fmt.Fprintf(&b, "Unstructured report:\n%s\n", run.Unstructured)
		}
	}
	return b.String()
}

// GuidanceJSON renders findings as {"MUST_FIX": [...], "VERIFY": [...],
// "NOTE": [...]} with issue/evidence/suggestion/file entries — the shape the
// web UI review tab's fix picker parses.
func (r *ReviewChangesResult) GuidanceJSON() string {
	sections := map[string][]map[string]string{}
	for _, f := range r.Findings {
		entry := map[string]string{"issue": f.Issue}
		if f.Evidence != "" {
			entry["evidence"] = f.Evidence
		}
		if f.Fix != "" {
			entry["suggestion"] = f.Fix
		}
		if f.File != "" {
			entry["file"] = f.File
			if f.Line > 0 {
				entry["file"] = fmt.Sprintf("%s:%d", f.File, f.Line)
			}
		}
		sections[f.Severity] = append(sections[f.Severity], entry)
	}
	if len(sections) == 0 {
		return ""
	}
	out, err := json.Marshal(sections)
	if err != nil {
		return ""
	}
	return string(out)
}

// Summary is the one-line verdict and finding counts.
func (r *ReviewChangesResult) Summary() string {
	return strings.SplitN(r.Markdown(), "\n", 2)[0]
}

// subagentContextWindow returns the context window (tokens) of the model a
// subagent with this provider/model would run on, resolved the way
// createSubagent resolves it, or 0 when it can't be determined.
func (a *Agent) subagentContextWindow(provider, model string) int {
	r := a.GetSubagentRunner()
	if r.shared == nil || r.shared.ConfigManager == nil {
		return 0
	}
	if p := a.GetProvider(); provider == "" && p != "" && p != "unknown" {
		provider = p
	}
	if m := a.GetModel(); model == "" && m != "" && m != "unknown" {
		model = m
	}
	clientType, finalModel, err := r.shared.ConfigManager.ResolveProviderModel(provider, model)
	if err != nil {
		return 0
	}
	var client api.ClientInterface
	if r.testClientFactory != nil {
		client, err = r.testClientFactory(clientType, finalModel)
	} else {
		client, err = factory.CreateProviderClient(clientType, finalModel)
	}
	if err != nil || client == nil {
		return 0
	}
	limit, err := client.GetModelContextLimit()
	if err != nil || limit <= 0 {
		return 0
	}
	if cfg := a.GetConfig(); cfg != nil && cfg.MaxContextTokens != nil && *cfg.MaxContextTokens > 0 && limit > *cfg.MaxContextTokens {
		limit = *cfg.MaxContextTokens
	}
	return limit
}

func displayOrDefault(s string) string {
	if s == "" {
		return "default"
	}
	return s
}

// handleReviewChanges is the review_changes tool entry point.
func handleReviewChanges(ctx context.Context, a *Agent, args map[string]any) (string, error) {
	opts := ReviewChangesOptions{}
	opts.Scope, _ = args["scope"].(string)
	opts.Range, _ = args["range"].(string)
	opts.Focus, _ = args["focus"].(string)
	background := true
	if v, ok := args["background"].(bool); ok {
		background = v
	}
	if background && a.backgroundAvailable() {
		return a.startBackgroundReview(opts)
	}
	review, err := a.ReviewChanges(ctx, opts)
	if err != nil {
		return "", err
	}
	return review.Markdown(), nil
}
