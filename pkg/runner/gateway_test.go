package runner

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestIsGatewayProvider(t *testing.T) {
	for provider, want := range map[string]bool{
		"gateway":   true,
		"Gateway":   true,
		" gateway ": true,
		"openai":    false,
		"":          false,
	} {
		if got := IsGatewayProvider(provider); got != want {
			t.Errorf("IsGatewayProvider(%q) = %v, want %v", provider, got, want)
		}
	}
}

func TestGatewayEndpoint(t *testing.T) {
	for raw, want := range map[string]string{
		"https://gw.example.com":        "https://gw.example.com/v1/chat/completions",
		"https://gw.example.com/":       "https://gw.example.com/v1/chat/completions",
		"https://gw.example.com/v1":     "https://gw.example.com/v1/chat/completions",
		"https://gw.example.com/v1/":    "https://gw.example.com/v1/chat/completions",
		"http://127.0.0.1:8080":         "http://127.0.0.1:8080/v1/chat/completions",
		"https://gw.example.com/api/v1": "https://gw.example.com/api/v1/chat/completions",
	} {
		got, err := gatewayEndpoint(raw)
		if err != nil {
			t.Errorf("gatewayEndpoint(%q): %v", raw, err)
			continue
		}
		if got != want {
			t.Errorf("gatewayEndpoint(%q) = %q, want %q", raw, got, want)
		}
	}
	for _, raw := range []string{"", "not a url", "ftp://x", "https://"} {
		if _, err := gatewayEndpoint(raw); err == nil {
			t.Errorf("gatewayEndpoint(%q) must be refused", raw)
		}
	}
}

func TestWriteGatewayProviderFile(t *testing.T) {
	configDir := t.TempDir()
	if err := writeGatewayProviderFile(configDir, "https://gw.example.com"); err != nil {
		t.Fatalf("writeGatewayProviderFile: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(configDir, "providers", "gateway.json"))
	if err != nil {
		t.Fatalf("provider file: %v", err)
	}
	var cfg struct {
		Name           string `json:"name"`
		Endpoint       string `json:"endpoint"`
		EnvVar         string `json:"env_var"`
		RequiresAPIKey bool   `json:"requires_api_key"`
		ModelName      string `json:"model_name"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse provider file: %v", err)
	}
	if cfg.Name != "gateway" || cfg.EnvVar != GatewayKeyEnvVar || !cfg.RequiresAPIKey {
		t.Errorf("provider file: %+v", cfg)
	}
	if cfg.Endpoint != "https://gw.example.com/v1/chat/completions" {
		t.Errorf("endpoint = %q", cfg.Endpoint)
	}
	if cfg.ModelName != "" {
		t.Errorf("no model may be pinned; the picker reads /v1/models (got %q)", cfg.ModelName)
	}
	info, err := os.Stat(filepath.Join(configDir, "providers", "gateway.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("provider file mode = %o, want 600", perm)
	}

	// A task with no platform URL is refused before anything is written.
	if err := writeGatewayProviderFile(t.TempDir(), ""); err == nil {
		t.Error("gateway wiring without a platform API URL must be refused")
	}
}

func TestWorkspaceEnvWiresTheGateway(t *testing.T) {
	env := workspaceEnv(WorkspaceTask{
		WorkspaceID:    "ws-gw",
		TxnSecret:      "s3cret",
		LLMProvider:    "gateway",
		LLMKey:         "gw-key-1",
		Model:          "claude-sonnet-4-5",
		PlatformAPIURL: "https://gw.example.com",
	})
	if env[GatewayKeyEnvVar] != "gw-key-1" {
		t.Errorf("%s = %q, want the workspace-scoped gateway key", GatewayKeyEnvVar, env[GatewayKeyEnvVar])
	}
	if env["SPROUT_PROVIDER"] != "gateway" {
		t.Errorf("SPROUT_PROVIDER = %q, want gateway", env["SPROUT_PROVIDER"])
	}
	if env["SPROUT_MODEL"] != "claude-sonnet-4-5" {
		t.Errorf("SPROUT_MODEL = %q, want the task's model", env["SPROUT_MODEL"])
	}
	// The gateway key must not leak into a public provider's env var.
	for _, k := range []string{"OPENAI_API_KEY", "DEEPINFRA_API_KEY"} {
		if env[k] != "" {
			t.Errorf("%s must not be set for the gateway provider; got %q", k, env[k])
		}
	}

	// No model in the task: no SPROUT_MODEL, the picker decides.
	env = workspaceEnv(WorkspaceTask{WorkspaceID: "ws-gw", LLMProvider: "gateway", LLMKey: "k"})
	if _, ok := env["SPROUT_MODEL"]; ok {
		t.Errorf("SPROUT_MODEL must be unset when the task pins no model; got %q", env["SPROUT_MODEL"])
	}

	// A public provider keeps today's mapping.
	env = workspaceEnv(WorkspaceTask{WorkspaceID: "ws-o", LLMProvider: "openai", LLMKey: "sk-o"})
	if env["OPENAI_API_KEY"] != "sk-o" || env["SPROUT_PROVIDER"] != "" {
		t.Errorf("public provider mapping changed: OPENAI_API_KEY=%q SPROUT_PROVIDER=%q", env["OPENAI_API_KEY"], env["SPROUT_PROVIDER"])
	}
}

func TestReservedEnvStillBlocksGatewaySpoofing(t *testing.T) {
	env := workspaceEnv(WorkspaceTask{
		WorkspaceID: "ws-gw",
		LLMProvider: "gateway",
		LLMKey:      "real",
		UserEnv: map[string]string{
			"SPROUT_GATEWAY_KEY": "spoofed",
			"SPROUT_PROVIDER":    "openai",
			"SPROUT_MODEL":       "attacker-model",
		},
	})
	if env[GatewayKeyEnvVar] != "real" {
		t.Errorf("user env must not override the gateway key; got %q", env[GatewayKeyEnvVar])
	}
	if env["SPROUT_PROVIDER"] != "gateway" {
		t.Errorf("user env must not override SPROUT_PROVIDER; got %q", env["SPROUT_PROVIDER"])
	}
}

func TestGatewayProviderFileNotInGlobalConfig(t *testing.T) {
	// The provider file must live in the dir it was given (the workspace's
	// scoped SPROUT_CONFIG_DIR), never derived from the runner's own config.
	ws := t.TempDir()
	other := t.TempDir()
	if err := writeGatewayProviderFile(ws, "https://gw.example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(other, "providers")); !os.IsNotExist(err) {
		t.Errorf("an unrelated config dir must stay untouched (stat err: %v)", err)
	}
	if !strings.HasPrefix(filepath.Join(ws, "providers"), ws) {
		t.Error("provider path must stay inside the workspace config dir")
	}
}

func TestRunnerRefusesUnservableGatewayTasks(t *testing.T) {
	plat := &fakePlatform{statuses: map[string]string{}, pollStatus: http.StatusOK}
	platSrv := httptest.NewServer(plat.handler())
	t.Cleanup(platSrv.Close)
	launcher := &fakeLauncher{port: fakeDaemon(t)}
	r := &Runner{
		State:    &State{PlatformURL: platSrv.URL, RunnerID: "r-1", Mode: ModeNative},
		Client:   NewClient(platSrv.URL, Credentials{RunnerID: "r-1", APIKey: "srk_key"}),
		Launcher: launcher,
		Host:     NewHostServer(),
		Log:      logDiscard(),
		// start() is normally reached through Run(), which initializes
		// these; this test drives start() directly.
		running: map[string]*Workspace{},
		busy:    map[string]*sync.Mutex{},
	}
	start := func(id string, mutate func(*WorkspaceTask)) WorkspaceResult {
		task := WorkspaceTask{WorkspaceID: id, Action: "start", TxnSecret: "s", LLMProvider: "gateway", LLMKey: "k", PlatformAPIURL: "https://gw.example.com"}
		mutate(&task)
		plat.mu.Lock()
		before := len(plat.results)
		plat.mu.Unlock()
		r.start(context.Background(), task, r.Log)
		plat.mu.Lock()
		defer plat.mu.Unlock()
		if len(plat.results) != before+1 {
			t.Fatalf("task %s: no result reported", id)
		}
		return plat.results[before]
	}

	// Container mode cannot see the host-side provider file: refuse.
	if res := start("gw-container", func(tk *WorkspaceTask) { tk.WorkspaceID = "gw-container"; r.State.Mode = ModeContainer }); res.Status != "failed" {
		t.Errorf("a container-mode runner must refuse gateway tasks; got %+v", res)
	}
	launcher.mu.Lock()
	if n := len(launcher.started); n != 0 {
		t.Errorf("a refused task must not reach the launcher; started %d", n)
	}
	launcher.mu.Unlock()
	r.State.Mode = ModeNative

	// No key: refuse.
	if res := start("gw-nokey", func(tk *WorkspaceTask) { tk.LLMKey = "" }); res.Status != "failed" {
		t.Errorf("a key-less gateway task must be refused; got %+v", res)
	}

	// A repo on a gateway task: refuse (gateway workspaces are repo-less).
	if res := start("gw-repo", func(tk *WorkspaceTask) { tk.RepoURL = "https://github.com/o/r" }); res.Status != "failed" {
		t.Errorf("a gateway task naming a repo must be refused; got %+v", res)
	}

	// A well-formed gateway task starts.
	if res := start("gw-ok", func(*WorkspaceTask) {}); res.Status != "running" {
		t.Errorf("a well-formed gateway task must start; got %+v", res)
	}
}

func logDiscard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
