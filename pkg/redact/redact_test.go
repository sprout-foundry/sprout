package redact

import (
	"strings"
	"testing"
)

func TestApply_AWSAccessKey(t *testing.T) {
	input := []byte(`key=AKIAIOSFODNN7EXAMPLE`)
	got := string(Apply(input))
	if strings.Contains(got, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("AWS access key not redacted: %s", got)
	}
	if !strings.Contains(got, "[REDACTED:aws-access-key]") {
		t.Errorf("expected aws-access-key label, got: %s", got)
	}
}

func TestApply_GitHubToken(t *testing.T) {
	// A raw GitHub token without env prefix should be caught by the github-token pattern
	input := []byte(`token: ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij1234`)
	got := string(Apply(input))
	if strings.Contains(got, "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij1234") {
		t.Errorf("GitHub token not redacted: %s", got)
	}
	if !strings.Contains(got, "[REDACTED:github-token]") {
		t.Errorf("expected github-token label, got: %s", got)
	}
}

func TestApply_GitHubTokenInEnv(t *testing.T) {
	// GH_TOKEN=... form is caught by the broader env-secret pattern
	input := []byte(`GH_TOKEN=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij1234`)
	got := string(Apply(input))
	if strings.Contains(got, "ghp_") {
		t.Errorf("GitHub token not redacted: %s", got)
	}
}

func TestApply_SlackToken(t *testing.T) {
	input := []byte(`token=xoxb-1234567890-1234567890123-abcdefghijklmnopqrstuvwx`)
	got := string(Apply(input))
	if strings.Contains(got, "xoxb-") {
		t.Errorf("Slack token not redacted: %s", got)
	}
}

func TestApply_OpenAIKey(t *testing.T) {
	input := []byte(`sk-proj-ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789abcdefghij`)
	got := string(Apply(input))
	if strings.Contains(got, "sk-proj-") {
		t.Errorf("OpenAI key not redacted: %s", got)
	}
	if !strings.Contains(got, "[REDACTED:api-key]") {
		t.Errorf("expected api-key label, got: %s", got)
	}
}

func TestApply_PrivateKey(t *testing.T) {
	input := []byte("-----BEGIN RSA PRIVATE KEY-----\nMIIEowI...\n-----END RSA PRIVATE KEY-----")
	got := string(Apply(input))
	if strings.Contains(got, "MIIEowI") {
		t.Errorf("Private key not redacted: %s", got)
	}
	if !strings.Contains(got, "[REDACTED:private-key]") {
		t.Errorf("expected private-key label, got: %s", got)
	}
}

func TestApply_AuthHeader(t *testing.T) {
	input := []byte("Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.x")
	got := string(Apply(input))
	if !strings.Contains(got, "[REDACTED:http-auth-header]") {
		t.Errorf("expected http-auth-header label, got: %s", got)
	}
	// The Bearer token portion should be caught by the http-auth-header pattern
	if strings.Contains(got, "Bearer eyJhbGciOiJIUzI1NiJ9") {
		t.Errorf("Bearer token not redacted: %s", got)
	}
}

func TestApply_XAPIKey(t *testing.T) {
	input := []byte("X-API-Key: my-secret-key-1234567890")
	got := string(Apply(input))
	if strings.Contains(got, "my-secret-key-1234567890") {
		t.Errorf("X-API-Key not redacted: %s", got)
	}
}

func TestApply_EnvSecret(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"TOKEN", "MY_TOKEN=secret123"},
		{"API_KEY", "API_KEY=sk-test1234567890abcdef"},
		{"SECRET", "CLIENT_SECRET=abc123def456"},
		{"PASSWORD", "DB_PASSWORD=hunter2"},
		{"colon separator", "MY_TOKEN:secret123"},
		{"PASSWD", "PASSWD=hunter2"},
		{"CREDENTIAL", "AWS_CREDENTIAL=something"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(Apply([]byte(tt.input)))
			if strings.Contains(got, "secret123") || strings.Contains(got, "hunter2") || strings.Contains(got, "something") {
				t.Errorf("env secret not redacted: input=%q got=%q", tt.input, got)
			}
		})
	}
}

func TestApply_PreservesNonSecrets(t *testing.T) {
	input := []byte("GREETING=hello\nNUMBER=42\nPATH=/usr/bin")
	got := string(Apply(input))
	if got != string(input) {
		t.Errorf("non-secret content was modified: got %q", got)
	}
}

func TestApply_DoesNotModifyOriginal(t *testing.T) {
	original := []byte("AWS_SECRET_ACCESS_KEY=mysecret")
	origCopy := string(original)
	_ = Apply(original)
	if string(original) != origCopy {
		t.Error("Apply modified the original slice")
	}
}

func TestString(t *testing.T) {
	got := String("sk-proj-supersecret12345678901234567890")
	if strings.Contains(got, "supersecret") {
		t.Errorf("String() did not redact: %s", got)
	}
}
