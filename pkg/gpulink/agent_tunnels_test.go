package gpulink

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/sprout-foundry/sprout/pkg/runner/relay"
)

// replicaPlatform is a fake platform whose tunnel connections land on
// "replicas" in turn (replicaFor picks the tag), with the accepted keys and
// a key rotation endpoint.
type replicaPlatform struct {
	srv        *httptest.Server
	mu         sync.Mutex
	keys       map[string]bool
	connected  []string // the key of each accepted connection
	replicaFor func(n int) string
	dials      atomic.Int32
	rotated    string
}

func newReplicaPlatform(t *testing.T, key string, replicaFor func(n int) string) *replicaPlatform {
	t.Helper()
	p := &replicaPlatform{keys: map[string]bool{key: true}, replicaFor: replicaFor}
	up := websocket.Upgrader{}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-GPU-Key")
		p.mu.Lock()
		ok := p.keys[key]
		p.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/internal/gpu/rotate-key":
			p.mu.Lock()
			p.rotated = "sfg_rotated"
			p.keys["sfg_rotated"] = true
			p.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"api_key": "sfg_rotated"})
			return
		case "/internal/gpu/tunnel":
		default:
			http.NotFound(w, r)
			return
		}
		n := int(p.dials.Add(1))
		conn, err := up.Upgrade(w, r, http.Header{replicaHeader: {p.replicaFor(n)}})
		if err != nil {
			return
		}
		p.mu.Lock()
		p.connected = append(p.connected, key)
		p.mu.Unlock()
		_ = relay.NewMux(conn, nil).Serve(r.Context())
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *replicaPlatform) connections() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.connected...)
}

// Two tunnels that land on the same replica dial again until they reach
// different ones.
func TestRun_TwoTunnelsSpreadAcrossReplicas(t *testing.T) {
	// The first three dials reach replica "a"; later ones "b".
	p := newReplicaPlatform(t, "sfg_key", func(n int) string {
		if n <= 3 {
			return "a"
		}
		return "b"
	})
	a := &Agent{State: &State{PlatformURL: p.srv.URL, LinkID: "link-1", Engine: DefaultEngine}, APIKey: "sfg_key", Tunnels: 2}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() { _ = a.Run(ctx) }()

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		replicas := map[string]bool{}
		for _, t := range a.live {
			replicas[t.replica] = true
		}
		n := len(a.live)
		a.mu.Unlock()
		if n == 2 && replicas["a"] && replicas["b"] {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the two tunnels never reached two replicas (dials: %d)", p.dials.Load())
}

// A refused key is retried with the stored one before giving up.
func TestRun_RetriesWithTheStoredKey(t *testing.T) {
	p := newReplicaPlatform(t, "sfg_new", func(int) string { return "a" })
	a := &Agent{State: &State{PlatformURL: p.srv.URL, LinkID: "link-1", Engine: DefaultEngine}, APIKey: "sfg_old",
		ReloadKey: func() (string, error) { return "sfg_new", nil }}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { _ = a.Run(ctx) }()
	for ctx.Err() == nil && len(p.connections()) == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if got := p.connections(); len(got) == 0 || got[0] != "sfg_new" {
		t.Fatalf("connections = %v, want one with the stored key", got)
	}

	// With nothing better stored, a refused key stops the agent.
	b := &Agent{State: &State{PlatformURL: p.srv.URL, LinkID: "link-1", Engine: DefaultEngine}, APIKey: "sfg_gone",
		ReloadKey: func() (string, error) { return "sfg_gone", nil }}
	if err := b.Run(ctx); !errors.Is(err, ErrLinkRemoved) {
		t.Fatalf("Run = %v, want ErrLinkRemoved", err)
	}
}

// Rotating stores the new key, then moves each tunnel over to it.
func TestRotateKey_StoresThenReconnectsWithTheNewKey(t *testing.T) {
	p := newReplicaPlatform(t, "sfg_key", func(n int) string { return []string{"a", "b"}[n%2] })
	var saved string
	var rotatedAt time.Time
	a := &Agent{State: &State{PlatformURL: p.srv.URL, LinkID: "link-1", Engine: DefaultEngine}, APIKey: "sfg_key", Tunnels: 2,
		SaveKey: func(k string) error { saved = k; return nil }, OnRotated: func(at time.Time) { rotatedAt = at }}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() { _ = a.Run(ctx) }()
	for ctx.Err() == nil && (a.liveMux(0) == nil || a.liveMux(1) == nil) {
		time.Sleep(20 * time.Millisecond)
	}
	if err := a.RotateKey(ctx); err != nil {
		t.Fatalf("RotateKey: %v", err)
	}
	if saved != "sfg_rotated" || rotatedAt.IsZero() {
		t.Fatalf("saved %q at %v, want the rotated key stored", saved, rotatedAt)
	}
	for ctx.Err() == nil {
		n := 0
		for _, k := range p.connections() {
			if k == "sfg_rotated" {
				n++
			}
		}
		if n >= 2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("connections = %v, want both tunnels back on the rotated key", p.connections())
}

func TestRotateKey_StoreFailureKeepsTheOldKey(t *testing.T) {
	p := newReplicaPlatform(t, "sfg_key", func(int) string { return "a" })
	a := &Agent{State: &State{PlatformURL: p.srv.URL, LinkID: "link-1"}, APIKey: "sfg_key",
		SaveKey: func(string) error { return errors.New("keyring locked") }}
	if err := a.RotateKey(context.Background()); err == nil || !strings.Contains(err.Error(), "keyring locked") {
		t.Fatalf("RotateKey = %v, want the store error", err)
	}
	if a.key() != "sfg_key" {
		t.Fatalf("key = %q, want the old key still in use", a.key())
	}
}
