package computer_use

import tools "github.com/sprout-foundry/sprout/pkg/agent_tools"

// Handlers returns the computer-use tool handlers in a stable order. The agent
// registers these into its execution registry (and derives LLM definitions
// from each handler's Definition()) only when computer use is enabled in
// config. Kept here so the unexported handler structs don't need to leak.
func Handlers() []tools.ToolHandler {
	return []tools.ToolHandler{
		&takeScreenshotHandler{},
		&mouseClickHandler{},
		&mouseDragHandler{},
		&keyboardTypeHandler{},
		&keyboardPressHandler{},
		&scrollHandler{},
		&waitHandler{},
	}
}

// ToolNames returns the names of all computer-use tools. Used by the agent's
// dispatch-layer guard to reject these tools for any persona other than
// computer_user.
func ToolNames() []string {
	hs := Handlers()
	names := make([]string, 0, len(hs))
	for _, h := range hs {
		names = append(names, h.Name())
	}
	return names
}
