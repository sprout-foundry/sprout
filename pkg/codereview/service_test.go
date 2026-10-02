package codereview

import (
	"context"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/factory"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

func TestPrepareReviewContextForPromptDropsFullFileContextFirst(t *testing.T) {
	cfg := &configuration.Config{}
	logger := utils.GetLogger(true)
	service := NewCodeReviewService(cfg, logger)

	ctx := &ReviewContext{
		Diff:            strings.Repeat("diff --git a/a.go b/a.go\n+line\n", 4000),
		FullFileContext: strings.Repeat("context line\n", 80000),
	}

	prepared := service.prepareReviewContextForPrompt(ctx)
	if prepared.FullFileContext != "" {
		t.Fatal("expected full file context to be dropped when prompt is too large")
	}
}

func TestPrepareReviewContextForPromptTruncatesDiffAsLastResort(t *testing.T) {
	cfg := &configuration.Config{}
	logger := utils.GetLogger(true)
	service := NewCodeReviewService(cfg, logger)

	largeDiff := strings.Repeat("diff --git a/a.go b/a.go\n@@\n+very long line content for review payload sizing\n", 20000)
	ctx := &ReviewContext{
		Diff: largeDiff,
	}

	prepared := service.prepareReviewContextForPrompt(ctx)
	if len(prepared.Diff) >= len(largeDiff) {
		t.Fatal("expected diff to be truncated for very large review payloads")
	}

	prompt := service.buildEnhancedReviewPrompt(prepared, false)
	if len(prompt) > maxReviewPromptBytes+1024 {
		t.Fatalf("expected prepared prompt to fit within budget, got %d bytes", len(prompt))
	}
}

func TestReviewPromptByteBudgetUsesModelContextLimit(t *testing.T) {
	cfg := &configuration.Config{}
	logger := utils.GetLogger(true)
	service := NewCodeReviewService(cfg, logger)
	client := &factory.TestClient{}

	ctx := &ReviewContext{
		Diff:        "small diff",
		AgentClient: client,
	}

	budget := service.reviewPromptByteBudget(ctx)
	// TestClient reports a 128K context window. reviewPromptByteBudget
	// computes usableTokens = 60% of 128000 = 76800, then
	// modelBudget = 76800 * 4 (estimatedCharsPerToken) = 307200.
	expected := 307200
	if budget != expected {
		t.Fatalf("expected model-aware budget %d, got %d", expected, budget)
	}
}

type fixedLimitClient struct {
	factory.TestClient
	limit int
}

func (c *fixedLimitClient) GetModelContextLimit() (int, error) {
	return c.limit, nil
}

func (c *fixedLimitClient) SendChatRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	return c.TestClient.SendChatRequest(context.Background(), messages, tools, reasoning, disableThinking)
}
