package agent

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// newCommitTestManager creates an isolated configuration manager in a temp
// directory so tests never touch the caller's real config.
func newCommitTestManager(t *testing.T) *configuration.Manager {
	t.Helper()
	mgr, err := configuration.NewManagerWithDir(t.TempDir() + "/.sprout")
	require.NoError(t, err)
	return mgr
}

// TestCommitMessageClient_FallsBackToConversationProvider verifies that with
// no commit provider configured, the conversation provider is used for commit
// message generation.
func TestCommitMessageClient_FallsBackToConversationProvider(t *testing.T) {
	mgr := newCommitTestManager(t)
	require.NoError(t, mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.LastUsedProvider = string(api.TestClientType)
		return nil
	}))
	agent := &Agent{configManager: mgr}

	client := commitMessageClient(agent)
	require.NotNil(t, client, "conversation provider must be used when no commit provider is set")
	assert.Equal(t, string(api.TestClientType), client.GetProvider())
}

// TestCommitMessageClient_CommitProviderWins verifies that an explicitly
// configured commit provider takes precedence over the conversation provider.
// The conversation provider is set to ollama (which would need a live local
// Ollama server), so if the precedence were broken the client creation would
// fail and the test would report a nil client.
func TestCommitMessageClient_CommitProviderWins(t *testing.T) {
	mgr := newCommitTestManager(t)
	require.NoError(t, mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.LastUsedProvider = string(api.OllamaClientType)
		cfg.SetCommitProvider(string(api.TestClientType))
		return nil
	}))
	agent := &Agent{configManager: mgr}

	client := commitMessageClient(agent)
	require.NotNil(t, client, "the configured commit provider must win over the conversation provider")
	assert.Equal(t, string(api.TestClientType), client.GetProvider(), "the commit provider must win over the conversation provider")
}

// TestCommitMessageClient_NoProviderAtAll verifies the nil result when neither
// a commit provider nor a conversation provider can be resolved.
func TestCommitMessageClient_NoProviderAtAll(t *testing.T) {
	mgr := newCommitTestManager(t)
	agent := &Agent{configManager: mgr}
	assert.Nil(t, commitMessageClient(agent))
}

// TestCommitMessageClient_NilManager verifies the nil result when the agent
// has no configuration manager at all.
func TestCommitMessageClient_NilManager(t *testing.T) {
	agent := &Agent{}
	assert.Nil(t, commitMessageClient(agent))
}

// TestHandleGenerateCommitMessage_EmptyDiff verifies the guard against an
// empty staged diff (the commit handler never calls the generator with one).
func TestHandleGenerateCommitMessage_EmptyDiff(t *testing.T) {
	agent := &Agent{}
	_, err := handleGenerateCommitMessage(agent, []byte("  \n\t"), "notes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "staged diff is empty")
}

// TestHandleGenerateCommitMessage_NoClient verifies that a generation attempt
// without any resolvable LLM client fails with a clear error — the commit
// tool then commits nothing.
func TestHandleGenerateCommitMessage_NoClient(t *testing.T) {
	mgr := newCommitTestManager(t)
	agent := &Agent{configManager: mgr}
	_, err := handleGenerateCommitMessage(agent, []byte("diff --git a/a.txt b/a.txt\n+1\n"), "notes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no LLM client")
}
