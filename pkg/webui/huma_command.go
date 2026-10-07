//go:build !js

// huma_command.go holds the Huma operations and their thin handlers for the
// command family: the /api/command/* routes (registered via
// registerCommandHumaOperations from registerHumaOperations in
// huma_routes.go). These are tagged conversation/query because the tag
// description explicitly covers "command execution".
//
// Each handler drives the existing plain handler through the live ResponseWriter
// and returns a no-op writtenResponseOutput, so the response bytes are unchanged.
package webui

import (
	"context"
	"net/http"

	huma "github.com/danielgtaylor/huma/v2"
)

// registerCommandHumaOperations registers the command-family Huma operations.
func registerCommandHumaOperations(api huma.API, ws *ReactWebServer) {
	huma.Register(api, huma.Operation{
		OperationID: "commandExecute",
		Method:      http.MethodPost,
		Path:        "/api/command/execute",
		Summary:     "Execute a SteerCapable command.",
		Description: "Executes the `command` (a `/`-prefixed slash command) against the chat named by `chat_id` (or the client's active chat). Commands must be SteerCapable — destructive commands stay CLI-only.",
		Tags:        []string{"conversation/query"},
	}, ws.commandExecuteHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "commandComplete",
		Method:      http.MethodPost,
		Path:        "/api/command/complete",
		Summary:     "Complete a command-bar command or argument.",
		Description: "Returns the command-name or argument completions for the `command` (a `/`-prefixed slash command) in the body, mirroring the terminal's slash-completer over HTTP.",
		Tags:        []string{"conversation/query"},
	}, ws.commandCompleteHumaHandler)
}

// commandExecuteHumaHandler is the Huma handler for POST /api/command/execute.
func (ws *ReactWebServer) commandExecuteHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPICommandExecute(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// commandCompleteHumaHandler is the Huma handler for POST /api/command/complete.
func (ws *ReactWebServer) commandCompleteHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPICommandComplete(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}
