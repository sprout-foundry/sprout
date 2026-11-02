package agent

import (
	"context"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// streamModelText makes one plain-text model call for a tool (no tools, no
// conversation history) through the agent's own client, so the call takes
// the same route as the agent's turns: in the browser build that is the
// host's metered proxy. Visible text is passed to onChunk as it streams;
// reasoning text is not. Usage is booked under role so it shows in the
// session's cost totals.
//
// Thinking is turned off for these calls: they write a document whose first
// bytes the person is waiting to see, and a reasoning pass before the first
// byte defeats that. The calling tool owns the prompt that makes the output
// good without it.
func (a *Agent) streamModelText(ctx context.Context, role, system, user string, onChunk func(string)) (string, error) {
	client := a.getClient()
	if client == nil {
		return "", agenterrors.NewConfig("no model client is available for this call", nil)
	}
	messages := []api.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}
	var streamed strings.Builder
	resp, err := client.SendChatRequestStream(ctx, messages, nil, "", true, func(content, contentType string) {
		if content == "" || contentType == "reasoning" {
			return
		}
		streamed.WriteString(content)
		if onChunk != nil {
			onChunk(content)
		}
	})
	if resp != nil {
		a.BookRoleUsage(role, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.EstimatedCost)
	}
	text := streamed.String()
	// Some providers deliver the text only on the final response.
	if text == "" && resp != nil && len(resp.Choices) > 0 {
		text = resp.Choices[0].Message.Content
		if text != "" && onChunk != nil {
			onChunk(text)
		}
	}
	if err != nil {
		return text, err
	}
	return text, nil
}
