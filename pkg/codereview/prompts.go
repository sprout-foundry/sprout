package codereview

import (
	"fmt"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/types"
)

// performStagedReview handles reviews of Git staged changes
func (s *CodeReviewService) performStagedReview(ctx *ReviewContext) (*types.CodeReviewResult, error) {
	// Use enhanced agent-based review with workspace intelligence
	return s.performAgentBasedCodeReview(ctx, false) // human-readable format for staged changes
}

// performAgentBasedCodeReview performs code review using the agent API with enhanced context
func (s *CodeReviewService) performAgentBasedCodeReview(ctx *ReviewContext, structured bool) (*types.CodeReviewResult, error) {
	if ctx.AgentClient == nil {
		return nil, fmt.Errorf("agent client not available for enhanced code review")
	}

	// Build enhanced review prompt with workspace context
	prompt := s.buildPreparedReviewPrompt(ctx, structured, false)

	// Create messages for agent API
	messages := []api.Message{
		{
			Role:    "user",
			Content: prompt,
		},
	}

	// Make agent API call
	response, err := ctx.AgentClient.SendChatRequest(messages, nil, "")
	if err != nil {
		return nil, fmt.Errorf("agent API call failed: %w", err)
	}

	// Parse response based on format
	if structured {
		return s.parseStructuredReviewResponse(response)
	} else {
		return s.parseHumanReadableReviewResponse(response)
	}
}

// performDeepAgentBasedCodeReview runs a stricter evidence-focused review and requires JSON output.
func (s *CodeReviewService) performDeepAgentBasedCodeReview(ctx *ReviewContext) (*types.CodeReviewResult, error) {
	if ctx.AgentClient == nil {
		return nil, fmt.Errorf("agent client not available for deep code review")
	}

	prompt := s.buildPreparedReviewPrompt(ctx, false, true)
	messages := []api.Message{
		{
			Role:    "user",
			Content: prompt,
		},
	}

	response, err := ctx.AgentClient.SendChatRequest(messages, nil, "")
	if err != nil {
		return nil, fmt.Errorf("agent API call failed: %w", err)
	}

	result, err := s.parseStructuredReviewResponse(response)
	if err != nil {
		return nil, err
	}

	status := strings.ToLower(strings.TrimSpace(result.Status))
	switch status {
	case "approved", "needs_revision", "rejected":
		result.Status = status
	default:
		result.Status = "needs_revision"
		if strings.TrimSpace(result.Feedback) == "" {
			result.Feedback = "Deep review returned an invalid status; manual review recommended."
		}
	}

	return result, nil
}

func (s *CodeReviewService) buildPreparedReviewPrompt(ctx *ReviewContext, structured bool, deep bool) string {
	prepared := s.prepareReviewContextForPrompt(ctx)
	if deep {
		return s.buildDeepReviewPrompt(prepared)
	}
	return s.buildEnhancedReviewPrompt(prepared, structured)
}

func (s *CodeReviewService) prepareReviewContextForPrompt(ctx *ReviewContext) *ReviewContext {
	if ctx == nil {
		return nil
	}

	prepared := *ctx
	if len(ctx.RelatedFiles) > 0 {
		prepared.RelatedFiles = append([]string(nil), ctx.RelatedFiles...)
	}

	prepared.CommitMessage = truncateForPromptSection(prepared.CommitMessage, maxReviewMetadataFieldBytes, "commit message")
	prepared.KeyComments = truncateForPromptSection(prepared.KeyComments, maxReviewMetadataFieldBytes, "key comments")
	prepared.ChangeCategories = truncateForPromptSection(prepared.ChangeCategories, maxReviewMetadataFieldBytes, "change categories")
	prepared.OriginalPrompt = truncateForPromptSection(prepared.OriginalPrompt, maxReviewMetadataFieldBytes, "original request")
	prepared.ProcessedInstructions = truncateForPromptSection(prepared.ProcessedInstructions, maxReviewMetadataFieldBytes, "processed instructions")

	if len(prepared.RelatedFiles) > maxReviewRelatedFiles {
		omitted := len(prepared.RelatedFiles) - maxReviewRelatedFiles
		prepared.RelatedFiles = append(prepared.RelatedFiles[:maxReviewRelatedFiles], fmt.Sprintf("... (%d additional related files omitted)", omitted))
	}

	promptBudget := s.reviewPromptByteBudget(&prepared)
	promptDirty := true
	prompt := ""
	rebuildPrompt := func() string {
		if promptDirty {
			prompt = s.buildEnhancedReviewPrompt(&prepared, false)
			promptDirty = false
		}
		return prompt
	}

	prompt = rebuildPrompt()
	if len(prompt) <= promptBudget {
		return &prepared
	}

	if prepared.FullFileContext != "" {
		prepared.FullFileContext = ""
		promptDirty = true
		prompt = rebuildPrompt()
	}

	if len(prompt) <= promptBudget {
		return &prepared
	}

	if len(prepared.RelatedFiles) > 0 {
		prepared.RelatedFiles = nil
		promptDirty = true
		prompt = rebuildPrompt()
	}

	if len(prompt) <= promptBudget {
		return &prepared
	}

	prepared.KeyComments = ""
	prepared.ChangeCategories = ""
	prepared.CommitMessage = truncateForPromptSection(prepared.CommitMessage, 4*1024, "commit message")
	prepared.OriginalPrompt = truncateForPromptSection(prepared.OriginalPrompt, 4*1024, "original request")
	prepared.ProcessedInstructions = truncateForPromptSection(prepared.ProcessedInstructions, 4*1024, "processed instructions")
	promptDirty = true
	prompt = rebuildPrompt()

	if len(prompt) <= promptBudget {
		return &prepared
	}

	overheadCtx := prepared
	overheadCtx.Diff = ""
	overheadCtx.FullFileContext = ""
	overheadCtx.RelatedFiles = nil
	overhead := len(s.buildEnhancedReviewPrompt(&overheadCtx, false))
	remaining := promptBudget - overhead - len("\n## Code Changes to Review\n```diff\n\n```")
	if remaining < 8*1024 {
		remaining = 8 * 1024
	}
	prepared.Diff = truncateForPromptSection(prepared.Diff, remaining, "diff")

	return &prepared
}

func (s *CodeReviewService) reviewPromptByteBudget(ctx *ReviewContext) int {
	budget := maxReviewPromptBytes
	if ctx == nil || ctx.AgentClient == nil {
		return budget
	}

	contextLimit, err := ctx.AgentClient.GetModelContextLimit()
	if err != nil || contextLimit <= 0 {
		return budget
	}

	usableTokens := int(float64(contextLimit) * 0.60)
	if usableTokens > contextLimit-2048 {
		usableTokens = contextLimit - 2048
	}
	if usableTokens < 2048 {
		usableTokens = contextLimit / 2
	}
	if usableTokens < 1024 {
		usableTokens = 1024
	}

	modelBudget := usableTokens * estimatedCharsPerToken
	if modelBudget < budget {
		return modelBudget
	}
	return budget
}

func truncateForPromptSection(content string, maxBytes int, label string) string {
	if maxBytes <= 0 || len(content) <= maxBytes {
		return content
	}
	if maxBytes < 128 {
		return content[:maxBytes]
	}

	headBytes := int(float64(maxBytes) * 0.7)
	tailBytes := maxBytes - headBytes
	notice := fmt.Sprintf("\n... [%s truncated for payload size] ...\n", label)
	if headBytes+tailBytes+len(notice) > maxBytes {
		tailBytes = maxBytes - headBytes - len(notice)
		if tailBytes < 0 {
			tailBytes = 0
			headBytes = maxBytes - len(notice)
			if headBytes < 0 {
				headBytes = maxBytes
				notice = ""
			}
		}
	}

	head := content[:headBytes]
	tail := ""
	if tailBytes > 0 {
		tail = content[len(content)-tailBytes:]
	}
	return head + notice + tail
}

// buildEnhancedReviewPrompt builds a review prompt with workspace intelligence and context
func (s *CodeReviewService) buildEnhancedReviewPrompt(ctx *ReviewContext, structured bool) string {
	var promptParts []string

	// Add base prompt based on review type
	if structured {
		promptParts = append(promptParts, "Please perform a structured code review of the following changes.")
	} else {
		promptParts = append(promptParts, prompts.CodeReviewStagedPrompt())
	}

	// Add metadata sections FIRST (before the diff) to help LLM understand intent
	// These are CRITICAL for avoiding false positives

	// Project type
	if ctx.ProjectType != "" {
		promptParts = append(promptParts, fmt.Sprintf("\n## Project Type\n%s", ctx.ProjectType))
	}

	// Commit message/intent
	if ctx.CommitMessage != "" {
		promptParts = append(promptParts, fmt.Sprintf("\n## Commit Message (Intent)\n%s", ctx.CommitMessage))
	}

	// Key code comments that explain WHY
	if ctx.KeyComments != "" {
		promptParts = append(promptParts, fmt.Sprintf("\n## Key Code Comments (Context)\n%s", ctx.KeyComments))
	}

	// Change categories
	if ctx.ChangeCategories != "" {
		promptParts = append(promptParts, fmt.Sprintf("\n## Change Categories\n%s", ctx.ChangeCategories))
	}

	// Add related files context if available
	if len(ctx.RelatedFiles) > 0 {
		promptParts = append(promptParts, fmt.Sprintf("\n## Related Files to Consider\nThe following files may be affected by or related to these changes:\n%s", strings.Join(ctx.RelatedFiles, "\n")))
	}

	// Add original prompt context
	if ctx.OriginalPrompt != "" {
		promptParts = append(promptParts, fmt.Sprintf("\n## Original Request\n%s", ctx.OriginalPrompt))
	}

	// Add processed instructions if available
	if ctx.ProcessedInstructions != "" {
		promptParts = append(promptParts, fmt.Sprintf("\n## Processed Instructions\n%s", ctx.ProcessedInstructions))
	}

	// Add full file context if available
	if ctx.FullFileContext != "" {
		promptParts = append(promptParts, fmt.Sprintf("\n## Full File Context\n%s", ctx.FullFileContext))
	}

	// Add the diff to review (LAST, after all context)
	promptParts = append(promptParts, fmt.Sprintf("\n## Code Changes to Review\n```diff\n%s\n```", ctx.Diff))

	return strings.Join(promptParts, "\n")
}

func (s *CodeReviewService) buildDeepReviewPrompt(ctx *ReviewContext) string {
	basePrompt := s.buildEnhancedReviewPrompt(ctx, false)

	return basePrompt + `

## Deep Review Requirements

Perform an evidence-based review focused on reducing false positives.

- Only report an issue when you can point to concrete evidence in the provided diff/context.
- If evidence is incomplete, classify as "verify" in guidance rather than presenting as a confirmed defect.
- Prefer high-signal findings (correctness, security, data loss, concurrency, API contract regressions).
- Ignore stylistic nits unless they create operational risk.
- Include line/file references where possible.

Return ONLY valid JSON using:
{
  "status": "approved|needs_revision|rejected",
  "feedback": "short summary",
  "detailed_guidance": "findings grouped as MUST_FIX and VERIFY, each with evidence and suggested next step"
}`
}
