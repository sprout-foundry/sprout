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

func TestEmbedMemory_NilManager(t *testing.T) {
	err := EmbedMemory(context.Background(), nil, "test", "content")
	if err != nil {
		t.Errorf("expected nil error with nil manager, got: %v", err)
	}
}

func TestEmbedMemory_EmptyName(t *testing.T) {
	mgr := setupMemoryEmbeddingManager(t)
	defer mgr.Close()

	err := EmbedMemory(context.Background(), mgr, "", "content")
	if err != nil {
		t.Errorf("expected nil error with empty name, got: %v", err)
	}
}

func TestEmbedMemory_EmptyContent(t *testing.T) {
	mgr := setupMemoryEmbeddingManager(t)
	defer mgr.Close()

	err := EmbedMemory(context.Background(), mgr, "test", "")
	if err != nil {
		t.Errorf("expected nil error with empty content, got: %v", err)
	}
}

func TestEmbedMemory_Success(t *testing.T) {
	mgr := setupMemoryEmbeddingManager(t)
	defer mgr.Close()
	ctx := context.Background()

	err := EmbedMemory(ctx, mgr, "test-memory", "# Test Memory\nThis is test content.")
	if err != nil {
		t.Fatalf("EmbedMemory failed: %v", err)
	}

	// Verify the record was stored
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("GetConversationStore failed: %v", err)
	}

	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	found := false
	for _, r := range all {
		if r.Type == "memory" && r.Name == "test-memory" {
			found = true
			if r.ID != "memory:test-memory" {
				t.Errorf("expected ID 'memory:test-memory', got '%s'", r.ID)
			}
			break
		}
	}
	if !found {
		t.Error("memory record not found in store")
	}
}

func TestEmbedMemory_Upsert(t *testing.T) {
	mgr := setupMemoryEmbeddingManager(t)
	defer mgr.Close()
	ctx := context.Background()

	// Store first version
	err := EmbedMemory(ctx, mgr, "upsert-test", "version 1")
	if err != nil {
		t.Fatalf("first EmbedMemory failed: %v", err)
	}

	// Store second version
	err = EmbedMemory(ctx, mgr, "upsert-test", "version 2 with more content")
	if err != nil {
		t.Fatalf("second EmbedMemory failed: %v", err)
	}

	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("GetConversationStore failed: %v", err)
	}

	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	count := 0
	for _, r := range all {
		if r.Type == "memory" && r.Name == "upsert-test" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 memory record after upsert, got %d", count)
	}
}

func TestDeleteMemoryEmbedding_NilManager(t *testing.T) {
	err := DeleteMemoryEmbedding(nil, "test")
	if err != nil {
		t.Errorf("expected nil error with nil manager, got: %v", err)
	}
}

func TestDeleteMemoryEmbedding_EmptyName(t *testing.T) {
	mgr := setupMemoryEmbeddingManager(t)
	defer mgr.Close()

	err := DeleteMemoryEmbedding(mgr, "")
	if err != nil {
		t.Errorf("expected nil error with empty name, got: %v", err)
	}
}

func TestDeleteMemoryEmbedding_Success(t *testing.T) {
	mgr := setupMemoryEmbeddingManager(t)
	defer mgr.Close()
	ctx := context.Background()

	// Store a memory first
	err := EmbedMemory(ctx, mgr, "delete-test", "content to delete")
	if err != nil {
		t.Fatalf("EmbedMemory failed: %v", err)
	}

	// Delete it
	err = DeleteMemoryEmbedding(mgr, "delete-test")
	if err != nil {
		t.Fatalf("DeleteMemoryEmbedding failed: %v", err)
	}

	// Verify it's gone
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("GetConversationStore failed: %v", err)
	}

	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	for _, r := range all {
		if r.Type == "memory" && r.Name == "delete-test" {
			t.Error("memory record should have been deleted")
		}
	}
}

func TestMigrateMemories_NilManager(t *testing.T) {
	// Should not panic
	ResetMigrationForTesting()
	MigrateMemories(context.Background(), nil)
}

func TestMigrateMemories_SkipsAlreadyEmbedded(t *testing.T) {
	mgr := setupMemoryEmbeddingManager(t)
	defer mgr.Close()
	ctx := context.Background()

	// Pre-embed a memory
	err := EmbedMemory(ctx, mgr, "existing-mem", "already embedded")
	if err != nil {
		t.Fatalf("pre-embed failed: %v", err)
	}

	// Create a memory file on disk
	memDir := getMemoryDir()
	if memDir == "" {
		t.Fatal("getMemoryDir returned empty")
	}
	err = os.WriteFile(filepath.Join(memDir, "existing-mem.md"), []byte("already embedded"), 0644)
	if err != nil {
		t.Fatalf("write memory file: %v", err)
	}

	ResetMigrationForTesting()
	MigrateMemories(ctx, mgr)

	// Should only have 1 record for "existing-mem"
	store, _ := mgr.GetConversationStore(ctx)
	all, _ := store.LoadAll()
	count := 0
	for _, r := range all {
		if r.Type == "memory" && r.Name == "existing-mem" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 record for existing-mem, got %d", count)
	}
}

// Helper to create a test EmbeddingManager using the same pattern as proactive_context_test.go
func setupMemoryEmbeddingManager(t *testing.T) *embedding.EmbeddingManager {
	t.Helper()
	ctx := context.Background()
	tempDir := t.TempDir()

	cfg := &configuration.EmbeddingIndexConfig{IndexDir: tempDir}
	mgr := embedding.NewEmbeddingManager(cfg, tempDir)

	if err := mgr.Init(ctx); err != nil {
		if strings.Contains(err.Error(), "ONNX") || strings.Contains(err.Error(), "onnx") {
			t.Skip("Skipping: ONNX runtime not available")
		}
		t.Fatalf("failed to init embedding manager: %v", err)
	}

	return mgr
}
