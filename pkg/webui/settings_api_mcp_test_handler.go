//go:build !js

package webui

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/mcp"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// ---------------------------------------------------------------------------
// POST /api/settings/mcp/servers/{name}/test
// ---------------------------------------------------------------------------

// handleAPISettingsMCPServersTest tests connectivity to an MCP server by
// spinning up a short-lived client that starts, initializes, and lists tools.
// It validates that the server command/URL is reachable and speaks the MCP
// protocol, returning a human-readable status for the settings UI.
//
// Path: /api/settings/mcp/servers/{name}/test
// Method: POST
//
// Response shapes:
//
//	ok    → { "status": "ok",    "message": "...", "tools": N }
//	error → { "status": "error", "message": "..." }
func (ws *ReactWebServer) handleAPISettingsMCPServersTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// The route is registered as a prefix handler under
	// /api/settings/mcp/servers/, so extract the server name and confirm the
	// path ends with /test.
	name := extractPathSegment(r.URL.Path, "/api/settings/mcp/servers/")
	name = strings.TrimSuffix(name, "/test")
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "server name is required in URL path")
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	cfg := cm.GetConfig()
	server, exists := cfg.MCP.Servers[name]
	if !exists {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("MCP server %q not found", name))
		return
	}

	// Validate the config before attempting to connect.
	if err := validateMCPServer(server); err != nil {
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	// Use a bounded timeout so a misbehaving server cannot hang the request.
	testCtx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	tools, testErr := testMCPServerConnection(testCtx, server)
	if testErr != nil {
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "error",
			"message": testErr.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"message": fmt.Sprintf("Connected successfully — %d tools available", len(tools)),
		"tools":   len(tools),
	})
}

// testMCPServerConnection creates an ephemeral MCP client from the provided
// config, starts it, initializes the protocol, and lists tools. It always
// stops the client before returning so no stray process is left behind.
func testMCPServerConnection(ctx context.Context, server mcp.MCPServerConfig) ([]mcp.MCPTool, error) {
	logger := utils.GetLogger(true)
	var client mcp.MCPServer
	if strings.EqualFold(server.Type, "http") {
		client = mcp.NewMCPHTTPClient(server, logger)
	} else {
		client = mcp.NewMCPClient(server, logger)
	}

	if err := client.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start MCP server %q: %w", server.Name, err)
	}

	// Ensure the server is always stopped, even on success, so the test
	// connection does not leak a long-lived process.
	defer func() {
		_ = client.Stop(ctx)
	}()

	if err := client.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize MCP server %q: %w", server.Name, err)
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools from MCP server %q: %w", server.Name, err)
	}

	return tools, nil
}
