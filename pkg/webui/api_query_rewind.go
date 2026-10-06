//go:build !js

package webui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// rewindRequest is the request body for POST /api/query/rewind. It mirrors the
// QueryRewindRequest schema documented in docs/api/openapi.base.yaml. to_turn is
// required (0-based: rewind to BEFORE this turn); revert_files defaults to true.
type rewindRequest struct {
	ToTurn      *int   `json:"to_turn"`
	RevertFiles *bool  `json:"revert_files"`
	ChatID      string `json:"chat_id"`
}

// buildAPIQueryRewind is the shared backend for POST /api/query/rewind. It
// truncates the conversation history back to a prior turn (optionally reverting
// file changes made during the discarded turns), then syncs agent state and
// notifies the UI of the session change, writing the result through w. The HTTP
// method gate is enforced by the Huma operation (registerHumaOperations in
// routes.go); a wrong method reaches the SPA catch-all and 404s.
//
// Request body:
//
//	{"to_turn": <int>, "revert_files": <bool>}
//
// to_turn is required (0-based: rewind to BEFORE this turn).
// revert_files defaults to true.
func (ws *ReactWebServer) buildAPIQueryRewind(w http.ResponseWriter, r *http.Request, req rewindRequest) {
	if req.ToTurn == nil {
		writeJSONErr(w, http.StatusBadRequest, "to_turn_required", "to_turn is required")
		return
	}

	revertFiles := true
	if req.RevertFiles != nil {
		revertFiles = *req.RevertFiles
	}

	clientID := ws.resolveClientID(r)
	chatID := strings.TrimSpace(req.ChatID)
	if chatID == "" {
		chatID = ws.resolveChatID(r, clientID)
	}

	// Reject if a query is currently running.
	ws.mutex.RLock()
	ctx := ws.clientContexts[clientID]
	if ctx == nil {
		ws.mutex.RUnlock()
		writeJSONErr(w, http.StatusBadRequest, "client_context_not_found", "Client context not found")
		return
	}
	if ctx.hasActiveQueryForChat(chatID) {
		ws.mutex.RUnlock()
		writeJSONErr(w, http.StatusConflict, "query_in_progress", "Cannot rewind while a query is running")
		return
	}
	ws.mutex.RUnlock()

	clientAgent, err := ws.getChatAgent(clientID, chatID)
	if err != nil {
		if errors.Is(err, ErrNoProviderConfigured) || isProviderConfigError(err) {
			writeJSONErr(w, http.StatusServiceUnavailable, "no_provider", "AI features require a provider. Please configure one in settings.")
		} else {
			writeJSONErr(w, http.StatusInternalServerError, "agent_access_failed", fmt.Sprintf("Failed to access chat agent: %v", err))
		}
		return
	}

	result, err := clientAgent.Rewind(agent.RewindOptions{
		ToTurnIndex: *req.ToTurn,
		RevertFiles: revertFiles,
	})
	if err != nil {
		ws.log().Error("rewind failed", slog.String("chat_id", chatID), slog.Any("err", err))
		writeJSONErr(w, http.StatusBadRequest, "rewind_failed", fmt.Sprintf("Rewind failed: %v", err))
		return
	}

	// Sync agent state so the UI reflects the truncated history.
	if syncErr := ws.syncAgentStateForClientWithChat(clientID, chatID); syncErr != nil {
		ws.log().Warn("failed to sync state after rewind", slog.String("chat_id", chatID), slog.Any("err", syncErr))
	}

	// Notify the UI that the session changed via rewind.
	ws.publishSessionChanged(clientID, chatID, "rewind", map[string]interface{}{
		"turns_discarded":     result.TurnsDiscarded,
		"messages_removed":    result.MessagesRemoved,
		"checkpoints_dropped": result.CheckpointsDropped,
	})

	ws.log().Info("rewind completed",
		slog.String("chat_id", chatID),
		slog.Int("turns_discarded", result.TurnsDiscarded),
		slog.Int("messages_removed", result.MessagesRemoved),
		slog.Int("files_reverted", len(result.FilesReverted)),
		slog.Int("files_skipped", len(result.FilesSkipped)))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"turns_discarded":     result.TurnsDiscarded,
		"messages_removed":    result.MessagesRemoved,
		"files_reverted":      result.FilesReverted,
		"files_skipped":       result.FilesSkipped,
		"checkpoints_dropped": result.CheckpointsDropped,
	})
}

// rewindInput is the input for the Huma POST /api/query/rewind operation.
type rewindInput struct {
	humaRequestInput
}

// rewindHumaHandler is the Huma handler for POST /api/query/rewind. It parses
// the body (a single rewindRequest, decoded without a size cap, matching the
// pre-migration behavior) and writes the response through in.Resp, so the
// migrated operation is byte-identical to the plain handler it replaced; the
// body is a no-op callback.
func (ws *ReactWebServer) rewindHumaHandler(ctx context.Context, in *rewindInput) (*writtenResponseOutput, error) {
	w := in.Resp
	r := in.Req

	var req rewindRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ws.log().Warn("invalid rewind request JSON", slog.Any("err", err))
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return &writtenResponseOutput{Body: noopWrittenResponse}, nil
	}

	ws.buildAPIQueryRewind(w, r, req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}
