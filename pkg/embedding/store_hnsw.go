//go:build !js

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

	// Configure search parameters.
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

	// Check model hash.
	storedHash, err := store.loadMeta()
	if err != nil {
		log.Printf("embedding: warn: failed to read hnsw meta: %v (skipping hash check)", err)
	} else if storedHash != "" && storedHash != modelHash {
		log.Printf("embedding: model hash changed (%s -> %s), clearing hnsw store %s",
			hashPrefix(storedHash), hashPrefix(modelHash), indexPath)
		store.clear()
	}

	// Save meta on first creation.
	if storedHash == "" {
		if err := store.saveMeta(modelHash); err != nil {
			log.Printf("embedding: warn: failed to write hnsw meta: %v", err)
		}
	}

	return store, nil
}

// clear empties the graph and metadata map and persists both.
func (s *HNSWStore) clear() {
	newGraph := hnsw.NewGraph[string]()
	newGraph.M = 16
	newGraph.EfSearch = 50
	newGraph.Distance = hnsw.CosineDistance
	s.graph = &hnsw.SavedGraph[string]{
		Graph: newGraph,
		Path:  s.path,
	}
	s.records = make(map[string]VectorRecord)
	s.dirty = true
}

// Save persists the graph and metadata to disk.
func (s *HNSWStore) Save() error {
	if err := s.graph.Save(); err != nil {
		return fmt.Errorf("hnsw: save graph: %w", err)
	}
	if err := s.saveRecords(); err != nil {
		return fmt.Errorf("hnsw: save records: %w", err)
	}
	return nil
}

// Store adds records to the store. Existing records with the same ID are replaced.
func (s *HNSWStore) Store(records []VectorRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range records {
		rec := &records[i]
		// Skip records with nil/empty embeddings — HNSW cannot index them.
		if len(rec.Embedding) == 0 {
			s.records[rec.ID] = *rec
			continue
		}
		if _, exists := s.records[rec.ID]; exists {
			s.graph.Delete(rec.ID)
			// After deletion, graph layers may be empty in a way that panics on Add.
			// Recreate the graph if it has no valid nodes.
			if s.graph.Len() == 0 {
				newG := hnsw.NewGraph[string]()
				newG.M = 16
				newG.EfSearch = 50
				newG.Distance = hnsw.CosineDistance
				s.graph.Graph = newG
			}
		}
		s.graph.Add(hnsw.MakeNode(rec.ID, rec.Embedding))
		s.records[rec.ID] = *rec
	}

	s.dirty = true
	if err := s.graph.Save(); err != nil {
		return fmt.Errorf("hnsw: save after store: %w", err)
	}
	if err := s.saveRecords(); err != nil {
		return fmt.Errorf("hnsw: save records after store: %w", err)
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

	var toDelete []string
	for id, rec := range s.records {
		if filepath.Clean(rec.File) == normalized {
			toDelete = append(toDelete, id)
		}
	}

	for i := range toDelete {
		s.graph.Delete(toDelete[i])
		delete(s.records, toDelete[i])
	}

	if len(toDelete) > 0 {
		s.dirty = true
		if err := s.graph.Save(); err != nil {
			return fmt.Errorf("hnsw: save after delete: %w", err)
		}
		if err := s.saveRecords(); err != nil {
			return fmt.Errorf("hnsw: save records after delete: %w", err)
		}
	}
	return nil
}

// ReplaceAll discards the existing index and builds a new one from records.
func (s *HNSWStore) ReplaceAll(records []VectorRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	newGraph := hnsw.NewGraph[string]()
	newGraph.M = 16
	newGraph.EfSearch = 50
	newGraph.Distance = hnsw.CosineDistance

	newRecords := make(map[string]VectorRecord, len(records))
	for i := range records {
		rec := &records[i]
		if len(rec.Embedding) > 0 {
			newGraph.Add(hnsw.MakeNode(rec.ID, rec.Embedding))
		}
		newRecords[rec.ID] = *rec
	}

	s.graph = &hnsw.SavedGraph[string]{
		Graph: newGraph,
		Path:  s.path,
	}
	s.records = newRecords
	s.dirty = true

	if err := s.graph.Save(); err != nil {
		return fmt.Errorf("hnsw: save after replace: %w", err)
	}
	if err := s.saveRecords(); err != nil {
		return fmt.Errorf("hnsw: save records after replace: %w", err)
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
		if err := s.graph.Save(); err != nil {
			return fmt.Errorf("hnsw: save on close: %w", err)
		}
		if err := s.saveRecords(); err != nil {
			return fmt.Errorf("hnsw: save records on close: %w", err)
		}
	}

	s.records = nil
	s.dirty = false
	return nil
}
