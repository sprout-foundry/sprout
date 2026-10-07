//go:build !js

// huma_terminal.go holds the Huma operations and their thin handlers for the
// terminal family: the /api/terminal/* routes (registered via
// registerTerminalHumaOperations from registerHumaOperations in
// huma_routes.go). /api/lsp/ws is a WebSocket bridge, not a JSON operation,
// so it stays a plain handler; /api/lsp/status is already a Huma operation in
// huma_files.go.
//
// Each handler drives the existing plain handler through the live ResponseWriter
// and returns a no-op writtenResponseOutput, so the response bytes are
// unchanged. /api/terminal/history is a multi-method route (GET reads the
// history, POST records a command) and /api/terminal/agent-sessions/ is a
// subtree route whose sub-segments are parsed by the handler.
package webui

import (
	"context"
	"net/http"

	huma "github.com/danielgtaylor/huma/v2"
)

// registerTerminalHumaOperations registers the terminal-family Huma operations.
func registerTerminalHumaOperations(api huma.API, ws *ReactWebServer) {
	huma.Register(api, huma.Operation{
		OperationID: "terminalHistoryGet",
		Method:      http.MethodGet,
		Path:        "/api/terminal/history",
		Summary:     "Read a terminal session's command history.",
		Description: "Returns the command history for the `session_id` terminal session (an empty list when no session ID is given).",
		Tags:        []string{"terminal"},
	}, ws.terminalHistoryGetHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "terminalHistoryPost",
		Method:      http.MethodPost,
		Path:        "/api/terminal/history",
		Summary:     "Record a command in a terminal session's history.",
		Description: "Records `command` in the `session_id` terminal session's history (stored even without an active session).",
		Tags:        []string{"terminal"},
	}, ws.terminalHistoryPostHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "terminalSessions",
		Method:      http.MethodGet,
		Path:        "/api/terminal/sessions",
		Summary:     "List the active terminal sessions.",
		Description: "Lists the client's active terminal sessions with their ID, active flag, last-used time, and size presence.",
		Tags:        []string{"terminal"},
	}, ws.terminalSessionsHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "terminalShells",
		Method:      http.MethodGet,
		Path:        "/api/terminal/shells",
		Summary:     "List the available terminal shells.",
		Description: "Lists the shells available for terminal sessions on this host.",
		Tags:        []string{"terminal"},
	}, ws.terminalShellsHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "agentSessionsList",
		Method:      http.MethodGet,
		Path:        "/api/terminal/agent-sessions",
		Summary:     "List the agent terminal sessions.",
		Description: "Lists the client's agent-driven terminal sessions (background and hidden) with their state.",
		Tags:        []string{"terminal"},
	}, ws.agentSessionsListHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "agentSessionAction",
		Method:      http.MethodGet,
		Path:        "/api/terminal/agent-sessions/",
		Summary:     "Act on an agent terminal session.",
		Description: "Performs an action on one agent terminal session at the /api/terminal/agent-sessions/{id}/{action} shape: GET {id}/output returns accumulated output; POST {id}/attach promotes a hidden session to visible; POST {id}/kill terminates it.",
		Tags:        []string{"terminal"},
	}, ws.agentSessionActionHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "agentSessionActionPost",
		Method:      http.MethodPost,
		Path:        "/api/terminal/agent-sessions/",
		Summary:     "Attach or kill an agent terminal session.",
		Description: "Performs a mutating action on one agent terminal session at the /api/terminal/agent-sessions/{id}/{action} shape: POST {id}/attach promotes a hidden session to visible; POST {id}/kill terminates it.",
		Tags:        []string{"terminal"},
	}, ws.agentSessionActionHumaHandler)
}

// terminalHistoryGetHumaHandler is the Huma handler for GET /api/terminal/history.
func (ws *ReactWebServer) terminalHistoryGetHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleTerminalHistory(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// terminalHistoryPostHumaHandler is the Huma handler for POST /api/terminal/history.
func (ws *ReactWebServer) terminalHistoryPostHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleTerminalHistory(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// terminalSessionsHumaHandler is the Huma handler for GET /api/terminal/sessions.
// The plain handler does not gate on method; the client contract uses GET, so the
// operation registers the read form.
func (ws *ReactWebServer) terminalSessionsHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPITerminalSessions(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// terminalShellsHumaHandler is the Huma handler for GET /api/terminal/shells.
func (ws *ReactWebServer) terminalShellsHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPITerminalShells(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// agentSessionsListHumaHandler is the Huma handler for GET /api/terminal/agent-sessions.
func (ws *ReactWebServer) agentSessionsListHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIAgentSessions(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// agentSessionActionHumaHandler is the Huma handler for GET and POST
// /api/terminal/agent-sessions/ (subtree). The plain dispatcher parses
// r.URL.Path (the {id}/{action} sub-segments) and routes to the output/attach/kill
// branch, each of which enforces its own method gate.
func (ws *ReactWebServer) agentSessionActionHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIAgentSessionActions(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}
