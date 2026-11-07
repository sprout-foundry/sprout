//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

func TestWriteProviderErrSeparatesUnreachableFromNotConfigured(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		code     string
		contains string
	}{
		{
			"unreachable",
			fmt.Errorf("create chat agent: %w", agent.NewProviderUnreachableError("LLM Bridge", fmt.Errorf("dial tcp 10.0.0.1:8080: connection refused"))),
			"provider_unreachable",
			"could not be reached: dial tcp 10.0.0.1:8080: connection refused",
		},
		{"not configured", fmt.Errorf("create chat agent: %w", agent.ErrProviderNotConfigured), "no_provider", "configure one in settings"},
		{"no provider", ErrNoProviderConfigured, "no_provider", "configure one in settings"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !isProviderConfigError(tc.err) {
				t.Fatal("isProviderConfigError = false")
			}
			rec := httptest.NewRecorder()
			writeProviderErr(rec, tc.err)
			assertProviderErr(t, rec, tc.code, tc.contains)
		})
	}
}

func assertProviderErr(t *testing.T, rec *httptest.ResponseRecorder, code, contains string) {
	t.Helper()
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	body := rec.Body.String()
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	if !strings.Contains(body, `"`+code+`"`) || !strings.Contains(body, contains) {
		t.Fatalf("body = %s, want code %q and %q", body, code, contains)
	}
}
