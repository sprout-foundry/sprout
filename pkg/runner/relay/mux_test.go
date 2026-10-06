package relay

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// pair connects a "platform" mux (accepts, opens streams) to a "runner" mux
// (dials, serves streams with handler) over a real WebSocket.
func pair(t *testing.T, handler Handler) (platform *Mux, runner *Mux) {
	t.Helper()
	accepted := make(chan *Mux, 1)
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		m := NewMux(conn, nil)
		accepted <- m
		_ = m.Serve(context.Background())
	}))
	t.Cleanup(srv.Close)

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	runner = NewMux(conn, handler)
	go func() { _ = runner.Serve(context.Background()) }()
	platform = <-accepted
	t.Cleanup(func() { platform.Close(); runner.Close() })
	return platform, runner
}

func client(m *Mux, ws string) *http.Client {
	return &http.Client{Transport: m.RoundTripper(ws)}
}

func TestRoundTripCarriesRequestAndResponse(t *testing.T) {
	platform, _ := pair(t, func(ws string, w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Seen", ws+" "+r.Method+" "+r.URL.RequestURI()+" "+r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write(append([]byte("echo:"), body...))
	})
	req, _ := http.NewRequest(http.MethodPost, "http://relay/api/txn/run?x=1", strings.NewReader(`{"command":"make"}`))
	req.Header.Set("Authorization", "Bearer s3cret")
	resp, err := client(platform, "ws-1").Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusTeapot || string(body) != `echo:{"command":"make"}` {
		t.Fatalf("got %d %q", resp.StatusCode, body)
	}
	if got := resp.Header.Get("X-Seen"); got != "ws-1 POST /api/txn/run?x=1 Bearer s3cret" {
		t.Errorf("handler saw %q", got)
	}
}

func TestLargeBodiesBothWaysRespectTheWindow(t *testing.T) {
	const size = 10 << 20 // well past the 4 MiB window
	platform, _ := pair(t, func(_ string, w http.ResponseWriter, r *http.Request) {
		h := sha256.New()
		n, _ := io.Copy(h, r.Body)
		w.Header().Set("X-Got", strconv.FormatInt(n, 10))
		w.Header().Set("X-Sum", hex.EncodeToString(h.Sum(nil)))
		_, _ = io.CopyN(w, rand.Reader, size)
	})
	payload := make([]byte, size)
	_, _ = rand.Read(payload)
	want := sha256.Sum256(payload)
	resp, err := client(platform, "ws").Post("http://relay/api/txn/push", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	n, err := io.Copy(io.Discard, resp.Body)
	if err != nil || n != size {
		t.Fatalf("response: read %d bytes, err %v", n, err)
	}
	if resp.Header.Get("X-Got") != strconv.Itoa(size) || resp.Header.Get("X-Sum") != hex.EncodeToString(want[:]) {
		t.Errorf("request body corrupted in transit: got %s bytes", resp.Header.Get("X-Got"))
	}
}

func TestStreamsDoNotBlockEachOther(t *testing.T) {
	release := make(chan struct{})
	platform, _ := pair(t, func(ws string, w http.ResponseWriter, r *http.Request) {
		if ws == "slow" {
			<-release
		}
		_, _ = io.WriteString(w, ws)
	})
	slowDone := make(chan struct{})
	go func() {
		defer close(slowDone)
		resp, err := client(platform, "slow").Get("http://relay/x")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client(platform, "fast").Get("http://relay/x")
			if err != nil {
				t.Error(err)
				return
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if string(body) != "fast" {
				t.Errorf("got %q", body)
			}
		}()
	}
	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("fast streams blocked behind a slow one")
	}
	close(release)
	<-slowDone
}

func TestCancellationReachesTheHandler(t *testing.T) {
	handlerCanceled := make(chan struct{})
	platform, _ := pair(t, func(_ string, w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		<-r.Context().Done()
		close(handlerCanceled)
	})
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://relay/long", nil)
	resp, err := client(platform, "ws").Do(req)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	_ = resp.Body.Close()
	select {
	case <-handlerCanceled:
	case <-time.After(5 * time.Second):
		t.Fatal("closing the response early must cancel the runner-side handler")
	}
}

func TestConnectionLossFailsInFlightStreams(t *testing.T) {
	started := make(chan struct{})
	platform, runner := pair(t, func(_ string, w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		close(started)
		<-r.Context().Done()
	})
	resp, err := client(platform, "ws").Get("http://relay/x")
	if err != nil {
		t.Fatal(err)
	}
	<-started
	runner.Close()
	done := make(chan error, 1)
	go func() { _, err := io.ReadAll(resp.Body); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a dropped tunnel must surface as an error, not a clean EOF")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reading a stream on a dropped tunnel hung")
	}
	if _, err := client(platform, "ws").Get("http://relay/y"); err == nil {
		t.Error("new requests on a closed tunnel must fail")
	}
}

func TestStreamsWithoutAHandlerAreRefused(t *testing.T) {
	platform, _ := pair(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://relay/x", nil)
	if _, err := client(platform, "ws").Do(req); err == nil || ctx.Err() != nil {
		t.Fatalf("want a prompt refusal, got err=%v ctxErr=%v", err, ctx.Err())
	}
}
