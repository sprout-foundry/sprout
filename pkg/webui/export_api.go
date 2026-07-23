//go:build !js

package webui

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/export"
)

// ---------------------------------------------------------------------------
// Route: GET /api/sessions/{id}/export
// ---------------------------------------------------------------------------

// handleAPISessionExport exports a saved session to markdown, HTML, or JSON.
//
// Query parameters:
//   - format: "markdown" (default), "html", or "json"
//   - include_tool_calls: "true" or "false" (default false)
//   - include_cost: "true" or "false" (default true)
//   - no_secret_redaction: "true" or "false" (default false)
//
// Content-Type is set per format (text/markdown, text/html, application/json).
// Content-Disposition is set to "attachment; filename=<id>.<ext>" to trigger
// a browser download.
//
// Returns 400 on invalid format, 404 on missing session, 500 on internal errors.
func (ws *ReactWebServer) handleAPISessionExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from path — the route is /api/sessions/{id}/export
	id := r.PathValue("id")
	if id == "" {
		// Fallback for direct handler calls (e.g., in tests) where PathValue
		// isn't populated because the request didn't go through the ServeMux.
		id = extractSessionIDFromExportPath(r.URL.Path)
	}
	if id == "" {
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	// Parse query parameters with defaults
	formatStr := strings.ToLower(r.URL.Query().Get("format"))
	if formatStr == "" {
		formatStr = "markdown"
	}

	includeToolCalls := r.URL.Query().Get("include_tool_calls") == "true"
	includeCost := r.URL.Query().Get("include_cost") != "false" // default true
	noSecretRedaction := r.URL.Query().Get("no_secret_redaction") == "true"

	format := export.ExportFormat(formatStr)
	switch format {
	case export.FormatMarkdown, export.FormatHTML, export.FormatJSON:
	default:
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("invalid format %q — must be markdown, html, or json", formatStr))
		return
	}

	// Load the session — try explicit cwd param first, then fall back to os.Getwd()
	var cs *agent.ConversationState
	var err error
	cwd := r.URL.Query().Get("cwd")
	if cwd != "" {
		cs, err = agent.LoadStateWithoutAgentScoped(id, cwd)
	} else {
		cs, err = agent.LoadStateWithoutAgent(id)
	}
	if err != nil {
		if isSessionNotFoundError(err) {
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}
		ws.log().Error("failed to load session for export", slog.String("session_id", id), slog.Any("err", err))
		writeJSONError(w, http.StatusInternalServerError, "failed to load session")
		return
	}

	src := conversationStateToSource(cs)

	opts := export.ExportOptions{
		IncludeToolCalls: includeToolCalls,
		IncludeCost:      includeCost,
		RedactSecrets:    !noSecretRedaction,
		PrettyPrintJSON:  true,
	}

	// Set Content-Type and Content-Disposition
	contentType, ext := formatHeaders(format)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.%s\"", id, ext))

	if err := export.Export(w, src, format, opts); err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("export failed: %v", err))
		return
	}
}

func formatHeaders(format export.ExportFormat) (string, string) {
	switch format {
	case export.FormatMarkdown:
		return "text/markdown; charset=utf-8", "md"
	case export.FormatHTML:
		return "text/html; charset=utf-8", "html"
	case export.FormatJSON:
		return "application/json; charset=utf-8", "json"
	default:
		return "application/octet-stream", "bin"
	}
}

func isSessionNotFoundError(err error) bool {
	errMsg := err.Error()
	return strings.Contains(errMsg, "not found") ||
		strings.Contains(errMsg, "no such file") ||
		strings.Contains(errMsg, "failed to resolve session state file")
}

// extractSessionIDFromExportPath parses /api/sessions/<id>/export and returns the ID.
// Used as a fallback when r.PathValue("id") is empty (e.g., direct handler calls in tests).
func extractSessionIDFromExportPath(path string) string {
	prefix := "/api/sessions/"
	suffix := "/export"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return ""
	}
	id := strings.TrimPrefix(path, prefix)
	id = strings.TrimSuffix(id, suffix)
	return id
}

// ---------------------------------------------------------------------------
// Adapter: ConversationState → export.SessionSource
// ---------------------------------------------------------------------------

func conversationStateToSource(cs *agent.ConversationState) export.SessionSource {
	now := time.Now()
	startedAt := cs.LastUpdated
	if startedAt.IsZero() {
		startedAt = now
	}

	src := export.SessionSource{
		ID:               cs.SessionID,
		Name:             cs.Name,
		WorkingDirectory: cs.WorkingDirectory,
		StartedAt:        startedAt,
		LastUpdated:      cs.LastUpdated,
		TotalCost:        cs.TotalCost,
		InputTokens:      cs.PromptTokens,
		OutputTokens:     cs.CompletionTokens,
	}

	for _, m := range cs.Messages {
		src.Messages = append(src.Messages, messageToSource(m))
	}
	return src
}

func messageToSource(m api.Message) export.MessageSource {
	src := export.MessageSource{
		Role:      m.Role,
		Content:   m.Content,
		Timestamp: time.Now(),
	}
	for _, tc := range m.ToolCalls {
		src.ToolCalls = append(src.ToolCalls, export.ToolCallSource{
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	if m.ReasoningContent != "" {
		src.Content += "\n\n<reasoning>\n" + m.ReasoningContent + "\n</reasoning>"
	}
	return src
}
