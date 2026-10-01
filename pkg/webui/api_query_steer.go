//go:build !js

package webui

// api_query_steer.go — the webui query steer + steer-retract handlers:
// handleAPIQuerySteer, handleAPIQuerySteerRetract, the safe steer-command
// execution (executeSafeSteerCommand, executeSafeSteerCommandStreaming), and
// the stream-pipe chunking helper (streamPipeChunks). Split out of
// api_query.go.

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sprout-foundry/sprout/pkg/agent"
	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
)

// handleAPIQuerySteer injects user input into the currently running query loop.
func (ws *ReactWebServer) handleAPIQuerySteer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var query struct {
		Query  string `json:"query"`
		ChatID string `json:"chat_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&query); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	query.Query = strings.TrimSpace(query.Query)
	if query.Query == "" {
		writeJSONErr(w, http.StatusBadRequest, "query_required", "Query is required")
		return
	}

	clientID := ws.resolveClientID(r)
	// Resolve chat_id the same way handleAPIQuery does: prefer the body
	// parameter (the frontend always sends it), fall back to the URL query
	// parameter and the client's active chat. Using only the active-chat
	// fallback misrouted steers to the wrong chat's agent when a query was
	// running in a non-active chat.
	chatID := strings.TrimSpace(query.ChatID)
	if chatID == "" {
		chatID = ws.resolveChatID(r, clientID)
	}

	// Handle slash commands during active query
	if strings.HasPrefix(query.Query, "/") {
		ws.mutex.RLock()
		ctx := ws.clientContexts[clientID]
		hasActiveQuery := ctx != nil && ctx.hasActiveQueryForChat(chatID)
		ws.mutex.RUnlock()

		if !hasActiveQuery {
			writeJSONErr(w, http.StatusConflict, "no_active_query", "No active query to steer")
			return
		}

		clientAgent, err := ws.getChatAgent(clientID, chatID)
		if err != nil {
			if isProviderConfigError(err) {
				writeJSONErr(w, http.StatusServiceUnavailable, "no_provider", "AI features require a provider. Please configure one in settings.")
			} else {
				writeJSONErr(w, http.StatusInternalServerError, "agent_access_failed", fmt.Sprintf("Failed to access chat agent: %v", err))
			}
			return
		}

		// Try to execute safe steer command
		cmd, output, cmdErr := ws.executeSafeSteerCommand(query.Query, clientAgent)
		if cmd != nil {
			// Command was found and executed (success or error)
			resp := map[string]interface{}{
				"accepted": cmdErr == nil,
				"mode":     "steer",
				"command":  cmd.Name(),
				"target":   "primary",
			}
			if output != "" {
				resp["output"] = output
			}
			if cmdErr != nil {
				resp["error"] = cmdErr.Error()
			}
			writeJSON(w, http.StatusOK, resp)
			return
		}

		// Command not found or not safe to run mid-turn
		writeJSONErr(w, http.StatusBadRequest, "slash_command_not_steerable", "Slash commands cannot be steered while a query is running")
		return
	}

	ws.mutex.RLock()
	ctx := ws.clientContexts[clientID]
	if ctx == nil || !ctx.hasActiveQueryForChat(chatID) {
		ws.mutex.RUnlock()
		writeJSONErr(w, http.StatusConflict, "no_active_query", "No active query to steer")
		return
	}
	ws.mutex.RUnlock()

	clientAgent, err := ws.getChatAgent(clientID, chatID)
	if err != nil {
		if isProviderConfigError(err) {
			writeJSONErr(w, http.StatusServiceUnavailable, "no_provider", "AI features require a provider. Please configure one in settings.")
		} else {
			writeJSONErr(w, http.StatusInternalServerError, "agent_access_failed", fmt.Sprintf("Failed to access chat agent: %v", err))
		}
		return
	}

	// SP-059 Phase 1b / SP-094-8: if a subagent is the active executor,
	// route the steer via InjectInputIntoActive. This now prefers the
	// primary agent first (the parent decides whether to abort subagents,
	// redirect them, or fold the steer into its own plan). Only if the
	// primary's channel is full does it fall back to the deepest running
	// subagent.
	target := "primary"
	subagentID := ""
	delivered := false
	if runner := clientAgent.GetSubagentRunner(); runner != nil {
		if id, ok := runner.InjectInputIntoActive(query.Query); ok {
			delivered = true
			if id == "primary" {
				target = "primary"
			} else {
				target = "subagent"
				subagentID = id
			}
		}
	}
	if !delivered {
		// No runner or runner couldn't deliver — fall back to primary directly.
		if err := clientAgent.InjectInputContext(query.Query); err != nil {
			ws.log().Error("steer failed",
				slog.String("handler", "handleAPIQuerySteer"),
				slog.String("chat_id", chatID),
				slog.String("client_id", clientID),
				slog.Any("err", err),
			)
			writeJSONErr(w, http.StatusConflict, "steer_failed", fmt.Sprintf("Failed to steer active query: %v", err))
			return
		}
	}

	resp := map[string]interface{}{
		"accepted":  true,
		"mode":      "steer",
		"query":     query.Query,
		"target":    target,
		"timestamp": time.Now().Unix(),
	}
	if subagentID != "" {
		resp["subagent_id"] = subagentID
	}
	ws.log().Info("query steered",
		slog.String("handler", "handleAPIQuerySteer"),
		slog.String("chat_id", chatID),
		slog.String("client_id", clientID),
		slog.String("target", target),
	)
	writeJSON(w, http.StatusAccepted, resp)
}

// handleAPIQuerySteerRetract pulls back the newest staged-but-unpicked steer
// message so the user can edit it (Up-arrow on empty input while processing).
// On success returns 200 with the retracted text. When nothing is pending
// returns 200 with success=false (not an error — the frontend treats it as
// "nothing to pull back").
//
// Note: if the steer was delivered to a SUBAGENT (rare fallback path in
// handleAPIQuerySteer), RetractLatestSteer on the primary agent returns
// false — the subagent's input channel is separate and not retractable.
// This is acceptable; the steer is already in-flight.
func (ws *ReactWebServer) handleAPIQuerySteerRetract(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// The frontend sends chat_id in the body (see retractSteer in chatApi.ts);
	// resolveChatID only reads the URL query param, so honor the body value
	// first — otherwise retract targets the active chat instead of the chat
	// that actually staged the steer.
	var body struct {
		ChatID string `json:"chat_id"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)).Decode(&body)

	clientID := ws.resolveClientID(r)
	chatID := strings.TrimSpace(body.ChatID)
	if chatID == "" {
		chatID = ws.resolveChatID(r, clientID)
	}

	clientAgent, err := ws.getChatAgent(clientID, chatID)
	if err != nil {
		if isProviderConfigError(err) {
			writeJSONErr(w, http.StatusServiceUnavailable, "no_provider", "AI features require a provider. Please configure one in settings.")
		} else {
			writeJSONErr(w, http.StatusInternalServerError, "agent_access_failed", fmt.Sprintf("Failed to access chat agent: %v", err))
		}
		return
	}

	message, ok := clientAgent.RetractLatestSteer()
	if !ok {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"message": "",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": message,
	})
}

// executeSafeSteerCommand tries to execute a slash command mid-turn.
// Returns (cmd, output, error) where:
//   - cmd is the command if found and executed
//   - output is the captured stdout from the command
//   - error is any execution error
//   - (nil, "", nil) is returned if the command was not found or not safe
func (ws *ReactWebServer) executeSafeSteerCommand(input string, chatAgent *agent.Agent) (agent_commands.Command, string, error) {
	return ws.executeSafeSteerCommandStreaming(input, chatAgent, nil, nil)
}

// executeSafeSteerCommandStreaming is the streaming variant of
// executeSafeSteerCommand (SP-114 Phase 2c). When onChunk is non-nil it
// receives each UTF-8-safe chunk from the command's configured output
// writer, in addition to being appended to the aggregated output string.
// When onChunk is nil the behavior is byte-for-byte identical to the
// non-streaming executeSafeSteerCommand — the /api/query/steer call
// site relies on this and uses the non-streaming entry point.
//
// onComplete is invoked exactly once after the command finishes
// (success or error). Use it for post-command housekeeping like state
// sync — the shared function calls it so callers don't have to remember.
//
// onChunk is invoked from a goroutine that reads the command output pipe
// concurrently with Execute. It MUST be safe to call concurrently with
// the rest of the program; in particular it must not block on slow
// consumers (callers are expected to fan out to the WebSocket
// non-blockingly via the event bus). The reader goroutine exits once
// Execute returns and writeEnd is closed; onChunk will not be called
// after this function returns.
func (ws *ReactWebServer) executeSafeSteerCommandStreaming(input string, chatAgent *agent.Agent, onChunk func(string), onComplete func(agent_commands.Command, string, error)) (agent_commands.Command, string, error) {
	// Parse command name from input
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "/") {
		return nil, "", nil
	}
	parts := strings.Fields(trimmed[1:]) // Remove leading /
	if len(parts) == 0 {
		return nil, "", nil
	}
	cmdName := parts[0]

	// Get the registry from the agent. Daemon-mode (`sprout agent --daemon`)
	// seeds the server with the CLI-created agent, which never had
	// SetSlashCommands called — its registry is nil, and every command
	// resolved through this helper returned "command_not_found" even for
	// /clear. runChatQuery avoids this by building its own registry; do the
	// same here: fall back to a fresh registry when a real agent merely
	// lacks one. A nil AGENT still short-circuits — commands need an agent
	// to act on.
	if chatAgent == nil {
		return nil, "", nil
	}
	registryRaw := chatAgent.SlashCommands()
	var registry *agent_commands.CommandRegistry
	if registryRaw != nil {
		registry, _ = registryRaw.(*agent_commands.CommandRegistry)
	}
	if registry == nil {
		registry = agent_commands.NewCommandRegistry()
	}

	cmd, ok := registry.GetCommand(cmdName)
	if !ok {
		return nil, "", nil
	}

	// Check if command is safe to run mid-turn
	sc, ok := cmd.(agent_commands.SteerCapable)
	if !ok || !sc.SafeDuringSteer() {
		return nil, "", nil
	}

	// Capture command output without redirecting process-global os.Stdout.
	// The registry wires this invocation-local writer into OutputCommand
	// implementations, so commands from different clients can run concurrently.
	output, cmdErr := captureCommandOutput(
		ws.log(), "executeSafeSteerCommandStreaming",
		registry, input, chatAgent, onChunk, true,
	)

	if onComplete != nil {
		onComplete(cmd, output, cmdErr)
	}

	return cmd, output, cmdErr
}

// streamPipeChunks drains r into buf while invoking onChunk for each
// UTF-8-safe chunk. It buffers trailing partial runes so onChunk never
// receives an incomplete multi-byte rune, then emits a single event per
// pipe read containing every complete rune from that read. Callers can
// batch events from multiple reads (the WebUI panel will append
// monotonically). The chunk size (4 KB) is large enough to amortize
// per-chunk overhead but small enough that WebSocket latency stays low.
// Public for tests; production code uses it via
// executeSafeSteerCommandStreaming.
func streamPipeChunks(r io.Reader, buf *strings.Builder, onChunk func(string)) {
	const chunkSize = 4096
	pending := make([]byte, 0, chunkSize)
	scratch := make([]byte, chunkSize)
	for {
		n, err := r.Read(scratch)
		if n > 0 {
			buf.Write(scratch[:n])
			pending = append(pending, scratch[:n]...)
			// Walk the pending buffer once and emit each complete
			// rune. We collect them into a per-read builder so a
			// 4 KB pipe-read becomes ONE onChunk call (not 4096
			// per-byte calls), which keeps the event bus / WS
			// pipeline from drowning under flood pressure during
			// normal-speed commands. Trailing partial runes stay in
			// `pending` for the next read.
			i := 0
			out := make([]byte, 0, len(pending))
			for i < len(pending) {
				if !utf8.FullRune(pending[i:]) {
					// Cap pending at UTFMax to prevent unbounded growth
					// from broken UTF-8 (e.g. continuation bytes with no
					// leading byte). Emit replacement character and reset.
					if len(pending)-i >= utf8.UTFMax {
						out = append(out, "\uFFFD"...)
						pending = pending[:0]
					}
					break
				}
				rn, size := utf8.DecodeRune(pending[i:])
				var rb [utf8.UTFMax]byte
				sz := utf8.EncodeRune(rb[:], rn)
				out = append(out, rb[:sz]...)
				i += size
			}
			if len(out) > 0 && onChunk != nil {
				onChunk(string(out))
			}
			if i > 0 {
				pending = pending[:copy(pending, pending[i:])]
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			// Pipe closed mid-read or other read error. We can't do
			// much — the command's writer is gone. Stop streaming
			// and let the caller assemble whatever we captured.
			break
		}
	}
}
