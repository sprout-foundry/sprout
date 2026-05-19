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

// ─── SaveMemoryWithEmbedding tests ───

// TestSaveMemoryWithEmbedding_Success verifies that saving a memory writes the
// file to disk AND stores a record in the conversation store with correct fields.
func TestSaveMemoryWithEmbedding_Success(t *testing.T) {
	ctx := context.Background()

	// Create a temporary directory for test isolation
	tempDir := t.TempDir()

	// Set config env vars to use temp dir
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("LEDIT_CONFIG", tempDir)

	// Create EmbeddingManager with minimal config
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: tempDir,
	}

	mgr := embedding.NewEmbeddingManager(cfg, tempDir)

	// Initialize the manager
	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("failed to initialize embedding manager: %v", err)
	}
	defer mgr.Close()

	// Save a memory with embedding
	name := "test-memory-success"
	content := "# Test Memory\n\nThis is important context to remember."

	if err := SaveMemoryWithEmbedding(ctx, mgr, name, content); err != nil {
		t.Fatalf("SaveMemoryWithEmbedding returned unexpected error: %v", err)
	}

	// Verify the memory file was created on disk
	filePath := filepath.Join(tempDir, "memories", name+".md")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("memory file not found on disk at %s: %v", filePath, err)
	}
	if string(data) != content {
		t.Errorf("file content mismatch: expected %q, got %q", content, string(data))
	}

	// Verify the embedding was stored in the conversation store
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}

	allRecords, err := store.LoadAll()
	if err != nil {
		t.Fatalf("failed to load records from conversation store: %v", err)
	}

	if len(allRecords) != 1 {
		t.Fatalf("expected 1 record in conversation store, got %d", len(allRecords))
	}

	record := allRecords[0]
	if record.ID != name {
		t.Errorf("expected record ID %q, got %q", name, record.ID)
	}

	if record.Type != "memory" {
		t.Errorf("expected record type 'memory', got %q", record.Type)
	}

	if record.Embedding == nil {
		t.Error("expected non-nil embedding in stored record")
	}
}

// TestSaveMemoryWithEmbedding_NilManager verifies that when the embedding
// manager is nil, the memory file is still saved and no error is returned.
func TestSaveMemoryWithEmbedding_NilManager(t *testing.T) {
	ctx := context.Background()

	// Create a temporary directory for test isolation
	tempDir := t.TempDir()

	// Set config env vars to use temp dir
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("LEDIT_CONFIG", tempDir)

	name := "test-memory-nil-mgr"
	content := "# Memory Without Manager\n\nContent here."

	// Call with nil manager — should not panic or return error
	if err := SaveMemoryWithEmbedding(ctx, nil, name, content); err != nil {
		t.Errorf("SaveMemoryWithEmbedding should return nil when manager is nil, got %v", err)
	}

	// Verify the memory file was still saved on disk
	filePath := filepath.Join(tempDir, "memories", name+".md")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("memory file should have been saved even with nil manager: %v", err)
	}
	if string(data) != content {
		t.Errorf("file content mismatch: expected %q, got %q", content, string(data))
	}
}

// TestSaveMemoryWithEmbedding_NilContext verifies that when the context is nil,
// the memory file is still saved and no error is returned.
func TestSaveMemoryWithEmbedding_NilContext(t *testing.T) {
	// Create a temporary directory for test isolation
	tempDir := t.TempDir()

	// Set config env vars to use temp dir
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("LEDIT_CONFIG", tempDir)

	// Create EmbeddingManager with minimal config
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: tempDir,
	}

	mgr := embedding.NewEmbeddingManager(cfg, tempDir)

	// Initialize the manager
	if err := mgr.Init(context.Background()); err != nil {
		t.Fatalf("failed to initialize embedding manager: %v", err)
	}
	defer mgr.Close()

	name := "test-memory-nil-ctx"
	content := "# Memory Without Context\n\nContent here."

	// Call with nil context — should not panic or return error
	if err := SaveMemoryWithEmbedding(nil, mgr, name, content); err != nil {
		t.Errorf("SaveMemoryWithEmbedding should return nil when context is nil, got %v", err)
	}

	// Verify the memory file was still saved on disk
	filePath := filepath.Join(tempDir, "memories", name+".md")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("memory file should have been saved even with nil context: %v", err)
	}
	if string(data) != content {
		t.Errorf("file content mismatch: expected %q, got %q", content, string(data))
	}

	// Verify no record was stored (embedding was skipped due to nil context)
	store, err := mgr.GetConversationStore(context.Background())
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}
	if store.Size() != 0 {
		t.Errorf("expected 0 records when context is nil, got %d", store.Size())
	}
}

// TestSaveMemoryWithEmbedding_EmptyName verifies that when the name is empty,
// the memory file is still saved (sanitized to "untitled") and no error is returned.
func TestSaveMemoryWithEmbedding_EmptyName(t *testing.T) {
	ctx := context.Background()

	// Create a temporary directory for test isolation
	tempDir := t.TempDir()

	// Set config env vars to use temp dir
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("LEDIT_CONFIG", tempDir)

	// Create EmbeddingManager with minimal config
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: tempDir,
	}

	mgr := embedding.NewEmbeddingManager(cfg, tempDir)

	// Initialize the manager
	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("failed to initialize embedding manager: %v", err)
	}
	defer mgr.Close()

	content := "# Untitled Memory\n\nContent for empty name."

	// Call with empty name — should not panic or return error
	if err := SaveMemoryWithEmbedding(ctx, mgr, "", content); err != nil {
		t.Errorf("SaveMemoryWithEmbedding should return nil when name is empty, got %v", err)
	}

	// Verify the memory file was saved under sanitized name "untitled"
	filePath := filepath.Join(tempDir, "memories", "untitled.md")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("memory file should have been saved as untitled.md: %v", err)
	}
	if string(data) != content {
		t.Errorf("file content mismatch: expected %q, got %q", content, string(data))
	}

	// The sanitized name "untitled" is non-empty, so embedding proceeds
	// and the store should contain one record with ID "untitled".
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}

	// The embedding should be stored with ID "untitled" (the sanitized name)
	allRecords, err := store.LoadAll()
	if err != nil {
		t.Fatalf("failed to load records: %v", err)
	}

	if len(allRecords) != 1 {
		t.Fatalf("expected 1 record in store, got %d", len(allRecords))
	}

	if allRecords[0].ID != "untitled" {
		t.Errorf("expected record ID 'untitled', got %q", allRecords[0].ID)
	}
}

// TestSaveMemoryWithEmbedding_EmptyContent verifies that when the content is empty,
// the memory file is still saved and no error is returned.
func TestSaveMemoryWithEmbedding_EmptyContent(t *testing.T) {
	ctx := context.Background()

	// Create a temporary directory for test isolation
	tempDir := t.TempDir()

	// Set config env vars to use temp dir
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("LEDIT_CONFIG", tempDir)

	// Create EmbeddingManager with minimal config
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: tempDir,
	}

	mgr := embedding.NewEmbeddingManager(cfg, tempDir)

	// Initialize the manager
	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("failed to initialize embedding manager: %v", err)
	}
	defer mgr.Close()

	name := "test-memory-empty-content"

	// Call with empty content — should not panic or return error
	if err := SaveMemoryWithEmbedding(ctx, mgr, name, ""); err != nil {
		t.Errorf("SaveMemoryWithEmbedding should return nil when content is empty, got %v", err)
	}

	// Verify the memory file was still saved (empty content)
	filePath := filepath.Join(tempDir, "memories", name+".md")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("memory file should have been saved even with empty content: %v", err)
	}
	if string(data) != "" {
		t.Errorf("file content should be empty, got %q", string(data))
	}

	// Embedding should be skipped (empty content), so no record should exist
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}
	if store.Size() != 0 {
		t.Errorf("expected 0 records with empty content, got %d", store.Size())
	}
}

// TestSaveMemoryWithEmbedding_SanitizesName verifies that names with spaces and
// special characters are sanitized for both the file name and the embedding record ID.
func TestSaveMemoryWithEmbedding_SanitizesName(t *testing.T) {
	ctx := context.Background()

	// Create a temporary directory for test isolation
	tempDir := t.TempDir()

	// Set config env vars to use temp dir
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("LEDIT_CONFIG", tempDir)

	// Create EmbeddingManager with minimal config
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: tempDir,
	}

	mgr := embedding.NewEmbeddingManager(cfg, tempDir)

	// Initialize the manager
	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("failed to initialize embedding manager: %v", err)
	}
	defer mgr.Close()

	name := "My Memory! (with spaces & special chars)"
	content := "# Sanitized Name Memory\n\nThis tests name sanitization."

	if err := SaveMemoryWithEmbedding(ctx, mgr, name, content); err != nil {
		t.Fatalf("SaveMemoryWithEmbedding returned unexpected error: %v", err)
	}

	sanitized := sanitizeMemoryName(name)
	// The regex strips !, (, ), & — the spaces become hyphens
	// and adjacent hyphens from stripped chars remain.

	// Verify the memory file was saved under the sanitized name
	filePath := filepath.Join(tempDir, "memories", sanitized+".md")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("memory file not found at sanitized path %s: %v", filePath, err)
	}
	if string(data) != content {
		t.Errorf("file content mismatch: expected %q, got %q", content, string(data))
	}

	// Verify the embedding was stored with the sanitized name as ID
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}

	allRecords, err := store.LoadAll()
	if err != nil {
		t.Fatalf("failed to load records: %v", err)
	}

	if len(allRecords) != 1 {
		t.Fatalf("expected 1 record, got %d", len(allRecords))
	}

	if allRecords[0].ID != sanitized {
		t.Errorf("expected record ID %q (sanitized name), got %q", sanitized, allRecords[0].ID)
	}
}

// TestSaveMemoryWithEmbedding_Overwrite verifies that saving a memory with the
// same name twice replaces the file AND the embedding record (not duplicated).
func TestSaveMemoryWithEmbedding_Overwrite(t *testing.T) {
	ctx := context.Background()

	// Create a temporary directory for test isolation
	tempDir := t.TempDir()

	// Set config env vars to use temp dir
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("LEDIT_CONFIG", tempDir)

	// Create EmbeddingManager with minimal config
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: tempDir,
	}

	mgr := embedding.NewEmbeddingManager(cfg, tempDir)

	// Initialize the manager
	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("failed to initialize embedding manager: %v", err)
	}
	defer mgr.Close()

	name := "test-memory-overwrite"
	content1 := "# Original Memory\n\nFirst version of the content."
	content2 := "# Updated Memory\n\nSecond version that replaces the original."

	// Save first version
	if err := SaveMemoryWithEmbedding(ctx, mgr, name, content1); err != nil {
		t.Fatalf("first SaveMemoryWithEmbedding failed: %v", err)
	}

	// Save second version with same name
	if err := SaveMemoryWithEmbedding(ctx, mgr, name, content2); err != nil {
		t.Fatalf("second SaveMemoryWithEmbedding failed: %v", err)
	}

	// Verify the file contains the updated content
	filePath := filepath.Join(tempDir, "memories", name+".md")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("memory file not found: %v", err)
	}
	if string(data) != content2 {
		t.Errorf("file should contain updated content, got %q", string(data))
	}

	// Verify only one record exists in the store (replaced, not duplicated)
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}

	if store.Size() != 1 {
		t.Errorf("expected 1 record after overwrite, got %d", store.Size())
	}

	// Verify the record reflects the new content
	allRecords, err := store.LoadAll()
	if err != nil {
		t.Fatalf("failed to load records: %v", err)
	}

	rec := allRecords[0]
	if rec.ID != name {
		t.Errorf("expected record ID %q, got %q", name, rec.ID)
	}

	// The signature should reflect the new content (or a prefix of it)
	if !strings.HasPrefix(rec.Signature, "# Updated Memory") {
		t.Errorf("expected signature to reflect updated content, got %q", rec.Signature)
	}
}

// ─── DeleteMemoryWithEmbedding tests ───

// TestDeleteMemoryWithEmbedding_Success verifies that deleting a memory removes
// both the file from disk AND the record from the conversation store.
func TestDeleteMemoryWithEmbedding_Success(t *testing.T) {
	ctx := context.Background()

	// Create a temporary directory for test isolation
	tempDir := t.TempDir()

	// Set config env vars to use temp dir
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("LEDIT_CONFIG", tempDir)

	// Create EmbeddingManager with minimal config
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: tempDir,
	}

	mgr := embedding.NewEmbeddingManager(cfg, tempDir)

	// Initialize the manager
	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("failed to initialize embedding manager: %v", err)
	}
	defer mgr.Close()

	name := "test-memory-delete-success"
	content := "# Memory to Delete\n\nThis will be removed."

	// Save the memory first
	if err := SaveMemoryWithEmbedding(ctx, mgr, name, content); err != nil {
		t.Fatalf("failed to save memory before delete test: %v", err)
	}

	// Verify file exists before delete
	filePath := filepath.Join(tempDir, "memories", name+".md")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatal("memory file should exist before deletion")
	}

	// Verify record exists before delete
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}
	if store.Size() != 1 {
		t.Fatalf("expected 1 record before deletion, got %d", store.Size())
	}

	// Delete the memory
	if err := DeleteMemoryWithEmbedding(ctx, mgr, name); err != nil {
		t.Fatalf("DeleteMemoryWithEmbedding returned unexpected error: %v", err)
	}

	// Verify the file is gone
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("memory file should be deleted from disk")
	}

	// Verify the record is gone from the store
	if store.Size() != 0 {
		t.Errorf("expected 0 records after deletion, got %d", store.Size())
	}
}

// TestDeleteMemoryWithEmbedding_NilManager verifies that when the embedding
// manager is nil, the memory file is still deleted and no error is returned.
func TestDeleteMemoryWithEmbedding_NilManager(t *testing.T) {
	ctx := context.Background()

	// Create a temporary directory for test isolation
	tempDir := t.TempDir()

	// Set config env vars to use temp dir
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("LEDIT_CONFIG", tempDir)

	name := "test-memory-delete-nil-mgr"
	content := "# Memory to Delete With Nil Manager\n\nContent here."

	// Save the memory file first (without embedding manager)
	if err := SaveMemory(name, content); err != nil {
		t.Fatalf("failed to save memory: %v", err)
	}

	// Verify file exists before delete
	filePath := filepath.Join(tempDir, "memories", name+".md")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatal("memory file should exist before deletion")
	}

	// Call with nil manager — should not panic or return error
	if err := DeleteMemoryWithEmbedding(ctx, nil, name); err != nil {
		t.Errorf("DeleteMemoryWithEmbedding should return nil when manager is nil, got %v", err)
	}

	// Verify the file was still deleted
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("memory file should be deleted even with nil manager")
	}
}

// TestDeleteMemoryWithEmbedding_NilContext verifies that when the context is nil,
// the memory file is still deleted and no error is returned.
func TestDeleteMemoryWithEmbedding_NilContext(t *testing.T) {
	// Create a temporary directory for test isolation
	tempDir := t.TempDir()

	// Set config env vars to use temp dir
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("LEDIT_CONFIG", tempDir)

	// Create EmbeddingManager with minimal config
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: tempDir,
	}

	mgr := embedding.NewEmbeddingManager(cfg, tempDir)

	// Initialize the manager
	if err := mgr.Init(context.Background()); err != nil {
		t.Fatalf("failed to initialize embedding manager: %v", err)
	}
	defer mgr.Close()

	name := "test-memory-delete-nil-ctx"
	content := "# Memory to Delete With Nil Context\n\nContent here."

	// Save the memory file first
	if err := SaveMemory(name, content); err != nil {
		t.Fatalf("failed to save memory: %v", err)
	}

	// Verify file exists before delete
	filePath := filepath.Join(tempDir, "memories", name+".md")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatal("memory file should exist before deletion")
	}

	// Call with nil context — should not panic or return error
	if err := DeleteMemoryWithEmbedding(nil, mgr, name); err != nil {
		t.Errorf("DeleteMemoryWithEmbedding should return nil when context is nil, got %v", err)
	}

	// Verify the file was still deleted
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("memory file should be deleted even with nil context")
	}
}

// TestDeleteMemoryWithEmbedding_EmptyName verifies that when the name is empty,
// the deletion handles gracefully. Because SaveMemory sanitizes "" to "untitled"
// but DeleteMemory does not sanitize (it appends ".md" to get ".md"), passing
// empty name to DeleteMemoryWithEmbedding returns an error from DeleteMemory.
// This test verifies the sanitized name works correctly for delete.
func TestDeleteMemoryWithEmbedding_EmptyName(t *testing.T) {
	ctx := context.Background()

	// Create a temporary directory for test isolation
	tempDir := t.TempDir()

	// Set config env vars to use temp dir
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("LEDIT_CONFIG", tempDir)

	// Create EmbeddingManager with minimal config
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: tempDir,
	}

	mgr := embedding.NewEmbeddingManager(cfg, tempDir)

	// Initialize the manager
	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("failed to initialize embedding manager: %v", err)
	}
	defer mgr.Close()

	// Save a memory with empty name — this creates "untitled.md"
	content := "# Untitled Memory\n\nThis will be deleted by sanitized name."
	if err := SaveMemoryWithEmbedding(ctx, mgr, "", content); err != nil {
		t.Fatalf("failed to save untitled memory: %v", err)
	}

	// Verify file exists before delete
	filePath := filepath.Join(tempDir, "memories", "untitled.md")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatal("untitled memory file should exist before deletion")
	}

	// Verify the record exists in the store with ID "untitled"
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}
	if store.Size() != 1 {
		t.Fatalf("expected 1 record before deletion, got %d", store.Size())
	}

	// Delete using the sanitized name "untitled" (matching what SaveMemory created)
	if err := DeleteMemoryWithEmbedding(ctx, mgr, "untitled"); err != nil {
		t.Errorf("DeleteMemoryWithEmbedding should succeed for sanitized name, got: %v", err)
	}

	// Verify the "untitled" file was deleted
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("untitled memory file should be deleted")
	}

	// Verify the record was removed from the store
	if store.Size() != 0 {
		t.Errorf("expected 0 records after deletion, got %d", store.Size())
	}
}

// TestDeleteMemoryWithEmbedding_NonExistent verifies that deleting a memory
// that doesn't exist returns an error.
func TestDeleteMemoryWithEmbedding_NonExistent(t *testing.T) {
	ctx := context.Background()

	// Create a temporary directory for test isolation
	tempDir := t.TempDir()

	// Set config env vars to use temp dir
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("LEDIT_CONFIG", tempDir)

	// Create EmbeddingManager with minimal config
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: tempDir,
	}

	mgr := embedding.NewEmbeddingManager(cfg, tempDir)

	// Initialize the manager
	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("failed to initialize embedding manager: %v", err)
	}
	defer mgr.Close()

	name := "this-memory-does-not-exist"

	// Try to delete a non-existent memory — should return an error
	err := DeleteMemoryWithEmbedding(ctx, mgr, name)
	if err == nil {
		t.Fatal("expected error when deleting non-existent memory, got nil")
	}

	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("expected error to mention 'does not exist', got: %v", err)
	}
}

// TestDeleteMemoryWithEmbedding_UnsanitizedName verifies that deleting a memory
// works when the original unsanitized name is provided. DeleteMemoryWithEmbedding
// sanitizes the name before calling DeleteMemory, so "My Memory!" correctly
// resolves to "my-memory" and removes the right file and store record.
func TestDeleteMemoryWithEmbedding_UnsanitizedName(t *testing.T) {
	ctx := context.Background()

	// Create a temporary directory for test isolation
	tempDir := t.TempDir()

	// Set config env vars to use temp dir
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("LEDIT_CONFIG", tempDir)

	// Create EmbeddingManager with minimal config
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: tempDir,
	}

	mgr := embedding.NewEmbeddingManager(cfg, tempDir)

	// Initialize the manager
	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("failed to initialize embedding manager: %v", err)
	}
	defer mgr.Close()

	// Save with unsanitized name
	name := "My Memory! (with spaces)"
	content := "# Memory to Delete via Unsanitized Name\n\nContent here."

	if err := SaveMemoryWithEmbedding(ctx, mgr, name, content); err != nil {
		t.Fatalf("failed to save memory: %v", err)
	}

	sanitized := sanitizeMemoryName(name)

	// Verify file exists
	filePath := filepath.Join(tempDir, "memories", sanitized+".md")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatal("memory file should exist before deletion")
	}

	// Verify record exists
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}
	if store.Size() != 1 {
		t.Fatalf("expected 1 record before deletion, got %d", store.Size())
	}

	// Delete using the original unsanitized name
	if err := DeleteMemoryWithEmbedding(ctx, mgr, name); err != nil {
		t.Fatalf("DeleteMemoryWithEmbedding with unsanitized name failed: %v", err)
	}

	// Verify file is gone
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("memory file should be deleted")
	}

	// Verify record is gone
	if store.Size() != 0 {
		t.Errorf("expected 0 records after deletion, got %d", store.Size())
	}
}

// ─── EmbedMemory tests (graceful failure paths) ───

// TestEmbedMemory_NilManager verifies that passing nil manager returns nil
// without panicking.
func TestEmbedMemory_NilManager(t *testing.T) {
	ctx := context.Background()

	// Call with nil manager — should return nil, no panic
	if err := EmbedMemory(ctx, nil, "some-name", "some content"); err != nil {
		t.Errorf("EmbedMemory should return nil when manager is nil, got %v", err)
	}
}

// TestEmbedMemory_NilContext verifies that passing nil context returns nil
// without panicking.
func TestEmbedMemory_NilContext(t *testing.T) {
	// Create a temporary directory for test isolation
	tempDir := t.TempDir()

	// Set config env vars to use temp dir
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("LEDIT_CONFIG", tempDir)

	// Create EmbeddingManager with minimal config
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: tempDir,
	}

	mgr := embedding.NewEmbeddingManager(cfg, tempDir)

	// Initialize the manager
	if err := mgr.Init(context.Background()); err != nil {
		t.Fatalf("failed to initialize embedding manager: %v", err)
	}
	defer mgr.Close()

	// Call with nil context — should return nil, no panic
	if err := EmbedMemory(nil, mgr, "some-name", "some content"); err != nil {
		t.Errorf("EmbedMemory should return nil when context is nil, got %v", err)
	}

	// Verify nothing was stored
	store, err := mgr.GetConversationStore(context.Background())
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}
	if store.Size() != 0 {
		t.Errorf("expected 0 records when context is nil, got %d", store.Size())
	}
}

// TestEmbedMemory_EmptyName verifies that passing an empty name returns nil
// without panicking.
func TestEmbedMemory_EmptyName(t *testing.T) {
	ctx := context.Background()

	// Create a temporary directory for test isolation
	tempDir := t.TempDir()

	// Set config env vars to use temp dir
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("LEDIT_CONFIG", tempDir)

	// Create EmbeddingManager with minimal config
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: tempDir,
	}

	mgr := embedding.NewEmbeddingManager(cfg, tempDir)

	// Initialize the manager
	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("failed to initialize embedding manager: %v", err)
	}
	defer mgr.Close()

	// Call with empty name — should return nil, no panic
	if err := EmbedMemory(ctx, mgr, "", "some content"); err != nil {
		t.Errorf("EmbedMemory should return nil when name is empty, got %v", err)
	}

	// Verify nothing was stored
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}
	if store.Size() != 0 {
		t.Errorf("expected 0 records when name is empty, got %d", store.Size())
	}
}

// TestEmbedMemory_EmptyContent verifies that passing empty content returns nil
// without panicking.
func TestEmbedMemory_EmptyContent(t *testing.T) {
	ctx := context.Background()

	// Create a temporary directory for test isolation
	tempDir := t.TempDir()

	// Set config env vars to use temp dir
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("LEDIT_CONFIG", tempDir)

	// Create EmbeddingManager with minimal config
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: tempDir,
	}

	mgr := embedding.NewEmbeddingManager(cfg, tempDir)

	// Initialize the manager
	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("failed to initialize embedding manager: %v", err)
	}
	defer mgr.Close()

	// Call with empty content — should return nil, no panic
	if err := EmbedMemory(ctx, mgr, "some-name", ""); err != nil {
		t.Errorf("EmbedMemory should return nil when content is empty, got %v", err)
	}

	// Verify nothing was stored
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}
	if store.Size() != 0 {
		t.Errorf("expected 0 records when content is empty, got %d", store.Size())
	}
}

// ─── UnembedMemory tests (graceful failure paths) ───

// TestUnembedMemory_NilManager verifies that passing nil manager returns nil
// without panicking.
func TestUnembedMemory_NilManager(t *testing.T) {
	ctx := context.Background()

	// Call with nil manager — should return nil, no panic
	if err := UnembedMemory(ctx, nil, "some-name"); err != nil {
		t.Errorf("UnembedMemory should return nil when manager is nil, got %v", err)
	}
}

// TestUnembedMemory_NilContext verifies that passing nil context returns nil
// without panicking.
func TestUnembedMemory_NilContext(t *testing.T) {
	// Create a temporary directory for test isolation
	tempDir := t.TempDir()

	// Set config env vars to use temp dir
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("LEDIT_CONFIG", tempDir)

	// Create EmbeddingManager with minimal config
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: tempDir,
	}

	mgr := embedding.NewEmbeddingManager(cfg, tempDir)

	// Initialize the manager
	if err := mgr.Init(context.Background()); err != nil {
		t.Fatalf("failed to initialize embedding manager: %v", err)
	}
	defer mgr.Close()

	// Call with nil context — should return nil, no panic
	if err := UnembedMemory(nil, mgr, "some-name"); err != nil {
		t.Errorf("UnembedMemory should return nil when context is nil, got %v", err)
	}
}

// TestUnembedMemory_EmptyName verifies that passing an empty name returns nil
// without panicking.
func TestUnembedMemory_EmptyName(t *testing.T) {
	ctx := context.Background()

	// Create a temporary directory for test isolation
	tempDir := t.TempDir()

	// Set config env vars to use temp dir
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("LEDIT_CONFIG", tempDir)

	// Create EmbeddingManager with minimal config
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: tempDir,
	}

	mgr := embedding.NewEmbeddingManager(cfg, tempDir)

	// Initialize the manager
	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("failed to initialize embedding manager: %v", err)
	}
	defer mgr.Close()

	// Call with empty name — should return nil, no panic
	if err := UnembedMemory(ctx, mgr, ""); err != nil {
		t.Errorf("UnembedMemory should return nil when name is empty, got %v", err)
	}
}

// ─── Integration round-trip test ───

// TestMemoryEmbedding_RoundTrip tests the full save→query→delete flow.
// It saves a memory, verifies it's queryable from the conversation store
// using store.Query, then deletes it and verifies it's gone.
func TestMemoryEmbedding_RoundTrip(t *testing.T) {
	ctx := context.Background()

	// Create a temporary directory for test isolation
	tempDir := t.TempDir()

	// Set config env vars to use temp dir
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("LEDIT_CONFIG", tempDir)

	// Create EmbeddingManager with minimal config
	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: tempDir,
	}

	mgr := embedding.NewEmbeddingManager(cfg, tempDir)

	// Initialize the manager
	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("failed to initialize embedding manager: %v", err)
	}
	defer mgr.Close()

	// Get the conversation store
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}

	// Step 1: Save a memory with embedding
	memName := "round-trip-memory"
	memContent := "# Important Project Note\n\nAlways use Go modules for dependency management."

	if err := SaveMemoryWithEmbedding(ctx, mgr, memName, memContent); err != nil {
		t.Fatalf("SaveMemoryWithEmbedding failed: %v", err)
	}

	// Verify file exists
	filePath := filepath.Join(tempDir, "memories", memName+".md")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatal("memory file should exist after save")
	}

	// Verify record exists in store
	if store.Size() != 1 {
		t.Fatalf("expected 1 record after save, got %d", store.Size())
	}

	// Step 2: Query the store to find the memory
	allRecords, err := store.LoadAll()
	if err != nil {
		t.Fatalf("failed to load records: %v", err)
	}

	if len(allRecords) != 1 {
		t.Fatalf("expected 1 record, got %d", len(allRecords))
	}

	record := allRecords[0]

	// Use the stored embedding as query vector to verify queryability
	results, err := store.Query(record.Embedding, 5, 0.0)
	if err != nil {
		t.Fatalf("failed to query store: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected at least one query result, got 0")
	}

	// The top result should be our memory
	topResult := results[0]
	if topResult.Record.ID != memName {
		t.Errorf("expected top result ID %q, got %q", memName, topResult.Record.ID)
	}

	// Verify similarity is non-trivial (self-query should be very high)
	if topResult.Similarity <= 0 {
		t.Errorf("expected similarity > 0, got %f", topResult.Similarity)
	}

	// Verify record type
	if topResult.Record.Type != "memory" {
		t.Errorf("expected record type 'memory', got %q", topResult.Record.Type)
	}

	// Step 3: Delete the memory
	if err := DeleteMemoryWithEmbedding(ctx, mgr, memName); err != nil {
		t.Fatalf("DeleteMemoryWithEmbedding failed: %v", err)
	}

	// Verify file is gone
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("memory file should be deleted")
	}

	// Verify record is gone from store
	if store.Size() != 0 {
		t.Errorf("expected 0 records after deletion, got %d", store.Size())
	}

	// Verify query returns no results
	results, err = store.Query(record.Embedding, 5, 0.0)
	if err != nil {
		t.Fatalf("failed to query store after deletion: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected 0 query results after deletion, got %d", len(results))
	}
}
