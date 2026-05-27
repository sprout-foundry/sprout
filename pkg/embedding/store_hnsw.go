// Package embedding — HNSW-backed vector store for fast approximate nearest
// neighbor search. Uses github.com/coder/hnsw for the graph index.
package embedding

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/coder/hnsw"
)

// metaFile holds the model hash in a sidecar JSON file.
type metaFile struct {
	ModelHash string `json:"modelHash"`
}

// hashPrefix returns the first 16 characters of s, or the full string if shorter.
func hashPrefix(s string) string {
	if len(s) > 16 {
		return s[:16]
	}
	return s
}

// HNSWStore is a thread-safe VectorStore backed by an HNSW index.
// The graph stores vectors keyed by record ID; a separate map holds
// full VectorRecord metadata. The index is persisted to disk via
// hnsw.SavedGraph and a sidecar .meta file tracks the model hash.
type HNSWStore struct {
	mu      sync.Mutex
	graph   *hnsw.SavedGraph[string]
	records map[string]VectorRecord
	path    string
	dirty   bool
}

// metaPath returns the path to the sidecar .meta JSON file for this store.
func (s *HNSWStore) metaPath() string {
	return s.path + ".meta"
}

// recordsPath returns the path to the records metadata sidecar file.
func (s *HNSWStore) recordsPath() string {
	return s.path + ".records.json"
}

// loadMeta reads the stored model hash. Returns "" if no meta file exists.
func (s *HNSWStore) loadMeta() (string, error) {
	data, err := os.ReadFile(s.metaPath())
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("hnsw: read meta %s: %w", s.metaPath(), err)
	}
	var m metaFile
	if err := json.Unmarshal(data, &m); err != nil {
		return "", fmt.Errorf("hnsw: unmarshal meta %s: %w", s.metaPath(), err)
	}
	return m.ModelHash, nil
}

// saveMeta writes the model hash to the sidecar file atomically.
func (s *HNSWStore) saveMeta(modelHash string) error {
	dir := filepath.Dir(s.metaPath())
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("hnsw: create meta directory %s: %w", dir, err)
		}
	}

	metaData := metaFile{ModelHash: modelHash}
	b, err := json.Marshal(metaData)
	if err != nil {
		return fmt.Errorf("hnsw: marshal meta: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".meta-tmp-*")
	if err != nil {
		return fmt.Errorf("hnsw: create meta temp: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("hnsw: write meta: %w", err)
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("hnsw: close meta temp: %w", err)
	}

	if err := os.Rename(tmpPath, s.metaPath()); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("hnsw: rename meta: %w", err)
	}
	return nil
}

// saveRecords persists the metadata records map to disk.
func (s *HNSWStore) saveRecords() error {
	b, err := json.Marshal(s.records)
	if err != nil {
		return fmt.Errorf("hnsw: marshal records: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "*.records.tmp")
	if err != nil {
		return fmt.Errorf("hnsw: create records temp: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("hnsw: write records: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("hnsw: close records temp: %w", err)
	}
	if err := os.Rename(tmpPath, s.recordsPath()); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("hnsw: rename records: %w", err)
	}
	return nil
}

// loadRecords reads the metadata records from disk.
func (s *HNSWStore) loadRecords() error {
	data, err := os.ReadFile(s.recordsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no records file yet
		}
		return fmt.Errorf("hnsw: read records %s: %w", s.recordsPath(), err)
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, &s.records)
}

// NewHNSWStore creates or loads an HNSW-backed vector store.
//
// indexPath is the path to the persisted HNSW index file.
// modelHash is the current provider's model hash; if it differs from the
// stored hash, the index is cleared to force a full rebuild.
func NewHNSWStore(indexPath string, modelHash string) (*HNSWStore, error) {
	dir := filepath.Dir(indexPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("hnsw: create directory %s: %w", dir, err)
		}
	}

	sg, err := hnsw.LoadSavedGraph[string](indexPath)
	if err != nil {
		return nil, fmt.Errorf("hnsw: load graph %s: %w", indexPath, err)
	}

	// Configure search parameters to match newConfiguredGraph() so the
	// loaded graph behaves identically to one created fresh.
	sg.M = 16
	sg.EfSearch = 50
	sg.Distance = hnsw.CosineDistance

	store := &HNSWStore{
		graph:   sg,
		path:    indexPath,
		records: make(map[string]VectorRecord),
	}

	// Load persisted records metadata.
	if err := store.loadRecords(); err != nil {
		log.Printf("embedding: warn: failed to load hnsw records: %v", err)
	}

	// Reconcile graph and records. The two files are written in separate
	// non-atomic steps; if a previous session crashed between them, the graph
	// can contain nodes that the records map has no metadata for. Querying
	// such orphans is useless, and re-adding their IDs later panics inside
	// hnsw.Add ("node not added"). Clear the graph so the next build rebuilds
	// from a consistent state.
	if len(store.records) == 0 && sg.Len() > 0 {
		debugLogf("embedding: hnsw store %s has %d graph nodes but no records metadata; clearing for rebuild",
			indexPath, sg.Len())
		store.clear()
	}

	// Check model hash. Persist the current hash whenever the meta is
	// missing OR stale — otherwise a hash mismatch would clear the store
	// every startup forever (the next run would re-read the same stale
	// hash, log the same "model hash changed" message, and re-clear).
	storedHash, err := store.loadMeta()
	if err != nil {
		log.Printf("embedding: warn: failed to read hnsw meta: %v (skipping hash check)", err)
	} else if storedHash != "" && storedHash != modelHash {
		log.Printf("embedding: model hash changed (%s -> %s), clearing hnsw store %s",
			hashPrefix(storedHash), hashPrefix(modelHash), indexPath)
		store.clear()
	}

	if err == nil && storedHash != modelHash {
		if err := store.saveMeta(modelHash); err != nil {
			log.Printf("embedding: warn: failed to write hnsw meta: %v", err)
		}
	}

	return store, nil
}

// clear empties the graph and metadata map and persists both.
func (s *HNSWStore) clear() {
	s.graph = &hnsw.SavedGraph[string]{
		Graph: newConfiguredGraph(),
		Path:  s.path,
	}
	s.records = make(map[string]VectorRecord)
	s.dirty = true
}

// newConfiguredGraph returns a fresh hnsw.Graph with the parameters this
// store uses everywhere. Centralized so M/EfSearch/Distance don't drift
// between clear(), reconcile, and the post-delete reset path.
func newConfiguredGraph() *hnsw.Graph[string] {
	g := hnsw.NewGraph[string]()
	g.M = 16
	g.EfSearch = 50
	g.Distance = hnsw.CosineDistance
	return g
}

// Save persists the graph and metadata to disk.
// Records are written before the graph so a crash mid-save leaves records
// ahead of the graph (recoverable) rather than vice versa (panic-prone).
func (s *HNSWStore) Save() error {
	if err := s.saveRecords(); err != nil {
		return fmt.Errorf("hnsw: save records: %w", err)
	}
	if err := s.graph.Save(); err != nil {
		return fmt.Errorf("hnsw: save graph: %w", err)
	}
	return nil
}

// Store adds records to the store. Existing records with the same ID are replaced.
func (s *HNSWStore) Store(records []VectorRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// First, update s.records with all incoming records (both with and without embeddings).
	for i := range records {
		rec := &records[i]
		s.records[rec.ID] = *rec
	}

	// Build a completely fresh graph from ALL records in s.records that have embeddings.
	// This avoids the hnsw library's Delete bugs (corrupts graph state, causes panics on Add).
	newGraph := newConfiguredGraph()
	for _, rec := range s.records {
		if len(rec.Embedding) > 0 {
			newGraph.Add(hnsw.MakeNode(rec.ID, rec.Embedding))
		}
	}

	// Replace s.graph.Graph with the new graph.
	s.graph.Graph = newGraph
	s.dirty = true

	// Persist records first so a crash mid-save leaves records ahead of the
	// graph (recoverable on next build) rather than graph ahead of records
	// (which triggers the "node not added" panic).
	if err := s.saveRecords(); err != nil {
		return fmt.Errorf("hnsw: save records after store: %w", err)
	}
	if err := s.graph.Save(); err != nil {
		return fmt.Errorf("hnsw: save after store: %w", err)
	}
	return nil
}

// LoadAll returns all records currently in the store.
func (s *HNSWStore) LoadAll() ([]VectorRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]VectorRecord, 0, len(s.records))
	for _, rec := range s.records {
		result = append(result, rec)
	}
	return result, nil
}

// Query returns the top-K records most similar to vec, with similarity >= threshold.
// Uses cosine distance from the hnsw library; similarity = 1 - distance.
func (s *HNSWStore) Query(vec []float32, topK int, threshold float32) ([]QueryResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.graph.Len() == 0 {
		return nil, nil
	}

	// Ask for enough candidates to account for threshold filtering.
	// If topK is 0, ask for all; otherwise ask for topK.
	requestK := topK
	if requestK <= 0 {
		requestK = s.graph.Len()
	}

	nodes := s.graph.Search(vec, requestK)

	var results []QueryResult
	for i := range nodes {
		dist := hnsw.CosineDistance(vec, nodes[i].Value)
		sim := 1.0 - dist
		if sim >= threshold {
			rec, ok := s.records[nodes[i].Key]
			if !ok {
				continue // orphan in graph, skip
			}
			results = append(results, QueryResult{
				Record:     rec,
				Similarity: float32(sim),
			})
		}
	}

	return results, nil
}

// DeleteByFile removes all records whose File path matches filePath.
func (s *HNSWStore) DeleteByFile(filePath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized := filepath.Clean(filePath)

	// Remove matching records from s.records.
	var deleted int
	for id, rec := range s.records {
		if filepath.Clean(rec.File) == normalized {
			delete(s.records, id)
			deleted++
		}
	}

	if deleted == 0 {
		return nil
	}

	// Rebuild the graph from scratch to avoid hnsw Delete bugs.
	newGraph := newConfiguredGraph()
	for _, rec := range s.records {
		if len(rec.Embedding) > 0 {
			newGraph.Add(hnsw.MakeNode(rec.ID, rec.Embedding))
		}
	}
	s.graph.Graph = newGraph
	s.dirty = true

	if err := s.saveRecords(); err != nil {
		return fmt.Errorf("hnsw: save records after delete: %w", err)
	}
	if err := s.graph.Save(); err != nil {
		return fmt.Errorf("hnsw: save after delete: %w", err)
	}
	return nil
}

// DeleteByIDs removes records with the given IDs in a single batched
// operation. IDs not present in the store are silently skipped.
// Saves to disk only once, regardless of how many IDs are deleted.
func (s *HNSWStore) DeleteByIDs(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove records from s.records.
	var deleted int
	for _, id := range ids {
		if _, ok := s.records[id]; !ok {
			continue
		}
		delete(s.records, id)
		deleted++
	}

	if deleted == 0 {
		return nil
	}

	// Rebuild the graph from scratch to avoid hnsw Delete bugs.
	newGraph := newConfiguredGraph()
	for _, rec := range s.records {
		if len(rec.Embedding) > 0 {
			newGraph.Add(hnsw.MakeNode(rec.ID, rec.Embedding))
		}
	}
	s.graph.Graph = newGraph
	s.dirty = true

	if err := s.saveRecords(); err != nil {
		return fmt.Errorf("hnsw: save records after delete-by-ids: %w", err)
	}
	if err := s.graph.Save(); err != nil {
		return fmt.Errorf("hnsw: save after delete-by-ids: %w", err)
	}
	return nil
}

// ReplaceAll discards the existing index and builds a new one from records.
//
// Records are deduplicated by ID before hitting the HNSW graph. The
// upstream extractors now produce line-disambiguated IDs (see
// `extractor.go:makeUnitID` — every code-unit ID is
// `<file>:<name>#L<startLine>`), so collisions should be impossible
// at the source. This dedupe is kept as belt-and-suspenders: any
// future extractor / caller that hands us a slice with duplicate
// IDs (intentional or otherwise) won't trigger the coder/hnsw
// library's `g.Len() == preLen+1` invariant panic at graph.go:405.
// Replacement semantics match the on-disk record store: the LAST
// record with a given ID wins.
//
// History: this branch was added after a real "node not added" panic
// during sprout's first-run auto-build on a workspace where the
// Python/TS extractors produced `path:methodname` collisions across
// classes in the same file.
func (s *HNSWStore) ReplaceAll(records []VectorRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Build the dedup'd records map first so the graph and the store
	// agree on which record won the collision (the LAST one wins,
	// matching `newRecords[rec.ID] = *rec` semantics).
	newRecords := make(map[string]VectorRecord, len(records))
	for i := range records {
		newRecords[records[i].ID] = records[i]
	}

	newGraph := newConfiguredGraph()
	for _, rec := range newRecords {
		if len(rec.Embedding) > 0 {
			newGraph.Add(hnsw.MakeNode(rec.ID, rec.Embedding))
		}
	}

	s.graph = &hnsw.SavedGraph[string]{
		Graph: newGraph,
		Path:  s.path,
	}
	s.records = newRecords
	s.dirty = true

	if err := s.saveRecords(); err != nil {
		return fmt.Errorf("hnsw: save records after replace: %w", err)
	}
	if err := s.graph.Save(); err != nil {
		return fmt.Errorf("hnsw: save after replace: %w", err)
	}
	return nil
}

// Size returns the number of records in the store.
func (s *HNSWStore) Size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.records)
}

// Close saves the graph and records to disk if there are pending changes,
// then clears internal state.
func (s *HNSWStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dirty {
		if err := s.saveRecords(); err != nil {
			return fmt.Errorf("hnsw: save records on close: %w", err)
		}
		if err := s.graph.Save(); err != nil {
			return fmt.Errorf("hnsw: save on close: %w", err)
		}
	}

	s.records = nil
	s.dirty = false
	return nil
}
