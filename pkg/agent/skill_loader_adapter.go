package agent

import (
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// skillLoaderAdapter wraps the agent's skill-loading capability for use
// by the new interface-based tool handlers. It lives in pkg/agent (not
// pkg/agent_tools) to avoid an import cycle — the adapter needs access to
// agent.GetConfigManager().GetConfig() to resolve skills.
type skillLoaderAdapter struct {
	agent *Agent
}

// newSkillLoaderAdapter creates a tools.SkillLoader backed by the agent.
func newSkillLoaderAdapter(a *Agent) tools.SkillLoader {
	if a == nil {
		return nil
	}
	return &skillLoaderAdapter{agent: a}
}

func (s *skillLoaderAdapter) LoadSkill(skillID string) (*tools.SkillInfo, error) {
	configManager := s.agent.GetConfigManager()
	config := configManager.GetConfig()

	skillInfo, err := LoadSkill(skillID, config)
	if err != nil {
		return nil, err
	}

	// Map agent.SkillInfo → tools.SkillInfo (field-by-field)
	return &tools.SkillInfo{
		ID:          skillInfo.ID,
		Name:        skillInfo.Name,
		Description: skillInfo.Description,
		Path:        skillInfo.Path,
		Content:     skillInfo.Content,
		Source:      skillInfo.Source,
	}, nil
}
