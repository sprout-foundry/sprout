//go:build !js

package cliui

// progress_summarizer.go — the optional model-written summary for the
// SP-151 progress events (SP-151 §151c, item 151.8). When the
// `summarizer` role is configured (SP-150), a short model-written
// summary is built only from event fields: the prompt carries the event
// type and the payload as deterministic JSON, nothing else, so the
// model cannot invent detail. The deterministic template
// (ProgressEventSummary, item 151.6) is the fallback: it is returned
// when the summarizer is not configured (nil client), when the model
// call errors or comes back empty, and — the invariant — when no
// passing verification exists, so a summary never states success
// unless a passing verification event does.
//
// The capability is opt-in: NewSummarizerModelClient returns nil unless
// the summarizer role is configured and a client can be created; a nil
// client means "use the deterministic template".

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/factory"
)

// ModelClient is the minimal model interface the progress summarizer
// needs: one system+user prompt to one completion. Callers adapt a
// concrete client (apiModelClient below); a fake keeps the summarizer
// testable without a real provider.
type ModelClient interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// SummarizeProgress returns a short summary of a progress event (SP-151
// §151c, item 151.8). It prefers the model-written summary when client
// is non-nil; the deterministic template (ProgressEventSummary) is
// returned when the client is nil (the summarizer role is not
// configured), when the event cannot claim success (see
// progressVerificationPassed), when the model call errors, or when the
// model output is empty.
func SummarizeProgress(ctx context.Context, client ModelClient, eventTypeName string, data map[string]interface{}) string {
	template := ProgressEventSummary(eventTypeName, data)
	if client == nil {
		return template
	}
	if !progressVerificationPassed(eventTypeName, data) {
		// The invariant: a failing or absent verification can never
		// yield a success summary. The template for these cases is
		// non-success ("Run complete — not verified (…)", "Checks:
		// 4/6 passed"), so falling back to it deterministically
		// guarantees the invariant — the model output is never
		// consulted for a success claim.
		return template
	}
	sys, user := progressSummaryPrompt(eventTypeName, data)
	if user == "" {
		// The prompt build failed (payload not JSON-marshalable) —
		// treat as an error path and fall back to the template.
		return template
	}
	out, err := client.Complete(ctx, sys, user)
	if err != nil {
		return template
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return template
	}
	return out
}

// progressVerificationPassed reports whether the event carries a
// passing verification result — the gate for the success invariant
// (SP-151 §151c, item 151.8): a summary never states success unless
// this returns true.
//
//   - progress_complete: the payload's "verified" flag (true only when
//     a passing verification result exists, SP-151 §151a).
//   - progress_verification: the payload's "passed" flag when present
//     as a bool; otherwise derived from "checks" — every non-skipped
//     check must have passed, and at least one non-skipped check must
//     have run (all skipped → false). A skipped check never counts as
//     passed, matching the 151.6 template.
//   - anything else (milestone, question): true — these are progress
//     notes, not final success claims, so the invariant does not
//     constrain them.
func progressVerificationPassed(eventTypeName string, data map[string]interface{}) bool {
	switch eventTypeName {
	case events.EventTypeProgressComplete:
		verified, _ := data["verified"].(bool)
		return verified
	case events.EventTypeProgressVerification:
		if passed, ok := data["passed"].(bool); ok {
			return passed
		}
		checks, _ := data["checks"].([]interface{})
		ran := false
		for _, c := range checks {
			check, _ := c.(map[string]interface{})
			skipped, _ := check["skipped"].(bool)
			if skipped {
				continue
			}
			ran = true
			if p, _ := check["passed"].(bool); !p {
				return false
			}
		}
		return ran
	default:
		return true
	}
}

// progressSummaryPrompt builds the model prompt for a progress event.
// The user prompt is built only from event fields — the event type name
// and the payload as deterministic JSON (encoding/json sorts map keys)
// — so the model has no material to invent detail. The system prompt
// additionally forbids stating success unless the verification result
// is passing. When the payload cannot be marshaled it returns ("", "")
// as the error signal; the caller falls back to the template.
func progressSummaryPrompt(eventTypeName string, data map[string]interface{}) (string, string) {
	payload, err := json.Marshal(data)
	if err != nil {
		return "", ""
	}
	system := "Summarize this agent run-progress event in ONE short, factual sentence. " +
		"Use ONLY the event fields provided; do not invent any detail. " +
		"Do not state success, completion, or that checks passed unless the verification result is passing."
	user := fmt.Sprintf("Event type: %s\nEvent fields (JSON): %s", eventTypeName, payload)
	return system, user
}

// NewSummarizerModelClient builds the optional model client for
// progress summaries (SP-151 §151c, item 151.8). It makes the model
// summary OPTIONAL: it returns nil when the `summarizer` role (SP-150)
// is not configured or when no client can be created for the resolved
// (provider, model) — in either case the caller uses the deterministic
// template.
func NewSummarizerModelClient(cfg *configuration.Config) ModelClient {
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
	return apiModelClient{c: client}
}

// apiModelClient adapts an api.ClientInterface to the summarizer's
// ModelClient: a single system+user chat request, trimmed text out.
type apiModelClient struct {
	c api.ClientInterface
}

func (m apiModelClient) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	messages := []api.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
	resp, err := m.c.SendChatRequest(ctx, messages, nil, "", false)
	if err != nil {
		return "", fmt.Errorf("progress summary model call: %w", err)
	}
	if resp == nil || len(resp.Choices) == 0 {
		return "", fmt.Errorf("progress summary model call: no choices in response")
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}
