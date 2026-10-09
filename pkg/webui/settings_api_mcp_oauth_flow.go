//go:build !js

// settings_api_mcp_oauth_flow.go — OAuth endpoints for MCP servers:
// GET /api/settings/mcp/servers/{name}/oauth (status),
// POST .../oauth (start the browser login; blocks until the callback),
// DELETE .../oauth (clear stored tokens).
package webui

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/mcp"
)

// handleAPISettingsMCPServerOAuth dispatches GET/POST/DELETE
// /api/settings/mcp/servers/{name}/oauth.
func (ws *ReactWebServer) handleAPISettingsMCPServerOAuth(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSuffix(extractPathSegment(r.URL.Path, "/api/settings/mcp/servers/"), "/oauth")
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "server name is required in URL path")
		return
	}

	var oauthServerCfg mcp.MCPServerConfig
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}
	cfg := cm.GetConfig()
	var exists bool
	oauthServerCfg, exists = cfg.MCP.Servers[name]
	if !exists {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("MCP server %q not found", name))
		return
	}

	switch r.Method {
	case http.MethodGet:
		loggedIn, detail := mcp.OAuthStatus(name)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"server":    name,
			"logged_in": loggedIn,
			"detail":    detail,
		})
	case http.MethodPost:
		// Blocking: the flow opens a browser and waits on the loopback
		// callback (bounded by the request context / the 5m login timeout).
		result, err := mcp.StartOAuthLogin(r.Context(), name, &oauthServerCfg)
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"server":  name,
			"message": result,
		})
	case http.MethodDelete:
		mcp.OAuthLogout(name)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"server":  name,
			"message": "OAuth state cleared",
		})
	default:
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	}
}
