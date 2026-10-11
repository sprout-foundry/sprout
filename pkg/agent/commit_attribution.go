package agent

import (
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/git"
)

// AttributeCommitMessage appends the opt-in Assisted-by trailer
// (commit_attribution: "trailer") to a commit message sprout is about to
// create on this agent's behalf. The agent's own provider/model — the one
// that made the changes in this session — is always listed; extra lists
// further tool+model pairs that contributed, such as the model that wrote
// the commit message. Duplicates collapse to one line. With the setting off
// (the default) the message is returned unchanged.
func (a *Agent) AttributeCommitMessage(message string, extra ...git.Assistant) string {
	if a == nil || !a.GetConfig().CommitAttributionEnabled() {
		return message
	}
	assistants := append([]git.Assistant{git.SproutAssistant(a.GetProvider(), a.GetModel())}, extra...)
	return git.AppendAssistedBy(message, assistants...)
}

// attributeToolCommit is the commit tools' attribution hook: the agent's
// model, plus the commit-message model when the message was generated.
func (a *Agent) attributeToolCommit(message string, generated bool) string {
	if !generated {
		return a.AttributeCommitMessage(message)
	}
	provider, model := commitMessageModel(a)
	return a.AttributeCommitMessage(message, git.SproutAssistant(provider, model))
}

// commitMessageModel reports the provider/model commit message generation
// uses, with the same precedence as commitMessageClient: the commit role
// first, then the conversation provider/model.
func commitMessageModel(a *Agent) (provider, model string) {
	if a == nil {
		return "", ""
	}
	cm := a.GetConfigManager()
	if cm == nil {
		return "", ""
	}
	if cfg := cm.GetConfig(); cfg != nil {
		if p, m := cfg.ResolveRole(configuration.RoleCommit); p != "" {
			return p, m
		}
	}
	p, err := cm.GetProvider()
	if err != nil {
		return "", ""
	}
	return string(p), cm.GetModelForProvider(p)
}
