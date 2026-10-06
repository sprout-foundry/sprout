package runner

import (
	"strings"
	"testing"
)

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
