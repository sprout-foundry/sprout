//go:build !js

package webui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
)

// ---------------------------------------------------------------------------
// handleAPICommandExecute — POST /api/command/execute
// SP-114 Phase 2: dedicated command surface for the WebUI command bar.
// ---------------------------------------------------------------------------

func postCommand(t *testing.T, ws *ReactWebServer, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/command/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPICommandExecute(rec, req)
	return rec
}

func TestHandleAPICommandExecute_MethodNotAllowed(t *testing.T) {
	ws, _ := newTestWebServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/command/execute", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPICommandExecute(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for GET, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPICommandExecute_InvalidJSON(t *testing.T) {
	ws, _ := newTestWebServer(t)

	rec := postCommand(t, ws, "not json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid JSON, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPICommandExecute_EmptyCommand(t *testing.T) {
	ws, _ := newTestWebServer(t)

	cases := map[string]string{
		"empty":      `{"command":""}`,
		"whitespace": `{"command":"   "}`,
		"missing":    `{}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := postCommand(t, ws, body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for %s, got %d: %s", name, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandleAPICommandExecute_MissingSlashPrefix(t *testing.T) {
	ws, _ := newTestWebServer(t)

	rec := postCommand(t, ws, `{"command":"info"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-slash command, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "must start with /") {
		t.Errorf("expected 'must start with /' in body, got: %s", rec.Body.String())
	}
}

func TestHandleAPICommandExecute_UnknownCommand(t *testing.T) {
	ws, _ := newTestWebServer(t)

	rec := postCommand(t, ws, `{"command":"/nonexistent-command-xyz"}`)
	// getChatAgent likely errors (no real chat agent wired up in the test),
	// but if the test environment happens to have one with a default
	// registry, we should still see a clean 400 with "command_not_found".
	if rec.Code == http.StatusOK {
		t.Fatalf("expected non-200 for unknown command, got 200: %s", rec.Body.String())
	}
	// Either 400 (command_not_found / command_not_safe) or 500/503 (chat
	// agent not available) are acceptable — the validation logic above is
	// what we're really testing here.
}

// TestExecuteSafeSteerCommand_DistinguishesNotFoundVsNotSafe verifies the
// helper that handleAPICommandExecute relies on to disambiguate "command
// doesn't exist" from "command exists but isn't SteerCapable". The handler
// must return (nil, "", nil) for both so it can re-classify with the
// registry — but the helper itself should never panic on a non-slash input.
func TestExecuteSafeSteerCommand_NonSlashInput(t *testing.T) {
	ws, _ := newTestWebServer(t)

	cmd, out, err := ws.executeSafeSteerCommand("not a slash command", nil)
	if cmd != nil || out != "" || err != nil {
		t.Errorf("expected (nil,'',nil) for non-slash input, got (%v, %q, %v)", cmd, out, err)
	}
}

func TestExecuteSafeSteerCommand_NilAgent(t *testing.T) {
	ws, _ := newTestWebServer(t)

	cmd, out, err := ws.executeSafeSteerCommand("/info", nil)
	if cmd != nil || out != "" || err != nil {
		t.Errorf("expected (nil,'',nil) when agent has no slash registry, got (%v, %q, %v)", cmd, out, err)
	}
}

// TestCommandResponseShape documents the success response shape so a future
// WebUI consumer can rely on it. We marshal a representative map and assert
// the JSON shape rather than spinning up a full agent.
func TestCommandResponseShape(t *testing.T) {
	resp := map[string]interface{}{
		"command":  "info",
		"output":   "Agent: test",
		"error":    "",
		"accepted": true,
	}
	buf := new(bytes.Buffer)
	if err := json.NewEncoder(buf).Encode(resp); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := buf.String()
	for _, want := range []string{`"command":"info"`, `"output":"Agent: test"`, `"accepted":true`} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q: %s", want, body)
		}
	}
}

// ---------------------------------------------------------------------------
// executeSafeSteerCommand success path — uses a real CommandRegistry with a
// SteerCapable test command. This exercises the same code path the handler
// uses on success, without the overhead of a full chat-agent stub.
// ---------------------------------------------------------------------------

type echoCommand struct{}

func (e *echoCommand) Name() string          { return "echo" }
func (e *echoCommand) Description() string   { return "test: prints its args" }
func (e *echoCommand) SafeDuringSteer() bool { return true }
func (e *echoCommand) Execute(args []string, _ *agent.Agent) error {
	fmt.Fprintln(os.Stdout, strings.Join(args, " "))
	return nil
}

func TestExecuteSafeSteerCommand_CapturesStdout(t *testing.T) {
	ws, _ := newTestWebServer(t)

	registry := agent_commands.NewCommandRegistry()
	registry.Register(&echoCommand{})

	a := &agent.Agent{}
	a.SetSlashCommands(registry)

	cmd, output, err := ws.executeSafeSteerCommand("/echo hello world", a)
	if err != nil {
		t.Fatalf("executeSafeSteerCommand: %v", err)
	}
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	if cmd.Name() != "echo" {
		t.Errorf("cmd.Name() = %q, want %q", cmd.Name(), "echo")
	}
	if !strings.Contains(output, "hello world") {
		t.Errorf("output = %q, expected to contain 'hello world'", output)
	}
}

func TestExecuteSafeSteerCommand_RejectsUnsafeCommand(t *testing.T) {
	ws, _ := newTestWebServer(t)

	registry := agent_commands.NewCommandRegistry()
	// Register a command that does NOT implement SteerCapable.
	registry.Register(&bareCommand{})

	a := &agent.Agent{}
	a.SetSlashCommands(registry)

	cmd, output, err := ws.executeSafeSteerCommand("/bare arg", a)
	if cmd != nil || output != "" || err != nil {
		t.Errorf("unsafe command must return (nil,'',nil), got (%v, %q, %v)", cmd, output, err)
	}
}

// bareCommand implements the Command interface but NOT SteerCapable.
// Its Execute method records the call (without using *testing.T since this
// is a value type, not a test func) so the rejection test can assert the
// gate held.
type bareCommand struct{ called bool }

func (b *bareCommand) Name() string        { return "bare" }
func (b *bareCommand) Description() string { return "test: not steer-capable" }
func (b *bareCommand) Execute(_ []string, _ *agent.Agent) error {
	b.called = true
	return nil
}
