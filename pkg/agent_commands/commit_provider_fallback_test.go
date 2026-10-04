package commands

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPrepareCommitClient_FallsBackToLastUsedProvider verifies that when no
// explicit commit provider is configured, the commit path resolves the
// last-used provider (the documented default for the CommitProvider field)
// and creates a client for it instead of falling through to manual input.
func TestPrepareCommitClient_FallsBackToLastUsedProvider(t *testing.T) {
	cfg := &configuration.Config{
		CommitProvider:   "",
		LastUsedProvider: string(api.TestClientType),
	}

	client, clientType, _ := (&CommitCommand{}).prepareCommitClient(cfg, nil)
	require.NotNil(t, client, "commit provider unset must fall back to the last-used provider")
	assert.Equal(t, api.ClientType(api.TestClientType), clientType)
}

// TestPrepareCommitClient_ExplicitCommitProviderWins verifies that an
// explicitly configured commit provider still wins over the last-used
// fallback (the last-used provider has no key configured, so a broken
// precedence would leave the client nil).
func TestPrepareCommitClient_ExplicitCommitProviderWins(t *testing.T) {
	cfg := &configuration.Config{
		CommitProvider:   string(api.TestClientType),
		LastUsedProvider: "openrouter",
	}

	client, clientType, _ := (&CommitCommand{}).prepareCommitClient(cfg, nil)
	require.NotNil(t, client, "the explicit commit provider must win over the last-used fallback")
	assert.Equal(t, api.ClientType(api.TestClientType), clientType)
}

// TestPrepareCommitClient_NoProviderAtAll verifies that with no commit
// provider, no last-used provider, and no chat agent, no client can be
// resolved — the caller must surface a clear error under --skip-prompt
// rather than aborting on an empty-message prompt.
func TestPrepareCommitClient_NoProviderAtAll(t *testing.T) {
	cfg := &configuration.Config{
		CommitProvider:   "",
		LastUsedProvider: "",
	}

	client, _, _ := (&CommitCommand{}).prepareCommitClient(cfg, nil)
	assert.Nil(t, client, "no provider at all must resolve to a nil client")
}
