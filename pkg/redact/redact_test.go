package redact

import (
	"strings"
	"testing"
)

// realisticOpenAIKey matches gitleaks' openai-api-key rule shape
// (sk- + 20 alphanumeric + T3BlbkFJ + 20 alphanumeric). Not a live key.
const realisticOpenAIKey = "sk-AbCdEfGhIjKlMnOpQrStT3BlbkFJ1234567890abcdefghij"

// realisticJWT is a syntactically valid JWT (header.payload.signature).
const realisticJWT = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"

func TestApply_OpenAIKey(t *testing.T) {
	input := []byte(`OPENAI_API_KEY="` + realisticOpenAIKey + `"`)
	got := string(Apply(input))
	if strings.Contains(got, realisticOpenAIKey) {
		t.Errorf("OpenAI key not redacted: %s", got)
	}
	if !strings.Contains(got, "[REDACTED:") {
		t.Errorf("expected tagged redaction token, got: %s", got)
	}
}

func TestApply_PrivateKey(t *testing.T) {
	input := []byte("-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEAabcdefghijklmnop\nMoreFakeKeyMaterialHere1234567890\n-----END RSA PRIVATE KEY-----")
	got := string(Apply(input))
	if strings.Contains(got, "MIIEowIBAAKCAQEAabcdefghijklmnop") {
		t.Errorf("Private key body not redacted: %s", got)
	}
}

func TestApply_JWT(t *testing.T) {
	input := []byte("Authorization: Bearer " + realisticJWT)
	got := string(Apply(input))
	if strings.Contains(got, realisticJWT) {
		t.Errorf("JWT not redacted: %s", got)
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
	original := []byte("AWS_SECRET_ACCESS_KEY=" + realisticOpenAIKey)
	origCopy := string(original)
	_ = Apply(original)
	if string(original) != origCopy {
		t.Error("Apply modified the original slice")
	}
}

func TestApply_EmptyInput(t *testing.T) {
	if got := Apply(nil); len(got) != 0 {
		t.Errorf("expected empty output for nil input, got %q", got)
	}
	if got := Apply([]byte{}); len(got) != 0 {
		t.Errorf("expected empty output for empty input, got %q", got)
	}
}

func TestString(t *testing.T) {
	got := String(realisticOpenAIKey)
	if strings.Contains(got, "T3BlbkFJ") {
		t.Errorf("String() did not redact: %s", got)
	}
}

func TestString_EmptyInput(t *testing.T) {
	if got := String(""); got != "" {
		t.Errorf("expected empty output for empty input, got %q", got)
	}
}
