//go:build !js

package daemon

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// mockRemoteProvider — deterministic unit-vector provider for the gate test
// ---------------------------------------------------------------------------

type mockRemoteProvider struct {
	dims int
	// embedCalls counts total Embed + EmbedBatch-element calls. Shared
	// across the test to prove all clients hit ONE server-side provider.
	embedCalls *atomic.Int64
}

func (m *mockRemoteProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	m.embedCalls.Add(1)
	return unitVector(m.dims), nil
}

func (m *mockRemoteProvider) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = unitVector(m.dims)
		m.embedCalls.Add(1)
	}
	return out, nil
}

func (m *mockRemoteProvider) Dimensions() int { return m.dims }

func (m *mockRemoteProvider) Name() string { return "mock-remote-gate" }

func (m *mockRemoteProvider) ModelHash() string { return "mock-model-hash" }

func (m *mockRemoteProvider) EmbedWithPrefix(_ context.Context, _ string, _ string) ([]float32, error) {
	m.embedCalls.Add(1)
	return unitVector(m.dims), nil
}

func (m *mockRemoteProvider) EmbedBatchWithPrefix(_ context.Context, texts []string, _ string) ([][]float32, error) {
	return m.EmbedBatch(context.Background(), texts)
}

func (m *mockRemoteProvider) Close() error { return nil }

// unitVector returns a unit vector (cosine similarity 1.0 with itself) so
// every query matches every indexed unit — the gate tests plumbing and
// consistency, not semantic quality.
func unitVector(dims int) []float32 {
	v := make([]float32, dims)
	val := float32(1.0 / math.Sqrt(float64(dims)))
	for i := range v {
		v[i] = val
	}
	return v
}

// ---------------------------------------------------------------------------
// TestEmbeddingSocketGate — SP-136 P3 gate test
//
// 3 concurrent "CLI processes" query the same workspace through ONE daemon
// socket. All must get results from ONE model load (a single server-side
// manager), ONE index, with zero corruption.
// ---------------------------------------------------------------------------

func TestEmbeddingSocketGate(t *testing.T) {
	// --- Workspace fixture: two Go files, two functions each ---
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte(
		"package main\nfunc Alpha() {}\nfunc Beta() {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.go"), []byte(
		"package main\nfunc Gamma() {}\nfunc Delta() {}\n"), 0o644))

	// --- Server side: ONE manager backed by ONE mock provider ---
	var embedCalls atomic.Int64
	mock := &mockRemoteProvider{dims: 3, embedCalls: &embedCalls}

	store, err := embedding.NewHNSWStore(filepath.Join(dir, "index.hnsw"), "mock-model-hash")
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	indexMgr := embedding.NewIndexManager(mock, store, embedding.IndexOptions{
		BatchSize:  16,
		MaxBodyLen: 500,
		IndexDir:   dir,
	})
	mgr := embedding.NewEmbeddingManager(&configuration.EmbeddingIndexConfig{}, dir)
	mgr.SetForTesting(mock, store, indexMgr)

	// Acquire returns the SAME manager for every workspace (the daemon owns
	// one writer per workspace). Count acquisitions to prove dedup.
	var acquisitions atomic.Int64
	svc := &EmbeddingManagerService{
		Acquire: func(string) *embedding.EmbeddingManager {
			acquisitions.Add(1)
			return mgr
		},
		Release: func(*embedding.EmbeddingManager) {},
	}

	sockPath := filepath.Join(t.TempDir(), "embed.sock")
	srv := &EmbeddingServer{SocketPath: sockPath, Service: svc}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	require.NoError(t, srv.Start(ctx))
	t.Cleanup(func() { srv.Close() })

	// --- 3 concurrent CLI processes, each with its own socket connection ---
	const clients = 3
	start := make(chan struct{})
	errCh := make(chan error, clients)
	var wg sync.WaitGroup

	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start

			provider, err := embedding.NewRemoteEmbeddingProvider(sockPath)
			if err != nil {
				errCh <- fmt.Errorf("client %d: dial: %w", id, err)
				return
			}
			defer provider.Close()
			client := embedding.NewRemoteClient(provider)

			// Build the shared index through the daemon. Later clients'
			// builds are incremental (manifest-based) and may embed 0 new
			// units — the success of the call is what matters.
			stats, err := client.BuildIndex(ctx, dir)
			if err != nil {
				errCh <- fmt.Errorf("client %d: build: %w", id, err)
				return
			}
			if stats == nil {
				errCh <- fmt.Errorf("client %d: build returned nil stats", id)
				return
			}

			// Query the shared index through the daemon.
			results, err := client.QuerySimilar(ctx, dir, "Alpha", 5, 0.5)
			if err != nil {
				errCh <- fmt.Errorf("client %d: query: %w", id, err)
				return
			}
			if len(results) == 0 {
				errCh <- fmt.Errorf("client %d: query returned no results", id)
				return
			}

			// Duplicate check through the daemon.
			dups, err := client.CheckDuplicates(ctx, dir, filepath.Join(dir, "new.go"), "package main\nfunc Alpha() {}\n")
			if err != nil {
				errCh <- fmt.Errorf("client %d: dup check: %w", id, err)
				return
			}
			if dups == nil {
				errCh <- fmt.Errorf("client %d: dup check returned nil", id)
				return
			}
			errCh <- nil
		}(i)
	}
	close(start)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err, "concurrent CLI process failed")
	}

	// --- One model load: the server-side manager (and its provider) was
	// created once; every embed call went through that single provider. ---
	require.GreaterOrEqual(t, acquisitions.Load(), int64(1), "server must acquire a manager")
	require.Greater(t, embedCalls.Load(), int64(0), "server-side provider must have embedded units")

	// --- Zero corruption: a fresh store must open cleanly with the exact
	// number of records the builds wrote (4 units), and queries still work. ---
	require.NoError(t, store.Close())
	fresh, err := embedding.NewHNSWStore(filepath.Join(dir, "index.hnsw"), "mock-model-hash")
	require.NoError(t, err, "fresh store must open without corruption")
	defer fresh.Close()

	all, err := fresh.LoadAll()
	require.NoError(t, err)
	require.Len(t, all, 4, "index must contain exactly 4 unit records (2 files × 2 functions), got %d", len(all))

	res, err := fresh.Query(unitVector(3), 5, 0.5)
	require.NoError(t, err, "fresh store must be queryable")
	assert.NotEmpty(t, res, "fresh store must return results")
}
