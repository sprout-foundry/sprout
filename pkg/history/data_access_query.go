package history

// data_access_query.go — the read / query + cleanup side of the change
// tracking data access layer: fetching all changes / metadata, the since-
// based queries, the revision-tier loaders, path resolution, and the
// ClearOlderThan / ClearAll / IsChangeOlderThan maintenance. Split out of
// data_access.go.

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
	"time"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

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
		// Tier-only probe: the manifest never surfaces instructions or
		// the LLM response, so reading those (potentially large) files
		// per change was pure waste — the dominant cost of manifest
		// loads on long histories.
		tier := loadRevisionTierOnly(revisionPath)
		hasConversation := tier != "" && fileExists(filepath.Join(revisionPath, "conversation.json"))

		changes = append(changes, ChangeLog{
			RequestHash:      metadata.RequestHash,
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
			HasConversation:  hasConversation,
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
//
// loadRevisionTierOnly is the cheap variant for callers that need the
// tier alone: one stat instead of reading instructions.txt and the
// (often hundreds-of-KB) llm_response.txt. Manifest-scale callers —
// GetAllChangesMetadata and every /api/changes/* list the WebUI panel
// issues — must use this; reading full LLM responses per change made
// those endpoints ~0.5s at 4k stored changes.
func loadRevisionTextForTier(revisionPath string) (tier, instructions, response string) {
	if _, err := os.Stat(filepath.Join(revisionPath, "instructions.txt")); err != nil {
		return "", "", ""
	}
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

func loadRevisionTierOnly(revisionPath string) string {
	if _, err := os.Stat(filepath.Join(revisionPath, "instructions.txt")); err != nil {
		return ""
	}
	if fileExists(filepath.Join(revisionPath, "conversation.json")) {
		return "hot"
	}
	return "warm"
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
