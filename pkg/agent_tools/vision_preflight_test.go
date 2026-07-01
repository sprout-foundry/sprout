package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// =============================================================================
// SP-103-A5 — Pre-flight Content-Length HEAD on remote downloads
//
// Covers:
//   - HEAD returns Content-Length above cap → *remoteSizeExceededError, no GET issued
//   - HEAD returns Content-Length below cap → nil (GET proceeds)
//   - HEAD returns no Content-Length → nil (fall back to streaming)
//   - HEAD returns 405 → nil (fall back to streaming, S3-style signed URLs)
//   - HEAD request fails (network error) → nil (fall back to streaming)
//   - Pre-cancelled ctx → fast error
// =============================================================================

func TestPreflightRemoteSize_ExceedsCap(t *testing.T) {
	cap := int64(1024) // 1KB cap
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "9999")
			w.WriteHeader(http.StatusOK)
			return
		}
		t.Errorf("GET should NOT be issued when HEAD says too large; got %s", r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := preflightRemoteSize(context.Background(), srv.URL, cap)
	if err == nil {
		t.Fatal("expected error when Content-Length exceeds cap, got nil")
	}
	if !IsRemoteSizeExceededError(err) {
		t.Errorf("expected IsRemoteSizeExceededError, got %v (type %T)", err, err)
	}
	if !contains(err.Error(), "exceeds") {
		t.Errorf("expected error to mention 'exceeds', got %q", err.Error())
	}
}

func TestPreflightRemoteSize_BelowCap(t *testing.T) {
	cap := int64(1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// HEAD returns Content-Length below cap — caller will GET.
		w.Header().Set("Content-Length", "500")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := preflightRemoteSize(context.Background(), srv.URL, cap)
	if err != nil {
		t.Errorf("expected nil when Content-Length below cap, got %v", err)
	}
}

func TestPreflightRemoteSize_NoContentLength(t *testing.T) {
	// Server uses chunked transfer encoding, so Content-Length is omitted.
	// preflightRemoteSize should return nil → caller falls back to streaming.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Flushing a chunked body via Chunked transfer encoding.
		flusher, _ := w.(http.Flusher)
		fmt.Fprintf(w, "%s", "hi")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer srv.Close()

	err := preflightRemoteSize(context.Background(), srv.URL, 1024)
	if err != nil {
		t.Errorf("expected nil when Content-Length missing (fall back to streaming), got %v", err)
	}
}

func TestPreflightRemoteSize_HEADNotAllowed(t *testing.T) {
	// Some S3-style URLs respond 405 to HEAD. preflightRemoteSize should
	// return nil — the caller will GET (which may succeed).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		// GET succeeds — caller eventually fetches this way.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	err := preflightRemoteSize(context.Background(), srv.URL, 1024)
	if err != nil {
		t.Errorf("expected nil when HEAD returns 405 (S3 signed URL pattern), got %v", err)
	}
}

func TestPreflightRemoteSize_HEADNetworkFailure(t *testing.T) {
	// Closed server → HEAD fails with connection refused. preflightRemoteSize
	// should return nil so caller can still try the GET.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.Close() // immediately closed → dial fails

	err := preflightRemoteSize(context.Background(), srv.URL, 1024)
	if err != nil {
		t.Errorf("expected nil when HEAD fails (fall back to streaming), got %v", err)
	}
}

func TestPreflightRemoteSize_PreCancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Pre-cancelled ctx: HEAD should fail-fast. We expect nil (fall back to
	// streaming) because the network-level failure mimics the no-HEAD case.
	err := preflightRemoteSize(ctx, srv.URL, 1024)
	if err != nil {
		t.Errorf("expected nil when ctx pre-cancelled (HEAD fails fast), got %v", err)
	}
}

func TestPreflightRemoteSize_AtCapBoundary(t *testing.T) {
	// Content-Length == cap → not exceeded (cap is "above this size").
	cap := int64(1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1024")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := preflightRemoteSize(context.Background(), srv.URL, cap)
	if err != nil {
		t.Errorf("expected nil when Content-Length equals cap (boundary, not exceeded), got %v", err)
	}
}

func TestPreflightRemoteSize_AboveCapBoundary(t *testing.T) {
	cap := int64(1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1025")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := preflightRemoteSize(context.Background(), srv.URL, cap)
	if !IsRemoteSizeExceededError(err) {
		t.Errorf("expected IsRemoteSizeExceededError when Content-Length = cap+1, got %v", err)
	}
}

func TestRemoteSizeExceededError_Message(t *testing.T) {
	e := &remoteSizeExceededError{
		URL:       "http://example.com/huge.bin",
		SizeBytes: 50 * 1024 * 1024,
		CapBytes:  10 * 1024 * 1024,
	}
	msg := e.Error()
	if !contains(msg, "exceeds") {
		t.Errorf("expected message to mention 'exceeds', got %q", msg)
	}
	if !contains(msg, "50") {
		t.Errorf("expected message to mention actual size, got %q", msg)
	}
}

func TestRemoteSizeExceededError_UnknownSize(t *testing.T) {
	e := &remoteSizeExceededError{
		URL:         "http://example.com/huge.bin",
		CapBytes:    10 * 1024 * 1024,
		IsHEADGuess: true,
	}
	msg := e.Error()
	if !contains(msg, "likely") {
		t.Errorf("expected 'likely' in size-unknown error, got %q", msg)
	}
}

func TestIsRemoteSizeExceededError_Wrapped(t *testing.T) {
	inner := &remoteSizeExceededError{URL: "x", CapBytes: 100}
	wrapped := fmt.Errorf("download failed: %w", inner)
	if !IsRemoteSizeExceededError(wrapped) {
		t.Error("expected IsRemoteSizeExceededError to detect wrapped error")
	}
}

// contains is a tiny substring check helper (kept local to avoid importing
// strings at the top).
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// Ensure imports are referenced even if some helpers below get removed.
var (
	_ = errors.New
	_ = time.Second
)
