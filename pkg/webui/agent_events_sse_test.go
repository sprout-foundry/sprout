//go:build !js

package webui

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// sseTestServer builds a ReactWebServer with an event bus and a client
// context carrying two chats, so the SSE handler can be exercised directly.
func sseTestServer(t *testing.T, authToken string) (*ReactWebServer, *events.EventBus) {
	t.Helper()
	bus := events.NewEventBus()
	srv, err := NewReactWebServer(nil, bus, 0, "127.0.0.1", "", authToken)
	if err != nil {
		t.Fatalf("NewReactWebServer: %v", err)
	}
	srv.clientContexts["sse-client"] = &webClientContext{
		ChatSessions: map[string]*chatSession{
			"chat-a": newChatSession("chat-a", "A"),
			"chat-b": newChatSession("chat-b", "B"),
		},
		DefaultChatID: "chat-a",
	}
	return srv, bus
}

// readSSEEvents reads SSE frames from a live response until it has collected
// want data frames or the deadline elapses. It returns the decoded UIEvents,
// skipping named control frames (an `event:` line preceding the `data:` line).
func readSSEEvents(t *testing.T, body *bufio.Reader, want int, deadline time.Duration) []events.UIEvent {
	t.Helper()
	var out []events.UIEvent
	done := make(chan struct{})
	var scanErr error
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		named := false
		for scanner.Scan() {
			line := scanner.Text()
			if _, ok := strings.CutPrefix(line, "event: "); ok {
				named = true
				continue
			}
			if line == "" {
				named = false
				continue
			}
			payload, ok := strings.CutPrefix(line, "data: ")
			if !ok || named {
				continue
			}
			var ev events.UIEvent
			if err := json.Unmarshal([]byte(payload), &ev); err != nil {
				scanErr = err
				return
			}
			out = append(out, ev)
			if len(out) >= want {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(deadline):
		t.Fatalf("timed out waiting for %d SSE events, got %d", want, len(out))
	}
	if scanErr != nil {
		t.Fatalf("decode SSE frame: %v", scanErr)
	}
	return out
}

func TestAgentEventsSSE_StreamsAgentTurnOverHTTP(t *testing.T) {
	srv, bus := sseTestServer(t, "")
	ts := httptest.NewServer(http.HandlerFunc(srv.handleAPIAgentEvents))
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/agent/events?chat_id=chat-a", nil)
	req.Header.Set(webClientIDHeader, "sse-client")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q, want text/event-stream", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("cache-control = %q, want no-cache", cc)
	}

	reader := bufio.NewReader(resp.Body)
	// Publish a small agent turn: started, a chunk, completed.
	for _, ev := range []struct {
		typ  string
		data map[string]interface{}
	}{
		{events.EventTypeQueryStarted, map[string]interface{}{"client_id": "sse-client", "chat_id": "chat-a"}},
		{events.EventTypeStreamChunk, map[string]interface{}{"client_id": "sse-client", "chat_id": "chat-a", "content": "hi"}},
		{events.EventTypeQueryCompleted, map[string]interface{}{"client_id": "sse-client", "chat_id": "chat-a"}},
	} {
		bus.Publish(ev.typ, ev.data)
	}

	got := readSSEEvents(t, reader, 3, 5*time.Second)
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3", len(got))
	}
	if got[0].Type != events.EventTypeQueryStarted || got[2].Type != events.EventTypeQueryCompleted {
		t.Errorf("turn boundaries wrong: %q .. %q", got[0].Type, got[2].Type)
	}
	if data, ok := got[1].Data.(map[string]interface{}); !ok || data["content"] != "hi" {
		t.Errorf("stream chunk payload lost: %#v", got[1].Data)
	}
}

func TestAgentEventsSSE_ScopesToTheSubscribersChat(t *testing.T) {
	srv, bus := sseTestServer(t, "")
	ts := httptest.NewServer(http.HandlerFunc(srv.handleAPIAgentEvents))
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/agent/events?chat_id=chat-a", nil)
	req.Header.Set(webClientIDHeader, "sse-client")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)

	// chat B's event must not reach a chat-A subscriber; chat A's must.
	bus.Publish(events.EventTypeStreamChunk, map[string]interface{}{"client_id": "sse-client", "chat_id": "chat-b", "content": "b"})
	bus.Publish(events.EventTypeStreamChunk, map[string]interface{}{"client_id": "sse-client", "chat_id": "chat-a", "content": "a"})

	got := readSSEEvents(t, reader, 1, 5*time.Second)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	data, _ := got[0].Data.(map[string]interface{})
	if data["content"] != "a" {
		t.Errorf("subscriber received the wrong chat's event: %#v", data)
	}
}

func TestAgentEventsSSE_RejectsUnauthenticatedWhenTokenConfigured(t *testing.T) {
	srv, _ := sseTestServer(t, "tok-123")
	ts := httptest.NewServer(http.HandlerFunc(srv.handleAPIAgentEvents))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/agent/events?chat_id=chat-a")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET status = %d, want 401", resp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/agent/events?chat_id=chat-a", nil)
	req.Header.Set("Authorization", "Bearer tok-123")
	okResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authorized request: %v", err)
	}
	defer okResp.Body.Close()
	if okResp.StatusCode != http.StatusOK {
		t.Fatalf("authorized GET status = %d, want 200", okResp.StatusCode)
	}
	if ct := okResp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("authorized content-type = %q, want text/event-stream", ct)
	}
}

func TestAgentEventsSSE_UnsubscribesOnClientDisconnect(t *testing.T) {
	srv, bus := sseTestServer(t, "")
	ts := httptest.NewServer(http.HandlerFunc(srv.handleAPIAgentEvents))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/agent/events?chat_id=chat-a", nil)
	req.Header.Set(webClientIDHeader, "sse-client")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	// Wait until the handler has subscribed, then disconnect.
	waitForSubscribers(t, bus, 1)
	cancel()
	_ = resp.Body.Close()

	waitForSubscribers(t, bus, 0)
}

func TestAgentEventsSSE_MethodNotAllowed(t *testing.T) {
	srv, _ := sseTestServer(t, "")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/agent/events", nil)
	srv.handleAPIAgentEvents(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", rec.Code)
	}
}

func TestAgentEventsSSE_ReplaysBufferedEventsAfterSeq(t *testing.T) {
	srv, _ := sseTestServer(t, "")
	// Buffer three events for chat-a directly (as the run-buffer subscriber
	// would), then reconnect with after_seq=1 and expect the tail.
	for _, body := range []string{"one", "two", "three"} {
		srv.publishClientEventWithChat("sse-client", "chat-a", events.EventTypeStreamChunk, map[string]interface{}{"content": body})
	}

	ts := httptest.NewServer(http.HandlerFunc(srv.handleAPIAgentEvents))
	defer ts.Close()
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/agent/events?chat_id=chat-a&after_seq=1", nil)
	req.Header.Set(webClientIDHeader, "sse-client")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer resp.Body.Close()

	// The first frame is the restored control frame; then the two replayed
	// events (seq 2 and 3). Read them in one pass over the body so a second
	// scanner does not lose buffered bytes.
	control, got := readSSERestoredThenEvents(t, bufio.NewReader(resp.Body), 2, 5*time.Second)
	if gap, _ := control["gap"].(bool); gap {
		t.Errorf("unexpected gap for after_seq=1 within the buffer: %#v", control)
	}
	if last, _ := control["last_seq"].(float64); int64(last) != 3 {
		t.Errorf("restored last_seq = %v, want 3", control["last_seq"])
	}
	if len(got) != 2 {
		t.Fatalf("got %d replayed events, want 2", len(got))
	}
	first, _ := got[0].Data.(map[string]interface{})
	if first["content"] != "two" {
		t.Errorf("replay started at %#v, want content=two", first)
	}
}

func TestAgentEventsSSE_EmptyChatScopesToActiveChat(t *testing.T) {
	srv, bus := sseTestServer(t, "")
	ts := httptest.NewServer(http.HandlerFunc(srv.handleAPIAgentEvents))
	defer ts.Close()

	// No chat_id: the client context's active chat is chat-a.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/agent/events", nil)
	req.Header.Set(webClientIDHeader, "sse-client")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)

	bus.Publish(events.EventTypeStreamChunk, map[string]interface{}{"client_id": "sse-client", "chat_id": "chat-b", "content": "b"})
	bus.Publish(events.EventTypeStreamChunk, map[string]interface{}{"client_id": "sse-client", "chat_id": "chat-a", "content": "a"})

	got := readSSEEvents(t, reader, 1, 5*time.Second)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	data, _ := got[0].Data.(map[string]interface{})
	if data["content"] != "a" {
		t.Errorf("empty chat_id did not scope to the active chat: %#v", data)
	}
}

func TestAgentEventsSSE_RejectsHead(t *testing.T) {
	srv, _ := sseTestServer(t, "")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodHead, "/api/agent/events", nil)
	srv.handleAPIAgentEvents(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("HEAD status = %d, want 405", rec.Code)
	}
}

// readSSERestoredThenEvents reads a single pass over the SSE body: it finds
// the `restored` control frame and returns its payload along with up to want
// subsequent agent events. A single pass avoids losing buffered bytes that a
// second scanner over the same reader would drop.
func readSSERestoredThenEvents(t *testing.T, body *bufio.Reader, want int, deadline time.Duration) (map[string]interface{}, []events.UIEvent) {
	t.Helper()
	type result struct {
		control map[string]interface{}
		events  []events.UIEvent
	}
	resCh := make(chan result, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		var res result
		named := ""
		for scanner.Scan() {
			line := scanner.Text()
			if ev, ok := strings.CutPrefix(line, "event: "); ok {
				named = ev
				continue
			}
			if line == "" {
				named = ""
				continue
			}
			payload, ok := strings.CutPrefix(line, "data: ")
			if !ok {
				continue
			}
			if named == "restored" {
				var m map[string]interface{}
				if err := json.Unmarshal([]byte(payload), &m); err != nil {
					return
				}
				res.control = m
				named = ""
				continue
			}
			if named != "" {
				continue
			}
			var ev events.UIEvent
			if err := json.Unmarshal([]byte(payload), &ev); err != nil {
				return
			}
			res.events = append(res.events, ev)
			if res.control != nil && len(res.events) >= want {
				resCh <- res
				return
			}
		}
		resCh <- res
	}()
	select {
	case res := <-resCh:
		if res.control == nil {
			t.Fatal("restored control frame not found")
		}
		return res.control, res.events
	case <-done:
		t.Fatal("restored control frame not found")
	case <-time.After(deadline):
		t.Fatalf("timed out waiting for the restored frame and %d events", want)
	}
	return nil, nil
}

// waitForSubscribers polls the event bus until it has the wanted number of
// subscribers, so a test can synchronize with the handler's subscribe /
// unsubscribe without a sleep.
func waitForSubscribers(t *testing.T, bus *events.EventBus, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if bus.SubscriberCount() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("event bus subscriber count = %d, want %d", bus.SubscriberCount(), want)
}
