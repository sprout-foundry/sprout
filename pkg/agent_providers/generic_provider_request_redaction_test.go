package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// redactionTestJWT builds a realistic eyJ-prefixed JWT (base64url header,
// base64url payload of idp claims, long base64url signature).
// Not a live token — every claim is static fixture data.
func redactionTestJWT(t *testing.T) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"fixture-key-id-1234"}`))
	claims := `{"sub":"1234567890abcdef","iss":"https://idp.example.com/us-east-1_AbCdEfGhI","client_id":"6abcdefghijklmnopqrstuvwxyz","origin_jti":"a1b2c3d4-e5f6-7890-abcd-ef1234567890","token_use":"access","scope":"example.signin.user.admin","auth_time":1720000000,"exp":1720003600,"iat":1720000000,"jti":"abcdef12-3456-7890-abcd-ef1234567890","username":"fixture-user","given_name":"Fixture","family_name":"User","email":"fixture@example.com","custom:tenant":"acme"}`
	payload := base64.RawURLEncoding.EncodeToString([]byte(claims))
	signature := strings.Repeat("Ab3", 96)
	signature = signature[:len(signature)-len(signature)%4]
	return header + "." + payload + "." + signature
}

// TestBuildHTTPRequestCtx_RedactionNeverCorruptsJSON is the full round-trip
// regression: a serialized request body containing a
// token-store dump (JWT inside an escaped JSON string) must
// pass through buildHTTPRequestCtx's egress redaction backstop and still be
// valid JSON with the JWT gone.
func TestBuildHTTPRequestCtx_RedactionNeverCorruptsJSON(t *testing.T) {
	jwt := redactionTestJWT(t)
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
	if !json.Valid(bodyBytes) {
		t.Fatalf("precondition: body must be valid JSON, got invalid (len=%d)", len(bodyBytes))
	}

	// Non-local endpoint so the egress backstop actually runs; bearer Key is
	// set so the auth path resolves without env vars. No network call happens
	// — buildHTTPRequestCtx only constructs the *http.Request.
	p := &GenericProvider{
		config: &ProviderConfig{
			Name:     "redaction-test",
			Endpoint: "https://api.test.invalid/v1/chat/completions",
			Auth:     AuthConfig{Type: "bearer", Key: "test-key"},
			Defaults: RequestDefaults{Model: "glm-5.3-flash"},
		},
		model: "glm-5.3-flash",
	}

	req, sentBody, err := p.buildHTTPRequestCtx(context.Background(), bodyBytes, false)
	if err != nil {
		t.Fatalf("buildHTTPRequestCtx failed: %v", err)
	}
	if req == nil {
		t.Fatal("expected non-nil *http.Request")
	}
	if !json.Valid(sentBody) {
		t.Fatalf("egress redaction corrupted valid JSON body; refusing-to-send path should have caught this\nbody:\n%s", sentBody)
	}
	if strings.Contains(string(sentBody), jwt) {
		t.Errorf("raw JWT still present in outbound body after egress redaction (len=%d)", len(jwt))
	}
}
