package trace

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/secretdetect"
)

// fixtureJWT builds a realistic eyJ-prefixed JWT (base64url header, base64url
// payload, long base64url signature) that the gitleaks default ruleset
// detects. Every claim is static fixture data — not a live token.
func fixtureJWT(t *testing.T) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"fixture-key-id-1234"}`))
	claims := `{"sub":"1234567890abcdef","iss":"https://idp.example.com/us-east-1_AbCdEfGhI","client_id":"6abcdefghijklmnopqrstuvwxyz","origin_jti":"a1b2c3d4-e5f6-7890-abcd-ef1234567890","token_use":"access","scope":"example.signin.user.admin","auth_time":1720000000,"exp":1720003600,"iat":1720000000,"jti":"abcdef12-3456-7890-abcd-ef1234567890","username":"fixture-user","given_name":"Fixture","family_name":"User","email":"fixture@example.com","custom:tenant":"acme"}`
	payload := base64.RawURLEncoding.EncodeToString([]byte(claims))
	signature := strings.Repeat("Ab3", 96)
	signature = signature[:len(signature)-len(signature)%4]
	return header + "." + payload + "." + signature
}

func mustDetect(t *testing.T, content string) {
	t.Helper()
	if got := secretdetect.RedactOpaque(content); got == content {
		t.Fatalf("fixture secret was not detected by the default scanner; test fixture is stale: %q", content)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode().Perm()
}

// TestTraceFiles_OwnerOnlyPermissions pins the on-disk hygiene: the run
// directory and every JSONL file it holds are owner-only, since they capture
// raw prompts and responses.
func TestTraceFiles_OwnerOnlyPermissions(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("NewTraceSession: %v", err)
	}
	defer s.Close()

	if got := fileMode(t, s.GetRunDir()); got != 0o700 {
		t.Errorf("run dir mode = %o, want 700", got)
	}

	for _, name := range []string{"runs.jsonl", "turns.jsonl", "tool_calls.jsonl", "artifacts_manifest.jsonl"} {
		if got := fileMode(t, filepath.Join(s.GetRunDir(), name)); got != 0o600 {
			t.Errorf("%s mode = %o, want 600", name, got)
		}
	}
}

// TestTraceFiles_ExistingLoosePermissionsAreTightened covers the case where a
// pre-existing file/dir at the run path carries world-readable permissions:
// opening with O_CREATE would leave them untouched, so the writer must tighten
// them explicitly.
func TestTraceFiles_ExistingLoosePermissionsAreTightened(t *testing.T) {
	traceDir := t.TempDir()

	predictedRunID := predictNextRunID(t)
	runDir := filepath.Join(traceDir, predictedRunID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("pre-create run dir: %v", err)
	}
	if err := os.Chmod(runDir, 0o755); err != nil {
		t.Fatalf("chmod run dir: %v", err)
	}
	for _, name := range []string{"runs.jsonl", "turns.jsonl", "tool_calls.jsonl", "artifacts_manifest.jsonl"} {
		p := filepath.Join(runDir, name)
		if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
			t.Fatalf("pre-create %s: %v", name, err)
		}
		if err := os.Chmod(p, 0o644); err != nil {
			t.Fatalf("chmod %s: %v", name, err)
		}
	}

	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("NewTraceSession: %v", err)
	}
	defer s.Close()

	if got := fileMode(t, s.GetRunDir()); got != 0o700 {
		t.Errorf("run dir mode = %o, want 700", got)
	}
	for _, name := range []string{"runs.jsonl", "turns.jsonl", "tool_calls.jsonl", "artifacts_manifest.jsonl"} {
		if got := fileMode(t, filepath.Join(s.GetRunDir(), name)); got != 0o600 {
			t.Errorf("%s mode = %o, want 600", name, got)
		}
	}
}

// TestRecordTurn_RedactsSecretByDefault verifies a secret in a prompt/response
// is replaced by the opaque token in the written JSONL, and the line stays
// valid JSON.
func TestRecordTurn_RedactsSecretByDefault(t *testing.T) {
	jwt := fixtureJWT(t)
	mustDetect(t, jwt)

	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("NewTraceSession: %v", err)
	}
	if !s.Redact {
		t.Fatal("redaction must be enabled by default")
	}

	record := TurnRecord{
		RunID:        s.GetRunID(),
		TurnIndex:    0,
		SystemPrompt: "You are a helpful assistant.",
		UserPrompt:   "here is my token " + jwt + " please continue",
		MessagesSent: []api.Message{
			{Role: "user", Content: "token: " + jwt},
		},
		RawResponse: `{"content":"echo ` + jwt + `"}`,
		Timestamp:   "2024-06-15T10:30:00Z",
	}
	if err := s.RecordTurn(record); err != nil {
		t.Fatalf("RecordTurn: %v", err)
	}
	s.Close()

	data, err := os.ReadFile(filepath.Join(s.GetRunDir(), "turns.jsonl"))
	if err != nil {
		t.Fatalf("read turns.jsonl: %v", err)
	}
	if strings.Contains(string(data), jwt) {
		t.Errorf("raw JWT leaked into turns.jsonl: %s", data)
	}
	if !strings.Contains(string(data), "[REDACTED]") {
		t.Errorf("expected [REDACTED] token in turns.jsonl, got: %s", data)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	if !scanner.Scan() {
		t.Fatal("no line in turns.jsonl")
	}
	var got TurnRecord
	if err := json.Unmarshal(scanner.Bytes(), &got); err != nil {
		t.Fatalf("redacted line is not valid JSON: %v\nline: %s", err, scanner.Text())
	}
	if strings.Contains(got.UserPrompt, jwt) || strings.Contains(got.RawResponse, jwt) {
		t.Error("decoded record still contains the raw secret")
	}
	if !strings.Contains(got.UserPrompt, "[REDACTED]") {
		t.Errorf("decoded UserPrompt not redacted: %q", got.UserPrompt)
	}
}

// TestRecordToolCall_RedactsSecretByDefault covers the tool-call path (args
// and results carry content too).
func TestRecordToolCall_RedactsSecretByDefault(t *testing.T) {
	jwt := fixtureJWT(t)
	mustDetect(t, jwt)

	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("NewTraceSession: %v", err)
	}

	record := ToolCallRecord{
		RunID:       s.GetRunID(),
		ToolName:    "shell_command",
		Args:        map[string]interface{}{"command": "curl -H 'Authorization: Bearer " + jwt + "'"},
		FullResult:  "response body contained " + jwt,
		ModelResult: "ok",
		Timestamp:   "2024-06-15T10:31:00Z",
	}
	if err := s.RecordToolCall(record); err != nil {
		t.Fatalf("RecordToolCall: %v", err)
	}
	s.Close()

	data, err := os.ReadFile(filepath.Join(s.GetRunDir(), "tool_calls.jsonl"))
	if err != nil {
		t.Fatalf("read tool_calls.jsonl: %v", err)
	}
	if strings.Contains(string(data), jwt) {
		t.Errorf("raw JWT leaked into tool_calls.jsonl: %s", data)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	if !scanner.Scan() {
		t.Fatal("no line in tool_calls.jsonl")
	}
	if err := json.Unmarshal(scanner.Bytes(), &ToolCallRecord{}); err != nil {
		t.Fatalf("redacted tool-call line is not valid JSON: %v", err)
	}
}

// TestRecordTurn_UnredactedOptionWritesRaw covers the explicit opt-in: with
// redaction disabled the content is written verbatim.
func TestRecordTurn_UnredactedOptionWritesRaw(t *testing.T) {
	jwt := fixtureJWT(t)
	mustDetect(t, jwt)

	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3", WithRedaction(false))
	if err != nil {
		t.Fatalf("NewTraceSession: %v", err)
	}
	if s.Redact {
		t.Fatal("expected Redact=false with WithRedaction(false)")
	}

	record := TurnRecord{
		RunID:      s.GetRunID(),
		TurnIndex:  0,
		UserPrompt: "token " + jwt,
		Timestamp:  "2024-06-15T10:30:00Z",
	}
	if err := s.RecordTurn(record); err != nil {
		t.Fatalf("RecordTurn: %v", err)
	}
	s.Close()

	data, err := os.ReadFile(filepath.Join(s.GetRunDir(), "turns.jsonl"))
	if err != nil {
		t.Fatalf("read turns.jsonl: %v", err)
	}
	if !strings.Contains(string(data), jwt) {
		t.Errorf("expected raw JWT with redaction disabled, got: %s", data)
	}
	if strings.Contains(string(data), "[REDACTED]") {
		t.Errorf("unexpected redaction token with redaction disabled: %s", data)
	}
}

// TestRedaction_PreservesJSONLEscapes is the JSONL-validity guard: a secret
// adjacent to a JSON escape (here a quote inside a string value) must not
// orphan the escape and corrupt the line.
func TestRedaction_PreservesJSONLEscapes(t *testing.T) {
	jwt := fixtureJWT(t)
	mustDetect(t, jwt)

	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("NewTraceSession: %v", err)
	}

	// A message whose Content is a JSON dump holding the JWT — serializing it
	// escapes the inner quotes, the shape that byte-level redaction can break.
	inner := `{"accessToken":"` + jwt + `","note":"has \"quotes\" and a \\ backslash"}`
	record := TurnRecord{
		RunID:     s.GetRunID(),
		TurnIndex: 0,
		MessagesSent: []api.Message{
			{Role: "tool", Content: inner},
		},
		Timestamp: "2024-06-15T10:30:00Z",
	}
	if err := s.RecordTurn(record); err != nil {
		t.Fatalf("RecordTurn: %v", err)
	}
	s.Close()

	data, err := os.ReadFile(filepath.Join(s.GetRunDir(), "turns.jsonl"))
	if err != nil {
		t.Fatalf("read turns.jsonl: %v", err)
	}
	if strings.Contains(string(data), jwt) {
		t.Errorf("raw JWT leaked: %s", data)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	if !scanner.Scan() {
		t.Fatal("no line in turns.jsonl")
	}
	var got TurnRecord
	if err := json.Unmarshal(scanner.Bytes(), &got); err != nil {
		t.Fatalf("redaction corrupted the JSONL line: %v\nline: %s", err, scanner.Text())
	}
	if len(got.MessagesSent) != 1 || !strings.Contains(got.MessagesSent[0].Content, "[REDACTED]") {
		t.Errorf("expected redacted message content, got %+v", got.MessagesSent)
	}
}

// TestRedaction_AllFilesValidJSONL writes through every writer with a secret
// present and confirms each file parses line-by-line.
func TestRedaction_AllFilesValidJSONL(t *testing.T) {
	jwt := fixtureJWT(t)
	mustDetect(t, jwt)

	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("NewTraceSession: %v", err)
	}
	if err := s.RecordTurn(TurnRecord{RunID: s.GetRunID(), UserPrompt: jwt, Timestamp: "2024-01-01T00:00:00Z"}); err != nil {
		t.Fatalf("RecordTurn: %v", err)
	}
	if err := s.RecordToolCall(ToolCallRecord{RunID: s.GetRunID(), ToolName: "t", FullResult: jwt, Timestamp: "2024-01-01T00:00:00Z"}); err != nil {
		t.Fatalf("RecordToolCall: %v", err)
	}
	if err := s.RecordArtifact(ArtifactManifest{RunID: s.GetRunID(), RelativePath: jwt, Timestamp: "2024-01-01T00:00:00Z"}); err != nil {
		t.Fatalf("RecordArtifact: %v", err)
	}
	s.Close()

	for _, name := range []string{"runs.jsonl", "turns.jsonl", "tool_calls.jsonl", "artifacts_manifest.jsonl"} {
		data, err := os.ReadFile(filepath.Join(s.GetRunDir(), name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.Contains(string(data), jwt) {
			t.Errorf("%s leaked the raw secret", name)
		}
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		scanner.Buffer(make([]byte, 1<<20), 1<<20)
		for scanner.Scan() {
			if !json.Valid(scanner.Bytes()) {
				t.Errorf("%s has an invalid JSON line: %s", name, scanner.Text())
			}
		}
	}
}
