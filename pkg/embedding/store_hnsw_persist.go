package embedding

// store_hnsw_persist.go — the on-disk persistence layer of the HNSW
// embedding store: the metaFile format, the hash-prefix helper, and
// the meta/records load+save methods. Split out of store_hnsw.go.
import (
	"encoding/json"
	"fmt"
	"github.com/coder/hnsw"
	"os"
	"path/filepath"
)

// metaFile holds the model hash in a sidecar JSON file.
//
// Note for anyone changing internal/hnsw: Add() picks a node's neighbours using
// the same layer search that queries use, so a change to that search alters the
// STRUCTURE of graphs built afterwards, not just how they are read. A graph
// built by the old code stays degraded when queried by fixed code — measured at
// 5/14 against 10/14 for the identical records reinserted. Any such change
// therefore has to invalidate existing graphs, which today happens for free
// only because ModelHash changes at the same time.
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
	if err := json.Unmarshal(data, &s.records); err != nil {
		return err
	}
	// Records files written before vectors lived only in the graph carry a
	// duplicate copy of every embedding (~3 KB per record). Move each into
	// the graph if missing, drop the record's copy, and mark the store dirty
	// so the next save writes the slim file. This recovers the legacy format
	// only: a records file written in the slim format carries no vector to
	// restore, so a crash between the records write and the graph write in
	// Store/Delete/ReplaceAll leaves that record unsearchable until a later
	// build re-embeds it (the incremental hash diff sees the intact hash and
	// skips it). That window is microseconds wide and degrades gracefully, so
	// it is accepted rather than guarded with an interface-level self-heal.
	for id, rec := range s.records {
		if len(rec.Embedding) == 0 {
			continue
		}
		if _, ok := s.graph.Lookup(id); !ok {
			s.graph.Add(hnsw.MakeNode(id, rec.Embedding))
		}
		rec.Embedding = nil
		s.records[id] = rec
		s.dirty = true
	}
	return nil
}
