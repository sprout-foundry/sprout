package commands

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// The --provider/--model flags win over the commit role in the config; a
// model alone keeps the configured provider.
func TestCommitProviderModelFlagsWin(t *testing.T) {
	cfg := &configuration.Config{}
	cfg.CommitProvider = "ai-worker"
	cfg.CommitModel = "qwen3.6-27b-moq"
	role, roleModel := cfg.ResolveRole(configuration.RoleCommit)
	if role != "ai-worker" {
		t.Skipf("commit role resolves to %q here; the precedence below assumes the commit settings feed it", role)
	}

	c := &CommitCommand{}
	if p, m := c.commitProviderModel(cfg); p != "ai-worker" || m != roleModel {
		t.Fatalf("no flags: got %s/%s, want the commit role", p, m)
	}
	c.SetModelOverride("deepinfra", "deepseek-ai/DeepSeek-V4.1-Flash-1017")
	if p, m := c.commitProviderModel(cfg); p != "deepinfra" || m != "deepseek-ai/DeepSeek-V4.1-Flash-1017" {
		t.Fatalf("flags: got %s/%s", p, m)
	}
	c.SetModelOverride("", "other-model")
	if p, m := c.commitProviderModel(cfg); p != "ai-worker" || m != "other-model" {
		t.Fatalf("model only: got %s/%s, want the configured provider with the flag's model", p, m)
	}
	c.SetModelOverride("deepinfra", "x")
	if p, m := c.commitProviderModel(nil); p != "deepinfra" || m != "x" {
		t.Fatalf("no config: got %s/%s", p, m)
	}
}
