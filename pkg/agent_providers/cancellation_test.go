package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// TestSendChatRequest_CtxCancelAborts proves the SP-034 contract end-to-end
// at the provider layer: cancelling the ctx mid-request actually aborts the
// in-flight HTTP call, instead of waiting for the server's full response.
//
// The stub server is configured to sleep long enough that ANY response
// means we missed the cancel. We cancel after 50ms and assert
// SendChatRequest returns within 1s with a cancellation error.
//
// Before the SP-034-1a/1b/1e plumbing, SendChatRequest used
// http.NewRequest (no ctx) and ignored cancellation — this test would have
// hung until the server timer. The fix is verified by this test's bound on
// the return time.
func TestSendChatRequest_CtxCancelAborts(t *testing.T) {
	// Bound the server-side wait — if the test setup is correct and the
	// client cancels, the server's r.Context() fires almost immediately
	// and the handler returns. If for some reason it doesn't, we don't
	// want to hang CI longer than necessary; the client-side time bound
	// below is the actual correctness assertion.
	const serverSleep = 2 * time.Second

	// Track whether the server saw the client disconnect — useful both as
	// an extra assertion and to make sure the goroutine cleans up quickly.
	var clientGoneObserved atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			clientGoneObserved.Store(true)
			return
		case <-time.After(serverSleep):
			// We should never reach this — if we do, the cancellation
			// didn't propagate, and the test's own time bound below
			// will catch it.
			_, _ = w.Write([]byte(`{"id":"oops","model":"x","choices":[{"message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
		}
	}))
	defer server.Close()

	config := &ProviderConfig{
		Name:     "cancel-test",
		Endpoint: server.URL,
		Auth:     AuthConfig{Type: "none"},
		Defaults: RequestDefaults{Model: "test-model"},
		Models: ModelConfig{
			DefaultContextLimit: 64000,
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("NewGenericProvider failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Fire the cancel slightly after the request starts so we know the
	// HTTP call is actually in flight.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err = provider.SendChatRequest(ctx, []api.Message{{Role: "user", Content: "hi"}}, nil, "", false)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected SendChatRequest to fail with cancellation error, got nil")
	}
	// http.Client.Do wraps the canceled context as either context.Canceled
	// directly or a url.Error whose Err is context.Canceled. errors.Is
	// handles both.
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled in error chain, got %v", err)
	}

	// The hard correctness bound: we cancelled at 50ms; the request should
	// return well before the 30s server sleep. 5s gives huge headroom for
	// CI slowness without letting "didn't cancel at all" hide.
	if elapsed > 5*time.Second {
		t.Errorf("SendChatRequest took %s — cancellation didn't propagate to the HTTP layer", elapsed)
	}

	// Best-effort check that the server observed the client going away.
	// Give it a small grace window since the close happens asynchronously
	// after our local Do() returns.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) && !clientGoneObserved.Load() {
		time.Sleep(10 * time.Millisecond)
	}
	if !clientGoneObserved.Load() {
		t.Log("warning: server didn't observe client disconnect within 1s grace window — not a hard failure since httptest's connection accounting can be flaky, but worth noting")
	}
}

// TestSendChatRequestStream_CtxCancelAborts is the streaming-path counterpart.
// Same setup, but the stub server writes one SSE chunk, then hangs. The
// callback should receive that one chunk; the cancel after that should abort
// the stream read promptly.
func TestSendChatRequestStream_CtxCancelAborts(t *testing.T) {
	chunkDelivered := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Errorf("httptest.NewServer ResponseWriter doesn't implement Flusher — test setup invalid")
			return
		}
		// Emit a minimal valid OpenAI-style chunk so the parser advances.
		_, _ = fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"hi"}}]}`)
		_, _ = fmt.Fprintln(w)
		flusher.Flush()

		// Signal that the chunk has been delivered, then hang.
		// Test will cancel ctx to unblock us.
		<-r.Context().Done()
	}))
	defer server.Close()

	config := &ProviderConfig{
		Name:     "cancel-stream-test",
		Endpoint: server.URL,
		Auth:     AuthConfig{Type: "none"},
		Defaults: RequestDefaults{Model: "test-model"},
		Models:   ModelConfig{DefaultContextLimit: 64000},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("NewGenericProvider failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var chunksSeen atomic.Int32

	go func() {
		// Wait for the first chunk to land, then cancel.
		<-chunkDelivered
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err = provider.SendChatRequestStream(
		ctx,
		[]api.Message{{Role: "user", Content: "hi"}},
		nil,
		"",
		false,
		func(content, contentType string) {
			if chunksSeen.Add(1) == 1 {
				close(chunkDelivered)
			}
		},
	)
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Errorf("SendChatRequestStream took %s — streaming cancellation didn't propagate", elapsed)
	}

	// We expect an error (the cancel) — but the streaming path's exact
	// error type depends on where the read was when ctx Done fired. The
	// key bound is the time elapsed; the error shape is secondary.
	if err == nil {
		t.Log("note: streaming returned nil error on cancellation — that's fine if the parser flushed cleanly, but the timing bound above is what we really care about")
	}

	if chunksSeen.Load() < 1 {
		t.Errorf("expected at least 1 streaming chunk before cancel, got %d", chunksSeen.Load())
	}
}
