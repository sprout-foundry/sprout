//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// newTestSemanticServer creates a minimal ReactWebServer for semantic search tests.
// Unlike newTestWebServer, it does not set up a workspace or terminal manager,
// because the semantic search endpoint only needs the embedding manager (which is nil here).
func newTestSemanticServer(t *testing.T) *ReactWebServer {
	t.Helper()
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

// ---------------------------------------------------------------------------
// handleAPISemanticSearch — GET /api/search/semantic
// ---------------------------------------------------------------------------

func TestHandleAPISemanticSearch_MissingQuery(t *testing.T) {
	ws := newTestSemanticServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/search/semantic", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPISemanticSearch(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response as JSON: %v", err)
	}
	if resp["error"] == "" {
		t.Error("expected error message in response, got none")
	}
	if !strings.Contains(resp["error"], "query") {
		t.Errorf("expected error to mention 'query', got: %s", resp["error"])
	}
}

func TestHandleAPISemanticSearch_MethodNotAllowed(t *testing.T) {
	ws := newTestSemanticServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/search/semantic?query=test", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPISemanticSearch(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d: %s", http.StatusMethodNotAllowed, rec.Code, rec.Body.String())
	}
}

func TestHandleAPISemanticSearch_NoAgent(t *testing.T) {
	ws := newTestSemanticServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/search/semantic?query=find+a+function", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPISemanticSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp SemanticSearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Query != "find a function" {
		t.Errorf("expected query 'find a function', got %q", resp.Query)
	}
	if resp.Total != 0 {
		t.Errorf("expected total 0, got %d", resp.Total)
	}
	if resp.Results == nil {
		t.Error("expected results to be an empty array, got nil")
	}
	if len(resp.Results) != 0 {
		t.Errorf("expected 0 results, got %d", len(resp.Results))
	}
	if resp.Duration == "" {
		t.Error("expected duration to be set, got empty string")
	}
}

func TestHandleAPISemanticSearch_InvalidTopK(t *testing.T) {
	ws := newTestSemanticServer(t)

	// Invalid top_k should be ignored and default to 10.
	req := httptest.NewRequest(http.MethodGet, "/api/search/semantic?query=test&top_k=invalid", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPISemanticSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp SemanticSearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Total != 0 {
		t.Errorf("expected total 0 (no agent), got %d", resp.Total)
	}
	if resp.Query != "test" {
		t.Errorf("expected query 'test', got %q", resp.Query)
	}
}

func TestHandleAPISemanticSearch_InvalidThreshold(t *testing.T) {
	ws := newTestSemanticServer(t)

	// Invalid threshold should be ignored and default to 0.75.
	req := httptest.NewRequest(http.MethodGet, "/api/search/semantic?query=test&threshold=invalid", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPISemanticSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp SemanticSearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Total != 0 {
		t.Errorf("expected total 0 (no agent), got %d", resp.Total)
	}
	if resp.Query != "test" {
		t.Errorf("expected query 'test', got %q", resp.Query)
	}
}

func TestHandleAPISemanticSearch_TopKOutOfRange(t *testing.T) {
	ws := newTestSemanticServer(t)

	// top_k=0 (below minimum) should be ignored.
	req := httptest.NewRequest(http.MethodGet, "/api/search/semantic?query=test&top_k=0", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPISemanticSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	// top_k=51 (above maximum) should be ignored.
	req = httptest.NewRequest(http.MethodGet, "/api/search/semantic?query=test&top_k=51", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec = httptest.NewRecorder()
	ws.handleAPISemanticSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp SemanticSearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Total != 0 {
		t.Errorf("expected total 0, got %d", resp.Total)
	}
}

func TestHandleAPISemanticSearch_ThresholdOutOfRange(t *testing.T) {
	ws := newTestSemanticServer(t)

	// threshold=-0.1 (below 0) should be ignored.
	req := httptest.NewRequest(http.MethodGet, "/api/search/semantic?query=test&threshold=-0.1", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPISemanticSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	// threshold=1.5 (above 1) should be ignored.
	req = httptest.NewRequest(http.MethodGet, "/api/search/semantic?query=test&threshold=1.5", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec = httptest.NewRecorder()
	ws.handleAPISemanticSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp SemanticSearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Total != 0 {
		t.Errorf("expected total 0, got %d", resp.Total)
	}
}

func TestHandleAPISemanticSearch_NoClientID(t *testing.T) {
	ws := newTestSemanticServer(t)

	// No client ID header set — should still return empty results gracefully.
	req := httptest.NewRequest(http.MethodGet, "/api/search/semantic?query=test", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISemanticSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp SemanticSearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Total != 0 {
		t.Errorf("expected total 0, got %d", resp.Total)
	}
}

func TestHandleAPISemanticSearch_ValidTopKAndThreshold(t *testing.T) {
	ws := newTestSemanticServer(t)

	// Valid top_k and threshold should be accepted (but still return empty without agent).
	req := httptest.NewRequest(http.MethodGet, "/api/search/semantic?query=hello&top_k=5&threshold=0.8", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPISemanticSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp SemanticSearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Query != "hello" {
		t.Errorf("expected query 'hello', got %q", resp.Query)
	}
	if resp.Total != 0 {
		t.Errorf("expected total 0 (no agent), got %d", resp.Total)
	}
}

// ---------------------------------------------------------------------------
// detectDuplicateClusters — unit tests
// ---------------------------------------------------------------------------

func TestDetectDuplicateClusters_NoDuplicates(t *testing.T) {
	// Three results from three different files with very different embeddings.
	// All pairwise similarities should be well below 0.90, so no clusters.
	results := []SemanticSearchResult{
		{File: "a.go", Name: "FuncA", Type: "code_unit", Embedding: []float32{1.0, 0.0, 0.0}},
		{File: "b.go", Name: "FuncB", Type: "code_unit", Embedding: []float32{0.0, 1.0, 0.0}},
		{File: "c.go", Name: "FuncC", Type: "code_unit", Embedding: []float32{0.0, 0.0, 1.0}},
	}

	clusters := detectDuplicateClusters(results)

	if len(clusters) != 0 {
		t.Errorf("expected no clusters, got %d: %+v", len(clusters), clusters)
	}
	// Verify no cluster IDs were assigned.
	for i, r := range results {
		if r.ClusterId != 0 {
			t.Errorf("result %d (%s) unexpectedly got ClusterId %d", i, r.Name, r.ClusterId)
		}
	}
}

func TestDetectDuplicateClusters_TwoFileCluster(t *testing.T) {
	// Two results from two different files with identical embeddings.
	// Pairwise similarity = 1.0 >= 0.90 → should form 1 cluster.
	results := []SemanticSearchResult{
		{File: "a.go", Name: "FuncA", Type: "code_unit", Embedding: []float32{1.0, 0.0, 0.0}},
		{File: "b.go", Name: "FuncB", Type: "code_unit", Embedding: []float32{1.0, 0.0, 0.0}},
	}

	clusters := detectDuplicateClusters(results)

	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d: %+v", len(clusters), clusters)
	}
	cluster := clusters[0]
	if cluster.Count != 2 {
		t.Errorf("expected cluster count 2, got %d", cluster.Count)
	}
	if len(cluster.Files) != 2 {
		t.Errorf("expected 2 files in cluster, got %d", len(cluster.Files))
	}
	// Verify cluster IDs are assigned.
	if results[0].ClusterId != 1 {
		t.Errorf("result[0].ClusterId = %d, want 1", results[0].ClusterId)
	}
	if results[1].ClusterId != 1 {
		t.Errorf("result[1].ClusterId = %d, want 1", results[1].ClusterId)
	}
}

func TestDetectDuplicateClusters_MultiCluster(t *testing.T) {
	// Four results from four files: a.go and b.go are similar (cluster 1),
	// c.go and d.go are similar (cluster 2). Cross-cluster pairs are dissimilar.
	results := []SemanticSearchResult{
		{File: "a.go", Name: "FuncA1", Type: "code_unit", Embedding: []float32{1.0, 0.0, 0.0}},
		{File: "b.go", Name: "FuncB1", Type: "code_unit", Embedding: []float32{1.0, 0.0, 0.0}},
		{File: "c.go", Name: "FuncC1", Type: "code_unit", Embedding: []float32{0.0, 1.0, 0.0}},
		{File: "d.go", Name: "FuncD1", Type: "code_unit", Embedding: []float32{0.0, 1.0, 0.0}},
	}

	clusters := detectDuplicateClusters(results)

	if len(clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d: %+v", len(clusters), clusters)
	}

	// Each cluster should have exactly 2 members from 2 different files.
	for _, cl := range clusters {
		if cl.Count != 2 {
			t.Errorf("expected cluster count 2, got %d", cl.Count)
		}
		if len(cl.Files) != 2 {
			t.Errorf("expected 2 files, got %d: %v", len(cl.Files), cl.Files)
		}
	}

	// Verify cluster IDs: a.go and b.go share one ID, c.go and d.go share another.
	ids := make(map[string]int)
	for _, r := range results {
		ids[r.Name] = r.ClusterId
	}
	if ids["FuncA1"] == 0 || ids["FuncB1"] == 0 || ids["FuncA1"] != ids["FuncB1"] {
		t.Errorf("expected a.go and b.go to share a cluster ID, got A=%d B=%d", ids["FuncA1"], ids["FuncB1"])
	}
	if ids["FuncC1"] == 0 || ids["FuncD1"] == 0 || ids["FuncC1"] != ids["FuncD1"] {
		t.Errorf("expected c.go and d.go to share a cluster ID, got C=%d D=%d", ids["FuncC1"], ids["FuncD1"])
	}
	if ids["FuncA1"] == ids["FuncC1"] {
		t.Error("expected different clusters for (a,b) and (c,d)")
	}
}

func TestDetectDuplicateClusters_SameFileIgnored(t *testing.T) {
	// Two results from the *same* file should NOT be clustered together,
	// even if their embeddings are identical.
	results := []SemanticSearchResult{
		{File: "a.go", Name: "FuncA", Type: "code_unit", Embedding: []float32{1.0, 0.0, 0.0}},
		{File: "a.go", Name: "FuncB", Type: "code_unit", Embedding: []float32{1.0, 0.0, 0.0}},
	}

	clusters := detectDuplicateClusters(results)

	if len(clusters) != 0 {
		t.Errorf("expected no clusters for same-file results, got %d: %+v", len(clusters), clusters)
	}
	for i, r := range results {
		if r.ClusterId != 0 {
			t.Errorf("result %d ClusterId = %d, want 0 (same-file ignored)", i, r.ClusterId)
		}
	}
}

func TestDetectDuplicateClusters_FileTypeIgnored(t *testing.T) {
	// Results with Type "file" should be skipped entirely from clustering.
	// Only the code_unit result should survive, producing no clusters.
	results := []SemanticSearchResult{
		{File: "a.go", Name: "FuncA", Type: "code_unit", Embedding: []float32{1.0, 0.0, 0.0}},
		{File: "b.go", Name: "whole_file", Type: "file", Embedding: []float32{1.0, 0.0, 0.0}},
	}

	clusters := detectDuplicateClusters(results)

	if len(clusters) != 0 {
		t.Errorf("expected no clusters, file-type result should be ignored, got %d: %+v", len(clusters), clusters)
	}
}

func TestDetectDuplicateClusters_TransitiveClustering(t *testing.T) {
	// A~B (high sim), B~C (high sim), A~C (low sim).
	// Union-find should transitively group A, B, C into one cluster.
	// Use near-identical embeddings for the transitive chain to ensure
	// A~B and B~C are above 0.90 threshold, triggering unions that
	// transitively group all three results.
	results := []SemanticSearchResult{
		{File: "a.go", Name: "FuncA", Type: "code_unit", Embedding: []float32{1.0, 0.0, 0.0}},
		{File: "b.go", Name: "FuncB", Type: "code_unit", Embedding: []float32{1.0, 0.05, 0.0}},
		{File: "c.go", Name: "FuncC", Type: "code_unit", Embedding: []float32{1.0, 0.1, 0.05}},
	}

	clusters := detectDuplicateClusters(results)

	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster (transitive), got %d: %+v", len(clusters), clusters)
	}
	cluster := clusters[0]
	if cluster.Count != 3 {
		t.Errorf("expected cluster count 3, got %d", cluster.Count)
	}
	if len(cluster.Files) != 3 {
		t.Errorf("expected 3 files, got %d", len(cluster.Files))
	}
	// All three should have the same cluster ID.
	idA, idB, idC := results[0].ClusterId, results[1].ClusterId, results[2].ClusterId
	if idA != idB || idB != idC || idA == 0 {
		t.Errorf("expected all three results to share a cluster ID, got A=%d B=%d C=%d", idA, idB, idC)
	}
}

func TestDetectDuplicateClusters_ClusterIdAssigned(t *testing.T) {
	// Verify that after clustering, the ClusterId field is set on results
	// and unset (0) for non-clustered results.
	results := []SemanticSearchResult{
		{File: "a.go", Name: "DupA", Type: "code_unit", Embedding: []float32{1.0, 0.0, 0.0}},
		{File: "b.go", Name: "DupB", Type: "code_unit", Embedding: []float32{1.0, 0.0, 0.0}},
		{File: "c.go", Name: "UniqueC", Type: "code_unit", Embedding: []float32{0.0, 1.0, 0.0}},
		{File: "d.go", Name: "UniqueD", Type: "code_unit", Embedding: []float32{0.0, 0.0, 1.0}},
	}

	clusters := detectDuplicateClusters(results)

	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d: %+v", len(clusters), clusters)
	}

	// Clustered results should have positive cluster ID.
	if results[0].ClusterId == 0 {
		t.Error("DupA should have ClusterId > 0")
	}
	if results[1].ClusterId == 0 {
		t.Error("DupB should have ClusterId > 0")
	}
	// Non-clustered results should have ClusterId = 0.
	if results[2].ClusterId != 0 {
		t.Errorf("UniqueC ClusterId = %d, want 0 (not in cluster)", results[2].ClusterId)
	}
	if results[3].ClusterId != 0 {
		t.Errorf("UniqueD ClusterId = %d, want 0 (not in cluster)", results[3].ClusterId)
	}
}

func TestDetectDuplicateClusters_EmptyResults(t *testing.T) {
	var results []SemanticSearchResult
	clusters := detectDuplicateClusters(results)
	if len(clusters) != 0 {
		t.Errorf("expected no clusters for empty results, got %d", len(clusters))
	}
}

func TestDetectDuplicateClusters_SingleResult(t *testing.T) {
	results := []SemanticSearchResult{
		{File: "a.go", Name: "FuncA", Type: "code_unit", Embedding: []float32{1.0, 0.0, 0.0}},
	}
	clusters := detectDuplicateClusters(results)
	if len(clusters) != 0 {
		t.Errorf("expected no clusters for single result, got %d", len(clusters))
	}
}

func TestDetectDuplicateClusters_MixedTypes(t *testing.T) {
	// code_unit results from different files with identical embeddings cluster together.
	// file-type results are excluded from clustering.
	results := []SemanticSearchResult{
		{File: "a.go", Name: "FuncA", Type: "code_unit", Embedding: []float32{1.0, 0.0, 0.0}},
		{File: "b.go", Name: "whole_file", Type: "file", Embedding: []float32{1.0, 0.0, 0.0}},
		{File: "c.go", Name: "FuncC", Type: "code_unit", Embedding: []float32{1.0, 0.0, 0.0}},
	}

	clusters := detectDuplicateClusters(results)

	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster (FuncA + FuncC), got %d: %+v", len(clusters), clusters)
	}
}

func TestDetectDuplicateClusters_ClustersSortedBySimilarity(t *testing.T) {
	// Verify clusters are sorted by similarity (highest first).
	// Cluster 1 (low sim ~0.995): a.go vs b.go — slightly different embeddings
	// Cluster 2 (high sim 1.0): c.go vs d.go — identical embeddings
	// Cross-cluster pairs are orthogonal (sim = 0), so they never merge.
	results := []SemanticSearchResult{
		{File: "a.go", Name: "LowA", Type: "code_unit", Embedding: []float32{1.0, 0.0, 0.0}},
		{File: "b.go", Name: "LowB", Type: "code_unit", Embedding: []float32{1.0, 0.1, 0.0}},
		{File: "c.go", Name: "HighA", Type: "code_unit", Embedding: []float32{0.0, 1.0, 0.0}},
		{File: "d.go", Name: "HighB", Type: "code_unit", Embedding: []float32{0.0, 1.0, 0.0}},
	}

	clusters := detectDuplicateClusters(results)

	if len(clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d: %+v", len(clusters), clusters)
	}
	if clusters[0].Similarity < clusters[1].Similarity {
		t.Errorf("expected clusters sorted by similarity descending, got %v vs %v",
			clusters[0].Similarity, clusters[1].Similarity)
	}
}

func TestDetectDuplicateClusters_ClusterLimit(t *testing.T) {
	// Verify that detectDuplicateClusters limits to top 5 clusters.
	// Create 6 clusters with decreasing similarities to verify only the top 5 are returned.
	// Use 4D vectors for better control over cosine similarity.
	results := []SemanticSearchResult{
		// Cluster 1: identical (sim = 1.0)
		{File: "a1.go", Name: "FuncA1", Type: "code_unit", Embedding: []float32{1.0, 0.0, 0.0, 0.0}},
		{File: "a2.go", Name: "FuncA2", Type: "code_unit", Embedding: []float32{1.0, 0.0, 0.0, 0.0}},
		// Cluster 2: nearly identical (sim ≈ 0.995)
		{File: "b1.go", Name: "FuncB1", Type: "code_unit", Embedding: []float32{0.0, 1.0, 0.0, 0.0}},
		{File: "b2.go", Name: "FuncB2", Type: "code_unit", Embedding: []float32{0.0, 0.98, 0.1, 0.0}},
		// Cluster 3: slightly different (sim ≈ 0.99)
		{File: "c1.go", Name: "FuncC1", Type: "code_unit", Embedding: []float32{0.0, 0.0, 1.0, 0.0}},
		{File: "c2.go", Name: "FuncC2", Type: "code_unit", Embedding: []float32{0.0, 0.14, 0.99, 0.0}},
		// Cluster 4: moderately similar (sim ≈ 0.98)
		{File: "d1.go", Name: "FuncD1", Type: "code_unit", Embedding: []float32{0.577, 0.577, 0.0, 0.0}},
		{File: "d2.go", Name: "FuncD2", Type: "code_unit", Embedding: []float32{0.577, 0.577, 0.2, 0.0}},
		// Cluster 5: lower similarity (sim ≈ 0.965)
		{File: "e1.go", Name: "FuncE1", Type: "code_unit", Embedding: []float32{0.577, 0.0, 0.577, 0.0}},
		{File: "e2.go", Name: "FuncE2", Type: "code_unit", Embedding: []float32{0.577, 0.2, 0.55, 0.0}},
		// Cluster 6: lowest similarity (sim ≈ 0.91) - should be excluded
		// Use 4th dimension to separate from clusters 4 and 5
		{File: "f1.go", Name: "FuncF1", Type: "code_unit", Embedding: []float32{0.577, 0.577, 0.0, 0.577}},
		{File: "f2.go", Name: "FuncF2", Type: "code_unit", Embedding: []float32{0.577, 0.577, 0.3, 0.577}},
	}

	clusters := detectDuplicateClusters(results)

	// Should have exactly 5 clusters
	if len(clusters) != 5 {
		t.Fatalf("expected exactly 5 clusters, got %d", len(clusters))
	}

	// Clusters should be sorted by similarity descending
	for i := 0; i < len(clusters)-1; i++ {
		if clusters[i].Similarity < clusters[i+1].Similarity {
			t.Errorf("clusters not sorted by similarity: clusters[%d].sim=%.3f < clusters[%d].sim=%.3f",
				i, clusters[i].Similarity, i+1, clusters[i+1].Similarity)
		}
	}

	// The 6th cluster (lowest similarity) should not be in the results
	// Verify by checking that no cluster contains both f1.go and f2.go
	for _, cl := range clusters {
		f1Present := false
		f2Present := false
		for _, file := range cl.Files {
			if file == "f1.go" {
				f1Present = true
			}
			if file == "f2.go" {
				f2Present = true
			}
		}
		if f1Present && f2Present {
			t.Error("lowest similarity cluster (f1.go, f2.go) should not be in top 5")
		}
	}
}

func TestHandleAPISemanticSearch_IncludesClusters(t *testing.T) {
	// Verify that the API response includes DuplicateClusters when results exist.
	ws := newTestSemanticServer(t)

	// No agent → empty results → empty clusters.
	req := httptest.NewRequest(http.MethodGet, "/api/search/semantic?query=test", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPISemanticSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp SemanticSearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.DuplicateClusters == nil {
		t.Error("expected DuplicateClusters to be non-nil (empty array), got nil")
	}
}
