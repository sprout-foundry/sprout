package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/mcp"
	"github.com/sprout-foundry/sprout/pkg/platformauth"
)

// isolatePlatform points the config, credential and MCP stores at a temp dir
// so a sign-in test neither reads nor writes the developer's own sign-in.
func isolatePlatform(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SPROUT_CONFIG", filepath.Join(home, ".sprout"))
	t.Setenv("SPROUT_CREDENTIAL_BACKEND", "file")
	t.Setenv("SPROUT_PLATFORM_URL", "")
	credentials.ResetStorageBackend()
	t.Cleanup(credentials.ResetStorageBackend)
}

func TestLoginCommand_Meta(t *testing.T) {
	reg := NewCommandRegistry()
	for name, wantDesc := range map[string]string{
		"login":  "platform account",
		"logout": "Sign out",
		"whoami": "platform account",
	} {
		cmd, ok := reg.GetCommand(name)
		if !ok {
			t.Fatalf("/%s not registered", name)
		}
		if !strings.Contains(cmd.Description(), wantDesc) {
			t.Errorf("/%s description = %q, want it to mention %q", name, cmd.Description(), wantDesc)
		}
		usage, ok := cmd.(UsageProvider)
		if !ok {
			t.Fatalf("/%s has no Usage()", name)
		}
		if !strings.Contains(usage.Usage(), "/"+name) {
			t.Errorf("/help %s should start with the invocation:\n%s", name, usage.Usage())
		}
	}
}

// TestLoginCommand_SteerSafety pins whoami as the read-only surface and keeps
// the two credential-changing commands out of the mid-turn and WebUI paths.
func TestLoginCommand_SteerSafety(t *testing.T) {
	reg := NewCommandRegistry()
	for name, safe := range map[string]bool{"login": false, "logout": false, "whoami": true} {
		cmd, ok := reg.GetCommand(name)
		if !ok {
			t.Fatalf("/%s not registered", name)
		}
		sc, ok := cmd.(SteerCapable)
		if !ok {
			t.Fatalf("/%s does not implement SteerCapable", name)
		}
		if sc.SafeDuringSteer() != safe {
			t.Errorf("/%s SafeDuringSteer() = %v, want %v", name, sc.SafeDuringSteer(), safe)
		}
	}
}

func TestParseLoginArgs(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		platform  string
		noBrowser bool
		wantErr   string
	}{
		{name: "bare"},
		{name: "platform", args: []string{"https://platform.example"}, platform: "https://platform.example"},
		{name: "no browser", args: []string{"--no-browser"}, noBrowser: true},
		{name: "short flag", args: []string{"-n"}, noBrowser: true},
		{name: "both", args: []string{"https://platform.example", "--no-browser"}, platform: "https://platform.example", noBrowser: true},
		{name: "flags first", args: []string{"--no-browser", "https://platform.example"}, platform: "https://platform.example", noBrowser: true},
		{name: "unknown flag", args: []string{"--no-brain"}, wantErr: "unknown flag --no-brain"},
		{name: "two platforms", args: []string{"https://a.example", "https://b.example"}, wantErr: "one platform URL"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := parseLoginArgs(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if opts.PlatformURL != tc.platform {
				t.Errorf("PlatformURL = %q, want %q", opts.PlatformURL, tc.platform)
			}
			if opts.NoBrowser != tc.noBrowser {
				t.Errorf("NoBrowser = %v, want %v", opts.NoBrowser, tc.noBrowser)
			}
		})
	}
}

func TestLoginCommand_Complete(t *testing.T) {
	c := &LoginCommand{}
	// The completer's convention: arguments without the command name, the
	// last one the token being typed.
	if got := c.Complete([]string{""}, nil); len(got) != 1 || got[0] != "--no-browser" {
		t.Errorf("Complete at a fresh prompt = %v, want --no-browser", got)
	}
	if got := c.Complete([]string{"https://platform.example", ""}, nil); len(got) != 1 {
		t.Errorf("Complete after a platform URL = %v, want the flag still offered", got)
	}
	if got := c.Complete([]string{"--no-browser", ""}, nil); got != nil {
		t.Errorf("Complete after the flag = %v, want nothing left to suggest", got)
	}
}

func TestWhoamiCommand_ReportsSignedOut(t *testing.T) {
	isolatePlatform(t)
	out := &bytes.Buffer{}
	c := &WhoamiCommand{}
	c.SetOutput(out)
	if err := c.Execute(nil, nil); err != nil {
		t.Fatalf("a machine that is not signed in is not an error: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "Not signed in") {
		t.Errorf("output = %q, want the signed-out hint", text)
	}
	if !strings.Contains(text, "/login") {
		t.Errorf("the signed-out hint should name /login:\n%s", text)
	}
}

func TestSignOutCommands_RefuseArgs(t *testing.T) {
	isolatePlatform(t)
	for _, c := range []Command{&LogoutCommand{}, &WhoamiCommand{}} {
		out := &bytes.Buffer{}
		oc := c.(OutputCommand)
		oc.SetOutput(out)
		err := c.Execute([]string{"extra"}, nil)
		if err == nil || !strings.Contains(err.Error(), "takes no arguments") {
			t.Errorf("/%s with an argument: err = %v, want a usage error", c.Name(), err)
		}
	}
}

// TestLoginCommand_SignsInThroughTheSink drives /login against a fake
// platform and asserts the approval URL lands in the command's output: a
// person reading the chat has to see the URL and the code, so a sign-in that
// writes to os.Stdout instead would appear to hang.
func TestLoginCommand_SignsInThroughTheSink(t *testing.T) {
	isolatePlatform(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cli/device":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code": "dc", "user_code": "WXYZ-1234",
				"verification_uri": "https://platform.example/cli/link", "interval": 1, "expires_in": 30,
			})
		case "/runners/device/poll":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "spt_ok", "account_email": "me@example.com"})
		case "/cli/whoami":
			_ = json.NewEncoder(w).Encode(map[string]any{"user_id": "u", "email": "me@example.com", "plan": "free"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out := &bytes.Buffer{}
	c := &LoginCommand{}
	c.SetOutput(out)
	if err := c.Execute([]string{srv.URL, "--no-browser"}, nil); err != nil {
		t.Fatalf("login = %v\n%s", err, out)
	}
	text := out.String()
	for _, want := range []string{"WXYZ-1234", "Signed in to", "free plan"} {
		if !strings.Contains(text, want) {
			t.Errorf("/login output missing %q:\n%s", want, text)
		}
	}
}

// TestLoginCommand_RefreshesTheRunningSessionsMCP pins the end-to-end fix:
// after a successful /login the running session must see the platform's MCP
// server without restarting. The flow writes it to the project folder's
// mcp_config.json (ConfigureMCP); the command's final RefreshRuntimeConfig is
// what reconciles the live manager from that file.
func TestLoginCommand_RefreshesTheRunningSessionsMCP(t *testing.T) {
	isolatePlatform(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cli/device":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code": "dc", "user_code": "WXYZ-1234",
				"verification_uri": "https://platform.example/cli/link", "interval": 1, "expires_in": 30,
			})
		case "/runners/device/poll":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "spt_ok", "account_email": "me@example.com"})
		case "/cli/whoami":
			_ = json.NewEncoder(w).Encode(map[string]any{"user_id": "u", "email": "me@example.com", "plan": "pro", "mcp_allowed": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// A real agent on the temp config root (createTestAgentWithTempConfig
	// repoints HOME and clears SPROUT_CONFIG after isolatePlatform). Its
	// project folder is a fresh temp dir so the reconcile's project folder layer
	// reads nothing — only the global layer the login wrote to.
	chatAgent := createTestAgentWithTempConfig(t)
	chatAgent.SetProjectFolder(t.TempDir())
	out := &syncWriter{}
	c := &LoginCommand{}
	c.SetOutput(out)
	if err := c.Execute([]string{srv.URL, "--no-browser"}, chatAgent); err != nil {
		t.Fatalf("/login = %v\n%s", err, out.String())
	}

	text := out.String()
	if !strings.Contains(text, "MCP servers were refreshed") {
		t.Errorf("the in-session refresh note is missing — the running session would not see the new server:\n%s", text)
	}

	// The login wrote the platform's server into the session's config root —
	// the file the running session's reconcile reads — with auto-start so a
	// live StartAll picks it up without an extra step.
	legacy, err := mcp.LoadGlobalMCPConfigFile()
	if err != nil {
		t.Fatalf("reading the login-written mcp_config.json: %v", err)
	}
	server, ok := legacy.Servers[platformauth.MCPServerName]
	if !ok {
		t.Fatalf("the login did not register %q in the session's mcp_config.json", platformauth.MCPServerName)
	}
	if !server.AutoStart {
		t.Error("the platform's MCP server must auto-start so a running session starts it without an extra step")
	}
	if server.URL != platformauth.MCPURL(srv.URL) {
		t.Errorf("server URL = %q, want %q", server.URL, platformauth.MCPURL(srv.URL))
	}
}

// syncWriter is an io.Writer the test can read while the command writes to it.
type syncWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *syncWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// TestLoginCommand_InterruptEndsTheWait pins the one thing that makes /login
// safe to run interactively: the approval wait ends when the person
// interrupts, rather than running to the code's expiry.
func TestLoginCommand_InterruptEndsTheWait(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cli/device":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code": "dc", "user_code": "CODE-1234",
				"verification_uri": "https://platform.example/cli/link", "expires_in": 3600,
			})
		default: // never approved
			w.WriteHeader(http.StatusPreconditionRequired)
		}
	}))
	defer srv.Close()

	chatAgent := createTestAgentWithTempConfig(t)
	// An agent built inside a test binary gets a no-op interruptCancel (see
	// isRunningUnderTest), so TriggerInterrupt would cancel nothing. Install a
	// real cancel, the way the production constructor does.
	chatAgent.ClearInterrupt()
	out := &syncWriter{}
	c := &LoginCommand{}
	c.SetOutput(out)
	done := make(chan error, 1)
	go func() { done <- c.Execute([]string{srv.URL, "--no-browser"}, chatAgent) }()

	// Interrupt once the wait has actually begun. InterruptCtx heals a
	// canceled context between turns, so an interrupt that lands before the
	// command captures it belongs to the previous turn, not to this one.
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "Waiting for approval") {
		if time.Now().After(deadline) {
			t.Fatalf("never reached the approval wait:\n%s", out)
		}
		time.Sleep(10 * time.Millisecond)
	}
	chatAgent.TriggerInterrupt()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want the canceled context", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("/login kept waiting after the interrupt")
	}
}
