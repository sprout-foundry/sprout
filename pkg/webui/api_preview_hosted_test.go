//go:build !js

package webui

// SP-155 §155a (TODO 155.5): the webui server's hosted-preview half. The
// agent's register_preview_port tool publishes preview_port_registered on
// the shared bus; the server's subscriber records the platform URL so
// /api/preview/* reports it (running + hosted) in preference to any local
// dev server. These tests stub the published event (no platform involved)
// and drive the real handlers the way the neighboring api_preview tests do.

import (
	"net/http"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/preview"
)

// publishHostedPreview fires a stub preview_port_registered event on the
// server's bus (the shape the register_preview_port tool publishes).
func publishHostedPreview(t *testing.T, ws *ReactWebServer, url string, port int, label string) {
	t.Helper()
	ws.eventBus.Publish(events.EventTypePreviewPortRegistered, map[string]any{
		"preview_url": url,
		"port":        port,
		"label":       label,
	})
}

// waitForHostedStatus polls the status handler until the active hosted
// preview carries wantURL (the subscriber is a goroutine, so state settles
// a tick after the publish), or the deadline elapses.
func waitForHostedStatus(t *testing.T, ws *ReactWebServer, wantURL string) previewStateJSON {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var body previewStateJSON
	for {
		body = previewStatus(t, ws)
		if body.Hosted && body.URL == wantURL {
			return body
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for hosted URL %q (last: %+v)", wantURL, body)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// hostedTestServer builds the standard preview test server with the hosted
// subscriber running (the production wiring, minus the Start() lifecycle).
func hostedTestServer(t *testing.T) *ReactWebServer {
	t.Helper()
	ws, root := newPreviewTestServer(t)
	_ = root
	ws.startHostedPreviewSubscriber()
	return ws
}

func TestHostedPreview_StatusPrecedence(t *testing.T) {
	t.Run("a registered hosted preview is reported running with the hosted URL", func(t *testing.T) {
		ws := hostedTestServer(t)
		publishHostedPreview(t, ws, "https://preview.example/ws1/8080", 8080, "dev server")

		body := waitForHostedStatus(t, ws, "https://preview.example/ws1/8080")
		if body.Status != string(preview.StatusRunning) {
			t.Fatalf("expected running, got %+v", body)
		}
		if !body.Hosted {
			t.Errorf("expected hosted=true, got %+v", body)
		}
		if body.Detected {
			t.Errorf("a hosted preview is not a detected local server, got %+v", body)
		}
	})

	t.Run("a second registration supersedes the first", func(t *testing.T) {
		ws := hostedTestServer(t)
		publishHostedPreview(t, ws, "https://preview.example/ws1/8080", 8080, "dev server")
		waitForHostedStatus(t, ws, "https://preview.example/ws1/8080")

		publishHostedPreview(t, ws, "https://preview.example/ws1/9000", 9000, "other app")
		body := waitForHostedStatus(t, ws, "https://preview.example/ws1/9000")
		if body.URL != "https://preview.example/ws1/9000" {
			t.Fatalf("the newest registration must win, got %+v", body)
		}
	})

	t.Run("without a registration the local manager state is reported", func(t *testing.T) {
		ws := hostedTestServer(t)
		// No event published: the hosted state is empty, so the status falls
		// back to the local dev-server manager (stopped, with the reason).
		body := previewStatus(t, ws)
		if body.Hosted {
			t.Fatalf("no hosted preview was registered, got %+v", body)
		}
		if body.Status != string(preview.StatusStopped) {
			t.Fatalf("expected the local manager's stopped state, got %+v", body)
		}
	})
}

func TestHostedPreview_ActionsEchoHostedState(t *testing.T) {
	// A valid local manifest is present (so a naive start would spawn a dev
	// server); the active hosted registration must short-circuit all three
	// actions to the hosted state instead.
	ws, root := newPreviewTestServer(t)
	ws.startHostedPreviewSubscriber()
	port := freePreviewPort(t)
	writePreviewManifest(t, root, "npm run dev", port)

	const hostedURL = "https://preview.example/ws1/8080"
	publishHostedPreview(t, ws, hostedURL, 8080, "dev server")
	waitForHostedStatus(t, ws, hostedURL)

	check := func(action func(http.ResponseWriter, *http.Request), path string) {
		t.Helper()
		body := previewAction(t, ws, action, path, http.StatusOK)
		if !body.Hosted {
			t.Errorf("%s: expected the hosted state, got %+v", path, body)
		}
		if body.Status != string(preview.StatusRunning) {
			t.Errorf("%s: expected running, got %+v", path, body)
		}
		if body.URL != hostedURL {
			t.Errorf("%s: expected the hosted URL, got %+v", path, body)
		}
	}
	check(ws.handleAPIPreviewStart, "/api/preview/start")
	check(ws.handleAPIPreviewRestart, "/api/preview/restart")
	check(ws.handleAPIPreviewStop, "/api/preview/stop")
}

func TestHostedPreview_ClearForgetsState(t *testing.T) {
	ws := hostedTestServer(t)
	publishHostedPreview(t, ws, "https://preview.example/ws1/8080", 8080, "dev server")
	waitForHostedStatus(t, ws, "https://preview.example/ws1/8080")

	ws.clearHostedPreview()

	body := previewStatus(t, ws)
	if body.Hosted || body.URL != "" {
		t.Fatalf("clear must forget the hosted preview, got %+v", body)
	}
	if body.Status != string(preview.StatusStopped) {
		t.Fatalf("after clear the local manager state is reported, got %+v", body)
	}
}
