package history

// data_access.go — the change-tracking data access layer: the path /
// revision state (changesDir / revisionsDir + the pathMu guard), the path
// test helpers, the ChangeMetadata / ChangeLog types, and the write / record
// side (RecordBaseRevision, RecordChangeWithDetails, RecordChange,
// updateChangeStatus, MarkChangeSuperseded). The read / query + cleanup side
// lives in data_access_query.go.

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

const (
	projectChangesDir   = ".sprout/changes"
	projectRevisionsDir = ".sprout/revisions"
	activeStatus        = "active"
	revertedStatus      = "reverted"
	restoredStatus      = "restored"
	metadataFile        = "metadata.json"
	originalSuffix      = ".original"
	updatedSuffix       = ".updated"
	metadataVersion     = 1
)

var (
	pathMu       sync.RWMutex
	changesDir   string = projectChangesDir
	revisionsDir string = projectRevisionsDir
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
	AgentModel       string    `json:"agent_model,omitempty"`     // Added: Editing model used
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
	AgentModel       string // Added: Editing model used
	HasConversation  bool   // Added: Whether conversation.json exists for this revision
	// Tier reflects the revision's compaction state: "hot" (full data
	// including conversation.json) or "warm" (conversation.json
	// dropped). Empty string is treated as hot for backward compat.
	Tier string
}

// InitializeHistoryPaths configures the history storage paths based on configuration
// This should be called at application startup to ensure correct path resolution.
//
// SP-133: changes/ and revisions/ are now workspace-local only (under
// <workspace>/.sprout/). The global "HistoryScope" branch is removed —
// it created a dual-role directory when the workspace was $HOME, causing
// the user-level state dir to accumulate per-repo snapshots.
func InitializeHistoryPaths(config *configuration.Config) {
	// History is always project-scoped: .sprout/changes and .sprout/revisions
	// under the workspace root. No global branch.
	if config != nil && config.HistoryScope == "global" {
		log.Printf("[history] warning: history_scope=\"global\" is no longer supported (SP-133); using project-scoped history")
	}
	pathMu.Lock()
	changesDir = projectChangesDir
	revisionsDir = projectRevisionsDir
	pathMu.Unlock()
}

// setPathsForTesting sets changesDir and revisionsDir while holding the mutex.
// This is intended for use only in tests in this package to avoid data races
// detected by -race. Tests in OTHER packages that need to isolate the history
// storage location must call SetPathsForTesting (the exported wrapper below)
// — using this unexported function would result in a compile error there.
func setPathsForTesting(cDir, rDir string) {
	pathMu.Lock()
	changesDir = cDir
	revisionsDir = rDir
	pathMu.Unlock()
}

// SetPathsForTesting is the cross-package test hook for redirecting
// the history storage to a temporary directory. Callers (typically
// tests in pkg/agent and other consumers) should set both SPROUT_CONFIG
// (via configuration.NewTestManager) AND call this function with a
// fresh t.TempDir()-derived path — NewTestManager alone is insufficient
// because HistoryScope="project" (the default) resolves changesDir and
// revisionsDir to relative paths under the process CWD, not the test's
// temp config dir. Without this hook, every test asserting exact change
// counts (e.g. TestChangeTrackingE2E's "len(allChanges) == 1") reads
// from the shared .sprout/changes/ in the repo root and fails on runs
// where prior tests or sessions have left residue.
//
// Designed for t.Cleanup use:
//
//	tmp := t.TempDir()
//	history.SetPathsForTesting(filepath.Join(tmp, "changes"), filepath.Join(tmp, "revisions"))
//	t.Cleanup(func() { history.SetPathsForTesting(originalChanges, originalRevisions) })
//
// Reads current values via GetPathsForTesting when restoring.
//
// Safe to call from multiple goroutines; takes the same package-level
// pathMu that the production path resolvers use.
func SetPathsForTesting(cDir, rDir string) {
	setPathsForTesting(cDir, rDir)
}

// GetPathsForTesting is the cross-package test hook for reading the
// current history storage paths. Tests typically pair this with
// SetPathsForTesting to capture the pre-test values and restore them
// in t.Cleanup, so a test that redirects storage to a temp dir does
// not leak that redirect into sibling tests or later runs of the
// same test in -count=N invocations.
//
// Returns (changesDir, revisionsDir). Safe to call from multiple
// goroutines.
func GetPathsForTesting() (string, string) {
	return getPathsForTesting()
}

// getPathsForTesting reads changesDir and revisionsDir while holding the mutex.
// This is intended for use only in tests in this package to avoid data races
// detected by -race. Tests in OTHER packages must call GetPathsForTesting.
func getPathsForTesting() (string, string) {
	pathMu.RLock()
	defer pathMu.RUnlock()
	return changesDir, revisionsDir
}

// GetChangesDir returns the current changes directory path
func GetChangesDir() string {
	pathMu.RLock()
	defer pathMu.RUnlock()
	return changesDir
}

// GetRevisionsDir returns the current revisions directory path
func GetRevisionsDir() string {
	pathMu.RLock()
	defer pathMu.RUnlock()
	return revisionsDir
}

func ensureChangesDirs() error {
	if err := filesystem.EnsureDir(GetChangesDir()); err != nil {
		return fmt.Errorf("failed to create changes directory: %w", err)
	}
	if err := filesystem.EnsureDir(GetRevisionsDir()); err != nil {
		return fmt.Errorf("failed to create revisions directory: %w", err)
	}
	return nil
}

// RecordBaseRevision saves the initial request and response, returning a revision ID.
// conversation is the full conversation history (all user/assistant/tool messages)
func RecordBaseRevision(requestHash, instructions, response string, conversation []APIMessage) (string, error) {
	if err := ensureChangesDirs(); err != nil {
		return "", fmt.Errorf("failed to ensure changes directories: %w", err)
	}

	revisionID := requestHash
	revisionPath := filepath.Join(GetRevisionsDir(), revisionID)
	if err := filesystem.EnsureDir(revisionPath); err != nil {
		return "", fmt.Errorf("failed to create revision directory: %w", err)
	}

	if err := filesystem.WriteFileWithDir(filepath.Join(revisionPath, "instructions.txt"), []byte(instructions), 0644); err != nil {
		return "", fmt.Errorf("failed to save instructions: %w", err)
	}
	if err := filesystem.WriteFileWithDir(filepath.Join(revisionPath, "llm_response.txt"), []byte(response), 0644); err != nil {
		return "", fmt.Errorf("failed to save LLM response: %w", err)
	}

	// Save conversation as JSON for multi-turn spec extraction
	if conversation != nil && len(conversation) > 0 {
		conversationBytes, err := json.MarshalIndent(conversation, "", "  ")
		if err != nil {
			return "", fmt.Errorf("failed to marshal conversation: %w", err)
		}
		if err := filesystem.WriteFileWithDir(filepath.Join(revisionPath, "conversation.json"), conversationBytes, 0644); err != nil {
			return "", fmt.Errorf("failed to save conversation: %w", err)
		}
	}

	return revisionID, nil
}

// RecordChangeWithDetails saves a specific file change against a base revision with additional details.
func RecordChangeWithDetails(baseRevisionID string, filename, originalCode, newCode, description, note string, originalPrompt string, llmMessage string, editingModel string) error {
	if err := ensureChangesDirs(); err != nil {
		return fmt.Errorf("ensure changes dirs: %w", err)
	}

	cDir := GetChangesDir()
	fileRevisionHash := utils.GenerateFileRevisionHash(filename, newCode)
	changeDir := filepath.Join(cDir, fileRevisionHash)
	if err := filesystem.EnsureDir(changeDir); err != nil {
		return fmt.Errorf("failed to create change directory: %w", err)
	}

	// Sanitize filename to avoid creating subdirectories within the change dir
	safeFilename := SafeChangeFilename(filename)

	// Encode file contents in base64 to avoid grep conflicts
	originalEncoded := base64.StdEncoding.EncodeToString([]byte(originalCode))
	newEncoded := base64.StdEncoding.EncodeToString([]byte(newCode))

	if err := filesystem.WriteFileWithDir(filepath.Join(changeDir, safeFilename+originalSuffix), []byte(originalEncoded), 0644); err != nil {
		return fmt.Errorf("failed to save original code: %w", err)
	}
	if err := filesystem.WriteFileWithDir(filepath.Join(changeDir, safeFilename+updatedSuffix), []byte(newEncoded), 0644); err != nil {
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
		AgentModel:       editingModel,
	}

	metadataBytes, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := filesystem.WriteFileWithDir(filepath.Join(changeDir, metadataFile), metadataBytes, 0644); err != nil {
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
	changeDir := filepath.Join(GetChangesDir(), fileRevisionHash)
	metadataPath := filepath.Join(changeDir, metadataFile)

	metadataBytes, err := filesystem.ReadFileBytes(metadataPath)
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

	if err := filesystem.WriteFileWithDir(metadataPath, updatedMetadata, 0644); err != nil {
		return fmt.Errorf("failed to write updated metadata: %w", err)
	}

	return nil
}

// MarkChangeSuperseded marks a change record as "superseded" — the
// change has been committed to version control and is no longer a
// recoverable agent edit. This is used by the SP-077 sweep in
// ChangeTracker.Commit() to prevent old snapshots from being reverted
// after their content has been committed to git HEAD.
func MarkChangeSuperseded(fileRevisionHash string) error {
	return updateChangeStatus(fileRevisionHash, "superseded")
}
