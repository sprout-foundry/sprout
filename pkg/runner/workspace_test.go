package runner

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func TestWorkspaceEnvAppliesTheTaskModelForEveryProvider(t *testing.T) {
	cases := []struct {
		name         string
		task         WorkspaceTask
		wantModel    string
		wantProvider string
	}{
		{"public provider", WorkspaceTask{LLMProvider: "openai", LLMKey: "sk", Model: " gpt-test "}, "gpt-test", "openai"},
		{"gateway", WorkspaceTask{LLMProvider: "gateway", LLMKey: "gw", Model: "m1"}, "m1", GatewayProviderName},
		{"model without key", WorkspaceTask{LLMProvider: "deepseek", Model: "deepseek-chat"}, "deepseek-chat", "deepseek"},
		{"unknown provider is not pinned", WorkspaceTask{LLMProvider: "nope", LLMKey: "k", Model: "m2"}, "m2", ""},
		{"no model leaves the daemon default", WorkspaceTask{LLMProvider: "openai", LLMKey: "sk"}, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.task.WorkspaceID = "ws-1"
			env := workspaceEnv(tc.task)
			if env["SPROUT_MODEL"] != tc.wantModel {
				t.Errorf("SPROUT_MODEL = %q, want %q", env["SPROUT_MODEL"], tc.wantModel)
			}
			if env["SPROUT_PROVIDER"] != tc.wantProvider {
				t.Errorf("SPROUT_PROVIDER = %q, want %q", env["SPROUT_PROVIDER"], tc.wantProvider)
			}
		})
	}
}

// The daemon resolves its starting model with configuration.ResolveProviderModel;
// the env the runner sets must win over the config's provider default there.
func TestTaskModelTakesEffectInTheDaemonsResolution(t *testing.T) {
	env := workspaceEnv(WorkspaceTask{WorkspaceID: "ws-1", LLMProvider: "openai", LLMKey: "sk", Model: "task-model"})
	t.Setenv("SPROUT_PROVIDER", env["SPROUT_PROVIDER"])
	t.Setenv("SPROUT_MODEL", env["SPROUT_MODEL"])
	provider, model, err := configuration.ResolveProviderModel(configuration.NewConfig(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if provider != "openai" || model != "task-model" {
		t.Errorf("resolved %s/%s, want openai/task-model", provider, model)
	}
}

func TestWorkspaceEnvUserVarsNeverOverrideTheRunner(t *testing.T) {
	env := workspaceEnv(WorkspaceTask{
		WorkspaceID: "ws-1", RepoURL: "https://github.com/o/r", TxnSecret: "s3cret",
		GitToken: "gh-tok", LLMProvider: "openai", LLMKey: "sk-llm",
		UserEnv: map[string]string{
			"NPM_TOKEN":         "npm-1",
			"SPROUT_AUTH_TOKEN": "spoofed",
			"WORKSPACE_TOKEN":   "spoofed",
			"REPO_URL":          "https://evil.example/r",
			"PATH":              "/evil",
			"BAD-NAME":          "x",
			"OPENAI_API_KEY":    "user-wins-only-if-unset",
		},
	})
	want := map[string]string{
		"SPROUT_AUTH_TOKEN": "s3cret",
		"WORKSPACE_TOKEN":   "s3cret",
		"REPO_URL":          "https://github.com/o/r",
		"SPROUT_GIT_TOKEN":  "gh-tok",
		"SPROUT_GIT_HOST":   "github.com",
		"OPENAI_API_KEY":    "sk-llm",
		"NPM_TOKEN":         "npm-1",
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("%s = %q, want %q", k, env[k], v)
		}
	}
	for _, k := range []string{"PATH", "BAD-NAME"} {
		if _, ok := env[k]; ok {
			t.Errorf("%s must not be settable from user env", k)
		}
	}
}

func TestGitAuthEnvKeepsTheTokenOutOfArguments(t *testing.T) {
	env := gitAuthEnv("https://github.com/o/r", "gh-tok")
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "GIT_CONFIG_KEY_0=http.extraHeader") || strings.Contains(joined, "gh-tok") {
		t.Errorf("token must be base64'd into a config env var, not passed raw: %v", env)
	}
	if gitAuthEnv("https://github.com/o/r", "") != nil {
		t.Error("no token, no auth env")
	}
}

func TestWorkspaceDirRejectsPathTricks(t *testing.T) {
	t.Setenv("SPROUT_STATE_DIR", t.TempDir())
	for _, bad := range []string{"", "..", "../x", "a/b", `a\b`, ".hidden", strings.Repeat("a", 200)} {
		if _, err := workspaceDir(bad); err == nil {
			t.Errorf("workspaceDir(%q) must be rejected", bad)
		}
	}
	if _, err := workspaceDir("6f1c2a3e-1b2c-4d5e-8f90-123456789abc"); err != nil {
		t.Errorf("a UUID must be accepted: %v", err)
	}
}

func TestOnlyHTTPSReposAreCloned(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://github.com/o/r":     true,
		"file:///Users/me/secrets":   false,
		"ext::sh -c touch% /tmp/pwn": false,
		"ssh://git@github.com/o/r":   false,
		"http://github.com/o/r":      false,
		"-uhttps://x":                false,
		"":                           false,
	} {
		if got := cloneableRepoURL(raw); got != want {
			t.Errorf("cloneableRepoURL(%q) = %v, want %v", raw, got, want)
		}
	}
}
