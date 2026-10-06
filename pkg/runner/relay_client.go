package runner

import (
	"context"
	"net/http"
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

// tunnelURL is the platform's relay endpoint for this runner.
func tunnelURL(platformURL, runnerID string) (string, error) {
	u, err := url.Parse(strings.TrimRight(platformURL, "/") + "/internal/runner/tunnel")
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	}
	u.RawQuery = url.Values{"runner_id": {runnerID}}.Encode()
	return u.String(), nil
}

// relayLoop keeps a tunnel to the platform open, reconnecting with backoff,
// and serves the platform's calls through the host server's handler — so
// relayed calls pass the same per-workspace secret check as direct ones.
func (r *Runner) relayLoop(ctx context.Context) {
	target, err := tunnelURL(r.State.PlatformURL, r.State.RunnerID)
	if err != nil {
		r.Log.Error("bad platform URL for the relay", "err", err)
		return
	}
	host := r.Host.Handler()
	serve := func(ws string, w http.ResponseWriter, req *http.Request) {
		req.URL.Path = "/daemon/" + ws + req.URL.Path
		req.URL.RawPath = ""
		host.ServeHTTP(w, req)
	}
	backoff := relayMinBackoff
	for ctx.Err() == nil {
		hdr := http.Header{"X-Runner-Key": {r.Client.Creds.APIKey}}
		conn, resp, err := websocket.DefaultDialer.DialContext(ctx, target, hdr)
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if err != nil {
			r.Log.Warn("relay connect failed", "err", err, "retry_in", backoff)
		} else {
			r.Log.Info("relay connected")
			started := time.Now()
			mux := relay.NewMux(conn, serve)
			err = mux.Serve(ctx)
			if ctx.Err() != nil {
				return
			}
			r.Log.Warn("relay disconnected", "err", err)
			if time.Since(started) > relayStable {
				backoff = relayMinBackoff
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > relayMaxBackoff {
			backoff = relayMaxBackoff
		}
	}
}
