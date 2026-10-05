package agent

import (
	"fmt"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/factory"
	"github.com/sprout-foundry/sprout/pkg/git"
)

// handleGenerateCommitMessage generates a Conventional Commit message from
// the staged diff for the commit tool (pkg/agent_tools commitHandler).
//
// It reuses the same generator the sprout commit CLI flow uses
// (git.GenerateCommitMessageFromStagedDiff) so the tool and the CLI share
// one prompt and one LLM call and cannot drift, building the LLM client the
// same way the CLI flow does: the configured commit provider first, falling
// back to the conversation provider. Notes are passed through as generation
// context (UserInstructions).
//
// On any failure it returns an error; the commit handler then commits
// nothing — there is no placeholder fallback.
func handleGenerateCommitMessage(a *Agent, diff []byte, notes string) (string, error) {
	diffText := strings.TrimSpace(string(diff))
	if diffText == "" {
		return "", fmt.Errorf("staged diff is empty")
	}

	client := commitMessageClient(a)
	if client == nil {
		return "", fmt.Errorf("no LLM client available for commit message generation (configure a commit provider or a conversation provider)")
	}

	result, err := git.GenerateCommitMessageFromStagedDiff(client, git.CommitMessageOptions{
		Diff:             diffText,
		UserInstructions: notes,
	})
	if err != nil {
		return "", fmt.Errorf("failed to generate commit message: %w", err)
	}
	if result == nil || strings.TrimSpace(result.Message) == "" {
		return "", fmt.Errorf("commit message generator returned an empty message")
	}
	return result.Message, nil
}

// commitMessageClient builds the LLM client for commit message generation,
// mirroring prepareCommitClient in the CLI commit flow: the commit role's
// provider/model first (SP-150 §150b — the commit settings alias the commit
// role), then the conversation provider/model.
// Returns nil when no client can be created.
func commitMessageClient(a *Agent) api.ClientInterface {
	if a == nil {
		return nil
	}
	cm := a.GetConfigManager()
	if cm == nil {
		return nil
	}
	if cfg := cm.GetConfig(); cfg != nil {
		provider, model := cfg.ResolveRole(configuration.RoleCommit)
		if provider != "" {
			if client, err := factory.CreateProviderClient(api.ClientType(provider), model); err == nil {
				return client
			}
		}
	}
	provider, err := cm.GetProvider()
	if err != nil {
		return nil
	}
	client, err := factory.CreateProviderClient(provider, cm.GetModelForProvider(provider))
	if err != nil {
		return nil
	}
	return client
}
