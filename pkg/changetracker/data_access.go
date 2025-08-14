package changetracker

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/utils"
)

const (
	changesDir      = ".ledit/changes"
	revisionsDir    = ".ledit/revisions"
	activeStatus    = "active"
	revertedStatus  = "reverted"
	restoredStatus  = "restored"
	metadataFile    = "metadata.json"
	originalSuffix  = ".original"
	updatedSuffix   = ".updated"
	metadataVersion = 1
)

// ChangeMetadata stores metadata about a specific file change.
type ChangeMetadata struct {
	Version          int       `json:"version"`
	Filename         string    `json:"filename"`
	FileRevisionHash string    `json:"file_revision_hash"`
	RequestHash      string    `json:"request_hash"` // This is the revision ID
	Timestamp        time.Time `json:"timestamp"`
	Status           string    `json:"status"`
	Note             string    `json:"note"`
	Description      string    `json:"description"`
	OriginalPrompt   string    `json:"original_prompt,omitempty"` // Added: Original user prompt
	LLMMessage       string    `json:"llm_message,omitempty"`     // Added: Full message sent to LLM
	EditingModel     string    `json:"editing_model,omitempty"`   // Added: Editing model used
}

// ChangeLog represents a logged change, including context from the base revision.
type ChangeLog struct {
	RequestHash      string
	Instructions     string
	Response         string
	FileRevisionHash string
	Filename         string
	OriginalCode     string
	NewCode          string
	Description      string
	Note             sql.NullString
	Status           string
	Timestamp        time.Time
	OriginalPrompt   string // Added: Original user prompt
	LLMMessage       string // Added: Full message sent to LLM
	EditingModel     string // Added: Editing model used
}

func ensureChangesDirs() error {
	if err := os.MkdirAll(changesDir, 0755); err != nil {
		return fmt.Errorf("failed to create changes directory: %w", err)
	}
	if err := os.MkdirAll(revisionsDir, 0755); err != nil {
		return fmt.Errorf("failed to create revisions directory: %w", err)
	}
	return nil
}

// RecordBaseRevision saves the initial request and response, returning a revision ID.
func RecordBaseRevision(requestHash, instructions, response string) (string, error) {
	if err := ensureChangesDirs(); err != nil {
		return "", err
	}

	revisionID := requestHash
	revisionPath := filepath.Join(revisionsDir, revisionID)
	if err := os.MkdirAll(revisionPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create revision directory: %w", err)
	}

	if err := os.WriteFile(filepath.Join(revisionPath, "instructions.txt"), []byte(instructions), 0644); err != nil {
		return "", fmt.Errorf("failed to save instructions: %w", err)
	}
	if err := os.WriteFile(filepath.Join(revisionPath, "llm_response.txt"), []byte(response), 0644); err != nil {
		return "", fmt.Errorf("failed to save LLM response: %w", err)
	}

	return revisionID, nil
}

// RecordChangeWithDetails saves a specific file change against a base revision with additional details.
func RecordChangeWithDetails(baseRevisionID string, filename, originalCode, newCode, description, note string, originalPrompt string, llmMessage string, editingModel string) error {
	if err := ensureChangesDirs(); err != nil {
		return err
	}

	fileRevisionHash := utils.GenerateFileRevisionHash(filename, newCode)
	changeDir := filepath.Join(changesDir, fileRevisionHash)
	if err := os.MkdirAll(changeDir, 0755); err != nil {
		return fmt.Errorf("failed to create change directory: %w", err)
	}

	// Sanitize filename to avoid creating subdirectories within the change dir
	safeFilename := strings.ReplaceAll(filename, "/", "_")
	safeFilename = strings.ReplaceAll(safeFilename, "\\", "_")

	if err := os.WriteFile(filepath.Join(changeDir, safeFilename+originalSuffix), []byte(originalCode), 0644); err != nil {
		return fmt.Errorf("failed to save original code: %w", err)
	}
	if err := os.WriteFile(filepath.Join(changeDir, safeFilename+updatedSuffix), []byte(newCode), 0644); err != nil {
		return fmt.Errorf("failed to save updated code: %w", err)
	}

	metadata := ChangeMetadata{
		Version:          metadataVersion,
		Filename:         filename,
		FileRevisionHash: fileRevisionHash,
		RequestHash:      baseRevisionID,
		Timestamp:        time.Now(),
		Status:           activeStatus,
		Note:             note,
		Description:      description,
		OriginalPrompt:   originalPrompt,
		LLMMessage:       llmMessage,
		EditingModel:     editingModel,
	}

	metadataBytes, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(filepath.Join(changeDir, metadataFile), metadataBytes, 0644); err != nil {
		return fmt.Errorf("failed to save metadata: %w", err)
	}

	return nil
}

// RecordChange saves a specific file change against a base revision.
func RecordChange(baseRevisionID string, filename, originalCode, newCode, description, note string) error {
	return RecordChangeWithDetails(baseRevisionID, filename, originalCode, newCode, description, note, "", "", "")
}

// updateChangeStatus updates the status of a change record.
func updateChangeStatus(fileRevisionHash, status string) error {
	changeDir := filepath.Join(changesDir, fileRevisionHash)
	metadataPath := filepath.Join(changeDir, metadataFile)

	metadataBytes, err := os.ReadFile(metadataPath)
	if err != nil {
		return fmt.Errorf("failed to read metadata: %w", err)
	}

	var metadata ChangeMetadata
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		return fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	metadata.Status = status

	updatedMetadata, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal updated metadata: %w", err)
	}

	if err := os.WriteFile(metadataPath, updatedMetadata, 0644); err != nil {
		return fmt.Errorf("failed to write updated metadata: %w", err)
	}

	return nil
}

// fetchAllChanges retrieves all change logs from the filesystem.
func fetchAllChanges() ([]ChangeLog, error) {
	if err := ensureChangesDirs(); err != nil {
		return nil, err
	}

	var changes []ChangeLog

	entries, err := os.ReadDir(changesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []ChangeLog{}, nil
		}
		return nil, fmt.Errorf("failed to read changes directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		changeDir := filepath.Join(changesDir, entry.Name())
		metadataPath := filepath.Join(changeDir, metadataFile)

		metadataBytes, err := os.ReadFile(metadataPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue // Not a valid change directory, skip.
			}
			return nil, fmt.Errorf("failed to read metadata for %s: %w", entry.Name(), err)
		}

		var metadata ChangeMetadata
		if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata for %s: %w", entry.Name(), err)
		}

		safeFilename := strings.ReplaceAll(metadata.Filename, "/", "_")
		safeFilename = strings.ReplaceAll(safeFilename, "\\", "_")

		originalBytes, err := os.ReadFile(filepath.Join(changeDir, safeFilename+originalSuffix))
		if err != nil {
			return nil, fmt.Errorf("failed to read original code for %s: %w", metadata.Filename, err)
		}
		originalCode := string(originalBytes)

		updatedBytes, err := os.ReadFile(filepath.Join(changeDir, safeFilename+updatedSuffix))
		if err != nil {
			return nil, fmt.Errorf("failed to read updated code for %s: %w", metadata.Filename, err)
		}
		newCode := string(updatedBytes)

		// Fetch instructions and response from revisions directory
		revisionPath := filepath.Join(revisionsDir, metadata.RequestHash)
		instructionsBytes, err := os.ReadFile(filepath.Join(revisionPath, "instructions.txt"))
		var instructions string
		if err == nil {
			instructions = string(instructionsBytes)
		}

		responseBytes, err := os.ReadFile(filepath.Join(revisionPath, "llm_response.txt"))
		var response string
		if err == nil {
			response = string(responseBytes)
		}

		changes = append(changes, ChangeLog{
			RequestHash:      metadata.RequestHash,
			Instructions:     instructions,
			Response:         response,
			FileRevisionHash: metadata.FileRevisionHash,
			Filename:         metadata.Filename,
			OriginalCode:     originalCode,
			NewCode:          newCode,
			Description:      metadata.Description,
			Note:             sql.NullString{String: metadata.Note, Valid: metadata.Note != ""},
			Status:           metadata.Status,
			Timestamp:        metadata.Timestamp,
			OriginalPrompt:   metadata.OriginalPrompt,
			LLMMessage:       metadata.LLMMessage,
			EditingModel:     metadata.EditingModel,
		})
	}

	// Sort changes by timestamp in descending order (most recent first)
	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Timestamp.After(changes[j].Timestamp)
	})

	return changes, nil
}

// GetAllChanges returns all recorded changes (most recent first).
func GetAllChanges() ([]ChangeLog, error) {
	return fetchAllChanges()
}

// GetChangesSince returns changes whose timestamp is strictly after the provided time.
func GetChangesSince(since time.Time) ([]ChangeLog, error) {
	changes, err := fetchAllChanges()
	if err != nil {
		return nil, err
	}
	var filtered []ChangeLog
	for _, c := range changes {
		if c.Timestamp.After(since) {
			filtered = append(filtered, c)
		}
	}
	return filtered, nil
}

// GetChangedFilesSince returns a unique list of filenames changed after the given time.
func GetChangedFilesSince(since time.Time) ([]string, error) {
	changes, err := GetChangesSince(since)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	files := []string{}
	for _, c := range changes {
		if !seen[c.Filename] {
			seen[c.Filename] = true
			files = append(files, c.Filename)
		}
	}
	// Keep file list stable order by timestamp order already provided
	return files, nil
}
