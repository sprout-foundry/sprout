package history

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
// This should be called at application startup to ensure correct path resolution
func InitializeHistoryPaths(config *configuration.Config) {
	var cDir, rDir string

	if config == nil {
		// Try to load config if not provided
		cfg, err := configuration.Load()
		if err != nil {
			cDir = projectChangesDir
			rDir = projectRevisionsDir
			pathMu.Lock()
			changesDir = cDir
			revisionsDir = rDir
			pathMu.Unlock()
			return
		}
		config = cfg
	}

	// Determine history path based on configuration scope
	if config.HistoryScope == "global" {
		// Use global history in ~/.config/sprout/
		configDir, err := configuration.GetConfigDir()
		if err != nil {
			// Fallback to project-scoped if global config dir fails
			cDir = projectChangesDir
			rDir = projectRevisionsDir
		} else {
			cDir = filepath.Join(configDir, "changes")
			rDir = filepath.Join(configDir, "revisions")
		}
	} else {
		// Default to project-scoped history (historyScope == "project" or empty)
		cDir = projectChangesDir
		rDir = projectRevisionsDir
	}

	pathMu.Lock()
	changesDir = cDir
	revisionsDir = rDir
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
	safeFilename := strings.ReplaceAll(filename, "/", "_")
	safeFilename = strings.ReplaceAll(safeFilename, "\\", "_")

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

// fetchAllChanges retrieves all change logs from the filesystem.
func fetchAllChanges() ([]ChangeLog, error) {
	if err := ensureChangesDirs(); err != nil {
		return nil, fmt.Errorf("get changes directory: %w", err)
	}

	var changes []ChangeLog

	entries, err := os.ReadDir(GetChangesDir())
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

		changeDir := filepath.Join(GetChangesDir(), entry.Name())
		metadataPath := filepath.Join(changeDir, metadataFile)

		metadataBytes, err := filesystem.ReadFileBytes(metadataPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue // Not a valid change directory, skip.
			}
			log.Printf("[history] skipping change %s: failed to read metadata: %v", entry.Name(), err)
			continue
		}

		var metadata ChangeMetadata
		if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
			log.Printf("[history] skipping change %s: failed to parse metadata: %v", entry.Name(), err)
			continue
		}

		safeFilename := strings.ReplaceAll(metadata.Filename, "/", "_")
		safeFilename = strings.ReplaceAll(safeFilename, "\\", "_")

		// Tier detection: a revision dir is "warm" when conversation.json
		// has been dropped (the compaction policy's only transition
		// before outright drop). Surface the tier so view_history can
		// label entries; payloads are always present for any revision
		// that hasn't been dropped entirely.
		revisionPath := filepath.Join(GetRevisionsDir(), metadata.RequestHash)
		tier, instructions, response := loadRevisionTextForTier(revisionPath)

		originalBytes, origErr := filesystem.ReadFileBytes(filepath.Join(changeDir, safeFilename+originalSuffix))
		if origErr != nil {
			log.Printf("[history] skipping change %s: failed to read original code for %s: %v", entry.Name(), metadata.Filename, origErr)
			continue
		}
		updatedBytes, updErr := filesystem.ReadFileBytes(filepath.Join(changeDir, safeFilename+updatedSuffix))
		if updErr != nil {
			log.Printf("[history] skipping change %s: failed to read updated code for %s: %v", entry.Name(), metadata.Filename, updErr)
			continue
		}
		originalDecoded, decErr := base64.StdEncoding.DecodeString(string(originalBytes))
		if decErr != nil {
			originalDecoded = originalBytes
		}
		originalCode := string(originalDecoded)
		updatedDecoded, decErr := base64.StdEncoding.DecodeString(string(updatedBytes))
		if decErr != nil {
			updatedDecoded = updatedBytes
		}
		newCode := string(updatedDecoded)

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
			AgentModel:       metadata.AgentModel,
			HasConversation:  fileExists(filepath.Join(revisionPath, "conversation.json")),
			Tier:             tier,
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

// GetAllChangesMetadata returns change metadata WITHOUT reading or
// base64-decoding the .original/.updated content files. This is the
// lightweight alternative to GetAllChanges for callers that only need
// the manifest fields (filename, revision, timestamp, status, tier) —
// primarily list_changes when include_diff/show_content aren't set.
//
// The OriginalCode and NewCode fields of the returned ChangeLog entries
// are left EMPTY. Callers that infer op/recoverability from content
// presence should instead use HasOriginal/HasNew, which report whether
// the content files exist on disk (a cheap os.Stat, not a read+decode).
// This avoids the O(total-history) base64 decode that fetchAllChanges
// performs on every list_changes invocation.
func GetAllChangesMetadata() ([]ChangeLog, error) {
	if err := ensureChangesDirs(); err != nil {
		return nil, fmt.Errorf("get changes directory: %w", err)
	}

	var changes []ChangeLog

	entries, err := os.ReadDir(GetChangesDir())
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

		changeDir := filepath.Join(GetChangesDir(), entry.Name())
		metadataPath := filepath.Join(changeDir, metadataFile)

		metadataBytes, err := filesystem.ReadFileBytes(metadataPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			log.Printf("[history] skipping change %s: failed to read metadata: %v", entry.Name(), err)
			continue
		}

		var metadata ChangeMetadata
		if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
			log.Printf("[history] skipping change %s: failed to parse metadata: %v", entry.Name(), err)
			continue
		}

		// Determine content presence via os.Stat (cheap) rather than
		// reading + base64-decoding. Store in OriginalCode/NewCode as
		// non-empty sentinels so existing callers that check
		// `OriginalCode != ""` for recoverability still work.
		safeFilename := strings.ReplaceAll(metadata.Filename, "/", "_")
		safeFilename = strings.ReplaceAll(safeFilename, "\\", "_")

		hasOriginal := fileExists(filepath.Join(changeDir, safeFilename+originalSuffix))
		hasNew := fileExists(filepath.Join(changeDir, safeFilename+updatedSuffix))

		var origSentinel, newSentinel string
		if hasOriginal {
			origSentinel = "(metadata-only: original exists)"
		}
		if hasNew {
			newSentinel = "(metadata-only: new exists)"
		}

		revisionPath := filepath.Join(GetRevisionsDir(), metadata.RequestHash)
		tier, instructions, response := loadRevisionTextForTier(revisionPath)

		changes = append(changes, ChangeLog{
			RequestHash:      metadata.RequestHash,
			Instructions:     instructions,
			Response:         response,
			FileRevisionHash: metadata.FileRevisionHash,
			Filename:         metadata.Filename,
			OriginalCode:     origSentinel,
			NewCode:          newSentinel,
			Description:      metadata.Description,
			Note:             sql.NullString{String: metadata.Note, Valid: metadata.Note != ""},
			Status:           metadata.Status,
			Timestamp:        metadata.Timestamp,
			OriginalPrompt:   metadata.OriginalPrompt,
			LLMMessage:       metadata.LLMMessage,
			AgentModel:       metadata.AgentModel,
			HasConversation:  fileExists(filepath.Join(revisionPath, "conversation.json")),
			Tier:             tier,
		})
	}

	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Timestamp.After(changes[j].Timestamp)
	})

	return changes, nil
}

// GetChangesSince returns changes whose timestamp is strictly after the provided time.
func GetChangesSince(since time.Time) ([]ChangeLog, error) {
	changes, err := fetchAllChanges()
	if err != nil {
		return nil, fmt.Errorf("get session file path: %w", err)
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
		return nil, fmt.Errorf("get session file path: %w", err)
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

// fileExists checks if a file exists without following symlinks
// loadRevisionTextForTier inspects a revision directory and returns
// (tier, instructions, response) based on what compaction has done.
//
//   - hot: all files present (conversation.json + instructions + response)
//   - warm: conversation.json missing, the other two present
//   - "": revision dir exists but has neither — treat as missing
func loadRevisionTextForTier(revisionPath string) (tier, instructions, response string) {
	instructionsBytes, err := filesystem.ReadFileBytes(filepath.Join(revisionPath, "instructions.txt"))
	if err != nil {
		return "", "", ""
	}
	instructions = string(instructionsBytes)
	if responseBytes, err := filesystem.ReadFileBytes(filepath.Join(revisionPath, "llm_response.txt")); err == nil {
		response = string(responseBytes)
	}
	if fileExists(filepath.Join(revisionPath, "conversation.json")) {
		return "hot", instructions, response
	}
	return "warm", instructions, response
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// resolvePaths returns the changes and revisions directory to use.
// If workspace is non-empty, it constructs absolute paths under that workspace.
// Otherwise it falls back to the global GetChangesDir()/GetRevisionsDir().
func resolvePaths(workspace string) (changesDir, revisionsDir string) {
	if workspace != "" {
		return filepath.Join(workspace, ".sprout", "changes"),
			filepath.Join(workspace, ".sprout", "revisions")
	}
	return GetChangesDir(), GetRevisionsDir()
}

// ClearOlderThan removes all change entries and revision directories where the
// change timestamp is strictly before 'since'.
// If workspace is non-empty, it operates on that workspace's .sprout directory.
// If workspace is empty, it uses the globally configured paths.
// Returns the number of changes cleared, revisions cleared, and any error.
func ClearOlderThan(workspace string, since time.Time) (changesCleared int, revisionsCleared int, err error) {
	changesDir, revisionsDir := resolvePaths(workspace)

	// Read all change directories
	changeEntries, err := os.ReadDir(changesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil // No changes directory, nothing to clear
		}
		return 0, 0, fmt.Errorf("failed to read changes directory: %w", err)
	}

	// Track which revision IDs are still referenced by remaining changes
	remainingRevisions := make(map[string]bool)

	for _, entry := range changeEntries {
		if !entry.IsDir() {
			continue
		}

		changeDir := filepath.Join(changesDir, entry.Name())
		metadataPath := filepath.Join(changeDir, metadataFile)

		metadataBytes, err := filesystem.ReadFileBytes(metadataPath)
		if err != nil {
			continue // Skip invalid change directories
		}

		var metadata ChangeMetadata
		if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
			continue // Skip unparseable metadata
		}

		if metadata.Timestamp.Before(since) {
			// Delete this change directory
			if err := os.RemoveAll(changeDir); err != nil {
				return changesCleared, revisionsCleared, fmt.Errorf("failed to remove change dir %s: %w", entry.Name(), err)
			}
			changesCleared++
		} else {
			// Keep track of revisions still in use
			remainingRevisions[metadata.RequestHash] = true
		}
	}

	// Now clean up orphaned revision directories (no remaining changes point to them)
	revisionEntries, err := os.ReadDir(revisionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return changesCleared, 0, nil // No revisions directory, done
		}
		return changesCleared, 0, fmt.Errorf("failed to read revisions directory: %w", err)
	}

	for _, entry := range revisionEntries {
		if !entry.IsDir() {
			continue
		}
		if !remainingRevisions[entry.Name()] {
			revisionPath := filepath.Join(revisionsDir, entry.Name())
			if err := os.RemoveAll(revisionPath); err != nil {
				return changesCleared, revisionsCleared, fmt.Errorf("failed to remove revision dir %s: %w", entry.Name(), err)
			}
			revisionsCleared++
		}
	}

	return changesCleared, revisionsCleared, nil
}

// ClearAll removes all change entries and all revision directories.
// If workspace is non-empty, it operates on that workspace's .sprout directory.
// If workspace is empty, it uses the globally configured paths.
// Returns the number of changes cleared, revisions cleared, and any error.
func ClearAll(workspace string) (changesCleared int, revisionsCleared int, err error) {
	changesDir, revisionsDir := resolvePaths(workspace)

	// Clear all change directories
	changeEntries, err := os.ReadDir(changesDir)
	if err != nil {
		if os.IsNotExist(err) {
			// No changes directory, try clearing revisions only
		} else {
			return 0, 0, fmt.Errorf("failed to read changes directory: %w", err)
		}
	}

	for _, entry := range changeEntries {
		if !entry.IsDir() {
			continue
		}
		changePath := filepath.Join(changesDir, entry.Name())
		if err := os.RemoveAll(changePath); err != nil {
			return changesCleared, revisionsCleared, fmt.Errorf("failed to remove change dir %s: %w", entry.Name(), err)
		}
		changesCleared++
	}

	// Clear all revision directories
	revisionEntries, err := os.ReadDir(revisionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return changesCleared, 0, nil
		}
		return changesCleared, 0, fmt.Errorf("failed to read revisions directory: %w", err)
	}

	for _, entry := range revisionEntries {
		if !entry.IsDir() {
			continue
		}
		revisionPath := filepath.Join(revisionsDir, entry.Name())
		if err := os.RemoveAll(revisionPath); err != nil {
			return changesCleared, revisionsCleared, fmt.Errorf("failed to remove revision dir %s: %w", entry.Name(), err)
		}
		revisionsCleared++
	}

	return changesCleared, revisionsCleared, nil
}

// IsChangeOlderThan reads a change's metadata.json and returns true if the
// change's timestamp is strictly before 'since'. Returns false if the file
// cannot be read or parsed.
func IsChangeOlderThan(metadataPath string, since time.Time) bool {
	metadataBytes, err := os.ReadFile(metadataPath)
	if err != nil {
		return false
	}
	var metadata ChangeMetadata
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		return false
	}
	return metadata.Timestamp.Before(since)
}
