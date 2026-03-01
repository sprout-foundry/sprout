package agent

import (
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/factory"
)

type reasoningProviderClient struct {
	*factory.TestClient
	provider string
	model    string
}

func (c *reasoningProviderClient) GetProvider() string {
	return c.provider
}

func (c *reasoningProviderClient) GetModel() string {
	return c.model
}

func TestDetermineReasoningEffort_CapsHighForGptOSS(t *testing.T) {
	agent := &Agent{
		client: &reasoningProviderClient{
			TestClient: &factory.TestClient{},
			provider:   "openai",
			model:      "gpt-oss:20b",
		},
	}

	messages := []api.Message{
		{
			Role: "user",
			Content: "Please analyze and debug this complex extraction workflow, compare approaches, evaluate trade-offs, " +
				"and implement a robust fix for repeated tool-call failures with detailed validation.",
		},
	}

	if got := agent.determineReasoningEffort(messages); got != "medium" {
		t.Fatalf("expected gpt-oss high effort to be capped to medium, got %q", got)
	}
}

func TestDetermineReasoningEffort_KeepsHighForNonGptOSS(t *testing.T) {
	agent := &Agent{
		client: &reasoningProviderClient{
			TestClient: &factory.TestClient{},
			provider:   "openai",
			model:      "gpt-4o-mini",
		},
	}

	messages := []api.Message{
		{
			Role: "user",
			Content: "Please analyze and debug this complex extraction workflow, compare approaches, evaluate trade-offs, " +
				"and implement a robust fix for repeated tool-call failures with detailed validation.",
		},
	}

	if got := agent.determineReasoningEffort(messages); got != "high" {
		t.Fatalf("expected non-gpt-oss model to keep high effort, got %q", got)
	}
}

func TestDetermineReasoningEffort_TemplatePayloadUsesMedium(t *testing.T) {
	agent := &Agent{
		client: &reasoningProviderClient{
			TestClient: &factory.TestClient{},
			provider:   "openai",
			model:      "gpt-4o-mini",
		},
	}

	longTemplatePrompt := `
# Agentic Restaurant Extraction Prompt
## OCR Trigger Policy (MANDATORY)
## Output Directory Layout
## Common JSON Envelope (all JSON files)
### Canonical structured tool calls
## Schema: Organization
## Schema: Menu
## Schema: Offer

Please analyze and implement extraction for this site.
`

	messages := []api.Message{
		{Role: "user", Content: longTemplatePrompt},
	}

	if got := agent.determineReasoningEffort(messages); got != "medium" {
		t.Fatalf("expected template-style payload to resolve to medium effort, got %q", got)
	}
}
