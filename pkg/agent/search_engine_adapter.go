package agent

import (
	"context"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// searchEngineAdapter wraps the agent's search capability for use
// by the new interface-based tool handlers. It lives in pkg/agent (not
// pkg/agent_tools) to avoid an import cycle — the adapter needs access to
// agent.GetConfigManager() to call tools.WebSearch().
type searchEngineAdapter struct {
	agent *Agent
}

// newSearchEngineAdapter creates a tools.SearchEngine backed by the agent.
func newSearchEngineAdapter(a *Agent) tools.SearchEngine {
	if a == nil {
		return nil
	}
	return &searchEngineAdapter{agent: a}
}

// Search wraps tools.WebSearch() to satisfy the tools.SearchEngine interface.
func (s *searchEngineAdapter) Search(ctx context.Context, query string) (string, error) {
	configManager := s.agent.GetConfigManager()
	return tools.WebSearch(query, configManager)
}
