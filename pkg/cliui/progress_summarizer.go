//go:build !js

package cliui

// progress_summarizer.go — the optional model-written summary for the
// progress events. When the
// `summarizer` role is configured, a short model-written
// summary is built only from event fields: the prompt carries the event
// type and the payload as deterministic JSON, nothing else, so the
// model cannot invent detail. The deterministic template
// (ProgressEventSummary) is the fallback: it is returned
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
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/factory"
	"github.com/sprout-foundry/sprout/pkg/providercatalog"
)

// progressSummaryTimeout bounds the optional model-written summary call.
// The summary layers on top of the deterministic
// template, which is always available, so a slow or hung provider must
// never stall the turn or the event stream: the call is capped and the
// template is used when the cap elapses. Kept short — a one-sentence
// summary is not worth making the user wait.
//
// It is a var (not a const) so tests can shrink it and exercise the
// timeout-fallback path in milliseconds instead of seconds.
var progressSummaryTimeout = 5 * time.Second

// ModelClient is the minimal model interface the progress summarizer
// needs: one system+user prompt to one completion. Callers adapt a
// concrete client (apiModelClient below); a fake keeps the summarizer
// testable without a real provider.
type ModelClient interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// SummarizeProgress returns a short summary of a progress event. It
// prefers the model-written summary when client
// is non-nil; the deterministic template (ProgressEventSummary) is
// returned when the client is nil (the summarizer role is not
// configured), when the event cannot claim success (see
// progressVerificationPassed), when the model call errors or times out,
// or when the model output is empty.
func SummarizeProgress(ctx context.Context, client ModelClient, eventTypeName string, data map[string]interface{}) string {
	summary, _ := summarizeProgress(ctx, client, eventTypeName, data)
	return summary
}

// summarizeProgress is SummarizeProgress plus a `used` flag: true iff the
// model was actually consulted and its non-empty answer was used. The
// metered seam needs that flag to decide whether to book usage — deriving
// it from output text would drop a booking whenever the model's answer
// happens to equal the template, and would book a call that never ran.
func summarizeProgress(ctx context.Context, client ModelClient, eventTypeName string, data map[string]interface{}) (string, bool) {
	template := ProgressEventSummary(eventTypeName, data)
	if client == nil {
		return template, false
	}
	if !progressVerificationPassed(eventTypeName, data) {
		// The invariant: a failing or absent verification can never
		// yield a success summary. The template for these cases is
		// non-success ("Run complete — not verified (…)", "Checks:
		// 4/6 passed"), so falling back to it deterministically
		// guarantees the invariant — the model output is never
		// consulted for a success claim.
		return template, false
	}
	sys, user := progressSummaryPrompt(eventTypeName, data)
	if user == "" {
		// The prompt build failed (payload not JSON-marshalable) —
		// treat as an error path and fall back to the template.
		return template, false
	}
	// A hung summarizer cannot stall the turn: the call is bounded, and
	// the template takes over when the bound elapses (the derived
	// context is cancelled then, so the client's in-flight call is
	// interrupted too).
	callCtx, cancel := context.WithTimeout(ctx, progressSummaryTimeout)
	defer cancel()
	out, err := client.Complete(callCtx, sys, user)
	if err != nil {
		return template, false
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return template, false
	}
	return out, true
}

// SummarizeProgressWithUsageFn is the metered entry point for the
// optional progress summary. It resolves the
// `summarizer` role through the injected client
// builder (production passes NewSummarizerModelClient), calls the model
// through the bounded, template-fallback SummarizeProgress path, and
// returns the summary alongside the model usage the call incurred so the
// caller can attribute the spend to the summarizer role.
// A nil/unconfigured summarizer role — or any fallback to the template —
// returns the deterministic template with zero usage and books nothing,
// so the capability stays opt-in.
//
// It is the summarizer's role-metering seam: the caller (the turn's
// progress-event path, with the parent agent's per-role metrics) books
// the returned usage under the summarizer role, so /cost-style views and
// the usage ledger attribute the spend to the summary purpose rather
// than to the main coder loop. The builder is a parameter so the
// contract is testable with a fake summarizer (no live model).
func SummarizeProgressWithUsageFn(ctx context.Context, cfg *configuration.Config, eventTypeName string, data map[string]interface{}, build func(*configuration.Config) ModelClient) (string, SummaryUsage) {
	template := ProgressEventSummary(eventTypeName, data)
	if build == nil {
		return template, SummaryUsage{}
	}
	client := build(cfg)
	if client == nil {
		return template, SummaryUsage{}
	}
	provider, model := "", ""
	if cfg != nil {
		provider, model = cfg.ResolveRole(configuration.RoleSummarizer)
	}
	summary, used := summarizeProgress(ctx, client, eventTypeName, data)
	// Only a model call that actually ran and whose answer was used is
	// metered; a fallback to the template (nil client, error, timeout,
	// empty output, or the no-passing-verification invariant) books
	// nothing.
	if !used {
		return template, SummaryUsage{}
	}
	return summary, estimateSummaryUsage(provider, model, client, data)
}

// SummaryUsage is the model usage one metered progress-summary call
// incurred: the tokens sent and returned and the estimated cost, ready to
// be booked under the summarizer role. Cost is an
// estimate from the provider/model pricing catalog because the summary
// seam does not surface provider-reported billing.
type SummaryUsage struct {
	PromptTokens     int
	CompletionTokens int
	Cost             float64
}

// estimateSummaryUsage estimates the usage a metered summary call
// incurred: the prompt and completion token counts plus the cost from the
// provider/model pricing catalog. Token counts are approximate — a
// summary is a short prompt and a one-sentence answer — so the estimate
// is deliberately conservative and never zero for a used call: the
// metering invariant is that a model call shows up in the summarizer
// role's totals, not that the figure is exact.
func estimateSummaryUsage(provider, model string, client ModelClient, data map[string]interface{}) SummaryUsage {
	// Prompt: the system prompt plus the event-type line and the payload
	// JSON. Completion: the model's answer. Roughly 4 characters per
	// token.
	promptChars := len(summarySystemPrompt) + len(summaryEventFields(data)) + 64
	completionChars := 64
	if truer, ok := client.(interface{ LastOutput() string }); ok {
		if out := truer.LastOutput(); out != "" {
			completionChars = len(out)
		}
	}
	usage := SummaryUsage{
		PromptTokens:     promptChars / 4,
		CompletionTokens: completionChars / 4,
	}
	if usage.PromptTokens == 0 {
		usage.PromptTokens = 1
	}
	if usage.CompletionTokens == 0 {
		usage.CompletionTokens = 1
	}
	if inPerM, outPerM, _, ok := providercatalog.FindModelPricing(provider, model); ok {
		usage.Cost = float64(usage.PromptTokens)/1e6*inPerM + float64(usage.CompletionTokens)/1e6*outPerM
	}
	return usage
}

// summarySystemPrompt mirrors the system instruction the summarizer
// prompt carries (progressSummaryPrompt); kept as a named constant so the
// usage estimate can size the prompt without rebuilding it.
const summarySystemPrompt = "Summarize this agent run-progress event in ONE short, factual sentence. " +
	"Use ONLY the event fields provided; do not invent any detail. " +
	"Do not state success, completion, or that checks passed unless the verification result is passing."

// summaryEventFields renders the event-fields portion of the prompt
// (the event type and payload JSON) for the usage estimate. A payload
// that cannot be marshaled contributes nothing.
func summaryEventFields(data map[string]interface{}) string {
	payload, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	return string(payload)
}

// progressVerificationPassed reports whether the event carries a
// passing verification result — the gate for the success invariant:
// a summary never states success unless this returns true.
//
//   - progress_complete: the payload's "verified" flag (true only when
//     a passing verification result exists).
//   - progress_verification: the payload's "passed" flag when present
//     as a bool; otherwise derived from "checks" — every non-skipped
//     check must have passed, and at least one non-skipped check must
//     have run (all skipped → false). A skipped check never counts as
//     passed, matching the completion template.
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
	system := summarySystemPrompt
	user := fmt.Sprintf("Event type: %s\nEvent fields (JSON): %s", eventTypeName, payload)
	return system, user
}

// NewSummarizerModelClient builds the optional model client for
// progress summaries. It makes the model
// summary OPTIONAL: it returns nil when the `summarizer` role
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
	return &apiModelClient{c: client}
}

// apiModelClient adapts an api.ClientInterface to the summarizer's
// ModelClient: a single system+user chat request, trimmed text out. It
// records the last completion text so the metering seam can size the
// completion for its usage estimate.
type apiModelClient struct {
	c          api.ClientInterface
	lastOutput string
}

// LastOutput returns the text of the most recent completion, for the
// usage estimate. Empty before the first call.
func (m *apiModelClient) LastOutput() string { return m.lastOutput }

func (m *apiModelClient) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
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
	out := strings.TrimSpace(resp.Choices[0].Message.Content)
	m.lastOutput = out
	return out, nil
}
