package embedding

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// countingStore counts build entries. BuildIndex opens with LoadAll before any
// incremental short-circuit, so loads counts builds that actually started —
// unlike write counts, which an incremental no-op build would leave untouched.
type countingStore struct {
	mu      sync.Mutex
	records map[string]VectorRecord
	writes  atomic.Int64
	loads   atomic.Int64
}

func newCountingStore() *countingStore {
	return &countingStore{records: map[string]VectorRecord{}}
}

func (s *countingStore) Store(records []VectorRecord) error {
	s.writes.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range records {
		s.records[r.ID] = r
	}
	return nil
}

func (s *countingStore) ReplaceAll(records []VectorRecord) error {
	s.writes.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = make(map[string]VectorRecord, len(records))
	for _, r := range records {
		s.records[r.ID] = r
	}
	return nil
}

func (s *countingStore) LoadAll() ([]VectorRecord, error) {
	s.loads.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]VectorRecord, 0, len(s.records))
	for _, r := range s.records {
		out = append(out, r)
	}
	return out, nil
}

func (s *countingStore) Query([]float32, int, float32) ([]QueryResult, error) { return nil, nil }
func (s *countingStore) DeleteByFile(string) error                            { return nil }
func (s *countingStore) DeleteByIDs([]string) error                           { return nil }
func (s *countingStore) Save() error                                          { return nil }
func (s *countingStore) Close() error                                         { return nil }

func (s *countingStore) Size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.records)
}

// The shared manager is handed to every agent on a workspace, and each agent's
// EnableEmbeddingIndex calls AutoBuildWhenReady. Without once-semantics that is
// back to one startup build per agent over the same files — the multiplication
// this whole change removes.
func TestAutoBuildWhenReadyBuildsOncePerManager(t *testing.T) {
	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, "main.go"), "package main\n\nfunc Alpha() int { return 1 }\n")

	store := newCountingStore()
	provider := newMockProvider(8)

	mgr := NewEmbeddingManager(&configuration.EmbeddingIndexConfig{
		IndexDir: t.TempDir(),
	}, workspace)
	mgr.SetForTesting(provider, store, NewIndexManager(provider, store, IndexOptions{}))

	const callers = 5
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mgr.AutoBuildWhenReady()
		}()
	}
	wg.Wait()

	if got := store.loads.Load(); got != 1 {
		t.Errorf("%d concurrent callers started %d builds, want exactly 1", callers, got)
	}
	if store.writes.Load() != 1 {
		t.Errorf("the one build wrote %d times, want 1", store.writes.Load())
	}
	if store.Size() == 0 {
		t.Error("the one build that did run indexed nothing")
	}

	// A later caller — a new agent attaching to an already-built workspace —
	// must not restart the build either. The per-manager `building` flag would
	// not catch this one: it is clear by now.
	mgr.AutoBuildWhenReady()
	if got := store.loads.Load(); got != 1 {
		t.Errorf("a sequential later call started another build (loads = %d)", got)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
