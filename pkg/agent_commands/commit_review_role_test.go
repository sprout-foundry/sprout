package commands

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCommitReviewTestManager creates an isolated configuration manager in a
// temp directory so tests never touch the caller's real config.
func newCommitReviewTestManager(t *testing.T) *configuration.Manager {
	t.Helper()
	mgr, err := configuration.NewManagerWithDir(t.TempDir() + "/.sprout")
	require.NoError(t, err)
	return mgr
}

// TestReviewFlowClient_RoleReviewerResolvesClient verifies (SP-150 §150b)
// that the commit-review flow resolves its LLM client through the reviewer
// role: with only roles.reviewer set (no legacy review settings, no
// last-used provider), the role's provider is used.
func TestReviewFlowClient_RoleReviewerResolvesClient(t *testing.T) {
	mgr := newCommitReviewTestManager(t)
	require.NoError(t, mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.Roles = map[string]configuration.RoleConfig{
			configuration.RoleReviewer: {Provider: string(api.TestClientType), Model: "review-role-model"},
		}
		return nil
	}))

	client := reviewFlowClient(mgr)
	require.NotNil(t, client, "the reviewer role must resolve a client")
	assert.Equal(t, string(api.TestClientType), client.GetProvider())
}

// TestReviewFlowClient_ReviewSettingsAliasReviewerRole verifies the 150.2
// alias direction at the call site: the legacy review settings resolve the
// reviewer role, so the pre-role configuration keeps working through the
// role resolver.
func TestReviewFlowClient_ReviewSettingsAliasReviewerRole(t *testing.T) {
	mgr := newCommitReviewTestManager(t)
	require.NoError(t, mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.ReviewProvider = string(api.TestClientType)
		cfg.ReviewModel = "legacy-review-model"
		return nil
	}))

	client := reviewFlowClient(mgr)
	require.NotNil(t, client, "the legacy review settings must resolve a client via the reviewer role")
	assert.Equal(t, string(api.TestClientType), client.GetProvider())
}

// TestReviewFlowClient_RoleReviewerBeatsLastUsedProvider verifies the
// precedence: an explicit reviewer role entry wins over the conversation's
// last-used provider (the last-used provider is an unreachable local
// Ollama, so a broken precedence would leave the client nil).
func TestReviewFlowClient_RoleReviewerBeatsLastUsedProvider(t *testing.T) {
	mgr := newCommitReviewTestManager(t)
	require.NoError(t, mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.LastUsedProvider = string(api.OllamaClientType)
		cfg.Roles = map[string]configuration.RoleConfig{
			configuration.RoleReviewer: {Provider: string(api.TestClientType), Model: "review-role-model"},
		}
		return nil
	}))

	client := reviewFlowClient(mgr)
	require.NotNil(t, client, "the reviewer role must beat the last-used provider")
	assert.Equal(t, string(api.TestClientType), client.GetProvider())
}

// TestReviewFlowClient_NoSelectionReturnsNil verifies the fallback shape:
// with no reviewer role, no review settings, and no last-used provider, no
// client can be resolved and the caller does the heuristic review.
func TestReviewFlowClient_NoSelectionReturnsNil(t *testing.T) {
	mgr := newCommitReviewTestManager(t)
	assert.Nil(t, reviewFlowClient(mgr))
}

// TestReviewFlowClient_NilManager verifies the nil-manager guard.
func TestReviewFlowClient_NilManager(t *testing.T) {
	assert.Nil(t, reviewFlowClient(nil))
}
