// Package webui provides history and rollback operation handlers
package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
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
	FileRevisionHash string `json:"file_revision_hash,omitempty"`
	Path             string `json:"path"`
	Operation        string `json:"operation"`
	LinesAdded       int    `json:"lines_added"`
	LinesDeleted     int    `json:"lines_deleted"`
}

type FileRevisionDetail struct {
	FileRevisionHash string `json:"file_revision_hash,omitempty"`
	Path             string `json:"path"`
	Operation        string `json:"operation"`
	LinesAdded       int    `json:"lines_added"`
	LinesDeleted     int    `json:"lines_deleted"`
	OriginalCode     string `json:"original_code"`
	NewCode          string `json:"new_code"`
	Diff             string `json:"diff"`
}

var ansiSequencePattern = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

const maxRevisions = 100

// relativeFilePath converts an absolute or workspace-relative path to a relative path
// from the workspace root for display purposes.
func (ws *ReactWebServer) relativeFilePath(path string) string {
	rel, err := filepath.Rel(ws.workspaceRoot, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return rel
}

// buildChangelogEntry converts a history.RevisionGroup into a ChangelogEntry with relative file paths
func (ws *ReactWebServer) buildChangelogEntry(group history.RevisionGroup) ChangelogEntry {
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
			FileRevisionHash: change.FileRevisionHash,
			Path:             ws.relativeFilePath(change.Filename),
			Operation:        operation,
			LinesAdded:       linesAdded,
			LinesDeleted:     linesDeleted,
		})
	}

	return ChangelogEntry{
		RevisionID:  group.RevisionID,
		Timestamp:   group.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
		Files:       files,
		Description: group.Instructions,
	}
}

// revisionGroupArrayToEntry converts a array of RevisionGroup to ChangelogEntry array
func (ws *ReactWebServer) convertRevisionGroups(revisionGroups []history.RevisionGroup, limit int) []ChangelogEntry {
	if len(revisionGroups) > limit {
		revisionGroups = revisionGroups[:limit]
	}
	revisions := make([]ChangelogEntry, 0, len(revisionGroups))
	for _, group := range revisionGroups {
		revisions = append(revisions, ws.buildChangelogEntry(group))
	}
	return revisions
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

	revisions := ws.convertRevisionGroups(revisionGroups, maxRevisions)

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
			"timestamp":   fmt.Sprintf("%d", 123), // Dummy timestamp
			"data": map[string]interface{}{
				"action":      "rollback",
				"path":        "multiple",
				"revision_id": req.RevisionID,
			},
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":     "Rollback successful",
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

	changes := ws.convertRevisionGroups(revisionGroups, maxRevisions)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "success",
		"changes": changes,
	})
}

// handleAPIHistoryRevision handles requests to get detailed changes for a revision
func (ws *ReactWebServer) handleAPIHistoryRevision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	revisionID := strings.TrimSpace(r.URL.Query().Get("revision_id"))
	if revisionID == "" {
		http.Error(w, "revision_id is required", http.StatusBadRequest)
		return
	}

	revisionGroups, err := history.GetRevisionGroups()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get revision history: %v", err), http.StatusInternalServerError)
		return
	}

	for _, group := range revisionGroups {
		if group.RevisionID != revisionID {
			continue
		}

		files := make([]FileRevisionDetail, 0, len(group.Changes))
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

			diff := history.GetDiff(change.Filename, change.OriginalCode, change.NewCode)
			diff = ansiSequencePattern.ReplaceAllString(diff, "")

			files = append(files, FileRevisionDetail{
				FileRevisionHash: change.FileRevisionHash,
				Path:             ws.relativeFilePath(change.Filename),
				Operation:        operation,
				LinesAdded:       linesAdded,
				LinesDeleted:     linesDeleted,
				OriginalCode:     change.OriginalCode,
				NewCode:          change.NewCode,
				Diff:             diff,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "success",
			"revision": map[string]interface{}{
				"revision_id": group.RevisionID,
				"timestamp":   group.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
				"description": group.Instructions,
				"files":       files,
			},
		})
		return
	}

	http.Error(w, fmt.Sprintf("revision %q not found", revisionID), http.StatusNotFound)
}
