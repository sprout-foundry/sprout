package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

func streamErrorProvider(t *testing.T, payload string) *GenericProvider {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "event: metadata\ndata: {\"source\":\"pending\"}\n\n")
		_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", payload)
	}))
	t.Cleanup(server.Close)

	provider, err := NewGenericProvider(&ProviderConfig{
		Name:     "stream-error-test",
		Endpoint: server.URL,
		Auth:     AuthConfig{Type: "none"},
		Defaults: RequestDefaults{Model: "test-model"},
		Models:   ModelConfig{DefaultContextLimit: 64000},
	})
	if err != nil {
		t.Fatalf("NewGenericProvider failed: %v", err)
	}
	return provider
}

func TestStreamErrorPayloadBecomesAnError(t *testing.T) {
	cases := []struct {
		name     string
		payload  string
		category agenterrors.ErrorCategory
	}{
		{"rate limit", `{"error":{"code":"rate_limited","message":"The model is rate-limited right now."}}`, agenterrors.CategoryRateLimited},
		{"numeric code", `{"error":{"code":429,"message":"The model is rate-limited right now."}}`, agenterrors.CategoryRateLimited},
		{"other failure", `{"error":{"code":"upstream_error","message":"The model is rate-limited right now."}}`, agenterrors.CategoryProvider},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := streamErrorProvider(t, tc.payload).SendChatRequestStream(
				context.Background(), []api.Message{{Role: "user", Content: "hi"}}, nil, "", false,
				func(string, string) {},
			)
			if err == nil {
				t.Fatal("expected an error, got an empty response")
			}
			if !strings.Contains(err.Error(), "rate-limited right now") {
				t.Errorf("error %q should carry the server's message", err)
			}
			var agentErr *agenterrors.AgentError
			if !errors.As(err, &agentErr) || agentErr.Category != tc.category {
				t.Errorf("error %v: want category %v", err, tc.category)
			}
		})
	}
}
