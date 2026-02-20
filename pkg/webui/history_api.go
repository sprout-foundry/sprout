// Package webui provides history and rollback operation handlers
package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/alantheprice/ledit/pkg/history"
)

// ChangelogEntry represents a changelog entry
type ChangelogEntry struct {
	RevisionID  string         `json:"revision_id"`
	Timestamp   string         `json:"timestamp"`
	Files       []FileRevision `json:"files"`
	Description string         `json:"description"`
}

// FileRevision represents a file revision
type FileRevision struct {
	Path         string `json:"path"`
	Operation    string `json:"operation"`
	LinesAdded   int    `json:"lines_added"`
	LinesDeleted int    `json:"lines_deleted"`
}

// handleAPIHistoryChangelog handles requests to get the changelog
func (ws *ReactWebServer) handleAPIHistoryChangelog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get revision groups from history package
	revisionGroups, err := history.GetRevisionGroups()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get revision history: %v", err), http.StatusInternalServerError)
		return
	}

	// Convert to API response format
	revisions := make([]ChangelogEntry, 0, len(revisionGroups))
	for _, group := range revisionGroups {
		files := make([]FileRevision, 0, len(group.Changes))
		for _, change := range group.Changes {
			operation := "edited"
			if change.OriginalCode == "" {
				operation = "created"
			} else if change.NewCode == "" {
				operation = "deleted"
			}

			linesAdded := 0
			linesDeleted := 0
			if change.OriginalCode != "" {
				linesDeleted = len(strings.Split(change.OriginalCode, "\n"))
			}
			if change.NewCode != "" {
				linesAdded = len(strings.Split(change.NewCode, "\n"))
			}

			files = append(files, FileRevision{
				Path:         change.Filename,
				Operation:    operation,
				LinesAdded:   linesAdded,
				LinesDeleted: linesDeleted,
			})
		}

		revisions = append(revisions, ChangelogEntry{
			RevisionID:  group.RevisionID,
			Timestamp:   group.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
			Files:       files,
			Description: group.Instructions,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":   "success",
		"revisions": revisions,
	})
}

// handleAPIHistoryRollback handles rollback requests
func (ws *ReactWebServer) handleAPIHistoryRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		RevisionID string `json:"revision_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.RevisionID == "" {
		http.Error(w, "Revision ID is required", http.StatusBadRequest)
		return
	}

	// Perform rollback
	err := history.RevertChangeByRevisionID(req.RevisionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Rollback failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Publish event
	if ws.eventBus != nil {
		ws.eventBus.Publish("rollback", map[string]interface{}{
			"revision_id": req.RevisionID,
			"timestamp": fmt.Sprintf("%d", 123), // Dummy timestamp
			"data": map[string]interface{}{
				"action": "rollback",
				"path": "multiple",
				"revision_id": req.RevisionID,
			},
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Rollback successful",
		"revision_id": req.RevisionID,
	})
}

// handleAPIHistoryChanges handles requests to get current session changes
func (ws *ReactWebServer) handleAPIHistoryChanges(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get revision groups from history package
	revisionGroups, err := history.GetRevisionGroups()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get changes: %v", err), http.StatusInternalServerError)
		return
	}

	// Convert to API response format (same as changelog)
	changes := make([]ChangelogEntry, 0, len(revisionGroups))
	for _, group := range revisionGroups {
		files := make([]FileRevision, 0, len(group.Changes))
		for _, change := range group.Changes {
			operation := "edited"
			if change.OriginalCode == "" {
				operation = "created"
			} else if change.NewCode == "" {
				operation = "deleted"
			}

			linesAdded := 0
			linesDeleted := 0
			if change.OriginalCode != "" {
				linesDeleted = len(strings.Split(change.OriginalCode, "\n"))
			}
			if change.NewCode != "" {
				linesAdded = len(strings.Split(change.NewCode, "\n"))
			}

			files = append(files, FileRevision{
				Path:         change.Filename,
				Operation:    operation,
				LinesAdded:   linesAdded,
				LinesDeleted: linesDeleted,
			})
		}

		changes = append(changes, ChangelogEntry{
			RevisionID:  group.RevisionID,
			Timestamp:   group.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
			Files:       files,
			Description: group.Instructions,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "success",
		"changes": changes,
	})
}
