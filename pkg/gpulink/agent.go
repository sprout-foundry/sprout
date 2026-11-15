package gpulink

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
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
		if !enginePaths[r.URL.Path] {
			http.NotFound(w, r)
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

// Run keeps the tunnel open, reconnecting with backoff, until ctx ends or
// the platform refuses the key (ErrLinkRemoved).
func (a *Agent) Run(ctx context.Context) error {
	target, err := tunnelURL(a.State.PlatformURL, a.State.LinkID)
	if err != nil {
		return err
	}
	dialer := a.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	handler := a.Handler()
	log := a.logger()
	backoff := relayMinBackoff
	for ctx.Err() == nil {
		conn, resp, err := dialer.DialContext(ctx, target, http.Header{"X-GPU-Key": {a.APIKey}})
		status := 0
		if resp != nil {
			status = resp.StatusCode
			if resp.Body != nil {
				_ = resp.Body.Close()
			}
		}
		if status == http.StatusUnauthorized {
			return ErrLinkRemoved
		}
		if err != nil {
			log.Warn("tunnel connect failed", "err", err, "retry_in", backoff)
		} else {
			log.Info("connected to the platform", "engine", a.State.Engine)
			started := time.Now()
			err = relay.NewMux(conn, handler).Serve(ctx)
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
