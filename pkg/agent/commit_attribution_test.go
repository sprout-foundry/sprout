package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/buildinfo"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/git"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setCommitAttribution(t *testing.T, a *Agent, mutate func(cfg *configuration.Config)) {
	t.Helper()
	require.NoError(t, a.GetConfigManager().UpdateConfigNoSave(func(cfg *configuration.Config) error {
		mutate(cfg)
		return nil
	}))
}

func TestAttributeCommitMessage_OffByDefault(t *testing.T) {
	a := newTestAgent(t)
	assert.False(t, a.GetConfig().CommitAttributionEnabled())
	assert.Equal(t, "fix: x", a.AttributeCommitMessage("fix: x"))
	assert.Equal(t, "fix: x", a.attributeToolCommit("fix: x", true))
	assert.Equal(t, "fix: x", buildAgentToolFuncs(a).AttributeCommitMessage("fix: x", false))
}

func TestAttributeCommitMessage_Trailer(t *testing.T) {
	old := buildinfo.Version
	buildinfo.Version = "v0.30.0"
	t.Cleanup(func() { buildinfo.Version = old })

	a := newTestAgent(t)
	setCommitAttribution(t, a, func(cfg *configuration.Config) {
		cfg.CommitAttribution = configuration.CommitAttributionTrailer
	})
	agentLine := git.SproutAssistant(a.GetProvider(), a.GetModel()).Trailer()
	require.True(t, strings.HasPrefix(agentLine, "Assisted-by: sprout/0.30.0 ("), agentLine)

	got := a.AttributeCommitMessage("fix: x")
	assert.Equal(t, "fix: x\n\n"+agentLine+"\n", got)
	// Re-running (an amend, or a hook that fires twice) adds nothing.
	assert.Equal(t, got, a.AttributeCommitMessage(got))

	// The tool hook used by the commit tools is wired per agent.
	assert.Equal(t, got, buildAgentToolFuncs(a).AttributeCommitMessage("fix: x", false))
	assert.NotContains(t, strings.ToLower(got), "co-authored-by")
}

func TestAttributeToolCommit_GeneratedListsCommitModel(t *testing.T) {
	old := buildinfo.Version
	buildinfo.Version = "dev"
	t.Cleanup(func() { buildinfo.Version = old })

	a := newTestAgent(t)
	setCommitAttribution(t, a, func(cfg *configuration.Config) {
		cfg.CommitAttribution = configuration.CommitAttributionTrailer
		cfg.CommitProvider = "openai"
		cfg.CommitModel = "gpt-5"
	})
	provider, model := commitMessageModel(a)
	require.Equal(t, "openai", provider)
	require.Equal(t, "gpt-5", model)

	got := a.attributeToolCommit("fix: x", true)
	assert.Contains(t, got, git.SproutAssistant(a.GetProvider(), a.GetModel()).Trailer())
	assert.Contains(t, got, "Assisted-by: sprout/dev (openai/gpt-5)")

	// A caller-written message names only the agent's model.
	assert.NotContains(t, a.attributeToolCommit("fix: x", false), "openai/gpt-5")
}

func TestCommitAttributionSetting(t *testing.T) {
	cfg := &configuration.Config{}
	require.NoError(t, SetSettingValue(cfg, "commit_attribution", "Trailer"))
	assert.Equal(t, configuration.CommitAttributionTrailer, cfg.CommitAttribution)
	v, err := GetSettingValue(cfg, "commit_attribution")
	require.NoError(t, err)
	assert.Equal(t, "trailer", v)
	require.NoError(t, SetSettingValue(cfg, "commit_attribution", "off"))
	assert.False(t, cfg.CommitAttributionEnabled())
	assert.Error(t, SetSettingValue(cfg, "commit_attribution", "co-author"))
	assert.Equal(t, []string{"off", "trailer"}, SettingEnumValues("commit_attribution"))
}

// TestExecuteCommit_Attribution covers the shell-redirect commit path
// (git commit typed into shell_command → handleCommitTool → executeCommit):
// the trailer is appended when on, absent when off.
func TestExecuteCommit_Attribution(t *testing.T) {
	for _, on := range []bool{false, true} {
		a := newTestAgent(t)
		dir := t.TempDir()
		runGit := func(args ...string) string {
			cmd := exec.Command("git", args...) //nolint:gosec // G204: test-driven git invocations with controlled args
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, "git %v: %s", args, out)
			return strings.TrimSpace(string(out))
		}
		runGit("init", "-q")
		runGit("config", "user.email", "test@example.com")
		runGit("config", "user.name", "Test")
		runGit("config", "commit.gpgsign", "false")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644))
		runGit("add", "a.txt")
		a.SetWorkspaceRoot(dir)
		if on {
			setCommitAttribution(t, a, func(cfg *configuration.Config) {
				cfg.CommitAttribution = configuration.CommitAttributionTrailer
			})
		}

		_, err := executeCommit("fix: add a", "", nil, a)
		require.NoError(t, err)
		body := runGit("log", "-1", "--format=%B")
		trailer := git.SproutAssistant(a.GetProvider(), a.GetModel()).Trailer()
		if on {
			assert.Equal(t, "fix: add a\n\n"+trailer, body)
		} else {
			assert.Equal(t, "fix: add a", body)
		}
	}
}
