package agent

import (
	"context"
	"fmt"
	"log"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// EmbedMemory embeds a memory file's content and stores it in the
// ConversationStore as a VectorRecord with Type "memory".
// This is called after SaveMemory() to keep the vector index in sync.
//
// Graceful failure: Errors are logged but not returned as fatal.
// Memory files are always saved to disk regardless of embedding success.
func EmbedMemory(ctx context.Context, mgr *embedding.EmbeddingManager, name string, content string) error {
	if mgr == nil {
		log.Printf("[memory-embedding] skipping embedding: embedding manager is nil")
		return nil
	}
	if name == "" {
		log.Printf("[memory-embedding] skipping embedding: empty name")
		return nil
	}
	if content == "" {
		log.Printf("[memory-embedding] skipping embedding: empty content for memory '%s'", name)
		return nil
	}

	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		log.Printf("[memory-embedding] failed to get conversation store: %v", err)
		return nil
	}

	if err := store.StoreMemory(ctx, name, content); err != nil {
		log.Printf("[memory-embedding] failed to store memory embedding for '%s': %v", name, err)
		return fmt.Errorf("failed to store memory embedding: %w", err)
	}

	log.Printf("[memory-embedding] successfully embedded memory '%s'", name)
	return nil
}

// DeleteMemoryEmbedding removes a memory's embedding from the ConversationStore.
// This is called after DeleteMemory() to keep the vector index in sync.
//
// Graceful failure: Errors are logged but not returned as fatal.
// Memory files are always deleted from disk regardless of embedding cleanup.
func DeleteMemoryEmbedding(mgr *embedding.EmbeddingManager, name string) error {
	if mgr == nil {
		log.Printf("[memory-embedding] skipping delete: embedding manager is nil")
		return nil
	}
	if name == "" {
		log.Printf("[memory-embedding] skipping delete: empty name")
		return nil
	}

	ctx := context.Background()
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		log.Printf("[memory-embedding] failed to get conversation store for delete: %v", err)
		return nil
	}

	if err := store.DeleteMemoryByName(name); err != nil {
		log.Printf("[memory-embedding] failed to delete memory embedding for '%s': %v", name, err)
		return fmt.Errorf("failed to delete memory embedding: %w", err)
	}

	log.Printf("[memory-embedding] successfully deleted memory embedding for '%s'", name)
	return nil
}
