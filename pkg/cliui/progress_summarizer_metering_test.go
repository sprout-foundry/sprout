//go:build !js

package cliui

// progress_summarizer_metering_test.go — the timeout bound and the
// role-metering seam for the optional model-written progress summaries:
// a hung summarizer falls back to the deterministic template instead of
// stalling the turn, and a configured summarizer role's call is returned
// with its usage so the caller can book it under that role.

import (
	"context"
	"strings"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestSummarizeProgressTimeoutFallsBackToTemplate proves a hung
// summarizer cannot stall the turn/stream: SummarizeProgress bounds the
// call and returns the deterministic template when the bound elapses.
// The fake's Complete blocks until its derived context is cancelled, so
// the timeout path is exercised without a real sleep or provider.
func TestSummarizeProgressTimeoutFallsBackToTemplate(t *testing.T) {
	// Shrink the bound so the timeout path is exercised in milliseconds
	// rather than the production 5s.
	orig := progressSummaryTimeout
	progressSummaryTimeout = 50 * time.Millisecond
	t.Cleanup(func() { progressSummaryTimeout = orig })

	fake := &fakeModelClient{result: "should never be returned", block: make(chan struct{})}
	start := time.Now()
	got := SummarizeProgress(context.Background(), fake, events.EventTypeProgressComplete, summarizerCompleteVerified)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("SummarizeProgress took %s, want it bounded near %s", elapsed, progressSummaryTimeout)
	}
	want := "Run complete — verified (Checks: 3/3 passed)"
	if got != want {
		t.Errorf("SummarizeProgress() on timeout = %q, want the template %q", got, want)
	}
	if fake.calls != 1 {
		t.Errorf("Complete calls = %d, want 1", fake.calls)
	}
}

// TestSummarizeProgressWithUsageFnMetersSummarizerRole proves the
// metering rule: a configured summarizer role calls the model and
// the call's usage is returned to be booked under the summarizer role,
// while an unconfigured role uses the template with zero
// usage and no call.
func TestSummarizeProgressWithUsageFnMetersSummarizerRole(t *testing.T) {
	t.Run("configured role meters the call", func(t *testing.T) {
		fake := &fakeModelClient{result: "Run complete — all checks green."}
		cfg := &configuration.Config{}
		cfg.Roles = map[string]configuration.RoleConfig{
			configuration.RoleSummarizer: {Provider: string(api.TestClientType), Model: "summarizer-model"},
		}
		got, usage := SummarizeProgressWithUsageFn(context.Background(), cfg, events.EventTypeProgressComplete, summarizerCompleteVerified,
			func(*configuration.Config) ModelClient { return fake })
		if got != "Run complete — all checks green." {
			t.Errorf("summary = %q, want the model output", got)
		}
		if fake.calls != 1 {
			t.Fatalf("Complete calls = %d, want 1", fake.calls)
		}
		if usage.PromptTokens <= 0 || usage.CompletionTokens <= 0 {
			t.Errorf("usage = %+v, want positive token counts", usage)
		}
	})

	t.Run("model answer equal to the template still meters", func(t *testing.T) {
		// The metering decision keys on whether the model was consulted,
		// not on text equality: a model that happens to echo the template
		// must still book its usage.
		template := "Run complete — verified (Checks: 3/3 passed)"
		fake := &fakeModelClient{result: template}
		cfg := &configuration.Config{}
		cfg.Roles = map[string]configuration.RoleConfig{
			configuration.RoleSummarizer: {Provider: string(api.TestClientType), Model: "summarizer-model"},
		}
		got, usage := SummarizeProgressWithUsageFn(context.Background(), cfg, events.EventTypeProgressComplete, summarizerCompleteVerified,
			func(*configuration.Config) ModelClient { return fake })
		if fake.calls != 1 {
			t.Fatalf("Complete calls = %d, want 1", fake.calls)
		}
		if got != template {
			t.Errorf("summary = %q, want the model answer %q", got, template)
		}
		if usage == (SummaryUsage{}) {
			t.Errorf("usage = %+v, want a booked call (the model was consulted)", usage)
		}
	})

	t.Run("unconfigured role uses the template with no usage", func(t *testing.T) {
		fake := &fakeModelClient{result: "unused"}
		got, usage := SummarizeProgressWithUsageFn(context.Background(), &configuration.Config{}, events.EventTypeProgressComplete, summarizerCompleteVerified,
			func(*configuration.Config) ModelClient { return nil })
		if got != "Run complete — verified (Checks: 3/3 passed)" {
			t.Errorf("summary = %q, want the template", got)
		}
		if usage != (SummaryUsage{}) {
			t.Errorf("usage = %+v, want zero", usage)
		}
		if fake.calls != 0 {
			t.Errorf("Complete calls = %d, want 0", fake.calls)
		}
	})

	t.Run("failed verification never consults the model", func(t *testing.T) {
		fake := &fakeModelClient{result: "Success! All checks passed."}
		cfg := &configuration.Config{}
		cfg.Roles = map[string]configuration.RoleConfig{
			configuration.RoleSummarizer: {Provider: string(api.TestClientType), Model: "summarizer-model"},
		}
		got, usage := SummarizeProgressWithUsageFn(context.Background(), cfg, events.EventTypeProgressVerification, summarizerVerificationFailing,
			func(*configuration.Config) ModelClient { return fake })
		if fake.calls != 0 {
			t.Fatalf("Complete calls = %d, want 0 (failing verification must not consult the model)", fake.calls)
		}
		if got != "Checks: 4/6 passed" {
			t.Errorf("summary = %q, want the non-success template", got)
		}
		if strings.Contains(got, "Success") {
			t.Errorf("summary %q states success without a passing verification", got)
		}
		if usage != (SummaryUsage{}) {
			t.Errorf("usage = %+v, want zero", usage)
		}
	})

	t.Run("nil config uses the template", func(t *testing.T) {
		// Production's builder returns nil for a nil config (no role to
		// resolve), so the template is used and the model is not called.
		got, usage := SummarizeProgressWithUsageFn(context.Background(), nil, events.EventTypeProgressComplete, summarizerCompleteVerified,
			func(c *configuration.Config) ModelClient {
				if c == nil {
					return nil
				}
				return &fakeModelClient{result: "unused"}
			})
		if got != "Run complete — verified (Checks: 3/3 passed)" {
			t.Errorf("summary = %q, want the template", got)
		}
		if usage != (SummaryUsage{}) {
			t.Errorf("usage = %+v, want zero", usage)
		}
	})
}
