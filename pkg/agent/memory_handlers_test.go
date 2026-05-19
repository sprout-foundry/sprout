package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// setupMemoryHandlers creates an isolated memory directory for testing
// by setting SPROUT_CONFIG and LEDIT_CONFIG to a temp dir.
func setupMemoryHandlers(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()

	// Set config dir to our temp dir
	t.Setenv("SPROUT_CONFIG", tmp)
	t.Setenv("LEDIT_CONFIG", tmp)

	// Create the memories subdirectory
	memDir := filepath.Join(tmp, "memories")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatalf("failed to create temp memory dir: %v", err)
	}
	return memDir
}

// --- handleAddMemory ---

func TestHandleAddMemoryMissingArgs(t *testing.T) {
	t.Parallel()

	// Missing "name"
	_, err := handleAddMemory(nil, nil, map[string]interface{}{
		"content": "some content",
	})
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("wrong error: %v", err)
	}

	// Missing "content"
	_, err = handleAddMemory(nil, nil, map[string]interface{}{
		"name": "test-memory",
	})
	if err == nil {
		t.Fatal("expected error for missing content")
	}
	if !strings.Contains(err.Error(), "content is required") {
		t.Errorf("wrong error: %v", err)
	}

	// Neither arg
	_, err = handleAddMemory(nil, nil, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for missing args")
	}
}

func TestHandleAddMemorySuccess(t *testing.T) {
	// Not parallel — uses env vars
	memDir := setupMemoryHandlers(t)

	result, err := handleAddMemory(nil, nil, map[string]interface{}{
		"name":    "test-mem",
		"content": "# Test Memory\n\nSome content here.",
	})
	if err != nil {
		t.Fatalf("handleAddMemory failed: %v", err)
	}

	// Verify file was written
	memFile := filepath.Join(memDir, "test-mem.md")
	data, err := os.ReadFile(memFile)
	if err != nil {
		t.Fatalf("memory file not found at %s: %v", memFile, err)
	}
	if string(data) != "# Test Memory\n\nSome content here." {
		t.Errorf("file content = %q; want original content", string(data))
	}

	// Verify result message mentions the memory name
	if !strings.Contains(result, "test-mem") {
		t.Errorf("result should mention memory name: %s", result)
	}
}

func TestHandleAddMemorySanitized(t *testing.T) {
	// Not parallel — uses env vars
	memDir := setupMemoryHandlers(t)

	result, err := handleAddMemory(nil, nil, map[string]interface{}{
		"name":    "My Memory! @#$%&*()",
		"content": "content",
	})
	if err != nil {
		t.Fatalf("handleAddMemory failed: %v", err)
	}

	// Name should be sanitized to "my-memory"
	memFile := filepath.Join(memDir, "my-memory.md")
	if _, err := os.Stat(memFile); os.IsNotExist(err) {
		t.Errorf("sanitized memory file not found at %s", memFile)
	}

	if !strings.Contains(result, "my-memory") {
		t.Errorf("result should mention sanitized name 'my-memory': %s", result)
	}
}

// --- handleReadMemory ---

func TestHandleReadMemoryMissingArgs(t *testing.T) {
	t.Parallel()

	_, err := handleReadMemory(nil, nil, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("wrong error: %v", err)
	}
}

func TestHandleReadMemoryNotFound(t *testing.T) {
	// Not parallel — uses env vars
	setupMemoryHandlers(t)

	_, err := handleReadMemory(nil, nil, map[string]interface{}{
		"name": "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent memory")
	}
}

func TestHandleReadMemorySuccess(t *testing.T) {
	// Not parallel — uses env vars
	memDir := setupMemoryHandlers(t)

	// Write a memory file first
	content := "# ReadMe\n\nThis is test content."
	memFile := filepath.Join(memDir, "readme.md")
	if err := os.WriteFile(memFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test memory: %v", err)
	}

	result, err := handleReadMemory(nil, nil, map[string]interface{}{
		"name": "readme",
	})
	if err != nil {
		t.Fatalf("handleReadMemory failed: %v", err)
	}

	if !strings.Contains(result, "## Memory: readme") {
		t.Error("result should contain '## Memory: readme'")
	}
	if !strings.Contains(result, "# ReadMe") {
		t.Error("result should contain memory content")
	}
}

// --- handleListMemories ---

func TestHandleListMemoriesEmpty(t *testing.T) {
	// Not parallel — uses env vars
	setupMemoryHandlers(t)

	result, err := handleListMemories(nil, nil, map[string]interface{}{})
	if err != nil {
		t.Fatalf("handleListMemories failed: %v", err)
	}

	if !strings.Contains(result, "No memories saved") {
		t.Errorf("should say 'No memories saved': %s", result)
	}
}

func TestHandleListMemoriesSuccess(t *testing.T) {
	// Not parallel — uses env vars
	memDir := setupMemoryHandlers(t)

	// Write two memory files
	os.WriteFile(filepath.Join(memDir, "alpha.md"), []byte("# Alpha Memory\nDetails..."), 0644)
	os.WriteFile(filepath.Join(memDir, "beta.md"), []byte("# Beta Memory\nOther details..."), 0644)

	result, err := handleListMemories(nil, nil, map[string]interface{}{})
	if err != nil {
		t.Fatalf("handleListMemories failed: %v", err)
	}

	if !strings.Contains(result, "alpha") {
		t.Error("should list alpha memory")
	}
	if !strings.Contains(result, "beta") {
		t.Error("should list beta memory")
	}
	if !strings.Contains(result, "2") {
		t.Error("should show count of 2")
	}
}

func TestHandleListMemoriesTruncation(t *testing.T) {
	// Not parallel — uses env vars
	memDir := setupMemoryHandlers(t)

	// Write a memory with a very long title line
	longTitle := "# " + strings.Repeat("A", 200)
	os.WriteFile(filepath.Join(memDir, "long.md"), []byte(longTitle+"\nBody"), 0644)

	result, err := handleListMemories(nil, nil, map[string]interface{}{})
	if err != nil {
		t.Fatalf("handleListMemories failed: %v", err)
	}

	// Should be truncated (title > 120 chars gets "...")
	if !strings.Contains(result, "...") {
		t.Error("long title should be truncated with ...")
	}
}

// --- handleDeleteMemory ---

func TestHandleDeleteMemoryMissingArgs(t *testing.T) {
	t.Parallel()

	_, err := handleDeleteMemory(nil, nil, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("wrong error: %v", err)
	}
}

func TestHandleDeleteMemoryNotFound(t *testing.T) {
	// Not parallel — uses env vars
	setupMemoryHandlers(t)

	_, err := handleDeleteMemory(nil, nil, map[string]interface{}{
		"name": "no-such-memory",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent memory")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("wrong error: %v", err)
	}
}

func TestHandleDeleteMemorySuccess(t *testing.T) {
	// Not parallel — uses env vars
	memDir := setupMemoryHandlers(t)

	// Write a memory file
	memFile := filepath.Join(memDir, "bye.md")
	if err := os.WriteFile(memFile, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to write test memory: %v", err)
	}

	result, err := handleDeleteMemory(nil, nil, map[string]interface{}{
		"name": "bye",
	})
	if err != nil {
		t.Fatalf("handleDeleteMemory failed: %v", err)
	}

	// Verify file was deleted
	if _, err := os.Stat(memFile); !os.IsNotExist(err) {
		t.Error("memory file should be deleted")
	}

	if !strings.Contains(result, "Memory 'bye' deleted") {
		t.Errorf("result should confirm deletion: %s", result)
	}
}

func TestHandleDeleteMemoryWithSuffix(t *testing.T) {
	// Not parallel — uses env vars
	memDir := setupMemoryHandlers(t)

	memFile := filepath.Join(memDir, "del-me.md")
	if err := os.WriteFile(memFile, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to write test memory: %v", err)
	}

	result, err := handleDeleteMemory(nil, nil, map[string]interface{}{
		"name": "del-me.md", // .md suffix should be stripped
	})
	if err != nil {
		t.Fatalf("handleDeleteMemory with .md suffix failed: %v", err)
	}

	if _, err := os.Stat(memFile); !os.IsNotExist(err) {
		t.Error("memory file should be deleted even when name has .md suffix")
	}

	if !strings.Contains(result, "del-me") {
		t.Errorf("result should mention name without suffix: %s", result)
	}
}

// --- handleSearchMemories ---

func TestHandleSearchMemories_MissingQuery(t *testing.T) {
	t.Parallel()

	_, err := handleSearchMemories(nil, nil, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for missing query")
	}
	if !strings.Contains(err.Error(), "query is required") {
		t.Errorf("wrong error: %v", err)
	}
}

func TestHandleSearchMemories_NotEnabled(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	// No embedding manager set (nil) — handler should return a helpful message.
	result, err := handleSearchMemories(context.Background(), agent, map[string]interface{}{
		"query": "git conventions",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "not available") {
		t.Errorf("expected 'not available' in result, got: %s", result)
	}
	if !strings.Contains(result, "Embedding index is not enabled") {
		t.Errorf("expected embedding index message, got: %s", result)
	}
}

func TestHandleSearchMemories_NoResults(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	dir := t.TempDir()
	em := embedding.NewEmbeddingManager(nil, dir)
	agent.embeddingMgr = em

	// Initialize the embedding manager so GetConversationStore works
	if err := em.Init(context.Background()); err != nil {
		t.Fatalf("failed to init embedding manager: %v", err)
	}

	result, err := handleSearchMemories(context.Background(), agent, map[string]interface{}{
		"query": "nonexistent topic",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "No memories found") {
		t.Errorf("expected no-results message, got: %s", result)
	}
	if !strings.Contains(result, "list_memories") {
		t.Errorf("expected list_memories suggestion, got: %s", result)
	}
}

func TestHandleSearchMemories_WithResults(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	dir := t.TempDir()
	cfg := &configuration.EmbeddingIndexConfig{IndexDir: dir}
	em := embedding.NewEmbeddingManager(cfg, dir)
	agent.embeddingMgr = em

	// Initialize the embedding manager
	if err := em.Init(context.Background()); err != nil {
		t.Fatalf("failed to init embedding manager: %v", err)
	}

	// Get the conversation store and add some memories
	store, err := em.GetConversationStore(context.Background())
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}

	// Add two memories with different content
	ctx := context.Background()
	if err := store.StoreMemory(ctx, "git-conventions", "# Git Conventions\n\nAlways use conventional commits with type(scope): description."); err != nil {
		t.Fatalf("failed to store memory: %v", err)
	}
	if err := store.StoreMemory(ctx, "test-patterns", "# Test Patterns\n\nUse table-driven tests for multiple scenarios."); err != nil {
		t.Fatalf("failed to store memory: %v", err)
	}

	// Search for git-related content
	result, err := handleSearchMemories(context.Background(), agent, map[string]interface{}{
		"query": "git commit format",
	})
	if err != nil {
		t.Fatalf("handleSearchMemories failed: %v", err)
	}

	// Should find results with proper formatting
	if !strings.Contains(result, "Memory Search Results") {
		t.Errorf("expected header in result, got: %s", result)
	}
	if !strings.Contains(result, "relevance:") {
		t.Errorf("expected relevance score in result, got: %s", result)
	}

	// The mock provider returns the same vector for all content, so all memories
	// will have similarity 1.0. Both should appear in results.
	if !strings.Contains(result, "git-conventions") {
		t.Error("expected 'git-conventions' in results")
	}
	if !strings.Contains(result, "Git Conventions") {
		t.Error("expected title 'Git Conventions' in results")
	}
}

func TestHandleSearchMemories_MaxResults(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	dir := t.TempDir()
	cfg := &configuration.EmbeddingIndexConfig{IndexDir: dir}
	em := embedding.NewEmbeddingManager(cfg, dir)
	agent.embeddingMgr = em

	// Initialize the embedding manager
	if err := em.Init(context.Background()); err != nil {
		t.Fatalf("failed to init embedding manager: %v", err)
	}

	// Get the conversation store and add multiple memories
	store, err := em.GetConversationStore(context.Background())
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}

	ctx := context.Background()
	memories := []struct {
		name    string
		content string
	}{
		{"mem-1", "# First Memory\nContent 1"},
		{"mem-2", "# Second Memory\nContent 2"},
		{"mem-3", "# Third Memory\nContent 3"},
	}

	for _, m := range memories {
		if err := store.StoreMemory(ctx, m.name, m.content); err != nil {
			t.Fatalf("failed to store memory %s: %v", m.name, err)
		}
	}

	// Request only 2 results
	result, err := handleSearchMemories(context.Background(), agent, map[string]interface{}{
		"query":       "test",
		"max_results": 2,
	})
	if err != nil {
		t.Fatalf("handleSearchMemories failed: %v", err)
	}

	// Should say "Found 2 result(s)" not 3
	if strings.Contains(result, "Found 3 result") {
		t.Error("max_results should limit to 2, got 3")
	}
	if !strings.Contains(result, "Found 2 result") {
		t.Errorf("expected 'Found 2 result', got: %s", result)
	}
}

func TestHandleSearchMemories_DefaultMaxResults(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	dir := t.TempDir()
	cfg := &configuration.EmbeddingIndexConfig{IndexDir: dir}
	em := embedding.NewEmbeddingManager(cfg, dir)
	agent.embeddingMgr = em

	// Initialize the embedding manager
	if err := em.Init(context.Background()); err != nil {
		t.Fatalf("failed to init embedding manager: %v", err)
	}

	// Get the conversation store and add memories
	store, err := em.GetConversationStore(context.Background())
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}

	ctx := context.Background()
	if err := store.StoreMemory(ctx, "default-test", "# Test\nContent"); err != nil {
		t.Fatalf("failed to store memory: %v", err)
	}

	// Search without max_results — should default to 5
	result, err := handleSearchMemories(context.Background(), agent, map[string]interface{}{
		"query": "test",
	})
	if err != nil {
		t.Fatalf("handleSearchMemories failed: %v", err)
	}

	// Should work without error and find the memory
	if !strings.Contains(result, "default-test") {
		t.Error("expected memory in results")
	}
}

func TestHandleSearchMemories_MaxResultsFloat64(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	dir := t.TempDir()
	cfg := &configuration.EmbeddingIndexConfig{IndexDir: dir}
	em := embedding.NewEmbeddingManager(cfg, dir)
	agent.embeddingMgr = em

	// Initialize the embedding manager
	if err := em.Init(context.Background()); err != nil {
		t.Fatalf("failed to init embedding manager: %v", err)
	}

	// Get the conversation store and add memories
	store, err := em.GetConversationStore(context.Background())
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}

	ctx := context.Background()
	if err := store.StoreMemory(ctx, "float-test", "# Test\nContent"); err != nil {
		t.Fatalf("failed to store memory: %v", err)
	}

	// max_results as float64 (common from JSON) should be accepted
	result, err := handleSearchMemories(context.Background(), agent, map[string]interface{}{
		"query":       "test",
		"max_results": float64(3), // float64
	})
	if err != nil {
		t.Fatalf("handleSearchMemories failed: %v", err)
	}

	// Should work without error
	if !strings.Contains(result, "float-test") {
		t.Error("expected memory in results")
	}
}

func TestHandleSearchMemories_NilAgent(t *testing.T) {
	// Pass nil agent — handler should return a graceful message without panicking.
	result, err := handleSearchMemories(context.Background(), nil, map[string]interface{}{
		"query": "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "not available") {
		t.Errorf("expected 'not available' in result, got: %s", result)
	}
	if !strings.Contains(result, "Embedding index is not enabled") {
		t.Errorf("expected embedding index message, got: %s", result)
	}
}

func TestHandleSearchMemories_MaxResultsClamp(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	dir := t.TempDir()
	cfg := &configuration.EmbeddingIndexConfig{IndexDir: dir}
	em := embedding.NewEmbeddingManager(cfg, dir)
	agent.embeddingMgr = em

	// Initialize the embedding manager
	if err := em.Init(context.Background()); err != nil {
		t.Fatalf("failed to init embedding manager: %v", err)
	}

	// Get the conversation store and add several memories
	store, err := em.GetConversationStore(context.Background())
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}

	ctx := context.Background()
	memories := []struct {
		name    string
		content string
	}{
		{"clamp-1", "# Clamp One\nContent one"},
		{"clamp-2", "# Clamp Two\nContent two"},
		{"clamp-3", "# Clamp Three\nContent three"},
		{"clamp-4", "# Clamp Four\nContent four"},
		{"clamp-5", "# Clamp Five\nContent five"},
	}

	for _, m := range memories {
		if err := store.StoreMemory(ctx, m.name, m.content); err != nil {
			t.Fatalf("failed to store memory %s: %v", m.name, err)
		}
	}

	t.Run("clamp_high_over_100_to_100", func(t *testing.T) {
		// max_results=200 should be clamped to 100; only 5 memories exist so all show
		result, err := handleSearchMemories(context.Background(), agent, map[string]interface{}{
			"query":       "content",
			"max_results": 200,
		})
		if err != nil {
			t.Fatalf("handleSearchMemories failed: %v", err)
		}

		// Should still return results (capped at 100 but only 5 exist)
		if !strings.Contains(result, "Memory Search Results") {
			t.Errorf("expected search results header, got: %s", result)
		}

		// All 5 memories should appear since 5 < 100
		for _, m := range memories {
			if !strings.Contains(result, m.name) {
				t.Errorf("expected %q in results (within 100 cap), got: %s", m.name, result)
			}
		}
	})

	t.Run("clamp_low_under_1_to_1", func(t *testing.T) {
		// max_results=-5 should be clamped to 1
		result, err := handleSearchMemories(context.Background(), agent, map[string]interface{}{
			"query":       "content",
			"max_results": -5,
		})
		if err != nil {
			t.Fatalf("handleSearchMemories failed: %v", err)
		}

		// Should return exactly 1 result
		if !strings.Contains(result, "Found 1 result") {
			t.Errorf("expected exactly 1 result (clamped from -5), got: %s", result)
		}

		// Should not return more than 1 result line
		// The mock provider returns all results with similarity 1.0,
		// so the first memory alphabetically will be returned.
		resultLines := strings.Count(result, "(relevance: ")
		if resultLines != 1 {
			t.Errorf("expected exactly 1 result line, got %d", resultLines)
		}
	})

	t.Run("clamp_zero_to_1", func(t *testing.T) {
		// max_results=0 should be clamped to 1
		result, err := handleSearchMemories(context.Background(), agent, map[string]interface{}{
			"query":       "content",
			"max_results": 0,
		})
		if err != nil {
			t.Fatalf("handleSearchMemories failed: %v", err)
		}

		if !strings.Contains(result, "Found 1 result") {
			t.Errorf("expected exactly 1 result (clamped from 0), got: %s", result)
		}
	})
}

// TestHandleSearchMemories_MixedStore verifies that handleSearchMemories
// correctly filters out non-memory records from the conversation store.
// The store may contain both "memory" records and "conversation_turn" records.
func TestHandleSearchMemories_MixedStore(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	dir := t.TempDir()
	cfg := &configuration.EmbeddingIndexConfig{IndexDir: dir}
	em := embedding.NewEmbeddingManager(cfg, dir)
	agent.embeddingMgr = em

	if err := em.Init(context.Background()); err != nil {
		t.Fatalf("failed to init embedding manager: %v", err)
	}

	store, err := em.GetConversationStore(context.Background())
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}

	ctx := context.Background()

	// Add 2 memory records
	if err := store.StoreMemory(ctx, "mem-alpha", "# Alpha Memory\n\nGit conventions and commit format rules."); err != nil {
		t.Fatalf("failed to store memory: %v", err)
	}
	if err := store.StoreMemory(ctx, "mem-beta", "# Beta Memory\n\nTesting patterns and strategies."); err != nil {
		t.Fatalf("failed to store memory: %v", err)
	}

	// Add 5 non-memory (conversation turn) records directly to the store.
	// These simulate conversation turns that share the same embedding space.
	nonMemoryRecords := make([]embedding.VectorRecord, 5)
	for i := range nonMemoryRecords {
		nonMemoryRecords[i] = embedding.VectorRecord{
			ID:        fmt.Sprintf("turn-%d", i),
			Name:      fmt.Sprintf("turn_%d", i),
			Signature: fmt.Sprintf("Some conversation turn %d about various topics", i),
			Type:      "conversation_turn",
			Embedding: make([]float32, 256), // dummy embedding (will be overwritten by mock provider)
			IndexedAt: time.Now().UTC(),
		}
	}
	// Embed the non-memory records using the same provider so they're comparable
	provider := store.Provider()
	probe, err := provider.Embed(ctx, "mixed store test")
	if err != nil {
		t.Fatalf("failed to embed for non-memory records: %v", err)
	}
	for i := range nonMemoryRecords {
		nonMemoryRecords[i].Embedding = make([]float32, len(probe))
		copy(nonMemoryRecords[i].Embedding, probe)
	}
	if err := store.Store(nonMemoryRecords); err != nil {
		t.Fatalf("failed to store non-memory records: %v", err)
	}

	// Total records: 2 memory + 5 conversation_turn = 7
	if store.Size() != 7 {
		t.Fatalf("expected 7 total records, got %d", store.Size())
	}

	// Search with max_results=1
	result, err := handleSearchMemories(ctx, agent, map[string]interface{}{
		"query":       "testing strategies",
		"max_results": 1,
	})
	if err != nil {
		t.Fatalf("handleSearchMemories failed: %v", err)
	}

	// Must NOT contain any conversation_turn records
	if strings.Contains(result, "turn_") {
		t.Errorf("result should not contain conversation turn records, got: %s", result)
	}
	if strings.Contains(result, "conversation_turn") {
		t.Errorf("result should not contain 'conversation_turn', got: %s", result)
	}

	// Should contain at most 1 memory result
	if !strings.Contains(result, "Found 1 result") {
		t.Errorf("expected exactly 1 result with max_results=1, got: %s", result)
	}

	// Must contain a memory name (either alpha or beta)
	if !strings.Contains(result, "mem-alpha") && !strings.Contains(result, "mem-beta") {
		t.Errorf("expected a memory name in results, got: %s", result)
	}
}
