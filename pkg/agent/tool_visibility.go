package agent

import (
	"sort"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// SetLastPreparedToolNames records the exact tool names prepared for the most recent model request.
func (a *Agent) SetLastPreparedToolNames(tools []api.Tool) {
	if a == nil {
		return
	}

	names := make([]string, 0, len(tools))
	seen := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Function.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)

	a.preparedTools.Lock()
	a.lastToolNames = names
	a.preparedTools.Unlock()
}

// GetLastPreparedToolNames returns the tool names sent in the most recent model request.
func (a *Agent) GetLastPreparedToolNames() []string {
	if a == nil {
		return nil
	}

	a.preparedTools.RLock()
	defer a.preparedTools.RUnlock()

	if len(a.lastToolNames) == 0 {
		return nil
	}

	out := make([]string, len(a.lastToolNames))
	copy(out, a.lastToolNames)
	return out
}
