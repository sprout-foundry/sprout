//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
)

// maxCommandCompletions caps the number of completion entries returned by
// /api/command/complete. Some Complete() implementations (e.g. /model)
// can return hundreds of candidates; bounding the response keeps the
// payload small and predictable for the browser UI. 50 comfortably covers
// any realistic command-argument list while keeping the JSON small.
const maxCommandCompletions = 50

// commandCompletion is one entry in the /api/command/complete response.
// In the command-name phase Text is "/"+name and Description carries the
// command's one-line summary; in the argument phase Text is a raw
// candidate string from Complete() and Description is always empty (the
// client renders argument candidates without descriptions, matching the
// terminal's rich completer).
type commandCompletion struct {
	Text        string `json:"text"`
	Description string `json:"description"`
}

// commandCompleteResponse is the 200 response body of /api/command/complete.
type commandCompleteResponse struct {
	Command     string              `json:"command"`
	Completions []commandCompletion `json:"completions"`
}

// handleAPICommandComplete implements POST /api/command/complete — slash
// command name and argument completion for the WebUI command bar. The
// semantics mirror cmd/slash_completer.go (the terminal's autocomplete)
// over HTTP:
//
//   - Name phase (no space/tab in the trimmed input and no trailing space
//     on the raw input): the user is still typing the command name. Return
//     every registry.CompletionCandidates() name (canonical + aliases)
//     that prefix-matches the text after "/" (case-insensitive), each as
//     "/"+name with the command's Description(), sorted by name. No steer
//     filtering — this is the command bar, the same as
//     buildRichSlashCommandCompleter with steerOnly=false.
//   - Argument phase (space/tab present): resolve the head command, split
//     the rest with strings.Fields, and delegate to the command's
//     Complete() (CompletableCommand) with args = fields[1:]. When the raw
//     input ends in a space an empty trailing arg is appended so the
//     Complete() implementations (which prefix-filter on the last arg)
//     offer all candidates. Complete() implementations prefix-filter
//     server-side per their contract; the returned list is rendered
//     verbatim by the client. Commands that don't implement
//     CompletableCommand, or aren't registered, yield an empty
//     completions array (200, not an error).
//
// The endpoint is deliberately uncached. Most Complete() implementations
// are fast/static; /model performs a bounded provider-API fetch on cold
// cache (stale-while-revalidate) and may briefly block that one request.
//
// Request:
//
//	POST /api/command/complete
//	Content-Type: application/json
//	{"command": "/risk-profile per"}
//
// Response (200):
//
//	{"command": "risk-profile", "completions": [{"text": "permissive", "description": ""}]}
//
// Errors:
//   - 400: invalid JSON, missing/empty command, command not starting with "/"
//   - 405: non-POST method
//   - 500: failed to access chat agent
//   - 503: no AI provider configured
func (ws *ReactWebServer) handleAPICommandComplete(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var req struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	rawCommand := req.Command
	command := strings.TrimSpace(rawCommand)
	if command == "" {
		writeJSONErr(w, http.StatusBadRequest, "command_required", "Command is required")
		return
	}
	if !strings.HasPrefix(command, "/") {
		writeJSONErr(w, http.StatusBadRequest, "invalid_command", "Command must start with /")
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
		writeJSONErr(w, http.StatusInternalServerError, "agent_access_failed", fmt.Sprintf("Failed to access chat agent: %v", err))
		return
	}

	// Resolve the registry the same way /api/command/execute does: the
	// agent's SlashCommands is an interface{} that normally holds a
	// *CommandRegistry. Fall back to the process-wide default registry
	// when the agent has none attached (e.g. a bare test agent).
	registry := agent_commands.DefaultRegistry()
	if clientAgent != nil {
		if registryRaw := clientAgent.SlashCommands(); registryRaw != nil {
			if reg, ok := registryRaw.(*agent_commands.CommandRegistry); ok {
				registry = reg
			}
		}
	}

	resp := commandCompleteResponse{
		Completions: make([]commandCompletion, 0, 8),
	}

	// Name phase. The trailing-space check uses the RAW request body
	// (before TrimSpace): "/risk-profile " means the user has already
	// finished the command name and is asking for argument completions,
	// so it must NOT be treated as a name-prefix match on "risk-profile".
	if !strings.ContainsAny(command, " \t") && !endsWithSpace(rawCommand) {
		head := strings.TrimPrefix(command, "/")
		resp.Command = resolveCanonicalCommandName(registry, head)
		prefix := strings.ToLower(head)
		for _, name := range registry.CompletionCandidates() {
			if !strings.HasPrefix(strings.ToLower(name), prefix) {
				continue
			}
			desc := ""
			if cmd, ok := registry.GetCommand(name); ok {
				desc = cmd.Description()
			}
			resp.Completions = append(resp.Completions, commandCompletion{
				Text:        "/" + name,
				Description: desc,
			})
			if len(resp.Completions) >= maxCommandCompletions {
				break
			}
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// Argument phase.
	fields := strings.Fields(command)
	if len(fields) > 0 {
		head := strings.ToLower(strings.TrimPrefix(fields[0], "/"))
		if cmd, ok := registry.GetCommand(head); ok {
			resp.Command = cmd.Name()
			if completable, ok := cmd.(agent_commands.CompletableCommand); ok {
				args := make([]string, 0, len(fields))
				if len(fields) > 1 {
					args = append(args, fields[1:]...)
				}
				if endsWithSpace(rawCommand) {
					args = append(args, "")
				}
				for _, cand := range completable.Complete(args, clientAgent) {
					resp.Completions = append(resp.Completions, commandCompletion{Text: cand})
					if len(resp.Completions) >= maxCommandCompletions {
						break
					}
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// endsWithSpace reports whether s ends with a whitespace separator that
// strings.Fields would treat as an argument delimiter (space or tab). The
// terminal checks the raw line for a trailing space to decide whether to
// append an empty argument; the trailing space must survive TrimSpace so
// the completion phase matches the terminal exactly.
func endsWithSpace(s string) bool {
	return strings.HasSuffix(s, " ") || strings.HasSuffix(s, "\t")
}

// resolveCanonicalCommandName returns the canonical name for the given
// (possibly aliased or differently-cased) command head, or "" when the
// name isn't registered. Aliases resolve transparently via GetCommand.
func resolveCanonicalCommandName(registry *agent_commands.CommandRegistry, name string) string {
	if registry == nil {
		return ""
	}
	cmd, ok := registry.GetCommand(strings.ToLower(name))
	if !ok {
		return ""
	}
	return cmd.Name()
}
