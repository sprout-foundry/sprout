package timeline

// timeline_summarizer.go — the optional model-written summary for a
// timeline entry. When the `summarizer` role is configured, a short
// model-written summary is built only from the entry's fields: the prompt
// carries the entry kind, its deterministic template summary, and the
// entry's own data as deterministic JSON, and nothing else, so the model
// cannot invent detail. The deterministic template summary
// (SummarizeRevision / SummarizeDeploy / SummarizeCheckpoint) is the
// fallback: it is returned when the summarizer is not configured (nil
// client), when the model call errors or times out, when the model output
// is empty, or when the entry carries nothing for a summary to be built
// from.
//
// The capability is opt-in: NewSummarizerModelClient returns nil unless
// the summarizer role is configured and a client can be created; a nil
// client means "use the deterministic template". The metered seam
// (SummarizeEntryWithUsageFn) reports whether the model was actually
// consulted so the caller books the summarizer role's usage only for a
// call that ran.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/factory"
	"github.com/sprout-foundry/sprout/pkg/providercatalog"
)

// summaryTimeout bounds the optional model-written summary call. The
// summary layers on top of the deterministic template, which is always
// available, so a slow or hung provider must never stall the caller that
// renders a timeline: the call is capped and the template is used when the
// cap elapses.
//
// It is a var (not a const) so tests can shrink it and exercise the
// timeout-fallback path in milliseconds instead of seconds.
var summaryTimeout = 5 * time.Second

// ModelClient is the minimal model interface the timeline summarizer
// needs: one system+user prompt to one completion. Callers adapt a
// concrete client (apiModelClient below); a fake keeps the summarizer
// testable without a real provider. Injecting this interface — rather than
// an agent or api client — keeps this package free of the heavy agent
// dependencies.
type ModelClient interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// SummaryUsage is the model usage one metered timeline-summary call
// incurred: the tokens sent and returned and the estimated cost, ready to
// be booked under the summarizer role. Cost is an estimate from the
// provider/model pricing catalog because the summary seam does not surface
// provider-reported billing.
type SummaryUsage struct {
	PromptTokens     int
	CompletionTokens int
	Cost             float64
}

// SummarizeEntry returns the short summary for a timeline entry: the
// model-written summary when client is non-nil and a summary can be
// produced, otherwise the entry's deterministic template summary. The
// entry is never mutated; the template summary is the floor, so a nil
// client, an error, a timeout, or empty model output all yield the same
// text the deterministic path would have rendered.
func SummarizeEntry(ctx context.Context, client ModelClient, entry Entry) string {
	summary, _ := summarizeEntry(ctx, client, entry)
	return summary
}

// summarizeEntry is SummarizeEntry plus a `used` flag: true iff the model
// was actually consulted and its non-empty answer was used. The metered
// seam needs that flag to decide whether to book usage — deriving it from
// output text would drop a booking whenever the model's answer happens to
// equal the template, and would book a call that never ran.
func summarizeEntry(ctx context.Context, client ModelClient, entry Entry) (string, bool) {
	template := entry.Summary()
	if client == nil {
		return template, false
	}
	if template == "" {
		// A malformed entry (or one whose template has nothing to say)
		// has no data for the model to summarize; the invariant is that
		// the model never fabricates an entry out of nothing.
		return template, false
	}
	sys, user := summaryPrompt(entry)
	if user == "" {
		// The prompt build failed (fields not JSON-marshalable) — treat
		// as an error path and fall back to the template.
		return template, false
	}
	// A hung summarizer cannot stall the caller: the call is bounded and
	// the template takes over when the bound elapses (the derived context
	// is cancelled then, so the client's in-flight call is interrupted
	// too).
	callCtx, cancel := context.WithTimeout(ctx, summaryTimeout)
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

// SummarizeEntryWithUsageFn is the metered entry point for the optional
// timeline summary. It resolves the `summarizer` role through the injected
// client builder (production passes NewSummarizerModelClient), calls the
// model through the bounded, template-fallback SummarizeEntry path, and
// returns the summary alongside the model usage the call incurred so the
// caller can attribute the spend to the summarizer role. A nil/unconfigured
// summarizer role — or any fallback to the template — returns the
// deterministic template with zero usage and books nothing, so the
// capability stays opt-in.
//
// It is the summarizer's role-metering seam: the caller books the returned
// usage under the summarizer role. The builder is a parameter so the
// contract is testable with a fake summarizer (no live model).
func SummarizeEntryWithUsageFn(ctx context.Context, cfg *configuration.Config, entry Entry, build func(*configuration.Config) ModelClient) (string, SummaryUsage) {
	template := entry.Summary()
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
	summary, used := summarizeEntry(ctx, client, entry)
	// Only a model call that actually ran and whose answer was used is
	// metered; a fallback to the template (nil client, error, timeout,
	// empty output, or nothing to summarize) books nothing.
	if !used {
		return template, SummaryUsage{}
	}
	return summary, estimateSummaryUsage(provider, model, client, entry)
}

// estimateSummaryUsage estimates the usage a metered summary call
// incurred: the prompt and completion token counts plus the cost from the
// provider/model pricing catalog. Token counts are approximate — a summary
// is a short prompt and a one-sentence answer — so the estimate is
// deliberately conservative and never zero for a used call: the metering
// invariant is that a model call shows up in the summarizer role's totals,
// not that the figure is exact.
func estimateSummaryUsage(provider, model string, client ModelClient, entry Entry) SummaryUsage {
	// Prompt: the system prompt plus the entry-kind line, the
	// deterministic summary line, and the payload JSON. Completion: the
	// model's answer. Roughly 4 characters per token.
	promptChars := len(summarySystemPrompt) + len(entry.Summary()) + len(summaryEntryFields(entry)) + 64
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

// summarySystemPrompt is the system instruction the summarizer prompt
// carries. It is a named constant so the usage estimate can size the
// prompt without rebuilding it. It forbids inventing detail and forbids
// deviating from the deterministic facts the template already carries.
const summarySystemPrompt = "Summarize this project-timeline entry in ONE short, factual sentence. " +
	"Use ONLY the entry fields provided; do not invent any detail. " +
	"Do not change the outcome the deterministic summary states."

// summaryEntryFields renders the entry-fields portion of the prompt (the
// entry kind and its data as JSON) for the usage estimate. An entry that
// cannot be marshaled contributes nothing.
func summaryEntryFields(entry Entry) string {
	payload, err := json.Marshal(entryPayload(entry))
	if err != nil {
		return ""
	}
	return string(payload)
}

// entryPayload is the entry's data as a plain map, built only from the
// entry's own fields. A change set carries its revision's diff facts and
// scope IDs; a deploy carries the deploy's own fields; a checkpoint
// carries the checkpoint's fields. A malformed entry yields nil (the caller
// then has nothing to summarize and falls back to the template).
func entryPayload(entry Entry) map[string]interface{} {
	switch entry.Kind() {
	case KindChangeSet:
		cs := entry.ChangeSet
		return map[string]interface{}{
			"kind":          string(KindChangeSet),
			"revision_id":   cs.RevisionID,
			"files":         cs.Files,
			"files_touched": cs.FilesTouched,
			"insertions":    cs.Insertions,
			"deletions":     cs.Deletions,
			"scope_ids":     cs.ScopeIDs,
			"summary":       cs.Summary,
		}
	case KindDeploy:
		d := entry.Deploy.Deployment
		return map[string]interface{}{
			"kind":    string(KindDeploy),
			"id":      d.ID,
			"version": d.Version,
			"status":  string(d.Status),
			"summary": entry.Deploy.Summary,
		}
	case KindCheckpoint:
		cp := entry.Checkpoint.Checkpoint
		return map[string]interface{}{
			"kind":        string(KindCheckpoint),
			"id":          cp.ID,
			"origin":      string(cp.Origin),
			"revision_id": cp.RevisionID,
			"files":       cp.Files,
			"scope_ids":   cp.ScopeIDs,
			"summary":     entry.Checkpoint.Summary,
		}
	default:
		return nil
	}
}

// summaryPrompt builds the model prompt for a timeline entry. The user
// prompt is built only from the entry's fields — the entry kind, the
// deterministic template summary, and the entry data as deterministic JSON
// (encoding/json sorts map keys) — so the model has no material to invent
// detail. When the entry carries nothing to summarize, or the payload
// cannot be marshaled, it returns ("", "") as the error signal; the caller
// falls back to the template.
func summaryPrompt(entry Entry) (string, string) {
	payload := entryPayload(entry)
	if payload == nil {
		return "", ""
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", ""
	}
	user := fmt.Sprintf("Entry kind: %s\nDeterministic summary: %s\nEntry fields (JSON): %s",
		entry.Kind(), entry.Summary(), raw)
	return summarySystemPrompt, user
}

// NewSummarizerModelClient builds the optional model client for timeline
// summaries. It makes the model summary OPTIONAL: it returns nil when the
// `summarizer` role is not configured or when no client can be created for
// the resolved (provider, model) — in either case the caller uses the
// deterministic template.
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

// LastOutput returns the text of the most recent completion, for the usage
// estimate. Empty before the first call.
func (m *apiModelClient) LastOutput() string { return m.lastOutput }

func (m *apiModelClient) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	messages := []api.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
	resp, err := m.c.SendChatRequest(ctx, messages, nil, "", false)
	if err != nil {
		return "", fmt.Errorf("timeline summary model call: %w", err)
	}
	if resp == nil || len(resp.Choices) == 0 {
		return "", fmt.Errorf("timeline summary model call: no choices in response")
	}
	out := strings.TrimSpace(resp.Choices[0].Message.Content)
	m.lastOutput = out
	return out, nil
}
