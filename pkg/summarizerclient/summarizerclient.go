// Package summarizerclient provides the optional model client shared by the
// summarizers that write a short, factual summary of an event or entry.
//
// The summarizers themselves (timeline entries, run-progress events) differ in
// their prompts and their fallback invariants, but the transport is the same:
// resolve the `summarizer` role, build a provider client, send one
// system+user prompt, and return the trimmed completion. This package holds
// that transport so the two summarizers cannot drift apart in it.
package summarizerclient

import (
	"context"
	"fmt"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/factory"
)

// Client is the minimal model interface a summarizer needs: one system+user
// prompt to one completion. Injecting this interface — rather than an agent or
// api client — keeps the summarizers testable without a real provider.
type Client interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// New builds the optional model client. It returns nil when the `summarizer`
// role is not configured or when no client can be created for the resolved
// (provider, model) — in either case the caller falls back to its
// deterministic template.
func New(cfg *configuration.Config) Client {
	if cfg == nil {
		return nil
	}
	role := cfg.GetRole(configuration.RoleSummarizer)
	if role.Provider == "" && role.Model == "" {
		return nil
	}
	provider, model := cfg.ResolveRole(configuration.RoleSummarizer)
	if provider == "" {
		return nil
	}
	client, err := factory.CreateProviderClient(api.ClientType(provider), model)
	if client == nil || err != nil {
		return nil
	}
	return &apiClient{c: client}
}

// apiClient adapts an api.ClientInterface to Client. It records the last
// completion text so a caller's metering seam can size the completion for its
// usage estimate.
type apiClient struct {
	c          api.ClientInterface
	lastOutput string
}

// LastOutput returns the text of the most recent completion, for the usage
// estimate. Empty before the first call.
func (m *apiClient) LastOutput() string { return m.lastOutput }

func (m *apiClient) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	messages := []api.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
	resp, err := m.c.SendChatRequest(ctx, messages, nil, "", false)
	if err != nil {
		return "", fmt.Errorf("summary model call: %w", err)
	}
	if resp == nil || len(resp.Choices) == 0 {
		return "", fmt.Errorf("summary model call: no choices in response")
	}
	out := strings.TrimSpace(resp.Choices[0].Message.Content)
	m.lastOutput = out
	return out, nil
}

// LastOutputOf reports the last completion text of a Client, when the client
// exposes it. Callers use it to size the completion for a usage estimate; a
// client that does not track it (such as a test fake) reports "".
func LastOutputOf(c Client) string {
	if l, ok := c.(interface{ LastOutput() string }); ok {
		return l.LastOutput()
	}
	return ""
}
