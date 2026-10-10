package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/sprout-foundry/sprout/pkg/runner/relay"
)

// relayPair connects a "platform" mux (accepts, opens streams) to a "runner"
// mux (dials, serves streams with handler) over a real WebSocket — the same
// topology the platform and runner use, mirroring the relay package's own
// test helper.
func relayPair(t *testing.T, handler relay.Handler) (platform, runner *relay.Mux) {
	t.Helper()
	accepted := make(chan *relay.Mux, 1)
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		m := relay.NewMux(conn, nil)
		accepted <- m
		_ = m.Serve(context.Background())
	}))
	t.Cleanup(srv.Close)

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	runner = relay.NewMux(conn, handler)
	go func() { _ = runner.Serve(context.Background()) }()
	platform = <-accepted
	t.Cleanup(func() { platform.Close(); runner.Close() })
	return platform, runner
}

// relayClient returns an HTTP client whose transport sends requests through
// the platform mux to workspace ws.
func relayClient(m *relay.Mux, ws string) *http.Client {
	return &http.Client{Transport: m.RoundTripper(ws)}
}

// sseFrame mirrors the daemon's UIEvent wire shape closely enough for the
// test: an id, a type, and a data payload.
type sseFrame struct {
	ID   string                 `json:"id"`
	Type string                 `json:"type"`
	Data map[string]interface{} `json:"data"`
}

// fakeAgentDaemon stands up an HTTP server serving GET /api/agent/events as
// an SSE stream, the way the daemon's handler does: subscribe to a channel,
// stream each event as a `data:` frame, flush, and terminate on client
// disconnect. It is the daemon half of the end-to-end relay path.
type fakeAgentDaemon struct {
	mu     sync.Mutex
	subs   map[chan sseFrame]struct{}
	closed bool
}

func newFakeAgentDaemon() *fakeAgentDaemon {
	return &fakeAgentDaemon{subs: make(map[chan sseFrame]struct{})}
}

func (d *fakeAgentDaemon) publish(f sseFrame) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for ch := range d.subs {
		select {
		case ch <- f:
		default:
		}
	}
}

func (d *fakeAgentDaemon) subscriberCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.subs)
}

func (d *fakeAgentDaemon) serveEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no flusher", http.StatusInternalServerError)
		return
	}
	ch := make(chan sseFrame, 32)
	d.mu.Lock()
	d.subs[ch] = struct{}{}
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.subs, ch)
		d.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case f := <-ch:
			b, _ := json.Marshal(f)
			if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// startFakeDaemon returns the loopback port of a running fake agent daemon.
func startFakeDaemon(t *testing.T) (*fakeAgentDaemon, int) {
	t.Helper()
	d := newFakeAgentDaemon()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/agent/events", d.serveEvents)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	return d, port
}

// waitForSSE reads SSE data frames from a live response until it has collected
// want frames or the deadline elapses.
func waitForSSE(t *testing.T, body *bufio.Reader, want int, deadline time.Duration) []sseFrame {
	t.Helper()
	var out []sseFrame
	done := make(chan struct{})
	var scanErr error
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for scanner.Scan() {
			payload, ok := strings.CutPrefix(scanner.Text(), "data: ")
			if !ok {
				continue
			}
			var f sseFrame
			if err := json.Unmarshal([]byte(payload), &f); err != nil {
				scanErr = err
				return
			}
			out = append(out, f)
			if len(out) >= want {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(deadline):
		t.Fatalf("timed out waiting for %d SSE frames over the relay, got %d", want, len(out))
	}
	if scanErr != nil {
		t.Fatalf("decode SSE frame: %v", scanErr)
	}
	return out
}

// TestAgentEventsStreamOverTheRelay is the end-to-end assertion: an agent
// turn's events published by a daemon-like server arrive at the platform as
// SSE frames, carried unchanged through the runner's host server and the
// relay mux's existing HTTP transport. No relay protocol change is involved —
// the SSE response is ordinary HTTP.
func TestAgentEventsStreamOverTheRelay(t *testing.T) {
	daemon, port := startFakeDaemon(t)
	host := NewHostServer()
	host.Bind("ws-1", port, "s3cret")

	platform, _ := relayPair(t, func(ws string, w http.ResponseWriter, r *http.Request) {
		host.Handler().ServeHTTP(w, r)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://relay/daemon/ws-1/api/agent/events", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	resp, err := relayClient(platform, "ws-1").Do(req)
	if err != nil {
		t.Fatalf("open relayed stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("relayed stream status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("relayed content-type = %q, want text/event-stream", ct)
	}

	// Wait for the daemon to see the subscription, then publish a turn.
	waitForDaemonSubscribers(t, daemon, 1)
	daemon.publish(sseFrame{ID: "1", Type: "query_started", Data: map[string]interface{}{"chat_id": "chat-a"}})
	daemon.publish(sseFrame{ID: "2", Type: "stream_chunk", Data: map[string]interface{}{"chat_id": "chat-a", "content": "hello"}})
	daemon.publish(sseFrame{ID: "3", Type: "query_completed", Data: map[string]interface{}{"chat_id": "chat-a"}})

	frames := waitForSSE(t, bufio.NewReader(resp.Body), 3, 10*time.Second)
	if len(frames) != 3 {
		t.Fatalf("got %d frames, want 3", len(frames))
	}
	if frames[0].Type != "query_started" || frames[2].Type != "query_completed" {
		t.Errorf("turn boundaries wrong over relay: %q .. %q", frames[0].Type, frames[2].Type)
	}
	if frames[1].Data["content"] != "hello" {
		t.Errorf("stream chunk payload lost over relay: %#v", frames[1].Data)
	}
}

// TestRelayTunnelSurvivesAStreamReset asserts that tearing down an in-flight
// SSE stream (the consumer disconnects mid-stream) leaves the tunnel usable:
// the daemon-side handler observes the cancellation, and a subsequent request
// over the same mux succeeds.
func TestRelayTunnelSurvivesAStreamReset(t *testing.T) {
	daemon, port := startFakeDaemon(t)
	host := NewHostServer()
	host.Bind("ws-1", port, "s3cret")

	platform, _ := relayPair(t, func(ws string, w http.ResponseWriter, r *http.Request) {
		host.Handler().ServeHTTP(w, r)
	})

	// Open a stream and reset it by cancelling the request context.
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://relay/daemon/ws-1/api/agent/events", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	resp, err := relayClient(platform, "ws-1").Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	waitForDaemonSubscribers(t, daemon, 1)
	cancel()
	_ = resp.Body.Close()
	// The daemon-side handler must observe the disconnect and unsubscribe.
	waitForDaemonSubscribers(t, daemon, 0)

	// The tunnel must still serve a fresh stream: open another and drive an
	// event through it.
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	req2, _ := http.NewRequestWithContext(ctx2, http.MethodGet, "http://relay/daemon/ws-1/api/agent/events", nil)
	req2.Header.Set("Authorization", "Bearer s3cret")
	resp2, err := relayClient(platform, "ws-1").Do(req2)
	if err != nil {
		t.Fatalf("reopen stream after reset: %v", err)
	}
	defer resp2.Body.Close()
	waitForDaemonSubscribers(t, daemon, 1)
	daemon.publish(sseFrame{ID: "9", Type: "stream_chunk", Data: map[string]interface{}{"content": "after-reset"}})
	frames := waitForSSE(t, bufio.NewReader(resp2.Body), 1, 10*time.Second)
	if len(frames) != 1 || frames[0].Data["content"] != "after-reset" {
		t.Fatalf("tunnel unusable after reset: %#v", frames)
	}
}

func waitForDaemonSubscribers(t *testing.T, d *fakeAgentDaemon, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if d.subscriberCount() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("fake daemon subscriber count = %d, want %d", d.subscriberCount(), want)
}
