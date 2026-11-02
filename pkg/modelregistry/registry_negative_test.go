package modelregistry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// hangingRegistry serves a registry that accepts requests and never
// answers, like an egress proxy that drops the connection silently.
func hangingRegistry(t *testing.T, hits *atomic.Int32) {
	t.Helper()
	originalURL := baseURLCopy()
	originalTimeout := httpTimeoutCopy()
	originalNegTTL := negativeTTLCopy()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		SetBaseURL(originalURL)
		SetHTTPTimeout(originalTimeout)
		SetNegativeTTL(originalNegTTL)
		ClearCache()
	})
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	SetBaseURL(srv.URL)
	SetHTTPTimeout(50 * time.Millisecond)
	SetNegativeTTL(time.Minute)
	ClearCache()
}

func TestFetchModels_UnreachableRegistryIsNegativeCached(t *testing.T) {
	var hits atomic.Int32
	hangingRegistry(t, &hits)

	if _, err := FetchModels(context.Background(), "unreachable"); err == nil {
		t.Fatal("first fetch: expected the timeout error")
	}

	for i := 0; i < 5; i++ {
		start := time.Now()
		models, err := FetchModels(context.Background(), "unreachable")
		if err != nil || models != nil {
			t.Fatalf("fetch %d inside the negative TTL = (%v, %v), want (nil, nil)", i, models, err)
		}
		if elapsed := time.Since(start); elapsed > 25*time.Millisecond {
			t.Fatalf("fetch %d inside the negative TTL took %v; it should not touch the network", i, elapsed)
		}
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("registry requests = %d, want 1", got)
	}
}

func TestFetchModels_CancelledCallerIsNotNegativeCached(t *testing.T) {
	var hits atomic.Int32
	hangingRegistry(t, &hits)
	SetHTTPTimeout(5 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := FetchModels(ctx, "cancelled"); err == nil {
		t.Fatal("expected the caller's deadline to fail the fetch")
	}

	mu.RLock()
	_, cached := negativeCache["cancelled"]
	mu.RUnlock()
	if cached {
		t.Fatal("a fetch failed by the caller's own deadline was negative-cached")
	}
}
