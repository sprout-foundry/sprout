//go:build !js

package cliui

// progress_summarizer_test.go — tests for the optional model-written
// SP-151 progress summaries (SP-151 §151c, item 151.8): the template
// fallbacks (nil client, model error, empty model output), the success
// invariant (a summary never states success without a passing
// verification), the model prompt built only from event fields, and
// the NewSummarizerModelClient guard paths.

import (
	"context"
	"errors"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// fakeModelClient is a ModelClient that returns a canned result (or
// error) and records the prompts and call count, so the tests assert
// the model prompt is built only from event fields without a real
// provider.
type fakeModelClient struct {
	result string
	err    error
	calls  int
	system string
	user   string
}

func (f *fakeModelClient) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	f.calls++
	f.system = systemPrompt
	f.user = userPrompt
	return f.result, f.err
}

// Progress event payloads shared by the tests (SP-151 §151a shapes).
var (
	summarizerCompleteVerified = map[string]interface{}{
		"run_id":   "run-1",
		"verified": true,
		"verification": map[string]interface{}{
			"passed": true,
			"checks": []interface{}{
				map[string]interface{}{"kind": "build", "passed": true},
				map[string]interface{}{"kind": "test", "passed": true},
				map[string]interface{}{"kind": "lint", "passed": true},
			},
		},
	}

	summarizerCompleteUnverified = map[string]interface{}{
		"run_id":              "run-1",
		"not_verified_reason": "verification disabled",
	}

	summarizerVerificationFailing = map[string]interface{}{
		"run_id": "run-1",
		"checks": []interface{}{
			map[string]interface{}{"kind": "build", "passed": true},
			map[string]interface{}{"kind": "test", "passed": true},
			map[string]interface{}{"kind": "lint", "passed": true},
			map[string]interface{}{"kind": "e2e", "passed": true},
			map[string]interface{}{"kind": "review", "passed": false, "reason": "two findings"},
			map[string]interface{}{"kind": "docs", "skipped": true, "reason": "no trusted docs command"},
		},
	}

	summarizerVerificationAllPassed = map[string]interface{}{
		"run_id": "run-1",
		"checks": []interface{}{
			map[string]interface{}{"kind": "build", "passed": true},
			map[string]interface{}{"kind": "test", "passed": true},
			map[string]interface{}{"kind": "lint", "passed": true},
		},
	}

	summarizerVerificationPassedFlat = map[string]interface{}{
		"run_id": "run-1",
		"passed": true,
	}

	summarizerMilestoneFinished = map[string]interface{}{
		"run_id":        "run-1",
		"scope_id":      "s1",
		"scope_title":   "sign-up form",
		"phase":         "finished",
		"files_touched": 4,
	}
)

func TestSummarizeProgress(t *testing.T) {
	cases := []struct {
		name          string
		client        ModelClient
		eventType     string
		data          map[string]interface{}
		want          string
		wantNoSuccess bool
		wantCalls     int
	}{
		{
			name:      "verified complete gets the model summary",
			client:    &fakeModelClient{result: "Run complete — all checks green."},
			eventType: events.EventTypeProgressComplete,
			data:      summarizerCompleteVerified,
			want:      "Run complete — all checks green.",
			wantCalls: 1,
		},
		{
			name:      "model error falls back to the template",
			client:    &fakeModelClient{err: errors.New("provider down")},
			eventType: events.EventTypeProgressComplete,
			data:      summarizerCompleteVerified,
			want:      "Run complete — verified (Checks: 3/3 passed)",
			wantCalls: 1,
		},
		{
			name:      "nil client uses the template",
			client:    nil,
			eventType: events.EventTypeProgressComplete,
			data:      summarizerCompleteVerified,
			want:      "Run complete — verified (Checks: 3/3 passed)",
			wantCalls: 0,
		},
		{
			name:      "empty model output falls back to the template",
			client:    &fakeModelClient{result: "   "},
			eventType: events.EventTypeProgressComplete,
			data:      summarizerCompleteVerified,
			want:      "Run complete — verified (Checks: 3/3 passed)",
			wantCalls: 1,
		},
		{
			name:          "unverified complete never yields a success claim",
			client:        &fakeModelClient{result: "Success! All checks passed and the run is complete."},
			eventType:     events.EventTypeProgressComplete,
			data:          summarizerCompleteUnverified,
			want:          "Run complete — not verified (verification disabled)",
			wantNoSuccess: true,
			wantCalls:     0,
		},
		{
			name:          "failing checks never yield a success claim",
			client:        &fakeModelClient{result: "All done — success!"},
			eventType:     events.EventTypeProgressVerification,
			data:          summarizerVerificationFailing,
			want:          "Checks: 4/6 passed",
			wantNoSuccess: true,
			wantCalls:     0,
		},
		{
			name:      "flat-passing verification gets the model summary",
			client:    &fakeModelClient{result: "All checks passed."},
			eventType: events.EventTypeProgressVerification,
			data:      summarizerVerificationPassedFlat,
			want:      "All checks passed.",
			wantCalls: 1,
		},
		{
			name:      "all-checks-passed verification gets the model summary",
			client:    &fakeModelClient{result: "Checks all green."},
			eventType: events.EventTypeProgressVerification,
			data:      summarizerVerificationAllPassed,
			want:      "Checks all green.",
			wantCalls: 1,
		},
		{
			name:      "milestone is unconstrained",
			client:    &fakeModelClient{result: "Finished the sign-up form."},
			eventType: events.EventTypeProgressMilestone,
			data:      summarizerMilestoneFinished,
			want:      "Finished the sign-up form.",
			wantCalls: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SummarizeProgress(context.Background(), tc.client, tc.eventType, tc.data)
			if got != tc.want {
				t.Errorf("SummarizeProgress() = %q, want %q", got, tc.want)
			}
			if tc.wantNoSuccess && strings.Contains(got, "Success") {
				t.Errorf("summary %q states success without a passing verification", got)
			}
			if f, ok := tc.client.(*fakeModelClient); ok && f.calls != tc.wantCalls {
				t.Errorf("Complete calls = %d, want %d", f.calls, tc.wantCalls)
			}
		})
	}
}

// TestSummarizeProgressPromptBuiltFromEventFields pins the contract
// that the model prompt is built only from event fields: the user
// prompt carries the event type and the payload as deterministic JSON
// (encoding/json sorts map keys), nothing else.
func TestSummarizeProgressPromptBuiltFromEventFields(t *testing.T) {
	fake := &fakeModelClient{result: "Finished the sign-up form."}
	SummarizeProgress(context.Background(), fake, events.EventTypeProgressMilestone, summarizerMilestoneFinished)
	if fake.calls != 1 {
		t.Fatalf("Complete calls = %d, want 1", fake.calls)
	}
	if fake.system == "" {
		t.Error("system prompt is empty")
	}
	if !strings.Contains(fake.system, "ONE short, factual sentence") {
		t.Errorf("system prompt = %q, want the one-sentence instruction", fake.system)
	}
	wantUser := "Event type: " + events.EventTypeProgressMilestone +
		"\nEvent fields (JSON): {\"files_touched\":4,\"phase\":\"finished\",\"run_id\":\"run-1\",\"scope_id\":\"s1\",\"scope_title\":\"sign-up form\"}"
	if fake.user != wantUser {
		t.Errorf("user prompt = %q, want %q", fake.user, wantUser)
	}
}

func TestProgressVerificationPassed(t *testing.T) {
	cases := []struct {
		name      string
		eventType string
		data      map[string]interface{}
		want      bool
	}{
		{
			name:      "complete verified",
			eventType: events.EventTypeProgressComplete,
			data:      map[string]interface{}{"verified": true},
			want:      true,
		},
		{
			name:      "complete not verified",
			eventType: events.EventTypeProgressComplete,
			data:      map[string]interface{}{"verified": false, "not_verified_reason": "verification disabled"},
			want:      false,
		},
		{
			name:      "complete without the flag",
			eventType: events.EventTypeProgressComplete,
			data:      map[string]interface{}{"run_id": "run-1"},
			want:      false,
		},
		{
			name:      "verification passed flat",
			eventType: events.EventTypeProgressVerification,
			data:      map[string]interface{}{"passed": true},
			want:      true,
		},
		{
			name:      "verification failed flat",
			eventType: events.EventTypeProgressVerification,
			data:      map[string]interface{}{"passed": false},
			want:      false,
		},
		{
			name:      "verification with a failing check",
			eventType: events.EventTypeProgressVerification,
			data: map[string]interface{}{
				"checks": []interface{}{
					map[string]interface{}{"kind": "build", "passed": true},
					map[string]interface{}{"kind": "test", "passed": false},
				},
			},
			want: false,
		},
		{
			name:      "verification all skipped",
			eventType: events.EventTypeProgressVerification,
			data: map[string]interface{}{
				"checks": []interface{}{
					map[string]interface{}{"kind": "test", "skipped": true, "reason": "no trusted test command"},
					map[string]interface{}{"kind": "docs", "skipped": true, "reason": "no trusted docs command"},
				},
			},
			want: false,
		},
		{
			name:      "verification all passed",
			eventType: events.EventTypeProgressVerification,
			data: map[string]interface{}{
				"checks": []interface{}{
					map[string]interface{}{"kind": "build", "passed": true},
					map[string]interface{}{"kind": "test", "passed": true},
				},
			},
			want: true,
		},
		{
			name:      "verification passed plus skipped",
			eventType: events.EventTypeProgressVerification,
			data: map[string]interface{}{
				"checks": []interface{}{
					map[string]interface{}{"kind": "build", "passed": true},
					map[string]interface{}{"kind": "test", "skipped": true, "reason": "no trusted test command"},
				},
			},
			want: true,
		},
		{
			name:      "verification empty payload",
			eventType: events.EventTypeProgressVerification,
			data:      map[string]interface{}{},
			want:      false,
		},
		{
			name:      "milestone unconstrained",
			eventType: events.EventTypeProgressMilestone,
			data:      map[string]interface{}{"phase": "finished"},
			want:      true,
		},
		{
			name:      "question unconstrained",
			eventType: events.EventTypeProgressQuestion,
			data:      map[string]interface{}{"question": "proceed?"},
			want:      true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := progressVerificationPassed(tc.eventType, tc.data); got != tc.want {
				t.Errorf("progressVerificationPassed(%q) = %v, want %v", tc.eventType, got, tc.want)
			}
		})
	}
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

	// The test provider is a pure in-memory mock (factory.TestClient):
	// the client build and a call are side-effect free, so the
	// configured-role path is testable without a real provider.
	t.Run("configured role builds a client", func(t *testing.T) {
		cfg := &configuration.Config{}
		cfg.Roles = map[string]configuration.RoleConfig{
			configuration.RoleSummarizer: {Provider: string(api.TestClientType)},
		}
		client := NewSummarizerModelClient(cfg)
		if client == nil {
			t.Fatal("NewSummarizerModelClient(configured role) = nil, want a client")
		}
		out, err := client.Complete(context.Background(), "summarize", "event fields")
		if err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
		if strings.TrimSpace(out) == "" {
			t.Error("Complete() returned an empty summary")
		}
	})
}
