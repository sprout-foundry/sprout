package embedding

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sync"
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

// JSONLFileStore is a thread-safe VectorStore backed by a JSONL file.
// Each line is a JSON-encoded VectorRecord. All records are loaded into
// memory on initialization and kept in memory for fast querying.
type JSONLFileStore struct {
	mu        sync.RWMutex
	path      string
	records   []VectorRecord
	dirty     bool
	modelHash string // immutable after construction
}

// metaPath returns the path to the sidecar .meta.json file for this store.
// The meta file lives in the same directory as the store, hidden from casual
// ls output by the leading dot (e.g., ".index.jsonl.meta.json").
func (s *JSONLFileStore) metaPath() string {
	return filepath.Join(filepath.Dir(s.path), fmt.Sprintf(".%s.meta.json", filepath.Base(s.path)))
}

// loadMeta reads the sidecar meta file and returns the stored model hash.
// Returns empty string if the meta file does not exist.
func (s *JSONLFileStore) loadMeta() (string, error) {
	data, err := os.ReadFile(s.metaPath())
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("embedding: read meta %s: %w", s.metaPath(), err)
	}
	var meta metaFile
	if err := json.Unmarshal(data, &meta); err != nil {
		return "", fmt.Errorf("embedding: unmarshal meta %s: %w", s.metaPath(), err)
	}
	return meta.ModelHash, nil
}

// saveMeta writes the model hash to the sidecar .meta.json file atomically
// using a temporary file and rename to prevent partial writes.
func (s *JSONLFileStore) saveMeta(modelHash string) error {
	metaDir := filepath.Dir(s.metaPath())
	if metaDir != "" && metaDir != "." {
		if err := os.MkdirAll(metaDir, 0o755); err != nil {
			return fmt.Errorf("embedding: create meta directory %s: %w", metaDir, err)
		}
	}

	metaData := metaFile{ModelHash: modelHash}
	b, err := json.Marshal(metaData)
	if err != nil {
		return fmt.Errorf("embedding: marshal meta: %w", err)
	}

	tmp, err := os.CreateTemp(metaDir, ".meta-tmp-*")
	if err != nil {
		return fmt.Errorf("embedding: create meta temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("embedding: write meta: %w", err)
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("embedding: close meta temp: %w", err)
	}

	if err := os.Rename(tmpPath, s.metaPath()); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("embedding: rename meta to %s: %w", s.metaPath(), err)
	}

	return nil
}

// NewJSONLFileStore creates a new JSONL-backed vector store at the given path.
// The parent directory is created if it does not exist.
// If a file already exists at path, its records are loaded into memory.
// The modelHash is used to detect model changes — if the stored meta file has
// a different model hash, all records are invalidated (cleared) to force rebuild.
func NewJSONLFileStore(filePath string, modelHash string) (*JSONLFileStore, error) {
	dir := filepath.Dir(filePath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("embedding: create directory %s: %w", dir, err)
		}
	}

	store := &JSONLFileStore{
		path:      filePath,
		records:   nil, // will be loaded below
		modelHash: modelHash,
	}

	records, err := store.loadAll()
	if err != nil {
		return nil, err
	}

	store.records = records
	return store, nil
}

// loadAll reads all VectorRecords from the JSONL file.
// Returns nil (not an error) if the file does not exist.
// If the stored model hash differs from the store's modelHash, logs a warning
// and returns nil records to force a full rebuild.
func (s *JSONLFileStore) loadAll() ([]VectorRecord, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("embedding: open %s: %w", s.path, err)
	}
	defer f.Close()

	var records []VectorRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024) // 1MB max line
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec VectorRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, fmt.Errorf("embedding: unmarshal JSONL line: %w", err)
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("embedding: scan JSONL: %w", err)
	}

	// Check model hash against meta file. If it differs, invalidate all records.
	if len(records) > 0 && s.modelHash != "" {
		storedHash, err := s.loadMeta()
		if err != nil {
			// Meta read failed — skip hash check, proceed with records.
			// The meta file is advisory; a corrupt or unreadable meta should not
			// block the entire store from loading.
			log.Printf("embedding: warn: failed to read meta %s: %v (skipping hash check)",
				s.metaPath(), err)
		} else if storedHash != "" && storedHash != s.modelHash {
			log.Printf("embedding: model hash changed (%s -> %s), clearing store %s",
				hashPrefix(storedHash), hashPrefix(s.modelHash), s.path)
			// Write new meta immediately so re-opening doesn't invalidate again.
			if err := s.saveMeta(s.modelHash); err != nil {
				log.Printf("embedding: warn: failed to update meta on invalidation: %v", err)
			}
			return nil, nil
		}
	}

	return records, nil
}

// saveAll writes all records to the JSONL file atomically using a temporary
// file and rename to prevent partial writes. After saving, writes the meta
// sidecar file with the current model hash.
func (s *JSONLFileStore) saveAll(records []VectorRecord) error {
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".store-tmp-*"+filepath.Ext(s.path))
	if err != nil {
		return fmt.Errorf("embedding: create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	w := bufio.NewWriter(tmp)
	for i := range records {
		b, err := json.Marshal(records[i])
		if err != nil {
			tmp.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("embedding: marshal record: %w", err)
		}
		if _, err := w.Write(append(b, '\n')); err != nil {
			tmp.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("embedding: write line: %w", err)
		}
	}

	if err := w.Flush(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("embedding: flush writer: %w", err)
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("embedding: close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("embedding: rename temp to %s: %w", s.path, err)
	}

	// Write sidecar meta file with model hash.
	// Advisory only — if it fails, the JSONL is already committed.
	if err := s.saveMeta(s.modelHash); err != nil {
		log.Printf("embedding: warn: failed to write meta: %v", err)
	}

	return nil
}

// Store adds records to the store. If a record with the same ID already
// exists, it is replaced. Records are kept sorted by ID for deterministic output.
func (s *JSONLFileStore) Store(records []VectorRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Build a map of existing records by ID for dedup.
	idMap := make(map[string]VectorRecord, len(s.records))
	for i := range s.records {
		idMap[s.records[i].ID] = s.records[i]
	}

	// Add or replace records.
	for i := range records {
		idMap[records[i].ID] = records[i]
	}

	// Flatten map back to slice.
	newRecords := make([]VectorRecord, 0, len(idMap))
	for _, rec := range idMap {
		newRecords = append(newRecords, rec)
	}

	// Sort by ID for deterministic output order.
	slices.SortFunc(newRecords, func(a, b VectorRecord) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})

	s.records = newRecords
	s.dirty = true
	return s.saveAll(newRecords)
}

// Query returns the top-K records most similar to vec, with similarity >= threshold.
func (s *JSONLFileStore) Query(vec []float32, topK int, threshold float32) ([]QueryResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.records) == 0 {
		return nil, nil
	}

	return TopK(vec, s.records, topK, threshold), nil
}

// DeleteByFile removes all records whose File path matches the given filePath.
// The comparison is done using strings.TrimPrefix to handle both absolute and
// relative path differences by normalizing both paths.
func (s *JSONLFileStore) DeleteByFile(filePath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized := filepath.Clean(filePath)

	var kept []VectorRecord
	for i := range s.records {
		if filepath.Clean(s.records[i].File) == normalized {
			continue
		}
		kept = append(kept, s.records[i])
	}

	s.records = kept
	s.dirty = true
	return s.saveAll(kept)
}

// Size returns the number of records currently in the store.
func (s *JSONLFileStore) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.records)
}

// LoadAll returns a copy of all records currently in the store.
func (s *JSONLFileStore) LoadAll() ([]VectorRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]VectorRecord, len(s.records))
	copy(result, s.records)
	return result, nil
}

// Close releases any resources held by the store.
// After calling Close, the store should not be used further.
// Only writes to disk if the store has been modified since creation or last write.
func (s *JSONLFileStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Only rewrite if changes were made.
	if s.dirty && len(s.records) > 0 {
		if err := s.saveAll(s.records); err != nil {
			return err
		}
	}

	s.records = nil
	s.path = ""
	s.dirty = false
	return nil
}
