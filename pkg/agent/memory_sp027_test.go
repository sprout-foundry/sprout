package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// =============================================================================
// SP-027-4f: End-to-end memory migration and search ranking tests
// =============================================================================

// ---------------------------------------------------------------------------
// Test 1: TestRunMemoryMigration_E2E
//
// End-to-end migration: create real .md files on disk, run migration, verify
// each memory is in the conversation store with the correct ID and type, then
// confirm a second run is a no-op via the marker check.
// ---------------------------------------------------------------------------
func TestRunMemoryMigration_E2E(t *testing.T) {
	ctx := context.Background()

	tmp := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmp)
	t.Setenv("LEDIT_CONFIG", tmp)

	// Create the memories directory with actual .md files.
	memDir := filepath.Join(tmp, memoryDirName)
	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatalf("failed to create memories dir: %v", err)
	}

	memories := []struct {
		name    string
		content string
	}{
		{
			name:    "git-safety",
			content: "# Git Safety Rules\n\nAlways double-check before force-pushing to shared branches.",
		},
		{
			name:    "my @test Memory!",
			content: "# Test Memory\n\nContent for a memory with special characters in the name.",
		},
		{
			name:    "webui-embed-setup",
			content: "# WebUI Embedding\n\nEmbedding index should be initialized on startup.",
		},
	}

	for _, m := range memories {
		err := os.WriteFile(filepath.Join(memDir, m.name+".md"), []byte(m.content), 0644)
		if err != nil {
			t.Fatalf("failed to write memory file %q: %v", m.name, err)
		}
	}

	// Create EmbeddingManager and initialize.
	cfg := &configuration.EmbeddingIndexConfig{IndexDir: tmp}
	em := embedding.NewEmbeddingManager(cfg, tmp)
	if err := em.Init(ctx); err != nil {
		t.Fatalf("failed to init embedding manager: %v", err)
	}
	defer em.Close()

	// Verify marker does NOT exist before migration.
	indexDir := em.GetIndexDir()
	if hasMigratedMemories(indexDir) {
		t.Fatal("marker should not exist before first migration")
	}

	// Run migration.
	if err := RunMemoryMigration(ctx, em); err != nil {
		t.Fatalf("RunMemoryMigration failed: %v", err)
	}

	// Verify marker exists after migration.
	if !hasMigratedMemories(indexDir) {
		t.Fatal("marker should exist after migration")
	}

	// Verify each memory is in the conversation store with correct ID and type.
	store, err := em.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}

	records, err := store.LoadAll()
	if err != nil {
		t.Fatalf("failed to load all records: %v", err)
	}

	// Build a map by ID for easier assertions.
	recordMap := make(map[string]*embedding.VectorRecord)
	for i := range records {
		recordMap[records[i].ID] = &records[i]
	}

	// Check each expected sanitized ID.
	expectedIDs := []struct {
		raw    string
		sanitized string
	}{
		{"git-safety", "git-safety"},
		{"my @test Memory!", "my-test-memory"},
		{"webui-embed-setup", "webui-embed-setup"},
	}

	for _, e := range expectedIDs {
		rec, ok := recordMap[e.sanitized]
		if !ok {
			t.Errorf("expected record with ID %q (from raw name %q), not found", e.sanitized, e.raw)
			continue
		}
		if rec.Type != "memory" {
			t.Errorf("record %q: expected type 'memory', got %q", e.sanitized, rec.Type)
		}
		if rec.Name != e.sanitized {
			t.Errorf("record %q: expected Name %q, got %q", e.sanitized, e.sanitized, rec.Name)
		}
	}

	if len(records) != 3 {
		t.Errorf("expected 3 records after migration, got %d", len(records))
	}

	// Run migration again — should be a no-op (marker check).
	if err := RunMemoryMigration(ctx, em); err != nil {
		t.Errorf("second RunMemoryMigration should be no-op, got: %v", err)
	}

	// Verify no duplicate records were created.
	if store.Size() != 3 {
		t.Errorf("expected 3 records after second migration (no-op), got %d", store.Size())
	}
}

// ---------------------------------------------------------------------------
// Test 2: TestRunMemoryMigration_NoMemories
//
// When the memories directory exists but is empty, migration should still
// write the marker file so future runs skip the migration.
// ---------------------------------------------------------------------------
func TestRunMemoryMigration_NoMemories(t *testing.T) {
	ctx := context.Background()

	tmp := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmp)
	t.Setenv("LEDIT_CONFIG", tmp)

	// Create an empty memories directory.
	memDir := filepath.Join(tmp, memoryDirName)
	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatalf("failed to create memories dir: %v", err)
	}

	cfg := &configuration.EmbeddingIndexConfig{IndexDir: tmp}
	em := embedding.NewEmbeddingManager(cfg, tmp)
	if err := em.Init(ctx); err != nil {
		t.Fatalf("failed to init embedding manager: %v", err)
	}
	defer em.Close()

	indexDir := em.GetIndexDir()
	if hasMigratedMemories(indexDir) {
		t.Fatal("marker should not exist before migration")
	}

	if err := RunMemoryMigration(ctx, em); err != nil {
		t.Fatalf("RunMemoryMigration failed: %v", err)
	}

	// Marker should be written even with no memories to migrate.
	if !hasMigratedMemories(indexDir) {
		t.Fatal("marker should exist after no-memories migration")
	}

	// No records should exist in the store.
	store, err := em.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}
	if store.Size() != 0 {
		t.Errorf("expected 0 records with no memories, got %d", store.Size())
	}
}

// ---------------------------------------------------------------------------
// Test 3: TestRunMemoryMigration_BatchSuccess
//
// When RunMemoryMigration processes multiple files with the mock provider,
// all embeddings succeed. Verify that all 3 files end up in the store with
// correct sanitized IDs and that the marker is written.
// ---------------------------------------------------------------------------
func TestRunMemoryMigration_BatchSuccess(t *testing.T) {
	ctx := context.Background()

	tmp := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmp)
	t.Setenv("LEDIT_CONFIG", tmp)

	memDir := filepath.Join(tmp, memoryDirName)
	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatalf("failed to create memories dir: %v", err)
	}

	// Create 3 memory files with diverse content.
	// The mock provider always succeeds, so all should be embedded.
	files := []struct {
		name      string
		content   string
	}{
		{
			name:    "go-formatter",
			content: "# Go Formatting\n\nAlways run gofmt before committing. Use goimports for import sorting.",
		},
		{
			name:    "database connections",
			content: "# Database\n\nUse connection pooling with maxIdle=10, maxOpen=100, and idleTimeout=30s.",
		},
		{
			name:    "test patterns",
			content: "# Testing\n\nWrite table-driven tests for all public functions.",
		},
	}

	for _, f := range files {
		err := os.WriteFile(filepath.Join(memDir, f.name+".md"), []byte(f.content), 0644)
		if err != nil {
			t.Fatalf("failed to write %q: %v", f.name, err)
		}
	}

	cfg := &configuration.EmbeddingIndexConfig{IndexDir: tmp}
	em := embedding.NewEmbeddingManager(cfg, tmp)
	if err := em.Init(ctx); err != nil {
		t.Fatalf("failed to init embedding manager: %v", err)
	}
	defer em.Close()

	if err := RunMemoryMigration(ctx, em); err != nil {
		t.Fatalf("RunMemoryMigration failed: %v", err)
	}

	// Verify marker was written.
	indexDir := em.GetIndexDir()
	if !hasMigratedMemories(indexDir) {
		t.Fatal("marker should exist after migration")
	}

	store, err := em.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}

	// All 3 files should be in the store.
	records, err := store.LoadAll()
	if err != nil {
		t.Fatalf("failed to load records: %v", err)
	}

	if len(records) != 3 {
		t.Fatalf("expected 3 records in store, got %d", len(records))
	}

	// Verify each record has correct sanitized ID and type.
	for _, rec := range records {
		if rec.Type != "memory" {
			t.Errorf("record %q: expected type 'memory', got %q", rec.ID, rec.Type)
		}
		// Verify the ID is one of the expected sanitized names.
		found := false
		for _, f := range files {
			expectedID := sanitizeMemoryName(f.name)
			if rec.ID == expectedID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("unexpected record ID %q — not one of expected sanitized names", rec.ID)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 4: TestSearchMemories_SemanticQuery_Ranking
//
// Store 3 memories and search with a query. The mock provider uses
// float32(len(text)+i)/1000 for embeddings, so we cannot guarantee a
// specific ranking. Instead, verify the search mechanism works: the
// result header exists, at least 2 memory names appear, and relevance
// scores are displayed.
// ---------------------------------------------------------------------------
func TestSearchMemories_SemanticQuery_Ranking(t *testing.T) {
	ctx := context.Background()

	tmp := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmp)
	t.Setenv("LEDIT_CONFIG", tmp)

	agent := newTestAgent(t)
	defer agent.Shutdown()

	cfg := &configuration.EmbeddingIndexConfig{IndexDir: tmp}
	em := embedding.NewEmbeddingManager(cfg, tmp)
	agent.embeddingMgr = em

	if err := em.Init(ctx); err != nil {
		t.Fatalf("failed to init embedding manager: %v", err)
	}

	// Add defer to close the embedding manager.
	defer em.Close()

	store, err := em.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}

	// Store 3 memories with distinctly different content.
	// The mock provider creates embeddings based on text length:
	//   vec[i] = float32(len(text)+i) / 1000.0
	// So texts with similar lengths produce similar vectors.
	memories := []struct {
		name    string
		content string
	}{
		{
			name:    "git-commit-format",
			content: "# Git Commit Format\n\nUse conventional commits: type(scope): description.",
		},
		{
			name:    "database-connections",
			content: "# Database Connections\n\nConnection pooling config for PostgreSQL.",
		},
		{
			name:    "test-patterns",
			content: "# Test Patterns\n\nUse table-driven tests in Go for comprehensive coverage and readability.",
		},
	}

	for _, m := range memories {
		if err := store.StoreMemory(ctx, m.name, m.content); err != nil {
			t.Fatalf("failed to store memory %q: %v", m.name, err)
		}
	}

	// Search for "version control conventions".
	result, err := handleSearchMemories(ctx, agent, map[string]interface{}{
		"query": "version control conventions",
	})
	if err != nil {
		t.Fatalf("handleSearchMemories failed: %v", err)
	}

	// Verify the search mechanism works:
	// - The mock provider uses length-based embeddings (float32(len(text)+i)/1000),
	//   so we cannot guarantee a specific memory will rank first.
	// - Instead, verify that the result header exists, at least 2 memory names
	//   appear, and a relevance score is shown.
	if !strings.Contains(result, "Memory Search Results") {
		t.Errorf("expected search results header, got: %s", result)
	}

	// At least 2 memory names should appear in the results.
	namesFound := 0
	for _, m := range memories {
		if strings.Contains(result, m.name) {
			namesFound++
		}
	}
	if namesFound < 2 {
		t.Errorf("expected at least 2 memory names in search results, got %d (%d stored): %s", namesFound, len(memories), result)
	}

	// A relevance score (decimal number) should appear in the output.
	if !strings.Contains(result, "relevance:") {
		t.Errorf("expected 'relevance:' in search results, got: %s", result)
	}
}

// ---------------------------------------------------------------------------
// Test 5: TestMemoryEmbedding_RoundTrip_Metadata
//
// Save a memory via StoreMemory and verify that the stored record contains
// correct metadata (title extracted from first line) and the signature
// contains the content.
// ---------------------------------------------------------------------------
func TestMemoryEmbedding_RoundTrip_Metadata(t *testing.T) {
	ctx := context.Background()

	tmp := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmp)
	t.Setenv("LEDIT_CONFIG", tmp)

	cfg := &configuration.EmbeddingIndexConfig{IndexDir: tmp}
	em := embedding.NewEmbeddingManager(cfg, tmp)
	if err := em.Init(ctx); err != nil {
		t.Fatalf("failed to init embedding manager: %v", err)
	}
	defer em.Close()

	store, err := em.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}

	// Store a memory with known content.
	memName := "metadata-test-memory"
	memContent := "# Important Project Convention\n\nAll public API functions must have godoc comments."

	if err := store.StoreMemory(ctx, memName, memContent); err != nil {
		t.Fatalf("StoreMemory failed: %v", err)
	}

	// Load the record back from the store.
	records, err := store.LoadAll()
	if err != nil {
		t.Fatalf("failed to load records: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	rec := records[0]

	// Verify ID and type.
	if rec.ID != memName {
		t.Errorf("expected ID %q, got %q", memName, rec.ID)
	}
	if rec.Type != "memory" {
		t.Errorf("expected type 'memory', got %q", rec.Type)
	}

	// Verify Metadata contains the "title" field extracted from first line.
	if rec.Metadata == nil {
		t.Fatal("Metadata should not be nil")
	}

	title, ok := rec.Metadata["title"].(string)
	if !ok {
		t.Fatal("Metadata should contain 'title' as string")
	}

	// Title should be the first non-empty line, trimmed.
	expectedTitle := "# Important Project Convention"
	if title != expectedTitle {
		t.Errorf("expected title %q, got %q", expectedTitle, title)
	}

	// Verify Metadata also contains contentLength.
	contentLen, ok := rec.Metadata["contentLength"].(int)
	if !ok {
		t.Fatal("Metadata should contain 'contentLength' as int")
	}
	if contentLen != len(memContent) {
		t.Errorf("expected contentLength %d, got %d", len(memContent), contentLen)
	}

	// Verify Signature contains the content (truncated to maxSignatureLen).
	if rec.Signature == "" {
		t.Fatal("Signature should not be empty")
	}
	if !strings.HasPrefix(rec.Signature, "# Important Project Convention") {
		t.Errorf("expected signature to start with title, got %q", rec.Signature)
	}
	if !strings.Contains(rec.Signature, "godoc comments") {
		t.Errorf("expected signature to contain 'godoc comments', got %q", rec.Signature)
	}
}
