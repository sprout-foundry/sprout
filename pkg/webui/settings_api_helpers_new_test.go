//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func TestValidateReasoningEffort(t *testing.T) {
	tests := []struct {
		name    string
		v       string
		wantErr bool
	}{
		{"empty string", "", false},
		{"low", "low", false},
		{"medium", "medium", false},
		{"high", "high", false},
		{"invalid", "invalid", true},
		{"uppercase LOW", "LOW", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateReasoningEffort(tt.v)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateReasoningEffort(%q) error = %v, wantErr %v", tt.v, err, tt.wantErr)
			}
		})
	}
}

func TestValidateHistoryScope(t *testing.T) {
	tests := []struct {
		name    string
		v       string
		wantErr bool
	}{
		{"project", "project", false},
		{"global", "global", false},
		{"invalid", "invalid", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateHistoryScope(tt.v)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateHistoryScope(%q) error = %v, wantErr %v", tt.v, err, tt.wantErr)
			}
		})
	}
}

func TestValidateAPITimeout(t *testing.T) {
	tests := []struct {
		name    string
		v       int
		wantErr bool
	}{
		{"one", 1, false},
		{"sixty", 60, false},
		{"zero", 0, true},
		{"negative", -1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAPITimeout(tt.v)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateAPITimeout(%d) error = %v, wantErr %v", tt.v, err, tt.wantErr)
			}
		})
	}
}

func TestExtractPathSegment(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		prefix string
		want   string
	}{
		{
			name:   "standard extraction",
			path:   "/api/settings/mcp/servers/myserver",
			prefix: "/api/settings/mcp/servers/",
			want:   "myserver",
		},
		{
			name:   "trailing slash trimmed",
			path:   "/api/settings/mcp/servers/myserver/",
			prefix: "/api/settings/mcp/servers/",
			want:   "myserver",
		},
		{
			name:   "no matching prefix",
			path:   "/api/other/path",
			prefix: "/api/settings/mcp/servers/",
			want:   "",
		},
		{
			name:   "empty after prefix",
			path:   "/api/settings/mcp/servers/",
			prefix: "/api/settings/mcp/servers/",
			want:   "",
		},
		{
			name:   "segment with path components",
			path:   "/api/settings/mcp/servers/my/server",
			prefix: "/api/settings/mcp/servers/",
			want:   "my/server",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPathSegment(tt.path, tt.prefix)
			if got != tt.want {
				t.Errorf("extractPathSegment(%q, %q) = %q, want %q", tt.path, tt.prefix, got, tt.want)
			}
		})
	}
}

func TestAsInt(t *testing.T) {
	tests := []struct {
		name   string
		v      interface{}
		want   int
		wantOK bool
	}{
		{"float64", float64(42), 42, true},
		{"int", int(42), 42, true},
		{"int64", int64(42), 42, true},
		{"string", "42", 0, false},
		{"nil", nil, 0, false},
		{"bool", true, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := asInt(tt.v)
			if ok != tt.wantOK {
				t.Errorf("asInt(%v) ok = %v, want %v", tt.v, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("asInt(%v) = %d, want %d", tt.v, got, tt.want)
			}
		})
	}
}

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, map[string]string{"key": "value"})

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if got["key"] != "value" {
		t.Errorf("body key = %q, want %q", got["key"], "value")
	}
}

func TestWriteJSONError(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSONError(rec, http.StatusBadRequest, "bad request")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if got["error"] != "bad request" {
		t.Errorf("error = %v, want %q", got["error"], "bad request")
	}
}

func TestWriteJSONErr(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSONErr(rec, http.StatusForbidden, "forbidden_code", "You are not allowed")

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if got["error"] != "You are not allowed" {
		t.Errorf("error = %v, want %q", got["error"], "You are not allowed")
	}
	if got["code"] != "forbidden_code" {
		t.Errorf("code = %v, want %q", got["code"], "forbidden_code")
	}
}

func TestSanitizedConfig(t *testing.T) {
	cfg := &configuration.Config{
		Version:          "1.0.0",
		LastUsedProvider: "anthropic",
		ReasoningEffort:  "high",
		HistoryScope:     "project",
	}
	out := sanitizedConfig(cfg)

	if out["version"] != "1.0.0" {
		t.Errorf("version = %v, want %q", out["version"], "1.0.0")
	}
	if out["last_used_provider"] != "anthropic" {
		t.Errorf("last_used_provider = %v, want %q", out["last_used_provider"], "anthropic")
	}
	if out["reasoning_effort"] != "high" {
		t.Errorf("reasoning_effort = %v, want %q", out["reasoning_effort"], "high")
	}
	if out["history_scope"] != "project" {
		t.Errorf("history_scope = %v, want %q", out["history_scope"], "project")
	}
}

func TestSanitizedCustomProviders(t *testing.T) {
	t.Run("nil returns nil", func(t *testing.T) {
		out := sanitizedCustomProviders(nil)
		if out != nil {
			t.Errorf("nil input -> %v, want nil", out)
		}
	})

	t.Run("returns defensive copy", func(t *testing.T) {
		orig := map[string]configuration.CustomProviderConfig{
			"my-provider": {
				Name:           "My Provider",
				Endpoint:       "https://api.example.com",
				ModelName:      "my-model",
				EnvVar:         "MY_API_KEY",
				RequiresAPIKey: true,
			},
		}
		out := sanitizedCustomProviders(orig)

		if out == nil {
			t.Fatal("output is nil")
		}
		// Verify it's a copy, not the same map
		out["extra"] = configuration.CustomProviderConfig{Name: "extra"}
		if _, exists := orig["extra"]; exists {
			t.Error("modifying output affected original — not a defensive copy")
		}

		p, ok := out["my-provider"]
		if !ok {
			t.Fatal("my-provider not found in output")
		}
		if p.Name != "My Provider" {
			t.Errorf("Name = %q, want %q", p.Name, "My Provider")
		}
	})
}
