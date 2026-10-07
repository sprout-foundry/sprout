package timeline

// timeline_summarizer_test.go — tests for the optional model-written
// timeline-entry summaries: the template fallbacks (nil client, model
// error, empty model output, timeout, nothing to summarize), the invariant
// that the model cannot change the outcome beyond the supplied data, the
// prompt built only from entry fields, the metering rule (usage booked only
// when the model ran), and the NewSummarizerModelClient guard paths.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/deploy"
	"github.com/sprout-foundry/sprout/pkg/history"
)

// fakeModelClient is a ModelClient that returns a canned result (or error)
// and records the prompts and call count, so the tests assert the model
// prompt is built only from entry fields without a real provider.
type fakeModelClient struct {
	result string
	err    error
	calls  int
	system string
	user   string
	// block, when non-nil, holds Complete until it is closed (or the
	// derived call context is done) so the timeout-fallback path can be
	// exercised deterministically.
	block chan struct{}
}

func (f *fakeModelClient) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	f.calls++
	f.system = systemPrompt
	f.user = userPrompt
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return f.result, f.err
}

func changeSetEntry(revisionID string, files ...string) Entry {
	changes := make([]history.ChangeLog, 0, len(files))
	for _, f := range files {
		changes = append(changes, history.ChangeLog{
			Filename:     f,
			Status:       activeStatus,
			OriginalCode: "x\n",
			NewCode:      "x\ny\n",
		})
	}
	return Entry{ChangeSet: summarizeRevision(&history.RevisionGroup{
		RevisionID: revisionID,
		Changes:    changes,
	}, []string{"scope-1"})}
}

func deployEntry() Entry {
	d := deploy.Deployment{ID: "dep-1", Kind: deploy.KindProduction, Version: "v3", Status: deploy.StatusReady}
	return Entry{Deploy: &Deploy{Deployment: d, Summary: SummarizeDeploy(d)}}
}

func checkpointEntry() Entry {
	cp := history.Checkpoint{
		ID:         "cp-1",
		Origin:     history.CheckpointVerification,
		RevisionID: "rev-1",
		Files:      []string{"a.go", "b.go"},
	}
	return Entry{Checkpoint: &Checkpoint{Checkpoint: cp, Summary: SummarizeCheckpoint(cp)}}
}

func TestSummarizeEntry_TemplateFallbacks(t *testing.T) {
	cases := []struct {
		name      string
		entry     Entry
		client    ModelClient
		want      string
		wantCalls int
	}{
		{
			name:      "change set gets the model summary",
			entry:     changeSetEntry("rev-1", "a.go", "b.go"),
			client:    &fakeModelClient{result: "Added the sign-up form."},
			want:      "Added the sign-up form.",
			wantCalls: 1,
		},
		{
			name:      "deploy gets the model summary",
			entry:     deployEntry(),
			client:    &fakeModelClient{result: "Shipped v3."},
			want:      "Shipped v3.",
			wantCalls: 1,
		},
		{
			name:      "checkpoint gets the model summary",
			entry:     checkpointEntry(),
			client:    &fakeModelClient{result: "Snapshot before verification."},
			want:      "Snapshot before verification.",
			wantCalls: 1,
		},
		{
			name:      "model error falls back to the change-set template",
			entry:     changeSetEntry("rev-1", "a.go", "b.go"),
			client:    &fakeModelClient{err: errors.New("provider down")},
			want:      "changed 2 files: a.go, b.go (+2/-0)",
			wantCalls: 1,
		},
		{
			name:      "model error falls back to the deploy template",
			entry:     deployEntry(),
			client:    &fakeModelClient{err: errors.New("provider down")},
			want:      "deployed v3 to production",
			wantCalls: 1,
		},
		{
			name:      "nil client uses the template",
			entry:     changeSetEntry("rev-1", "a.go"),
			client:    nil,
			want:      "changed 1 file: a.go (+1/-0)",
			wantCalls: 0,
		},
		{
			name:      "empty model output falls back to the template",
			entry:     changeSetEntry("rev-1", "a.go"),
			client:    &fakeModelClient{result: "   "},
			want:      "changed 1 file: a.go (+1/-0)",
			wantCalls: 1,
		},
		{
			name:      "empty entry uses the template and never consults the model",
			entry:     Entry{},
			client:    &fakeModelClient{result: "invented"},
			want:      "",
			wantCalls: 0,
		},
		{
			name:      "no-change revision still has a template the model may rephrase",
			entry:     Entry{ChangeSet: summarizeRevision(&history.RevisionGroup{RevisionID: "rev-0"}, nil)},
			client:    &fakeModelClient{result: "Nothing to see."},
			want:      "Nothing to see.",
			wantCalls: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SummarizeEntry(context.Background(), tc.client, tc.entry)
			if got != tc.want {
				t.Errorf("SummarizeEntry() = %q, want %q", got, tc.want)
			}
			if f, ok := tc.client.(*fakeModelClient); ok && f.calls != tc.wantCalls {
				t.Errorf("Complete calls = %d, want %d", f.calls, tc.wantCalls)
			}
		})
	}
}

// TestSummarizeEntryNeverConsultsModelWithoutTemplate pins the "nothing to
// summarize" invariant: a malformed entry has no data, so the model is
// never consulted and can never fabricate an entry out of nothing.
func TestSummarizeEntryNeverConsultsModelWithoutTemplate(t *testing.T) {
	fake := &fakeModelClient{result: "Totally made up."}
	if got := SummarizeEntry(context.Background(), fake, Entry{}); got != "" {
		t.Errorf("SummarizeEntry(empty) = %q, want empty", got)
	}
	if fake.calls != 0 {
		t.Errorf("Complete calls = %d, want 0 for an entry with nothing to summarize", fake.calls)
	}
}

// TestSummarizeEntryPromptBuiltFromEntryFields pins the contract that the
// model prompt is built only from entry fields: the user prompt carries the
// entry kind, the deterministic template summary, and the entry data as
// deterministic JSON (encoding/json sorts map keys), nothing else.
func TestSummarizeEntryPromptBuiltFromEntryFields(t *testing.T) {
	fake := &fakeModelClient{result: "Added the sign-up form."}
	entry := changeSetEntry("rev-1", "a.go")
	SummarizeEntry(context.Background(), fake, entry)
	if fake.calls != 1 {
		t.Fatalf("Complete calls = %d, want 1", fake.calls)
	}
	if fake.system == "" {
		t.Error("system prompt is empty")
	}
	if !strings.Contains(fake.system, "ONE short, factual sentence") {
		t.Errorf("system prompt = %q, want the one-sentence instruction", fake.system)
	}
	if !strings.Contains(fake.user, "Entry kind: change_set") {
		t.Errorf("user prompt = %q, want the entry kind line", fake.user)
	}
	if !strings.Contains(fake.user, "Deterministic summary: "+entry.Summary()) {
		t.Errorf("user prompt = %q, want the deterministic summary", fake.user)
	}
	// The JSON is the entry's own fields — the model has no material for
	// anything the entry did not supply.
	for _, want := range []string{`"revision_id":"rev-1"`, `"files":["a.go"]`, `"scope_ids":["scope-1"]`} {
		if !strings.Contains(fake.user, want) {
			t.Errorf("user prompt = %q, want it to carry %s", fake.user, want)
		}
	}
}

// TestSummarizeEntryModelCannotChangeOutcomeBeyondData is the rule-breaker:
// a model that claims a different outcome than the entry carries cannot
// change the outcome when it errors, times out, or is unconfigured — the
// deterministic template is what renders.
func TestSummarizeEntryModelCannotChangeOutcomeBeyondData(t *testing.T) {
	// A deploy that failed must read "failed" whenever the model is not
	// used; a model insisting on success never reaches the caller.
	failed := deploy.Deployment{ID: "dep-2", Kind: deploy.KindProduction, Version: "v9", Status: deploy.StatusFailed}
	entry := Entry{Deploy: &Deploy{Deployment: failed, Summary: SummarizeDeploy(failed)}}

	cases := []struct {
		name   string
		client ModelClient
	}{
		{name: "nil client", client: nil},
		{name: "model error", client: &fakeModelClient{err: errors.New("down")}},
		{name: "empty model output", client: &fakeModelClient{result: " "}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SummarizeEntry(context.Background(), tc.client, entry)
			if got != "deployed v9 to production (failed)" {
				t.Errorf("summary = %q, want the failed template", got)
			}
			if strings.Contains(got, "success") {
				t.Errorf("summary %q claims success the entry does not carry", got)
			}
		})
	}
}

// TestSummarizeEntryTimeoutFallsBackToTemplate proves a hung summarizer
// cannot stall the caller: the call is bounded and the deterministic
// template is used when the bound elapses. The fake's Complete blocks until
// its derived context is cancelled, so the timeout path is exercised
// without a real sleep or provider.
func TestSummarizeEntryTimeoutFallsBackToTemplate(t *testing.T) {
	// Shrink the bound so the timeout path is exercised in milliseconds
	// rather than the production 5s.
	orig := summaryTimeout
	summaryTimeout = 50 * time.Millisecond
	t.Cleanup(func() { summaryTimeout = orig })

	fake := &fakeModelClient{result: "should never be returned", block: make(chan struct{})}
	entry := changeSetEntry("rev-1", "a.go")
	start := time.Now()
	got := SummarizeEntry(context.Background(), fake, entry)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("SummarizeEntry took %s, want it bounded near %s", elapsed, summaryTimeout)
	}
	if got != entry.Summary() {
		t.Errorf("SummarizeEntry() on timeout = %q, want the template %q", got, entry.Summary())
	}
	if fake.calls != 1 {
		t.Errorf("Complete calls = %d, want 1", fake.calls)
	}
}

func TestSummarizeEntryWithUsageFnMetersSummarizerRole(t *testing.T) {
	configured := func() *configuration.Config {
		cfg := &configuration.Config{}
		cfg.Roles = map[string]configuration.RoleConfig{
			configuration.RoleSummarizer: {Provider: string(api.TestClientType), Model: "summarizer-model"},
		}
		return cfg
	}

	t.Run("configured role meters the call", func(t *testing.T) {
		fake := &fakeModelClient{result: "Added the sign-up form."}
		entry := changeSetEntry("rev-1", "a.go", "b.go")
		got, usage := SummarizeEntryWithUsageFn(context.Background(), configured(), entry,
			func(*configuration.Config) ModelClient { return fake })
		if got != "Added the sign-up form." {
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
		entry := changeSetEntry("rev-1", "a.go")
		fake := &fakeModelClient{result: entry.Summary()}
		got, usage := SummarizeEntryWithUsageFn(context.Background(), configured(), entry,
			func(*configuration.Config) ModelClient { return fake })
		if fake.calls != 1 {
			t.Fatalf("Complete calls = %d, want 1", fake.calls)
		}
		if got != entry.Summary() {
			t.Errorf("summary = %q, want the model answer", got)
		}
		if usage == (SummaryUsage{}) {
			t.Errorf("usage = %+v, want a booked call (the model was consulted)", usage)
		}
	})

	t.Run("unconfigured role uses the template with no usage", func(t *testing.T) {
		fake := &fakeModelClient{result: "unused"}
		entry := changeSetEntry("rev-1", "a.go")
		got, usage := SummarizeEntryWithUsageFn(context.Background(), &configuration.Config{}, entry,
			func(*configuration.Config) ModelClient { return nil })
		if got != entry.Summary() {
			t.Errorf("summary = %q, want the template %q", got, entry.Summary())
		}
		if usage != (SummaryUsage{}) {
			t.Errorf("usage = %+v, want zero", usage)
		}
		if fake.calls != 0 {
			t.Errorf("Complete calls = %d, want 0", fake.calls)
		}
	})

	t.Run("model error books nothing", func(t *testing.T) {
		fake := &fakeModelClient{err: errors.New("provider down")}
		entry := changeSetEntry("rev-1", "a.go")
		got, usage := SummarizeEntryWithUsageFn(context.Background(), configured(), entry,
			func(*configuration.Config) ModelClient { return fake })
		if got != entry.Summary() {
			t.Errorf("summary = %q, want the template", got)
		}
		if usage != (SummaryUsage{}) {
			t.Errorf("usage = %+v, want zero for a failed call", usage)
		}
	})

	t.Run("nil builder uses the template", func(t *testing.T) {
		entry := changeSetEntry("rev-1", "a.go")
		got, usage := SummarizeEntryWithUsageFn(context.Background(), configured(), entry, nil)
		if got != entry.Summary() {
			t.Errorf("summary = %q, want the template", got)
		}
		if usage != (SummaryUsage{}) {
			t.Errorf("usage = %+v, want zero", usage)
		}
	})

	t.Run("nil config uses the template", func(t *testing.T) {
		// Production's builder returns nil for a nil config (no role to
		// resolve), so the template is used and the model is not called.
		entry := changeSetEntry("rev-1", "a.go")
		got, usage := SummarizeEntryWithUsageFn(context.Background(), nil, entry,
			func(c *configuration.Config) ModelClient {
				if c == nil {
					return nil
				}
				return &fakeModelClient{result: "unused"}
			})
		if got != entry.Summary() {
			t.Errorf("summary = %q, want the template", got)
		}
		if usage != (SummaryUsage{}) {
			t.Errorf("usage = %+v, want zero", usage)
		}
	})
}

func TestNewSummarizerModelClient(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		if got := NewSummarizerModelClient(nil); got != nil {
			t.Errorf("NewSummarizerModelClient(nil) = %v, want nil", got)
		}
	})

	t.Run("summarizer role unset", func(t *testing.T) {
		if got := NewSummarizerModelClient(&configuration.Config{}); got != nil {
			t.Errorf("NewSummarizerModelClient(empty config) = %v, want nil", got)
		}
	})

	t.Run("model-only role with no resolvable provider", func(t *testing.T) {
		cfg := &configuration.Config{}
		cfg.Roles = map[string]configuration.RoleConfig{
			configuration.RoleSummarizer: {Model: "some-model"},
		}
		if got := NewSummarizerModelClient(cfg); got != nil {
			t.Errorf("NewSummarizerModelClient(model-only role) = %v, want nil", got)
		}
	})

	// The test provider is a pure in-memory mock (factory.TestClient): the
	// client build and a call are side-effect free, so the configured-role
	// path is testable without a real provider.
	t.Run("configured role builds a client", func(t *testing.T) {
		cfg := &configuration.Config{}
		cfg.Roles = map[string]configuration.RoleConfig{
			configuration.RoleSummarizer: {Provider: string(api.TestClientType)},
		}
		client := NewSummarizerModelClient(cfg)
		if client == nil {
			t.Fatal("NewSummarizerModelClient(configured role) = nil, want a client")
		}
		out, err := client.Complete(context.Background(), "summarize", "entry fields")
		if err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
		if strings.TrimSpace(out) == "" {
			t.Error("Complete() returned an empty summary")
		}
	})
}

// TestSummarizeEntryWithUsageFnEndToEndThroughBuilder drives the metered
// seam through the production builder shape (a configured role resolved to
// the in-memory test provider), proving the whole path — resolve role, call
// model, book a non-zero usage — without a live provider.
func TestSummarizeEntryWithUsageFnEndToEndThroughBuilder(t *testing.T) {
	cfg := &configuration.Config{}
	cfg.Roles = map[string]configuration.RoleConfig{
		configuration.RoleSummarizer: {Provider: string(api.TestClientType), Model: "summarizer-model"},
	}
	entry := changeSetEntry("rev-1", "a.go")
	got, usage := SummarizeEntryWithUsageFn(context.Background(), cfg, entry, NewSummarizerModelClient)
	if got == "" {
		t.Fatal("summary is empty, want the model output")
	}
	if usage.PromptTokens <= 0 || usage.CompletionTokens <= 0 {
		t.Errorf("usage = %+v, want positive token counts for a metered call", usage)
	}
}
