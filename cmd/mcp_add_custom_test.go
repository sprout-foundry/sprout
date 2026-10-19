//go:build !js

package cmd

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/envutil"
	"github.com/sprout-foundry/sprout/pkg/mcp"
	"github.com/sprout-foundry/sprout/pkg/testutil"
)

// scriptedCustomAdd builds the stdin script a piped `sprout mcp add` run
// reads: the server-type menu choice, then the flow's prompts in order.
func scriptedCustomAdd(menuChoice string, answers ...string) string {
	lines := append([]string{menuChoice}, answers...)
	return strings.Join(lines, "\n") + "\n"
}

func loadSavedMCPServer(t *testing.T) mcp.MCPConfig {
	t.Helper()

	raw, err := os.ReadFile(configFile(t))
	if err != nil {
		t.Fatalf("read saved mcp config: %v", err)
	}
	var cfg mcp.MCPConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal saved mcp config: %v", err)
	}
	return cfg
}

func savedCustomServer(t *testing.T, name string) mcp.MCPServerConfig {
	t.Helper()
	cfg := loadSavedMCPServer(t)
	srv, ok := cfg.Servers[name]
	if !ok {
		t.Fatalf("server %q missing from saved config: %s", name, mustJSONIndent(cfg))
	}
	return srv
}

func configFile(t *testing.T) string {
	t.Helper()
	configDir, err := envutil.ConfigDir()
	if err != nil {
		t.Fatalf("resolve config dir: %v", err)
	}
	return filepath.Join(configDir, "mcp_config.json")
}

func mustJSONIndent(v interface{}) string {
	raw, _ := json.MarshalIndent(v, "", "  ")
	return string(raw)
}

// forceNonTTY pins the TTY check to false so the scripted flows take the
// non-TTY branch even when the surrounding environment runs with a TTY stdin
// (e.g. an interactive test wrapper).
func forceNonTTY(t *testing.T) {
	t.Helper()
	prev := testIsTerminal
	testIsTerminal = func() bool { return false }
	t.Cleanup(func() { testIsTerminal = prev })
}

func TestSetupCustomMCPServer_HTTPFlow(t *testing.T) {
	setupMCPTestEnv(t)
	forceNonTTY(t)

	input := scriptedCustomAdd("2",
		"remote",
		"https://mcp.example.com/mcp",
		// Comma+space separates headers, so a comma inside a value survives.
		"Authorization: Bearer tok-123, Accept: text/html,application/xhtml+xml",
		"",   // env
		"n",  // auto-start
		"45", // timeout
	)
	mcpCfg := mcp.MCPConfig{Servers: make(map[string]mcp.MCPServerConfig), Enabled: true}

	out := testutil.CaptureStdout(t, func() {
		if err := setupCustomMCPServer(&mcpCfg, bufio.NewReader(strings.NewReader(input)), mcp.NewMCPServerRegistry()); err != nil {
			t.Fatalf("setupCustomMCPServer: %v", err)
		}
	})

	for _, want := range []string{"http", "Enter server URL", "configured successfully"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Enter command to run the server") {
		t.Errorf("HTTP flow must not prompt for a command:\n%s", out)
	}

	srv, ok := mcpCfg.Servers["remote"]
	if !ok {
		t.Fatalf("server not added to in-memory config: %s", mustJSONIndent(mcpCfg))
	}
	if srv.Type != "http" {
		t.Errorf("Type = %q, want http", srv.Type)
	}
	if srv.URL != "https://mcp.example.com/mcp" {
		t.Errorf("URL = %q", srv.URL)
	}
	if srv.Command != "" {
		t.Errorf("Command = %q, want empty for HTTP server", srv.Command)
	}
	if got := srv.Headers["Authorization"]; !mcp.IsSecretRef(got) {
		t.Errorf("Headers[Authorization] = %q, want a credential placeholder — a bearer token must not land in the config file", got)
	}
	if got := srv.Headers["Accept"]; got != "text/html,application/xhtml+xml" {
		t.Errorf("Headers[Accept] = %q — a comma inside a header value must survive", got)
	}
	if raw, err := os.ReadFile(configFile(t)); err == nil && strings.Contains(string(raw), "Bearer tok-123") {
		t.Error("config file contains the plaintext bearer token")
	}
	if got, _, err := credentials.GetFromActiveBackend(mcp.CredentialKey("remote", "Authorization")); err != nil || got != "Bearer tok-123" {
		t.Errorf("credential store should hold the bearer token, got %q (err=%v)", got, err)
	}
	if srv.AutoStart {
		t.Error("AutoStart = true, want false")
	}
	if srv.Timeout.String() != "45s" {
		t.Errorf("Timeout = %s", srv.Timeout)
	}

	persisted := savedCustomServer(t, "remote")
	if persisted.Type != "http" || persisted.URL != "https://mcp.example.com/mcp" {
		t.Errorf("persisted server = %+v", persisted)
	}
}

func TestSetupCustomMCPServer_StdioFlowUnchanged(t *testing.T) {
	setupMCPTestEnv(t)
	forceNonTTY(t)

	input := scriptedCustomAdd("1",
		"local",
		"npx",
		"-y some-server",
		"LOG_LEVEL=debug",
		"/tmp/workdir",
		"", // auto-start default (Y)
		"", // timeout default (30s)
	)
	mcpCfg := mcp.MCPConfig{Servers: make(map[string]mcp.MCPServerConfig), Enabled: true}

	out := testutil.CaptureStdout(t, func() {
		if err := setupCustomMCPServer(&mcpCfg, bufio.NewReader(strings.NewReader(input)), mcp.NewMCPServerRegistry()); err != nil {
			t.Fatalf("setupCustomMCPServer: %v", err)
		}
	})

	if !strings.Contains(out, "Enter command to run the server") {
		t.Errorf("stdio flow should prompt for a command:\n%s", out)
	}
	if strings.Contains(out, "Enter server URL") {
		t.Errorf("stdio flow must not prompt for a URL:\n%s", out)
	}

	srv, ok := mcpCfg.Servers["local"]
	if !ok {
		t.Fatalf("server not added to in-memory config: %s", mustJSONIndent(mcpCfg))
	}
	if srv.Type != "stdio" {
		t.Errorf("Type = %q, want stdio", srv.Type)
	}
	if srv.Command != "npx" {
		t.Errorf("Command = %q", srv.Command)
	}
	if strings.Join(srv.Args, " ") != "-y some-server" {
		t.Errorf("Args = %v", srv.Args)
	}
	if srv.Env["LOG_LEVEL"] != "debug" {
		t.Errorf("Env = %v", srv.Env)
	}
	if srv.WorkingDir != "/tmp/workdir" {
		t.Errorf("WorkingDir = %q", srv.WorkingDir)
	}
	if !srv.AutoStart {
		t.Error("AutoStart = false, want default true")
	}
	if srv.Timeout.String() != "30s" {
		t.Errorf("Timeout = %s", srv.Timeout)
	}

	persisted := savedCustomServer(t, "local")
	if persisted.Command != "npx" || persisted.URL != "" {
		t.Errorf("persisted server = %+v", persisted)
	}
}

func TestSetupCustomMCPServer_HTTPRequiresURL(t *testing.T) {
	setupMCPTestEnv(t)
	forceNonTTY(t)

	input := scriptedCustomAdd("2",
		"broken",
		"", // URL left empty
	)
	mcpCfg := mcp.MCPConfig{Servers: make(map[string]mcp.MCPServerConfig), Enabled: true}

	var addErr error
	testutil.CaptureStdout(t, func() {
		addErr = setupCustomMCPServer(&mcpCfg, bufio.NewReader(strings.NewReader(input)), mcp.NewMCPServerRegistry())
	})

	if addErr == nil {
		t.Fatal("expected error for HTTP server without URL, got nil")
	}
	if !strings.Contains(addErr.Error(), "URL is required") {
		t.Errorf("error should name the missing URL, got: %v", addErr)
	}
	if _, exists := mcpCfg.Servers["broken"]; exists {
		t.Error("rejected server must not be added to the config")
	}
}

func TestSetupCustomMCPServer_HTTPRejectsMalformedURL(t *testing.T) {
	setupMCPTestEnv(t)
	forceNonTTY(t)

	input := scriptedCustomAdd("2",
		"broken",
		"mcp.example.com/mcp", // no scheme
	)
	mcpCfg := mcp.MCPConfig{Servers: make(map[string]mcp.MCPServerConfig), Enabled: true}

	var addErr error
	testutil.CaptureStdout(t, func() {
		addErr = setupCustomMCPServer(&mcpCfg, bufio.NewReader(strings.NewReader(input)), mcp.NewMCPServerRegistry())
	})

	if addErr == nil {
		t.Fatal("expected error for malformed URL, got nil")
	}
	if !strings.Contains(addErr.Error(), "not a valid HTTP URL") {
		t.Errorf("error should name the URL problem, got: %v", addErr)
	}
}

func TestSetupCustomMCPServer_StdioCommandThatLooksLikeURL(t *testing.T) {
	setupMCPTestEnv(t)
	forceNonTTY(t)

	input := scriptedCustomAdd("1",
		"oops",
		"https://mcp.example.com/mcp",
	)
	mcpCfg := mcp.MCPConfig{Servers: make(map[string]mcp.MCPServerConfig), Enabled: true}

	var addErr error
	testutil.CaptureStdout(t, func() {
		addErr = setupCustomMCPServer(&mcpCfg, bufio.NewReader(strings.NewReader(input)), mcp.NewMCPServerRegistry())
	})

	if addErr == nil {
		t.Fatal("expected error when a URL is entered as the command, got nil")
	}
	if !strings.Contains(addErr.Error(), "looks like a URL") || !strings.Contains(addErr.Error(), "HTTP server") {
		t.Errorf("error should redirect to the HTTP flow, got: %v", addErr)
	}
	if _, exists := mcpCfg.Servers["oops"]; exists {
		t.Error("rejected server must not be added to the config")
	}
}

func TestSetupCustomMCPServer_CancelAtTypePicker(t *testing.T) {
	setupMCPTestEnv(t)
	forceNonTTY(t)

	mcpCfg := mcp.MCPConfig{Servers: make(map[string]mcp.MCPServerConfig), Enabled: true}

	var addErr error
	out := testutil.CaptureStdoutAndStderr(t, func() {
		addErr = setupCustomMCPServer(&mcpCfg, bufio.NewReader(strings.NewReader("\n")), mcp.NewMCPServerRegistry())
	})

	if addErr != nil {
		t.Fatalf("cancel at the type picker should return nil, got: %v", addErr)
	}
	if !strings.Contains(out, "Setup cancelled") {
		t.Errorf("expected cancellation message:\n%s", out)
	}
	if len(mcpCfg.Servers) != 0 {
		t.Errorf("no server should be added on cancel, got %v", mcpCfg.Servers)
	}
}

func TestLooksLikeHTTPURL(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"https://mcp.example.com/mcp", true},
		{"http://localhost:3000/mcp", true},
		{"mcp.example.com/mcp", false},
		{"ftp://example.com", false},
		{"npx", false},
		{"", false},
		{"http://", false},
	}
	for _, tc := range cases {
		if got := looksLikeHTTPURL(tc.in); got != tc.want {
			t.Errorf("looksLikeHTTPURL(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestValidateHTTPURL(t *testing.T) {
	if err := validateHTTPURL("https://example.com/mcp"); err != nil {
		t.Errorf("valid URL rejected: %v", err)
	}
	if err := validateHTTPURL("example.com/mcp"); err == nil {
		t.Error("scheme-less host accepted")
	}
}

func TestMCPAddMenuItems_CustomEntryCoversBothTypes(t *testing.T) {
	templates := mcp.NewMCPServerRegistry().ListTemplates()

	items := mcpAddMenuItems(templates)

	custom := -1
	for i, item := range items {
		if item.Value == "__custom__" {
			custom = i
		}
		if item.Value == "http-generic" || item.Value == "stdio-generic" {
			t.Errorf("generic template %q must stay filtered from the menu", item.Value)
		}
	}
	if custom < 0 {
		t.Fatal("custom entry missing from the menu")
	}
	label := items[custom].Label
	if !strings.Contains(label, "stdio") || !strings.Contains(label, "http") {
		t.Errorf("menu label %q must promise exactly what the custom flow supports", label)
	}

	// The custom flow really does offer both branches: the type picker's
	// values must include stdio and http.
	_, ok, err := promptChoice(bufio.NewReader(strings.NewReader("1\n")), "types", []console.SelectItem{
		{Label: "stdio", Value: "stdio"},
		{Label: "http", Value: "http"},
	})
	if err != nil || !ok {
		t.Fatalf("promptChoice stdio branch failed: ok=%v err=%v", ok, err)
	}
}

// customMenuChoice returns the 1-based menu position of the custom entry —
// the fallback picker takes a position, and template order is map-random.
func customMenuChoice(t *testing.T) string {
	t.Helper()
	for i, item := range mcpAddMenuItems(mcp.NewMCPServerRegistry().ListTemplates()) {
		if item.Value == "__custom__" {
			return strconv.Itoa(i + 1)
		}
	}
	t.Fatal("custom entry missing from the menu")
	return ""
}

// TestRunMCPAdd_CustomHTTPMenuToEndToEnd drives the piped `sprout mcp add`
// session end to end: the template menu choice, then the custom flow's
// prompts. Both pickers and the flow share one buffered reader, so the
// whole script is consumed in order.
func TestRunMCPAdd_CustomHTTPMenuToEndToEnd(t *testing.T) {
	setupMCPTestEnv(t)
	forceNonTTY(t)
	restoreStdin := replaceStdinWithClosedPipe(t)
	defer restoreStdin()

	input := scriptedCustomAdd(customMenuChoice(t),
		"2", // http at the custom flow's type picker
		"e2e",
		"https://mcp.example.com/mcp",
		"",  // headers
		"",  // env
		"n", // auto-start
		"",  // timeout
	)
	replaceStdinWithContent(t, input)

	var addErr error
	testutil.CaptureStdout(t, func() {
		addErr = runMCPAdd()
	})
	if addErr != nil {
		t.Fatalf("runMCPAdd: %v", addErr)
	}

	persisted := savedCustomServer(t, "e2e")
	if persisted.Type != "http" || persisted.URL != "https://mcp.example.com/mcp" {
		t.Errorf("persisted server = %+v", persisted)
	}
}

// replaceStdinWithContent swaps os.Stdin for a pipe fed the given script.
// Returns a restore function.
func replaceStdinWithContent(t *testing.T, content string) (restore func()) {
	t.Helper()
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	if _, err := w.WriteString(content); err != nil {
		t.Fatalf("failed to write stdin script: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("failed to close stdin script writer: %v", err)
	}
	os.Stdin = r
	return func() {
		_ = r.Close()
		os.Stdin = oldStdin
	}
}

// TestMCPTest_HTTPServerAddedViaCustomFlow proves `sprout mcp test` exercises
// an HTTP server added by the custom flow: the manager routes Type=http to the
// HTTP client, which performs the Streamable-HTTP handshake against a live
// httptest MCP endpoint.
func TestMCPTest_HTTPServerAddedViaCustomFlow(t *testing.T) {
	setupMCPTestEnv(t)
	forceNonTTY(t)

	var initializeCalled, toolsCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody struct {
			ID     interface{} `json:"id"`
			Method string      `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch reqBody.Method {
		case "initialize":
			initializeCalled = true
			w.Header().Set("Mcp-Session-Id", "session-1")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","capabilities":{}}}`))
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			toolsCalled = true
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"echo","description":"echoes","inputSchema":{"type":"object"}}]}}`))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	input := scriptedCustomAdd("2",
		"httptest",
		server.URL,
		"",   // headers
		"",   // env
		"n",  // auto-start
		"10", // timeout seconds
	)
	mcpCfg := mcp.MCPConfig{Servers: make(map[string]mcp.MCPServerConfig), Enabled: true}

	testutil.CaptureStdout(t, func() {
		if err := setupCustomMCPServer(&mcpCfg, bufio.NewReader(strings.NewReader(input)), mcp.NewMCPServerRegistry()); err != nil {
			t.Fatalf("setupCustomMCPServer: %v", err)
		}
	})

	var testErr error
	out := testutil.CaptureStdoutAndStderr(t, func() {
		testErr = runMCPTest("httptest")
	})
	if testErr != nil {
		t.Fatalf("runMCPTest on HTTP server: %v", testErr)
	}

	if !initializeCalled || !toolsCalled {
		t.Errorf("HTTP server must receive the initialize+tools/list handshake (initialize=%v tools/list=%v)", initializeCalled, toolsCalled)
	}
	for _, want := range []string{"URL: ", "Server initialized successfully", "Found 1 tools", "Test completed successfully"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in test output:\n%s", want, out)
		}
	}
}
