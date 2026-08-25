package agent

import tools "github.com/sprout-foundry/sprout/pkg/agent_tools"

// IsInteractiveTool reports whether the named tool is registered with
// Interactive=true in the handler registry. Unknown tools return false.
// Use this from CLI subscribers (e.g. the activity-indicator goroutine)
// to decide whether to suppress transient chrome that would clobber the
// tool's own prompt.
func IsInteractiveTool(name string) bool {
	h, ok := tools.GetNewToolRegistry().Lookup(name)
	if !ok {
		return false
	}
	return h.Interactive()
}
