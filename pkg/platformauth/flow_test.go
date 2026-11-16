package platformauth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/mcp"
)

// fastPolls shortens the approval-poll floor so a flow test waits in
// milliseconds rather than seconds.
func fastPolls(t *testing.T) {
	t.Helper()
	original := minPollInterval
	minPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { minPollInterval = original })
}

// approvals answers the sign-in endpoints; pollsApproved is how many polls
// return "pending" before the token is handed out.
func approvals(t *testing.T, pollsApproved int, mcpAllowed bool, email string) *httptest.Server {
	t.Helper()
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cli/device":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code": "dc", "user_code": "ABCD-EFGH",
				"verification_uri": "https://platform.example/cli/link",
				"expires_in":       60,
			})
		case "/runners/device/poll":
			polls++
			if polls <= pollsApproved {
				w.WriteHeader(http.StatusPreconditionRequired)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "spt_tok", "account_email": email})
		case "/cli/whoami":
			if r.Header.Get("Authorization") != "Bearer spt_tok" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"user_id": "u1", "email": email, "plan": "pro", "mcp_allowed": mcpAllowed,
			})
		case "/cli/logout":
			if r.Header.Get("Authorization") != "Bearer spt_tok" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"status":"revoked"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFlowLoginStoresAndConnectsMCP(t *testing.T) {
	isolate(t)
	t.Setenv("SPROUT_PLATFORM_URL", "")
	fastPolls(t)
	srv := approvals(t, 1, true, "me@example.com")
	var opened []string
	out := &bytes.Buffer{}

	err := NewFlow(out).Login(context.Background(), LoginOptions{
		PlatformURL: srv.URL,
		OpenURL:     func(u string) { opened = append(opened, u) },
	})
	if err != nil {
		t.Fatalf("login = %v\n%s", err, out)
	}
	if _, token, ok := Load(); !ok || token != "spt_tok" {
		t.Fatalf("sign-in not stored: %+v %q %v", out, token, ok)
	}
	text := out.String()
	for _, want := range []string{
		"https://platform.example/cli/link", "ABCD-EFGH",
		// The whole phrase, not its halves: the sign-in reads "signed in to
		// <platform> as <email>", which is not the link message's wording.
		"Signed in to " + srv.URL + " as me@example.com",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
	if len(opened) != 1 || opened[0] != "https://platform.example/cli/link" {
		t.Errorf("browser opened %v", opened)
	}
	// Pro plans get the platform's MCP server, with the token kept out of the
	// config file.
	cfg, err := mcp.LoadGlobalMCPConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	server, found := cfg.Servers[MCPServerName]
	if !found {
		t.Fatalf("MCP server not added:\n%s", text)
	}
	if want := MCPURL(srv.URL); server.URL != want {
		t.Errorf("MCP url = %q, want %q", server.URL, want)
	}
	if strings.Contains(strings.Join([]string{server.URL, server.Headers["Authorization"]}, " "), "spt_tok") {
		t.Error("the token must not be written to the MCP config")
	}
}

func TestFlowLoginKeepsURLClosedWithoutBrowser(t *testing.T) {
	isolate(t)
	t.Setenv("SPROUT_PLATFORM_URL", "")
	fastPolls(t)
	srv := approvals(t, 0, false, "me@example.com")
	opened := 0
	out := &bytes.Buffer{}

	err := NewFlow(out).Login(context.Background(), LoginOptions{
		PlatformURL: srv.URL,
		NoBrowser:   true,
		OpenURL:     func(string) { opened++ },
	})
	if err != nil {
		t.Fatalf("login = %v", err)
	}
	if opened != 0 {
		t.Errorf("NoBrowser opened the browser %d times", opened)
	}
	if !strings.Contains(out.String(), "does not include the platform's MCP server") {
		t.Errorf("a plan without MCP should say so:\n%s", out)
	}
	cfg, err := mcp.LoadGlobalMCPConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	if _, found := cfg.Servers[MCPServerName]; found {
		t.Error("a plan without MCP must not get the server")
	}
}

func TestFlowLoginRefusesInsecurePlatform(t *testing.T) {
	isolate(t)
	t.Setenv("SPROUT_PLATFORM_URL", "")
	fastPolls(t)
	out := &bytes.Buffer{}
	err := NewFlow(out).Login(context.Background(), LoginOptions{PlatformURL: "http://evil.example"})
	if err == nil || !strings.Contains(err.Error(), "must be https") {
		t.Fatalf("err = %v, want an https refusal", err)
	}
	if _, _, ok := Load(); ok {
		t.Error("a refused sign-in must not store anything")
	}
}

func TestFlowLoginStopsWhenContextEnds(t *testing.T) {
	isolate(t)
	t.Setenv("SPROUT_PLATFORM_URL", "")
	fastPolls(t)
	srv := approvals(t, 99, true, "me@example.com") // never approved
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := NewFlow(nil).Login(ctx, LoginOptions{PlatformURL: srv.URL, NoBrowser: true}); ctx.Err() == nil {
		t.Fatalf("login returned %v with the context still live", err)
	}
}

func TestFlowStatusAndSignOut(t *testing.T) {
	isolate(t)
	t.Setenv("SPROUT_PLATFORM_URL", "")
	fastPolls(t)
	srv := approvals(t, 0, true, "me@example.com")
	out := &bytes.Buffer{}
	flow := NewFlow(out)
	if err := flow.Login(context.Background(), LoginOptions{PlatformURL: srv.URL, NoBrowser: true}); err != nil {
		t.Fatalf("login = %v", err)
	}

	out.Reset()
	if err := flow.Status(context.Background()); err != nil {
		t.Fatalf("status = %v", err)
	}
	for _, want := range []string{"me@example.com", "pro plan", MCPServerName} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("status missing %q:\n%s", want, out)
		}
	}

	out.Reset()
	if err := flow.SignOut(context.Background()); err != nil {
		t.Fatalf("sign out = %v", err)
	}
	if _, _, ok := Load(); ok {
		t.Error("still signed in after SignOut")
	}
	if !strings.Contains(out.String(), "revoked the sign-in on the platform") {
		t.Errorf("sign-out should report the revocation:\n%s", out)
	}
	cfg, err := mcp.LoadGlobalMCPConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	if _, found := cfg.Servers[MCPServerName]; found {
		t.Error("sign-out must remove the platform MCP server")
	}

	// Signing out while already signed out is not an error.
	out.Reset()
	if err := flow.SignOut(context.Background()); err != nil {
		t.Fatalf("second sign out = %v", err)
	}
	if !strings.Contains(out.String(), "Not signed in.") {
		t.Errorf("second sign out:\n%s", out)
	}
}

func TestResolvePlatformOrder(t *testing.T) {
	isolate(t)
	t.Setenv("SPROUT_PLATFORM_URL", "")
	fastPolls(t)
	if got := ResolvePlatform("https://explicit.example"); got != "https://explicit.example" {
		t.Errorf("explicit = %q", got)
	}
	t.Setenv("SPROUT_PLATFORM_URL", "https://env.example")
	if got := ResolvePlatform(""); got != "https://env.example" {
		t.Errorf("env = %q", got)
	}
	t.Setenv("SPROUT_PLATFORM_URL", "")
	if got := ResolvePlatform(""); got != "https://app.sproutfoundry.com" {
		t.Errorf("default = %q", got)
	}
	if _, err := Save(&Login{PlatformURL: "https://saved.example"}, "tok"); err != nil {
		t.Fatal(err)
	}
	if got := ResolvePlatform(""); got != "https://saved.example" {
		t.Errorf("saved = %q", got)
	}
}
