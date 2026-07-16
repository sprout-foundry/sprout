// Package agent: conversation state, tokens/cost, tasks, output, and file reading (split from agent_getters.go)
package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// GetMessages returns the current conversation messages
func (a *Agent) GetMessages() []api.Message {
	if a.state == nil {
		return nil
	}
	return a.state.GetMessages()
}

// SetMessages sets the conversation messages (for restore)
func (a *Agent) SetMessages(messages []api.Message) {
	if a.state != nil {
		a.state.SetMessages(messages)
	}
}

// AddMessage adds a single message to the conversation history
func (a *Agent) AddMessage(message api.Message) {
	if a.state != nil {
		a.state.AddMessage(message)
	}
}

// GetContextTokens returns the current and max token counts for the active
// model's context window. (0, 0) when state is unavailable. SP-048-3.
func (a *Agent) GetContextTokens() (used, limit int) {
	if a == nil || a.state == nil {
		return 0, 0
	}
	return a.state.GetCurrentContextTokens(), a.state.GetMaxContextTokens()
}

// GetTotalCost returns the total cost of the conversation
func (a *Agent) GetTotalCost() float64 {
	return a.state.GetTotalCost()
}

// GetChargedCostTotal returns the total charged cost
func (a *Agent) GetChargedCostTotal() float64 {
	if a.state == nil {
		return 0
	}
	return a.state.GetChargedCostTotal()
}

// GetTokenCostTotal returns the total token-based cost
func (a *Agent) GetTokenCostTotal() float64 {
	if a.state == nil {
		return 0
	}
	return a.state.GetTokenCostTotal()
}

// GetTaskActions returns completed task actions
func (a *Agent) GetTaskActions() []TaskAction {
	mu := a.state.GetTaskActionsMutex()
	mu.RLock()
	defer mu.RUnlock()
	return a.state.GetTaskActions()
}

// IsInteractiveMode returns true if running in interactive mode
func (a *Agent) IsInteractiveMode() bool {
	return configuration.GetEnvSimple("INTERACTIVE") == "1" ||
		(a != nil && !a.IsSubagent())
}

// SetStatsUpdateCallback sets a callback for token/cost updates
func (a *Agent) SetStatsUpdateCallback(callback func(int, float64)) {
	a.statsUpdateCallback.Store(callback)
}

// OutputRouter returns the current output router (nil if not initialized)
func (a *Agent) OutputRouter() *OutputRouter { return a.output.GetOutputRouter() }

// PrintTerminalOnly writes text to the terminal without publishing to the event bus.
// Use this for output already published via a more specific event type.
func (a *Agent) PrintTerminalOnly(text string) {
	if a == nil {
		return
	}
	if a.output == nil {
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		fmt.Print(text)
		return
	}
	router := a.output.GetOutputRouter()
	if router == nil {
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		fmt.Print(text)
		return
	}
	router.RouteTerminalOnly(text)
}

// SetTraceSession sets the trace session for dataset collection
func (a *Agent) SetTraceSession(traceSession interface{}) {
	a.traceSession = traceSession
	a.state.SetTraceSession(traceSession)
}

// ReadFileContent reads the content of a file from the workspace.
// The path is resolved relative to the agent's workspace root.
// Returns an error if the file does not exist or cannot be read.
func (a *Agent) ReadFileContent(path string) (string, error) {
	if a == nil {
		return "", agenterrors.NewValidation("agent is nil", nil)
	}
	workspaceRoot := a.currentWorkspaceRoot()
	absPath := filepath.Join(workspaceRoot, path)
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", agenterrors.Wrap(err, fmt.Sprintf("failed to read file %s", path))
	}
	return string(data), nil
}
