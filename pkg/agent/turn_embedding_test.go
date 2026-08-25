package agent

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// initEmbeddingMgrWithTimeout bounds mgr.Init so a stuck ONNX runtime / model
// download fails the call within a finite window instead of hanging the whole
// pkg/agent suite. Each caller still gets the "ONNX runtime not available"
// skip branch via the returned error. See TODO SP-008-C1-testEmbedDownloadTimeout.
//
// NOTE: This helper is retained for the remaining ONNX integration tests in
// memory_embedding_test.go and proactive_context_test.go. The turn_embedding
// tests now use newTestEmbeddingMgr (mock provider) instead.
func initEmbeddingMgrWithTimeout(parent context.Context, mgr *embedding.EmbeddingManager) error {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	return mgr.Init(ctx)
}

// --- Mock embedding provider ---

// mockEmbeddingProvider produces deterministic, content-dependent embeddings
// for testing without requiring the ONNX runtime. Each byte of the input text
// is distributed across the vector dimensions, so different texts yield
// different vectors and identical texts yield identical vectors.
type mockEmbeddingProvider struct {
	dims int
}

func newMockEmbeddingProvider(dims int) *mockEmbeddingProvider {
	return &mockEmbeddingProvider{dims: dims}
}

// embedText is the shared deterministic embedding routine.
func (m *mockEmbeddingProvider) embedText(text string) []float32 {
	vec := make([]float32, m.dims)
	if len(text) == 0 {
		return vec
	}
	for i := 0; i < len(text); i++ {
		dim := i % m.dims
		vec[dim] += float32(text[i]) / 255.0
	}
	return vec
}

func (m *mockEmbeddingProvider) Embed(_ context.Context, text string) ([]float32, error) {
	return m.embedText(text), nil
}

func (m *mockEmbeddingProvider) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, t := range texts {
		results[i] = m.embedText(t)
	}
	return results, nil
}

func (m *mockEmbeddingProvider) EmbedWithPrefix(_ context.Context, text string, prefix string) ([]float32, error) {
	return m.embedText(prefix + text), nil
}

func (m *mockEmbeddingProvider) EmbedBatchWithPrefix(_ context.Context, texts []string, prefix string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, t := range texts {
		results[i] = m.embedText(prefix + t)
	}
	return results, nil
}

func (m *mockEmbeddingProvider) Dimensions() int   { return m.dims }
func (m *mockEmbeddingProvider) Name() string      { return "mock" }
func (m *mockEmbeddingProvider) ModelHash() string { return "mock-model-hash" }
func (m *mockEmbeddingProvider) Close() error      { return nil }

// --- Test helper ---

// newTestEmbeddingMgr builds an EmbeddingManager wired with a mock embedding
// provider via SetForTesting. This bypasses ONNX initialization entirely so
// tests run in any environment without the ~300MB model files.
//
// Config isolation is handled by setting SPROUT_CONFIG and SPROUT_CONFIG to a
// temp directory so no real config is touched.
func newTestEmbeddingMgr(t *testing.T) *embedding.EmbeddingManager {
	t.Helper()

	tempDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tempDir)

	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: tempDir,
	}
	mgr := embedding.NewEmbeddingManager(cfg, tempDir)

	// Wire mock provider + real stores via SetForTesting.
	provider := newMockEmbeddingProvider(128)
	store, err := embedding.NewHNSWStore(filepath.Join(tempDir, "index.hnsw"), provider.ModelHash())
	if err != nil {
		t.Fatalf("failed to create HNSW store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	indexMgr := embedding.NewIndexManager(provider, store, embedding.IndexOptions{
		BatchSize:  32,
		MaxBodyLen: 2000,
	})
	mgr.SetForTesting(provider, store, indexMgr)

	return mgr
}

// --- Tests for EmbedAndStoreTurn ---

// TestEmbedAndStoreTurn_RealProvider tests the full flow with a mock provider.
// It creates an EmbeddingManager with a mock provider, calls EmbedAndStoreTurn,
// and verifies the record was stored by loading it back from the conversation store.
func TestEmbedAndStoreTurn_RealProvider(t *testing.T) {
	ctx := context.Background()
	mgr := newTestEmbeddingMgr(t)
	defer mgr.Close()

	// Create a test conversation turn
	turn, err := NewConversationTurn("test-session", 1, "How do I implement a REST API in Go?", "/tmp/workspace")
	if err != nil {
		t.Fatalf("failed to create conversation turn: %v", err)
	}
	turn.ActionableSummary = "Implement a REST API using net/http package with handlers for GET and POST endpoints"

	// Call EmbedAndStoreTurn
	if err := EmbedAndStoreTurn(ctx, mgr, turn, ""); err != nil {
		t.Errorf("EmbedAndStoreTurn returned unexpected error: %v", err)
	}

	// Verify that the turn's PromptEmbedding was set
	if turn.PromptEmbedding == nil {
		t.Error("PromptEmbedding was not set after EmbedAndStoreTurn")
	}

	// Verify the embedding was stored by loading it back from the conversation store
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}

	allRecords, err := store.LoadAll()
	if err != nil {
		t.Fatalf("failed to load records from conversation store: %v", err)
	}

	if len(allRecords) != 1 {
		t.Errorf("expected 1 record in conversation store, got %d", len(allRecords))
	}

	record := allRecords[0]
	if record.ID != turn.ID {
		t.Errorf("expected record ID %s, got %s", turn.ID, record.ID)
	}

	if record.Type != "conversation_turn" {
		t.Errorf("expected record type 'conversation_turn', got '%s'", record.Type)
	}

	if record.Metadata == nil {
		t.Error("expected record metadata to be non-nil")
	} else {
		if sessionID, ok := record.Metadata["sessionId"].(string); !ok || sessionID != turn.SessionID {
			t.Errorf("expected sessionId %s in metadata, got %v", turn.SessionID, record.Metadata["sessionId"])
		}
	}
}

// TestEmbedAndStoreTurn_EmptySummary tests the case where only the prompt is embedded
// because the actionable summary is empty.
func TestEmbedAndStoreTurn_EmptySummary(t *testing.T) {
	ctx := context.Background()
	mgr := newTestEmbeddingMgr(t)
	defer mgr.Close()

	// Create a test conversation turn without an actionable summary
	turn, err := NewConversationTurn("test-session", 1, "How do I declare a variable in Go?", "/tmp/workspace")
	if err != nil {
		t.Fatalf("failed to create conversation turn: %v", err)
	}
	// Leave ActionableSummary empty

	// Call EmbedAndStoreTurn
	if err := EmbedAndStoreTurn(ctx, mgr, turn, ""); err != nil {
		t.Errorf("EmbedAndStoreTurn returned unexpected error: %v", err)
	}

	// Verify that the turn's PromptEmbedding was set
	if turn.PromptEmbedding == nil {
		t.Error("PromptEmbedding was not set after EmbedAndStoreTurn")
	}

	// Verify the embedding was stored by loading it back from the conversation store
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}

	allRecords, err := store.LoadAll()
	if err != nil {
		t.Fatalf("failed to load records from conversation store: %v", err)
	}

	if len(allRecords) != 1 {
		t.Errorf("expected 1 record in conversation store, got %d", len(allRecords))
	}

	// Verify the stored record matches the turn
	record := allRecords[0]
	if record.ID != turn.ID {
		t.Errorf("expected record ID %s, got %s", turn.ID, record.ID)
	}
}

// TestEmbedAndStoreTurn_GracefulFailure_NilManager tests graceful failure when
// the embedding manager is nil.
func TestEmbedAndStoreTurn_GracefulFailure_NilManager(t *testing.T) {
	ctx := context.Background()

	// Create a test conversation turn
	turn, err := NewConversationTurn("test-session", 1, "test prompt", "/tmp/workspace")
	if err != nil {
		t.Fatalf("failed to create conversation turn: %v", err)
	}

	// Call EmbedAndStoreTurn with nil manager - should not panic or return error
	if err := EmbedAndStoreTurn(ctx, nil, turn, ""); err != nil {
		t.Errorf("EmbedAndStoreTurn should return nil on graceful failure, got %v", err)
	}

	// Verify PromptEmbedding was not set
	if turn.PromptEmbedding != nil {
		t.Error("PromptEmbedding should remain nil when manager is nil")
	}
}

// TestEmbedAndStoreTurn_GracefulFailure_NilTurn tests graceful failure when
// the conversation turn is nil.
func TestEmbedAndStoreTurn_GracefulFailure_NilTurn(t *testing.T) {
	ctx := context.Background()
	mgr := newTestEmbeddingMgr(t)
	defer mgr.Close()

	// Call EmbedAndStoreTurn with nil turn - should not panic or return error
	if err := EmbedAndStoreTurn(ctx, mgr, nil, ""); err != nil {
		t.Errorf("EmbedAndStoreTurn should return nil on graceful failure, got %v", err)
	}
}

// TestEmbedAndStoreTurn_GracefulFailure_NilContext tests graceful failure when
// the context is nil.
func TestEmbedAndStoreTurn_GracefulFailure_NilContext(t *testing.T) {
	mgr := newTestEmbeddingMgr(t)
	defer mgr.Close()

	// Create a test conversation turn
	turn, err := NewConversationTurn("test-session", 1, "test prompt", "/tmp/workspace")
	if err != nil {
		t.Fatalf("failed to create conversation turn: %v", err)
	}

	// Call EmbedAndStoreTurn with nil context - should not panic or return error
	if err := EmbedAndStoreTurn(nil, mgr, turn, ""); err != nil {
		t.Errorf("EmbedAndStoreTurn should return nil on graceful failure, got %v", err)
	}

	// Verify PromptEmbedding was not set
	if turn.PromptEmbedding != nil {
		t.Error("PromptEmbedding should remain nil when context is nil")
	}
}

// TestEmbedAndStoreTurn_GracefulFailure_CancelledContext tests graceful failure when
// the context is cancelled before embedding completes.
func TestEmbedAndStoreTurn_GracefulFailure_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	mgr := newTestEmbeddingMgr(t)
	defer mgr.Close()

	// Create a test conversation turn
	turn, err := NewConversationTurn("test-session", 1, "test prompt", "/tmp/workspace")
	if err != nil {
		t.Fatalf("failed to create conversation turn: %v", err)
	}

	// Call EmbedAndStoreTurn with cancelled context - should handle gracefully.
	// The mock provider does not check context cancellation (it returns
	// instantly), so the function may succeed and store the record. The
	// contract being tested is that no error is returned either way.
	if err := EmbedAndStoreTurn(ctx, mgr, turn, ""); err != nil {
		t.Errorf("EmbedAndStoreTurn should return nil on graceful failure, got %v", err)
	}

	// PromptEmbedding may or may not be set depending on when the cancellation is processed,
	// but the function should not return an error
}

// TestEmbedAndStoreTurn_GracefulFailure_EmptyPrompt tests graceful failure when
// the user prompt is empty.
func TestEmbedAndStoreTurn_GracefulFailure_EmptyPrompt(t *testing.T) {
	ctx := context.Background()
	mgr := newTestEmbeddingMgr(t)
	defer mgr.Close()

	// Create a turn with empty prompt
	turn := &ConversationTurn{
		ID:         "test-empty-prompt",
		SessionID:  "test-session",
		TurnNumber: 1,
		UserPrompt: "",
		WorkingDir: "/tmp/workspace",
	}

	if err := EmbedAndStoreTurn(ctx, mgr, turn, ""); err != nil {
		t.Errorf("EmbedAndStoreTurn should return nil on graceful failure, got %v", err)
	}

	// Verify nothing was stored
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}
	if store.Size() != 0 {
		t.Errorf("expected 0 records with empty prompt, got %d", store.Size())
	}

	// Verify PromptEmbedding was not set
	if turn.PromptEmbedding != nil {
		t.Error("PromptEmbedding should remain nil when prompt is empty")
	}
}

// TestMeanEmbedding_SameLength tests the meanEmbedding helper with vectors of the same length.
func TestMeanEmbedding_SameLength(t *testing.T) {
	a := []float32{1.0, 2.0, 3.0}
	b := []float32{3.0, 5.0, 7.0}

	result := meanEmbedding(a, b)

	expected := []float32{2.0, 3.5, 5.0}
	if len(result) != len(expected) {
		t.Fatalf("expected result length %d, got %d", len(expected), len(result))
	}
	for i := range expected {
		if result[i] != expected[i] {
			t.Errorf("at index %d: expected %f, got %f", i, expected[i], result[i])
		}
	}

	// Verify original slices are not modified
	if a[0] != 1.0 {
		t.Error("original slice a was modified")
	}
	if b[0] != 3.0 {
		t.Error("original slice b was modified")
	}
}

// TestMeanEmbedding_DifferentLength tests the meanEmbedding helper with vectors of different lengths.
func TestMeanEmbedding_DifferentLength(t *testing.T) {
	a := []float32{1.0, 2.0}
	b := []float32{3.0, 5.0, 7.0, 9.0}

	result := meanEmbedding(a, b)

	// Should return a copy of the longer vector (b)
	if len(result) != len(b) {
		t.Fatalf("expected result length %d, got %d", len(b), len(result))
	}
	for i := range b {
		if result[i] != b[i] {
			t.Errorf("at index %d: expected %f, got %f", i, b[i], result[i])
		}
	}
}

// TestMeanEmbedding_FirstLonger tests the meanEmbedding helper when the first vector is longer.
func TestMeanEmbedding_FirstLonger(t *testing.T) {
	a := []float32{1.0, 2.0, 3.0, 4.0}
	b := []float32{5.0, 6.0}

	result := meanEmbedding(a, b)

	// Should return a copy of the longer vector (a)
	if len(result) != len(a) {
		t.Fatalf("expected result length %d, got %d", len(a), len(result))
	}
	for i := range a {
		if result[i] != a[i] {
			t.Errorf("at index %d: expected %f, got %f", i, a[i], result[i])
		}
	}
}

// TestMeanEmbedding_EmptyVectors tests the meanEmbedding helper with empty vectors.
func TestMeanEmbedding_EmptyVectors(t *testing.T) {
	a := []float32{}
	b := []float32{}

	result := meanEmbedding(a, b)

	if len(result) != 0 {
		t.Errorf("expected empty result, got length %d", len(result))
	}
}

// TestMeanEmbedding_OneEmpty tests the meanEmbedding helper with one empty vector.
func TestMeanEmbedding_OneEmpty(t *testing.T) {
	a := []float32{1.0, 2.0, 3.0}
	b := []float32{}

	result := meanEmbedding(a, b)

	// Should return a copy of the non-empty vector
	if len(result) != len(a) {
		t.Fatalf("expected result length %d, got %d", len(a), len(result))
	}
	for i := range a {
		if result[i] != a[i] {
			t.Errorf("at index %d: expected %f, got %f", i, a[i], result[i])
		}
	}
}

// TestMeanEmbedding_NegativeValues tests the meanEmbedding helper with negative values.
func TestMeanEmbedding_NegativeValues(t *testing.T) {
	a := []float32{-2.0, 0.0, 2.0}
	b := []float32{-4.0, 0.0, 4.0}

	result := meanEmbedding(a, b)

	expected := []float32{-3.0, 0.0, 3.0}
	if len(result) != len(expected) {
		t.Fatalf("expected result length %d, got %d", len(expected), len(result))
	}
	for i := range expected {
		if result[i] != expected[i] {
			t.Errorf("at index %d: expected %f, got %f", i, expected[i], result[i])
		}
	}
}

// TestEmbedAndStoreTurn_RoundTripWithQuery tests the full embed→store→query round-trip.
// It creates and stores two conversation turns, then queries the store using one
// turn's embedding and verifies the results match expectations.
func TestEmbedAndStoreTurn_RoundTripWithQuery(t *testing.T) {
	ctx := context.Background()
	mgr := newTestEmbeddingMgr(t)
	defer mgr.Close()

	// Create and embed/store the first conversation turn
	turn1, err := NewConversationTurn("test-query-session", 1, "How do I implement a REST API in Go?", "/tmp/workspace")
	if err != nil {
		t.Fatalf("failed to create first conversation turn: %v", err)
	}
	turn1.ActionableSummary = "Implement a REST API using net/http package with handlers for GET and POST endpoints"

	if err := EmbedAndStoreTurn(ctx, mgr, turn1, ""); err != nil {
		t.Errorf("EmbedAndStoreTurn returned unexpected error for turn1: %v", err)
	}

	// Verify that the turn's PromptEmbedding was set
	if turn1.PromptEmbedding == nil {
		t.Error("PromptEmbedding was not set after EmbedAndStoreTurn for turn1")
	}

	// Create and embed/store a second conversation turn with different content
	turn2, err := NewConversationTurn("test-query-session", 2, "What is the difference between channels and mutexes in Go?", "/tmp/workspace")
	if err != nil {
		t.Fatalf("failed to create second conversation turn: %v", err)
	}
	turn2.ActionableSummary = "Channels are for communication between goroutines, mutexes are for protecting shared state from concurrent access"

	if err := EmbedAndStoreTurn(ctx, mgr, turn2, ""); err != nil {
		t.Errorf("EmbedAndStoreTurn returned unexpected error for turn2: %v", err)
	}

	// Verify that the turn's PromptEmbedding was set
	if turn2.PromptEmbedding == nil {
		t.Error("PromptEmbedding was not set after EmbedAndStoreTurn for turn2")
	}

	// Get the conversation store
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}

	// Query the store using the first turn's embedding as the query vector
	results, err := store.Query(turn1.PromptEmbedding, 5, 0.0)
	if err != nil {
		t.Fatalf("failed to query conversation store: %v", err)
	}

	// Verify query returned at least one result
	if len(results) == 0 {
		t.Fatal("expected at least one result from query, got 0")
	}

	// The first result should be the most similar (turn1 itself, since we're using its embedding)
	firstResult := results[0]

	// Verify the top-ranked result is turn1 (querying with its own embedding should rank it first)
	if firstResult.Record.ID != turn1.ID {
		t.Errorf("expected first result to be turn1 (ID %s), got %s", turn1.ID, firstResult.Record.ID)
	}

	// Verify similarity score is non-trivial (> 0)
	if firstResult.Similarity <= 0 {
		t.Errorf("expected similarity > 0, got %f", firstResult.Similarity)
	}

	// Verify the stored VectorRecord has correct fields
	record := firstResult.Record
	if record.Type != "conversation_turn" {
		t.Errorf("expected record type 'conversation_turn', got '%s'", record.Type)
	}

	// Verify Signature contains the prompt text
	if record.Signature == "" {
		t.Error("expected non-empty Signature field")
	}
	// The signature should contain part of the prompt (truncated to maxSignatureLen)
	promptSnippet := "REST API"
	if !strings.Contains(record.Signature, promptSnippet) {
		t.Errorf("expected Signature to contain prompt snippet '%s', got '%s'", promptSnippet, record.Signature)
	}

	// Verify Metadata has the expected fields
	if record.Metadata == nil {
		t.Fatal("expected record metadata to be non-nil")
	}

	// Check actionableSummary in metadata
	if summary, ok := record.Metadata["actionableSummary"].(string); !ok || summary == "" {
		t.Errorf("expected actionableSummary in metadata, got %v", record.Metadata["actionableSummary"])
	}

	// Check sessionId in metadata
	if sessionID, ok := record.Metadata["sessionId"].(string); !ok || sessionID != turn1.SessionID {
		t.Errorf("expected sessionId %s in metadata, got %v", turn1.SessionID, record.Metadata["sessionId"])
	}

	// Check turnNumber in metadata
	if turnNum, ok := record.Metadata["turnNumber"].(int); !ok || turnNum != turn1.TurnNumber {
		t.Errorf("expected turnNumber %d in metadata, got %v", turn1.TurnNumber, record.Metadata["turnNumber"])
	}

	// Load all records to verify both turns are present
	allRecords, err := store.LoadAll()
	if err != nil {
		t.Fatalf("failed to load all records from conversation store: %v", err)
	}

	if len(allRecords) != 2 {
		t.Errorf("expected 2 records in conversation store, got %d", len(allRecords))
	}

	// Verify both turn IDs are present and embeddings are non-nil
	ids := make(map[string]bool)
	for _, rec := range allRecords {
		ids[rec.ID] = true
		if rec.Embedding == nil {
			t.Errorf("expected non-nil embedding for record ID %s", rec.ID)
		}
	}

	if !ids[turn1.ID] {
		t.Errorf("expected to find turn1 ID %s in records", turn1.ID)
	}
	if !ids[turn2.ID] {
		t.Errorf("expected to find turn2 ID %s in records", turn2.ID)
	}
}

// TestEmbedAndStoreTurn_StampsCheckpointID proves that passing a non-empty
// checkpointID ends up in the stored record's metadata. This is the
// production-side complement to TestRefineRollupEnd_FindsTopicShift: that
// test asserts the boundary detector finds records when metadata is set
// correctly; this one proves the production code sets it correctly.
func TestEmbedAndStoreTurn_StampsCheckpointID(t *testing.T) {
	ctx := context.Background()
	mgr := newTestEmbeddingMgr(t)
	defer mgr.Close()

	turn, err := NewConversationTurn("sess-cp", 1, "fix the auth bug", "/tmp/workspace")
	if err != nil {
		t.Fatalf("failed to create turn: %v", err)
	}

	if err := EmbedAndStoreTurn(ctx, mgr, turn, "cp-test-stamp"); err != nil {
		t.Fatalf("EmbedAndStoreTurn failed: %v", err)
	}

	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("get conversation store: %v", err)
	}
	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("load all records: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 record, got %d", len(all))
	}

	got, ok := all[0].Metadata["checkpoint_id"].(string)
	if !ok {
		t.Fatalf("metadata[\"checkpoint_id\"] missing or wrong type: %#v", all[0].Metadata["checkpoint_id"])
	}
	if got != "cp-test-stamp" {
		t.Errorf("checkpoint_id = %q, want %q", got, "cp-test-stamp")
	}
}

// TestEmbedAndStoreTurn_OmitsEmptyCheckpointID confirms we don't pollute
// metadata with empty-string sentinel values. collectCheckpointVectors
// treats `cid == ""` as a skip, so an empty-string key would confuse the
// lookup logic.
func TestEmbedAndStoreTurn_OmitsEmptyCheckpointID(t *testing.T) {
	ctx := context.Background()
	mgr := newTestEmbeddingMgr(t)
	defer mgr.Close()

	turn, err := NewConversationTurn("sess-no-cp", 1, "set up CI", "/tmp/workspace")
	if err != nil {
		t.Fatalf("failed to create turn: %v", err)
	}

	if err := EmbedAndStoreTurn(ctx, mgr, turn, ""); err != nil {
		t.Fatalf("EmbedAndStoreTurn failed: %v", err)
	}

	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("get conversation store: %v", err)
	}
	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("load all records: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 record, got %d", len(all))
	}

	if _, present := all[0].Metadata["checkpoint_id"]; present {
		t.Errorf("metadata[\"checkpoint_id\"] should be absent when caller passed empty string, got: %#v", all[0].Metadata["checkpoint_id"])
	}
}

// TestEmbedAndStoreTurn_GracefulFailure_ProviderUnavailable tests graceful failure
// when the embedding manager points to an unwritable directory and cannot initialize.
func TestEmbedAndStoreTurn_GracefulFailure_ProviderUnavailable(t *testing.T) {
	ctx := context.Background()

	// For the unwritable-dir approach, /proc/nonexistent only exists on Linux.
	// On other platforms, create a temp dir and remove permissions instead.
	var unwritableDir string
	if runtime.GOOS == "linux" {
		unwritableDir = "/proc/nonexistent/embeddings"
	} else {
		// Create a temp dir and remove write permissions to simulate unwritable path
		readOnlyDir := filepath.Join(t.TempDir(), "readonly")
		if err := os.MkdirAll(readOnlyDir, 0o555); err != nil {
			t.Fatalf("failed to create read-only dir: %v", err)
		}
		unwritableDir = filepath.Join(readOnlyDir, "nested", "embeddings")
	}

	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: unwritableDir,
	}

	mgr := embedding.NewEmbeddingManager(cfg, unwritableDir)

	// Verify init fails — if it unexpectedly succeeds, the test premise is invalid.
	if err := mgr.Init(ctx); err == nil {
		t.Skip("provider initialized despite unwritable dir; skipping platform-specific test")
	}
	defer mgr.Close()

	// Create a valid ConversationTurn
	turn, err := NewConversationTurn("test-unavailable", 1, "Test prompt with unavailable provider", "/tmp/workspace")
	if err != nil {
		t.Fatalf("failed to create conversation turn: %v", err)
	}
	turn.ActionableSummary = "Test summary"

	// Call EmbedAndStoreTurn - should return nil (graceful failure, not an error)
	if err := EmbedAndStoreTurn(ctx, mgr, turn, ""); err != nil {
		t.Errorf("EmbedAndStoreTurn should return nil on graceful failure, got %v", err)
	}

	// Verify the turn's PromptEmbedding is still nil (nothing was embedded)
	if turn.PromptEmbedding != nil {
		t.Error("PromptEmbedding should remain nil when provider is unavailable")
	}

	// Optionally verify that store cannot be retrieved or is empty
	store, err := mgr.GetConversationStore(ctx)
	if err == nil && store != nil {
		// Store was available, verify nothing was stored
		if store.Size() != 0 {
			t.Errorf("expected 0 records with unavailable provider, got %d", store.Size())
		}
	}
}
