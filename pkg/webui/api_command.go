//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
)

// handleAPICommandExecute executes a slash command via the dedicated command
// surface (SP-114 Phase 2 §2a). Unlike the steer endpoint, this surface does
// not require an active query — it is the canonical way to invoke safe
// commands from the WebUI command bar at any time.
//
// Request:
//
//	POST /api/command/execute
//	Content-Type: application/json
//	{"command": "/info"}
//
// Response (200 OK):
//
//	{"command": "info", "output": "Agent: ...", "error": ""}
//
// Errors:
//   - 400: invalid JSON, missing/empty command, command not registered,
//     command not SteerCapable (destructive commands like /commit, /clear,
//     /exit — see pkg/agent_commands.SteerCapable)
//   - 405: non-POST method
//   - 500: failed to access chat agent
//   - 503: no AI provider configured
//
// The reuse of executeSafeSteerCommand is intentional: that helper already
// implements stdout-capture-via-os.Pipe with mutex serialization (so
// concurrent commands across chats can't race on the process-global
// os.Stdout) and the SteerCapable safety gate.
func (ws *ReactWebServer) handleAPICommandExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var req struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	cmdLine := strings.TrimSpace(req.Command)
	if cmdLine == "" {
		http.Error(w, "Command is required", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(cmdLine, "/") {
		http.Error(w, "Command must start with /", http.StatusBadRequest)
		return
	}

	clientID := ws.resolveClientID(r)
	chatID := ws.resolveChatID(r, clientID)
	clientAgent, err := ws.getChatAgent(clientID, chatID)
	if err != nil {
		if isProviderConfigError(err) {
			writeJSONErr(w, http.StatusServiceUnavailable, "no_provider", "AI features require a provider. Please configure one in settings.")
			return
		}
		http.Error(w, fmt.Sprintf("Failed to access chat agent: %v", err), http.StatusInternalServerError)
		return
	}

	// Delegate to the existing safe-command executor. It returns (nil, "", nil)
	// when the command is missing or not SteerCapable — distinguish "not found"
	// vs "not safe" by re-parsing the command name ourselves so the WebUI gets
	// an actionable error code.
	cmd, output, cmdErr := ws.executeSafeSteerCommand(cmdLine, clientAgent)
	if cmd == nil {
		parts := strings.Fields(cmdLine)
		if len(parts) > 0 {
			cmdName := strings.TrimPrefix(parts[0], "/")
			if registryRaw := clientAgent.SlashCommands(); registryRaw != nil {
				if registry, ok := registryRaw.(*agent_commands.CommandRegistry); ok {
					if _, found := registry.GetCommand(cmdName); found {
						writeJSONErr(w, http.StatusBadRequest, "command_not_safe",
							"Command /"+cmdName+" is not safe to run from the WebUI command surface (mutates state or requires interactive input)")
						return
					}
				}
			}
		}
		writeJSONErr(w, http.StatusBadRequest, "command_not_found",
			"Unknown command: "+cmdLine)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := map[string]interface{}{
		"command":  cmd.Name(),
		"output":   output,
		"error":    "",
		"accepted": cmdErr == nil,
	}
	if cmdErr != nil {
		resp["error"] = cmdErr.Error()
	}
	_ = json.NewEncoder(w).Encode(resp)
}
