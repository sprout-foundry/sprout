package embedding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func slimTestRecords() []VectorRecord {
	return []VectorRecord{
		{ID: "a", File: "a.go", Name: "A", Embedding: []float32{1, 0, 0, 0}},
		{ID: "b", File: "b.go", Name: "B", Embedding: []float32{0, 1, 0, 0}},
	}
}

func TestHNSWStore_VectorsLiveOnlyInGraph(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.hnsw")
	s, err := NewHNSWStore(path, "hash")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Store(slimTestRecords()); err != nil {
		t.Fatal(err)
	}

	for id, rec := range s.records {
		if len(rec.Embedding) != 0 {
			t.Errorf("record %s keeps a duplicate vector in memory", id)
		}
	}
	all, _ := s.LoadAll()
	for _, rec := range all {
		if len(rec.Embedding) != 4 {
			t.Errorf("LoadAll record %s returned without its vector", rec.ID)
		}
	}
	res, err := s.Query([]float32{1, 0, 0, 0}, 1, 0)
	if err != nil || len(res) != 1 || res[0].Record.ID != "a" || len(res[0].Record.Embedding) != 4 {
		t.Errorf("Query result missing its vector: %+v %v", res, err)
	}
	data, _ := os.ReadFile(s.recordsPath())
	if strings.Contains(string(data), `"embedding"`) {
		t.Errorf("records file still stores vectors: %s", data)
	}
}

func TestHNSWStore_MigratesLegacyRecordsWithVectors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.hnsw")
	s, err := NewHNSWStore(path, "hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store(slimTestRecords()[:1]); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// A records file from before vectors moved out: both records carry
	// vectors, and "b" isn't in the saved graph at all.
	legacy := map[string]VectorRecord{}
	for _, r := range slimTestRecords() {
		legacy[r.ID] = r
	}
	b, _ := json.Marshal(legacy)
	if err := os.WriteFile(path+".records.json", b, 0o600); err != nil {
		t.Fatal(err)
	}

	s, err = NewHNSWStore(path, "hash")
	if err != nil {
		t.Fatal(err)
	}
	for id, rec := range s.records {
		if len(rec.Embedding) != 0 {
			t.Errorf("legacy vector for %s still held in the records map", id)
		}
	}
	if vec, ok := s.graph.Lookup("b"); !ok || len(vec) != 4 {
		t.Errorf("legacy record missing from the graph was not added back")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path + ".records.json")
	if strings.Contains(string(data), `"embedding"`) {
		t.Errorf("migrated store not saved slim: %s", data)
	}
}
