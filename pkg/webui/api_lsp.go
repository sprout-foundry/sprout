//go:build !js

package webui

import (
	"net/http"

	lspproxy "github.com/sprout-foundry/sprout/pkg/lsp/proxy"
)

// handleLSPStatus returns information about available and running LSP servers.
// GET /api/lsp/status
func (ws *ReactWebServer) handleLSPStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	configs := lspproxy.DefaultLanguageServers()

	type serverInfo struct {
		ID          string   `json:"id"`
		Languages   []string `json:"languages"`
		Binary      string   `json:"binary"`
		Available   bool     `json:"available"`
		BinaryPath  string   `json:"binaryPath,omitempty"`
		InstallHint string   `json:"installHint,omitempty"`
	}

	servers := make([]serverInfo, 0, len(configs))
	for _, cfg := range configs {
		info := serverInfo{
			ID:          cfg.ID,
			Languages:   cfg.LanguageIDs,
			Binary:      cfg.Binary,
			InstallHint: cfg.InstallHint,
		}
		path, err := lspproxy.ResolveBinaryPath(cfg.Binary)
		if err == nil {
			info.Available = true
			info.BinaryPath = path
		}
		servers = append(servers, info)
	}

	resp := struct {
		Servers   []serverInfo `json:"servers"`
		Active    int          `json:"active"`
		Workspace string       `json:"workspace"`
	}{
		Servers:   servers,
		Active:    ws.lspManager.Count(),
		Workspace: ws.workspaceRoot,
	}

	writeJSON(w, http.StatusOK, resp)
}
