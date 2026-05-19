package agent

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// migrationMarkerName is the filename used to record that the one-time memory
// migration has already been performed. The marker lives in the same directory
// as the conversation store so that per-user migration state is co-located
// with the embedding data.
const migrationMarkerName = ".memory_migration_done"

// migrationMarkerPath returns the full path to the migration marker file for
// the given embedding manager. It derives the path from the index directory
// (same directory as conversation_turns.jsonl).
//
// Returns empty string if the index directory cannot be determined.
func migrationMarkerPath(indexDir string) string {
	return filepath.Join(indexDir, migrationMarkerName)
}

// hasMigratedMemories returns true if the migration marker file exists,
// indicating that existing memories have already been embedded into the
// conversation store.
func hasMigratedMemories(indexDir string) bool {
	_, err := os.Stat(migrationMarkerPath(indexDir))
	return err == nil
}

// writeMigrationMarker creates the migration marker file, signaling that
// the one-time memory migration has completed. Uses a temp-file + rename
// pattern for atomicity.
func writeMigrationMarker(indexDir string) error {
	markerPath := migrationMarkerPath(indexDir)
	tmpPath := markerPath + ".tmp"

	if err := os.WriteFile(tmpPath, nil, 0600); err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	return os.Rename(tmpPath, markerPath)
}

// RunMemoryMigration performs a one-time migration of existing memory files
// into the conversation store. It scans ~/.config/sprout/memories/*.md,
// embeds each into the conversation store, and writes a marker file so the
// migration never runs again.
//
// The migration is designed to be idempotent and graceful:
//   - If the marker exists, it returns immediately (no-op)
//   - If the embedding manager or provider is unavailable, it logs but returns nil
//   - If individual memory embeddings fail, they are logged but don't block the rest
//   - If no memories exist, it writes the marker to prevent future re-attempts
//
// Memory names are sanitized before embedding so they match the IDs used by
// SaveMemoryWithEmbedding — preventing phantom duplicate records.
//
// This is called during agent startup so that memories are searchable before
// any search_memories tool call. If search_memories is implemented later,
// it can call this function again — the marker check ensures no duplicate work.
func RunMemoryMigration(ctx context.Context, mgr *embedding.EmbeddingManager) error {
	if mgr == nil {
		log.Printf("[memory-migration] skipping: embedding manager is nil")
		return nil
	}

	// Get the conversation store (this also initializes the EmbeddingManager
	// so we can read its resolved index directory).
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		log.Printf("[memory-migration] skipping: failed to get conversation store: %v", err)
		return nil
	}

	// Get the index directory from the EmbeddingManager itself.
	// This uses the same resolution as GetConversationStore, avoiding
	// config-divergence bugs with independent path resolution.
	indexDir := mgr.GetIndexDir()
	if indexDir == "" {
		log.Printf("[memory-migration] skipping: embedding manager has no resolved index directory")
		return nil
	}

	// Check if migration already completed
	if hasMigratedMemories(indexDir) {
		log.Printf("[memory-migration] migration marker found; skipping (already migrated)")
		return nil
	}

	// Load all existing memories
	memories, err := LoadAllMemories()
	if err != nil {
		log.Printf("[memory-migration] failed to load memories: %v", err)
		// Do NOT write marker — allow retry on next startup in case the
		// directory was temporarily inaccessible.
		return nil
	}

	if len(memories) == 0 {
		log.Printf("[memory-migration] no existing memories found; writing marker")
		if err := writeMigrationMarker(indexDir); err != nil {
			log.Printf("[memory-migration] failed to write marker: %v", err)
		}
		return nil
	}

	log.Printf("[memory-migration] migrating %d memory file(s) to conversation store", len(memories))

	migrated := 0
	failed := 0
	failedNames := make([]string, 0)
	for _, mem := range memories {
		// Sanitize the name so the store ID matches what SaveMemoryWithEmbedding
		// would produce. Without this, old files with non-standard names create
		// phantom records that can't be cleaned up by DeleteMemoryWithEmbedding.
		name := sanitizeMemoryName(mem.Name)
		content := mem.Content

		if err := store.StoreMemory(ctx, name, content); err != nil {
			if ctx.Err() != nil {
				log.Printf("[memory-migration] cancelled during migration of '%s': %v", name, ctx.Err())
				// Write marker even on cancellation to avoid repeated re-embedding.
				// StoreMemory is idempotent by ID, so re-embeds are harmless.
				_ = writeMigrationMarker(indexDir)
				return nil
			}
			log.Printf("[memory-migration] failed to embed memory '%s': %v", name, err)
			failed++
			failedNames = append(failedNames, name)
			continue
		}
		migrated++
	}

	// Write the marker regardless of partial failures — on next startup,
	// any already-embedded memories are just re-written (idempotent by ID),
	// and we don't want to keep trying forever on partial failures.
	if err := writeMigrationMarker(indexDir); err != nil {
		log.Printf("[memory-migration] embedded %d/%d memories but failed to write marker: %v", migrated, len(memories), err)
		return nil
	}

	log.Printf("[memory-migration] complete: migrated %d, failed %d, total %d", migrated, failed, len(memories))
	if failed > 0 {
		log.Printf("[memory-migration] failed memories: %v", failedNames)
	}
	return nil
}
