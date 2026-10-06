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

// TestRedactOpaqueJSON_StaysValidJSON proves the JSON-aware redaction never
// corrupts a serialized payload, including the escape-adjacent shape that
// byte-level RedactOpaque risks. It also checks the secret is actually
// removed and non-string structure (keys, numbers) survives.
func TestRedactOpaqueJSON_StaysValidJSON(t *testing.T) {
	jwt := fixtureJWT(t)
	// Raw JSON bodies exercising escape-adjacent placements of the secret.
	bodies := []string{
		`{"k":"` + jwt + `"}`,
		`{"k":"` + jwt + `\u0041"}`,
		`{"k":"` + jwt + `\n"}`,
		`{"k":"` + jwt + `\\"}`,
		`{"c":"{\"accessToken\":\"` + jwt + `\"}"}`,
		`{"messages":[{"role":"tool","content":"` + jwt + `"}],"n":3,"ok":true}`,
	}
	for i, b := range bodies {
		if !json.Valid([]byte(b)) {
			t.Fatalf("case %d precondition: input must be valid JSON: %s", i, b)
		}
		out := RedactOpaqueJSON(b)
		if !json.Valid([]byte(out)) {
			t.Errorf("case %d: RedactOpaqueJSON produced invalid JSON:\n  in : %s\n  out: %s", i, b, out)
		}
		if strings.Contains(out, jwt) {
			t.Errorf("case %d: secret survived redaction", i)
		}
		if !strings.Contains(out, "[REDACTED]") {
			t.Errorf("case %d: expected a [REDACTED] token, got %s", i, out)
		}
	}

	// Non-string structure and keys are preserved.
	in := `{"model":"gpt","n":3,"ok":true,"key":"` + jwt + `"}`
	out := RedactOpaqueJSON(in)
	var got, want map[string]any
	if err := json.Unmarshal([]byte(in), &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output not parseable: %v (%s)", err, out)
	}
	if got["model"] != "gpt" || got["n"].(float64) != 3 || got["ok"].(bool) != true {
		t.Errorf("non-string fields altered: %s", out)
	}
	if got["key"] != "[REDACTED]" {
		t.Errorf("secret value not redacted: %v", got["key"])
	}
}

// TestRedactOpaqueJSON_NonJSONFallsBack: a non-JSON body is handled by the
// byte-level path unchanged.
func TestRedactOpaqueJSON_NonJSONFallsBack(t *testing.T) {
	in := "plain text with " + fixtureJWT(t) + " inline"
	out := RedactOpaqueJSON(in)
	if strings.Contains(out, fixtureJWT(t)) == false && !strings.Contains(out, "[REDACTED]") {
		t.Errorf("expected the inline secret to be redacted, got: %s", out)
	}
}

// TestRedactOpaqueJSON_CleanPayloadUnchanged: a JSON payload with no secret is
// returned byte-for-byte (no re-marshaling / key reordering).
func TestRedactOpaqueJSON_CleanPayloadUnchanged(t *testing.T) {
	// Non-canonical key order/whitespace that Marshal would normalize.
	in := `{"b": 1, "a":  2, "nested": {"z": "plain value", "y": [1, 2, 3]}}`
	if !json.Valid([]byte(in)) {
		t.Fatalf("precondition: must be valid JSON")
	}
	out := RedactOpaqueJSON(in)
	if out != in {
		t.Errorf("clean payload was reformatted:\n  in : %s\n  out: %s", in, out)
	}
}

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
