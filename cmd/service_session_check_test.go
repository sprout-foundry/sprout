//go:build !js

package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ── checkActiveSessions integration tests ────────────────────────────

func TestCheckActiveSessions_DaemonNotRunning(t *testing.T) {
	// Port 56000 should not be in use during tests. If it is, skip.
	count, err := checkActiveSessions()
	if err != nil {
		// If something IS on 56000, the test can't meaningfully verify the
		// "unreachable" path — skip rather than fail flakily.
		t.Skipf("daemon or something else responded on port 56000: %v (skipping)", err)
	}
	if count != 0 {
		t.Errorf("expected 0 sessions when daemon is not running, got %d", count)
	}
}

func TestCheckActiveSessions_ZeroSessions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"count":    0,
			"sessions": []interface{}{},
		})
	}))
	defer srv.Close()

	overrideSessionCheckURL(t, srv.URL)

	count, err := checkActiveSessions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 sessions, got %d", count)
	}
}

func TestCheckActiveSessions_NonzeroSessions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"count":    3,
			"sessions": []interface{}{"a", "b", "c"},
		})
	}))
	defer srv.Close()

	overrideSessionCheckURL(t, srv.URL)

	count, err := checkActiveSessions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 sessions, got %d", count)
	}
}

func TestCheckActiveSessions_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	overrideSessionCheckURL(t, srv.URL)

	// Even with a 5xx response, we still try to parse the body.
	// httptest servers return empty body on 500, so parsing will fail.
	_, err := checkActiveSessions()
	if err == nil {
		t.Error("expected an error when server returns 5xx with no body, got nil")
	}
}

// ── parseSessionResponse unit tests (table-driven) ───────────────────

func TestParseSessionResponse(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantCount   int
		wantErr     bool
		errContains string
	}{
		{
			name:      "zero_count_with_empty_sessions_array",
			body:      `{"count":0,"sessions":[]}`,
			wantCount: 0,
		},
		{
			name:      "three_count",
			body:      `{"count":3,"sessions":[{"id":"1"},{"id":"2"},{"id":"3"}]}`,
			wantCount: 3,
		},
		{
			name:      "count_first_then_sessions",
			body:      `{"count":5,"sessions":[]}`,
			wantCount: 5,
		},
		{
			name:      "sessions_first_then_count",
			body:      `{"sessions":[],"count":1}`,
			wantCount: 1,
		},
		{
			name:      "only_count_key",
			body:      `{"count":7}`,
			wantCount: 7,
		},
		{
			name:      "count_zero_only",
			body:      `{"count":0}`,
			wantCount: 0,
		},
		{
			name:      "missing_count_key_returns_zero",
			body:      `{"sessions":[{"id":"1"}]}`,
			wantCount: 0,
		},
		{
			name:      "empty_object",
			body:      `{}`,
			wantCount: 0,
		},
		{
			name:        "count_as_string_is_error",
			body:        `{"count":"five"}`,
			wantErr:     true,
			errContains: "unexpected type for count",
		},
		{
			name:        "count_as_bool_is_error",
			body:        `{"count":true}`,
			wantErr:     true,
			errContains: "unexpected type for count",
		},
		{
			name:        "count_as_array_is_error",
			body:        `{"count":[1,2]}`,
			wantErr:     true,
			errContains: "unexpected type for count",
		},
		{
			name:        "count_as_object_is_error",
			body:        `{"count":{"nested":1}}`,
			wantErr:     true,
			errContains: "unexpected type for count",
		},
		{
			name:        "invalid_json",
			body:        `not json`,
			wantErr:     true,
			errContains: "failed to parse response",
		},
		{
			name:        "empty_body",
			body:        ``,
			wantErr:     true,
			errContains: "failed to parse response",
		},
		{
			name:      "null_json_treated_as_empty_object",
			body:      `null`,
			wantCount: 0,
		},
		{
			name:      "large_count_value",
			body:      `{"count":999999}`,
			wantCount: 999999,
		},
		{
			name:      "float_count_truncated_to_int",
			body:      `{"count":2.7}`,
			wantCount: 2,
		},
		{
			name:      "extra_fields_ignored",
			body:      `{"count":1,"sessions":[],"extra_field":"ignored","another":null}`,
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSessionResponse([]byte(tt.body))

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
					return
				}
				if tt.errContains != "" {
					gotErr := err.Error()
					if !containsSubstring(gotErr, tt.errContains) {
						t.Errorf("expected error containing %q, got %q", tt.errContains, gotErr)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantCount {
				t.Errorf("expected %d sessions, got %d", tt.wantCount, got)
			}
		})
	}
}

// ── URL initialization test ──────────────────────────────────────────

func TestSessionCheckURL_Default(t *testing.T) {
	expected := "http://localhost:56000/api/terminal/agent-sessions"
	if sessionCheckURL != expected {
		t.Errorf("sessionCheckURL = %q, want %q", sessionCheckURL, expected)
	}
}

// ── serviceUninstallCmd flag test ────────────────────────────────────

func TestServiceUninstallCmd_HasYesFlag(t *testing.T) {
	flag := serviceUninstallCmd.Flags().Lookup("yes")
	if flag == nil {
		t.Fatal("serviceUninstallCmd has no 'yes' flag")
	}
	if flag.Shorthand != "y" {
		t.Errorf("yes flag shorthand = %q, want %q", flag.Shorthand, "y")
	}
	if flag.DefValue != "false" {
		t.Errorf("yes flag default = %q, want %q", flag.DefValue, "false")
	}
}

// ── helpers ──────────────────────────────────────────────────────────

// overrideSessionCheckURL temporarily replaces sessionCheckURL and restores it
// on test completion.
func overrideSessionCheckURL(t *testing.T, url string) {
	t.Helper()
	prev := sessionCheckURL
	sessionCheckURL = url
	t.Cleanup(func() { sessionCheckURL = prev })
}

// containsSubstring is a simple string contains helper to avoid importing
// strings in test assertions when the standard library is sufficient.
func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
