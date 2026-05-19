package agent

import (
	"context"
	"log"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// SaveMemoryWithEmbedding saves a memory to disk and embeds it into the conversation store.
// The memory file is always saved; embedding failures are logged but not returned,
// so memory save succeeds even if embedding is unavailable.
func SaveMemoryWithEmbedding(ctx context.Context, mgr *embedding.EmbeddingManager, name, content string) error {
	// Save the memory file first (this should always succeed)
	if err := SaveMemory(name, content); err != nil {
		return err
	}

	// If embedding manager is nil, skip embedding gracefully
	if mgr == nil {
		log.Printf("[memory-embedding] skipping embedding: embedding manager is nil")
		return nil
	}

	// Use sanitized name for embedding so the record ID matches the file name
	sanitized := sanitizeMemoryName(name)

	// Embed the memory (graceful failure)
	if err := EmbedMemory(ctx, mgr, sanitized, content); err != nil {
		// EmbedMemory already logs errors and returns nil, but check anyway
		log.Printf("[memory-embedding] warning: embedding failed for memory '%s': %v", sanitized, err)
	}

	return nil
}

// EmbedMemory embeds a memory into the conversation store.
// Graceful failure: errors are logged but not returned. The caller (memory save/delete)
// should always succeed regardless of embedding failures.
func EmbedMemory(ctx context.Context, mgr *embedding.EmbeddingManager, name, content string) error {
	// Validate inputs
	if mgr == nil {
		log.Printf("[memory-embedding] skipping embedding: embedding manager is nil")
		return nil
	}
	if ctx == nil {
		log.Printf("[memory-embedding] skipping embedding: context is nil")
		return nil
	}
	if name == "" {
		log.Printf("[memory-embedding] skipping embedding: memory name is empty")
		return nil
	}
	if content == "" {
		log.Printf("[memory-embedding] skipping embedding: memory content is empty")
		return nil
	}

	// Get the conversation store from the manager
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		log.Printf("[memory-embedding] failed to get conversation store: %v", err)
		return nil
	}

	// Store the memory embedding
	if err := store.StoreMemory(ctx, name, content); err != nil {
		if ctx.Err() != nil {
			log.Printf("[memory-embedding] embedding cancelled for memory '%s': %v", name, ctx.Err())
		} else {
			log.Printf("[memory-embedding] failed to store memory embedding '%s': %v", name, err)
		}
		return nil
	}

	log.Printf("[memory-embedding] successfully embedded memory '%s' in conversation store", name)
	return nil
}

// DeleteMemoryWithEmbedding deletes a memory file and removes it from the conversation store.
// The memory file is always deleted; unembedding failures are logged but not returned.
func DeleteMemoryWithEmbedding(ctx context.Context, mgr *embedding.EmbeddingManager, name string) error {
	// Sanitize the name so both the file path and store record use the same ID
	sanitized := sanitizeMemoryName(name)

	// Delete the memory file first (this should always succeed)
	if err := DeleteMemory(sanitized); err != nil {
		return err
	}

	// If embedding manager is nil, skip unembedding gracefully
	if mgr == nil {
		log.Printf("[memory-embedding] skipping unembedding: embedding manager is nil")
		return nil
	}

	// Remove the embedding (graceful failure)
	if err := UnembedMemory(ctx, mgr, sanitized); err != nil {
		// UnembedMemory already logs errors and returns nil, but check anyway
		log.Printf("[memory-embedding] warning: unembedding failed for memory '%s': %v", sanitized, err)
	}

	return nil
}

// UnembedMemory removes a memory embedding from the conversation store.
// Graceful failure: errors are logged but not returned. The caller (memory delete)
// should always succeed regardless of unembedding failures.
func UnembedMemory(ctx context.Context, mgr *embedding.EmbeddingManager, name string) error {
	// Validate inputs
	if mgr == nil {
		log.Printf("[memory-embedding] skipping unembedding: embedding manager is nil")
		return nil
	}
	if ctx == nil {
		log.Printf("[memory-embedding] skipping unembedding: context is nil")
		return nil
	}
	if name == "" {
		log.Printf("[memory-embedding] skipping unembedding: memory name is empty")
		return nil
	}

	// Get the conversation store from the manager
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		log.Printf("[memory-embedding] failed to get conversation store: %v", err)
		return nil
	}

	// Remove the memory embedding
	if err := store.RemoveMemory(name); err != nil {
		if ctx.Err() != nil {
			log.Printf("[memory-embedding] unembedding cancelled for memory '%s': %v", name, ctx.Err())
		} else {
			log.Printf("[memory-embedding] failed to remove memory embedding '%s': %v", name, err)
		}
		return nil
	}

	log.Printf("[memory-embedding] successfully removed memory '%s' from conversation store", name)
	return nil
}
