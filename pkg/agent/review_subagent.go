package agent

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

// StagedReviewResult is the outcome of RunStagedReview.
type StagedReviewResult struct {
	// Verdict is "approved", "needs_revision", or "inconclusive" when the
	// reviewer ended without a VERDICT line (cancelled, budget cut, or a
	// model that ignored the format).
	Verdict string
	Report  string
	Run     *SubagentResult
}

var reviewVerdictPattern = regexp.MustCompile(`(?i)VERDICT:\s*\**\s*(APPROVE|CHANGES_REQUIRED)`)

// RunStagedReview runs the reviewer persona as a subagent over the staged
// change: it starts with the staged diff and repo conventions pre-loaded and
// can open files to check its findings. The model follows the reviewer
// persona's resolution, which honors review_provider/review_model.
func (a *Agent) RunStagedReview(ctx context.Context, focus string) (*StagedReviewResult, error) {
	root, err := filepath.Abs(a.currentWorkspaceRoot())
	if err != nil {
		return nil, agenterrors.NewConfig("failed to resolve absolute workspace path", err)
	}

	changeContext := buildReviewerChangeContextForScope(ctx, root, reviewScopeStaged)
	if !strings.Contains(changeContext, "# Change Under Review") {
		return nil, agenterrors.NewValidation("no staged changes to review", nil)
	}

	resolvedProvider, resolvedModel, systemPrompt, err := resolveSubagentProviderModel(a, personas.IDReviewer, true, root)
	if err != nil {
		return nil, err
	}

	task := "Review the staged change above for real problems before it is committed."
	if focus = strings.TrimSpace(focus); focus != "" {
		task += "\n\nFocus: " + focus
	}

	printSubagentStart(personas.IDReviewer, displayOrDefault(resolvedProvider), displayOrDefault(resolvedModel))
	run := a.GetSubagentRunner().Run(ctx, changeContext+"# Your Task\n\n"+task, SubagentOptions{
		Persona:      personas.IDReviewer,
		Provider:     resolvedProvider,
		Model:        resolvedModel,
		SystemPrompt: systemPrompt,
	})
	printSubagentDone(personas.IDReviewer, run)

	if run.TokensUsed > 0 || run.Cost > 0 {
		a.TrackMetricsFromResponse(0, 0, run.TokensUsed, run.Cost, 0, 0, 0)
	}
	if run.Error != nil && strings.TrimSpace(run.Output) == "" {
		return nil, agenterrors.Wrap(run.Error, "reviewer subagent")
	}

	return &StagedReviewResult{
		Verdict: parseReviewVerdict(run.Output),
		Report:  strings.TrimSpace(run.Output),
		Run:     run,
	}, nil
}

func parseReviewVerdict(report string) string {
	matches := reviewVerdictPattern.FindAllStringSubmatch(report, -1)
	if len(matches) == 0 {
		return "inconclusive"
	}
	if strings.EqualFold(matches[len(matches)-1][1], "APPROVE") {
		return "approved"
	}
	return "needs_revision"
}

func displayOrDefault(s string) string {
	if s == "" {
		return "default"
	}
	return s
}
