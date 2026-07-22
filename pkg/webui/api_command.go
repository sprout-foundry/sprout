//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// Backpressure thresholds for the command_output stream (SP-114 Phase 2c).
// commandOutputDropThreshold is the number of bytes the streaming emitter
// may drop (because the bounded ring overflowed) before it emits a single
// command_output_dropped warning. The ring itself is bounded at
// commandOutputRingCap. Tuned small enough that warnings fire while a
// stall is still actionable, but large enough that chatty commands don't
// spam warnings.
const (
	commandOutputDropThreshold = 4096
	commandOutputRingCap       = 64 * 1024
)

// commandOutputStreamer carries the streaming state for one
// /api/command/execute invocation. It holds the monotonic seq, the
// bounded backpressure ring, and the dropped-bytes accumulator. The
// ring is only used for drop accounting — chunks are always fanned out
// to WebSocket subscribers via publishClientEventWithChat (fire and
// forget, non-blocking). When the ring overflows, the oldest bytes are
// dropped and counted toward the next drop-warning threshold.
type commandOutputStreamer struct {
	clientID string
	chatID   string
	cmdName  string
	seq      int

	ring      []byte // bounded backpressure buffer (ringCap bytes)
	ringCap   int
	dropBytes int // bytes dropped since last command_output_dropped warning
}

// newCommandOutputStreamer constructs a streamer for one command
// invocation. chatID may be empty; in that case the fan-out degrades
// to client-wide only (no chat-scoped filtering).
func newCommandOutputStreamer(clientID, chatID string) *commandOutputStreamer {
	return &commandOutputStreamer{
		clientID: clientID,
		chatID:   chatID,
		ringCap:  commandOutputRingCap,
	}
}

// setCommandName records the resolved command name on the streamer so
// subsequent events carry it in their payload. Called once the lookup
// succeeds and we know the canonical name.
func (s *commandOutputStreamer) setCommandName(name string) { s.cmdName = name }

// onChunk is the per-rune callback wired into
// executeSafeSteerCommandStreaming. Each UTF-8 rune the command emits
// arrives as a separate onChunk call. The streamer:
//  1. Increments seq (monotonic per-command, starts at 1 so the
//     terminal "is_final" marker has a known seq).
//  2. Appends the chunk to the bounded backpressure ring, dropping
//     oldest bytes on overflow and counting the dropped bytes.
//  3. Publishes a command_output event with is_final=false.
//  4. If the drop counter crossed commandOutputDropThreshold since
//     the last warning, emits one command_output_dropped event and
//     resets the counter.
//
// The chunk is always emitted — even when the ring overflowed earlier,
// THIS chunk still reaches the WS subscribers. The ring is for
// accounting only; it does NOT buffer or delay chunks.
func (s *commandOutputStreamer) onChunk(ws *ReactWebServer, chunk string) {
	if len(chunk) == 0 {
		return
	}
	s.seq++

	// Backpressure accounting.
	s.appendToRing([]byte(chunk))

	// Fan out to WS subscribers via the event bus. publishClientEventWithChat
	// is a no-op when eventBus is nil and tolerates missing chat/client
	// contexts — so this stays safe even when the test fixture has no
	// active WebSocket.
	data := map[string]interface{}{
		"command":  s.cmdName,
		"chunk":    chunk,
		"is_final": false,
		"seq":      s.seq,
	}
	ws.publishClientEventWithChat(s.clientID, s.chatID, events.EventTypeCommandOutput, data)

	// Fire the drop warning when the drop counter has accumulated
	// at least commandOutputDropThreshold bytes since the last
	// warning. The threshold check is `>=`, so a single chunk that
	// drops >= 4096 bytes produces exactly one warning per crossing;
	// a slow trickle that accumulates one byte at a time would also
	// produce one warning per 4 KB window. The warning is therefore
	// one event per `commandOutputDropThreshold` bytes of dropped
	// output, never more frequent.
	if s.dropBytes >= commandOutputDropThreshold {
		dropped := s.dropBytes
		s.dropBytes = 0
		warn := map[string]interface{}{
			"command":       s.cmdName,
			"dropped_bytes": dropped,
		}
		ws.publishClientEventWithChat(s.clientID, s.chatID, events.EventTypeCommandOutputDropped, warn)
	}
}

// emitFinal publishes the stream terminator: a command_output event
// with is_final=true and chunk="". The WebUI consumer uses this to
// stop its spinner / hide its loading indicator. Emitted exactly
// once per command, after the aggregated HTTP response has been
// queued (so a non-WebSocket caller never sees the final marker
// block their response).
func (s *commandOutputStreamer) emitFinal(ws *ReactWebServer) {
	data := map[string]interface{}{
		"command":  s.cmdName,
		"chunk":    "",
		"is_final": true,
		"seq":      s.seq,
	}
	ws.publishClientEventWithChat(s.clientID, s.chatID, events.EventTypeCommandOutput, data)
}

// appendToRing pushes p into the bounded ring, dropping oldest bytes
// on overflow and accumulating the drop count into s.dropBytes. The
// ring is a flat byte slice — front-aligned, with simple slice copy
// shifts on overflow. Performance is fine: chunks are 4 KB and we
// shift at most once per chunk.
func (s *commandOutputStreamer) appendToRing(p []byte) {
	if len(p) == 0 || s.ringCap == 0 {
		return
	}
	if len(p) >= s.ringCap {
		// Chunk larger than the ring — keep only its tail.
		s.dropBytes += len(p) - s.ringCap
		tail := p[len(p)-s.ringCap:]
		s.ring = append(s.ring[:0], tail...)
		return
	}
	overflow := len(s.ring) + len(p) - s.ringCap
	if overflow > 0 {
		s.dropBytes += overflow
		copy(s.ring, s.ring[overflow:])
		s.ring = s.ring[:len(s.ring)-overflow]
	}
	s.ring = append(s.ring, p...)
}

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
// SP-114 Phase 2c: in addition to the HTTP response, the command's
// stdout is streamed chunk-by-chunk over the chat session's WebSocket
// connection as `command_output` events. The HTTP response still
// returns the aggregated output for backwards compatibility —
// non-WebSocket callers see no change. If the bounded backpressure
// ring overflows, the streamer emits `command_output_dropped` events
// every 4 KB of dropped data.
//
// Errors:
//   - 400: invalid JSON, missing/empty command, command not registered,
//     command not SteerCapable (destructive commands like /commit, /clear,
//     /exit — see pkg/agent_commands.SteerCapable)
//   - 405: non-POST method
//   - 500: failed to access chat agent
//   - 503: no AI provider configured
//
// The reuse of executeSafeSteerCommandStreaming is intentional: that
// helper already implements stdout-capture-via-os.Pipe with mutex
// serialization (so concurrent commands across chats can't race on the
// process-global os.Stdout) and the SteerCapable safety gate. When the
// onChunk callback is nil the streaming variant is byte-for-byte
// identical to the original executeSafeSteerCommand (used by the
// /api/query/steer path).
func (ws *ReactWebServer) handleAPICommandExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
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

	cmdLine := strings.TrimSpace(req.Command)
	if cmdLine == "" {
		writeJSONErr(w, http.StatusBadRequest, "command_required", "Command is required")
		return
	}
	if !strings.HasPrefix(cmdLine, "/") {
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

	// Resolve the canonical command name eagerly so chunk events carry
	// it from the very first rune (not after the command finishes).
	// The lookup is best-effort: if the command isn't registered we
	// still proceed — the downstream streaming executor returns
	// (nil, "", nil) and we re-classify the failure mode.
	var cmdName string
	if parts := strings.Fields(cmdLine); len(parts) > 0 {
		cmdName = strings.TrimPrefix(parts[0], "/")
	}

	// SP-114 Phase 2c: stream stdout over WS while still returning the
	// aggregated output in the HTTP response. The streamer collects
	// dropped-byte accounting and warns when the bounded ring
	// overflows.
	streamer := newCommandOutputStreamer(clientID, chatID)
	streamer.setCommandName(cmdName)
	chunkCallback := func(chunk string) {
		streamer.onChunk(ws, chunk)
	}

	// Delegate to the streaming variant. It returns (nil, "", nil) when
	// the command is missing or not SteerCapable — distinguish "not
	// found" vs "not safe" by re-parsing the command name ourselves so
	// the WebUI gets an actionable error code.
	resolvedCmd, output, cmdErr := ws.executeSafeSteerCommandStreaming(cmdLine, clientAgent, chunkCallback)
	if resolvedCmd == nil {
		// Command not found / not safe — re-parse to give the WebUI an
		// actionable error code. We don't emit any WS events for a
		// rejected command (no output to stream).
		parts := strings.Fields(cmdLine)
		if len(parts) > 0 {
			headCmd := strings.TrimPrefix(parts[0], "/")
			if registryRaw := clientAgent.SlashCommands(); registryRaw != nil {
				if registry, ok := registryRaw.(*agent_commands.CommandRegistry); ok {
					if _, found := registry.GetCommand(headCmd); found {
						writeJSONErr(w, http.StatusBadRequest, "command_not_safe",
							"Command /"+headCmd+" is not safe to run from the WebUI command surface (mutates state or requires interactive input)")
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
		"command":  resolvedCmd.Name(),
		"output":   output,
		"error":    "",
		"accepted": cmdErr == nil,
	}
	if cmdErr != nil {
		resp["error"] = cmdErr.Error()
	}
	_ = json.NewEncoder(w).Encode(resp)

	// Emit the final WS marker AFTER the HTTP response is queued so a
	// synchronous HTTP caller that doesn't have a WS open still gets
	// the JSON body without the WS path blocking the response. The
	// final marker signals "no more chunks" to WS subscribers.
	streamer.emitFinal(ws)
}
