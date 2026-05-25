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

	// Best-effort: also embed into the parallel ONNX conversation store so
	// memory search can fuse static + ONNX rankings via RRF. Failure here is
	// never propagated — the static record above is the source of truth.
	if onnxStore, oerr := mgr.GetONNXConversationStore(ctx); oerr == nil && onnxStore != nil {
		if err := onnxStore.StoreMemory(ctx, name, content); err != nil {
			debugLogf("[memory-embedding] onnx store failed for '%s' (static record kept): %v", name, err)
		}
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

	// Mirror deletion in the ONNX store so search results stay consistent.
	// Missing records (e.g. memory predated ONNX availability) are a no-op.
	if onnxStore, oerr := mgr.GetONNXConversationStore(ctx); oerr == nil && onnxStore != nil {
		if err := onnxStore.DeleteMemoryByName(name); err != nil {
			debugLogf("[memory-embedding] onnx delete failed for '%s': %v", name, err)
		}
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
func MigrateMemories(ctx context.Context, mgr *embedding.EmbeddingManager) {
	migrationOnce.Do(func() {
		if mgr == nil {
			return
		}

		memories, err := LoadAllMemories()
		if err != nil {
			packageLogErrorf("[memory-embedding] migration: failed to list memories: %v", err)
			return
		}

		if len(memories) == 0 {
			debugLogf("[memory-embedding] migration: no existing memories to migrate")
			return
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

		// Snapshot the ONNX store once so we don't pay the lookup-per-iteration
		// cost. nil means ONNX isn't available; we silently skip dual-write.
		onnxStore, oerr := mgr.GetONNXConversationStore(ctx)
		if oerr != nil {
			debugLogf("[memory-embedding] migration: onnx store unavailable: %v", oerr)
			onnxStore = nil
		}

		migrated := 0
		for _, mem := range memories {
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
			if onnxStore != nil {
				if err := onnxStore.StoreMemory(ctx, mem.Name, mem.Content); err != nil {
					debugLogf("[memory-embedding] migration: onnx embed failed for '%s': %v", mem.Name, err)
				}
			}
			migrated++
		}

		if migrated > 0 {
			debugLogf("[memory-embedding] migration: embedded %d/%d memories", migrated, len(memories))
		} else {
			debugLogf("[memory-embedding] migration: all %d memories already embedded", len(memories))
		}

		// After the disk-to-static migration, backfill any memories the static
		// store has but the ONNX store doesn't. This catches the case where a
		// previous session migrated memories before ONNX dual-write existed.
		if filled := BackfillMemoryONNX(ctx, mgr); filled > 0 {
			debugLogf("[memory-embedding] migration: backfilled %d memories into ONNX store", filled)
		}
	})
}

// ResetMigrationForTesting resets the one-time migration guard for testing purposes.
func ResetMigrationForTesting() {
	migrationOnce = sync.Once{}
}

// BackfillMemoryONNX brings the ONNX conversation store in sync with the
// static one by re-embedding any memories that exist in the static store
// but are missing from the ONNX store. This handles the common case of
// memories that were written before ONNX support was available (or before
// ONNX had finished lazy-initialization on a given session).
//
// Returns the number of memories newly embedded into the ONNX store.
// The function is a no-op (returns 0) when ONNX isn't available — that
// case is normal, not an error, and never gets propagated upward.
//
// Embeddings cannot be ported across providers: the static and ONNX vectors
// live in different spaces. The original memory CONTENT is what we re-embed,
// resolved from disk via LoadAllMemories so we don't have to store the full
// body inside the static store's record metadata.
func BackfillMemoryONNX(ctx context.Context, mgr *embedding.EmbeddingManager) int {
	if mgr == nil {
		return 0
	}

	staticStore, err := mgr.GetConversationStore(ctx)
	if err != nil {
		debugLogf("[memory-backfill] static store unavailable: %v", err)
		return 0
	}
	onnxStore, oerr := mgr.GetONNXConversationStore(ctx)
	if oerr != nil || onnxStore == nil {
		// ONNX not ready: nothing to backfill into. The MigrateMemories
		// sync.Once is per-process, so the next process invocation gets
		// another chance once ONNX has had time to initialize.
		return 0
	}

	staticRecords, err := staticStore.LoadAll()
	if err != nil {
		packageLogErrorf("[memory-backfill] failed to load static records: %v", err)
		return 0
	}
	onnxRecords, err := onnxStore.LoadAll()
	if err != nil {
		packageLogErrorf("[memory-backfill] failed to load onnx records: %v", err)
		return 0
	}

	onnxMemoryIDs := make(map[string]struct{}, len(onnxRecords))
	for _, r := range onnxRecords {
		if r.Type == "memory" {
			onnxMemoryIDs[r.ID] = struct{}{}
		}
	}

	// Resolve original content by name. We read disk once rather than
	// per-record because the memories directory is usually small.
	diskMemories, err := LoadAllMemories()
	if err != nil {
		packageLogErrorf("[memory-backfill] failed to list memories on disk: %v", err)
		return 0
	}
	contentByName := make(map[string]string, len(diskMemories))
	for _, m := range diskMemories {
		contentByName[m.Name] = m.Content
	}

	backfilled := 0
	for _, r := range staticRecords {
		if r.Type != "memory" {
			continue
		}
		if _, present := onnxMemoryIDs[r.ID]; present {
			continue
		}
		content, ok := contentByName[r.Name]
		if !ok || content == "" {
			// Static store has the embedding but the underlying file is gone.
			// Don't fabricate content — leave the ONNX index out of sync
			// rather than guess.
			debugLogf("[memory-backfill] '%s' missing from disk, skipping", r.Name)
			continue
		}
		if err := onnxStore.StoreMemory(ctx, r.Name, content); err != nil {
			debugLogf("[memory-backfill] failed for '%s': %v", r.Name, err)
			continue
		}
		backfilled++
	}

	if backfilled > 0 {
		debugLogf("[memory-backfill] embedded %d memories into ONNX store", backfilled)
	}
	return backfilled
}
