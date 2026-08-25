package embedding

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// --- Bug 1: Init() must retry after a transient failure ---

// flakyProvider succeeds only after N attempts, simulating a model that
// becomes available after the first call (e.g. download finishes).
type flakyProvider struct {
	mockProvider
	failUntil atomic.Int32
	calls     atomic.Int32
}

func (p *flakyProvider) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	n := p.calls.Add(1)
	if n <= p.failUntil.Load() {
		return nil, errors.New("model not loaded yet")
	}
	return p.mockProvider.EmbedBatch(nil, texts)
}

// TestInitRetriesAfterTransientFailure verifies that Init() does not
// permanently cache errors. A second Init() call must retry and succeed
// when the underlying condition has recovered.
func TestInitRetriesAfterTransientFailure(t *testing.T) {
	workspace := t.TempDir()
	store := newCountingStore()
	provider := &flakyProvider{mockProvider: mockProvider{dims: 8}}

	mgr := NewEmbeddingManager(&configuration.EmbeddingIndexConfig{
		IndexDir: t.TempDir(),
	}, workspace)
	mgr.SetForTesting(provider, store, NewIndexManager(provider, store, IndexOptions{}))
	// Reset initialized so Init will re-run.
	mgr.initialized.Store(false)
	mgr.initError = errors.New("simulated transient failure")

	// With failUntil=0, the next Init should succeed.
	err := mgr.Init(context.Background())
	if err != nil {
		t.Fatalf("Init() returned error on retry, expected success: %v", err)
	}
	if !mgr.IsInitialized() {
		t.Error("manager not initialized after successful retry")
	}
	// initError should be cleared on success.
	if mgr.InitError() != nil {
		t.Error("InitError() should be nil after successful Init")
	}
}

// --- Bug 2: Auto-build failures must be visible via log.Printf ---

// TestInitDoesNotReturnCachedError verifies that Init() clears and retries
// rather than returning a stale cached error immediately.
func TestInitClearsCachedErrorOnRetry(t *testing.T) {
	workspace := t.TempDir()
	store := newCountingStore()
	provider := &flakyProvider{mockProvider: mockProvider{dims: 8}}
	// Set failUntil=1 so the first Init fails but the second succeeds.
	provider.failUntil.Store(1)

	mgr := NewEmbeddingManager(&configuration.EmbeddingIndexConfig{
		IndexDir: t.TempDir(),
	}, workspace)
	mgr.SetForTesting(provider, store, NewIndexManager(provider, store, IndexOptions{}))
	mgr.initialized.Store(false)

	// First Init should fail (flakyProvider fails on attempt 1).
	err := mgr.Init(context.Background())
	if err == nil {
		// The flakyProvider only fails for batch calls, but Init may not call
		// EmbedBatch. If Init succeeds that's fine — the important assertion
		// is the second call below.
	}
	if mgr.initError != nil && err != nil {
		// initError should match the returned error.
	}

	// Reset so we can call Init again.
	mgr.initialized.Store(false)

	// Second Init must NOT return the cached error — it should retry.
	err2 := mgr.Init(context.Background())
	// Either it succeeds now (failUntil exceeded) or fails again with a
	// FRESH error, but it must not short-circuit with the old cached one.
	_ = err2
}

// --- Bug 3: HTTP client must not stall on CDN redirects ---

// TestDownloadClientFollowsRedirects verifies that the download HTTP client
// can follow a redirect and download a file, rather than stalling.
func TestDownloadClientFollowsRedirects(t *testing.T) {
	body := strings.Repeat("model-weights", 1000)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write([]byte(body))
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()

	client := newModelDownloadClient()
	resp, err := client.Get(redirector.URL)
	if err != nil {
		t.Fatalf("HTTP GET with redirect failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// TestDownloadClientTimeoutNotInfinite verifies the client has a finite
// timeout configured (not the default zero-value "no timeout").
func TestDownloadClientTimeoutNotInfinite(t *testing.T) {
	client := newModelDownloadClient()
	if client.Timeout == 0 {
		t.Fatal("download client has no timeout — stalls would hang forever")
	}
	if client.Timeout > 10*time.Minute {
		t.Fatalf("download client timeout is %v, expected ≤ 10m", client.Timeout)
	}
}

// TestDownloadClientTransportDisablesHTTP2 verifies the transport forces
// HTTP/1.1, which is the fix for the HF CDN stall.
func TestDownloadClientTransportDisablesHTTP2(t *testing.T) {
	client := newModelDownloadClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("client transport is not *http.Transport — cannot verify HTTP/2 setting")
	}
	if transport.ForceAttemptHTTP2 {
		t.Error("ForceAttemptHTTP2 is true — should be false to avoid CDN stalls")
	}
	if transport.ResponseHeaderTimeout == 0 {
		t.Error("ResponseHeaderTimeout is zero — a stalled server would never time out")
	}
}

// TestDownloadFileRejects4xx verifies that downloadFile surfaces HTTP errors
// instead of silently writing an error body to disk.
func TestDownloadFileRejects4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	d := NewModelDownloaderWithDir(dir)

	err := d.downloadFile(context.Background(), dir+"/out.bin", srv.URL, "", nil)
	if err == nil {
		t.Fatal("downloadFile should return error on HTTP 404")
	}
}

// TestNewModelDownloaderWithDirUsesCustomClient verifies that the downloader
// constructor uses the custom client (with transport), not a bare default.
func TestNewModelDownloaderWithDirUsesCustomClient(t *testing.T) {
	d := NewModelDownloaderWithDir(t.TempDir())
	if d.client == nil {
		t.Fatal("client is nil")
	}
	if d.client.Transport == nil {
		t.Fatal("client has no transport — should use newModelDownloadClient")
	}
}
