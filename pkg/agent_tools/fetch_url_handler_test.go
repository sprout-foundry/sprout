package tools

import (
	"context"
	"strings"
	"testing"
)

func TestFetchURLHandler_Name(t *testing.T) {
	h := NewFetchURLHandler()
	if got := h.Name(); got != "fetch_url" {
		t.Errorf("Name() = %q, want %q", got, "fetch_url")
	}
}

func TestFetchURLHandler_Definition(t *testing.T) {
	h := NewFetchURLHandler()
	def := h.Definition()

	if def.Type != "function" {
		t.Errorf("Definition().Type = %q, want %q", def.Type, "function")
	}
	if def.Function.Name != "fetch_url" {
		t.Errorf("Definition().Function.Name = %q, want %q", def.Function.Name, "fetch_url")
	}
	if def.Function.Description == "" {
		t.Error("Definition().Function.Description is empty")
	}

	// Check parameter structure
	params, ok := def.Function.Parameters.(map[string]interface{})
	if !ok {
		t.Fatal("Definition().Function.Parameters is not a map")
	}
	properties, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("Definition().Function.Parameters.properties is not a map")
	}
	urlProp, ok := properties["url"].(map[string]interface{})
	if !ok {
		t.Fatal("Definition().Function.Parameters.properties.url is not a map")
	}
	if urlProp["type"] != "string" {
		t.Errorf("url parameter type = %q, want %q", urlProp["type"], "string")
	}

	required, ok := params["required"].([]string)
	if !ok {
		t.Fatal("Definition().Function.Parameters.required is not a []string")
	}
	if len(required) != 1 || required[0] != "url" {
		t.Errorf("required = %v, want [url]", required)
	}
}

func TestFetchURLHandler_Validate(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
	}{
		{
			name:    "valid url",
			args:    map[string]any{"url": "https://example.com"},
			wantErr: false,
		},
		{
			name:    "missing url",
			args:    map[string]any{},
			wantErr: true,
		},
		{
			name:    "empty url",
			args:    map[string]any{"url": ""},
			wantErr: true,
		},
		{
			name:    "url is not a string",
			args:    map[string]any{"url": 12345},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewFetchURLHandler()
			err := h.Validate(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFetchURLHandler_Execute(t *testing.T) {
	tests := []struct {
		name       string
		args       map[string]any
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:       "empty url returns error",
			args:       map[string]any{"url": ""},
			wantErr:    true,
			wantErrMsg: "URL cannot be empty",
		},
		{
			name:       "missing url returns error",
			args:       map[string]any{},
			wantErr:    true,
			wantErrMsg: "URL cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewFetchURLHandler()
			env := &ToolEnv{
				ConfigManager: nil, // Not needed for empty-url error cases
			}
			result, err := h.Execute(context.Background(), env, tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				if result == nil {
					t.Error("Execute() returned nil result on error")
				} else if result.ErrorMessage == "" {
					t.Error("Execute() returned error but result.ErrorMessage is empty")
				} else if !strings.Contains(result.ErrorMessage, tt.wantErrMsg) {
					t.Errorf("Execute() ErrorMessage = %q, want to contain %q",
						result.ErrorMessage, tt.wantErrMsg)
				}
			}
		})
	}
}

func TestNewFetchURLHandler(t *testing.T) {
	h := NewFetchURLHandler()
	if h == nil {
		t.Fatal("NewFetchURLHandler() returned nil")
	}
}
