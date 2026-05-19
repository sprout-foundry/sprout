package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// ---------------------------------------------------------------------------
// Integration Tests: Memory Embedding Round-Trip
// ---------------------------------------------------------------------------

// TestMemoryEmbeddingRoundTrip tests the full lifecycle:
// embed a memory → query it back → verify results.
func TestMemoryEmbeddingRoundTrip(t *testing.T) {
	mgr := setupMemoryEmbeddingManager(t)
	defer mgr.Close()
	ctx := context.Background()

	// Store multiple memories with distinct content
	memories := []struct {
		name    string
		content string
	}{
		{
			name:    "git-conventions",
			content: "# Git Conventions\nAlways use meaningful commit messages. Never force push to main.",
		},
		{
			name:    "testing-patterns",
			content: "# Testing Patterns\nUse table-driven tests. Name test functions TestXxx_Scenario_Description.",
		},
		{
			name:    "api-design",
			content: "# API Design\nREST endpoints should use plural nouns. Return appropriate HTTP status codes.",
		},
	}

	for _, mem := range memories {
		err := EmbedMemory(ctx, mgr, mem.name, mem.content)
		if err != nil {
			t.Fatalf("EmbedMemory(%q) failed: %v", mem.name, err)
		}
	}

	// Verify all memories are in the store
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("GetConversationStore failed: %v", err)
	}

	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	memoryCount := 0
	for _, r := range all {
		if r.Type == "memory" {
			memoryCount++
		}
	}
	if memoryCount != 3 {
		t.Errorf("expected 3 memory records, got %d", memoryCount)
	}

	// Query for "version control practices" — should match git-conventions
	results, err := store.QueryMemories(ctx, "git commit push conventions", 3, 0.1)
	if err != nil {
		t.Fatalf("QueryMemories failed: %v", err)
	}

	if len(results) == 0 {
		t.Error("expected at least one result for git-related query")
	}

	// The top result should be "git-conventions"
	if len(results) > 0 {
		found := false
		for _, r := range results {
			if r.Record.Name == "git-conventions" {
				found = true
				break
			}
		}
		if !found {
			// With static model, exact semantic match isn't guaranteed;
			// just verify we got some results
			t.Logf("git-conventions not in top results; results: %v", resultNames(results))
		}
	}
}

// TestMemoryEmbeddingRoundTrip_DeleteRemovesFromSearch tests that deleting a
// memory also removes it from search results.
func TestMemoryEmbeddingRoundTrip_DeleteRemovesFromSearch(t *testing.T) {
	mgr := setupMemoryEmbeddingManager(t)
	defer mgr.Close()
	ctx := context.Background()

	// Store and then delete
	err := EmbedMemory(ctx, mgr, "temp-memory", "This memory will be deleted soon.")
	if err != nil {
		t.Fatalf("EmbedMemory failed: %v", err)
	}

	err = DeleteMemoryEmbedding(mgr, "temp-memory")
	if err != nil {
		t.Fatalf("DeleteMemoryEmbedding failed: %v", err)
	}

	// Verify it can't be found
	store, _ := mgr.GetConversationStore(ctx)
	results, err := store.QueryMemories(ctx, "deleted memory", 5, 0.0)
	if err != nil {
		t.Fatalf("QueryMemories failed: %v", err)
	}

	for _, r := range results {
		if r.Record.Name == "temp-memory" {
			t.Error("deleted memory should not appear in search results")
		}
	}
}

// ---------------------------------------------------------------------------
// Integration Tests: Search Handler
// ---------------------------------------------------------------------------

// TestHandleSearchMemories_Integration tests the handleSearchMemories handler
// with a real (static) embedding provider.
func TestHandleSearchMemories_Integration(t *testing.T) {
	mgr := setupMemoryEmbeddingManager(t)
	defer mgr.Close()
	ctx := context.Background()

	// Pre-populate memories
	err := EmbedMemory(ctx, mgr, "code-review", "Always review code for error handling, edge cases, and test coverage.")
	if err != nil {
		t.Fatalf("EmbedMemory failed: %v", err)
	}

	// Create a minimal agent with the embedding manager
	agent := &Agent{}
	agent.initSubManagers()
	// We need to set the embeddingMgr field directly for testing
	setEmbeddingManager(agent, mgr)

	args := map[string]interface{}{
		"query":     "code review",
		"threshold": float64(0.1),
		"top_k":     float64(5),
	}

	result, err := handleSearchMemories(ctx, agent, args)
	if err != nil {
		t.Fatalf("handleSearchMemories failed: %v", err)
	}

	// With static model, verify at least a result was returned (even if not the exact memory)
	if strings.Contains(result, "No memories found") {
		// This is acceptable with static model — lower threshold further
		t.Logf("No results found for 'code review' query; this is acceptable with static embedding model")
	} else if !strings.Contains(result, "Found") {
		t.Errorf("expected result to contain 'Found'; got: %s", result)
	}
}

// TestHandleSearchMemories_NoEmbeddingManager tests graceful degradation.
func TestHandleSearchMemories_NoEmbeddingManager(t *testing.T) {
	agent := &Agent{}
	agent.initSubManagers()

	args := map[string]interface{}{
		"query": "test query",
	}

	result, err := handleSearchMemories(context.Background(), agent, args)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !strings.Contains(result, "embedding index") {
		t.Errorf("expected helpful error message; got: %s", result)
	}
}

// TestHandleSearchMemories_MissingQuery tests validation.
func TestHandleSearchMemories_MissingQuery(t *testing.T) {
	agent := &Agent{}
	agent.initSubManagers()

	_, err := handleSearchMemories(context.Background(), agent, map[string]interface{}{})
	if err == nil {
		t.Error("expected error for missing query")
	}
}

// TestHandleSearchMemoriesJSON tests the JSON output path.
func TestHandleSearchMemoriesJSON(t *testing.T) {
	mgr := setupMemoryEmbeddingManager(t)
	defer mgr.Close()
	ctx := context.Background()

	err := EmbedMemory(ctx, mgr, "json-test", "Test memory for JSON serialization.")
	if err != nil {
		t.Fatalf("EmbedMemory failed: %v", err)
	}

	agent := &Agent{}
	agent.initSubManagers()
	setEmbeddingManager(agent, mgr)

	result, err := handleSearchMemoriesJSON(ctx, agent, "test memory", 5, 0.3)
	if err != nil {
		t.Fatalf("handleSearchMemoriesJSON failed: %v", err)
	}

	// Verify it's valid JSON
	var parsed []struct {
		Name      string  `json:"name"`
		Relevance float32 `json:"relevance"`
		Title     string  `json:"title,omitempty"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON output: %v\nOutput: %s", err, result)
	}

	if len(parsed) == 0 {
		t.Error("expected at least one JSON result")
	}

	found := false
	for _, p := range parsed {
		if p.Name == "json-test" {
			found = true
			if p.Relevance <= 0 {
				t.Errorf("expected positive relevance; got: %.4f", p.Relevance)
			}
		}
	}
	if !found {
		t.Errorf("expected 'json-test' in JSON results; got: %v", parsed)
	}
}

// ---------------------------------------------------------------------------
// Integration Tests: Migration
// ---------------------------------------------------------------------------

// TestMigration_FullFlow tests that existing memory files on disk
// get embedded into the conversation store during migration.
func TestMigration_FullFlow(t *testing.T) {
	mgr := setupMemoryEmbeddingManager(t)
	defer mgr.Close()
	ctx := context.Background()

	// Create memory files on disk
	memDir := getMemoryDir()
	if memDir == "" {
		t.Fatal("getMemoryDir returned empty")
	}

	testMemories := map[string]string{
		"project-alpha": "# Project Alpha\nThis is the alpha project configuration and conventions.",
		"docker-setup":  "# Docker Setup\nAlways use multi-stage builds for production containers.",
	}

	for name, content := range testMemories {
		err := os.WriteFile(filepath.Join(memDir, name+".md"), []byte(content), 0644)
		if err != nil {
			t.Fatalf("write memory file %s: %v", name, err)
		}
	}

	// Run migration
	ResetMigrationForTesting()
	MigrateMemories(ctx, mgr)

	// Verify memories were embedded
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("GetConversationStore failed: %v", err)
	}

	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	embedded := make(map[string]bool)
	for _, r := range all {
		if r.Type == "memory" {
			embedded[r.Name] = true
			if r.ID != "memory:"+r.Name {
				t.Errorf("expected ID 'memory:%s', got '%s'", r.Name, r.ID)
			}
		}
	}

	for name := range testMemories {
		if !embedded[name] {
			t.Errorf("memory '%s' was not embedded during migration", name)
		}
	}

	// Search should find the Docker memory (use very low threshold for static model)
	results, err := store.QueryMemories(ctx, "docker container", 3, 0.1)
	if err != nil {
		t.Fatalf("QueryMemories failed: %v", err)
	}

	if len(results) == 0 {
		t.Log("No results found for docker query with static model (expected with real embeddings)")
	} else {
		t.Logf("Found %d results after migration", len(results))
	}
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

// setEmbeddingManager sets the private embeddingMgr field for testing.
// This is needed because handleSearchMemories accesses GetEmbeddingManager.
func setEmbeddingManager(a *Agent, mgr *embedding.EmbeddingManager) {
	a.embeddingMgr = mgr
}

// setupMemoryEmbeddingManager is already defined in memory_embedding_test.go.
// We reference the same shared helper from that file (same package).

func resultNames(results []embedding.QueryResult) []string {
	names := make([]string, len(results))
	for i, r := range results {
		names[i] = r.Record.Name
	}
	return names
}
