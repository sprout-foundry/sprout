package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// migrationOnce ensures the one-time memory migration runs at most once per process.
var migrationOnce sync.Once

// EmbedMemory embeds a memory file's content and stores it in the
// ConversationStore as a VectorRecord with Type "memory".
// This is called after SaveMemory() to keep the vector index in sync.
//
// Graceful failure: Errors are logged but not returned as fatal.
// Memory files are always saved to disk regardless of embedding success.
func EmbedMemory(ctx context.Context, mgr *embedding.EmbeddingManager, name string, content string) error {
	if mgr == nil {
		debugLogf("[memory-embedding] skipping embedding: embedding manager is nil")
		return nil
	}
	if name == "" {
		debugLogf("[memory-embedding] skipping embedding: empty name")
		return nil
	}
	if content == "" {
		debugLogf("[memory-embedding] skipping embedding: empty content for memory '%s'", name)
		return nil
	}

	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		packageLogErrorf("[memory-embedding] failed to get conversation store: %v", err)
		return nil
	}

	if err := store.StoreMemory(ctx, name, content); err != nil {
		packageLogErrorf("[memory-embedding] failed to store memory embedding for '%s': %v", name, err)
		return fmt.Errorf("failed to store memory embedding: %w", err)
	}

	debugLogf("[memory-embedding] successfully embedded memory '%s'", name)
	return nil
}

// DeleteMemoryEmbedding removes a memory's embedding from the ConversationStore.
// This is called after DeleteMemory() to keep the vector index in sync.
//
// Graceful failure: Errors are logged but not returned as fatal.
// Memory files are always deleted from disk regardless of embedding cleanup.
func DeleteMemoryEmbedding(mgr *embedding.EmbeddingManager, name string) error {
	if mgr == nil {
		debugLogf("[memory-embedding] skipping delete: embedding manager is nil")
		return nil
	}
	if name == "" {
		debugLogf("[memory-embedding] skipping delete: empty name")
		return nil
	}

	ctx := context.Background()
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		packageLogErrorf("[memory-embedding] failed to get conversation store for delete: %v", err)
		return nil
	}

	if err := store.DeleteMemoryByName(name); err != nil {
		packageLogErrorf("[memory-embedding] failed to delete memory embedding for '%s': %v", name, err)
		return fmt.Errorf("failed to delete memory embedding: %w", err)
	}

	debugLogf("[memory-embedding] successfully deleted memory embedding for '%s'", name)
	return nil
}

// MigrateMemories performs a one-time migration of all existing memory files
// to the ConversationStore. It uses sync.Once to ensure it only runs once
// per process lifetime, even if called multiple times.
//
// Migration skips files that are already embedded (by checking if a record
// with ID "memory:<name>" exists in the store).
//
// The manager's closeChan is also selected alongside ctx.Done() so a
// DisableEmbeddingIndex call that arrives mid-migration aborts the loop
// promptly instead of continuing to call provider.Embed / store.Store on
// a torn-down manager.
func MigrateMemories(ctx context.Context, mgr *embedding.EmbeddingManager) {
	migrationOnce.Do(func() {
		if mgr == nil {
			return
		}

		closeCh := mgr.CloseNotify()

		memories, err := LoadAllMemories()
		if err != nil {
			packageLogErrorf("[memory-embedding] migration: failed to list memories: %v", err)
			return
		}

		if len(memories) == 0 {
			debugLogf("[memory-embedding] migration: no existing memories to migrate")
			return
		}

		// Abort before opening the store if the manager has already been
		// closed (e.g. DisableEmbeddingIndex raced with EnableEmbeddingIndex).
		select {
		case <-closeCh:
			debugLogf("[memory-embedding] migration: manager closed before start")
			return
		default:
		}

		store, err := mgr.GetConversationStore(ctx)
		if err != nil {
			packageLogErrorf("[memory-embedding] migration: failed to get conversation store: %v", err)
			return
		}

		// Load existing records to skip already-migrated memories
		existing, err := store.LoadAll()
		if err != nil {
			packageLogErrorf("[memory-embedding] migration: failed to load existing records: %v", err)
			return
		}

		existingIDs := make(map[string]bool)
		for _, r := range existing {
			if r.Type == "memory" {
				existingIDs[r.ID] = true
			}
		}

		migrated := 0
		for _, mem := range memories {
			// Check for cancellation before each memory migration.
			// closeCh closes when the manager is closed via Disable; ctx.Done
			// fires when the agent's interrupt ctx is cancelled. Either is a
			// clean stop signal.
			select {
			case <-ctx.Done():
				debugLogf("[memory-embedding] migration: cancelled, stopping after %d memories", migrated)
				return
			case <-closeCh:
				debugLogf("[memory-embedding] migration: manager closed, stopping after %d memories", migrated)
				return
			default:
			}

			recordID := "memory:" + mem.Name
			if existingIDs[recordID] {
				continue // already migrated
			}

			if mem.Content == "" {
				continue
			}

			if err := store.StoreMemory(ctx, mem.Name, mem.Content); err != nil {
				packageLogErrorf("[memory-embedding] migration: failed to embed '%s': %v", mem.Name, err)
				continue
			}
			migrated++
		}

		if migrated > 0 {
			debugLogf("[memory-embedding] migration: embedded %d/%d memories", migrated, len(memories))
		} else {
			debugLogf("[memory-embedding] migration: all %d memories already embedded", migrated)
		}
	})
}

// ResetMigrationForTesting resets the one-time migration guard for testing purposes.
func ResetMigrationForTesting() {
	migrationOnce = sync.Once{}
}
