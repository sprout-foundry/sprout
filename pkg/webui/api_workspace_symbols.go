package webui

import (
	"encoding/json"
	"fmt"
	"log"
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
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	if workspaceRoot == "" {
		http.Error(w, "Workspace not found", http.StatusBadRequest)
		return
	}

	// Build the symbol index for the workspace
	idx, err := index.BuildSymbols(workspaceRoot)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to build symbol index: %v", err), http.StatusInternalServerError)
		return
	}

	// Get the query parameter
	query := strings.TrimSpace(r.URL.Query().Get("query"))

	var resultFiles = make([]index.FileSymbols, 0)
	var total int

	if query != "" {
		// Split query into tokens (split on whitespace)
		tokens := strings.Fields(query)
		if len(tokens) == 0 {
			// Empty query, return empty results
			resultFiles = []index.FileSymbols{}
			total = 0
		} else {
			// Search for matching files
			matchingFiles := index.SearchSymbols(idx, tokens)
			if len(matchingFiles) == 0 {
				resultFiles = []index.FileSymbols{}
				total = 0
			} else {
				// Build result with symbols from matching files, capped at maxSymbolResults
				fileSet := make(map[string]bool)
				for _, f := range matchingFiles {
					fileSet[f] = true
				}
				for _, fs := range idx.Files {
					if fileSet[fs.File] {
						if total+len(fs.Symbols) > maxSymbolResults {
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
		log.Printf("Failed to encode symbol response: %v", err)
	}
}
