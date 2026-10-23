package providers

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestNewProviderTransportSettings pins the dedicated transport knobs: the
// idle window must stay well under http.DefaultTransport's 90 s so a pooled
// connection cannot outlive a Cloudflare-fronted provider's / home NAT's idle
// limit, and the dialer must carry a 15 s TCP keepalive.
func TestNewProviderTransportSettings(t *testing.T) {
	tr := newProviderTransport()
	if tr == nil {
		t.Fatal("newProviderTransport returned nil")
	}
	if tr.IdleConnTimeout != providerTransportIdleConn {
		t.Errorf("IdleConnTimeout = %s, want %s", tr.IdleConnTimeout, providerTransportIdleConn)
	}
	if tr.DialContext == nil {
		t.Fatal("DialContext is nil; the keepalive dialer was not installed")
	}
	if tr.Proxy == nil {
		t.Error("Proxy is nil; ProxyFromEnvironment should be set")
	}
}

// TestShouldRetryOnFreshConnection pins the stale-pool retry decision: only a
// failure on a REUSED (pooled) connection is a transport artifact worth an
// immediate fresh-connection retry; a freshly-dialed failure is a genuine
// network fault and is left to the LLM retry loop.
func TestShouldRetryOnFreshConnection(t *testing.T) {
	reset := errors.New("connection reset by peer")
	if !shouldRetryOnFreshConnection(true, reset) {
		t.Error("a reused-connection failure must be retried on a fresh connection")
	}
	if shouldRetryOnFreshConnection(false, reset) {
		t.Error("a freshly-dialed connection failure must NOT be retried")
	}
	if shouldRetryOnFreshConnection(true, nil) {
		t.Error("no failure, no retry")
	}
}

// TestSecondRequestSucceedsAfterIdleConnClosed is the integration form of
// rel.5 (b): a server that closes idle keep-alive connections after a short
// delay. The client's first request opens a connection that is then pooled;
// the server closes it while it sits idle; the second request must succeed
// (either by reusing a still-valid connection or by retrying on a fresh one).
func TestSecondRequestSucceedsAfterIdleConnClosed(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	s := httptest.NewUnstartedServer(handler)
	// Close idle keep-alive connections quickly so a pooled connection goes
	// stale while the client still holds it in its idle pool.
	s.Config.IdleTimeout = 10 * time.Millisecond
	s.Start()
	defer s.Close()

	p := &GenericProvider{}
	client := &http.Client{Transport: newProviderTransport(), Timeout: 5 * time.Second}

	body := []byte(`{"model":"m"}`)
	req1, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, s.URL, bytes.NewReader(body))
	resp1, diag1, err := p.executeChatRequest(client, req1, body, false)
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	_ = resp1.Body.Close()
	// The connection is now in the idle pool; the server closes it after 10 ms.
	time.Sleep(50 * time.Millisecond)

	req2, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, s.URL, bytes.NewReader(body))
	resp2, diag2, err := p.executeChatRequest(client, req2, body, false)
	if err != nil {
		t.Fatalf("second request (after the idle connection was closed) failed: %v", err)
	}
	_ = resp2.Body.Close()
	t.Logf("request1 reused=%v, request2 reused=%v", diag1.Reused, diag2.Reused)
}
