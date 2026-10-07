//go:build !js

package cliui

// progress_summarizer_wiring.go — the CLI-side wiring of the optional
// model-written progress summary: the accessors the terminal subscriber
// uses to reach the summarizer and the metering callback, and the
// terminal render path that calls the summarizer where the spec says
// (the progress-event handler).

import (
	"context"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// SetProgressSummarizerModelClient overrides the model client the
// subscriber uses for the model-written progress summary. Tests
// inject a stub; production leaves it nil so the client is resolved from
// the `summarizer` role at summary time.
func (s *TerminalSubscriberState) SetProgressSummarizerModelClient(client ModelClient) {
	s.summaryModelClient = client
}

// SetProgressSummarizerUsage overrides where the subscriber books the
// summarizer role's model usage. Tests inject a recorder; production
// leaves it nil so the usage is booked into the chat agent's per-role
// metrics.
func (s *TerminalSubscriberState) SetProgressSummarizerUsage(record func(SummaryUsage)) {
	s.summaryUsage = record
}

// progressConfig returns the subscriber's live configuration, or nil when
// no config manager is attached (non-agent callers / tests).
func (s *TerminalSubscriberState) progressConfig() *configuration.Config {
	if s.configMgr == nil {
		return nil
	}
	return s.configMgr.GetConfig()
}

// summarizeProgressEvent renders the one-line summary for a progress
// event. When the summarizer role is configured it calls the model
// through the role-metered seam and books the usage under the summarizer
// role; otherwise, and on any error or timeout, it renders the
// deterministic template. Either way the text is built only from the
// event fields, and a success claim requires a passing verification (the
// template gate in SummarizeProgress).
func (s *TerminalSubscriberState) summarizeProgressEvent(evtType string, data map[string]interface{}) string {
	cfg := s.progressConfig()
	if cfg == nil && s.summaryModelClient == nil {
		return ProgressEventSummary(evtType, data)
	}
	build := NewSummarizerModelClient
	if s.summaryModelClient != nil {
		build = func(*configuration.Config) ModelClient { return s.summaryModelClient }
	}
	// The call is bounded by SummarizeProgress's own timeout, so a
	// background parent is correct: the summary must not be cancelled by
	// an unrelated caller context and must never outlive its own timeout.
	summary, usage := SummarizeProgressWithUsageFn(context.Background(), cfg, evtType, data, build)
	if usage != (SummaryUsage{}) {
		s.bookSummaryUsage(usage)
	}
	return summary
}

// bookSummaryUsage attributes a progress summary's model usage to the
// summarizer role. A test recorder wins; otherwise the usage is booked
// into the chat agent's per-role metrics, so /cost-style views and the
// usage ledger see the summarizer's spend. A nil agent and nil recorder
// leave the usage unbooked (non-agent callers).
func (s *TerminalSubscriberState) bookSummaryUsage(usage SummaryUsage) {
	if s.summaryUsage != nil {
		s.summaryUsage(usage)
		return
	}
	if s.chatAgent == nil {
		return
	}
	s.chatAgent.BookRoleUsage(configuration.RoleSummarizer, usage.PromptTokens, usage.CompletionTokens, usage.Cost)
}
