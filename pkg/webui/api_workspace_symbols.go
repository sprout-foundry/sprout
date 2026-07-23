//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/index"
)

const (
	maxSymbolResults = 2000 // Maximum total symbols to return when no query is provided
)

// handleAPIWorkspaceSymbols handles GET /api/workspace/symbols?query=xxx
// It returns a JSON response with all symbols or filtered symbols.
func (ws *ReactWebServer) handleAPIWorkspaceSymbols(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	if workspaceRoot == "" {
		writeJSONErr(w, http.StatusBadRequest, "workspace_not_found", "Workspace not found")
		return
	}

	// Try loading cached symbols first
	idx, err := index.LoadSymbols(workspaceRoot)
	if err != nil {
		ws.log().Debug("failed to load symbol index", slog.Any("err", err))
	}
	// If cache doesn't exist or has no files, build fresh
	if idx == nil || len(idx.Files) == 0 {
		idx, err = index.BuildSymbols(workspaceRoot)
		if err != nil {
			writeJSONErr(w, http.StatusInternalServerError, "symbol_index_build_failed", fmt.Sprintf("Failed to build symbol index: %v", err))
			return
		}
	}

	// Get the query parameter
	query := strings.TrimSpace(r.URL.Query().Get("query"))

	var resultFiles []index.FileSymbols
	var total int

	if query != "" {
		// Split query into tokens (split on whitespace)
		tokens := strings.Fields(query)
		if len(tokens) == 0 {
			// Empty query, return empty results
			resultFiles = []index.FileSymbols{}
			total = 0
		} else {
			// Search for matching files with per-symbol filtering
			resultFiles = index.SearchSymbolFiles(idx, tokens)
			total = 0
			for _, fs := range resultFiles {
				total += len(fs.Symbols)
			}
			// Cap results
			if total > maxSymbolResults {
				capped := resultFiles[:0]
				remaining := maxSymbolResults
				for _, fs := range resultFiles {
					if remaining <= 0 {
						break
					}
					if len(fs.Symbols) > remaining {
						capped = append(capped, index.FileSymbols{
							File:    fs.File,
							Symbols: fs.Symbols[:remaining],
						})
						remaining = 0
					} else {
						capped = append(capped, fs)
						remaining -= len(fs.Symbols)
					}
				}
				resultFiles = capped
				total = maxSymbolResults
			}
		}
	} else {
		// No query, return all symbols up to the limit
		total = 0
		for _, fs := range idx.Files {
			if total+len(fs.Symbols) > maxSymbolResults {
				// Truncate symbols from this file to fit the limit
				remaining := maxSymbolResults - total
				if remaining > 0 {
					resultFiles = append(resultFiles, index.FileSymbols{
						File:    fs.File,
						Symbols: fs.Symbols[:remaining],
					})
					total += remaining
				}
				break
			}
			resultFiles = append(resultFiles, fs)
			total += len(fs.Symbols)
		}
	}

	// Build response
	response := map[string]interface{}{
		"message": "ok",
		"files":   resultFiles,
		"total":   total,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		// Logging error but can't do much at this point
		ws.log().Error("failed to encode symbol response", slog.Any("err", err))
	}
}
