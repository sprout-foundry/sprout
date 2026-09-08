package secretdetect

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// fixtureJWT builds a realistic eyJ-prefixed JWT: base64url header, base64url
// payload of idp claims, and a long base64url signature (~970 chars — long
// tokens hit the same gitleaks jwt-rule capture shape).
// Not a live token — every claim is static fixture data.
func fixtureJWT(t *testing.T) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"fixture-key-id-1234"}`))
	claims := `{"sub":"1234567890abcdef","iss":"https://idp.example.com/us-east-1_AbCdEfGhI","client_id":"6abcdefghijklmnopqrstuvwxyz","origin_jti":"a1b2c3d4-e5f6-7890-abcd-ef1234567890","token_use":"access","scope":"example.signin.user.admin","auth_time":1720000000,"exp":1720003600,"iat":1720000000,"jti":"abcdef12-3456-7890-abcd-ef1234567890","username":"fixture-user","given_name":"Fixture","family_name":"User","email":"fixture@example.com","custom:tenant":"acme"}`
	payload := base64.RawURLEncoding.EncodeToString([]byte(claims))
	signature := strings.Repeat("Ab3", 96)
	signature = signature[:len(signature)-len(signature)%4]
	return header + "." + payload + "." + signature
}

// fixtureRequestBody serializes a request body whose tool-message content is
// a token-store dump. Embedding the dump as a JSON *string*
// escapes its inner quotes to \" in the serialized body — the exact shape
// that makes gitleaks' jwt capture swallow the closing escape backslash.
func fixtureRequestBody(t *testing.T) ([]byte, string) {
	t.Helper()
	jwt := fixtureJWT(t)
	inner := map[string]string{
		"accessToken":  jwt,
		"idToken":      jwt,
		"refreshToken": "fixture-refresh-token",
		"clockDrift":   "0",
	}
	innerJSON, err := json.Marshal(inner)
	if err != nil {
		t.Fatalf("marshal inner localStorage dump: %v", err)
	}
	body := map[string]interface{}{
		"model": "glm-5.3-flash",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "continue the session please"},
			{"role": "tool", "content": string(innerJSON)},
		},
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	return bodyBytes, jwt
}

// TestRedactOpaque_JSONBodyWithJWTStaysValidJSON is the core incident
// regression: redacting a serialized request body that holds a JWT inside an
// escaped JSON string must never orphan the escape backslash. Pre-fix this
// produced `"accessToken\": \"[REDACTED]"` — unbalanced quote, invalid JSON.
func TestRedactOpaque_JSONBodyWithJWTStaysValidJSON(t *testing.T) {
	body, jwt := fixtureRequestBody(t)
	if !json.Valid(body) {
		t.Fatalf("precondition: body must be valid JSON, got invalid (len=%d)", len(body))
	}

	// Precondition asserting the incident shape: the jwt rule's capture ends
	// on the backslash of an escaped quote in the serialized body.
	s, err := Default()
	if err != nil {
		t.Fatalf("Default() failed: %v", err)
	}
	matches := s.Scan(string(body))
	backslashEnding := false
	for _, m := range matches {
		if m.RuleID == "jwt" && strings.HasSuffix(m.Secret, "\\") {
			backslashEnding = true
			break
		}
	}
	if !backslashEnding {
		t.Fatalf("precondition: expected a jwt match whose Secret ends with a backslash; got %d matches", len(matches))
	}

	cases := map[string]func(string) string{
		"RedactOpaque": RedactOpaque,
		"RedactTagged": RedactTagged,
	}
	for name, redactFn := range cases {
		t.Run(name, func(t *testing.T) {
			out := redactFn(string(body))
			if strings.Contains(out, jwt) {
				t.Errorf("JWT body still present after redaction (len=%d)", len(jwt))
			}
			if !json.Valid([]byte(out)) {
				t.Fatalf("redaction corrupted valid JSON; output:\n%s", out)
			}
		})
	}
}

// TestRedact_TrimsBoundaryBackslashesFromNeedle exercises the display-layer
// Redact path with hand-built matches so leading, trailing, and degenerate
// boundary cases are covered independently of the gitleaks scanner.
func TestRedact_TrimsBoundaryBackslashesFromNeedle(t *testing.T) {
	t.Run("trailing backslash preserves escaped quote", func(t *testing.T) {
		content := `{"content":"{\"accessToken\":\"JWT-SECRET\"}"}`
		if !json.Valid([]byte(content)) {
			t.Fatalf("precondition: content must be valid JSON, got %q", content)
		}
		matches := []Match{{RuleID: "jwt", Secret: "JWT-SECRET\\", Match: "JWT-SECRET\\\"", Entropy: 5.0}}
		out := Redact(content, matches)
		if strings.Contains(out, "JWT-SECRET") {
			t.Errorf("secret still present: %s", out)
		}
		if !json.Valid([]byte(out)) {
			t.Errorf("Redact corrupted valid JSON: %s", out)
		}
	})

	t.Run("leading backslash is trimmed too", func(t *testing.T) {
		content := `prefix \JWT-SECRET suffix`
		matches := []Match{{RuleID: "jwt", Secret: "\\JWT-SECRET", Match: "\\JWT-SECRET", Entropy: 5.0}}
		out := Redact(content, matches)
		if strings.Contains(out, "JWT-SECRET") {
			t.Errorf("secret still present: %s", out)
		}
		if !strings.Contains(out, "\\[REDACTED") {
			t.Errorf("expected leading backslash to survive redaction, got: %s", out)
		}
	})

	t.Run("all-backslash secret is skipped", func(t *testing.T) {
		content := `{"k":"\\"}`
		matches := []Match{{RuleID: "jwt", Secret: "\\", Match: "\\", Entropy: 5.0}}
		out := Redact(content, matches)
		if out != content {
			t.Errorf("expected content unchanged when trimmed needle is empty, got %q", out)
		}
	})
}
