package agent

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	core "github.com/sprout-foundry/seed/core"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

func TestWrapError_Classification(t *testing.T) {
	// Every case here is reachable via a plain error (formatProviderHTTPError's
	// "HTTP <code>: <body>") or via seed's classification (a chain prefixed
	// with "transient error during chat …" or "[network] …"). The assertions
	// cover the branch ordering: 5xx/transient never reports an auth problem,
	// 401 does, and the provider's own body text survives.
	cases := []struct {
		name         string
		err          error
		wantContains []string
		wantNot      []string
	}{
		{
			name:         "HTTP 401 -> auth",
			err:          errors.New("HTTP 401: invalid api key"),
			wantContains: []string{"Authentication failed", "invalid api key"},
			wantNot:      []string{"rejected"},
		},
		{
			name:         "HTTP 403 -> auth",
			err:          errors.New("HTTP 403: forbidden"),
			wantContains: []string{"Authentication failed"},
			wantNot:      []string{"rejected"},
		},
		{
			name:         "HTTP 429 -> rate limit",
			err:          errors.New("HTTP 429: too many requests"),
			wantContains: []string{"Rate limit exceeded", "too many requests"},
			wantNot:      []string{"Authentication failed"},
		},
		{
			name:         "HTTP 400 -> rejected",
			err:          errors.New("HTTP 400: bad request body"),
			wantContains: []string{"The provider rejected the request", "bad request body"},
			wantNot:      []string{"Authentication failed"},
		},
		{
			name:         "HTTP 500 -> server error, never auth",
			err:          errors.New("HTTP 500: inference server ran out of GPU memory"),
			wantContains: []string{"Provider server error (HTTP 500)", "GPU memory"},
			wantNot:      []string{"Authentication failed", "check your API key"},
		},
		{
			name:         "HTTP 503 -> server error, body text survives",
			err:          errors.New("HTTP 503: service unavailable, upstream busy"),
			wantContains: []string{"Provider server error (HTTP 503)", "service unavailable"},
			wantNot:      []string{"Authentication failed", "check your API key"},
		},
		{
			name:         "connection reset -> server error, never auth",
			err:          errors.New("connection reset by peer"),
			wantContains: []string{"Provider server error", "connection reset by peer"},
			wantNot:      []string{"Authentication failed", "check your API key"},
		},
		{
			name:         "transient typed error -> server error",
			err:          agenterrors.NewTransientError("upstream network failure", nil),
			wantContains: []string{"Provider server error", "upstream network failure"},
			wantNot:      []string{"Authentication failed", "check your API key"},
		},
		{
			name:         "legacy rate-limited provider -> rate limit",
			err:          agenterrors.NewRateLimitError("requests are too frequent", nil, "deepseek"),
			wantContains: []string{"Rate limit exceeded", "too frequent"},
			wantNot:      []string{"Authentication failed"},
		},
		{
			name:         "legacy provider error (no key wording) -> provider error, never auth",
			err:          agenterrors.NewProviderError("model not found", nil, "deepseek", ""),
			wantContains: []string{"Provider error", "model not found"},
			wantNot:      []string{"Authentication failed", "check your API key"},
		},
		{
			name:         "core-wrapped transient 500 -> server error, chain stripped",
			err:          core.ClassifyError(errors.New("HTTP 500: inference server ran out of GPU memory"), "deepseek"),
			wantContains: []string{"Provider server error (HTTP 500)", "GPU memory"},
			wantNot:      []string{"Authentication failed", "check your API key", "transient error during chat"},
		},
		{
			name:         "core-wrapped transient 503 -> server error, chain stripped",
			err:          core.ClassifyError(errors.New("HTTP 503: upstream busy"), "deepseek"),
			wantContains: []string{"Provider server error (HTTP 503)", "upstream busy"},
			wantNot:      []string{"Authentication failed", "check your API key", "transient error during chat"},
		},
		{
			name:         "chained network prefix stripped",
			err:          fmt.Errorf("transient error during chat (deepseek): [network] chat request failed (streaming): [Provider] HTTP 503: service unavailable"),
			wantContains: []string{"Provider server error (HTTP 503)", "service unavailable"},
			wantNot:      []string{"transient error during chat", "chat request failed", "[Provider]", "[network]"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := wrapError(tc.err)
			for _, want := range tc.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("wrapError() = %q, want it to contain %q", got, want)
				}
			}
			for _, not := range tc.wantNot {
				if strings.Contains(got, not) {
					t.Errorf("wrapError() = %q, must not contain %q", got, not)
				}
			}
		})
	}
}
