//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
)

// ---------------------------------------------------------------------------
// handleAPICommandComplete — POST /api/command/complete
// Exposes slash-command name + argument completion to the browser UI,
// mirroring cmd/slash_completer.go over HTTP.
// ---------------------------------------------------------------------------

// postCommandComplete drives POST /api/command/complete with the given JSON
// body and returns the recorder.
func postCommandComplete(t *testing.T, ws *ReactWebServer, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/command/complete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPICommandComplete(rec, req)
	return rec
}

// newCommandCompleteHarness wires a chat session agent into the
// ReactWebServer so handleAPICommandComplete can resolve it, mirroring
// newCommandOutputTestHarness from api_command_test.go. The agent carries
// the caller-supplied registry (pass agent_commands.DefaultRegistry() for
// the full command set).
func newCommandCompleteHarness(t *testing.T, registry *agent_commands.CommandRegistry) *ReactWebServer {
	t.Helper()
	ws, _ := newTestWebServer(t)

	const chatID = "default"
	ws.mutex.Lock()
	ctx := ws.clientContexts["test-client"]
	if ctx == nil {
		ctx = &webClientContext{}
		ws.clientContexts["test-client"] = ctx
	}
	ctx.DefaultChatID = chatID
	if ctx.ChatSessions == nil {
		ctx.ChatSessions = make(map[string]*chatSession)
	}
	cs, ok := ctx.ChatSessions[chatID]
	if !ok {
		cs = newChatSession(chatID, "Test Chat")
		ctx.ChatSessions[chatID] = cs
	}
	if cs.Agent == nil {
		// Initialise the agent's sub-managers so chatSession.getOrCreateAgent's
		// "Agent already exists" path (SetEventMetadata + EnableStreaming)
		// doesn't panic on nil pointers.
		a := &agent.Agent{}
		a.InitSubManagersForTest()
		a.SetEventBus(ws.eventBus)
		a.SetSlashCommands(registry)
		cs.Agent = a
	}
	ws.mutex.Unlock()
	return ws
}

// commandCompleteAPIResponse is a typed view of the 200 response body.
type commandCompleteAPIResponse struct {
	Command     string `json:"command"`
	Completions []struct {
		Text        string `json:"text"`
		Description string `json:"description"`
	} `json:"completions"`
}

func decodeCommandComplete(t *testing.T, rec *httptest.ResponseRecorder) commandCompleteAPIResponse {
	t.Helper()
	var resp commandCompleteAPIResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

func commandCompleteTexts(resp commandCompleteAPIResponse) []string {
	texts := make([]string, 0, len(resp.Completions))
	for _, c := range resp.Completions {
		texts = append(texts, c.Text)
	}
	return texts
}

func hasCommandCompletion(resp commandCompleteAPIResponse, text string) bool {
	for _, c := range resp.Completions {
		if c.Text == text {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Validation and method gate
// ---------------------------------------------------------------------------

func TestHandleAPICommandComplete_MethodNotAllowed(t *testing.T) {
	ws := newCommandCompleteHarness(t, agent_commands.DefaultRegistry())

	req := httptest.NewRequest(http.MethodGet, "/api/command/complete", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPICommandComplete(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for GET, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPICommandComplete_BadRequest(t *testing.T) {
	ws := newCommandCompleteHarness(t, agent_commands.DefaultRegistry())

	cases := map[string]string{
		"invalid json":       "not json",
		"empty body":         "",
		"empty command":      `{"command": ""}`,
		"whitespace command": `{"command": "   "}`,
		"missing command":    `{}`,
		"no slash prefix":    `{"command": "nope"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := postCommandComplete(t, ws, body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "code") {
				t.Errorf("expected JSON error envelope with code field, got: %s", rec.Body.String())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Argument phase — Complete(args, agent) is delegated to, and the last
// argument is the prefix the Complete implementations filter on.
// ---------------------------------------------------------------------------

// TestHandleAPICommandComplete_ArgumentPrefix verifies the core contract:
// "/risk-profile per" prefix-filters the built-in profile names down to
// "permissive" (and drops "readonly"), and the response carries the
// canonical command name.
func TestHandleAPICommandComplete_ArgumentPrefix(t *testing.T) {
	ws := newCommandCompleteHarness(t, agent_commands.DefaultRegistry())

	rec := postCommandComplete(t, ws, `{"command": "/risk-profile per"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	resp := decodeCommandComplete(t, rec)
	if resp.Command != "risk-profile" {
		t.Errorf("command = %q, want %q", resp.Command, "risk-profile")
	}
	if !hasCommandCompletion(resp, "permissive") {
		t.Errorf("completions = %v, want permissive", commandCompleteTexts(resp))
	}
	if hasCommandCompletion(resp, "readonly") {
		t.Errorf("completions = %v, must NOT contain readonly (prefix 'per' filters it out)", commandCompleteTexts(resp))
	}
	// Argument candidates carry no description.
	for _, c := range resp.Completions {
		if c.Description != "" {
			t.Errorf("argument completion %q has description %q, want empty", c.Text, c.Description)
		}
	}
}

// TestHandleAPICommandComplete_TrailingSpace verifies that a trailing space
// appends an empty argument, so Complete() filters on "" and returns every
// candidate (all built-in profiles plus the subcommands).
func TestHandleAPICommandComplete_TrailingSpace(t *testing.T) {
	ws := newCommandCompleteHarness(t, agent_commands.DefaultRegistry())

	rec := postCommandComplete(t, ws, `{"command": "/risk-profile "}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	resp := decodeCommandComplete(t, rec)
	texts := commandCompleteTexts(resp)
	for _, want := range []string{"readonly", "cautious", "default", "permissive", "unrestricted", "clear", "list", "show"} {
		if !hasCommandCompletion(resp, want) {
			t.Errorf("completions = %v, missing %q (trailing space should offer all candidates)", texts, want)
		}
	}
}

// TestHandleAPICommandComplete_VerboseTrailingSpace verifies the /verbose
// argument completions (compact/default/verbose).
func TestHandleAPICommandComplete_VerboseTrailingSpace(t *testing.T) {
	ws := newCommandCompleteHarness(t, agent_commands.DefaultRegistry())

	rec := postCommandComplete(t, ws, `{"command": "/verbose "}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	resp := decodeCommandComplete(t, rec)
	texts := commandCompleteTexts(resp)
	for _, want := range []string{"compact", "default", "verbose"} {
		if !hasCommandCompletion(resp, want) {
			t.Errorf("completions = %v, missing %q", texts, want)
		}
	}
}

// TestHandleAPICommandComplete_AliasResolution verifies that an alias head
// (/m → /model) resolves to the canonical name in the response.
func TestHandleAPICommandComplete_AliasResolution(t *testing.T) {
	ws := newCommandCompleteHarness(t, agent_commands.DefaultRegistry())

	rec := postCommandComplete(t, ws, `{"command": "/m "}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	resp := decodeCommandComplete(t, rec)
	if resp.Command != "model" {
		t.Errorf("command = %q, want %q (alias /m must resolve to canonical /model)", resp.Command, "model")
	}
}

// TestHandleAPICommandComplete_NoCompleteImplementation verifies that a
// registered command without CompletableCommand (/info) returns an empty
// completions array — not an error.
func TestHandleAPICommandComplete_NoCompleteImplementation(t *testing.T) {
	ws := newCommandCompleteHarness(t, agent_commands.DefaultRegistry())

	rec := postCommandComplete(t, ws, `{"command": "/info "}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	resp := decodeCommandComplete(t, rec)
	if resp.Completions == nil {
		t.Errorf("completions = nil, want empty array")
	}
	if len(resp.Completions) != 0 {
		t.Errorf("completions = %v, want empty", commandCompleteTexts(resp))
	}
	if resp.Command != "info" {
		t.Errorf("command = %q, want %q", resp.Command, "info")
	}
}

// TestHandleAPICommandComplete_UnknownCommandWithArgs verifies that an
// unregistered command with arguments returns 200 with an empty
// completions array and an empty command field.
func TestHandleAPICommandComplete_UnknownCommandWithArgs(t *testing.T) {
	ws := newCommandCompleteHarness(t, agent_commands.DefaultRegistry())

	rec := postCommandComplete(t, ws, `{"command": "/nosuchcmd foo"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	resp := decodeCommandComplete(t, rec)
	if resp.Command != "" {
		t.Errorf("command = %q, want \"\" (command not resolvable)", resp.Command)
	}
	if len(resp.Completions) != 0 {
		t.Errorf("completions = %v, want empty", commandCompleteTexts(resp))
	}
}

// ---------------------------------------------------------------------------
// Name phase — command-name candidates with descriptions.
// ---------------------------------------------------------------------------

// TestHandleAPICommandComplete_NamePhase verifies the no-space path returns
// matching command names with non-empty descriptions and no steer filtering.
func TestHandleAPICommandComplete_NamePhase(t *testing.T) {
	ws := newCommandCompleteHarness(t, agent_commands.DefaultRegistry())

	rec := postCommandComplete(t, ws, `{"command": "/risk"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	resp := decodeCommandComplete(t, rec)
	// "/risk" is not an exact command name, so no canonical resolution.
	if resp.Command != "" {
		t.Errorf("command = %q, want \"\" (partial name)", resp.Command)
	}
	found := false
	for _, c := range resp.Completions {
		if !strings.HasPrefix(c.Text, "/") {
			t.Errorf("name-phase completion %q must start with /", c.Text)
		}
		if c.Text == "/risk-profile" {
			found = true
			if c.Description == "" {
				t.Errorf("description for /risk-profile = %q, want non-empty", c.Description)
			}
		}
	}
	if !found {
		t.Errorf("completions = %v, want /risk-profile", commandCompleteTexts(resp))
	}
}

// TestHandleAPICommandComplete_NamePhaseIncludesAliases verifies that the
// name phase offers aliases (e.g. "m" → /m) in addition to canonical names.
func TestHandleAPICommandComplete_NamePhaseIncludesAliases(t *testing.T) {
	ws := newCommandCompleteHarness(t, agent_commands.DefaultRegistry())

	rec := postCommandComplete(t, ws, `{"command": "/m"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	resp := decodeCommandComplete(t, rec)
	if !hasCommandCompletion(resp, "/m") {
		t.Errorf("completions = %v, want alias /m", commandCompleteTexts(resp))
	}
	if !hasCommandCompletion(resp, "/model") {
		t.Errorf("completions = %v, want canonical /model", commandCompleteTexts(resp))
	}
	// Alias candidates carry the canonical command's description.
	for _, c := range resp.Completions {
		if c.Text == "/m" && c.Description == "" {
			t.Errorf("alias /m description = %q, want non-empty (resolved via canonical command)", c.Description)
		}
	}
}

// ---------------------------------------------------------------------------
// Payload bound — completions are capped at maxCommandCompletions.
// ---------------------------------------------------------------------------

// manyCompleteCommand returns more candidates than the cap so the handler
// must truncate the response.
type manyCompleteCommand struct{}

func (m *manyCompleteCommand) Name() string          { return "many" }
func (m *manyCompleteCommand) Description() string   { return "test: many candidates" }
func (m *manyCompleteCommand) SafeDuringSteer() bool { return true }
func (m *manyCompleteCommand) Execute(_ []string, _ *agent.Agent) error {
	return nil
}
func (m *manyCompleteCommand) Complete(_ []string, _ *agent.Agent) []string {
	out := make([]string, 100)
	for i := range out {
		out[i] = "candidate-" + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('0'+i%10))
	}
	return out
}

func TestHandleAPICommandComplete_CapsAtMax(t *testing.T) {
	registry := agent_commands.NewCommandRegistry()
	registry.Register(&manyCompleteCommand{})
	ws := newCommandCompleteHarness(t, registry)

	rec := postCommandComplete(t, ws, `{"command": "/many "}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	resp := decodeCommandComplete(t, rec)
	if len(resp.Completions) != maxCommandCompletions {
		t.Errorf("completions = %d entries, want capped at %d", len(resp.Completions), maxCommandCompletions)
	}
}
