package gpulink

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/sprout-foundry/sprout/pkg/runner/relay"
)

const (
	relayMinBackoff = time.Second
	relayMaxBackoff = 30 * time.Second
	// relayStable is how long a tunnel must stay up before a later drop
	// reconnects quickly again instead of backing off further.
	relayStable = time.Minute
)

// ErrLinkRemoved is the platform refusing the key: the link was removed or
// replaced, and only linking again helps.
var ErrLinkRemoved = errors.New("the platform refused this GPU's key: the link was removed; run `sprout gpu link` again")

// enginePaths are the only paths the agent sends to the engine; the platform
// sends no others (docs/runners/GPU-LINK.md §2).
var enginePaths = map[string]bool{
	"/v1/chat/completions": true,
	"/v1/completions":      true,
	"/v1/embeddings":       true,
	"/v1/models":           true,
	"/health":              true,
}

// Agent relays the platform's requests to the engine for one link.
type Agent struct {
	State     *State
	APIKey    string
	EngineKey string
	Log       *slog.Logger
	// Dialer opens the tunnel; nil is the default dialer.
	Dialer *websocket.Dialer
	// Local, when set, serves the requests in-process (the sinter engine)
	// instead of proxying them to State.Engine.
	Local http.Handler
	// Tunnels is how many tunnels the agent keeps open (0 means one). A
	// fleet GPU keeps two, each to a different platform replica when it can,
	// so a platform deploy never leaves it unreachable.
	Tunnels int
	// ReloadKey, when set, reads the stored key again: a key the platform
	// refuses is retried with the stored one when that differs (rotated by
	// `sprout gpu rotate-key` while this agent ran).
	ReloadKey func() (string, error)
	// RotateEvery, when set, is how often the agent rotates its key, with
	// SaveKey storing the new one and OnRotated recording when; RotatedAt is
	// when it last did.
	RotateEvery time.Duration
	RotatedAt   time.Time
	SaveKey     func(string) error
	OnRotated   func(time.Time)

	stats statsCollector

	mu   sync.Mutex
	live map[int]*liveTunnel
}

// liveTunnel is one of the agent's open tunnels and the replica it reached.
type liveTunnel struct {
	mux     *relay.Mux
	replica string
}

// replicaHeader names, on the tunnel's upgrade response, the platform
// replica the tunnel reached.
const replicaHeader = "X-Relay-Replica"

// spreadTries bounds how often a tunnel that reached the same replica as
// another one dials again before it settles for it.
const spreadTries = 3

func (a *Agent) key() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.APIKey
}

func (a *Agent) setKey(k string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.APIKey = k
}

// tunnelURL is the platform's tunnel endpoint for the link.
func tunnelURL(platformURL, linkID string) (string, error) {
	u, err := url.Parse(strings.TrimRight(platformURL, "/") + "/internal/gpu/tunnel")
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	}
	u.RawQuery = url.Values{"link_id": {linkID}}.Encode()
	return u.String(), nil
}

// Handler serves one relayed request from the engine: model-serving paths
// only, to the declared engine only, with the platform's Authorization
// dropped and the engine's own key added when there is one.
func (a *Agent) Handler() relay.Handler {
	target := &url.URL{Scheme: "http", Host: a.State.Engine}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			path := pr.In.URL.Path
			pr.SetURL(target)
			pr.Out.URL.Path = path
			pr.Out.URL.RawPath = ""
			pr.Out.Host = target.Host
			pr.Out.Header.Del("Authorization")
			pr.Out.Header.Del("Cookie")
			if a.EngineKey != "" {
				pr.Out.Header.Set("Authorization", "Bearer "+a.EngineKey)
			}
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			a.logger().Warn("engine request failed", "engine", a.State.Engine, "err", err)
			http.Error(w, "the GPU's engine is not answering at "+a.State.Engine, http.StatusBadGateway)
		},
	}
	return func(_ string, w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == StatsPath {
			// The agent's own metrics endpoint: answered here, never sent
			// to the engine.
			a.serveStats(w, r)
			return
		}
		if !enginePaths[r.URL.Path] {
			http.NotFound(w, r)
			return
		}
		if a.Local != nil {
			r.Header.Del("Authorization")
			a.Local.ServeHTTP(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	}
}

func (a *Agent) logger() *slog.Logger {
	if a.Log != nil {
		return a.Log
	}
	return slog.Default()
}

// Run keeps the agent's tunnels open, each reconnecting with backoff, until
// ctx ends or the platform refuses the key (ErrLinkRemoved).
func (a *Agent) Run(ctx context.Context) error {
	target, err := tunnelURL(a.State.PlatformURL, a.State.LinkID)
	if err != nil {
		return err
	}
	n := a.Tunnels
	if n < 1 {
		n = 1
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	handler := a.Handler()
	errc := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) { errc <- a.runTunnel(ctx, i, n, target, handler) }(i)
	}
	if a.RotateEvery > 0 && a.SaveKey != nil {
		go a.rotateLoop(ctx)
	}
	var first error
	for i := 0; i < n; i++ {
		if err := <-errc; err != nil && first == nil {
			first = err
			cancel()
		}
	}
	return first
}

// runTunnel keeps tunnel i of n open.
func (a *Agent) runTunnel(ctx context.Context, i, n int, target string, handler relay.Handler) error {
	dialer := a.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	log := a.logger().With("tunnel", i+1)
	backoff := relayMinBackoff
	spread := 0
	for ctx.Err() == nil {
		key := a.key()
		conn, resp, err := dialer.DialContext(ctx, target, http.Header{"X-GPU-Key": {key}})
		status, replica := 0, ""
		if resp != nil {
			status = resp.StatusCode
			replica = resp.Header.Get(replicaHeader)
			if resp.Body != nil {
				_ = resp.Body.Close()
			}
		}
		if status == http.StatusUnauthorized {
			if a.ReloadKey != nil {
				if stored, rerr := a.ReloadKey(); rerr == nil && stored != "" && stored != key {
					log.Info("the platform refused the key in use; retrying with the stored one")
					a.setKey(stored)
					continue
				}
			}
			return ErrLinkRemoved
		}
		if err == nil && n > 1 && replica != "" && spread < spreadTries && a.replicaInUse(i, replica) {
			// Both tunnels on one replica would both end in its deploy:
			// dial again for another.
			spread++
			_ = conn.Close()
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(500 * time.Millisecond):
			}
			continue
		}
		spread = 0
		if err != nil {
			log.Warn("tunnel connect failed", "err", err, "retry_in", backoff)
		} else {
			log.Info("connected to the platform", "engine", a.State.Engine)
			started := time.Now()
			mux := relay.NewMux(conn, handler)
			a.setLive(i, &liveTunnel{mux: mux, replica: replica})
			err = mux.Serve(ctx)
			a.setLive(i, nil)
			if ctx.Err() != nil {
				return nil
			}
			log.Warn("tunnel disconnected", "err", err)
			if time.Since(started) > relayStable {
				backoff = relayMinBackoff
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > relayMaxBackoff {
			backoff = relayMaxBackoff
		}
	}
	return nil
}

func (a *Agent) setLive(i int, t *liveTunnel) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.live == nil {
		a.live = map[int]*liveTunnel{}
	}
	if t == nil {
		delete(a.live, i)
		return
	}
	a.live[i] = t
}

// replicaInUse reports whether another of the agent's tunnels is open to
// replica.
func (a *Agent) replicaInUse(i int, replica string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for j, t := range a.live {
		if j != i && t.replica == replica {
			return true
		}
	}
	return false
}

// liveMux is tunnel i's open connection, or nil.
func (a *Agent) liveMux(i int) *relay.Mux {
	a.mu.Lock()
	defer a.mu.Unlock()
	if t := a.live[i]; t != nil {
		return t.mux
	}
	return nil
}

// rotateLoop rotates the key once it is RotateEvery old.
func (a *Agent) rotateLoop(ctx context.Context) {
	tick := time.NewTicker(time.Hour)
	defer tick.Stop()
	for {
		if time.Since(a.RotatedAt) >= a.RotateEvery {
			if err := a.RotateKey(ctx); err != nil && ctx.Err() == nil {
				a.logger().Warn("rotating the GPU link key failed; trying again in an hour", "err", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// RotateKey trades the key for a new one, stores it, and reconnects each
// tunnel with it in turn, so one stays open while the other moves over. The
// platform keeps the old key working until the new one connects, so a key
// that could not be stored is simply not used.
func (a *Agent) RotateKey(ctx context.Context) error {
	next, err := RotateKey(ctx, a.State.PlatformURL, a.State.LinkID, a.key())
	if err != nil {
		return err
	}
	if err := a.SaveKey(next); err != nil {
		return fmt.Errorf("storing the rotated key: %w", err)
	}
	a.setKey(next)
	now := time.Now().UTC()
	a.RotatedAt = now
	if a.OnRotated != nil {
		a.OnRotated(now)
	}
	a.logger().Info("rotated the GPU link key")
	n := a.Tunnels
	if n < 1 {
		n = 1
	}
	for i := 0; i < n; i++ {
		m := a.liveMux(i)
		if m == nil {
			continue
		}
		m.Close()
		// Wait for this tunnel to be back before moving the next one.
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) && ctx.Err() == nil {
			if cur := a.liveMux(i); cur != nil && cur != m {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	return nil
}
