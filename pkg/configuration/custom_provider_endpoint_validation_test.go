package configuration

import (
	"strings"
	"testing"
)

func TestValidateCustomProviderEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid https", "https://api.example.com/v1", false},
		{"valid http", "http://localhost:11434/v1", false},
		{"valid with port", "https://api.openai.com:443/v1", false},
		{"valid models path", "https://api.example.com/v1/models", false},
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"no scheme", "api.example.com/v1", true},
		{"ftp scheme", "ftp://example.com/v1", true},
		{"file scheme", "file:///etc/passwd", true},
		{"missing host", "https:///v1", true},
		{"missing host no slash", "https://", true},
		{"garbage", "://not a url", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateCustomProviderEndpoint(tc.input)
			if tc.wantErr && err == nil {
				t.Errorf("ValidateCustomProviderEndpoint(%q) expected error, got nil", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("ValidateCustomProviderEndpoint(%q) unexpected error: %v", tc.input, err)
			}
		})
	}
}

func TestValidateCustomProviderEndpoint_ErrorMentionsScheme(t *testing.T) {
	err := ValidateCustomProviderEndpoint("ftp://example.com")
	if err == nil {
		t.Fatal("expected error for ftp scheme")
	}
	if !strings.Contains(err.Error(), "scheme") && !strings.Contains(err.Error(), "http") {
		t.Errorf("error should mention scheme/http, got: %v", err)
	}
}

func TestValidateCustomProviderEndpoint_ErrorMentionsHost(t *testing.T) {
	err := ValidateCustomProviderEndpoint("https://")
	if err == nil {
		t.Fatal("expected error for missing host")
	}
	if !strings.Contains(err.Error(), "host") {
		t.Errorf("error should mention host, got: %v", err)
	}
}